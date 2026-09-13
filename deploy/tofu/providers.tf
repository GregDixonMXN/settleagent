terraform {
  required_version = ">= 1.6.0"
  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.40"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
  # Solo-founder default: local state. Move to Terraform Cloud / S3-compatible
  # remote state (Spaces) before a second operator touches this.
}

provider "digitalocean" {
  token = var.do_token
}
