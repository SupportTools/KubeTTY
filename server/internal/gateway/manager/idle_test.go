package manager

import (
	"context"
	"sync"
	"testing"
	"time"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
)

// mockTabStore implements tabs.Store for testing
type mockTabStore struct {
	mu      sync.Mutex
	tabs    map[string]tabs.Tab
	deleted []string
}

func newMockTabStore() *mockTabStore {
	return &mockTabStore{
		tabs: make(map[string]tabs.Tab),
	}
}

func (m *mockTabStore) Create(ctx context.Context, tab tabs.Tab) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tabs[tab.TabID] = tab
	return nil
}

func (m *mockTabStore) Get(ctx context.Context, tabID string) (*tabs.Tab, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tab, ok := m.tabs[tabID]
	if !ok {
		return nil, tabs.ErrNotFound
	}
	return &tab, nil
}

func (m *mockTabStore) ListByClient(ctx context.Context, clientID string, limit int) ([]tabs.Tab, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []tabs.Tab
	for _, tab := range m.tabs {
		if tab.ClientID == clientID {
			result = append(result, tab)
		}
	}
	return result, nil
}

func (m *mockTabStore) UpdateStatus(ctx context.Context, tabID string, status tabs.Status, lastError *string, downstreamURI *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tab, ok := m.tabs[tabID]
	if !ok {
		return tabs.ErrNotFound
	}
	tab.Status = status
	tab.LastError = lastError
	tab.DownstreamURI = downstreamURI
	tab.UpdatedAt = time.Now()
	m.tabs[tabID] = tab
	return nil
}

func (m *mockTabStore) Delete(ctx context.Context, tabID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tabs, tabID)
	m.deleted = append(m.deleted, tabID)
	return nil
}

func (m *mockTabStore) ListAll(ctx context.Context) ([]tabs.Tab, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []tabs.Tab
	for _, tab := range m.tabs {
		result = append(result, tab)
	}
	return result, nil
}

func (m *mockTabStore) CountByClientAndProject(ctx context.Context, clientID, projectID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, tab := range m.tabs {
		if tab.ClientID == clientID && tab.ProjectID == projectID {
			count++
		}
	}
	return count, nil
}

func (m *mockTabStore) UpdateClientID(ctx context.Context, tabID, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tab, ok := m.tabs[tabID]
	if !ok {
		return tabs.ErrNotFound
	}
	tab.ClientID = clientID
	tab.UpdatedAt = time.Now()
	m.tabs[tabID] = tab
	return nil
}

func TestManager_IdleTimeout_MinimumValidation(t *testing.T) {
	catalog := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "test-project", Service: "test-svc", Namespace: "default", Port: 8080},
		},
	}
	store := newMockTabStore()

	// Test that timeout below minimum (10 minutes) is enforced to 10 minutes
	mgr := New(catalog, store, 5*time.Minute)
	if mgr.idleTimeout != 10*time.Minute {
		t.Errorf("Expected idle timeout to be 10m when below minimum, got %v", mgr.idleTimeout)
	}

	// Test that timeout at minimum is accepted
	mgr = New(catalog, store, 10*time.Minute)
	if mgr.idleTimeout != 10*time.Minute {
		t.Errorf("Expected idle timeout to be 10m, got %v", mgr.idleTimeout)
	}

	// Test that timeout above minimum is accepted
	mgr = New(catalog, store, 1*time.Hour)
	if mgr.idleTimeout != 1*time.Hour {
		t.Errorf("Expected idle timeout to be 1h, got %v", mgr.idleTimeout)
	}
}

func TestManager_IdleTimeout_WarningAndClosure(t *testing.T) {
	catalog := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "test-project", Service: "test-svc", Namespace: "default", Port: 8080},
		},
	}
	store := newMockTabStore()

	// Use short timeout for testing (10 minutes minimum, so 10 minutes)
	idleTimeout := 10 * time.Minute
	mgr := New(catalog, store, idleTimeout)

	// Track status updates
	var statusUpdates []tabs.Tab
	var statusMu sync.Mutex
	mgr.SetStatusCallback(func(tab tabs.Tab) {
		statusMu.Lock()
		defer statusMu.Unlock()
		statusUpdates = append(statusUpdates, tab)
	})

	// Create a tab
	ctx := context.Background()
	tab, err := mgr.CreateTab(ctx, "test-project", "test-client")
	if err != nil {
		t.Fatalf("CreateTab failed: %v", err)
	}

	// Manually set lastActivity to trigger idle warning
	mgr.mu.Lock()
	entry := mgr.tabs[tab.TabID]
	entry.lastActivity = time.Now().Add(-9*time.Minute - 30*time.Second) // 9.5 minutes ago (warning threshold at 5 min before 10 min)
	mgr.mu.Unlock()

	// Run idle check
	mgr.checkIdleTabs(ctx)

	// Verify warning was sent
	statusMu.Lock()
	warningFound := false
	for _, update := range statusUpdates {
		if update.TabID == tab.TabID && update.LastError != nil && update.Status == tabs.StatusConnected {
			if update.LastError != nil && len(*update.LastError) > 0 {
				warningFound = true
				t.Logf("Warning message: %s", *update.LastError)
			}
		}
	}
	statusMu.Unlock()
	if !warningFound {
		t.Error("Expected warning to be sent, but none was found")
	}

	// Verify warned flag is set
	mgr.mu.Lock()
	if !entry.warned {
		t.Error("Expected warned flag to be true after warning")
	}
	mgr.mu.Unlock()

	// Set lastActivity to exceed timeout
	mgr.mu.Lock()
	entry.lastActivity = time.Now().Add(-11 * time.Minute) // 11 minutes ago (exceeds 10 min timeout)
	mgr.mu.Unlock()

	// Run idle check again
	mgr.checkIdleTabs(ctx)

	// Verify tab was closed
	mgr.mu.Lock()
	_, exists := mgr.tabs[tab.TabID]
	mgr.mu.Unlock()
	if exists {
		t.Error("Expected tab to be removed from manager after idle timeout")
	}

	// Wait briefly for async database delete to complete
	time.Sleep(50 * time.Millisecond)

	// Verify delete was called on store
	store.mu.Lock()
	deleted := false
	for _, id := range store.deleted {
		if id == tab.TabID {
			deleted = true
			break
		}
	}
	store.mu.Unlock()
	if !deleted {
		t.Error("Expected tab to be deleted from store after idle timeout")
	}

	// Verify closure event was sent with "idle timeout" error
	statusMu.Lock()
	closureFound := false
	for _, update := range statusUpdates {
		if update.TabID == tab.TabID && update.Status == tabs.StatusClosed {
			if update.LastError != nil && *update.LastError == "idle timeout" {
				closureFound = true
			}
		}
	}
	statusMu.Unlock()
	if !closureFound {
		t.Error("Expected closure event with 'idle timeout' error, but none was found")
	}
}

func TestManager_IdleTimeout_ActivityResetsTimer(t *testing.T) {
	catalog := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "test-project", Service: "test-svc", Namespace: "default", Port: 8080},
		},
	}
	store := newMockTabStore()

	mgr := New(catalog, store, 10*time.Minute)

	// Create a tab
	ctx := context.Background()
	tab, err := mgr.CreateTab(ctx, "test-project", "test-client")
	if err != nil {
		t.Fatalf("CreateTab failed: %v", err)
	}

	// Set lastActivity to approach timeout and set warned flag
	mgr.mu.Lock()
	entry := mgr.tabs[tab.TabID]
	entry.lastActivity = time.Now().Add(-9 * time.Minute)
	entry.warned = true
	mgr.mu.Unlock()

	// Record activity
	mgr.recordActivity(tab.TabID)

	// Verify lastActivity was updated and warned flag was cleared
	mgr.mu.Lock()
	idleDuration := time.Since(entry.lastActivity)
	warned := entry.warned
	mgr.mu.Unlock()

	if idleDuration > 1*time.Second {
		t.Errorf("Expected lastActivity to be recent, but it was %v ago", idleDuration)
	}

	if warned {
		t.Error("Expected warned flag to be cleared after activity")
	}
}

func TestManager_IdleChecker_StartsAndStops(t *testing.T) {
	catalog := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "test-project", Service: "test-svc", Namespace: "default", Port: 8080},
		},
	}
	store := newMockTabStore()

	mgr := New(catalog, store, 10*time.Minute)

	// Start idle checker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go mgr.StartIdleChecker(ctx)

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop via context cancellation
	cancel()

	// Give it a moment to stop
	time.Sleep(100 * time.Millisecond)

	// Test Stop() method
	mgr2 := New(catalog, store, 10*time.Minute)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	go mgr2.StartIdleChecker(ctx2)
	time.Sleep(100 * time.Millisecond)

	// Call Stop() - should close the stopIdleChecker channel
	mgr2.Stop()

	// Give it a moment to stop
	time.Sleep(100 * time.Millisecond)

	// If we reach here without deadlock, test passes
}

func TestManager_IdleTimeout_NoWarningIfAlreadyWarned(t *testing.T) {
	catalog := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{ID: "test-project", Service: "test-svc", Namespace: "default", Port: 8080},
		},
	}
	store := newMockTabStore()

	mgr := New(catalog, store, 10*time.Minute)

	// Track status updates
	var statusUpdates []tabs.Tab
	var statusMu sync.Mutex
	mgr.SetStatusCallback(func(tab tabs.Tab) {
		statusMu.Lock()
		defer statusMu.Unlock()
		statusUpdates = append(statusUpdates, tab)
	})

	// Create a tab
	ctx := context.Background()
	tab, err := mgr.CreateTab(ctx, "test-project", "test-client")
	if err != nil {
		t.Fatalf("CreateTab failed: %v", err)
	}

	// Set lastActivity to warning threshold and mark as already warned
	mgr.mu.Lock()
	entry := mgr.tabs[tab.TabID]
	entry.lastActivity = time.Now().Add(-9*time.Minute - 30*time.Second)
	entry.warned = true
	mgr.mu.Unlock()

	// Clear previous status updates
	statusMu.Lock()
	statusUpdates = []tabs.Tab{}
	statusMu.Unlock()

	// Run idle check
	mgr.checkIdleTabs(ctx)

	// Verify no new warning was sent
	statusMu.Lock()
	warningCount := 0
	for _, update := range statusUpdates {
		if update.TabID == tab.TabID && update.LastError != nil {
			warningCount++
		}
	}
	statusMu.Unlock()

	if warningCount > 0 {
		t.Errorf("Expected no warnings since already warned, but got %d warnings", warningCount)
	}
}
