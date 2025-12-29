# Task Execution Workflow for KubeTTY

This document defines the mandatory workflow for executing all development tasks in the KubeTTY project.

## Available Resources

### Task Management System

The project uses PostgreSQL-backed TaskForge for persistent task management:
- **TaskForge Project ID: 79**
- **Priority-based**: Urgent, High, Medium, Low
- **Persistent across sessions**: All Claude Code instances share the same task state
- **MCP Tools**: `mcp__taskforge__getTasks`, `mcp__taskforge__updateTask`, `mcp__taskforge__getTask`

### Development Environment

**Requirements:**
- Go 1.23+
- Node.js 20+
- Docker (for building images)
- Helm 3.x (for deployment)
- PostgreSQL (for session persistence testing)

**Test Database Setup:**
```bash
# Start test database
make test-db-up

# Run tests with database
make test-with-db

# Stop test database
make test-db-down
```

### Repository Structure

KubeTTY is a **web-based terminal emulator for Kubernetes pods**:

```
KubeTTY/
├── server/                    # Go backend
│   ├── cmd/
│   │   ├── gateway/main.go   # Gateway mode binary (multi-project tab UI)
│   │   └── project/main.go   # Project mode binary (single PTY)
│   ├── internal/
│   │   ├── handlers/auth/    # Authentication handlers
│   │   ├── handlers/session/ # Session handlers
│   │   ├── gateway/          # Multi-project gateway logic
│   │   ├── sessions/         # Session persistence (pgx_store)
│   │   └── shared/           # Shared utilities (config, errors, health)
│   ├── migrations/           # Database migrations
│   └── ui/dist/              # Embedded frontend
├── web/                       # React frontend
│   └── src/
│       ├── components/       # UI components
│       └── contexts/         # React contexts
├── deploy/helm/              # Helm chart
└── CLAUDE.md                 # Project coding standards
```

---

## Workflow Steps

### 1. Receive and Initiate Task

- Query TaskForge for task: `mcp__taskforge__getTask({taskId: <id>})`
- Verify task priority and check for dependencies (`blockedByTaskId`)
- If task has unmet dependencies, report and await instruction
- Update task status to `in_progress`: `mcp__taskforge__updateTask({taskId: <id>, status: "in_progress"})`

**TodoWrite Integration (MANDATORY):**
```
Use TodoWrite tool to create task breakdown:
- Research and design phase
- Implementation phase
- Build verification
- Testing
- Task completion

Mark first item as "in_progress" immediately.

NOTE: TodoWrite tracks workflow steps within current session.
TaskForge tracks task completion across all sessions.
```

### 2. Research & Design Phase (MANDATORY - NO CODING WITHOUT THIS)

**Research the task requirements:**
- Review CLAUDE.md for architecture and coding standards
- Identify affected packages and files:
  - Server code: `server/cmd/`, `server/internal/`
  - Frontend code: `web/src/`
  - Deployment: `deploy/helm/`
- Review existing code patterns
- Understand impact on terminal sessions and authentication

**Present a comprehensive implementation plan:**

#### Task Overview
Clear summary aligned with project architecture

#### Design Approach
Technical strategy following established patterns:
- HTTP handler patterns
- WebSocket/PTY session management
- JWT authentication flow
- Gateway vs Project mode separation

#### Files Affected
Complete list with brief explanation

#### Dependencies
Prerequisites and blockers

#### Success Criteria
Specific, measurable conditions:
- Build passes (`make build-server-local`)
- All tests pass (`make test-all-local`)
- No session state corruption
- Authentication remains secure

#### Risks & Mitigations
Potential issues, especially:
- Session token exposure
- Authentication bypass
- WebSocket message handling errors
- Pod exec permission issues

### 3. Wait for Approval (MANDATORY GATE)

**Pause here and await explicit instruction.** Do not proceed without approval.

- `Approve`: Proceed to Implementation
- `Request changes`: Revise plan and re-present
- `Skip`: Mark task as skipped and await new assignment

### 4. Implementation Phase

Implement the approved design with strict adherence to:

#### Coding Standards
- Go formatting (`gofmt`)
- Comprehensive error handling
- Clear logging for debugging (use logrus)
- Thread-safe operations for concurrent sessions

#### Testing
- Write tests alongside code (MANDATORY)
- Test happy paths, edge cases, and error conditions
- Consider concurrent session scenarios

#### Session Safety
- Validate session state before operations
- Handle WebSocket disconnections gracefully
- Implement proper cleanup on session termination
- Guard against race conditions in PTY operations

**If implementation reveals issues with approved plan, stop and report immediately**

### 5. Quality Assurance Cycle (MANDATORY)

#### Step 5.1: Build Verification (BLOCKER)

```bash
make build-server-local
```

- **MUST PASS**: Zero compilation errors
- If build fails, fix immediately before proceeding

#### Step 5.2: Format and Vet Check (BLOCKER)

```bash
make fmt
make vet
```

- Must pass with no issues

#### Step 5.3: Run Test Suite (BLOCKER)

```bash
make test-all-local
```

- Report test coverage and results
- **If tests fail**: Fix issues, re-build, re-test
- Test results must include: pass/fail counts

#### Step 5.4: QA Check (Recommended)

```bash
make qa-check
```

- Runs fmt, vet, and lint checks

#### Step 5.5: Integration Tests (when applicable)

```bash
# Run tests with real PostgreSQL database
make test-with-db
```

### 6. Task Completion

#### Provide completion summary:
- Files changed/created/deleted
- Tests written with coverage metrics
- Any known limitations or follow-up work needed

#### Update TaskForge:
```
mcp__taskforge__updateTask({taskId: <id>, status: "done"})
```

#### Commit with proper format:
```bash
git add .
git commit -m "<type>: <description>

- Detailed changes
- Task: #<task_id>

Co-Authored-By: Claude <noreply@anthropic.com>"
```

Types: feat, fix, refactor, test, docs, chore

---

## KubeTTY-Specific Requirements

### ABSOLUTE RULES

1. **ONE TASK AT A TIME** - Never work on multiple tasks simultaneously
2. **RESEARCH BEFORE CODING** - Mandatory research phase, no exceptions
3. **BUILD VERIFICATION** - Must pass before testing
4. **SESSION SAFETY** - All changes must preserve session integrity
5. **AUTH SECURITY** - Never bypass or weaken authentication

### FORBIDDEN ACTIONS

- Starting new task before current fully completed
- Skipping workflow steps
- Proceeding with failed builds
- Exposing JWT tokens in logs or responses
- Bypassing auth middleware
- Breaking WebSocket message protocols

### Session Integrity Checklist

Before marking task complete, verify:
- [ ] Session state is properly managed across WebSocket reconnects
- [ ] Error paths don't leave sessions in inconsistent state
- [ ] Concurrent session access is handled safely
- [ ] Authentication tokens are handled securely
- [ ] Pod exec permissions are validated

---

## Quality Gates Summary

| Phase | Gate | Blocker | Tool |
|-------|------|---------|------|
| Research | Design approved | Yes | Manual approval |
| Build | Binary compiles | Yes | `make build-server-local` |
| Format | Code formatted | Yes | `make fmt` |
| Vet | Static analysis | Yes | `make vet` |
| Go Test | All Go tests pass | Yes | `make test-server-local` |
| Web Test | All web tests pass | Yes | `make test-web-local` |
| QA Check | Lint passes | Recommended | `make qa-check` |
| Integration | DB tests pass | Conditional | `make test-with-db` |

---

## Workflow Diagram

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Receive Task                                             │
│    - Verify priority & dependencies                         │
│    - Mark [IN-PROGRESS] in TaskForge                        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Research & Design Phase (MANDATORY)                      │
│    - Review CLAUDE.md                                       │
│    - Identify affected files                                │
│    - Present implementation plan                            │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. Wait for Approval (GATE)                                 │
│    ✅ Approve → Continue                                     │
│    🔄 Revise → Back to Step 2                               │
│    ⏭️ Skip → Await new task                                 │
└────────────────────────┬────────────────────────────────────┘
                         │ (approved)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. Implementation Phase                                     │
│    - Follow coding standards                                │
│    - Write tests alongside code                             │
│    - Ensure session safety                                  │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 5.1 Build Verification (BLOCKER)                            │
│     make build-server-local                                 │
│     ❌ Failed → Fix → Retry                                  │
└────────────────────────┬────────────────────────────────────┘
                         │ (passed)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 5.2 Format and Vet Check (BLOCKER)                          │
│     make fmt && make vet                                    │
│     ❌ Issues → Fix → Retry                                  │
└────────────────────────┬────────────────────────────────────┘
                         │ (passed)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 5.3 Run Test Suite (BLOCKER)                                │
│     make test-all-local                                     │
│     ❌ Failed → Fix → Back to 5.1                            │
└────────────────────────┬────────────────────────────────────┘
                         │ (passed)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 5.4-5.5 QA and Integration (when applicable)                │
│     make qa-check                                           │
│     make test-with-db                                       │
│     ❌ Failed → Fix → Back to 5.1                            │
└────────────────────────┬────────────────────────────────────┘
                         │ (passed)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 6. Task Completion                                          │
│    - Provide summary                                        │
│    - Mark [DONE] in TaskForge                               │
│    - Commit changes                                         │
│    - Await next task                                        │
└─────────────────────────────────────────────────────────────┘
```

---

This workflow ensures systematic, quality-driven development aligned with KubeTTY architecture and session safety requirements.
