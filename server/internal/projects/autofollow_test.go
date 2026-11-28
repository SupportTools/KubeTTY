package projects

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v2"
	"github.com/stretchr/testify/require"
)

// cluster-services-1376: AutoFollowImage is the whole "no live user" safety
// check for automatic sandbox upgrades, done in one conditional UPDATE.

func TestPGStore_AutoFollowImage_Args(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	id := uuid.New()
	idleBefore := time.Date(2026, 11, 14, 11, 30, 0, 0, time.UTC)
	mock.ExpectExec(`UPDATE kubetty_projects p\s+SET image_tag = \$3, status = 'updating'`).
		WithArgs(id, "1.15.0", "1.16.1", "Auto-upgrading to 1.16.1", idleBefore).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	ok, err := store.AutoFollowImage(context.Background(), id, "1.15.0", "1.16.1", idleBefore)
	require.NoError(t, err)
	require.True(t, ok)

	mock.ExpectExec(`UPDATE kubetty_projects p`).
		WithArgs(id, "1.15.0", "1.16.1", "Auto-upgrading to 1.16.1", idleBefore).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	ok, err = store.AutoFollowImage(context.Background(), id, "1.15.0", "1.16.1", idleBefore)
	require.NoError(t, err)
	require.False(t, ok, "0 rows means a condition did not hold; nothing was upgraded")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPGStore_AutoFollowImage_StatementGuards(t *testing.T) {
	// Every guard the controller relies on must be in the statement.
	for _, guard := range []string{
		"p.image_tag = $2",
		"p.status = 'running'",
		"p.paused = FALSE",
		"p.image_auto_follow = TRUE",
		"p.deleted_at IS NULL",
		"(p.last_activity IS NULL OR p.last_activity::timestamptz < $5::timestamptz)",
		"NOT EXISTS",
		"t.project_id = p.name",
		"t.status IN ('connecting', 'connected', 'reconnecting')",
	} {
		require.Contains(t, autoFollowImageStmt, guard)
	}
}

// ---- Integration (real Postgres with all migrations applied) ----

func TestIntegration_AutoFollowImage(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	pool := store.pool
	addTab := func(project, status string) {
		_, err := pool.Exec(ctx, `INSERT INTO gateway_tabs (tab_id, project_id, client_id, status) VALUES ($1, $2, 'c1', $3)`,
			uuid.New(), project, status)
		require.NoError(t, err)
	}
	clearTabs := func(project string) {
		_, err := pool.Exec(ctx, `DELETE FROM gateway_tabs WHERE project_id = $1`, project)
		require.NoError(t, err)
	}
	defer clearTabs("af-int")

	p, err := store.Create(ctx, CreateProjectRequest{Name: "af-int", DisplayName: "AF", UserName: "u", ImageTag: "1.15.0"})
	require.NoError(t, err)
	require.True(t, p.ImageAutoFollow, "new projects follow by default (column default TRUE)")
	require.NoError(t, store.SetStatus(ctx, p.ID, StatusRunning, ""))

	future := time.Now().Add(time.Minute) // "idle since before now+1m": activity never blocks

	// An open tab blocks, whatever its activity.
	for _, st := range []string{"connecting", "connected", "reconnecting"} {
		clearTabs("af-int")
		addTab("af-int", st)
		ok, err := store.AutoFollowImage(ctx, p.ID, "1.15.0", "1.16.1", future)
		require.NoError(t, err)
		require.False(t, ok, "tab in %s must block the upgrade", st)
	}
	// A closed tab is not a session.
	clearTabs("af-int")
	addTab("af-int", "closed")

	// Recent activity blocks.
	require.NoError(t, store.UpdateLastActivity(ctx, "af-int"))
	ok, err := store.AutoFollowImage(ctx, p.ID, "1.15.0", "1.16.1", time.Now().Add(-30*time.Minute))
	require.NoError(t, err)
	require.False(t, ok, "activity within the grace period must block")

	// Opt-out blocks.
	off := false
	_, err = store.Update(ctx, p.ID, UpdateProjectRequest{ImageAutoFollow: &off})
	require.NoError(t, err)
	ok, err = store.AutoFollowImage(ctx, p.ID, "1.15.0", "1.16.1", future)
	require.NoError(t, err)
	require.False(t, ok, "image_auto_follow=false must block")
	on := true
	_, err = store.Update(ctx, p.ID, UpdateProjectRequest{ImageAutoFollow: &on})
	require.NoError(t, err)

	// Paused blocks.
	require.NoError(t, store.SetPaused(ctx, p.ID, true))
	ok, err = store.AutoFollowImage(ctx, p.ID, "1.15.0", "1.16.1", future)
	require.NoError(t, err)
	require.False(t, ok, "paused must block")
	require.NoError(t, store.SetPaused(ctx, p.ID, false))

	// A tag changed since the decision blocks.
	ok, err = store.AutoFollowImage(ctx, p.ID, "1.14.0", "1.16.1", future)
	require.NoError(t, err)
	require.False(t, ok, "stale fromTag must block")

	// Idle: last activity 2h ago, grace 30m. This also pins the time zone
	// handling: last_activity is a TIMESTAMP without zone, and comparing it to
	// an uncast Go time is off by the client's UTC offset.
	_, err = pool.Exec(ctx, `UPDATE kubetty_projects SET last_activity = NOW() - INTERVAL '2 hours' WHERE id = $1`, p.ID)
	require.NoError(t, err)
	ok, err = store.AutoFollowImage(ctx, p.ID, "1.15.0", "1.16.1", time.Now().Add(-30*time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	got, err := store.Get(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, "1.16.1", got.ImageTag)
	require.Equal(t, StatusUpdating, got.Status)
	require.Equal(t, "Auto-upgrading to 1.16.1", got.StatusMessage)

	// Not running any more (updating): a second pass is a no-op.
	ok, err = store.AutoFollowImage(ctx, p.ID, "1.16.1", "1.16.2", future)
	require.NoError(t, err)
	require.False(t, ok, "only running projects are upgraded")
}
