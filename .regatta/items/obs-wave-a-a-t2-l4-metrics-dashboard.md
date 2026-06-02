---
id: OBS-WAVE-A-T2
title: L4 gate metrics — invocations + latency histogram + cache hit/miss + second-opinion + per-category (item #2)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0a
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-1 row item #2, §7 Wave-A table row A-T2, §10 dispatch brief A-T2.

## Task

Instrument the L4 gate paths (`internal/gates/l4/gate.go` + `percategory.go` + `reload.go`) with five OTel metric calls. Resolve the meter from the package's `Config.Meter` field (landed in A-T0a's `internal/gates/l4` Config retrofit); nil falls back to `otel.Meter("gates/l4")`.

Emitter set:

```go
meter.Int64Counter("regatta.l4.invocations").Add(ctx, 1,
    attribute.String("verdict", v),    // allow | deny | needs_review | escalate | skip
    attribute.String("category", cat)) // ≤ 12 per #388
meter.Float64Histogram("regatta.l4.latency_ms").Record(ctx, elapsedMs)
meter.Int64Counter("regatta.l4.cache.hits").Add(ctx, 1)
meter.Int64Counter("regatta.l4.cache.misses").Add(ctx, 1)
meter.Int64Counter("regatta.l4.second_opinion.fired").Add(ctx, 1)
```

Existing L4 paths (#381 #380 #388) already carry the labels in slog events — wire from the existing event-field set. Do NOT introduce new label dimensions; cardinality cap is 5 verdicts × 12 categories × 3 cache_outcomes = 180 cells (spec §3 row 2, "safe").

Add `docs/operator/dashboards/l4-gate.json` with 5 panels per spec §3 tile shape:

1. "L4 — invocations/sec by verdict" — `sum by (verdict) (rate(regatta_l4_invocations_total[5m]))`.
2. "L4 — p50/p95/p99 latency" — `histogram_quantile({0.50, 0.95, 0.99}, rate(regatta_l4_latency_ms_bucket[5m]))`.
3. "L4 — cache hit ratio" — `rate(regatta_l4_cache_hits_total[5m]) / (rate(regatta_l4_cache_hits_total[5m]) + rate(regatta_l4_cache_misses_total[5m]))`.
4. "L4 — second-opinion fire-rate" — `rate(regatta_l4_second_opinion_fired_total[5m])`.
5. "L4 — per-category stack" — `sum by (category) (rate(regatta_l4_invocations_total[5m]))`.

`slo/l4-latency.yaml` (SLO-2 — p95 ≤ 30s) ships in A-T5; do NOT duplicate here. Alarm threshold "p95 latency > 30s for 10 min" is Sloth-generated downstream.

Per the trap-9 lint shipped in A-T0a (`TestEveryGateAdapterHasInvocationsCounter` per amendment review §5 L6 closure), this L4 emitter set is the canonical example the AST walker validates against — keep the counter name exactly `regatta.l4.invocations` so the lint passes.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; all 5 emitters land; `TestMetricCardinality_PRNumberLabelBanned` (from A-T0a) green; dashboard JSON exists with 5 panels; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer subagent clears; A1 + A2 + A4 from spec §8; `TestDashboardMetricNames_MatchEmitted` green for all 5 panels (spec §9 R2 drift gate); `TestEveryGateAdapterHasInvocationsCounter` (trap-9 lint) passes against L4.
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0 on `docs/operator/dashboards/l4-gate.json` (A+1); cache-hit-ratio panel verified against a synthetic load fixture (10 hits + 10 misses → 0.5 ratio).

## Acceptance criteria

- [planned] c1: 5 emitters land on `internal/gates/l4/gate.go` + `percategory.go` + `reload.go` with exact metric names per spec §3 (spec §2.1 naming convention).
- [planned] c2: Tag set scoped to `verdict` + `category` only on `regatta.l4.invocations`; AST-walk lint stays green (spec §2.2 + §9 R1).
- [planned] c3: `docs/operator/dashboards/l4-gate.json` checked in with the 5 panels; PromQL refs resolve to emitted metric names (spec §9 R2).
- [planned] c4: `TestEveryGateAdapterHasInvocationsCounter` (trap-9 lint from A-T0a) green for the L4 path (amendment review §5 L6 closure).
- [planned] c5: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
