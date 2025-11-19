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
