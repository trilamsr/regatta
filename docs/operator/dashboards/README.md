# Grafana dashboards (obs roadmap §6.4)

Six dashboard JSON files realize the per-item dashboard tiles committed in
`docs/engineer/specs/2026-06-02-observability-roadmap.md` §3. Metric names and
label sets match §2.1 / §2.2 verbatim — drift between dashboard query and
emitted metric is caught by the CI test `TestDashboardMetricNames_MatchEmitted`
once Wave-A lands (§4 anti-pattern #6).

## Index

| File | Tier | Spec ref | Tile |
|---|---|---|---|
| [`per-dag-cost.json`](per-dag-cost.json) | Tier 1 (Wave A) | §3 item #1 | Per-DAG-run cost — slice `regatta.cost.usd` by `dag_id` / `operator_id` / `lane`; tokens by `direction` |
| [`l4-gate.json`](l4-gate.json) | Tier 1 (Wave A) | §3 item #2 | L4 gate invocations by verdict, p50/p95/p99 latency, cache hit ratio, second-opinion fire-rate, per-category stack |
| [`scheduler-tick.json`](scheduler-tick.json) | Tier 1 (Wave A) | §3 item #4 / SLO-1 | Scheduler tick latency p50/p95/p99, heatmap, per-step p95 breakdown |
| [`substrate-event-rate.json`](substrate-event-rate.json) | Tier 2 (Wave B) | §3 item #5 / SLO-3 | Event rate by kind, chain-break counter, divergence-detection counter, ±3σ baseline band |
| [`pr-lifecycle.json`](pr-lifecycle.json) | Tier 3 (Wave C) | §3 item #10 | PR stage duration heatmap, per-stage p50/p95, dispatch→first-commit alarm tile, merge-rate KPI |
| [`trigger-clock.json`](trigger-clock.json) | Tier 4 (Wave D) | §3 item #15 | Days-remaining gauge by trigger (`30_day_green`, `external_customer_signal`, `phase_g_gate`), 30-day trend, snapshot table |

## Import instructions

Per Grafana v10 import flow
(<https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/import-dashboards/>):

1. Grafana UI → Dashboards → New → Import → Upload JSON file.
2. Select the file under `docs/operator/dashboards/`.
3. Pick a Prometheus datasource for the `datasource` variable when prompted
   (each panel reads `${datasource}` so the choice cascades to every query).
4. Save.

Automated path (lands with Wave-A): `make provision-dashboards` calls the
Grafana HTTP API (`POST /api/dashboards/db`) to upsert each JSON.

## Wire-name note

The OTel Prom exporter renders dotted metric names with underscores and
appends `_total` on counters / `_bucket` on histograms (§2.1). Dashboard
PromQL queries use the rendered wire form, e.g.:

- meter API: `regatta.cost.usd` → PromQL: `regatta_cost_usd_total`
- meter API: `regatta.scheduler.tick.latency_ms` → PromQL:
  `regatta_scheduler_tick_latency_ms_bucket` (double-unit-suffix accepted per
  §2.1).

## Schema

Grafana dashboard schema v39 (Grafana 10.x). The forthcoming
`tools/grafana-schema/` vendored schema (§6.4) gates this directory in CI.
