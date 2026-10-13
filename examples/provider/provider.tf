terraform {
  # Write-only arguments (config.secrets, monad_secret.value) need Terraform 1.11+.
  required_version = ">= 1.11"

  required_providers {
    monad = {
      source = "monad-inc/monad"
      # Pre-1.0: breaking changes ship as minor bumps, so pin the minor.
      version = "~> 0.5.0"
    }
  }
}

provider "monad" {
  base_url        = "https://app.monad.com" # the provider appends /api
  api_token       = var.monad_api_token     # or MONAD_API_TOKEN
  organization_id = var.monad_org_id        # or MONAD_ORGANIZATION_ID
}

variable "monad_api_token" {
  type      = string
  sensitive = true
}

variable "monad_org_id" {
  type = string
}
