# Changelog

All notable changes to Regatta are recorded here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Pre-v1.0 anything may break; entries record the break. After v1.0
contracts follow the deprecation cycle described in `PRINCIPLES.md`
#11 (warn one minor, fail the next).

## v0.1.0 - 2026-05-30 - MVP-1 Planner-as-DAG

### Added

- `state.work_items` table; universal queue, single source of truth.
- `internal/orchestrator/adaptersync`; mirrors SpecAdapter into
  `work_items` each tick.
- `internal/program.BriefLoader` + `LoadAndVerifyBrief`; verifies
  signed program briefs and upserts child work_items.
- `internal/program.LoadPlannerPrompt`; operator-pinned sha
  verification with embedded-prompt fallback.
- `internal/orchestrator/lockfile`; `gofrs/flock` wrapper with
  stale-PID reclaim.
- `internal/orchestrator/clock`; `Clock` interface for deterministic
  time injection.
- `internal/orchestrator/errors`; typed sentinels
  (`ErrBriefSHAMismatch`, `ErrHMACInvalid`, `ErrTargetExists`,
  `ErrFlockHeld`, `ErrSchemaTooNew`, `ErrCycleDetected`).
- `regatta program plan --write`; atomic temp+rename of signed brief
  into `.regatta/programs/<program_id>.json`.
- `pressly/goose` migration runner
  (`internal/orchestrator/state/migrations/`).
- DAG enforcement: blocked children wait until upstream
  `status=merged`; cycle detection at upsert.
- Operator docs: program plan walkthrough, flock troubleshooting,
  slog event reference.
- [`docs/engineer/mvp-1-dod-checklist.md`](docs/engineer/mvp-1-dod-checklist.md);
  series-complete + grade-A + grade-A+ tickable gates with
  verification commands.

### Changed

- `orchestrator.PollOnce` rewired: flock acquire -> AdapterSync ->
  BriefLoader -> Scheduler.Tick (fail-fast).
- `scheduler.Tick` now reads `state.ListSpawnable` and reserves
  agents in a single transaction (replaces direct `adapter.List` +
  `UpsertPending` path).
- `state.Open` uses `Migrate(ctx, db)` (goose) instead of inline
  `schema.sql` apply.

### Removed

- `internal/orchestrator/state/schema.sql`; extracted into
  `migrations/0001_initial.sql`.
- `docs/engineer/specs/mvp-1-planner.md`; superseded by the MVP-1
  Planner-as-DAG design spec under `docs/superpowers/specs/`
  (local-only working spec; CHANGELOG + DoD checklist are the
  customer-visible record).

## v0.0.1 - 2026-05-30

First canary tag. Mutation-verifies the release.yml workflow
end-to-end: signed-tag verify, make ci-check at tagged commit,
artifact build, SLSA provenance attestation, gh release create.
No customer-visible product code yet; v0.0.1 is the release-
pipeline smoke release.

### Changed

- Repo restructure Wave 1 (Foundation). Tree shape now satisfies
  the principles in `docs/superpowers/specs/2026-05-21-org-repo-restructure-design.md`.
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
- Repo restructure Wave 3 (Governance + closeout). Tag-triggered
  release workflow with provenance, prose-dup regression gate,
  dedupe pass against the design spec.
- Branch protection required-contexts updated to the live PR-time
  set (verify, lint, pr-lint, govulncheck, validate); aggregate
  and scan dropped after the aggregator + PR-time stale-todo were
  removed as ceremony-without-consumer.
- `required_conversation_resolution` flipped to false; solo
  maintainer has no separate reviewer threads to forget.
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
- `.github/workflows/release.yml` tag-triggered release: signed-tag
  verify, SLSA build provenance attestation, gh release artifact
  upload. Maintainer flips CHANGELOG manually before tagging.
- `scripts/check-prose-dup.sh` + `_test.sh` - regression-seed
  detector preventing previously-collapsed prose duplicates from
  drifting back into 2+ markdown files.
- `docs/engineer/post-mortems/README.md` replaces the bare
  `.gitkeep` and declares the activation trigger.

### Removed

- `.github/workflows/gates.yml` aggregator (no falsifying consumer
  while branch protection lists each workflow individually).
- `scripts/check-ci-shape.sh` + `_test.sh` (aggregator gone).
- `scripts/check-empty-dirs.sh` + `_test.sh` (eight dirs total;
  speculation caught faster in review).
- `pr-lint` root-cause block requirement + PR template `root-cause`
  block (no downstream consumer; mandate lives in PR-author
  discipline + reviewer eye, not gate text).
- `stale-todo` PR-time trigger (kept as weekly cron + manual
  dispatch); old TODOs no longer block unrelated PRs.
- `release.yml` CHANGELOG auto-flip + auto-PR-back + PR-body
  release-notes derivation (~85 lines); solo maintainer hand-flips
  CHANGELOG and uses `gh release create --generate-notes`.

### Internal

- L0 fixture test now fails loudly on missing fixture dir instead
  of silently skipping (path-drift detector).
- `.golangci.yml` exclusion paths updated to match Wave 1 tree
  (internal/gates/*, internal/config/*).
- `contracts/schemas/sign.go` extracts SigAlg + SigKey constants.
- `make check` constituents: `doc-check`, `prose-dup`, `vet`,
  `tidy-check`, `mod-verify`, `go-check`; total local runtime stays
  under 60 seconds.
- `scripts/apply-branch-protection.sh` required-context list set to
  the live PR-time workflow names.
