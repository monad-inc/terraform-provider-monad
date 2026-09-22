---
page_title: "monad_pipeline Resource - monad"
subcategory: ""
description: |-
  Manages a pipeline: the graph of input, transform, enrichment and output nodes, and the conditional edges that route records between them.
---

# Resource: monad_pipeline

A pipeline is the graph that moves data. Its **nodes** are references to components you have already created — a `monad_input`, one or more `monad_transform` and `monad_enrichment` resources, and one or more `monad_output` resources — and its **edges** connect nodes by slug, each carrying a **condition** that decides which records pass. Data flows forward only: a node has exactly one incoming edge but may fan out along many outgoing edges, and a record follows every outgoing edge whose condition matches. A record that matches no outgoing edge is dropped at that node, which is how filtering works.

Creating the pipeline is what starts data moving: components on their own do nothing. A pipeline is created enabled unless `enabled = false` is set. See [Pipelines](https://app.monad.com/docs/pipelines) and [Data Routing](https://app.monad.com/docs/routing) in the Monad docs.

~> `nodes` and `edges` are **sets**. Their order in the configuration carries no meaning, a node is identified by its slug and component, and an edge by its `from`/`to` pair. Reordering blocks never produces a diff, and a set element cannot be addressed by index (`edges[0]`); select by value with a `for` expression instead.

## Example Usage

### Input, transform, output

```terraform
# The smallest useful pipeline: input → transform → output. Nodes name the
# components; edges connect nodes by slug. An "always" condition passes every
# record.
resource "monad_pipeline" "basic" {
  name        = "CloudTrail to archive"
  description = "Tags each record, then archives it"

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
  }
  nodes {
    slug           = "tag"
    component_type = "transform"
    component_id   = monad_transform.tag_and_trim.id
  }
  nodes {
    slug           = "archive"
    component_type = "output"
    component_id   = monad_output.archive.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "tag"
    condition {
      operator = "always"
    }
  }
  edges {
    from_node_instance_slug = "tag"
    to_node_instance_slug   = "archive"
    condition {
      operator = "always"
    }
  }
}
```

### Fan-out with conditional routing

```terraform
# Fan-out with conditional routing. A node may have many outgoing edges, each
# with its own condition; a record follows every edge whose condition matches
# and is dropped if none does. The root operator is always a logical operator
# (always, never, and, or, nor, xor) wrapping one or more leaf conditions.
resource "monad_pipeline" "routed" {
  name    = "CloudTrail routed by severity"
  enabled = true

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
  }
  nodes {
    slug           = "normalize"
    component_type = "transform"
    component_id   = monad_transform.cloudtrail_to_ecs.id
  }
  nodes {
    slug           = "asset-context"
    component_type = "enrichment"
    component_id   = monad_enrichment.asset_context.id
  }
  nodes {
    slug           = "siem"
    component_type = "output"
    component_id   = monad_output.siem.id
  }
  nodes {
    slug           = "archive"
    component_type = "output"
    component_id   = monad_output.archive.id
  }
  nodes {
    slug           = "sample"
    component_type = "output"
    component_id   = monad_output.sample_bucket.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "normalize"
    condition {
      operator = "always"
    }
  }
  edges {
    from_node_instance_slug = "normalize"
    to_node_instance_slug   = "asset-context"
    condition {
      operator = "always"
    }
  }

  # Everything goes to the archive.
  edges {
    from_node_instance_slug = "asset-context"
    to_node_instance_slug   = "archive"
    condition {
      operator = "always"
    }
  }

  # Only failures and console logins from outside the corporate ranges reach
  # the SIEM: (error_code exists OR event is ConsoleLogin) AND NOT internal.
  edges {
    name                    = "high value to SIEM"
    from_node_instance_slug = "asset-context"
    to_node_instance_slug   = "siem"

    condition {
      operator = "and"

      conditions {
        type_id = "equals_any"
        config {
          key    = "event.action"
          values = ["ConsoleLogin", "AssumeRole", "CreateAccessKey"]
        }
      }
      conditions {
        type_id = "starts_with"
        config {
          key   = "source.ip"
          value = "10."
          not   = true
        }
      }
    }
  }

  # A 5% sample of everything else, for tuning detections offline.
  edges {
    name                    = "sample for tuning"
    from_node_instance_slug = "asset-context"
    to_node_instance_slug   = "sample"

    condition {
      operator = "and"
      conditions {
        type_id = "sample"
        config {
          percent = 5
        }
      }
    }
  }
}
```

### Every condition rule

```terraform
# One edge showing each leaf rule. Every rule reads `key`, and most accept
# `not`; the remaining fields are rule-specific and the provider rejects a
# mismatch at plan time.
resource "monad_pipeline" "every_rule" {
  name = "Condition rule reference"

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
  }
  nodes {
    slug           = "sink"
    component_type = "output"
    component_id   = monad_output.sink.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "sink"

    condition {
      # "nor" passes a record only when NONE of the leaves match.
      operator = "nor"

      conditions {
        type_id = "equals"
        config {
          key              = "event.outcome"
          value            = "success"
          case_insensitive = true
        }
      }
      conditions {
        type_id = "contains"
        config {
          key   = "user.name"
          value = "svc-"
          raw   = true
        }
      }
      conditions {
        type_id = "ends_with"
        config {
          key   = "user.email"
          value = "@example.com"
        }
      }
      conditions {
        type_id = "greater_than"
        config {
          key   = "http.response.status_code"
          value = "499"
        }
      }
      conditions {
        type_id = "less_than"
        config {
          key   = "event.duration"
          value = "10"
        }
      }
      conditions {
        type_id = "matches_regex"
        config {
          key     = "source.ip"
          pattern = "^(10|192\\.168)\\."
        }
      }
      conditions {
        type_id = "key_exists"
        config {
          key = "error.code"
        }
      }
      conditions {
        type_id = "is_empty"
        config {
          key               = "user.id"
          null              = true
          whitespace_string = true
        }
      }
    }
  }
}
```

### Schema drift detection on an edge

```terraform
# Schema drift detection is per edge and off unless the block is present.
# Declare it on every edge where detection should stay on; omitting the block
# on a later apply turns detection off and discards the learned schema.
resource "monad_pipeline" "monitored" {
  name = "CloudTrail (schema-monitored)"

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
  }
  nodes {
    slug           = "siem"
    component_type = "output"
    component_id   = monad_output.siem.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "siem"

    condition {
      operator = "always"
    }

    schema_detection_spec {
      enabled = true
      # Learn and report drift, but do not raise alerts for this edge.
      disable_alerting = true
    }
  }
}
```

### Create disabled, enable later

```terraform
# Create a pipeline disabled, then flip enabled once secrets are populated and
# a test run looks right. enabled is optional; omitting it adopts the server
# default (enabled) rather than forcing a value.
resource "monad_pipeline" "staged" {
  name    = "CloudTrail (staged)"
  enabled = false

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.archive.id
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

### A longer create budget

```terraform
# Pipeline creation is serialized on the API side, so a large apply can push
# individual creates past the provider's request_timeout. Give this resource
# its own budget rather than raising the provider-wide one.
resource "monad_pipeline" "big_fanout" {
  name = "CloudTrail fan-out"

  timeouts {
    create = "10m"
    update = "10m"
  }

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
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

The following arguments are required:

* `name` - (Required) Display name of the pipeline. Also the name alerts and the Monad UI use for it.

The following arguments are optional:

* `description` - (Optional) Free-text description.
* `enabled` - (Optional) Whether the pipeline processes data. Omitted, the pipeline adopts the server default (`true`) and later out-of-band changes are adopted rather than reverted; set it explicitly to have Terraform enforce a value. Toggling it is an in-place update.
* `nodes` - (Optional) Set of `nodes` blocks, one per component in the graph. [See below](#nodes-block).
* `edges` - (Optional) Set of `edges` blocks, one per connection between nodes. [See below](#edges-block).
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

### `nodes` Block

Each `nodes` block places one component in the pipeline and supports the following:

* `component_type` - (Required) One of `input`, `transform`, `enrichment`, `output`.
* `component_id` - (Required) ID of the component: `monad_input.x.id`, `monad_transform.x.id`, `monad_enrichment.x.id` or `monad_output.x.id`. The referenced resource's type must match `component_type`.
* `slug` - (Optional) Identifier for this node within the pipeline; edges refer to nodes by slug. Set it explicitly — an omitted slug is generated by the server, and edges cannot name a slug that is not known until apply.

A pipeline has exactly one `input` node, and every other node needs exactly one incoming edge.

### `edges` Block

Each `edges` block connects two nodes and supports the following:

* `from_node_instance_slug` - (Required) Slug of the source node.
* `to_node_instance_slug` - (Required) Slug of the destination node.
* `name` - (Optional) Display name for the edge, shown in the pipeline editor.
* `description` - (Optional) Free-text description.
* `condition` - (Optional) Which records pass along this edge. [See below](#edgescondition-block). Every edge carries a condition in Monad; declare `condition { operator = "always" }` to pass every record.
* `schema_detection_spec` - (Optional) Schema drift detection for this edge. [See below](#edgesschema_detection_spec-block). Omitting the block means detection is **off**.

#### `edges.condition` Block

The root of a condition is always a **logical** operator that combines zero or more **leaf** conditions. A single leaf still needs a wrapping operator (`and` with one child is the idiom).

* `operator` - (Required) How the child `conditions` combine. One of `always` (pass every record), `never`, `and`, `or`, `nor` (pass when no child matches), `xor`.
* `conditions` - (Optional) List of leaf conditions, evaluated by `operator`. [See below](#edgesconditionconditions-block). Omit for `always` and `never`.

#### `edges.condition.conditions` Block

Each leaf tests one field of the record.

* `type_id` - (Optional) The rule to evaluate. One of `equals`, `equals_any`, `contains`, `starts_with`, `ends_with`, `greater_than`, `less_than`, `matches_regex`, `key_exists`, `is_empty`, `sample`. A leaf without a `type_id` fails validation. An unrecognized rule is sent as written with a warning, so a rule the API adds later still works.
* `config` - (Optional) The rule's parameters. [See below](#edgesconditionconditionsconfig-block). Which fields apply depends on `type_id`; the provider rejects a missing required field or a field the rule does not read at plan time, rather than letting the API accept a leaf that silently routes nothing.

#### `edges.condition.conditions.config` Block

* `key` - (Optional) Path of the field to test, in [GJSON dot-path syntax](https://app.monad.com/docs/conditionals) (`user.email`, `items.0.id`). `*` tests every key. Required by every rule except `sample`, where it is optional and switches on hash-based sampling of that field's value.
* `value` - (Optional) Single value to compare against. Required by `equals`, `contains`, `starts_with`, `ends_with`, `greater_than`, `less_than`. Always a string in HCL: numeric rules parse it (`"499"`), and the value accepts JSON syntax (quoted strings, bare words, numbers, booleans).
* `values` - (Optional) List of strings to match any of. Required by `equals_any`.
* `pattern` - (Optional) Regular expression. Required by `matches_regex`.
* `percent` - (Optional) Percentage of records to pass, `0`–`100` (e.g. `12.5`). Required by `sample`.
* `not` - (Optional) Negate the leaf's result. Accepted by every rule except `sample`.
* `case_insensitive` - (Optional) Compare strings case-insensitively. Accepted by `equals`, `equals_any`, `contains`, `starts_with`, `ends_with`.
* `raw` - (Optional) For `contains`: treat the field as a raw string and substring-match it. When `false`, arrays and objects are checked for an exact element match.
* `null` - (Optional) For `is_empty`: also treat an explicit JSON `null` as empty.
* `whitespace_string` - (Optional) For `is_empty`: also treat a whitespace-only string as empty.
* `rate` - (Optional, **Deprecated**) Rate for the legacy `sample_rate` rule (`"100ms"`, `"1s"`). Use `sample` with `percent`.

The fields each rule reads:

| `type_id` | Required | Optional |
|-----------|----------|----------|
| `equals` | `key`, `value` | `case_insensitive`, `not` |
| `equals_any` | `key`, `values` | `case_insensitive`, `not` |
| `contains` | `key`, `value` | `case_insensitive`, `raw`, `not` |
| `starts_with` | `key`, `value` | `case_insensitive`, `not` |
| `ends_with` | `key`, `value` | `case_insensitive`, `not` |
| `greater_than` | `key`, `value` | `not` |
| `less_than` | `key`, `value` | `not` |
| `matches_regex` | `key`, `pattern` | `not` |
| `key_exists` | `key` | `not` |
| `is_empty` | `key` | `null`, `whitespace_string`, `not` |
| `sample` | `percent` | `key` |

#### `edges.schema_detection_spec` Block

Schema drift detection learns the shape of records crossing an edge (about 48 hours to graduate) and then raises an alert when the shape changes. It is configured per edge and requires the schema drift detection feature on the organization.

* `enabled` - (Optional) Learn the record schema on this edge and detect drift. Omitted or `false` is off; omit rather than writing `false`.
* `disable_alerting` - (Optional) Keep detecting drift but raise no alerts for this edge. Omitted or `false` alerts normally; omit rather than writing `false`.

The API rebuilds every edge from the request on each pipeline save, so an edge whose block is absent is saved with detection off, and disabling detection discards the learned schema. Declare the block on every edge where detection should stay on. Detection switched on outside Terraform appears in the next plan as the block being removed; add it to the configuration to keep it.

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the pipeline. Reference it from `monad_alert_rule.pipeline_ids`, and use it to build the push-ingest host for HTTP inputs (`https://<id>.data.monad.com`).

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | |
| `description` | Input | |
| `enabled` | Input and output | Optional; adopts the server value when omitted, so out-of-band toggles are adopted, not reverted |
| `nodes` | Input | Set; server-generated slugs and node-instance IDs are not surfaced |
| `edges` | Input | Set; refreshed from the API on read |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `id` | Output | |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

Each operation runs under its own deadline. A value here may exceed the provider default, so one slow pipeline can be given more time without raising the budget for every call.

A pipeline create that outlives its `create` timeout is not simply reported as failed. Pipeline creation is serialized on the API side, so a burst of concurrent creates (Terraform's default `-parallelism=10`) can push the tail past the budget while the API finishes the create anyway. The provider then polls the organization's pipelines for up to two minutes for a same-named pipeline created since the request started: exactly one match is adopted into state with a warning, none is reported as "not created, safe to retry", and several are listed with a request to `terraform import` the right one rather than guess.

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import pipelines using the pipeline `id`. For example:

```terraform
import {
  to = monad_pipeline.example
  id = "7a9d2e5f-3c1b-4f8e-9d6a-0b1c2d3e4f5a"
}
```

Using `terraform import`, import pipelines using the pipeline `id`. For example:

```shell
# Import a pipeline by its ID. Nodes and edges are sets, so the first plan after
# import is clean regardless of the order they appear in your configuration.
terraform import monad_pipeline.example 7a9d2e5f-3c1b-4f8e-9d6a-0b1c2d3e4f5a
```

Read populates `nodes`, `edges` and `enabled` from the API, so `terraform plan -generate-config-out` produces a complete configuration and the first plan after import is clean. A `nodes` or `edges` diff after import means a set element genuinely differs from the configuration, not a cosmetic ordering artifact.
