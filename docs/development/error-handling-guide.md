# Error Handling Guide

This document defines error handling patterns for KubeTTY server development.

## Core Principles

1. **Consistent Format** - All API errors follow a standard JSON structure
2. **Appropriate Status Codes** - Use semantically correct HTTP status codes
3. **Safe Error Messages** - Never expose internal details to clients
4. **Structured Logging** - Log errors with context using logrus

## Standard Error Response Format

```json
{
  "status": 400,
  "error": "bad_request",
  "message": "Human-readable description",
  "details": "Optional additional context"
}
```

## Using the Standardized Error Package

**Preferred Method:** Use the standardized error package for all API endpoints.

```go
import "github.com/supporttools/KubeTTY/server/internal/shared/errors"

// Bad Request (validation failure)
if len(req.Username) > maxUsernameLength {
    errors.WriteError(w, errors.BadRequest(
        fmt.Sprintf("username must be %d characters or less", maxUsernameLength),
        "",
    ))
    return
}

// Not Found
if errors.Is(err, sessions.ErrNotFound) {
    errors.WriteError(w, errors.NotFound("session not found", ""))
    return
}

// Conflict (state conflict)
if ps.hasClients() {
    errors.WriteError(w, errors.Conflict(
        "session already attached",
        "only one client allowed per session",
    ))
    return
}

// Internal Server Error (unexpected)
if err != nil {
    log.WithError(err).Error("unexpected database error")
    errors.WriteError(w, errors.InternalServerError("internal error", ""))
    return
}

// All available error types:
errors.BadRequest(message, details)          // 400
errors.Unauthorized(message, details)        // 401
errors.Forbidden(message, details)           // 403
errors.NotFound(message, details)            // 404
errors.Conflict(message, details)            // 409
errors.ValidationError(message, details)     // 422
errors.RateLimitExceeded(message, details)   // 429
errors.InternalServerError(message, details) // 500
errors.ServiceUnavailable(message, details)  // 503
```

**Benefits:**
- Consistent JSON format across all endpoints
- Type-safe error responses
- Automatic Content-Type header management
- Details field automatically omitted when empty
- Clear, documented error codes

## HTTP Status Code Reference

| Code | Error Type | When to Use |
|------|------------|-------------|
| 400 | Bad Request | Invalid input, malformed JSON, validation failures |
| 401 | Unauthorized | Missing or invalid authentication token |
| 403 | Forbidden | Valid auth but insufficient permissions |
| 404 | Not Found | Resource does not exist |
| 409 | Conflict | Resource state conflict (e.g., session already has client) |
| 422 | Unprocessable Entity | Valid JSON but semantically invalid |
| 429 | Too Many Requests | Rate limit exceeded |
| 500 | Internal Server Error | Unexpected server errors |
| 503 | Service Unavailable | Database or dependency unavailable |

## Go Error Patterns

### Handler Error Responses

```go
// Bad Request (validation failure)
if len(req.Username) > maxUsernameLength {
    http.Error(w, fmt.Sprintf("username must be %d characters or less", maxUsernameLength), http.StatusBadRequest)
    return
}

// Not Found
if errors.Is(err, sessions.ErrNotFound) {
    http.Error(w, "session not found", http.StatusNotFound)
    return
}

// Conflict (state conflict)
if ps.hasClients() {
    http.Error(w, "Session already has an active client connection", http.StatusConflict)
    return
}

// Internal Server Error (unexpected)
if err != nil {
    log.WithError(err).Error("unexpected database error")
    http.Error(w, "internal error", http.StatusInternalServerError)
    return
}
```

### JSON Error Responses

For API endpoints that return JSON:

```go
func writeErrorJSON(w http.ResponseWriter, status int, errorType, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(map[string]any{
        "status":  status,
        "error":   errorType,
        "message": message,
    })
}

// Usage
writeErrorJSON(w, http.StatusBadRequest, "validation_error", "username is required")
```

### Structured Logging

Always use logrus for error logging with context:

```go
import log "github.com/sirupsen/logrus"

// With error context
log.WithError(err).Error("failed to create PTY")

// With multiple fields
log.WithFields(log.Fields{
    "session_uuid": sessionUUID,
    "client_id":    clientID,
    "error":        err,
}).Error("WebSocket connection failed")

// Warn level for recoverable issues
log.WithField("delay_ms", delay).Warn("retrying database connection")
```

## Error Wrapping

Use error wrapping to preserve context:

```go
import "fmt"

// Wrap errors with context
if err := db.Ping(ctx); err != nil {
    return fmt.Errorf("database health check: %w", err)
}

// Check wrapped errors
if errors.Is(err, context.DeadlineExceeded) {
    // Handle timeout
}
```

## WebSocket Error Handling

WebSocket connections require special handling:

```go
// Connection errors
if err != nil {
    if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
        log.Printf("[WS %s] Client closed connection normally", connID)
    } else if errors.Is(err, io.EOF) {
        log.Printf("[WS %s] Connection EOF", connID)
    } else {
        log.Printf("[WS %s] Read error: %v", connID, err)
    }
    return
}

// Send error to client before closing
conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"session expired"}`))
conn.Close()
```

## Database Error Handling

```go
// Check for specific database errors
if err != nil {
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, ErrNotFound
    }
    return nil, fmt.Errorf("query sessions: %w", err)
}

// Transaction rollback on error
tx, err := pool.Begin(ctx)
if err != nil {
    return fmt.Errorf("begin transaction: %w", err)
}
defer tx.Rollback(ctx) // No-op if committed

// ... operations ...

if err := tx.Commit(ctx); err != nil {
    return fmt.Errorf("commit transaction: %w", err)
}
```

## Authentication Errors

Use standardized auth error types:

```go
var (
    errAuthMissingToken = errors.New("authentication token missing")
    errAuthDisabled     = errors.New("authentication disabled")
)

// In auth handler
switch {
case errors.Is(err, errAuthMissingToken):
    msg = "authentication required"
case errors.Is(err, auth.ErrTokenExpired):
    msg = "token expired"
case errors.Is(err, auth.ErrTokenMalformed):
    msg = "token malformed"
}
```

## Security Considerations

1. **Never expose stack traces** to clients
2. **Sanitize error messages** - don't include internal paths, IPs, or credentials
3. **Log sensitive errors** but return generic messages to clients
4. **Rate limit** error responses to prevent enumeration attacks

```go
// Bad - exposes internal details
http.Error(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)

// Good - safe error message
log.WithError(err).Error("database query failed")
http.Error(w, "internal error", http.StatusInternalServerError)
```

## Testing Error Conditions

Always test error paths:

```go
func TestHandlerValidationError(t *testing.T) {
    // Test invalid input
    req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":""}`))
    w := httptest.NewRecorder()

    handler.handleAuthLogin(w, req)

    if w.Code != http.StatusBadRequest {
        t.Errorf("expected 400, got %d", w.Code)
    }
}

func TestHandlerNotFound(t *testing.T) {
    // Test missing resource
    req := httptest.NewRequest("GET", "/api/sessions/nonexistent", nil)
    w := httptest.NewRecorder()

    handler.handleGetSession(w, req)

    if w.Code != http.StatusNotFound {
        t.Errorf("expected 404, got %d", w.Code)
    }
}
```

## Checklist

Before completing a handler, verify:

- [ ] All error paths return appropriate HTTP status codes
- [ ] Error messages are safe for client exposure
- [ ] Errors are logged with sufficient context
- [ ] Database/external errors are wrapped
- [ ] WebSocket errors are handled gracefully
- [ ] Authentication errors use standard types
- [ ] Error paths have test coverage
