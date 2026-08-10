# Changelog

All notable changes to this provider are documented here. This project adheres
to [Semantic Versioning](https://semver.org/). While the provider is pre-1.0,
breaking changes are released as minor version bumps.

## Unreleased

Contains a breaking change (edge condition `config.value`) — see below. It fixes
conditional edge routing, which did not work through Terraform for any rule that
compares a value, and modernizes the generated SDK the provider is built on.

### Fixed

- **Edge conditions that compare values now work.** The provider serialized every
  condition leaf as `{key, value, rate}` regardless of `type_id`, with `value`
  always a JSON array. The API's rules expect different shapes, so 9 of the 11
  rules routed **zero records** — and nothing errored at plan, at apply, or at
  runtime. On a routing fork that is data loss with a green check. Only
  `key_exists` and `is_empty` worked, because they read `key` alone. (ENG-9546)

  Each leaf is now serialized from the API's rule catalogue: only the fields that
  rule reads, and only when set.

### Added

- **The full condition rule vocabulary** on `edges.condition.conditions.config`:
  `values` (for `equals_any`), `pattern` (`matches_regex`), `percent` (`sample`),
  and the modifier flags `not` (every rule except `sample`), `case_insensitive`
  (the five string rules), `raw` (`contains`), and `null` / `whitespace_string`
  (`is_empty`). Previously none of these could be expressed at all.
- **Plan-time validation of each condition leaf.** A leaf missing a field its
  rule requires, or setting one the rule ignores, is now an error at `plan` with
  the offending block's path — instead of a pipeline that applies cleanly and
  silently routes nothing. An unrecognized `type_id` warns rather than errors, so
  a rule newly shipped by the API does not break an existing configuration.
- **Plan-time validation of the pipeline graph**, mirroring the checks the API
  runs: duplicate node slugs, an edge naming a slug no node declares, a node with
  more than one incoming edge, a pipeline without exactly one input root, an
  outgoing edge from an output, a middle node that is not a transform or
  enrichment, a non-output node that leads nowhere, and cycles. These previously
  surfaced as a `400` part-way through an apply, after other components had
  already been created.

### Changed (BREAKING)

- **`edges.condition.conditions.config.value` is now a string, not a list of
  strings.**

  **Migration:**
  - Single-value rules (`equals`, `contains`, `starts_with`, `ends_with`,
    `greater_than`, `less_than`) take a scalar: `value = ["hot"]` → `value = "hot"`.
    Numeric rules accept a numeric string, e.g. `value = "100"`.
  - Matching several values is `equals_any` with the new attribute:
    `value = ["hot", "warm"]` → `type_id = "equals_any"`, `values = ["hot", "warm"]`.
  - State migrates automatically (schema version 0 → 1): the old `value` list is
    moved to `values` and `value` is left null. Configuration must still be
    updated by hand, and the first plan will show that as a diff.

  Every configuration this breaks is one that was already silently routing
  nothing, so the failure surfaces as an error where it used to be invisible.

### Changed

- **Modernized the generated Monad Go SDK pin** from `v0.0.0-20250711173942`
  (2025-07-11) to `v0.0.0-20260710180932` (2026-07-10). The old pin predated
  the API's operation-id sweep, so every SDK call site used a path-derived name
  (`V2OrganizationIdPipelinesPost`) that no longer exists. Call sites now use
  the operation-id names (`CreatePipeline`, `GetPipelineConfig`,
  `ReplaceInput`, …). Every rename was checked against the new SDK's path and
  HTTP verb so each call still targets the same endpoint and API version it did
  before.
- **Outputs now send the canonical `type` field.** `RoutesV2CreateOutputRequest`
  / `RoutesV2PutOutputRequest` renamed `output_type` to `type`. The API still
  accepts `output_type` as a deprecated alias, so this is not a behavior change
  today, but the provider now sends the documented field rather than the
  transitional one.
- **Pipeline Read now calls `GetPipelineConfig`.** In the new SDK, `GetPipeline`
  is the v1 endpoint and returns pipeline metadata with no nodes or edges;
  `GetPipelineConfig` is the v2 endpoint the provider was already calling. The
  URL and response shape are unchanged from before this bump.
- **Edge conditions use the SDK's recursive `ModelsConditionEvaluatable`**,
  which replaced the two-level `ModelsPipelineEdgeConditions` /
  `ModelsPipelineEdgeCondition` pair. The emitted JSON is unchanged
  (`operator`, `conditions[]`, `type_id`, `config`).

### Deprecated

- **`config.rate` / the `sample_rate` rule.** It predates `sample`, is not in the
  API's published rule catalogue, and is not surfaced in the Monad UI. It still
  works and is still sent; use `type_id = "sample"` with `percent` instead.

### Known issues

- The SDK pin is deliberately **not** the SDK's `main`. As of the 2026-08-07
  regeneration, the connector settings `oneOf` lost its free-form
  `MapmapOfStringAny` variant and enumerates only concrete per-connector types,
  which would make the provider unable to send arbitrary `config.settings` —
  the mechanism the Dynamic `settings`/`config` attributes depend on. The
  2026-07-10 pin is the newest commit that keeps that variant. (ENG-9585)

## 0.3.1

No breaking changes. A documentation-only patch: corrects resource schema
descriptions that were wrong from copy-paste, and regenerates the registry
resource pages so their titles use the canonical provider name.

### Fixed

- **`monad_transform` was described as "Monad Secret".** The resource's
  `MarkdownDescription` had been copy-pasted from `monad_secret`, so both the
  provider schema and the published registry page for `monad_transform`
  announced it as a secret. It now reads "Monad Transform".
- **`id` on `monad_input` / `monad_output` was described as "Monad
  ConnectorIdentifier".** Corrected to "Connector identifier".

### Changed

- Registry resource pages regenerated so `page_title` uses the canonical
  provider name, and the `id` attribute descriptions above are reflected in
  `docs/resources/`.

No provider behaviour changes in this release — schema descriptions and
generated docs only. Practitioners upgrading from 0.3.0 need no configuration
changes and will see no plan diff.

## 0.3.0

No breaking changes. Resolves the `monad_pipeline` import issue listed under
0.2.0's known issues, and makes the provider recover on its own from resources
deleted outside Terraform.

### Fixed

- **Out-of-band deletion no longer wedges `plan`/`apply`.** Every resource's
  `Read` (`monad_pipeline`, `monad_input`, `monad_output`, `monad_transform`,
  `monad_enrichment`, `monad_secret`) previously treated a failed lookup as a
  fatal `Client Error`, so a resource deleted outside Terraform left the
  configuration permanently stuck on refresh until a manual
  `terraform state rm`. Read now drops the resource from state when the API
  reports it is gone, and the next plan recreates it. (ENG-9259)

  Not-found is detected from either a real `404`/`410` **or** the legacy
  Monad API response of `500` with the body
  `An item of this type does not exist.` Both are accepted, so self-heal works
  against instances that predate the API's 404 fix as well as current ones.
  (ENG-9258)
- **`monad_pipeline` import: clean first plan.** After `terraform import`, the
  first `plan` no longer reports a spurious change from reordered `nodes`/
  `edges` or from `enabled`. Read cannot see the practitioner's HCL on import,
  so it could not reconstruct the authored ordering. `nodes` and `edges` now
  use order-insensitive plan modifiers that keep the prior state when the
  configured set matches in a different order — a genuine add, remove or edit
  still diffs. (ENG-9221)
- **`monad_transform` import: clean first plan.** After `terraform import`, the
  first `plan` no longer adds `+ description = ""` to every operation in the
  Dynamic `config`. The API omits empty-string operation fields that the HCL
  carries explicitly, and Read adopted the API value verbatim when prior state
  was null. The `config` attribute now keeps the state value when it is
  semantically equal to the configuration, using the same `pruneEmpty`-based
  comparison Read already applies. (ENG-9263)

### Changed

- **`monad_pipeline.enabled` is now `Optional + Computed`** (with
  `UseStateForUnknown`) and is populated from the API in Create, Update and
  Read. This replaces the previous null-preservation reconcile, which could not
  cover import because prior state is null there.

  No configuration change is required. The practical difference is that when
  `enabled` is omitted from the configuration the provider now adopts the
  server's value instead of treating the attribute as unset — which is what
  makes an imported, otherwise-unchanged pipeline plan clean.

## 0.2.0

Contains a breaking change (write-only `config.secrets`) — see below.

### Fixed

- **All resources: `Provider produced inconsistent result after apply`.**
  Create/Update now treat the API response as authoritative only for the
  computed `id` and preserve every plan-known value (config/settings, nodes,
  edges, name, description). A practitioner's `jsondecode` produces a `tuple`
  whose cty type differed from the API-derived value, tripping Terraform's
  apply-consistency check. (ENG-9129)
- **`monad_output`: `description` null/`""` inconsistency** for description-less
  resources (e.g. a `dev-null` sink).
- **Read no longer causes perpetual diffs.** Read refreshes for drift again but
  reconciles at the comparison layer — settings/config (Dynamic) and pipeline
  nodes/edges are compared to prior state as normalized JSON and the prior
  value is kept when semantically equal, so cty-type churn and server-populated
  fields (generated node slugs, echoed edge name/description, omitted empty
  values) no longer read as drift.
- **`terraform import` restored.** With Read refreshing again, imported
  resources populate correctly instead of landing with null config.

### Added

- **`config.secrets_hash`** (computed) on `monad_input`, `monad_output`, and
  `monad_enrichment` — an HMAC fingerprint of the write-only `secrets` used to
  detect rotation.

### Changed (BREAKING)

- **`config.secrets` is now write-only** (`WriteOnly` + `Sensitive`) on
  `monad_input`, `monad_output`, and `monad_enrichment`. The value is sent to
  the Monad API but **no longer stored in Terraform state**.

  **Migration:**
  - No configuration change is required for the common case. On the first
    `plan`/`apply` after upgrading, the provider nulls the previously-stored
    secret material out of state automatically (the Terraform framework
    enforces that write-only values are null in state). Re-running `apply`
    repopulates `secrets_hash`.
  - Secrets must be supplied as a **structured value**, not a raw string:
    either a new secret `{ value = "...", name = "...", description = "..." }`
    (all three non-empty) or a reference to an existing secret `{ id = "..." }`.
    A bare string now errors at apply.
  - Because secrets are write-only, they are never read back; rotation is
    detected via `secrets_hash`. Changing the configured secret triggers an
    update; an unchanged secret is a no-op.

### Known issues

- **`monad_pipeline` import** populates correctly but the first `plan` after
  import may show a one-time diff (edge ordering and `enabled`) because Read
  cannot see the practitioner's HCL config on import. A single `apply`
  normalizes it; stable thereafter. (ENG-9221)
