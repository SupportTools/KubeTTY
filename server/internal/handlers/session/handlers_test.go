package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/sessions"
)

// mockSessionStore implements sessions.Store for testing
type mockSessionStore struct {
	logs        []sessions.LogEntry
	sessions    map[string]*sessions.Session
	listLogsErr error
	matchCount  int
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{
		logs:     make([]sessions.LogEntry, 0),
		sessions: make(map[string]*sessions.Session),
	}
}

func (m *mockSessionStore) GetSession(ctx context.Context, sessionID string) (*sessions.Session, error) {
	if s, ok := m.sessions[sessionID]; ok {
		return s, nil
	}
	return nil, sessions.ErrNotFound
}

func (m *mockSessionStore) ListSessions(ctx context.Context, deploymentID string) ([]sessions.Session, error) {
	var result []sessions.Session
	for _, s := range m.sessions {
		if s.DeploymentID == deploymentID {
			result = append(result, *s)
		}
	}
	return result, nil
}

func (m *mockSessionStore) UpsertSession(ctx context.Context, s sessions.Session) error {
	m.sessions[s.SessionID] = &s
	return nil
}

func (m *mockSessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	delete(m.sessions, sessionID)
	return nil
}

func (m *mockSessionStore) ClearAttachments(ctx context.Context, deploymentID string) error {
	return nil
}

func (m *mockSessionStore) SetAttachment(ctx context.Context, sessionID, clientID string, attached bool) error {
	return nil
}

func (m *mockSessionStore) AppendLog(ctx context.Context, entry sessions.LogEntry) error {
	m.logs = append(m.logs, entry)
	return nil
}

func (m *mockSessionStore) ListLogs(ctx context.Context, sessionID string, limit int, filter *sessions.LogFilter) (sessions.LogsResult, error) {
	if m.listLogsErr != nil {
		return sessions.LogsResult{}, m.listLogsErr
	}

	var filtered []sessions.LogEntry
	for _, log := range m.logs {
		if log.SessionID != sessionID {
			continue
		}

		// Apply direction filter
		if filter != nil && filter.Direction != "" && log.Direction != filter.Direction {
			continue
		}

		// Apply search filter
		if filter != nil && filter.Search != "" {
			if !strings.Contains(strings.ToLower(string(log.Data)), strings.ToLower(filter.Search)) {
				continue
			}
		}

		filtered = append(filtered, log)
	}

	matchCount := len(filtered)
	if m.matchCount > 0 {
		matchCount = m.matchCount
	}

	// Apply limit
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return sessions.LogsResult{
		Logs:       filtered,
		MatchCount: matchCount,
	}, nil
}

func (m *mockSessionStore) PruneLogs(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, nil
}

func (m *mockSessionStore) TrimLogs(ctx context.Context, maxEntries int) (int64, error) {
	return 0, nil
}

// AddLog is a test helper to add logs to the mock store
func (m *mockSessionStore) AddLog(sessionID, direction string, data []byte) {
	m.logs = append(m.logs, sessions.LogEntry{
		SessionID: sessionID,
		Direction: direction,
		Data:      data,
		CreatedAt: time.Now(),
	})
}

// mockObserver implements StoreMetricsObserver for testing
type mockObserver struct {
	operations []string
	lastErr    error
}

func (m *mockObserver) ObserveStore(operation string, start time.Time, err error) {
	m.operations = append(m.operations, operation)
	m.lastErr = err
}

// =============================================================================
// NewSessionLogsHandler Tests
// =============================================================================

func TestNewSessionLogsHandler_Success(t *testing.T) {
	store := newMockSessionStore()
	store.AddLog("test-session", "out", []byte("hello world"))
	store.AddLog("test-session", "in", []byte("ls -la"))

	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.SessionID != "test-session" {
		t.Errorf("expected sessionId 'test-session', got %q", resp.SessionID)
	}

	if len(resp.Logs) != 2 {
		t.Errorf("expected 2 logs, got %d", len(resp.Logs))
	}

	if resp.MatchCount != 2 {
		t.Errorf("expected matchCount 2, got %d", resp.MatchCount)
	}
}

func TestNewSessionLogsHandler_MissingSession(t *testing.T) {
	store := newMockSessionStore()
	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/session/logs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "missing session") {
		t.Errorf("expected error about missing session, got %q", body)
	}
}

func TestNewSessionLogsHandler_SessionIDTooLong(t *testing.T) {
	store := newMockSessionStore()
	handler := NewSessionLogsHandler(store, nil)

	longSessionID := strings.Repeat("a", 65)
	req := httptest.NewRequest(http.MethodGet, "/session/logs?session="+longSessionID, nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "invalid session") || !strings.Contains(body, "too long") {
		t.Errorf("expected error about session ID too long, got %q", body)
	}
}

func TestNewSessionLogsHandler_LimitParameter(t *testing.T) {
	store := newMockSessionStore()
	// Add more logs than the limit
	for i := 0; i < 10; i++ {
		store.AddLog("test-session", "out", []byte("log entry"))
	}
	store.matchCount = 10 // Override to simulate total match count

	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session&limit=5", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.Logs) > 5 {
		t.Errorf("expected at most 5 logs, got %d", len(resp.Logs))
	}
}

func TestNewSessionLogsHandler_LimitDefaults(t *testing.T) {
	tests := []struct {
		name          string
		limitParam    string
		expectedLimit int // Not directly testable, but we can infer from behavior
	}{
		{
			name:          "no limit",
			limitParam:    "",
			expectedLimit: 200, // default
		},
		{
			name:          "zero limit",
			limitParam:    "0",
			expectedLimit: 200, // keeps default
		},
		{
			name:          "negative limit",
			limitParam:    "-1",
			expectedLimit: 200, // keeps default
		},
		{
			name:          "over max limit",
			limitParam:    "5000",
			expectedLimit: 2000, // capped
		},
		{
			name:          "invalid limit",
			limitParam:    "abc",
			expectedLimit: 200, // keeps default on parse error
		},
		{
			name:          "valid limit",
			limitParam:    "100",
			expectedLimit: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockSessionStore()
			handler := NewSessionLogsHandler(store, nil)

			url := "/session/logs?session=test-session"
			if tt.limitParam != "" {
				url += "&limit=" + tt.limitParam
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			handler(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
			}
		})
	}
}

func TestNewSessionLogsHandler_DirectionFilter(t *testing.T) {
	store := newMockSessionStore()
	store.AddLog("test-session", "out", []byte("output data"))
	store.AddLog("test-session", "in", []byte("input data"))
	store.AddLog("test-session", "out", []byte("more output"))

	handler := NewSessionLogsHandler(store, nil)

	// Test filtering by "in" direction
	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session&direction=in", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	for _, log := range resp.Logs {
		if log.Direction != "in" {
			t.Errorf("expected all logs to have direction 'in', got %q", log.Direction)
		}
	}
}

func TestNewSessionLogsHandler_InvalidDirection(t *testing.T) {
	store := newMockSessionStore()
	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session&direction=invalid", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "direction") {
		t.Errorf("expected error about direction, got %q", body)
	}
}

func TestNewSessionLogsHandler_SearchFilter(t *testing.T) {
	store := newMockSessionStore()
	store.AddLog("test-session", "out", []byte("hello world"))
	store.AddLog("test-session", "out", []byte("goodbye world"))
	store.AddLog("test-session", "out", []byte("test data"))

	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session&search=hello", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.Logs) != 1 {
		t.Errorf("expected 1 log matching 'hello', got %d", len(resp.Logs))
	}
}

func TestNewSessionLogsHandler_SearchTooLong(t *testing.T) {
	store := newMockSessionStore()
	handler := NewSessionLogsHandler(store, nil)

	longSearch := strings.Repeat("a", 501)
	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session&search="+longSearch, nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "search") || !strings.Contains(body, "too long") {
		t.Errorf("expected error about search too long, got %q", body)
	}
}

func TestNewSessionLogsHandler_CombinedFilters(t *testing.T) {
	store := newMockSessionStore()
	store.AddLog("test-session", "out", []byte("hello world"))
	store.AddLog("test-session", "in", []byte("hello there"))
	store.AddLog("test-session", "out", []byte("goodbye"))

	handler := NewSessionLogsHandler(store, nil)

	// Search for "hello" with direction "out"
	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session&search=hello&direction=out", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.Logs) != 1 {
		t.Errorf("expected 1 log matching 'hello' with direction 'out', got %d", len(resp.Logs))
	}
}

func TestNewSessionLogsHandler_StoreError(t *testing.T) {
	store := newMockSessionStore()
	store.listLogsErr = errors.New("database error")

	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestNewSessionLogsHandler_EmptyLogs(t *testing.T) {
	store := newMockSessionStore()
	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Should return empty array, not null
	if resp.Logs == nil {
		t.Error("expected non-nil empty array for logs")
	}

	if len(resp.Logs) != 0 {
		t.Errorf("expected 0 logs, got %d", len(resp.Logs))
	}
}

func TestNewSessionLogsHandler_WithObserver(t *testing.T) {
	store := newMockSessionStore()
	observer := &mockObserver{}

	handler := NewSessionLogsHandler(store, observer)

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Check that observer was called
	if len(observer.operations) != 1 {
		t.Errorf("expected 1 operation recorded, got %d", len(observer.operations))
	}

	if observer.operations[0] != "ListLogs" {
		t.Errorf("expected operation 'ListLogs', got %q", observer.operations[0])
	}

	if observer.lastErr != nil {
		t.Errorf("expected no error, got %v", observer.lastErr)
	}
}

func TestNewSessionLogsHandler_ObserverWithError(t *testing.T) {
	store := newMockSessionStore()
	store.listLogsErr = errors.New("database error")
	observer := &mockObserver{}

	handler := NewSessionLogsHandler(store, observer)

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	// Observer should record the error
	if observer.lastErr == nil {
		t.Error("expected observer to record the error")
	}
}

func TestNewSessionLogsHandler_ContentType(t *testing.T) {
	store := newMockSessionStore()
	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=test-session", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}
}

// =============================================================================
// WebSocket Types Tests
// =============================================================================

func TestResizeMessage_Constants(t *testing.T) {
	// Verify PTY limits are reasonable
	if MaxPTYCols != 500 {
		t.Errorf("expected MaxPTYCols=500, got %d", MaxPTYCols)
	}
	if MaxPTYRows != 200 {
		t.Errorf("expected MaxPTYRows=200, got %d", MaxPTYRows)
	}
}

func TestResizeMessage_JSON(t *testing.T) {
	msg := ResizeMessage{
		Type: "resize",
		Cols: 80,
		Rows: 24,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ResizeMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != "resize" {
		t.Errorf("expected type 'resize', got %q", decoded.Type)
	}
	if decoded.Cols != 80 {
		t.Errorf("expected cols 80, got %d", decoded.Cols)
	}
	if decoded.Rows != 24 {
		t.Errorf("expected rows 24, got %d", decoded.Rows)
	}
}

func TestPingPongMessage_JSON(t *testing.T) {
	ping := PingMessage{Type: "ping"}
	pong := PongResponse{Type: "pong"}

	pingData, err := json.Marshal(ping)
	if err != nil {
		t.Fatalf("failed to marshal ping: %v", err)
	}

	pongData, err := json.Marshal(pong)
	if err != nil {
		t.Fatalf("failed to marshal pong: %v", err)
	}

	var decodedPing PingMessage
	if err := json.Unmarshal(pingData, &decodedPing); err != nil {
		t.Fatalf("failed to unmarshal ping: %v", err)
	}

	var decodedPong PongResponse
	if err := json.Unmarshal(pongData, &decodedPong); err != nil {
		t.Fatalf("failed to unmarshal pong: %v", err)
	}

	if decodedPing.Type != "ping" {
		t.Errorf("expected type 'ping', got %q", decodedPing.Type)
	}
	if decodedPong.Type != "pong" {
		t.Errorf("expected type 'pong', got %q", decodedPong.Type)
	}
}

// =============================================================================
// Table-Driven Tests
// =============================================================================

func TestSessionLogsHandler_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		setupStore     func(*mockSessionStore)
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "missing session",
			queryParams:    "",
			setupStore:     nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing session",
		},
		{
			name:           "session too long",
			queryParams:    "session=" + strings.Repeat("x", 65),
			setupStore:     nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "too long",
		},
		{
			name:           "invalid direction",
			queryParams:    "session=test&direction=foo",
			setupStore:     nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "direction",
		},
		{
			name:           "search too long",
			queryParams:    "session=test&search=" + strings.Repeat("x", 501),
			setupStore:     nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "search",
		},
		{
			name:           "valid in direction",
			queryParams:    "session=test&direction=in",
			setupStore:     nil,
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
		{
			name:           "valid out direction",
			queryParams:    "session=test&direction=out",
			setupStore:     nil,
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
		{
			name:        "database error",
			queryParams: "session=test",
			setupStore: func(s *mockSessionStore) {
				s.listLogsErr = errors.New("db error")
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockSessionStore()
			if tt.setupStore != nil {
				tt.setupStore(store)
			}

			handler := NewSessionLogsHandler(store, nil)

			url := "/session/logs"
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			handler(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedError != "" {
				body := w.Body.String()
				if !strings.Contains(body, tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, body)
				}
			}
		})
	}
}
