package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v2"
)

// Helper to create a mock pool for testing
func setupMockPool(t *testing.T) (pgxmock.PgxPoolIface, *PGXStore) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	store := NewPGXStoreWithPool(mock)
	return mock, store
}

func TestNewPGXStoreWithPool(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewPGXStoreWithPool(mock)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.pool == nil {
		t.Fatal("expected non-nil pool in store")
	}
}

func TestPGXStore_Ping(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, err := pgxmock.NewPool(pgxmock.MonitorPingsOption(true))
		if err != nil {
			t.Fatalf("failed to create mock pool: %v", err)
		}
		defer mock.Close()
		store := NewPGXStoreWithPool(mock)

		mock.ExpectPing()

		err = store.Ping(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("error", func(t *testing.T) {
		mock, err := pgxmock.NewPool(pgxmock.MonitorPingsOption(true))
		if err != nil {
			t.Fatalf("failed to create mock pool: %v", err)
		}
		defer mock.Close()
		store := NewPGXStoreWithPool(mock)

		expectedErr := errors.New("connection refused")
		mock.ExpectPing().WillReturnError(expectedErr)

		err = store.Ping(context.Background())
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("error = %v, want %v", err, expectedErr)
		}
	})
}

func TestPGXStore_Close(t *testing.T) {
	mock, store := setupMockPool(t)
	mock.ExpectClose()

	store.Close()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestPGXStore_GetSession(t *testing.T) {
	columns := []string{
		"session_uuid", "deployment_id", "shell_pid", "created_at", "updated_at",
		"forked_from", "attached_to", "attached_at", "state", "last_log_at", "log_count",
	}
	now := time.Now()

	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		sessionID := "test-session-uuid"
		attachedTo := "client-123"
		attachedAt := now.Add(-time.Hour)

		mock.ExpectQuery(`SELECT s.session_uuid`).
			WithArgs(sessionID).
			WillReturnRows(pgxmock.NewRows(columns).
				AddRow(sessionID, "deploy-1", 12345, now, now, nil, &attachedTo, &attachedAt, []byte("active"), &now, 10))

		sess, err := store.GetSession(context.Background(), sessionID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sess.SessionID != sessionID {
			t.Errorf("SessionID = %s, want %s", sess.SessionID, sessionID)
		}
		if sess.DeploymentID != "deploy-1" {
			t.Errorf("DeploymentID = %s, want deploy-1", sess.DeploymentID)
		}
		if sess.ShellPID != 12345 {
			t.Errorf("ShellPID = %d, want 12345", sess.ShellPID)
		}
		if sess.AttachedTo == nil || *sess.AttachedTo != attachedTo {
			t.Errorf("AttachedTo = %v, want %s", sess.AttachedTo, attachedTo)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT s.session_uuid`).
			WithArgs("nonexistent").
			WillReturnError(pgx.ErrNoRows)

		_, err := store.GetSession(context.Background(), "nonexistent")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		expectedErr := errors.New("database error")
		mock.ExpectQuery(`SELECT s.session_uuid`).
			WithArgs("test-id").
			WillReturnError(expectedErr)

		_, err := store.GetSession(context.Background(), "test-id")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGXStore_ListSessions(t *testing.T) {
	columns := []string{
		"session_uuid", "deployment_id", "shell_pid", "created_at", "updated_at",
		"forked_from", "attached_to", "attached_at", "state", "last_log_at", "log_count",
	}
	now := time.Now()

	t.Run("success with results", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		deploymentID := "deploy-1"
		mock.ExpectQuery(`SELECT s.session_uuid`).
			WithArgs(deploymentID).
			WillReturnRows(pgxmock.NewRows(columns).
				AddRow("sess-1", deploymentID, 1001, now, now, nil, nil, nil, []byte("active"), nil, 0).
				AddRow("sess-2", deploymentID, 1002, now, now, nil, nil, nil, []byte("active"), nil, 5))

		sessions, err := store.ListSessions(context.Background(), deploymentID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sessions) != 2 {
			t.Errorf("len(sessions) = %d, want 2", len(sessions))
		}
		if sessions[0].SessionID != "sess-1" {
			t.Errorf("sessions[0].SessionID = %s, want sess-1", sessions[0].SessionID)
		}
	})

	t.Run("empty results", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT s.session_uuid`).
			WithArgs("no-sessions").
			WillReturnRows(pgxmock.NewRows(columns))

		sessions, err := store.ListSessions(context.Background(), "no-sessions")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sessions) != 0 {
			t.Errorf("len(sessions) = %d, want 0", len(sessions))
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT s.session_uuid`).
			WithArgs("deploy-1").
			WillReturnError(errors.New("connection lost"))

		_, err := store.ListSessions(context.Background(), "deploy-1")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGXStore_UpsertSession(t *testing.T) {
	t.Run("success with zero timestamps", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		sess := Session{
			SessionID:    "new-session",
			DeploymentID: "deploy-1",
			ShellPID:     5000,
			State:        []byte("active"),
		}

		// Use AnyArg for timestamp and pointer fields as pgxmock is strict about types
		mock.ExpectExec(`INSERT INTO sessions`).
			WithArgs(
				sess.SessionID,
				sess.DeploymentID,
				sess.ShellPID,
				pgxmock.AnyArg(), // createdAt
				pgxmock.AnyArg(), // updatedAt
				pgxmock.AnyArg(), // forkedFrom
				pgxmock.AnyArg(), // attachedTo
				pgxmock.AnyArg(), // attachedAt
				sess.State,
			).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := store.UpsertSession(context.Background(), sess)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("success with timestamps", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		now := time.Now()
		sess := Session{
			SessionID:    "existing-session",
			DeploymentID: "deploy-1",
			ShellPID:     5001,
			CreatedAt:    now,
			UpdatedAt:    now,
			State:        []byte("active"),
		}

		mock.ExpectExec(`INSERT INTO sessions`).
			WithArgs(
				sess.SessionID,
				sess.DeploymentID,
				sess.ShellPID,
				pgxmock.AnyArg(), // createdAt
				pgxmock.AnyArg(), // updatedAt
				pgxmock.AnyArg(), // forkedFrom
				pgxmock.AnyArg(), // attachedTo
				pgxmock.AnyArg(), // attachedAt
				sess.State,
			).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := store.UpsertSession(context.Background(), sess)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		sess := Session{SessionID: "fail-session", DeploymentID: "d", State: []byte("active")}

		mock.ExpectExec(`INSERT INTO sessions`).
			WithArgs(
				sess.SessionID,
				sess.DeploymentID,
				sess.ShellPID,
				pgxmock.AnyArg(), // createdAt
				pgxmock.AnyArg(), // updatedAt
				pgxmock.AnyArg(), // forkedFrom
				pgxmock.AnyArg(), // attachedTo
				pgxmock.AnyArg(), // attachedAt
				sess.State,
			).
			WillReturnError(errors.New("constraint violation"))

		err := store.UpsertSession(context.Background(), sess)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGXStore_DeleteSession(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`DELETE FROM sessions`).
			WithArgs("session-to-delete").
			WillReturnResult(pgxmock.NewResult("DELETE", 1))

		err := store.DeleteSession(context.Background(), "session-to-delete")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`DELETE FROM sessions`).
			WithArgs("session-id").
			WillReturnError(errors.New("delete failed"))

		err := store.DeleteSession(context.Background(), "session-id")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGXStore_ClearAttachments(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`UPDATE sessions`).
			WithArgs("deploy-1").
			WillReturnResult(pgxmock.NewResult("UPDATE", 3))

		err := store.ClearAttachments(context.Background(), "deploy-1")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`UPDATE sessions`).
			WithArgs("deploy-1").
			WillReturnError(errors.New("update failed"))

		err := store.ClearAttachments(context.Background(), "deploy-1")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGXStore_SetAttachment(t *testing.T) {
	t.Run("attach success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`UPDATE sessions SET attached_to=\$2`).
			WithArgs("session-1", "client-1").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := store.SetAttachment(context.Background(), "session-1", "client-1", true)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("detach success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`UPDATE sessions SET attached_to=NULL`).
			WithArgs("session-1", "client-1").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := store.SetAttachment(context.Background(), "session-1", "client-1", false)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("detach with empty client ID", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`UPDATE sessions SET attached_to=NULL`).
			WithArgs("session-1", "").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := store.SetAttachment(context.Background(), "session-1", "", false)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("attach error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`UPDATE sessions SET attached_to=\$2`).
			WithArgs("session-1", "client-1").
			WillReturnError(errors.New("update failed"))

		err := store.SetAttachment(context.Background(), "session-1", "client-1", true)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("detach error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`UPDATE sessions SET attached_to=NULL`).
			WithArgs("session-1", "client-1").
			WillReturnError(errors.New("update failed"))

		err := store.SetAttachment(context.Background(), "session-1", "client-1", false)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGXStore_AppendLog(t *testing.T) {
	t.Run("success with zero timestamp", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		entry := LogEntry{
			SessionID: "session-1",
			Direction: "output",
			Data:      []byte("hello world"),
		}

		mock.ExpectExec(`INSERT INTO session_logs`).
			WithArgs(entry.SessionID, entry.Direction, entry.Data, nil).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := store.AppendLog(context.Background(), entry)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("success with timestamp", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		now := time.Now()
		entry := LogEntry{
			SessionID: "session-1",
			Direction: "input",
			Data:      []byte("ls -la"),
			CreatedAt: now,
		}

		mock.ExpectExec(`INSERT INTO session_logs`).
			WithArgs(entry.SessionID, entry.Direction, entry.Data, now).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := store.AppendLog(context.Background(), entry)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		entry := LogEntry{SessionID: "s", Direction: "output", Data: []byte("x")}

		mock.ExpectExec(`INSERT INTO session_logs`).
			WithArgs(entry.SessionID, entry.Direction, entry.Data, nil).
			WillReturnError(errors.New("insert failed"))

		err := store.AppendLog(context.Background(), entry)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGXStore_ListLogs(t *testing.T) {
	columns := []string{"session_uuid", "direction", "payload", "created_at"}
	now := time.Now()

	t.Run("success without filter", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		// Expect count query
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_logs`).
			WithArgs("session-1").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(5))

		// Expect data query
		mock.ExpectQuery(`SELECT session_uuid, direction, payload, created_at`).
			WithArgs("session-1", 200).
			WillReturnRows(pgxmock.NewRows(columns).
				AddRow("session-1", "output", []byte("line1"), now).
				AddRow("session-1", "input", []byte("cmd"), now))

		result, err := store.ListLogs(context.Background(), "session-1", 0, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.MatchCount != 5 {
			t.Errorf("MatchCount = %d, want 5", result.MatchCount)
		}
		if len(result.Logs) != 2 {
			t.Errorf("len(Logs) = %d, want 2", len(result.Logs))
		}
	})

	t.Run("with direction filter", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		filter := &LogFilter{Direction: "output"}

		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_logs`).
			WithArgs("session-1", "output").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3))

		mock.ExpectQuery(`SELECT session_uuid, direction, payload, created_at`).
			WithArgs("session-1", "output", 100).
			WillReturnRows(pgxmock.NewRows(columns).
				AddRow("session-1", "output", []byte("data"), now))

		result, err := store.ListLogs(context.Background(), "session-1", 100, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.MatchCount != 3 {
			t.Errorf("MatchCount = %d, want 3", result.MatchCount)
		}
	})

	t.Run("with search filter", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		filter := &LogFilter{Search: "error"}

		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_logs`).
			WithArgs("session-1", "%error%").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(`SELECT session_uuid, direction, payload, created_at`).
			WithArgs("session-1", "%error%", 50).
			WillReturnRows(pgxmock.NewRows(columns).
				AddRow("session-1", "output", []byte("error occurred"), now))

		result, err := store.ListLogs(context.Background(), "session-1", 50, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.MatchCount != 1 {
			t.Errorf("MatchCount = %d, want 1", result.MatchCount)
		}
	})

	t.Run("count query error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_logs`).
			WithArgs("session-1").
			WillReturnError(errors.New("count failed"))

		_, err := store.ListLogs(context.Background(), "session-1", 0, nil)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("data query error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_logs`).
			WithArgs("session-1").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(5))

		mock.ExpectQuery(`SELECT session_uuid, direction, payload, created_at`).
			WithArgs("session-1", 200).
			WillReturnError(errors.New("query failed"))

		_, err := store.ListLogs(context.Background(), "session-1", 0, nil)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGXStore_PruneLogs(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		cutoff := time.Now().Add(-24 * time.Hour)
		mock.ExpectExec(`DELETE FROM session_logs WHERE created_at < \$1`).
			WithArgs(cutoff).
			WillReturnResult(pgxmock.NewResult("DELETE", 100))

		count, err := store.PruneLogs(context.Background(), cutoff)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 100 {
			t.Errorf("count = %d, want 100", count)
		}
	})

	t.Run("zero cutoff returns zero", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		count, err := store.PruneLogs(context.Background(), time.Time{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Errorf("count = %d, want 0", count)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		cutoff := time.Now()
		mock.ExpectExec(`DELETE FROM session_logs WHERE created_at < \$1`).
			WithArgs(cutoff).
			WillReturnError(errors.New("delete failed"))

		_, err := store.PruneLogs(context.Background(), cutoff)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGXStore_TrimLogs(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`WITH ranked AS`).
			WithArgs(1000).
			WillReturnResult(pgxmock.NewResult("DELETE", 50))

		count, err := store.TrimLogs(context.Background(), 1000)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 50 {
			t.Errorf("count = %d, want 50", count)
		}
	})

	t.Run("zero max entries returns zero", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		count, err := store.TrimLogs(context.Background(), 0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Errorf("count = %d, want 0", count)
		}
	})

	t.Run("negative max entries returns zero", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		count, err := store.TrimLogs(context.Background(), -5)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Errorf("count = %d, want 0", count)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`WITH ranked AS`).
			WithArgs(500).
			WillReturnError(errors.New("trim failed"))

		_, err := store.TrimLogs(context.Background(), 500)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}
