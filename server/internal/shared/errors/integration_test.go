package errors_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	handlers_session "github.com/supporttools/KubeTTY/server/internal/handlers/session"
	"github.com/supporttools/KubeTTY/server/internal/sessions"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
)

// TestAllErrorCodes verifies that all standard HTTP error codes are properly formatted as JSON
func TestAllErrorCodes(t *testing.T) {
	tests := []struct {
		name         string
		errorFunc    func() apierrors.ErrorResponse
		expectedCode int
		errorType    string
	}{
		{
			name:         "400 Bad Request",
			errorFunc:    func() apierrors.ErrorResponse { return apierrors.BadRequest("test error", "details") },
			expectedCode: 400,
			errorType:    "bad_request",
		},
		{
			name:         "401 Unauthorized",
			errorFunc:    func() apierrors.ErrorResponse { return apierrors.Unauthorized("test error", "details") },
			expectedCode: 401,
			errorType:    "unauthorized",
		},
		{
			name:         "403 Forbidden",
			errorFunc:    func() apierrors.ErrorResponse { return apierrors.Forbidden("test error", "details") },
			expectedCode: 403,
			errorType:    "forbidden",
		},
		{
			name:         "404 Not Found",
			errorFunc:    func() apierrors.ErrorResponse { return apierrors.NotFound("test error", "details") },
			expectedCode: 404,
			errorType:    "not_found",
		},
		{
			name:         "409 Conflict",
			errorFunc:    func() apierrors.ErrorResponse { return apierrors.Conflict("test error", "details") },
			expectedCode: 409,
			errorType:    "conflict",
		},
		{
			name:         "422 Validation Error",
			errorFunc:    func() apierrors.ErrorResponse { return apierrors.ValidationError("test error", "details") },
			expectedCode: 422,
			errorType:    "validation_error",
		},
		{
			name:         "500 Internal Server Error",
			errorFunc:    func() apierrors.ErrorResponse { return apierrors.InternalServerError("test error", "details") },
			expectedCode: 500,
			errorType:    "internal_server_error",
		},
		{
			name:         "503 Service Unavailable",
			errorFunc:    func() apierrors.ErrorResponse { return apierrors.ServiceUnavailable("test error", "details") },
			expectedCode: 503,
			errorType:    "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errResp := tt.errorFunc()

			// Verify status code
			if errResp.Status != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, errResp.Status)
			}

			// Verify error type
			if errResp.Error != tt.errorType {
				t.Errorf("Expected error type %q, got %q", tt.errorType, errResp.Error)
			}

			// Verify message is set
			if errResp.Message == "" {
				t.Error("Expected non-empty message")
			}

			// Verify details is set
			if errResp.Details == "" {
				t.Error("Expected non-empty details")
			}

			// Test WriteError creates valid JSON
			w := httptest.NewRecorder()
			if err := apierrors.WriteError(w, errResp); err != nil {
				t.Fatalf("WriteError failed: %v", err)
			}

			// Verify Content-Type
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", ct)
			}

			// Verify JSON structure
			var parsed apierrors.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			if parsed.Status != tt.expectedCode {
				t.Errorf("Parsed status %d does not match expected %d", parsed.Status, tt.expectedCode)
			}
		})
	}
}

// TestSessionLogsErrorResponses tests that the session logs handler returns proper JSON errors
func TestSessionLogsErrorResponses(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		storeError     error
		expectedStatus int
		expectedError  string
		expectedMsg    string
	}{
		{
			name:           "Missing session parameter",
			queryParams:    "",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "bad_request",
			expectedMsg:    "missing session parameter",
		},
		{
			name:           "Database error",
			queryParams:    "?session=test-session",
			storeError:     errors.New("database connection failed"),
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal_server_error",
			expectedMsg:    "failed to retrieve session logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockSessionStore{listLogsErr: tt.storeError}
			handler := handlers_session.NewSessionLogsHandler(store, nil)

			req := httptest.NewRequest("GET", "/session/logs"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			// Verify status code
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			// Verify Content-Type is application/json
			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
			}

			// Parse JSON response
			var errResp apierrors.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("Failed to unmarshal JSON response: %v\nBody: %s", err, w.Body.String())
			}

			// Verify error response structure
			if errResp.Status != tt.expectedStatus {
				t.Errorf("Expected response status %d, got %d", tt.expectedStatus, errResp.Status)
			}

			if errResp.Error != tt.expectedError {
				t.Errorf("Expected error code %q, got %q", tt.expectedError, errResp.Error)
			}

			if errResp.Message != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, errResp.Message)
			}

			// Verify response has all required fields
			if errResp.Status == 0 {
				t.Error("Response missing 'status' field")
			}
			if errResp.Error == "" {
				t.Error("Response missing 'error' field")
			}
			if errResp.Message == "" {
				t.Error("Response missing 'message' field")
			}
		})
	}
}

// TestJSONErrorStructure verifies the JSON structure of error responses
func TestJSONErrorStructure(t *testing.T) {
	tests := []struct {
		name    string
		errResp apierrors.ErrorResponse
	}{
		{
			name:    "Error with details",
			errResp: apierrors.BadRequest("invalid request", "missing field: username"),
		},
		{
			name:    "Error without details",
			errResp: apierrors.Unauthorized("unauthorized", ""),
		},
		{
			name:    "Internal server error",
			errResp: apierrors.InternalServerError("database error", "connection timeout"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := apierrors.WriteError(w, tt.errResp)
			if err != nil {
				t.Fatalf("WriteError failed: %v", err)
			}

			// Verify Content-Type header
			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", ct)
			}

			// Parse as generic map to verify structure
			var jsonMap map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &jsonMap); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			// Verify required fields exist
			if _, ok := jsonMap["status"]; !ok {
				t.Error("Missing 'status' field in JSON response")
			}
			if _, ok := jsonMap["error"]; !ok {
				t.Error("Missing 'error' field in JSON response")
			}
			if _, ok := jsonMap["message"]; !ok {
				t.Error("Missing 'message' field in JSON response")
			}

			// Verify optional details field behavior
			if tt.errResp.Details != "" {
				if _, ok := jsonMap["details"]; !ok {
					t.Error("Expected 'details' field in JSON response when details are provided")
				}
			}

			// Verify field types
			if status, ok := jsonMap["status"].(float64); !ok {
				t.Errorf("Expected 'status' to be a number, got %T", jsonMap["status"])
			} else if int(status) != tt.errResp.Status {
				t.Errorf("Expected status %d, got %d", tt.errResp.Status, int(status))
			}

			if _, ok := jsonMap["error"].(string); !ok {
				t.Errorf("Expected 'error' to be a string, got %T", jsonMap["error"])
			}

			if _, ok := jsonMap["message"].(string); !ok {
				t.Errorf("Expected 'message' to be a string, got %T", jsonMap["message"])
			}
		})
	}
}

// TestHTTPErrorStatusCodes verifies that HTTP status codes match the error responses
func TestHTTPErrorStatusCodes(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.HandlerFunc
		expectedStatus int
	}{
		{
			name: "Bad Request returns 400",
			handler: func(w http.ResponseWriter, r *http.Request) {
				apierrors.WriteError(w, apierrors.BadRequest("test", ""))
			},
			expectedStatus: 400,
		},
		{
			name: "Unauthorized returns 401",
			handler: func(w http.ResponseWriter, r *http.Request) {
				apierrors.WriteError(w, apierrors.Unauthorized("test", ""))
			},
			expectedStatus: 401,
		},
		{
			name: "Forbidden returns 403",
			handler: func(w http.ResponseWriter, r *http.Request) {
				apierrors.WriteError(w, apierrors.Forbidden("test", ""))
			},
			expectedStatus: 403,
		},
		{
			name: "Not Found returns 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				apierrors.WriteError(w, apierrors.NotFound("test", ""))
			},
			expectedStatus: 404,
		},
		{
			name: "Conflict returns 409",
			handler: func(w http.ResponseWriter, r *http.Request) {
				apierrors.WriteError(w, apierrors.Conflict("test", ""))
			},
			expectedStatus: 409,
		},
		{
			name: "Validation Error returns 422",
			handler: func(w http.ResponseWriter, r *http.Request) {
				apierrors.WriteError(w, apierrors.ValidationError("test", ""))
			},
			expectedStatus: 422,
		},
		{
			name: "Internal Server Error returns 500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				apierrors.WriteError(w, apierrors.InternalServerError("test", ""))
			},
			expectedStatus: 500,
		},
		{
			name: "Service Unavailable returns 503",
			handler: func(w http.ResponseWriter, r *http.Request) {
				apierrors.WriteError(w, apierrors.ServiceUnavailable("test", ""))
			},
			expectedStatus: 503,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			tt.handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			// Verify it's valid JSON
			var errResp apierrors.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			// Verify status in body matches HTTP status
			if errResp.Status != tt.expectedStatus {
				t.Errorf("Expected status in body %d, got %d", tt.expectedStatus, errResp.Status)
			}
		})
	}
}

// TestErrorResponseFieldValidation tests that error responses contain valid data
func TestErrorResponseFieldValidation(t *testing.T) {
	tests := []struct {
		name        string
		errResp     apierrors.ErrorResponse
		expectError bool
	}{
		{
			name:        "Valid error response",
			errResp:     apierrors.BadRequest("test message", "test details"),
			expectError: false,
		},
		{
			name:        "Error response without details",
			errResp:     apierrors.Unauthorized("unauthorized", ""),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify required fields are non-zero
			if tt.errResp.Status == 0 {
				t.Error("Status should not be zero")
			}
			if tt.errResp.Error == "" {
				t.Error("Error code should not be empty")
			}
			if tt.errResp.Message == "" {
				t.Error("Message should not be empty")
			}

			// Test serialization
			data, err := json.Marshal(tt.errResp)
			if err != nil {
				t.Fatalf("Failed to marshal error response: %v", err)
			}

			// Test deserialization
			var parsed apierrors.ErrorResponse
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("Failed to unmarshal error response: %v", err)
			}

			// Verify fields match
			if parsed.Status != tt.errResp.Status {
				t.Errorf("Status mismatch: expected %d, got %d", tt.errResp.Status, parsed.Status)
			}
			if parsed.Error != tt.errResp.Error {
				t.Errorf("Error code mismatch: expected %q, got %q", tt.errResp.Error, parsed.Error)
			}
			if parsed.Message != tt.errResp.Message {
				t.Errorf("Message mismatch: expected %q, got %q", tt.errResp.Message, parsed.Message)
			}
			if parsed.Details != tt.errResp.Details {
				t.Errorf("Details mismatch: expected %q, got %q", tt.errResp.Details, parsed.Details)
			}
		})
	}
}

// TestContentTypeHeader verifies that all error responses have the correct Content-Type
func TestContentTypeHeader(t *testing.T) {
	errorFuncs := []func() apierrors.ErrorResponse{
		func() apierrors.ErrorResponse { return apierrors.BadRequest("test", "") },
		func() apierrors.ErrorResponse { return apierrors.Unauthorized("test", "") },
		func() apierrors.ErrorResponse { return apierrors.Forbidden("test", "") },
		func() apierrors.ErrorResponse { return apierrors.NotFound("test", "") },
		func() apierrors.ErrorResponse { return apierrors.Conflict("test", "") },
		func() apierrors.ErrorResponse { return apierrors.ValidationError("test", "") },
		func() apierrors.ErrorResponse { return apierrors.InternalServerError("test", "") },
		func() apierrors.ErrorResponse { return apierrors.ServiceUnavailable("test", "") },
	}

	for _, errFunc := range errorFuncs {
		errResp := errFunc()
		t.Run(errResp.Error, func(t *testing.T) {
			w := httptest.NewRecorder()
			apierrors.WriteError(w, errResp)

			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", ct)
			}

			// Verify no other Content-Type headers
			headers := w.Header().Values("Content-Type")
			if len(headers) != 1 {
				t.Errorf("Expected exactly one Content-Type header, got %d", len(headers))
			}
		})
	}
}

// Mock implementations for testing

type mockSessionStore struct {
	listLogsErr error
}

func (m *mockSessionStore) GetSession(ctx context.Context, sessionID string) (*sessions.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSessionStore) ListSessions(ctx context.Context, deploymentID string) ([]sessions.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSessionStore) UpsertSession(ctx context.Context, s sessions.Session) error {
	return errors.New("not implemented")
}

func (m *mockSessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	return errors.New("not implemented")
}

func (m *mockSessionStore) ClearAttachments(ctx context.Context, deploymentID string) error {
	return errors.New("not implemented")
}

func (m *mockSessionStore) SetAttachment(ctx context.Context, sessionID, clientID string, attached bool) error {
	return errors.New("not implemented")
}

func (m *mockSessionStore) AppendLog(ctx context.Context, entry sessions.LogEntry) error {
	return errors.New("not implemented")
}

func (m *mockSessionStore) ListLogs(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error) {
	if m.listLogsErr != nil {
		return nil, m.listLogsErr
	}
	return []sessions.LogEntry{}, nil
}

func (m *mockSessionStore) PruneLogs(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, errors.New("not implemented")
}

func (m *mockSessionStore) TrimLogs(ctx context.Context, maxEntries int) (int64, error) {
	return 0, errors.New("not implemented")
}
