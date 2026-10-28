---
page_title: "monad_transform Data Source - monad"
subcategory: ""
description: |-
  Looks up an existing transform by ID or name.
---

# Data Source: monad_transform

Looks up an existing transform in the organization by `id` or exact `name`, so a configuration can reference one that is managed elsewhere (another Terraform configuration, or the Monad UI) without hard-coding its UUID. To manage the transform, use the [`monad_transform` resource](../resources/transform.md).

## Example Usage

```terraform
data "monad_transform" "ocsf" {
  name = "CloudTrail to OCSF"
}
```

## Argument Reference

Exactly one of the following is required:

* `id` - (Optional) ID of the transform. Set exactly one of `id` or `name`.
* `name` - (Optional) Exact, case-sensitive name. Set exactly one of `id` or `name`. Names are unique per transform type in an organization.

No match is an error that lists some of the names that do exist; an ID that does not exist is an error.

## Attribute Reference

* `id` - ID of the transform.
* `name` - Name of the transform.
* `description` - Description.
* `pipeline_ids` - Set of IDs of the pipelines that use this transform. It reflects the organization when the data source is read, so a pipeline created in the same apply appears on the next plan.
* `created_at` - When it was created (RFC 3339).
* `updated_at` - When it was last changed (RFC 3339).
* `managed_by` - `customer` for objects you create; `system` or `internal` for ones Monad maintains.
