# Current Implementation State (2026-10-01)

Snapshot of what is in the code at `e4f5b99`. For open defects see `QA_REVIEW.md`. For the
original architecture spec see `DESIGN.md`.

## Binaries (`server/cmd/`)

| Binary | Role |
|--------|------|
| `kubetty-gateway` (`cmd/gateway`) | Browser-facing service: React UI, JWT auth, project catalog, tabs (REST + SSE), WebSocket/VNC relay to project pods, admin API (projects, settings, dashboard), and the in-process project controller. Requires CNPG/PostgreSQL. |
| `kubetty-project` (`cmd/project`) | Runs inside each project pod. **Stateless** (no DB since `8924f27`). Owns the PTY(s) and serves `/ws`, `/api/healthz`, `/api/metrics`, `/api/gui/status`. Output goes to an 8MB ring buffer (`OUTPUT_BUFFER_SIZE`) that is replayed on connect. Optional PTY transcript logging to stdout or a JSONL file for Loki (`PTY_LOG_*`, `PTY_FILE_LOG_*`). |
| `kubetty-authuser` (`cmd/kubetty-authuser`) | CLI to create, update and list users and to toggle activation. |

One image runs both server binaries; `KUBETTY_MODE` selects which one. Frontend assets are built
by Vite into `server/cmd/gateway/ui/dist` and embedded in both binaries (the Dockerfile copies the
same output into `cmd/project/ui/dist`).

## Session model

- **Session modes** (per project, `kubetty_projects.session_mode`, migration 0016, `e2d489d`).
  The controller passes the mode to pods as `SESSION_MODE`:
  - `exclusive_takeover` (default): one client per PTY. A second client gets HTTP 409.
    `?force=true` disconnects the current client (close code 4000) and takes over. Admission uses an
    atomic `reserveSlot()` (TOCTOU fix `6661fa4`), and a takeover epoch makes `?force=true` also
    displace clients still mid-upgrade (`ec2c48c`).
  - `shared_concurrent`: any number of clients share one PTY.
  - `independent_shells`: each gateway tab gets its own PTY in the pod, keyed by `?shell=<tabID>`.
- **Gateway tabs** belong to a user (`user:<id>`), or to a client cookie when auth is disabled.
  Re-attaching preempts the tab's stale proxy, and `force=true` transfers ownership. Concurrent
  `Proxy()` calls on a tab are serialized (`48e1f0d`). Tab limits per client and per project
  (429) and an idle timeout (`TAB_IDLE_TIMEOUT`, default 2h) are enforced. Tab metadata and
  position persist in `gateway_tabs`.
- **Downstream connection**: a WebSocket relay to the project `/ws` with backoff (default), or a
  Kubernetes exec stream (`KUBETTY_EXEC_MODE=exec`). Optional noVNC GUI desktop per project
  (`/vnc`, `GUI_ENABLED`).
- The `sessions`/`session_logs` tables and `/session/logs` still exist, but nothing writes to them
  any more (QA_REVIEW N5).

## Authentication

- `AUTH_MODE=local` enables JWT auth: a short-lived access token (default 15m) and a rotating
  hashed refresh token (default 30 days), stored as HttpOnly, SameSite=Lax cookies (Secure by
  default). There is also a password-change endpoint.
- If `AUTH_MODE` is anything else, routes are open. The gateway then logs a security warning and
  adds an `X-Auth-Warning` header. The `helm-gateway` chart defaults to `auth.mode: local`.
- There are no roles: every authenticated user can use the admin API (QA_REVIEW N1).

## Project controller and HA

- The controller runs inside the gateway when `CONTROLLER_ENABLED=true`. It reconciles
  `kubetty_projects` rows into Deployment, Service, PVC, ServiceAccount, env Secret and
  NetworkPolicy objects in a single projects namespace (`PROJECTS_NAMESPACE`, prefix
  `kubetty-project-`). It also handles template-PVC sync Jobs, pause/unpause, image upgrades and
  restart/resync.
- A storage monitor expands PVCs automatically above 70% usage (`STORAGE_EXPAND_*`).
- Lease-based leader election (`99d3721`, `LEADER_ELECTION_*`, enabled by default) ensures only
  one replica runs the controller. The lease lives in the release namespace. The production chart
  runs `replicas: 1`.
- Admin UI and API cover project CRUD, global settings with an audit history, and an admin
  dashboard (`/api/admin/dashboard/{summary,metrics,errors,usage}`, `AdminDashboard.tsx`).

## Storage

- The default project storage class is TrueNAS `freenas-iscsi-csi` (`c9069bd`, migrated from
  Longhorn). `PVC_SUFFIX` (default `-data`, `-data-truenas` for migrated environments, `8b09fe5`)
  selects PVC names.
- The init-permissions chown is hardened against transient CSI hangs (`4bb1845`).
- Some DB and UI defaults still say `longhorn` (QA_REVIEW N6).
- Gateway state is in CNPG (`kubetty-db-rw.kubetty-gateway-prd.svc` in production).

## Helm (`deploy/`)

- `helm-gateway/`: production and dev gateway chart. Since `5ff9bba`, RBAC is
  **namespace-scoped**. The gateway and controller ClusterRoles are only permission templates,
  bound through RoleBindings in the project namespace(s). The leader-election Lease uses a
  Role/RoleBinding in the release namespace. There are no ClusterRoleBindings, and no
  namespace or RBAC-management verbs.
- `helm-project/`: standalone project pod chart (used by `scripts/dev.sh`).
- `helm/`: original combined chart (`KUBETTY_MODE` switch). It is not used by CI, and it still
  binds the controller cluster-wide (QA_REVIEW N4).

## CI/CD (`.github/workflows/pipeline.yml`, `validate-pr.yml`)

- **Runners:** all jobs run on `self-hosted-linux`, the on-prem ARC scale set
  `arc-runners-supporttools` in `a1-ops-prd` (moved back from the DFW pool in `d2d80c9`).
- **Credentials:** keyless GitHub OIDC to Vault (`ae3b2b1`). Vault issues the repo-scoped Harbor
  robot for push and a namespace-scoped Kubernetes token with a lease of at most 1h for deploy.
  The old `HARBOR_*` and `KUBECONFIG_PROD` secrets are retired.
- **Cluster access:** `.github/actions/setup-kubeconfig-onprem` builds the kubeconfig against the
  in-cluster apiserver `https://kubernetes.default.svc:443` with a committed CA, and checks the
  identity with `whoami` (`e4f5b99`; replaced the retired Comcast IP).
- **Deploy:** `helm upgrade --install kubetty-gateway deploy/helm-gateway -n kubetty-gateway-prd`
  with `image.digest` pinned to the pushed digest (`999cb01`, `c620e1a`). After the rollout, the job
  checks that every Ready pod runs the linux/amd64 runtime digest, and it rolls back on failure.
  Production deploys run on `main` or on clean `vX.Y.Z` tags.
- **Gates:** gofmt, vet, `go mod tidy`, npm audit, Go tests with `-race`, Vitest, and Helm lint
  and template. The coverage threshold (30%) is advisory. Grype image scanning is opt-in.
