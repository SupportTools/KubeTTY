# Execute Feature Tasks - KubeTTY

Execute all tasks in Feature #$ARGUMENTS

TaskForge Project ID: 79

## Workflow Instructions

Read and follow the workflow in `.claude/docs/multi-task-execution-workflow.md` exactly.

## Feature Reference

Use dynamic lookup to get current features:
```
mcp__taskforge__listFeatures({ projectId: 79 })
```

Features are organized by priority (P0 Critical, P1 Resource Management, P2 Quality Improvements).

## Execution Requirements

### Phase 1: Batch Selection
1. Query TaskForge for Feature #$ARGUMENTS to get feature metadata
2. Get all pending tasks (`status: "todo"`) in the feature
3. Build dependency graph from `blockedByTaskId` fields
4. Topologically sort tasks for execution order
5. Check for incomplete previous batch (offer resume option)

### Phase 2: Batch Planning (MANDATORY)
1. Research ALL tasks before presenting plan
2. Identify shared code patterns and file dependencies:
   - Server code: `server/cmd/`, `server/internal/`
   - Frontend code: `web/src/`
   - Deployment: `deploy/helm/`
3. Present consolidated plan with:
   - Execution order with dependencies
   - Per-task implementation summary
   - Shared file modifications across tasks
   - Risks and mitigations (especially session security, auth bypass, pod access)

### Phase 3: Batch Approval Gate (MANDATORY BLOCKER)
- Wait for explicit user approval before any implementation
- Options: Approve, Request changes, Remove tasks, Abort

### Phase 4: Per-Task Execution Loop
For each task in dependency order:

1. **Update TaskForge**: Set status to `in_progress`
2. **Implementation**: Follow coding standards from CLAUDE.md
3. **Build Verification** (BLOCKER):
   ```bash
   make build-server-local
   ```
4. **Format and Vet Check** (BLOCKER):
   ```bash
   make fmt
   make vet
   ```
5. **Test Suite** (BLOCKER):
   ```bash
   make test-all-local
   ```
6. **QA Check** (recommended):
   ```bash
   make qa-check
   ```
7. **Task Completion**: Update TaskForge status to `done`, git commit

### Phase 5: Failure Handling
- On any gate failure: STOP, record state, present options (Fix/Skip/Abort)
- Persist batch state to TaskForge notes for resume capability

### Phase 6: Batch Completion
- Generate completion summary with all tasks, commits, follow-up tasks created
- Single git commit at end (unless per-task commits specified)

## Quality Gates (Per Task)

| Gate | Command | Blocker |
|------|---------|---------|
| Build | `make build-server-local` | YES |
| Format | `make fmt` | YES |
| Vet | `make vet` | YES |
| Go Tests | `make test-server-local` | YES |
| Web Tests | `make test-web-local` | YES |
| QA Check | `make qa-check` | Recommended |

## Security Handling

**Fix immediately and continue** for:
- Session token exposure in logs or responses
- Authentication bypass vulnerabilities
- Session state corruption in WebSocket handling
- Unauthorized pod access via exec

**Create follow-up task** for complex security enhancements that don't block current functionality.

## Commit Strategy

Default: Commit at end of feature with comprehensive message listing all tasks.
Alternative: Per-task commits if specified by user.

---

Now execute Feature #$ARGUMENTS following this workflow exactly.
