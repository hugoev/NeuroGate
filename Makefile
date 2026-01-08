# NeuroGate Makefile
# Provides automation for build, test, and deployment

.PHONY: all build test clean proto docker docker-push kind-create kind-delete deploy undeploy run-gateway run-worker lint

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOLINT=golangci-lint

# Binary names
GATEWAY_BINARY=gateway
WORKER_BINARY=worker

# Docker parameters
DOCKER_REGISTRY?=neurogate
GATEWAY_IMAGE=$(DOCKER_REGISTRY)/gateway
WORKER_IMAGE=$(DOCKER_REGISTRY)/worker
VERSION?=latest

# Kubernetes parameters
KIND_CLUSTER=neurogate
NAMESPACE=neurogate

# Proto parameters
PROTO_DIR=api/proto
PROTO_FILES=$(shell find $(PROTO_DIR) -name '*.proto')

# Default target
all: build

# =====================
# Development Commands
# =====================

## build: Build all binaries
build: build-gateway build-worker

## build-gateway: Build the gateway binary
build-gateway:
	@echo "Building gateway..."
	$(GOBUILD) -o bin/$(GATEWAY_BINARY) ./cmd/gateway

## build-worker: Build the worker binary
build-worker:
	@echo "Building worker..."
	$(GOBUILD) -o bin/$(WORKER_BINARY) ./cmd/worker

## run-gateway: Run the gateway locally (single worker mode)
run-gateway: build-gateway
	@echo "Starting gateway (connecting to single worker at localhost:50051)..."
	WORKER_ADDRESSES=localhost:50051 \
	LOG_LEVEL=debug \
	./bin/$(GATEWAY_BINARY)

## run-gateway-multi: Run the gateway with 3 workers (requires 3 worker instances)
run-gateway-multi: build-gateway
	@echo "Starting gateway (connecting to 3 workers)..."
	WORKER_ADDRESSES=localhost:50051,localhost:50052,localhost:50053 \
	LOG_LEVEL=debug \
	./bin/$(GATEWAY_BINARY)

## run-worker: Run a single worker locally
run-worker: build-worker
	@echo "Starting worker..."
	GRPC_PORT=50051 \
	METRICS_PORT=9090 \
	OLLAMA_URL=http://localhost:11434 \
	LOG_LEVEL=debug \
	./bin/$(WORKER_BINARY)

## test: Run tests
test:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...

## lint: Run linters
lint:
	$(GOLINT) run ./...

## tidy: Tidy go modules
tidy:
	$(GOMOD) tidy

## clean: Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out

# =====================
# Proto Generation
# =====================

## proto: Generate Go code from protobuf definitions
proto:
	@echo "Generating Go code from proto files..."
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)

## proto-install: Install protoc plugins
proto-install:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# =====================
# Docker Commands
# =====================

## docker: Build all Docker images
docker: docker-gateway docker-worker

## docker-gateway: Build gateway Docker image
docker-gateway:
	@echo "Building gateway Docker image..."
	docker build -t $(GATEWAY_IMAGE):$(VERSION) -f Dockerfile.gateway .

## docker-worker: Build worker Docker image
docker-worker:
	@echo "Building worker Docker image..."
	docker build -t $(WORKER_IMAGE):$(VERSION) -f Dockerfile.worker .

## docker-push: Push Docker images to registry
docker-push:
	docker push $(GATEWAY_IMAGE):$(VERSION)
	docker push $(WORKER_IMAGE):$(VERSION)

## docker-run-worker: Run worker in Docker (connects to host Ollama)
docker-run-worker:
	docker run -d --name neurogate-worker \
		-p 50051:50051 -p 9090:9090 \
		-e OLLAMA_URL=http://host.docker.internal:11434 \
		-e LOG_LEVEL=debug \
		$(WORKER_IMAGE):$(VERSION)

## docker-run-gateway: Run gateway in Docker
docker-run-gateway:
	docker run -d --name neurogate-gateway \
		-p 8080:8080 -p 9091:9091 \
		-e WORKER_ADDRESSES=host.docker.internal:50051 \
		-e LOG_LEVEL=debug \
		$(GATEWAY_IMAGE):$(VERSION)

# =====================
# Kind (Kubernetes in Docker)
# =====================

## kind-create: Create a Kind cluster
kind-create:
	@echo "Creating Kind cluster..."
	kind create cluster --name $(KIND_CLUSTER) --config deploy/kind-config.yaml
	@echo "Loading Docker images into Kind..."
	kind load docker-image $(GATEWAY_IMAGE):$(VERSION) --name $(KIND_CLUSTER)
	kind load docker-image $(WORKER_IMAGE):$(VERSION) --name $(KIND_CLUSTER)

## kind-delete: Delete the Kind cluster
kind-delete:
	kind delete cluster --name $(KIND_CLUSTER)

## kind-load: Load Docker images into Kind cluster
kind-load:
	kind load docker-image $(GATEWAY_IMAGE):$(VERSION) --name $(KIND_CLUSTER)
	kind load docker-image $(WORKER_IMAGE):$(VERSION) --name $(KIND_CLUSTER)

# =====================
# Kubernetes Deployment
# =====================

## deploy: Deploy to Kubernetes using kubectl
deploy:
	@echo "Deploying to Kubernetes..."
	kubectl apply -f deploy/k8s/00-namespace.yaml
	kubectl apply -f deploy/k8s/01-worker.yaml
	kubectl apply -f deploy/k8s/02-gateway.yaml
	kubectl apply -f deploy/k8s/03-prometheus.yaml
	kubectl apply -f deploy/k8s/04-grafana.yaml
	@echo "Waiting for deployments..."
	kubectl -n $(NAMESPACE) rollout status deployment/neurogate-worker
	kubectl -n $(NAMESPACE) rollout status deployment/neurogate-gateway

## undeploy: Remove from Kubernetes
undeploy:
	@echo "Removing from Kubernetes..."
	kubectl delete -f deploy/k8s/ --ignore-not-found

## logs-gateway: View gateway logs
logs-gateway:
	kubectl -n $(NAMESPACE) logs -f -l component=gateway

## logs-worker: View worker logs
logs-worker:
	kubectl -n $(NAMESPACE) logs -f -l component=worker

## status: Show deployment status
status:
	@echo "=== Pods ==="
	kubectl -n $(NAMESPACE) get pods
	@echo "\n=== Services ==="
	kubectl -n $(NAMESPACE) get services
	@echo "\n=== Deployments ==="
	kubectl -n $(NAMESPACE) get deployments

# =====================
# Terraform
# =====================

## tf-init: Initialize Terraform
tf-init:
	cd deploy/terraform && terraform init

## tf-plan: Plan Terraform changes
tf-plan:
	cd deploy/terraform && terraform plan

## tf-apply: Apply Terraform changes
tf-apply:
	cd deploy/terraform && terraform apply -auto-approve

## tf-destroy: Destroy Terraform resources
tf-destroy:
	cd deploy/terraform && terraform destroy -auto-approve

# =====================
# Testing & Demo
# =====================

## demo: Run a demo request against the gateway
demo:
	@echo "Sending test request to gateway..."
	curl -X POST http://localhost:8080/prompt \
		-H "Content-Type: application/json" \
		-H "Authorization: Bearer neurogate-secret-key-1" \
		-d '{"query": "Why is the sky blue?", "model": "llama3.2"}'

## demo-k8s: Run a demo request against the Kind cluster
demo-k8s:
	@echo "Sending test request to gateway (Kind)..."
	curl -X POST http://localhost:30080/prompt \
		-H "Content-Type: application/json" \
		-H "Authorization: Bearer neurogate-secret-key-1" \
		-d '{"query": "Why is the sky blue?", "model": "llama3.2"}'

## health: Check gateway health
health:
	curl http://localhost:8080/health

## workers: List workers and their status
workers:
	curl http://localhost:8080/workers

# =====================
# Help
# =====================

## help: Show this help message
help:
	@echo "NeuroGate - Distributed LLM Inference Gateway"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
