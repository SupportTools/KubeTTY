#!/bin/bash
set -e

# KubeTTY Production Deployment Script
# This script deploys KubeTTY to the new production namespaces
#
# Prerequisites:
# - kubectl configured with cluster access
# - helm v3 installed
# - pv-migrate installed (https://github.com/utkuozdemir/pv-migrate)
# - Docker image tagged and pushed as v1.0.0

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Configuration
GATEWAY_NS="kubetty-gateway-prd"
PROJECTS_NS="kubetty-projects-prd"
OLD_NS="kubetty-shared"
IMAGE_TAG="${IMAGE_TAG:-v1.0.0}"
DRY_RUN="${DRY_RUN:-false}"

echo "========================================"
echo "KubeTTY Production Deployment"
echo "========================================"
echo "Gateway Namespace: $GATEWAY_NS"
echo "Projects Namespace: $PROJECTS_NS"
echo "Old Namespace: $OLD_NS"
echo "Image Tag: $IMAGE_TAG"
echo "Dry Run: $DRY_RUN"
echo "========================================"
echo ""

if [ "$DRY_RUN" = "true" ]; then
    HELM_FLAGS="--dry-run"
    KUBECTL_FLAGS="--dry-run=client"
else
    HELM_FLAGS=""
    KUBECTL_FLAGS=""
fi

# Function to prompt for confirmation
confirm() {
    read -p "$1 [y/N] " response
    case "$response" in
        [yY][eE][sS]|[yY]) return 0 ;;
        *) return 1 ;;
    esac
}

echo "=== Phase 1: Create Namespaces ==="
kubectl create namespace $GATEWAY_NS $KUBECTL_FLAGS --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace $PROJECTS_NS $KUBECTL_FLAGS --dry-run=client -o yaml | kubectl apply -f -
echo "Namespaces created/verified"
echo ""

echo "=== Phase 2: Copy Secrets ==="
# Copy CNPG secret
if kubectl get secret kubetty-postgres-user -n $GATEWAY_NS &>/dev/null; then
    echo "Secret kubetty-postgres-user already exists in $GATEWAY_NS"
else
    echo "Copying kubetty-postgres-user secret..."
    kubectl get secret kubetty-postgres-user -n $OLD_NS -o yaml | \
        sed "s/namespace: $OLD_NS/namespace: $GATEWAY_NS/" | \
        kubectl apply -f -
fi

# Copy registry secret to gateway namespace
if kubectl get secret harbor-supporttools -n $GATEWAY_NS &>/dev/null; then
    echo "Secret harbor-supporttools already exists in $GATEWAY_NS"
else
    echo "Copying harbor-supporttools secret to $GATEWAY_NS..."
    kubectl get secret harbor-supporttools -n kubetty-gateway-dev -o yaml | \
        sed "s/namespace: kubetty-gateway-dev/namespace: $GATEWAY_NS/" | \
        kubectl apply -f -
fi

# Copy registry secret to projects namespace
if kubectl get secret harbor-supporttools -n $PROJECTS_NS &>/dev/null; then
    echo "Secret harbor-supporttools already exists in $PROJECTS_NS"
else
    echo "Copying harbor-supporttools secret to $PROJECTS_NS..."
    kubectl get secret harbor-supporttools -n kubetty-gateway-dev -o yaml | \
        sed "s/namespace: kubetty-gateway-dev/namespace: $PROJECTS_NS/" | \
        kubectl apply -f -
fi

# Create JWT secret for auth if not exists
if kubectl get secret kubetty-gateway-auth -n $GATEWAY_NS &>/dev/null; then
    echo "Secret kubetty-gateway-auth already exists"
else
    echo "Creating JWT secret..."
    JWT_SECRET=$(openssl rand -base64 32)
    kubectl create secret generic kubetty-gateway-auth \
        --from-literal=jwt-secret="$JWT_SECRET" \
        -n $GATEWAY_NS $KUBECTL_FLAGS
fi

# Create env-secrets for projects if not exists
if kubectl get secret env-secrets -n $PROJECTS_NS &>/dev/null; then
    echo "Secret env-secrets already exists in $PROJECTS_NS"
else
    echo "Creating env-secrets in $PROJECTS_NS..."
    kubectl create secret generic env-secrets \
        --from-literal=placeholder="placeholder" \
        -n $PROJECTS_NS $KUBECTL_FLAGS
fi
echo ""

echo "=== Phase 3: Handle Old Ingress ==="
echo "Current ingress in $OLD_NS:"
kubectl get ingress -n $OLD_NS 2>/dev/null || echo "No ingress found"
echo ""

if kubectl get ingress kubetty-ingress -n $OLD_NS &>/dev/null; then
    echo "WARNING: Old ingress exists at kubetty.support.tools"
    echo "This must be deleted or modified before deploying the new gateway."
    echo ""
    echo "Options:"
    echo "  1. Delete the old ingress"
    echo "  2. Modify the old ingress to use a different hostname"
    echo "  3. Skip (manual handling required)"
    echo ""

    read -p "Choose option [1/2/3]: " choice
    case "$choice" in
        1)
            echo "Deleting old ingress..."
            kubectl delete ingress kubetty-ingress -n $OLD_NS $KUBECTL_FLAGS
            ;;
        2)
            echo "Modifying old ingress to use kubetty-old.support.tools..."
            kubectl patch ingress kubetty-ingress -n $OLD_NS \
                --type='json' \
                -p='[{"op": "replace", "path": "/spec/rules/0/host", "value": "kubetty-old.support.tools"}]' $KUBECTL_FLAGS
            ;;
        3)
            echo "Skipping - you must manually handle the ingress before continuing"
            if ! confirm "Continue anyway?"; then
                echo "Aborted"
                exit 1
            fi
            ;;
        *)
            echo "Invalid option, skipping"
            ;;
    esac
fi
echo ""

echo "=== Phase 4: Deploy Gateway ==="
echo "Deploying kubetty-gateway to $GATEWAY_NS..."
helm upgrade --install kubetty-gateway "$PROJECT_ROOT/deploy/helm-gateway" \
    -n $GATEWAY_NS \
    -f "$PROJECT_ROOT/deploy/helm-gateway/values.prd.yaml" \
    --set image.tag="$IMAGE_TAG" \
    $HELM_FLAGS

echo "Waiting for gateway deployment..."
if [ "$DRY_RUN" != "true" ]; then
    kubectl rollout status deployment/gateway -n $GATEWAY_NS --timeout=120s
fi
echo ""

echo "=== Phase 5: Verify Gateway ==="
echo "Gateway pods:"
kubectl get pods -n $GATEWAY_NS
echo ""
echo "Gateway service:"
kubectl get svc -n $GATEWAY_NS
echo ""
echo "Gateway ingress:"
kubectl get ingress -n $GATEWAY_NS
echo ""

echo "=== Phase 6: Beacon PVC Migration (pv-migrate) ==="
echo ""
echo "This step uses pv-migrate to rsync data from the old beacon PVC to the new namespace."
echo ""

# Check if old PVC exists
if kubectl get pvc beacon-support-data -n $OLD_NS &>/dev/null; then
    echo "Source PVC found: beacon-support-data in $OLD_NS"
    kubectl get pvc beacon-support-data -n $OLD_NS
    echo ""

    if confirm "Do you want to migrate the beacon PVC now?"; then
        # Step 1: Create destination PVC
        echo "Creating destination PVC in $PROJECTS_NS..."
        SOURCE_SIZE=$(kubectl get pvc beacon-support-data -n $OLD_NS -o jsonpath='{.spec.resources.requests.storage}')
        SOURCE_SC=$(kubectl get pvc beacon-support-data -n $OLD_NS -o jsonpath='{.spec.storageClassName}')

        cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: beacon-support-data
  namespace: $PROJECTS_NS
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: ${SOURCE_SIZE:-10Gi}
  storageClassName: ${SOURCE_SC:-freenas-iscsi-csi}
EOF

        # Step 2: Scale down old deployment
        echo ""
        echo "Scaling down old beacon deployment for data consistency..."
        kubectl scale deployment beacon-support -n $OLD_NS --replicas=0 2>/dev/null || echo "No old deployment to scale"

        # Step 3: Run pv-migrate
        echo ""
        echo "Running pv-migrate..."
        if command -v pv-migrate &>/dev/null; then
            pv-migrate migrate \
                --source-namespace $OLD_NS \
                --source beacon-support-data \
                --dest-namespace $PROJECTS_NS \
                --dest beacon-support-data \
                --strategies rsync
        else
            echo "WARNING: pv-migrate not found. Please install it and run manually:"
            echo ""
            echo "  pv-migrate migrate \\"
            echo "    --source-namespace $OLD_NS \\"
            echo "    --source beacon-support-data \\"
            echo "    --dest-namespace $PROJECTS_NS \\"
            echo "    --dest beacon-support-data \\"
            echo "    --strategies rsync"
        fi
    else
        echo "Skipping PVC migration"
    fi
else
    echo "No beacon-support-data PVC found in $OLD_NS - skipping migration"
fi
echo ""

echo "=== Phase 7: Create beacon-support Project ==="
echo ""
echo "After the gateway is running, you'll need to create the beacon-support project via the API."
echo ""
echo "Example curl command (after logging in):"
echo ""
cat <<'EOF'
# Login
curl -X POST -c cookies.txt \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"YOUR_PASSWORD"}' \
  "https://kubetty.support.tools/api/auth/login"

# Create beacon-support project
curl -X POST -b cookies.txt \
  -H "Content-Type: application/json" \
  -d '{
    "name": "beacon-support",
    "displayName": "Beacon Support",
    "image": "harbor.support.tools/kubetty/kubetty:v1.0.0",
    "cpuLimit": "500m",
    "memoryLimit": "512Mi",
    "storageLimit": "10Gi",
    "idleTimeout": "4h"
  }' \
  "https://kubetty.support.tools/api/admin/projects"
EOF
echo ""

echo "=== Phase 8: Final Verification ==="
echo ""
echo "Gateway Status:"
kubectl get pods,svc,ingress -n $GATEWAY_NS
echo ""
echo "Projects Namespace Status:"
kubectl get pods,svc,pvc -n $PROJECTS_NS
echo ""

echo "========================================"
echo "Deployment Complete!"
echo "========================================"
echo ""
echo "Next steps:"
echo "1. Verify ingress is working: curl -I https://kubetty.support.tools"
echo "2. Login and create the beacon-support project via API"
echo "3. Verify the project comes up and has the migrated data"
echo "4. Once verified, clean up old resources in $OLD_NS"
echo ""
