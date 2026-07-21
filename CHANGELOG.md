# Changelog

All notable changes to this provider are documented here. This project adheres
to [Semantic Versioning](https://semver.org/). While the provider is pre-1.0,
breaking changes are released as minor version bumps.

## Unreleased

Recommended release: **0.2.0** (contains a breaking change — see below).

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
