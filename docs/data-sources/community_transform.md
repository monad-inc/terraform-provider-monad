---
page_title: "monad_community_transform Data Source - monad"
subcategory: ""
description: |-
  Reads a transform from Monad's community transform repository.
---

# Data Source: monad_community_transform

Reads a published transform from Monad's community transform repository: the same catalog the Monad UI offers when adding a transform. Its `config` drops straight into a [`monad_transform`](../resources/transform.md), so a configuration can use a community mapping without copying it.

The repository updates independently of your configuration. When a transform is revised, the next plan shows the change to the `monad_transform` that uses it.

## Example Usage

```terraform
# Use a published community transform as-is.
data "monad_community_transform" "okta_ocsf" {
  id = "ocsf:v1.9.0:okta:okta-systemlog"
}

resource "monad_transform" "okta_ocsf" {
  name        = data.monad_community_transform.okta_ocsf.name
  description = data.monad_community_transform.okta_ocsf.description
  config      = data.monad_community_transform.okta_ocsf.config
}
```

## Argument Reference

* `id` - (Required) The transform's repository path, such as `ocsf:v1.9.0:okta:okta-systemlog` (`<schema>:<version>:<vendor>:<input type>`).

## Attribute Reference

* `name` - Name.
* `description` - Description.
* `author` - Author.
* `contributors` - List of contributors.
* `tags` - List of tags.
* `inputs` - List of input type IDs the transform is written for.
* `config` - The transform configuration (`operations`), ready for `monad_transform.config`.
* `config_json` - The same configuration, JSON-encoded.
