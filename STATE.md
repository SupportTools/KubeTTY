# Current Implementation State (2025-11-19)

## Session Model
- **Single session per pod**: One PTY is created on first WebSocket connection and reused for the pod lifetime.
- **Single client enforcement**: Only one browser can connect at a time; additional connections are rejected.
- **No multi-session features**: Fork/resume/continue commands were designed but never implemented; documentation has been updated to reflect actual behavior.
- **Output buffering**: 64KB buffer replays initial output (MOTD) to new clients on connect.

## Repository context
- Backend now requires `AUTH_MODE=local` to enable the new JWT auth stack; migrations (`0004_auth_tables`) and `internal/auth/{store,manager}` implement users, refresh tokens, token issuance, and validation.
- New CLI helper `server/cmd/kubetty-authuser` lets operators seed users, rotate passwords, and toggle activation without touching SQL. README documents the auth env vars, Helm `auth` block, and helper usage.
- React UI probes `/api/auth/me`, shows a login form when needed, and sends `credentials: "include"` with every request; session log fetches also send cookies so auditing works once auth is turned on.
- Helm charts expose the `auth` block, inject all `AUTH_*` env vars, and default to `AUTH_MODE=disabled` unless overridden by `values.gateway`/`values.beacon-support`.

## Local deployment state
- Built frontend (`npm --prefix web run build`) and Go server (`go test ./...`); generated assets live under `server/ui/dist`.
- No running cluster state tracked locally; the current work is purely code/config updates for the auth flow.

## Outstanding issues / remediation
1. **Migration rollout** – ensure the new `0004_auth_tables` migration is applied before enabling auth, and document how to seed the first user (`kubetty-authuser`).
2. **Secret rotation** – `AUTH_JWT_SECRET` should live in a Kubernetes Secret (referenced via `auth.jwtSecretSecret`) and rotating it will log everyone out; note this in ops runbooks.
3. **Login verification** – after restart confirm the SPA login form appears, successful login yields cookies, and `/session/logs` works under auth. Also test `curl -u` (Bearer header) flows once tokens are available.
4. **Metrics & cleanup** – consider scheduling `auth.DeleteExpiredRefreshTokens` and exporting login metrics once more ideas land; currently only the infrastructure is in place.

## Next steps after restart
1. Recreate any temporary experiment branches/notes (e.g., `server/kubetty` binary) if needed.
2. Seed initial user via `go run ./server/cmd/kubetty-authuser create ...` and store the hash per instructions.
3. Validate Helm values (`values.yaml`, `values.gateway`, `values.beacon-support`) against secrets and adjust tokens before redeploying.
4. Rebuild the Docker image once secrets/configs are stable, restart the pod, and manually verify the auth workflow end-to-end before handing back control.
