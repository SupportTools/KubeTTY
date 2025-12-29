# Execute Single Task - KubeTTY

Execute TaskForge Task #$ARGUMENTS

TaskForge Project ID: 79

## Workflow Instructions

Read and follow the workflow in `.claude/docs/task-execution-workflow.md` exactly.

## Execution Requirements

### Step 1: Receive and Initiate Task
1. Query TaskForge for Task #$ARGUMENTS to get task metadata
2. Verify task priority level and check no higher priority tasks are pending
3. Check for task dependencies (`blockedByTaskId`)
4. If task has unmet dependencies or is already completed, report and await instruction
5. Update task status to `in_progress` using `mcp__taskforge__updateTask`

### Step 2: Research & Design Phase (MANDATORY - NO CODING WITHOUT THIS)
1. Review CLAUDE.md for architecture specifications and coding standards
2. Identify affected packages and files in the codebase:
   - Server code: `server/cmd/`, `server/internal/`
   - Frontend code: `web/src/`
   - Deployment: `deploy/helm/`
3. Review existing code patterns:
   - Handlers in `server/internal/handlers/`
   - Session management in `server/internal/sessions/`
   - Gateway vs Project mode entry points
4. Understand the task's impact on terminal sessions and authentication
5. Present comprehensive implementation plan with:
   - Task overview aligned with project architecture
   - Design approach following established patterns
   - Files affected (complete list)
   - Dependencies and blockers
   - Success criteria (specific, measurable)
   - Risks and mitigations (especially session security, auth bypass, pod access)

### Step 3: Wait for Approval (MANDATORY GATE)
- **PAUSE HERE** and await explicit instruction
- Options:
  - `Approve`: Proceed to Step 4 (Implementation)
  - `Request changes`: Revise plan and re-present
  - `Skip`: Mark task as skipped and await new assignment

### Step 4: Implementation Phase
1. Follow all coding standards:
   - Go formatting (`gofmt`)
   - Comprehensive error handling
   - Clear logging for debugging
   - Thread-safe operations for concurrent sessions
2. Write tests alongside code (MANDATORY)
3. Ensure session integrity (no state corruption during WebSocket operations)

### Step 5: Quality Assurance Cycle (MANDATORY)

**5.1 Build Verification (BLOCKER)**
```bash
make build-server-local
```
- **MUST PASS**: Zero errors before proceeding

**5.2 Format and Vet Check (BLOCKER)**
```bash
make fmt
make vet
```
- Must pass with no issues

**5.3 Run Test Suite (BLOCKER)**
```bash
make test-all-local
```
- If tests fail: Fix -> Re-build -> Re-test

**5.4 QA Check (recommended)**
```bash
make qa-check
```
- Runs fmt, vet, and lint checks

### Step 6: Task Completion
1. Provide completion summary:
   - Files changed/created/deleted
   - Tests written with coverage metrics
   - Any known limitations or follow-up work needed
2. Update TaskForge: `mcp__taskforge__updateTask({taskId: $ARGUMENTS, status: "done"})`
3. Commit with proper format describing the change

## Quality Gates

| Gate | Command | Blocker |
|------|---------|---------|
| Build | `make build-server-local` | YES |
| Format | `make fmt` | YES |
| Vet | `make vet` | YES |
| Go Tests | `make test-server-local` | YES |
| Web Tests | `make test-web-local` | YES |
| QA Check | `make qa-check` | Recommended |

## Security Handling

When implementing, be aware of these KubeTTY-specific security concerns:
- **Session token exposure**: Ensure JWT tokens are handled securely
- **Authentication bypass**: Validate auth middleware on all protected routes
- **Session state corruption**: Guard WebSocket message handling
- **Unauthorized pod access**: Verify proper RBAC and namespace restrictions

## Commit Message Format

```
<type>: <short description>

<detailed description of changes>

Task: #<task_id>
```

Types: feat, fix, refactor, test, docs, chore
