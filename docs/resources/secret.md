---
page_title: "monad_secret Resource - monad"
subcategory: ""
description: |-
  Manages an organization secret: a named credential that inputs, outputs, enrichments and transforms reference by ID.
---

# Resource: monad_secret

A secret is an organization-scoped, named credential. Components never hold credential values inline; they hold a **reference** to a secret (`{ id = ... }`) and Monad resolves it at run time. Managing secrets in Terraform lets you declare the reference next to the component that uses it, while the value itself comes from a variable or a secrets-manager data source and never lands in state.

Two platform rules shape how this resource behaves. Monad **never returns a secret's value**, so the provider cannot compare what is stored with what is configured; it tracks a fingerprint instead. And Monad **refuses to delete a referenced secret**, so a `monad_secret` can be destroyed only after every component that references it has been updated or removed. Referencing the secret through `monad_secret.example.id` gives Terraform the dependency it needs to destroy things in the right order. See [Secrets](https://app.monad.com/docs/guides/secrets) in the Monad docs.

## Example Usage

### Create a secret

```terraform
# The value comes from a variable (or TF_VAR_*, or a secrets-manager data
# source) and is write-only: it is sent to Monad but never written to state.
variable "archive_secret_access_key" {
  type      = string
  sensitive = true
}

resource "monad_secret" "archive_secret_key" {
  name        = "archive-bucket-secret-key"
  description = "AWS secret access key for the archive bucket writer"
  value       = var.archive_secret_access_key
}
```

### Reference it from a component

```terraform
# Reference the secret's id wherever a component expects a credential. The
# reference is also what stops Terraform from destroying the secret while a
# component still uses it: Monad refuses to delete a referenced secret, and the
# dependency makes Terraform remove the component first.
resource "monad_output" "archive" {
  name = "Archive (S3)"
  type = "s3"

  config {
    settings = jsondecode(jsonencode({
      bucket = var.archive_bucket
      region = "us-west-2"
      prefix = "archive"
    }))

    secrets = {
      access_key = { id = monad_secret.archive_access_key.id }
      secret_key = { id = monad_secret.archive_secret_key.id }
    }
  }
}
```

## Argument Reference

The following arguments are required:

* `name` - (Required) Name of the secret. Shown in component credential selectors in the Monad UI.
* `value` - (Required, Sensitive, Write-only) The credential. Write-only: it is sent to the Monad API and never stored in Terraform state. Changing the value updates the secret in place; the provider detects the change through `value_hash`.

The following arguments are optional:

* `description` - (Optional) Free-text description. Removing it clears the description on the server.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the secret. This is what components reference: `{ id = monad_secret.example.id }`.
* `value_hash` - HMAC fingerprint of `value`, maintained by the provider. A changed `value` marks this `(known after apply)` and the apply re-sends the value; an unchanged value leaves it stable.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | |
| `description` | Input | |
| `value` | Input (write-only) | Never stored in state, never returned by the API |
| `value_hash` | Output | Provider-computed rotation fingerprint |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `id` | Output | |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

Each operation runs under its own deadline. A value here may exceed the provider default, so one slow secret can be given more time without raising the budget for every call.

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import secrets using the secret `id`. For example:

```terraform
import {
  to = monad_secret.example
  id = "3f1c9e64-8a12-4c7d-9f30-1b2c4d5e6f70"
}
```

Using `terraform import`, import secrets using the secret `id`. For example:

```shell
# Import a secret by its ID. Monad never returns the value, so the first plan
# after import re-sends the configured value once and records its fingerprint.
terraform import monad_secret.example 3f1c9e64-8a12-4c7d-9f30-1b2c4d5e6f70
```

Because the API never returns the value, the first plan after import shows a one-time update of `value_hash`: the apply re-sends the configured `value` and records its fingerprint. Plans are clean from then on.
