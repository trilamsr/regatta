---
id: OBS-WAVE-A-T3
title: scheduler tick latency histogram + per-step duration breakdown (item #4)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0b
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-1 row item #4, §7 Wave-A table row A-T3, §10 dispatch brief A-T3, §4 anti-pattern trap #2 (span-per-loop).

## Task

Instrument `internal/orchestrator/scheduler/scheduler.go` Tick path with two histograms. Resolve the meter from the scheduler `Config.Meter` field (landed in A-T0b). Nil falls back to `otel.Meter("orchestrator/scheduler")`.

Emitters:

```go
meter.Float64Histogram("regatta.scheduler.tick.latency_ms")
    .Record(ctx, tickElapsedMs)  // on tick-span close (W6 lifecycle hook)
meter.Float64Histogram("regatta.scheduler.tick.step_duration_ms")
    .Record(ctx, stepElapsedMs, attribute.String("step", stepName))
```

The eight named steps (per spec §3 row 4): `dispatch`, `gate_l0`, `gate_l4`, `gate_approval`, `gate_cost`, `reaper`, `fold`, `persist`. Cardinality safe (8 enums).

**Critical (spec §4 trap #2):** open ONE span around the step loop with an iteration counter. Do NOT open one span per step iteration. The histogram records latency per step; the span scopes the whole loop. This is the documented anti-pattern — reviewer subagent flags any per-iteration span on diff.

Use the existing W6 tick-span lifecycle hook to record `regatta.scheduler.tick.latency_ms` on span close (do NOT duplicate timing — span-close timestamp minus span-start is the same number).

Add `dashboards/grafana/scheduler.json` per spec §3 tile shape:

1. "Scheduler tick — p50/p95/p99 over time" — `histogram_quantile({0.50, 0.95, 0.99}, rate(regatta_scheduler_tick_latency_ms_bucket[5m]))`.
2. "Tick step breakdown — stacked histogram" — `sum by (step) (rate(regatta_scheduler_tick_step_duration_ms_sum[5m]))`.

`slo/scheduler-tick.yaml` (SLO-1 — p95 ≤ 5s) ships in A-T5; do NOT duplicate. Alarm "p95 tick latency > 5s for 5 min" is Sloth-generated downstream.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; both histograms land; `TestMetricCardinality_PRNumberLabelBanned` (from A-T0a) green; `step` label scoped to the 8 named enums; dashboard JSON exists with 2 panels; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer subagent clears + verifies NO per-iteration span (spec §4 trap #2); A1 + A2 + A4 from spec §8; `TestDashboardMetricNames_MatchEmitted` green.
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; benchmark `BenchmarkSchedulerTick_HistogramOverhead` documents histogram overhead ≤ 100 ns/tick (spec §3 row 4 alarm budget headroom).

## Acceptance criteria

- [planned] c1: `regatta.scheduler.tick.latency_ms` Float64Histogram emits on tick-span close; `regatta.scheduler.tick.step_duration_ms` Float64Histogram emits per step with `step` label (spec §3 item #4).
- [planned] c2: ONE span around the step loop with iteration counter — no per-iteration spans (spec §4 trap #2 — reviewer-enforced).
- [planned] c3: `step` label restricted to the 8 named enums; AST-walk lint stays green.
- [planned] c4: `dashboards/grafana/scheduler.json` checked in with 2 panels; PromQL refs resolve to emitted names (spec §9 R2).
- [planned] c5: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
