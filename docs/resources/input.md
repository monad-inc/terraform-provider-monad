---
page_title: "monad_input Resource - monad"
subcategory: ""
description: |-
  Manages an input connector: the source that pulls or receives records into a Monad pipeline.
---

# Resource: monad_input

An input is where records enter Monad. Each input has a **type** naming the connector (an Okta system log poller, an S3 bucket reader, an HTTP ingest endpoint, a synthetic event generator, and [several hundred more](https://app.monad.com/docs/inputs)) and a **config** holding that connector's settings and credentials. Creating an input registers the connector; it does not start moving data until a `monad_pipeline` names it in an `input` node.

Pull connectors run on a schedule and keep their own cursor, so a single input can feed several pipelines. Push connectors (HTTP, syslog, OTLP) expose an endpoint per pipeline instead.

## Example Usage

### Synthetic data

```terraform
# Synthetic CloudTrail events, one record per second. The "demo" input needs
# no credentials, so it is the quickest way to get data flowing.
resource "monad_input" "demo" {
  name        = "CloudTrail (synthetic)"
  description = "Event Generator producing CloudTrail-shaped records for testing"
  type        = "demo"

  config {
    settings = {
      record_type = "cloudtrail"
      rate        = 1
    }
  }
}
```

### A connector with no settings

```terraform
# Some connector types take no settings at all. Omit the config block entirely.
resource "monad_input" "http" {
  name        = "HTTP ingest"
  description = "Records POSTed to the pipeline's ingest endpoint"
  type        = "monad-http"
}
```

### Pull connector with settings

```terraform
# A pull connector with settings that reference other Terraform values. Every
# key under settings is the connector's API field name — see the "API Examples"
# section of that connector's page in the Monad docs.
resource "monad_input" "archive" {
  name        = "CloudTrail archive (S3)"
  description = "Reads NDJSON CloudTrail objects from the ingest bucket"
  type        = "s3"

  config {
    settings = jsondecode(jsonencode({
      bucket              = var.ingest_bucket
      region              = "us-west-2"
      prefix              = "cloudtrail"
      role_arn            = var.ingest_role_arn
      compression         = "none"
      format              = "jsonl"
      partition_format    = "simple date"
      backfill_start_time = "2026-01-01T00:00:00Z"
    }))
  }
}
```

### Credentials under `config.secrets`

```terraform
# Credentials go under config.secrets, keyed by the connector's secret field
# names. Each value is either a reference to an existing secret ({ id }) or a
# new secret created inline ({ value, name, description } — all three required).
# The block is write-only: nothing under secrets is ever stored in state.
resource "monad_secret" "aws_secret_key" {
  name  = "s3-archive-secret-key"
  value = var.aws_secret_access_key
}

resource "monad_input" "archive_static_creds" {
  name = "CloudTrail archive (S3, static credentials)"
  type = "s3"

  config {
    settings = jsondecode(jsonencode({
      bucket = var.ingest_bucket
      region = "us-west-2"
      prefix = "cloudtrail"
      format = "jsonl"
    }))

    secrets = {
      # Reference a secret managed elsewhere in this configuration.
      secret_key = { id = monad_secret.aws_secret_key.id }

      # Or create one inline. The secret is stored by name, so rotate the
      # value by giving it a new name too.
      access_key = {
        name        = "s3-archive-access-key"
        description = "Access key ID for the archive bucket reader"
        value       = var.aws_access_key_id
      }
    }
  }
}
```

## Argument Reference

The following arguments are required:

* `name` - (Required) Display name of the input. Shown in the Monad UI and in alert payloads.
* `type` - (Required) Connector type ID, exactly as the Monad inputs catalog shows it — for example `okta-systemlog`, `s3`, `monad-http`, `demo`. Changing it updates the input in place; the new type's settings must be supplied at the same time.

The following arguments are optional:

* `description` - (Optional) Free-text description.
* `config` - (Optional) Connector configuration. [See below](#config-block). Omit the block for a connector type that takes no settings and no credentials.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

### `config` Block

The `config` block supports the following:

* `settings` - (Optional) The connector's settings as a free-form value. Keys are the connector's API field names, documented on its page in the [Monad inputs catalog](https://app.monad.com/docs/inputs) — the "API Examples" section shows the exact JSON. Any HCL object works; wrap it in `jsondecode(jsonencode({ ... }))` when the object mixes value types or must be `{}`.
* `secrets` - (Optional, Sensitive, Write-only) The connector's credentials as a map keyed by the connector's secret field names. Each value is one of:
    * `{ id = "..." }` — a reference to an existing secret, usually `monad_secret.example.id`.
    * `{ name = "...", description = "...", value = "..." }` — a new secret created inline, all three non-empty. Monad stores inline secrets by name, so give a rotated value a new name too.

    A bare string is rejected. The value is write-only: it is sent to the Monad API but never persisted in Terraform state, and Monad never echoes secret material back. Rotation is detected through `secrets_hash`.

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the input. Reference it from `monad_pipeline` nodes as `component_id`.
* `config.secrets_hash` - HMAC fingerprint of the configured `secrets`, maintained by the provider. When the configured secrets change, this becomes `(known after apply)` and the update sends the new values; it is null when no secrets are configured.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | |
| `type` | Input | |
| `description` | Input | |
| `config.settings` | Input | Refreshed from the API on read; genuine out-of-band changes show as drift |
| `config.secrets` | Input (write-only) | Never stored in state, never read back |
| `config.secrets_hash` | Output | Provider-computed rotation fingerprint |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `id` | Output | |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

Each operation runs under its own deadline. A value here may exceed the provider default, so one slow input can be given more time without raising the budget for every call.

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import inputs using the input `id`. For example:

```terraform
import {
  to = monad_input.example
  id = "8f0c7a2e-4b1d-4c3e-9a6f-2d5e8b7c1a90"
}
```

Using `terraform import`, import inputs using the input `id`. For example:

```shell
# Import an input by its ID (shown in the Monad UI and returned by the API).
terraform import monad_input.example 8f0c7a2e-4b1d-4c3e-9a6f-2d5e8b7c1a90
```

Monad never returns secret material, so an imported input has a null `config.secrets_hash`. If the configuration declares `secrets`, the first plan after import shows a one-time update that re-sends them and records the fingerprint.
