---
page_title: "monad_role Resource - monad"
subcategory: ""
description: |-
  Manages a custom organization role: a named set of permissions that users and API keys act as.
---

# Resource: monad_role

A role is a named set of permissions in one organization. Users and API keys are each assigned a role and can do exactly what its permissions allow. Every organization has four **built-in** roles: System Administrator, Contributor, Reader and Data Sender. The API protects them from change, so they cannot be managed by this resource; reference them with the [`monad_role` data source](../data-sources/role.md) instead. This resource manages custom roles, typically narrower ones for automation. See [Organization RBAC](https://app.monad.com/docs/guides/organization-rbac) in the Monad docs.

Two platform rules apply:

* **No privilege escalation.** The API refuses to create a role, or change a role's permissions, to include a permission that the credentials Terraform runs with do not hold.
* **A role in use cannot be deleted.** A role still assigned to a user or an API key must be reassigned first. When an API key in the same configuration references the role, Terraform destroys the key first.

## Example Usage

### A read-only role and a key that uses it

```terraform
# A read-only role for dashboards and audits. Permissions are slugs; see the
# monad_permissions data source for the full catalog.
resource "monad_role" "pipeline_viewer" {
  name        = "Pipeline viewer"
  description = "Read pipelines and their components"
  permissions = [
    "pipeline:read",
    "input:read",
    "output:read",
    "transform:read",
  ]
}

# A least-privilege key that acts as the role.
resource "monad_api_key" "dashboards" {
  name            = "dashboards"
  role_id         = monad_role.pipeline_viewer.id
  expiration_time = "2027-01-01T00:00:00Z"
}
```

## Argument Reference

The following arguments are required:

* `name` - (Required) Name of the role.
* `permissions` - (Required) Set of permission slugs the role grants, such as `pipeline:read` or `input:write`. At least one is required: the API cannot remove a role's last permission. An unknown slug fails at **plan** time with the list of valid ones; the provider reads the catalog only when the set is new or changed. Use the [`monad_permissions` data source](../data-sources/permissions.md) to read the catalog.

The following arguments are optional:

* `description` - (Optional) Free-text description. Removing it clears the description on the server.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the role. Use it as `monad_api_key.role_id`.
* `created_at` - When the role was created, as an RFC 3339 timestamp set by the API.
* `updated_at` - When the role was last changed. Shown as `(known after apply)` whenever the resource has other changes, because every write moves it.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | Refreshed from the API on read |
| `description` | Input | |
| `permissions` | Input | Set of slugs; refreshed on read, so a permission added or removed outside Terraform shows as drift |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `id` | Output | |
| `created_at` | Output | Set by the API |
| `updated_at` | Output | Changes on every write |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

A create that outlives its `create` timeout is not simply reported as failed, because the API can finish the create after the provider stops waiting. The provider polls the organization's roles for up to two minutes for one with the same `name` created since the request started: a match is adopted into state with a warning, and none is reported as "not created, safe to retry".

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import custom roles using the role `id`. For example:

```terraform
import {
  to = monad_role.example
  id = "5e1c5b09-04c0-4714-bd57-2ca745d779e1"
}
```

Using `terraform import`, import custom roles using the role `id`. For example:

```shell
# Import a custom role by its ID. Built-in roles cannot be imported; use the
# monad_role data source to reference them.
terraform import monad_role.example 5e1c5b09-04c0-4714-bd57-2ca745d779e1
```

The first plan after import is clean. Importing a built-in role fails with an error that points to the `monad_role` data source.
