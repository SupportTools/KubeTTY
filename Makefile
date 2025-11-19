SHELL := /bin/bash
IMAGE ?= harbor.support.tools/kubetty/kubetty
TAG ?= dev
REGISTRY_IMAGE := $(IMAGE):$(TAG)
GO_VERSION ?= 1.23.2
NODE_MAJOR ?= 20

.PHONY: ui server test fmt docker-build docker-push helm-install clean

ui:
	@echo "==> Installing web dependencies"
	npm --prefix web install
	@echo "==> Building web bundle"
	npm --prefix web run build

server: ui
	@echo "==> Running Go fmt"
	cd server && go fmt ./...
	@echo "==> Running Go tests"
	cd server && go test ./...
	@echo "==> Building kubetty binary"
	mkdir -p bin
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/kubetty .

docker-build:
	@echo "==> Building Docker image $(REGISTRY_IMAGE)"
	docker build --build-arg GO_VERSION=$(GO_VERSION) --build-arg NODE_MAJOR=$(NODE_MAJOR) -t $(REGISTRY_IMAGE) .

docker-push:
	@echo "==> Pushing Docker image $(REGISTRY_IMAGE)"
	docker push $(REGISTRY_IMAGE)

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
	helm upgrade --install $(RELEASE) deploy/helm -n $(NAMESPACE) -f $(VALUES)

clean:
	rm -rf server/ui/dist web/node_modules web/dist bin
