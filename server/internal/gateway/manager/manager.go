package manager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"

	"github.com/supporttools/KubeTTY/server/internal/config"
	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/exec"
	"github.com/supporttools/KubeTTY/server/internal/gateway/health"
	"github.com/supporttools/KubeTTY/server/internal/gateway/metrics"
	"github.com/supporttools/KubeTTY/server/internal/gateway/relay"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
)

// ProjectStore defines methods for updating project activity.
type ProjectStore interface {
	UpdateLastActivity(ctx context.Context, projectName string) error
}

// Manager orchestrates tab creation, relay lifecycle, and persistence.
type Manager struct {
	projects          map[string]gatewayconfig.Project
	store             tabs.Store
	projectStore      ProjectStore // Store for updating project activity timestamps
	dialer            *websocket.Dialer
	mu                sync.Mutex
	tabs              map[string]*tabEntry
	statusCb          func(tabs.Tab)
	metricsCb         func(string, metrics.TabMetrics) // Callback for metrics updates (tabID, metrics)
	idleTimeout       time.Duration                    // Tab idle timeout duration
	idleWarningBefore time.Duration                    // Warning time before idle timeout
	stopIdleChecker   chan struct{}                    // Signal to stop idle checker goroutine
	healthChecker     *health.Checker
	metricsCollector  *metrics.Collector  // Resource metrics collector
	metricsEnabled    bool                // Whether metrics collection is enabled
	metricsInterval   time.Duration       // Metrics collection interval
	execMode          config.ExecModeType // Exec mode: "websocket" or "exec"
	restConfig        *rest.Config        // Kubernetes rest config for exec mode
}

type tabEntry struct {
	proxier       relay.Proxier // Can be either *relay.Relay or *exec.ExecRelay
	clientID      string
	project       gatewayconfig.Project
	created       time.Time
	lastActivity  time.Time // Last activity timestamp for idle timeout tracking
	warned        bool      // Whether idle warning has been sent
	downstreamURI string
	cancel        context.CancelFunc
}

// ManagerConfig holds configuration for the Manager.
type ManagerConfig struct {
	IdleTimeout     time.Duration
	MetricsEnabled  bool
	MetricsInterval time.Duration
	ExecMode        config.ExecModeType // "websocket" (default) or "exec"
	RestConfig      *rest.Config        // Required when ExecMode is "exec"
}

// DefaultManagerConfig returns default manager configuration.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		IdleTimeout:     2 * time.Hour,
		MetricsEnabled:  true,
		MetricsInterval: 15 * time.Second,
	}
}

// New creates a manager with the given idle timeout configuration.
func New(cat gatewayconfig.Catalog, store tabs.Store, idleTimeout time.Duration) *Manager {
	return NewWithConfig(cat, store, ManagerConfig{
		IdleTimeout:     idleTimeout,
		MetricsEnabled:  true,
		MetricsInterval: 15 * time.Second,
	})
}

// NewWithConfig creates a manager with full configuration.
func NewWithConfig(cat gatewayconfig.Catalog, store tabs.Store, cfg ManagerConfig) *Manager {
	projects := make(map[string]gatewayconfig.Project, len(cat.Projects))
	for _, p := range cat.Projects {
		projects[p.ID] = p
	}

	// Validate minimum idle timeout (10 minutes)
	if cfg.IdleTimeout < 10*time.Minute {
		log.WithFields(log.Fields{
			"requested_timeout": cfg.IdleTimeout.String(),
			"minimum_timeout":   (10 * time.Minute).String(),
		}).Warn("gateway/manager: idle timeout below minimum, enforcing minimum")
		cfg.IdleTimeout = 10 * time.Minute
	}

	// Default to websocket mode if not specified
	execMode := cfg.ExecMode
	if execMode == "" {
		execMode = config.ExecModeWebSocket
	}

	return &Manager{
		projects:          projects,
		store:             store,
		dialer:            websocket.DefaultDialer,
		tabs:              make(map[string]*tabEntry),
		idleTimeout:       cfg.IdleTimeout,
		idleWarningBefore: 5 * time.Minute, // Fixed: warn 5 minutes before timeout
		stopIdleChecker:   make(chan struct{}),
		healthChecker:     health.NewChecker(cat.Projects),
		metricsEnabled:    cfg.MetricsEnabled,
		metricsInterval:   cfg.MetricsInterval,
		execMode:          execMode,
		restConfig:        cfg.RestConfig,
	}
}

// RegisterProject adds a project to the manager dynamically.
// This is used by the controller to register projects that are created via the API.
func (m *Manager) RegisterProject(project gatewayconfig.Project) {
	m.mu.Lock()
	m.projects[project.ID] = project
	m.mu.Unlock()

	// Add to health checker for monitoring
	if m.healthChecker != nil {
		m.healthChecker.AddProject(project)
	}

	log.WithFields(log.Fields{
		"project_id": project.ID,
		"namespace":  project.Namespace,
		"port":       project.Port,
		"service":    project.Service,
	}).Info("gateway/manager: registered project")
}

// UnregisterProject removes a project from the manager.
// Existing tabs for this project are not affected until they're closed.
func (m *Manager) UnregisterProject(projectID string) {
	m.mu.Lock()
	delete(m.projects, projectID)
	m.mu.Unlock()

	// Remove from health checker
	if m.healthChecker != nil {
		m.healthChecker.RemoveProject(projectID)
	}

	log.WithFields(log.Fields{
		"project_id": projectID,
	}).Info("gateway/manager: unregistered project")
}

// HasProject returns whether a project is registered.
func (m *Manager) HasProject(projectID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.projects[projectID]
	return ok
}

// CreateTab allocates metadata and starts a relay (if not already running).
// Uses mutex to ensure atomic limit check and tab creation, preventing race conditions.
func (m *Manager) CreateTab(ctx context.Context, projectID, clientID string) (tabs.Tab, error) {
	m.mu.Lock()

	project, ok := m.projects[projectID]
	if !ok {
		m.mu.Unlock()
		return tabs.Tab{}, fmt.Errorf("unknown project %q", projectID)
	}

	// Enforce per-client tab limit using in-memory count (atomic with creation)
	if project.Limits.MaxTabsPerClient > 0 {
		count := m.countTabsForClientProjectLocked(clientID, projectID)
		if count >= project.Limits.MaxTabsPerClient {
			m.mu.Unlock()
			return tabs.Tab{}, &TabLimitExceededError{
				ProjectID: projectID,
				Limit:     project.Limits.MaxTabsPerClient,
			}
		}
	}

	id := uuid.NewString()
	e := m.newEntry(project, clientID, time.Now())
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

	// Persist to database - if this fails, we need to clean up
	if err := m.store.Create(ctx, tab); err != nil {
		// Remove from in-memory map
		m.mu.Lock()
		delete(m.tabs, id)
		m.mu.Unlock()
		return tabs.Tab{}, err
	}

	m.startTracking(id, e)
	return tab, nil
}

// countTabsForClientProjectLocked counts in-memory tabs for a client/project.
// MUST be called with mutex held.
func (m *Manager) countTabsForClientProjectLocked(clientID, projectID string) int {
	count := 0
	for _, entry := range m.tabs {
		if entry.clientID == clientID && entry.project.ID == projectID {
			count++
		}
	}
	return count
}

// Attach proxies between the caller WebSocket and the downstream relay.
func (m *Manager) Attach(ctx context.Context, tabID, clientID string, upstream *websocket.Conn) error {
	return m.AttachWithOptions(ctx, tabID, clientID, upstream, false)
}

// AttachWithOptions proxies between the caller WebSocket and the downstream relay.
// If forceTakeover is true, allows a different client to take over the tab.
func (m *Manager) AttachWithOptions(ctx context.Context, tabID, clientID string, upstream *websocket.Conn, forceTakeover bool) error {
	m.mu.Lock()
	e, ok := m.tabs[tabID]
	if !ok {
		m.mu.Unlock()
		return tabs.ErrNotFound
	}
	if e.clientID != clientID {
		if !forceTakeover {
			m.mu.Unlock()
			return fmt.Errorf("tab %s owned by another client", tabID)
		}
		// Force takeover: update clientID
		log.WithFields(log.Fields{
			"tab_id":        tabID,
			"old_client_id": e.clientID,
			"new_client_id": clientID,
		}).Info("gateway/manager: force takeover of tab")
		e.clientID = clientID
		// Update in database
		go func() {
			if err := m.store.UpdateClientID(context.Background(), tabID, clientID); err != nil {
				log.WithFields(log.Fields{
					"tab_id":    tabID,
					"client_id": clientID,
					"error":     err.Error(),
				}).Warn("gateway/manager: failed to update client ID in database")
			}
		}()
	}
	m.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return e.proxier.Proxy(ctx, upstream)
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
		_ = e.proxier.Close()
		// Unregister from metrics collection
		m.unregisterTabForMetrics(tabID)
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

// ProjectWithStatus combines project metadata with health status.
type ProjectWithStatus struct {
	gatewayconfig.Project
	Status        string     `json:"status"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
}

// ListProjectsWithStatus returns all projects with their current health status.
func (m *Manager) ListProjectsWithStatus() []ProjectWithStatus {
	result := make([]ProjectWithStatus, 0, len(m.projects))
	statuses := make(map[string]*health.ProjectStatus)
	if m.healthChecker != nil {
		statuses = m.healthChecker.GetAllStatuses()
	}

	for _, p := range m.projects {
		pws := ProjectWithStatus{
			Project: p,
			Status:  string(health.StatusUnknown),
		}
		if status, ok := statuses[p.ID]; ok {
			pws.Status = string(status.Status)
			if !status.LastCheckedAt.IsZero() {
				pws.LastCheckedAt = &status.LastCheckedAt
			}
		}
		result = append(result, pws)
	}
	return result
}

// RestoreTabs loads persisted rows at startup so clients can reconnect.
// Tabs for unknown projects are deleted to prevent stale tab loops.
func (m *Manager) RestoreTabs(ctx context.Context) error {
	rows, err := m.store.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		project, ok := m.projects[row.ProjectID]
		if !ok {
			log.WithFields(log.Fields{
				"tab_id":     row.TabID,
				"project_id": row.ProjectID,
			}).Warn("gateway/manager: deleting orphaned tab for unknown project")
			if err := m.store.Delete(ctx, row.TabID); err != nil {
				log.WithFields(log.Fields{
					"tab_id": row.TabID,
					"error":  err.Error(),
				}).Error("gateway/manager: failed to delete orphaned tab")
			}
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

	var proxier relay.Proxier
	if m.execMode == config.ExecModeExec && m.restConfig != nil {
		// Use kubectl exec mode
		log.WithFields(log.Fields{
			"project_id": project.ID,
			"namespace":  project.Namespace,
			"pod":        project.Service, // In exec mode, service name is treated as pod selector prefix
		}).Info("gateway/manager: creating exec relay for tab")

		proxier = exec.NewExecRelay(m.restConfig, exec.RelayConfig{
			Namespace:  project.Namespace,
			PodName:    project.Service, // Pod name will be resolved based on service
			Container:  "",              // Use default container
			Command:    []string{"/bin/bash", "-l"},
			BufferSize: 64 * 1024,
		})
	} else {
		// Use WebSocket relay mode (default)
		proxier = relay.New(relay.Config{
			ProjectID:     project.ID,
			Endpoint:      endpoint,
			Dialer:        m.dialer,
			Headers:       http.Header{"X-Kubetty-Project": []string{project.ID}},
			DownstreamURI: uri,
		})
	}

	return &tabEntry{
		proxier:       proxier,
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
	go m.watchStatus(ctx, tabID, entry, entry.proxier.Subscribe())
	go m.watchActivity(ctx, tabID, entry, entry.proxier.ActivityChan())

	// Register tab for metrics collection
	m.registerTabForMetrics(tabID, entry)
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
	entry, ok := m.tabs[tabID]
	if !ok {
		// Tab was deleted, activity signal is stale - ignore safely
		m.mu.Unlock()
		return
	}
	entry.lastActivity = time.Now()
	entry.warned = false // Clear warning state on activity
	projectID := entry.project.ID
	m.mu.Unlock()

	log.WithFields(log.Fields{
		"tab_id":     tabID,
		"project_id": projectID,
	}).Debug("gateway/manager: recorded tab activity")

	// Update project activity in database (async, don't block on errors)
	if m.projectStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.projectStore.UpdateLastActivity(ctx, projectID); err != nil {
			log.WithFields(log.Fields{
				"project_id": projectID,
				"error":      err.Error(),
			}).Warn("gateway/manager: failed to update project activity")
		}
	}
}

func (m *Manager) watchStatus(ctx context.Context, tabID string, entry *tabEntry, ch <-chan relay.StatusEvent) {
	for {
		select {
		case <-ctx.Done():
			log.WithFields(log.Fields{
				"tab_id":     tabID,
				"project_id": entry.project.ID,
			}).Debug("gateway/manager: watchStatus context cancelled")
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

			logFields := log.Fields{
				"tab_id":     tabID,
				"project_id": entry.project.ID,
				"status":     status,
			}
			if errStr != nil {
				logFields["error"] = *errStr
			}
			log.WithFields(logFields).Debug("gateway/manager: received status update from relay")

			if err := m.store.UpdateStatus(ctx, tabID, status, errStr, &downURI); err != nil && !errors.Is(err, tabs.ErrNotFound) {
				log.WithFields(log.Fields{
					"tab_id":     tabID,
					"project_id": entry.project.ID,
					"status":     status,
					"error":      err.Error(),
				}).Warn("gateway/manager: failed to update tab status in database")
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

// SetMetricsCallback registers a callback invoked when tab metrics are updated.
func (m *Manager) SetMetricsCallback(cb func(string, metrics.TabMetrics)) {
	m.metricsCb = cb
}

// SetProjectStore sets the project store for updating project activity timestamps.
func (m *Manager) SetProjectStore(store ProjectStore) {
	m.projectStore = store
}

// StartMetricsCollector begins the background metrics collection goroutine.
func (m *Manager) StartMetricsCollector() {
	if !m.metricsEnabled {
		log.Info("gateway/manager: metrics collection disabled")
		return
	}

	collector, err := metrics.NewCollector(m.metricsInterval, m.handleMetricsUpdate)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("gateway/manager: failed to create metrics collector")
		return
	}

	m.metricsCollector = collector
	m.metricsCollector.Start()
	log.WithFields(log.Fields{
		"interval": m.metricsInterval.String(),
	}).Info("gateway/manager: metrics collector started")

	// Register all existing tabs
	m.mu.Lock()
	tabCount := len(m.tabs)
	for tabID, entry := range m.tabs {
		m.registerTabForMetrics(tabID, entry)
	}
	m.mu.Unlock()

	log.WithFields(log.Fields{
		"tab_count": tabCount,
	}).Debug("gateway/manager: registered existing tabs for metrics collection")
}

// handleMetricsUpdate is called when metrics are collected for a tab.
func (m *Manager) handleMetricsUpdate(tabID string, tabMetrics metrics.TabMetrics) {
	if m.metricsCb != nil {
		m.metricsCb(tabID, tabMetrics)
	}
}

// registerTabForMetrics registers a tab with the metrics collector.
func (m *Manager) registerTabForMetrics(tabID string, entry *tabEntry) {
	if m.metricsCollector == nil {
		log.WithFields(log.Fields{
			"tab_id": tabID,
		}).Debug("gateway/manager: metrics collector is nil, skipping tab registration")
		return
	}

	// Build downstream URI for metrics endpoint
	downstreamBase := fmt.Sprintf("http://%s.%s.svc:%d",
		entry.project.Service,
		entry.project.Namespace,
		entry.project.Port,
	)

	log.WithFields(log.Fields{
		"project_id":          entry.project.ID,
		"cpu_millicores":      entry.project.Limits.CPUMillicores,
		"memory_bytes":        entry.project.Limits.MemoryBytes,
		"max_tabs_per_client": entry.project.Limits.MaxTabsPerClient,
	}).Debug("gateway/manager: preparing tab registration for metrics collection")

	info := metrics.TabInfo{
		TabID:         tabID,
		ProjectID:     entry.project.ID,
		ProjectName:   entry.project.ID, // Used for label selector: kubetty.io/project=<name>
		Namespace:     entry.project.Namespace,
		DownstreamURI: downstreamBase,
		CPULimit:      entry.project.Limits.CPUMillicores,
		MemoryLimit:   entry.project.Limits.MemoryBytes,
	}

	log.WithFields(log.Fields{
		"tab_id":         info.TabID,
		"project_name":   info.ProjectName,
		"namespace":      info.Namespace,
		"cpu_limit":      info.CPULimit,
		"memory_limit":   info.MemoryLimit,
		"downstream_uri": info.DownstreamURI,
	}).Debug("gateway/manager: registering tab for metrics collection")

	m.metricsCollector.RegisterTab(info)
}

// unregisterTabForMetrics removes a tab from the metrics collector.
func (m *Manager) unregisterTabForMetrics(tabID string) {
	if m.metricsCollector != nil {
		m.metricsCollector.UnregisterTab(tabID)
	}
}

// StartIdleChecker begins monitoring tabs for idle timeout.
// Should be called after RestoreTabs() during gateway startup.
func (m *Manager) StartIdleChecker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	log.WithFields(log.Fields{
		"timeout": m.idleTimeout.String(),
		"warning": m.idleWarningBefore.String(),
	}).Info("gateway/manager: starting idle checker")

	for {
		select {
		case <-ctx.Done():
			log.Info("gateway/manager: idle checker stopped (context cancelled)")
			return
		case <-m.stopIdleChecker:
			log.Info("gateway/manager: idle checker stopped (shutdown signal)")
			return
		case <-ticker.C:
			m.checkIdleTabs(ctx)
		}
	}
}

// Stop gracefully shuts down the manager, idle checker, health checker, and metrics collector.
func (m *Manager) Stop() {
	close(m.stopIdleChecker)
	if m.healthChecker != nil {
		m.healthChecker.Stop()
	}
	if m.metricsCollector != nil {
		m.metricsCollector.Stop()
	}
}

// StartHealthChecker begins the background health checking goroutine.
func (m *Manager) StartHealthChecker() {
	if m.healthChecker != nil {
		m.healthChecker.Start()
	}
}

// checkIdleTabs scans all tabs and handles idle warnings and closures.
// Uses snapshot approach to avoid map iteration panic when closing tabs.
func (m *Manager) checkIdleTabs(ctx context.Context) {
	// Snapshot tab IDs to avoid iterating while modifying
	m.mu.Lock()
	tabIDs := make([]string, 0, len(m.tabs))
	for tabID := range m.tabs {
		tabIDs = append(tabIDs, tabID)
	}
	m.mu.Unlock()

	now := time.Now()
	for _, tabID := range tabIDs {
		// Re-acquire lock to check each tab individually
		m.mu.Lock()
		entry, exists := m.tabs[tabID]
		if !exists {
			// Tab was deleted by another goroutine, skip
			m.mu.Unlock()
			continue
		}

		idleDuration := now.Sub(entry.lastActivity)

		// Tab has exceeded idle timeout - close it
		if idleDuration >= m.idleTimeout {
			log.WithFields(log.Fields{
				"tab_id":        tabID,
				"project_id":    entry.project.ID,
				"idle_duration": idleDuration.String(),
				"timeout":       m.idleTimeout.String(),
			}).Info("gateway/manager: closing idle tab")
			// closeIdleTabLocked expects mutex to be held and will handle unlock/relock
			m.closeIdleTabLocked(ctx, tabID, entry)
			m.mu.Unlock()
			continue
		}

		// Tab approaching idle timeout - send warning
		warningThreshold := m.idleTimeout - m.idleWarningBefore
		if idleDuration >= warningThreshold && !entry.warned {
			remaining := m.idleTimeout - idleDuration
			log.WithFields(log.Fields{
				"tab_id":        tabID,
				"project_id":    entry.project.ID,
				"idle_duration": idleDuration.String(),
				"remaining":     remaining.String(),
			}).Info("gateway/manager: sending idle warning for tab")
			m.sendIdleWarning(tabID, entry, remaining)
			entry.warned = true
		}
		m.mu.Unlock()
	}
}

// closeIdleTabLocked closes a tab due to idle timeout.
// MUST be called with mutex held. Releases mutex during cleanup to avoid deadlock,
// but does NOT re-acquire it - caller is responsible for releasing.
func (m *Manager) closeIdleTabLocked(ctx context.Context, tabID string, entry *tabEntry) {
	// Extract data needed for cleanup while mutex is held
	delete(m.tabs, tabID)
	cancel := entry.cancel
	proxier := entry.proxier
	project := entry.project
	clientID := entry.clientID
	created := entry.created
	downstreamURI := entry.downstreamURI
	statusCb := m.statusCb
	metricsCollector := m.metricsCollector

	// Release mutex before calling external methods to avoid deadlock
	m.mu.Unlock()

	// Cancel relay context
	if cancel != nil {
		cancel()
	}

	// Close proxier (may acquire relay mutex)
	_ = proxier.Close()

	// Unregister from metrics collection
	if metricsCollector != nil {
		metricsCollector.UnregisterTab(tabID)
	}

	// Delete from database (spawn goroutine to avoid blocking)
	go func() {
		if err := m.store.Delete(ctx, tabID); err != nil {
			log.WithFields(log.Fields{
				"tab_id": tabID,
				"error":  err.Error(),
			}).Warn("gateway/manager: failed to delete idle tab from database")
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

	// Re-acquire mutex so caller can release it uniformly
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
