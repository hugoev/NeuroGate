# NeuroGate 🧠⚡

A **Distributed, Fault-Tolerant Inference Gateway for LLMs** built with Go, gRPC, Kubernetes, and Terraform.

![Architecture](https://img.shields.io/badge/Architecture-Microservices-blue)
![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)
![Kubernetes](https://img.shields.io/badge/Kubernetes-Kind-326CE5?logo=kubernetes)
![Terraform](https://img.shields.io/badge/IaC-Terraform-7B42BC?logo=terraform)

## 🎯 Overview

NeuroGate is not a chatbot—it's the **platform that serves the chatbot**. It provides:

- **Load Balancing**: Round-robin distribution across multiple LLM workers
- **Fault Tolerance**: Circuit breaker pattern to handle failing workers gracefully
- **Observability**: Prometheus metrics + Grafana dashboards for tokens/second, latency, etc.
- **Cloud-Native**: Kubernetes deployment with Terraform IaC

## 🏗️ Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                              CLIENTS                                  │
│                     curl / SDK / Web Application                      │
└────────────────────────────────┬─────────────────────────────────────┘
                                 │ HTTP (REST)
                                 ▼
┌──────────────────────────────────────────────────────────────────────┐
│                         GATEWAY (Load Balancer)                       │
│  ┌────────────┐  ┌─────────────────┐  ┌──────────────────────────┐   │
│  │ API Auth   │  │ Round Robin LB  │  │ Circuit Breaker (3 fails)│   │
│  └────────────┘  └─────────────────┘  └──────────────────────────┘   │
└────────────────────────────────┬─────────────────────────────────────┘
                                 │ gRPC
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                  ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│    Worker 1     │  │    Worker 2     │  │    Worker 3     │
│  (gRPC Server)  │  │  (gRPC Server)  │  │  (gRPC Server)  │
│  :50051/:9090   │  │  :50052/:9092   │  │  :50053/:9093   │
└────────┬────────┘  └────────┬────────┘  └────────┬────────┘
         │                    │                    │
         └────────────────────┴────────────────────┘
                              │ HTTP
                              ▼
                    ┌─────────────────┐
                    │     Ollama      │
                    │  (LLM Engine)   │
                    │    :11434       │
                    └─────────────────┘
```

## 📁 Project Structure

```
neurogate/
├── api/proto/              # gRPC Protocol Buffer definitions
├── cmd/
│   ├── gateway/            # Load Balancer REST API
│   └── worker/             # gRPC Worker connecting to Ollama
├── deploy/
│   ├── k8s/                # Kubernetes YAML manifests
│   ├── terraform/          # Terraform IaC for K8s resources
│   └── prometheus/         # Prometheus configuration
├── pkg/
│   ├── circuitbreaker/     # Circuit Breaker pattern implementation
│   ├── health/             # Health checking utilities
│   ├── logger/             # Structured logging with slog
│   ├── metrics/            # Prometheus instrumentation
│   └── ollama/             # Ollama API client
├── Dockerfile.gateway      # Multi-stage build for Gateway
├── Dockerfile.worker       # Multi-stage build for Worker
├── docker-compose.yaml     # Local development with Docker
├── Makefile                # Build automation
└── go.mod                  # Go dependencies
```

## 🚀 Quick Start

### Prerequisites

- **Go 1.22+**
- **Docker** and **Docker Compose**
- **Ollama** (running locally with a model like `llama3.2`)
- **Kind** (optional, for Kubernetes)
- **Terraform** (optional, for IaC)

### 1. Start Ollama

```bash
# Install Ollama (if not already)
brew install ollama

# Start Ollama and pull a model
ollama serve &
ollama pull llama3.2
```

### 2. Run Locally (No Docker)

```bash
# Terminal 1: Start a worker
make run-worker

# Terminal 2: Start the gateway
make run-gateway

# Terminal 3: Test it!
make demo
```

### 3. Run with Docker Compose

```bash
# Build and start all services
docker-compose up --build

# Test the gateway
curl -X POST http://localhost:8080/prompt \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer neurogate-secret-key-1" \
  -d '{"query": "Explain quantum computing in simple terms"}'
```

### 4. Deploy to Kubernetes (Kind)

```bash
# Build Docker images
make docker

# Create Kind cluster and deploy
make kind-create
make deploy

# Check status
make status

# Test it
make demo-k8s
```

## 📡 API Reference

### POST /prompt

Generate text from the LLM.

**Request:**
```bash
curl -X POST http://localhost:8080/prompt \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer neurogate-secret-key-1" \
  -d '{
    "query": "Why is the sky blue?",
    "model": "llama3.2",
    "max_tokens": 500,
    "temperature": 0.7
  }'
```

**Response:**
```json
{
  "request_id": "req-1704567890123456789",
  "response": "The sky appears blue due to a phenomenon called Rayleigh scattering...",
  "model": "llama3.2",
  "tokens": 156,
  "latency_ms": 2340,
  "worker_id": "worker-0"
}
```

### GET /health

Check gateway health status.

### GET /workers

List all workers and their status including circuit breaker state.

## 📊 Observability

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `neurogate_gateway_requests_total` | Counter | Total HTTP requests |
| `neurogate_gateway_request_duration_seconds` | Histogram | Request latency |
| `neurogate_gateway_circuit_breaker_state` | Gauge | CB state per worker |
| `neurogate_worker_inference_duration_seconds` | Histogram | LLM inference time |
| `neurogate_worker_tokens_generated_total` | Counter | Tokens generated |
| `neurogate_worker_tokens_per_second` | Gauge | Current TPS |

### Grafana Dashboards

Access Grafana at `http://localhost:3000` (admin/neurogate) with pre-configured dashboards showing:

- Requests per second
- P95 latency
- Tokens per second
- Circuit breaker states

## 🔧 Configuration

### Environment Variables

**Gateway:**
| Variable | Default | Description |
|----------|---------|-------------|
| `HTTP_PORT` | 8080 | HTTP listen port |
| `METRICS_PORT` | 9091 | Prometheus metrics port |
| `WORKER_ADDRESSES` | localhost:50051 | Comma-separated worker addresses |
| `API_KEYS` | (none) | Comma-separated valid API keys |
| `LOG_LEVEL` | info | Log level (debug, info, warn, error) |

**Worker:**
| Variable | Default | Description |
|----------|---------|-------------|
| `GRPC_PORT` | 50051 | gRPC listen port |
| `METRICS_PORT` | 9090 | Prometheus metrics port |
| `OLLAMA_URL` | http://localhost:11434 | Ollama API URL |
| `LOG_LEVEL` | info | Log level |

## 🛡️ Fault Tolerance

### Circuit Breaker

The gateway implements a circuit breaker for each worker:

- **Closed** (normal): Requests flow through
- **Open** (tripped): After 3 consecutive failures, traffic stops for 30 seconds
- **Half-Open** (testing): One request allowed through to test recovery

```
   [CLOSED] ──3 failures──> [OPEN] ──30s timeout──> [HALF-OPEN]
       ▲                                                  │
       └────────────success─────────────────┘             │
                                                          │
       └────────────failure───────────────────────────────┘
```

## 🧪 Make Commands

```bash
make help           # Show all available commands

# Development
make build          # Build all binaries
make test           # Run tests
make lint           # Run linters
make clean          # Clean build artifacts

# Docker
make docker         # Build all Docker images
make docker-push    # Push images to registry

# Kubernetes
make kind-create    # Create Kind cluster
make deploy         # Deploy to K8s
make undeploy       # Remove from K8s
make status         # Show deployment status

# Terraform
make tf-init        # Initialize Terraform
make tf-apply       # Apply Terraform changes

# Demo
make demo           # Test local gateway
make demo-k8s       # Test Kind gateway
```

## 🏆 Tech Stack Highlights

| Component | Technology | Enterprise Value |
|-----------|------------|------------------|
| **Language** | Go 1.22+ | Cloud-native standard |
| **Communication** | gRPC + Protobuf | High-performance internal comms |
| **Orchestration** | Kubernetes (Kind) | Industry-standard container orchestration |
| **IaC** | Terraform | Infrastructure as code |
| **AI Engine** | Ollama | Local LLM inference |
| **Observability** | Prometheus + Grafana | Industry-standard monitoring |
| **Patterns** | Circuit Breaker | Resilience engineering |

## 📜 License

MIT License - see [LICENSE](LICENSE) for details.

---

Built with ❤️ for learning distributed systems and cloud-native development.
