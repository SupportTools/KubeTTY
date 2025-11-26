package tabs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/supporttools/KubeTTY/server/internal/gateway/metrics"
)

// ---- Unit tests (no database required) ----

// TestStatusConstants verifies the status constant values.
func TestStatusConstants(t *testing.T) {
	tests := []struct {
		status   Status
		expected string
	}{
		{StatusConnecting, "connecting"},
		{StatusConnected, "connected"},
		{StatusReconnecting, "reconnecting"},
		{StatusClosed, "closed"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("Status %v = %q, want %q", tt.status, string(tt.status), tt.expected)
		}
	}
}

// TestErrNotFound verifies the error sentinel.
func TestErrNotFound(t *testing.T) {
	if ErrNotFound == nil {
		t.Error("ErrNotFound should not be nil")
	}
	if ErrNotFound.Error() != "tab not found" {
		t.Errorf("ErrNotFound.Error() = %q, want %q", ErrNotFound.Error(), "tab not found")
	}
}

// TestTabStruct tests the Tab struct fields and JSON serialization.
func TestTabStruct(t *testing.T) {
	now := time.Now()
	errStr := "some error"
	uri := "ws://example.com"

	tab := Tab{
		TabID:         "test-tab-id",
		ProjectID:     "test-project",
		ClientID:      "test-client",
		Status:        StatusConnected,
		CreatedAt:     now,
		UpdatedAt:     now,
		LastError:     &errStr,
		DownstreamURI: &uri,
		Metrics:       &metrics.TabMetrics{},
	}

	// Verify fields
	if tab.TabID != "test-tab-id" {
		t.Errorf("TabID = %q, want %q", tab.TabID, "test-tab-id")
	}
	if tab.ProjectID != "test-project" {
		t.Errorf("ProjectID = %q, want %q", tab.ProjectID, "test-project")
	}
	if tab.ClientID != "test-client" {
		t.Errorf("ClientID = %q, want %q", tab.ClientID, "test-client")
	}
	if tab.Status != StatusConnected {
		t.Errorf("Status = %v, want %v", tab.Status, StatusConnected)
	}
	if *tab.LastError != errStr {
		t.Errorf("LastError = %q, want %q", *tab.LastError, errStr)
	}
	if *tab.DownstreamURI != uri {
		t.Errorf("DownstreamURI = %q, want %q", *tab.DownstreamURI, uri)
	}
}

// TestTabJSONSerialization tests Tab JSON encoding.
func TestTabJSONSerialization(t *testing.T) {
	now := time.Now().UTC().Round(time.Second) // Round for comparison

	tab := Tab{
		TabID:     "test-id",
		ProjectID: "project-1",
		ClientID:  "client-1",
		Status:    StatusConnecting,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Marshal
	data, err := json.Marshal(tab)
	if err != nil {
		t.Fatalf("failed to marshal Tab: %v", err)
	}

	// Unmarshal
	var decoded Tab
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Tab: %v", err)
	}

	// Verify fields
	if decoded.TabID != tab.TabID {
		t.Errorf("decoded TabID = %q, want %q", decoded.TabID, tab.TabID)
	}
	if decoded.Status != tab.Status {
		t.Errorf("decoded Status = %v, want %v", decoded.Status, tab.Status)
	}
}

// TestTabWithNilOptionalFields tests Tab with nil optional fields.
func TestTabWithNilOptionalFields(t *testing.T) {
	tab := Tab{
		TabID:         "test-id",
		ProjectID:     "project-1",
		ClientID:      "client-1",
		Status:        StatusConnecting,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		LastError:     nil,
		DownstreamURI: nil,
		Metrics:       nil,
	}

	// Marshal should succeed
	data, err := json.Marshal(tab)
	if err != nil {
		t.Fatalf("failed to marshal Tab with nil fields: %v", err)
	}

	// Should not contain omitempty fields
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Tab: %v", err)
	}

	// These should be omitted due to omitempty tag
	if _, ok := decoded["lastError"]; ok {
		t.Error("lastError should be omitted when nil")
	}
	if _, ok := decoded["downstreamUri"]; ok {
		t.Error("downstreamUri should be omitted when nil")
	}
	if _, ok := decoded["metrics"]; ok {
		t.Error("metrics should be omitted when nil")
	}
}

// TestNewPGXStore tests store creation.
func TestNewPGXStore(t *testing.T) {
	// Test with nil pool - should still create store (caller responsible for valid pool)
	store := NewPGXStore(nil)
	if store == nil {
		t.Error("NewPGXStore should return non-nil store")
	}
}

// TestStatusValues tests all status values are distinct.
func TestStatusValues(t *testing.T) {
	statuses := []Status{
		StatusConnecting,
		StatusConnected,
		StatusReconnecting,
		StatusClosed,
	}

	seen := make(map[Status]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate status value: %v", s)
		}
		seen[s] = true
	}
}

// ---- Integration tests (require database) ----

// testUUID generates a deterministic UUID for testing based on an index.
// Format: 00000000-0000-0000-0000-00000000000X where X is the index.
func testUUID(index int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", index)
}

func setupTestStore(t *testing.T) (*PGXStore, func()) {
	t.Helper()

	// Build connection string from environment variables
	host := os.Getenv("CNPG_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("CNPG_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("CNPG_USER")
	if user == "" {
		user = "kubetty_test"
	}
	password := os.Getenv("CNPG_PASSWORD")
	if password == "" {
		password = "kubetty_test"
	}
	database := os.Getenv("CNPG_DATABASE")
	if database == "" {
		database = "kubetty_test"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, database)
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Skipf("Skipping database test: database not available: %v", err)
	}

	// Clean up any existing test data
	_, err = pool.Exec(context.Background(), "DELETE FROM gateway_tabs")
	if err != nil {
		pool.Close()
		t.Skipf("Skipping database test: failed to clean test data: %v", err)
	}

	store := NewPGXStore(pool)

	cleanup := func() {
		pool.Exec(context.Background(), "DELETE FROM gateway_tabs")
		pool.Close()
	}

	return store, cleanup
}

func TestCountByClientAndProject_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	count, err := store.CountByClientAndProject(ctx, "client-1", "project-a")
	require.NoError(t, err)
	require.Equal(t, 0, count, "empty database should return count of 0")
}

func TestCountByClientAndProject_SingleTab(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a tab for client-1 on project-a
	tab := Tab{
		TabID:     testUUID(1),
		ProjectID: "project-a",
		ClientID:  "client-1",
		Status:    StatusConnecting,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := store.Create(ctx, tab)
	require.NoError(t, err)

	// Count should be 1
	count, err := store.CountByClientAndProject(ctx, "client-1", "project-a")
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestCountByClientAndProject_MultipleTabs_SameClient(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create 3 tabs for client-1 on project-a
	for i := 1; i <= 3; i++ {
		tab := Tab{
			TabID:     testUUID(i),
			ProjectID: "project-a",
			ClientID:  "client-1",
			Status:    StatusConnected,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err := store.Create(ctx, tab)
		require.NoError(t, err)
	}

	// Count should be 3
	count, err := store.CountByClientAndProject(ctx, "client-1", "project-a")
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func TestCountByClientAndProject_DifferentClients(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create tabs for different clients on same project
	tabs := []Tab{
		{TabID: testUUID(1), ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(2), ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(3), ProjectID: "project-a", ClientID: "client-2", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(4), ProjectID: "project-a", ClientID: "client-3", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	for _, tab := range tabs {
		err := store.Create(ctx, tab)
		require.NoError(t, err)
	}

	// client-1 should have 2 tabs
	count, err := store.CountByClientAndProject(ctx, "client-1", "project-a")
	require.NoError(t, err)
	require.Equal(t, 2, count, "client-1 should have 2 tabs")

	// client-2 should have 1 tab
	count, err = store.CountByClientAndProject(ctx, "client-2", "project-a")
	require.NoError(t, err)
	require.Equal(t, 1, count, "client-2 should have 1 tab")

	// client-3 should have 1 tab
	count, err = store.CountByClientAndProject(ctx, "client-3", "project-a")
	require.NoError(t, err)
	require.Equal(t, 1, count, "client-3 should have 1 tab")
}

func TestCountByClientAndProject_DifferentProjects(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create tabs for same client on different projects
	tabs := []Tab{
		{TabID: testUUID(1), ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(2), ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(3), ProjectID: "project-b", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(4), ProjectID: "project-c", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	for _, tab := range tabs {
		err := store.Create(ctx, tab)
		require.NoError(t, err)
	}

	// project-a should have 2 tabs for client-1
	count, err := store.CountByClientAndProject(ctx, "client-1", "project-a")
	require.NoError(t, err)
	require.Equal(t, 2, count, "client-1 should have 2 tabs on project-a")

	// project-b should have 1 tab for client-1
	count, err = store.CountByClientAndProject(ctx, "client-1", "project-b")
	require.NoError(t, err)
	require.Equal(t, 1, count, "client-1 should have 1 tab on project-b")

	// project-c should have 1 tab for client-1
	count, err = store.CountByClientAndProject(ctx, "client-1", "project-c")
	require.NoError(t, err)
	require.Equal(t, 1, count, "client-1 should have 1 tab on project-c")
}

func TestCountByClientAndProject_MixedScenario(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a complex scenario with multiple clients and projects
	tabs := []Tab{
		{TabID: testUUID(1), ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(2), ProjectID: "project-a", ClientID: "client-2", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(3), ProjectID: "project-b", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(4), ProjectID: "project-b", ClientID: "client-2", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(5), ProjectID: "project-a", ClientID: "client-1", Status: StatusClosed, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	for _, tab := range tabs {
		err := store.Create(ctx, tab)
		require.NoError(t, err)
	}

	// Verify counts include all statuses (closed tabs are still counted)
	count, err := store.CountByClientAndProject(ctx, "client-1", "project-a")
	require.NoError(t, err)
	require.Equal(t, 2, count, "should count both connected and closed tabs")

	count, err = store.CountByClientAndProject(ctx, "client-2", "project-b")
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestCountByClientAndProject_AfterDelete(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create 3 tabs
	for i := 1; i <= 3; i++ {
		tab := Tab{
			TabID:     testUUID(i),
			ProjectID: "project-a",
			ClientID:  "client-1",
			Status:    StatusConnected,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err := store.Create(ctx, tab)
		require.NoError(t, err)
	}

	// Verify count is 3
	count, err := store.CountByClientAndProject(ctx, "client-1", "project-a")
	require.NoError(t, err)
	require.Equal(t, 3, count)

	// Delete one tab
	err = store.Delete(ctx, testUUID(1))
	require.NoError(t, err)

	// Count should now be 2
	count, err = store.CountByClientAndProject(ctx, "client-1", "project-a")
	require.NoError(t, err)
	require.Equal(t, 2, count, "count should decrease after delete")
}

// ---- Additional integration tests for remaining Store methods ----

func TestGet(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a tab
	tab := Tab{
		TabID:     testUUID(100),
		ProjectID: "project-get",
		ClientID:  "client-get",
		Status:    StatusConnecting,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := store.Create(ctx, tab)
	require.NoError(t, err)

	// Get it
	fetched, err := store.Get(ctx, testUUID(100))
	require.NoError(t, err)
	require.NotNil(t, fetched)
	require.Equal(t, testUUID(100), fetched.TabID)
	require.Equal(t, "project-get", fetched.ProjectID)
	require.Equal(t, "client-get", fetched.ClientID)
	require.Equal(t, StatusConnecting, fetched.Status)
}

func TestGet_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Use a valid UUID format that doesn't exist
	_, err := store.Get(ctx, "99999999-9999-9999-9999-999999999999")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestGet_WithOptionalFields(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	errStr := "connection failed"
	uri := "ws://localhost:8080/ws"

	tab := Tab{
		TabID:         testUUID(101),
		ProjectID:     "project-opt",
		ClientID:      "client-opt",
		Status:        StatusReconnecting,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		LastError:     &errStr,
		DownstreamURI: &uri,
	}
	err := store.Create(ctx, tab)
	require.NoError(t, err)

	fetched, err := store.Get(ctx, testUUID(101))
	require.NoError(t, err)
	require.NotNil(t, fetched.LastError)
	require.Equal(t, "connection failed", *fetched.LastError)
	require.NotNil(t, fetched.DownstreamURI)
	require.Equal(t, "ws://localhost:8080/ws", *fetched.DownstreamURI)
}

func TestUpdateStatus(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a tab
	tab := Tab{
		TabID:     testUUID(200),
		ProjectID: "project-upd",
		ClientID:  "client-upd",
		Status:    StatusConnecting,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := store.Create(ctx, tab)
	require.NoError(t, err)

	// Update status to connected
	err = store.UpdateStatus(ctx, testUUID(200), StatusConnected, nil, nil)
	require.NoError(t, err)

	// Verify
	fetched, err := store.Get(ctx, testUUID(200))
	require.NoError(t, err)
	require.Equal(t, StatusConnected, fetched.Status)
}

func TestUpdateStatus_WithError(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a tab
	tab := Tab{
		TabID:     testUUID(201),
		ProjectID: "project-err",
		ClientID:  "client-err",
		Status:    StatusConnecting,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := store.Create(ctx, tab)
	require.NoError(t, err)

	// Update with error
	errMsg := "connection timeout"
	err = store.UpdateStatus(ctx, testUUID(201), StatusReconnecting, &errMsg, nil)
	require.NoError(t, err)

	fetched, err := store.Get(ctx, testUUID(201))
	require.NoError(t, err)
	require.Equal(t, StatusReconnecting, fetched.Status)
	require.NotNil(t, fetched.LastError)
	require.Equal(t, "connection timeout", *fetched.LastError)
}

func TestUpdateStatus_WithDownstreamURI(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	tab := Tab{
		TabID:     testUUID(202),
		ProjectID: "project-uri",
		ClientID:  "client-uri",
		Status:    StatusConnecting,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := store.Create(ctx, tab)
	require.NoError(t, err)

	// Update with downstream URI
	uri := "ws://pod-1.project-uri:8080/ws"
	err = store.UpdateStatus(ctx, testUUID(202), StatusConnected, nil, &uri)
	require.NoError(t, err)

	fetched, err := store.Get(ctx, testUUID(202))
	require.NoError(t, err)
	require.NotNil(t, fetched.DownstreamURI)
	require.Equal(t, "ws://pod-1.project-uri:8080/ws", *fetched.DownstreamURI)
}

func TestUpdateStatus_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Use a valid UUID format that doesn't exist
	err := store.UpdateStatus(ctx, "99999999-9999-9999-9999-999999999999", StatusConnected, nil, nil)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUpdateClientID(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a tab
	tab := Tab{
		TabID:     testUUID(300),
		ProjectID: "project-client",
		ClientID:  "original-client",
		Status:    StatusConnected,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := store.Create(ctx, tab)
	require.NoError(t, err)

	// Update client ID (force takeover scenario)
	err = store.UpdateClientID(ctx, testUUID(300), "new-client")
	require.NoError(t, err)

	// Verify
	fetched, err := store.Get(ctx, testUUID(300))
	require.NoError(t, err)
	require.Equal(t, "new-client", fetched.ClientID)
}

func TestUpdateClientID_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Use a valid UUID format that doesn't exist
	err := store.UpdateClientID(ctx, "99999999-9999-9999-9999-999999999999", "new-client")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestListByClient(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create tabs for different clients
	tabs := []Tab{
		{TabID: testUUID(400), ProjectID: "proj-a", ClientID: "client-list", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(401), ProjectID: "proj-b", ClientID: "client-list", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(402), ProjectID: "proj-c", ClientID: "client-list", Status: StatusClosed, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(403), ProjectID: "proj-d", ClientID: "other-client", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	for _, tab := range tabs {
		err := store.Create(ctx, tab)
		require.NoError(t, err)
	}

	// List for client-list
	result, err := store.ListByClient(ctx, "client-list", 50)
	require.NoError(t, err)
	require.Len(t, result, 3)

	// List for other-client
	result, err = store.ListByClient(ctx, "other-client", 50)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestListByClient_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	result, err := store.ListByClient(ctx, "nonexistent-client", 50)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestListByClient_WithLimit(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create 10 tabs
	for i := 0; i < 10; i++ {
		tab := Tab{
			TabID:     testUUID(500 + i),
			ProjectID: fmt.Sprintf("proj-%d", i),
			ClientID:  "limit-client",
			Status:    StatusConnected,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err := store.Create(ctx, tab)
		require.NoError(t, err)
	}

	// List with limit of 5
	result, err := store.ListByClient(ctx, "limit-client", 5)
	require.NoError(t, err)
	require.Len(t, result, 5)

	// List with zero limit (defaults to 50)
	result, err = store.ListByClient(ctx, "limit-client", 0)
	require.NoError(t, err)
	require.Len(t, result, 10)
}

func TestListAll(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create tabs for different clients
	tabs := []Tab{
		{TabID: testUUID(600), ProjectID: "proj-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(601), ProjectID: "proj-b", ClientID: "client-2", Status: StatusConnecting, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: testUUID(602), ProjectID: "proj-c", ClientID: "client-3", Status: StatusReconnecting, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	for _, tab := range tabs {
		err := store.Create(ctx, tab)
		require.NoError(t, err)
	}

	result, err := store.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, result, 3)
}

func TestListAll_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	result, err := store.ListAll(ctx)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestCreate_Upsert(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create initial tab
	tab := Tab{
		TabID:     testUUID(700),
		ProjectID: "proj-upsert",
		ClientID:  "client-upsert",
		Status:    StatusConnecting,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := store.Create(ctx, tab)
	require.NoError(t, err)

	// Create again with same ID but different data (upsert)
	tab.Status = StatusConnected
	tab.ProjectID = "proj-upsert-updated"
	err = store.Create(ctx, tab)
	require.NoError(t, err)

	// Verify the upsert worked
	fetched, err := store.Get(ctx, testUUID(700))
	require.NoError(t, err)
	require.Equal(t, StatusConnected, fetched.Status)
	require.Equal(t, "proj-upsert-updated", fetched.ProjectID)
}

func TestFullTabLifecycle(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Create tab
	tab := Tab{
		TabID:     testUUID(800),
		ProjectID: "lifecycle-proj",
		ClientID:  "lifecycle-client",
		Status:    StatusConnecting,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := store.Create(ctx, tab)
	require.NoError(t, err)

	// 2. Verify exists
	fetched, err := store.Get(ctx, testUUID(800))
	require.NoError(t, err)
	require.Equal(t, StatusConnecting, fetched.Status)

	// 3. Update status to connected with downstream URI
	uri := "ws://pod.ns:8080"
	err = store.UpdateStatus(ctx, testUUID(800), StatusConnected, nil, &uri)
	require.NoError(t, err)

	// 4. Verify update
	fetched, err = store.Get(ctx, testUUID(800))
	require.NoError(t, err)
	require.Equal(t, StatusConnected, fetched.Status)
	require.Equal(t, "ws://pod.ns:8080", *fetched.DownstreamURI)

	// 5. Simulate force takeover
	err = store.UpdateClientID(ctx, testUUID(800), "new-lifecycle-client")
	require.NoError(t, err)

	// 6. Verify client changed
	fetched, err = store.Get(ctx, testUUID(800))
	require.NoError(t, err)
	require.Equal(t, "new-lifecycle-client", fetched.ClientID)

	// 7. Set error on reconnecting
	errMsg := "pod restarted"
	err = store.UpdateStatus(ctx, testUUID(800), StatusReconnecting, &errMsg, nil)
	require.NoError(t, err)

	// 8. Verify shows in client list
	list, err := store.ListByClient(ctx, "new-lifecycle-client", 50)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, StatusReconnecting, list[0].Status)

	// 9. Delete tab
	err = store.Delete(ctx, testUUID(800))
	require.NoError(t, err)

	// 10. Verify deleted
	_, err = store.Get(ctx, testUUID(800))
	require.ErrorIs(t, err, ErrNotFound)

	// 11. Verify not in list anymore
	list, err = store.ListByClient(ctx, "new-lifecycle-client", 50)
	require.NoError(t, err)
	require.Empty(t, list)
}
