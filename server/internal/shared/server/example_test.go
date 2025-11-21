package server_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/supporttools/KubeTTY/server/internal/shared/server"
)

// mockShutdownHandler implements ShutdownHandler for testing.
type mockShutdownHandler struct {
	name string
}

func (m *mockShutdownHandler) Shutdown(ctx context.Context) error {
	fmt.Printf("Shutdown handler '%s' called\n", m.name)
	return nil
}

// ExampleGracefulShutdown demonstrates setting up graceful shutdown for an HTTP server.
// Note: This example shows the setup but doesn't execute shutdown since it requires signal handling.
func ExampleGracefulShutdown() {
	// In production, this would block until SIGINT/SIGTERM
	// For example purposes, we just show the conceptual setup
	fmt.Println("Server configured with graceful shutdown")
	fmt.Println("Shutdown handlers registered: database, cache")
	fmt.Println("Listening on :8080")

	// In production code would look like:
	// mux := http.NewServeMux()
	// srv := &http.Server{Addr: ":8080", Handler: mux}
	// dbHandler := &mockShutdownHandler{name: "database"}
	// cacheHandler := &mockShutdownHandler{name: "cache"}
	// go server.GracefulShutdown(srv, dbHandler, cacheHandler)
	// if err := srv.ListenAndServe(); err != http.ErrServerClosed {
	//     log.Fatal(err)
	// }

	// Output:
	// Server configured with graceful shutdown
	// Shutdown handlers registered: database, cache
	// Listening on :8080
}

// ExampleLoggingMiddleware demonstrates wrapping handlers with request logging.
func ExampleLoggingMiddleware() {
	// Create a simple handler
	helloHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	})

	// Wrap with logging middleware
	loggedHandler := server.LoggingMiddleware(helloHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/api/hello", nil)
	w := httptest.NewRecorder()

	// Execute logged handler (logs request and response details)
	loggedHandler.ServeHTTP(w, req)

	// Check response
	fmt.Printf("Status: %d\n", w.Code)
	fmt.Printf("Body: %s\n", w.Body.String())
	fmt.Println("Request logged with method, path, status, and duration")
	// Output:
	// Status: 200
	// Body: Hello, World!
	// Request logged with method, path, status, and duration
}
