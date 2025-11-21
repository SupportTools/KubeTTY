package util

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWriteJSON_Success verifies that WriteJSON correctly encodes and sends valid JSON payloads
func TestWriteJSON_Success(t *testing.T) {
	tests := []struct {
		name           string
		payload        any
		status         int
		expectedBody   string
		expectedStatus int
	}{
		{
			name:           "simple map",
			payload:        map[string]string{"message": "hello"},
			status:         http.StatusOK,
			expectedBody:   `{"message":"hello"}` + "\n",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "struct with fields",
			payload:        struct{ Name string }{Name: "test"},
			status:         http.StatusCreated,
			expectedBody:   `{"Name":"test"}` + "\n",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "empty slice",
			payload:        []string{},
			status:         http.StatusOK,
			expectedBody:   "[]\n",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "nil payload",
			payload:        nil,
			status:         http.StatusNoContent,
			expectedBody:   "null\n",
			expectedStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()

			err := WriteJSON(w, tt.status, tt.payload)
			if err != nil {
				t.Errorf("WriteJSON() error = %v, want nil", err)
			}

			// Verify status code
			if w.Code != tt.expectedStatus {
				t.Errorf("WriteJSON() status = %d, want %d", w.Code, tt.expectedStatus)
			}

			// Verify Content-Type header
			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("WriteJSON() Content-Type = %q, want %q", ct, "application/json")
			}

			// Verify response body
			if body := w.Body.String(); body != tt.expectedBody {
				t.Errorf("WriteJSON() body = %q, want %q", body, tt.expectedBody)
			}
		})
	}
}

// unencodableType is a type that cannot be JSON-encoded
type unencodableType struct {
	// channels cannot be JSON-encoded
	Channel chan int
}

// TestWriteJSON_EncodingError verifies that encoding errors are handled properly
// and that headers are NOT sent when encoding fails
func TestWriteJSON_EncodingError(t *testing.T) {
	w := httptest.NewRecorder()

	// Attempt to encode a channel, which is not JSON-encodable
	invalidPayload := unencodableType{
		Channel: make(chan int),
	}

	err := WriteJSON(w, http.StatusOK, invalidPayload)

	// Verify error is returned
	if err == nil {
		t.Fatal("WriteJSON() error = nil, want error for unencodable type")
	}

	// Verify it's a JSON encoding error
	if _, ok := err.(*json.UnsupportedTypeError); !ok {
		t.Errorf("WriteJSON() error type = %T, want *json.UnsupportedTypeError", err)
	}

	// CRITICAL: Verify headers were NOT sent (status code should be 200, the default)
	// This is the key security fix - if encoding fails, we don't send any status code
	if w.Code != http.StatusOK {
		t.Errorf("WriteJSON() wrote status code %d despite encoding error, headers should not be written", w.Code)
	}

	// Verify Content-Type header was NOT set
	ct := w.Header().Get("Content-Type")
	if ct == "application/json" {
		t.Error("WriteJSON() set Content-Type despite encoding error, headers should not be written")
	}

	// Verify no body was written
	if w.Body.Len() > 0 {
		t.Errorf("WriteJSON() wrote %d bytes despite encoding error, body should be empty", w.Body.Len())
	}
}

// TestWriteJSON_MultipleStatusCodes verifies different HTTP status codes work correctly
func TestWriteJSON_MultipleStatusCodes(t *testing.T) {
	statusCodes := []int{
		http.StatusOK,                  // 200
		http.StatusCreated,             // 201
		http.StatusAccepted,            // 202
		http.StatusNoContent,           // 204
		http.StatusBadRequest,          // 400
		http.StatusUnauthorized,        // 401
		http.StatusForbidden,           // 403
		http.StatusNotFound,            // 404
		http.StatusInternalServerError, // 500
		http.StatusServiceUnavailable,  // 503
	}

	for _, status := range statusCodes {
		t.Run(http.StatusText(status), func(t *testing.T) {
			w := httptest.NewRecorder()
			payload := map[string]int{"status": status}

			err := WriteJSON(w, status, payload)
			if err != nil {
				t.Errorf("WriteJSON() error = %v, want nil", err)
			}

			if w.Code != status {
				t.Errorf("WriteJSON() status = %d, want %d", w.Code, status)
			}
		})
	}
}

// TestWriteJSON_LargePayload verifies WriteJSON handles large payloads correctly
func TestWriteJSON_LargePayload(t *testing.T) {
	w := httptest.NewRecorder()

	// Create a large payload (100 items)
	largeSlice := make([]map[string]int, 100)
	for i := 0; i < 100; i++ {
		largeSlice[i] = map[string]int{
			"id":    i,
			"value": i * 2,
		}
	}

	err := WriteJSON(w, http.StatusOK, largeSlice)
	if err != nil {
		t.Errorf("WriteJSON() error = %v, want nil for large payload", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("WriteJSON() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify the response is valid JSON
	var decoded []map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Errorf("WriteJSON() produced invalid JSON: %v", err)
	}

	if len(decoded) != 100 {
		t.Errorf("WriteJSON() decoded length = %d, want 100", len(decoded))
	}
}
