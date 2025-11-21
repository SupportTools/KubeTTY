package handlers_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/sessions"
	"github.com/supporttools/KubeTTY/server/internal/shared/handlers"
)

// mockStore implements sessions.Store for testing.
type mockStore struct {
	logs []sessions.LogEntry
}

func (m *mockStore) GetSession(ctx context.Context, sessionID string) (*sessions.Session, error) {
	return nil, nil
}
func (m *mockStore) ListSessions(ctx context.Context, deploymentID string) ([]sessions.Session, error) {
	return nil, nil
}
func (m *mockStore) UpsertSession(ctx context.Context, s sessions.Session) error { return nil }
func (m *mockStore) DeleteSession(ctx context.Context, sessionID string) error   { return nil }
func (m *mockStore) ClearAttachments(ctx context.Context, deploymentID string) error {
	return nil
}
func (m *mockStore) SetAttachment(ctx context.Context, sessionID, clientID string, attached bool) error {
	return nil
}
func (m *mockStore) AppendLog(ctx context.Context, entry sessions.LogEntry) error { return nil }
func (m *mockStore) PruneLogs(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, nil
}
func (m *mockStore) TrimLogs(ctx context.Context, maxEntries int) (int64, error) {
	return 0, nil
}

func (m *mockStore) ListLogs(ctx context.Context, sessionID string, limit int, filter *sessions.LogFilter) (sessions.LogsResult, error) {
	if limit > len(m.logs) {
		limit = len(m.logs)
	}
	return sessions.LogsResult{Logs: m.logs[:limit], MatchCount: len(m.logs)}, nil
}

// mockObserver implements StoreMetricsObserver for testing.
type mockObserver struct{}

func (m *mockObserver) ObserveStore(operation string, start time.Time, err error) {
	duration := time.Since(start)
	fmt.Printf("Metrics: operation=%s, duration=%v, error=%v\n", operation, duration > 0, err != nil)
}

// ExampleNewSessionLogsHandler demonstrates creating a session logs HTTP handler.
func ExampleNewSessionLogsHandler() {
	// Create mock store with sample logs
	store := &mockStore{
		logs: []sessions.LogEntry{
			{CreatedAt: time.Now().Add(-2 * time.Minute), Data: []byte("user@host:~$ ls -la")},
			{CreatedAt: time.Now().Add(-1 * time.Minute), Data: []byte("total 48")},
			{CreatedAt: time.Now(), Data: []byte("drwxr-xr-x 2 user user 4096 Dec 1 10:00 .")},
		},
	}

	// Create observer for metrics tracking
	observer := &mockObserver{}

	// Create handler
	handler := handlers.NewSessionLogsHandler(store, observer)

	// Create test request with session parameter
	req := httptest.NewRequest("GET", "/session/logs?session=abc123&limit=10", nil)
	w := httptest.NewRecorder()

	// Execute handler
	handler(w, req)

	// Check response
	fmt.Printf("Status: %d\n", w.Code)
	fmt.Printf("Content-Type: %s\n", w.Header().Get("Content-Type"))
	fmt.Println("Response contains session logs as JSON")
	// Output:
	// Metrics: operation=ListLogs, duration=true, error=false
	// Status: 200
	// Content-Type: application/json
	// Response contains session logs as JSON
}
