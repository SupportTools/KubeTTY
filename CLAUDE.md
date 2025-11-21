# CLAUDE.md - KubeTTY Project Guide

This document provides guidance for AI agents working on the KubeTTY project.

## Project Overview

**KubeTTY** is a web-based terminal emulator for Kubernetes pods. It provides secure shell access to pods through a browser interface with features including:

- WebSocket-based PTY streaming
- Session persistence with CloudNativePG (PostgreSQL)
- JWT authentication with refresh tokens
- Multi-project gateway mode
- MOTD (Message of the Day) display
- Session logging and replay

## Architecture

KubeTTY is a **monorepo** with the following structure:

```
KubeTTY/
├── server/           # Go backend
│   ├── cmd/          # Binary entry points
│   │   ├── gateway/main.go      # Gateway mode binary (multi-project tab UI)
│   │   ├── project/main.go      # Project mode binary (single PTY)
│   │   └── kubetty-authuser/    # User management CLI
│   ├── internal/     # Internal packages
│   │   ├── handlers/auth/       # Authentication handlers
│   │   ├── handlers/session/    # Session handlers
│   │   ├── gateway/             # Multi-project gateway logic
│   │   ├── sessions/            # Session persistence (pgx_store)
│   │   └── shared/              # Shared utilities (config, errors, health, metrics)
│   ├── migrations/   # Database migrations
│   └── ui/dist/      # Embedded frontend (built from web/)
├── web/              # React frontend
│   └── src/
│       ├── components/  # UI components
│       └── contexts/    # React contexts
├── deploy/helm/      # Helm chart (supports both gateway and project modes)
└── docs/             # Documentation
```

### Binary Modes

KubeTTY builds two separate binaries from the `server/cmd/` directory:

| Binary | Entry Point | Description |
|--------|-------------|-------------|
| `kubetty-gateway` | `server/cmd/gateway/main.go` | Multi-project gateway with tabbed UI, auth, SSE events |
| `kubetty-project` | `server/cmd/project/main.go` | Single PTY session per pod, WebSocket streaming |

The Dockerfile and Helm chart use `KUBETTY_MODE` environment variable to select which binary runs.

## Technology Stack

### Backend (server/)
- **Language**: Go 1.23
- **HTTP Server**: net/http with ServeMux
- **WebSocket**: gorilla/websocket
- **Database**: PostgreSQL via pgxpool (pgx/v5)
- **Authentication**: JWT (golang-jwt/jwt/v5)
- **PTY**: github.com/creack/pty

### Frontend (web/)
- **Framework**: React 18 with TypeScript
- **Terminal**: xterm.js
- **Build**: Vite
- **Styling**: CSS (no framework)

### Infrastructure
- **Database**: CloudNativePG (CNPG)
- **Container**: Docker
- **Orchestration**: Kubernetes with Helm

## Key Design Decisions

### Single-Client Per Session
Each PTY session allows only one connected client at a time. Additional connection attempts receive HTTP 409 Conflict.

### Output Buffering
PTY output is buffered and broadcast to all clients (for gateway mode where backend maintains session while frontend reconnects).

### Session Persistence
Sessions are persisted to PostgreSQL with:
- Session metadata (UUID, deployment ID, PID)
- Attachment tracking
- Session logs for replay

### Authentication Flow
1. Local auth mode: username/password login
2. JWT access token (15m TTL)
3. Refresh token (7d TTL) with automatic refresh
4. HttpOnly cookies for token storage

## TaskForge Task Management

The project uses PostgreSQL-backed TaskForge for task management.

### Project ID: 79 (KubeTTY Production Readiness)

### Features/Priorities
- **P0 Critical Fixes** (Feature 332): Security and stability issues
- **P1 Resource Management** (Feature 335): Documentation and patterns
- **P2 Quality Improvements** (Feature 336): Testing and observability

### Task Workflow
1. Get tasks: `mcp__taskforge__getTasks({ projectId: 79 })`
2. Update status: `mcp__taskforge__updateTask({ taskId: X, status: "in_progress" })`
3. Complete: `mcp__taskforge__updateTask({ taskId: X, status: "done", outcome: "..." })`

**Valid statuses**: todo, in_progress, done, blocked

See `docs/development/task-execution-workflow.md` for complete workflow.

## Development Workflows

### Building

```bash
# Server (both binaries)
cd server && go build ./cmd/gateway && go build ./cmd/project

# Or using Makefile
make build-server-local

# Web (outputs to server/ui/dist/)
cd web && npm run build

# Docker image
docker build -t harbor.support.tools/kubetty/kubetty:latest .
```

### Testing

```bash
# Go tests
cd server && go test -v ./...

# Web tests
cd web && npm test

# With coverage
go test -cover ./...
```

### Deployment

```bash
# Helm lint
helm lint deploy/helm/

# Deploy
helm upgrade --install kubetty ./deploy/helm \
  -n kubetty-dev \
  -f deploy/helm/values.yaml
```

## Code Conventions

### Go Standards

```go
// Use logrus for structured logging
log.WithFields(log.Fields{
    "session_uuid": sessionUUID,
    "client_id":    clientID,
}).Info("Client connected")

// Use standardized error responses
http.Error(w, "session not found", http.StatusNotFound)

// Input validation with limits
const (
    maxUsernameLength = 64
    maxPTYCols        = 500
    maxPTYRows        = 200
)
```

### TypeScript Standards

```typescript
// Use strict typing
interface Props {
    sessionUUID: string;
    onConnect: () => void;
}

// No console.log in production code
// Use environment-based logging if needed
```

### Git Commit Format

```
feat(component): brief description

- Detailed changes
- Task completion: [task description]

Generated with [Claude Code](https://claude.com/claude-code)
Co-Authored-By: Claude <noreply@anthropic.com>
```

## Key Files

### Configuration
- `server/internal/config/config.go` - Server configuration
- `deploy/helm/values.yaml` - Helm values

### Core Handlers
- `server/cmd/gateway/main.go` - Gateway mode: tabs, projects, SSE, auth middleware
- `server/cmd/project/main.go` - Project mode: PTY, WebSocket, session management
- `server/internal/handlers/auth/` - Authentication handlers (login, logout, refresh, middleware)
- `server/internal/handlers/session/` - Session log handlers

### Frontend Components
- `web/src/App.tsx` - Main application
- `web/src/components/TerminalView.tsx` - Terminal component
- `web/src/contexts/AuthContext.tsx` - Auth state management

### Database
- `server/internal/sessions/pgx_store.go` - Session persistence
- `server/migrations/*.sql` - Database migrations

## API Endpoints

### Public
- `GET /api/healthz` - Health check
- `GET /metrics` - Prometheus metrics
- `POST /api/auth/login` - Authentication

### Protected (requires auth)
- `GET /api/auth/me` - Current user
- `POST /api/auth/refresh` - Refresh token
- `GET /session/logs` - Session logs
- `GET /ws` - WebSocket connection

### Gateway Mode
- `GET /api/projects` - List projects
- `POST /api/tabs` - Create tab
- `GET /api/tabs/events` - Tab events stream

## Common Tasks

### Adding a New API Endpoint

1. Determine which binary needs the endpoint (gateway or project)
2. Add handler in the appropriate location:
   - Gateway routes: `server/cmd/gateway/main.go` or `server/internal/handlers/`
   - Project routes: `server/cmd/project/main.go` or `server/internal/handlers/`
   - Shared handlers: `server/internal/shared/handlers/`
3. Register route in the binary's `main()` mux setup
4. Add input validation using shared utilities
5. Implement operation with proper error handling (use `shared/errors`)
6. Write tests
7. Update documentation if needed

### Adding Database Feature

1. Create migration files in `server/migrations/`
2. Add model structures
3. Implement store methods
4. Add tests
5. Apply migration to test database

### Adding Frontend Component

1. Create component in `web/src/components/`
2. Add TypeScript interfaces
3. Wire up to state/context
4. Write tests
5. Build and verify

## Important Rules

1. **Go 1.23** - Always use Go 1.23 (check server/go.mod)
2. **No console.log** - Remove debug logs before committing
3. **Input validation** - Validate all user inputs
4. **Test coverage** - Write tests for all new code
5. **Error handling** - Use proper HTTP status codes
6. **Single task** - Complete one task before starting another
7. **Semantic versioning only** - Use clean version tags (v0.5.1, v1.0.0). No suffixes like -rc, -beta, -auth-ui, etc.

## Documentation References

- `DESIGN.md` - Detailed architecture specifications
- `QA_REVIEW.md` - Known issues and priorities
- `docs/development/task-execution-workflow.md` - Mandatory workflow
- `docs/development/error-handling-guide.md` - Error patterns
- `docs/development/api-handler-standards.md` - Handler architecture
- `docs/development/testing-guide.md` - Testing requirements
- `docs/development/deployment-guide.md` - Deployment process

## Environment Variables

### Common (Both Gateway & Project)
| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `PORT` | Server port | No | 8080 |
| `SESSION_ID` | Session UUID | Yes | - |
| `DEPLOYMENT_ID` | Deployment identifier | No | SESSION_ID value |
| `CNPG_HOST` | PostgreSQL host | Yes | - |
| `CNPG_PORT` | PostgreSQL port | No | 5432 |
| `CNPG_DATABASE` | PostgreSQL database | Yes | - |
| `CNPG_USER` | PostgreSQL user | Yes | - |
| `CNPG_PASSWORD` | PostgreSQL password | Yes | - |
| `SESSION_LOG_RETENTION_HOURS` | Log retention (hours) | No | 720 (30 days) |
| `SESSION_LOG_MAX_ENTRIES` | Max log entries per session | No | 5000 |

### Gateway-Specific
| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `PROJECT_CATALOG_PATH` | Path to project catalog YAML | No | - |
| `TAB_IDLE_TIMEOUT` | Tab idle timeout | No | 2h |
| `AUTH_MODE` | Auth mode (disabled/local) | No | disabled |
| `AUTH_JWT_SECRET` | JWT secret | If auth=local | - |
| `AUTH_ACCESS_TTL` | Access token TTL | No | 15m |
| `AUTH_REFRESH_TTL` | Refresh token TTL | No | 720h (30 days) |
| `AUTH_ISSUER` | JWT issuer | No | kubetty |
| `AUTH_COOKIE_DOMAIN` | Cookie domain | No | - |
| `AUTH_COOKIE_SECURE` | Cookie secure flag | No | true |

### Project-Specific
| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `SHELL` | Shell to use | No | /bin/bash |
| `KUBETTY_USER` | KubeTTY user | No | USER env var |
| `KUBETTY_PROJECT` | KubeTTY project | No | DEPLOYMENT_ID value |

## Getting Help

- Review `DESIGN.md` for architecture questions
- Check `QA_REVIEW.md` for known issues
- Follow `docs/development/task-execution-workflow.md` for task workflow
- Use TaskForge to track work progress
