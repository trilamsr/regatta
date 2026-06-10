---
id: OBS-WAVE-A-T1
title: per-DAG-run cost counter + dashboard tile (item #1)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0a
linked_artifact: docs/engineer/specs/phase-x/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/phase-x/2026-06-02-observability-roadmap.md` §3 Tier-1 row item #1, §7 Wave-A table row A-T1, §10 dispatch brief A-T1, §11 cross-wedge (shared-owner pin on `cost/spend/writer.go`).
Amendment ref: review of PR #410 §7 RISK-A confirms A-T1 owns `internal/cost/spend/writer.go` across Wave A + Wave C.

## Task

Instrument `internal/cost/spend/writer.go` (existing #283 writer). After the event row is appended to the substrate, also emit two metric calls:

```go
meter.Float64Counter("regatta.cost.usd").Add(ctx, usd,
    attribute.String("dag_id", dagID),
    attribute.String("operator_id", opID))
meter.Int64Counter("regatta.cost.tokens").Add(ctx, n,
    attribute.String("dag_id", dagID),
    attribute.String("operator_id", opID),
    attribute.String("direction", dir)) // input | output | cache_read
```

Resolve the meter from the package's `Config.Meter` field (landed in A-T0a's `internal/cost/spend` Config retrofit). Nil falls back to `otel.Meter("cost/spend")` per the W6 mirror pattern.

Tag set (per spec §3 Tier-1 + §2.2 cardinality budget): `dag_id` (≤ 50), `operator_id` (≤ 100), `direction` (3 enums). Cardinality safe (≤ 15k cells). **DO NOT** add `pr_number`, `run_id`, or `work_item_id` to the meter calls — per spec §2.2 those are banned on metrics; C-T3 (Wave C) lands the per-PR log-event + unlabeled aggregate.

Add `docs/operator/dashboards/per-dag-cost.json` per spec §3 tile shape:

1. Stacked-bar panel "Cost USD by DAG run" — `sum by (dag_id) (rate(regatta_cost_usd_total[5m]))`.
2. Line panel "Tokens by direction" — `sum by (direction) (rate(regatta_cost_tokens_total[5m]))`.

Alarm threshold (spec §3 row 1): cost spike > 2× 7-day median per `dag_id`. Sloth-compiled alarm rule lands in A-T5; this PR just emits the counter.

**Shared-owner pin (per `feedback_shared_primitive_owner`):** A-T1 OWNS `internal/cost/spend/writer.go` across Wave A AND Wave C. C-T3 (Wave C item #11 — per-PR cost attribution) extends this same file later; C-T3 dispatch will coordinate via a single follow-up PR after A-T1 merges. Do NOT pre-empt C-T3's log-event + aggregate-counter scope here — keep this PR to the two `Float64Counter` + `Int64Counter` emit calls.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; `TestMetricCardinality_PRNumberLabelBanned` (from A-T0a) passes against this writer; dashboard JSON exists at `docs/operator/dashboards/per-dag-cost.json` with both panels; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer subagent clears; A1 + A2 + A4 from spec §8. `TestDashboardMetricNames_MatchEmitted` (drift gate) green — dashboard refs match emitter names exactly (spec §9 R2).
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0 on `docs/operator/dashboards/per-dag-cost.json` (A+1 from spec §8); property test verifies `direction` enum stays ≤ 3 distinct values over 200 synthetic spend events.

## Acceptance criteria

- [planned] c1: `internal/cost/spend/writer.go` emits `regatta.cost.usd` Float64Counter + `regatta.cost.tokens` Int64Counter after each event-row append (spec §3 item #1).
- [planned] c2: Tag set strictly `dag_id` + `operator_id` + `direction`; AST-walk lint stays green — no `pr_number`/`run_id`/`work_item_id` on meter calls (spec §2.2).
- [planned] c3: `docs/operator/dashboards/per-dag-cost.json` checked in with the two panels; both PromQL refs resolve to emitted metric names (spec §9 R2 drift gate).
- [planned] c4: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`; PR body notes the shared-owner pin handoff to C-T3.
