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

resource "monad_input" "demo_input_generic" {
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

resource "monad_output" "demo_output_generic" {
  name        = "Terraform Example Generic Output"
  description = "Terraform Example Generic output for Monad"
  component_type = "dev-null"
}

resource "monad_secret" "secret" {
  name        = "Terraform Example Secret"
  description = "Terraform Example Secret for Monad"
  value       = "your-secret-value"
}

resource "monad_input_demo" "input_demo" {
  name        = "Terraform Demo Jira Users"
  description = "Terraform Example Input for Jira Users"

  config {
    settings {
      record_type = "jira_users"
      rate = 5
    }
  }
}

resource "monad_input_okta_systemlog" "input_okta_system_audit_logs" {
  name        = "Terraform Demo Okta System Audit Logs"
  description = "Terraform Example Input for Okta System Audit Logs"

  config {
    settings {
      org_url = "https://example.okta.com"
    }
    secrets {
      api_key = {
        id = monad_secret.secret.id
      }
    }
  }
}

resource "monad_output_http" "output_http" {
  name        = "Terraform Example HTTP Output"
  description = "Terraform Example HTTP output for sending data to webhook"

  config {
    settings {
      endpoint                = "https://google.com"
      method                  = "POST"
      max_batch_data_size     = 1024
      max_batch_record_count  = 100
      payload_structure       = "wrapped"
      rate_limit              = 10
      tls_skip_verify        = false
      wrapper_key            = "data"

      headers = [
        {
          key   = "Content-Type"
          value = "application/json"
        },
        {
          key   = "User-Agent"
          value = "Monad-HTTP-Output/1.0"
        }
      ]
    }
  }
}

resource "monad_pipeline" "pipeline" {
  name        = "Terraform Example Pipeline"
  description = "Terraform Example Pipeline for Monad"

  nodes {
    slug           = "input-demo"
    component_type = "input"
    component_id   = monad_input_demo.input_demo.id
  }

  nodes {
    slug           = "output-http"
    component_type = "output"
    component_id   = monad_output_http.output_http.id
  }

  edges {
    from_node_instance_slug = "input-demo"
    to_node_instance_slug   = "output-http"
    conditions {
      operator = "always"
    }
  }
}

# Output the created resource IDs
output "input_demo_id" {
  value = monad_input_demo.input_demo.id
}

output "output_http_id" {
  value = monad_output_http.output_http.id
}

output "pipeline_id" {
  value = monad_pipeline.pipeline.id
}