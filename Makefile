SHELL := /bin/bash
.DEFAULT_GOAL := help

# =============================================================================
# Configuration
# =============================================================================
IMAGE ?= harbor.support.tools/kubetty/kubetty
TAG ?= dev
REGISTRY_IMAGE := $(IMAGE):$(TAG)
GO_VERSION ?= 1.23.2
NODE_MAJOR ?= 20
COVERAGE_THRESHOLD ?= 60

# Helm deployment defaults
HELM_CHART := deploy/helm
HELM_RELEASE ?= kubetty
HELM_NAMESPACE ?= kubetty

# Colors for output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
BLUE := \033[0;34m
NC := \033[0m # No Color

# =============================================================================
# Prerequisites Checks
# =============================================================================
.PHONY: check-prerequisites check-docker check-go-version check-node-version

## check-prerequisites: Verify all required tools are installed
check-prerequisites: check-go-version check-node-version check-docker
	@echo -e "$(GREEN)All prerequisites satisfied$(NC)"

## check-docker: Verify Docker daemon is running
check-docker:
	@echo -e "$(BLUE)Checking Docker...$(NC)"
	@if ! docker info > /dev/null 2>&1; then \
		echo -e "$(RED)ERROR: Docker daemon is not running$(NC)"; \
		echo "Please start Docker and try again"; \
		exit 1; \
	fi
	@echo -e "$(GREEN)Docker is running$(NC)"

## check-go-version: Verify Go 1.23+ is installed
check-go-version:
	@echo -e "$(BLUE)Checking Go version...$(NC)"
	@if ! command -v go &> /dev/null; then \
		echo -e "$(RED)ERROR: Go is not installed$(NC)"; \
		echo "Please install Go 1.23+ from https://golang.org/dl/"; \
		exit 1; \
	fi
	@GO_VER=$$(go version | awk '{print $$3}' | sed 's/go//'); \
	GO_MAJOR=$$(echo $$GO_VER | cut -d. -f1); \
	GO_MINOR=$$(echo $$GO_VER | cut -d. -f2); \
	if [ "$$GO_MAJOR" -lt 1 ] || ([ "$$GO_MAJOR" -eq 1 ] && [ "$$GO_MINOR" -lt 23 ]); then \
		echo -e "$(RED)ERROR: Go version 1.23+ required, found $$GO_VER$(NC)"; \
		exit 1; \
	fi
	@echo -e "$(GREEN)Go version OK: $$(go version | awk '{print $$3}')$(NC)"

## check-node-version: Verify Node.js is installed
check-node-version:
	@echo -e "$(BLUE)Checking Node.js version...$(NC)"
	@if ! command -v node &> /dev/null; then \
		echo -e "$(RED)ERROR: Node.js is not installed$(NC)"; \
		echo "Please install Node.js 20+ from https://nodejs.org/"; \
		exit 1; \
	fi
	@NODE_VER=$$(node --version | sed 's/v//'); \
	NODE_MAJOR_VER=$$(echo $$NODE_VER | cut -d. -f1); \
	if [ "$$NODE_MAJOR_VER" -lt 18 ]; then \
		echo -e "$(YELLOW)WARNING: Node.js 20+ recommended, found v$$NODE_VER$(NC)"; \
	else \
		echo -e "$(GREEN)Node.js version OK: v$$NODE_VER$(NC)"; \
	fi

# =============================================================================
# Local Build Targets
# =============================================================================
.PHONY: build-web-local build-server-local build-all-local ui server

## build-web-local: Build web assets locally
build-web-local:
	@echo -e "$(BLUE)==> Installing web dependencies$(NC)"
	npm --prefix web install
	@echo -e "$(BLUE)==> Building web bundle$(NC)"
	npm --prefix web run build
	@echo -e "$(GREEN)==> Web build complete$(NC)"

## build-server-local: Build all server binaries locally
build-server-local: build-web-local
	@echo -e "$(BLUE)==> Building server binaries$(NC)"
	mkdir -p bin
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/kubetty-gateway ./cmd/gateway
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/kubetty-project ./cmd/project
	@echo -e "$(GREEN)==> Server binaries built in bin/$(NC)"

## build-all-local: Build all components locally
build-all-local: build-server-local
	@echo -e "$(GREEN)==> All local builds complete$(NC)"

# Legacy targets for backward compatibility
ui: build-web-local

server: build-web-local
	@echo -e "$(BLUE)==> Running Go fmt$(NC)"
	cd server && go fmt ./...
	@echo -e "$(BLUE)==> Running Go tests$(NC)"
	cd server && go test ./...
	@echo -e "$(BLUE)==> Building kubetty binary$(NC)"
	mkdir -p bin
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/kubetty .

# =============================================================================
# Docker Build Targets
# =============================================================================
.PHONY: build-server-image build-all-images push-all-images docker-build docker-push

## build-server-image: Build Docker image
build-server-image: check-docker
	@echo -e "$(BLUE)==> Building Docker image $(REGISTRY_IMAGE)$(NC)"
	docker build --build-arg GO_VERSION=$(GO_VERSION) --build-arg NODE_MAJOR=$(NODE_MAJOR) -t $(REGISTRY_IMAGE) .
	@echo -e "$(GREEN)==> Docker image built: $(REGISTRY_IMAGE)$(NC)"

## build-all-images: Build all Docker images (alias for build-server-image)
build-all-images: build-server-image

## push-all-images: Push all images to registry
push-all-images: check-docker
	@echo -e "$(BLUE)==> Pushing Docker image $(REGISTRY_IMAGE)$(NC)"
	docker push $(REGISTRY_IMAGE)
	@echo -e "$(GREEN)==> Docker image pushed: $(REGISTRY_IMAGE)$(NC)"

# Legacy targets for backward compatibility
docker-build: build-server-image

docker-push: push-all-images

# =============================================================================
# Testing Targets
# =============================================================================
.PHONY: test-server-local test-web-local test-all-local test-coverage npm-audit

## test-server-local: Run Go tests with race detection
test-server-local:
	@echo -e "$(BLUE)==> Running Go tests$(NC)"
	cd server && go test -v -race ./...
	@echo -e "$(GREEN)==> Go tests passed$(NC)"

## test-web-local: Run web tests
test-web-local:
	@echo -e "$(BLUE)==> Running web tests$(NC)"
	npm --prefix web run test
	@echo -e "$(GREEN)==> Web tests passed$(NC)"

## test-all-local: Run all tests
test-all-local: test-server-local test-web-local
	@echo -e "$(GREEN)==> All tests passed$(NC)"

## test-coverage: Run tests with coverage report
test-coverage:
	@echo -e "$(BLUE)==> Running Go tests with coverage$(NC)"
	cd server && go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@COVERAGE=$$(cd server && go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo -e "$(BLUE)Coverage: $${COVERAGE}%$(NC)"; \
	if [ $$(echo "$$COVERAGE < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo -e "$(RED)ERROR: Coverage $${COVERAGE}% is below threshold $(COVERAGE_THRESHOLD)%$(NC)"; \
		exit 1; \
	fi
	@echo -e "$(GREEN)==> Coverage meets threshold$(NC)"

## npm-audit: Run npm security audit (mirrors CI)
npm-audit:
	@echo -e "$(BLUE)==> Running npm security audit$(NC)"
	npm --prefix web audit --audit-level=high || true
	@echo -e "$(GREEN)==> npm audit complete$(NC)"

# =============================================================================
# Validation Targets (CI/CD Mirror)
# =============================================================================
.PHONY: validate-pipeline-local validate-quick qa-check fmt vet lint

## validate-pipeline-local: Run full CI/CD pipeline locally (mirrors CI exactly)
validate-pipeline-local: check-prerequisites npm-audit qa-check test-coverage build-server-image helm-lint-all
	@echo -e "$(GREEN)==> Full pipeline validation passed$(NC)"

## validate-quick: Quick validation without Docker build
validate-quick: check-go-version check-node-version qa-check test-server-local helm-lint
	@echo -e "$(GREEN)==> Quick validation passed$(NC)"

## qa-check: Run all QA checks (fmt, vet, lint)
qa-check: fmt vet lint
	@echo -e "$(GREEN)==> QA checks passed$(NC)"

## fmt: Format Go code and check for changes
fmt:
	@echo -e "$(BLUE)==> Checking Go formatting$(NC)"
	@cd server && gofmt -l . > /tmp/gofmt.out 2>&1 || true
	@if [ -s /tmp/gofmt.out ]; then \
		echo -e "$(RED)ERROR: The following files are not formatted:$(NC)"; \
		cat /tmp/gofmt.out; \
		echo -e "$(YELLOW)Run: cd server && gofmt -w .$(NC)"; \
		exit 1; \
	fi
	@echo -e "$(GREEN)==> Go files are properly formatted$(NC)"

## vet: Run go vet
vet:
	@echo -e "$(BLUE)==> Running go vet$(NC)"
	cd server && go vet ./...
	@echo -e "$(GREEN)==> go vet passed$(NC)"

## lint: Run linting checks
lint: fmt vet
	@echo -e "$(BLUE)==> Checking go.mod tidy$(NC)"
	@cd server && go mod tidy
	@if ! git diff --quiet server/go.mod server/go.sum 2>/dev/null; then \
		echo -e "$(YELLOW)WARNING: go.mod or go.sum changed after tidy$(NC)"; \
	fi
	@echo -e "$(GREEN)==> Linting passed$(NC)"

# =============================================================================
# Helm Targets
# =============================================================================
.PHONY: helm-lint helm-lint-all helm-package helm-template helm-install

## helm-lint: Lint Helm chart (basic)
helm-lint:
	@echo -e "$(BLUE)==> Linting Helm chart$(NC)"
	helm lint $(HELM_CHART)
	@echo -e "$(GREEN)==> Helm lint passed$(NC)"

## helm-lint-all: Lint Helm chart with all value files (mirrors CI)
helm-lint-all:
	@echo -e "$(BLUE)==> Linting Helm chart with all value files$(NC)"
	helm lint $(HELM_CHART) \
		--set cnpg.host=test-db.svc.cluster.local \
		--set cnpg.userSecret=test-secret \
		--set env.sessionID=00000000-0000-0000-0000-000000000001
	helm lint $(HELM_CHART) -f $(HELM_CHART)/values.project-template.yaml \
		--set env.sessionID=00000000-0000-0000-0000-000000000001
	@echo -e "$(GREEN)==> Helm lint (all configs) passed$(NC)"

## helm-package: Package Helm chart
helm-package: helm-lint
	@echo -e "$(BLUE)==> Packaging Helm chart$(NC)"
	helm package $(HELM_CHART)
	@echo -e "$(GREEN)==> Helm chart packaged$(NC)"

## helm-template: Render Helm templates (dry-run)
helm-template:
	@echo -e "$(BLUE)==> Rendering Helm templates$(NC)"
	helm template $(HELM_RELEASE) $(HELM_CHART) -f $(HELM_CHART)/values.yaml

## helm-install: Install/upgrade Helm release (requires VALUES, NAMESPACE, RELEASE)
helm-install:
ifndef VALUES
	$(error "VALUES=<path/to/values.yaml> is required")
endif
ifndef NAMESPACE
	$(error "NAMESPACE=<target namespace> is required")
endif
ifndef RELEASE
	$(error "RELEASE=<helm release> is required")
endif
	@echo -e "$(BLUE)==> Installing Helm release $(RELEASE) in $(NAMESPACE)$(NC)"
	helm upgrade --install $(RELEASE) $(HELM_CHART) -n $(NAMESPACE) -f $(VALUES)
	@echo -e "$(GREEN)==> Helm release $(RELEASE) installed$(NC)"

# =============================================================================
# Deployment Targets
# =============================================================================
.PHONY: deploy-prod

## deploy-prod: Deploy to production (requires confirmation)
deploy-prod: check-docker helm-lint
	@echo -e "$(YELLOW)==> WARNING: Deploying to PRODUCTION$(NC)"
	@read -p "Are you sure you want to deploy to production? [y/N] " confirm; \
	if [ "$$confirm" != "y" ] && [ "$$confirm" != "Y" ]; then \
		echo -e "$(RED)Deployment cancelled$(NC)"; \
		exit 1; \
	fi
	@echo -e "$(BLUE)==> Building and pushing production image$(NC)"
	$(MAKE) build-server-image push-all-images TAG=$(TAG)
	@echo -e "$(BLUE)==> Deploying to production$(NC)"
	helm upgrade --install $(HELM_RELEASE) $(HELM_CHART) \
		-n $(HELM_NAMESPACE) \
		-f $(HELM_CHART)/values.yaml \
		--set image.tag=$(TAG)
	@echo -e "$(GREEN)==> Production deployment complete$(NC)"

# =============================================================================
# Utility Targets
# =============================================================================
.PHONY: clean clean-docker bump info help

## clean: Remove build artifacts
clean:
	@echo -e "$(BLUE)==> Cleaning build artifacts$(NC)"
	rm -rf server/ui/dist web/node_modules web/dist bin
	rm -f server/coverage.out server/coverage-*.out
	@echo -e "$(GREEN)==> Clean complete$(NC)"

## clean-docker: Remove Docker images
clean-docker:
	@echo -e "$(BLUE)==> Removing Docker images$(NC)"
	-docker rmi $(IMAGE):dev 2>/dev/null || true
	-docker rmi $(IMAGE):latest 2>/dev/null || true
	@echo -e "$(GREEN)==> Docker images removed$(NC)"

## bump: Show version bump helper
bump:
	@echo -e "$(BLUE)Version Bump Helper$(NC)"
	@echo ""
	@echo "Current tags:"
	@git tag --sort=-version:refname | head -5
	@echo ""
	@echo "To create a new version tag:"
	@echo "  git tag v1.x.x"
	@echo "  git push origin v1.x.x"
	@echo ""
	@echo "Note: Use semantic versioning (v1.2.3), no suffixes like -rc or -beta"

## info: Display build information
info:
	@echo -e "$(BLUE)Build Information$(NC)"
	@echo "  Image:      $(IMAGE)"
	@echo "  Tag:        $(TAG)"
	@echo "  Registry:   $(REGISTRY_IMAGE)"
	@echo "  Go Version: $(GO_VERSION)"
	@echo "  Node Major: $(NODE_MAJOR)"
	@echo "  Coverage:   $(COVERAGE_THRESHOLD)%"
	@echo ""
	@echo "Environment:"
	@echo "  Go:    $$(go version 2>/dev/null || echo 'not installed')"
	@echo "  Node:  $$(node --version 2>/dev/null || echo 'not installed')"
	@echo "  Docker: $$(docker --version 2>/dev/null || echo 'not installed')"
	@echo "  Helm:  $$(helm version --short 2>/dev/null || echo 'not installed')"

## help: Show this help message
help:
	@echo -e "$(BLUE)KubeTTY Makefile$(NC)"
	@echo ""
	@echo "Usage: make [target] [VAR=value]"
	@echo ""
	@echo -e "$(GREEN)Prerequisites:$(NC)"
	@grep -E '^## check-' $(MAKEFILE_LIST) | sed 's/## /  /' | sed 's/: /\t- /'
	@echo ""
	@echo -e "$(GREEN)Local Builds:$(NC)"
	@grep -E '^## build-.*-local' $(MAKEFILE_LIST) | sed 's/## /  /' | sed 's/: /\t- /'
	@echo ""
	@echo -e "$(GREEN)Docker:$(NC)"
	@grep -E '^## (build-.*-image|push-)' $(MAKEFILE_LIST) | sed 's/## /  /' | sed 's/: /\t- /'
	@echo ""
	@echo -e "$(GREEN)Testing:$(NC)"
	@grep -E '^## (test-|npm-audit)' $(MAKEFILE_LIST) | sed 's/## /  /' | sed 's/: /\t- /'
	@echo ""
	@echo -e "$(GREEN)Validation:$(NC)"
	@grep -E '^## (validate-|qa-|fmt|vet|lint)' $(MAKEFILE_LIST) | sed 's/## /  /' | sed 's/: /\t- /'
	@echo ""
	@echo -e "$(GREEN)Helm:$(NC)"
	@grep -E '^## helm-' $(MAKEFILE_LIST) | sed 's/## /  /' | sed 's/: /\t- /'
	@echo ""
	@echo -e "$(GREEN)Deployment:$(NC)"
	@grep -E '^## deploy-' $(MAKEFILE_LIST) | sed 's/## /  /' | sed 's/: /\t- /'
	@echo ""
	@echo -e "$(GREEN)Utility:$(NC)"
	@grep -E '^## (clean|bump|info|help)' $(MAKEFILE_LIST) | sed 's/## /  /' | sed 's/: /\t- /'
	@echo ""
	@echo -e "$(YELLOW)Variables:$(NC)"
	@echo "  IMAGE=$(IMAGE)"
	@echo "  TAG=$(TAG)"
	@echo "  GO_VERSION=$(GO_VERSION)"
	@echo "  NODE_MAJOR=$(NODE_MAJOR)"
	@echo "  COVERAGE_THRESHOLD=$(COVERAGE_THRESHOLD)"
	@echo "  HELM_RELEASE=$(HELM_RELEASE)"
	@echo "  HELM_NAMESPACE=$(HELM_NAMESPACE)"
