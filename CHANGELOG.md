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

### Internal

- L0 fixture test now fails loudly on missing fixture dir instead
  of silently skipping (path-drift detector).
- `.golangci.yml` exclusion paths updated to match Wave 1 tree
  (internal/gates/*, internal/config/*).
- `contracts/schemas/sign.go` extracts SigAlg + SigKey constants.
