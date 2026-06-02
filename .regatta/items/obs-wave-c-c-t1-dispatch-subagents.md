---
id: OBS-WAVE-C-T1
title: dispatch sub-agents counter + dashboard tile (item #9)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0b
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-2 row item #9, §7 Wave-C table row C-T1.

## Task

Instrument `internal/orchestrator/spawner/spawner.go` Spawn path. On every sub-agent dispatch, emit:

```go
meter.Int64Counter("regatta.dispatch.subagents").Add(ctx, 1,
    attribute.String("template", template),
    attribute.String("task_type", taskType),
    attribute.String("agent_id", agentID))
```

Resolve meter from spawner `Config.Meter` field (A-T0b retrofit). Nil falls back to `otel.Meter("orchestrator/spawner")` (covered by `TestDispatchCounter_NilMeterFallback` so existing spawner tests that construct without a meter do not panic post-A-T0b).

Tag set per spec §2.2 budget: `template` (≤ 20 enums), `task_type` (≤ 15 enums), `agent_id` (≤ 100). Cardinality ceiling = 20 × 15 × 100 = 30k cells — safe vs. the spec §2.2 750k breach threshold. Steady-state cross-product is much smaller because `template` × `task_type` is not a full Cartesian set (most templates serve one task type); a property test (A+ rubric) measures real cross-product against the bound. **DO NOT** add `pr_number`/`run_id`/`work_item_id` — banned per spec §2.2. C-T2 ships the per-PR correlation via the PR-lifecycle collector.

Add `dashboards/grafana/dispatch.json`:

1. Stacked-bar panel "Dispatches/min by template" — `sum by (template) (rate(regatta_dispatch_subagents_total[1m]))`.
2. Heatmap panel "Dispatches by task_type" — `sum by (task_type) (rate(regatta_dispatch_subagents_total[5m]))`.
3. Top-10 panel "Most active agents (last 1h)" — `topk(10, sum by (agent_id) (increase(regatta_dispatch_subagents_total[1h])))`.

Per `feedback_research_design_principles`: lean on OTel SDK + spawner's existing span — no custom counter primitive. Trace-metric correlation via exemplars (OTel SDK auto-attaches `trace_id` exemplar on the counter when sampling captures the span).

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; AST-walk lint passes; dashboard JSON checked in; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 + A2 + A4 from spec §8. `TestDashboardMetricNames_MatchEmitted` green. Exemplar drill from counter → trace → spawn span works on test fixture.
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; property test sweeps 200 synthetic dispatches across the full `template` × `task_type` enum cross-product + asserts no cardinality-cap breach (spec §8 A+5).

## Acceptance criteria

- [planned] c1: `internal/orchestrator/spawner/spawner.go` Spawn emits `regatta.dispatch.subagents` Int64Counter (spec §3 item #9).
- [planned] c2: Tag set strictly `template` + `task_type` + `agent_id`; AST-walk lint stays green (spec §2.2).
- [planned] c3: `dashboards/grafana/dispatch.json` checked in with three panels (spec §9 R2).
- [planned] c4: Exemplar attached to counter so operators drill counter → trace → span (spec §10 R8 drill path).
- [planned] c5: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
