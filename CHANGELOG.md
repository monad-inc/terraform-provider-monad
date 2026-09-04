# Changelog

All notable changes to this provider are documented here. This project adheres
to [Semantic Versioning](https://semver.org/). While the provider is pre-1.0,
breaking changes are released as minor version bumps.

## Unreleased

### Fixed

- **`monad_secret` no longer fails every apply after the first with
  `Provider produced inconsistent result after apply` (`.description: was null,
  but now cty.StringVal("")`).** The API echoes an unset description as `""`,
  and Read stored that verbatim, so a secret declared without `description`
  showed a spurious update on the next plan; Update then copied the API's `""`
  back into state, which Terraform rejected. The error aborted the run and
  blocked every resource referencing the secret. Read now maps an API `""` to
  null when the configuration omits `description`, and keeps `""` when the
  configuration says `description = ""` — so the documented workaround stays
  stable and can be removed at leisure. Update keeps the planned
  `name`/`description` instead of the response's. (ENG-9867)

  The same `""`-vs-null reconciliation now applies to `description` on
  `monad_input`, `monad_output`, `monad_enrichment`, `monad_transform` and
  `monad_pipeline`, which mapped `""` to null unconditionally and so would have
  churned on an explicit `description = ""`.

- **Removing `description` from a `monad_secret` now clears it on the server.**
  The API preserves an omitted description on update, and the provider omitted
  it when the attribute was null, so the old text survived and every later plan
  showed the same `-> null` diff. Update now sends an explicit `""`.

- **Changing only `monad_secret.value` now updates the secret.** `value` is
  write-only, so it is null in state and in the plan and could not produce a
  diff on its own — a value-only change planned as `No changes` and Update never
  ran. When Update did run (for a name or description change) it read `value`
  from the plan, where a write-only value is always null, so the API received an
  empty value and kept the old ciphertext while the provider recorded the hash
  of `""`. The resource now has a `ModifyPlan` that fingerprints the configured
  value and, when it differs from the stored `value_hash`, marks the hash unknown
  so Update runs; Update reads `value` from the configuration. `value_hash`
  gained `UseStateForUnknown`, so an unchanged value no longer shows
  `(known after apply)` on unrelated updates. (ENG-9235)

  Secrets last applied with 0.4.0 or earlier carry a `value_hash` of the empty
  string; the first plan on this version shows a one-time `value_hash` update
  that re-sends the configured value and records the correct hash. After
  `terraform import` of a `monad_secret` the first plan is the same one-time
  update — the API never returns secret material, so the provider cannot confirm
  the imported secret already holds the configured value.

## 0.4.0

Contains two breaking changes (edge condition `config.value`, and `nodes` /
`edges` becoming sets) — see below. It fixes conditional edge routing, which did
not work through Terraform for any rule that compares a value; makes pipeline
node/edge order stop being load-bearing, which resolves the `terraform import`
plan-diff saga for good; and modernizes the generated SDK the provider is built
on.

### Fixed

- **Edge conditions that compare values now work.** The provider serialized every
  condition leaf as `{key, value, rate}` regardless of `type_id`, with `value`
  always a JSON array. The API's rules expect different shapes, so 9 of the 11
  rules routed **zero records** — and nothing errored at plan, at apply, or at
  runtime. On a routing fork that is data loss with a green check. Only
  `key_exists` and `is_empty` worked, because they read `key` alone. (ENG-9546)

  Each leaf is now serialized from the API's rule catalogue: only the fields that
  rule reads, and only when set.

- **`terraform import` of a `monad_pipeline` no longer produces
  `Provider produced invalid plan`.** Importing a pipeline whose edges carry a
  `name` or a nested `condition.conditions` block left state that Terraform
  rejected: the next `plan` failed with one error per mismatched attribute, and
  `destroy` was blocked with the same errors until the pipeline was removed
  from state by hand. (ENG-9572)

  Cause: the order-insensitive plan modifiers added in 0.3.0 set the planned
  value to the prior state whenever state and config held the same `nodes` /
  `edges` set in a different order. Terraform requires a plan-known attribute
  to equal the config value at the **same index**, so pinning the plan to state
  order made every position where the two orders disagreed an invalid plan.
  After import — the case the modifiers were written for — state carries API
  order while config carries the authored order, so they disagree by
  construction. The `nodes` modifier had the same defect; it stayed latent only
  because imported node order happened to match config order in practice.

  Both modifiers are removed. They cannot be repaired: for a List, when config
  order and state order differ, no single plan can be element-wise equal to
  config **and** equal to state.

### Added

- **New resource `monad_alert_rule`** — manage alert rules as code alongside
  pipelines, closing the gap where a pipeline could be fully GitOps-managed but
  the alerting that watches it could not (it was clicked into the UI or POSTed by
  hand, and silently lost on a rebuild). Supports create / read / update /
  destroy / import. `type` is immutable and carries `RequiresReplace`; `rule_config`
  is a free-form dynamic/JSON value (each alert type has its own settings schema,
  validated by the API on write, exactly like `monad_transform.config`); update is
  a full replace, so the provider always sends the complete desired state;
  `pipeline_ids` is optional, so org-level alert types with no pipeline are valid.
  (ENG-9549)
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

  This validates the provider's own serialization contract — the fields the
  provider would otherwise drop before the request reaches the API — not the
  pipeline graph, whose topology rules (cycles, node in-degree, root type, node
  and slug limits) remain the API's responsibility.

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

- **`nodes` and `edges` are now sets, not lists.** Pipeline topology is a graph:
  a node is identified by its slug, an edge by its `from`/`to` pair, and the
  position of either in the HCL file carries no meaning. Modelling them as lists
  made index load-bearing, which is what produced the `terraform import` plan
  diff (a list forces state's API order and config's authored order to disagree
  by index) and then the `Provider produced invalid plan` error when 0.3.0 tried
  to hide that. Sets remove the dilemma: Terraform compares them by element
  value, not index, so reordering is not a diff and import is followed by a clean
  plan — no normalizing apply, no invalid plan, no order-rewriting plan modifier.

  **Migration:**
  - **No configuration change is required, and no re-import.** State migrates
    automatically (schema version 1 → 2): the stored `nodes` / `edges` lists are
    re-encoded as sets. Reordering the blocks in HCL no longer shows a diff.
  - **Positional references stop working.** A set element cannot be addressed by
    index, so any expression like `monad_pipeline.x.edges[0]` must be rewritten
    to select by value (e.g. a `for`/`one()` expression keyed on
    `to_node_instance_slug`). Most configurations use neither.
  - **Duplicate edges are unexpressible.** Two edges identical in every attribute
    collapse into a single set element. This is not a real limitation — a node
    has exactly one incoming edge, so a `from`/`to` pair is unique and the API
    rejects a duplicate anyway — but it is now enforced by the schema.

### Changed

- **The `terraform import` plan diff is gone.** Importing a `monad_pipeline` —
  including one with named or conditional edges — is now followed by a clean
  `No changes` plan. This supersedes the 0.3.x behavior, where an imported
  pipeline showed a one-time `nodes` / `edges` reorder diff (0.3.1, after the
  ENG-9572 revert) or failed with an invalid plan (0.3.0). The set migration
  above is what removes it; the order-insensitive plan modifiers those releases
  relied on are gone and are not needed.
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
