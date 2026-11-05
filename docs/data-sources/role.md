---
page_title: "monad_role Data Source - monad"
subcategory: ""
description: |-
  Looks up an organization role by name, including the built-in roles.
---

# Data Source: monad_role

Looks up a role in the organization by its exact name. Use it to reference the built-in roles (System Administrator, Contributor, Reader, Data Sender), whose IDs differ per organization and which cannot be managed as [`monad_role` resources](../resources/role.md), or a custom role managed elsewhere.

## Example Usage

```terraform
# Reference a built-in role without hard-coding its per-organization ID.
data "monad_role" "data_sender" {
  name = "Data Sender"
}

resource "monad_api_key" "ingest" {
  name            = "ingest"
  role_id         = data.monad_role.data_sender.id
  expiration_time = "2027-01-01T00:00:00Z"
}
```

## Argument Reference

* `name` - (Required) Exact, case-sensitive name of the role. No match, or more than one role with that name, is an error listing the organization's roles.

## Attribute Reference

* `id` - UUID of the role, for `monad_api_key.role_id`.
* `description` - The role's description.
* `protected` - `true` for a built-in role, which the API does not allow to be changed or deleted.
* `permissions` - Set of permission slugs the role grants.
