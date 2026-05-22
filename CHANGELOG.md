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
  the principles documented in PR #57 (design spec, not yet merged
  to main).
  - Fixture corpora consolidated under repo-root `testdata/`.
  - Schemas + signed-prompts + wire-protocol docs colocated under
    `contracts/`. Go import path `github.com/trilamsr/regatta/schemas`
    -> `github.com/trilamsr/regatta/contracts/schemas`.
  - `internal/` packages domain-grouped: `internal/gates/{l0,security}`,
    `internal/config/{validate,verify}`.
  - Top-level `gates/` directory removed.

### Internal

- L0 fixture test now fails loudly on missing fixture dir instead
  of silently skipping (path-drift detector).
