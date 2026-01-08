# NeuroGate Terraform Variables
# Separate file for variable definitions (best practice)

variable "kube_context" {
  description = "Kubernetes context to use (e.g., 'kind-neurogate', 'docker-desktop')"
  type        = string
  default     = "kind-neurogate"
}

variable "namespace" {
  description = "Kubernetes namespace for NeuroGate resources"
  type        = string
  default     = "neurogate"

  validation {
    condition     = can(regex("^[a-z0-9-]+$", var.namespace))
    error_message = "Namespace must contain only lowercase letters, numbers, and hyphens."
  }
}

variable "gateway_replicas" {
  description = "Number of Gateway replicas to deploy"
  type        = number
  default     = 1

  validation {
    condition     = var.gateway_replicas >= 1 && var.gateway_replicas <= 10
    error_message = "Gateway replicas must be between 1 and 10."
  }
}

variable "worker_replicas" {
  description = "Number of Worker replicas to deploy"
  type        = number
  default     = 3

  validation {
    condition     = var.worker_replicas >= 1 && var.worker_replicas <= 20
    error_message = "Worker replicas must be between 1 and 20."
  }
}

variable "ollama_host" {
  description = "Ollama API host URL"
  type        = string
  default     = "http://host.docker.internal:11434"
}

variable "api_keys" {
  description = "Comma-separated list of valid API keys for authentication"
  type        = string
  default     = "neurogate-secret-key-1,neurogate-secret-key-2"
  sensitive   = true
}

variable "gateway_image" {
  description = "Docker image for the Gateway service"
  type        = string
  default     = "neurogate/gateway:latest"
}

variable "worker_image" {
  description = "Docker image for the Worker service"
  type        = string
  default     = "neurogate/worker:latest"
}

variable "log_level" {
  description = "Log level for services (debug, info, warn, error)"
  type        = string
  default     = "info"

  validation {
    condition     = contains(["debug", "info", "warn", "error"], var.log_level)
    error_message = "Log level must be one of: debug, info, warn, error."
  }
}

variable "gateway_node_port" {
  description = "NodePort for external Gateway access (30000-32767)"
  type        = number
  default     = 30080

  validation {
    condition     = var.gateway_node_port >= 30000 && var.gateway_node_port <= 32767
    error_message = "NodePort must be between 30000 and 32767."
  }
}

variable "enable_prometheus" {
  description = "Whether to deploy Prometheus for monitoring"
  type        = bool
  default     = true
}

variable "enable_grafana" {
  description = "Whether to deploy Grafana for dashboards"
  type        = bool
  default     = true
}

variable "grafana_admin_password" {
  description = "Grafana admin password"
  type        = string
  default     = "neurogate"
  sensitive   = true
}

# Resource limits
variable "worker_cpu_request" {
  description = "CPU request for worker pods"
  type        = string
  default     = "100m"
}

variable "worker_cpu_limit" {
  description = "CPU limit for worker pods"
  type        = string
  default     = "500m"
}

variable "worker_memory_request" {
  description = "Memory request for worker pods"
  type        = string
  default     = "128Mi"
}

variable "worker_memory_limit" {
  description = "Memory limit for worker pods"
  type        = string
  default     = "512Mi"
}

variable "gateway_cpu_request" {
  description = "CPU request for gateway pods"
  type        = string
  default     = "100m"
}

variable "gateway_cpu_limit" {
  description = "CPU limit for gateway pods"
  type        = string
  default     = "500m"
}

variable "gateway_memory_request" {
  description = "Memory request for gateway pods"
  type        = string
  default     = "64Mi"
}

variable "gateway_memory_limit" {
  description = "Memory limit for gateway pods"
  type        = string
  default     = "256Mi"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
  default     = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "Environment must be one of: dev, staging, prod."
  }
}

variable "tags" {
  description = "Additional labels to apply to all resources"
  type        = map(string)
  default     = {}
}
