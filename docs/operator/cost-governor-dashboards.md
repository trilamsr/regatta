# Cost-governor dashboards

Reader: operator wiring regatta cost spans + slog events into
Honeycomb, Grafana (Tempo TraceQL), Jaeger, or another OTel-shaped
backend.
Read time: 5 minutes.
Goal: paste-ready queries for every `regatta.cost.*` span attribute
and every `obs.EventCost*` slog event, mapped to the panel shape
operators usually want.

Read the operator runbook first — [./cost-governor.md](./cost-governor.md) — for
cost-governor semantics. This doc only maps the telemetry surface to
queries; it does not explain what the caps mean or how the gate
decides.

## Span attributes mapped to panels

The `cost.evaluate` span fires once per spawnable work_item per
scheduler tick (parent: W6 `tick` span). The `cost.reconcile` span
fires once per `reconcile_interval` — default 1/hour. Spec §3.7
table is the source of truth for attribute names.

| Span attr | Span | Panel type | Honeycomb | Grafana (Tempo TraceQL) | Jaeger query |
| --- | --- | --- | --- | --- | --- |
| `regatta.cost.usd_estimate` | `cost.evaluate` | scatter / heatmap | `VISUALIZE: HEATMAP(regatta.cost.usd_estimate) WHERE name = "cost.evaluate"` | `{name="cost.evaluate"} \| select(span.regatta.cost.usd_estimate)` | service: `regatta` / operation: `cost.evaluate` / tag: `regatta.cost.usd_estimate` |
| `regatta.cost.cap_dag_usd` | `cost.evaluate` | distribution | `VISUALIZE: P95(regatta.cost.cap_dag_usd) WHERE name = "cost.evaluate"` | `{name="cost.evaluate"} \| select(span.regatta.cost.cap_dag_usd)` | tag: `regatta.cost.cap_dag_usd!=0` |
| `regatta.cost.cap_op_usd` | `cost.evaluate` | distribution | `VISUALIZE: P95(regatta.cost.cap_op_usd) WHERE name = "cost.evaluate"` | `{name="cost.evaluate"} \| select(span.regatta.cost.cap_op_usd)` | tag: `regatta.cost.cap_op_usd!=0` |
| `regatta.cost.allow` | `cost.evaluate` | rate | `VISUALIZE: RATE_AVG WHERE name = "cost.evaluate" AND regatta.cost.allow = false` | `{name="cost.evaluate" && span.regatta.cost.allow = false}` | tag: `regatta.cost.allow=false` |
| `regatta.cost.soft_breached` | `cost.evaluate` | rate | `VISUALIZE: RATE_AVG WHERE name = "cost.evaluate" AND regatta.cost.soft_breached = true` | `{name="cost.evaluate" && span.regatta.cost.soft_breached = true}` | tag: `regatta.cost.soft_breached=true` |
| `regatta.cost.period_start` | `cost.reconcile` | timeline | `WHERE name = "cost.reconcile" GROUP BY regatta.cost.period_start` | `{name="cost.reconcile"} \| select(span.regatta.cost.period_start)` | operation: `cost.reconcile` |
| `regatta.cost.period_end` | `cost.reconcile` | timeline | `WHERE name = "cost.reconcile" GROUP BY regatta.cost.period_end` | `{name="cost.reconcile"} \| select(span.regatta.cost.period_end)` | operation: `cost.reconcile` |
| `regatta.cost.drift_pct` | `cost.reconcile` | scatter + threshold | `VISUALIZE: MAX(regatta.cost.drift_pct) WHERE name = "cost.reconcile"` overlay `drift_alert_threshold_pct` | `{name="cost.reconcile" && span.regatta.cost.drift_pct > 10}` | tag: `regatta.cost.drift_pct>10` |
| `regatta.cost.api_source` | `cost.reconcile` | rate by attr-value | `WHERE name = "cost.reconcile" GROUP BY regatta.cost.api_source` | `{name="cost.reconcile"} \| select(span.regatta.cost.api_source)` | tag: `regatta.cost.api_source=usage_fallback` |

## Slog events mapped to alerts

The cost-governor exports four load-bearing `obs.EventCost*` slog
event names from `internal/obs/events.go`. Operators wire each to an
alerting policy per the recommendations below.

| Slog event | Severity | Recommended alert | Panel cite |
| --- | --- | --- | --- |
| `obs.EventCostReconcileFailing` | ERROR | Page on-call after 4h of continuous fire (suppress prior to that — backoff is in effect). | Reconciler health tile (drift-pct timeline goes flat). |
| `obs.EventCostReconcileSkipped` | WARN | Ticket if env-var ever unset in production. Pre-call deny continues against recorded spend; safe but degraded. | `api_source` rate; absent rows indicate skipped ticks. |
| `obs.EventCostReconcileFallback` | WARN | Trend-line dashboard tile. A spike means Cost API outage; sustained fire means pricing-table drift is invisible. | `regatta.cost.api_source = "usage_fallback"` rate. |
| `obs.EventCostDriftAlert` | WARN | Page on `drift_pct > 25` for ≥ 3 consecutive ticks; ticket on every fire below that. | `regatta.cost.drift_pct` scatter with threshold overlay. |
| `obs.EventCostSoftCapBreached` | INFO | Dashboard tile only — never page. Soft-cap is advisory by design. | `regatta.cost.soft_breached = true` rate on `cost.evaluate`. |

Note: `EventCostSoftCapBreached` surfaces today via the
`regatta.cost.soft_breached` span attribute on `cost.evaluate`. The
slog-event-constant promotion is a follow-up — track #289 for the
W6 T2 slog→OTel error-log bridge promotion. The panel works today
via the span attribute; the alert recommendation lands when the
slog constant ships.

## Suggested dashboard layout

Four tiles cover the operator's day-1 question set ("am I spending,
am I getting denied, is the reconciler healthy, are caps about to
fire"). Each tile cites the underlying attr / event verbatim so the
dashboard JSON is reproducible.

1. **Spend rate** — heatmap of `regatta.cost.usd_estimate` on
   `cost.evaluate`. Reads the gate's upper-bound estimate; high
   density = high pre-call spend rate.
2. **Cap denials** — rate of `regatta.cost.allow = false` on
   `cost.evaluate`. A sudden spike is the early-warning that caps
   are too tight or a runaway DAG kicked off.
3. **Drift** — scatter of `regatta.cost.drift_pct` on
   `cost.reconcile` with the `drift_alert_threshold_pct` value
   overlaid as a constant line. Sustained over-threshold = the
   `obs.EventCostDriftAlert` story.
4. **Reconciler health** — rate of `obs.EventCostReconcileFailing`
   ERROR slog. Page-worthy when continuous; ignorable during a known
   Anthropic incident.

Auto-provision for Honeycomb is tracked at #291.

## Sampling and cost dashboards

The `cost.evaluate` span fires per work_item per tick. Cardinality is
bounded by `lane_cap × num_lanes` per spec §9 R14, so dashboards stay
bounded even at high tick rates. For deployments with `lane_cap ×
num_lanes > 20` or `tick_interval < 5s`, sample down via
`OTEL_TRACES_SAMPLER=parentbased_traceidratio` with `OTEL_TRACES_SAMPLER_ARG=0.01`.
The drop-sampled spans do not affect enforcement correctness; only
dashboard density changes.

The `cost.reconcile` span fires 1/hour by default — operators should
NEVER sample these out. Set the sampler-arg only at the parent-tick
level; the reconciler tick inherits its own ratio (1.0) via the
parent context.

Sampler details live at
[./observability.md#sampler-customization](./observability.md#sampler-customization).

## Cross-references

- Operator runbook (cost-governor semantics): [./cost-governor.md](./cost-governor.md).
- Incident playbook (on-call triage): [../engineer/runbooks/cost-governor-incidents.md](../engineer/runbooks/cost-governor-incidents.md).
- W6 observability surface (general OTel wiring): [./observability.md](./observability.md).
- Design spec (engineering reference): [../engineer/specs/2026-06-01-cost-governor-design.md](../engineer/specs/2026-06-01-cost-governor-design.md).
