---
page_title: "monad_transform Resource - monad"
subcategory: ""
description: |-
  Manages a transform: an ordered list of operations that reshape each record as it passes through a Monad pipeline.
---

# Resource: monad_transform

A transform is a chain of up to 20 **operations** applied to each record, in order, as it passes through a `transform` node. Operations add, drop, rename, hash, mask or convert fields, or run an arbitrary [jq](https://app.monad.com/docs/transforms/jq) program; the full list is in the [transforms catalog](https://app.monad.com/docs/transforms). Almost every operation works on one record at a time, so transforms cannot aggregate across records.

A transform is a component like any other: it is created once and can be placed in several pipelines. Chain transforms in the pipeline when you need more than 20 operations, or to keep unrelated concerns (field trimming, normalization, routing flags) in separately reviewable resources.

## Example Usage

### Simple operations

```terraform
# A transform is an ordered list of operations (at most 20). Each operation
# names the operation and passes its arguments; the argument names are the ones
# on that operation's page in the Monad docs.
resource "monad_transform" "tag_and_trim" {
  name        = "Tag and trim"
  description = "Stamps the environment, then drops fields with no IR value"

  config = {
    operations = [
      {
        operation = "add"
        arguments = {
          key   = "environment"
          value = "production"
        }
      },
      {
        operation = "drop_key"
        arguments = { key = "requestID" }
      },
      {
        operation = "drop_key"
        arguments = { key = "responseElements.credentials.sessionToken" }
      },
    ]
  }
}
```

### A jq program from a file

```terraform
# Keep long jq programs in their own file and load them with file(). An empty
# key replaces the record with the query result; a non-empty key stores the
# result under that key instead.
resource "monad_transform" "cloudtrail_to_ecs" {
  name        = "CloudTrail to ECS"
  description = "Normalizes CloudTrail into ECS v8.11.0"

  config = {
    operations = [
      {
        operation = "jq"
        arguments = {
          key   = ""
          query = file("${path.module}/jq/cloudtrail-to-ecs.jq")
        }
      },
    ]
  }
}
```

### A jq program templated from other resources

```terraform
# templatefile() lets a jq program embed values from other resources — here a
# pipeline-id → name map — so a rebuild updates the transform in place.
resource "monad_transform" "label_alerts" {
  name = "Label alerts with pipeline names"

  config = {
    operations = [
      {
        operation = "jq"
        arguments = {
          key = ""
          query = templatefile("${path.module}/jq/label-alerts.jq.tftpl", {
            pipelines_json = jsonencode({
              (monad_pipeline.cloudtrail.id) = monad_pipeline.cloudtrail.name
              (monad_pipeline.archive.id)    = monad_pipeline.archive.name
            })
          })
        }
      },
    ]
  }
}
```

### An operation that references a secret

```terraform
# Operations that need key material reference a secret by ID, exactly like a
# connector does. Here the mask operation's deterministic mode hashes the value
# under _dedup_key with an HMAC key held in a monad_secret.
resource "monad_secret" "dedup_hmac_key" {
  name        = "dedup-hmac-key"
  description = "HMAC key for the deterministic dedup mask"
  value       = var.dedup_hmac_key
}

resource "monad_transform" "fingerprint" {
  name = "Fingerprint record"

  config = {
    operations = [
      {
        operation = "jq"
        arguments = {
          key   = "_dedup_key"
          query = "del(.ingested_at) | tojson"
        }
      },
      {
        operation = "mask"
        arguments = {
          key = "_dedup_key"
          mode = {
            type = "deterministic"
            deterministic = {
              hash_key = { id = monad_secret.dedup_hmac_key.id }
            }
          }
        }
      },
    ]
  }
}
```

## Argument Reference

The following arguments are required:

* `name` - (Required) Display name of the transform.
* `config` - (Required) The transform configuration: an object with a single `operations` list. [See below](#config-argument).

The following arguments are optional:

* `description` - (Optional) Free-text description.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

### `config` Argument

`config` is a free-form value shaped like the transform configuration the Monad API accepts. Write it as a plain HCL object. A list literal whose elements have different `arguments` shapes is an ordinary tuple and needs no special handling; `jsondecode(jsonencode({ ... }))`, which older examples wrapped around it, is a no-op for a literal and makes the whole `config` `(sensitive value)` in the plan if any part of it is sensitive.

* `operations` - (Required) Ordered list of at most 20 operations. Each element is an object:
    * `operation` - (Required) The operation name as the transforms catalog spells it, for example `add`, `drop_key`, `jq`, `mask`, `hash`, `flatten`, `convert_timestamp`.
    * `arguments` - (Required) That operation's arguments. Field names are the ones on the operation's page in the [transforms catalog](https://app.monad.com/docs/transforms). Arguments that take key material — the `mask` operation's deterministic `hash_key`, for instance — take a secret reference `{ id = monad_secret.example.id }`, never an inline string.

Two operations cover most needs:

* `jq` — `query` (required) is the program; `key` (optional) stores the result under that key instead of replacing the record; `prevent_data_dropping` (optional) errors instead of dropping a record whose query produces no output. Load long programs with `file()` or `templatefile()`.
* `drop_key`, `add`, `duplicate_key_value_to_key` and the other field operations — take a `key` in GJSON dot-path syntax (`responseElements.credentials.sessionToken`), plus a `value` where the operation writes one.

The provider sends `config` exactly as written and compares it to the API's stored copy semantically, so an empty-string field the API drops or a re-ordered object key does not show up as drift.

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the transform. Reference it from `monad_pipeline` nodes as `component_id`.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | |
| `description` | Input | |
| `config` | Input | Refreshed from the API on read; compared semantically |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `id` | Output | |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

Each operation runs under its own deadline. A value here may exceed the provider default, so one slow transform can be given more time without raising the budget for every call.

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import transforms using the transform `id`. For example:

```terraform
import {
  to = monad_transform.example
  id = "c4e2a7b9-1d3f-4b5c-a6d8-e9f0a1b2c3d4"
}
```

Using `terraform import`, import transforms using the transform `id`. For example:

```shell
# Import a transform by its ID (shown in the Monad UI and returned by the API).
terraform import monad_transform.example c4e2a7b9-1d3f-4b5c-a6d8-e9f0a1b2c3d4
```

The imported `config` is populated from the API, so `terraform plan -generate-config-out` produces a usable configuration. The first plan after import is clean when the configuration's `config` is semantically equal to the stored one.
