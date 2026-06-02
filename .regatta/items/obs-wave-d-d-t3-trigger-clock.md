---
id: OBS-WAVE-D-T3
title: trigger-clock gauge + regatta triggers subcommand + dashboard tile (item #15)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0a, OBS-WAVE-C-T2
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-2 row item #15, §7 Wave-D table row D-T3 (HARD dep on C-T2), §8 B6 (D-T3 PR body precondition).

## Task

Create new file `internal/obs/triggers/clock.go` (path-exclusive new surface). Emits a gauge per known trigger:

```go
meter.Float64ObservableGauge("regatta.trigger.days_remaining").Observe(daysRemaining,
    attribute.String("trigger", trigger)) // 30_day_green | external_customer | self_host_30
```

Resolve meter from a new `internal/obs/triggers/config.go` Config struct (Config.Meter field added inline). Nil falls back to `otel.Meter("obs/triggers")` (covered by `TestTriggers_NilMeterFallback`).

Tag set: `trigger` (3 enums per the 3 phase-S relaxation triggers). Cardinality safe.

**`30_day_green` trigger reads C-T2's PR-stage histogram** to compute "days since last PR-stage anomaly." This is the HARD dep: D-T3 dispatch MUST wait for C-T2 to merge OR the gauge reads zero. Per spec §7 D-T3 row + §8 B6 precondition.

Other triggers:
- `external_customer` — reads an operator-set timestamp from `slo/triggers.yaml` (no live data source yet; gauge stays static until operator updates the file).
- `self_host_30` — reads the self-host start date from `slo/triggers.yaml` + computes days remaining to the 30-d milestone.

Create new sibling subcommand `cmd/regatta/triggers.go` — one stat-line per trigger:
```
30_day_green:       21 days remaining (9 elapsed)
external_customer:  trigger pending (no customer set)
self_host_30:       8 days remaining (22 elapsed)
```

Add `docs/operator/dashboards/trigger-clock.json`:
1. Stat-row panel "Days remaining by trigger" — `regatta_trigger_days_remaining`.
2. Time-series panel "Trigger countdown" — same metric over 30-d window.

Trigger thresholds + start dates live in `slo/triggers.yaml` (operator-editable; checked in). Schema (pinned so the implementer cannot drift):

```yaml
triggers:
  30_day_green:
    start_date: 2026-05-24    # ISO-8601; days_remaining = max(0, 30 - (today - start_date))
    window_days: 30
  external_customer:
    activated: false          # operator flips to true + sets start_date when first paying customer signs
    start_date: null
    window_days: 30
  self_host_30:
    start_date: 2026-05-09    # self-host cutover date
    window_days: 30
```

CUE schema at `slo/triggers.cue` validates the YAML on `make slo-compile`. Test `TestTriggersYAML_ValidatesAgainstCUE` covers structural drift.

**B6 precondition (spec §8):** D-T3 PR body MUST cite C-T2's PR number + show `prom http GET /api/v1/query?query=regatta_pr_stage_duration_seconds_count` returns non-zero series. Do NOT dispatch D-T3 before C-T2 merges — the `30_day_green` gauge would compute against an empty histogram.

Per `feedback_research_design_principles`: lean on OTel SDK ObservableGauge — no custom polling primitive.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; AST-walk lint passes; dashboard JSON + `slo/triggers.yaml` + subcommand check in; B1+B2+B3+B4+B5 + B6 (D-T3 dep precondition cited) from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 + A2 + A4 from spec §8. `TestTriggers_30DayGreenReadsPRStageHistogram` proves the gauge reads C-T2's data correctly on a synthetic-PR fixture. `regatta triggers` stat-line renders for all 3 triggers.
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; trigger-clock dashboard panel shows days-remaining for all 3 triggers on a real backend (Wave-D exit gate).

## Acceptance criteria

- [planned] c1: New `internal/obs/triggers/clock.go` emits `regatta.trigger.days_remaining` Float64ObservableGauge (spec §3 item #15).
- [planned] c2: Tag set strictly `trigger`; AST-walk lint stays green (spec §2.2).
- [planned] c3: `30_day_green` trigger reads C-T2's PR-stage histogram for last-anomaly timestamp (spec §7 D-T3 dep).
- [planned] c4: New `cmd/regatta/triggers.go` subcommand renders one stat-line per trigger.
- [planned] c5: `docs/operator/dashboards/trigger-clock.json` + `slo/triggers.yaml` checked in.
- [planned] c6: Dispatches AFTER C-T2 merges; PR body cites C-T2 PR number + shows non-zero `regatta_pr_stage_duration_seconds_count` (spec §8 B6).
- [planned] c7: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
