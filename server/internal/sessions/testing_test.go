package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNewMockStore verifies the mock store constructor.
func TestNewMockStore(t *testing.T) {
	store := NewMockStore()

	require.NotNil(t, store)
	require.NotNil(t, store.sessions)
	require.NotNil(t, store.logs)
}

// TestMockStore_SetError verifies error injection.
func TestMockStore_SetError(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()
	testErr := errors.New("test error")

	store.SetError(testErr)

	_, err := store.GetSession(ctx, "test")
	require.Equal(t, testErr, err)

	// Error should be cleared after use
	_, err = store.GetSession(ctx, "test")
	require.Equal(t, ErrNotFound, err)
}

// TestMockStore_GetSession verifies session retrieval.
func TestMockStore_GetSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		sessionID string
		setup     func(*MockStore)
		wantErr   error
	}{
		{
			name:      "found",
			sessionID: "sess-123",
			setup: func(store *MockStore) {
				store.AddSession(&Session{SessionID: "sess-123", DeploymentID: "dep-1"})
			},
			wantErr: nil,
		},
		{
			name:      "not found",
			sessionID: "nonexistent",
			setup:     func(store *MockStore) {},
			wantErr:   ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			sess, err := store.GetSession(ctx, tt.sessionID)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.sessionID, sess.SessionID)
			}
		})
	}
}

// TestMockStore_ListSessions verifies session listing.
func TestMockStore_ListSessions(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		deploymentID string
		setup        func(*MockStore)
		wantCount    int
	}{
		{
			name:         "multiple sessions",
			deploymentID: "dep-1",
			setup: func(store *MockStore) {
				store.AddSession(&Session{SessionID: "sess-1", DeploymentID: "dep-1"})
				store.AddSession(&Session{SessionID: "sess-2", DeploymentID: "dep-1"})
				store.AddSession(&Session{SessionID: "sess-3", DeploymentID: "dep-2"})
			},
			wantCount: 2,
		},
		{
			name:         "no sessions",
			deploymentID: "dep-99",
			setup:        func(store *MockStore) {},
			wantCount:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			sessions, err := store.ListSessions(ctx, tt.deploymentID)

			require.NoError(t, err)
			require.Len(t, sessions, tt.wantCount)
		})
	}
}

// TestMockStore_UpsertSession verifies session creation/update.
func TestMockStore_UpsertSession(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	sess := Session{
		SessionID:    "sess-123",
		DeploymentID: "dep-1",
		ShellPID:     1234,
	}

	// Create
	err := store.UpsertSession(ctx, sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(ctx, "sess-123")
	require.NoError(t, err)
	require.Equal(t, "dep-1", retrieved.DeploymentID)
	require.Equal(t, 1234, retrieved.ShellPID)

	// Update
	sess.ShellPID = 5678
	err = store.UpsertSession(ctx, sess)
	require.NoError(t, err)

	retrieved, err = store.GetSession(ctx, "sess-123")
	require.NoError(t, err)
	require.Equal(t, 5678, retrieved.ShellPID)
}

// TestMockStore_DeleteSession verifies session deletion.
func TestMockStore_DeleteSession(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		sessionID string
		setup     func(*MockStore)
		wantErr   error
	}{
		{
			name:      "successful delete",
			sessionID: "sess-123",
			setup: func(store *MockStore) {
				store.AddSession(&Session{SessionID: "sess-123"})
			},
			wantErr: nil,
		},
		{
			name:      "not found",
			sessionID: "nonexistent",
			setup:     func(store *MockStore) {},
			wantErr:   ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			err := store.DeleteSession(ctx, tt.sessionID)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				_, err := store.GetSession(ctx, tt.sessionID)
				require.ErrorIs(t, err, ErrNotFound)
			}
		})
	}
}

// TestMockStore_ClearAttachments verifies attachment clearing.
func TestMockStore_ClearAttachments(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	clientID := "client-1"
	now := time.Now()

	store.AddSession(&Session{
		SessionID:    "sess-1",
		DeploymentID: "dep-1",
		AttachedTo:   &clientID,
		AttachedAt:   &now,
	})
	store.AddSession(&Session{
		SessionID:    "sess-2",
		DeploymentID: "dep-1",
		AttachedTo:   &clientID,
		AttachedAt:   &now,
	})
	store.AddSession(&Session{
		SessionID:    "sess-3",
		DeploymentID: "dep-2",
		AttachedTo:   &clientID,
		AttachedAt:   &now,
	})

	err := store.ClearAttachments(ctx, "dep-1")
	require.NoError(t, err)

	// dep-1 sessions should be cleared
	sess1, _ := store.GetSession(ctx, "sess-1")
	require.Nil(t, sess1.AttachedTo)
	require.Nil(t, sess1.AttachedAt)

	sess2, _ := store.GetSession(ctx, "sess-2")
	require.Nil(t, sess2.AttachedTo)
	require.Nil(t, sess2.AttachedAt)

	// dep-2 session should be unchanged
	sess3, _ := store.GetSession(ctx, "sess-3")
	require.NotNil(t, sess3.AttachedTo)
}

// TestMockStore_SetAttachment verifies attachment setting.
func TestMockStore_SetAttachment(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		sessionID string
		clientID  string
		attached  bool
		setup     func(*MockStore)
		wantErr   error
	}{
		{
			name:      "attach",
			sessionID: "sess-123",
			clientID:  "client-1",
			attached:  true,
			setup: func(store *MockStore) {
				store.AddSession(&Session{SessionID: "sess-123"})
			},
			wantErr: nil,
		},
		{
			name:      "detach",
			sessionID: "sess-123",
			clientID:  "client-1",
			attached:  false,
			setup: func(store *MockStore) {
				clientID := "client-1"
				now := time.Now()
				store.AddSession(&Session{
					SessionID:  "sess-123",
					AttachedTo: &clientID,
					AttachedAt: &now,
				})
			},
			wantErr: nil,
		},
		{
			name:      "not found",
			sessionID: "nonexistent",
			clientID:  "client-1",
			attached:  true,
			setup:     func(store *MockStore) {},
			wantErr:   ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			err := store.SetAttachment(ctx, tt.sessionID, tt.clientID, tt.attached)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				sess, _ := store.GetSession(ctx, tt.sessionID)
				if tt.attached {
					require.NotNil(t, sess.AttachedTo)
					require.Equal(t, tt.clientID, *sess.AttachedTo)
					require.NotNil(t, sess.AttachedAt)
				} else {
					require.Nil(t, sess.AttachedTo)
					require.Nil(t, sess.AttachedAt)
				}
			}
		})
	}
}

// TestMockStore_AppendLog verifies log appending.
func TestMockStore_AppendLog(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()
	store.AddSession(&Session{SessionID: "sess-123"})

	entry := LogEntry{
		SessionID: "sess-123",
		Direction: "out",
		Data:      []byte("hello world"),
	}

	err := store.AppendLog(ctx, entry)
	require.NoError(t, err)

	result, err := store.ListLogs(ctx, "sess-123", 100, nil)
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Equal(t, "out", result.Logs[0].Direction)
	require.Equal(t, []byte("hello world"), result.Logs[0].Data)

	// Verify session log count updated
	sess, _ := store.GetSession(ctx, "sess-123")
	require.Equal(t, 1, sess.LogCount)
	require.NotNil(t, sess.LastLogAt)
}

// TestMockStore_ListLogs verifies log listing with filters.
func TestMockStore_ListLogs(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		sessionID string
		limit     int
		filter    *LogFilter
		setup     func(*MockStore)
		wantCount int
	}{
		{
			name:      "all logs",
			sessionID: "sess-123",
			limit:     100,
			filter:    nil,
			setup: func(store *MockStore) {
				store.logs["sess-123"] = []LogEntry{
					{SessionID: "sess-123", Direction: "in", Data: []byte("cmd")},
					{SessionID: "sess-123", Direction: "out", Data: []byte("output")},
				}
			},
			wantCount: 2,
		},
		{
			name:      "filter by direction",
			sessionID: "sess-123",
			limit:     100,
			filter:    &LogFilter{Direction: "out"},
			setup: func(store *MockStore) {
				store.logs["sess-123"] = []LogEntry{
					{SessionID: "sess-123", Direction: "in", Data: []byte("cmd")},
					{SessionID: "sess-123", Direction: "out", Data: []byte("output")},
				}
			},
			wantCount: 1,
		},
		{
			name:      "filter by search",
			sessionID: "sess-123",
			limit:     100,
			filter:    &LogFilter{Search: "error"},
			setup: func(store *MockStore) {
				store.logs["sess-123"] = []LogEntry{
					{SessionID: "sess-123", Data: []byte("success")},
					{SessionID: "sess-123", Data: []byte("error occurred")},
					{SessionID: "sess-123", Data: []byte("ERROR: failed")},
				}
			},
			wantCount: 2,
		},
		{
			name:      "with limit",
			sessionID: "sess-123",
			limit:     2,
			filter:    nil,
			setup: func(store *MockStore) {
				store.logs["sess-123"] = []LogEntry{
					{SessionID: "sess-123", Data: []byte("1")},
					{SessionID: "sess-123", Data: []byte("2")},
					{SessionID: "sess-123", Data: []byte("3")},
				}
			},
			wantCount: 2,
		},
		{
			name:      "no session logs",
			sessionID: "sess-999",
			limit:     100,
			filter:    nil,
			setup:     func(store *MockStore) {},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			result, err := store.ListLogs(ctx, tt.sessionID, tt.limit, tt.filter)

			require.NoError(t, err)
			require.Len(t, result.Logs, tt.wantCount)
		})
	}
}

// TestMockStore_PruneLogs verifies log pruning by time.
func TestMockStore_PruneLogs(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	now := time.Now()
	old := now.Add(-48 * time.Hour)

	store.logs["sess-123"] = []LogEntry{
		{SessionID: "sess-123", Data: []byte("old"), CreatedAt: old},
		{SessionID: "sess-123", Data: []byte("new"), CreatedAt: now},
	}
	store.logs["sess-456"] = []LogEntry{
		{SessionID: "sess-456", Data: []byte("old"), CreatedAt: old},
	}

	cutoff := now.Add(-24 * time.Hour)
	count, err := store.PruneLogs(ctx, cutoff)

	require.NoError(t, err)
	require.Equal(t, int64(2), count)

	result, _ := store.ListLogs(ctx, "sess-123", 100, nil)
	require.Len(t, result.Logs, 1)
	require.Equal(t, []byte("new"), result.Logs[0].Data)

	result, _ = store.ListLogs(ctx, "sess-456", 100, nil)
	require.Len(t, result.Logs, 0)
}

// TestMockStore_TrimLogs verifies log trimming by count.
func TestMockStore_TrimLogs(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	store.logs["sess-123"] = []LogEntry{
		{SessionID: "sess-123", Data: []byte("1")},
		{SessionID: "sess-123", Data: []byte("2")},
		{SessionID: "sess-123", Data: []byte("3")},
		{SessionID: "sess-123", Data: []byte("4")},
		{SessionID: "sess-123", Data: []byte("5")},
	}

	count, err := store.TrimLogs(ctx, 3)

	require.NoError(t, err)
	require.Equal(t, int64(2), count)

	result, _ := store.ListLogs(ctx, "sess-123", 100, nil)
	require.Len(t, result.Logs, 3)
	// Should keep the most recent entries
	require.Equal(t, []byte("3"), result.Logs[0].Data)
	require.Equal(t, []byte("4"), result.Logs[1].Data)
	require.Equal(t, []byte("5"), result.Logs[2].Data)
}

// TestMockStore_ConcurrentAccess verifies thread safety.
func TestMockStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()
	store.AddSession(&Session{SessionID: "sess-123", DeploymentID: "dep-1"})

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = store.GetSession(ctx, "sess-123")
			_, _ = store.ListSessions(ctx, "dep-1")
			_, _ = store.ListLogs(ctx, "sess-123", 100, nil)
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func() {
			_ = store.AppendLog(ctx, LogEntry{
				SessionID: "sess-123",
				Direction: "out",
				Data:      []byte("test"),
			})
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

// TestSession_Fields verifies session field types.
func TestSession_Fields(t *testing.T) {
	now := time.Now()
	forkedFrom := "parent-session"
	attachedTo := "client-1"
	state := []byte("state data")

	sess := Session{
		SessionID:    "sess-123",
		DeploymentID: "dep-1",
		ShellPID:     1234,
		CreatedAt:    now,
		UpdatedAt:    now,
		ForkedFrom:   &forkedFrom,
		AttachedTo:   &attachedTo,
		AttachedAt:   &now,
		State:        state,
		LastLogAt:    &now,
		LogCount:     10,
	}

	require.Equal(t, "sess-123", sess.SessionID)
	require.Equal(t, "dep-1", sess.DeploymentID)
	require.Equal(t, 1234, sess.ShellPID)
	require.NotNil(t, sess.ForkedFrom)
	require.NotNil(t, sess.AttachedTo)
	require.NotNil(t, sess.AttachedAt)
	require.NotNil(t, sess.State)
	require.NotNil(t, sess.LastLogAt)
	require.Equal(t, 10, sess.LogCount)
}

// TestLogEntry_Fields verifies log entry field types.
func TestLogEntry_Fields(t *testing.T) {
	now := time.Now()
	entry := LogEntry{
		SessionID: "sess-123",
		Direction: "out",
		Data:      []byte("output data"),
		CreatedAt: now,
	}

	require.Equal(t, "sess-123", entry.SessionID)
	require.Equal(t, "out", entry.Direction)
	require.Equal(t, []byte("output data"), entry.Data)
	require.Equal(t, now, entry.CreatedAt)
}

// TestLogFilter_Fields verifies log filter field types.
func TestLogFilter_Fields(t *testing.T) {
	filter := LogFilter{
		Search:    "error",
		Direction: "out",
	}

	require.Equal(t, "error", filter.Search)
	require.Equal(t, "out", filter.Direction)
}

// TestLogsResult_Fields verifies logs result field types.
func TestLogsResult_Fields(t *testing.T) {
	result := LogsResult{
		Logs: []LogEntry{
			{SessionID: "sess-1"},
			{SessionID: "sess-2"},
		},
		MatchCount: 5,
	}

	require.Len(t, result.Logs, 2)
	require.Equal(t, 5, result.MatchCount)
}

// TestErrNotFound verifies the error constant.
func TestErrNotFound(t *testing.T) {
	require.Equal(t, "session not found", ErrNotFound.Error())
}
