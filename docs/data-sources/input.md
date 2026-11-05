---
page_title: "monad_input Data Source - monad"
subcategory: ""
description: |-
  Looks up an existing input by ID or name.
---

# Data Source: monad_input

Looks up an existing source connector in the organization by `id` or exact `name`, so a configuration can reference one that is managed elsewhere (another Terraform configuration, or the Monad UI) without hard-coding its UUID. To manage the input, use the [`monad_input` resource](../resources/input.md).

## Example Usage

```terraform
data "monad_input" "cloudtrail" {
  name = "CloudTrail (production)"
}

# Route an input managed in another repository into a pipeline managed here.
resource "monad_pipeline" "archive" {
  name = "CloudTrail archive"

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = data.monad_input.cloudtrail.id
  }
  nodes {
    slug           = "archive"
    component_type = "output"
    component_id   = monad_output.archive.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "archive"
    condition {
      operator = "always"
    }
  }
}
```

## Argument Reference

Exactly one of the following is required:

* `id` - (Optional) ID of the input. Set exactly one of `id` or `name`.
* `name` - (Optional) Exact, case-sensitive name. Set exactly one of `id` or `name`. Names are unique per input type in an organization.

No match is an error that lists some of the names that do exist; an ID that does not exist is an error.

## Attribute Reference

* `id` - ID of the input.
* `name` - Name of the input.
* `description` - Description.
* `type` - Connector type ID, such as `aws-s3`.
* `pipeline_ids` - Set of IDs of the pipelines that use this input. It reflects the organization when the data source is read, so a pipeline created in the same apply appears on the next plan.
* `created_at` - When it was created (RFC 3339).
* `updated_at` - When it was last changed (RFC 3339).
* `managed_by` - `customer` for objects you create; `system` or `internal` for ones Monad maintains.
