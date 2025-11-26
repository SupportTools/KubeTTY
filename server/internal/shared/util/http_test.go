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

// ---- ClientIPFromRequest tests ----

// TestClientIPFromRequest_NilRequest verifies nil request handling
func TestClientIPFromRequest_NilRequest(t *testing.T) {
	ip := ClientIPFromRequest(nil)
	if ip != "" {
		t.Errorf("ClientIPFromRequest(nil) = %q, want empty string", ip)
	}
}

// TestClientIPFromRequest_XForwardedFor verifies X-Forwarded-For header parsing
func TestClientIPFromRequest_XForwardedFor(t *testing.T) {
	tests := []struct {
		name       string
		xForwarded string
		wantIP     string
	}{
		{
			name:       "single IP",
			xForwarded: "192.168.1.1",
			wantIP:     "192.168.1.1",
		},
		{
			name:       "multiple IPs - first is client",
			xForwarded: "10.0.0.1, 192.168.1.1, 172.16.0.1",
			wantIP:     "10.0.0.1",
		},
		{
			name:       "multiple IPs with spaces",
			xForwarded: "  203.0.113.195  ,  70.41.3.18  ,  150.172.238.178  ",
			wantIP:     "203.0.113.195",
		},
		{
			name:       "IPv6 address",
			xForwarded: "2001:db8::1",
			wantIP:     "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("X-Forwarded-For", tt.xForwarded)

			got := ClientIPFromRequest(r)
			if got != tt.wantIP {
				t.Errorf("ClientIPFromRequest() = %q, want %q", got, tt.wantIP)
			}
		})
	}
}

// TestClientIPFromRequest_RemoteAddr verifies fallback to RemoteAddr
func TestClientIPFromRequest_RemoteAddr(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		wantIP     string
	}{
		{
			name:       "IP with port",
			remoteAddr: "192.168.1.100:54321",
			wantIP:     "192.168.1.100",
		},
		{
			name:       "IPv6 with port",
			remoteAddr: "[::1]:54321",
			wantIP:     "::1",
		},
		{
			name:       "IP without port - fallback to full value",
			remoteAddr: "192.168.1.100",
			wantIP:     "192.168.1.100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			// No X-Forwarded-For header

			got := ClientIPFromRequest(r)
			if got != tt.wantIP {
				t.Errorf("ClientIPFromRequest() = %q, want %q", got, tt.wantIP)
			}
		})
	}
}

// TestClientIPFromRequest_XForwardedForPriority verifies X-Forwarded-For takes priority
func TestClientIPFromRequest_XForwardedForPriority(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Forwarded-For", "203.0.113.1")

	got := ClientIPFromRequest(r)
	if got != "203.0.113.1" {
		t.Errorf("ClientIPFromRequest() = %q, want %q (X-Forwarded-For should take priority)", got, "203.0.113.1")
	}
}

// ---- WebSocketScheme tests ----

// TestWebSocketScheme_NoTLS verifies ws scheme for non-TLS requests
func TestWebSocketScheme_NoTLS(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	// No TLS, no X-Forwarded-Proto

	got := WebSocketScheme(r)
	if got != "ws" {
		t.Errorf("WebSocketScheme() = %q, want %q", got, "ws")
	}
}

// TestWebSocketScheme_WithTLS verifies wss scheme for TLS requests
func TestWebSocketScheme_WithTLS(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "https://example.com/ws", nil)
	// httptest.NewRequest with https sets TLS to non-nil

	got := WebSocketScheme(r)
	if got != "wss" {
		t.Errorf("WebSocketScheme() = %q, want %q", got, "wss")
	}
}

// TestWebSocketScheme_XForwardedProto verifies wss scheme via X-Forwarded-Proto header
func TestWebSocketScheme_XForwardedProto(t *testing.T) {
	tests := []struct {
		name        string
		headerValue string
		wantScheme  string
	}{
		{
			name:        "https header",
			headerValue: "https",
			wantScheme:  "wss",
		},
		{
			name:        "HTTPS uppercase",
			headerValue: "HTTPS",
			wantScheme:  "wss",
		},
		{
			name:        "http header",
			headerValue: "http",
			wantScheme:  "ws",
		},
		{
			name:        "HTTP uppercase",
			headerValue: "HTTP",
			wantScheme:  "ws",
		},
		{
			name:        "empty header",
			headerValue: "",
			wantScheme:  "ws",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tt.headerValue != "" {
				r.Header.Set("X-Forwarded-Proto", tt.headerValue)
			}

			got := WebSocketScheme(r)
			if got != tt.wantScheme {
				t.Errorf("WebSocketScheme() = %q, want %q", got, tt.wantScheme)
			}
		})
	}
}
