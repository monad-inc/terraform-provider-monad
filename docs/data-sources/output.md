---
page_title: "monad_output Data Source - monad"
subcategory: ""
description: |-
  Looks up an existing output by ID or name.
---

# Data Source: monad_output

Looks up an existing destination connector in the organization by `id` or exact `name`, so a configuration can reference one that is managed elsewhere (another Terraform configuration, or the Monad UI) without hard-coding its UUID. To manage the output, use the [`monad_output` resource](../resources/output.md).

## Example Usage

```terraform
data "monad_output" "siem" {
  name = "Production SIEM"
}

output "siem_feeds" {
  description = "Pipelines that write to the SIEM"
  value       = data.monad_output.siem.pipeline_ids
}
```

## Argument Reference

Exactly one of the following is required:

* `id` - (Optional) ID of the output. Set exactly one of `id` or `name`.
* `name` - (Optional) Exact, case-sensitive name. Set exactly one of `id` or `name`. Names are unique per output type in an organization.

No match is an error that lists some of the names that do exist; an ID that does not exist is an error.

## Attribute Reference

* `id` - ID of the output.
* `name` - Name of the output.
* `description` - Description.
* `type` - Connector type ID, such as `aws-s3`.
* `pipeline_ids` - Set of IDs of the pipelines that use this output. It reflects the organization when the data source is read, so a pipeline created in the same apply appears on the next plan.
* `created_at` - When it was created (RFC 3339).
* `updated_at` - When it was last changed (RFC 3339).
* `managed_by` - `customer` for objects you create; `system` or `internal` for ones Monad maintains.
