# Changelog

All notable changes to this provider are documented here. This project adheres
to [Semantic Versioning](https://semver.org/). While the provider is pre-1.0,
breaking changes are released as minor version bumps.

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
