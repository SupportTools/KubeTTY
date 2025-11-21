package errors_test

import (
	"fmt"
	"net/http/httptest"

	"github.com/supporttools/KubeTTY/server/internal/shared/errors"
)

// ExampleErrorResponse demonstrates creating a standardized error response.
func ExampleErrorResponse() {
	// Create a 404 Not Found error
	err := errors.NotFound("session not found", "session UUID: abc123")

	fmt.Printf("Status: %d\n", err.Status)
	fmt.Printf("Error Code: %s\n", err.Error)
	fmt.Printf("Message: %s\n", err.Message)
	fmt.Printf("Details: %s\n", err.Details)
	// Output:
	// Status: 404
	// Error Code: not_found
	// Message: session not found
	// Details: session UUID: abc123
}

// ExampleWriteError demonstrates writing JSON error responses to HTTP clients.
func ExampleWriteError() {
	// Create a 400 Bad Request error
	err := errors.BadRequest("invalid PTY dimensions", "cols must be between 1 and 500")

	// Write to response writer
	w := httptest.NewRecorder()
	if writeErr := errors.WriteError(w, err); writeErr != nil {
		fmt.Printf("Failed to write error: %v\n", writeErr)
		return
	}

	// Check response
	fmt.Printf("Status: %d\n", w.Code)
	fmt.Printf("Content-Type: %s\n", w.Header().Get("Content-Type"))
	fmt.Printf("Body: %s", w.Body.String())
	// Output:
	// Status: 400
	// Content-Type: application/json
	// Body: {"status":400,"error":"bad_request","message":"invalid PTY dimensions","details":"cols must be between 1 and 500"}
}

// ExampleNotFound demonstrates common error types used throughout KubeTTY.
func ExampleNotFound() {
	// 404 Not Found - resource doesn't exist
	notFoundErr := errors.NotFound("session not found", "")
	fmt.Printf("404: %s\n", notFoundErr.Message)

	// 401 Unauthorized - authentication required
	unauthorizedErr := errors.Unauthorized("authentication required", "missing JWT token")
	fmt.Printf("401: %s\n", unauthorizedErr.Message)

	// 409 Conflict - resource state conflict
	conflictErr := errors.Conflict("session already attached", "only one client allowed per session")
	fmt.Printf("409: %s\n", conflictErr.Message)

	// 429 Rate Limit - too many requests
	rateLimitErr := errors.RateLimitExceeded("maximum tabs per client exceeded", "limit: 1 tab")
	fmt.Printf("429: %s\n", rateLimitErr.Message)

	// Output:
	// 404: session not found
	// 401: authentication required
	// 409: session already attached
	// 429: maximum tabs per client exceeded
}
