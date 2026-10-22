---
page_title: "monad_input_type Data Source - monad"
subcategory: ""
description: |-
  Describes the configuration schema of a input type.
---

# Data Source: monad_input_type

Describes an input connector type's configuration: every `settings` and `secrets` field a `monad_input` of that type takes, with which are required, their defaults and allowed values. Use it to discover field names, or to check a configuration before apply. The schema comes from the Monad API, so it is always the one the API will validate against.

## Example Usage

```terraform
data "monad_input_type" "demo" {
  type_id = "demo"
}

# Every setting the type requires.
output "demo_required_settings" {
  value = [for f in data.monad_input_type.demo.settings : f.path if f.required]
}
```

## Argument Reference

* `type_id` - (Required) The input type to describe, such as `cloudtrail`. An unknown ID fails with an error; the API reports most unknown type IDs as a server error, so check the spelling first.

## Attribute Reference

* `name` - Display name.
* `description` - Description.
* `vendor` - Vendor (the API's `house`).
* `category` - Category.
* `in_beta` - Whether the type is in beta.
* `settings` - List of the `settings` fields, flattened and sorted by path. [See below](#field-elements).
* `secrets` - List of the `secrets` fields, flattened and sorted by path. [See below](#field-elements).
* `schema_json` - The full schema as the API returns it, JSON-encoded, for anything the lists do not express.

### Field elements

Each element of a field list has:

* `path` - Dotted path. A nested field is `parent.child`. A `one_of` field's variants nest under the variant name (`metric_config.records.threshold`), matching how the configuration writes them, and its discriminator appears as `parent.<field>` with the variant names as `allowed_values`.
* `name` - Display name.
* `type` - Field type, such as `string`, `int`, `bool`, `array`, `object` or `one_of`.
* `item_type` - Element type of an array field.
* `required` - Whether the field must be set.
* `immutable` - Whether the field cannot change after creation.
* `default` - Default value, JSON-encoded (use `jsondecode`); null when there is none.
* `description` - What the field does.
* `allowed_values` - Allowed values for an enum field; empty otherwise.
