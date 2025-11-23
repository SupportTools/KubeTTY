#!/bin/bash
# KubeTTY Kubernetes Development Script
# Usage: ./scripts/dev.sh [command]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
DEV_NAMESPACE="${DEV_NAMESPACE:-kubetty-dev}"
DEV_IMAGE_TAG="${DEV_IMAGE_TAG:-dev}"
IMAGE="${IMAGE:-harbor.support.tools/kubetty/kubetty}"

usage() {
    echo -e "${BLUE}KubeTTY Kubernetes Development Script${NC}"
    echo ""
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  deploy      Build, push, and deploy to dev namespace"
    echo "  build       Build dev Docker image"
    echo "  push        Push dev image to registry"
    echo "  gateway     Deploy gateway only"
    echo "  project     Deploy project only"
    echo "  status      Show dev deployment status"
    echo "  logs        Show gateway logs"
    echo "  logs-project Show project logs"
    echo "  shell       Shell into gateway pod"
    echo "  restart     Restart deployments (pull new image)"
    echo "  destroy     Remove dev deployment"
    echo "  web         Run Vite dev server locally"
    echo "  help        Show this help"
    echo ""
    echo "Environment:"
    echo "  DEV_NAMESPACE=${DEV_NAMESPACE}"
    echo "  DEV_IMAGE_TAG=${DEV_IMAGE_TAG}"
    echo "  IMAGE=${IMAGE}"
    echo ""
}

check_kubectl() {
    if ! kubectl cluster-info > /dev/null 2>&1; then
        echo -e "${RED}ERROR: Cannot connect to Kubernetes cluster${NC}"
        exit 1
    fi
}

build_image() {
    echo -e "${BLUE}Building dev image...${NC}"
    cd "$PROJECT_ROOT"
    docker build -t "${IMAGE}:${DEV_IMAGE_TAG}" .
    echo -e "${GREEN}Image built: ${IMAGE}:${DEV_IMAGE_TAG}${NC}"
}

push_image() {
    echo -e "${BLUE}Pushing dev image...${NC}"
    docker push "${IMAGE}:${DEV_IMAGE_TAG}"
    echo -e "${GREEN}Image pushed: ${IMAGE}:${DEV_IMAGE_TAG}${NC}"
}

deploy_gateway() {
    check_kubectl
    echo -e "${BLUE}Creating namespace if needed...${NC}"
    kubectl create namespace "$DEV_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

    echo -e "${BLUE}Deploying gateway to ${DEV_NAMESPACE}...${NC}"
    helm upgrade --install gateway-dev "$PROJECT_ROOT/deploy/helm-gateway" \
        -n "$DEV_NAMESPACE" \
        -f "$PROJECT_ROOT/deploy/helm-gateway/values.dev.yaml" \
        --set "image.tag=${DEV_IMAGE_TAG}"
    echo -e "${GREEN}Gateway deployed${NC}"
}

deploy_project() {
    check_kubectl
    echo -e "${BLUE}Deploying project to ${DEV_NAMESPACE}...${NC}"
    helm upgrade --install dev-project "$PROJECT_ROOT/deploy/helm-project" \
        -n "$DEV_NAMESPACE" \
        -f "$PROJECT_ROOT/deploy/helm-project/values.dev.yaml" \
        --set "image.tag=${DEV_IMAGE_TAG}"
    echo -e "${GREEN}Project deployed${NC}"
}

full_deploy() {
    build_image
    push_image
    deploy_gateway
    deploy_project
    echo ""
    echo -e "${GREEN}Dev deployment complete!${NC}"
    echo -e "Gateway URL: https://kubetty-dev.support.tools"
}

show_status() {
    check_kubectl
    echo -e "${BLUE}Dev Namespace Status (${DEV_NAMESPACE})${NC}"
    echo ""
    echo "Pods:"
    kubectl get pods -n "$DEV_NAMESPACE" -o wide
    echo ""
    echo "Services:"
    kubectl get svc -n "$DEV_NAMESPACE"
    echo ""
    echo "Ingress:"
    kubectl get ingress -n "$DEV_NAMESPACE"
    echo ""
    echo "Helm Releases:"
    helm list -n "$DEV_NAMESPACE"
}

show_logs() {
    check_kubectl
    echo -e "${BLUE}Gateway logs (${DEV_NAMESPACE})${NC}"
    kubectl logs -n "$DEV_NAMESPACE" -l app.kubernetes.io/name=kubetty-gateway -f --tail=100
}

show_project_logs() {
    check_kubectl
    echo -e "${BLUE}Project logs (${DEV_NAMESPACE})${NC}"
    kubectl logs -n "$DEV_NAMESPACE" -l app.kubernetes.io/name=kubetty-project -f --tail=100
}

shell_gateway() {
    check_kubectl
    echo -e "${BLUE}Shelling into gateway pod...${NC}"
    kubectl exec -it -n "$DEV_NAMESPACE" deploy/gateway-dev-kubetty-gateway -- /bin/sh
}

restart_deployments() {
    check_kubectl
    echo -e "${BLUE}Restarting dev deployments...${NC}"
    kubectl rollout restart deployment -n "$DEV_NAMESPACE" gateway-dev-kubetty-gateway 2>/dev/null || true
    kubectl rollout restart deployment -n "$DEV_NAMESPACE" dev-project-kubetty-project 2>/dev/null || true
    echo -e "${GREEN}Restart initiated${NC}"
}

destroy_deployment() {
    check_kubectl
    echo -e "${YELLOW}WARNING: This will remove the dev deployment!${NC}"
    read -p "Are you sure? [y/N] " confirm
    if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
        echo "Cancelled"
        exit 0
    fi

    echo -e "${BLUE}Removing dev deployment...${NC}"
    helm uninstall gateway-dev -n "$DEV_NAMESPACE" 2>/dev/null || true
    helm uninstall dev-project -n "$DEV_NAMESPACE" 2>/dev/null || true
    echo -e "${GREEN}Dev deployment removed${NC}"
}

run_web_dev() {
    echo -e "${BLUE}Starting Vite dev server...${NC}"
    cd "$PROJECT_ROOT/web"
    npm install
    npm run dev
}

# Main command handler
case "${1:-help}" in
    deploy)
        full_deploy
        ;;
    build)
        build_image
        ;;
    push)
        push_image
        ;;
    gateway)
        deploy_gateway
        ;;
    project)
        deploy_project
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs
        ;;
    logs-project)
        show_project_logs
        ;;
    shell)
        shell_gateway
        ;;
    restart)
        restart_deployments
        ;;
    destroy)
        destroy_deployment
        ;;
    web)
        run_web_dev
        ;;
    help|--help|-h)
        usage
        ;;
    *)
        echo -e "${RED}Unknown command: $1${NC}"
        usage
        exit 1
        ;;
esac
