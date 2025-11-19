# Current Debug State (2025-11-18)

## Repository context
- Repo: `github.com/supporttools/KubeTTY`
- Recent backend updates:
  - Added baseline + attached_at migrations and removed runtime schema creation.
  - PGX store now expects migrations; `DeleteSession` + `ClearAttachments` were added.
  - HTTP server now listens via `httpSrv` and installs SIGINT/SIGTERM handlers that call `srv.shutdown()`; `shutdown()` iterates all `ptySessions`, kills the PTY, and deletes the row.
  - On startup we call `store.ClearAttachments` for the deployment so restarts don’t leave `attached_to` set.
- Frontend: refreshed `web/src/assets/logo.svg` (dark logo).
- Helm: deployment strategy set to `Recreate`. Beacon Support values currently point at `harbor.support.tools/kubetty/beacon-support:v0.1.22` (release revision 42).

## Deployment state
- Namespace: `kubetty-beacon-support`
- Pod: `kubetty-beacon-support-kubetty-8c8d4fbd5-8nxsp` (running image `v0.1.22`).
- CNPG cleanup performed: only session `302ed84c-08b9-4445-ad92-c9899e7abea6` remains; `attached_to` cleared manually.

## Outstanding issue
- UI still fails to connect to the default session; WebSocket attempts hit HTTP 409 (`session already attached`).
- Manual Go websocket client inside the pod reproduces the 409 even immediately after restart.
- Need to instrument `ensureSession`/`tryAttach` or inspect `s.ptySessions` map at runtime to see why the server thinks the session is still attached despite DB cleanup.

## Next steps after restart
1. Recreate any temporary tooling (e.g., `/tmp/wsclient`) if needed for diagnostics.
2. Add logging or temporary endpoints to inspect `srv.ptySessions` to confirm whether the in-memory map retains stale entries.
3. If necessary, add an admin endpoint or CLI flag to force clearing `ptySessions` and `attached_to` without manual SQL.
4. Once root cause is known, rebuild image (bump tag) and redeploy via `helm upgrade --install kubetty-beacon-support …`.

