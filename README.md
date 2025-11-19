# KubeTTY

A lightweight, internal-use-only browser-based terminal backed by a Kubernetes pod. KubeTTY provides a persistent development environment with full PTY support, session management, and integrated AI tooling.

## Features

- **Browser-based Terminal**: Full interactive shell with xterm.js (supports colors, cursor control, vim, tmux, etc.)
- **Session Persistence**: Sessions stored in CNPG database survive pod restarts
- **AI Tools Integration**: Pre-installed Claude Code, Codex, and other LLM CLIs
- **Development Tooling**: kubectl, helm, docker, go, node/npm, git, and more
- **Resume/Fork Sessions**: Claude-style session management (`--continue`, `--resume`, `--fork-session`)
- **Logging**: Built-in session logging for Claude CLI interactions

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

Sessions are automatically persisted in CNPG. You can:

- **Resume** a session after browser disconnect
- **Fork** a session to create a new branch
- **Continue** from the most recent session

The UI provides a session picker on connection.

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

- **Go Backend**: PTY server with WebSocket support, session management
- **React Frontend**: xterm.js-based terminal UI with session picker
- **CNPG Database**: Session metadata and persistence
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
│   ├── main.go
│   ├── go.mod
│   └── internal/
│       └── sessions/    # CNPG session management
├── web/                 # React frontend
│   ├── src/
│   ├── public/
│   └── package.json
├── deploy/
│   └── helm/           # Helm chart
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
├── scripts/            # Helper scripts
│   └── claude_with_log.sh
├── Dockerfile          # Multi-stage build
└── .bash_profile       # Shell configuration (no secrets)
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
