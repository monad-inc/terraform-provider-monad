---
page_title: "monad_sso_connection Resource - monad"
subcategory: ""
description: |-
  Manages a SAML single sign-on connection for the organization.
---

# Resource: monad_sso_connection

An SSO connection lets users sign in to Monad through your SAML identity provider (IdP), such as Okta or Microsoft Entra ID. Monad reads the IdP's sign-in URL and signing certificate from its SAML metadata URL, so the connection holds no secrets, and nothing sensitive is stored in Terraform state.

Users whose email address is in one of the connection's `email_domains` are sent to the IdP when they sign in. In the IdP, configure the application with `saml.entity_id` as its audience (service provider entity ID).

## Example Usage

### Create a connection

```terraform
resource "monad_sso_connection" "okta" {
  name          = "okta"
  description   = "Corporate Okta"
  email_domains = ["example.com"]
  public_name   = "Example Corp SSO"

  saml = {
    # Enter this value in the identity provider as the audience (SP entity ID).
    entity_id    = "0b6f1c3e-2d4a-4e8b-9f10-3c5d7e9a1b2c"
    metadata_url = "https://example.okta.com/app/exk1a2b3c4d5e6f7g8h9/sso/saml/metadata"
  }
}
```

### Generate the entity ID

```terraform
# random_uuid keeps the same value for the life of the configuration, so the
# identity provider's audience setting never has to change.
resource "random_uuid" "sso_entity_id" {}

resource "monad_sso_connection" "okta" {
  name          = "okta"
  email_domains = ["example.com"]

  saml = {
    entity_id    = random_uuid.sso_entity_id.result
    metadata_url = var.okta_metadata_url
  }
}

output "sso_entity_id" {
  value = monad_sso_connection.okta.saml.entity_id
}
```

### Protect the connection from deletion

~> **Warning:** Deleting an SSO connection also deletes every user role, auth provider and session bound to it, which locks those users out of the organization. We recommend setting `prevent_destroy` on every `monad_sso_connection`.

The provider never replaces a connection: a change to `email_domains` or `saml.entity_id` fails the plan instead. `prevent_destroy` covers the remaining paths to a delete, `terraform destroy` and removing the resource from configuration, by making Terraform refuse the plan. To delete a connection on purpose, remove the `lifecycle` block first.

```terraform
# Deleting an SSO connection deletes the users, roles and sessions bound to
# it. prevent_destroy makes Terraform refuse any plan that would delete it,
# including terraform destroy and removing the resource from configuration.
resource "monad_sso_connection" "okta" {
  name          = "okta"
  email_domains = ["example.com"]

  saml = {
    entity_id    = "0b6f1c3e-2d4a-4e8b-9f10-3c5d7e9a1b2c"
    metadata_url = "https://example.okta.com/app/exk1a2b3c4d5e6f7g8h9/sso/saml/metadata"
  }

  lifecycle {
    prevent_destroy = true
  }
}
```

## Argument Reference

The following arguments are required:

* `name` - (Required) Name of the connection.
* `saml` - (Required) SAML settings. [See below](#saml-block).

The following arguments are optional:

* `description` - (Optional) Free-text description. Removing it clears the description on the server.
* `email_domains` - (Optional) Email domains whose users sign in through this connection, such as `example.com`. Write each in lowercase, without `@` or a scheme, the form the API stores; other forms fail at plan time. Public email providers are rejected by the API. The API cannot change domains on an existing connection, so a change fails the plan instead of replacing the connection, which would delete the users bound to it. Set the domains you need when you create the connection.
* `public_name` - (Optional) Label shown to users in the SSO sign-in picker. When omitted, the API generates one (`sso-` followed by hex characters). It cannot be empty. Once set it can be changed but not cleared; removing it from configuration keeps the current value.
* `session_timeout_minutes` - (Optional) Session length, in minutes, for sign-ins through this connection: 30 to 1440. When omitted, the platform default applies. Once set it can be changed but not cleared; removing it from configuration keeps the current value.
* `timeouts` - (Optional) Operation timeouts for this resource. [See below](#timeouts).

### `saml` Block

* `entity_id` - (Required) Service provider entity ID: a UUID that the IdP sends as the audience of its assertions. It must be unique across Monad. The API cannot change it on an existing connection, so a change fails the plan instead of replacing the connection. Use a fixed value or a `random_uuid` resource, which keeps its value for the life of the configuration.
* `metadata_url` - (Required) HTTPS URL of the IdP's SAML metadata. Monad fetches and validates it when the connection is created and whenever it changes, so it must be reachable from the internet. It can be changed in place.

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `id` - UUID of the connection.
* `created_at` - When the object was created, as an RFC 3339 timestamp set by the API.
* `updated_at` - When the object was last changed. Shown as `(known after apply)` whenever the resource has other changes, because every write moves it.

### Inputs and outputs

| Attribute | Direction | Notes |
|-----------|-----------|-------|
| `name` | Input | |
| `description` | Input | |
| `email_domains` | Input | Set at create; a change fails the plan |
| `public_name` | Input / Output | Generated when omitted; cannot be cleared |
| `session_timeout_minutes` | Input / Output | Platform default when omitted; cannot be cleared |
| `saml.entity_id` | Input | Set at create; a change fails the plan |
| `saml.metadata_url` | Input | Validated by the API on create and change |
| `timeouts` | Input | Provider-side deadlines; never sent to the API |
| `created_at` | Output | Set by the API |
| `updated_at` | Output | Changes on every write |
| `id` | Output | |

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default: the provider's `request_timeout`, `5m` unless set)
* `read` - (Default: the provider's `request_timeout`, `5m` unless set)
* `update` - (Default: the provider's `request_timeout`, `5m` unless set)
* `delete` - (Default: the provider's `request_timeout`, `5m` unless set)

Each operation runs under its own deadline. A value here may exceed the provider default, so one slow call can be given more time without raising the budget for every call.

A create that outlives its `create` timeout is not simply reported as failed, because the API can finish the create after the provider stops waiting. The provider then polls the organization's connections for up to two minutes for one with the same `saml.entity_id` created since the request started: a match is adopted into state with a warning, and none is reported as "not created, safe to retry".

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import SSO connections using the connection `id`. For example:

```terraform
import {
  to = monad_sso_connection.okta
  id = "5e2a9c1d-7b3f-4a60-8d2e-1f4c6b8a0d93"
}
```

Using `terraform import`, import SSO connections using the connection `id`. For example:

```shell
# Import an SSO connection by its ID.
terraform import monad_sso_connection.okta 5e2a9c1d-7b3f-4a60-8d2e-1f4c6b8a0d93
```
