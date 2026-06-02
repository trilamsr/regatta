---
id: OBS-WAVE-B-T1
title: substrate event-rate counter + SLO-3 warn-tier + dashboard tile (item #5)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0b
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-1 row item #5, §7 Wave-B table row B-T1, §5 SLO-3 entry, §9 R6 + R10.

## Task

Instrument `internal/orchestrator/state/substrate/event.go` Append path. After a successful row append, emit:

```go
meter.Int64Counter("regatta.substrate.events.appended").Add(ctx, 1,
    attribute.String("layer", layer),
    attribute.String("kind", kind))
```

Resolve the meter from the substrate `Config.Meter` field (landed in A-T0b's substrate Config retrofit). Nil falls back to `otel.Meter("orchestrator/state/substrate")`.

Tag set per spec §2.2 budget: `layer` (≤ 5 enums: dispatch/cost/divergence/audit/policy), `kind` (≤ 30 enums). Cardinality safe. **DO NOT** add `pr_number`/`run_id`/`work_item_id` — banned on metrics per spec §2.2.

Ship `slo/substrate-event-rate.yaml` (SLO-3, warn-tier per §5): tracks events/sec rolling-rate; warn-tier alarm fires on > 2× 24-h trailing P95 (substrate-event-storm signal). Sloth compile to `dashboards/prometheus/rules/`.

Add `dashboards/grafana/substrate-events.json`:

1. Line panel "Events/sec by layer" — `sum by (layer) (rate(regatta_substrate_events_appended_total[1m]))`.
2. Heatmap panel "Events by kind over time" — `sum by (kind) (rate(regatta_substrate_events_appended_total[5m]))`.

Per `feedback_research_design_principles`: lean on OTel SDK + Sloth verbatim; no custom rate calc.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; `TestMetricCardinality_PRNumberLabelBanned` passes against this emitter; dashboard JSON checked in; SLO YAML compiles via `make slo-compile`; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer subagent clears; A1 + A2 + A4 from spec §8. `TestDashboardMetricNames_MatchEmitted` green. SLO-3 warn-tier alarm fires on synthetic burst (10× normal rate for 60 s).
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; `TestSLOBurnRate_FiresOnSyntheticBreach` covers SLO-3 (A+1 + A+2 from spec §8).

## Acceptance criteria

- [planned] c1: `internal/orchestrator/state/substrate/event.go` Append emits `regatta.substrate.events.appended` Int64Counter (spec §3 item #5).
- [planned] c2: Tag set strictly `layer` + `kind`; AST-walk lint stays green (spec §2.2).
- [planned] c3: `slo/substrate-event-rate.yaml` SLO-3 warn-tier compiles + fires on synthetic burst (spec §5 SLO-3).
- [planned] c4: `dashboards/grafana/substrate-events.json` checked in with two panels referencing emitted names (spec §9 R2).
- [planned] c5: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
