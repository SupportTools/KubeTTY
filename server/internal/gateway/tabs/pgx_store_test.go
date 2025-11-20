package tabs

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) (*PGXStore, func()) {
	t.Helper()

	// Use test database connection from environment
	connStr := "postgres://postgres:postgres@localhost:5432/kubetty_test?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err, "failed to connect to test database")

	// Clean up any existing test data
	_, err = pool.Exec(context.Background(), "DELETE FROM gateway_tabs")
	require.NoError(t, err, "failed to clean test data")

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
		TabID:     "tab-1",
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
			TabID:     "tab-" + string(rune(i)),
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
		{TabID: "tab-1", ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-2", ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-3", ProjectID: "project-a", ClientID: "client-2", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-4", ProjectID: "project-a", ClientID: "client-3", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
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
		{TabID: "tab-1", ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-2", ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-3", ProjectID: "project-b", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-4", ProjectID: "project-c", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
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
		{TabID: "tab-1", ProjectID: "project-a", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-2", ProjectID: "project-a", ClientID: "client-2", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-3", ProjectID: "project-b", ClientID: "client-1", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-4", ProjectID: "project-b", ClientID: "client-2", Status: StatusConnected, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{TabID: "tab-5", ProjectID: "project-a", ClientID: "client-1", Status: StatusClosed, CreatedAt: time.Now(), UpdatedAt: time.Now()},
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
			TabID:     "tab-" + string(rune(i)),
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
	err = store.Delete(ctx, "tab-1")
	require.NoError(t, err)

	// Count should now be 2
	count, err = store.CountByClientAndProject(ctx, "client-1", "project-a")
	require.NoError(t, err)
	require.Equal(t, 2, count, "count should decrease after delete")
}
