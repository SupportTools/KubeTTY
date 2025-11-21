package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

// getMetrics returns the shared test metrics instance from prometheus_test.go
// to avoid duplicate Prometheus registration.
func getMetrics() *AppMetrics {
	return testMetrics
}

// ExampleNewAppMetrics demonstrates creating a Prometheus metrics collector.
// Note: In production, create this once at application startup.
func ExampleNewAppMetrics() {
	// Metrics collector is created and registers with Prometheus default registry
	// For demonstration, we use a pre-created instance to avoid registration conflicts
	m := getMetrics()
	fmt.Printf("Metrics collector type: %T\n", m)
	fmt.Println("Metrics registered: websocket, store, http")
	// Output:
	// Metrics collector type: *metrics.AppMetrics
	// Metrics registered: websocket, store, http
}

// ExampleAppMetrics_ObserveWSBytes demonstrates tracking WebSocket byte transmission.
func ExampleAppMetrics_ObserveWSBytes() {
	m := getMetrics()

	// Record received bytes
	m.ObserveWSBytes("rx", 1024)
	fmt.Println("Recorded 1024 bytes received")

	// Record transmitted bytes
	m.ObserveWSBytes("tx", 512)
	fmt.Println("Recorded 512 bytes transmitted")

	// Output:
	// Recorded 1024 bytes received
	// Recorded 512 bytes transmitted
}

// ExampleAppMetrics_ObserveStore demonstrates tracking database operation metrics.
func ExampleAppMetrics_ObserveStore() {
	m := getMetrics()

	// Simulate successful database operation
	duration := 15 * time.Millisecond

	m.ObserveStore("create_session", duration, nil)
	fmt.Println("Recorded successful store operation")

	// Simulate failed database operation
	m.ObserveStore("delete_session", duration, fmt.Errorf("connection timeout"))
	fmt.Println("Recorded failed store operation")

	// Output:
	// Recorded successful store operation
	// Recorded failed store operation
}

// ExampleAppMetrics_InstrumentHandler demonstrates HTTP request instrumentation.
func ExampleAppMetrics_InstrumentHandler() {
	m := getMetrics()

	// Create a simple handler
	helloHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	})

	// Wrap handler with metrics instrumentation
	instrumentedHandler := m.InstrumentHandler("/api/hello", helloHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/api/hello", nil)
	w := httptest.NewRecorder()

	// Execute instrumented handler
	instrumentedHandler.ServeHTTP(w, req)

	// Metrics are automatically recorded (duration + request count)
	fmt.Printf("Status: %d\n", w.Code)
	fmt.Printf("Body: %s\n", w.Body.String())
	fmt.Println("Metrics recorded: duration + request count")
	// Output:
	// Status: 200
	// Body: Hello, World!
	// Metrics recorded: duration + request count
}
