---
id: OBS-WAVE-C-T3
title: per-PR cost attribution log-event + aggregate counter (item #11)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T1
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-2 row item #11, §7 Wave-C table row C-T3 (shared-owner pin), §11 RISK-B (cost-per-agent rollup deferral), §9 follow-up issue #3.

## Task

Extend `internal/cost/spend/writer.go` (file owned by A-T1 across Wave A + Wave C per `feedback_shared_primitive_owner` — coordinate edit ordering: A-T1 lands first, C-T3 extends). Two additions:

1. Emit a structured log event on every spend write — carries `pr_number`, `dag_id`, `operator_id`, `usd`, `tokens`. Lives in the existing log path (e.g., `slog` / repo's logging primitive). `pr_number` is on the log only — NOT on the metric.

2. Emit an unlabeled aggregate counter:

```go
meter.Float64Counter("regatta.pr.cost_usd_total").Add(ctx, usd)
```

No tags. Cardinality = 1 series total. Per-PR attribution flows via the structured log event; the aggregate counter is just a low-overhead "total spend since boot" gauge for the dashboard headline.

Tag set on aggregate counter: none. `pr_number` strictly banned per spec §2.2 cardinality budget — log-event is the attribution surface.

Add a panel to `dashboards/grafana/cost.json` (file owned by A-T1; coordinate edit ordering): stat panel "Total cost USD (since boot)" — `regatta_pr_cost_usd_total`.

**File the §9 follow-up #3 issue at merge** (per `feedback_unaddressed_load_bearing`): `[OBS-followup] Cost-per-agent rollup (Prom recording rule OR sqlite view joining event_token_spend × dispatch trace tree on trace_id → agent_id)`. Owner: C-T3 by default; reassign at follow-up triage if scope grows. The rollup ships as a derived view (Prom recording rule OR sqlite view), NOT as a new `agent_id` label on the cost counter — adding the label would breach the cardinality budget per spec §11 RISK-B.

**Shared-owner coordination:** A-T1 is sole owner of `internal/cost/spend/writer.go` across Waves A+C. C-T3 dispatch MUST wait for A-T1 to merge. Per `feedback_shared_primitive_owner`.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; AST-walk lint passes (no `pr_number` on metric); dashboard panel checked in; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 + A2 + A4 + A5 (follow-up #3 filed) from spec §8. Log event renders with full attribution on test fixture; aggregate counter increments on every spend.
- **A+ (stretch):** A + property test verifies aggregate counter sum equals sum of per-PR log-event `usd` values across 200 synthetic spends.

## Acceptance criteria

- [planned] c1: `internal/cost/spend/writer.go` emits structured log event with `pr_number` + `dag_id` + `operator_id` + `usd` + `tokens` on every spend (spec §3 item #11).
- [planned] c2: `regatta.pr.cost_usd_total` unlabeled aggregate counter emits on every spend (cardinality = 1 series).
- [planned] c3: AST-walk lint stays green — no `pr_number` on any metric (spec §2.2).
- [planned] c4: `dashboards/grafana/cost.json` extended with total-cost stat panel (spec §9 R2).
- [planned] c5: `[OBS-followup] Cost-per-agent rollup` issue filed at merge per spec §9 follow-up #3 (spec §8 A5).
- [planned] c6: PR body cites A-T1 shared-owner pin + waits for A-T1 merge before dispatch.
- [planned] c7: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
