---
page_title: "monad_output Resource - monad"
subcategory: ""
description: |-
  Manages an output connector: the destination a Monad pipeline delivers records to.
---

# Resource: monad_output

An output is where records leave Monad. Each output has a **type** naming the destination connector (a SIEM such as Splunk or Elasticsearch, object storage such as S3, a messaging hook such as Slack, a Monad-internal store such as KV Lookup, or the `dev-null` sink; see the [outputs catalog](https://app.monad.com/docs/outputs)) and a **config** holding that connector's settings and credentials. Creating an output registers the destination; records flow to it once a `monad_pipeline` names it in an `output` node.

Outputs buffer and retry per node, so one destination going offline does not stop delivery to the others in the same pipeline.

## Example Usage

### A placeholder sink

```terraform
# The simplest output: discard everything. Useful as a placeholder sink while
# a pipeline is being built.
resource "monad_output" "sink" {
  name        = "Discard"
  description = "dev-null sink — swap type and settings for a real destination"
  type        = "dev-null"
}
```

### Object storage with nested settings

```terraform
# An object-storage destination with nested settings (format and batching).
resource "monad_output" "archive" {
  name        = "CloudTrail archive (S3)"
  description = "Durable archive of every ingested record, unmodified"
  type        = "s3"

  config {
    settings = {
      role_arn         = var.archive_role_arn
      bucket           = var.archive_bucket
      region           = "us-west-2"
      prefix           = "archive"
      compression      = "none"
      partition_format = "simple date"
      format_config = {
        Format      = "json"
        json_format = { type = "line" }
      }
      batch_config = {
        batch_record_count = 500
        batch_data_size    = 1048576
        publish_rate       = 5
      }
    }
  }
}
```

### Credentials referenced from `settings`

```terraform
# Some connectors take their credentials inside settings as a secret reference
# ({ id = ... }) rather than under config.secrets. The connector's docs page
# shows which; the shape below is the Slack output's webhook variant.
resource "monad_secret" "slack_webhook" {
  name  = "slack-alerts-webhook"
  value = var.slack_webhook_url
}

resource "monad_output" "slack" {
  name        = "Slack #security-alerts"
  description = "Posts fired and resolved Monad alerts to Slack"
  type        = "slack"

  config {
    settings = {
      auth_config = {
        type = "webhook"
        webhook = {
          webhook_url = { id = monad_secret.slack_webhook.id }
        }
      }
      message_template = file("${path.module}/slack-alert-template.tmpl")
    }
  }
}
```

### A KV Lookup table for enrichments

```terraform
# A KV Lookup output is a key-value table other pipelines can read from via a
# monad_enrichment of type "kv-lookup". ttl is the entry lifetime in seconds.
resource "monad_output" "dedup_store" {
  name        = "Dedup fingerprints (KV)"
  description = "Record fingerprints already shipped; 48h TTL is the dedup window"
  type        = "kv-lookup"

  config {
    settings = {
      key_field   = "_dedup_key"
      value_field = "_dedup_key"
      ttl         = 172800
    }
  }
}
```

## Argument Reference

The following arguments are required:

* `name` - (Required) Display name of the output.
* `type` - (Required) Connector type ID, exactly as the Monad outputs catalog shows it — for example `s3`, `splunk`, `slack`, `kv-lookup`, `dev-null`.

The following arguments are optional:

* `description` - (Optional) Free-text description.
* `config` - (Optional) Connector configuration. [See below](#config-block). Omit the block for a connector type that takes no settings and no credentials, such as `dev-null`.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

### `config` Block

The `config` block supports the following:

* `settings` - (Optional) The connector's settings as a free-form value. Keys are the connector's API field names, documented on its page in the [Monad outputs catalog](https://app.monad.com/docs/outputs). Nested objects (format, batching, authentication variants) are written as nested HCL objects. Write it as a plain HCL object; the provider sends whatever Terraform type the expression produces and hands the same value back after apply. Wrapping the value in `jsondecode(jsonencode({ ... }))` is a no-op for a literal and is only useful to flatten a set or map that arrives from a typed variable or another resource's attribute into JSON arrays and objects — and it has a cost: one sensitive value inside makes the whole object `(sensitive value)` in the plan.
* `secrets` - (Optional, Sensitive, Write-only) The connector's credentials as a map keyed by the connector's secret field names. Each value is either a reference `{ id = "..." }` or a new inline secret `{ name = "...", description = "...", value = "..." }` (all three non-empty). Write-only: sent to the API, never stored in state. Some connectors take the `{ id }` reference inside `settings` instead (the Slack example above); the connector's docs page says which.

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the output. Reference it from `monad_pipeline` nodes as `component_id`, and from a `kv-lookup` `monad_enrichment` as `kv_lookup_output_id`.
* `config.secrets_hash` - HMAC fingerprint of the configured `secrets`, maintained by the provider; null when no secrets are configured.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | |
| `type` | Input | |
| `description` | Input | |
| `config.settings` | Input | Refreshed from the API on read |
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

Each operation runs under its own deadline. A value here may exceed the provider default, so one slow output can be given more time without raising the budget for every call.

A create that outlives its `create` timeout is not simply reported as failed, because the API can finish the create after the provider stops waiting. The provider then polls the organization's outputs for up to two minutes for an output with the same `name` and `type` created since the request started: exactly one match is adopted into state with a warning, none is reported as "not created, safe to retry", and several are listed by `id` with a request to [import](#import) the right one rather than guess.

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import outputs using the output `id`. For example:

```terraform
import {
  to = monad_output.example
  id = "2b6e1f4a-9c3d-4e5f-8a7b-6c5d4e3f2a10"
}
```

Using `terraform import`, import outputs using the output `id`. For example:

```shell
# Import an output by its ID (shown in the Monad UI and returned by the API).
terraform import monad_output.example 2b6e1f4a-9c3d-4e5f-8a7b-6c5d4e3f2a10
```

If the configuration declares `config.secrets`, the first plan after import shows a one-time update that re-sends them and records `secrets_hash`; Monad never returns secret material for the provider to compare against.
