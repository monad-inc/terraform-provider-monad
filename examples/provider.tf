terraform {
  required_providers {
    monad = {
      source = "monad-inc/monad"
    }
  }
}

variable "api_token" {
  description = "Monad API token"
  type        = string
  sensitive   = true
}

variable "organization_id" {
  description = "Organization ID for all resources"
  type        = string
}

provider "monad" {
  base_url        = "https://localhost"
  api_token       = var.api_token
  organization_id = var.organization_id
}