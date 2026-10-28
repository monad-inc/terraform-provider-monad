---
page_title: "monad_api_key Resource - monad"
subcategory: ""
description: |-
  Manages an organization API key: a token that authenticates calls to the Monad API with the permissions of one role.
---

# Resource: monad_api_key

An API key authenticates requests to the Monad API (the `x-api-key` header) as a role in one organization. It carries that role's permissions, expires at a fixed time, and is scoped to the organization it was created in. A sub-organization needs a key of its own. See [API Keys](https://app.monad.com/docs/guides/api-keys) in the Monad docs.

Three platform rules shape this resource:

* **The token is shown once.** The API returns a key's token only when the key is created or regenerated, never on read. The provider therefore stores `token` in Terraform **state**, as a sensitive attribute, because that is the only way to pass it to anything else in the configuration. Protect your state accordingly (an encrypted remote backend with restricted access), as you would for any resource that holds a credential.
* **Only the name and description can change in place.** Changing `expiration_time` **regenerates** the key: the `id` stays the same, `token` changes, and the old token stops working when the apply finishes. Changing `role_id` **replaces** the key, giving a new `id` and a new token.
* **No privilege escalation.** The API refuses to create or regenerate a key whose role holds a permission that the credentials Terraform runs with do not have.

## Example Usage

### Create a key and pass its token on

```terraform
# An API key for a CI job that only sends data. Pick the narrowest role that
# works; the API refuses to grant a key permissions the caller lacks.
data "monad_role" "data_sender" {
  name = "Data Sender"
}

resource "monad_api_key" "ci_sender" {
  name            = "ci-data-sender"
  description     = "Used by the ingest job in CI"
  role_id         = data.monad_role.data_sender.id
  expiration_time = "2027-01-01T00:00:00Z"
}

# The token is returned only when the key is created or regenerated, so the
# provider keeps it in state (sensitive). Pass it on from here, for example
# into your CI system's secret store.
output "ci_sender_token" {
  value     = monad_api_key.ci_sender.token
  sensitive = true
}
```

### Rotate on a schedule

```terraform
# Rotate by moving the expiration. Changing expiration_time regenerates the key
# in place: the id stays the same, `token` changes, and the previous token stops
# working as soon as the apply finishes. Update whatever consumes the token in
# the same apply.
#
# time_rotating (hashicorp/time) moves its timestamp every 30 days, so each
# apply after that point rotates the key. The new expiration is only known at
# apply time; the plan shows `token` as known after apply.
data "monad_role" "reader" {
  name = "Reader"
}

resource "time_rotating" "api_key" {
  rotation_days = 30
}

resource "monad_api_key" "rotated" {
  name            = "nightly-export"
  role_id         = data.monad_role.reader.id
  expiration_time = timeadd(time_rotating.api_key.rfc3339, "1440h") # 60 days: a 30-day overlap
}
```

## Argument Reference

The following arguments are required:

* `name` - (Required) Name of the key. Can be changed in place.
* `role_id` - (Required) ID of the organization role the key acts as: a `monad_role` resource, or a built-in role looked up with the `monad_role` data source (`data.monad_role.reader.id`). Changing it replaces the key.
* `expiration_time` - (Required) When the key expires, as an RFC 3339 timestamp (`2027-01-01T00:00:00Z`). Must be in the future when it is set or changed; the provider checks this at plan time. Changing it regenerates the key and its token. Rewriting the same moment in another form (an offset instead of `Z`) does not regenerate.

The following arguments are optional:

* `description` - (Optional) Free-text description. Removing it clears the description on the server.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the key.
* `token` - (Sensitive) The token to send in the `x-api-key` header. Stored in state. `(known after apply)` whenever the key is created, replaced or regenerated. Null after `terraform import`, because the API does not return it; change `expiration_time` to regenerate the key and obtain one.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | Refreshed from the API on read |
| `description` | Input | |
| `role_id` | Input | Changing it replaces the key |
| `expiration_time` | Input | Compared as an instant, so the API's UTC form never reads as drift |
| `token` | Output (sensitive) | Stored in state; returned only by create and regenerate |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `id` | Output | |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

A create that outlives its `create` timeout is not simply reported as failed, because the API can finish the create after the provider stops waiting. The provider polls the organization's keys for up to two minutes for one with the same `name` created since the request started. A match is adopted into state, and because its token was in the response that never arrived, the provider immediately **regenerates** it to obtain one, with a warning. No match is reported as "not created, safe to retry".

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import API keys using the key `id`. For example:

```terraform
import {
  to = monad_api_key.example
  id = "37989e32-7c9f-48a8-bdae-eb84d45a79ee"
}
```

Using `terraform import`, import API keys using the key `id`. For example:

```shell
# Import an API key by its ID. The API never returns a key's token after
# creation, so `token` is null in state until the key is next regenerated.
terraform import monad_api_key.example 37989e32-7c9f-48a8-bdae-eb84d45a79ee
```

The first plan after import is clean. `token` stays null until the key is regenerated.
