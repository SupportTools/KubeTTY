# Implementation Plan

## 1. Shared Infrastructure
- Finalize CNPG cluster config so `kubetty-shared-app` secret is created (ensure `bootstrap.initdb` + `managedRoles` and `kubetty-postgres-user` secret exist).
- Document the secret name/keys needed by Helm (`username`, `password`, `dbname`, `host`, `port`) and ensure it lives in `kubetty-shared`.
- Decide how namespaces are created (script or manual `kubectl create namespace <name>`), especially for per-project releases.

## 2. Backend Tasks
- Flesh out `internal/sessions` with real CNPG queries (already scaffolded) and add unit tests covering upsert/list/attachment logic.
- Expand PTY management: handle resize messages, heartbeat logging, and graceful shutdown when shell exits.
- Implement reconnect handshake messages (ack/nack) so the frontend can surface errors.
- Add structured logging and configurable log destination (stdout + optional file).

## 3. Frontend Tasks
- Polish SessionPicker UX (loading states, error surfaces, ability to display fork lineage).
- Wire terminal resize events to backend when implemented; persist theme/keyboard settings locally.
- Add status banner for Claude-style CLI shortcuts (e.g., showing `--continue` mapping).
- Include minimal smoke tests (Vitest/React Testing Library) for SessionPicker state handling.

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
- Document deployment procedure: create namespace, `helm upgrade --install`, `kubectl port-forward`, and verifying `/session` + `/ws`.

## 7. Validation & Ops
- Smoke test end-to-end: start backend, connect via browser, run CLI tools (Claude alias), confirm logs land under `$HOME/claude_logs`.
- Validate CNPG persistence by restarting pod and ensuring session resumes.
- Add operational runbook: how to rotate session UUIDs, how to fork sessions via CLI flags, how to clean up CNPG rows.
- Capture troubleshooting steps for WebSocket errors, database outages, or missing tools in the container.

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
- TODO: update DESIGN/README with session picker refresh behavior.

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
