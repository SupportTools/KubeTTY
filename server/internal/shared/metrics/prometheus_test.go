package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Package-level metrics instance shared across all tests to avoid
// duplicate Prometheus metric registration
var testMetrics *AppMetrics

func init() {
	testMetrics = NewAppMetrics()
}

// TestNewAppMetrics tests that metrics are created successfully
func TestNewAppMetrics(t *testing.T) {
	if testMetrics == nil {
		t.Fatal("testMetrics is nil")
	}

	if testMetrics.wsBytes == nil {
		t.Error("wsBytes metric is nil")
	}
	if testMetrics.storeDuration == nil {
		t.Error("storeDuration metric is nil")
	}
	if testMetrics.storeErrors == nil {
		t.Error("storeErrors metric is nil")
	}
	if testMetrics.httpDuration == nil {
		t.Error("httpDuration metric is nil")
	}
	if testMetrics.httpRequests == nil {
		t.Error("httpRequests metric is nil")
	}
}

// TestObserveWSBytes tests WebSocket byte observation
func TestObserveWSBytes(t *testing.T) {
	tests := []struct {
		name  string
		typ   string
		bytes int
	}{
		{
			name:  "receive bytes",
			typ:   "rx",
			bytes: 1024,
		},
		{
			name:  "transmit bytes",
			typ:   "tx",
			bytes: 2048,
		},
		{
			name:  "zero bytes",
			typ:   "rx",
			bytes: 0,
		},
		{
			name:  "large transfer",
			typ:   "tx",
			bytes: 1048576,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Observe bytes
			testMetrics.ObserveWSBytes(tt.typ, tt.bytes)

			// Verify metric was incremented
			// Note: We can't easily verify the exact value without exposing the registry
			// but we can verify the call doesn't panic
		})
	}
}

// TestObserveWSBytes_Concurrent tests concurrent byte observations
func TestObserveWSBytes_Concurrent(t *testing.T) {
	m := testMetrics

	var wg sync.WaitGroup
	const numGoroutines = 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			m.ObserveWSBytes("rx", id*10)
			m.ObserveWSBytes("tx", id*20)
		}(i)
	}

	wg.Wait()
	// Test passes if no race conditions or panics occur
}

// TestObserveStore tests store operation observation
func TestObserveStore(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		duration  time.Duration
		err       error
	}{
		{
			name:      "successful save",
			operation: "save",
			duration:  100 * time.Millisecond,
			err:       nil,
		},
		{
			name:      "failed save",
			operation: "save",
			duration:  50 * time.Millisecond,
			err:       errors.New("database error"),
		},
		{
			name:      "successful delete",
			operation: "delete",
			duration:  10 * time.Millisecond,
			err:       nil,
		},
		{
			name:      "zero duration",
			operation: "query",
			duration:  0,
			err:       nil,
		},
		{
			name:      "long operation",
			operation: "migrate",
			duration:  5 * time.Second,
			err:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testMetrics

			// Observe store operation
			m.ObserveStore(tt.operation, tt.duration, tt.err)

			// Test passes if no panics occur
		})
	}
}

// TestObserveStore_Concurrent tests concurrent store observations
func TestObserveStore_Concurrent(t *testing.T) {
	m := testMetrics

	var wg sync.WaitGroup
	const numGoroutines = 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			var err error
			if id%2 == 0 {
				err = errors.New("test error")
			}
			m.ObserveStore("save", time.Duration(id)*time.Millisecond, err)
		}(i)
	}

	wg.Wait()
	// Test passes if no race conditions or panics occur
}

// TestInstrumentHandler tests HTTP handler instrumentation
func TestInstrumentHandler(t *testing.T) {
	tests := []struct {
		name       string
		route      string
		method     string
		statusCode int
	}{
		{
			name:       "GET success",
			route:      "/api/sessions",
			method:     "GET",
			statusCode: http.StatusOK,
		},
		{
			name:       "POST created",
			route:      "/api/sessions",
			method:     "POST",
			statusCode: http.StatusCreated,
		},
		{
			name:       "DELETE success",
			route:      "/api/sessions/123",
			method:     "DELETE",
			statusCode: http.StatusNoContent,
		},
		{
			name:       "GET not found",
			route:      "/api/sessions/999",
			method:     "GET",
			statusCode: http.StatusNotFound,
		},
		{
			name:       "POST bad request",
			route:      "/api/sessions",
			method:     "POST",
			statusCode: http.StatusBadRequest,
		},
		{
			name:       "GET internal error",
			route:      "/api/sessions",
			method:     "GET",
			statusCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testMetrics

			// Create a test handler
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(10 * time.Millisecond) // Simulate some work
				w.WriteHeader(tt.statusCode)
			})

			// Wrap with instrumentation
			instrumented := m.InstrumentHandler(tt.route, handler)

			// Make request
			req := httptest.NewRequest(tt.method, "/test", nil)
			w := httptest.NewRecorder()

			instrumented.ServeHTTP(w, req)

			// Verify status code was preserved
			if w.Code != tt.statusCode {
				t.Errorf("status code = %d, want %d", w.Code, tt.statusCode)
			}
		})
	}
}

// TestInstrumentHandler_WithoutExplicitStatus tests default 200 status
func TestInstrumentHandler_WithoutExplicitStatus(t *testing.T) {
	m := testMetrics

	// Handler that doesn't call WriteHeader explicitly
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	instrumented := m.InstrumentHandler("/test", handler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	instrumented.ServeHTTP(w, req)

	// Should default to 200 OK
	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestInstrumentHandler_Concurrent tests concurrent request instrumentation
func TestInstrumentHandler_Concurrent(t *testing.T) {
	m := testMetrics

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	instrumented := m.InstrumentHandler("/test", handler)

	var wg sync.WaitGroup
	const numRequests = 50

	wg.Add(numRequests)
	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			instrumented.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
			}
		}()
	}

	wg.Wait()
}

// TestInstrumentHandler_DurationMeasurement tests that duration is measured
func TestInstrumentHandler_DurationMeasurement(t *testing.T) {
	m := testMetrics

	sleepDuration := 100 * time.Millisecond
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(sleepDuration)
		w.WriteHeader(http.StatusOK)
	})

	instrumented := m.InstrumentHandler("/test", handler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	instrumented.ServeHTTP(w, req)
	actualDuration := time.Since(start)

	// Actual duration should be at least the sleep duration
	if actualDuration < sleepDuration {
		t.Errorf("duration = %v, want at least %v", actualDuration, sleepDuration)
	}
}

// TestInstrumentHandler_MultipleRoutes tests metrics for different routes
func TestInstrumentHandler_MultipleRoutes(t *testing.T) {
	m := testMetrics

	routes := []string{
		"/api/sessions",
		"/api/health",
		"/api/metrics",
		"/ws",
	}

	for _, route := range routes {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		instrumented := m.InstrumentHandler(route, handler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		instrumented.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("route %s: status code = %d, want %d", route, w.Code, http.StatusOK)
		}
	}
}

// TestInstrumentHandler_Panic tests panic propagation
func TestInstrumentHandler_Panic(t *testing.T) {
	m := testMetrics

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	instrumented := m.InstrumentHandler("/test", handler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Should panic (not recovered by instrumentation)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, but none occurred")
		}
	}()

	instrumented.ServeHTTP(w, req)
}

// TestStatusRecorder tests the status recorder
func TestStatusRecorder(t *testing.T) {
	tests := []struct {
		name       string
		writeCode  bool
		statusCode int
		expectCode int
	}{
		{
			name:       "explicit 200",
			writeCode:  true,
			statusCode: http.StatusOK,
			expectCode: http.StatusOK,
		},
		{
			name:       "explicit 404",
			writeCode:  true,
			statusCode: http.StatusNotFound,
			expectCode: http.StatusNotFound,
		},
		{
			name:       "explicit 500",
			writeCode:  true,
			statusCode: http.StatusInternalServerError,
			expectCode: http.StatusInternalServerError,
		},
		{
			name:       "default (no WriteHeader call)",
			writeCode:  false,
			statusCode: 0,
			expectCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			rec := &statusRecorder{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			if tt.writeCode {
				rec.WriteHeader(tt.statusCode)
			}

			rec.Write([]byte("test"))

			// Check recorded status
			if rec.status != tt.expectCode {
				t.Errorf("recorded status = %d, want %d", rec.status, tt.expectCode)
			}

			// Check actual HTTP status
			if w.Code != tt.expectCode {
				t.Errorf("HTTP status = %d, want %d", w.Code, tt.expectCode)
			}
		})
	}
}

// TestStatusRecorder_MultipleWriteHeader tests multiple WriteHeader calls
func TestStatusRecorder_MultipleWriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rec := &statusRecorder{
		ResponseWriter: w,
		status:         http.StatusOK,
	}

	// First call
	rec.WriteHeader(http.StatusOK)

	// Second call - our recorder captures it even though HTTP layer ignores it
	rec.WriteHeader(http.StatusInternalServerError)

	// Our recorder captures the last WriteHeader call
	if rec.status != http.StatusInternalServerError {
		t.Errorf("recorded status = %d, want %d (last WriteHeader call)",
			rec.status, http.StatusInternalServerError)
	}

	// HTTP layer should use first status per HTTP spec
	if w.Code != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d (first WriteHeader call per HTTP spec)", w.Code, http.StatusOK)
	}
}

// TestStatusRecorder_WriteBeforeWriteHeader tests Write called before WriteHeader
func TestStatusRecorder_WriteBeforeWriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rec := &statusRecorder{
		ResponseWriter: w,
		status:         http.StatusOK,
	}

	// Write without calling WriteHeader first
	n, err := rec.Write([]byte("test"))

	if err != nil {
		t.Errorf("Write error = %v, want nil", err)
	}

	if n != 4 {
		t.Errorf("bytes written = %d, want 4", n)
	}

	// Should have default 200 status
	if rec.status != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.status, http.StatusOK)
	}

	if w.Code != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", w.Code, http.StatusOK)
	}

	if w.Body.String() != "test" {
		t.Errorf("body = %q, want %q", w.Body.String(), "test")
	}
}

// TestStatusRecorder_EmptyWrite tests empty Write call
func TestStatusRecorder_EmptyWrite(t *testing.T) {
	w := httptest.NewRecorder()
	rec := &statusRecorder{
		ResponseWriter: w,
		status:         http.StatusOK,
	}

	n, err := rec.Write([]byte{})

	if err != nil {
		t.Errorf("Write error = %v, want nil", err)
	}

	if n != 0 {
		t.Errorf("bytes written = %d, want 0", n)
	}
}

// TestStatusRecorder_LargeWrite tests large data writes
func TestStatusRecorder_LargeWrite(t *testing.T) {
	w := httptest.NewRecorder()
	rec := &statusRecorder{
		ResponseWriter: w,
		status:         http.StatusOK,
	}

	// Create large payload
	data := make([]byte, 1024*1024) // 1MB
	for i := range data {
		data[i] = byte(i % 256)
	}

	n, err := rec.Write(data)

	if err != nil {
		t.Errorf("Write error = %v, want nil", err)
	}

	if n != len(data) {
		t.Errorf("bytes written = %d, want %d", n, len(data))
	}

	if w.Body.Len() != len(data) {
		t.Errorf("buffer length = %d, want %d", w.Body.Len(), len(data))
	}
}

// TestMetricsIntegration tests full integration flow
func TestMetricsIntegration(t *testing.T) {
	m := testMetrics

	// Simulate WebSocket activity
	m.ObserveWSBytes("rx", 1024)
	m.ObserveWSBytes("tx", 2048)

	// Simulate store operations
	m.ObserveStore("save", 100*time.Millisecond, nil)
	m.ObserveStore("delete", 50*time.Millisecond, errors.New("error"))

	// Simulate HTTP requests
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	instrumented := m.InstrumentHandler("/test", handler)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		instrumented.ServeHTTP(w, req)
	}

	// Test passes if no panics occur
}
