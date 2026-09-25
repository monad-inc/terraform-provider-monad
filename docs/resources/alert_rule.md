---
page_title: "monad_alert_rule Resource - monad"
subcategory: ""
description: |-
  Manages an alert rule: a condition evaluated against pipeline metrics, status, logs or billing that raises an alert when it is met.
---

# Resource: monad_alert_rule

An alert rule watches your pipelines and fires when a condition holds: records or bytes above or below a threshold, an error rate over a percentage, a pipeline stuck in an erroring state, a log line matching a pattern, or organization spend past a budget. Rules are evaluated every five minutes (log alerts fire in real time) and raise `FIRING` and, fifteen minutes after the condition clears, `RESOLVED` events. Those events are themselves a data source: a `monad_input` of type `monad-alerts` consumes them, so a pipeline can route them to Slack, PagerDuty or a SIEM. See [Alerts](https://app.monad.com/docs/alerts) in the Monad docs.

Each rule has a **type**, a **severity**, the **pipelines** it watches, and a type-specific **rule_config**. Managing rules here keeps the alerting that watches a pipeline in the same configuration as the pipeline, so a rebuild recreates both.

## Example Usage

### Threshold on record volume

```terraform
# Fire when a pipeline ingests more than 10,000 records in five minutes.
# rule_config is free-form: each alert type has its own settings schema, which
# the API validates on write. Write it as a plain HCL object.
resource "monad_alert_rule" "ingest_spike" {
  name        = "CloudTrail — ingest volume spike"
  description = "More than 10,000 records ingested in a 5-minute window"
  type        = "threshold-alert"
  severity    = "critical"

  pipeline_ids = [monad_pipeline.basic.id]

  rule_config = {
    settings = {
      metric_config = {
        type = "records"
        records = {
          direction = "ingress"
          threshold = 10000
        }
      }
      operator    = "greater_than"
      time_window = "5m"
    }
  }
}
```

### Threshold on bytes, watching for a stall

```terraform
# The bytes metric adds a unit. Watching egress fall BELOW a floor catches a
# destination that has gone quiet.
resource "monad_alert_rule" "egress_stalled" {
  name     = "SIEM egress stalled"
  type     = "threshold-alert"
  severity = "high"

  pipeline_ids = [monad_pipeline.routed.id]

  rule_config = {
    settings = {
      metric_config = {
        type = "bytes"
        bytes = {
          direction = "egress"
          threshold = 1
          unit      = "MB"
        }
      }
      operator    = "less_than"
      time_window = "1h"
    }
  }
}
```

### Sustained pipeline status, across every pipeline

```terraform
# Sustained Erroring or Throttled status. An empty pipeline_ids watches every
# pipeline in the organization.
resource "monad_alert_rule" "erroring" {
  name     = "Pipeline erroring for 5 minutes"
  type     = "pipeline-status-alert"
  severity = "high"
  active   = true

  rule_config = {
    settings = {
      status      = "Erroring"
      time_window = "5m"
    }
  }
}
```

### Error rate

```terraform
# Error rate as a percentage of ingested records, with a floor on record count
# so quiet pipelines do not false-alarm.
resource "monad_alert_rule" "error_rate" {
  name     = "Error rate above 5%"
  type     = "error-rate-alert"
  severity = "medium"

  pipeline_ids = [monad_pipeline.basic.id, monad_pipeline.routed.id]

  rule_config = {
    settings = {
      threshold   = 5.0
      time_window = "1h"
      min_records = 100
    }
  }
}
```

### Log pattern

```terraform
# Real-time match on pipeline log events. At least one of levels or
# message_filter is required; operator and value must be set together.
resource "monad_alert_rule" "schema_mismatch_logged" {
  name     = "Schema mismatch logged"
  type     = "monad-log-alert"
  severity = "medium"

  rule_config = {
    settings = {
      log_type = "pipeline"
      levels   = ["error", "fatal"]
      message_filter = {
        operator = "contains"
        value    = "schema mismatch"
      }
      dedupe_window = "30m"
    }
  }
}
```

### Organization spend

```terraform
# Organization-level alert types take no pipeline_ids.
resource "monad_alert_rule" "budget" {
  name     = "Monthly spend above $5,000"
  type     = "billing-metrics-cost-budget"
  severity = "high"

  rule_config = {
    settings = {
      usd_amount = 5000
    }
  }
}
```

## Argument Reference

The following arguments are required:

* `name` - (Required) Display name of the rule. Carried on every alert it raises.
* `type` - (Required, Forces new resource) The alert type. One of `threshold-alert`, `error-rate-alert`, `pipeline-status-alert`, `volume-anomaly-alert`, `monad-log-alert`, `billing-metrics-cost-budget`. Immutable on the API, so changing it destroys and recreates the rule. (`schema-detection-alert` rules are created by the platform from `schema_detection_spec` on pipeline edges and cannot be declared here.)
* `severity` - (Required) One of `critical`, `high`, `medium`, `low`, `info`.
* `rule_config` - (Required) Type-specific configuration. [See below](#rule_config-argument).

The following arguments are optional:

* `description` - (Optional) Free-text description.
* `active` - (Optional) Whether the rule is evaluated. Defaults to `true`; an inactive rule keeps its configuration but never fires.
* `pipeline_ids` - (Optional) Set of pipeline IDs the rule watches, usually `monad_pipeline.x.id`. Omitted or empty, a pipeline-scoped rule watches **every** pipeline in the organization. Organization-level types (`billing-metrics-cost-budget`) ignore it.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

### `rule_config` Argument

`rule_config` is a free-form value with a single `settings` object whose fields depend on `type`. The API validates it on write, so a mistake surfaces at apply rather than silently. Write it as a plain HCL object; the `jsondecode(jsonencode({ ... }))` wrapper seen in older examples is a no-op for a literal. The per-type `settings` are:

#### `threshold-alert`

Fires when a metric crosses a threshold over a window. See [Pipeline Threshold Alert](https://app.monad.com/docs/alerts/threshold-alert).

* `metric_config` - (Required) Which metric to watch:
    * `type` - (Required) `bytes`, `records` or `errors`.
    * `bytes` - (Required when `type = "bytes"`) Object with `direction` (`ingress` or `egress`), `threshold` (integer ≥ 0) and `unit` (`KB`, `MB`, `GB`).
    * `records` - (Required when `type = "records"`) Object with `direction` (`ingress` or `egress`) and `threshold` (integer ≥ 0).
    * `errors` - (Required when `type = "errors"`) Object with `threshold` (integer ≥ 0).
* `operator` - (Required) `greater_than` or `less_than`.
* `time_window` - (Required) One of `5m`, `1h`, `6h`, `24h`.

#### `error-rate-alert`

Fires when errors as a percentage of ingested records exceed a threshold. See [Pipeline Error Rate Alert](https://app.monad.com/docs/alerts/error-rate-alert).

* `threshold` - (Required) Percentage, `0`–`100` (`5.0` is five percent).
* `time_window` - (Required) One of `5m`, `1h`, `6h`, `24h`.
* `min_records` - (Optional) Minimum ingested records in the window before the rate is evaluated. Defaults to `100`.

#### `pipeline-status-alert`

Fires when a pipeline holds a status for the whole window. See [Pipeline Status Alert](https://app.monad.com/docs/alerts/pipeline-status-alert).

* `status` - (Required) `Erroring` or `Throttled`.
* `time_window` - (Required) One of `5m`, `1h`, `6h`, `24h`. The status must be sustained for the entire window; brief flickers do not fire.

#### `volume-anomaly-alert`

Fires when a metric falls outside its historical interquartile range. Needs four weeks of history per pipeline before it evaluates. See [Volume Anomaly Detection Alert](https://app.monad.com/docs/alerts/volume-anomaly-alert).

* `metric` - (Required) `ingress_bytes` or `egress_bytes`.
* `iqr_k_factor` - (Required) Sensitivity multiplier: `1.5` for outliers, `2.5` for far outliers, `3.0` for extreme outliers only.

#### `monad-log-alert`

Fires in real time when a pipeline log event matches every configured filter. At least one of `levels` or `message_filter` is required. See [Monad Organization Logs Alert](https://app.monad.com/docs/alerts/monad-log-alert).

* `log_type` - (Required) Currently only `pipeline`.
* `levels` - (Optional) List of log levels to match: `debug`, `info`, `warn`, `error`, `fatal`.
* `message_filter` - (Optional) Object with `operator` (`contains`, `starts_with`, `ends_with`, `matches_regex`) and `value`; both must be set together.
* `dedupe_window` - (Optional) Minimum time between repeat alerts for the same pipeline node: `5m`, `30m` or `1h`. Defaults to `1h`.

#### `billing-metrics-cost-budget`

Fires when the organization's current invoice exceeds a budget. Organization-level: `pipeline_ids` does not apply. See [Billing Metrics Cost Budget Alert](https://app.monad.com/docs/alerts/billing-metrics-cost-budget).

* `usd_amount` - (Required) Budget threshold in USD, greater than `0`.

The API injects a `billing_account_id` into this type's stored settings; the provider masks it, so it never shows as drift.

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the alert rule. Alert payloads carry it as `rule_id`.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | |
| `description` | Input | |
| `type` | Input | Changing it replaces the resource |
| `severity` | Input | |
| `active` | Input and output | Optional; defaults to `true` and adopts the server value on import |
| `pipeline_ids` | Input | Set; omitted and empty are equivalent |
| `rule_config` | Input | Refreshed from the API on read; compared semantically, server-injected keys masked |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `id` | Output | |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

Each operation runs under its own deadline. A value here may exceed the provider default, so one slow alert rule can be given more time without raising the budget for every call.

A create that outlives its `create` timeout is not simply reported as failed, because the API can finish the create after the provider stops waiting. The provider then polls the organization's alert rules for up to two minutes for an alert rule with the same `name` and `type` created since the request started: exactly one match is adopted into state with a warning, none is reported as "not created, safe to retry", and several are listed by `id` with a request to [import](#import) the right one rather than guess.

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import alert rules using the rule `id`. For example:

```terraform
import {
  to = monad_alert_rule.example
  id = "c387bece-6a1d-4f2e-8b3c-9d0e1f2a3b4c"
}
```

Using `terraform import`, import alert rules using the rule `id`. For example:

```shell
# Import an alert rule by its ID (shown in the Monad UI and returned by the API).
terraform import monad_alert_rule.example c387bece-6a1d-4f2e-8b3c-9d0e1f2a3b4c
```

Read populates every argument from the API, including `rule_config`, so the first plan after import is clean when the configuration matches.
