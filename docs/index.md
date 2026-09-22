---
page_title: "Provider: Monad"
description: |-
  Use the Monad provider to manage security data pipelines and their components — inputs, transforms, enrichments, outputs, secrets and alert rules — as code.
---

# Monad Provider

[Monad](https://monad.com) is a security data pipeline platform. A pipeline is a directed graph: **inputs** pull or receive records from a source, optional **transforms** and **enrichments** reshape and contextualize them, and **outputs** deliver them to a SIEM, data lake or other destination. **Edges** connect the nodes and carry conditions that decide which records flow where. **Secrets** hold credentials that components reference by ID, and **alert rules** watch pipelines for thresholds, error rates, status changes and log patterns.

The Monad provider manages every one of those objects through the [Monad API](https://app.monad.com/docs/api). Build components first, wire them into a pipeline, and let Terraform order the creates and destroys through the references between them.

-> **Terraform 1.11 or later is required.** Secret material (`monad_secret.value` and `config.secrets` on connectors) is declared as [write-only](https://developer.hashicorp.com/terraform/language/resources/ephemeral#write-only-arguments): it is sent to the API but never stored in Terraform state. Earlier Terraform releases reject a provider schema that carries write-only arguments.

~> The provider is pre-1.0. Breaking changes ship as **minor** version bumps, so pin to a minor series (`~> 0.4`) and treat each minor upgrade as a reviewed change. The [changelog](https://github.com/monad-inc/terraform-provider-monad/blob/main/CHANGELOG.md) documents a migration path for each one.

## Example Usage

```terraform
terraform {
  # Write-only arguments (config.secrets, monad_secret.value) need Terraform 1.11+.
  required_version = ">= 1.11"

  required_providers {
    monad = {
      source = "monad-inc/monad"
      # Pre-1.0: breaking changes ship as minor bumps, so pin the minor.
      version = "~> 0.4"
    }
  }
}

provider "monad" {
  base_url        = "https://app.monad.com" # the provider appends /api
  api_token       = var.monad_api_token     # or MONAD_API_TOKEN
  organization_id = var.monad_org_id        # or MONAD_ORGANIZATION_ID
}

variable "monad_api_token" {
  type      = string
  sensitive = true
}

variable "monad_org_id" {
  type = string
}
```

A typical configuration declares the components, then a pipeline that references them by ID:

```terraform
resource "monad_input" "source" {
  name = "CloudTrail"
  type = "monad-http"
}

resource "monad_output" "archive" {
  name = "Archive"
  type = "dev-null"
}

resource "monad_pipeline" "archive" {
  name = "CloudTrail archive"

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.source.id
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

## Authentication

The provider authenticates with an **organization API key**, sent in the `x-api-key` header of every request. Create one under **Settings → API Keys** in the Monad UI; the key needs read and write permissions on the resource types you manage. See [API keys](https://app.monad.com/docs/guides/api-keys) in the Monad docs.

Each configuration argument can instead come from an environment variable, which keeps the token out of your configuration files:

| Argument          | Environment variable     |
|-------------------|--------------------------|
| `api_token`       | `MONAD_API_TOKEN`        |
| `organization_id` | `MONAD_ORGANIZATION_ID`  |
| `base_url`        | `MONAD_BASE_URL`         |
| `use_insecure`    | `MONAD_USE_INSECURE`     |
| `request_timeout` | `MONAD_REQUEST_TIMEOUT`  |

An explicit argument takes precedence over its environment variable. `api_token` and `organization_id` are required by one route or the other; the provider fails to configure without them.

API keys are scoped to a single organization. To manage several organizations (for example a parent and its teams), declare one provider block per organization with an [alias](https://developer.hashicorp.com/terraform/language/providers/configuration#alias-multiple-provider-configurations).

## Argument Reference

This provider supports the following arguments:

* `api_token` - (Optional, Sensitive) Organization API key used to authenticate. Falls back to `MONAD_API_TOKEN`. Required by one of the two routes.
* `organization_id` - (Optional) ID of the organization every resource is created in. Falls back to `MONAD_ORGANIZATION_ID`. Required by one of the two routes.
* `base_url` - (Optional) Base URL of the Monad platform, **without** the `/api` suffix — the provider appends it. Falls back to `MONAD_BASE_URL`, then to `https://beta.monad.com`. Set it to `https://app.monad.com` for the production platform, or to your own host for a self-hosted deployment.
* `use_insecure` - (Optional) Skip TLS certificate verification. Falls back to `MONAD_USE_INSECURE=true`. Only for self-hosted deployments with private certificates; never for production.
* `request_timeout` - (Optional) Per-request deadline for calls to the Monad API, as a Go duration (`5m`, `90s`). Falls back to `MONAD_REQUEST_TIMEOUT`, then to `5m`. It applies to every API call that does not carry its own deadline; a resource's `timeouts` block overrides it per operation. Pipeline creation is serialized on the API side and can take over a minute when several pipelines are created concurrently, so a budget that is too short makes Terraform record a create as failed while the API finishes it — see the `monad_pipeline` page for how the provider recovers from that.

## Resources

| Resource | Manages |
|----------|---------|
| [`monad_input`](resources/input) | A source connector that pulls or receives records |
| [`monad_transform`](resources/transform) | An ordered list of operations applied to each record |
| [`monad_enrichment`](resources/enrichment) | A lookup that adds context to each record |
| [`monad_output`](resources/output) | A destination connector |
| [`monad_pipeline`](resources/pipeline) | The graph of nodes and conditional edges that connects them |
| [`monad_secret`](resources/secret) | A credential that components reference by ID |
| [`monad_alert_rule`](resources/alert_rule) | A rule that watches pipelines and raises alerts |

## Connector configuration

`monad_input`, `monad_output` and `monad_enrichment` share one shape: a `type` naming the connector, and a `config` block holding that connector's `settings` and, where it takes credentials, its `secrets`. The settings and secret field names are the connector's API field names; every connector's page in the Monad docs ([inputs](https://app.monad.com/docs/inputs), [outputs](https://app.monad.com/docs/outputs), [enrichments](https://app.monad.com/docs/enrichments)) lists them, and its "API Examples" section shows the JSON to mirror. Write `settings` as a plain HCL object. `jsondecode(jsonencode({ ... }))`, which older examples wrapped around it, is a no-op for a literal; see the `settings` argument on any connector page for when it is and is not useful.

Secrets are never written inline as strings. Every credential is either a **reference** to a `monad_secret`, `{ id = monad_secret.example.id }`, or a **new secret created inline**, `{ name = "...", description = "...", value = "..." }` with all three set. Where the credential lives depends on the connector: most take it under `config.secrets`, while some newer connectors take the same `{ id }` reference inside `settings` (the Slack output's `auth_config` is one example). The connector's docs page is authoritative.
