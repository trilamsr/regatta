---
id: OBS-WAVE-B-T4
title: replay-latency histogram + SLO + dashboard tile (item #8)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0b
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-1 row item #8, §7 Wave-B table row B-T4, §5 SLO entry for replay latency.

## Task

Instrument `internal/history/substrate_impl.go` Replay path. Wrap the replay call with a histogram timer:

```go
meter.Float64Histogram("regatta.replay.latency_ms").Record(ctx, durationMs,
    attribute.String("impl", impl)) // sqlite | substrate | hybrid
```

Resolve meter from `internal/history` Config.Meter field (A-T0b retrofit). Nil falls back to `otel.Meter("history")`.

Tag set: `impl` (3 enums max). Cardinality safe.

Ship `slo/replay-latency.yaml`: tracks P95 replay latency per `impl`; warn-tier alarm fires on P95 > 500 ms over 10-min window; critical-tier on P95 > 2 s. Sloth compile to `dashboards/prometheus/rules/`. Operator runbook `docs/operator/runbooks/replay-latency.md` covers triage (sqlite VACUUM, substrate compaction, hybrid-fallback toggle).

Add `dashboards/grafana/replay.json`:

1. Line panel "Replay P50/P95/P99 by impl" — `histogram_quantile(0.95, sum by (le, impl) (rate(regatta_replay_latency_ms_bucket[5m])))`.
2. Heatmap panel "Latency distribution" — `sum by (le) (rate(regatta_replay_latency_ms_bucket[5m]))`.

Per `feedback_research_design_principles`: lean on OTel SDK histogram primitives + Sloth burn-rate compile; no custom percentile math.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; AST-walk lint passes; dashboard JSON + SLO YAML + runbook all check in; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 + A2 + A3 + A4 from spec §8. `TestDashboardMetricNames_MatchEmitted` green. Synthetic-replay-load test verifies histogram populates with realistic latency distribution.
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; `TestSLOBurnRate_FiresOnSyntheticBreach` covers replay-latency SLO (A+1 + A+2 from spec §8).

## Acceptance criteria

- [planned] c1: `internal/history/substrate_impl.go` Replay emits `regatta.replay.latency_ms` Float64Histogram (spec §3 item #8).
- [planned] c2: Tag set strictly `impl`; AST-walk lint stays green (spec §2.2).
- [planned] c3: `slo/replay-latency.yaml` compiles via `make slo-compile`; warn + critical tiers fire on synthetic load (spec §5).
- [planned] c4: `dashboards/grafana/replay.json` checked in with two panels (spec §9 R2).
- [planned] c5: Operator runbook `docs/operator/runbooks/replay-latency.md` covers triage (spec §8 A3).
- [planned] c6: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
