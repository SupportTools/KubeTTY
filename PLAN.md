# Implementation Plan

## 1. Shared Infrastructure
- Finalize CNPG cluster config so `kubetty-shared-app` secret is created (ensure `bootstrap.initdb` + `managedRoles` and `kubetty-postgres-user` secret exist).
- Document the secret name/keys needed by Helm (`username`, `password`, `dbname`, `host`, `port`) and ensure it lives in `kubetty-shared`.
- Decide how namespaces are created (script or manual `kubectl create namespace <name>`), especially for per-project releases.

## 2. Backend Tasks
- [DONE] Flesh out `internal/sessions` with real CNPG queries for single-session-per-pod model.
- [DONE] Expand PTY management: handle resize messages, heartbeat logging, and graceful shutdown when shell exits.
- [DONE] Implement single-client enforcement (reject additional connections until first disconnects).
- Add structured logging and configurable log destination (stdout + optional file).

## 3. Frontend Tasks
- [DONE] Wire terminal resize events to backend; persist theme/keyboard settings locally.
- [DONE] Implement login form and auth flow for protected deployments.
- Add session logs viewer modal for reviewing past PTY transcripts.
- Include minimal smoke tests (Vitest/React Testing Library) for auth and terminal state handling.

## 4. Tooling & Scripts
- Add `Makefile` targets: `build-ui`, `build-server`, `docker-build`, `docker-push`, `helm-install`.
- Create `/etc/profile.d/claude.sh` install step in Dockerfile sourcing `scripts/claude_with_log.sh`.
- Verify required CLIs (kubectl, helm, docker, go, node, git, jq, yq, curl/httpie, ripgrep, fd, tmux, make, python3/pip, psql, Claude/Codex/Gemini) are installed and pinned to versions.
- Add lint/format checks (`golangci-lint`, `npm run lint`) if desired.

## 5. Image Build & Registry
- Author Dockerfile that compiles the Go server, copies `server/ui/dist`, installs toolchain, and sets entrypoint.
- Document local build + push workflow (`docker build -t harbor.support.tools/kubetty/<repo>:<tag> .` then `docker push`).
- Provide example tags per project (`kubetty-ai-dev:2024-05-01`) and note how to prune old images in Harbor.

## 6. Helm & Deployment
- Finish Helm chart templates (Service, Deployment, ConfigMap/Secret if needed) and add NOTES about port-forwarding.
- Parameterize env vars (`SESSION_ID`, `CLAUDE_*`, `ANTHROPIC_BASE_URL`, CNPG host/port/db/user/pass) via `values.yaml`.
- Offer sample values files for Project A/B, each with unique namespaces and session UUIDs.
- Document deployment procedure: create namespace, `helm upgrade --install`, `kubectl port-forward`, and verifying `/ws` + `/api/auth/me`.

## 7. Validation & Ops
- Smoke test end-to-end: start backend, connect via browser, run CLI tools (Claude alias), confirm logs land under `$HOME/claude_logs`.
- Validate CNPG persistence by restarting pod and ensuring session resumes.
- Add operational runbook: session management, auth user creation/rotation, JWT secret rotation, CNPG cleanup.
- Capture troubleshooting steps for WebSocket errors, database outages, auth failures, or missing tools in the container.

## 8. Documentation
- Update README/AGENTS to describe build/run instructions, env vars, CLI aliases, and deployment workflow.
- Provide Helm chart README with configuration table and CNPG requirements.
- Include onboarding checklist covering CNPG access, Harbor credentials, and namespace prep.

---

## Build Workflow (current reference)
1. `make ui` – installs `web` dependencies and produces `server/ui/dist`.
2. `make server` – runs `go fmt`, `go test`, and builds a local `bin/kubetty`.
3. `make docker-build IMAGE=harbor.support.tools/kubetty/<repo> TAG=<tag>` – multi-stage build producing a tool-rich runtime image.
4. `make docker-push IMAGE=... TAG=...` – pushes the image to Harbor once authenticated (`docker login harbor.support.tools`).
5. `helm upgrade --install <release> deploy/helm -n <namespace> -f <values>` – deploys using the freshly pushed image, referencing CNPG secrets in `kubetty-shared`.

---

## 9. Multi-Project Gateway Implementation Plan

1. **Gateway Configuration & Catalog**
   - Define a `projects.yaml` (or `PROJECTS_JSON`) schema with `id`, `displayName`, `namespace`, `service`, `port`, and optional metadata (icon, description).
   - Extend the Go config package to load and validate the catalog; add unit tests.
   - Document how to onboard a new project (update ConfigMap + redeploy gateway).
   - Implement hot-reload or config hash comparison to detect drift and emit metrics/logs when the catalog changes.

2. **Downstream WebSocket Relay**
   - Introduce a `relay` package that can dial `ws://<service>.<namespace>.svc:<port>/ws` with TLS support if needed.
   - Manage lifecycle for each relay (connect, read/write pumps, retry with exponential backoff).
   - Track metrics (latency, bytes, reconnect counts) per project.
   - Handle backpressure to avoid unbounded buffering when the browser or downstream pod stalls.
   - Capture structured events (connect, disconnect, retry) and surface them to both logs and `/api/tabs` consumers.

3. **Gateway API Surface**
   - Implement `/api/projects` (list) and `/api/tabs` (POST create, GET list, DELETE close) endpoints.
   - Persist tab metadata in memory plus CNPG (reuse `sessions` table with new columns or add a `gateway_tabs` table) so browser reloads can resume.
   - Add `/ws?tab=<id>` handler that enforces tab ownership, wires to the relay, and streams structured status events (connected, reconnecting, closed).
   - Expose `/api/healthz` aggregating downstream status so SREs can monitor the gateway itself.
   - Add migrations + DAO layer for the new `gateway_tabs` table; include unit tests.

4. **React Tabbed UI**
   - Build a `TabManager` component with reducer/actions for open tabs, focus changes, and persistence via `localStorage`.
   - Create a `ProjectPicker` modal that calls `/api/projects` and handles empty/offline states.
   - Update `TerminalView` to accept `wsUrl` + `tabId` props and to display project metadata (badge, status pill).
   - Add Vitest/RTL coverage for tab reducer, picker, and reconnect messaging.
   - Instrument analytics/logging hooks so backend can correlate client actions with server events.

5. **Deployment & Security Hardening**
   - Add a Helm chart (or extend the existing one) for the gateway deployment, mounting the project catalog and the CNPG creds the gateway needs for tab persistence.
   - Author NetworkPolicies that only allow the gateway namespace to reach each project service.
   - Expose Prometheus metrics + logs for per-project visibility; ensure log lines include tab and project IDs.
   - Provide runbooks for rotating CNPG credentials and gateway certificates without downtime.
   - Decide on sticky session strategy (cookie affinity vs. shared relay store) before scaling to multiple gateway replicas.

6. **Validation & Runbook**
   - Write an end-to-end validation script: open multiple tabs, verify each hits its project pod, simulate pod restarts, confirm reconnection works.
   - Document troubleshooting steps (e.g., downstream pod offline, NetworkPolicy denies, catalog drift).
   - Update DESIGN.md/README with the new architecture diagrams and operational guidance.
   - Capture load-testing expectations (e.g., 20 concurrent tabs) and include soak test checklist.
   - Add synthetic monitoring job that periodically opens a tab for each project to verify end-to-end connectivity.

## 10. Authentication Implementation Plan (complete)

1. **Configuration & Secrets**
   - Added `AUTH_*` env var plumbing to `internal/config`, added validation, and surfaced the values in the Helm charts (`deploy/helm/values*.yaml`).
   - Documented secret handling plus helper instructions in `README.md` and exposed `auth.jwtSecretSecret` so operators can point to a Kubernetes Secret instead of embedding raw secrets.

2. **Database Layer**
   - Created migrations for `kubetty_users` and `kubetty_refresh_tokens` with citext usernames and indexed refresh token metadata.
   - Implemented `internal/auth/store.go` for user and refresh token CRUD with proper error handling.

3. **Auth Manager & Tokens**
   - Built `internal/auth/manager.go` to hash passwords, issue HMAC-SHA256 JWTs, rotate hashed refresh tokens, and validate incoming tokens.

4. **HTTP Middleware & Handlers**
   - Added `/api/auth/login`, `/api/auth/me`, `/api/auth/refresh`, `/api/auth/logout` and wrapped all routes (including `/ws`) with middleware that enforces access tokens and injects user context.
   - Auth cookies are now Secure/HttpOnly and configurable via `AUTH_COOKIE_*` env vars.

5. **CLI & Ops Tooling**
   - Added `server/cmd/kubetty-authuser` for creating/updating/listing users and toggling activation states.
   - README now includes step-by-step auth enablement, user creation commands, and notes on token rotation.

6. **Frontend Experience**
   - SPA probes `/api/auth/me`, renders login UI when unauthenticated, sends credentials with every request, and shows logout/user info once logged in.
   - Session log requests now send cookies to remain operable under auth.

7. **Testing & Validation**
   - Ran `go test ./...` and `npm --prefix web run build`; tests currently rely on the embedded binaries without additional coverage at this stage.

## 11. Post-auth Deployment & Docs

1. **Secrets & User Bootstrap**
   - Seed the first user via `go run ./server/cmd/kubetty-authuser create ...`.
   - Store `AUTH_JWT_SECRET` in a Kubernetes Secret and reference it from the Helm `auth` block before enabling `mode: local`.
   - Document rotation steps in `README.md`.

2. **Verification**
   - Manually verify login/refresh/logout flows via browser + curl (using cookies and Bearer tokens).
   - Ensure `/session/logs` + gateway tab APIs continue working when auth is enabled.
   - Update any CI or rollout scripts to pass `AUTH_*` env vars where needed.

3. **Next Incorporations**
   - Consider adding health endpoints or metrics around login failures/refresh attempts once real usage starts.
   - Monitor `kubetty_refresh_tokens` growth and tune cleanup (via `auth.DeleteExpiredRefreshTokens`) as part of ongoing maintenance.
