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
	projects map[string]gatewayconfig.Project
	store    tabs.Store
	dialer   *websocket.Dialer
	mu       sync.Mutex
	tabs     map[string]*tabEntry
	statusCb func(tabs.Tab)
}

type tabEntry struct {
	relay         *relay.Relay
	clientID      string
	project       gatewayconfig.Project
	created       time.Time
	downstreamURI string
	cancel        context.CancelFunc
}

// New creates a manager.
func New(cat gatewayconfig.Catalog, store tabs.Store) *Manager {
	projects := make(map[string]gatewayconfig.Project, len(cat.Projects))
	for _, p := range cat.Projects {
		projects[p.ID] = p
	}
	return &Manager{
		projects: projects,
		store:    store,
		dialer:   websocket.DefaultDialer,
		tabs:     make(map[string]*tabEntry),
	}
}

// CreateTab allocates metadata and starts a relay (if not already running).
func (m *Manager) CreateTab(ctx context.Context, projectID, clientID string) (tabs.Tab, error) {
	project, ok := m.projects[projectID]
	if !ok {
		return tabs.Tab{}, fmt.Errorf("unknown project %q", projectID)
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
	return &tabEntry{relay: rel, clientID: clientID, project: project, created: created, downstreamURI: uri}
}

func (m *Manager) startTracking(tabID string, entry *tabEntry) {
	ctx, cancel := context.WithCancel(context.Background())
	entry.cancel = cancel
	go m.watchStatus(ctx, tabID, entry, entry.relay.Subscribe())
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
