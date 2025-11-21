# Task Execution Workflow

**Core Framework Document** - This is a production-tested workflow adapted from the Nexmonyx repository template for KubeTTY.

This document defines the mandatory workflow for executing all development tasks in KubeTTY.

## Available Resources

### Task Management System

The project uses PostgreSQL-backed TaskForge for persistent task management:
- **Tasks organized by project** (P0-P2 priorities, Critical Fixes, Resource Management, Quality Improvements)
- **Priority-based**: Urgent, High, Medium, Low
- **Persistent across sessions**: All Claude Code instances share the same task state
- **MCP Tools**: `mcp__taskforge__listProjects`, `mcp__taskforge__getTasks`, `mcp__taskforge__updateTask`
- See CLAUDE.md "TaskForge Task Management" section for complete usage guide

### Development Environment

- Local development with Docker Compose for testing
- Kubernetes cluster access via configured kubeconfig
- CNPG database for session persistence
- Use this environment to:
  - Test WebSocket PTY streaming
  - Validate database migrations and schema changes
  - Run integration tests with live services
  - Verify Helm chart deployments
  - Debug auth and session management

### Project Structure

KubeTTY is a **monorepo** with two main components:

| Component | Path | Purpose |
|-----------|------|---------|
| **server** | `server/` | Go backend with Fiber, PTY management, auth, WebSocket |
| **web** | `web/` | React frontend with xterm.js, auth context, terminal UI |
| **deploy** | `deploy/helm/` | Helm chart for Kubernetes deployment |

**CRITICAL RULE**: When changes span both server and web components, BOTH must be updated in the same task workflow before marking the task complete.

---

## Workflow Steps

### 1. Receive and Initiate Task

- Retrieve tasks using `mcp__taskforge__getTasks` with project ID 79 (KubeTTY Production Readiness)
- Verify the task priority level and ensure no higher priority tasks are pending
- Check for task dependencies (look for blockedByTaskId in task data)
- If task has unmet dependencies or is already completed, report this and await further instruction
- Update task status to "in_progress" using `mcp__taskforge__updateTask` when starting work

**TodoWrite Integration (MANDATORY for session tracking)**:
```
Use TodoWrite tool to create initial task breakdown:
- Research and design phase
- Implementation phase (server, web, or both)
- Build verification
- QA review
- Testing
- Deployment
- Devils advocate verification
- Task completion

Mark first item as "in_progress" immediately.

NOTE: TodoWrite tracks workflow steps WITHIN the current session.
TaskForge tracks project-level task completion ACROSS all sessions.
Both are mandatory - use TodoWrite for workflow progress, TaskForge for task completion.
```

### 2. Research & Design Phase (MANDATORY - NO CODING WITHOUT THIS)

**Thoroughly research the task requirements:**
- Review DESIGN.md for architecture specifications
- Review QA_REVIEW.md for known gaps and priorities
- Identify integration points (server, web, database)
- Review existing codebase for similar patterns
- Understand business logic and user requirements

**Present a comprehensive implementation plan that includes:**

#### Task Overview
Clear summary aligned with design document

#### Design Approach
Technical strategy following KubeTTY's established patterns:
- Fiber framework for HTTP/WebSocket handlers
- React with TypeScript for frontend
- CNPG (PostgreSQL) for persistence
- JWT authentication with refresh tokens
- WebSocket PTY streaming with output buffering

#### Files Affected
Complete list with brief explanation:
- Server binary files (in `server/cmd/gateway/` or `server/cmd/project/`)
- Server handler files (in `server/internal/handlers/`)
- Shared utilities (in `server/internal/shared/`)
- Database migrations (in `server/migrations/`)
- React components (in `web/src/components/`)
- Helm chart modifications if needed
- Documentation updates

#### Dependencies
Prerequisites and blockers:
- CNPG database availability
- Authentication state requirements
- WebSocket connectivity
- Kubernetes cluster access

#### Success Criteria
Specific, measurable conditions:
- Server builds successfully (`go build ./server/cmd/gateway && go build ./server/cmd/project`)
- Web builds successfully (`npm --prefix web run build`)
- All tests pass (`go test ./server/...`)
- QA agent verification passes
- No console.log statements in production code
- Helm chart lints successfully

#### Risks & Mitigations
Potential issues and fallbacks

**TodoWrite Update**: Mark "Research and design phase" as completed, mark next phase as in_progress.

### 3. Wait for Approval (MANDATORY GATE)

**You must pause here and await explicit instruction.** Do not proceed to coding without approval.

Based on feedback:
- `Approve`: Proceed immediately to Step 4 (Implementation)
- `Request changes`: Revise plan according to feedback and re-present
- `Skip`: Mark task as skipped and await new assignment

**TodoWrite Update**: When approval received, mark "Implementation phase" as in_progress.

### 4. Implementation Phase

**IMPORTANT**: Work on components in the correct order to maintain consistency:

1. **Database** - Create migrations first
2. **Server** - Implement backend handlers and logic
3. **Web** - Update frontend to use new backend features

Implement the approved design with strict adherence to:

#### Go Version
- **ALWAYS use Go 1.23** - verify server/go.mod specifies "go 1.23"

#### Logging Standards
```go
// Use logrus for structured logging
log.WithFields(log.Fields{
    "session_uuid": sessionUUID,
    "client_id":    clientID,
}).Info("Client connected")

// For errors
log.WithError(err).Error("Failed to create PTY")
```

#### Error Handling Standards
```go
// Use standardized error responses (from pkg/errors when created)
return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
    "status":  400,
    "error":   "bad_request",
    "message": "Invalid session UUID",
    "details": err.Error(),
})
```

#### WebSocket Patterns
```go
// Always handle connection lifecycle
defer func() {
    conn.Close()
    session.removeClient(conn)
}()

// Send structured messages
conn.WriteJSON(fiber.Map{
    "type": "resize",
    "cols": cols,
    "rows": rows,
})
```

#### Database Changes
- Create migrations in `server/migrations/` with up/down files
- NEVER run migrations in API startup code
- Use sequential numbering (0001, 0002, etc.)
- Test migrations can be applied and rolled back

#### Frontend Patterns
```typescript
// Use TypeScript strictly
interface Props {
    sessionUUID: string;
    onConnect: () => void;
}

// Use React hooks properly
const [state, setState] = useState<StateType>(initialValue);

// Remove console.log before committing
// Use environment-based logging if needed
```

#### Input Validation
- Validate all user inputs on both client and server
- Cap PTY resize dimensions (max 500 cols, 200 rows)
- Limit username length (max 64 chars)
- Sanitize error messages to avoid leaking internals

#### Write Tests as You Code (MANDATORY)

**Critical Rule: NEVER write code without corresponding tests.**

**Unit Tests (Go):**
- For EVERY handler/function, write corresponding tests in `*_test.go` files
- Follow Go testing conventions with table-driven tests
- Minimum coverage requirements:
  - **Happy path**: Valid inputs with expected behavior
  - **Edge cases**: Boundary conditions, empty/nil values
  - **Error cases**: Error handling, validation failures
- Run tests incrementally: `go test -v ./server/...`

**Frontend Tests:**
- Use Vitest and React Testing Library
- Test component behavior, not implementation
- Test auth flows and state management
- Place tests in `*.test.tsx` files

**If implementation reveals issues with approved plan, stop and report immediately**

**TodoWrite Update**: Mark implementation tasks as completed, mark "Build verification" as in_progress.

### 5. Quality Assurance Cycle (MANDATORY)

#### Step 5.1: Build Verification (BLOCKER)

**Server:**
```bash
cd /home/mmattox/go/src/github.com/supporttools/KubeTTY
go build ./server
go test ./server/...
```

**Web:**
```bash
cd /home/mmattox/go/src/github.com/supporttools/KubeTTY
npm --prefix web run build
npm --prefix web run lint
```

**Helm:**
```bash
helm lint deploy/helm/
```

- **MUST PASS**: Zero compilation errors, zero linting errors
- If any build fails, fix immediately before proceeding

**TodoWrite Update**: Mark "Build verification" as completed, mark "QA review" as in_progress.

#### Step 5.2: QA Agent Review (BLOCKER)

- Invoke QA agent using Task tool with qa-engineer subagent
- Provide QA agent with:
  - Changed files and context
  - Original task requirements
  - Success criteria
  - Design document references

- QA agent will verify:
  - Code quality and standards compliance
  - Error handling patterns
  - Logging implementation
  - Database migrations correct
  - No debug console.log statements
  - Input validation complete
  - Security considerations
  - Integration with existing systems

- **BLOCKER**: Address ALL QA findings before proceeding

**TodoWrite Update**: Mark "QA review" as completed, mark "Testing" as in_progress.

#### Step 5.3: Run Full Test Suite

```bash
# Server tests with verbose output
go test -v ./server/...

# Server tests with coverage
go test -cover -coverprofile=coverage.out ./server/...
go tool cover -html=coverage.out

# Web tests (when configured)
npm --prefix web run test
```

- Report test coverage and results
- **If tests fail**: Fix issues, re-run QA review if needed, re-test
- Test results must include: files tested, pass/fail counts, coverage %

**TodoWrite Update**: Mark "Testing" as completed, mark "Deployment" as in_progress.

### 6. Deployment (MANDATORY)

```bash
# Build Docker image
docker build -t harbor.support.tools/kubetty/kubetty:latest .

# Push to registry
docker push harbor.support.tools/kubetty/kubetty:latest

# Deploy with Helm
helm upgrade --install kubetty ./deploy/helm \
  -n kubetty-dev \
  -f deploy/helm/values.yaml
```

#### Deployment Success Criteria
- Docker build completes without errors
- Image pushes to Harbor successfully
- Helm upgrade succeeds
- Pod starts and passes health checks
- WebSocket connections work
- Auth flow functions correctly

**If deployment fails, enter fix cycle (fix -> build -> QA -> deploy)**

**TodoWrite Update**: Mark "Deployment" as completed, mark "Devils advocate verification" as in_progress.

### 7. Devils Advocate Verification (MANDATORY)

- Use Task tool with appropriate verification agent
- Provide complete context:
  - Original task specification
  - Implementation details
  - Test results
  - Deployment status

- Devils advocate will verify:
  - Original request was fully fulfilled
  - Business requirements met
  - No edge cases missed
  - Implementation matches task specification
  - All success criteria satisfied
  - No security vulnerabilities introduced

#### If issues found: Enter Quality Cycle
- Fix identified issues
- Build verification
- QA agent re-review
- Re-deploy
- Devils advocate re-verification
- **STAY IN CYCLE** until fully resolved

**TodoWrite Update**: When devils advocate approves, mark "Devils advocate verification" as completed, mark "Task completion" as in_progress.

### 8. Task Completion (ONLY AFTER DEVILS ADVOCATE APPROVAL)

#### Provide completion summary:

**Server changes:**
- Files changed/created/deleted
- Tests written/updated with coverage metrics
- Migrations added

**Web changes:**
- Components updated/created
- Tests written

**Helm changes:**
- Values updated
- Templates modified

**Test results:**
- All passing
- Coverage percentage

**Deployment confirmation:**
- Image tag deployed
- Pod status

#### Update TaskForge task system:
- Mark task as completed using `mcp__taskforge__updateTask({taskId: <id>, status: "completed"})`
- This automatically records completion timestamp in database
- Note any discoveries or deviations from design in task notes

#### Commit with proper format:

```bash
git add .
git commit -m "feat(component): brief description

- Detailed changes
- Task completion: [task description]

Generated with [Claude Code](https://claude.com/claude-code)
Co-Authored-By: Claude <noreply@anthropic.com>"

git push origin main
```

**TodoWrite Update**: Mark "Task completion" as completed. Clean up todo list - all workflow steps done.

**Await next task assignment**

---

## KubeTTY-Specific Requirements

### ABSOLUTE RULES

1. **ONE TASK AT A TIME** - Never work on multiple tasks simultaneously
2. **RESEARCH BEFORE CODING** - Mandatory research phase, no exceptions
3. **DESIGN COMPLIANCE** - Must follow DESIGN.md specifications
4. **GO 1.23** - Verify version in server/go.mod
5. **BUILD VERIFICATION** - Must pass before QA review
6. **QA VALIDATION** - Must pass before deployment
7. **DEPLOYMENT MANDATORY** - Must deploy and verify
8. **DEVILS ADVOCATE** - Must pass before completion
9. **QUALITY CYCLE** - Stay in cycle until fully resolved
10. **PRIORITY ENFORCEMENT** - Never skip priority levels

### FORBIDDEN ACTIONS

- Starting new task before current fully completed
- Skipping workflow steps
- Proceeding with failed builds
- Ignoring QA findings
- Bypassing deployment
- Marking complete without devils advocate approval
- Leaving console.log statements in production code
- Using Go version < 1.23
- Skipping input validation
- Adding code without tests

### EMERGENCY PROCEDURES

If workflow cannot be completed due to infrastructure issues:

1. Document blocker in task description
2. Use TaskForge to track blocker with high/urgent priority
3. Link blocker using `blockedByTaskId` field
4. Do not start new tasks until blocker resolved
5. Escalate for assistance

---

## Common KubeTTY Scenarios

### Scenario 1: Adding New API Endpoint

**Components affected**: server, web (if UI needed)

**Workflow**:
1. Determine which binary (gateway or project) needs the endpoint
2. Create handler in `server/internal/handlers/` or `server/internal/shared/handlers/`
3. Register route in the appropriate `cmd/gateway/main.go` or `cmd/project/main.go`
4. Add Swagger documentation
5. Create tests for handler
6. Update web if UI integration needed
7. Test end-to-end

### Scenario 2: Adding Database Feature

**Components affected**: server (migrations, models, handlers)

**Workflow**:
1. Create migration files (up and down)
2. Add model structures
3. Create handler/queries
4. Add tests
5. Apply migration to test database
6. Verify rollback works

### Scenario 3: Adding Frontend Component

**Components affected**: web, possibly server

**Workflow**:
1. Create component in web/src/components
2. Add TypeScript interfaces
3. Wire up to existing state/context
4. Create tests
5. Build and verify

### Scenario 4: Fixing Critical Bug

**Affected**: depends on bug location

**Workflow**:
1. Reproduce and understand the bug
2. Write failing test that demonstrates bug
3. Implement fix
4. Verify test passes
5. Check for related issues
6. Deploy and verify

---

## Quality Gates Summary

| Phase | Gate | Blocker | Tool |
|-------|------|---------|------|
| Research | Design approved | Yes | Manual approval |
| Build | Binary compiles | Yes | `go build ./server` |
| QA | Agent review passes | Yes | Task tool (qa-engineer) |
| Test | All tests pass | Yes | `go test ./server/...` |
| Deploy | Deployment succeeds | Yes | Helm upgrade |
| Verify | Devils advocate approves | Yes | Task tool (verification) |
| Complete | All gates passed | Yes | Manual confirmation |

---

## Workflow Diagram

```
+-------------------------------------------------------------+
| 1. Receive Task                                             |
|    - Verify priority & dependencies                         |
|    - Mark [IN-PROGRESS]                                     |
+------------------------+------------------------------------+
                         |
                         v
+-------------------------------------------------------------+
| 2. Research & Design Phase (MANDATORY)                      |
|    - Review DESIGN.md                                       |
|    - Identify dependencies                                  |
|    - Present implementation plan                            |
+------------------------+------------------------------------+
                         |
                         v
+-------------------------------------------------------------+
| 3. Wait for Approval (GATE)                                 |
|    Approve -> Continue                                      |
|    Revise -> Back to Step 2                                 |
|    Skip -> Await new task                                   |
+------------------------+------------------------------------+
                         | (approved)
                         v
+-------------------------------------------------------------+
| 4. Implementation Phase                                     |
|    - Work in order: Database -> Server -> Web               |
|    - Follow all coding standards                            |
|    - Write tests alongside code                             |
+------------------------+------------------------------------+
                         |
                         v
+-------------------------------------------------------------+
| 5.1 Build Verification (BLOCKER)                            |
|     Build server and web                                    |
|     Failed -> Fix -> Retry                                  |
+------------------------+------------------------------------+
                         | (passed)
                         v
+-------------------------------------------------------------+
| 5.2 QA Agent Review (BLOCKER)                               |
|     Review all changes                                      |
|     Issues found -> Fix -> Retry                            |
+------------------------+------------------------------------+
                         | (passed)
                         v
+-------------------------------------------------------------+
| 5.3 Run Test Suite (BLOCKER)                                |
|     Run all tests                                           |
|     Tests failed -> Fix -> Back to 5.2                      |
+------------------------+------------------------------------+
                         | (passed)
                         v
+-------------------------------------------------------------+
| 6. Deployment (MANDATORY)                                   |
|    Build and push Docker image                              |
|    Deploy with Helm                                         |
|    Failed -> Fix -> Back to 5.1                             |
+------------------------+------------------------------------+
                         | (deployed)
                         v
+-------------------------------------------------------------+
| 7. Devils Advocate Verification (MANDATORY)                 |
|    Verify implementation complete                           |
|    Issues found -> Back to 5.1 (Quality Cycle)              |
+------------------------+------------------------------------+
                         | (approved)
                         v
+-------------------------------------------------------------+
| 8. Task Completion                                          |
|    - Provide summary                                        |
|    - Mark [COMPLETED]                                       |
|    - Commit changes                                         |
|    - Await next task                                        |
+-------------------------------------------------------------+
```

---

This workflow ensures systematic, quality-driven development aligned with KubeTTY architecture and standards.
