# KubeTTY

A lightweight, internal-use-only browser-based terminal backed by a Kubernetes pod. KubeTTY provides a persistent development environment with full PTY support, session management, and integrated AI tooling.

## Features

- **Browser-based Terminal**: Full interactive shell with xterm.js (supports colors, cursor control, vim, tmux, etc.)
- **Single Session Per Pod**: One PTY session is created and reused for the lifetime of each pod
- **Session Persistence**: Session metadata and logs stored in CNPG database survive pod restarts
- **Single Client Enforcement**: Only one browser client can connect at a time per session
- **Built-in Authentication**: Local user auth with JWT tokens (optional, configurable)
- **AI Tools Integration**: Pre-installed Claude Code, Codex, and other LLM CLIs
- **Development Tooling**: kubectl, helm, docker, go, node/npm, git, and more
- **Logging**: Built-in session logging for Claude CLI interactions and PTY transcripts

## Quick Start

### Prerequisites

- Access to a Kubernetes cluster with `~/.kube/config` configured
- Harbor registry access at `harbor.support.tools`
- CNPG database cluster for session persistence
- Kubernetes secrets for API keys (see [Secret Management](#secret-management))

### Building the Image

```bash
# Build frontend
cd web
npm install
npm run build
cd ..

# Build Docker image
docker build -t harbor.support.tools/kubetty/kubetty:latest .

# Push to Harbor
docker push harbor.support.tools/kubetty/kubetty:latest
```

### Deploying

```bash
# Create namespace
kubectl create namespace kubetty-dev

# Create API secrets (see SECRETS.md for details)
kubectl create secret generic kubetty-api-keys \
  -n kubetty-dev \
  --from-literal=github-token='YOUR_TOKEN' \
  --from-literal=openai-api-key='YOUR_KEY'
# ... add other secrets as needed

# Deploy with Helm
helm upgrade --install kubetty-dev ./deploy/helm \
  -n kubetty-dev \
  -f deploy/helm/values.yaml \
  --set apiSecrets.existingSecret=kubetty-api-keys
```

### Accessing KubeTTY

```bash
# Port forward to the pod
kubectl port-forward -n kubetty-dev deployment/kubetty-dev 8080:8080

# Open browser
open http://localhost:8080
```

## Secret Management

KubeTTY requires several API keys and credentials to function. These are managed through Kubernetes Secrets and injected as environment variables.

### Required Secrets

- `GITHUB_TOKEN` - GitHub personal access token
- `OPENAI_API_KEY` - OpenAI API key
- `GOOGLE_CLOUD_PROJECT` - GCP project ID
- `NEXMONYX_ACCESS_KEY` - Nexmonyx access key
- `NEXMONYX_ACCESS_SECRET` - Nexmonyx secret key
- `ANTHROPIC_BASE_URL` - (Optional) Custom Anthropic endpoint

### Setup Instructions

See **[SECRETS.md](./SECRETS.md)** for comprehensive secret management documentation including:

- How to create Kubernetes secrets
- Security best practices
- Rotating credentials
- Troubleshooting

**⚠️ Security Warning:** Never commit secrets to git. The `.bash_profile` included in the Docker image contains no secrets - all sensitive values are injected via Kubernetes Secrets.

## Usage

### Inside the Terminal

KubeTTY provides several helpful aliases and functions:

```bash
# Run Claude Code with session logging
c

# Or manually
claude_with_log

# Access logs
ls ~/claude_logs/
```

### Session Management

Each pod has a single session that is created on first connection and reused for the pod lifetime:

- **Automatic Reconnection**: Browser automatically reconnects after network drops
- **Single Client**: Only one browser can connect at a time (second connection is rejected)
- **PTY Persistence**: Shell remains running even when browser disconnects
- **Session Logs**: All PTY I/O is logged to CNPG for auditing via the logs modal

## Architecture

```
Browser UI (React + xterm.js)
      |
      | WebSocket
      v
Go Server (KubeTTY)
     / \
    /   \
   v     v
CNPG     PTY (/bin/bash)
Session
Store
```

### Components

- **Go Backend**: PTY server with WebSocket support, session management, and authentication
- **React Frontend**: xterm.js-based terminal UI with login form and auto-reconnection
- **CNPG Database**: Session metadata, logs, user credentials, and persistence
- **Docker Image**: All development tools pre-installed

## Documentation

- **[DESIGN.md](./DESIGN.md)** - Comprehensive design document and architecture
- **[SECRETS.md](./SECRETS.md)** - Secret management guide
- **[AGENTS.md](./AGENTS.md)** - Agent-specific documentation (if applicable)

## Development

### Project Structure

```
KubeTTY/
├── server/              # Go backend
│   ├── cmd/             # Binary entry points
│   │   ├── gateway/main.go      # Gateway mode (multi-project tabs)
│   │   ├── project/main.go      # Project mode (single PTY)
│   │   └── kubetty-authuser/    # User management CLI
│   ├── internal/
│   │   ├── handlers/            # HTTP handlers (auth, session)
│   │   ├── gateway/             # Gateway logic
│   │   ├── sessions/            # CNPG session management
│   │   └── shared/              # Shared utilities
│   ├── migrations/              # Database migrations
│   └── go.mod
├── web/                 # React frontend
│   ├── src/
│   ├── public/
│   └── package.json
├── deploy/
│   └── helm/           # Helm chart (supports both modes via KUBETTY_MODE)
│       ├── Chart.yaml
│       ├── values.yaml                    # Base defaults
│       ├── values.project-template.yaml   # Template for new projects
│       └── templates/
├── scripts/            # Helper scripts
│   └── claude_with_log.sh
├── Dockerfile          # Multi-stage build (builds both binaries)
└── .bash_profile       # Shell configuration (no secrets)
```

### Git Hooks Setup

The repository includes Git hooks to catch issues before they reach CI/CD:

```bash
# Enable the hooks (one-time setup)
git config core.hooksPath .githooks
```

**Pre-commit hook** runs automatically on `git commit`:
- Forbidden file detection (*.key, *.pem, secrets, credentials)
- Go formatting check (gofmt)
- Go linting (go vet)
- Merge conflict marker detection

**Pre-push hook** runs automatically on `git push`:
- Full validation for main/release/* branches
- Quick validation for feature branches
- Calls `validate-pipeline-local.sh` to mirror CI checks

To bypass hooks temporarily (not recommended):
```bash
git commit --no-verify
git push --no-verify
```

### Local Pipeline Validation

Before pushing, you can manually run the local validation:

```bash
# Full validation (matches CI/CD)
./validate-pipeline-local.sh

# Quick validation (skip tests)
./validate-pipeline-local.sh --quick

# Full validation with Docker build and security scan
./validate-pipeline-local.sh --full
```

### Environment Variables

KubeTTY is configured via environment variables injected by Helm:

**Session Management:**
- `SESSION_ID` - Default session UUID for this deployment
- `DEPLOYMENT_ID` - Unique deployment identifier

**Database:**
- `CNPG_HOST` - CNPG database host
- `CNPG_PORT` - CNPG database port
- `CNPG_DATABASE` - Database name
- `CNPG_USER` - Database username (from secret)
- `CNPG_PASSWORD` - Database password (from secret)

**AI/API Keys:** (from apiSecrets, see SECRETS.md)
- `GITHUB_TOKEN`
- `OPENAI_API_KEY`
- `GOOGLE_CLOUD_PROJECT`
- `NEXMONYX_ACCESS_KEY`
- `NEXMONYX_ACCESS_SECRET`
- `ANTHROPIC_BASE_URL`

**Claude Configuration:**
- `CLAUDE_CODE_MAX_OUTPUT_TOKENS` - Token limit for Claude Code
- `CLAUDE_CONFIG_DIR` - Claude configuration directory

**Authentication (required configuration):**
- `AUTH_MODE` - **Required.** Set to `local` to enable built-in login, or `disabled` to run without authentication. Empty values are rejected to prevent accidental insecure deployments.
- `AUTH_JWT_SECRET` - Base64 (recommended) or raw secret used to sign JWTs. Generate via `openssl rand -base64 48`. Required when `AUTH_MODE=local`.
- `AUTH_ACCESS_TTL` / `AUTH_REFRESH_TTL` - Token lifetimes (e.g., `15m`, `720h`).
- `AUTH_ISSUER` - JWT issuer claim (default: `kubetty`).
- `AUTH_COOKIE_DOMAIN` - Optional domain for auth cookies.
- `AUTH_COOKIE_SECURE` - `true` by default; set to `false` only for HTTP/dev environments.

> **Security Warning: AUTH_MODE=disabled**
>
> When `AUTH_MODE=disabled`, all API routes are unprotected and accessible without authentication:
> - All WebSocket endpoints (`/ws`) are open
> - All tab/project management APIs (`/api/tabs`, `/api/projects`) are accessible
> - Session logs (`/session/logs`) can be read by anyone
>
> The gateway will log a security warning at startup and add an `X-Auth-Warning` header to all responses when authentication is disabled.
>
> **Only use `AUTH_MODE=disabled` when:**
> - Running behind a VPN or private network with strict access controls
> - Using Kubernetes NetworkPolicies to restrict access
> - For local development/testing purposes
>
> **For production deployments, always use `AUTH_MODE=local` with a strong `AUTH_JWT_SECRET`.**

These knobs are exposed through the Helm chart under the `auth` section (`deploy/helm/values.yaml`). You can point `AUTH_JWT_SECRET` at a Kubernetes Secret via `auth.jwtSecretSecret` to avoid storing raw secrets in values files.

### Creating Users

After enabling `AUTH_MODE=local`, create a CNPG user with the helper:

```bash
# ensure SESSION_ID is set (the helper defaults to kubetty-authuser)
export SESSION_ID=kubetty-authuser
export CNPG_HOST=...
export CNPG_DATABASE=...
export CNPG_USER=...
export CNPG_PASSWORD=...

go run ./server/cmd/kubetty-authuser create --username alice --password 's3cret'
go run ./server/cmd/kubetty-authuser list
go run ./server/cmd/kubetty-authuser update-password --username alice --password 'newpass'
go run ./server/cmd/kubetty-authuser set-active --username alice --active=false
```

Subsequent logins happen through the `/api/auth/login` endpoint (the UI shows a form automatically). You can also call `/api/auth/refresh` and `/api/auth/logout`, and every request now includes auth cookies/callbacks.

## Security

KubeTTY is designed for **internal-use only**:

- ✅ Access via `kubectl port-forward` or private network only
- ✅ No public ingress or authentication (relies on network isolation)
- ✅ Secrets managed via Kubernetes Secrets
- ✅ NetworkPolicy restrictions recommended
- ❌ Not suitable for public internet exposure
- ❌ No multi-tenancy support

See [SECRETS.md](./SECRETS.md) for detailed security practices.

## Deployment Scenarios

### Development Environment

```bash
helm upgrade --install kubetty-dev ./deploy/helm \
  -n kubetty-dev \
  --set deploymentId=kubetty-dev \
  --set apiSecrets.existingSecret=kubetty-api-keys
```

### Multiple Projects

Deploy separate instances for each project:

```bash
# Project A
helm upgrade --install kubetty-project-a ./deploy/helm \
  -n project-a \
  -f deploy/helm/values.project-a.yaml \
  --set apiSecrets.existingSecret=kubetty-api-keys

# Project B
helm upgrade --install kubetty-project-b ./deploy/helm \
  -n project-b \
  -f deploy/helm/values.project-b.yaml \
  --set apiSecrets.existingSecret=kubetty-api-keys
```

Each deployment gets its own session UUID and maintains independent shell state.

### Gateway Mode (Tabbed UI)

To provide a single entrypoint (e.g., https://kubetty.support.tools) that fans out to multiple project pods, enable the **gateway** feature:

1. Create a catalog file describing the downstream projects. Example `projects.yaml`:

   ```yaml
   projects:
     - id: ai-dev
       displayName: "AI Platform"
       namespace: kubetty-ai
       service: kubetty-ai-kubetty
       port: 8080
       description: "LLM tooling"
     - id: infra
       namespace: kubetty-infra
       service: kubetty-infra-kubetty
       port: 8080
   ```

2. Mount the file into the gateway deployment and set `PROJECT_CATALOG_PATH=/etc/kubetty/projects.yaml` (or similar) via Helm values.
3. Deploy the gateway chart so it can reach each project Service (ClusterIP) over the cluster network. NetworkPolicies should allow only the gateway namespace to talk to those Services.
4. When a user opens https://kubetty.support.tools they will see a tab bar with a `+` button. Each tab corresponds to one downstream project and the browser WebSocket `wss://…/ws?tab=<id>` is proxied through the gateway to the project pod’s `/ws` endpoint.

Gateway APIs:

| Method | Path            | Description                                     |
| ------ | --------------- | ----------------------------------------------- |
| GET    | `/api/projects` | Lists catalog entries (id, displayName, etc.).  |
| GET    | `/api/tabs`     | Lists tabs owned by the current browser client. |
| POST   | `/api/tabs`     | `{ "projectId": "ai-dev" }` → opens a new tab. |
| DELETE | `/api/tabs/{id}`| Closes the tab/tunnel if owned by the client.   |
| GET    | `/ws?tab=<id>`  | WebSocket endpoint for a specific tab.          |

The server automatically mints an opaque `kubetty_client` cookie to associate tabs with a browser. Tabs persist in CNPG so reloads reconnect automatically, and the React UI polls `/api/tabs` every ~8 seconds to refresh status.

If `PROJECT_CATALOG_PATH` is unset, the app behaves as before (single PTY, no tab bar).

## Troubleshooting

### Cannot connect to pod
```bash
# Check pod status
kubectl get pods -n NAMESPACE

# View logs
kubectl logs -n NAMESPACE deployment/kubetty-DEPLOYMENT

# Check port forward
kubectl port-forward -n NAMESPACE deployment/kubetty-DEPLOYMENT 8080:8080
```

### Session not persisting
- Verify CNPG connection settings in Helm values
- Check CNPG credentials secret exists
- Review pod logs for database connection errors

### Missing API keys
- Ensure secrets are created before deployment
- Verify `apiSecrets.existingSecret` is set correctly
- Check environment variables in running pod: `kubectl exec POD -- env`

See [SECRETS.md](./SECRETS.md) for secret-specific troubleshooting.

## License

Internal use only. Not for public distribution.

## Support

For issues or questions:
- Review documentation: [DESIGN.md](./DESIGN.md), [SECRETS.md](./SECRETS.md)
- Check pod logs: `kubectl logs`
- Contact your cluster administrator for infrastructure issues
