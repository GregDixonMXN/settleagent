output "api_url" {
  description = "Public base URL of the API service."
  value       = digitalocean_app.settleagent.live_url
}

output "dashboard_url" {
  description = "Public URL of the dashboard."
  value       = "https://${digitalocean_app.settleagent.spec[0].service[1].name}-${digitalocean_app.settleagent.default_ingress}.ondigitalocean.app"
}

output "db_host" {
  description = "Managed Postgres hostname (private)."
  value       = digitalocean_database_cluster.pg.private_host
}

output "db_password" {
  description = "Password for the settleagent_api database user."
  value       = digitalocean_database_user.api.password
  sensitive   = true
}
