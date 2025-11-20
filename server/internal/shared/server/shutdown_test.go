package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

// mockShutdownHandler implements the ShutdownHandler interface for testing
type mockShutdownHandler struct {
	shutdownFunc func(ctx context.Context) error
	callCount    int
	mu           sync.Mutex
}

func (m *mockShutdownHandler) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()

	if m.shutdownFunc != nil {
		return m.shutdownFunc(ctx)
	}
	return nil
}

func (m *mockShutdownHandler) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// TestGracefulShutdown_SIGINT tests shutdown on SIGINT
func TestGracefulShutdown_SIGINT(t *testing.T) {
	// Create a test HTTP server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Start()
	defer srv.Close()

	// Create HTTP server from test server
	httpServer := &http.Server{
		Addr: srv.Listener.Addr().String(),
	}

	// Create mock shutdown handler
	mockHandler := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			return nil
		},
	}

	// Run GracefulShutdown in goroutine
	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer, mockHandler)
		done <- true
	}()

	// Give the goroutine time to set up signal handlers
	time.Sleep(100 * time.Millisecond)

	// Send SIGINT
	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)

	// Wait for shutdown to complete (with timeout)
	select {
	case <-done:
		// Shutdown completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete within timeout")
	}

	// Verify mock handler was called
	if mockHandler.getCallCount() != 1 {
		t.Errorf("shutdown handler called %d times, want 1", mockHandler.getCallCount())
	}
}

// TestGracefulShutdown_SIGTERM tests shutdown on SIGTERM
func TestGracefulShutdown_SIGTERM(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Start()
	defer srv.Close()

	httpServer := &http.Server{
		Addr: srv.Listener.Addr().String(),
	}

	mockHandler := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			return nil
		},
	}

	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer, mockHandler)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	// Send SIGTERM
	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGTERM)

	select {
	case <-done:
		// Shutdown completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete within timeout")
	}

	if mockHandler.getCallCount() != 1 {
		t.Errorf("shutdown handler called %d times, want 1", mockHandler.getCallCount())
	}
}

// TestGracefulShutdown_MultipleHandlers tests multiple shutdown handlers
func TestGracefulShutdown_MultipleHandlers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Start()
	defer srv.Close()

	httpServer := &http.Server{
		Addr: srv.Listener.Addr().String(),
	}

	// Create multiple mock handlers
	handler1 := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			return nil
		},
	}

	handler2 := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			return nil
		},
	}

	handler3 := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			return nil
		},
	}

	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer, handler1, handler2, handler3)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Shutdown completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete within timeout")
	}

	// Verify all handlers were called
	if handler1.getCallCount() != 1 {
		t.Errorf("handler1 called %d times, want 1", handler1.getCallCount())
	}
	if handler2.getCallCount() != 1 {
		t.Errorf("handler2 called %d times, want 1", handler2.getCallCount())
	}
	if handler3.getCallCount() != 1 {
		t.Errorf("handler3 called %d times, want 1", handler3.getCallCount())
	}
}

// TestGracefulShutdown_HandlerError tests error handling in shutdown handlers
func TestGracefulShutdown_HandlerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Start()
	defer srv.Close()

	httpServer := &http.Server{
		Addr: srv.Listener.Addr().String(),
	}

	// Create handler that returns error
	errorHandler := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			return errors.New("shutdown error")
		},
	}

	// Create successful handler after error handler
	successHandler := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			return nil
		},
	}

	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer, errorHandler, successHandler)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Shutdown completed (errors are logged but don't stop shutdown)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete within timeout")
	}

	// Both handlers should have been called despite the error
	if errorHandler.getCallCount() != 1 {
		t.Errorf("error handler called %d times, want 1", errorHandler.getCallCount())
	}
	if successHandler.getCallCount() != 1 {
		t.Errorf("success handler called %d times, want 1", successHandler.getCallCount())
	}
}

// TestGracefulShutdown_NoHandlers tests shutdown with no custom handlers
func TestGracefulShutdown_NoHandlers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Start()
	defer srv.Close()

	httpServer := &http.Server{
		Addr: srv.Listener.Addr().String(),
	}

	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer) // No handlers
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Shutdown completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete within timeout")
	}
}

// TestGracefulShutdown_HandlerTimeout tests handler that takes longer than timeout
func TestGracefulShutdown_HandlerTimeout(t *testing.T) {
	t.Skip("Skipping timeout test as it takes >30 seconds")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Start()
	defer srv.Close()

	httpServer := &http.Server{
		Addr: srv.Listener.Addr().String(),
	}

	// Create handler that takes longer than 30s timeout
	slowHandler := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(35 * time.Second):
				return nil
			}
		},
	}

	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer, slowHandler)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)

	// Should complete within 31 seconds (30s timeout + buffer)
	select {
	case <-done:
		// Shutdown completed (with context timeout)
	case <-time.After(35 * time.Second):
		t.Fatal("shutdown did not complete even after extended timeout")
	}
}

// TestGracefulShutdown_HandlerContextAware tests handler that respects context
func TestGracefulShutdown_HandlerContextAware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Start()
	defer srv.Close()

	httpServer := &http.Server{
		Addr: srv.Listener.Addr().String(),
	}

	contextRespected := false
	contextAwareHandler := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			// Check that context has a deadline
			if _, ok := ctx.Deadline(); ok {
				contextRespected = true
			}
			return nil
		},
	}

	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer, contextAwareHandler)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Shutdown completed
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete within timeout")
	}

	if !contextRespected {
		t.Error("shutdown handler did not receive context with deadline")
	}
}

// TestGracefulShutdown_ServerShutdownError tests error during server shutdown
func TestGracefulShutdown_ServerShutdownError(t *testing.T) {
	// Create a server that will error on Shutdown
	httpServer := &http.Server{
		Addr: "invalid:99999", // Invalid address
	}

	mockHandler := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			return nil
		},
	}

	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer, mockHandler)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Shutdown completed (error is logged)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete within timeout")
	}

	// Handler should still be called even if server shutdown fails
	if mockHandler.getCallCount() != 1 {
		t.Errorf("handler called %d times, want 1", mockHandler.getCallCount())
	}
}

// TestShutdownHandler_Interface tests that types implement ShutdownHandler
func TestShutdownHandler_Interface(t *testing.T) {
	var _ ShutdownHandler = (*mockShutdownHandler)(nil)

	// Verify interface signature
	handler := &mockShutdownHandler{}
	ctx := context.Background()
	err := handler.Shutdown(ctx)

	if err != nil {
		t.Errorf("Shutdown() error = %v, want nil", err)
	}
}

// TestGracefulShutdown_ConcurrentSignals tests behavior with multiple signals
func TestGracefulShutdown_ConcurrentSignals(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Start()
	defer srv.Close()

	httpServer := &http.Server{
		Addr: srv.Listener.Addr().String(),
	}

	mockHandler := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			time.Sleep(100 * time.Millisecond)
			return nil
		},
	}

	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer, mockHandler)
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)

	// Send multiple signals rapidly
	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)
	proc.Signal(syscall.SIGTERM)
	proc.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Shutdown completed (should only process first signal)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete within timeout")
	}

	// Handler should only be called once
	if mockHandler.getCallCount() != 1 {
		t.Errorf("handler called %d times, want 1", mockHandler.getCallCount())
	}
}

// TestGracefulShutdown_HandlerOrder tests that handlers are called in order
func TestGracefulShutdown_HandlerOrder(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Start()
	defer srv.Close()

	httpServer := &http.Server{
		Addr: srv.Listener.Addr().String(),
	}

	order := []int{}
	var mu sync.Mutex

	handler1 := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			mu.Lock()
			order = append(order, 1)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			return nil
		},
	}

	handler2 := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			mu.Lock()
			order = append(order, 2)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			return nil
		},
	}

	handler3 := &mockShutdownHandler{
		shutdownFunc: func(ctx context.Context) error {
			mu.Lock()
			order = append(order, 3)
			mu.Unlock()
			return nil
		},
	}

	done := make(chan bool)
	go func() {
		GracefulShutdown(httpServer, handler1, handler2, handler3)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Shutdown completed
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete within timeout")
	}

	// Verify handlers were called in order
	mu.Lock()
	defer mu.Unlock()

	if len(order) != 3 {
		t.Fatalf("expected 3 handlers called, got %d", len(order))
	}

	for i, expected := range []int{1, 2, 3} {
		if order[i] != expected {
			t.Errorf("handler at position %d = %d, want %d", i, order[i], expected)
		}
	}
}
