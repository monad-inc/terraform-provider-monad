---
page_title: "monad_alert_rule_type Data Source - monad"
subcategory: ""
description: |-
  Describes the configuration schema of a alert rule type.
---

# Data Source: monad_alert_rule_type

Describes an alert rule type: the `rule_config.settings` fields a `monad_alert_rule` of that type takes, including each variant of a `one_of` field such as a threshold alert's `metric_config`. The schema comes from the Monad API, so it is always the one the API will validate against.

## Example Usage

```terraform
data "monad_alert_rule_type" "threshold" {
  type_id = "threshold-alert"
}

# The allowed time windows and operators.
output "threshold_choices" {
  value = {
    for f in data.monad_alert_rule_type.threshold.settings : f.path => f.allowed_values
    if length(f.allowed_values) > 0
  }
}
```

## Argument Reference

* `type_id` - (Required) The alert rule type to describe, such as `threshold-alert`. An unknown ID fails with an error; the API reports most unknown type IDs as a server error, so check the spelling first.

## Attribute Reference

* `name` - Display name.
* `description` - Description.
* `vendor` - Vendor (the API's `house`).
* `category` - Category.
* `granularity` - What the rule evaluates: `pipeline`, `organization` or `billing_account`.
* `settings` - List of the `settings` fields, flattened and sorted by path. [See below](#field-elements).
* `schema_json` - The full schema as the API returns it, JSON-encoded, for anything the lists do not express.

### Field elements

Each element of a field list has:

* `path` - Dotted path. A nested field is `parent.child`. A `one_of` field's variants nest under the variant name (`metric_config.records.threshold`), matching how the configuration writes them, and its discriminator appears as `parent.<field>` with the variant names as `allowed_values`.
* `name` - Display name.
* `type` - Field type, such as `string`, `int`, `bool`, `array`, `object` or `one_of`.
* `item_type` - Element type of an array field, such as `string`. Empty when the API gives none.
* `sub_type` - A refinement of `type` that changes how the field is handled. Values the API uses include `monad_secret` (an object that holds a secret reference), `cron` (a cron schedule string), `rate_limit`, `url`, `duration` and `json`. Empty when the API gives none. Unlike `item_type`, it does not describe array elements.
* `required` - Whether the field must be set.
* `immutable` - Whether the field cannot change after creation.
* `default` - Default value, JSON-encoded (use `jsondecode`); null when there is none.
* `description` - What the field does.
* `allowed_values` - Allowed values for an enum field; empty otherwise.
