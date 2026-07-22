# CLAUDE.md

Guidance for Claude Code (and any future AI coding session) working in the
**terraform-provider-monad** repo. It encodes the hard-won lessons from
ENG-9129 / PR #7, where a class of `Provider produced inconsistent result after
apply` bugs was fixed across every resource. Read this before touching any
`internal/provider/*.go` file.

## What this repo is

A Terraform provider for [Monad](https://monad.com), built on the
**terraform-plugin-framework** (not the legacy SDKv2). It wraps the Monad HTTP
API via the generated Go SDK `github.com/monad-inc/sdk/go`.

- Resources: `monad_input`, `monad_output`, `monad_enrichment`,
  `monad_transform`, `monad_pipeline`, `monad_secret`.
- `input`/`output`/`enrichment`/`transform` share `resource_connector.go`
  (schema, Create/Update/Read helpers, the secrets/hash machinery).
- Conversion + reconciliation helpers live in `utils.go`; their unit tests are
  in `reconcile_test.go` and `utils_test.go`.
- `docs/resources/*.md` are **generated** — never hand-edit them (see Docs).

## Toolchain

- **Go** `1.23.0` (toolchain `go1.24.3`) per `go.mod`. The README still says
  1.21 — it is stale; trust `go.mod`.
- **terraform-plugin-framework** `v1.15.0`. `WriteOnly` attributes require
  `>= 1.15` — do not downgrade below that.
- **Terraform** `1.x` for local runs.

## Build, check, verify

```sh
go build -o terraform-provider-monad .   # or: task build
go vet ./internal/...
go test ./internal/...
gofmt -l internal/                        # must print nothing
```

Run all four before every commit and before reporting work done. `task` targets
exist (`task build`, `task generate`, `task example-apply`,
`task example-destroy`) — see `Taskfile.yml`.

### Always keep import ordering / gofmt clean

`gofmt` (and goimports-style grouping: stdlib, then third-party, then this
module) is enforced by review. Run `gofmt -w` on any file you touch.

## The core invariant: apply-consistency (why PR #7 existed)

Terraform requires that for any **plan-known** attribute, the value returned
after Create/Update **equals the planned value**. Violating this is the
`Provider produced inconsistent result after apply` error.

The original bug: Create/Update rebuilt the `Dynamic` `config`/`settings` (and
pipeline `nodes`/`edges`) from the **API response**. A practitioner's
`jsondecode(...)` yields a cty **tuple**; the API-derived value is a **list**.
Same data, different cty type → apply-consistency violation:

```
.config: wrong final value type: attribute "operations": tuple required.
```

### NEVER, in Create/Update

- **Never rebuild a Required, plan-known attribute from the API response.**
  That includes `config`/`settings` (Dynamic), pipeline `nodes`/`edges`,
  `name`, `description`, `type`. Rebuilding changes cty type and/or drops
  write-only values the API never echoes.
- Never call the old `connectorConfigToTF` / `transformConfigToMap` style
  helpers to repopulate state from a response. They were deleted in PR #7 for
  exactly this reason — do not reintroduce them.

### ALWAYS, in Create/Update

- **Set only the computed `id` from the response**; keep every plan-known value
  as it came from the plan. `data` is populated from `req.Plan.Get` (or
  `req.Config.Get` — see secrets), then only `data.ID` is overwritten.
- The `id` attribute uses `stringplanmodifier.UseStateForUnknown()`.

## Read: refresh for drift WITHOUT perpetual diffs

You cannot just skip refreshing in Read to dodge the consistency problem —
**`Read` is the sole state populator on import.** Every resource uses
`ImportStatePassthroughID`, which seeds only `id`. If Read doesn't refresh, an
imported resource lands with null config and shows a full after-import diff, and
`import -generate-config-out` emits empty configs.

The resolution (do it this way): **reconcile at the comparison layer, not by
overwriting.**

- Build the API-derived value, compare it to prior state as **normalized JSON**,
  and **keep the prior state value verbatim when semantically equal**. Only
  adopt the API value when there is genuine drift.
- On import, prior state is null/empty, so the API value populates — import
  works.
- Use the existing helpers, don't roll your own:
  - `reconcileDynamic` for Dynamic `settings`/`config`.
  - `reconcilePipelineNodes` / `reconcilePipelineEdges` / `reconcilePipelineEnabled`.
  - `dynamicsSemanticallyEqual`, `jsonNormalize`, `pruneEmpty` underneath.

### Server-populated / non-round-trippable fields must be masked

When comparing prior vs API, mask anything the practitioner didn't author or
that can't round-trip, so it never reads as false drift:

- Server-generated node **slugs** the practitioner omitted (null).
- Echoed edge **name**/**description** the practitioner left null.
- Node-instance **ids** and server **ordering**.
- Fields with **no field on the API model** (e.g. transform operation
  `description`) — mask them out of the comparison entirely and keep the prior
  value. (PR #7 first tried masking operation description, then dropped it as
  dead config — if a field can't round-trip, don't compare it.)

### Empty values: the API drops them (`omitempty`)

The SDK serializes with `omitempty`, so a practitioner's `""` comes back
**omitted**. `pruneEmpty` removes `""` / null / empty-object / empty-array on
**both** sides before comparison so they compare equal. **Never prune booleans
or numbers** — `false` and `0` are real values; masking them hides genuine
changes. `pruneEmpty` prunes slice elements in place but never drops them
(array length and order are significant).

### null-implies-a-default equivalence

If a null attribute implies a create-time default, preserve null when the API
reports that default, or you get a perpetual diff. Concrete case:
`monad_pipeline.enabled` is Optional with no default; Create/Update treat null
as `true`. So `reconcilePipelineEnabled` keeps a null prior when the API returns
`true`, and only adopts the API value otherwise (an explicit value, or drift
away from the default such as a UI-side disable).

### Scalar null/`""` guards

`monad_output` (and any description-less resource, e.g. a `dev-null` sink) hit
`.description: was null, but now cty.StringVal("")`. Convert an API `""` to
`types.StringNull()` for Optional string scalars in Read.

## Write-only secrets (BREAKING change in PR #7 — follow this pattern)

Secret material must **never** be persisted in Terraform state, and the API
never echoes raw secrets (it returns references/ids). The pattern:

- `config.secrets` is `Optional + Sensitive + WriteOnly` (a `DynamicAttribute`).
- **Write-only values are `null` in the plan.** So:
  - **Create** reads the whole model from `req.Config.Get` (not `req.Plan.Get`).
  - **Update** reads plan-known values from `req.Plan.Get`, then reads secrets
    separately from `req.Config.Get`.
- After Create/Update, `finalizeConnectorSecrets` nulls `config.secrets` and
  sets the computed `config.secrets_hash`.
- **Rotation detection** = a computed `secrets_hash` (HMAC; key is
  `MONAD_SECRETS_KEY`, falling back to the org id — see `secretsHashKey`).
  `ModifyPlan` (`modifyConnectorPlanForSecrets`) compares a fresh hash of the
  configured secrets against the stored hash and, on mismatch, marks
  `secrets_hash` **Unknown**. An unknown planned value accepts any final value,
  so Update can recompute the hash without an apply-consistency violation.
  - **Never** set `secrets_hash` to a concrete computed value in `ModifyPlan`;
    mark it Unknown and let Update fill it.
- Secrets must be a **structured value**, never a bare string: a new secret
  `{ value, name, description }` (all non-empty) or a reference `{ id }`. Monad
  stores inline secrets **by name**, so a rotated value needs a fresh name.

### Never swallow diagnostics

`SetAttribute`/`GetAttribute` return `diag.Diagnostics` — **append them**
(`resp.Diagnostics.Append(...)`), never discard. On conversion/hash failure in a
plan modifier, emit at least a warning (`AddAttributeWarning`) rather than
silently degrading (this was review nit #1 on PR #7). Silent early-returns are
only acceptable for genuinely-benign "nothing to reconcile" cases (create/
destroy with null state/plan, an absent config block).

## Live verification is mandatory for Read/plan changes

Unit tests are necessary but **not sufficient**. In PR #7, unit tests passed but
live verification (`dev_overrides` against a real org) surfaced **three**
additional Read-refresh perpetual-diff defects (block-less config synthesized as
all-null, `omitempty` empty-value mismatch, non-round-trippable operation
description). Always live-verify anything touching Create/Update/Read/ModifyPlan.

### Live-check recipe (dev_overrides against a real org)

1. Build into a dir and point `dev_overrides` at that **directory** (the binary
   must be named `terraform-provider-monad`):

   ```hcl
   # dev.tfrc
   provider_installation {
     dev_overrides { "monad-inc/monad" = "/abs/path/to/bindir" }
     direct {}
   }
   ```

2. **Do NOT run `terraform init`** with dev_overrides — there is no registry
   release to install; init fails. Just `plan`/`apply`. `terraform validate`
   works (it only warns about the override).
3. Provider config gotcha: the SDK client **appends `/api`** to `base_url`. Use
   `base_url = "https://app.monad.com"` — **not** `.../api` (that yields
   `/api/api` → `404 Not Found`).
4. Credentials via env: `MONAD_API_TOKEN`, `MONAD_ORGANIZATION_ID`,
   `MONAD_BASE_URL`. Use the **`kenneth-testing`** org for throwaway tests. Never
   print the token; read it from 1Password / an env var and pass it through.
5. The verification matrix that must pass:
   - `apply` → `N added`, **zero** inconsistency errors.
   - `plan` again → **`No changes`** (no perpetual diff).
   - Mutate a field out-of-band (e.g. toggle pipeline `enabled` via the API/UI)
     → `plan` **shows that drift** → `apply` → `plan` is clean again.
   - `import` → clean `No changes` plan.
   - `destroy` → everything removed cleanly.
6. **Always `terraform destroy` and clean up** test resources afterward.

Known non-defect to expect: `monad_pipeline` **import** populates correctly but
the first post-import `apply` may show a one-time diff (edge ordering /
`enabled`) because Read can't see the practitioner's HCL on import. Intermittent
**500s** on pipeline update/delete on a busy org are transient API flakiness —
retry before concluding it's a provider bug (compare the request payload to
`main` to confirm it's byte-identical).

## Docs are generated

`docs/resources/*.md` are produced by `task generate` (`cd tools && go generate`,
tfplugindocs). After any schema change, **regenerate** rather than editing the
markdown by hand, and commit the regenerated files. Keep `README.md`'s hand-
written resource tables in sync too (they drifted before PR #7).

## Changelog & versioning

- The provider is **pre-1.0**; **breaking changes ship as minor bumps** (per
  `CHANGELOG.md`) — e.g. the breaking write-only-secrets change shipped in
  `0.2.0`.
- Land user-visible changes under a `## Unreleased` section (Fixed / Added /
  Changed (BREAKING) / Known issues) and document a **migration** path for
  breaking ones. (The write-only-secrets break needed no state upgrader — the
  framework auto-nullifies write-only attributes on every Read/plan, so old
  state carrying secret refs drops gracefully — but it is still a state break
  and had to be called out.)
- **When you cut a release, promote `## Unreleased` to the version heading**
  (`## X.Y.Z`), in that PR or a preceding one, so the changelog and the tag
  agree. See Releasing.

## Releasing

Releases are automated by **GoReleaser**, triggered by pushing a **`vX.Y.Z`**
git tag (`.github/workflows/release.yml`). The job cross-compiles every
platform, writes `*_SHA256SUMS`, **GPG-signs** the checksums, publishes a GitHub
Release (the zips + `terraform-registry-manifest.json`), and the Terraform
Registry then auto-ingests the new tag.

- **A merge to `main` is NOT a release.** Until a `vX.Y.Z` tag is pushed and the
  workflow runs, the registry still serves the previous version and consumers'
  `.terraform.lock.hcl` stays pinned to it. To exercise an unreleased fix, build
  from `main` and use `dev_overrides` (see the live-check recipe) — don't expect
  `terraform init` to pick it up.
- **Cut a release:** from an up-to-date `main`,
  `git tag -a vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z`. Version per SemVer
  (pre-1.0: breaking = minor bump). Promote the CHANGELOG heading first.
- **Requires** the `GPG_PRIVATE_KEY` + `PASSPHRASE` Actions secrets (public key
  registered on the registry publisher account); `GITHUB_TOKEN` is automatic.
  The tag MUST match `v*` or the workflow never fires.
- **Keep `.goreleaser.yml` clean:** run `goreleaser check` before releasing —
  deprecations make it exit non-zero and a future GoReleaser bump will hard-fail
  the release. Archives use the list form `formats: [zip]`, not the deprecated
  scalar `format:`. Dry-run without publishing or GPG via
  `goreleaser release --snapshot --clean --skip=sign` and inspect `dist/`
  (release zips contain only `LICENSE`, `README.md`, and the binary — GoReleaser's
  default file set — so root docs like this file are never shipped).
- **If a release run fails partway**, delete the tag locally and remotely
  (`git push origin :refs/tags/vX.Y.Z`), fix the cause, and re-push.

## Commits & PRs

- **Conventional Commits 1.0.0**, scoped per component:
  `fix(pipeline):`, `fix(provider):`, `fix(transform):`, `docs:`, `chore:`.
  Imperative subject, ≤72 chars, no trailing period. Add a body explaining
  *why*. Document breaking changes with a `BREAKING CHANGE:` footer.
- Reference the tracking issue with a `Refs: ENG-XXXX` footer.
- **`gh pr edit` is broken across `monad-inc` repos** (GitHub Projects-classic
  GraphQL deprecation → exits 1). Set the assignee at creation
  (`gh pr create --assignee "$(gh api user --jq .login)"`); add a team reviewer
  via REST, not `gh pr edit`:

  ```sh
  gh api --method POST repos/monad-inc/terraform-provider-monad/pulls/<N>/requested_reviewers \
    -f 'team_reviewers[]=eng'
  ```

  To reference an issue on an existing PR, post a `gh pr comment` — don't edit
  the body.

## Quick checklist before you push a provider change

- [ ] Create/Update set only `id` from the response; all plan-known values preserved.
- [ ] Read refreshes via the reconcile helpers; server-populated / non-round-trippable fields masked; empties pruned (booleans/numbers untouched).
- [ ] Any new secret handling is write-only + hash; secrets read from `req.Config`; diags appended, not swallowed.
- [ ] `go build` / `go vet` / `go test ./internal/...` / `gofmt -l` all clean.
- [ ] Docs regenerated (`task generate`) if the schema changed; README/CHANGELOG updated.
- [ ] Live-verified with `dev_overrides`: apply → no-op plan → drift detected → import clean → destroy clean.
