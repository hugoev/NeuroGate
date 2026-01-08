# NeuroGate Terraform Outputs
# All outputs from the infrastructure deployment

output "namespace" {
  value       = kubernetes_namespace.neurogate.metadata[0].name
  description = "The Kubernetes namespace where NeuroGate is deployed"
}

output "gateway_service_name" {
  value       = kubernetes_service.gateway.metadata[0].name
  description = "Gateway service name for internal Kubernetes access"
}

output "gateway_url" {
  value       = "http://localhost:${var.gateway_node_port}"
  description = "URL to access the Gateway externally (when using Kind/NodePort)"
}

output "gateway_cluster_ip" {
  value       = kubernetes_service.gateway.spec[0].cluster_ip
  description = "Gateway ClusterIP for internal access"
}

output "worker_service_name" {
  value       = kubernetes_service.worker.metadata[0].name
  description = "Worker service name for internal gRPC access"
}

output "worker_service_dns" {
  value       = "${kubernetes_service.worker.metadata[0].name}.${var.namespace}.svc.cluster.local"
  description = "Worker service DNS name for in-cluster access"
}

output "worker_grpc_endpoint" {
  value       = "${kubernetes_service.worker.metadata[0].name}:50051"
  description = "Worker gRPC endpoint"
}

output "prometheus_url" {
  value       = var.enable_prometheus ? "http://localhost:30090" : null
  description = "Prometheus URL (when enabled and using Kind)"
}

output "grafana_url" {
  value       = var.enable_grafana ? "http://localhost:30030" : null
  description = "Grafana URL (when enabled and using Kind)"
}

output "grafana_credentials" {
  value       = var.enable_grafana ? "admin / ${var.grafana_admin_password}" : null
  description = "Grafana admin credentials"
  sensitive   = true
}

output "deployment_summary" {
  value = {
    namespace        = kubernetes_namespace.neurogate.metadata[0].name
    gateway_replicas = var.gateway_replicas
    worker_replicas  = var.worker_replicas
    environment      = var.environment
    gateway_image    = var.gateway_image
    worker_image     = var.worker_image
  }
  description = "Summary of the deployment configuration"
}

output "kubectl_commands" {
  value       = <<-EOT
    # View pods
    kubectl -n ${var.namespace} get pods
    
    # View services
    kubectl -n ${var.namespace} get services
    
    # View gateway logs
    kubectl -n ${var.namespace} logs -f -l component=gateway
    
    # View worker logs
    kubectl -n ${var.namespace} logs -f -l component=worker
    
    # Port forward gateway (alternative to NodePort)
    kubectl -n ${var.namespace} port-forward svc/neurogate-gateway 8080:80
    
    # Test the API
    curl -X POST http://localhost:${var.gateway_node_port}/prompt \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer neurogate-secret-key-1" \
      -d '{"query": "Hello, NeuroGate!"}'
  EOT
  description = "Useful kubectl commands for managing the deployment"
}
