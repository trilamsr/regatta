# Metrics, dashboards, and SLOs

Reader: customer-operator who already has the OTel trace + log pipe wired
per [observability.md](observability.md) and now wants to wire metrics,
import the Grafana dashboards, and stand up the two SLO alerts.
Read time: 12 minutes.
Goal: pick the metric wire format, provision the dashboards, understand
the cardinality budget that protects your bill, and tune the
trace-sampling knobs.
Expires when: spec
[`docs/engineer/specs/2026-06-02-observability-roadmap.md`](../engineer/specs/2026-06-02-observability-roadmap.md)
§2 (metric naming + tag schema + sampling policy) changes.

## 1. Pick a wire format: OTLP push vs Prometheus pull

Regatta wires both exporter shapes from one MeterProvider —
[`internal/obs/otel/meter.go`](../../internal/obs/otel/meter.go)
`SetupMeter`. Operator picks one at boot via env var:

| Env var | Wire format | When to pick |
|---|---|---|
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` (or `OTEL_EXPORTER_OTLP_ENDPOINT`) | OTLP/gRPC push | Honeycomb, Datadog, Grafana Cloud OTLP, any managed backend that accepts OTLP. Push model survives behind NAT. |
| `OTEL_METRICS_PROMETHEUS_PORT` | Prometheus pull on `/metrics` | Self-host with a Prometheus or VictoriaMetrics server scraping regatta directly. No collector tier needed. |

Both transport the same metric set verbatim — every histogram, counter,
and label is identical on the wire regardless of which exporter wins.
Swapping is an env-var change, not a code change.

**Both set is operator error.** `SetupMeter` returns
`ErrOTelMetricExporterConflict` and refuses to boot when both vars are
non-empty, because two wire formats from one process would double-emit
measurements with divergent unit-suffix rules (Prometheus appends
`_total` on counters, OTLP does not). Pinned by
`TestMeterSetup_BothExportersSet_ReturnsConflict` in
[`internal/obs/otel/meter_test.go`](../../internal/obs/otel/meter_test.go).

**Neither set is also valid** — `SetupMeter` returns a no-op shutdown
and the OTel SDK's default noop MeterProvider wins. The binary opens
no sockets and starts no goroutines, so the metric layer costs zero
when an operator has not opted in.

Verify by running:

```sh
OTEL_METRICS_PROMETHEUS_PORT=29317 regatta serve &
curl -s localhost:29317/metrics | grep regatta_
```

## 2. Sample `/metrics` scrape (Prometheus exporter)

The OTel Prometheus exporter renders dotted meter names with
underscores and appends `_total` on counters or
`_bucket`/`_count`/`_sum` on histograms. Today the in-code instruments
are constructed WITHOUT `metric.WithUnit(...)`, so the unit-suffix
double-render path is dormant — `regatta.scheduler.tick.latency_ms`
renders as `regatta_scheduler_tick_latency_ms_bucket` (single suffix).
Every dashboard JSON and Sloth SLI in `dashboards/prometheus/rules/`
references this single-suffix shape verbatim.

Captured by A-T0a against `OTEL_METRICS_PROMETHEUS_PORT=29317` with
one `regatta.cost.usd` counter increment:

```
# HELP regatta_cost_usd_total
# TYPE regatta_cost_usd_total counter
regatta_cost_usd_total{otel_scope_name="github.com/trilamsr/regatta/internal/cost/spend",otel_scope_schema_url="",otel_scope_version=""} 0.012345
# HELP target_info Target metadata
# TYPE target_info gauge
target_info{...,regatta_tenant_id="default",service_name="regatta",telemetry_sdk_language="go",telemetry_sdk_name="opentelemetry",telemetry_sdk_version="1.44.0"} 1
```

The `target_info` gauge carries the resource attributes
(`service.name`, `regatta.tenant_id`, SDK identity) so a Grafana panel
can join `target_info` on `instance` to surface deployment metadata
without per-metric label duplication.

If a future OTel SDK release changes the double-suffix behaviour, the
existing dashboards drift in one direction (queries miss data). The
follow-up tag is `[OBS-followup]` so an operator can grep the issue
tracker for a re-normalization PR.

Verify by running:

```sh
OTEL_METRICS_PROMETHEUS_PORT=29317 regatta serve &
curl -s localhost:29317/metrics | head -20
```

## 3. Provisioning the dashboards

Seven Grafana JSON dashboards live under
[`docs/operator/dashboards/`](dashboards/README.md), one per metric
tier landed in Wave-A through Wave-D. The
[dashboards/README.md](dashboards/README.md) index lists each file's
metric reference and SLO link.

Manual import path (Grafana 10.x UI): Dashboards → New → Import →
Upload JSON. Pick a Prometheus datasource when prompted. The OTLP-side
equivalent is to point the OTel Collector's Prometheus exporter at the
same Grafana datasource and use the same JSONs unchanged.

Automated import path: `make provision-dashboards` (future target — see #523) posts each JSON
under `docs/operator/dashboards/` to the Grafana HTTP API
(`POST /api/dashboards/db`). Two env vars:

| Env var | Purpose |
|---|---|
| `GRAFANA_URL` | Base URL of the Grafana instance (e.g. `http://grafana:3000`). |
| `GRAFANA_API_TOKEN` | Service-account token with dashboard write scope. |

The per-PR drift gate `TestDashboardMetricNames_MatchEmitted` in
[`internal/cost/spend/writer_metrics_test.go`](../../internal/cost/spend/writer_metrics_test.go)
asserts every metric name a dashboard JSON references matches an
emitter call site. A dashboard editor who renames a panel query to
a metric the code does not emit fails CI before the dashboard
ships — the gate covers spec §9 R2 (dashboard drift vs emitted
metrics).

Verify by running:

```sh
GRAFANA_URL=http://localhost:3000 GRAFANA_API_TOKEN=... make provision-dashboards  # NOT YET WIRED — manual import for now; see #523
```

## 4. SLOs and alert runbooks

Wave-A ships two SLOs that page on multi-burn-rate breach. Both source
from histograms the metric foundation emits; both compile from OpenSLO
YAML via Sloth to Prometheus recording + alert rules.

| SLO | Objective | Window | Error budget | SLI | Runbook |
|---|---|---|---|---|---|
| SLO-1 — Scheduler tick latency | p95 ≤ 5 s | 7d rolling | 5% of ticks | `histogram_quantile(0.95, rate(regatta_scheduler_tick_latency_ms_bucket[5m])) <= 5000` | [scheduler-tick runbook](runbooks/scheduler-tick.md) |
| SLO-2 — L4 gate latency | p95 ≤ 30 s | 7d rolling | 1% of L4 invocations | `histogram_quantile(0.95, rate(regatta_l4_latency_ms_bucket[5m]))` | [l4-latency runbook](runbooks/l4-latency.md) |

**Multi-burn-rate alerting.** The compiled Sloth output at
`dashboards/prometheus/rules/{scheduler-tick,l4-latency}.yaml`
generates 4 burn-rate tiers per `tools/sloth/windows/7d.yaml`:
13.44× (1h window, page), 3.5× (6h window, page), 1.4× (1d window,
ticket), 0.98× (3d window, ticket). The two-page-tier pattern catches
fast outages within an hour and partial-degradation patterns that
would otherwise drain the budget over a day.

**Sloth version pin.** Spec §9 R3: Sloth major bumps rewrite burn-rate
window arithmetic, which would change the alert semantics silently. The
version pin lives at `tools/sloth/version` and `make slo-compile` reads
it; the compiled output under `dashboards/prometheus/rules/` is
checked in, so a version bump produces a reviewable diff in the bump
PR rather than a quiet runtime change.

**SLO-2 tuning followup.** The 1%/7d L4 budget burns in roughly 50
minutes during a real LLM-provider tail (5-10× normal latency for
30-60 min is common). `[OBS-followup]` is filed to widen the budget
to 5% or extend the window to 28 days once Wave-B has 30 days of
real burn-rate data — see [the obs-roadmap spec](../engineer/specs/2026-06-02-observability-roadmap.md)
§5.

Verify by running:

```sh
make slo-compile && ls dashboards/prometheus/rules/
```

## 5. Cardinality budget and the AST-walk lint

Every metric tag carries a documented cardinality cap in
[spec §2.2](../engineer/specs/2026-06-02-observability-roadmap.md).
Three labels are **banned on metrics** because they are unbounded or
high-cardinality in steady state:

| Label | Why banned on metrics |
|---|---|
| `pr_number` | **UNBOUNDED** — grows with every GitHub PR forever. Per-PR cost is a log event with `trace_id` correlation (see spec §4); metric is `regatta.pr.*` aggregated over time without per-PR labels. |
| `run_id` | **HIGH-CARD** — one value per scheduler run; thousands per day in steady state. Lives on spans + logs only; metric correlation is via `trace_id`. |
| `work_item_id` | **HIGH-CARD** — one value per row touched per tick; saturates a backend index within hours. Lives on spans + logs only. |

The hard rule from spec §2.2: any tag that may exceed 200 distinct
values over a 7-day rolling window is banned on metrics. Such
dimensions emit as log events tagged with `trace_id`; the operator
drills metric → exemplar → trace → log.

**Why log + trace correlation beats high-card labels.** The metric
backend (Prom, Mimir, Tempo's Prometheus surface) indexes every
distinct label combination. A `pr_number` label adds one new series
per PR; at 100 PRs/week the histogram series count grows by ~5,000
per year, multiplied by every other label combination on the metric.
Log events carry the same dimension without index pressure because
log search is full-scan over the relevant window, not a label index.

**AST-walk enforcement.** The lint test
`TestMetricCardinality_PRNumberLabelBanned` lives at
[`internal/obs/otel/cardinality_lint_test.go`](../../internal/obs/otel/cardinality_lint_test.go).
It walks every non-test Go file under `internal/`, parses each
`meter.*Counter` / `*Histogram` / `*Gauge` call site, and fails when
a banned label literal appears in the instrument's attribute set.
The lint blocks the offending PR at `make check` time — no banned
label reaches `main`.

Verify by running:

```sh
go test ./internal/obs/otel/ -run TestMetricCardinality_PRNumberLabelBanned -v
```

## 6. Trace head-sampling knobs

Metrics carry the cardinality budget (§5 above). Traces carry their own
volume budget: scheduler ticks at sub-second cadence × per-step spans ×
per-agent fan-out push past 10⁴ spans/sec in steady state. Spec §2.5
pins a head-sampling policy at the SDK so the OTLP pipe stays within
the backend ingest budget.

**Default policy.** `ParentBased(TraceIDRatioBased(0.1))` — sample 10%
of root spans; every span in the same trace inherits the parent's
sampled bit (sticky-per-trace). Override `p` via
`OTEL_TRACES_SAMPLER_ARG`; the default `0.1` matches spec §2.5.

**Always-on override for high-signal traces.** The `ErrorOverrideSampler`
wraps the parent sampler and returns `RecordAndSample` whenever:

- a span has the `error.type` attribute set (any error in the trace
  flips the whole trace to 100%), OR
- a span originates in `internal/orchestrator/state/substrate/sign.go`
  (HMAC chain-verify), OR
- a span originates in the substrate divergence-audit path
  (`divergence_emit`).

Real incidents (chain breaks, HMAC failures, divergence rows) ship at
100% even when steady-state sampling is 10%. Pinned by
[`internal/obs/otel/sampler_test.go`](../../internal/obs/otel/sampler_test.go).

**Operator overrides** (read by the OTel Go SDK directly, no regatta
wrapper):

| Env var | Effect |
|---|---|
| `OTEL_TRACES_SAMPLER=parentbased_traceidratio` + `OTEL_TRACES_SAMPLER_ARG=0.1` | Default — 10% head-sampling. |
| `OTEL_TRACES_SAMPLER=always_on` | Debug fidelity (every span ships). Expect a per-second OTLP traffic spike of 10× and a proportional backend cost increase. |
| `OTEL_TRACES_SAMPLER=always_off` | Emergency cost-stop (no spans ship). Use during a runaway-cost incident when shedding trace volume buys the operator time to fix root cause. |

The error-override semantics survive the operator setting `always_off`
**only** when the override sits on top of the parent sampler — the
SDK-level `always_off` is a hard kill switch and the error override
does not re-enable it. If a chain-break debug session is needed during
an `always_off` cost-stop, the operator must temporarily flip back to
`always_on` and bound the window with a follow-up cost reconciliation.

**Why head-sampling, not tail-sampling.** Tail-sampling needs an OTel
Collector tier buffering full traces before deciding. Self-host
default has no collector — direct OTLP to backend. Head-sampling at
the SDK is the OTel-idiomatic shape for that topology. Operators who
want tail-sampling install an OTel Collector and point regatta's OTLP
at it; no code change in regatta.

Verify by running:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 \
  OTEL_TRACES_SAMPLER=parentbased_traceidratio \
  OTEL_TRACES_SAMPLER_ARG=0.5 regatta serve
```

## 7. Where the spec lives

Single source of truth for what regatta emits, what env vars it reads,
what tags appear on each metric, and what sampling policy ships:
[docs/engineer/specs/2026-06-02-observability-roadmap.md](../engineer/specs/2026-06-02-observability-roadmap.md).

When this doc drifts from the spec, the spec wins; file an issue with
the `[OBS-followup]` tag.
