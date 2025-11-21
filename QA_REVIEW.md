# KubeTTY QA Review: Design vs Implementation

**Review Date:** 2025-11-19
**Status:** 75% Complete
**Reviewer:** Devils Advocate QA

## Executive Summary

The KubeTTY codebase is substantially complete with most core features implemented. However, there are several gaps, missing features, incomplete implementations, and potential issues found when comparing the actual code against DESIGN.md, PLAN.md, and README.md specifications.

**Critical Finding:** Single-client enforcement is NOT implemented - multiple clients can connect to the same PTY simultaneously.

---

## Critical Findings

| Issue | Severity | Impact | Location |
|-------|----------|--------|----------|
| Single-client enforcement missing | CRITICAL | Multiple clients can connect to same PTY | `server/cmd/project/main.go` |
| Debug console.log in production | MEDIUM | Exposes internal behavior | `TerminalView.tsx`, `AuthContext.tsx`, `App.tsx` |
| Auth not enforced by default | MEDIUM | Routes unprotected unless explicitly enabled | `server/cmd/gateway/main.go` |
| Placeholder session UUID in Helm | MEDIUM | Won't work without manual replacement | `values.yaml:11` |
| Max tabs enforcement missing | LOW | Could exhaust server resources | `manager.go` |
| Tab idle timeout not implemented | LOW | Idle tabs consume resources indefinitely | `manager/manager.go` |

---

## Detailed Findings

### 1. Single-Client Enforcement (CRITICAL)

**DESIGN.md requirement:** "Enforces single client per session: If another client is already connected, respond with HTTP 409 or WebSocket close with reason."

**Current Implementation:** The code allows multiple concurrent clients to connect to the same PTY:

```go
// server/cmd/project/main.go (ptySession struct)
type ptySession struct {
    clients map[*websocket.Conn]bool
    // ...
}

func (ps *ptySession) addClient(conn *websocket.Conn) {
    ps.mu.Lock()
    defer ps.mu.Unlock()
    ps.clients[conn] = true  // Simply adds to map without checking!
}
```

**Expected:** Reject additional connections with HTTP 409 or WebSocket close message
**Actual:** Multiple clients connect simultaneously, all receiving the same output

---

### 2. Debug Logging in Production

Console.log statements throughout frontend code:

- `web/src/components/TerminalView.tsx` - Lines 73, 86, 96, 103, 112, 144
- `web/src/contexts/AuthContext.tsx` - Lines 63, 65, 70, 78, 84, 91, 98
- `web/src/App.tsx` - Lines 92, 104, 117, 126, 139, 147, 150

**Recommendation:** Remove or wrap with debug flag controlled by environment variable.

---

### 3. Auth Middleware Gaps

When `AUTH_MODE != "local"`, routes are completely unprotected (`server/cmd/gateway/main.go`).

**Issues:**
- No validation that `AUTH_MODE=local` in production
- Could lead to accidental exposure
- No warning when auth is disabled

---

### 4. Missing Endpoints

| Endpoint | Spec | Status | Notes |
|----------|------|--------|-------|
| `GET /api/healthz` | DESIGN.md 5.2 | NOT IMPLEMENTED | Optional but recommended |
| `POST /api/auth/password` | Not in spec | IMPLEMENTED | Enhancement beyond spec |

---

### 5. Gateway Mode Gaps

**Missing implementations:**

1. **Project health checks** (DESIGN.md 5.4)
   - Catalog has `healthCheckPath` field but it's not used
   - No downstream health monitoring

2. **Max tabs enforcement** (DESIGN.md 5.9)
   - Spec: `max_tabs_per_user` with 429 response
   - Not implemented in `manager.go`

3. **Idle timeout** (DESIGN.md 5.9)
   - Spec: `TAB_IDLE_TIMEOUT` configuration
   - Not found in implementation

---

### 6. Input Validation Gaps

1. **WebSocket resize messages** (`server/cmd/project/main.go`)
   - Cols/Rows checked for >0 but not capped
   - Could resize to invalid sizes

2. **Username validation** (`server/internal/handlers/auth/login.go`)
   - No length limits or character restrictions
   - Could allow very long usernames

---

### 7. Test Coverage

**Tests found:**
- `server/main_test.go` - Basic structure
- `server/internal/gateway/manager/manager_test.go`
- `server/internal/gateway/relay/relay_test.go`
- `server/internal/gateway/config/catalog_test.go`

**No tests for:**
- Auth manager/store
- Session store queries
- PTY lifecycle
- WebSocket message handling
- Configuration loading
- React components (zero test files)

---

### 8. Helm Chart Issues

1. **Placeholder UUID** (`values.yaml:11`)
   ```yaml
   sessionID: "00000000-0000-0000-0000-000000000000"
   ```
   Must be replaced per deployment but no validation

2. **Hardcoded IP** (`values.yaml:13`)
   ```yaml
   anthropicBaseURL: "http://172.25.1.66:8080"
   ```
   Should be configurable

3. **Missing documentation** for required CNPG secret format

---

### 9. Database Schema

**Gap found:** Missing index on `session_logs.created_at` for log retention queries.
- Current: `session_logs_session_created_idx` indexes `(session_uuid, created_at)`
- Needed: Separate index on `created_at` for pruning old logs by timestamp

---

## Working Features

### Fully Implemented

- [x] PTY management and WebSocket streaming
- [x] Session persistence to CNPG
- [x] Session logs with retention
- [x] Terminal resize handling
- [x] Output buffering (64KB)
- [x] Authentication with JWT tokens
- [x] Refresh token rotation
- [x] Password change functionality
- [x] Gateway mode with multi-tab support
- [x] Project catalog loading
- [x] Tab persistence
- [x] Downstream relay with backoff
- [x] Auto-reconnection with exponential backoff
- [x] Secure cookies (HttpOnly, SameSite)
- [x] CNPG configuration validation
- [x] Metrics endpoint (/metrics)

### Enhanced Beyond Spec

- [x] Password change endpoint (`POST /api/auth/password`)
- [x] Profile modal with user info
- [x] Logout confirmation dialog
- [x] Password strength validation
- [x] Last login tracking
- [x] Token metadata (user agent, IP)

---

## Recommendations

### High Priority (P0)

1. **Implement single-client enforcement**
   - Add check before accepting WebSocket
   - Return HTTP 409 if client already connected
   - Add WebSocket close reason for rejected connections

2. **Remove/control debug logging**
   - Create debug utility with environment flag
   - Remove all console.log in production builds

3. **Add input validation limits**
   - Cap PTY resize dimensions (e.g., max 500 cols, 200 rows)
   - Add username length limit (e.g., 64 chars)
   - Validate username characters

4. **Add health endpoint**
   - Implement `GET /api/healthz`
   - Check CNPG connectivity
   - Check PTY process status

### Medium Priority (P1)

5. **Implement max tabs per user**
   - Add limit checking in tab creation
   - Return 429 when exceeded
   - Make configurable per project

6. **Implement tab idle timeout**
   - Track last activity timestamp
   - Clean up idle tabs after timeout
   - Make configurable via `TAB_IDLE_TIMEOUT`

7. **Fix Helm placeholders**
   - Generate proper default UUIDs
   - Add validation in chart
   - Document required values

8. **Add React component tests**
   - Set up Vitest/RTL
   - Test Login, TerminalView, AuthContext
   - Add CI integration

### Low Priority (P2)

9. **Improve backend test coverage**
   - Auth manager/store tests
   - Session store tests
   - PTY lifecycle tests

10. **Implement project health checks**
    - Monitor downstream pod health
    - Update project status in catalog
    - Surface in UI

11. **Add session log search**
    - Implement search UI
    - Add backend search endpoint
    - Index log content for search

12. **Add missing database index**
    - Index on `session_logs.created_at`
    - Improves log retention queries

---

## Files Requiring Changes

### Critical

- `server/cmd/project/main.go` - Single-client enforcement
- `web/src/components/TerminalView.tsx` - Remove debug logs
- `web/src/contexts/AuthContext.tsx` - Remove debug logs
- `web/src/App.tsx` - Remove debug logs

### High Priority

- `server/internal/gateway/manager/manager.go` - Max tabs, idle timeout
- `deploy/helm/values.yaml` - Fix placeholders, documentation

### Medium Priority

- `server/migrations/` - Add `created_at` index
- `web/src/` - Add test files

---

## Conclusion

KubeTTY is **75% complete** with solid core functionality. The critical gap is **single-client enforcement** which contradicts the design specification and allows unintended multi-client access. This must be fixed before production use.

Secondary concerns are debug logging exposure, missing resource limits in gateway mode, and limited test coverage. The authentication system is well-implemented and even exceeds the original specification.
