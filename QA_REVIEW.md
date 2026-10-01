# KubeTTY QA Review

**Review date:** 2026-10-01 (supersedes the 2025-11-19 "75% complete" review)
**Scope:** `server/`, `web/`, `deploy/helm*`, `.github/workflows/` at commit `e4f5b99`
**Method:** every finding from the 2025-11-19 review was re-checked against current code;
new findings below were observed while doing so. Line numbers refer to the files as of this review.

## Summary

All P0/P1 items from the original review are fixed except one partial (unconditional auth
debug logging in the browser). The remaining risk is concentrated in authorization
(no admin role, cross-user tab takeover), the per-project RBAC the controller still tries
to create, and a few dead or inconsistent subsystems (DB session logs, storage-class defaults).

| # | Finding | Severity | Status |
|---|---------|----------|--------|
| 1 | Auth debug logging always on in `AuthContext.tsx` | LOW | Open (partially fixed) |
| N1 | No admin role: any authenticated user can use every `/api/admin/*` endpoint | MEDIUM | New |
| N2 | `?force=true` lets any authenticated user take over another user's tab | MEDIUM | New |
| N3 | Controller still creates per-project cluster-wide `*` ClusterRoles/Bindings | MEDIUM | New |
| N4 | Legacy `deploy/helm` chart still binds the controller cluster-wide | MEDIUM | New |
| N5 | DB session logs are never written; `/session/logs` and retention settings do nothing | MEDIUM | New |
| N6 | Default storage class is still `longhorn` in DB/UI defaults | MEDIUM | New |
| N7 | `independent_shells` PTYs are never reaped on tab close and are uncapped | LOW | New |
| N8 | Single-client reservation edge cases in project `/ws` | LOW | New |
| N9 | Gateway WebSocket upgrader accepts any Origin | LOW | New |
| N10 | No login rate limiting / lockout | LOW | New |
| N11 | Expired refresh tokens are never pruned | LOW | New |
| N12 | `/debug/vars` (expvar) served unauthenticated on the gateway | LOW | New |
| N13 | Exec-mode resize is not capped like the WebSocket path | LOW | New |
| N14 | Test-coverage gaps; CI coverage gate is advisory | LOW | New |

---

## Open Findings

### 1. Auth debug logging is unconditional (partially fixed)

`TerminalView.tsx`, `TabPane.tsx` and `GUIView.tsx` now gate `console.debug` behind
`import.meta.env.DEV` (e.g. `web/src/components/TerminalView.tsx:8-17`), and `App.tsx` has no
console output. However `authLog` in `web/src/contexts/AuthContext.tsx:45-48` calls
`console.log` unconditionally and is used 27 times, logging usernames and token refresh timing in
production builds (e.g. lines 165, 180, 244).

**Fix:** gate `authLog` on `import.meta.env.DEV` like the other components.

### N1. No authorization tier for admin endpoints

`kubetty_users` has no role column (`server/migrations/0004_auth_tables.up.sql:3-11`). Admin routes
are wrapped only in `requireAuth` (`server/cmd/gateway/main.go:545-558`, `580-583`, `594-602`), so
any active user can create, delete or upgrade projects, change global settings, and read or write
project env secrets (`GET/PUT /api/admin/projects/{id}/secrets`, `main.go:557-558`).
With `AUTH_MODE` other than `local`, the same routes are registered with no auth at all
(`main.go:559-573`, `585-588`, `604-612`). The gateway logs a warning in that case (see Resolved).

### N2. Cross-user tab takeover

`AttachWithOptions` reassigns tab ownership to the caller whenever `force=true`
(`server/internal/gateway/manager/manager.go:415-436`). The caller is identified by user ID
(`server/cmd/gateway/main.go:1379-1386`), but nothing checks that the old and new owners are the
same user, so any authenticated user who knows a tab ID can take over another user's live shell.
`AttachVNC` behaves the same way (`manager.go:633-642`).

### N3. Controller still tries to create cluster-wide per-project RBAC

`createProjectResources` creates an admin ClusterRole with `*` verbs on `*` resources in the
core, apps, batch, extensions and networking groups, plus a ClusterRoleBinding
(`server/internal/controller/controller.go:313-338`, `server/internal/controller/resources.go:697-716`).
Since `5ff9bba` the `helm-gateway` chart no longer grants the controller `clusterroles` or
`clusterrolebindings`, so these calls now fail. The controller only logs a warning, so project
creation continues without the RBAC. The intent should be redesigned as namespaced Roles, or the
code removed.

### N4. Legacy `deploy/helm` chart is still cluster-wide

`deploy/helm/templates/controller-rbac.yaml:18-48,51-66` grants namespaces, `clusterroles` and
`clusterrolebindings` create/delete and binds them with a ClusterRoleBinding. Only `deploy/helm-gateway`
got the namespace-scoped fix in `5ff9bba`. CI and `scripts/deploy-prod.sh` deploy `helm-gateway`.
`README.md:55,316` still documents installing `./deploy/helm`.
That chart also has another problem: it requires `env.sessionID` even in gateway mode
(`deploy/helm/templates/deployment.yaml:14-19`), although `deploy/helm/values.yaml:31` says to
leave it empty for the gateway. The legacy chart should either be retired or brought in line
with `helm-gateway`.

### N5. DB session logging is dead code

The project binary has been stateless since `8924f27` (2025-11-21). No production code calls
`AppendLog`, `UpsertSession`, `PruneLogs` or `TrimLogs`; only the interface and store define them
(`server/internal/sessions/store.go:25-32`). The gateway still serves `/session/logs`
(`server/cmd/gateway/main.go:624`), and the UI still offers `SessionLogsModal`, but the
`session_logs` table is never populated. `SESSION_LOG_RETENTION_HOURS` and
`SESSION_LOG_MAX_ENTRIES` are parsed (`server/internal/config/common.go:34-35`) but never used.
PTY transcripts actually go to stdout or a JSONL file for Loki (`server/internal/shared/ptylogger`,
`server/internal/shared/filelogger`, `PTY_LOG_ENABLED` / `PTY_FILE_LOG_*` in
`server/internal/config/project.go:61-75`).

### N6. Storage-class defaults still point at Longhorn

`c9069bd` (2026-02-08) changed `projects.DefaultStorageClass` to `freenas-iscsi-csi`
(`server/internal/projects/models.go:231`), but three other defaults still say `longhorn`:
- the column default in `server/migrations/0008_projects.up.sql:22`
- the seeded `project_defaults.storage_class` setting in `server/migrations/0013_settings.up.sql:126`,
  which `server/internal/handlers/admin/projects.go:68-69` prefers over the code constant
- the create-project form default in `web/src/components/AdminProjectForm.tsx:21`

So a new project gets `longhorn` unless an operator overrides it in the form or in settings.

### N7. `independent_shells` PTYs accumulate

In `independent_shells` mode, each gateway tab gets its own PTY, keyed by `?shell=<tabID>`
(`server/cmd/project/main.go:434-443`, `server/internal/gateway/manager/manager.go:499-505`). A PTY
is removed only when its process exits (`server/cmd/project/main.go:974-981`). Closing a tab does
not tear down its shell, and the number of shells per pod has no cap.

### N8. Single-client enforcement edge cases (`server/cmd/project/main.go`)

- The non-force path defers `ps.releaseSlot()` (lines 504-507). The comment says it is for
  upgrade failure, but it runs on every handler exit, after `removeClient` (line 562). It can
  therefore decrement a reservation made by a newer connection, which reopens a narrow
  double-admit window.
- The force path (lines 478-489) disconnects existing clients without reserving a slot, so two
  simultaneous `force=true` connects, or a force connect racing a normal one, can both be admitted.

### N9. WebSocket Origin is not checked

Both upgraders use `CheckOrigin: func(r *http.Request) bool { return true }`
(`server/cmd/gateway/main.go:491`, `server/cmd/project/main.go:339`). On the gateway, auth relies
on cookies with `SameSite=Lax` (`server/internal/handlers/auth/helpers.go:158`). That blocks
cross-site pages, but any same-site origin, such as another `*.support.tools` host, could open an
authenticated terminal WebSocket. The project pod `/ws` is unauthenticated by design and relies on
the controller-created NetworkPolicy (`server/internal/controller/controller.go:352-358`).

### N10. No login throttling

`/api/auth/login` (`server/internal/handlers/auth/login.go:86-113`) validates input but has no rate
limiting, backoff or lockout.

### N11. Refresh-token cleanup is never scheduled

`DeleteExpiredRefreshTokens` exists (`server/internal/auth/store.go:264`), but nothing calls it, so
`kubetty_refresh_tokens` grows without bound.

### N12. `/debug/vars` is public

`server/cmd/gateway/main.go:509` registers `expvar.Handler()` outside the auth middleware. It
exposes memstats and the process command line.

### N13. Exec-mode resize bounds

The WebSocket path clamps resize to 500x200 (`server/cmd/project/main.go:47-48,638-655`). The
gateway exec relay (`KUBETTY_EXEC_MODE=exec`) checks only for values > 0
(`server/internal/gateway/exec/relay.go:659-661`). The fields are `uint16`.

### N14. Test coverage

There are now 62 Go test files and 9 web test files (see Resolved). Remaining gaps:
- No tests for `server/internal/settings`.
- No web tests for the admin UI (`AdminDashboard`, `AdminProject*`, `AdminSettings`),
  `ProjectPicker` or `SessionLogsModal`.
- The CI coverage gate (30%) is `continue-on-error: true` (`.github/workflows/pipeline.yml:96-106`).

### Operational notes (not defects)

- `deploy/helm-gateway/templates/deployment.yaml:17` hard-codes `replicas: 1`. Leader election
  (`99d3721`) is enabled by default, but production runs a single gateway, and tab state lives
  in gateway memory.
- Grype image scanning is opt-in and the deploy gate for it is disabled (`if: false`) because the
  image is too large (`.github/workflows/pipeline.yml`, "Verify security scan passed").

---

## Resolved (from the 2025-11-19 review)

| Original finding | Resolution | Evidence |
|------------------|------------|----------|
| Single-client enforcement missing (CRITICAL) | 409 on second client added in `ba7a23b` (2025-11-20). TOCTOU race fixed with `reserveSlot()` in `6661fa4` (2026-02-08). Superseded by per-project session modes in `e2d489d` (2026-03-03): `exclusive_takeover` (default, 409 plus `?force=true` takeover), `shared_concurrent`, `independent_shells` | `server/cmd/project/main.go:429-520`, `server/migrations/0016_project_session_mode.up.sql`, gateway tab ownership at `server/internal/gateway/manager/manager.go:408-505` |
| Debug `console.log` in `TerminalView`/`App` | Dev-gated `devLog` helpers (`d515859`, `39ac98c`); `App.tsx` clean | `web/src/components/TerminalView.tsx:8-17`. `AuthContext` still open (#1) |
| Auth not enforced / no warning | Startup warning (`cbdb234`), `X-Auth-Warning` header middleware; Helm `helm-gateway` defaults `auth.mode: local` | `server/cmd/gateway/main.go:95-100,651`, `server/internal/shared/server/auth_warning.go`, `deploy/helm-gateway/values.yaml:30` |
| Placeholder session UUID in Helm | Default is empty; chart `fail`s on missing/placeholder UUID (`29ff6bf`) | `deploy/helm/templates/deployment.yaml:14-19`, `deploy/helm-project/templates/deployment.yaml:4-9` |
| Hard-coded `anthropicBaseURL` IP | Now empty by default, documented (`29ff6bf`) | `deploy/helm/values.project-template.yaml:61-65` |
| Missing CNPG secret docs | Documented (`29ff6bf`) | `deploy/helm/README.md:27,40,111` |
| Max tabs enforcement missing | Per-client and per-project limits with 429 (`b29ffa4`) | `server/internal/gateway/manager/manager.go:208-227`, `server/cmd/gateway/main.go:968-983` |
| Tab idle timeout not implemented | `TAB_IDLE_TIMEOUT` (default 2h, min 10m) with 5-min warning (`dc7a330`) | `server/internal/config/gateway.go:112`, `server/internal/gateway/manager/manager.go:121-143,1179+` |
| `GET /api/healthz` missing | Implemented in both binaries (gateway checks DB; project checks PTY); unified in `a569c59`; leader status at `/api/healthz/leader` | `server/cmd/gateway/main.go:524-527`, `server/cmd/project/main.go:372-375` |
| Project health checks unused | Downstream poller (`a19e330`), surfaced via `HealthIndicator` | `server/internal/gateway/health/checker.go`, `server/internal/gateway/manager/manager.go:1171` |
| Resize dimensions uncapped | Capped at 500 cols / 200 rows (`ba7a23b`) | `server/cmd/project/main.go:47-48,638-655` (exec mode: see N13) |
| Username not validated | Max 64 chars, `^[a-zA-Z0-9_-]+$` (`1ec2eb1`) | `server/internal/handlers/auth/helpers.go:26-32`, `login.go:94-104` |
| Missing `session_logs.created_at` index | Migration 0007 (`d781273`) | `server/migrations/0007_session_logs_created_idx.up.sql` (moot while N5 stands) |
| Session log search | Backend `search`/`direction` filters plus UI (`d781273`, migration 0006) | `server/internal/handlers/session/logs.go:93-109`, `web/src/components/SessionLogsModal.tsx` (moot while N5 stands) |
| No backend tests for auth/sessions/config | Added (`ba12a54`, `65cdb82`), now 62 `_test.go` files | `server/internal/auth/*_test.go`, `server/internal/sessions/pgx_store_test.go`, `server/internal/config/*_test.go` |
| Zero React tests | Vitest + RTL (`b3d2d38`), 9 test files, run in CI `test-web` | `web/src/**/*.test.tsx`, `.github/workflows/pipeline.yml:115-140` |
