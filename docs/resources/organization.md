---
page_title: "monad_organization Resource - monad"
subcategory: ""
description: |-
  Manages a sub-organization of the provider's organization.
---

# Resource: monad_organization

A sub-organization is a child of the organization the provider is configured for (`organization_id`). It has its own pipelines, components, secrets, API keys and user assignments, and inherits the parent's billing account. Managing sub-organizations in Terraform lets one configuration create a tenant and populate it in the same apply.

Deleting a sub-organization deletes everything in it, and it cannot be undone. This resource therefore has `deletion_protection`, on by default: while it is `true`, any plan that would destroy or replace the sub-organization fails at plan time, before Terraform touches anything that depends on it.

## Example Usage

### Create a sub-organization

```terraform
resource "monad_organization" "acme" {
  name          = "acme"
  friendly_name = "Acme Corp"
  description   = "Managed by Terraform"
}
```

### Manage resources inside it

Point an aliased provider at the sub-organization's `id`, and give resources inside it `provider = monad.<alias>`. The provider defers connecting until the `id` is known, so the sub-organization and its contents can be created in one apply. The same API key works as long as its role permits managing those resources in sub-organizations.

```terraform
# The default provider manages the sub-organization in the parent org.
provider "monad" {
  organization_id = var.parent_organization_id
}

# An aliased provider points at the new sub-organization. The same API key
# works if its role permits managing these resources in sub-organizations.
provider "monad" {
  alias           = "acme"
  organization_id = monad_organization.acme.id
}

resource "monad_organization" "acme" {
  name = "acme"
}

resource "monad_secret" "acme_token" {
  provider = monad.acme
  name     = "acme-token"
  value    = var.acme_token
}
```

## Argument Reference

The following arguments are required:

* `name` - (Required) Name of the sub-organization: lowercase letters, digits and `-`, starting and ending with a letter or digit, at most 22 characters. The API stores it with a random four-character suffix; see `slug`. Changing `name` replaces the sub-organization, which `deletion_protection` refuses while it is `true`.

The following arguments are optional:

* `friendly_name` - (Optional) Display name shown in the Monad UI. Defaults to `name`. It cannot be empty; omit it instead.
* `description` - (Optional) Free-text description. Removing it clears the description on the server.
* `deletion_protection` - (Optional) Defaults to `true`. While `true`, a plan that would destroy or replace the sub-organization fails. To delete one, set this to `false`, apply, then destroy. The setting exists only in Terraform state; it is never sent to the API, so it does not stop deletion from the Monad UI or API.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the sub-organization. Use it as an aliased provider's `organization_id`.
* `slug` - The name as the API stored it, with its random suffix, such as `acme-161d`.
* `parent_organization_id` - The parent organization: the provider's `organization_id`.
* `billing_account_id` - Billing account, inherited from the parent.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | Changing it replaces the sub-organization |
| `friendly_name` | Input / Output | Defaults to `name` |
| `description` | Input | |
| `deletion_protection` | Input | Provider-side only; never sent to the API |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `id` | Output | |
| `slug` | Output | `name` plus a random suffix |
| `parent_organization_id` | Output | |
| `billing_account_id` | Output | |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

Each operation runs under its own deadline. A value here may exceed the provider default, so one slow call can be given more time without raising the budget for every call.

A create that outlives its `create` timeout is not simply reported as failed, because the API can finish the create after the provider stops waiting. The provider then polls the parent's sub-organizations for up to two minutes for one whose `slug` starts with `name` and that was created since the request started: exactly one match is adopted into state with a warning, none is reported as "not created, safe to retry", and several are listed by `id` with a request to [import](#import) the right one rather than guess.

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import sub-organizations using the sub-organization `id`. For example:

```terraform
import {
  to = monad_organization.acme
  id = "6f1d2c3b-4a5e-4f60-8a7b-9c0d1e2f3a4b"
}
```

Using `terraform import`, import sub-organizations using the sub-organization `id`. For example:

```shell
# Import a sub-organization by its ID. name is derived from the slug, and
# deletion_protection starts as true.
terraform import monad_organization.acme 6f1d2c3b-4a5e-4f60-8a7b-9c0d1e2f3a4b
```

The import derives `name` from the `slug` by dropping the suffix, and sets `deletion_protection` to `true`. The sub-organization must be a direct child of the provider's `organization_id`.
