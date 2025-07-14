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

resource "monad_output_http" "example" {
  name        = "example-http-output"
  description = "Example HTTP output for sending data to webhook"

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

# Output the created resource ID
output "output_http_id" {
  value = monad_output_http.example.id
}
