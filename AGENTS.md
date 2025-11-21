# Repository Guidelines

## Project Structure & Module Organization
`server/` holds the Go backend with two binaries under `cmd/`: `gateway/main.go` (multi-project tabbed UI) and `project/main.go` (single PTY mode). Shared code lives in `internal/` including handlers, sessions, and shared utilities. `web/` contains the React UI with `public/` for the HTML shell and `src/` for terminal components; keep UI tests next to their components and Go tests beside the packages they target. Kubernetes manifests live under `deploy/helm/` which supports both modes via `KUBETTY_MODE` environment variable.

## Build, Test, and Development Commands
- `go build ./server/cmd/gateway && go build ./server/cmd/project` – builds both binaries.
- `make build-server-local` – builds both binaries using the Makefile.
- `npm --prefix web install` – installs React dependencies; rerun on any `package*.json` change.
- `npm --prefix web run build` – compiles the UI bundle consumed by the Go server.
- `go test ./server/...` – runs all backend unit tests; append `-run Handler` to zero in on a suite.
- `npm --prefix web test -- --watch=false` – executes the React test suite once in CI.

## Coding Style & Naming Conventions
Rely on `gofmt`/`goimports` with tabs and concise receiver names (`func (srv *server)`), and keep package names lowercase and singular. TypeScript files use 2-space indentation, camelCase variables, and PascalCase components with descriptive filenames (`TerminalView.tsx`). Run `go fmt ./...` and, once wired, `npm --prefix web run lint` before each commit to avoid formatting-only diffs.

## Testing Guidelines
Backend tests follow the table-driven pattern, mocking PTY behavior to keep runs deterministic. Cover WebSocket negotiation, reconnect behavior, and shell lifecycle edges. In the React app, favor React Testing Library and user-level assertions; snapshots suit only stable layouts. Keep tests beside the code (`*_test.go`, `.test.tsx`) and ensure CI runs both `go test ./...` and `npm --prefix web test -- --watch=false`.

## Commit & Pull Request Guidelines
With no history yet, adopt Conventional Commits (`feat:`, `fix:`, `chore:`) so the log stays searchable; keep subjects imperative and under 72 characters. Split backend and frontend work when practical and mention API or protocol changes in the body. Pull requests should link the design doc or issue, list verification commands, include UI screenshots when relevant, and call out deployment follow-ups.

## Security & Deployment Notes
KubeTTY must stay internal-only: run behind `kubectl port-forward` or a locked-down Service and never expose `/ws` publicly. Provide secrets and kubeconfig files via environment variables or mounted volumes, not Git. When validating locally, build the React bundle, launch the Go server, and verify reconnect behavior mirrors the target pod.
