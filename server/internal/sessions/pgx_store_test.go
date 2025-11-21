package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const testConnString = "postgres://postgres:postgres@localhost:5432/kubetty_test?sslmode=disable"

// newTestStore creates a PGXStore connected to the test database.
// Skips the test if the database is not available or tables don't exist.
func newTestStore(t *testing.T) *PGXStore {
	t.Helper()
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(testConnString)
	if err != nil {
		t.Skipf("Skipping database test: failed to parse connection string: %v", err)
	}

	store, err := NewPGXStore(ctx, config)
	if err != nil {
		t.Skipf("Skipping database test: database not available: %v", err)
	}
	if store == nil {
		t.Skip("Skipping database test: store is nil")
	}

	// Verify the required tables exist
	var exists bool
	err = store.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_name = 'sessions'
		)
	`).Scan(&exists)
	if err != nil || !exists {
		store.Close()
		t.Skipf("Skipping database test: sessions table not found: %v", err)
	}

	return store
}

// cleanupSessions removes all sessions from the test database.
func cleanupSessions(t *testing.T, ctx context.Context, store *PGXStore) {
	t.Helper()
	_, err := store.pool.Exec(ctx, "DELETE FROM sessions")
	require.NoError(t, err)
}

// cleanupLogs removes all log entries from the test database.
func cleanupLogs(t *testing.T, ctx context.Context, store *PGXStore) {
	t.Helper()
	_, err := store.pool.Exec(ctx, "DELETE FROM session_logs")
	require.NoError(t, err)
}

// cleanupAll removes all test data.
func cleanupAll(t *testing.T, ctx context.Context, store *PGXStore) {
	t.Helper()
	cleanupLogs(t, ctx, store)
	cleanupSessions(t, ctx, store)
}

func TestPGXStore_UpsertSession(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	t.Run("insert new session", func(t *testing.T) {
		cleanupSessions(t, ctx, store)

		sessionID := uuid.NewString()
		session := Session{
			SessionID:    sessionID,
			DeploymentID: "deploy-1",
			ShellPID:     12345,
		}

		err := store.UpsertSession(ctx, session)
		require.NoError(t, err)

		// Verify session was created
		retrieved, err := store.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.Equal(t, sessionID, retrieved.SessionID)
		require.Equal(t, "deploy-1", retrieved.DeploymentID)
		require.Equal(t, 12345, retrieved.ShellPID)
	})

	t.Run("update existing session", func(t *testing.T) {
		cleanupSessions(t, ctx, store)

		// Insert initial session
		sessionID := uuid.NewString()
		session := Session{
			SessionID:    sessionID,
			DeploymentID: "deploy-2",
			ShellPID:     100,
		}
		err := store.UpsertSession(ctx, session)
		require.NoError(t, err)

		// Update with new PID
		session.ShellPID = 200
		err = store.UpsertSession(ctx, session)
		require.NoError(t, err)

		// Verify update
		retrieved, err := store.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.Equal(t, 200, retrieved.ShellPID)
	})

	t.Run("upsert with attachment info", func(t *testing.T) {
		cleanupSessions(t, ctx, store)

		sessionID := uuid.NewString()
		clientID := "client-123"
		attachedAt := time.Now()
		session := Session{
			SessionID:    sessionID,
			DeploymentID: "deploy-3",
			ShellPID:     300,
			AttachedTo:   &clientID,
			AttachedAt:   &attachedAt,
		}

		err := store.UpsertSession(ctx, session)
		require.NoError(t, err)

		retrieved, err := store.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.NotNil(t, retrieved.AttachedTo)
		require.Equal(t, "client-123", *retrieved.AttachedTo)
		require.NotNil(t, retrieved.AttachedAt)
	})
}

func TestPGXStore_GetSession(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	t.Run("get existing session", func(t *testing.T) {
		cleanupSessions(t, ctx, store)

		sessionID := uuid.NewString()
		session := Session{
			SessionID:    sessionID,
			DeploymentID: "deploy-1",
			ShellPID:     555,
		}
		err := store.UpsertSession(ctx, session)
		require.NoError(t, err)

		retrieved, err := store.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.Equal(t, sessionID, retrieved.SessionID)
		require.Equal(t, 0, retrieved.LogCount)
		require.Nil(t, retrieved.LastLogAt)
	})

	t.Run("get non-existent session", func(t *testing.T) {
		nonexistentID := uuid.NewString()
		_, err := store.GetSession(ctx, nonexistentID)
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("get session with logs", func(t *testing.T) {
		cleanupAll(t, ctx, store)

		// Create session
		sessionID := uuid.NewString()
		session := Session{
			SessionID:    sessionID,
			DeploymentID: "deploy-1",
			ShellPID:     777,
		}
		err := store.UpsertSession(ctx, session)
		require.NoError(t, err)

		// Add log entries
		for i := 0; i < 3; i++ {
			log := LogEntry{
				SessionID: sessionID,
				Direction: "in",
				Data:      []byte("test data"),
			}
			err := store.AppendLog(ctx, log)
			require.NoError(t, err)
		}

		// Retrieve session - should have log count
		retrieved, err := store.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.Equal(t, 3, retrieved.LogCount)
		require.NotNil(t, retrieved.LastLogAt)
	})
}

func TestPGXStore_ListSessions(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	t.Run("list empty", func(t *testing.T) {
		cleanupSessions(t, ctx, store)

		sessions, err := store.ListSessions(ctx, "deploy-1")
		require.NoError(t, err)
		require.Empty(t, sessions)
	})

	t.Run("list multiple sessions", func(t *testing.T) {
		cleanupSessions(t, ctx, store)

		// Create 3 sessions for same deployment
		for i := 1; i <= 3; i++ {
			session := Session{
				SessionID:    uuid.NewString(),
				DeploymentID: "deploy-list",
				ShellPID:     100 + i,
			}
			err := store.UpsertSession(ctx, session)
			require.NoError(t, err)
		}

		// Create 1 session for different deployment
		other := Session{
			SessionID:    uuid.NewString(),
			DeploymentID: "deploy-other",
			ShellPID:     999,
		}
		err := store.UpsertSession(ctx, other)
		require.NoError(t, err)

		// List should only return sessions for target deployment
		sessions, err := store.ListSessions(ctx, "deploy-list")
		require.NoError(t, err)
		require.Len(t, sessions, 3)
	})
}

func TestPGXStore_DeleteSession(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	t.Run("delete existing session", func(t *testing.T) {
		cleanupSessions(t, ctx, store)

		sessionID := uuid.NewString()
		session := Session{
			SessionID:    sessionID,
			DeploymentID: "deploy-1",
			ShellPID:     123,
		}
		err := store.UpsertSession(ctx, session)
		require.NoError(t, err)

		// Delete session
		err = store.DeleteSession(ctx, sessionID)
		require.NoError(t, err)

		// Verify session is gone
		_, err = store.GetSession(ctx, sessionID)
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("delete non-existent session", func(t *testing.T) {
		// Should not error
		nonexistentID := uuid.NewString()
		err := store.DeleteSession(ctx, nonexistentID)
		require.NoError(t, err)
	})

	t.Run("delete cascades logs", func(t *testing.T) {
		cleanupAll(t, ctx, store)

		// Create session with logs
		sessionID := uuid.NewString()
		session := Session{
			SessionID:    sessionID,
			DeploymentID: "deploy-1",
			ShellPID:     456,
		}
		err := store.UpsertSession(ctx, session)
		require.NoError(t, err)

		log := LogEntry{
			SessionID: sessionID,
			Direction: "out",
			Data:      []byte("test"),
		}
		err = store.AppendLog(ctx, log)
		require.NoError(t, err)

		// Delete session
		err = store.DeleteSession(ctx, sessionID)
		require.NoError(t, err)

		// Logs should be gone too
		result, err := store.ListLogs(ctx, sessionID, 100, nil)
		require.NoError(t, err)
		require.Empty(t, result.Logs)
	})
}

func TestPGXStore_SetAttachment(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	sessionID := uuid.NewString()
	session := Session{
		SessionID:    sessionID,
		DeploymentID: "deploy-1",
		ShellPID:     789,
	}
	err := store.UpsertSession(ctx, session)
	require.NoError(t, err)

	t.Run("attach client", func(t *testing.T) {
		err := store.SetAttachment(ctx, sessionID, "client-1", true)
		require.NoError(t, err)

		retrieved, err := store.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.NotNil(t, retrieved.AttachedTo)
		require.Equal(t, "client-1", *retrieved.AttachedTo)
		require.NotNil(t, retrieved.AttachedAt)
	})

	t.Run("detach client", func(t *testing.T) {
		err := store.SetAttachment(ctx, sessionID, "client-1", false)
		require.NoError(t, err)

		retrieved, err := store.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.Nil(t, retrieved.AttachedTo)
		require.Nil(t, retrieved.AttachedAt)
	})
}

func TestPGXStore_ClearAttachments(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	t.Run("clear all attachments for deployment", func(t *testing.T) {
		cleanupSessions(t, ctx, store)

		// Create 2 attached sessions
		for i := 1; i <= 2; i++ {
			sessionID := uuid.NewString()
			session := Session{
				SessionID:    sessionID,
				DeploymentID: "deploy-clear",
				ShellPID:     100 + i,
			}
			err := store.UpsertSession(ctx, session)
			require.NoError(t, err)

			clientID := "client-" + string(rune('0'+i))
			err = store.SetAttachment(ctx, sessionID, clientID, true)
			require.NoError(t, err)
		}

		// Clear all attachments
		err := store.ClearAttachments(ctx, "deploy-clear")
		require.NoError(t, err)

		// Verify both sessions are detached
		sessions, err := store.ListSessions(ctx, "deploy-clear")
		require.NoError(t, err)
		for _, s := range sessions {
			require.Nil(t, s.AttachedTo)
			require.Nil(t, s.AttachedAt)
		}
	})
}

func TestPGXStore_AppendLog(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	// Create session
	sessionID := uuid.NewString()
	session := Session{
		SessionID:    sessionID,
		DeploymentID: "deploy-1",
		ShellPID:     321,
	}
	err := store.UpsertSession(ctx, session)
	require.NoError(t, err)

	t.Run("append log entry", func(t *testing.T) {
		cleanupLogs(t, ctx, store)

		log := LogEntry{
			SessionID: sessionID,
			Direction: "in",
			Data:      []byte("ls -la"),
		}
		err := store.AppendLog(ctx, log)
		require.NoError(t, err)

		// Retrieve logs
		result, err := store.ListLogs(ctx, sessionID, 10, nil)
		require.NoError(t, err)
		require.Len(t, result.Logs, 1)
		require.Equal(t, "in", result.Logs[0].Direction)
		require.Equal(t, []byte("ls -la"), result.Logs[0].Data)
	})

	t.Run("append multiple logs", func(t *testing.T) {
		cleanupLogs(t, ctx, store)

		for i := 0; i < 5; i++ {
			log := LogEntry{
				SessionID: sessionID,
				Direction: "out",
				Data:      []byte("line " + string(rune('0'+i))),
			}
			err := store.AppendLog(ctx, log)
			require.NoError(t, err)
		}

		result, err := store.ListLogs(ctx, sessionID, 100, nil)
		require.NoError(t, err)
		require.Len(t, result.Logs, 5)
	})
}

func TestPGXStore_ListLogs(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	// Create session
	sessionID := uuid.NewString()
	session := Session{
		SessionID:    sessionID,
		DeploymentID: "deploy-1",
		ShellPID:     654,
	}
	err := store.UpsertSession(ctx, session)
	require.NoError(t, err)

	t.Run("list with limit", func(t *testing.T) {
		cleanupLogs(t, ctx, store)

		// Create 10 log entries
		for i := 0; i < 10; i++ {
			log := LogEntry{
				SessionID: sessionID,
				Direction: "in",
				Data:      []byte("entry " + string(rune('0'+i))),
			}
			err := store.AppendLog(ctx, log)
			require.NoError(t, err)
		}

		// List with limit of 5
		result, err := store.ListLogs(ctx, sessionID, 5, nil)
		require.NoError(t, err)
		require.Len(t, result.Logs, 5)
	})

	t.Run("list empty", func(t *testing.T) {
		cleanupLogs(t, ctx, store)

		result, err := store.ListLogs(ctx, sessionID, 10, nil)
		require.NoError(t, err)
		require.Empty(t, result.Logs)
	})
}

func TestPGXStore_PruneLogs(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	// Create session
	sessionID := uuid.NewString()
	session := Session{
		SessionID:    sessionID,
		DeploymentID: "deploy-1",
		ShellPID:     987,
	}
	err := store.UpsertSession(ctx, session)
	require.NoError(t, err)

	t.Run("prune old logs", func(t *testing.T) {
		cleanupLogs(t, ctx, store)

		now := time.Now()

		// Create old log (simulate)
		oldLog := LogEntry{
			SessionID: sessionID,
			Direction: "out",
			Data:      []byte("old"),
			CreatedAt: now.Add(-2 * time.Hour),
		}
		err := store.AppendLog(ctx, oldLog)
		require.NoError(t, err)

		// Create recent log
		recentLog := LogEntry{
			SessionID: sessionID,
			Direction: "out",
			Data:      []byte("recent"),
		}
		err = store.AppendLog(ctx, recentLog)
		require.NoError(t, err)

		// Prune logs older than 1 hour
		cutoff := now.Add(-1 * time.Hour)
		count, err := store.PruneLogs(ctx, cutoff)
		require.NoError(t, err)
		require.Equal(t, int64(1), count)

		// Recent log should remain
		result, err := store.ListLogs(ctx, sessionID, 10, nil)
		require.NoError(t, err)
		require.Len(t, result.Logs, 1)
		require.Equal(t, []byte("recent"), result.Logs[0].Data)
	})
}

func TestPGXStore_TrimLogs(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	// Create session
	sessionID := uuid.NewString()
	session := Session{
		SessionID:    sessionID,
		DeploymentID: "deploy-1",
		ShellPID:     111,
	}
	err := store.UpsertSession(ctx, session)
	require.NoError(t, err)

	t.Run("trim excess logs", func(t *testing.T) {
		cleanupLogs(t, ctx, store)

		// Create 10 log entries
		for i := 0; i < 10; i++ {
			log := LogEntry{
				SessionID: sessionID,
				Direction: "in",
				Data:      []byte("entry " + string(rune('0'+i))),
			}
			err := store.AppendLog(ctx, log)
			require.NoError(t, err)
			// Small delay to ensure distinct timestamps
			time.Sleep(1 * time.Millisecond)
		}

		// Trim to keep only 5 newest
		count, err := store.TrimLogs(ctx, 5)
		require.NoError(t, err)
		require.Equal(t, int64(5), count)

		// Verify only 5 logs remain
		result, err := store.ListLogs(ctx, sessionID, 100, nil)
		require.NoError(t, err)
		require.Len(t, result.Logs, 5)
	})

	t.Run("trim with zero max entries", func(t *testing.T) {
		count, err := store.TrimLogs(ctx, 0)
		require.NoError(t, err)
		require.Equal(t, int64(0), count)
	})
}

func TestPGXStore_Ping(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	err := store.Ping(ctx)
	require.NoError(t, err)
}
