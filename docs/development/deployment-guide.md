# Deployment Guide

This document describes the deployment process for KubeTTY.

## Prerequisites

- Docker installed and configured
- Kubernetes cluster access via kubectl
- Helm 3.x installed
- Access to Harbor registry (harbor.support.tools)

## Build Process

### Local Development Build

```bash
# Build server binary
cd server && go build -o kubetty .

# Build web frontend
cd web && npm run build
# Output goes to ../server/ui/dist/
```

### Docker Build

```bash
# From repository root
docker build -t harbor.support.tools/kubetty/kubetty:latest .

# With specific tag
docker build -t harbor.support.tools/kubetty/kubetty:v1.0.0 .
```

The Dockerfile performs a multi-stage build:
1. Build Go server binary
2. Build React frontend
3. Create minimal runtime image

### Push to Registry

```bash
# Login to Harbor
docker login harbor.support.tools

# Push image
docker push harbor.support.tools/kubetty/kubetty:latest

# Push tagged version
docker push harbor.support.tools/kubetty/kubetty:v1.0.0
```

## Helm Chart

The Helm chart is located in `deploy/helm/`.

### Chart Structure

```
deploy/helm/
├── Chart.yaml
├── values.yaml                    # Base defaults
├── values.project-template.yaml   # Template for new project deployments
├── templates/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── configmap.yaml
│   └── ...
```

**Note:** Environment-specific values (hostnames, secrets, namespaces) are provided via `helm --set` parameters or ArgoCD Application helm.parameters, not via values files in the repository.

### Validate Chart

```bash
# Lint the chart
helm lint deploy/helm/

# Dry run installation
helm install kubetty deploy/helm/ --dry-run --debug

# Template rendering
helm template kubetty deploy/helm/
```

## Deployment Environments

### Development

```bash
# Deploy to dev namespace
helm upgrade --install kubetty ./deploy/helm \
  -n kubetty-dev \
  --create-namespace \
  -f deploy/helm/values.yaml \
  --set image.tag=latest
```

### Staging

```bash
# Deploy to staging with specific tag
helm upgrade --install kubetty ./deploy/helm \
  -n kubetty-staging \
  -f deploy/helm/values.yaml \
  --set image.tag=v1.0.0-rc1
```

### Production

```bash
# Deploy to production
helm upgrade --install kubetty ./deploy/helm \
  -n kubetty \
  -f deploy/helm/values.yaml \
  --set image.tag=v1.0.0
```

## Configuration

### Environment Variables

Key environment variables set via Helm values:

| Variable | Description | Default |
|----------|-------------|---------|
| `KUBETTY_PORT` | HTTP server port | 8080 |
| `KUBETTY_SHELL` | Shell command | /bin/bash |
| `KUBETTY_SESSION_ID` | Unique session identifier | (generated) |
| `KUBETTY_DEPLOYMENT_ID` | Deployment identifier | (from metadata) |
| `KUBETTY_CONN_STRING` | PostgreSQL connection string | (required) |
| `KUBETTY_AUTH_MODE` | Authentication mode | disabled |
| `KUBETTY_AUTH_JWT_SECRET` | JWT signing secret | (required for auth) |

### Database Configuration

KubeTTY uses CloudNativePG (CNPG) for PostgreSQL:

```yaml
# values.yaml
database:
  host: kubetty-db-rw
  port: 5432
  name: kubetty
  user: kubetty
  sslmode: require
```

The connection string is constructed as:
```
postgres://user:pass@host:port/dbname?sslmode=require
```

### Authentication Configuration

Enable local authentication:

```yaml
auth:
  mode: local
  jwtSecret: "your-secret-key"
  issuer: "kubetty"
  accessTTL: 15m
  refreshTTL: 7d
```

### Gateway Mode Configuration

For multi-project support:

```yaml
gateway:
  enabled: true
  projects:
    - id: project-1
      displayName: "Project 1"
      image: "project-image:latest"
```

## Database Migrations

Migrations run automatically on startup. For manual control:

```bash
# Apply migrations
migrate -source file://server/migrations \
  -database "postgres://..." up

# Rollback
migrate -source file://server/migrations \
  -database "postgres://..." down 1

# Check status
migrate -source file://server/migrations \
  -database "postgres://..." version
```

## Health Checks

### Endpoints

- **Liveness**: `/api/healthz` - Returns 200 if service is running
- **Readiness**: `/api/healthz` - Returns 503 if database unavailable
- **Metrics**: `/metrics` - Prometheus metrics

### Kubernetes Probes

```yaml
livenessProbe:
  httpGet:
    path: /api/healthz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /api/healthz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

## Monitoring

### Prometheus Metrics

Available at `/metrics`:

- `kubetty_sessions_total` - Total sessions created
- `kubetty_active_connections` - Current WebSocket connections
- `kubetty_ws_bytes_total` - WebSocket traffic
- `kubetty_request_duration_seconds` - HTTP request latency

### Grafana Dashboard

Import the dashboard from `deploy/grafana/dashboard.json`.

## Troubleshooting

### Check Pod Status

```bash
kubectl get pods -n kubetty
kubectl describe pod kubetty-xxx -n kubetty
kubectl logs kubetty-xxx -n kubetty
```

### Database Connectivity

```bash
# Check database pod
kubectl get pods -n kubetty -l app=kubetty-db

# Connect to database
kubectl exec -it kubetty-db-0 -n kubetty -- psql -U kubetty
```

### WebSocket Issues

```bash
# Check service endpoints
kubectl get endpoints kubetty -n kubetty

# Test WebSocket from pod
kubectl exec -it kubetty-xxx -n kubetty -- \
  curl -v http://localhost:8080/api/healthz
```

### Common Issues

**Pod CrashLoopBackOff:**
- Check logs for startup errors
- Verify database connection string
- Ensure migrations have run

**WebSocket 502 errors:**
- Check ingress timeout settings
- Verify WebSocket upgrade support
- Check service port configuration

**Authentication failures:**
- Verify JWT secret is set
- Check token expiration
- Ensure cookies are being set correctly

## Rollback

```bash
# View release history
helm history kubetty -n kubetty

# Rollback to previous release
helm rollback kubetty -n kubetty

# Rollback to specific revision
helm rollback kubetty 3 -n kubetty
```

## Deployment Checklist

Before deploying:

- [ ] All tests pass
- [ ] Docker image builds successfully
- [ ] Image pushed to registry
- [ ] Helm chart lints without errors
- [ ] Database migrations tested
- [ ] Environment variables configured
- [ ] Secrets properly set
- [ ] Health checks configured

After deploying:

- [ ] Pod starts successfully
- [ ] Health endpoint returns 200
- [ ] Database connectivity verified
- [ ] WebSocket connections work
- [ ] Authentication flow works (if enabled)
- [ ] Metrics being collected
- [ ] Logs appearing correctly
