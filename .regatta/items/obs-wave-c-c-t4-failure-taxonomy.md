---
id: OBS-WAVE-C-T4
title: spawner failure-taxonomy counter + dashboard tile (item #12)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0b, OBS-WAVE-C-T1
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-2 row item #12, §7 Wave-C table row C-T4, §7 Wave-C exit gate (≥ 8 mode buckets).

## Task

Create new file `internal/orchestrator/spawner/failure_taxonomy.go` (path-exclusive new surface). Parses CI failure logs into a bounded `mode` enum + emits:

```go
meter.Int64Counter("regatta.dispatch.failure").Add(ctx, 1,
    attribute.String("mode", mode),
    attribute.String("template", template))
```

Resolve meter from the spawner-failure-taxonomy ctor's `Config.Meter` field (A-T0b retrofit lands this Config struct — spec §7 Wave-A table row A-T0b explicitly includes the spawner-failure-taxonomy ctor in its 6 retrofit set).

`mode` enum (≤ 15 buckets, must cover ≥ 8 known failure modes from the last 30 d CI history per Wave-C exit gate): `lint_fail`, `test_fail`, `compile_fail`, `timeout`, `oom`, `network_flake`, `merge_conflict`, `permission_denied`, `dep_missing`, `policy_block`, `other`. Reserved bucket `other` catches unparseable logs — paired with a `TestFailureTaxonomy_UnknownModeRouteToOther` test so cardinality stays bounded.

Tag set: `mode` (≤ 15 enums), `template` (≤ 20 enums, mirrors C-T1). Cardinality safe (≤ 300 cells).

Add `dashboards/grafana/failure-modes.json`:

1. Stacked-bar panel "Failures by mode (1h rolling)" — `sum by (mode) (increase(regatta_dispatch_failure_total[1h]))`.
2. Heatmap panel "Mode × template" — `sum by (mode, template) (rate(regatta_dispatch_failure_total[5m]))`.
3. Top-5 panel "Hottest failure modes (last 24h)" — `topk(5, sum by (mode) (increase(regatta_dispatch_failure_total[24h])))`.

Per `feedback_research_design_principles`: prefer regex-table over a custom parser; if you find yourself writing > 100 LoC of parsing logic, STOP and re-spawn the design subagent.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; AST-walk lint passes; dashboard JSON checked in; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 + A2 + A4 from spec §8. `mode` enum covers ≥ 8 known buckets from 30-d CI history (Wave-C exit gate); `TestFailureTaxonomy_KnownModesCoverage` cross-references against a `testdata/` corpus of real CI logs.
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; property test sweeps 100 synthetic-failure-log strings + asserts every one routes to a defined `mode` bucket OR `other` (no cardinality leak).

## Acceptance criteria

- [planned] c1: New `internal/orchestrator/spawner/failure_taxonomy.go` parses CI failure logs + emits `regatta.dispatch.failure` (spec §3 item #12).
- [planned] c2: Tag set strictly `mode` + `template`; AST-walk lint stays green (spec §2.2).
- [planned] c3: `mode` enum covers ≥ 8 known buckets from 30-d CI history; `other` reserved bucket catches unparseable logs (spec §7 Wave-C exit gate).
- [planned] c4: `dashboards/grafana/failure-modes.json` checked in with three panels (spec §9 R2).
- [planned] c5: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
