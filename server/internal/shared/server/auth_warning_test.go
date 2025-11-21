package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthWarningMiddleware(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	tests := []struct {
		name          string
		authMode      string
		wantHeader    bool
		wantHeaderVal string
	}{
		{
			name:          "auth disabled - should add warning header",
			authMode:      "disabled",
			wantHeader:    true,
			wantHeaderVal: "Authentication is disabled",
		},
		{
			name:          "auth enabled (local) - should not add warning header",
			authMode:      "local",
			wantHeader:    false,
			wantHeaderVal: "",
		},
		{
			name:          "empty auth mode - should add warning header",
			authMode:      "",
			wantHeader:    true,
			wantHeaderVal: "Authentication is disabled",
		},
		{
			name:          "unknown auth mode - should add warning header",
			authMode:      "unknown",
			wantHeader:    true,
			wantHeaderVal: "Authentication is disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := AuthWarningMiddleware(tt.authMode)
			handler := middleware(testHandler)

			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			headerVal := rec.Header().Get("X-Auth-Warning")
			hasHeader := headerVal != ""

			if hasHeader != tt.wantHeader {
				t.Errorf("AuthWarningMiddleware(%q) header present = %v, want %v", tt.authMode, hasHeader, tt.wantHeader)
			}

			if tt.wantHeader && headerVal != tt.wantHeaderVal {
				t.Errorf("AuthWarningMiddleware(%q) header value = %q, want %q", tt.authMode, headerVal, tt.wantHeaderVal)
			}

			if rec.Code != http.StatusOK {
				t.Errorf("AuthWarningMiddleware(%q) status = %d, want %d", tt.authMode, rec.Code, http.StatusOK)
			}
		})
	}
}

func TestAuthWarningMiddleware_PassthroughForLocalAuth(t *testing.T) {
	// Verify that when auth is "local", the middleware is essentially a no-op
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("Created"))
	})

	middleware := AuthWarningMiddleware("local")
	handler := middleware(testHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/resource", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should pass through without modification
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec.Header().Get("X-Custom") != "value" {
		t.Errorf("X-Custom header = %q, want %q", rec.Header().Get("X-Custom"), "value")
	}
	if rec.Header().Get("X-Auth-Warning") != "" {
		t.Errorf("X-Auth-Warning header should not be present when auth is local")
	}
}
