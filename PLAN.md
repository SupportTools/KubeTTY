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
