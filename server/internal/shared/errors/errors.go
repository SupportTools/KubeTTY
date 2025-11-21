// Package errors provides standardized HTTP API error responses for KubeTTY.
//
// This package implements the error handling patterns defined in docs/development/error-handling-guide.md,
// ensuring consistent JSON error responses across all KubeTTY API endpoints. All error responses follow
// a uniform structure with status code, machine-readable error code, human-readable message, and optional details.
//
// Error response format:
//
//	{
//	  "status": 404,
//	  "error": "not_found",
//	  "message": "session not found",
//	  "details": "optional context"
//	}
//
// Available error constructors:
//   - BadRequest (400): Invalid input, malformed JSON, validation failures
//   - Unauthorized (401): Missing or invalid authentication tokens
//   - Forbidden (403): Valid auth but insufficient permissions
//   - NotFound (404): Requested resource does not exist
//   - Conflict (409): Resource state conflicts (duplicates, locks)
//   - ValidationError (422): Semantically incorrect data
//   - RateLimitExceeded (429): Client exceeded rate limits
//   - InternalServerError (500): Unexpected server errors (never expose internals)
//   - ServiceUnavailable (503): Database or dependency unavailable
//
// All error responses should be written using WriteError() to ensure consistent
// JSON formatting and proper Content-Type headers.
package errors

import "net/http"

// ErrorResponse represents a standardized API error response.
// It follows the format specified in docs/development/error-handling-guide.md.
type ErrorResponse struct {
	Status  int    `json:"status"`            // HTTP status code
	Error   string `json:"error"`             // Machine-readable error code
	Message string `json:"message"`           // Human-readable description
	Details string `json:"details,omitempty"` // Optional additional context
}

// BadRequest creates a 400 Bad Request error response.
// Use for invalid input, malformed JSON, or validation failures.
//
// Example:
//
//	BadRequest("invalid session UUID", "")
func BadRequest(message, details string) ErrorResponse {
	return ErrorResponse{
		Status:  http.StatusBadRequest,
		Error:   CodeBadRequest,
		Message: message,
		Details: details,
	}
}

// Unauthorized creates a 401 Unauthorized error response.
// Use for missing or invalid authentication tokens.
//
// Example:
//
//	Unauthorized("authentication required", "")
func Unauthorized(message, details string) ErrorResponse {
	return ErrorResponse{
		Status:  http.StatusUnauthorized,
		Error:   CodeUnauthorized,
		Message: message,
		Details: details,
	}
}

// Forbidden creates a 403 Forbidden error response.
// Use when authentication is valid but permissions are insufficient.
//
// Example:
//
//	Forbidden("access denied", "insufficient permissions")
func Forbidden(message, details string) ErrorResponse {
	return ErrorResponse{
		Status:  http.StatusForbidden,
		Error:   CodeForbidden,
		Message: message,
		Details: details,
	}
}

// NotFound creates a 404 Not Found error response.
// Use when the requested resource does not exist.
//
// Example:
//
//	NotFound("session not found", "")
func NotFound(message, details string) ErrorResponse {
	return ErrorResponse{
		Status:  http.StatusNotFound,
		Error:   CodeNotFound,
		Message: message,
		Details: details,
	}
}

// Conflict creates a 409 Conflict error response.
// Use for resource state conflicts like duplicates or locked resources.
//
// Example:
//
//	Conflict("session already attached", "only one client allowed")
func Conflict(message, details string) ErrorResponse {
	return ErrorResponse{
		Status:  http.StatusConflict,
		Error:   CodeConflict,
		Message: message,
		Details: details,
	}
}

// ValidationError creates a 422 Unprocessable Entity error response.
// Use when JSON is valid but data is semantically incorrect.
//
// Example:
//
//	ValidationError("invalid PTY dimensions", "cols must be between 1 and 500")
func ValidationError(message, details string) ErrorResponse {
	return ErrorResponse{
		Status:  http.StatusUnprocessableEntity,
		Error:   CodeValidationError,
		Message: message,
		Details: details,
	}
}

// RateLimitExceeded creates a 429 Too Many Requests error response.
// Use when client has exceeded rate limits.
//
// Example:
//
//	RateLimitExceeded("too many requests", "limit: 100/minute")
func RateLimitExceeded(message, details string) ErrorResponse {
	return ErrorResponse{
		Status:  http.StatusTooManyRequests,
		Error:   CodeRateLimitExceeded,
		Message: message,
		Details: details,
	}
}

// InternalServerError creates a 500 Internal Server Error response.
// Use for unexpected server errors. NEVER expose internal details to clients.
//
// Example:
//
//	InternalServerError("internal error", "")
func InternalServerError(message, details string) ErrorResponse {
	return ErrorResponse{
		Status:  http.StatusInternalServerError,
		Error:   CodeInternalServerError,
		Message: message,
		Details: details,
	}
}

// ServiceUnavailable creates a 503 Service Unavailable error response.
// Use when database or external dependencies are unavailable.
//
// Example:
//
//	ServiceUnavailable("database unavailable", "")
func ServiceUnavailable(message, details string) ErrorResponse {
	return ErrorResponse{
		Status:  http.StatusServiceUnavailable,
		Error:   CodeServiceUnavailable,
		Message: message,
		Details: details,
	}
}
