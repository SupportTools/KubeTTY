package util_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// ExampleWriteJSON demonstrates how to write JSON responses with proper error handling.
func ExampleWriteJSON() {
	// Create a test response writer
	w := httptest.NewRecorder()

	// Write a JSON response
	payload := map[string]string{"status": "ok", "message": "Server is healthy"}
	if err := util.WriteJSON(w, http.StatusOK, payload); err != nil {
		fmt.Printf("error writing JSON: %v\n", err)
		return
	}

	// Check the response
	fmt.Printf("Status: %d\n", w.Code)
	fmt.Printf("Content-Type: %s\n", w.Header().Get("Content-Type"))
	fmt.Printf("Body: %s", w.Body.String())
	// Output:
	// Status: 200
	// Content-Type: application/json
	// Body: {"message":"Server is healthy","status":"ok"}
}

// ExampleClientIPFromRequest demonstrates client IP extraction with proxy support.
func ExampleClientIPFromRequest() {
	// Example 1: Direct connection (no proxy)
	req1 := httptest.NewRequest("GET", "/", nil)
	req1.RemoteAddr = "192.168.1.100:54321"
	fmt.Printf("Direct connection: %s\n", util.ClientIPFromRequest(req1))

	// Example 2: Behind proxy with X-Forwarded-For header
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("X-Forwarded-For", "203.0.113.42, 192.168.1.1")
	req2.RemoteAddr = "192.168.1.1:54321"
	fmt.Printf("Proxied connection: %s\n", util.ClientIPFromRequest(req2))

	// Example 3: Nil request handling
	fmt.Printf("Nil request: %s\n", util.ClientIPFromRequest(nil))
	// Output:
	// Direct connection: 192.168.1.100
	// Proxied connection: 203.0.113.42
	// Nil request:
}

// ExampleWebSocketScheme demonstrates WebSocket scheme selection based on TLS configuration.
func ExampleWebSocketScheme() {
	// Example 1: HTTP request (no TLS)
	req1 := httptest.NewRequest("GET", "http://example.com/ws", nil)
	fmt.Printf("HTTP request: %s\n", util.WebSocketScheme(req1))

	// Example 2: Request behind HTTPS proxy (X-Forwarded-Proto header)
	req2 := httptest.NewRequest("GET", "http://example.com/ws", nil)
	req2.Header.Set("X-Forwarded-Proto", "https")
	fmt.Printf("HTTPS proxy: %s\n", util.WebSocketScheme(req2))

	// Example 3: Direct HTTPS connection would have req.TLS != nil
	// (Cannot be easily demonstrated in example test without TLS setup)
	// In production: req.TLS != nil -> returns "wss"
	// Output:
	// HTTP request: ws
	// HTTPS proxy: wss
}
