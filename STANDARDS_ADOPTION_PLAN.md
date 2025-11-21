# KubeTTY Standards Adoption Plan

**Date:** 2025-11-19
**Source:** nexmonyx/repo-template
**Target:** supporttools/KubeTTY

## Executive Summary

This plan outlines the work required to bring KubeTTY into compliance with the Nexmonyx repository template standards. The goal is to adopt consistent patterns for code organization, error handling, testing, deployment, and documentation.

**Estimated Effort:** 40-60 hours across all categories

---

## Priority Order

| Priority | Category | Effort | Impact |
|----------|----------|--------|--------|
| P1 | Documentation & Workflow | 8h | Foundation for all work |
| P2 | Error Handling Standardization | 12h | Code quality & consistency |
| P3 | Project Structure & Organization | 8h | Maintainability |
| P4 | Build & Validation Pipeline | 10h | CI/CD reliability |
| P5 | Testing Standards | 12h | Quality assurance |
| P6 | Helm Chart Alignment | 6h | Deployment consistency |

---

## P1: Documentation & Workflow Standards

### 1.1 Copy and Adapt Task Execution Workflow (REQUIRED)

**Source:** `repo-template/docs/development/task-execution-workflow.md`
**Target:** `KubeTTY/docs/development/task-execution-workflow.md`

**Adaptations needed:**
- Update project name references
- Adjust component names (server, web)
- Update makefile target references
- Customize for KubeTTY-specific workflows
- Add KubeTTY-specific quality gates

### 1.2 Create docs/development Directory Structure

```
docs/
└── development/
    ├── task-execution-workflow.md      # REQUIRED - 8-step workflow
    ├── error-handling-guide.md         # Error patterns
    ├── api-handler-standards.md        # Handler architecture
    ├── testing-guide.md                # Test requirements
    └── deployment-guide.md             # Deployment process
```

### 1.3 Create/Update CLAUDE.md

Add project-specific AI orchestration guide:
- KubeTTY architecture overview
- Component descriptions
- Technology stack
- Development workflows
- Common tasks and patterns

### 1.4 Update README.md

Align with template structure:
- Add Prerequisites section with versions
- Add Contributing guidelines
- Add Architecture diagram
- Reference docs/development/ guides

---

## P2: Error Handling Standardization

### 2.1 Create pkg/errors Package

**Location:** `server/pkg/errors/`

Create standardized error response package:
```go
// errors.go
package errors

type ErrorResponse struct {
    Status  int    `json:"status"`
    Error   string `json:"error"`
    Message string `json:"message"`
    Details string `json:"details,omitempty"`
}

func WriteError(w http.ResponseWriter, status int, errType, message string)
func BadRequest(w http.ResponseWriter, message string)
func Unauthorized(w http.ResponseWriter, message string)
func Forbidden(w http.ResponseWriter, message string)
func NotFound(w http.ResponseWriter, message string)
func Conflict(w http.ResponseWriter, message string)
func ValidationError(w http.ResponseWriter, message string)
func InternalServerError(w http.ResponseWriter, message string)
func ServiceUnavailable(w http.ResponseWriter, message string)
```

### 2.2 Refactor Existing Error Handling

**Files to update:**
- `server/cmd/gateway/main.go` - Gateway mode handlers
- `server/cmd/project/main.go` - Project mode handlers
- `server/internal/handlers/auth/` - Auth error responses
- `server/internal/gateway/` - Gateway error responses

**Changes:**
- Replace inline `http.Error()` with `pkg/errors.*` functions
- Standardize error messages
- Ensure consistent JSON error format across all endpoints

---

## P3: Project Structure & Organization

### 3.1 Reorganize Server Package Structure

**Status: COMPLETED** - The server has been split into two binaries.

**Current (Completed) Structure:**
```
server/
├── cmd/                 # Binary entry points
│   ├── gateway/main.go      # Gateway mode (multi-project tabs)
│   ├── project/main.go      # Project mode (single PTY)
│   └── kubetty-authuser/    # User management CLI
├── internal/
│   ├── handlers/        # HTTP handlers (one per file)
│   │   ├── auth/            # Auth handlers (login, logout, refresh, etc.)
│   │   └── session/         # Session log handlers
│   ├── gateway/             # Gateway-specific logic
│   ├── sessions/            # CNPG session persistence
│   └── shared/              # Shared utilities
│       ├── config/          # Configuration helpers
│       ├── errors/          # Standardized error handling
│       ├── handlers/        # Shared handlers
│       ├── health/          # Health check utilities
│       ├── metrics/         # Prometheus metrics
│       ├── server/          # HTTP server utilities
│       └── util/            # Common utilities
```

### 3.2 Split Handlers into Individual Files

**Status: COMPLETED** - Handlers have been extracted to individual files.

**Current Structure:**

```
internal/handlers/
├── auth/
│   ├── helpers.go           # Logger, shared types
│   ├── login.go             # handleLogin
│   ├── logout.go            # handleLogout
│   ├── refresh.go           # handleRefresh
│   ├── me.go                # handleMe
│   ├── middleware.go        # Auth middleware
│   └── password.go          # handlePasswordChange
└── session/
    └── logs.go              # handleSessionLogs

internal/shared/handlers/
└── session_logs.go          # Shared session log utilities

internal/gateway/
├── manager/                 # Tab manager
├── relay/                   # WebSocket relay
└── config/                  # Project catalog
```

### 3.3 Create Structured Logging Package

**Location:** `server/pkg/logging/`

```go
package logging

func NewLogger(component, domain string) *Logger
func (l *Logger) ShouldTrace(funcName string) bool
func (l *Logger) WithFields(fields logrus.Fields) *logrus.Entry
```

---

## P4: Build & Validation Pipeline

### 4.1 Create Comprehensive Makefile

**Add targets from template:**

```makefile
# Prerequisites
check-prerequisites
check-docker
check-go-version

# Build
build-server-local
build-web-local
build-all-local
build-server-image
build-all-images
push-all-images

# Test
test-server-local
test-all-local

# Validation
validate-pipeline-local
validate-quick
validate-go-version
validate-repo-cleanliness

# Deployment
deploy-dev
deploy-stg
deploy-prd

# Helm
helm-lint
helm-package

# Quality
qa-check

# Version
bump
```

### 4.2 Create .validation.json

```json
{
  "components": {
    "server": {
      "path": "./server",
      "test_timeout": "5m",
      "coverage_threshold": 60
    },
    "web": {
      "path": "./web",
      "test_timeout": "2m"
    }
  },
  "tools": {
    "gofmt": { "enabled": true },
    "go_vet": { "enabled": true },
    "staticcheck": { "enabled": true },
    "gosec": { "enabled": true, "severity": "medium" }
  },
  "hooks": {
    "pre_commit": ["gofmt", "go_vet"],
    "pre_push": ["validate-pipeline-local"]
  }
}
```

### 4.3 Create Git Hooks

**`.githooks/pre-commit`:**
- Run gofmt
- Run go vet
- Check for forbidden patterns

**`.githooks/pre-push`:**
- Run full validation pipeline
- Cannot be skipped

### 4.4 Create Validation Scripts

```
scripts/
├── validate-pipeline-local.sh
├── validate-go-version.sh
├── validate-repo-cleanliness.sh
└── init-hooks.sh
```

---

## P5: Testing Standards

### 5.1 Set Coverage Requirements

| Component | Minimum Coverage |
|-----------|-----------------|
| server/internal/auth | 70% |
| server/internal/sessions | 60% |
| server/internal/gateway | 60% |
| server/main handlers | 50% |

### 5.2 Create Test Structure

```
server/
├── main_test.go
├── internal/
│   ├── auth/
│   │   ├── manager_test.go
│   │   └── store_test.go
│   ├── sessions/
│   │   └── store_test.go
│   └── gateway/
│       └── manager/
│           └── manager_test.go
└── tests/
    └── integration/
        ├── auth_test.go
        └── session_test.go
```

### 5.3 Add Frontend Test Setup

```
web/
├── src/
│   └── components/
│       ├── Login.tsx
│       ├── Login.test.tsx        # Add tests
│       ├── TerminalView.tsx
│       └── TerminalView.test.tsx # Add tests
├── vitest.config.ts              # Test configuration
└── src/test/
    ├── setup.ts
    └── mocks/
```

---

## P6: Helm Chart Alignment

### 6.1 Restructure Helm Chart

**Current:** `deploy/helm/`
**Keep structure but add:**

```
deploy/helm/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── _helpers.tpl
    ├── deployment.yaml
    ├── service.yaml
    ├── serviceaccount.yaml
    ├── configmap.yaml          # Add if needed
    └── servicemonitor.yaml     # Add for Prometheus
```

### 6.2 Remove Environment-Specific Values Files

**Files to remove (per standard):**
- `values.beacon-support.yaml`
- `values.gateway.yaml`

**Instead:** Use ArgoCD parameters or separate ArgoCD manifests

### 6.3 Add Helm Validation

```bash
# Add to Makefile
helm-lint:
    helm lint deploy/helm/

helm-template-test:
    helm template test deploy/helm/ --debug
```

---

## Additional Standards to Adopt

### GitHub Actions Pipeline

Create `.github/workflows/pipeline.yml`:
1. Validation stage (go version, cleanliness)
2. Test matrix (server, web)
3. Build Docker images
4. Security scanning (Trivy)
5. Helm publish
6. Multi-environment deployment

### Swagger Documentation

Add complete Swagger annotations to all handlers:
```go
// @Summary Login user
// @Description Authenticate user with username and password
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login credentials"
// @Success 200 {object} LoginResponse
// @Failure 400 {object} errors.ErrorResponse
// @Failure 401 {object} errors.ErrorResponse
// @Router /api/auth/login [post]
func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
```

### Agent System Setup

Create `.claude/` directory with:
- Agent definitions for KubeTTY
- Project-specific prompts
- Workflow configurations

---

## Implementation Order

### Phase 1: Foundation (Week 1)
1. Copy task-execution-workflow.md and adapt
2. Create docs/development/ structure
3. Create CLAUDE.md
4. Create pkg/errors package
5. Add request ID middleware

### Phase 2: Code Organization (Week 2)
1. Create pkg/logging package
2. Split handlers into individual files
3. Refactor error handling to use pkg/errors
4. Add Swagger annotations

### Phase 3: Build Pipeline (Week 3)
1. Create comprehensive Makefile
2. Create .validation.json
3. Create validation scripts
4. Set up git hooks

### Phase 4: Testing & Deployment (Week 4)
1. Add backend tests to meet coverage
2. Add frontend test setup
3. Restructure Helm chart
4. Create GitHub Actions pipeline

---

## Files to Create

| File | Priority | Description |
|------|----------|-------------|
| `docs/development/task-execution-workflow.md` | P1 | **REQUIRED** - 8-step workflow |
| `docs/development/error-handling-guide.md` | P1 | Error patterns |
| `CLAUDE.md` | P1 | AI orchestration guide |
| `server/pkg/errors/errors.go` | P2 | Standard error responses |
| `server/pkg/logging/logging.go` | P3 | Structured logging |
| `.validation.json` | P4 | Validation configuration |
| `.githooks/pre-commit` | P4 | Pre-commit hook |
| `.githooks/pre-push` | P4 | Pre-push hook |
| `scripts/validate-pipeline-local.sh` | P4 | Local CI validation |
| `.github/workflows/pipeline.yml` | P4 | CI/CD pipeline |

## Files to Modify

| File | Changes | Status |
|------|---------|--------|
| ~~`server/main.go`~~ | Extract handlers, use pkg/errors | **COMPLETED** - Split into `cmd/gateway/` and `cmd/project/` |
| `Makefile` | Add standard targets | **COMPLETED** |
| `README.md` | Align with template structure | **COMPLETED** |
| `deploy/helm/values.yaml` | Remove placeholders | In progress |

## Files to Delete

| File | Reason |
|------|--------|
| `deploy/helm/values.beacon-support.yaml` | Use ArgoCD parameters instead |
| `deploy/helm/values.gateway.yaml` | Use ArgoCD parameters instead |

---

## Success Criteria

- [ ] task-execution-workflow.md copied and adapted
- [ ] All handlers use pkg/errors.* functions
- [ ] Single function per file for handlers
- [ ] Swagger documentation complete
- [ ] Validation pipeline passes locally
- [ ] Git hooks enforce standards
- [ ] Test coverage meets minimums
- [ ] Helm chart follows standards
- [ ] CI/CD pipeline operational
- [ ] CLAUDE.md documents project

---

## Next Steps

1. Create TaskForge feature for "Standards Adoption"
2. Break down into individual tasks
3. Begin with P1 documentation tasks
4. Follow 8-step workflow for each task
