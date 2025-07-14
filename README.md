# Terraform Provider for Monad

This provider allows you to manage Monad outputs using Terraform.

## Features

- **HTTP Output**: Configure HTTP endpoints to send data to external APIs
- **PostgreSQL Output**: Configure PostgreSQL databases to store data
- Both output types use the unified Monad `/v2/{organization_id}/outputs` API

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
  base_url        = "https://api.monad.inc"  # Optional, defaults to this value
  api_token       = var.monad_api_token       # Can use MONAD_API_TOKEN env var
  organization_id = var.organization_id       # Can use MONAD_ORGANIZATION_ID env var
}
```

### HTTP Output Example

```hcl
resource "monad_http_output" "webhook" {
  name        = "webhook-output"
  description = "Send logs to external webhook"
  
  config {
    settings {
      endpoint             = "https://api.example.com/webhooks/logs"
      method              = "POST"
      headers = {
        "Content-Type" = "application/json"
        "User-Agent"   = "Monad-Output/1.0"
      }
      max_batch_data_size    = 1024
      max_batch_record_count = 100
      payload_structure      = "array"
      rate_limit            = 10
      tls_skip_verify       = false
    }
    
    secrets {
      auth_headers = {
        "Authorization" = "Bearer ${var.webhook_token}"
        "X-API-Key"     = var.api_key
      }
    }
  }
}
```

### PostgreSQL Output Example

```hcl
resource "monad_postgresql_output" "database" {
  name        = "postgres-logs"
  description = "Store logs in PostgreSQL database"
  
  config {
    settings {
      host         = "localhost"
      port         = 5432
      database     = "logs"
      table        = "events"
      user         = "logger"
      column_names = ["timestamp", "level", "message", "source"]
    }
    
    secrets {
      password = var.db_password
      # Alternatively, use connection string:
      # connection_string = "postgresql://user:pass@host:5432/dbname"
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

### monad_http_output

#### Configuration

- `name` (string, required) - Name of the output
- `description` (string, optional) - Description of the output
- `config` (block, optional) - HTTP configuration
  - `settings` (block, optional) - HTTP settings
    - `endpoint` (string, required) - HTTP endpoint URL
    - `method` (string, optional) - HTTP method
    - `headers` (map, optional) - Non-secret headers
    - `max_batch_data_size` (number, optional) - Maximum batch size in KB
    - `max_batch_record_count` (number, optional) - Maximum records per batch
    - `payload_structure` (string, optional) - Payload structure type
    - `rate_limit` (number, optional) - Requests per second limit
    - `tls_skip_verify` (boolean, optional) - Skip TLS verification
    - `wrapper_key` (string, optional) - Wrapper key for wrapped payloads
  - `secrets` (block, optional) - HTTP secrets
    - `auth_headers` (map, optional, sensitive) - Authentication headers

### monad_postgresql_output

#### Configuration

- `name` (string, required) - Name of the output
- `description` (string, optional) - Description of the output
- `config` (block, optional) - PostgreSQL configuration
  - `settings` (block, optional) - PostgreSQL settings
    - `host` (string, required) - Database host
    - `port` (number, optional) - Database port
    - `database` (string, required) - Database name
    - `table` (string, required) - Table name
    - `user` (string, required) - Database user
    - `column_names` (list, optional) - Column names for data
  - `secrets` (block, optional) - PostgreSQL secrets
    - `connection_string` (string, optional, sensitive) - Complete connection string
    - `password` (string, optional, sensitive) - Database password

## Import

Both resources support import using the output ID:

```bash
terraform import monad_http_output.example output-id-here
terraform import monad_postgresql_output.example output-id-here
```