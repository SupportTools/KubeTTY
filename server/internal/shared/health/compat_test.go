package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestNewCompatHandler_DatabaseHealthy tests healthy database scenario
func TestNewCompatHandler_DatabaseHealthy(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	handler := NewCompatHandler(db)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("status = %v, want 'healthy'", response["status"])
	}

	components, ok := response["components"].(map[string]any)
	if !ok {
		t.Fatalf("components is not a map")
	}

	if components["database"] != "ok" {
		t.Errorf("database = %v, want 'ok'", components["database"])
	}
}

// TestNewCompatHandler_DatabaseUnhealthy tests unhealthy database scenario
func TestNewCompatHandler_DatabaseUnhealthy(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return errors.New("connection refused")
		},
	}

	handler := NewCompatHandler(db)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["status"] != "unhealthy" {
		t.Errorf("status = %v, want 'unhealthy'", response["status"])
	}

	components, ok := response["components"].(map[string]any)
	if !ok {
		t.Fatalf("components is not a map")
	}

	dbStatus, ok := components["database"].(string)
	if !ok {
		t.Fatalf("database status is not a string")
	}

	if dbStatus != "error: connection refused" {
		t.Errorf("database = %v, want 'error: connection refused'", dbStatus)
	}
}

// TestNewCompatHandler_NilDatabase tests nil database scenario
func TestNewCompatHandler_NilDatabase(t *testing.T) {
	handler := NewCompatHandler(nil)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("status = %v, want 'healthy'", response["status"])
	}

	components, ok := response["components"].(map[string]any)
	if !ok {
		t.Fatalf("components is not a map")
	}

	if components["database"] != "not_configured" {
		t.Errorf("database = %v, want 'not_configured'", components["database"])
	}
}

// TestNewCompatHandler_GatewayChecker tests gateway component checker
func TestNewCompatHandler_GatewayChecker(t *testing.T) {
	tests := []struct {
		name            string
		managerEnabled  bool
		expectedGateway string
	}{
		{
			name:            "gateway enabled",
			managerEnabled:  true,
			expectedGateway: "enabled",
		},
		{
			name:            "gateway disabled",
			managerEnabled:  false,
			expectedGateway: "disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &mockPinger{
				pingFunc: func(ctx context.Context) error {
					return nil
				},
			}

			gatewayChecker := NewComponentChecker("gateway", func() string {
				if tt.managerEnabled {
					return "enabled"
				}
				return "disabled"
			})

			handler := NewCompatHandler(db, gatewayChecker)

			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			var response map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			components, ok := response["components"].(map[string]any)
			if !ok {
				t.Fatalf("components is not a map")
			}

			if components["gateway"] != tt.expectedGateway {
				t.Errorf("gateway = %v, want %v", components["gateway"], tt.expectedGateway)
			}
		})
	}
}

// TestNewCompatHandler_PTYChecker tests PTY component checker
func TestNewCompatHandler_PTYChecker(t *testing.T) {
	tests := []struct {
		name        string
		ptyAlive    bool
		expectedPTY string
	}{
		{
			name:        "pty alive",
			ptyAlive:    true,
			expectedPTY: "alive",
		},
		{
			name:        "pty not started",
			ptyAlive:    false,
			expectedPTY: "not_started",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &mockPinger{
				pingFunc: func(ctx context.Context) error {
					return nil
				},
			}

			var mu sync.RWMutex
			ptyChecker := NewPTYChecker(&mu, func() bool {
				return tt.ptyAlive
			})

			handler := NewCompatHandler(db, ptyChecker)

			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			var response map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			components, ok := response["components"].(map[string]any)
			if !ok {
				t.Fatalf("components is not a map")
			}

			if components["pty"] != tt.expectedPTY {
				t.Errorf("pty = %v, want %v", components["pty"], tt.expectedPTY)
			}
		})
	}
}

// TestNewCompatHandler_MultipleCheckers tests multiple component checkers
func TestNewCompatHandler_MultipleCheckers(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	checker1 := NewComponentChecker("component1", func() string {
		return "active"
	})

	checker2 := NewComponentChecker("component2", func() string {
		return "ready"
	})

	handler := NewCompatHandler(db, checker1, checker2)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	components, ok := response["components"].(map[string]any)
	if !ok {
		t.Fatalf("components is not a map")
	}

	if components["database"] != "ok" {
		t.Errorf("database = %v, want 'ok'", components["database"])
	}

	if components["component1"] != "active" {
		t.Errorf("component1 = %v, want 'active'", components["component1"])
	}

	if components["component2"] != "ready" {
		t.Errorf("component2 = %v, want 'ready'", components["component2"])
	}
}

// TestNewCompatHandler_ResponseFormat tests exact response format matching
func TestNewCompatHandler_ResponseFormat(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	handler := NewCompatHandler(db)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify Content-Type header
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want 'application/json'", ct)
	}

	// Verify response structure
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify top-level keys
	if _, ok := response["status"]; !ok {
		t.Error("response missing 'status' field")
	}

	if _, ok := response["components"]; !ok {
		t.Error("response missing 'components' field")
	}

	// Verify components is an object
	if _, ok := response["components"].(map[string]any); !ok {
		t.Error("components is not an object")
	}
}

// TestComponentChecker_ThreadSafety tests concurrent access to component checker
func TestComponentChecker_ThreadSafety(t *testing.T) {
	counter := 0
	var counterMu sync.Mutex
	checker := NewComponentChecker("test", func() string {
		counterMu.Lock()
		counter++
		counterMu.Unlock()
		return "ok"
	})

	var wg sync.WaitGroup
	const numGoroutines = 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			checker.Check(context.Background())
		}()
	}

	wg.Wait()

	counterMu.Lock()
	defer counterMu.Unlock()
	if counter != numGoroutines {
		t.Errorf("counter = %d, want %d", counter, numGoroutines)
	}
}

// TestPTYChecker_MutexProtection tests that PTY checker properly uses mutex
func TestPTYChecker_MutexProtection(t *testing.T) {
	var mu sync.RWMutex
	callCount := 0
	var countMu sync.Mutex

	checker := NewPTYChecker(&mu, func() bool {
		countMu.Lock()
		callCount++
		countMu.Unlock()
		return true
	})

	// Test that multiple concurrent Check calls work (RLock allows concurrent readers)
	ctx := context.Background()

	var wg sync.WaitGroup
	const numChecks = 10

	wg.Add(numChecks)
	for i := 0; i < numChecks; i++ {
		go func() {
			defer wg.Done()
			checker.Check(ctx)
		}()
	}

	wg.Wait()

	countMu.Lock()
	defer countMu.Unlock()
	if callCount != numChecks {
		t.Errorf("callCount = %d, want %d", callCount, numChecks)
	}
}

// TestNewCompatHandler_ConcurrentRequests tests thread safety
func TestNewCompatHandler_ConcurrentRequests(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	handler := NewCompatHandler(db)

	var wg sync.WaitGroup
	const numRequests = 50

	wg.Add(numRequests)
	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
			}
		}()
	}

	wg.Wait()
}
