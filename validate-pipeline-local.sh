#!/bin/bash
#
# validate-pipeline-local.sh
# Local validation script that mirrors the GitHub Actions CI/CD pipeline
# Run this before pushing to verify changes will pass CI
#
# Usage:
#   ./validate-pipeline-local.sh           # Run all validation stages
#   ./validate-pipeline-local.sh --quick   # Skip tests and Docker build
#   ./validate-pipeline-local.sh --full    # Include Docker build and security scan

set -e  # Exit on first error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
GO_VERSION="1.23"
NODE_VERSION="20"
COVERAGE_THRESHOLD=60
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Parse command line arguments
QUICK_MODE=false
FULL_MODE=false
SKIP_TESTS=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --quick)
      QUICK_MODE=true
      shift
      ;;
    --full)
      FULL_MODE=true
      shift
      ;;
    --skip-tests)
      SKIP_TESTS=true
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--quick|--full] [--skip-tests]"
      exit 1
      ;;
  esac
done

# Helper functions
print_stage() {
    echo -e "\n${BLUE}===================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}===================================================${NC}\n"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Stage 1: Validation
print_stage "Stage 1: Validation"

# Check Go version
print_stage "Checking Go version"
if ! command -v go &> /dev/null; then
    print_error "Go is not installed"
    exit 1
fi

GO_CURRENT_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
if [[ ! "$GO_CURRENT_VERSION" =~ ^${GO_VERSION}\. ]]; then
    print_error "Go version mismatch. Expected ${GO_VERSION}.x, got ${GO_CURRENT_VERSION}"
    echo "Please install Go ${GO_VERSION} or update GO_VERSION in this script"
    exit 1
fi
print_success "Go version: ${GO_CURRENT_VERSION}"

# Check Node.js version
print_stage "Checking Node.js version"
if ! command -v node &> /dev/null; then
    print_error "Node.js is not installed"
    exit 1
fi

NODE_CURRENT_VERSION=$(node --version | sed 's/v//' | cut -d'.' -f1)
if [ "$NODE_CURRENT_VERSION" != "$NODE_VERSION" ]; then
    print_warning "Node.js version mismatch. Expected ${NODE_VERSION}.x, got ${NODE_CURRENT_VERSION}.x"
    echo "Pipeline uses Node ${NODE_VERSION}, but continuing with current version"
fi
print_success "Node.js version: $(node --version)"

# Check Go formatting
print_stage "Checking Go formatting"
cd "${SCRIPT_DIR}/server"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
    print_error "The following files are not formatted:"
    echo "$unformatted"
    echo ""
    echo "Run: cd server && gofmt -w ."
    exit 1
fi
print_success "All Go files are properly formatted"

# Run go vet
print_stage "Running go vet"
if ! go vet ./...; then
    print_error "go vet found issues"
    exit 1
fi
print_success "go vet passed"

# Verify go.mod is tidy
print_stage "Verifying go.mod is tidy"
go mod tidy
if ! git diff --exit-code go.mod go.sum 2>/dev/null; then
    print_error "go.mod or go.sum is not tidy"
    echo ""
    echo "Run: cd server && go mod tidy"
    git diff go.mod go.sum
    exit 1
fi
print_success "go.mod and go.sum are tidy"

# Run npm audit
print_stage "Running npm audit"
cd "${SCRIPT_DIR}/web"
if npm audit --audit-level=high; then
    print_success "npm audit passed (no high/critical vulnerabilities)"
else
    print_warning "npm audit found vulnerabilities (non-blocking)"
fi

cd "${SCRIPT_DIR}"

# Stage 2: Tests
if [ "$QUICK_MODE" = false ] && [ "$SKIP_TESTS" = false ]; then
    print_stage "Stage 2: Tests"

    # Server tests
    print_stage "Running server tests"
    cd "${SCRIPT_DIR}/server"

    if go test -v -race -coverprofile=coverage.out -covermode=atomic ./...; then
        print_success "Server tests passed"

        # Check coverage
        coverage=$(go tool cover -func=coverage.out | grep total | awk '{print substr($3, 1, length($3)-1)}')
        echo "Test coverage: ${coverage}%"

        if (( $(echo "$coverage < $COVERAGE_THRESHOLD" | bc -l) )); then
            print_error "Coverage ${coverage}% is below threshold ${COVERAGE_THRESHOLD}%"
            exit 1
        fi
        print_success "Coverage ${coverage}% meets threshold ${COVERAGE_THRESHOLD}%"
    else
        print_error "Server tests failed"
        exit 1
    fi

    # Web tests
    print_stage "Running web tests"
    cd "${SCRIPT_DIR}/web"

    if [ ! -d "node_modules" ]; then
        echo "Installing npm dependencies..."
        npm ci
    fi

    if npm run test; then
        print_success "Web tests passed"
    else
        print_error "Web tests failed"
        exit 1
    fi

    # Web build (linter)
    print_stage "Running web build/linter"
    if npm run build; then
        print_success "Web build/linter passed"
    else
        print_error "Web build/linter failed"
        exit 1
    fi

    cd "${SCRIPT_DIR}"
else
    print_warning "Skipping tests (quick mode or --skip-tests)"
fi

# Stage 3: Helm validation
print_stage "Stage 3: Helm Chart Validation"

if ! command -v helm &> /dev/null; then
    print_warning "Helm is not installed, skipping Helm validation"
else
    # Lint Helm charts
    print_stage "Linting Helm charts"

    if helm lint deploy/helm/; then
        print_success "Helm lint passed (default values)"
    else
        print_error "Helm lint failed (default values)"
        exit 1
    fi

    if helm lint deploy/helm/ -f deploy/helm/values.gateway.yaml; then
        print_success "Helm lint passed (gateway values)"
    else
        print_error "Helm lint failed (gateway values)"
        exit 1
    fi

    if [ -f "deploy/helm/values.project-template.yaml" ]; then
        if helm lint deploy/helm/ -f deploy/helm/values.project-template.yaml; then
            print_success "Helm lint passed (project-template values)"
        else
            print_error "Helm lint failed (project-template values)"
            exit 1
        fi
    fi

    # Validate Helm templates
    print_stage "Validating Helm templates"

    if helm template kubetty deploy/helm/ > /dev/null; then
        print_success "Helm template validation passed (default)"
    else
        print_error "Helm template validation failed (default)"
        exit 1
    fi

    if helm template kubetty-gateway deploy/helm/ -f deploy/helm/values.gateway.yaml > /dev/null; then
        print_success "Helm template validation passed (gateway)"
    else
        print_error "Helm template validation failed (gateway)"
        exit 1
    fi

    if [ -f "deploy/helm/values.project-template.yaml" ]; then
        if helm template kubetty-project deploy/helm/ -f deploy/helm/values.project-template.yaml > /dev/null; then
            print_success "Helm template validation passed (project-template)"
        else
            print_error "Helm template validation failed (project-template)"
            exit 1
        fi
    fi
fi

# Stage 4: Docker build and security scan (only in full mode)
if [ "$FULL_MODE" = true ]; then
    print_stage "Stage 4: Docker Build and Security Scan"

    if ! command -v docker &> /dev/null; then
        print_error "Docker is not installed, cannot run full validation"
        exit 1
    fi

    # Build Docker image
    print_stage "Building Docker image"
    IMAGE_TAG="kubetty:local-validation-$(date +%s)"

    if docker build -t "$IMAGE_TAG" \
        --build-arg GO_VERSION=1.23.3 \
        --build-arg NODE_MAJOR=20 \
        .; then
        print_success "Docker build succeeded: $IMAGE_TAG"
    else
        print_error "Docker build failed"
        exit 1
    fi

    # Security scan with Trivy (if available)
    if command -v trivy &> /dev/null; then
        print_stage "Running Trivy security scan"

        echo "Scanning for HIGH and CRITICAL vulnerabilities..."
        if trivy image --severity HIGH,CRITICAL --exit-code 0 "$IMAGE_TAG"; then
            print_success "Trivy scan completed (informational)"
        else
            print_warning "Trivy scan found vulnerabilities (non-blocking in local mode)"
        fi

        echo ""
        echo "Checking for CRITICAL vulnerabilities (blocking)..."
        if trivy image --severity CRITICAL --exit-code 1 "$IMAGE_TAG"; then
            print_success "No CRITICAL vulnerabilities found"
        else
            print_error "CRITICAL vulnerabilities found - this will block CI/CD pipeline"
            echo ""
            echo "Clean up Docker image..."
            docker rmi "$IMAGE_TAG" 2>/dev/null || true
            exit 1
        fi
    else
        print_warning "Trivy not installed, skipping security scan"
        echo "Install with: brew install trivy  # or appropriate package manager"
    fi

    # Clean up Docker image
    echo ""
    echo "Cleaning up Docker image..."
    docker rmi "$IMAGE_TAG" 2>/dev/null || true
    print_success "Docker image cleaned up"
else
    print_warning "Skipping Docker build and security scan (use --full to enable)"
fi

# Summary
print_stage "Validation Summary"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
print_success "All validation checks passed!"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "Stages completed:"
echo "  ✓ Go version verification (${GO_VERSION})"
echo "  ✓ Go formatting check"
echo "  ✓ go vet"
echo "  ✓ go mod tidy verification"
echo "  ✓ npm audit"

if [ "$SKIP_TESTS" = false ] && [ "$QUICK_MODE" = false ]; then
    echo "  ✓ Server tests with race detection"
    echo "  ✓ Code coverage (${coverage}% >= ${COVERAGE_THRESHOLD}%)"
    echo "  ✓ Web tests"
    echo "  ✓ Web build/linter"
fi

if command -v helm &> /dev/null; then
    echo "  ✓ Helm chart linting"
    echo "  ✓ Helm template validation"
fi

if [ "$FULL_MODE" = true ]; then
    echo "  ✓ Docker image build"
    if command -v trivy &> /dev/null; then
        echo "  ✓ Trivy security scan"
    fi
fi

echo ""
echo "Your changes are ready to be pushed and should pass the CI/CD pipeline."
echo ""

# Provide next steps
if [ "$QUICK_MODE" = true ]; then
    echo "Note: Quick mode was used. Run without --quick to execute full test suite."
fi

if [ "$FULL_MODE" = false ]; then
    echo "Note: Run with --full to include Docker build and security scan."
fi

echo ""
echo "Recommended git workflow:"
echo "  1. Review your changes: git status"
echo "  2. Stage your changes: git add ."
echo "  3. Commit: git commit -m \"your message\""
echo "  4. Push: git push"
echo ""
