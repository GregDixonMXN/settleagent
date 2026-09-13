# Production stack: App Platform (api + dashboard) + Managed PostgreSQL.
# Container-first, one region, solo-founder operable. No Kafka, no Redis,
# no separate worker tier until event volume demands it (see ADR-005).

resource "random_password" "db" {
  length  = 32
  special = false
}

resource "digitalocean_database_cluster" "pg" {
  name       = "agentguard-${var.environment}"
  engine     = "pg"
  version    = "16"
  size       = var.db_size
  region     = var.region
  node_count = 1
}

resource "digitalocean_database_db" "app" {
  cluster_id = digitalocean_database_cluster.pg.id
  name       = "agentguard"
}

resource "digitalocean_database_user" "api" {
  cluster_id = digitalocean_database_cluster.pg.id
  name       = "agentguard_api"
}

locals {
  database_url = "postgres://${digitalocean_database_user.api.name}:${digitalocean_database_user.api.password}@${digitalocean_database_cluster.pg.host}:${digitalocean_database_cluster.pg.port}/${digitalocean_database_db.app.name}?sslmode=require"
}

resource "digitalocean_app" "agentguard" {
  spec {
    name   = "agentguard-${var.environment}"
    region = var.region

    service {
      name               = "api"
      environment_slug   = "go"
      instance_size_slug = var.api_instance_size

      github {
        repo           = var.repo_url
        branch         = var.repo_branch
        deploy_on_push = true
      }
      source_dir      = "."
      dockerfile_path = "deploy/docker/Dockerfile.api"

      http_port = 8080
      health_check {
        http_path = "/health"
      }

      env {
        key   = "DATABASE_URL"
        value = local.database_url
        type  = "SECRET"
      }

      alert {
        value    = 80
        operator = "GREATER_THAN"
        window   = "FIVE_MINUTES"
        rule     = "CPU_UTILIZATION"
      }
    }

    service {
      name               = "dashboard"
      environment_slug   = "node-js"
      instance_size_slug = "apps-s-1vcpu-1gb"

      github {
        repo           = var.repo_url
        branch         = var.repo_branch
        deploy_on_push = true
      }
      source_dir      = "apps/dashboard"
      dockerfile_path = "apps/dashboard/Dockerfile"

      http_port = 3000

      env {
        key   = "NEXT_PUBLIC_API_URL"
        value = var.dashboard_api_url
      }
      env {
        key   = "DASHBOARD_PASSWORD"
        value = var.dashboard_password
        type  = "SECRET"
      }
      # NEXT_PUBLIC_API_TOKEN (operator token for the dashboard's API calls)
      # is set after boot: register/mint it via the API, then add it here.
    }
  }
}
