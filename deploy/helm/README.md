# KubeTTY Helm Chart

This chart deploys a single-user KubeTTY pod plus a ClusterIP Service. Each release corresponds to one deployment (e.g., Project A, Project B) and expects a shared CNPG cluster + credentials in the `kubetty-shared` namespace.

## Prerequisites
- Namespace created (e.g., `kubectl create namespace kubetty-project-a`).
- CNPG cluster reachable; credentials secret (default: `kubetty-shared-app`) already exists in `kubetty-shared` with keys `username`, `password`, `dbname`, `host`, `port`.
- Docker image published to `harbor.support.tools/kubetty/<repo>:<tag>` following the `Makefile` workflow.

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
