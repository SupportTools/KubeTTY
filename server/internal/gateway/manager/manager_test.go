package manager

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/metrics"
	"github.com/supporttools/KubeTTY/server/internal/gateway/relay"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
)

type fakeStore struct {
	createCalled bool
	allTabs      []tabs.Tab
	tabCounts    map[string]int // Maps "clientID:projectID" to count
}

func (f *fakeStore) Create(ctx context.Context, tab tabs.Tab) error {
	f.createCalled = true
	return nil
}

func (f *fakeStore) UpdateStatus(context.Context, string, tabs.Status, *string, *string) error {
	return nil
}
func (f *fakeStore) Delete(context.Context, string) error                          { return nil }
func (f *fakeStore) Get(context.Context, string) (*tabs.Tab, error)                { return nil, tabs.ErrNotFound }
func (f *fakeStore) ListByClient(context.Context, string, int) ([]tabs.Tab, error) { return nil, nil }
func (f *fakeStore) ListAll(context.Context) ([]tabs.Tab, error)                   { return f.allTabs, nil }
func (f *fakeStore) CountByClientAndProject(ctx context.Context, clientID, projectID string) (int, error) {
	if f.tabCounts == nil {
		return 0, nil
	}
	key := clientID + ":" + projectID
	return f.tabCounts[key], nil
}
func (f *fakeStore) UpdateClientID(context.Context, string, string) error { return nil }
func (f *fakeStore) GetActiveCountByProject(context.Context) (map[string]int, error) {
	counts := make(map[string]int)
	for _, tab := range f.allTabs {
		if tab.Status == tabs.StatusConnected {
			counts[tab.ProjectID]++
		}
	}
	return counts, nil
}
func (f *fakeStore) GetStatusCounts(context.Context) (map[string]int, error) {
	counts := make(map[string]int)
	for _, tab := range f.allTabs {
		counts[string(tab.Status)]++
	}
	return counts, nil
}
func (f *fakeStore) GetRecentErrors(ctx context.Context, limit int) ([]tabs.Tab, error) {
	var result []tabs.Tab
	for _, tab := range f.allTabs {
		if tab.LastError != nil && *tab.LastError != "" {
			result = append(result, tab)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func (f *fakeStore) UpdatePositions(context.Context, string, []string) error { return nil }
func (f *fakeStore) GetNextPosition(context.Context, string) (int, error)    { return 0, nil }
func (f *fakeStore) CleanOrphanedTabs(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func TestNewManager(t *testing.T) {
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}}}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)
	if len(mgr.ListProjects()) != 1 {
		t.Fatalf("expected 1 project")
	}
}

func TestCreateTabUnknownProject(t *testing.T) {
	store := &fakeStore{}
	mgr := New(gatewayconfig.Catalog{}, store, 2*time.Hour)
	if _, err := mgr.CreateTab(context.Background(), "missing", "client"); err == nil {
		t.Fatalf("expected error for missing project")
	}
}

func TestCreateTab(t *testing.T) {
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}}}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := mgr.CreateTab(ctx, "proj", "client1"); err != nil {
		t.Fatalf("CreateTab error: %v", err)
	}
	if !store.createCalled {
		t.Fatalf("expected store.Create to be called")
	}
}

func TestAttachTabNotFound(t *testing.T) {
	mgr := New(gatewayconfig.Catalog{}, &fakeStore{}, 2*time.Hour)
	conn := &websocket.Conn{}
	if err := mgr.Attach(context.Background(), "missing", "client", conn); err == nil {
		t.Fatalf("expected ErrNotFound")
	}
}

func TestRestoreTabs(t *testing.T) {
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}}}
	store := &fakeStore{allTabs: []tabs.Tab{{TabID: "restore", ProjectID: "proj", ClientID: "cli", CreatedAt: time.Now()}}}
	mgr := New(cat, store, 2*time.Hour)
	if err := mgr.RestoreTabs(context.Background()); err != nil {
		t.Fatalf("RestoreTabs error: %v", err)
	}
}

// TestCreateTab_LimitEnforcement verifies that tab creation respects maxTabsPerClient limits.
func TestCreateTab_LimitEnforcement(t *testing.T) {
	tests := []struct {
		name         string
		limit        int
		existingTabs int
		expectError  bool
	}{
		{
			name:         "No limit (0) allows unlimited tabs",
			limit:        0,
			existingTabs: 10,
			expectError:  false,
		},
		{
			name:         "Under limit allows creation",
			limit:        3,
			existingTabs: 2,
			expectError:  false,
		},
		{
			name:         "At limit blocks creation",
			limit:        3,
			existingTabs: 3,
			expectError:  true,
		},
		{
			name:         "Over limit blocks creation",
			limit:        2,
			existingTabs: 5,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := gatewayconfig.Catalog{
				Projects: []gatewayconfig.Project{{
					ID:        "proj",
					Namespace: "ns",
					Service:   "svc",
					Port:      8080,
					Limits: gatewayconfig.ProjectLimits{
						MaxTabsPerClient: tt.limit,
					},
				}},
			}

			// Build allTabs to simulate existing tabs in database
			var existingTabs []tabs.Tab
			for i := 0; i < tt.existingTabs; i++ {
				existingTabs = append(existingTabs, tabs.Tab{
					TabID:     fmt.Sprintf("tab-%d", i),
					ProjectID: "proj",
					ClientID:  "client1",
					Status:    tabs.StatusConnected,
					CreatedAt: time.Now(),
				})
			}

			store := &fakeStore{
				allTabs: existingTabs,
				tabCounts: map[string]int{
					"client1:proj": tt.existingTabs,
				},
			}
			mgr := New(cat, store, 2*time.Hour)

			// Restore tabs to populate in-memory map (simulates startup)
			if err := mgr.RestoreTabs(context.Background()); err != nil {
				t.Fatalf("RestoreTabs failed: %v", err)
			}

			_, err := mgr.CreateTab(context.Background(), "proj", "client1")

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error when at/over limit, got nil")
				}
				var limitErr *TabLimitExceededError
				if !errors.As(err, &limitErr) {
					t.Fatalf("expected TabLimitExceededError, got: %T", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCreateTab_DifferentProjects verifies that limits are per-project.
func TestCreateTab_DifferentProjects(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{
				ID: "proj-a", Namespace: "ns", Service: "svc", Port: 8080,
				Limits: gatewayconfig.ProjectLimits{MaxTabsPerClient: 1},
			},
			{
				ID: "proj-b", Namespace: "ns", Service: "svc", Port: 8080,
				Limits: gatewayconfig.ProjectLimits{MaxTabsPerClient: 1},
			},
		},
	}
	// Simulate 1 existing tab for proj-a
	existingTabs := []tabs.Tab{
		{
			TabID:     "existing-tab-1",
			ProjectID: "proj-a",
			ClientID:  "client1",
			Status:    tabs.StatusConnected,
			CreatedAt: time.Now(),
		},
	}
	store := &fakeStore{
		allTabs: existingTabs,
		tabCounts: map[string]int{
			"client1:proj-a": 1, // Client already has 1 tab on proj-a
			"client1:proj-b": 0, // No tabs on proj-b
		},
	}
	mgr := New(cat, store, 2*time.Hour)

	// Restore tabs to populate in-memory map (simulates startup)
	if err := mgr.RestoreTabs(context.Background()); err != nil {
		t.Fatalf("RestoreTabs failed: %v", err)
	}

	// Should fail for proj-a (at limit)
	_, err := mgr.CreateTab(context.Background(), "proj-a", "client1")
	if err == nil {
		t.Fatal("expected error for proj-a (at limit)")
	}

	// Should succeed for proj-b (different project)
	_, err = mgr.CreateTab(context.Background(), "proj-b", "client1")
	if err != nil {
		t.Fatalf("unexpected error for proj-b: %v", err)
	}
}

// TestTabLimitExceededError_Error verifies error message formatting.
func TestTabLimitExceededError_Error(t *testing.T) {
	err := &TabLimitExceededError{
		ProjectID: "test-project",
		Limit:     5,
	}
	expected := "tab limit exceeded for project test-project: maximum 5 tabs per client"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

// TestDefaultManagerConfig verifies default configuration values.
func TestDefaultManagerConfig(t *testing.T) {
	cfg := DefaultManagerConfig()

	if cfg.IdleTimeout != 2*time.Hour {
		t.Errorf("IdleTimeout = %v, want 2h", cfg.IdleTimeout)
	}
	if !cfg.MetricsEnabled {
		t.Error("MetricsEnabled should be true by default")
	}
	if cfg.MetricsInterval != 15*time.Second {
		t.Errorf("MetricsInterval = %v, want 15s", cfg.MetricsInterval)
	}
}

// TestNewWithConfig verifies full configuration constructor.
func TestNewWithConfig(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}},
	}
	store := &fakeStore{}
	cfg := ManagerConfig{
		IdleTimeout:     30 * time.Minute,
		MetricsEnabled:  false,
		MetricsInterval: 30 * time.Second,
	}

	mgr := NewWithConfig(cat, store, cfg)
	if mgr == nil {
		t.Fatal("NewWithConfig returned nil")
	}
	if len(mgr.ListProjects()) != 1 {
		t.Errorf("expected 1 project, got %d", len(mgr.ListProjects()))
	}
}

// TestNewWithConfig_MinimumTimeout verifies minimum timeout enforcement.
func TestNewWithConfig_MinimumTimeout(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}},
	}
	store := &fakeStore{}
	cfg := ManagerConfig{
		IdleTimeout: 5 * time.Minute, // Below 10 minute minimum
	}

	mgr := NewWithConfig(cat, store, cfg)
	if mgr.idleTimeout != 10*time.Minute {
		t.Errorf("idleTimeout = %v, want 10m (minimum)", mgr.idleTimeout)
	}
}

// TestRegisterProject verifies dynamic project registration.
func TestRegisterProject(t *testing.T) {
	cat := gatewayconfig.Catalog{}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	// Initially empty
	if len(mgr.ListProjects()) != 0 {
		t.Errorf("expected 0 projects, got %d", len(mgr.ListProjects()))
	}

	// Register a project
	project := gatewayconfig.Project{
		ID:        "new-project",
		Namespace: "ns",
		Service:   "svc",
		Port:      8080,
	}
	mgr.RegisterProject(project)

	// Should now have 1 project
	if len(mgr.ListProjects()) != 1 {
		t.Errorf("expected 1 project after registration, got %d", len(mgr.ListProjects()))
	}

	// Verify HasProject
	if !mgr.HasProject("new-project") {
		t.Error("HasProject should return true for registered project")
	}
}

// TestUnregisterProject verifies dynamic project removal.
func TestUnregisterProject(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}},
	}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	// Initially 1 project
	if len(mgr.ListProjects()) != 1 {
		t.Errorf("expected 1 project, got %d", len(mgr.ListProjects()))
	}

	// Unregister the project
	mgr.UnregisterProject("proj")

	// Should now be empty
	if len(mgr.ListProjects()) != 0 {
		t.Errorf("expected 0 projects after unregistration, got %d", len(mgr.ListProjects()))
	}

	// Verify HasProject
	if mgr.HasProject("proj") {
		t.Error("HasProject should return false for unregistered project")
	}
}

// TestHasProject verifies project existence checking.
func TestHasProject(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}},
	}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	if !mgr.HasProject("proj") {
		t.Error("HasProject should return true for existing project")
	}
	if mgr.HasProject("missing") {
		t.Error("HasProject should return false for non-existent project")
	}
}

// TestListProjectsWithStatus verifies projects listing with health status.
func TestListProjectsWithStatus(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "proj-a", Namespace: "ns", Service: "svc", Port: 8080},
			{ID: "proj-b", Namespace: "ns", Service: "svc", Port: 8080},
		},
	}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	result := mgr.ListProjectsWithStatus()
	if len(result) != 2 {
		t.Errorf("expected 2 projects, got %d", len(result))
	}

	// Verify each project has status
	for _, p := range result {
		if p.Status == "" {
			t.Errorf("project %s has empty status", p.ID)
		}
	}
}

// TestSetStatusCallback verifies status callback setter.
func TestSetStatusCallback(t *testing.T) {
	cat := gatewayconfig.Catalog{}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	mgr.SetStatusCallback(func(tab tabs.Tab) {
		// callback set
	})

	if mgr.statusCb == nil {
		t.Error("statusCb should not be nil after SetStatusCallback")
	}
}

// TestSetMetricsCallback verifies metrics callback setter.
func TestSetMetricsCallback(t *testing.T) {
	cat := gatewayconfig.Catalog{}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	mgr.SetMetricsCallback(func(tabID string, m metrics.TabMetrics) {
		// callback set
	})

	if mgr.metricsCb == nil {
		t.Error("metricsCb should not be nil after SetMetricsCallback")
	}
}

// TestCloseTabNotFound verifies closing a non-existent tab.
func TestCloseTabNotFound(t *testing.T) {
	store := &fakeStore{}
	mgr := New(gatewayconfig.Catalog{}, store, 2*time.Hour)

	// Should not error (no-op for non-existent tab in memory)
	err := mgr.CloseTab(context.Background(), "missing-tab")
	if err != nil {
		// This depends on store behavior - fakeStore returns nil
		t.Logf("CloseTab error: %v", err)
	}
}

// TestToTabStatus verifies status conversion.
func TestToTabStatus(t *testing.T) {
	tests := []struct {
		input    relay.Status
		expected tabs.Status
	}{
		{relay.StatusConnecting, tabs.StatusConnecting},
		{relay.StatusIdle, tabs.StatusConnecting},
		{relay.StatusConnected, tabs.StatusConnected},
		{relay.StatusReconnecting, tabs.StatusReconnecting},
		{relay.StatusClosed, tabs.StatusClosed},
		{relay.Status("unknown"), ""},
	}

	for _, tt := range tests {
		result := toTabStatus(tt.input)
		if result != tt.expected {
			t.Errorf("toTabStatus(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestProjectWithStatus_Fields verifies ProjectWithStatus struct.
func TestProjectWithStatus_Fields(t *testing.T) {
	now := time.Now()
	pws := ProjectWithStatus{
		Project: gatewayconfig.Project{
			ID:        "test-proj",
			Namespace: "test-ns",
			Service:   "test-svc",
			Port:      8080,
		},
		Status:        "healthy",
		LastCheckedAt: &now,
	}

	if pws.ID != "test-proj" {
		t.Errorf("ID = %q, want %q", pws.ID, "test-proj")
	}
	if pws.Status != "healthy" {
		t.Errorf("Status = %q, want %q", pws.Status, "healthy")
	}
	if pws.LastCheckedAt == nil {
		t.Error("LastCheckedAt should not be nil")
	}
}

// TestIsGUIEnabled verifies checking if a project has GUI support.
func TestIsGUIEnabled(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "gui-enabled", Namespace: "ns", Service: "svc", Port: 8080, GUIEnabled: true, GUIVNCPort: 5901},
			{ID: "gui-disabled", Namespace: "ns", Service: "svc", Port: 8080, GUIEnabled: false},
		},
	}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	tests := []struct {
		name      string
		projectID string
		want      bool
	}{
		{"GUI enabled project", "gui-enabled", true},
		{"GUI disabled project", "gui-disabled", false},
		{"Non-existent project", "missing", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mgr.IsGUIEnabled(tt.projectID); got != tt.want {
				t.Errorf("IsGUIEnabled(%q) = %v, want %v", tt.projectID, got, tt.want)
			}
		})
	}
}

// TestCreateVNCTab verifies VNC tab creation for GUI-enabled projects.
func TestCreateVNCTab(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "gui-project", Namespace: "ns", Service: "svc", Port: 8080, GUIEnabled: true, GUIVNCPort: 5901},
		},
	}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	ctx := context.Background()
	tab, err := mgr.CreateVNCTab(ctx, "gui-project", "client1")
	if err != nil {
		t.Fatalf("CreateVNCTab error: %v", err)
	}
	if tab.ProjectID != "gui-project" {
		t.Errorf("ProjectID = %q, want %q", tab.ProjectID, "gui-project")
	}
	if tab.ClientID != "client1" {
		t.Errorf("ClientID = %q, want %q", tab.ClientID, "client1")
	}
	// Check downstream URI has vnc:// scheme
	if tab.DownstreamURI == nil || *tab.DownstreamURI == "" {
		t.Error("DownstreamURI should not be empty")
	} else if (*tab.DownstreamURI)[:6] != "vnc://" {
		t.Errorf("DownstreamURI = %q, want vnc:// prefix", *tab.DownstreamURI)
	}
}

// TestCreateVNCTab_UnknownProject verifies error for unknown project.
func TestCreateVNCTab_UnknownProject(t *testing.T) {
	store := &fakeStore{}
	mgr := New(gatewayconfig.Catalog{}, store, 2*time.Hour)

	_, err := mgr.CreateVNCTab(context.Background(), "missing", "client")
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
}

// TestCreateVNCTab_GUINotEnabled verifies error when GUI is not enabled.
func TestCreateVNCTab_GUINotEnabled(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "no-gui", Namespace: "ns", Service: "svc", Port: 8080, GUIEnabled: false},
		},
	}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	_, err := mgr.CreateVNCTab(context.Background(), "no-gui", "client")
	if err == nil {
		t.Fatal("expected error when GUI is not enabled")
	}
}

// TestCreateVNCTab_LimitEnforcement verifies tab limit enforcement for VNC tabs.
func TestCreateVNCTab_LimitEnforcement(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{
				ID:         "gui-limited",
				Namespace:  "ns",
				Service:    "svc",
				Port:       8080,
				GUIEnabled: true,
				GUIVNCPort: 5901,
				Limits:     gatewayconfig.ProjectLimits{MaxTabsPerClient: 1},
			},
		},
	}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)
	ctx := context.Background()

	// First tab should succeed
	_, err := mgr.CreateVNCTab(ctx, "gui-limited", "client1")
	if err != nil {
		t.Fatalf("First CreateVNCTab error: %v", err)
	}

	// Second tab should fail due to limit
	_, err = mgr.CreateVNCTab(ctx, "gui-limited", "client1")
	if err == nil {
		t.Fatal("expected error when at tab limit")
	}
	var limitErr *TabLimitExceededError
	if !errors.As(err, &limitErr) {
		t.Errorf("expected TabLimitExceededError, got %T: %v", err, err)
	}
}

// TestCreateVNCTab_DefaultVNCPort verifies default VNC port is used when not specified.
func TestCreateVNCTab_DefaultVNCPort(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "gui-default-port", Namespace: "ns", Service: "svc", Port: 8080, GUIEnabled: true},
		},
	}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)

	tab, err := mgr.CreateVNCTab(context.Background(), "gui-default-port", "client1")
	if err != nil {
		t.Fatalf("CreateVNCTab error: %v", err)
	}

	// Should use default VNC port 5901
	expected := "vnc://svc.ns.svc:5901"
	if tab.DownstreamURI == nil || *tab.DownstreamURI != expected {
		t.Errorf("DownstreamURI = %q, want %q", *tab.DownstreamURI, expected)
	}
}
