# NeuroGate Main Infrastructure
# Core Kubernetes resources for the inference gateway

# ============================================================================
# NAMESPACE
# ============================================================================

resource "kubernetes_namespace" "neurogate" {
  metadata {
    name = var.namespace

    labels = merge({
      app         = "neurogate"
      environment = var.environment
      managed-by  = "terraform"
    }, var.tags)
  }
}

# ============================================================================
# CONFIGURATION
# ============================================================================

# ConfigMap for shared configuration
resource "kubernetes_config_map" "neurogate_config" {
  metadata {
    name      = "neurogate-config"
    namespace = kubernetes_namespace.neurogate.metadata[0].name

    labels = {
      app        = "neurogate"
      managed-by = "terraform"
    }
  }

  data = {
    LOG_LEVEL  = var.log_level
    LOG_FORMAT = "json"
    OLLAMA_URL = var.ollama_host
  }
}

# Secret for API keys
resource "kubernetes_secret" "neurogate_secrets" {
  metadata {
    name      = "neurogate-secrets"
    namespace = kubernetes_namespace.neurogate.metadata[0].name

    labels = {
      app        = "neurogate"
      managed-by = "terraform"
    }
  }

  data = {
    API_KEYS = var.api_keys
  }

  type = "Opaque"
}

# ============================================================================
# WORKER DEPLOYMENT
# ============================================================================

resource "kubernetes_deployment" "worker" {
  metadata {
    name      = "neurogate-worker"
    namespace = kubernetes_namespace.neurogate.metadata[0].name

    labels = merge({
      app         = "neurogate"
      component   = "worker"
      environment = var.environment
    }, var.tags)
  }

  spec {
    replicas = var.worker_replicas

    selector {
      match_labels = {
        app       = "neurogate"
        component = "worker"
      }
    }

    template {
      metadata {
        labels = merge({
          app         = "neurogate"
          component   = "worker"
          environment = var.environment
        }, var.tags)

        annotations = {
          "prometheus.io/scrape" = "true"
          "prometheus.io/port"   = "9090"
          "prometheus.io/path"   = "/metrics"
        }
      }

      spec {
        container {
          name  = "worker"
          image = var.worker_image

          port {
            container_port = 50051
            name           = "grpc"
          }

          port {
            container_port = 9090
            name           = "metrics"
          }

          env_from {
            config_map_ref {
              name = kubernetes_config_map.neurogate_config.metadata[0].name
            }
          }

          resources {
            requests = {
              cpu    = var.worker_cpu_request
              memory = var.worker_memory_request
            }
            limits = {
              cpu    = var.worker_cpu_limit
              memory = var.worker_memory_limit
            }
          }

          liveness_probe {
            http_get {
              path = "/health"
              port = 9090
            }
            initial_delay_seconds = 10
            period_seconds        = 30
            timeout_seconds       = 5
            failure_threshold     = 3
          }

          readiness_probe {
            http_get {
              path = "/ready"
              port = 9090
            }
            initial_delay_seconds = 5
            period_seconds        = 10
            timeout_seconds       = 3
            failure_threshold     = 3
          }
        }
      }
    }
  }

  lifecycle {
    create_before_destroy = true
  }
}

# Worker Service (ClusterIP for internal gRPC access)
resource "kubernetes_service" "worker" {
  metadata {
    name      = "neurogate-worker"
    namespace = kubernetes_namespace.neurogate.metadata[0].name

    labels = {
      app       = "neurogate"
      component = "worker"
    }
  }

  spec {
    selector = {
      app       = "neurogate"
      component = "worker"
    }

    port {
      name        = "grpc"
      port        = 50051
      target_port = 50051
    }

    port {
      name        = "metrics"
      port        = 9090
      target_port = 9090
    }

    type = "ClusterIP"
  }
}

# ============================================================================
# GATEWAY DEPLOYMENT
# ============================================================================

resource "kubernetes_deployment" "gateway" {
  metadata {
    name      = "neurogate-gateway"
    namespace = kubernetes_namespace.neurogate.metadata[0].name

    labels = merge({
      app         = "neurogate"
      component   = "gateway"
      environment = var.environment
    }, var.tags)
  }

  spec {
    replicas = var.gateway_replicas

    selector {
      match_labels = {
        app       = "neurogate"
        component = "gateway"
      }
    }

    template {
      metadata {
        labels = merge({
          app         = "neurogate"
          component   = "gateway"
          environment = var.environment
        }, var.tags)

        annotations = {
          "prometheus.io/scrape" = "true"
          "prometheus.io/port"   = "9091"
          "prometheus.io/path"   = "/metrics"
        }
      }

      spec {
        container {
          name  = "gateway"
          image = var.gateway_image

          port {
            container_port = 8080
            name           = "http"
          }

          port {
            container_port = 9091
            name           = "metrics"
          }

          env {
            name  = "WORKER_ADDRESSES"
            value = "${kubernetes_service.worker.metadata[0].name}:50051"
          }

          env_from {
            config_map_ref {
              name = kubernetes_config_map.neurogate_config.metadata[0].name
            }
          }

          env_from {
            secret_ref {
              name = kubernetes_secret.neurogate_secrets.metadata[0].name
            }
          }

          resources {
            requests = {
              cpu    = var.gateway_cpu_request
              memory = var.gateway_memory_request
            }
            limits = {
              cpu    = var.gateway_cpu_limit
              memory = var.gateway_memory_limit
            }
          }

          liveness_probe {
            http_get {
              path = "/health"
              port = 8080
            }
            initial_delay_seconds = 10
            period_seconds        = 30
            timeout_seconds       = 5
            failure_threshold     = 3
          }

          readiness_probe {
            http_get {
              path = "/health"
              port = 8080
            }
            initial_delay_seconds = 5
            period_seconds        = 10
            timeout_seconds       = 3
            failure_threshold     = 3
          }
        }
      }
    }
  }

  lifecycle {
    create_before_destroy = true
  }

  depends_on = [kubernetes_deployment.worker]
}

# Gateway Service (NodePort for external access)
resource "kubernetes_service" "gateway" {
  metadata {
    name      = "neurogate-gateway"
    namespace = kubernetes_namespace.neurogate.metadata[0].name

    labels = {
      app       = "neurogate"
      component = "gateway"
    }
  }

  spec {
    selector = {
      app       = "neurogate"
      component = "gateway"
    }

    port {
      name        = "http"
      port        = 80
      target_port = 8080
      node_port   = var.gateway_node_port
    }

    port {
      name        = "metrics"
      port        = 9091
      target_port = 9091
    }

    type = "NodePort"
  }
}

# ============================================================================
# OPTIONAL: PROMETHEUS (for monitoring)
# ============================================================================

resource "kubernetes_service_account" "prometheus" {
  count = var.enable_prometheus ? 1 : 0

  metadata {
    name      = "prometheus"
    namespace = kubernetes_namespace.neurogate.metadata[0].name
  }
}

resource "kubernetes_cluster_role" "prometheus" {
  count = var.enable_prometheus ? 1 : 0

  metadata {
    name = "neurogate-prometheus"
  }

  rule {
    api_groups = [""]
    resources  = ["nodes", "services", "endpoints", "pods"]
    verbs      = ["get", "list", "watch"]
  }

  rule {
    api_groups = [""]
    resources  = ["configmaps"]
    verbs      = ["get"]
  }
}

resource "kubernetes_cluster_role_binding" "prometheus" {
  count = var.enable_prometheus ? 1 : 0

  metadata {
    name = "neurogate-prometheus"
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = kubernetes_cluster_role.prometheus[0].metadata[0].name
  }

  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.prometheus[0].metadata[0].name
    namespace = kubernetes_namespace.neurogate.metadata[0].name
  }
}

resource "kubernetes_config_map" "prometheus_config" {
  count = var.enable_prometheus ? 1 : 0

  metadata {
    name      = "prometheus-config"
    namespace = kubernetes_namespace.neurogate.metadata[0].name
  }

  data = {
    "prometheus.yml" = <<-EOT
      global:
        scrape_interval: 15s
        evaluation_interval: 15s

      scrape_configs:
        - job_name: 'prometheus'
          static_configs:
            - targets: ['localhost:9090']

        - job_name: 'neurogate-gateway'
          kubernetes_sd_configs:
            - role: pod
              namespaces:
                names:
                  - ${var.namespace}
          relabel_configs:
            - source_labels: [__meta_kubernetes_pod_label_component]
              regex: gateway
              action: keep
            - source_labels: [__meta_kubernetes_pod_ip]
              target_label: __address__
              replacement: $1:9091

        - job_name: 'neurogate-worker'
          kubernetes_sd_configs:
            - role: pod
              namespaces:
                names:
                  - ${var.namespace}
          relabel_configs:
            - source_labels: [__meta_kubernetes_pod_label_component]
              regex: worker
              action: keep
            - source_labels: [__meta_kubernetes_pod_ip]
              target_label: __address__
              replacement: $1:9090
    EOT
  }
}

resource "kubernetes_deployment" "prometheus" {
  count = var.enable_prometheus ? 1 : 0

  metadata {
    name      = "prometheus"
    namespace = kubernetes_namespace.neurogate.metadata[0].name

    labels = {
      app = "prometheus"
    }
  }

  spec {
    replicas = 1

    selector {
      match_labels = {
        app = "prometheus"
      }
    }

    template {
      metadata {
        labels = {
          app = "prometheus"
        }
      }

      spec {
        service_account_name = kubernetes_service_account.prometheus[0].metadata[0].name

        container {
          name  = "prometheus"
          image = "prom/prometheus:v2.48.0"

          args = [
            "--config.file=/etc/prometheus/prometheus.yml",
            "--storage.tsdb.path=/prometheus",
            "--web.enable-lifecycle"
          ]

          port {
            container_port = 9090
            name           = "http"
          }

          volume_mount {
            name       = "config"
            mount_path = "/etc/prometheus"
          }

          volume_mount {
            name       = "data"
            mount_path = "/prometheus"
          }

          resources {
            requests = {
              cpu    = "100m"
              memory = "256Mi"
            }
            limits = {
              cpu    = "500m"
              memory = "512Mi"
            }
          }
        }

        volume {
          name = "config"
          config_map {
            name = kubernetes_config_map.prometheus_config[0].metadata[0].name
          }
        }

        volume {
          name = "data"
          empty_dir {}
        }
      }
    }
  }
}

resource "kubernetes_service" "prometheus" {
  count = var.enable_prometheus ? 1 : 0

  metadata {
    name      = "prometheus"
    namespace = kubernetes_namespace.neurogate.metadata[0].name
  }

  spec {
    selector = {
      app = "prometheus"
    }

    port {
      port        = 9090
      target_port = 9090
      node_port   = 30090
    }

    type = "NodePort"
  }
}

# ============================================================================
# OPTIONAL: GRAFANA (for dashboards)
# ============================================================================

resource "kubernetes_config_map" "grafana_datasources" {
  count = var.enable_grafana ? 1 : 0

  metadata {
    name      = "grafana-datasources"
    namespace = kubernetes_namespace.neurogate.metadata[0].name
  }

  data = {
    "datasources.yaml" = <<-EOT
      apiVersion: 1
      datasources:
        - name: Prometheus
          type: prometheus
          access: proxy
          url: http://prometheus:9090
          isDefault: true
          editable: false
    EOT
  }
}

resource "kubernetes_deployment" "grafana" {
  count = var.enable_grafana ? 1 : 0

  metadata {
    name      = "grafana"
    namespace = kubernetes_namespace.neurogate.metadata[0].name

    labels = {
      app = "grafana"
    }
  }

  spec {
    replicas = 1

    selector {
      match_labels = {
        app = "grafana"
      }
    }

    template {
      metadata {
        labels = {
          app = "grafana"
        }
      }

      spec {
        container {
          name  = "grafana"
          image = "grafana/grafana:10.2.0"

          port {
            container_port = 3000
            name           = "http"
          }

          env {
            name  = "GF_SECURITY_ADMIN_PASSWORD"
            value = var.grafana_admin_password
          }

          env {
            name  = "GF_USERS_ALLOW_SIGN_UP"
            value = "false"
          }

          volume_mount {
            name       = "datasources"
            mount_path = "/etc/grafana/provisioning/datasources"
          }

          resources {
            requests = {
              cpu    = "100m"
              memory = "128Mi"
            }
            limits = {
              cpu    = "500m"
              memory = "256Mi"
            }
          }
        }

        volume {
          name = "datasources"
          config_map {
            name = kubernetes_config_map.grafana_datasources[0].metadata[0].name
          }
        }
      }
    }
  }

  depends_on = [kubernetes_deployment.prometheus]
}

resource "kubernetes_service" "grafana" {
  count = var.enable_grafana ? 1 : 0

  metadata {
    name      = "grafana"
    namespace = kubernetes_namespace.neurogate.metadata[0].name
  }

  spec {
    selector = {
      app = "grafana"
    }

    port {
      port        = 3000
      target_port = 3000
      node_port   = 30030
    }

    type = "NodePort"
  }
}
