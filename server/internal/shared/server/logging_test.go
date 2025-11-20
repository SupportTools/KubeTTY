package server

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

// TestLoggingMiddleware_BasicLogging tests basic logging functionality
func TestLoggingMiddleware_BasicLogging(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		remoteAddr string
	}{
		{
			name:       "GET request",
			method:     "GET",
			path:       "/api/sessions",
			remoteAddr: "192.168.1.1:12345",
		},
		{
			name:       "POST request",
			method:     "POST",
			path:       "/api/sessions/new",
			remoteAddr: "10.0.0.1:54321",
		},
		{
			name:       "DELETE request",
			method:     "DELETE",
			path:       "/api/sessions/123",
			remoteAddr: "172.16.0.1:9999",
		},
		{
			name:       "PUT request",
			method:     "PUT",
			path:       "/api/config",
			remoteAddr: "127.0.0.1:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test hook and add it to the global logger
			hook := test.NewGlobal()
			defer hook.Reset()

			// Set log level to debug to capture all logs
			originalLevel := log.GetLevel()
			log.SetLevel(log.DebugLevel)
			defer log.SetLevel(originalLevel)

			// Create a simple handler
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap with logging middleware
			wrappedHandler := LoggingMiddleware(handler)

			// Create request
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.RemoteAddr = tt.remoteAddr
			w := httptest.NewRecorder()

			// Execute request
			wrappedHandler.ServeHTTP(w, req)

			// Verify we have at least 2 log entries (debug + info)
			if len(hook.Entries) < 2 {
				t.Fatalf("expected at least 2 log entries, got %d", len(hook.Entries))
			}

			// Find debug and info entries
			var debugEntry, infoEntry *log.Entry
			for i := range hook.Entries {
				entry := &hook.Entries[i]
				if entry.Level == log.DebugLevel && entry.Message == "Request received" {
					debugEntry = entry
				}
				if entry.Level == log.InfoLevel && entry.Message == "Request completed" {
					infoEntry = entry
				}
			}

			// Verify debug entry
			if debugEntry == nil {
				t.Fatal("expected debug log entry not found")
			}
			if debugEntry.Data["method"] != tt.method {
				t.Errorf("debug method = %v, want %v", debugEntry.Data["method"], tt.method)
			}
			if debugEntry.Data["path"] != tt.path {
				t.Errorf("debug path = %v, want %v", debugEntry.Data["path"], tt.path)
			}
			if debugEntry.Data["remote"] != tt.remoteAddr {
				t.Errorf("debug remote = %v, want %v", debugEntry.Data["remote"], tt.remoteAddr)
			}

			// Verify info entry
			if infoEntry == nil {
				t.Fatal("expected info log entry not found")
			}
			if infoEntry.Data["method"] != tt.method {
				t.Errorf("info method = %v, want %v", infoEntry.Data["method"], tt.method)
			}
			if infoEntry.Data["path"] != tt.path {
				t.Errorf("info path = %v, want %v", infoEntry.Data["path"], tt.path)
			}
			if _, ok := infoEntry.Data["duration"]; !ok {
				t.Error("info entry missing duration field")
			}
		})
	}
}

// TestLoggingMiddleware_Duration tests that duration is measured correctly
func TestLoggingMiddleware_Duration(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	originalLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(originalLevel)

	// Create handler that sleeps to ensure measurable duration
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := LoggingMiddleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Find the info entry with duration
	var infoEntry *log.Entry
	for i := range hook.Entries {
		entry := &hook.Entries[i]
		if entry.Level == log.InfoLevel && entry.Message == "Request completed" {
			infoEntry = entry
			break
		}
	}

	if infoEntry == nil {
		t.Fatal("expected info log entry not found")
	}

	duration, ok := infoEntry.Data["duration"].(time.Duration)
	if !ok {
		t.Fatalf("duration field is not a time.Duration: %T", infoEntry.Data["duration"])
	}

	// Duration should be at least 50ms (the sleep time)
	if duration < 50*time.Millisecond {
		t.Errorf("duration = %v, want at least 50ms", duration)
	}

	// But should be reasonable (less than 1 second for a simple handler)
	if duration > 1*time.Second {
		t.Errorf("duration = %v, seems too long", duration)
	}
}

// TestLoggingMiddleware_QueryParameters tests logging with query parameters
func TestLoggingMiddleware_QueryParameters(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	originalLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(originalLevel)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := LoggingMiddleware(handler)

	req := httptest.NewRequest("GET", "/api/sessions?limit=10&offset=20", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Verify the path is logged correctly (with query params)
	var debugEntry *log.Entry
	for i := range hook.Entries {
		entry := &hook.Entries[i]
		if entry.Level == log.DebugLevel {
			debugEntry = entry
			break
		}
	}

	if debugEntry == nil {
		t.Fatal("expected debug log entry not found")
	}

	// The path should be just the path without query params
	if debugEntry.Data["path"] != "/api/sessions" {
		t.Errorf("path = %v, want /api/sessions", debugEntry.Data["path"])
	}
}

// TestLoggingMiddleware_RootPath tests logging for root path
func TestLoggingMiddleware_RootPath(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	originalLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(originalLevel)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := LoggingMiddleware(handler)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	var debugEntry *log.Entry
	for i := range hook.Entries {
		entry := &hook.Entries[i]
		if entry.Level == log.DebugLevel {
			debugEntry = entry
			break
		}
	}

	if debugEntry == nil {
		t.Fatal("expected debug log entry not found")
	}

	if debugEntry.Data["path"] != "/" {
		t.Errorf("path = %v, want /", debugEntry.Data["path"])
	}
}

// TestLoggingMiddleware_HandlerPanic tests that panics are not caught
func TestLoggingMiddleware_HandlerPanic(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	originalLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(originalLevel)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	wrappedHandler := LoggingMiddleware(handler)

	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()

	// Should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, but none occurred")
		} else if r != "test panic" {
			t.Errorf("panic value = %v, want 'test panic'", r)
		}
	}()

	wrappedHandler.ServeHTTP(w, req)
}

// TestLoggingMiddleware_ConcurrentRequests tests thread safety
func TestLoggingMiddleware_ConcurrentRequests(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	originalLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(originalLevel)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := LoggingMiddleware(handler)

	const numRequests = 10
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(w, req)
		}(i)
	}

	wg.Wait()

	// We should have at least numRequests * 2 log entries (debug + info for each)
	if len(hook.Entries) < numRequests*2 {
		t.Errorf("expected at least %d log entries, got %d", numRequests*2, len(hook.Entries))
	}
}

// TestLoggingMiddleware_DifferentStatusCodes tests logging with various status codes
func TestLoggingMiddleware_DifferentStatusCodes(t *testing.T) {
	statusCodes := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusInternalServerError,
	}

	for _, code := range statusCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			hook := test.NewGlobal()
			defer hook.Reset()

			originalLevel := log.GetLevel()
			log.SetLevel(log.DebugLevel)
			defer log.SetLevel(originalLevel)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			})

			wrappedHandler := LoggingMiddleware(handler)

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(w, req)

			// Verify logs were created regardless of status code
			if len(hook.Entries) < 2 {
				t.Errorf("expected at least 2 log entries, got %d", len(hook.Entries))
			}

			// Verify the actual response status code
			if w.Code != code {
				t.Errorf("response code = %d, want %d", w.Code, code)
			}
		})
	}
}

// TestLoggingMiddleware_DefaultPath tests logging with default path
func TestLoggingMiddleware_DefaultPath(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	originalLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(originalLevel)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := LoggingMiddleware(handler)

	// Create request with explicit path
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Should still log successfully
	if len(hook.Entries) < 2 {
		t.Errorf("expected at least 2 log entries, got %d", len(hook.Entries))
	}
}

// TestLoggingMiddleware_SpecialCharactersInPath tests logging with special characters
func TestLoggingMiddleware_SpecialCharactersInPath(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "URL encoded characters",
			path: "/api/sessions/%2Ftest%2F",
		},
		{
			name: "unicode characters",
			path: "/api/世界",
		},
		{
			name: "special symbols",
			path: "/api/test-special!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()
			defer hook.Reset()

			originalLevel := log.GetLevel()
			log.SetLevel(log.DebugLevel)
			defer log.SetLevel(originalLevel)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrappedHandler := LoggingMiddleware(handler)

			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(w, req)

			// Should log successfully even with special characters
			if len(hook.Entries) < 2 {
				t.Errorf("expected at least 2 log entries, got %d", len(hook.Entries))
			}
		})
	}
}
