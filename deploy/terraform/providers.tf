# NeuroGate Terraform Provider Configuration
# Separating provider configuration for clarity

terraform {
  required_version = ">= 1.0"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.25"
    }
  }

  # Uncomment to use remote backend (S3, GCS, etc.)
  # backend "s3" {
  #   bucket         = "neurogate-terraform-state"
  #   key            = "neurogate/terraform.tfstate"
  #   region         = "us-east-1"
  #   encrypt        = true
  #   dynamodb_table = "neurogate-terraform-locks"
  # }

  # Uncomment for GCS backend
  # backend "gcs" {
  #   bucket = "neurogate-terraform-state"
  #   prefix = "neurogate"
  # }
}

# Kubernetes Provider Configuration
provider "kubernetes" {
  config_path    = "~/.kube/config"
  config_context = var.kube_context
}

# Optional: Helm provider for installing Prometheus/Grafana from charts
# provider "helm" {
#   kubernetes {
#     config_path    = "~/.kube/config"
#     config_context = var.kube_context
#   }
# }
