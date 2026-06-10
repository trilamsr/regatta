---
id: OBS-WAVE-B-T3
title: substrate divergence-detected counter + dashboard tile (item #7)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0b
linked_artifact: docs/engineer/specs/phase-x/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/phase-x/2026-06-02-observability-roadmap.md` §3 Tier-1 row item #7, §7 Wave-B table row B-T3, §2.5 trace head-sampling (divergence-audit always-on override).

## Task

Create new file `internal/orchestrator/state/substrate/divergence_emit.go` (kept separate from existing audit writers per spec §7 path-exclusivity rule). The new file hosts a reader that consumes the divergence-audit table and emits:

```go
meter.Int64Counter("regatta.substrate.divergence.detected").Add(ctx, 1,
    attribute.String("layer", layer))
```

Resolve meter from substrate `Config.Meter` (A-T0b retrofit). Nil falls back to `otel.Meter("orchestrator/state/substrate")`.

Tag set: `layer` (≤ 5 enums). Cardinality safe.

Per spec §2.5, divergence-audit is on the always-sample override list — A-T0a's `ErrorOverride` sampler captures every divergence trace regardless of head-sampling ratio. Span on detection carries `error.type=divergence`.

Resolve meter from substrate `Config.Meter` (A-T0b retrofit) — nil-meter fallback covered by `TestDivergenceCounter_NilMeterFallback` so existing tests that construct substrate without a meter do not panic post-A-T0b.

**Critical-tier alarm rule** (parity with B-T2 chain-break — divergence is the substrate-tamper sibling signal): fires on any non-zero increment (`increase(regatta_substrate_divergence_detected_total[5m]) > 0`). Lives in `slo/substrate-divergence.yaml` (alarm-only, no SLO). Operator runbook `docs/operator/runbooks/substrate-divergence.md` covers triage (cross-layer compare, audit-trail walk, recovery vs replay-from-snapshot). Alert dedup at Alertmanager, not the rule (mirrors B-T2 — first incident must page).

Add `docs/operator/dashboards/substrate-divergence.json`:

1. Stat panel "Divergences detected (last 24h)" — `sum(increase(regatta_substrate_divergence_detected_total[24h]))`.
2. Stacked-bar panel "Divergences by layer" — `sum by (layer) (rate(regatta_substrate_divergence_detected_total[5m]))`.

**File-ownership fence:** This task creates `divergence_emit.go` only. Do NOT edit existing audit-writer files (they remain owned by the original audit-writer authors). The reader pattern is one-way — read divergence-audit table, emit metric — no cross-edits.

Per `feedback_research_design_principles`: lean on the OTel SDK + existing audit-table query primitives.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; AST-walk lint passes; dashboard JSON checked in; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 + A2 + A4 from spec §8. `TestDashboardMetricNames_MatchEmitted` green. Synthetic divergence-row insertion → counter increments + trace carries `error.type=divergence` + sampler captures it (spec §2.5 + §7 Wave-B exit gate).
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; zero edits to existing audit-writer files on PR diff.

## Acceptance criteria

- [planned] c1: New `internal/orchestrator/state/substrate/divergence_emit.go` reads divergence-audit table + emits `regatta.substrate.divergence.detected` (spec §3 item #7).
- [planned] c2: Tag set strictly `layer`; AST-walk lint stays green (spec §2.2).
- [planned] c3: `docs/operator/dashboards/substrate-divergence.json` checked in with two panels (spec §9 R2).
- [planned] c4: Trace span on divergence carries `error.type=divergence`; sampler always-on override captures it (spec §2.5).
- [planned] c5: PR diff touches `divergence_emit.go` + dashboard JSON + `slo/substrate-divergence.yaml` + `docs/operator/runbooks/substrate-divergence.md` only; no edits to existing audit-writer files.
- [planned] c6: Critical-tier alarm rule fires on any non-zero increment; synthetic divergence-row insertion test fixture proves it (parity with B-T2 chain-break).
- [planned] c7: Operator runbook `docs/operator/runbooks/substrate-divergence.md` covers triage (cross-layer compare + recovery).
- [planned] c8: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
