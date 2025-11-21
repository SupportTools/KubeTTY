package health_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/supporttools/KubeTTY/server/internal/shared/health"
)

// mockDB simulates a database with controllable ping behavior.
type mockDB struct {
	shouldFail bool
}

func (m *mockDB) Ping(ctx context.Context) error {
	if m.shouldFail {
		return fmt.Errorf("database connection failed")
	}
	return nil
}

// ExampleNewHandler demonstrates creating a health check HTTP handler.
func ExampleNewHandler() {
	// Create a mock database that's healthy
	db := &mockDB{shouldFail: false}

	// Create health check handler
	handler := health.NewHandler(db)

	// Create test request
	req := httptest.NewRequest("GET", "/api/healthz", nil)
	w := httptest.NewRecorder()

	// Execute health check
	handler(w, req)

	// Check response
	fmt.Printf("Status: %d\n", w.Code)
	fmt.Printf("Healthy: %v\n", w.Code == http.StatusOK)
	// Output:
	// Status: 200
	// Healthy: true
}

// ExampleChecker demonstrates implementing a custom health checker.
func ExampleChecker() {
	// Define a simple custom checker
	type customChecker struct {
		componentName string
	}

	checkFunc := func(ctx context.Context) (bool, string) {
		// Perform custom health check logic here
		return true, "cache:connected"
	}

	// Simulate check execution
	healthy, message := checkFunc(context.Background())
	fmt.Printf("Healthy: %v\n", healthy)
	fmt.Printf("Message: %s\n", message)
	// Output:
	// Healthy: true
	// Message: cache:connected
}

// ExampleNewComponentChecker demonstrates creating a component status checker.
func ExampleNewComponentChecker() {
	// Create a component checker that reports session count
	sessionCount := 42
	checker := health.NewComponentChecker("sessions", func() string {
		return fmt.Sprintf("%d_active", sessionCount)
	})

	// Execute the check
	healthy, message := checker.Check(context.Background())

	fmt.Printf("Healthy: %v\n", healthy)
	fmt.Printf("Status: %s\n", message)
	// Output:
	// Healthy: true
	// Status: sessions:42_active
}

// ExampleNewPTYChecker demonstrates creating a PTY process health checker.
func ExampleNewPTYChecker() {
	// Simulate PTY state with mutex protection
	var mu sync.RWMutex
	ptyAlive := true

	checker := health.NewPTYChecker(&mu, func() bool {
		return ptyAlive
	})

	// Check when PTY is alive
	healthy, message := checker.Check(context.Background())
	fmt.Printf("PTY Alive - Healthy: %v, Message: %s\n", healthy, message)

	// Simulate PTY stopping
	mu.Lock()
	ptyAlive = false
	mu.Unlock()

	healthy, message = checker.Check(context.Background())
	fmt.Printf("PTY Not Started - Healthy: %v, Message: %s\n", healthy, message)
	// Output:
	// PTY Alive - Healthy: true, Message: pty:alive
	// PTY Not Started - Healthy: true, Message: pty:not_started
}
