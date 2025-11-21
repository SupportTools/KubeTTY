# Testing Guide

This document defines testing requirements and patterns for KubeTTY development.

## Testing Philosophy

1. **Test as you code** - Write tests alongside implementation, not after
2. **Test behavior, not implementation** - Focus on what the code does, not how
3. **Cover error paths** - Every error condition needs a test
4. **Meaningful coverage** - Aim for quality tests over coverage percentage

## Go Testing

### Running Tests

```bash
# Run all tests
cd server && go test -v ./...

# Run with coverage
go test -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific package
go test -v ./internal/auth/...

# Run specific test
go test -v -run TestHandleSessionLogs ./...
```

### Test File Structure

Tests go in `*_test.go` files alongside the code:

```
server/
├── cmd/
│   ├── gateway/main.go       # Gateway binary
│   └── project/main.go       # Project binary
├── internal/
│   ├── handlers/
│   │   └── auth/
│   │       ├── login.go
│   │       └── login_test.go
│   ├── sessions/
│   │   ├── pgx_store.go
│   │   └── pgx_store_test.go
│   └── shared/
│       └── errors/
│           ├── errors.go
│           └── errors_test.go
```

### Table-Driven Tests

Use table-driven tests for comprehensive coverage:

```go
func TestValidateUsername(t *testing.T) {
    tests := []struct {
        name     string
        username string
        wantErr  bool
    }{
        {"valid alphanumeric", "user123", false},
        {"valid with underscore", "user_name", false},
        {"valid with dash", "user-name", false},
        {"empty", "", true},
        {"too long", strings.Repeat("a", 65), true},
        {"invalid chars", "user@name", true},
        {"spaces", "user name", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validateUsername(tt.username)
            if (err != nil) != tt.wantErr {
                t.Errorf("validateUsername(%q) error = %v, wantErr %v", tt.username, err, tt.wantErr)
            }
        })
    }
}
```

### HTTP Handler Tests

Use `httptest` for handler testing:

```go
func TestHandleSessionLogs(t *testing.T) {
    // Setup
    store := newMockStore()
    srv := &server{store: store}

    // Create request
    req := httptest.NewRequest("GET", "/session/logs?session_id=test-uuid", nil)
    w := httptest.NewRecorder()

    // Execute
    srv.handleSessionLogs(w, req)

    // Assert
    if w.Code != http.StatusOK {
        t.Errorf("expected status 200, got %d", w.Code)
    }

    var response map[string]any
    if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
        t.Fatalf("invalid JSON response: %v", err)
    }

    if _, ok := response["logs"]; !ok {
        t.Error("response missing 'logs' field")
    }
}
```

### Mock Implementations

Create mock implementations for testing:

```go
type mockStore struct {
    sessions map[string]*sessions.Session
    err      error
}

func newMockStore() *mockStore {
    return &mockStore{
        sessions: make(map[string]*sessions.Session),
    }
}

func (m *mockStore) GetSession(ctx context.Context, id string) (*sessions.Session, error) {
    if m.err != nil {
        return nil, m.err
    }
    sess, ok := m.sessions[id]
    if !ok {
        return nil, sessions.ErrNotFound
    }
    return sess, nil
}
```

### WebSocket Tests

Test WebSocket connections:

```go
func TestWebSocketConnection(t *testing.T) {
    // Create test server
    srv := &server{...}
    ts := httptest.NewServer(http.HandlerFunc(srv.handleWebsocket))
    defer ts.Close()

    // Connect WebSocket
    wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
    conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
    if err != nil {
        t.Fatalf("dial error: %v", err)
    }
    defer conn.Close()

    // Send message
    if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
        t.Fatalf("write error: %v", err)
    }

    // Read response
    _, data, err := conn.ReadMessage()
    if err != nil {
        t.Fatalf("read error: %v", err)
    }

    var msg map[string]string
    if err := json.Unmarshal(data, &msg); err != nil {
        t.Fatalf("unmarshal error: %v", err)
    }

    if msg["type"] != "pong" {
        t.Errorf("expected pong, got %s", msg["type"])
    }
}
```

### Test Coverage Requirements

Every function should have tests for:

1. **Happy path** - Normal successful operation
2. **Edge cases** - Boundary values, empty inputs
3. **Error cases** - Invalid inputs, failure conditions

Example:

```go
func TestCreateSession(t *testing.T) {
    t.Run("success", func(t *testing.T) {
        // Test successful creation
    })

    t.Run("empty session ID", func(t *testing.T) {
        // Test validation error
    })

    t.Run("database error", func(t *testing.T) {
        // Test database failure handling
    })

    t.Run("duplicate session", func(t *testing.T) {
        // Test conflict handling
    })
}
```

## Frontend Testing

### Test Framework

KubeTTY uses Vitest with React Testing Library:

```bash
# Run tests
npm --prefix web run test

# Run with coverage
npm --prefix web run test -- --coverage

# Watch mode
npm --prefix web run test -- --watch
```

### Component Tests

Test component behavior:

```typescript
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import Login from './Login';

describe('Login', () => {
  it('renders login form', () => {
    render(<Login />);
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /login/i })).toBeInTheDocument();
  });

  it('submits form with credentials', async () => {
    const mockLogin = vi.fn();
    render(<Login onLogin={mockLogin} />);

    fireEvent.change(screen.getByLabelText(/username/i), {
      target: { value: 'testuser' }
    });
    fireEvent.change(screen.getByLabelText(/password/i), {
      target: { value: 'password123' }
    });
    fireEvent.click(screen.getByRole('button', { name: /login/i }));

    expect(mockLogin).toHaveBeenCalledWith('testuser', 'password123');
  });

  it('shows error on invalid credentials', async () => {
    // Test error display
  });
});
```

### Context Tests

Test React context providers:

```typescript
import { renderHook, act } from '@testing-library/react';
import { AuthProvider, useAuth } from './AuthContext';

describe('AuthContext', () => {
  it('provides authentication state', () => {
    const wrapper = ({ children }) => <AuthProvider>{children}</AuthProvider>;
    const { result } = renderHook(() => useAuth(), { wrapper });

    expect(result.current.authState).toBe('checking');
  });

  it('handles login', async () => {
    const wrapper = ({ children }) => <AuthProvider>{children}</AuthProvider>;
    const { result } = renderHook(() => useAuth(), { wrapper });

    await act(async () => {
      await result.current.login('user', 'pass');
    });

    expect(result.current.authState).toBe('authenticated');
  });
});
```

### Mocking Fetch

Mock API calls in tests:

```typescript
import { vi } from 'vitest';

beforeEach(() => {
  global.fetch = vi.fn();
});

it('fetches data on mount', async () => {
  (global.fetch as any).mockResolvedValueOnce({
    ok: true,
    json: async () => ({ data: 'test' })
  });

  render(<Component />);

  await waitFor(() => {
    expect(global.fetch).toHaveBeenCalledWith('/api/data');
  });
});
```

## Integration Testing

### Database Tests

Test database operations against real PostgreSQL:

```go
func TestPGXStoreIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    // Use test database
    connString := os.Getenv("TEST_DATABASE_URL")
    if connString == "" {
        t.Skip("TEST_DATABASE_URL not set")
    }

    ctx := context.Background()
    store, err := sessions.NewPGXStore(ctx, connString)
    if err != nil {
        t.Fatalf("connect: %v", err)
    }
    defer store.Close()

    // Test operations
    session := &sessions.Session{
        SessionID:    uuid.NewString(),
        DeploymentID: "test-deployment",
        ShellPID:     12345,
    }

    if err := store.UpsertSession(ctx, *session); err != nil {
        t.Fatalf("upsert: %v", err)
    }

    retrieved, err := store.GetSession(ctx, session.SessionID)
    if err != nil {
        t.Fatalf("get: %v", err)
    }

    if retrieved.SessionID != session.SessionID {
        t.Errorf("session ID mismatch")
    }
}
```

### API Integration Tests

Test full API flow:

```go
func TestAPIFlow(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    // Start test server
    srv := setupTestServer(t)
    ts := httptest.NewServer(srv.handler())
    defer ts.Close()

    // Test auth flow
    loginResp := doRequest(t, ts, "POST", "/api/auth/login", map[string]string{
        "username": "testuser",
        "password": "testpass",
    })
    if loginResp.StatusCode != http.StatusOK {
        t.Fatalf("login failed: %d", loginResp.StatusCode)
    }

    // Extract token
    var loginData map[string]any
    json.NewDecoder(loginResp.Body).Decode(&loginData)
    token := loginData["accessToken"].(string)

    // Test authenticated endpoint
    req, _ := http.NewRequest("GET", ts.URL+"/api/auth/me", nil)
    req.Header.Set("Authorization", "Bearer "+token)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("request error: %v", err)
    }
    if resp.StatusCode != http.StatusOK {
        t.Errorf("expected 200, got %d", resp.StatusCode)
    }
}
```

## Test Data Management

### Test Fixtures

Create reusable test data:

```go
var testSession = sessions.Session{
    SessionID:    "test-session-001",
    DeploymentID: "test-deployment",
    ShellPID:     12345,
    CreatedAt:    time.Now(),
    UpdatedAt:    time.Now(),
}

func withTestSession(store *mockStore) *mockStore {
    store.sessions[testSession.SessionID] = &testSession
    return store
}
```

### Cleanup

Always clean up test resources:

```go
func TestWithDatabase(t *testing.T) {
    // Setup
    store := setupTestStore(t)
    t.Cleanup(func() {
        store.Close()
        cleanupTestData(t)
    })

    // Tests
}
```

## Continuous Integration

### Pre-commit Checks

Run before committing:

```bash
# Go
go build ./...
go test ./...
go vet ./...

# Web
npm --prefix web run build
npm --prefix web run lint
npm --prefix web run test
```

### CI Pipeline Requirements

All tests must pass:
- Go unit tests
- Go integration tests (if enabled)
- Frontend tests
- Linting checks
- Build verification

## Test Checklist

Before marking a task complete:

- [ ] Unit tests for all new functions
- [ ] Table-driven tests for validation logic
- [ ] Handler tests with httptest
- [ ] Mock implementations for dependencies
- [ ] Error path coverage
- [ ] Integration tests if database/API involved
- [ ] Frontend component tests
- [ ] All tests passing
- [ ] No skipped tests without reason
