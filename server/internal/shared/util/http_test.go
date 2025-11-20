package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestWriteJSON_SimpleTypes tests WriteJSON with basic types
func TestWriteJSON_SimpleTypes(t *testing.T) {
	tests := []struct {
		name       string
		payload    any
		statusCode int
		wantBody   string
	}{
		{
			name:       "string payload",
			payload:    "hello world",
			statusCode: http.StatusOK,
			wantBody:   `"hello world"`,
		},
		{
			name:       "integer payload",
			payload:    42,
			statusCode: http.StatusCreated,
			wantBody:   "42",
		},
		{
			name:       "boolean payload",
			payload:    true,
			statusCode: http.StatusAccepted,
			wantBody:   "true",
		},
		{
			name:       "nil payload",
			payload:    nil,
			statusCode: http.StatusNoContent,
			wantBody:   "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := WriteJSON(w, tt.statusCode, tt.payload)

			if err != nil {
				t.Errorf("WriteJSON() error = %v, want nil", err)
			}

			if w.Code != tt.statusCode {
				t.Errorf("status code = %d, want %d", w.Code, tt.statusCode)
			}

			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			if body := w.Body.String(); body != tt.wantBody+"\n" {
				t.Errorf("body = %q, want %q", body, tt.wantBody+"\n")
			}
		})
	}
}

// TestWriteJSON_ComplexTypes tests WriteJSON with structs and maps
func TestWriteJSON_ComplexTypes(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Age   int    `json:"age"`
		Email string `json:"email,omitempty"`
	}

	tests := []struct {
		name       string
		payload    any
		statusCode int
		wantBody   string
	}{
		{
			name: "struct payload",
			payload: testStruct{
				Name: "John Doe",
				Age:  30,
			},
			statusCode: http.StatusOK,
			wantBody:   `{"name":"John Doe","age":30}`,
		},
		{
			name: "map payload",
			payload: map[string]any{
				"status":  "healthy",
				"version": "1.0.0",
			},
			statusCode: http.StatusOK,
			wantBody:   `{"status":"healthy","version":"1.0.0"}`,
		},
		{
			name: "slice payload",
			payload: []string{
				"item1",
				"item2",
				"item3",
			},
			statusCode: http.StatusOK,
			wantBody:   `["item1","item2","item3"]`,
		},
		{
			name: "nested struct",
			payload: map[string]any{
				"user": map[string]any{
					"name": "Alice",
					"age":  25,
				},
				"active": true,
			},
			statusCode: http.StatusOK,
			wantBody:   `{"active":true,"user":{"age":25,"name":"Alice"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := WriteJSON(w, tt.statusCode, tt.payload)

			if err != nil {
				t.Errorf("WriteJSON() error = %v, want nil", err)
			}

			if w.Code != tt.statusCode {
				t.Errorf("status code = %d, want %d", w.Code, tt.statusCode)
			}

			// For complex types, verify JSON is valid by unmarshaling
			var result any
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Errorf("failed to unmarshal response: %v", err)
			}
		})
	}
}

// TestWriteJSON_StatusCodes tests various HTTP status codes
func TestWriteJSON_StatusCodes(t *testing.T) {
	statusCodes := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNoContent,
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	}

	for _, code := range statusCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			w := httptest.NewRecorder()
			payload := map[string]any{"status": code}

			err := WriteJSON(w, code, payload)
			if err != nil {
				t.Errorf("WriteJSON() error = %v, want nil", err)
			}

			if w.Code != code {
				t.Errorf("status code = %d, want %d", w.Code, code)
			}
		})
	}
}

// TestWriteJSON_EmptyPayloads tests edge cases with empty data
func TestWriteJSON_EmptyPayloads(t *testing.T) {
	tests := []struct {
		name     string
		payload  any
		wantBody string
	}{
		{
			name:     "empty string",
			payload:  "",
			wantBody: `""`,
		},
		{
			name:     "empty map",
			payload:  map[string]any{},
			wantBody: `{}`,
		},
		{
			name:     "empty slice",
			payload:  []string{},
			wantBody: `[]`,
		},
		{
			name:     "empty struct",
			payload:  struct{}{},
			wantBody: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := WriteJSON(w, http.StatusOK, tt.payload)

			if err != nil {
				t.Errorf("WriteJSON() error = %v, want nil", err)
			}

			if body := w.Body.String(); body != tt.wantBody+"\n" {
				t.Errorf("body = %q, want %q", body, tt.wantBody+"\n")
			}
		})
	}
}

// unencodableType is a type that cannot be JSON encoded
type unencodableType struct {
	Ch chan int
}

// TestWriteJSON_EncodingError tests error handling for unencodable types
func TestWriteJSON_EncodingError(t *testing.T) {
	w := httptest.NewRecorder()

	// Channels cannot be JSON encoded
	payload := unencodableType{
		Ch: make(chan int),
	}

	err := WriteJSON(w, http.StatusOK, payload)

	if err == nil {
		t.Error("WriteJSON() expected error for unencodable type, got nil")
	}

	// Status code should still be written even if encoding fails
	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	// Content-Type should still be set
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

// TestWriteJSON_ConcurrentCalls tests thread safety
func TestWriteJSON_ConcurrentCalls(t *testing.T) {
	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			w := httptest.NewRecorder()
			payload := map[string]any{
				"id":      id,
				"message": "concurrent test",
			}

			if err := WriteJSON(w, http.StatusOK, payload); err != nil {
				errCh <- err
				return
			}

			if w.Code != http.StatusOK {
				errCh <- errors.New("incorrect status code")
				return
			}

			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				errCh <- errors.New("incorrect content type")
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent call failed: %v", err)
	}
}

// TestWriteJSON_HeadersAlreadyWritten tests behavior when headers are already set
func TestWriteJSON_HeadersAlreadyWritten(t *testing.T) {
	w := httptest.NewRecorder()

	// Set some headers before calling WriteJSON
	w.Header().Set("X-Custom-Header", "custom-value")

	payload := map[string]string{"message": "test"}
	err := WriteJSON(w, http.StatusOK, payload)

	if err != nil {
		t.Errorf("WriteJSON() error = %v, want nil", err)
	}

	// Content-Type should be set by WriteJSON
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Custom header should still be present
	if ch := w.Header().Get("X-Custom-Header"); ch != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want %q", ch, "custom-value")
	}
}

// TestWriteJSON_LargePayload tests handling of large JSON payloads
func TestWriteJSON_LargePayload(t *testing.T) {
	// Create a large slice of data
	largeSlice := make([]map[string]any, 1000)
	for i := 0; i < 1000; i++ {
		largeSlice[i] = map[string]any{
			"id":      i,
			"name":    fmt.Sprintf("User %d", i),
			"email":   fmt.Sprintf("user%d@example.com", i),
			"active":  i%2 == 0,
			"score":   float64(i) * 1.5,
		}
	}

	w := httptest.NewRecorder()
	err := WriteJSON(w, http.StatusOK, largeSlice)

	if err != nil {
		t.Errorf("WriteJSON() error = %v, want nil", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify the response can be decoded
	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Errorf("failed to unmarshal large payload: %v", err)
	}

	if len(result) != 1000 {
		t.Errorf("decoded slice length = %d, want 1000", len(result))
	}
}

// TestWriteJSON_SpecialCharacters tests handling of special characters in JSON
func TestWriteJSON_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name    string
		payload any
	}{
		{
			name:    "unicode characters",
			payload: map[string]string{"message": "Hello 世界 🌍"},
		},
		{
			name:    "escaped characters",
			payload: map[string]string{"message": "Line1\nLine2\tTabbed"},
		},
		{
			name:    "quotes and backslashes",
			payload: map[string]string{"message": `He said "Hello" \ Backslash`},
		},
		{
			name:    "HTML tags",
			payload: map[string]string{"message": "<script>alert('xss')</script>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := WriteJSON(w, http.StatusOK, tt.payload)

			if err != nil {
				t.Errorf("WriteJSON() error = %v, want nil", err)
			}

			// Verify the response can be decoded back
			var result map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Errorf("failed to unmarshal response: %v", err)
			}
		})
	}
}

// TestWriteJSON_NilResponseWriter tests behavior with nil ResponseWriter
// This test is commented out as it would panic - documenting expected behavior
// func TestWriteJSON_NilResponseWriter(t *testing.T) {
// 	// This would panic, which is acceptable behavior
// 	// err := WriteJSON(nil, http.StatusOK, "test")
// }
