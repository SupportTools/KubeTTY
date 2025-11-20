package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockPinger implements the Pinger interface for testing
type mockPinger struct {
	pingFunc func(ctx context.Context) error
}

func (m *mockPinger) Ping(ctx context.Context) error {
	if m.pingFunc != nil {
		return m.pingFunc(ctx)
	}
	return nil
}

// mockChecker implements the Checker interface for testing
type mockChecker struct {
	checkFunc func(ctx context.Context) (healthy bool, message string)
}

func (m *mockChecker) Check(ctx context.Context) (healthy bool, message string) {
	if m.checkFunc != nil {
		return m.checkFunc(ctx)
	}
	return true, "ok"
}

// TestNewHandler_Healthy tests the handler when everything is healthy
func TestNewHandler_Healthy(t *testing.T) {
	// Mock database that's healthy
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	// Create checker
	checker := &mockChecker{
		checkFunc: func(ctx context.Context) (bool, string) {
			return true, "cache"
		},
	}

	handler := NewHandler(db, checker)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	// Should contain "healthy" status
	body := w.Body.String()
	if !strings.Contains(body, "healthy") {
		t.Errorf("body missing 'healthy' status: %s", body)
	}
	if !strings.Contains(body, "database") {
		t.Errorf("body missing 'database' field: %s", body)
	}
	if !strings.Contains(body, "cache") {
		t.Errorf("body missing 'cache' field: %s", body)
	}
}

// TestNewHandler_DatabaseUnhealthy tests when database is unavailable
func TestNewHandler_DatabaseUnhealthy(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return errors.New("connection refused")
		},
	}

	handler := NewHandler(db)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	body := w.Body.String()
	if !strings.Contains(body, "unhealthy") {
		t.Errorf("body missing 'unhealthy' status: %s", body)
	}
	if !strings.Contains(body, "unavailable") {
		t.Errorf("body missing 'unavailable' field: %s", body)
	}
}

// TestNewHandler_CheckerUnhealthy tests when a checker reports unhealthy
func TestNewHandler_CheckerUnhealthy(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	checker := &mockChecker{
		checkFunc: func(ctx context.Context) (bool, string) {
			return false, "cache"
		},
	}

	handler := NewHandler(db, checker)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	body := w.Body.String()
	if !strings.Contains(body, "unhealthy") {
		t.Errorf("body missing 'unhealthy' status: %s", body)
	}
}

// TestNewHandler_NilDatabase tests with no database (nil pinger)
func TestNewHandler_NilDatabase(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "healthy") {
		t.Errorf("body missing 'healthy' status: %s", body)
	}
}

// TestNewHandler_MultipleCheckers tests with multiple custom checkers
func TestNewHandler_MultipleCheckers(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	checker1 := &mockChecker{
		checkFunc: func(ctx context.Context) (bool, string) {
			return true, "cache"
		},
	}

	checker2 := &mockChecker{
		checkFunc: func(ctx context.Context) (bool, string) {
			return true, "queue"
		},
	}

	checker3 := &mockChecker{
		checkFunc: func(ctx context.Context) (bool, string) {
			return true, "storage"
		},
	}

	handler := NewHandler(db, checker1, checker2, checker3)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "cache") || !strings.Contains(body, "queue") || !strings.Contains(body, "storage") {
		t.Errorf("body missing expected checker fields: %s", body)
	}
}

// TestNewHandler_FirstCheckerFails tests early exit when first checker fails
func TestNewHandler_FirstCheckerFails(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	checker1 := &mockChecker{
		checkFunc: func(ctx context.Context) (bool, string) {
			return false, "cache"
		},
	}

	// This checker should not be called since the first one fails
	checker2Called := false
	checker2 := &mockChecker{
		checkFunc: func(ctx context.Context) (bool, string) {
			checker2Called = true
			return true, "queue"
		},
	}

	handler := NewHandler(db, checker1, checker2)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	if checker2Called {
		t.Error("checker2 was called when it should have been skipped due to early exit")
	}
}

// TestNewHandler_CheckerWithoutMessage tests checker that returns empty message
func TestNewHandler_CheckerWithoutMessage(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	// Checker returns empty message
	checker := &mockChecker{
		checkFunc: func(ctx context.Context) (bool, string) {
			return true, ""
		},
	}

	handler := NewHandler(db, checker)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	// Should contain checker_0 for unnamed checker
	body := w.Body.String()
	if !strings.Contains(body, "checker_0") {
		t.Errorf("body missing 'checker_0' field: %s", body)
	}
}

// TestNewHandler_Timeout tests context timeout handling
func TestNewHandler_Timeout(t *testing.T) {
	// Create a database that takes longer than the timeout
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
				return nil
			}
		},
	}

	handler := NewHandler(db)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(w, req)
	duration := time.Since(start)

	// Should timeout around 5 seconds (the handler's timeout)
	if duration > 6*time.Second {
		t.Errorf("handler took too long: %v, expected around 5s", duration)
	}

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

// TestNewHandler_ConcurrentRequests tests thread safety
func TestNewHandler_ConcurrentRequests(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			mu.Lock()
			callCount++
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			return nil
		},
	}

	handler := NewHandler(db)

	const numRequests = 10
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
			}
		}()
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if callCount != numRequests {
		t.Errorf("call count = %d, want %d", callCount, numRequests)
	}
}

// TestNewHandler_HTTPMethods tests various HTTP methods
func TestNewHandler_HTTPMethods(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	handler := NewHandler(db)

	methods := []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/health", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			// All methods should be accepted
			if w.Code != http.StatusOK {
				t.Errorf("method %s: status code = %d, want %d", method, w.Code, http.StatusOK)
			}
		})
	}
}

// TestNewHandler_DatabasePanicRecovery tests panic in database ping
func TestNewHandler_DatabasePanicRecovery(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			panic("database panic")
		},
	}

	handler := NewHandler(db)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Should panic (not recovered by handler)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, but none occurred")
		}
	}()

	handler.ServeHTTP(w, req)
}

// TestNewHandler_CheckerPanic tests panic in checker
func TestNewHandler_CheckerPanic(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	checker := &mockChecker{
		checkFunc: func(ctx context.Context) (bool, string) {
			panic("checker panic")
		},
	}

	handler := NewHandler(db, checker)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Should panic (not recovered by handler)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, but none occurred")
		}
	}()

	handler.ServeHTTP(w, req)
}

// TestNewHandler_ContextCancellation tests context cancellation
func TestNewHandler_ContextCancellation(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return nil
			}
		},
	}

	handler := NewHandler(db)

	// Create request with pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := httptest.NewRequest("GET", "/health", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should return unhealthy due to cancelled context
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

// TestNewHandler_EmptyCheckersList tests with empty checkers list
func TestNewHandler_EmptyCheckersList(t *testing.T) {
	db := &mockPinger{
		pingFunc: func(ctx context.Context) error {
			return nil
		},
	}

	handler := NewHandler(db) // No checkers

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "healthy") {
		t.Errorf("body missing 'healthy' status: %s", body)
	}
	if !strings.Contains(body, "connected") {
		t.Errorf("body missing 'connected' status: %s", body)
	}
}
