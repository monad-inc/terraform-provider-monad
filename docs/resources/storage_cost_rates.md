---
page_title: "monad_storage_cost_rates Resource - monad"
subcategory: ""
description: |-
  Manages an organization's storage cost rates: the $/GB prices the Storage Costs view applies.
---

# Resource: monad_storage_cost_rates

Storage cost rates are the prices, in dollars per GB, that the Monad **Storage Costs** view applies to the data each output writes. Every storage type (output type) can have a default rate, and an individual output can have its own rate, overriding the default. Several `/dev/null` sinks that stand in for different destinations can then be priced separately. See [Storage Costs](https://app.monad.com/docs/guides/storage-costs) in the Monad docs.

There is one set of rates per organization, so this resource is a singleton: declare it once per organization.

Three platform rules shape how it behaves:

* **It manages only what it declares.** The storage types in `costs` and the outputs in `output_costs` are managed; any other rate in the organization is left alone and never shows as drift. A change to a managed rate made outside Terraform does show as drift.
* **A storage type's rate cannot be deleted, only overwritten.** A type with no rate is priced at $0, so removing a type from `costs`, or destroying the resource, sets its rate to `0`, the same cost as never having set it. The type then still appears in the Monad UI with a $0 rate.
* **A per-output rate can be removed.** Removing an output from `output_costs`, or destroying the resource, deletes its rate, and the output goes back to its storage type's default.

## Example Usage

```terraform
# Price storage by output type, and give one SIEM output its own rate.
# Only the types and outputs listed here are managed; rates set elsewhere in
# the organization are left alone.
resource "monad_storage_cost_rates" "this" {
  costs = {
    "dev-null"  = 0
    "kv-lookup" = 0.10
  }

  output_costs = {
    (monad_output.siem.id) = {
      storage_type = "dev-null"
      cost_per_gb  = 2.75
    }
  }
}
```

## Argument Reference

The following arguments are optional:

* `costs` - (Optional) Map of storage type (output type ID, such as `dev-null` or `kv-lookup`) to its default price in $/GB. Applies to every output of that type that has no entry in `output_costs`. Prices are `0`–`1000`, checked at plan time, and stored at single precision by the API.
* `output_costs` - (Optional) Map of output ID to that output's own price. [See below](#output_costs). Keys are usually `monad_output.x.id`.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

### `output_costs`

Each value is an object with:

* `storage_type` - (Required) The output's type ID, such as `dev-null`. Changing it clears the output's old rate before setting the new one.
* `cost_per_gb` - (Required) Price in $/GB, `0`–`1000`.

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - The organization ID.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `costs` | Input | Only the listed types are managed; a removed type is set to `0` |
| `output_costs` | Input | Only the listed outputs are managed; a removed output's rate is deleted |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `id` | Output | The organization ID |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

Setting rates is idempotent, so a create or update that times out converges on the next apply; there is nothing to adopt.

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import the rates using the organization ID, which must be the organization the provider is configured for. For example:

```terraform
import {
  to = monad_storage_cost_rates.this
  id = "6f1d2c3b-4a5e-4f60-8a7b-9c0d1e2f3a4b" # the organization ID
}
```

Using `terraform import`, import the rates using the organization ID. For example:

```shell
# Import the organization's storage cost rates by the organization ID.
# Every rate the organization currently has comes under management.
terraform import monad_storage_cost_rates.this 6f1d2c3b-4a5e-4f60-8a7b-9c0d1e2f3a4b
```

Import brings **every** rate the organization currently has under management, so the generated configuration lists them all; drop the ones you do not want Terraform to own before applying. A dropped type is then set to `0` on that apply, so keep any rate you want to preserve.
