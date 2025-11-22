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
3. Single client per session enforced (second connection rejected until first disconnects).
4. Ability to disconnect and reconnect without killing the underlying shell, backed by a long-lived PTY whose metadata lives in CNPG.
5. Backend assigns a stable session UUID per deployment (via `SESSION_ID` env var) and persists PTY metadata in CNPG. One session per pod is reused for the lifetime of the pod.
6. Backend serves the React frontend statically.
7. Access only via:

   * Kubernetes port-forward **OR**
   * Internal cluster network (no public exposure).
8. Built-in local-user authentication protects every HTTP/WebSocket endpoint via CNPG-backed credentials and short-lived JWTs; no external IdP dependency.
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
* Handles terminal resizing via JSON control messages.
* Persists session metadata (session UUID, shell PID, timestamps, log pointers) in CNPG.
* Enforces single-client-per-session by rejecting additional connections until the first client disconnects.

#### 2. **React Frontend**

* Displays terminal output using **xterm.js** from the start for full PTY fidelity (colors, cursor control, alternate screen).
* Captures user keystrokes and sends them to backend.
* Shows connection status with automatic reconnection on disconnect.
* Auto-scrolls output.
* Connects directly to `/ws` endpoint; the backend manages the single PTY per pod.
* Gated by an auth-aware bootstrap: call `/api/auth/me`, show login form (username/password) until authenticated, then render the existing terminal UI; include logout action and automatic refresh-token handling when fetches receive 401.

#### 3. **CNPG Session Store**

* Shared CNPG cluster stores a `sessions` table keyed by `session_uuid`.
* Tracks PTY metadata (session UUID, deployment ID, timestamps) and session logs.
* Credentials are injected via Kubernetes Secrets/env vars managed by the Helm chart.
* One session per deployment is upserted and reused for the pod lifetime.

#### 4. **Pod Deployment**

* Part of the dev container image.
* Runs on internal port, e.g., `:8080`.
* Includes CLI tooling (kubectl, helm, docker, go, node/npm, git, jq, yq, curl/httpie, ripgrep, tmux, make, psql, Claude/Codex/Gemini CLIs, etc.) plus `/etc/profile.d/claude.sh` installing the `claude_with_log` helper.

#### 5. **Authentication Layer**

* Stores local-user credentials in CNPG (new `kubetty_users` + `kubetty_refresh_tokens` tables).
* Hashes passwords with bcrypt (cost 12) and rotates refresh tokens on each login/refresh.
* Issues short-lived access JWTs (HMAC-SHA256) plus long-lived refresh tokens (Secure, HttpOnly cookies).
* Adds `/api/auth` endpoints (login, refresh, logout, whoami) and middleware that protects all other routes (HTTP + WS/SSE) by validating JWTs.
* Ships CLI tooling (`kubetty-authuser`) so operators can create/update users without manual SQL.

---

## 4. Detailed Implementation Plan

### 4.1 Backend (Go)

#### 4.1.1 Endpoints

| Endpoint          | Method        | Purpose                                    |
| ----------------- | ------------- | ------------------------------------------ |
| `/`               | GET           | Serves React static files                  |
| `/ws`             | GET (upgrade) | Main WebSocket shell session               |
| `/api/auth/login` | POST          | Username/password auth, issues JWT cookies |
| `/api/auth/me`    | GET           | Returns current user info (SPA bootstrap)  |
| `/api/auth/refresh` | POST        | Rotates refresh token + access JWT         |
| `/api/auth/logout` | POST         | Revokes refresh token + clears cookies     |

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

* Table schema: `session_uuid UUID PRIMARY KEY`, `deployment_id TEXT`, `created_at TIMESTAMPTZ`, `updated_at TIMESTAMPTZ`.
* Session logs stored in `session_logs` table with PTY transcript data.
* On startup, the backend receives `SESSION_ID` (from Helm value/env var). It upserts a session row in CNPG.
* On first WebSocket connection, the server initializes the PTY and keeps it alive for the pod lifetime.
* The same PTY is reused for all subsequent connections from that deployment.
* Output is buffered (64KB) so new clients receive initial output (MOTD) on connect.

#### 4.1.4 Reconnection & WS Handshake

* Frontend connects directly to `GET /ws` (no session parameter needed).
* Server enforces single client per session:
  * If another client is already connected, respond with HTTP 409 or WebSocket close with reason.
  * New connections must wait until the existing client disconnects.
* On successful connection:
  * Server sends buffered output (MOTD, recent history) immediately.
  * Bidirectional PTY streaming begins.
* When the WebSocket closes unexpectedly, the PTY remains alive and the server accepts reconnection.
* Session logs (PTY transcripts) are stored in CNPG for auditing and can be retrieved via `GET /session/logs`.

#### 4.1.5 Security & Network

* Default deployment remains private (port-forward or cluster-internal Service guarded by NetworkPolicy).
* All browser + CLI traffic is authenticated:

  * `/api/auth/login` verifies bcrypt hashes stored in CNPG and issues JWTs.
  * Access tokens expire quickly (default 15m) while refresh tokens last longer (30d) and are revocable per record.
  * Middleware validates JWTs for HTTP/WebSocket/SSE before handing off to business logic.
* Tokens flow via Secure, HttpOnly cookies; HTTPS (or TLS-terminating port-forward) is required whenever auth is enabled.
* Prometheus/expvar endpoints can remain unauthenticated (configurable) so monitoring keeps working, but default stance is “auth everywhere”.

#### 4.1.6 CLI Helpers & Logging

* Ship `/etc/profile.d/claude.sh` containing the `claude_with_log` function plus alias `c`.
* Function uses `script -q -c "~/.local/bin/claude --dangerously-skip-permissions"` and stores logs under `$HOME/claude_logs/claude_interactive_session_<timestamp>.log`.
* Helm chart injects env vars such as `CLAUDE_CODE_MAX_OUTPUT_TOKENS` and `ANTHROPIC_BASE_URL`; no defaults baked into the image.

#### 4.1.7 Authentication Workflow

* **Schema**: add CNPG migrations for `kubetty_users` (uuid pk, username citext unique, password_hash bytea, timestamps, is_active) and `kubetty_refresh_tokens` (uuid pk, token_id uuid unique, user_id fk, expires_at, revoked_at, metadata).
* **Config**: env vars `AUTH_MODE`, `AUTH_JWT_SECRET`, `AUTH_ACCESS_TTL`, `AUTH_REFRESH_TTL`, `AUTH_ISSUER`, `AUTH_COOKIE_DOMAIN`, `AUTH_COOKIE_SECURE`. Server refuses to boot if auth enabled but secret missing.
* **Login**: `POST /api/auth/login` accepts `{username,password}`, verifies bcrypt hash, creates access/refresh pair, and stores refresh metadata; also records Prometheus counters.
* **Refresh**: `POST /api/auth/refresh` validates refresh token row + embedded token id, rotates it, and re-issues JWTs.
* **Logout**: `POST /api/auth/logout` revokes refresh token (delete or set `revoked_at`) and clears cookies.
* **Session check**: `GET /api/auth/me` returns `{user}` and is used by the SPA to decide whether to show the login form; returns 401 otherwise.
* **Middleware**: wraps every handler (except `/api/auth/*`, `/metrics`, `/debug/vars`). Extracts bearer token or cookie, validates signature/expiry/issuer, loads user info (cached by ID) for downstream logging, and rejects unauthorized requests with 401.
* **CLI tooling**: a small Go program (`go run ./server/cmd/kubetty-authuser create --username ... --password ...`) calls the same auth package to create/update users via CNPG. Include docs in README.

### 4.2 Frontend (React)

#### Key UI components:

* xterm.js terminal with full ANSI handling, resize support, and clipboard integration.
* Connection indicator with reconnection countdown + automatic retry.
* Login form (when auth enabled) with logout action in header.
* Minimal dark theme aligned with Claude Code palette.

#### Actions:

* On load, call `/api/auth/me` to check authentication status; show login form if needed.
* Connect directly to `ws://.../ws` and wire xterm.js to the socket.
* Automatic reconnection with exponential backoff on disconnect.
* Send terminal resize events to backend when terminal dimensions change.

### 4.3 Build & Deploy Steps

1. Run `npm --prefix web install && npm --prefix web run build` locally to produce `web/build/`.
2. Run `go build ./server/cmd/gateway && go build ./server/cmd/project` (or `make build-server-local`) to build both binaries.
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

### 5.18 Tab Resource Metrics

The gateway collects and displays real-time resource metrics for each open tab, providing visibility into CPU, memory, disk, and network usage of the underlying project pods.

#### 5.18.1 Data Sources

Metrics are collected from two sources:

1. **Kubernetes Metrics API** (`metrics.k8s.io`) - Provides CPU and memory usage/limits for pods
2. **Project Pod `/api/metrics` Endpoint** - Provides disk and network metrics from within the container

#### 5.18.2 Metrics Types

```go
type TabMetrics struct {
    CPU       ResourceMetric `json:"cpu"`
    Memory    ResourceMetric `json:"memory"`
    Disk      ResourceMetric `json:"disk"`
    Network   NetworkMetric  `json:"network"`
    UpdatedAt time.Time      `json:"updatedAt"`
}

type ResourceMetric struct {
    Usage   int64 `json:"usage"`   // Current usage (millicores for CPU, bytes for memory/disk)
    Limit   int64 `json:"limit"`   // Limit/capacity in same units
    Percent int   `json:"percent"` // Usage percentage (0-100)
}

type NetworkMetric struct {
    RxBytes int64 `json:"rxBytes"` // Total bytes received
    TxBytes int64 `json:"txBytes"` // Total bytes transmitted
    RxRate  int64 `json:"rxRate"`  // Receive rate in bytes/sec
    TxRate  int64 `json:"txRate"`  // Transmit rate in bytes/sec
}
```

#### 5.18.3 Collection Architecture

```
Gateway Pod
    │
    ├── MetricsCollector (background goroutine)
    │       │
    │       ├── K8s Metrics API ──► CPU/Memory for each tab's pod
    │       │
    │       └── HTTP GET /api/metrics ──► Disk/Network from project pod
    │
    └── WebSocket Event Broadcaster
            │
            └── { type: "metrics", tabId, metrics } ──► Browser
```

#### 5.18.4 Project Pod Metrics Endpoint

Each project pod exposes `GET /api/metrics` returning:

```json
{
  "disk": { "usage": 1073741824, "limit": 53687091200, "percent": 2 },
  "network": { "rxBytes": 123456789, "txBytes": 987654321 }
}
```

Implementation:
- Disk: Uses `syscall.Statfs("/")` to get filesystem usage
- Network: Parses `/proc/net/dev` to sum interface bytes (excluding `lo`)

#### 5.18.5 UI Components

**MetricsIndicator** (in tab header)
- Four color-coded dots representing CPU, MEM, DISK, NET
- Colors: green (0-60%), yellow (60-80%), red (80-100%)
- Hover tooltips show exact percentage

**StatusBar** (below tab bar)
- Progress bars for CPU, Memory, Disk with percentages
- Network rates (↓ RX / ↑ TX) in human-readable format
- Only shown when a tab is active

#### 5.18.6 Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `METRICS_ENABLED`   | `true`  | Enable/disable metrics collection |
| `METRICS_INTERVAL`  | `15s`   | How often to collect metrics |

#### 5.18.7 Event Protocol

Metrics updates are broadcast via the existing tab events WebSocket:

```json
{
  "type": "metrics",
  "tabId": "550e8400-e29b-41d4-a716-446655440000",
  "metrics": {
    "cpu": { "usage": 250, "limit": 1000, "percent": 25 },
    "memory": { "usage": 536870912, "limit": 2147483648, "percent": 25 },
    "disk": { "usage": 1073741824, "limit": 53687091200, "percent": 2 },
    "network": { "rxBytes": 123456789, "txBytes": 987654321, "rxRate": 1024, "txRate": 512 },
    "updatedAt": "2025-01-15T10:30:00Z"
  }
}
```

#### 5.18.8 RBAC Requirements

Gateway ServiceAccount needs access to `metrics.k8s.io` API:

```yaml
- apiGroups: ["metrics.k8s.io"]
  resources: ["pods"]
  verbs: ["get", "list"]
```

#### 5.18.9 Package Layout

```
server/internal/gateway/
    metrics/
        types.go      # TabMetrics, ResourceMetric, NetworkMetric
        collector.go  # Background collection, K8s client, HTTP fetcher
```

---

## 6. Single-Namespace Project Controller

The gateway controller dynamically creates and manages project pods within a **shared namespace** per environment, rather than creating separate namespaces for each project. This simplifies resource management, monitoring, and RBAC while maintaining environment isolation.

### 6.0 Design Evolution

This section describes the **recommended shared-namespace approach** for controller-managed projects, which supersedes the per-project namespace model referenced in Section 5.

**Relationship to Section 5 (Multi-Project Gateway Mode):**

| Aspect | Section 5 (Gateway Mode) | Section 6 (Controller Model) |
|--------|--------------------------|------------------------------|
| Scope | Gateway WebSocket routing | Project lifecycle management |
| Namespace model | Per-project (legacy) | Shared per environment (recommended) |
| Project creation | Manual Helm releases | Automated via Admin API |
| Resource management | External | Controller-managed |

**When to use which model:**

* **Section 6 (Recommended)**: New deployments using the Admin API to dynamically create projects
* **Section 5 patterns**: Legacy deployments with manually-managed project Helm releases

**Migration path**: Existing per-namespace projects can continue operating. New projects created via the Admin API will use the shared-namespace model. Both models can coexist in the same cluster.

### 6.1 Motivation & Design Goals

* **Simplified namespace management**: Avoid proliferation of namespaces (one per project)
* **Centralized resource quotas**: Apply limits at the shared namespace level
* **Easier monitoring**: Single namespace to watch for all project pods
* **Environment isolation**: Dev and prod projects remain in separate namespaces
* **Reduced RBAC complexity**: Fewer namespace-scoped roles to manage

### 6.2 Architecture Overview

```
Admin API Request
      │
      │ POST /api/admin/projects { name: "beacon", ... }
      v
┌─────────────────────────────────────────────────────────────────┐
│                    Gateway Pod                                   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Project Controller (reconciliation loop)                  │   │
│  │                                                           │   │
│  │  • Watches projects table for pending/creating/deleting  │   │
│  │  • Creates/updates/deletes K8s resources                 │   │
│  │  • Polls health endpoints for running projects           │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
      │
      │ Creates resources
      v
┌─────────────────────────────────────────────────────────────────┐
│              Shared Namespace: kubetty-projects-{env}           │
│                                                                  │
│  ┌─────────────────────┐  ┌─────────────────────┐               │
│  │ kubetty-project-    │  │ kubetty-project-    │               │
│  │ beacon (Deployment) │  │ alpha (Deployment)  │  ...          │
│  │                     │  │                     │               │
│  │ kubetty-project-    │  │ kubetty-project-    │               │
│  │ beacon (Service)    │  │ alpha (Service)     │               │
│  └─────────────────────┘  └─────────────────────┘               │
└─────────────────────────────────────────────────────────────────┘
      │
      │ Cluster-scoped RBAC (with env suffix)
      v
┌─────────────────────────────────────────────────────────────────┐
│                    Cluster-Scoped Resources                      │
│                                                                  │
│  ClusterRole: kubetty-project-beacon-admin-dev                  │
│  ClusterRoleBinding: kubetty-project-beacon-admin-dev           │
│                                                                  │
│  (Grants cross-namespace access to configured namespaces)       │
└─────────────────────────────────────────────────────────────────┘
```

### 6.3 Namespace Model

Projects deploy to environment-specific shared namespaces:

| Environment | Namespace | Usage |
|-------------|-----------|-------|
| Development | `kubetty-projects-dev` | Development and testing |
| Production | `kubetty-projects-prd` | Production workloads |

**Environment Detection**: The controller parses the environment from the namespace suffix:
```go
// kubetty-projects-dev → "dev"
// kubetty-projects-prd → "prd"
func ParseEnvironment(namespace string) string {
    parts := strings.Split(namespace, "-")
    return parts[len(parts)-1]
}
```

### 6.4 Resource Naming Convention

All resources use a consistent naming pattern to avoid collisions in the shared namespace:

#### Namespaced Resources (no environment suffix needed)

| Resource | Pattern | Example |
|----------|---------|---------|
| Deployment | `kubetty-project-{name}` | `kubetty-project-beacon` |
| Service | `kubetty-project-{name}` | `kubetty-project-beacon` |
| PVC | `kubetty-project-{name}-data` | `kubetty-project-beacon-data` |
| ServiceAccount | `kubetty-project-{name}-sa` | `kubetty-project-beacon-sa` |

#### Cluster-Scoped Resources (include environment suffix)

| Resource | Pattern | Example (dev) | Example (prd) |
|----------|---------|---------------|---------------|
| ClusterRole (admin) | `kubetty-project-{name}-admin-{env}` | `kubetty-project-beacon-admin-dev` | `kubetty-project-beacon-admin-prd` |
| ClusterRole (read) | `kubetty-project-{name}-read-{env}` | `kubetty-project-beacon-read-dev` | `kubetty-project-beacon-read-prd` |
| ClusterRoleBinding | `kubetty-project-{name}-admin-{env}` | `kubetty-project-beacon-admin-dev` | `kubetty-project-beacon-admin-prd` |

**Why environment suffix for cluster-scoped resources?**

ClusterRoles and ClusterRoleBindings are cluster-wide. Without the environment suffix, dev and prod deployments in the same cluster would have naming collisions.

### 6.5 Resource Types & Lifecycle

The controller creates the following resources for each project:

#### 1. ServiceAccount
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kubetty-project-beacon-sa
  namespace: kubetty-projects-dev
  labels:
    app.kubernetes.io/name: kubetty
    app.kubernetes.io/instance: beacon
    app.kubernetes.io/managed-by: kubetty-controller
    kubetty.support.tools/environment: dev
```

#### 2. PersistentVolumeClaim
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: kubetty-project-beacon-data
  namespace: kubetty-projects-dev
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: longhorn
  resources:
    requests:
      storage: 50Gi
```

#### 3. Service
```yaml
apiVersion: v1
kind: Service
metadata:
  name: kubetty-project-beacon
  namespace: kubetty-projects-dev
spec:
  type: ClusterIP
  ports:
    - port: 8080
      targetPort: 8080
  selector:
    app.kubernetes.io/instance: beacon
```

#### 4. Deployment
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kubetty-project-beacon
  namespace: kubetty-projects-dev
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/instance: beacon
  template:
    spec:
      serviceAccountName: kubetty-project-beacon-sa
      containers:
        - name: kubetty
          image: harbor.support.tools/kubetty/kubetty:latest
          env:
            - name: KUBETTY_MODE
              value: "project"
            - name: SESSION_ID
              value: "{project.SessionID}"
          # ... resource limits, volume mounts
```

#### 5. ClusterRole & ClusterRoleBinding
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubetty-project-beacon-admin-dev
rules:
  - apiGroups: ["", "apps", "batch"]
    resources: ["*"]
    verbs: ["*"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubetty-project-beacon-admin-dev
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kubetty-project-beacon-admin-dev
subjects:
  - kind: ServiceAccount
    name: kubetty-project-beacon-sa
    namespace: kubetty-projects-dev
```

### 6.6 RBAC & Cross-Namespace Access

Projects can be granted access to additional namespaces beyond their shared home:

```go
type Project struct {
    // ...
    AdminNamespaces []string  // Full access to these namespaces
    ReadNamespaces  []string  // Read-only access to these namespaces
}
```

**Example**: A project configured with `AdminNamespaces: ["app-staging", "app-production"]` will have ClusterRole rules granting full access to those namespaces.

The ClusterRoleBinding always references the ServiceAccount in the shared namespace:
```yaml
subjects:
  - kind: ServiceAccount
    name: kubetty-project-beacon-sa
    namespace: kubetty-projects-dev  # Shared namespace
```

### 6.7 Configuration Schema

#### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `CONTROLLER_ENABLED` | No | `false` | Enable project controller |
| `PROJECTS_NAMESPACE` | If enabled | - | Shared namespace for projects |
| `RESOURCE_PREFIX` | No | `kubetty-project-` | Prefix for all resources |
| `RECONCILE_INTERVAL` | No | `30s` | How often to reconcile projects |
| `HEALTH_CHECK_INTERVAL` | No | `60s` | How often to check project health |

#### Helm Values

```yaml
controller:
  enabled: true
  projectsNamespace: "kubetty-projects-dev"
  resourcePrefix: "kubetty-project-"
  reconcileInterval: "30s"
  healthCheckInterval: "60s"
```

### 6.8 Controller Reconciliation Flow

The controller runs two background loops:

#### Reconciliation Loop (default: 30s)

```
┌─────────────────────────────────────────────────────────────┐
│                    Reconciliation Loop                       │
│                                                              │
│  1. Query projects with status: pending, creating,          │
│     updating, deleting                                       │
│                                                              │
│  2. For each project, call appropriate handler:             │
│     ┌──────────────┬────────────────────────────────────┐   │
│     │ Status       │ Handler                             │   │
│     ├──────────────┼────────────────────────────────────┤   │
│     │ pending      │ Create all K8s resources           │   │
│     │ creating     │ Wait for deployment ready          │   │
│     │ updating     │ Update deployment, wait for ready  │   │
│     │ deleting     │ Delete resources, hard delete row  │   │
│     └──────────────┴────────────────────────────────────┘   │
│                                                              │
│  3. Update project status and notify gateway                │
└─────────────────────────────────────────────────────────────┘
```

#### Health Check Loop (default: 60s)

```
For each project with status "running":
  1. GET http://kubetty-project-{name}.{namespace}.svc:8080/api/healthz
  2. If 200 OK: update LastHealthCheck timestamp
  3. If error: log warning (don't auto-fail)
```

### 6.9 Gateway Integration

When a project transitions to `running`, the controller notifies the gateway to register it:

```go
projCtrl.SetStatusCallback(func(p *projects.Project, status projects.ProjectStatus) {
    if status == projects.StatusRunning {
        tabManager.RegisterProject(gatewayconfig.Project{
            ID:          p.Name,
            DisplayName: p.DisplayName,
            Namespace:   cfg.Controller.ProjectsNamespace,
            Service:     fmt.Sprintf("kubetty-project-%s", p.Name),
            Port:        8080,
        })
    } else if status == projects.StatusDeleting {
        tabManager.UnregisterProject(p.Name)
    }
})
```

The gateway then routes WebSocket connections to the project service:
```
wss://gateway/ws?tab=<tabId>
    → http://kubetty-project-beacon.kubetty-projects-dev.svc:8080/ws
```

### 6.10 Environment Separation Strategy

Running both dev and prod in the same cluster:

| Aspect | Dev | Prod |
|--------|-----|------|
| Namespace | `kubetty-projects-dev` | `kubetty-projects-prd` |
| Gateway deployment | Separate Helm release | Separate Helm release |
| Database | Can share or separate CNPG | Typically separate |
| ClusterRole suffix | `-dev` | `-prd` |
| Resource quotas | Lower limits | Higher limits |

**Key isolation points**:
1. **Namespace isolation**: Pods cannot access each other's namespace without explicit RBAC
2. **RBAC isolation**: ClusterRoles are environment-specific (no cross-contamination)
3. **Network isolation**: NetworkPolicies can restrict traffic between environments
4. **Resource isolation**: Each namespace can have separate ResourceQuotas

---

## 7. File Structure

```
kube-tty/
│
├── server/
│   ├── go.mod
│   ├── cmd/                 # Binary entry points
│   │   ├── gateway/main.go      # Gateway mode (multi-project tabs)
│   │   ├── project/main.go      # Project mode (single PTY)
│   │   └── kubetty-authuser/    # User management CLI
│   └── internal/
│       ├── handlers/            # HTTP handlers
│       │   ├── auth/            # Auth handlers (login, logout, refresh)
│       │   └── session/         # Session log handlers
│       ├── gateway/             # Gateway-specific logic
│       ├── sessions/            # CNPG registry + resume logic
│       └── shared/              # Shared utilities
│           ├── config/          # Configuration helpers
│           ├── errors/          # Standardized error handling
│           ├── health/          # Health check utilities
│           ├── metrics/         # Prometheus metrics
│           └── server/          # HTTP server utilities
│
└── web/
    ├── package.json
    ├── public/
    └── src/
        ├── index.tsx
        ├── App.tsx
        └── components/
            ├── TerminalView.tsx
            ├── Login.tsx
            └── SessionLogsModal.tsx

deploy/
└── helm/
    ├── Chart.yaml
    ├── values.yaml                    # Default values
    ├── values.project-template.yaml   # Template for new projects
    └── templates/
        ├── deployment.yaml
        ├── secret.yaml
        └── service.yaml

scripts/
└── claude_with_log.sh       # installs the alias during image build
```

---

## 8. Code Summary

### Go backend

The server is split into two binaries under `server/cmd/`:
- **gateway/main.go** - Multi-project gateway with tabbed UI, auth, SSE events
- **project/main.go** - Single PTY session per pod, WebSocket streaming

Shared code lives in `server/internal/`:
- `handlers/auth/` - Authentication handlers (login, logout, refresh, middleware)
- `handlers/session/` - Session log handlers
- `sessions/` - CNPG session persistence
- `shared/` - Utilities (config, errors, health, metrics, server)

Build with: `go build ./server/cmd/gateway && go build ./server/cmd/project`

### React frontend

Already provided; engineer places in `web/` folder, installs deps, and ensures xterm.js + auth components are enabled by default:

```
npm install
npm run build
```

### Shell tooling

Include `scripts/claude_with_log.sh` in the image build to install the logging alias and confirm that `~/.local/bin/claude`, Codex CLI, Gemini CLI, kubectl, helm, docker, go, npm/node, git, jq, yq, curl/httpie, ripgrep, fd, tmux, make, python3, pip, psql, and common LLM-support utilities are available.

---

## 9. Kubernetes Integration

* **Image build:** everything (Go binary, React build, CLI tools) is baked into a single image that engineers build locally and push to `harbor.support.tools/kubetty/<repo>:<tag>`.
* **Helm release:** deployments are managed solely through a private Helm chart kept in this repo; no GitHub Actions or external registry integrations.
* **Namespaces/contexts:** engineers create/update namespaces manually via `~/.kube/config` before installing the chart.
* **CNPG:** all deployments share the same CNPG cluster; connection strings + credentials are injected through Helm values and projected into the pod as environment variables.
* **Session UUID:** each deployment gets a fixed `SESSION_ID` Helm value; if a pod restarts it reuses that UUID to resume the same PTY automatically.
* **Multiple deployments:** running Project A and Project B simultaneously simply means deploying separate Helm releases (with different namespaces, session IDs, and image tags) pointing at the shared CNPG instance.

---

## 10. Security Considerations

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

## 11. Acceptance Criteria

An engineer is **finished** when:

### Backend

* [ ] `/` serves the React app and `/ws` provides a PTY-backed WebSocket.
* [ ] Single PTY per pod is created on first connection and reused for pod lifetime.
* [ ] PTY metadata (session UUID, timestamps) persists in CNPG and survives pod restarts.
* [ ] Single-client-per-session enforcement rejects additional connections.
* [ ] Environment variables (`SESSION_ID`, `CNPG_*`, `CLAUDE_*`, `AUTH_*`) are read at startup and validated.
* [ ] Session logs are stored in CNPG and accessible via `/session/logs`.

### Frontend

* [ ] xterm.js renders the PTY output with correct colors, cursor control, and resizing.
* [ ] Login form displayed when auth is enabled and user is not authenticated.
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

## 12. Extras for Phase 2 (optional but easy)

* Integrate tmux as the PTY backend for richer multi-window workflows.
* Stream command transcripts into CNPG or object storage for searchable history/auditing.
* Add session log viewer/search UI for auditing past sessions.
* Provide Kubernetes job templates to launch ephemeral helper pods from within the UI.
* Add metrics and alerting for auth failures, session usage, and connection patterns.

---

## 13. Deliverables to Engineer

This doc and:

* Provided Go backend code (`server/cmd/gateway/main.go`, `server/cmd/project/main.go` + `go.mod`) plus `internal/` packages for handlers, sessions, and shared utilities.
* React frontend (`App.tsx`, `TerminalView.tsx`, `Login.tsx`) pre-wired with xterm.js and auth.
* Helm chart (`deploy/helm`) with sample values for `SESSION_ID`, CNPG creds, LLM env vars, and auth settings.
* `scripts/claude_with_log.sh` and documentation on local image build + Harbor push.
* Instructions for configuring CNPG, namespaces, `~/.kube/config`, and user creation.

Everything above remains intentionally minimal and focused on single-user pods with one session per deployment.

---
