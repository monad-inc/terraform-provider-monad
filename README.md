# Terraform Provider for Monad

This provider allows you to manage complete Monad data pipelines using Terraform, including data sources, destinations, secrets, and pipeline orchestration.

## Features

- **Pipeline Management**: Create and manage data pipelines with nodes and conditional logic
- **Secret Management**: Securely store and reference organization secrets
- **Input Connectors**: Configure data sources including demo generators and Okta System Logs
- **Output Connectors**: Configure data destinations including HTTP webhooks and PostgreSQL databases
- **Unified API**: Uses Monad's V2 APIs for modern resource management

## Usage

### Provider Configuration

```hcl
terraform {
  required_providers {
    monad = {
      source = "monad-inc/monad"
    }
  }
}

provider "monad" {
  base_url        = "https://beta.monad.com"  # Optional, defaults to this value
  api_token       = var.monad_api_token       # Can use MONAD_API_TOKEN env var
  organization_id = var.organization_id       # Can use MONAD_ORGANIZATION_ID env var
}
```

### Complete Pipeline Example

```hcl
# Create a secret for API authentication
resource "monad_secret" "webhook_token" {
  name  = "webhook-auth-token"
  value = var.webhook_token
}

# Configure a demo input for testing
resource "monad_input_demo" "test_events" {
  name        = "demo-generator"
  description = "Generate test events for pipeline"

  config {
    settings {
      record_type = "event"
      rate        = 10  # Records per second
    }
  }
}

# Configure HTTP output destination
resource "monad_output_http" "webhook" {
  name        = "webhook-output"
  description = "Send events to external webhook"

  config {
    settings {
      endpoint             = "https://api.example.com/webhooks/events"
      method              = "POST"
      headers = {
        "Content-Type" = "application/json"
        "User-Agent"   = "Monad-Pipeline/1.0"
      }
      max_batch_data_size    = 1024
      max_batch_record_count = 100
      payload_structure      = "array"
      rate_limit            = 10
      tls_skip_verify       = false
    }

    secrets {
      auth_headers = {
        "Authorization" = "Bearer ${monad_secret.webhook_token.reference}"
      }
    }
  }
}

# Create a pipeline connecting input to output
resource "monad_pipeline" "demo_pipeline" {
  name        = "demo-to-webhook"
  description = "Demo events to webhook pipeline"

  # Define pipeline nodes
  nodes = [
    {
      id   = "input"
      type = "input"
      input_id = monad_input_demo.test_events.id
    },
    {
      id   = "output"
      type = "output"
      output_id = monad_output_http.webhook.id
    }
  ]

  # Define data flow
  edges = [
    {
      from = "input"
      to   = "output"
    }
  ]
}
```

### Okta System Log Input Example

```hcl
resource "monad_secret" "okta_api_key" {
  name  = "okta-api-key"
  value = var.okta_api_key
}

resource "monad_input_okta_systemlog" "audit_logs" {
  name        = "okta-audit-logs"
  description = "Collect Okta system audit logs"

  config {
    settings {
      organization_url = "https://yourorg.okta.com"
    }

    secrets {
      api_key = monad_secret.okta_api_key.reference
    }
  }
}
```

### PostgreSQL Output Example

```hcl
resource "monad_output_postgresql" "database" {
  name        = "postgres-logs"
  description = "Store events in PostgreSQL database"

  config {
    settings {
      host         = "localhost"
      port         = 5432
      database     = "events"
      table        = "audit_logs"
      user         = "monad_user"
      column_names = ["timestamp", "event_type", "actor", "target"]
    }

    secrets {
      password = var.db_password
    }
  }
}
```

## Environment Variables

- `MONAD_BASE_URL` - Base URL for the Monad API
- `MONAD_API_TOKEN` - API token for authentication
- `MONAD_ORGANIZATION_ID` - Organization ID for all resources

## Building

```bash
go build -o terraform-provider-monad
```

## Development

To use the provider locally:

1. Build the provider: `go build -o terraform-provider-monad`
2. Create a `.terraformrc` file in your home directory:

```hcl
provider_installation {
  dev_overrides {
    "monad-inc/monad" = "/path/to/your/terraform-provider-monad"
  }

  direct {}
}
```

3. Use the provider in your Terraform configurations

## Resources

### monad_secret

Manages organization secrets that can be referenced by other resources.

- `name` (string, required) - Name of the secret
- `value` (string, required, sensitive) - Secret value
- `reference` (string, computed) - Reference string to use in other resources

### monad_pipeline

Manages data pipelines that connect inputs to outputs with conditional logic.

- `name` (string, required) - Name of the pipeline
- `description` (string, optional) - Description of the pipeline
- `nodes` (list, required) - Pipeline nodes configuration
  - `id` (string, required) - Unique node identifier
  - `type` (string, required) - Node type: "input", "output", or "condition"
  - `input_id` (string, optional) - Input resource ID for input nodes
  - `output_id` (string, optional) - Output resource ID for output nodes
  - `condition` (string, optional) - Condition expression for condition nodes
- `edges` (list, required) - Pipeline edge connections
  - `from` (string, required) - Source node ID
  - `to` (string, required) - Destination node ID

### monad_input_demo

Event generator for testing and development.

- `name` (string, required) - Name of the input
- `description` (string, optional) - Description of the input
- `config` (block, optional) - Demo input configuration
  - `settings` (block, optional) - Demo settings
    - `record_type` (string, required) - Type of records to generate
    - `rate` (number, required) - Generation rate (1-1000 records/second)

### monad_input_okta_systemlog

Okta System Audit Logs integration.

- `name` (string, required) - Name of the input
- `description` (string, optional) - Description of the input
- `config` (block, optional) - Okta configuration
  - `settings` (block, optional) - Okta settings
    - `organization_url` (string, required) - Okta organization URL
  - `secrets` (block, optional) - Okta secrets
    - `api_key` (string, required, sensitive) - Okta API key

### monad_output_http

HTTP webhook output destination.

- `name` (string, required) - Name of the output
- `description` (string, optional) - Description of the output
- `config` (block, optional) - HTTP configuration
  - `settings` (block, optional) - HTTP settings
    - `endpoint` (string, required) - HTTP endpoint URL
    - `method` (string, optional) - HTTP method (default: POST)
    - `headers` (map, optional) - Non-secret headers
    - `max_batch_data_size` (number, optional) - Maximum batch size in KB
    - `max_batch_record_count` (number, optional) - Maximum records per batch
    - `payload_structure` (string, optional) - Payload structure type
    - `rate_limit` (number, optional) - Requests per second limit
    - `tls_skip_verify` (boolean, optional) - Skip TLS verification
    - `wrapper_key` (string, optional) - Wrapper key for wrapped payloads
  - `secrets` (block, optional) - HTTP secrets
    - `auth_headers` (map, optional, sensitive) - Authentication headers

### monad_output_postgresql

PostgreSQL database output destination.

- `name` (string, required) - Name of the output
- `description` (string, optional) - Description of the output
- `config` (block, optional) - PostgreSQL configuration
  - `settings` (block, optional) - PostgreSQL settings
    - `host` (string, required) - Database host
    - `port` (number, optional) - Database port (default: 5432)
    - `database` (string, required) - Database name
    - `table` (string, required) - Table name
    - `user` (string, required) - Database user
    - `column_names` (list, optional) - Column names for data
  - `secrets` (block, optional) - PostgreSQL secrets
    - `connection_string` (string, optional, sensitive) - Complete connection string
    - `password` (string, optional, sensitive) - Database password

## Import

All resources support import using their respective resource IDs:

```bash
# Import secrets
terraform import monad_secret.example secret-id-here

# Import pipelines
terraform import monad_pipeline.example pipeline-id-here

# Import inputs
terraform import monad_input_demo.example input-id-here
terraform import monad_input_okta_systemlog.example input-id-here

# Import outputs
terraform import monad_output_http.example output-id-here
terraform import monad_output_postgresql.example output-id-here
```
