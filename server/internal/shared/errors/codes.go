package errors

// Error code constants for API responses.
// These provide machine-readable error identifiers following snake_case convention.
const (
	// CodeBadRequest indicates invalid input, malformed JSON, or validation failures.
	// HTTP Status: 400
	CodeBadRequest = "bad_request"

	// CodeUnauthorized indicates missing or invalid authentication token.
	// HTTP Status: 401
	CodeUnauthorized = "unauthorized"

	// CodeForbidden indicates valid authentication but insufficient permissions.
	// HTTP Status: 403
	CodeForbidden = "forbidden"

	// CodeNotFound indicates the requested resource does not exist.
	// HTTP Status: 404
	CodeNotFound = "not_found"

	// CodeConflict indicates a resource state conflict (e.g., duplicate, locked).
	// HTTP Status: 409
	CodeConflict = "conflict"

	// CodeValidationError indicates semantically invalid data (valid JSON, invalid semantics).
	// HTTP Status: 422
	CodeValidationError = "validation_error"

	// CodeRateLimitExceeded indicates too many requests from the client.
	// HTTP Status: 429
	CodeRateLimitExceeded = "rate_limit_exceeded"

	// CodeInternalServerError indicates an unexpected server error.
	// HTTP Status: 500
	CodeInternalServerError = "internal_server_error"

	// CodeServiceUnavailable indicates database or dependency is unavailable.
	// HTTP Status: 503
	CodeServiceUnavailable = "service_unavailable"
)
