# Changelog

All notable changes to Regatta are recorded here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Pre-v1.0 anything may break; entries record the break. After v1.0
contracts follow the deprecation cycle described in `PRINCIPLES.md`
#11 (warn one minor, fail the next).

## Unreleased

### Changed

- Repo restructure Wave 1 (Foundation). Tree shape now satisfies
  the principles documented in PR #57 (design spec).
  - Fixture corpora consolidated under repo-root `testdata/`.
  - Schemas + signed-prompts + wire-protocol docs colocated under
    `contracts/`. Go import path `github.com/trilamsr/regatta/schemas`
    -> `github.com/trilamsr/regatta/contracts/schemas`.
  - `internal/` packages domain-grouped: `internal/gates/{l0,security}`,
    `internal/config/{validate,verify}`.
  - Top-level `gates/` directory removed.
- Repo restructure Wave 2 (Customer surface). Operator + auditor +
  engineer docs land under `docs/`; runnable examples land under
  `examples/`; ADR template lands under `docs/rfcs/`.
- Repo restructure Wave 3 (Governance + closeout). Aggregator
  workflow, tag-triggered release workflow with provenance, three
  new fail-closed gates, dedupe pass against the design spec.
- Branch protection required-contexts expanded to cover every
  PR-time workflow (verify, lint, pr-lint, govulncheck, validate,
  scan, aggregate) instead of just `verify` + `pr-lint`.
- `docs/design.md` §Day 1 → Day 30 Runbook collapsed to a thin
  pointer table into `docs/operator/`; operator docs are now
  canonical for runbook content (spec D3).
- `docs/operator/install.md` reproducibility paragraph collapsed
  to a one-line link to `docs/auditor/reproducibility.md`.

### Added

- `examples/minimal/regatta.yaml` and `examples/full/regatta.yaml`
  (both validate under `regatta validate-config`).
- `examples/target-repo/` stub (activation trigger: e2e quickstart
  smoke).
- `.github/workflows/examples-validate.yml` runs validate-config
  against every examples/*/regatta.yaml on PR + weekly.
- `docs/operator/{quickstart,install,configure,day1,day7,day30,upgrade}.md`.
- `docs/auditor/{threat-model,audit-log,reproducibility,data-flow}.md`.
- `docs/engineer/{how-to-add-a-gate,how-to-add-an-adapter,release-runbook,string-review}.md`
  + `post-mortems/.gitkeep`.
- `docs/rfcs/0000-template.md` (Michael-Nygard ADR template).
- `.github/workflows/gates.yml` cross-workflow aggregator job with
  `if: always()` + `needs.*.result` checks (closes the GitHub
  silent-required-check-bypass class).
- `.github/workflows/release.yml` tag-triggered release: signed-tag
  verify, SLSA build provenance attestation, CHANGELOG Unreleased
  flip, customer release-notes derivation from PR bodies.
- `scripts/check-ci-shape.sh` + `_test.sh` - gate that asserts the
  aggregator + release workflow shape.
- `scripts/check-prose-dup.sh` + `_test.sh` - regression-seed
  detector preventing previously-collapsed prose duplicates from
  drifting back into 2+ markdown files.
- `scripts/check-empty-dirs.sh` + `_test.sh` - earn-or-delete gate
  for README-only / .gitkeep-only directories.
- `docs/engineer/post-mortems/README.md` replaces the bare
  `.gitkeep` and declares the activation trigger.

### Internal

- L0 fixture test now fails loudly on missing fixture dir instead
  of silently skipping (path-drift detector).
- `.golangci.yml` exclusion paths updated to match Wave 1 tree
  (internal/gates/*, internal/config/*).
- `contracts/schemas/sign.go` extracts SigAlg + SigKey constants.
- `make check` now folds in `ci-shape`, `prose-dup`, `empty-dirs`;
  total local runtime stays under 60 seconds.
- `scripts/apply-branch-protection.sh` required-context list
  expanded from `verify` + `pr-lint` to the full PR-time set.
