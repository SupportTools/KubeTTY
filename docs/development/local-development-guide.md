# Development Environment Guide

This guide covers setting up and running KubeTTY in a development environment within the Kubernetes cluster.

## Prerequisites

- **Go 1.23+** - [Download](https://golang.org/dl/)
- **Node.js 20+** - [Download](https://nodejs.org/)
- **Docker** - For building container images
- **kubectl** - Configured with cluster access
- **Helm 3** - For deployments
- **Make** - Build automation

Verify prerequisites:
```bash
make check-prerequisites
```

## Quick Start

```bash
# Build, push, and deploy to dev namespace
make dev-deploy

# Check status
make dev-status

# View logs
make dev-logs

# Access at https://kubetty-dev.support.tools
```

## Development Workflow

### Full Deploy (Build + Push + Deploy)

```bash
# One command to build image, push to registry, and deploy
make dev-deploy

# Or use the script directly
./scripts/dev.sh deploy
```

This will:
1. Build Docker image with `dev` tag
2. Push to `harbor.support.tools/kubetty/kubetty:dev`
3. Create `kubetty-dev` namespace if needed
4. Deploy gateway and project via Helm

### Incremental Updates

After making code changes:

```bash
# Rebuild and push image
make dev-push

# Restart pods to pick up new image
make dev-restart
```

Or for a full redeploy:
```bash
make dev-deploy
```

### Deploy Individual Components

```bash
# Gateway only
make dev-deploy-gateway

# Project only
make dev-deploy-project
```

## Dev Environment Details

### Namespace

All dev resources deploy to `kubetty-dev` namespace.

### URLs

| Service | URL |
|---------|-----|
| Gateway | https://kubetty-dev.support.tools |
| Project | Internal (accessed via gateway) |

### Image Tag

Dev builds use the `dev` tag:
- `harbor.support.tools/kubetty/kubetty:dev`

Override with:
```bash
DEV_IMAGE_TAG=my-feature make dev-deploy
```

### Database

Dev uses the same CNPG cluster as production (`kubetty-shared-rw`), sharing the `kubetty` database. Session data is isolated by deployment ID.

## Available Commands

### Make Targets

| Command | Description |
|---------|-------------|
| `make dev-deploy` | Full build, push, and deploy |
| `make dev-build` | Build dev Docker image |
| `make dev-push` | Build and push dev image |
| `make dev-deploy-gateway` | Deploy gateway only |
| `make dev-deploy-project` | Deploy project only |
| `make dev-status` | Show deployment status |
| `make dev-logs` | Stream gateway logs |
| `make dev-logs-project` | Stream project logs |
| `make dev-shell` | Shell into gateway pod |
| `make dev-restart` | Restart pods (pull new image) |
| `make dev-web` | Run Vite dev server locally |
| `make dev-destroy` | Remove dev deployment |

### Script Commands

```bash
./scripts/dev.sh deploy      # Full deploy
./scripts/dev.sh build       # Build image
./scripts/dev.sh push        # Push image
./scripts/dev.sh gateway     # Deploy gateway
./scripts/dev.sh project     # Deploy project
./scripts/dev.sh status      # Show status
./scripts/dev.sh logs        # Gateway logs
./scripts/dev.sh logs-project # Project logs
./scripts/dev.sh shell       # Shell into gateway
./scripts/dev.sh restart     # Restart pods
./scripts/dev.sh destroy     # Remove deployment
./scripts/dev.sh web         # Vite dev server
```

## Monitoring & Debugging

### View Logs

```bash
# Gateway logs
make dev-logs

# Project logs
make dev-logs-project

# Or via kubectl
kubectl logs -n kubetty-dev -l app.kubernetes.io/name=kubetty-gateway -f
```

### Check Status

```bash
make dev-status
```

Shows pods, services, ingress, and Helm releases.

### Shell Access

```bash
# Gateway pod
make dev-shell

# Project pod
kubectl exec -it -n kubetty-dev deploy/dev-project-kubetty-project -- /bin/bash
```

### Debug Pod Issues

```bash
# Describe pod
kubectl describe pod -n kubetty-dev -l app.kubernetes.io/name=kubetty-gateway

# Check events
kubectl get events -n kubetty-dev --sort-by='.lastTimestamp'
```

## Frontend Development

For rapid frontend iteration with hot reload:

```bash
# Terminal 1: Ensure dev backend is deployed
make dev-deploy

# Terminal 2: Run Vite dev server locally
make dev-web
# Opens at http://localhost:5173
```

The Vite dev server provides hot module replacement. Configure it to proxy API requests to the dev cluster if needed.

## Configuration

### Helm Values

Dev-specific values are in:
- `deploy/helm-gateway/values.dev.yaml` - Gateway config
- `deploy/helm-project/values.dev.yaml` - Project config

Key differences from production:
- Lower resource limits
- Debug logging enabled
- Auth disabled by default
- Smaller PVC size
- Separate ingress hostname

### Environment Variables

Override defaults:
```bash
export DEV_NAMESPACE=kubetty-feature-x
export DEV_IMAGE_TAG=my-branch
make dev-deploy
```

## Testing Changes

### Unit Tests

```bash
# Go tests
make test-server-local

# Web tests
make test-web-local

# All tests
make test-all-local
```

### Integration Testing

1. Deploy to dev: `make dev-deploy`
2. Open https://kubetty-dev.support.tools
3. Test functionality manually
4. Check logs: `make dev-logs`

### Validation Pipeline

Before creating a PR:
```bash
# Quick validation
make validate-quick

# Full CI mirror
make validate-pipeline-local
```

## Cleanup

### Remove Dev Deployment

```bash
make dev-destroy
```

This removes the Helm releases but preserves PVCs.

### Full Cleanup

```bash
# Remove releases
make dev-destroy

# Delete namespace (removes PVCs too)
kubectl delete namespace kubetty-dev
```

## Troubleshooting

### Image Pull Errors

```bash
# Check image exists
docker pull harbor.support.tools/kubetty/kubetty:dev

# Verify image pull secret
kubectl get secret -n kubetty-dev harbor-supporttools
```

### Pod Not Starting

```bash
# Check pod status
kubectl get pods -n kubetty-dev

# View events
kubectl describe pod -n kubetty-dev <pod-name>

# Check logs
kubectl logs -n kubetty-dev <pod-name> --previous
```

### Database Connection Issues

```bash
# Verify CNPG cluster
kubectl get cluster -n kubetty-shared

# Check secret exists
kubectl get secret -n kubetty-dev kubetty-postgres-user
```

The dev gateway connects to the production CNPG cluster. Ensure the secret is copied or accessible.

### Ingress Not Working

```bash
# Check ingress
kubectl get ingress -n kubetty-dev

# Verify DNS
nslookup kubetty-dev.support.tools

# Check nginx controller logs
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx
```
