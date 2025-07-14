# Terraform Provider for Monad

This provider allows you to manage complete Monad data pipelines using Terraform, including data sources, destinations, secrets, and pipeline orchestration.

## Features

- **Pipeline Management**: Create and manage data pipelines with nodes and conditional logic
- **Secret Management**: Securely store and reference organization secrets
- **Input Connectors**: Configure data sources with flexible input configurations
- **Output Connectors**: Configure data destinations with flexible output configurations
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

# Configure an input for testing
resource "monad_input" "test_events" {
  name        = "demo-generator"
  description = "Generate test events for pipeline"
  type        = "demo"

  config {
    settings = {
      record_type = "event"
      rate        = 10  # Records per second
    }
  }
}

# Configure an output destination
resource "monad_output" "webhook" {
  name        = "webhook-output"
  description = "Send events to external webhook"
  type        = "http"

  config {
    settings = {
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

    secrets = {
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
      input_id = monad_input.test_events.id
    },
    {
      id   = "output"
      type = "output"
      output_id = monad_output.webhook.id
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

resource "monad_input" "audit_logs" {
  name        = "okta-audit-logs"
  description = "Collect Okta system audit logs"
  type        = "okta_systemlog"

  config {
    settings = {
      organization_url = "https://yourorg.okta.com"
    }

    secrets = {
      api_key = monad_secret.okta_api_key.reference
    }
  }
}
```

### PostgreSQL Output Example

```hcl
resource "monad_output" "database" {
  name        = "postgres-logs"
  description = "Store events in PostgreSQL database"
  type        = "postgresql"

  config {
    settings = {
      host         = "localhost"
      port         = 5432
      database     = "events"
      table        = "audit_logs"
      user         = "monad_user"
      column_names = ["timestamp", "event_type", "actor", "target"]
    }

    secrets = {
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

### monad_input

Generic input connector for data sources.

- `name` (string, required) - Name of the input
- `description` (string, optional) - Description of the input
- `type` (string, required) - Type of input connector (e.g., "demo", "okta_systemlog")
- `config` (block, optional) - Input configuration
  - `settings` (map, optional) - Type-specific settings
  - `secrets` (map, optional, sensitive) - Type-specific secrets

### monad_output

Generic output connector for data destinations.

- `name` (string, required) - Name of the output
- `description` (string, optional) - Description of the output
- `type` (string, required) - Type of output connector (e.g., "http", "postgresql")
- `config` (block, optional) - Output configuration
  - `settings` (map, optional) - Type-specific settings
  - `secrets` (map, optional, sensitive) - Type-specific secrets

## Import

All resources support import using their respective resource IDs:

```bash
# Import secrets
terraform import monad_secret.example secret-id-here

# Import pipelines
terraform import monad_pipeline.example pipeline-id-here

# Import inputs
terraform import monad_input.example input-id-here

# Import outputs
terraform import monad_output.example output-id-here
```
