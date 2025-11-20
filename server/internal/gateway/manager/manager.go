package manager

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/relay"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
)

// Manager orchestrates tab creation, relay lifecycle, and persistence.
type Manager struct {
	projects          map[string]gatewayconfig.Project
	store             tabs.Store
	dialer            *websocket.Dialer
	mu                sync.Mutex
	tabs              map[string]*tabEntry
	statusCb          func(tabs.Tab)
	idleTimeout       time.Duration // Tab idle timeout duration
	idleWarningBefore time.Duration // Warning time before idle timeout
	stopIdleChecker   chan struct{} // Signal to stop idle checker goroutine
}

type tabEntry struct {
	relay         *relay.Relay
	clientID      string
	project       gatewayconfig.Project
	created       time.Time
	lastActivity  time.Time // Last activity timestamp for idle timeout tracking
	warned        bool      // Whether idle warning has been sent
	downstreamURI string
	cancel        context.CancelFunc
}

// New creates a manager with the given idle timeout configuration.
func New(cat gatewayconfig.Catalog, store tabs.Store, idleTimeout time.Duration) *Manager {
	projects := make(map[string]gatewayconfig.Project, len(cat.Projects))
	for _, p := range cat.Projects {
		projects[p.ID] = p
	}

	// Validate minimum idle timeout (10 minutes)
	if idleTimeout < 10*time.Minute {
		log.Printf("gateway: idle timeout %v is below minimum 10m, enforcing minimum", idleTimeout)
		idleTimeout = 10 * time.Minute
	}

	return &Manager{
		projects:          projects,
		store:             store,
		dialer:            websocket.DefaultDialer,
		tabs:              make(map[string]*tabEntry),
		idleTimeout:       idleTimeout,
		idleWarningBefore: 5 * time.Minute, // Fixed: warn 5 minutes before timeout
		stopIdleChecker:   make(chan struct{}),
	}
}

// CreateTab allocates metadata and starts a relay (if not already running).
func (m *Manager) CreateTab(ctx context.Context, projectID, clientID string) (tabs.Tab, error) {
	project, ok := m.projects[projectID]
	if !ok {
		return tabs.Tab{}, fmt.Errorf("unknown project %q", projectID)
	}

	// Enforce per-client tab limit if configured (0 means unlimited)
	// Note: There is a small race window between count check and tab creation
	// where concurrent requests could exceed the limit. This is acceptable given:
	// - Rare occurrence (same client creating multiple tabs simultaneously)
	// - Small window (milliseconds)
	// - Soft limit nature (advisory, not security-critical)
	// To fully prevent this would require database-level constraints or
	// SELECT FOR UPDATE transactions, adding complexity for minimal benefit.
	if project.Limits.MaxTabsPerClient > 0 {
		count, err := m.store.CountByClientAndProject(ctx, clientID, projectID)
		if err != nil {
			return tabs.Tab{}, fmt.Errorf("check tab limit: %w", err)
		}
		if count >= project.Limits.MaxTabsPerClient {
			return tabs.Tab{}, &TabLimitExceededError{
				ProjectID: projectID,
				Limit:     project.Limits.MaxTabsPerClient,
			}
		}
	}

	id := uuid.NewString()
	e := m.newEntry(project, clientID, time.Now())

	m.mu.Lock()
	m.tabs[id] = e
	m.mu.Unlock()

	tab := tabs.Tab{
		TabID:         id,
		ProjectID:     projectID,
		ClientID:      clientID,
		Status:        tabs.StatusConnecting,
		CreatedAt:     e.created,
		UpdatedAt:     e.created,
		DownstreamURI: &e.downstreamURI,
	}
	if err := m.store.Create(ctx, tab); err != nil {
		return tabs.Tab{}, err
	}
	m.startTracking(id, e)
	return tab, nil
}

// Attach proxies between the caller WebSocket and the downstream relay.
func (m *Manager) Attach(ctx context.Context, tabID, clientID string, upstream *websocket.Conn) error {
	m.mu.Lock()
	e, ok := m.tabs[tabID]
	m.mu.Unlock()
	if !ok {
		return tabs.ErrNotFound
	}
	if e.clientID != clientID {
		return fmt.Errorf("tab %s owned by another client", tabID)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return e.relay.Proxy(ctx, upstream)
}

// CloseTab tears down the relay and removes persisted metadata.
func (m *Manager) CloseTab(ctx context.Context, tabID string) error {
	m.mu.Lock()
	e, ok := m.tabs[tabID]
	if ok {
		delete(m.tabs, tabID)
	}
	m.mu.Unlock()
	if ok {
		if e.cancel != nil {
			e.cancel()
		}
		_ = e.relay.Close()
	}
	if err := m.store.Delete(ctx, tabID); err != nil {
		return err
	}
	if m.statusCb != nil && ok {
		payload := tabs.Tab{
			TabID:         tabID,
			ProjectID:     e.project.ID,
			ClientID:      e.clientID,
			Status:        tabs.StatusClosed,
			CreatedAt:     e.created,
			UpdatedAt:     time.Now(),
			DownstreamURI: &e.downstreamURI,
		}
		m.statusCb(payload)
	}
	return nil
}

// ListProjects returns the current catalog.
func (m *Manager) ListProjects() []gatewayconfig.Project {
	result := make([]gatewayconfig.Project, 0, len(m.projects))
	for _, p := range m.projects {
		result = append(result, p)
	}
	return result
}

// RestoreTabs loads persisted rows at startup so clients can reconnect.
func (m *Manager) RestoreTabs(ctx context.Context) error {
	rows, err := m.store.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		project, ok := m.projects[row.ProjectID]
		if !ok {
			log.Printf("gateway: skipping tab %s for unknown project %s", row.TabID, row.ProjectID)
			continue
		}
		entry := m.newEntry(project, row.ClientID, row.CreatedAt)
		m.mu.Lock()
		m.tabs[row.TabID] = entry
		m.mu.Unlock()
		m.startTracking(row.TabID, entry)
	}
	return nil
}

func (m *Manager) newEntry(project gatewayconfig.Project, clientID string, created time.Time) *tabEntry {
	endpoint := &url.URL{
		Scheme: "ws",
		Host:   fmt.Sprintf("%s.%s.svc:%d", project.Service, project.Namespace, project.Port),
		Path:   "/ws",
	}
	uri := endpoint.String()
	rel := relay.New(relay.Config{ProjectID: project.ID, Endpoint: endpoint, Dialer: m.dialer, Headers: http.Header{"X-Kubetty-Project": []string{project.ID}}, DownstreamURI: uri})
	return &tabEntry{
		relay:         rel,
		clientID:      clientID,
		project:       project,
		created:       created,
		lastActivity:  time.Now(), // Initialize with current time
		warned:        false,
		downstreamURI: uri,
	}
}

func (m *Manager) startTracking(tabID string, entry *tabEntry) {
	ctx, cancel := context.WithCancel(context.Background())
	entry.cancel = cancel
	go m.watchStatus(ctx, tabID, entry, entry.relay.Subscribe())
	go m.watchActivity(ctx, tabID, entry, entry.relay.ActivityChan())
}

// watchActivity monitors relay activity and updates the lastActivity timestamp.
func (m *Manager) watchActivity(ctx context.Context, tabID string, entry *tabEntry, activityCh <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-activityCh:
			m.recordActivity(tabID)
		}
	}
}

// recordActivity updates the lastActivity timestamp for a tab and clears the warned flag.
// Safe to call even if tab has been deleted - will simply be a no-op.
func (m *Manager) recordActivity(tabID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.tabs[tabID]
	if !ok {
		// Tab was deleted, activity signal is stale - ignore safely
		return
	}
	entry.lastActivity = time.Now()
	entry.warned = false // Clear warning state on activity
}

func (m *Manager) watchStatus(ctx context.Context, tabID string, entry *tabEntry, ch <-chan relay.StatusEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-ch:
			status := toTabStatus(evt.Status)
			if status == "" {
				continue
			}
			var errStr *string
			if evt.Err != nil {
				msg := evt.Err.Error()
				errStr = &msg
			}
			downURI := entry.downstreamURI
			if err := m.store.UpdateStatus(ctx, tabID, status, errStr, &downURI); err != nil && !errors.Is(err, tabs.ErrNotFound) {
				log.Printf("gateway: update tab %s status: %v", tabID, err)
			} else if m.statusCb != nil {
				payload := tabs.Tab{
					TabID:         tabID,
					ProjectID:     entry.project.ID,
					ClientID:      entry.clientID,
					Status:        status,
					CreatedAt:     entry.created,
					UpdatedAt:     time.Now(),
					DownstreamURI: &downURI,
					LastError:     errStr,
				}
				m.statusCb(payload)
			}
		}
	}
}

// SetStatusCallback registers a callback invoked on status changes.
func (m *Manager) SetStatusCallback(cb func(tabs.Tab)) {
	m.statusCb = cb
}

// StartIdleChecker begins monitoring tabs for idle timeout.
// Should be called after RestoreTabs() during gateway startup.
func (m *Manager) StartIdleChecker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	log.Printf("gateway: starting idle checker (timeout=%v, warning=%v)", m.idleTimeout, m.idleWarningBefore)

	for {
		select {
		case <-ctx.Done():
			log.Printf("gateway: idle checker stopped (context cancelled)")
			return
		case <-m.stopIdleChecker:
			log.Printf("gateway: idle checker stopped (shutdown signal)")
			return
		case <-ticker.C:
			m.checkIdleTabs(ctx)
		}
	}
}

// Stop gracefully shuts down the manager and idle checker.
func (m *Manager) Stop() {
	close(m.stopIdleChecker)
}

// checkIdleTabs scans all tabs and handles idle warnings and closures.
func (m *Manager) checkIdleTabs(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for tabID, entry := range m.tabs {
		idleDuration := now.Sub(entry.lastActivity)

		// Tab has exceeded idle timeout - close it
		if idleDuration >= m.idleTimeout {
			log.Printf("gateway: tab %s idle for %v (timeout=%v), closing", tabID, idleDuration, m.idleTimeout)
			m.closeIdleTab(ctx, tabID, entry)
			continue
		}

		// Tab approaching idle timeout - send warning
		warningThreshold := m.idleTimeout - m.idleWarningBefore
		if idleDuration >= warningThreshold && !entry.warned {
			remaining := m.idleTimeout - idleDuration
			log.Printf("gateway: tab %s idle for %v, sending warning (remaining=%v)", tabID, idleDuration, remaining)
			m.sendIdleWarning(tabID, entry, remaining)
			entry.warned = true
		}
	}
}

// closeIdleTab closes a tab due to idle timeout (called with mutex held).
// Releases mutex before calling external methods to avoid deadlock.
func (m *Manager) closeIdleTab(ctx context.Context, tabID string, entry *tabEntry) {
	// Extract data needed for cleanup while mutex is held
	delete(m.tabs, tabID)
	cancel := entry.cancel
	relay := entry.relay
	project := entry.project
	clientID := entry.clientID
	created := entry.created
	downstreamURI := entry.downstreamURI
	statusCb := m.statusCb

	// Release mutex before calling external methods to avoid deadlock
	m.mu.Unlock()

	// Cancel relay context
	if cancel != nil {
		cancel()
	}

	// Close relay (may acquire relay mutex)
	_ = relay.Close()

	// Delete from database (spawn goroutine to avoid blocking)
	go func() {
		if err := m.store.Delete(ctx, tabID); err != nil {
			log.Printf("gateway: delete idle tab %s: %v", tabID, err)
		}
	}()

	// Send closure event (may call user callback)
	if statusCb != nil {
		payload := tabs.Tab{
			TabID:         tabID,
			ProjectID:     project.ID,
			ClientID:      clientID,
			Status:        tabs.StatusClosed,
			CreatedAt:     created,
			UpdatedAt:     time.Now(),
			DownstreamURI: &downstreamURI,
		}
		// Add "idle timeout" as the last error
		msg := "idle timeout"
		payload.LastError = &msg
		statusCb(payload)
	}

	// Re-acquire mutex for caller
	m.mu.Lock()
}

// sendIdleWarning sends a warning event when tab is approaching idle timeout.
func (m *Manager) sendIdleWarning(tabID string, entry *tabEntry, remaining time.Duration) {
	if m.statusCb != nil {
		// Send status event with warning metadata
		// Note: We use the existing status callback mechanism
		// The frontend can detect the warning by checking if LastError contains "idle warning"
		warningMsg := fmt.Sprintf("idle warning: tab will close in %v due to inactivity", remaining.Round(time.Second))
		payload := tabs.Tab{
			TabID:         tabID,
			ProjectID:     entry.project.ID,
			ClientID:      entry.clientID,
			Status:        tabs.StatusConnected, // Keep status as connected
			CreatedAt:     entry.created,
			UpdatedAt:     time.Now(),
			DownstreamURI: &entry.downstreamURI,
			LastError:     &warningMsg,
		}
		m.statusCb(payload)
	}
}

func toTabStatus(s relay.Status) tabs.Status {
	switch s {
	case relay.StatusConnecting, relay.StatusIdle:
		return tabs.StatusConnecting
	case relay.StatusConnected:
		return tabs.StatusConnected
	case relay.StatusReconnecting:
		return tabs.StatusReconnecting
	case relay.StatusClosed:
		return tabs.StatusClosed
	default:
		return ""
	}
}
