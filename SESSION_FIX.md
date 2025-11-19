# Session Attachment Race Condition Fix

## Problem

The KubeTTY application had a critical race condition in session attachment logic that caused connection failures with HTTP 409 (Conflict) errors.

### Symptoms
- Users see "session already attached" errors
- WebSocket connections fail with 409 status code
- Terminal shows "Connecting..." but never connects
- Even after clicking "Force Attach", subsequent reconnections fail

### Root Cause

In `server/main.go:483-502`, the `tryAttach()` function had an asynchronous detachment mechanism:

```go
if ps.attached && force {
    if fn := ps.detachFn; fn != nil {
        go fn()  // ← ASYNC CALL - Creates race condition!
    }
    // ... state updates
}
```

This created a race condition where:

1. Old connection's `detachFn` runs **asynchronously** in a goroutine
2. New connection establishes before old connection closes
3. Database attachment state updates asynchronously via deferred functions
4. In-memory state and database state become inconsistent
5. Subsequent connection attempts fail because session appears "attached"

### The Fix

Changed the detachment to be **synchronous** (server/main.go:483-511):

```go
func (ps *ptySession) tryAttach(force bool, clientID string) (bool, bool) {
	ps.mu.Lock()
	// ... checks and state updates
	var detachToRun func()
	if ps.attached && force {
		if fn := ps.detachFn; fn != nil {
			detachToRun = fn  // Store function reference
		}
		// ... reset state
	}
	// ... set new attachment
	ps.mu.Unlock()

	// Call detachFn OUTSIDE the lock and SYNCHRONOUSLY
	if detachToRun != nil {
		detachToRun()  // ← SYNC CALL - No race condition!
	}

	return true, forced
}
```

**Key improvements:**
1. ✅ Detachment happens synchronously before new connection proceeds
2. ✅ Old WebSocket receives close message and actually closes
3. ✅ Database state updates complete before new attachment
4. ✅ No inconsistency between in-memory and database state
5. ✅ Function called outside mutex to prevent deadlock

## Testing

All existing tests pass:
```bash
$ go test -v ./...
=== RUN   TestHandleSessionLogs
--- PASS: TestHandleSessionLogs (0.00s)
=== RUN   TestEnforceLogRetention
--- PASS: TestEnforceLogRetention (0.00s)
=== RUN   TestPtySessionForceAttach
--- PASS: TestPtySessionForceAttach (0.00s)
PASS
```

## Deployment

### Build
```bash
cd server
make build
```

### Deploy to Kubernetes
```bash
kubectl apply -f deploy/
kubectl rollout restart deployment kubetty
```

### Verify
1. Navigate to https://beacon-kubetty.support.tools/
2. Click "Resume" on an existing session
3. If prompted about another client, click "Force Attach"
4. Terminal should connect immediately without retry loops
5. Check browser console - no more 409 errors

## Impact

- **Before**: Multiple 409 errors, infinite retry loops, sessions stuck as "attached"
- **After**: Clean force-attach behavior, immediate reconnection, consistent state

## Related Files
- `server/main.go:483-511` - Fixed tryAttach() function
- `server/main.go:290-293` - detachFn registration (unchanged)
- `server/main.go:260-276` - Database attachment handling (unchanged)
