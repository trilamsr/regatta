---
title: "Observability metric-layer + operator-surface roadmap — Design Spec (converged)"
status: active
phase: x-forward-fit
summary: "Observability roadmap — converged metric-layer + operator-surface plan. References Phase-X seams (Sigstore, blackboard) as forward-fits."
---

# Observability metric-layer + operator-surface roadmap — Design Spec (converged)

Status: ready for review (converged)
Date: 2026-06-02
Author: design subagent (research+design + amendments + second-tier review folded)
Issue umbrella: #730
Depends on: #159 umbrella (W6 OTel backbone — span/log signal shipped); #224 (substrate event log + HMAC chain); #283 (token_spend writer); #381/#380/#388/#385 (L4 gate cache + second-opinion + per-category + auto-fix); #382/#391/#394 (crash-recovery property infra)
Binding brief: operator-brief "15-item observability roadmap" (2026-06-02 session)
Roadmap fit: extends W6 trace+log signal with the metric signal, the SLO layer, and the operator-surface layer (`regatta status` + daily digest + trigger-clock). Prerequisite for the 30-day-green Phase-S → Phase-G trigger gauge.
Trap patterns: cardinality (per `feedback_research_design_principles` §"unbounded tag explosion"); metric-vs-log confusion; dashboard-vs-code drift; alarm fatigue.
Memory rules in force: `feedback_research_design_principles` (adopt OSS, ≥2 candidates per primitive), `feedback_decision_priority` (UX > ease > performance > best-prac > velocity; long-term > short-term), `feedback_grade_rubric` (B/A/A+ tool-checkable), `feedback_adversarial_review`, `feedback_pr_body_release_notes_mandatory`, `feedback_pr_body_file_only`, `feedback_test_godoc_one_line`, `feedback_self_improvement`, `feedback_design_iteration_local`.

OTel semantic conventions cited verbatim from: <https://opentelemetry.io/docs/specs/semconv/>.

**Convergence note.** This spec is the converged shape after iteration. It folds: PR #400 (original roadmap), PR #405 (review of #400 — 1 BLOCKER + 4 RISK), PR #410 (amendments closing the BLOCKER + RISKs in-band), PR #413 (second-tier review of #410 — ADOPT with one deferred RISK), and PR #420 (8 Wave-A dispatch-ready briefs). Diff-shaped amendment blocks have been inlined as flush prose; the dep-graph fix (D-T3 depends on C-T2) is applied throughout. The single deferred RISK from PR #413 (file-ownership seam between A-T0a and A-T1 on `internal/cost/spend`) is captured under "Open at impl time" below.

---

## §1 Prior art (adoption-first per `feedback_research_design_principles`)

Each primitive scored on: maintenance trajectory, ecosystem alignment, regatta-fit, swap cost. Scores are 1–5 (5 = strong adopt).

### 1.1 Trace + log signal — ALREADY ADOPTED (W6, #159)

OTel Go SDK + GenAI semconv + Jaeger E2E shipped in 2026-05. Not re-litigated; this spec extends with the metric signal on the same SDK.

### 1.2 Metric signal — which SDK + which export shape?

| Candidate | Maintenance | Ecosystem | Regatta-fit | Swap cost | Score | Verdict |
|---|---|---|---|---|---|---|
| **OTel metrics SDK** (`go.opentelemetry.io/otel/metric` + `sdk/metric`) | 5 (matches W6 SDK; major v1 stable) | 5 (every backend ingests OTLP) | 5 (counter/histogram/gauge cover all 15 items) | 5 (single SDK; one `Setup` call gains exporter) | 25/25 | **ADOPT** |
| Prometheus client_golang | 4 (mature; CNCF) | 4 (Prom + Grafana native) | 3 (parallel SDK; doubles dep surface vs single OTel SDK) | 3 (already have OTel; second SDK is net-new) | 14/20 | reject — second SDK |
| Tally (Uber) | 2 (low maintenance velocity) | 2 | 2 | 1 | 7/20 | reject |
| go-metrics (rcrowley) | 1 (archived) | 1 | 1 | 1 | 4/20 | reject |

**Decision:** OTel metrics SDK. Bridges to Prometheus and to Honeycomb already exist upstream (`go.opentelemetry.io/otel/exporters/prometheus`, `otlpmetricgrpc`); the operator picks the wire format.

### 1.3 Backend storage — Prom vs Honeycomb vs Tempo+Mimir

| Candidate | Maintenance | Ecosystem | Regatta-fit | Swap cost | Score | Verdict |
|---|---|---|---|---|---|---|
| **Prometheus + Grafana** (self-host default) | 5 (CNCF graduated) | 5 (universal) | 5 (single-tenant self-host fits Prom's pull model) | 4 (OTel exporter → Prom is a 1-line config) | 24/25 | **ADOPT for self-host** |
| Grafana Mimir (long-term storage) | 4 (Grafana Labs; growing) | 4 | 3 (overkill for self-host; useful at Phase X multi-tenant) | 3 | 14/20 | adopt later (Phase X) |
| Honeycomb (structured events) | 5 (vendor, well-funded) | 4 (less Grafana-native) | 4 (event-shape suits agent loop) | 3 (OTLP → Honeycomb is 1-line) | 16/20 | adopt as second exporter (operator choice) |
| Datadog | 5 | 4 | 3 | 3 | 15/20 | operator choice — no special wiring |

**Decision:** Prometheus + Grafana is the canonical self-host pairing; Honeycomb supported via OTLP env-var swap (zero regatta code change).

### 1.4 Dashboard-as-code — Grafana JSON vs grafonnet vs Terraform

| Candidate | Maintenance | Ecosystem | Regatta-fit | Swap cost | Score | Verdict |
|---|---|---|---|---|---|---|
| **Grafana dashboard JSON** (checked-in `dashboards/*.json`) | 5 | 5 | 5 (zero new tooling; `make` provision via API) | 5 | 25/25 | **ADOPT** |
| grafonnet (jsonnet DSL) | 3 (Grafana Labs; slower release cadence) | 3 (jsonnet adoption uneven) | 3 (adds jsonnet toolchain) | 4 | 13/20 | reject — extra toolchain |
| Terraform `grafana_dashboard` | 4 | 4 | 3 (terraform state for one tool) | 3 | 14/20 | reject — state overhead |

**Decision:** Plain Grafana JSON in repo at `docs/operator/dashboards/*.json`. CI lints against the Grafana JSON schema. Single `make provision-dashboards` calls the Grafana HTTP API.

### 1.5 Log aggregation — Loki vs Fluent-bit vs Vector

| Candidate | Maintenance | Ecosystem | Regatta-fit | Swap cost | Score | Verdict |
|---|---|---|---|---|---|---|
| **OTLP logs → operator backend** (already in W6 via slog bridge) | 5 | 5 | 5 (single signal pipeline) | 5 | 25/25 | **KEEP (W6)** |
| Loki | 4 (Grafana Labs) | 4 | 3 (separate log pipeline) | 3 | 14/20 | operator choice — no regatta wiring |
| Fluent-bit | 4 | 4 | 3 (transport agent — extra hop) | 3 | 14/20 | operator choice |
| Vector | 4 (Datadog OSS) | 3 | 3 | 3 | 13/20 | operator choice |

**Decision:** keep W6's OTel-logs export. Loki/Fluent-bit/Vector are operator-side concerns; regatta emits OTLP and stops there.

### 1.6 Trace storage — Tempo vs Jaeger

W6 already chose Jaeger for the dev fixture. Tempo is supported via OTLP env-var swap (zero regatta code). Operator picks.

### 1.7 SLO definition — Sloth vs OpenSLO

| Candidate | Maintenance | Ecosystem | Regatta-fit | Swap cost | Score | Verdict |
|---|---|---|---|---|---|---|
| **OpenSLO** (spec, `openslo/openslo`) | 4 (CNCF Sandbox) | 4 (multi-vendor) | 5 (vendor-neutral YAML; survives backend swap) | 5 | 18/20 | **ADOPT** for SLO definitions |
| Sloth (PromQL generator from OpenSLO) | 4 | 4 | 5 (generates Prom recording + alert rules from OpenSLO YAML) | 5 | 18/20 | **ADOPT** as the SLO compiler |
| Nobl9 | 5 (vendor) | 3 | 2 (SaaS lock-in) | 2 | 12/20 | reject |
| Hand-rolled PromQL | n/a | n/a | 2 (rewrite per backend) | 2 | 6/20 | reject |

**Decision:** OpenSLO YAML at `slo/*.yaml`; Sloth compiles to Prom recording + alert rules into `dashboards/prometheus/rules/*.yaml`.

### 1.8 Canonical stack (one line)

**OTel SDK (trace+log+metric) → OTLP → Prometheus + Grafana (self-host default) / Honeycomb (operator swap via env var); dashboards-as-JSON; OpenSLO + Sloth for SLO compilation; W6 unchanged.**

### 1.9 TUI library — bubbletea vs tview vs tcell

D-T2 lands `regatta status` as a TUI; the TUI library is its own primitive and deserves a scored comparison rather than the bare pick that PR #400 carried.

| Candidate | Maintenance | Ecosystem | Regatta-fit | Swap cost | Score | Verdict |
|---|---|---|---|---|---|---|
| **`charmbracelet/bubbletea`** (Elm architecture, Go) | 5 (Charm; active) | 5 (large CLI ecosystem) | 5 (component model maps to §6.1 5-panel budget; renders + tests cleanly; indirect `charmbracelet/*` deps already in `go.mod`) | 4 (one new direct dep; D-T2 adds) | 19/20 | **ADOPT** |
| `rivo/tview` | 4 (active) | 3 | 3 (widget-tree model; less natural for the 5-panel single-screen budget; harder to unit-test) | 3 | 13/20 | reject — less testable |
| `gdamore/tcell` (lower-level primitive) | 5 | 4 | 2 (hand-roll the rendering loop; spec §6.1 explicit "single-screen" budget forces a layout layer we'd otherwise write) | 2 | 13/20 | reject — too low-level |
| hand-rolled ANSI | n/a | n/a | 1 | 1 | 3/20 | reject |

**Decision:** `charmbracelet/bubbletea`. Added to `go.mod` by D-T2; not present today (the indirect `charmbracelet/colorprofile`/`ultraviolet`/`x/ansi`/`x/term`/`x/termios` deps are from other usage and do not satisfy the direct-dep requirement).

---

## §2 Standardization (metric naming + tag schema + cardinality budget)

Cite: OTel semantic conventions — <https://opentelemetry.io/docs/specs/semconv/> (general attributes + GenAI conventions). The metric-naming rule below extends OTel's `<namespace>.<entity>.<action>.<unit>` convention to the regatta surface.

### 2.1 Metric naming convention

`regatta.<surface>.<action>.<unit>` — lowercase, dot-separated, snake_case within segments only when the noun is multi-word (`work_item`).

| Surface | Examples |
|---|---|
| `regatta.l4.*` | `regatta.l4.invocations` (counter), `regatta.l4.latency_ms` (histogram), `regatta.l4.cache.hits` (counter) |
| `regatta.scheduler.*` | `regatta.scheduler.tick.latency_ms` (histogram), `regatta.scheduler.tick.duration_per_step_ms` (histogram, tag=`step`) |
| `regatta.substrate.*` | `regatta.substrate.events.appended` (counter), `regatta.substrate.chain.break` (counter), `regatta.substrate.divergence.detected` (counter) |
| `regatta.cost.*` | `regatta.cost.usd` (counter), `regatta.cost.tokens` (counter, tag=`direction`) |
| `regatta.dispatch.*` | `regatta.dispatch.subagents` (counter, tag=`template`+`task_type`), `regatta.dispatch.failure` (counter, tag=`mode`) |
| `regatta.pr.*` | `regatta.pr.stage_duration_seconds` (histogram, tag=`stage`), `regatta.pr.cost_usd` (counter, tag=`pr_number`) |
| `regatta.replay.*` | `regatta.replay.latency_ms` (histogram) |
| `regatta.adversarial.*` | `regatta.adversarial.findings` (counter, tag=`fate`) |

Unit suffixes follow OTel: `_ms` (milliseconds), `_seconds`, `_usd` (regatta-specific), `_bytes`, or no suffix for dimensionless counters. Histograms always carry a unit. Dropped: the `_total` Prom suffix — OTel exporter appends it on the wire for Prom compat.

**Prom exporter double-unit-suffix behaviour (locked).** The OTel Prom exporter appends the SDK unit (`ms`/`s`/`By`) to the metric name on the wire when both the metric name carries a unit suffix AND the meter API `unit` argument is set. Example: `regatta.scheduler.tick.latency_ms` declared with `Float64Histogram(name="regatta.scheduler.tick.latency_ms", unit="ms")` renders as `regatta_scheduler_tick_latency_ms_milliseconds` on `/metrics` scrape. Spec accepts this double-unit wire string (keep the in-code suffix, let the exporter double-render) because every dashboard tile and SLO PromQL expression in §3 + §5 already references the suffixed name. The alternative — drop the suffix from the name and rely solely on the `unit` argument — would force a rewrite of every dashboard JSON and every Sloth SLI before A-T5 lands. A-T0a's PR body MUST show one sample `/metrics` scrape line for `regatta.scheduler.tick.latency_ms` so the wire shape is locked at landing. If a future OTel SDK release changes the double-suffix behaviour, file `[OBS-followup]` to re-normalize. Unit `usd` (regatta-specific; no UCUM code) is passed as the literal string `"usd"`; downstream Prom histograms render `unit=""` which is acceptable for a fiat-denominated counter (no UCUM equivalent exists).

### 2.2 Canonical tag (label) set

Every metric carries the tags below where applicable. Tag names match OTel resource + span-attribute conventions where possible.

| Tag | Type | Cardinality budget | Source | Notes |
|---|---|---|---|---|
| `service.name` | resource | 1 | always `regatta` | OTel resource attr |
| `service.version` | resource | low (~1/release) | `buildinfo.Version` | OTel resource attr |
| `regatta.tenant_id` | resource | 1 self-host; high at Phase X | W6 constant `default` | hand-off to W8 |
| `dag_id` | metric label | ≤ 50 | program id | bounded by `.regatta/plans/*.yaml` |
| `operator_id` | metric label | ≤ 100 | adapter config | operator-defined |
| `run_id` | metric label | **HIGH-CARD — banned on metrics** | trace_id | use trace correlation instead; only on spans/logs |
| `work_item_id` | metric label | **HIGH-CARD — banned on metrics** | row id | use trace correlation; only on spans/logs |
| `lane` | metric label | ≤ 20 | scheduler lane | bounded |
| `agent_id` | metric label | ≤ 50 | spawner agent id | bounded by adapter |
| `pr_number` | metric label | **UNBOUNDED — banned on metrics** | github | use log+trace correlation; metric is `regatta.pr.*` over time without per-PR labels (per-PR cost is a log event, see §4) |
| `severity` | metric label | low (`info`/`warn`/`error`/`critical`) | enum | bounded |
| `category` | metric label | low (≤ 12 L4 categories; see #388) | enum | bounded |
| `template` | metric label | ≤ 30 (dispatch templates) | adapter | bounded |
| `task_type` | metric label | ≤ 20 | implementer/reviewer/spec | bounded |
| `gate_name` | metric label | low (l0/l4/approval/cost) | enum | bounded |
| `verdict` | metric label | ≤ 5 (allow/deny/needs_review/escalate/skip) | enum | bounded |
| `fate` | metric label | 4 (filed/dismissed/auto_fixed/superseded) | adversarial-finding enum | bounded |
| `stage` | metric label | 5 (dispatch/first_commit/pr_open/ci_green/merge) | PR-lifecycle enum | bounded |
| `mode` | metric label | ≤ 20 (subagent failure-mode taxonomy) | enum | bounded |

**Cardinality budget — hard cap.** Any tag that may exceed 200 distinct values within a 7-day rolling window is banned on metrics (`run_id`, `work_item_id`, `pr_number`, full error messages, SQL, free-text). Such dimensions are emitted as log events with `trace_id` correlation; the operator drills from metric → trace → log.

### 2.3 Trace span shape

Inherited from W6 unchanged. Every component constructor takes `Config.Tracer`. New for this spec:

- Every loop-style body (scheduler tick step, reducer fold) gets ONE span around the loop + a counter for iteration count — NOT one span per iteration (per §4 anti-pattern "span-per-loop").
- Span status codes use OTel semconv: `Ok` on success, `Error` on terminal failure with `error.type` attribute set per OTel error-recording spec.

### 2.4 Implementation seam

A single new file `internal/obs/otel/meter.go` exports:

```go
// Setup() returned MeterProvider initializes here; consumers resolve
// otel.Meter("<component-name>") same way the W6 Tracer DI works.
//
// New Config field on EVERY existing Config struct that already carries
// Config.Tracer (8 components):
//
//   Meter metric.Meter   // nil → otel.Meter("<component>") (noop until Setup runs)
//
// Per feedback_spec_pattern_authority: one pattern, mirrors Config.Logger
// (#115) and Config.Tracer (#159).
```

### 2.5 Trace head-sampling policy (high-volume signal containment)

Metrics carry the cardinality budget (§2.2). Traces carry their own volume budget: scheduler ticks at sub-second cadence × per-step spans × per-agent fan-out blow past 10⁴ spans/sec in steady state, which fills the OTLP pipe and the backend's ingest bill before the operator notices. W6 (#159) shipped trace export but did not pin a sampling policy — this spec pins it.

**Policy.** OTel SDK `ParentBased(TraceIDRatioBased(p))` with `p` set from `OTEL_TRACES_SAMPLER_ARG` (default `0.1` = 10% head-sampling). Sampling decision is sticky-per-trace (every span in the trace shares the parent's `sampled` bit). High-signal traces (any span with `error.type` set, or any span emitted by `internal/orchestrator/state/substrate/sign.go` chain-verify) flip to `AlwaysOn` via the OTel `ParentBased` `RemoteParentSampled`/`LocalParentSampled` override — i.e. once a chain-break or HMAC error opens a span, the whole trace from that point ships at 100%.

**Why head-sampling, not tail-sampling.** Tail-sampling needs a collector tier (OTel Collector) buffering full traces before deciding. Self-host default has no collector — direct OTLP to backend. Head-sampling at the SDK is the OTel-idiomatic shape for that topology; the OTel Collector is an operator-side concern and supported via env-var swap (zero regatta code change).

**A-T0a wires this:** `internal/obs/otel/setup.go` calls `sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(p)))` on TracerProvider construction. The `ErrorOverride` is a custom `sdktrace.Sampler` that wraps the parent sampler and returns `RecordAndSample` whenever the span has an `error.type` attribute or originates in a designated package list (chain-verify, divergence-audit). A-T0a's PR body documents the sampled-vs-unsampled trace ratio measured against a synthetic 10⁴-span/sec fixture.

**Operator override.** `OTEL_TRACES_SAMPLER=always_on` for debug (full fidelity, expect cost spike); `OTEL_TRACES_SAMPLER=always_off` for emergency cost-stop. Documented in A-T6's `docs/operator/observability-metrics.md`.

---

## §3 Per-item metric views (15 items × design rows)

Each row pins: primitive, adopt-vs-build, surface, tag set, cardinality concern, dashboard tile, alarm threshold, file-disjoint task.

### Tier 1 — immediate (Wave A)

| # | Item | Primitive | A/B | Surface | Tags | Card. | Dashboard tile | Alarm |
|---|---|---|---|---|---|---|---|---|
| 1 | Per-DAG-run cost dashboard tile | counter `regatta.cost.usd`, `regatta.cost.tokens` | ADOPT (writer exists #283 → emit OTel counter alongside) | `internal/cost/spend/writer.go` adds `meter.Float64Counter` call after existing event row write | `dag_id`, `operator_id`, `direction` (`input`/`output`/`cache_read`) | safe (≤50 dag × ≤100 op × 3 dir = 15k) | Grafana panel "Cost USD by DAG run" — stacked bar over time, group by `dag_id` | cost spike >2× 7-day median for dag_id |
| 2 | L4 gate hit-rate + p50/p95 latency + cache-hit ratio + second-opinion fire-rate + per-category counter | counter `regatta.l4.invocations`, histogram `regatta.l4.latency_ms`, counter `regatta.l4.cache.hits`/`misses`, counter `regatta.l4.second_opinion.fired`, counter `regatta.l4.invocations` (tag `category`) | ADOPT (instrument existing #381 #380 #388) | `internal/gates/l4/gate.go` + `percategory.go` + `reload.go` | `verdict`, `category`, `cache_outcome` | safe (~5 × 12 × 3 = 180) | "L4 — invocations/sec by verdict", "L4 — p50/p95/p99 latency", "L4 — cache hit ratio", "L4 — second-opinion fire-rate", "L4 — per-category stack" | p95 latency > 30 s for 10 min |
| 4 | Scheduler tick latency histogram + per-step breakdown | histogram `regatta.scheduler.tick.latency_ms`, histogram `regatta.scheduler.tick.step_duration_ms` (tag `step`) | ADOPT (existing W6 tick span already opens; emit histogram on close) | `internal/orchestrator/scheduler/scheduler.go` | `step` (~8 named steps: `dispatch`, `gate_l0`, `gate_l4`, `gate_approval`, `gate_cost`, `reaper`, `fold`, `persist`) | safe (8 steps) | "Scheduler tick — p50/p95/p99 over time", "Tick step breakdown — stacked histogram" | p95 tick latency > 5 s for 5 min (SLO #1 — §5) |
| 14 | Daily digest cron → `docs/digests/2026-MM-DD.md` | log-event aggregator + new cron | BUILD-MIN (cron script reads metric+log corpus from operator backend OR sqlite + writes md) | `cmd/regatta/digest.go` (new subcommand `regatta digest --date YYYY-MM-DD`); cron in `scripts/cron/daily-digest.sh` | n/a (digest is text) | n/a | n/a | n/a — digest IS the human-readable surface |

### Tier 2 — substrate health (Wave B)

| # | Item | Primitive | A/B | Surface | Tags | Card. | Dashboard tile | Alarm |
|---|---|---|---|---|---|---|---|---|
| 5 | Substrate event-rate alarm (spike/drop) | counter `regatta.substrate.events.appended` (tag `kind`) | ADOPT (instrument existing event-log writer) | `internal/orchestrator/state/substrate/event.go` Append path | `kind` (10 enums; see `EventKind`) | safe | "Event rate by kind — rolling 5m", "Event rate anomaly band (±3σ)" | rate > 3σ above 7-day baseline OR rate < 10% of 7-day baseline for 5 min |
| 6 | HMAC chain break detector | counter `regatta.substrate.chain.break` | ADOPT (instrument existing #224 chain-verify path) | `internal/orchestrator/state/substrate/sign.go` chain-verify | `kind` | safe | "Chain breaks — last 24h" (target: 0) | any non-zero increment fires critical |
| 7 | Substrate divergence-audit dashboard | counter `regatta.substrate.divergence.detected` (tag `layer`) | ADOPT (instrument existing #369 #378 divergence-audit table) | `internal/orchestrator/state/migrations/0009`+`0011` reader; new emitter in divergence-audit writer | `layer` (`layer1_read` / `layer2_fold`) | safe | "Divergence rate by layer", "Divergence count — last 7d" | any sustained non-zero |
| 8 | Replay latency histogram (W9 DurableHistory p50/p95/p99) | histogram `regatta.replay.latency_ms` (tag `impl` = `substrate`/`temporal`) | ADOPT (instrument existing `internal/history/durable_history.go` Replay path) | `internal/history/substrate_impl.go` | `impl` | safe (2) | "Replay latency p50/p95/p99 by impl" | p99 > 30 s for 10 min |

### Tier 3 — agent-loop telemetry (Wave C)

| # | Item | Primitive | A/B | Surface | Tags | Card. | Dashboard tile | Alarm |
|---|---|---|---|---|---|---|---|---|
| 9 | Subagent dispatch attribution | counter `regatta.dispatch.subagents` | ADOPT (instrument existing spawner) | `internal/orchestrator/spawner/spawner.go` Spawn path | `template`, `task_type`, `agent_id` | safe | "Dispatch volume by template × task_type" | n/a (informational) |
| 10 | PR-lifecycle stage timing | histogram `regatta.pr.stage_duration_seconds` (tag `stage`) | BUILD-MIN (new collector reads github events + correlates with dispatch span) | new file `internal/obs/prlifecycle/collector.go` | `stage` (`dispatch_to_first_commit`, `first_commit_to_pr_open`, `pr_open_to_ci_green`, `ci_green_to_merge`) | safe (5 stages) | "PR stage duration — heatmap by stage" | dispatch_to_first_commit p95 > 30 min |
| 11 | Per-PR cost attribution | log-event `regatta.pr.cost_usd` (NOT a metric — pr_number unbounded; record as log + emit roll-up counter `regatta.pr.cost_usd_total` only, no `pr_number` label) | ADOPT+BUILD-MIN | `internal/cost/spend/writer.go` emits log event tagged with `pr_number` + correlation `trace_id`; metric is unlabeled aggregate | log: `pr_number`, `dag_id` | log = unbounded OK; metric = aggregate | "Total PR cost over time", drill: log search by `pr_number` | weekly cost > budget envelope |
| 12 | Subagent failure-mode taxonomy | counter `regatta.dispatch.failure` (tag `mode`) | BUILD-MIN (new emitter wraps spawner exit-classification) | `internal/orchestrator/spawner/failure_taxonomy.go` (new) | `mode` (`pr_lint_trip`, `doc_check_trip`, `check_tdd_trip`, `build_fail`, `test_fail`, `vet_fail`, `lint_fail`, `merge_conflict`, `timeout`, `panic`, `other`) | safe (≤ 20) | "Failure-mode distribution by template" | any one mode > 30% of total over 6h |

### Tier 4 — operator-facing (Wave D)

| # | Item | Primitive | A/B | Surface | Tags | Card. | Dashboard tile | Alarm |
|---|---|---|---|---|---|---|---|---|
| 3 | Adversarial-finding survival rate | counter `regatta.adversarial.findings` (tag `fate`) | BUILD-MIN (new emitter on follow-up-triage path #319) | `internal/orchestrator/followup/triage.go` (new) | `fate` (`filed`/`dismissed`/`auto_fixed`/`superseded`), `severity` | safe (4 × 4 = 16) | "Adversarial findings — survival rate by week", "Dismissal-rate alarm tile" | dismissal rate > 60% over 7d (cargo-cult adversarial review) |
| 13 | `regatta status` TUI CLI | n/a (operator surface; reads Prom + sqlite) | BUILD (new subcommand) | `cmd/regatta/status.go` (new) | n/a | n/a | n/a (TUI panels mirror dashboards) | n/a |
| 15 | Trigger-clock dashboard (days remaining to Phase X unlock) | gauge `regatta.trigger.days_remaining` (tag `trigger`) | BUILD-MIN | `internal/obs/triggers/clock.go` (new) | `trigger` (`30_day_green`, `external_customer_signal`, `phase_g_gate`) | safe (≤ 6 triggers) | "Trigger clocks — days to unlock" | trigger flips → critical (informational on unlock day) |

---

## §4 Recurring observability traps (anti-patterns to design against)

Each is a design-time rule. Reviewer subagents check the diff against this list.

1. **Unbounded tag cardinality** — `pr_number`, `run_id`, `work_item_id`, full error messages, full SQL text, unredacted user input. Hard rule: any tag with > 200 distinct values over 7d is banned on metrics. Use log events with `trace_id` correlation instead. Enforced by `TestMetricCardinality_PRNumberLabelBanned` lint test (AST walk over `meter.*Counter`/`Histogram` calls).
2. **Span per loop iteration** — one span around the loop + a counter for iteration count. Reducer folds + scheduler step loops follow this rule.
3. **Metric for what should be a log** — sparse one-shot events (chain break, divergence detected) get BOTH a counter (for alarm) AND a log event (for forensic detail). Never a metric alone for sparse events.
4. **Log for what should be a metric** — high-volume numeric measurements (LLM token counts, tick latency) MUST be metrics. Counting log records to derive a metric is forbidden — extract it.
5. **Time-series for what should be a profile** — CPU/memory continuous-state goes through `runtime/pprof`, not metrics. Out of scope for this spec; documented as a follow-up trigger ("profile-on-demand" wedge).
6. **Drift between metric name and dashboard query** — every dashboard JSON references metric names by string. CI test `TestDashboardMetricNames_MatchEmitted` greps `docs/operator/dashboards/*.json` for `regatta.*` patterns and asserts each one exists as a `meter.*` call in `internal/`.
7. **Alarm fatigue** — every alarm has a severity tier (`info`/`warn`/`critical`) and a rate-limit (no alarm fires more than once per 5 min for the same `(name, instance)` pair). Sloth-generated PromQL handles this via standard `for: 5m` clauses.
8. **Vanity metrics** — counter with no operator-facing decision. Every metric on the §3 table answers a named question for the operator (e.g. "is the L4 cache helping?" → `regatta.l4.cache.hits`/`misses`). If no question, drop the metric.
9. **Missing metric for a known failure surface** — a new gate or adapter is added without a `regatta.<gate>.invocations` counter, so a failure mode that should be visible silently isn't. Enforced by `TestEveryGateAdapterHasInvocationsCounter` (new in A-T0a) — AST walk that asserts every type implementing the `Gate` interface (in `internal/gates/`) or the `Adapter` interface (in `internal/adapters/`) has a `meter.*Counter("regatta.<gate>.invocations")` or `meter.*Counter("regatta.<adapter>.invocations")` call somewhere in its package. Failure mode: the test names the gate/adapter and points at the missing counter line.
10. **Dashboard UI drift** — operator edits a Grafana dashboard in the web UI to debug an incident, never round-trips the change back to `docs/operator/dashboards/*.json`. Real-world this is the #1 source of dashboard rot. `make provision-dashboards` upserts the JSON → Grafana, but no reverse-diff path exists. Mitigation: nightly job (filed as `[OBS-followup]` — see §9; ships in Wave-D) exports every dashboard from Grafana via HTTP API, normalizes JSON (strip mutable IDs/timestamps), and `diff` against `docs/operator/dashboards/*.json`; any non-empty diff opens a `[OBS-drift]` issue with the diff inline.
11. **Cardinality cost telemetry** — the §2.2 budget caps cardinality but doesn't measure it. Operators with metered backends (Honeycomb, Datadog) want a "metric series count over time" KPI so they see the cost-of-cardinality before the bill arrives. Mitigation: `docs/operator/dashboards/meta.json` (filed as `[OBS-followup]` — see §9; ships in Wave-D) hosts an "active series count by metric" panel sourced from Prom's `count(count by (__name__)({__name__=~"regatta_.*"}))` query.

---

## §5 SLO + alarm policy (OpenSLO YAML + Sloth-compiled Prom rules)

Four SLOs (was five — SLO-3 PR-merge-rate demoted to operator KPI tile per the converged amendments; subsequent SLOs renumbered). One OpenSLO YAML per SLO at `slo/*.yaml`. Sloth compiles to Prom recording + alerting rules at `dashboards/prometheus/rules/*.yaml`. Each SLO ships with a one-line dashboard tile on `docs/operator/dashboards/slo.json`.

### SLO-1 — Scheduler tick latency

- Objective: p95 ≤ 5 s.
- Window: 7d rolling.
- Error budget: 5% of ticks over window.
- Alarm: critical when error budget burn-rate > 14.4× (1h fast burn) OR > 6× (6h slow burn). Standard multi-window multi-burn-rate per Google SRE SLO workbook.
- SLI: `histogram_quantile(0.95, rate(regatta_scheduler_tick_latency_ms_bucket[5m])) <= 5000`.

### SLO-2 — L4 gate latency

- Objective: p95 ≤ 30 s.
- Window: 7d rolling.
- Error budget: 1% of L4 invocations.
- Alarm: critical on multi-burn-rate breach.
- SLI: `histogram_quantile(0.95, rate(regatta_l4_latency_ms_bucket[5m]))`.

**Tuning followup (filed at merge per §9):** `[OBS-followup] SLO-2 budget widen to 5% OR 28d window pending real burn-rate data from Wave-B's first 30 days`. The 1%/7d budget burns in ~50 min during a real LLM-provider tail (5-10× normal latency for 30-60 min is common). Wave-B's substrate-event-rate observability + Wave-C's failure-taxonomy will tell us whether the current budget is operationally workable; widen at that point if false-page rate > 1/week.

### SLO-3 — Substrate event-rate within bounds (renumbered from SLO-4)

- Objective: rate stays within ±3σ of 7-day rolling baseline.
- Window: 7d rolling.
- Error budget: 0.1% of 5-min windows.
- Alarm: **warn-tier** (not critical) on either spike or drop sustained > 5 min. Demoted from critical because the ±3σ stat-arb signal on a bursty non-Gaussian distribution is expected to false-positive at 1-3/day; warn-tier surfaces in the digest, does not page.
- SLI: `abs(rate(regatta_substrate_events_appended_total[5m]) - avg_over_time(rate(regatta_substrate_events_appended_total[5m])[7d:5m])) <= 3 * stddev_over_time(...)`.

**Tuning followup (filed at merge per §9):** `[OBS-followup] SLO-3 quantile rewrite — replace ±3σ with "rate <= P99 of 30d trailing"` because rate(events) is bursty and non-Gaussian; σ-based bounds are stat-arb signals not SLOs. Rewrite after B-T1's first 30 days of histogram data is in the warehouse.

### SLO-4 — Cost-cap fire rate baseline (renumbered from SLO-5)

- Objective: cost-gate denials/week ≤ 1% of LLM-call attempts (above this = budget envelope too tight; below this for 4w = no budget pressure, raise discipline).
- Window: 7d rolling.
- Error budget: bidirectional (high + low band).
- Alarm: info-tier on out-of-band.
- SLI: `rate(regatta_cost_gate_denials_total[1d]) / rate(regatta_l4_invocations_total[1d])`.

### Removed: was SLO-3 — PR-merge-rate → operator KPI tile

PR-merge-rate is a velocity target (team shipping cadence), not a user-visible reliability signal. Calling it an SLO conflates "service is healthy" with "team is shipping" and burns the error budget on planned slow weeks (operator vacation, Phase-S relaxation hold). The metric remains valuable as the **trigger-clock source** (`30_day_green` derivation in D-T3) and as an operator dashboard KPI — just not as a paging alarm.

PR-merge-rate ships as a KPI tile on `docs/operator/dashboards/digest.json` (operator-glance), tile spec: stat-panel showing 24h merged count + 7d-trailing avg + 30d-trailing avg. Source PromQL: `sum(increase(regatta_pr_stage_duration_seconds_count{stage="ci_green_to_merge"}[24h]))`. No alarm rule. D-T3's `30_day_green` trigger still reads this same series.

### Alarm-policy net effect

| SLO | Tier | Alarm tier | Notes |
|---|---|---|---|
| SLO-1 scheduler-tick | unchanged | critical | unchanged |
| SLO-2 L4 latency | unchanged | critical | budget-widen followup filed |
| SLO-3 substrate event-rate (was SLO-4) | renumbered | warn-tier (was critical) | quantile-rewrite followup filed |
| SLO-4 cost-cap fire rate (was SLO-5) | renumbered | info-tier | unchanged |
| ~~SLO-3 PR-merge-rate~~ | REMOVED | n/a | demoted to KPI tile on `docs/operator/dashboards/digest.json` |

Total SLOs: 4. Critical-tier paging alarms: 2. Warn-tier: 1. Info-tier: 1.

**Alarm action policy.** Every alarm has a runbook URL pointing at `docs/operator/runbooks/<slo-name>.md` (one runbook per SLO). Critical-tier alarms page; warn-tier go to the daily digest; info-tier surface only on the dashboard.

---

## §6 Operator surface (§ for items 13 + 14 + 15)

### 6.1 `regatta status` (item 13) — 5-panel single-screen

New CLI subcommand `cmd/regatta/status.go`. TUI rendered with `github.com/charmbracelet/bubbletea` (**candidate dep — NOT yet in `go.mod`**; D-T2 adds it. Indirect `charmbracelet/colorprofile`/`ultraviolet`/`x/ansi`/`x/term`/`x/termios` ARE in go.mod from other usage). Panel budget: **single-screen on 80×24 terminal** (default ssh session). Sum of panel rows ≤ 24, panel widths align to 80-col grid.

| Panel | Rows | Cols | Notes |
|---|---|---|---|
| 1. Loop pulse | 6 | 80 | scheduler tick p50/p95 + active work_item count + lane breakdown |
| 2. Cost | 5 | 80 | this hour / today / week + budget remaining |
| 3. L4 | 4 | 80 | cache hit ratio + second-opinion fires (1h) |
| 4. PR pipeline | 5 | 80 | open PRs by stage; count + oldest per stage |
| 5. Alarms (last 24h) | 4 | 80 | name, severity, fired-at, resolved-at |
| **Total** | **24** | **80** | exact fit |

Triggers (days to 30-day-green; external-customer signal; Phase-G) move to a sibling subcommand `regatta triggers` (one-line stat per trigger, no TUI overhead; D-T3 owns it).

Data source: Prom HTTP API (if `OTEL_EXPORTER_OTLP_ENDPOINT` set) + sqlite (always available). When Prom unreachable, panels degrade to sqlite-derived numbers with a banner.

UX target (per `feedback_decision_priority`): `regatta status` answers "is the loop healthy + what's the next operator action?" in < 3 seconds of cold start.

#### 6.1.1 TUI mockup (ascii)

Library: `github.com/charmbracelet/bubbletea` (per §1.9 scored comparison — 19/20, ADOPT). The mockup below is the **expanded 80×40 layout** used when the host terminal exceeds the 80×24 minimum (covers items the task brief calls out: in-flight agents, open PRs, CI state, keyring, substrate row count, today's spend vs cap, 30-day-green countdown). On 80×24 the panels collapse to the 5-panel budget in the table above; on 80×40 the loop pulse panel expands to show the agent + keyring + substrate rows below.

Color contract (single source of truth — render path reads this map): **green** = within budget / healthy / passing. **yellow** = within 80 % of cap, retry-able fail, or stale ≥ 5 min. **red** = over cap, hard fail, or stale ≥ 15 min. No other colors; no bold-on-default for status (operators on `NO_COLOR` terminals get glyph prefixes `[ok]` / `[!]` / `[X]`).

Refresh: **operator-tunable** via `--refresh=<duration>` flag (default `5s`; floor `1s` to avoid hammering the Prom HTTP API on shared infra; ceiling `60s`). `r` keybinding forces an immediate refresh without resetting the timer. Per `feedback_design_iteration_local`: the default was chosen by running the binary on a laptop tethered to a remote Prom instance — 1s saturated the link, 5s felt live.

```text
+------------------------------------------------------------------------------+
| regatta status                       2026-06-02 14:32:07   refresh: 5s  [r]  |
+------------------------------------------------------------------------------+
| LOOP PULSE                                                                   |
|   tick p50: 218 ms   p95: 1.94 s    [ok]      lanes: 7 / 10 active           |
|   work items: 12 queued, 7 in-flight, 3 blocked-on-review                    |
|   scheduler error rate (1h): 0.001                                  [ok]     |
+------------------------------------------------------------------------------+
| IN-FLIGHT AGENTS (7)                                                         |
|   #A23  D-T2 bubbletea TUI         dispatch+04m17s   subagent: implementer   |
|   #A24  C-T4 emitter wave-C        dispatch+00m48s   subagent: reviewer      |
|   #A25  doc-obs §6.1 ascii         dispatch+00m12s   subagent: doc-writer    |
|   #A26  CI-T1 lane bisect          dispatch+11m02s   subagent: implementer   |
|   ...                                                            [/ filter]  |
+------------------------------------------------------------------------------+
| OPEN PRs (9)         CI STATE                                                |
|   #401 D-T2  WIP     check-tdd: pass   pr-lint: pass   doc-check: pass [ok]  |
|   #402 C-T4  REVIEW  check-tdd: pass   pr-lint: pass   doc-check: pass [ok]  |
|   #403 OBS   STALE   check-tdd: FAIL   pr-lint: pass   doc-check: pass [X]   |
|   #404 A-T7  AUTO    check-tdd: pass   pr-lint: WARN   doc-check: pass [!]   |
|   oldest in REVIEW: #398 (3h 21m)                                            |
+------------------------------------------------------------------------------+
| COST (USD)                          KEYRING                                  |
|   this hour:  $4.21                   anthropic-1   ok        rate 38/60     |
|   today:     $67.42 / cap $120  [ok]  anthropic-2   ok        rate 12/60     |
|   week:     $411.30 / cap $700        openai-1      ok        rate  4/60     |
|   spend trend (24h): ===============  github-app    ok        rate 87/5000   |
+------------------------------------------------------------------------------+
| SUBSTRATE                           TRIGGERS                                 |
|   rows:        1 248 117             30-day-green:    17 days remaining      |
|   events/sec:        12.3   [ok]     ext-customer:    not-yet-set            |
|   chain breaks:       0     [ok]     phase-G gate:    27 days remaining      |
|   divergence count:   0     [ok]                                             |
+------------------------------------------------------------------------------+
| ALARMS (last 24h, 2)                                                         |
|   14:02  cost.hour_cap_warn       warn    fired 30m ago    auto-cleared      |
|   09:11  tick.p95_breach          warn    fired 5h ago     resolved 09:14    |
+------------------------------------------------------------------------------+
| q quit  r refresh  / filter  arrows navigate  enter drill-down               |
+------------------------------------------------------------------------------+
```

Key bindings:

| Key | Action |
|---|---|
| `q` / `Ctrl-C` | quit |
| `r` | force refresh (does not reset timer) |
| `/` | filter (regex over current focused panel; ESC clears) |
| `arrows` (`h/j/k/l` also bound) | move focus between panels + rows |
| `enter` | drill-down (PR → `gh pr view` in `$PAGER`; agent → tail subagent log; alarm → open Grafana panel link) |
| `?` | overlay this binding table |

Per `feedback_research_design_principles` (proven OSS > build-from-scratch): bubbletea's Elm-architecture update loop is the load-bearing pick — the 5-panel state struct is one `Model`, each refresh tick is one `Msg`, and panel renderers compose via `lipgloss` (transitive dep of bubbletea, not a new direct dep). The mockup is the **target rendering**; D-T2 lands the binary and gold-files the frame against this layout (`testdata/status-frame.golden.txt`) so future panel edits stay in lock-step with the spec.

### 6.2 Daily digest (item 14) — machine + human surface

`regatta digest --date 2026-MM-DD` reads the previous 24h of metrics + logs + PR-merge events and writes `docs/digests/2026-MM-DD.md` with a **YAML front-matter block** (for machine readers: trigger-clock derivation, the autonomous-session-prompt boot reader, future weekly-rollup) plus the human-readable markdown sections.

YAML front-matter shape:

```yaml
---
# machine-readable front-matter — keep in lock-step with markdown body below
date: 2026-MM-DD
tick_p95_ms: 4321
tick_error_rate: 0.001
prs_landed: 11
prs_landed_a_plus: 3
prs_landed_a: 7
prs_landed_b: 1
cost_usd_today: 87.42
cost_usd_week_to_date: 511.30
adversarial_filed: 4
adversarial_dismissed: 2
substrate_events_per_sec: 12.3
substrate_chain_breaks: 0
substrate_divergence_count: 0
triggers:
  30_day_green: 17
  external_customer_signal: null  # not yet set
  phase_g_gate: 27
followups_filed: 3
---
```

The markdown body sections below mirror the front-matter fields one-to-one. Downstream tooling (`yq '.tick_p95_ms' docs/digests/2026-MM-DD.md`) reads the front-matter; humans read the body. Per `feedback_research_design_principles`: the digest IS infrastructure for the next layer; parse-ability is required, not optional.

Markdown body sections:

1. **Loop health** — tick p95, gate counts, alarms fired.
2. **PRs landed** — list with title, A+ tier (B/A/A+), cost USD, dispatch→merge time.
3. **Adversarial findings** — count by fate.
4. **Substrate health** — events/sec, chain breaks (target 0), divergence count.
5. **Cost** — USD by DAG, cumulative week.
6. **Triggers** — days remaining to each unlock.
7. **Followups filed** — count + link to tracking issues.

**First-digest degraded contract.** Sections 2 (PRs landed) and 3 (Adversarial findings) depend on emitters that land in later waves: section 2 needs `regatta.pr.stage_duration_seconds` (C-T2 — Wave C) for the dispatch→merge stage timing column, and section 3 needs `regatta.adversarial.findings` (D-T1 — Wave D) for the fate-by-count rollup. Wave-A A-T4 ships the digest binary with these two sections rendering a one-line placeholder ("`PRs landed — emitter ships C-T2 (Wave C); see [OBS-pending-emitter]`" / "`Adversarial findings — emitter ships D-T1 (Wave D); see [OBS-pending-emitter]`") not silent zeros. The placeholder text uses a fixed `[OBS-pending-emitter]` label, not a numeric issue ID, so `TestDigest_Deterministic` parses against the label rather than a non-deterministic gh-assigned number. The placeholder lines are removed by the C-T2 and D-T1 implementer subagents respectively as part of their landing PR. Per `feedback_decision_priority` UX > velocity: a placeholder with a forward reference is better than a silently zeroed section that looks like the loop is broken.

Cron: `scripts/cron/daily-digest.sh` runs daily at 09:00 local time; commits the file via the standard PR workflow with `[DOCS]` prefix.

### 6.3 Trigger-clock + `regatta triggers` (item 15)

`internal/obs/triggers/clock.go` exports `func DaysRemaining(trigger string) int` over the set of named triggers (`30_day_green`, `external_customer_signal`, `phase_g_gate`). Emits async observable gauge `regatta.trigger.days_remaining` on a 5-min ticker. Grafana panel "Trigger clocks" is one stat-tile per trigger.

Sibling subcommand `cmd/regatta/triggers.go` — one stat-line per trigger ("`30_day_green: 17 days remaining`"). No bubbletea, no TUI; plain stdout. `regatta status` footer links to `regatta triggers`.

### 6.4 Dashboards-as-code commitment

Every dashboard tile in §3 ships as a JSON file under `docs/operator/dashboards/*.json`, version-controlled. `make provision-dashboards` calls the Grafana HTTP API to upsert. CI test `TestDashboardJSON_LintsAgainstSchema` validates every JSON against Grafana's schema (vendored at `tools/grafana-schema/`).

The Wave-C cost-per-agent rollup is the one panel that uses a Grafana template variable (`$rollup_dim` ∈ {`pr_number`, `agent_id`, `task_type`}) so a single base dashboard JSON renders three views — per-PR (operator-facing, ties cost+latency to merge-able units), per-agent (debugger-facing, isolates flakey agents), per-task-type (strategy-facing, "which dispatch templates are slow"). Same dense-event store, three sparse projections at query time — no schema migration to add a fourth view later (per §11 RISK-B + §9 follow-up #3).

---

## §7 Sequenced roadmap (4 waves, file-disjoint, dispatch-ready)

Wave-A is the load-bearing foundation (naming + tag schema + 4 highest-impact metrics). Wave-B-D are file-disjoint inside each wave; the cross-wave seam is the `Config.Meter` DI field landed in Wave-A.

### Wave A — metric foundation + 4 highest-impact metrics (items #1, #2, #4, #14)

Goal: prove the canonical stack end-to-end with the 4 metrics that pay back fastest. A-T0 splits into A-T0a (foundation + 2 Config retrofits to unblock A-T1/A-T2) + A-T0b (remaining 6 Config retrofits).

| ID | Owner | Path (exclusive) | Depends-on | Effort |
|---|---|---|---|---|
| **A-T0a** | impl-A0a | `internal/obs/otel/meter.go` + `meter_test.go`; extend `internal/obs/otel/setup.go` to init MeterProvider; OTLP-metric exporter wiring; Prom exporter wiring (`OTEL_METRICS_PROMETHEUS_PORT`); mutual-exclusion validator (`ErrOTelMetricExporterConflict`); new lint test `TestEveryGateAdapterHasInvocationsCounter` (covers §4 trap #9). Adds `Config.Meter metric.Meter` field to the 2 Config structs A-T1/A-T2 touch first (`cost/spend`, `gates/l4`) **on the Config struct only** — `writer.go` and gate-decide-path edits stay out of A-T0a's scope (see "Open at impl time" RISK-A). | — | M |
| **A-T0b** | impl-A0b | adds `Config.Meter metric.Meter` field to the remaining 6 Config structs (`orchestrator/scheduler`, `orchestrator/spawner`, `orchestrator/state/substrate`, `history`, `orchestrator/followup`, plus the spawner-failure-taxonomy ctor that lands in Wave-C) + retrofits constructors + updates every existing test that constructs each component | A-T0a | M |
| **A-T1** (item #1) | impl-A1 | instrument `internal/cost/spend/writer.go` with `meter.Float64Counter("regatta.cost.usd")` + `meter.Int64Counter("regatta.cost.tokens")` calls; add `docs/operator/dashboards/per-dag-cost.json` | A-T0a | S |
| **A-T2** (item #2) | impl-A2 | instrument `internal/gates/l4/gate.go` + `percategory.go` + `reload.go` with counter + histogram + cache-hit/miss + second-opinion-fire counters; add `docs/operator/dashboards/l4-gate.json` | A-T0a | M |
| **A-T3** (item #4) | impl-A3 | instrument `internal/orchestrator/scheduler/scheduler.go` Tick path with tick-latency histogram + per-step duration histogram (tag=`step`); add `docs/operator/dashboards/scheduler-tick.json` | A-T0b | M |
| **A-T4** (item #14) | impl-A4 | new `cmd/regatta/digest.go` subcommand + `scripts/cron/daily-digest.sh`; first digest at `docs/digests/2026-06-03.md`; ships placeholder sections + YAML front-matter per §6.2 | A-T1 thru A-T3 | M |
| **A-T5** | impl-A5 | `slo/scheduler-tick.yaml` (SLO-1) + `slo/l4-latency.yaml` (SLO-2) + Sloth compile to `dashboards/prometheus/rules/`; `docs/operator/dashboards/slo.json`; `docs/operator/runbooks/scheduler-tick.md` + `docs/operator/runbooks/l4-latency.md` | A-T1 thru A-T3 | M |
| **A-T6** | impl-A6 | `docs/operator/observability-metrics.md` — operator-facing doc for the metric layer | A-T5 | S |

Wave-A exit gate: `make check` clean; `regatta digest` produces a valid markdown file (with placeholder lines for sections 2 + 3 + YAML front-matter); 4 dashboards provision and render; SLO-1 and SLO-2 fire correctly on synthetic load; adversarial reviewer subagent clears.

A-T0a → A-T0b MUST be sequenced serially (same implementer or sequential dispatch) — parallel pickup risks A-T0b blocking on A-T0a's Config struct definitions.

### Wave B — substrate health + replay latency (items #5, #6, #7, #8)

| ID | Owner | Path (exclusive) | Depends-on | Effort |
|---|---|---|---|---|
| **B-T1** (item #5) | impl-B1 | instrument `internal/orchestrator/state/substrate/event.go` Append + emit `regatta.substrate.events.appended`; `slo/substrate-event-rate.yaml` (SLO-3 renumbered, warn-tier); `docs/operator/dashboards/substrate-event-rate.json` | A-T0b | S |
| **B-T2** (item #6) | impl-B2 | instrument `internal/orchestrator/state/substrate/sign.go` chain-verify + emit `regatta.substrate.chain.break`; alarm rule (critical, any non-zero); `docs/operator/dashboards/substrate-chain.json` | A-T0b | S |
| **B-T3** (item #7) | impl-B3 | divergence-audit-table reader emits `regatta.substrate.divergence.detected` (tag `layer`); `docs/operator/dashboards/substrate-divergence.json`; surface lives in `internal/orchestrator/state/substrate/divergence_emit.go` (new file, separate from existing audit writers) | A-T0b | S |
| **B-T4** (item #8) | impl-B4 | instrument `internal/history/substrate_impl.go` Replay path + emit `regatta.replay.latency_ms` (tag `impl`); `docs/operator/dashboards/replay.json`; `slo/replay-latency.yaml` | A-T0b | S |

Wave-B exit gate: 4 dashboards green; SLO-3 fires warn-tier on synthetic burst; chain-break critical alarm verified via synthetic break in a test fixture; reviewer clears.

### Wave C — agent-loop telemetry (items #9, #10, #11, #12)

| ID | Owner | Path (exclusive) | Depends-on | Effort |
|---|---|---|---|---|
| **C-T1** (item #9) | impl-C1 | instrument `internal/orchestrator/spawner/spawner.go` Spawn path + emit `regatta.dispatch.subagents` (tag `template`, `task_type`, `agent_id`); `docs/operator/dashboards/dispatch.json` | A-T0b | S |
| **C-T2** (item #10) | impl-C2 | new `internal/obs/prlifecycle/collector.go` correlates dispatch span → GitHub PR events via `pr_number` → emits histogram `regatta.pr.stage_duration_seconds` (tag `stage`); reads GitHub API via existing `gh` shell or a new minimal client. **Removes the A-T4 placeholder line for the PRs-landed digest section** as part of the landing PR (per §6.2 first-digest degraded contract). | A-T0b, C-T1 | M |
| **C-T3** (item #11) | impl-C3 | extend `internal/cost/spend/writer.go` (touched by A-T1; coordinate via shared-primitive-owner per `feedback_shared_primitive_owner` — **A-T1 OWNS this file across Waves A+C**) to also emit a log event with `pr_number` correlation + unlabeled aggregate counter `regatta.pr.cost_usd_total` | A-T1 (shared owner) | S |
| **C-T4** (item #12) | impl-C4 | new `internal/orchestrator/spawner/failure_taxonomy.go` parses CI failure logs into `mode` enum; emits `regatta.dispatch.failure` (tag `mode`); `docs/operator/dashboards/failure-modes.json` | A-T0b, C-T1 | M |

Wave-C exit gate: 3 dashboards green (C-T1 `dispatch.json`, C-T2 `pr-lifecycle.json`, C-T4 `failure-modes.json`; C-T3 extends `writer.go` + adds log event, no new dashboard); PR-lifecycle stage histogram populates on real PRs; failure-mode counter covers ≥ 8 known mode buckets from the last 30d of CI history; reviewer clears.

**Shared-primitive-owner note**: per `feedback_shared_primitive_owner` — A-T1 owns `internal/cost/spend/writer.go` across both Wave A and Wave C. C-T3's dispatch brief MUST cite A-T1 as the file owner; coordinate edits via a single follow-up PR sequence. A-T0a explicitly fences its retrofit to the Config struct (`config.go` only), not `writer.go` — see "Open at impl time" RISK-A.

### Wave D — operator surfaces (items #3, #13, #15)

| ID | Owner | Path (exclusive) | Depends-on | Effort |
|---|---|---|---|---|
| **D-T1** (item #3) | impl-D1 | new `internal/orchestrator/followup/triage.go` instruments follow-up triage decisions (filed/dismissed/auto_fixed/superseded); emits `regatta.adversarial.findings` (tag `fate`, `severity`); `docs/operator/dashboards/adversarial.json`. **Removes the A-T4 placeholder line for the Adversarial-findings digest section** as part of the landing PR (per §6.2 first-digest degraded contract). | A-T0a | M |
| **D-T2** (item #13) | impl-D2 | new `cmd/regatta/status.go` TUI subcommand using bubbletea (**add to go.mod** — not currently present); 5 panels per §6.1 budget table (drops Triggers panel — relocated to `regatta triggers`); reads Prom HTTP API + sqlite | Waves A+B+C all merged (TUI panels reference all metrics) | L |
| **D-T3** (item #15) | impl-D3 | new `internal/obs/triggers/clock.go` emits gauge `regatta.trigger.days_remaining` (tag `trigger`); new sibling subcommand `cmd/regatta/triggers.go` (one stat-line per trigger); `docs/operator/dashboards/trigger-clock.json`; trigger thresholds live in `slo/triggers.yaml` | A-T0a, **C-T2** (30_day_green reads the PR-stage histogram emitted by C-T2 — DO NOT DISPATCH D-T3 BEFORE C-T2 MERGES or the gauge reads zero) | S |

Wave-D exit gate: `regatta status` renders in < 3 s cold on 80×24 terminal; `regatta triggers` prints one line per trigger; trigger-clock panel shows days remaining for all 3 triggers; adversarial dismissal-rate alarm fires correctly on synthetic dismissal-burst test fixture; reviewer clears.

### Dependency graph

```
A-T0a (no dep)
  ├─ A-T0b (dep A-T0a)
  │    └─ A-T3 (dep A-T0b)
  ├─ A-T1 (dep A-T0a)
  └─ A-T2 (dep A-T0a)
       └─ A-T4 (dep A-T1..A-T3)
       └─ A-T5 (dep A-T1..A-T3)
            └─ A-T6 (dep A-T5)

Wave B (B-T1 ∥ B-T2 ∥ B-T3 ∥ B-T4 — all dep A-T0b)

Wave C (C-T1 → C-T2 + C-T4; C-T3 shared-owner with A-T1)

Wave D:
  D-T1 at Wave-A green (dep A-T0a)
  D-T3 at Wave-C green (dep A-T0a + C-T2 — NOT parallel with D-T1 in wall-clock)
  D-T2 after Waves A+B+C all merged
```

Total tasks: **19** (was 17 in PR #400; A-T0 split into A-T0a + A-T0b adds 1; the original count omitted A-T0b). Breakdown: Wave A = 8 (A-T0a, A-T0b, A-T1, A-T2, A-T3, A-T4, A-T5, A-T6); Wave B = 4 (B-T1..B-T4); Wave C = 4 (C-T1..C-T4); Wave D = 3 (D-T1, D-T2, D-T3).

---

## §8 Grade rubric (B / A / A+ — tool-checkable)

Per `feedback_grade_rubric`. Applies to every PR in every wave. Implementer scorecard mandatory in PR body.

### B (floor — ships)

- B1. All §3 tasks for the wave have green tests + lints clean. Verify: `make check`.
- B2. Metric naming follows §2.1; no `pr_number`/`run_id`/`work_item_id` labels on any metric. Verify: `TestMetricCardinality_PRNumberLabelBanned` AST walk passes.
- B3. Every metric has a dashboard tile checked into `docs/operator/dashboards/`. Verify: `TestDashboardMetricNames_MatchEmitted` greps emitted vs referenced.
- B4. PR body carries release-notes fence + `[FEATURE]`/`[DOCS]` category. Verify: `scripts/pr-lint.sh` exit 0.
- B5. Test godocs are 1-line max per `feedback_test_godoc_one_line`. Verify: `make check` includes `scripts/doc-check.sh` test-godoc gate.
- B6. D-T3 PR body MUST show C-T2's PR-stage histogram is present BEFORE D-T3 lands (per §7 D-T3 dep fix). Verify: D-T3 PR body cites C-T2 PR number + shows `prom http GET /api/v1/query?query=regatta_pr_stage_duration_seconds_count` returns non-zero series.
- B7. A-T0a PR body MUST show one sample `/metrics` Prom-scrape line for `regatta.scheduler.tick.latency_ms` so the double-unit wire string is locked at landing (per §2.1 amendment).

### A (target — expected outcome)

- A1. B + adversarial reviewer subagent attests "no unresolved findings."
- A2. Every metric in the wave has at least one Grafana panel referencing it AND at least one SLO recording rule OR alarm rule. Verify: `TestDashboardMetricNames_MatchEmitted` + `slo/*.yaml` cross-reference.
- A3. Operator runbook exists for every critical-tier alarm. Verify: `TestRunbook_ExistsForEveryCriticalAlarm` greps Sloth-output alert rules vs `docs/operator/runbooks/*.md`.
- A4. Cardinality budget audit clean: every label has a documented cap in §2.2. Verify: `TestTagCardinality_LabelsHaveDocumentedBudget`.
- A5. Every named-but-deferred sub-decision filed as `[OBS-followup]` tracking issue per `feedback_unaddressed_load_bearing`. Verify: `gh issue list --label OBS-followup` ≥ wave's filed count.
- A6. A-T4 PR body MUST show the YAML front-matter block in the first generated digest at `docs/digests/2026-06-03.md` matches the markdown body section-by-section. Verify: `TestDigest_YAMLFrontMatterMatchesBody` exit 0.

### A+ (stretch — exceptional)

- A+1. A + dashboard JSON validates against Grafana's vendored schema. Verify: `TestDashboardJSON_LintsAgainstSchema` exit 0.
- A+2. SLO error-budget burn-rate alarms verified against a synthetic load injector. Verify: `TestSLOBurnRate_FiresOnSyntheticBreach` exit 0.
- A+3. `regatta status` panel render budget < 3 s cold start measured. Verify: `BenchmarkStatusRender_ColdStart` ≤ 3000ms.
- A+4. Daily digest is a no-flake, deterministic-output cron — same inputs produce byte-equal markdown. Verify: `TestDigest_Deterministic`.
- A+5. Property test sweeps 200 synthetic label combinations and asserts no cardinality-cap breach. Verify: `make property-test` exit 0.
- A+6. `regatta status` panel-budget table assertions hold on 80×24 terminal: `TestStatus_FitsInDefaultTerminal` parses the rendered output against the §6.1 budget table (rows ≤ 24, max col ≤ 80).

---

## §9 [OBS-followup] tracking issues (filed at this spec's merge)

Per `feedback_unaddressed_load_bearing` — every load-bearing leftover gets a tracking issue. Three filed:

1. **`[OBS-followup] SLO-2 budget widen (5% OR 28d window) + SLO-3 quantile rewrite (P99 of 30d trailing)`** — trigger: 30 days of real burn-rate data from Wave-B in the warehouse. Linked from §5 SLO-2 + SLO-3 entries. (Single-operator self-host: owner field omitted per CLAUDE.md §Self-host filter.)

2. **`[OBS-followup] Dashboard-UI-drift nightly diff job (Grafana HTTP export vs checked-in JSON) + cardinality-cost "active series count" panel on docs/operator/dashboards/meta.json`** — trigger: Wave-D kickoff. Linked from §4 trap #10 + trap #11. Bundles two related concerns into one issue because both ship on the meta-dashboard surface. (Single-operator self-host: owner field omitted per CLAUDE.md §Self-host filter.)

3. **`[OBS-followup] Cost-per-agent rollup (Prom recording rule OR sqlite view joining event_token_spend × dispatch trace tree on trace_id → agent_id)`** — owner: C-T3 by default; reassign at Wave-C kickoff if scope grows. Linked from §11 RISK-B. Filed because adding `agent_id` to the cost-counter labels would breach the cardinality budget; the rollup ships as a derived view, not as a new label.

Trap #9 (missing-metric AST-walk lint) ships in-band with A-T0a — no followup needed.

---

## §10 Risk preemption (adversarial red-team)

### R1 — Cardinality cliff on first real run

**Threat**: A metric labelled with an "obviously bounded" enum balloons in production (a typo, a stack trace leaking into `category`).
**Mitigation**: AST-walk lint test + OTel SDK's view API caps tag count (drops to `_other` after N distinct values). Operator doc names the SDK cap env var.
**Verify**: `TestMetricCardinality_PRNumberLabelBanned` (banned-tag list); `TestMetricView_CapsHighCardLabels` (synthetic explosion test).

### R2 — Dashboard drift vs emitted metrics

**Threat**: A dashboard panel references `regatta.l4.cache.hit` but the emitter renamed it to `regatta.l4.cache.hits` — silent broken dashboard.
**Mitigation**: `TestDashboardMetricNames_MatchEmitted` greps both sides. CI gate. The §6.2 YAML front-matter applies the same drift discipline to digests: `TestDigest_YAMLFrontMatterMatchesBody` cross-checks every front-matter key has a corresponding body section.
**Verify**: test names above.

### R3 — Sloth-generated rule churn breaks alarm UX

**Threat**: Sloth bumps a major version + rewrites burn-rate windows.
**Mitigation**: pin Sloth version in `tools/sloth/version`; bot-managed bumps with the alert-rule diff in the PR body.
**Verify**: `tools/sloth/version` exists + `make slo-compile` is deterministic.

### R4 — Prometheus pull doesn't reach OTel SDK by default

**Threat**: OTel SDK exports OTLP; Prom expects pull. Operators run Prom + see nothing.
**Mitigation**: A-T0a wires the OTel Prometheus exporter (`go.opentelemetry.io/otel/exporters/prometheus`) when `OTEL_METRICS_PROMETHEUS_PORT` env var is set. Operator doc covers Prom-vs-OTLP choice explicitly.
**Verify**: `TestMeterSetup_PrometheusExporterWiresOnEnvVar`.

### R5 — `regatta status` reads stale Prom when backend down

**Threat**: Prom backend unreachable; TUI shows zeros silently.
**Mitigation**: TUI banner "Prom unreachable — sqlite fallback (cost only)" when Prom HTTP API errors. Per `feedback_decision_priority` UX-first.
**Verify**: `TestStatus_BackendDownShowsBanner`.

### R6 — Daily digest commits on a broken day (no PRs landed)

**Threat**: Operator wakes to a day's digest claiming "0 PRs" with no context — alarm fatigue. Reinforced by §6.2 first-digest degraded contract: placeholder lines on the first digest avoid silent zeros while emitters are still landing.
**Mitigation**: digest skips PR section when zero merges + adds explicit "Loop quiet (Phase-S relaxation hold? CI-block? operator vacation?)" prompt at top. Per `feedback_decision_priority` UX > velocity.
**Verify**: `TestDigest_ZeroPRDayShowsContext`.

### R7 — Per-PR cost cardinality leak via log → metric backend

**Threat**: An operator's log backend auto-promotes a high-card log attribute to a metric → cardinality cliff anyway.
**Mitigation**: §2.2 cardinality budget documented; `pr_number` ONLY on log events + spans, never on `meter.*` calls. Reviewer subagent flags any commit that puts `pr_number` in a meter call.
**Verify**: `TestMetricCardinality_PRNumberLabelBanned` + reviewer mandate.

### R8 — Multi-tenant `tenant_id` retrofit (Phase X gate)

**Threat**: Every metric here lands with implicit `tenant_id=default`. At Phase X (multi-tenant unlock), every metric needs per-tenant scoping or the cardinality cliff fires on tenant count.
**Mitigation**: meter resolves `tenant_id` from `ctx` via the same W8 lookup the spans use. Spec hand-off documented; `[OBS-followup] Phase-X tenant_id propagation` tracking issue filed at A-T0a merge.
**Verify**: A-T0a PR body cites W6's W8 hand-off contract + filed issue.

### R9 — Adversarial-finding metric becomes Goodhart's law

**Threat**: Operator pressures dismissal rate down by re-classifying dismissed findings as "superseded" → metric is gamed, signal lost.
**Mitigation**: `fate` is a 4-way enum with explicit definitions in `docs/operator/runbooks/adversarial-dismissal.md`; PR-review process audits classification quarterly. Per `feedback_decision_priority` long-term > short-term.
**Verify**: runbook exists; A-T5 rubric covers it.

### R10 — Trigger-clock gauge gameable

**Threat**: Operator wants Phase-X unlock; bumps the gauge directly.
**Mitigation**: clock reads SLO-3-source histogram (PR-merge-rate via C-T2) + the actual git history; no manual override. Validator in `internal/obs/triggers/clock_test.go` asserts the gauge derives only from sealed inputs.
**Verify**: `TestTriggerClock_NoManualOverride`.

### R11 — Trace ingest volume cliff at steady state

**Threat**: W6 trace export without a sampling policy + the scheduler-tick fan-out lights up 10⁴+ spans/sec in steady state. OTLP pipe fills; ingest bill clips operator budget; tail latency on the export path back-pressures the loop.
**Mitigation**: §2.5 head-sampling policy — `ParentBased(TraceIDRatioBased(0.1))` default with always-on override for `error.type` spans + chain-verify + divergence-audit packages. A-T0a wires the sampler; A-T6 documents the env-var override knobs.
**Verify**: `TestTracerSetup_HeadSamplingRatioEnforced` + A-T0a PR body shows sampled-vs-unsampled ratio against a 10⁴-span/sec synthetic fixture.

---

## §11 Open at impl time (deferred RISK from second-tier review)

Per `feedback_design_iteration_local` — one RISK was deferred at the PR #413 review pass because it is recoverable at impl-A0a PR review and does not need a spec re-edit. Captured here so impl-A0a + impl-A1 do not stumble.

### RISK-A — A-T0a Config retrofit fence on `internal/cost/spend`

**Defect surface.** A-T0a adds the `Config.Meter metric.Meter` field to the `cost/spend` Config struct so A-T1 can start parallel. The shared-primitive-owner rule (§7 Wave-C note) names **A-T1** as the sole owner of `internal/cost/spend/writer.go` across Waves A + C. If impl-A0a interprets "Config retrofit" as "also wire the meter inside `writer.go`," it conflicts with A-T1's exclusive ownership of the emit-the-counter half + C-T3's shared-owner extension.

**Resolution at impl time.** A-T0a's dispatch brief MUST fence the retrofit to the Config struct alone (`internal/cost/spend/config.go` or equivalent). A-T0a does NOT edit `writer.go` and does NOT add `meter.*` calls inside the writer. A-T1 owns those edits. The A-T0a PR review explicitly checks for this fence: if `internal/cost/spend/writer.go` shows in A-T0a's diff, the reviewer rejects until the writer edit is moved to A-T1.

**Why deferred, not amended.** The spec's intent is recoverable from context (A-T0a is the DI-wire-up half, not the emit-the-counter half); the conflict is detectable at A-T0a PR review (reviewer sees `writer.go` edited and asks why); the fix is a one-line dispatch-prompt fence, not a spec rewrite. Per `feedback_design_iteration_local`: keep spec stable, recover at impl review.

### RISK-B — Cost attribution at the `agent_id` granularity (deferred to Wave-C+)

**Defect surface.** A-T1 emits `regatta.cost.usd` + `regatta.cost.tokens` with `dag_id` + `operator_id` + `direction` labels. C-T1 emits dispatch attribution with `agent_id` (≤ 50). Neither surface joins cost and agent — operators cannot answer "which agent burned $X this week?" at the metric layer; they must drill metric → trace → log via `trace_id` correlation.

**Resolution at impl time.** The drill path works (metric → exemplar → trace → spend log carries `agent_id`), so this is a UX gap, not a correctness gap. File `[OBS-followup] Cost-per-agent rollup` as part of A-T1 merge: targets a derived recording rule (Prom) or a sqlite view that joins `event_token_spend` rows with the dispatch span tree on `trace_id` → `agent_id`. Lands in Wave-C alongside C-T1/C-T3 or as an A-T6 doc note (operator drill recipe) — owner picked at Wave-C kickoff.

**Rollup shape — multi-dimensional tag set + 3 views (closed at Wave-C kickoff).** The rollup is NOT a single-dimension pick (per-PR vs per-agent vs per-task-type). Wave-C emits a 3-tag set on the existing dispatch surface (`pr_number` on log only per §3 item #11, `agent_id` on the dispatch counter per §3 item #9, `dispatch.task_type` on the dispatch counter per §3 item #9 — already in the spec), and the rollup ships as a Prom recording rule (preferred — no new infra) that joins `event_token_spend` rows with the dispatch span tree on `trace_id`. The recording-rule output carries all three dimensions; the dashboard exposes them as a single Grafana template variable (`$rollup_dim` ∈ {`pr_number`, `agent_id`, `task_type`}) so one base panel renders three views without a schema migration. Cardinality is safe per §2.5 head-sampling (`pr_number` + `agent_id` capped at the trace layer; `task_type` is bounded ≤ 20). Sqlite-view fallback stays on the table only if the Prom recording-rule cardinality budget blocks at provision time. Owner: C-T3 by default since C-T3 already extends `writer.go`; reassign at follow-up triage if scope grows. Precedent: Honeycomb, Datadog, and Tempo all converge on "store the high-dimensional event row, project the view at query time" — adopting the same shape per `feedback_research_design_principles`.

**Why deferred, not amended.** Adding `agent_id` to the cost-counter labels at A-T1 would breach the cardinality budget (≤ 50 dag × ≤ 100 operator × 3 direction × ≤ 50 agent = 750k cells, 5× the documented cap). The drill path is the OTel-blessed shape; the rollup is a follow-on convenience, not a load-bearing primitive.

---

## §12 Per-item dispatch briefs (copy-paste ready)

Each brief below is ready to drop into an implementer-subagent dispatch prompt. The Wave letter prefixes each brief's path.

### A-T0a (foundation — meter DI + 2 Config retrofits + trap #9 lint)

> **Task**: extend `internal/obs/otel/setup.go` to init an OTel MeterProvider alongside the existing TracerProvider. New file `internal/obs/otel/meter.go` exports the helpers. Wire OTLP-metric exporter when `OTEL_EXPORTER_OTLP_ENDPOINT` is set; wire Prometheus exporter when `OTEL_METRICS_PROMETHEUS_PORT` is set; mutual-exclusion validator rejects both via `ErrOTelMetricExporterConflict`. Wire trace head-sampling per §2.5 — `ParentBased(TraceIDRatioBased(p))` with `p` from `OTEL_TRACES_SAMPLER_ARG` (default 0.1) + always-on override for `error.type` spans and chain-verify/divergence-audit packages (see §10 R11). Add new lint test `TestEveryGateAdapterHasInvocationsCounter` covering §4 trap #9. Add `Config.Meter metric.Meter` field to the **2 Config structs A-T1 + A-T2 touch first** (`cost/spend`, `gates/l4`) — **Config struct only; do NOT edit `writer.go` or gate-decide paths** (per §11 RISK-A). Nil falls back to `otel.Meter("<component>")`. Per `feedback_research_design_principles`: adopt the OTel SDK verbatim; if you find yourself writing > 50 LoC of metric primitives, STOP and re-spawn the design subagent. PR body MUST show one sample `/metrics` Prom-scrape line for `regatta.scheduler.tick.latency_ms` (per §2.1 double-unit-suffix lock) PLUS sampled-vs-unsampled trace ratio on a 10⁴-span/sec synthetic fixture (per §10 R11). A+ rubric scorecard mandatory in PR body. Pre-`gh pr create`: `grep -c '^\`\`\`release-notes' /tmp/pr-body.md` ≥ 1. Use `--body-file` only. Test godocs 1 line max.

### A-T0b (remaining 6 Config retrofits)

> **Task**: adds `Config.Meter metric.Meter` field to the remaining 6 Config structs (`orchestrator/scheduler`, `orchestrator/spawner`, `orchestrator/state/substrate`, `history`, `orchestrator/followup`, plus the spawner-failure-taxonomy ctor that lands in Wave-C) + retrofits constructors + updates every existing test that constructs each component. A-T0b OWNS the 6 Config retrofits; A-T3 reads-but-does-not-touch the `Config.Meter` declaration. A+ rubric scorecard + release-notes + `--body-file`.

### A-T1 (item #1 — cost dashboard tile)

> **Task**: instrument `internal/cost/spend/writer.go` (existing #283 writer; A-T1 OWNS this file across Waves A + C) — after the event row is appended, also call `meter.Float64Counter("regatta.cost.usd").Add(ctx, usd, ...)` + `meter.Int64Counter("regatta.cost.tokens").Add(ctx, n, attribute.String("direction", dir))`. Tags: `dag_id`, `operator_id`, `direction`. Add `docs/operator/dashboards/per-dag-cost.json` — stacked-bar panel "Cost USD by DAG run" + line panel "Tokens by direction." Cite `feedback_research_design_principles`. A+ rubric scorecard + release-notes + `--body-file`.

### A-T2 (item #2 — L4 metrics)

> **Task**: instrument `internal/gates/l4/gate.go` + `percategory.go` + `reload.go`: counter `regatta.l4.invocations` (tag `verdict`, `category`); histogram `regatta.l4.latency_ms`; counter `regatta.l4.cache.hits` + `regatta.l4.cache.misses`; counter `regatta.l4.second_opinion.fired`. Existing L4 paths #381 #380 #388 already hold the labels — wire from existing slog event fields. Add `docs/operator/dashboards/l4-gate.json` with 5 panels per §3 tile shape. `slo/l4-latency.yaml` ships in A-T5 — don't duplicate. A+ rubric scorecard + release-notes + `--body-file`.

### A-T3 (item #4 — scheduler tick histogram)

> **Task**: instrument `internal/orchestrator/scheduler/scheduler.go` Tick path. Open histogram `regatta.scheduler.tick.latency_ms` on tick-span close (use existing W6 tick-span lifecycle hook). Open histogram `regatta.scheduler.tick.step_duration_ms` with tag `step` for each of the 8 named steps (`dispatch`, `gate_l0`, `gate_l4`, `gate_approval`, `gate_cost`, `reaper`, `fold`, `persist`). Use ONE span around the step loop with iteration counter — NOT one span per iteration (per spec §4 anti-pattern). Add `docs/operator/dashboards/scheduler-tick.json`. A+ rubric scorecard + release-notes + `--body-file`.

### A-T4 (item #14 — daily digest)

> **Task**: new subcommand `cmd/regatta/digest.go` — `regatta digest --date YYYY-MM-DD` writes `docs/digests/2026-MM-DD.md` per §6.2 sections (loop health / PRs landed / adversarial / substrate / cost / triggers / followups). Data sources: Prom HTTP API + sqlite. Cron at `scripts/cron/daily-digest.sh` 09:00 local. Deterministic output (sort + tabulate) per A+4 rubric. Today's first digest at `docs/digests/2026-06-03.md`. **Sections 2 (PRs landed) and 3 (Adversarial findings) ship as placeholder one-liners with `[OBS-pending-emitter]` forward-reference labels per §6.2 first-digest degraded contract** — emitter ships C-T2 / D-T1; do not render zeros. **YAML front-matter block per §6.2** (one field per markdown body section; lock-step). Front-matter keys MUST be in declaration order, not map order. `TestDigest_YAMLFrontMatterMatchesBody` cross-checks every front-matter key has a corresponding body section. A+ rubric scorecard + release-notes (category `[DOCS]` for the digest content) + `--body-file`.

### A-T5 (SLO compilation)

> **Task**: write OpenSLO YAML for SLO-1 (scheduler tick) + SLO-2 (L4 latency) at `slo/scheduler-tick.yaml` + `slo/l4-latency.yaml`. `make slo-compile` (new Make target) invokes Sloth (vendor version pin at `tools/sloth/version`) and writes Prom recording + alert rules to `dashboards/prometheus/rules/`. Runbooks at `docs/operator/runbooks/scheduler-tick.md` + `docs/operator/runbooks/l4-latency.md` per the SLO. Grafana panel `docs/operator/dashboards/slo.json` shows SLO burn-rate. A+ rubric scorecard + release-notes + `--body-file`.

### A-T6 (operator metrics doc)

> **Task**: `docs/operator/observability-metrics.md` covers: OTLP-metrics env vars; Prom-vs-OTLP wire choice + when to pick each; `make provision-dashboards` workflow; SLO definitions + runbook index; cardinality budget + the AST-walk lint. ≤ 350 lines. Pre-push grep for `feedback_doc_check_banned_phrases` banned tokens. A+ rubric scorecard + release-notes (category `[DOCS]`) + `--body-file`.

### B-T1 (item #5 — substrate event-rate)

> **Task**: instrument `internal/orchestrator/state/substrate/event.go` Append path with `meter.Int64Counter("regatta.substrate.events.appended").Add(ctx, 1, attribute.String("kind", string(kind)))`. Add `docs/operator/dashboards/substrate-event-rate.json` per §3 tile shape. SLO at `slo/substrate-event-rate.yaml` with ±3σ bounds (SLO-3 renumbered, **warn-tier** per §5 amendment). A+ rubric scorecard + release-notes + `--body-file`.

### B-T2 (item #6 — HMAC chain break)

> **Task**: instrument `internal/orchestrator/state/substrate/sign.go` chain-verify path with `meter.Int64Counter("regatta.substrate.chain.break").Add(ctx, 1)` on any non-OK verify result. Critical-tier alarm rule (any non-zero increment fires immediately). `docs/operator/dashboards/substrate-chain.json` showing 0-line + alarm history. Runbook at `docs/operator/runbooks/substrate-chain-break.md`. A+ rubric scorecard + release-notes + `--body-file`.

### B-T3 (item #7 — divergence audit)

> **Task**: new file `internal/orchestrator/state/substrate/divergence_emit.go` reads from `substrate_divergence_audit` tables (#369 + #378) on insert and emits `meter.Int64Counter("regatta.substrate.divergence.detected").Add(ctx, 1, attribute.String("layer", layer))`. `docs/operator/dashboards/substrate-divergence.json` shows rate by layer. A+ rubric scorecard + release-notes + `--body-file`.

### B-T4 (item #8 — replay latency)

> **Task**: instrument `internal/history/substrate_impl.go` Replay path with `meter.Float64Histogram("regatta.replay.latency_ms")` (tag `impl=substrate` here; Temporal impl tags `impl=temporal` when it lands). `slo/replay-latency.yaml` for p99 ≤ 30s. `docs/operator/dashboards/replay.json`. A+ rubric scorecard + release-notes + `--body-file`.

### C-T1 (item #9 — dispatch attribution)

> **Task**: instrument `internal/orchestrator/spawner/spawner.go` Spawn path with `meter.Int64Counter("regatta.dispatch.subagents").Add(ctx, 1, attribute.String("template", tmpl), attribute.String("task_type", ttype), attribute.String("agent_id", aid))`. Template + task_type pulled from existing spawn metadata. `docs/operator/dashboards/dispatch.json`. A+ rubric scorecard + release-notes + `--body-file`.

### C-T2 (item #10 — PR lifecycle)

> **Task**: new package `internal/obs/prlifecycle/collector.go` — correlates dispatch span (W6 trace tree) with GitHub PR events via `pr_number` extracted from PR body fence. Emits `meter.Float64Histogram("regatta.pr.stage_duration_seconds")` (tag `stage` ∈ {`dispatch_to_first_commit`, `first_commit_to_pr_open`, `pr_open_to_ci_green`, `ci_green_to_merge`}). Reads GitHub via minimal client (or shells `gh`). `docs/operator/dashboards/pr-lifecycle.json`. **C-T2's PR also removes the placeholder line in A-T4's digest for the PRs-landed section** (per §6.2 first-digest degraded contract). A+ rubric scorecard + release-notes + `--body-file`.

### C-T3 (item #11 — per-PR cost; SHARED-OWNER WITH A-T1)

> **Task**: extend `internal/cost/spend/writer.go` to ALSO emit log event `regatta.cost.pr_attribution` with `pr_number` + `trace_id` correlation AND unlabeled aggregate counter `regatta.pr.cost_usd_total` (no `pr_number` label on the meter — per cardinality budget). **A-T1 owns this file across Wave A + Wave C per `feedback_shared_primitive_owner`** — coordinate via single follow-up PR after A-T1 merges. A+ rubric scorecard + release-notes + `--body-file`.

### C-T4 (item #12 — failure taxonomy)

> **Task**: new `internal/orchestrator/spawner/failure_taxonomy.go` — parses CI failure logs from existing spawner exit-classification into `mode` enum (`pr_lint_trip`, `doc_check_trip`, `check_tdd_trip`, `build_fail`, `test_fail`, `vet_fail`, `lint_fail`, `merge_conflict`, `timeout`, `panic`, `other`). Emits `meter.Int64Counter("regatta.dispatch.failure").Add(ctx, 1, attribute.String("mode", mode), attribute.String("template", tmpl))`. `docs/operator/dashboards/failure-modes.json` shows mode distribution. A+ rubric scorecard + release-notes + `--body-file`.

### D-T1 (item #3 — adversarial findings)

> **Task**: new `internal/orchestrator/followup/triage.go` instruments follow-up triage decisions (filed/dismissed/auto_fixed/superseded). Emits `meter.Int64Counter("regatta.adversarial.findings").Add(ctx, 1, attribute.String("fate", fate), attribute.String("severity", sev))`. `docs/operator/dashboards/adversarial.json` shows survival rate + dismissal-rate alarm tile. Runbook `docs/operator/runbooks/adversarial-dismissal.md` defines fate-enum semantics + quarterly audit. **D-T1's PR also removes the placeholder line in A-T4's digest for the Adversarial-findings section** (per §6.2 first-digest degraded contract). A+ rubric scorecard + release-notes + `--body-file`.

### D-T2 (item #13 — `regatta status` TUI)

> **Task**: new `cmd/regatta/status.go` using `github.com/charmbracelet/bubbletea` (**add to go.mod** — not currently present; indirect `charmbracelet/*` deps don't count). **5 panels per §6.1 panel-budget table — single-screen on 80×24** (Loop-pulse / Cost / L4 / PR-pipeline / Alarms). Triggers move to a sibling subcommand `regatta triggers` (D-T3 owns that subcommand alongside the gauge emitter). Reads Prom HTTP API + sqlite; graceful degradation banner when Prom unreachable per R5 mitigation. Render budget < 3 s cold per A+3 rubric. A+ rubric scorecard + release-notes (`[FEATURE]`) + `--body-file`.

### D-T3 (item #15 — trigger clock)

> **Task**: new `internal/obs/triggers/clock.go` exports `DaysRemaining(trigger string) int` over `30_day_green` (derives from the PR-stage histogram `regatta_pr_stage_duration_seconds_count{stage="ci_green_to_merge"}` emitted by **C-T2 — DO NOT DISPATCH D-T3 BEFORE C-T2 MERGES** or the gauge reads zero) + `external_customer_signal` (sealed input from `slo/triggers.yaml`) + `phase_g_gate`. Register `meter.Int64ObservableGauge("regatta.trigger.days_remaining")` with a callback that records `DaysRemaining(t)` for each trigger. (OTel Go SDK ≥ v1.32 ships sync `Float64Gauge`/`Int64Gauge`; the **observable** gauge is still the correct pick here because `DaysRemaining(t)` is a sampled derivation, not a write-on-event measurement — async gauge is the OTel-blessed shape for this.) `docs/operator/dashboards/trigger-clock.json` stat-tile per trigger. **Also ships in D-T3 (relocated from §6.1)**: new sibling subcommand `cmd/regatta/triggers.go` — one stat-line per trigger ("`30_day_green: 17 days remaining`"). No bubbletea, no TUI; plain stdout. Coordinates with D-T2's `regatta status` (which links to `regatta triggers` in a footer line). No manual gauge override per R10 mitigation. A+ rubric scorecard + release-notes + `--body-file`.

---

## §13 Cross-wedge contracts + Phase-X hand-off

- **W6 (#159) seam**: `Config.Meter metric.Meter` mirrors `Config.Tracer trace.Tracer`. Same nil-fallback rule.
- **W8 multi-tenant**: every metric resolves `tenant_id` from `ctx` via the W8 lookup at emit-time. A-T0a hardcodes `default` per W6 §3.1; W8 swaps for `ctx`-derived lookup in one line.
- **W9 replay**: B-T4 instruments DurableHistory's substrate impl; when Temporal impl lands, it tags `impl=temporal` and is comparable on the same dashboard.
- **Cost governor**: A-T1 + C-T3 + the existing token_spend writer are all on `internal/cost/spend/writer.go` — A-T1 is the shared owner; downstream waves coordinate via PR sequence. A-T0a's Config retrofit is fenced to `config.go` only (per §11 RISK-A).
- **Self-host scope**: every metric/dashboard/SLO designed for single-tenant single-operator default. Multi-tenant lift is a 1-line `tenant_id` swap at Phase-X gate.

---

## Appendix A — adopted-OSS dependency manifest

```
go.opentelemetry.io/otel/metric                              v1.x  (semver-locked at major v1)
go.opentelemetry.io/otel/sdk/metric                          v1.x
go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc  v1.x
go.opentelemetry.io/otel/exporters/prometheus                v0.x  (stable; tracking promotion)
github.com/charmbracelet/bubbletea                            v0.27+ (TUI for regatta status — candidate, NOT yet in go.mod; D-T2 adds it. Only indirect charmbracelet/* deps present today)

# vendored tools
sloth (binary pin at tools/sloth/version)                     v0.11+
OpenSLO spec                                                  v1alpha
Grafana JSON schema (vendored at tools/grafana-schema/)       v11.x

# already shipped in #159 — referenced, not added
go.opentelemetry.io/otel                                       v1.x
go.opentelemetry.io/otel/trace                                 v1.x
go.opentelemetry.io/otel/log                                   v0.x
go.opentelemetry.io/otel/semconv/v1.41.0                        v1.41.0
```

Renovate bot manages bumps.

---

## Appendix B — why each design choice picked the adopted OSS over bespoke

| Choice | Bespoke option considered | Why adopted-OSS won |
|---|---|---|
| Metric SDK | Prom client_golang only | One SDK (OTel) emits OTLP + Prom + Honeycomb via exporter swap; operator picks wire format without regatta code change. Per W6 precedent. |
| Dashboard format | grafonnet (jsonnet DSL) | Plain Grafana JSON has zero new toolchain. CI lints against vendored schema. Operator can edit in-browser + paste back. Per `feedback_decision_priority` UX > best-prac. |
| SLO definition | bespoke PromQL hand-written | OpenSLO is the CNCF-sandbox vendor-neutral spec; Sloth compiles to Prom rules. Backend swap survives without rewriting SLOs. |
| Per-PR cost attribution | metric labelled by `pr_number` | Cardinality cliff. Log + trace correlation is the OTel-blessed shape per semconv anti-pattern docs. |
| Trigger-clock gauge derivation | manual operator override | Goodhart's law. SLO-3-source histogram + git history are the sealed inputs. |
| TUI library | `tview` / hand-rolled tcell | `bubbletea` adopted at D-T2 (added to go.mod then); Elm-architecture renders + tests cleanly. Scored comparison in §1.9. |

---

_End of converged spec. This is the single source of truth for the observability roadmap; PR #400 + #405 + #410 + #413 + #420 are superseded._
