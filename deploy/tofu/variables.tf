variable "do_token" {
  description = "DigitalOcean API token (write scope for apps + databases)."
  type        = string
  sensitive   = true
}

variable "region" {
  description = "Region for the app and database."
  type        = string
  default     = "nyc"
}

variable "environment" {
  description = "Deployment environment name (prod, staging)."
  type        = string
  default     = "prod"
}

variable "repo_url" {
  description = "GitHub repo URL of agentguard, e.g. https://github.com/ORG/agentguard."
  type        = string
}

variable "repo_branch" {
  description = "Branch App Platform deploys from."
  type        = string
  default     = "main"
}

variable "dashboard_password" {
  description = "Operator gate password for the dashboard login."
  type        = string
  sensitive   = true
}

variable "dashboard_api_url" {
  description = "Public API base URL baked into the dashboard (NEXT_PUBLIC_API_URL). Set after the first apply from the api_url output, then re-apply."
  type        = string
  default     = ""
}

variable "db_size" {
  description = "Managed Postgres droplet size."
  type        = string
  default     = "db-s-1vcpu-2gb"
}

variable "api_instance_size" {
  description = "App Platform instance size for the API."
  type        = string
  default     = "apps-s-1vcpu-2gb"
}
