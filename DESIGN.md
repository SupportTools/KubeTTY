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

## 5. File Structure

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

## 6. Code Summary

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

## 7. Kubernetes Integration

* **Image build:** everything (Go binary, React build, CLI tools) is baked into a single image that engineers build locally and push to `harbor.support.tools/kubetty/<repo>:<tag>`.
* **Helm release:** deployments are managed solely through a private Helm chart kept in this repo; no GitHub Actions or external registry integrations.
* **Namespaces/contexts:** engineers create/update namespaces manually via `~/.kube/config` before installing the chart.
* **CNPG:** all deployments share the same CNPG cluster; connection strings + credentials are injected through Helm values and projected into the pod as environment variables.
* **Session UUID:** each deployment gets a fixed `SESSION_ID` Helm value; if a pod restarts it reuses that UUID to resume the same PTY automatically.
* **Multiple deployments:** running Project A and Project B simultaneously simply means deploying separate Helm releases (with different namespaces, session IDs, and image tags) pointing at the shared CNPG instance.

---

## 8. Security Considerations

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

## 9. Acceptance Criteria

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

## 10. Extras for Phase 2 (optional but easy)

* Integrate tmux as the PTY backend for richer multi-window workflows.
* Stream command transcripts into CNPG or object storage for searchable history/auditing.
* Add multi-session dashboard (even if still single-user) to quickly fork/archive sessions.
* Provide Kubernetes job templates to launch ephemeral helper pods from within the UI.
* Explore lightweight auth (header token or OAuth) if exposure ever leaves the private network.

---

## 11. Deliverables to Engineer

This doc and:

* Provided Go backend code (`main.go` + `go.mod`) plus `internal/sessions` stub.
* React frontend (`App.tsx`, `TerminalView.tsx`, `SessionPicker.tsx`) pre-wired with xterm.js.
* Helm chart (`deploy/helm`) with sample values for `SESSION_ID`, CNPG creds, LLM env vars.
* `scripts/claude_with_log.sh` and documentation on local image build + Harbor push.
* Instructions for configuring CNPG, namespaces, and `~/.kube/config`.

Everything above remains intentionally minimal and focused on single-user pods.

---
