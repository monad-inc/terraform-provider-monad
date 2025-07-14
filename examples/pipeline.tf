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

resource "monad_secret" "secret" {
  name        = "Terraform Example Secret"
  description = "Terraform Example Secret for Monad"
  value       = "your-secret-value"
}

resource "monad_input" "input" {
  name        = "Terraform Example Generic Input"
  description = "Terraform Example Generic input for Monad"
  component_type = "demo"

  config {
    settings = {
      record_type = "jira_users"
      rate        = 5
    }
    secrets = {
      api_key = {
        id = monad_secret.secret.id
      }
    }
  }
}

resource "monad_output" "output" {
  name        = "Terraform Example Generic Output"
  description = "Terraform Example Generic output for Monad"
  component_type = "dev-null"
}

resource "monad_pipeline" "pipeline" {
  name        = "Terraform Example Pipeline"
  description = "Terraform Example Pipeline for Monad"

  nodes {
    slug           = "input"
    component_type = "input"
    component_id   = monad_input.input.id
  }

  nodes {
    slug           = "output"
    component_type = "output"
    component_id   = monad_output.output.id
  }

  edges {
    from_node_instance_slug = "input"
    to_node_instance_slug   = "output"
    conditions {
      operator = "always"
    }
  }
}

output "pipeline_id" {
  value = monad_pipeline.pipeline.id
}