package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/sessions"
)

// mockStore implements sessions.Store for testing
type mockStore struct {
	listLogsFunc func(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error)
}

func (m *mockStore) GetSession(ctx context.Context, sessionID string) (*sessions.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *mockStore) ListSessions(ctx context.Context, deploymentID string) ([]sessions.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *mockStore) UpsertSession(ctx context.Context, s sessions.Session) error {
	return errors.New("not implemented")
}

func (m *mockStore) DeleteSession(ctx context.Context, sessionID string) error {
	return errors.New("not implemented")
}

func (m *mockStore) ClearAttachments(ctx context.Context, deploymentID string) error {
	return errors.New("not implemented")
}

func (m *mockStore) SetAttachment(ctx context.Context, sessionID, clientID string, attached bool) error {
	return errors.New("not implemented")
}

func (m *mockStore) AppendLog(ctx context.Context, entry sessions.LogEntry) error {
	return errors.New("not implemented")
}

func (m *mockStore) ListLogs(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error) {
	if m.listLogsFunc != nil {
		return m.listLogsFunc(ctx, sessionID, limit)
	}
	return nil, errors.New("not implemented")
}

func (m *mockStore) PruneLogs(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, errors.New("not implemented")
}

func (m *mockStore) TrimLogs(ctx context.Context, maxEntries int) (int64, error) {
	return 0, errors.New("not implemented")
}

// mockObserver implements StoreMetricsObserver for testing
type mockObserver struct {
	observedOperations []string
	observedErrors     []error
}

func (m *mockObserver) ObserveStore(operation string, start time.Time, err error) {
	m.observedOperations = append(m.observedOperations, operation)
	m.observedErrors = append(m.observedErrors, err)
}

func TestNewSessionLogsHandler_Success(t *testing.T) {
	logs := []sessions.LogEntry{
		{SessionID: "test-session-123", Direction: "output", Data: []byte("Test log 1"), CreatedAt: time.Now()},
		{SessionID: "test-session-123", Direction: "output", Data: []byte("Test log 2"), CreatedAt: time.Now()},
	}

	store := &mockStore{
		listLogsFunc: func(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error) {
			if sessionID != "test-session-123" {
				t.Errorf("Expected sessionID 'test-session-123', got %q", sessionID)
			}
			if limit != 200 {
				t.Errorf("Expected limit 200, got %d", limit)
			}
			return logs, nil
		},
	}

	observer := &mockObserver{}
	handler := NewSessionLogsHandler(store, observer)

	req := httptest.NewRequest("GET", "/api/session-logs?session=test-session-123", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %q", ct)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["sessionId"] != "test-session-123" {
		t.Errorf("Expected sessionId 'test-session-123', got %v", response["sessionId"])
	}

	// Verify observer was called
	if len(observer.observedOperations) != 1 {
		t.Errorf("Expected 1 observed operation, got %d", len(observer.observedOperations))
	}
	if len(observer.observedOperations) > 0 && observer.observedOperations[0] != "ListLogs" {
		t.Errorf("Expected operation 'ListLogs', got %q", observer.observedOperations[0])
	}
}

func TestNewSessionLogsHandler_MissingSessionParameter(t *testing.T) {
	store := &mockStore{}
	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest("GET", "/api/session-logs", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %q", ct)
	}

	var errResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errResp["error"] != "bad_request" {
		t.Errorf("Expected error code 'bad_request', got %v", errResp["error"])
	}
	if errResp["message"] != "missing session parameter" {
		t.Errorf("Expected message 'missing session parameter', got %v", errResp["message"])
	}
}

func TestNewSessionLogsHandler_CustomLimit(t *testing.T) {
	tests := []struct {
		name          string
		limitParam    string
		expectedLimit int
	}{
		{
			name:          "default limit",
			limitParam:    "",
			expectedLimit: 200,
		},
		{
			name:          "custom valid limit",
			limitParam:    "500",
			expectedLimit: 500,
		},
		{
			name:          "limit exceeds maximum",
			limitParam:    "3000",
			expectedLimit: 2000,
		},
		{
			name:          "zero limit",
			limitParam:    "0",
			expectedLimit: 200, // Default
		},
		{
			name:          "negative limit",
			limitParam:    "-100",
			expectedLimit: 200, // Default
		},
		{
			name:          "invalid limit",
			limitParam:    "abc",
			expectedLimit: 200, // Default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualLimit := 0
			store := &mockStore{
				listLogsFunc: func(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error) {
					actualLimit = limit
					return []sessions.LogEntry{}, nil
				},
			}

			handler := NewSessionLogsHandler(store, nil)

			url := "/api/session-logs?session=test"
			if tt.limitParam != "" {
				url += "&limit=" + tt.limitParam
			}

			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if actualLimit != tt.expectedLimit {
				t.Errorf("Expected limit %d, got %d", tt.expectedLimit, actualLimit)
			}
		})
	}
}

func TestNewSessionLogsHandler_StoreError(t *testing.T) {
	store := &mockStore{
		listLogsFunc: func(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error) {
			return nil, errors.New("database connection failed")
		},
	}

	observer := &mockObserver{}
	handler := NewSessionLogsHandler(store, observer)

	req := httptest.NewRequest("GET", "/api/session-logs?session=test-session", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %q", ct)
	}

	var errResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errResp["error"] != "internal_server_error" {
		t.Errorf("Expected error code 'internal_server_error', got %v", errResp["error"])
	}
	if errResp["message"] != "failed to retrieve session logs" {
		t.Errorf("Expected message 'failed to retrieve session logs', got %v", errResp["message"])
	}
	// Security fix: details should be empty to prevent information disclosure
	if details := errResp["details"]; details != "" && details != nil {
		t.Errorf("Expected empty details for security, got %v", details)
	}

	// Verify observer recorded the error
	if len(observer.observedErrors) != 1 {
		t.Errorf("Expected 1 observed error, got %d", len(observer.observedErrors))
	}
	if len(observer.observedErrors) > 0 && observer.observedErrors[0] == nil {
		t.Error("Expected non-nil error to be observed")
	}
}

func TestNewSessionLogsHandler_NilLogs(t *testing.T) {
	store := &mockStore{
		listLogsFunc: func(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error) {
			return nil, nil // Explicitly return nil to test handling
		},
	}

	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest("GET", "/api/session-logs?session=test-session", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	logs, ok := response["logs"].([]any)
	if !ok {
		t.Fatalf("Expected logs to be an array, got %T", response["logs"])
	}

	if len(logs) != 0 {
		t.Errorf("Expected empty logs array, got %d items", len(logs))
	}
}

func TestNewSessionLogsHandler_NilObserver(t *testing.T) {
	store := &mockStore{
		listLogsFunc: func(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error) {
			return []sessions.LogEntry{}, nil
		},
	}

	// Pass nil observer - should not panic
	handler := NewSessionLogsHandler(store, nil)

	req := httptest.NewRequest("GET", "/api/session-logs?session=test-session", nil)
	w := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestNewSessionLogsHandler_LargeLimitBoundary(t *testing.T) {
	tests := []struct {
		limitParam    string
		expectedLimit int
	}{
		{"1999", 1999},
		{"2000", 2000},
		{"2001", 2000},
		{"10000", 2000},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("limit=%s", tt.limitParam), func(t *testing.T) {
			actualLimit := 0
			store := &mockStore{
				listLogsFunc: func(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error) {
					actualLimit = limit
					return []sessions.LogEntry{}, nil
				},
			}

			handler := NewSessionLogsHandler(store, nil)

			req := httptest.NewRequest("GET", fmt.Sprintf("/api/session-logs?session=test&limit=%s", tt.limitParam), nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if actualLimit != tt.expectedLimit {
				t.Errorf("Expected limit %d, got %d", tt.expectedLimit, actualLimit)
			}
		})
	}
}
