# KubeTTY Helm Chart

This chart deploys KubeTTY in either gateway mode (multi-project tabbed interface) or project mode (single-user terminal). All deployments require a PostgreSQL database (CloudNativePG) for session persistence.

## Prerequisites

- Kubernetes cluster (v1.24+)
- Helm 3.x
- CloudNativePG cluster deployed and reachable
- Namespace created (e.g., `kubectl create namespace kubetty-project-a`)
- Docker image published to `harbor.support.tools/kubetty/<repo>:<tag>`

## Required Configuration

Before deploying KubeTTY, you **must** configure the following:

### 1. PostgreSQL Database (CNPG)

KubeTTY requires PostgreSQL for session persistence. You must provide:

- **`cnpg.host`**: PostgreSQL host (CNPG cluster service name or IP)
  ```yaml
  cnpg:
    host: "kubetty-postgres-rw.kubetty-shared.svc.cluster.local"
  ```

- **`cnpg.userSecret`**: Kubernetes secret containing database credentials

  Create the secret with:
  ```bash
  kubectl create secret generic kubetty-postgres-user \
    --namespace kubetty-shared \
    --from-literal=username=kubetty_user \
    --from-literal=password=<secure-random-password>
  ```

  Then reference it in values:
  ```yaml
  cnpg:
    userSecret: kubetty-postgres-user
    usernameKey: username
    passwordKey: password
  ```

### 2. Session UUID (Project Mode)

Each project deployment needs a **stable** UUID that persists sessions across pod restarts:

- **Generate once** during project creation:
  ```bash
  # Linux/Mac
  uuidgen

  # PowerShell
  New-Guid
  ```

- **Do NOT change** after initial deployment (breaks session continuity)
- **Do NOT use** placeholder UUIDs like `00000000-0000-0000-0000-000000000000`

Example:
```yaml
env:
  sessionID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"  # Generate and keep stable
```

Gateway mode can leave `sessionID` empty:
```yaml
env:
  sessionID: ""  # Valid for gateway mode
```

### 3. JWT Secret (Gateway Mode Only)

If deploying in gateway mode with authentication:

```bash
# Generate secure random secret
kubectl create secret generic kubetty-gateway-auth \
  --namespace kubetty-shared \
  --from-literal=jwt-secret=$(openssl rand -base64 32)
```

Reference in values:
```yaml
auth:
  mode: local
  jwtSecretSecret:
    name: kubetty-gateway-auth
    key: jwt-secret
```

## Installing
```
helm upgrade --install kubetty-project-a ./deploy/helm \
  -n kubetty-project-a \
  -f deploy/helm/values.project-a.yaml
```

After deployment, expose locally:
```
kubectl -n kubetty-project-a port-forward deploy/kubetty-project-a-kubetty 8080:8080
open http://localhost:8080
```

## Configuration
Key values in `values.yaml`:
- `image.repository` / `image.tag`: reference Harbor image.
- `env.sessionID`: UUID that identifies the default session for this deployment.
- `env.claudeMaxTokens`, `env.anthropicBaseURL`, `env.extra`: LLM env vars.
- `cnpg` block: host/port/database + `userSecret` name/keys.
- `deploymentId`: label used when persisting sessions.

See `values.project-a.yaml` as a template.

## Project Mode (Individual Terminals)

Project pods provide isolated terminal environments accessed through the gateway. They run in project mode with authentication disabled (gateway handles auth).

### Creating a New Project Deployment

1. Copy the template:
```bash
cp deploy/helm/values.project-template.yaml deploy/helm/values.my-project.yaml
```

2. Edit `values.my-project.yaml` and update:
   - `namespace`: The Kubernetes namespace for this project (e.g., `kubetty-my-project`)
   - `releaseName`: Helm release name (typically matches project name)
   - `deploymentId`: Unique identifier for this project
   - `env.sessionID`: Generate a new UUID for this project
   - `env.kubettyProject`: Project identifier matching gateway catalog
   - `rbac.adminNamespaces`: Namespaces this project can manage

3. Create the namespace:
```bash
kubectl create namespace kubetty-my-project
```

4. Deploy the project pod:
```bash
helm upgrade --install kubetty-my-project ./deploy/helm \
  -n kubetty-my-project \
  -f deploy/helm/values.my-project.yaml
```

5. Add the project to the gateway catalog using `--set` parameters or update the gateway deployment:
```bash
# Update the gateway deployment with a new project in the catalog
helm upgrade kubetty-gateway ./deploy/helm \
  -n kubetty-shared \
  --reuse-values \
  --set-string 'gateway.catalog.inline=projects:\n  - id: my-project\n    displayName: "My Project"\n    namespace: kubetty-my-project\n    service: kubetty-my-project-kubetty\n    port: 8080'
```

Or configure the catalog inline in your gateway deployment values.

### Project Mode Configuration

Key differences from gateway mode:
- `env.kubettyMode`: Set to `"project"` (not `"gateway"`)
- `auth.mode`: Set to `"disabled"` (gateway handles authentication)
- `ingress.enabled`: Set to `false` (accessed via gateway WebSocket proxy)
- `dataPVC.enabled`: Set to `true` (project pods need persistent storage)
- `rbac`: Namespace-scoped access (not cluster-wide)

## Gateway Mode (Tabbed UI)

Deploying the shared “hub” instance only requires toggling the `gateway` values:

```yaml
gateway:
  enabled: true
  catalog:
    inline: |
      projects:
        - id: ai-dev
          displayName: "AI Platform"
          namespace: kubetty-ai
          service: kubetty-ai-kubetty
          port: 8080
    mountPath: /etc/kubetty
    fileName: projects.yaml
```

- Setting `gateway.enabled=true` injects the `PROJECT_CATALOG_PATH` env var so the server boots in gateway mode.
- Provide either `gateway.catalog.inline` (embedded YAML) or `gateway.catalog.existingConfigMap` (name of a pre-created ConfigMap). The chart mounts the config and points the server at the rendered file.
- The catalog entries describe downstream releases (namespace + Service) that already run the standard KubeTTY pod.

Once deployed, expose the gateway via ingress or port-forwarding; the React UI will show a tab bar with a `+` button to open shells for each configured project.

## Troubleshooting

### Pod Won't Start

**Validation errors during deployment:**
```
Error: execution error at (kubetty/templates/deployment.yaml:4:5):
cnpg.host is required. Please specify the PostgreSQL host
```

**Solution**: Ensure required values are configured:
- `cnpg.host` must be set to your PostgreSQL service name or IP
- `cnpg.userSecret` must reference an existing Kubernetes secret
- `env.sessionID` cannot be the placeholder UUID `00000000-0000-0000-0000-000000000000`

**Database connection errors:**
```
Error: failed to connect to database: connection refused
```

**Solution**:
1. Verify CNPG secret exists in the correct namespace:
   ```bash
   kubectl get secret kubetty-postgres-user -n kubetty-shared
   ```

2. Test database connectivity from a debug pod:
   ```bash
   kubectl run -it --rm debug --image=postgres:15 --restart=Never -- \
     psql -h kubetty-postgres-rw.kubetty-shared.svc.cluster.local \
          -U kubetty_user -d kubetty
   ```

3. Check CNPG cluster status:
   ```bash
   kubectl get cluster -n kubetty-shared
   kubectl get pods -n kubetty-shared -l cnpg.io/cluster=kubetty-postgres
   ```

### WebSocket Connection Issues

**Symptoms**: Terminal connects but immediately disconnects, or "connecting..." never completes

**Solutions**:

1. **Check ingress annotations for WebSocket support:**
   ```yaml
   ingress:
     annotations:
       nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
       nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
       nginx.ingress.kubernetes.io/proxy-connect-timeout: "60"
   ```

2. **Verify service and pod are in same namespace:**
   ```bash
   kubectl get svc,pods -n kubetty-project-a
   ```

3. **Check pod logs for connection errors:**
   ```bash
   kubectl logs -n kubetty-project-a -l app.kubernetes.io/name=kubetty -f
   ```

### Sessions Not Persisting

**Symptoms**: Terminal sessions are lost after pod restart

**Solutions**:

1. **Verify sessionID is stable (not changing between deployments):**
   ```bash
   # Check current sessionID value
   kubectl get deploy kubetty-project-a-kubetty -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="SESSION_ID")].value}'

   # Ensure it's the same UUID across deployments
   ```

2. **Check PostgreSQL sessions table:**
   ```bash
   kubectl exec -it -n kubetty-shared kubetty-postgres-rw-1 -- \
     psql -U kubetty_user -d kubetty -c "SELECT * FROM sessions;"
   ```

3. **Verify dataPVC is mounted correctly (project mode):**
   ```bash
   kubectl describe pod -n kubetty-project-a -l app.kubernetes.io/name=kubetty | grep -A5 Mounts
   ```

### Gateway Can't Connect to Project Pods

**Symptoms**: Gateway shows project in catalog but tab creation fails

**Solutions**:

1. **Verify project pod is running:**
   ```bash
   kubectl get pods -n kubetty-my-project -l app.kubernetes.io/name=kubetty
   ```

2. **Check service DNS is resolvable from gateway:**
   ```bash
   kubectl exec -it -n kubetty-shared deploy/kubetty-shared-gateway-kubetty -- \
     nslookup kubetty-my-project-kubetty.kubetty-my-project.svc.cluster.local
   ```

3. **Verify gateway catalog configuration:**
   ```bash
   kubectl exec -it -n kubetty-shared deploy/kubetty-shared-gateway-kubetty -- \
     cat /etc/kubetty/projects.yaml
   ```

4. **Check network policies allow traffic between namespaces:**
   ```bash
   kubectl get networkpolicies -n kubetty-my-project
   kubectl get networkpolicies -n kubetty-shared
   ```

### Common Configuration Mistakes

| Issue | Symptom | Solution |
|-------|---------|----------|
| Placeholder UUID | `sessionID: "00000000..."` | Generate new UUID with `uuidgen` |
| Changed UUID | Sessions lost after upgrade | Never change sessionID after initial deployment |
| Missing CNPG secret | Pod CrashLoopBackOff | Create secret with database credentials |
| Wrong CNPG host | Connection timeout | Use full service name: `<svc>.<ns>.svc.cluster.local` |
| Missing JWT secret | Auth fails (gateway mode) | Create secret with random JWT signing key |
| Wrong service name in catalog | Gateway can't connect to project | Verify service name matches: `kubectl get svc -n <project-ns>` |

### Debugging Tips

1. **Enable debug logging:**
   ```yaml
   env:
     extra:
       DEBUG: "true"
   ```

2. **Check all pod events:**
   ```bash
   kubectl describe pod -n <namespace> -l app.kubernetes.io/name=kubetty
   ```

3. **Monitor logs from all containers:**
   ```bash
   # Main container
   kubectl logs -n <namespace> -l app.kubernetes.io/name=kubetty -c kubetty -f

   # DinD sidecar (if enabled)
   kubectl logs -n <namespace> -l app.kubernetes.io/name=kubetty -c dind -f
   ```

4. **Test database connectivity from pod:**
   ```bash
   kubectl exec -it -n <namespace> deploy/<deployment-name> -- \
     psql -h $CNPG_HOST -U $CNPG_USER -d $CNPG_DATABASE
   ```

### Getting Help

- **Documentation**: [GitHub Repository](https://github.com/supporttools/KubeTTY)
- **Issues**: [Bug Reports & Feature Requests](https://github.com/supporttools/KubeTTY/issues)
- **Logs**: Always include pod logs when reporting issues
