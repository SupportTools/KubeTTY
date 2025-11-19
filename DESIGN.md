---

# 📘 KubeTTY Design Document

## 1. Overview

**KubeTTY** is a lightweight, internal-use-only tool that provides a persistent browser-based terminal backed by a Kubernetes pod. It is designed to act as a “remote dev box” from which we can run:

* Claude Code
* Codex
* Go toolchain
* Docker builds (via sidecar or Docker host)
* Arbitrary commands inside a PTY (fully interactive)

The goal is to provide simple, low-overhead shell access without relying on `kubectl exec` or external SSH, while supporting long-running LLM workflows (Claude Code, Codex, Gemini CLI, etc.) that can be paused and resumed through stable session IDs.

KubeTTY is deliberately minimalist: a Go backend + a React UI, plus a persistent session registry in CNPG so every deployment can reconnect to the same PTY after browser drops or pod restarts.

---

## 2. Goals & Requirements

### 2.1 Functional Requirements

1. Web interface exposes an interactive shell running inside the pod.
2. Backend runs a *real PTY* (not just stdin/stdout) for full interactivity:

   * Arrow keys
   * Tab completion
   * Vim / nano / top
   * Tmux inside the shell
3. One client per session (single-user internal tool).
4. Ability to disconnect and reconnect without killing the underlying shell, backed by a long-lived PTY whose metadata lives in CNPG (tmux remains optional for later).
5. Backend assigns a stable session UUID per deployment (via `--session-id`), persists PTY metadata in CNPG, and supports Claude-style commands (`--continue`, `--resume <id>`, `--fork-session`).
6. Backend serves the React frontend statically.
7. Access only via:

   * Kubernetes port-forward **OR**
   * Internal cluster network (no public exposure).
8. No user accounts, no auth (security relies on network isolation).
9. Session logging helper (`claude_with_log`) records interactive Claude CLI sessions to disk for later review.

### 2.2 Non-Functional Requirements

* Lightweight (minimal libraries, ~150 LoC backend) while integrating CNPG client and session registry.
* Full terminal fidelity on day one using xterm.js (no `<textarea>` fallback).
* Easy to deploy inside an existing dev pod with all required CLI tools baked into the image (kubectl, helm, docker, go, node/npm, git, jq, yq, curl/httpie, ripgrep, tmux, make, psql, Claude CLI, Codex CLI, Gemini CLI, etc.).
* Build pipeline is local-only: engineers build the Docker image locally, push to `harbor.support.tools/kubetty/<repository>[:tag]`, and update namespaces via `~/.kube/config`.
* Minimal dependencies: Go, React, PTY lib, WebSocket lib, CNPG client.
* Secure enough for *internal-only* use (private IP ranges only, no ingress auth).
* Easy for an engineer to understand and extend.

---

## 3. Architecture

### 3.1 High-Level Diagram

```
Browser UI / CLI (React, Claude alias)
      |
      |  WebSocket (binary/text)
      v
Go Server (KubeTTY)
     / \
    /   \  PTY stdin/stdout
   v     v
CNPG Session Store   /bin/bash (interactive)
```

### 3.2 Components

#### 1. **Go Backend (PTY server)**

* Exposes:

  * `GET /` → static React app
  * `GET /ws` → WebSocket endpoint
* Creates a PTY using:

  * `github.com/creack/pty`
* Upgrades connection using:

  * `github.com/gorilla/websocket`
* Pipes:

  * Shell → WS (stdout)
  * WS → Shell (stdin)
* Handles resizing (optional future enhancement)
* Persists session metadata (session UUID, shell PID, resume token, timestamps, log pointers) in CNPG.
* Enforces single-client-per-session by rejecting a second attachment unless `--fork-session` is provided.

#### 2. **React Frontend**

* Displays terminal output using **xterm.js** from the start for full PTY fidelity (colors, cursor control, alternate screen).
* Captures user keystrokes and sends them to backend.
* Shows connection status.
* Auto-scrolls output.
* Implements Claude-style resume flow: on load it calls `/session` to fetch the deployment’s default UUID, supports explicit resume/fork requests, and passes the chosen ID as a query param when opening `/ws`.

#### 3. **CNPG Session Store**

* Shared CNPG cluster stores a `sessions` table keyed by `session_uuid`.
* Tracks PTY metadata, resume/fork lineage, and rolling log offsets.
* Credentials are injected via Kubernetes Secrets/env vars managed by the Helm chart.
* Retains historical sessions to support `--resume <id>` selection UI.

#### 4. **Pod Deployment**

* Part of the dev container image.
* Runs on internal port, e.g., `:8080`.
* Includes CLI tooling (kubectl, helm, docker, go, node/npm, git, jq, yq, curl/httpie, ripgrep, tmux, make, psql, Claude/Codex/Gemini CLIs, etc.) plus `/etc/profile.d/claude.sh` installing the `claude_with_log` helper.

---

## 4. Detailed Implementation Plan

### 4.1 Backend (Go)

#### 4.1.1 Endpoints

| Endpoint | Method        | Purpose                      |
| -------- | ------------- | ---------------------------- |
| `/`      | GET           | Serves React static files    |
| `/ws`    | GET (upgrade) | Main WebSocket shell session |

#### 4.1.2 PTY Management

* Create PTY with `pty.Start()`
* Use `/bin/bash` or `$SHELL`
* On WS open, attach to PTY:

  * PTY → WS loop
  * WS → PTY loop
* On client reconnect:

  * Preferred: reattach to the existing PTY kept alive in memory + CNPG metadata.
  * Fallback: spawn a new shell (logged as a fork) if the PTY exited.

**Decision:**
Use **direct PTY** for v1 (simplest) but persist the PTY handle + metadata in CNPG so the server can reattach without tmux.

#### 4.1.3 Session Registry (CNPG)

* Table schema (minimum): `session_uuid UUID PRIMARY KEY`, `deployment_id TEXT`, `shell_pid INT`, `created_at TIMESTAMPTZ`, `updated_at TIMESTAMPTZ`, `state JSONB`, `log_path TEXT`, `forked_from UUID`.
* On startup, the backend receives `SESSION_ID` (from Helm value). It queries CNPG:
  * If a live PTY exists for that `session_uuid`, the server reattaches and marks it active.
  * If not, it spawns a new shell, stores metadata, and begins streaming logs.
* `--continue` maps to “select most recent session by `updated_at` for this deployment”.
* `--resume <uuid>` fetches that session row; if active, reattach, otherwise display a warning and offer `--fork-session`.
* `--fork-session` clones metadata, creates a fresh PTY, and writes a new UUID row linked via `forked_from`.

#### 4.1.4 Reconnection & WS Handshake

* REST endpoint `GET /session` returns `{ sessionUUID, availableSessions[] }`.
* Frontend (or CLI) chooses a `sessionUUID` and opens `GET /ws?session=<uuid>&mode=resume|fork|new`.
* Server verifies single attachment:
  * If another client is already attached, respond with HTTP 409 instructing the user to fork.
  * If allowed, mark the row as “attached” (timestamp + client info) until disconnect.
* When the WebSocket closes unexpectedly, the PTY remains alive; the server clears the attachment flag but keeps the PTY handle in memory for the next resume.
* Logs are streamed via the same WS channel; CNPG only stores metadata and historical log pointers (not full transcripts).
* tmux remains an optional enhancement for v2, but v1 must already support resume/fork through the registry above.

#### 4.1.5 Security & Network

* No external access.
* Only run behind:

  * k8s port-forward
  * or cluster internal network + NetworkPolicy

No tokens, no login, no HTTPS (unless behind ingress); ingress basic auth is intentionally omitted to avoid interfering with CLI/WebSocket flows.

#### 4.1.6 CLI Helpers & Logging

* Ship `/etc/profile.d/claude.sh` containing the `claude_with_log` function plus alias `c`.
* Function uses `script -q -c "~/.local/bin/claude --dangerously-skip-permissions"` and stores logs under `$HOME/claude_logs/claude_interactive_session_<timestamp>.log`.
* Helm chart injects env vars such as `CLAUDE_CODE_MAX_OUTPUT_TOKENS` and `ANTHROPIC_BASE_URL`; no defaults baked into the image.

### 4.2 Frontend (React)

#### Key UI components:

* xterm.js terminal with full ANSI handling, resize support, and clipboard integration.
* Session picker that lists `availableSessions[]` returned by `/session` (includes resume/fork controls).
* Connection indicator with reconnection countdown + retry button.
* Minimal dark theme aligned with Claude Code palette.

#### Actions:

* Call `/session` on load to fetch deployment’s default session UUID and the list of historical sessions.
* On resume/fork selection, open `ws://.../ws?session=<uuid>&mode=<mode>` and wire xterm.js to the socket.
* Buffer outbound keystrokes while reconnecting to avoid dropping user input.
* Persist the last-used session UUID in `localStorage` so `--continue` semantics match Claude CLI expectations.

### 4.3 Build & Deploy Steps

1. Run `npm --prefix web install && npm --prefix web run build` locally to produce `web/build/`.
2. Run `go build ./server` (or `make build`) to embed the static assets into the Go binary.
3. Build the Docker image locally (`docker build -t harbor.support.tools/kubetty/<repo>:<tag> .`) including all CLI tools and `/etc/profile.d/claude.sh`.
4. Push manually to Harbor (`docker push harbor.support.tools/kubetty/<repo>:<tag>`); no GitHub/remote CI is involved.
5. Use the Helm chart to deploy, injecting:
   * `SESSION_ID` (UUID per deployment, also used by default for `--continue`)
   * CNPG connection info + credentials (Secret → env vars)
   * `CLAUDE_CODE_MAX_OUTPUT_TOKENS`, `ANTHROPIC_BASE_URL`, and any LLM endpoints
   * Namespace/context details that will also exist in `~/.kube/config`
6. Port-forward the resulting pod (`kubectl port-forward pod/<deployment>-0 8080:8080`) from a workstation on the private network and browse `http://localhost:8080`.

---

## 5. Multi-Project Gateway Mode

The single-session deployment model remains the default, but we also need a "hub" experience where https://kubetty.support.tools exposes a tabbed UI and fans out to multiple project-specific pods that keep their own PTY/process space. This section covers that additional architecture.

### 5.1 Motivation & Constraints

* Each project already runs its own KubeTTY pod (and usually its own namespace) with local tooling and state we do not want to co-mingle.
* Engineers want to jump between projects through one browser tab and spawn multiple shells at once.
* `kubectl exec`/`pods/exec` is **not** acceptable for the hub because the Kubernetes API server throttles long-running streams; we need stable, low-latency tunnels that can stay up for hours.

### 5.2 High-Level Architecture

```
Browser (tabbed UI)
     |
     | wss://kubetty.support.tools/ws?tab=<id>
     v
Gateway Pod (shared namespace)
     |
     | (cluster-internal WebSocket relay)
     v
Project Pods (one per namespace, each exposing /ws via ClusterIP Service)
```

*Gateway responsibilities*

1. Serve the React bundle and tab UX.
2. Host REST APIs: `/api/projects`, `/api/tabs`, `/api/health`.
3. Maintain a catalog of known projects (either static config or discovery via labels/annotations) including namespace + Service DNS for their `/ws` endpoint.
4. For every open browser tab, hold a downstream WebSocket to the selected project service and relay frames between the browser and downstream PTY. No PTY is created in the gateway itself.
5. Track per-tab state (project ID, downstream status, retries, metrics) in memory and optionally in CNPG so reconnects survive gateway restarts.

*Project pod responsibilities*

1. Continue to run the existing single-session server (PTY, CNPG logs, local toolchain).
2. Publish `/ws` via a Service that only the gateway namespace can reach (enforced by NetworkPolicy).
3. Optionally expose readiness probes that the gateway can poll for health information.

### 5.3 Connection Lifecycle

1. User clicks "+" in the tab bar; the UI fetches `/api/projects` to list options.
2. UI POSTs `/api/tabs` with `{ projectId }`. Gateway validates the ID, resolves the service (e.g., `http://kubetty-proj-a.kubetty-proj-a.svc:8080/ws`), opens a downstream WebSocket, and records a new `tabId`.
3. Browser opens `wss://gateway/ws?tab=<tabId>`; gateway simply proxies frames. Resize/ping payloads remain unchanged because the downstream pods are unaware of the gateway.
4. If the downstream connection drops, the gateway emits a structured event to the browser so the tab can show "Reconnecting…" and optionally trigger a retry on behalf of the user.
5. Closing a tab tears down the downstream WebSocket and marks the tab as closed, freeing resources.

### 5.4 Tabbed UI Enhancements

* New React components manage tab state, persistence (localStorage), and a `ProjectPicker` modal.
* Each `<TerminalView>` instance receives its own `tabId` + status, keeping the existing reconnect/ping logic isolated per tab.
* Visual indicators show which project a tab targets (namespace badge, icon, etc.).

### 5.5 Observability & Security

* Metrics: gateway exports per-project tab counts, downstream latency, error ratios, and reconnect counts.
* Logging: every tab open/close includes project ID, namespace, and downstream pod for auditing.
* Security: gateway ServiceAccount only needs network access (enforced via NetworkPolicy) rather than `pods/exec`. Any future auth (mTLS, header token) is centralized at the gateway.

### 5.6 Implementation Checklist

1. **Config plumbing** – extend `config.Config` with a `Projects` list sourced from env var or mounted YAML.
2. **Gateway server** – add `/api/projects` + `/api/tabs` handlers, downstream WebSocket dialer, connection registry, and metrics.
3. **React tab UX** – tab reducer, picker modal, multi-`TerminalView` layout, and Vitest coverage.
4. **Project services** – ensure each project pod exposes `/ws` via ClusterIP and that NetworkPolicies allow only the gateway namespace.
5. **Docs & ops** – update README/Helm notes about onboarding a new project (add entry to catalog, ensure Service DNS, redeploy gateway).

### 5.7 Configuration Schema

Gateway pods mount a ConfigMap or secret-backed file, e.g. `projects.yaml`:

```yaml
projects:
  - id: kubetty-ai
    displayName: "AI Platform"
    namespace: kubetty-ai
    service: kubetty-ai-kubetty
    port: 8080
    description: "Claude tooling & infra pods"
    icon: ai
    tags: ["beta", "llm"]
    healthCheckPath: /healthz      # optional HTTP probe
    maxTabsPerUser: 4              # optional overrides
```

Rules:

* `id` must be DNS-safe; used in URLs and metrics labels.
* `service` resolves to `<service>.<namespace>.svc`. `port` defaults to 8080.
* Optional overrides (shell, env vars) can be added later. Config loader merges defaults with per-project overrides.
* Config reload: simplest is restart-on-change, but we can watch the file and hot-reload with a RWMutex.

### 5.8 API Contracts

* `GET /api/projects` → `{ projects: ProjectSummary[] }`, where each summary includes health info (`status: online|degraded|offline`, `lastCheckedAt`).
* `POST /api/tabs` with `{ projectId, title? }` → `{ tabId, projectId, wsUrl }` (browser still connects via `/ws`, but `wsUrl` helps future native clients).
* `GET /api/tabs` → list of open tabs scoped to the current browser (identified via cookie/short-lived token) or to the deployment.
* `DELETE /api/tabs/{tabId}` closes the tunnel.
* `WS /ws?tab=<tabId>` – binary data is passthrough PTY bytes; text frames use JSON for control events:
  * `{ "type": "status", "state": "connecting" | "connected" | "reconnecting" | "closed", "reason"?: string }
  * `{ "type": "project-status", "status": "offline", "projectId": "kubetty-ai" }`

### 5.9 Failure Handling

* Downstream connect failure → immediate structured status event; UI shows retry button.
* Gateway retries with exponential backoff (e.g., 1s, 2s, 5s, 10s, capped) while user keeps tab open.
* If the downstream pod restarts, gateway detects `CloseAbnormalClosure` or EOF and triggers the retry loop.
* Idle timeout: configurable `TAB_IDLE_TIMEOUT` (default 2h). Gateway closes idle tunnels and notifies UI.
* Max tabs per user/project: enforce limits to protect downstream resources; respond with HTTP 429 + reason.

### 5.10 Persistence & Identity

* Tabs are keyed by UUIDv4. For now, identity is just a client-generated cookie (e.g., `kubetty_client=<uuid>`). Later we can integrate with real auth.
* Store `{ tab_id, project_id, client_id, created_at, last_seen_at, status }` in CNPG; reuse `sessions` schema or create `gateway_tabs` for clarity.
* On gateway restart, reload open tabs from CNPG, attempt to re-establish downstream connections, and emit status events so browsers can reattach.

### 5.11 Metrics & Alerting

* Counters: `gateway_tabs_open{project="kubetty-ai"}`, `gateway_tab_spawns_total`, `gateway_downstream_retries_total`.
* Histograms: downstream connection latency, bytes transferred per tab.
* Alerts:
  * Project offline for >5 minutes.
  * Tab spawn failures >5 within 1 minute.
  * Gateway memory usage nearing limits (since each downstream WS consumes buffers).

### 5.12 Security Notes

* NetworkPolicy ensures only gateway namespace → project namespace traffic on port 8080.
* Consider mutual TLS between gateway and project pods (optional) by issuing shared certificates.
* Audit logs record `{ timestamp, clientId, tabId, projectId, action }` for compliance.

### 5.13 Data Model & Persistence

**Projects** (in-memory, config-driven)

```go
type Project struct {
    ID            string
    DisplayName   string
    Namespace     string
    Service       string
    Port          int
    Description   string
    Icon          string
    Tags          []string
    HealthCheck   *HealthConfig
    Limits        ProjectLimits
}
```

**Tabs** (persisted in CNPG)

| Column         | Type        | Notes                                      |
|----------------|-------------|--------------------------------------------|
| tab_id         | UUID PK     | Generated by gateway                       |
| project_id     | TEXT        | FK to configured project                   |
| client_id      | TEXT        | Opaque browser identifier                  |
| status         | TEXT        | `connecting`/`connected`/`reconnecting`    |
| created_at     | TIMESTAMPTZ |                                            |
| updated_at     | TIMESTAMPTZ | refreshed on activity                      |
| last_error     | TEXT        | most recent failure reason                 |
| downstream_uri | TEXT        | cached Service endpoint (for debugging)    |

Add migration `0003_gateway_tabs` with indexes on `(project_id)` and `(client_id)`.

### 5.14 Package Layout (Gateway)

```
server/
  internal/
    gateway/
      config/        # load & validate projects catalog
      relay/         # downstream WS dialer + lifecycle
      tabs/          # persistence & business logic
    api/
      projects.go    # /api/projects handler
      tabs.go        # /api/tabs CRUD
      ws.go          # WS proxy handler
```

Each relay instance owns two goroutines (read/write) with contexts for cancellation.

### 5.15 Sequence Diagrams

**Tab Creation**

1. Browser POST `/api/tabs` with `{ projectId }`.
2. Gateway validates limits, persists `tab` row (status `connecting`).
3. Gateway dials downstream `/ws`; on success updates status → `connected` and replies `{ tabId }`.
4. Browser opens `wss://gateway/ws?tab=<tabId>`; gateway joins read/write pumps immediately, sending buffered data if downstream already producing output.

**Reconnect**

1. Browser loses socket; `TerminalView` retries and reopens `wss://.../ws?tab=<id>`.
2. Gateway verifies client ownership (via cookie header) before attaching; if mismatch, respond 403.
3. Downstream connection reused if still alive; otherwise gateway attempts to re-dial before confirming to browser (emitting status events meanwhile).

**Project Pod Restart**

1. Downstream `/ws` closes (CloseAbnormalClosure).
2. Relay sets tab status `reconnecting`, notifies browsers, and enters retry loop.
3. When downstream `/ws` is reachable again, relay swaps the connection and emits `connected` status; PTY output resumes.

### 5.16 Scaling Considerations

* Each tab consumes ~a few goroutines + buffers; cap max tabs per gateway pod (e.g., 100) and use HPA for load.
* For HA, run ≥2 gateway replicas; use sticky cookies so browsers stay attached to the same pod, or persist downstream connection info in Redis to allow shared ownership (future work).
* If a project scales to multiple pods, consider adding a lightweight service within the namespace that multiplexes to the session pod, or extend the gateway to discover pods via labels and pick one dynamically.

### 5.17 Runtime Configuration

* `PROJECT_CATALOG_PATH` (env var) points to the mounted YAML/JSON catalog. When unset, gateway features are disabled and the app falls back to the legacy single-session mode.
* Gateway stores tab metadata in the existing CNPG via the `gateway_tabs` table. Helm values should inject the same CNPG credentials as the project pods.
* The server issues a `kubetty_client` cookie (opaque UUID) to associate tabs with a browser. Tabs are scoped per client; `/api/tabs` returns only rows for the requester.
* `/api/tabs` responses include the tab metadata; the client computes the WebSocket URL as `wss://<host>/ws?tab=<id>`.

---

## 6. File Structure

```
kube-tty/
│
├── server/
│   ├── go.mod
│   ├── main.go
│   └── internal/
│       └── sessions/        # CNPG registry + resume logic
│
└── web/
    ├── package.json
    ├── public/
    └── src/
        ├── index.tsx
        ├── App.tsx
        └── components/
            ├── TerminalView.tsx
            └── SessionPicker.tsx

deploy/
└── helm/
    ├── Chart.yaml
    ├── values.yaml          # session UUID, CNPG creds, env vars
    └── templates/
        ├── deployment.yaml
        ├── secret.yaml
        └── service.yaml

scripts/
└── claude_with_log.sh       # installs the alias during image build
```

---

## 7. Code Summary

### Go backend

Already provided; engineer just needs to copy the `main.go` and `go.mod`, then add the `internal/sessions` package for CNPG access plus the `/session` endpoint, resume/fork logic, and env var wiring (`SESSION_ID`, `CNPG_*`, `CLAUDE_*`).

### React frontend

Already provided; engineer places in `web/` folder, installs deps, and ensures xterm.js + SessionPicker components are enabled by default:

```
npm install
npm run build
```

### Shell tooling

Include `scripts/claude_with_log.sh` in the image build to install the logging alias and confirm that `~/.local/bin/claude`, Codex CLI, Gemini CLI, kubectl, helm, docker, go, npm/node, git, jq, yq, curl/httpie, ripgrep, fd, tmux, make, python3, pip, psql, and common LLM-support utilities are available.

---

## 8. Kubernetes Integration

* **Image build:** everything (Go binary, React build, CLI tools) is baked into a single image that engineers build locally and push to `harbor.support.tools/kubetty/<repo>:<tag>`.
* **Helm release:** deployments are managed solely through a private Helm chart kept in this repo; no GitHub Actions or external registry integrations.
* **Namespaces/contexts:** engineers create/update namespaces manually via `~/.kube/config` before installing the chart.
* **CNPG:** all deployments share the same CNPG cluster; connection strings + credentials are injected through Helm values and projected into the pod as environment variables.
* **Session UUID:** each deployment gets a fixed `SESSION_ID` Helm value; if a pod restarts it reuses that UUID to resume the same PTY automatically.
* **Multiple deployments:** running Project A and Project B simultaneously simply means deploying separate Helm releases (with different namespaces, session IDs, and image tags) pointing at the shared CNPG instance.

---

## 9. Security Considerations

### Must have:

* **Private-only** deployment: pods live on non-routable IP ranges; access is via `kubectl port-forward` or direct connectivity from a workstation on the same network.
* **NetworkPolicy + firewall** rules restrict ingress to trusted CIDRs; no Internet exposure.
* **No ingress auth/basic auth:** avoided intentionally to keep PTY/WebSocket flows simple for CLI tools.
* **Secrets outside Git:** CNPG credentials, LLM API endpoints, and env vars (`CLAUDE_CODE_MAX_OUTPUT_TOKENS`, `ANTHROPIC_BASE_URL`, etc.) live in Helm values/Secrets. See **[SECRETS.md](./SECRETS.md)** for comprehensive secret management documentation including setup, rotation, and security best practices.

### Optional future security:

* Header-based auth token
* mTLS between ingress and pod
* OAuth or identity-aware proxy
* Role-based session controls with audit logging

---

## 10. Acceptance Criteria

An engineer is **finished** when:

### Backend

* [ ] `/` serves the React app and `/ws` provides a PTY-backed WebSocket.
* [ ] `/session` returns the deployment’s default UUID plus resume/fork metadata pulled from CNPG.
* [ ] PTY metadata (session UUID, PID, timestamps) persists in CNPG and survives pod restarts.
* [ ] Resume/fork logic enforces single-client-per-session and matches Claude CLI semantics (`--continue`, `--resume <id>`, `--fork-session`, `--session-id`).
* [ ] Environment variables (`SESSION_ID`, `CNPG_*`, `CLAUDE_*`) are read at startup and validated.

### Frontend

* [ ] xterm.js renders the PTY output with correct colors, cursor control, and resizing.
* [ ] SessionPicker lists available sessions, supports resume/fork, and persists the last-used UUID locally.
* [ ] Connection indicator surfaces attach/detach events and auto-retries after transient drops.

### Deployment

* [ ] Docker image includes Go server, React build, CNPG client, and required tooling (kubectl, helm, docker, go, node/npm, git, jq, yq, curl/httpie, ripgrep, fd, tmux, make, python3/pip, psql, Claude/Codex/Gemini CLIs).
* [ ] `/etc/profile.d/claude.sh` installs the `claude_with_log` alias and creates logs under `$HOME/claude_logs`.
* [ ] Helm chart values inject CNPG credentials, `SESSION_ID`, and LLM env vars; deploying to a namespace referenced in `~/.kube/config` yields a working pod.
* [ ] Local build/push workflow to `harbor.support.tools/kubetty/<repo>:<tag>` is documented and verified.

### Quality

* [ ] Code remains small (<600 LoC backend) and well-documented.
* [ ] README/AGENTS mention session persistence flow, local build instructions, and required environment variables.

---

## 11. Extras for Phase 2 (optional but easy)

* Integrate tmux as the PTY backend for richer multi-window workflows.
* Stream command transcripts into CNPG or object storage for searchable history/auditing.
* Add multi-session dashboard (even if still single-user) to quickly fork/archive sessions.
* Provide Kubernetes job templates to launch ephemeral helper pods from within the UI.
* Explore lightweight auth (header token or OAuth) if exposure ever leaves the private network.

---

## 12. Deliverables to Engineer

This doc and:

* Provided Go backend code (`main.go` + `go.mod`) plus `internal/sessions` stub.
* React frontend (`App.tsx`, `TerminalView.tsx`, `SessionPicker.tsx`) pre-wired with xterm.js.
* Helm chart (`deploy/helm`) with sample values for `SESSION_ID`, CNPG creds, LLM env vars.
* `scripts/claude_with_log.sh` and documentation on local image build + Harbor push.
* Instructions for configuring CNPG, namespaces, and `~/.kube/config`.

Everything above remains intentionally minimal and focused on single-user pods.

---
