# API Handler Standards

This document defines standards for implementing HTTP handlers in KubeTTY.

## Handler Architecture

KubeTTY uses the standard `net/http` package with `http.ServeMux` for routing.

### Handler Signature

All handlers follow this pattern:

```go
func (s *server) handleResourceAction(w http.ResponseWriter, r *http.Request) {
    // 1. Extract parameters
    // 2. Validate input
    // 3. Perform operation
    // 4. Return response
}
```

### Route Registration

Routes are registered in `main()` using the mux:

```go
mux := http.NewServeMux()

// Public endpoints (no auth)
mux.Handle("/metrics", promhttp.Handler())
mux.Handle("/api/healthz", http.HandlerFunc(srv.handleHealthz))

// Protected endpoints (with auth)
if srv.authEnabled() {
    mux.Handle("/api/resource", srv.requireAuth(http.HandlerFunc(srv.handleResource)))
}
```

## Input Validation

### Request Body Parsing

```go
func (s *server) handleCreateResource(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Name string `json:"name"`
        Size int    `json:"size"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid JSON", http.StatusBadRequest)
        return
    }

    // Validate required fields
    req.Name = strings.TrimSpace(req.Name)
    if req.Name == "" {
        http.Error(w, "name is required", http.StatusBadRequest)
        return
    }

    // Validate limits
    if len(req.Name) > 64 {
        http.Error(w, "name must be 64 characters or less", http.StatusBadRequest)
        return
    }

    // Validate format
    if !validNameRegex.MatchString(req.Name) {
        http.Error(w, "name contains invalid characters", http.StatusBadRequest)
        return
    }

    // ... continue with operation
}
```

### URL Parameters

```go
func (s *server) handleGetResource(w http.ResponseWriter, r *http.Request) {
    // Extract from path (e.g., /api/resource/{id})
    id := strings.TrimPrefix(r.URL.Path, "/api/resource/")
    if id == "" {
        http.Error(w, "resource ID required", http.StatusBadRequest)
        return
    }

    // Validate UUID format
    if _, err := uuid.Parse(id); err != nil {
        http.Error(w, "invalid resource ID format", http.StatusBadRequest)
        return
    }

    // ... continue with operation
}
```

### Query Parameters

```go
func (s *server) handleListResources(w http.ResponseWriter, r *http.Request) {
    // Extract query parameters
    limitStr := r.URL.Query().Get("limit")
    limit := 100 // default

    if limitStr != "" {
        var err error
        limit, err = strconv.Atoi(limitStr)
        if err != nil || limit < 1 || limit > 1000 {
            http.Error(w, "limit must be between 1 and 1000", http.StatusBadRequest)
            return
        }
    }

    // ... continue with operation
}
```

## Response Handling

### JSON Responses

Use the helper function for consistent JSON responses:

```go
func writeJSON(w http.ResponseWriter, status int, data any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}

// Usage
writeJSON(w, http.StatusOK, map[string]any{
    "resource": resource,
    "message":  "Resource created successfully",
})
```

### Standard Response Formats

**Success (single resource):**
```json
{
    "resource": { ... }
}
```

**Success (list):**
```json
{
    "resources": [ ... ],
    "total": 42
}
```

**Error:**
```json
{
    "error": "error_type",
    "message": "Human-readable message"
}
```

## Authentication

### Requiring Authentication

Wrap handlers with `requireAuth`:

```go
mux.Handle("/api/protected", srv.requireAuth(http.HandlerFunc(srv.handleProtected)))
```

### Accessing Authenticated User

```go
func (s *server) handleProtected(w http.ResponseWriter, r *http.Request) {
    user := authUserFromContext(r.Context())
    if user == nil {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }

    // Use user.ID, user.Username
}
```

## Method Handling

Handle multiple HTTP methods in one handler:

```go
func (s *server) handleResource(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        s.handleGetResource(w, r)
    case http.MethodPost:
        s.handleCreateResource(w, r)
    case http.MethodDelete:
        s.handleDeleteResource(w, r)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}
```

Or check method explicitly:

```go
func (s *server) handleCreateResource(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    // ...
}
```

## WebSocket Handlers

WebSocket handlers use gorilla/websocket:

```go
func (s *server) handleWebsocket(w http.ResponseWriter, r *http.Request) {
    conn, err := s.upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("WebSocket upgrade error: %v", err)
        return
    }
    defer conn.Close()

    // Set connection parameters
    conn.SetReadLimit(maxMessageSize)
    conn.SetReadDeadline(time.Now().Add(pongWait))
    conn.SetPongHandler(func(string) error {
        conn.SetReadDeadline(time.Now().Add(pongWait))
        return nil
    })

    // Main read loop
    for {
        messageType, data, err := conn.ReadMessage()
        if err != nil {
            if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
                return
            }
            log.Printf("WebSocket read error: %v", err)
            return
        }

        // Process message
        switch messageType {
        case websocket.TextMessage:
            // Handle text message (JSON commands)
        case websocket.BinaryMessage:
            // Handle binary data (PTY input)
        }
    }
}
```

## Context Usage

Use context for cancellation and timeouts:

```go
func (s *server) handleLongOperation(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Set operation timeout
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    result, err := s.performOperation(ctx)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            http.Error(w, "operation timed out", http.StatusGatewayTimeout)
            return
        }
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }

    writeJSON(w, http.StatusOK, result)
}
```

## Concurrency Safety

### Protecting Shared State

```go
func (s *server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
    s.mu.RLock()
    pty := s.pty
    s.mu.RUnlock()

    // Now safe to use pty
    if pty == nil {
        writeJSON(w, http.StatusOK, map[string]any{"status": "not_initialized"})
        return
    }

    writeJSON(w, http.StatusOK, map[string]any{"status": "running"})
}
```

## Logging

Log significant operations:

```go
func (s *server) handleCreateResource(w http.ResponseWriter, r *http.Request) {
    // ... validation ...

    resource, err := s.store.Create(r.Context(), req)
    if err != nil {
        log.WithError(err).Error("failed to create resource")
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }

    log.WithField("resource_id", resource.ID).Info("resource created")
    writeJSON(w, http.StatusCreated, map[string]any{"resource": resource})
}
```

## Handler Checklist

Before completing a handler:

- [ ] Input validation for all parameters
- [ ] Appropriate HTTP status codes
- [ ] Consistent JSON response format
- [ ] Authentication check if needed
- [ ] Context timeout for long operations
- [ ] Proper error handling and logging
- [ ] Thread-safe access to shared state
- [ ] Unit tests for all code paths
