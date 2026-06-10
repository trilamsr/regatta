---
status: phase-x-deferred
deferred_on: 2026-06-10
---
# OBS-C Agent-loop telemetry — Design Spec

Status: ready for review
Date: 2026-06-02
Author: designer subagent (Phase OBS-C wave dispatch — 4 items combined)
Parent spec: [`2026-06-02-observability-roadmap.md`](2026-06-02-observability-roadmap.md) (#432)
Items in scope (all 4): `obs-wave-c-c-t1-dispatch-subagents.md`, `obs-wave-c-c-t2-pr-lifecycle.md`, `obs-wave-c-c-t3-per-pr-cost-attribution.md`, `obs-wave-c-c-t4-failure-taxonomy.md`
Depends on: OBS-WAVE-A-T0b (Config.Meter retrofit), OBS-WAVE-A-T1 (`internal/cost/spend/writer.go` shared-owner pin); successor wave OBS-D consumes the surfaces this spec lands.
Memory rules in force: `feedback_research_design_principles`, `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_unaddressed_load_bearing`, `feedback_shared_primitive_owner`, `feedback_spec_pattern_authority`, `feedback_pr_body_release_notes_fence`, `feedback_pr_body_file_only`, `feedback_test_godoc_one_line`, `feedback_design_iteration_local`, `feedback_no_signatures`, `feedback_deletion_default`.

---

## §1 Problem

After OBS-A wired the OTel meter scaffold + `Config.Meter` retrofit and OBS-B instrumented substrate health (event-rate, chain-break, divergence, replay), the operator still has zero structured visibility into the autonomous loop itself. Specifically:

1. **Which subagent ran which task** — the spawner dispatches implementer / reviewer / designer / triage subagents across templates and task types, but the rate, latency, and outcome of dispatches are emitted only as ad-hoc log lines. No dashboard panel; no SLO surface; no exemplar drill from a hot template back to a trace.
2. **How long each PR took to land** — the operator sees PR list via `gh pr list`, but the time-from-dispatch to first-commit, first-commit to PR-open, PR-open to CI-green, CI-green to merge is invisible. Slow stages stay slow because nobody measures them.
3. **How much each PR cost** — token-spend lands in `internal/cost/spend/writer.go` via OBS-A-T1, but the writer carries no `pr_number` attribution surface. Operator can see total spend; cannot see "PR #561 cost $4.20."
4. **Recurring failure modes** — when a dispatch fails, the failure log is free-text. Operator cannot answer "what fraction of failures this week are `merge_conflict` vs `lint_fail` vs `timeout`?" without manual log triage.

OBS-C closes those four gaps with one coordinated wave (C-T1, C-T2, C-T3, C-T4). The cardinality budget from spec §2.2 of the parent roadmap is preserved: `pr_number` stays banned from metric labels, and per-PR attribution flows via spans + structured log events with `trace_id` correlation. The decision priority is **performance** (telemetry must not block the dispatch hot path) → **UX** (operator dashboards reveal loop health at a glance) → **best-practices** (OTel semconv compliance, OpenSLO SLO definitions) → **velocity** (4 file-disjoint tasks dispatch in parallel after C-T1 lands first).

**Why this matters now.** The autonomous loop is the product. The operator can already see the substrate is healthy (OBS-B) and the SDK is wired (OBS-A); without OBS-C, the loop's own behaviour is invisible — slow dispatches, expensive PRs, and recurring failure modes accumulate without measurement. The 30-day-green Phase-S → Phase-G trigger gauge depends on stable per-PR cost + per-stage latency baselines, which OBS-C is the first wave to surface.

---

## §2 T1 — Dispatch-subagents trace span + counter

### 2.1 Surface

Instrument `internal/orchestrator/spawner/spawner.go` Spawn path. On every subagent dispatch, open a span and emit a counter:

```go
// Span — envelopes the subagent invocation.
ctx, span := tracer.Start(ctx, "regatta.dispatch.subagent",
    trace.WithAttributes(
        attribute.String("kind", kind),           // implementer|reviewer|designer|triage
        attribute.String("template", template),   // ≤ 30 enums (parent §2.2)
        attribute.String("task_type", taskType),  // ≤ 20 enums
        attribute.String("agent_id", agentID),    // ≤ 50 enums
        attribute.String("task_id", taskID),      // SPAN-ONLY (never a metric label)
        attribute.String("model", model),         // SPAN-ONLY (≤ 5 enums in practice)
    ))
defer span.End()
// ... subagent runs ...
span.SetAttributes(
    attribute.Int64("input_tokens", inputTokens),
    attribute.Int64("output_tokens", outputTokens),
    attribute.Float64("duration_seconds", dur.Seconds()),
    attribute.String("exit_outcome", outcome),     // success|refused|error|timeout
)

// Counter — total dispatches, bounded labels only.
meter.Int64Counter("regatta.dispatch.subagents").Add(ctx, 1,
    attribute.String("kind", kind),
    attribute.String("template", template),
    attribute.String("task_type", taskType),
    attribute.String("agent_id", agentID))

// Histogram — duration distribution per subagent kind.
meter.Float64Histogram("regatta.dispatch.subagent_duration_seconds",
    metric.WithUnit("s")).Record(ctx, dur.Seconds(),
    attribute.String("kind", kind),
    attribute.String("template", template))

// Outcome counter — required for failure-rate SLI.
meter.Int64Counter("regatta.dispatch.subagent_outcome").Add(ctx, 1,
    attribute.String("kind", kind),
    attribute.String("outcome", outcome))
```

### 2.2 Cardinality math

- Counter: `kind` (4) × `template` (≤ 30) × `task_type` (≤ 20) × `agent_id` (≤ 50) ⇒ ceiling 120 000 cells; steady-state cross-product far smaller because `template` × `task_type` is not Cartesian (most templates serve one task_type). Property test `TestDispatchSpan_CrossProductWithinBudget` measures real cross-product against the spec §2.2 hard cap of 200 distinct values per label-7-day-window.
- Histogram: `kind` (4) × `template` (≤ 30) ⇒ 120 cells. Safe.
- Outcome counter: `kind` (4) × `outcome` (4) = 16 cells. Trivially safe.
- `task_id` and `model` are span-only attributes — the OTel SDK exemplar pipeline attaches `trace_id` to the counter, and the operator drills counter → exemplar → trace → span to see `task_id` / `model` / token counts.

### 2.3 Meter resolution

`Config.Meter` from OBS-A-T0b retrofit. Nil falls back to `otel.Meter("orchestrator/spawner")` (covered by `TestDispatchCounter_NilMeterFallback`).

### 2.4 Why exemplars over a per-PR label

Adding `pr_number` to the dispatch counter would breach the cardinality budget (spec §2.2 lists `pr_number` as banned). The trace-exemplar drill path (metric → exemplar `trace_id` → span — which carries `pr_number` and `task_id` as span attributes) gives the operator the same drill UX at zero metric-label cost. OTel SDK auto-attaches `trace_id` exemplars on counters when sampling captures the parent span (§2.5 head-sampling policy already pins `ParentBased(TraceIDRatioBased(0.1))` + error-override = always-sample).

---

## §3 T2 — PR-lifecycle stages

### 3.1 Stage enum (5 stages — matches parent spec §2.2)

`dispatch_to_first_commit` → `first_commit_to_pr_open` → `pr_open_to_ci_green` → `ci_green_to_merge` → `merged`. A stage-transition reaching `failed` is a terminal stage emitted by C-T4's failure-taxonomy collector (cross-wedge contract — same transition shape, different terminal node).

Parent spec §3 Tier-2 row #10 names these stages verbatim; this spec inherits them unchanged. The 5-enum ceiling is hard — adding a 6th stage MUST update parent §3 item #10 in the same PR.

### 3.2 Surface

New file `internal/obs/prlifecycle/collector.go` (path-exclusive — does not touch spawner or cost/spend). The collector correlates two streams:

1. **Dispatch spans** (from T1) carry `pr_number` and `task_id` as span attributes — when a PR opens with a head branch matching `regatta/agent-{agent_id}`, the collector joins by `agent_id` to find the parent dispatch trace and records the elapsed time as `dispatch_to_first_commit` (timer started on T1 span open, stopped on first-commit webhook).
2. **GitHub PR events** (open / review-requested / approved / merged) via `gh` CLI shell — same primitive `internal/orchestrator/prwatch` already uses (see `2026-06-02-orchestrator-pr-watcher.md`).

On correlation, emit:

```go
meter.Float64Histogram("regatta.pr.stage_duration_seconds",
    metric.WithUnit("s")).Record(ctx, durationS,
    attribute.String("stage", stage),  // 5 enums only
    attribute.String("template", template)) // ≤ 30 enums; useful for SLO breakdown by dispatch shape

meter.Int64Counter("regatta.pr.stage_transitions").Add(ctx, 1,
    attribute.String("from_stage", fromStage),
    attribute.String("to_stage", toStage))
```

`pr_number` stays banned from metric labels; flows via the dispatch span attribute (T1) + the substrate event `pr_stage_transition` (see §3.4).

### 3.3 Stage-ordering invariant

Stages MUST be monotone. Out-of-order GitHub events (a review pushed before the open webhook is observed due to event reordering, or a late-arrival merge for a PR the collector missed open on) are dropped with a warn-log + a `regatta.pr.lifecycle.out_of_order` counter increment (tagged `stage` only — same enum set, cardinality unchanged). Test fixture `TestPRLifecycle_OutOfOrderEventsDropped` covers reorder + late-arrival.

### 3.4 Substrate event emission

Stage transitions also append a `pr_stage_transition` substrate event (HMAC-chained — uses existing `internal/orchestrator/state/substrate/event.go` Append path). Payload: `{pr_number, from_stage, to_stage, trace_id, duration_seconds}`. The substrate event is the long-term audit trail; the metric is the operational signal. Append is async (channel-buffered, batched per 100ms) to keep the PR-watch hot path off the substrate Append latency.

### 3.5 GitHub API rate-limit handling

`gh` CLI inherits the operator's auth token and surfaces HTTP 403 / 429 on rate-limit hit. The collector retries with exponential backoff (`1s, 2s, 4s, 8s`) up to 4 attempts; on persistent failure, log a warn + increment `regatta.pr.lifecycle.gh_rate_limited` counter (no tags — single series) so the operator can spot a quota-burn against the steady-state event rate. Surface on the OBS-A-T6 operator runbook — no new dashboard.

### 3.6 Meter resolution

`internal/obs/prlifecycle/config.go` Config struct (Config.Meter field added inline in this PR; not part of OBS-A-T0b's retrofit list because the package is new in this wave). Nil falls back to `otel.Meter("obs/prlifecycle")` (covered by `TestPRLifecycle_NilMeterFallback`).

---

## §4 T3 — Per-PR cost attribution

### 4.1 Surface (extends OBS-A-T1's `internal/cost/spend/writer.go`)

Two additions in one PR. **A-T1 is the shared owner** of `writer.go` across Waves A + C per `feedback_shared_primitive_owner`; C-T3 dispatch MUST wait for A-T1 to merge.

1. **Structured log event on every spend write** — carries the full attribution set:

```go
slog.LogAttrs(ctx, slog.LevelInfo, "regatta.pr.cost_usd",
    slog.Int64("pr_number", prNumber),
    slog.String("dag_id", dagID),
    slog.String("operator_id", operatorID),
    slog.String("trace_id", traceIDFromCtx(ctx)),
    slog.Float64("usd", usd),
    slog.Int64("input_tokens", inputTokens),
    slog.Int64("output_tokens", outputTokens),
    slog.Int64("usd_micro", int64(usd*1_000_000))) // for integer SUM queries
```

`pr_number` is on the log only — NEVER on the metric. The log event is the per-PR attribution surface; downstream SQL aggregation lives on the log warehouse.

2. **Unlabeled aggregate counter** (dashboard headline only):

```go
meter.Float64Counter("regatta.pr.cost_usd_total",
    metric.WithUnit("usd")).Add(ctx, usd)
```

No tags. Cardinality = 1 series total. The dashboard stat panel "Total cost USD (since boot)" reads this directly. Per-PR attribution flows via the log event; cost-per-PR roll-up to the dashboard ships as a **Prom recording rule** (preferred) OR sqlite-view fallback — filed as `[OBS-followup]` issue per parent spec §9 #3.

### 4.2 Idempotency / double-count avoidance

Token-spend writes are already idempotent via the existing `event_token_spend` uniqueness constraint on (`request_id`, `pr_number`) (OBS-A-T1 lands the schema; C-T3 inherits). Retries of a failed spend write hit the unique index and no-op — counter is incremented only on first successful insert. Test `TestPRCost_IdempotencyOnRetry` writes the same (`request_id`, `pr_number`) tuple 3× and asserts `regatta_pr_cost_usd_total` advances exactly once.

### 4.3 Aggregate SQL query (operator drill)

The roll-up follow-up issue specifies the per-PR query shape so the recording-rule author has no ambiguity:

```sql
SELECT SUM(usd_micro) AS total_usd_micro
  FROM event_token_spend
 WHERE pr_number = $1;
```

Equivalent PromQL recording rule (when the rule lands):

```promql
sum by (pr_number) (regatta_pr_cost_usd_micro_log_event_total)
```

The `_log_event_total` series exists only as a Loki / log-warehouse aggregation; it never enters Prom directly. Decision is closed (per parent §11 RISK-B Wave-C kickoff resolution), not a kickoff-time open question.

### 4.4 Dashboard

Extends `docs/operator/dashboards/per-dag-cost.json` (owned by OBS-A-T1):

- Stat panel "Total cost USD (since boot)" — `regatta_pr_cost_usd_total`.

---

## §5 T4 — Failure-mode taxonomy

### 5.1 Enum (11 buckets — covers ≥ 8 modes from 30-d CI history per Wave-C exit gate)

`lint_fail`, `test_fail`, `compile_fail`, `timeout`, `oom`, `network_flake`, `merge_conflict`, `permission_denied`, `dep_missing`, `policy_block`, `other`.

The `≤ 20` cardinality ceiling (parent §2.2 `mode` row) leaves 9 spare slots — a new bucket can land at PR-time without a cardinality re-budget. Adding a 12th mode MUST update parent §3 item #12 in the same PR. Reserved bucket `other` catches unparseable logs (paired with `TestFailureTaxonomy_UnknownModeRouteToOther`).

### 5.2 Surface

New file `internal/orchestrator/spawner/failure_taxonomy.go` (path-exclusive new surface):

```go
meter.Int64Counter("regatta.dispatch.failure").Add(ctx, 1,
    attribute.String("mode", mode),
    attribute.String("template", template))
```

Tag set: `mode` (≤ 20 enums) × `template` (≤ 30 enums) ⇒ 600 cells. Safe.

### 5.3 Classifier shape — rule-table first, LLM verify-only fallback

The classifier is a **regex-table** keyed on canonical CI log signatures. Each rule is `{pattern, mode}`; first-match wins; default `other`. The known-modes corpus lives at `internal/orchestrator/spawner/testdata/failure_taxonomy/ci_failures_30d.txt` (one log excerpt per file; ≥ 8 mode coverage asserted by `TestFailureTaxonomy_KnownModesCoverage`).

**LLM-driven classification is deliberately out of scope** for the hot path. Reasoning:

- Latency: an LLM call on the PR-fail path adds 5–30s and bumps cost-cap pressure.
- Determinism: LLM output is non-deterministic; the W6 verify-only audit posture (from #550) explicitly bans LLM in the operational signal path.
- Sufficiency: 30-d CI history covers ≥ 8 known modes; the regex table reaches ≥ 95% coverage on real logs (`other` < 5%), which is the Wave-C exit gate.

If the `other` bucket exceeds 10% sustained over 7 days, a follow-up issue (`[OBS-followup] Failure-taxonomy verify-only LLM second-opinion`) is filed; the LLM ships only as an offline labelling tool that proposes new regex rules — operator reviews and merges as code. **Never** as a runtime classifier.

If the regex implementation grows past 100 LoC, STOP and re-spawn the design subagent per `feedback_research_design_principles`.

### 5.4 Meter resolution

Spawner-failure-taxonomy ctor's `Config.Meter` field — explicitly included in OBS-A-T0b's 6-component retrofit set. Nil falls back to `otel.Meter("orchestrator/spawner")`.

### 5.5 Dashboard

New file `docs/operator/dashboards/failure-modes.json`:

1. Stacked-bar "Failures by mode (1h rolling)" — `sum by (mode) (increase(regatta_dispatch_failure_total[1h]))`.
2. Heatmap "Mode × template" — `sum by (mode, template) (rate(regatta_dispatch_failure_total[5m]))`.
3. Top-5 "Hottest failure modes (last 24h)" — `topk(5, sum by (mode) (increase(regatta_dispatch_failure_total[24h])))`.

---

## §6 Dashboard JSON additions — 6 panels across 3 files

Coordinate edit ordering: C-T1 lands `dispatch.json` first; C-T2 extends it; C-T3 extends OBS-A-T1's `per-dag-cost.json`; C-T4 lands new `failure-modes.json`.

| Panel | Dashboard | Query |
|---|---|---|
| Subagent latency p95 by kind | `dispatch.json` (C-T1 owns) | `histogram_quantile(0.95, sum by (le, kind) (rate(regatta_dispatch_subagent_duration_seconds_bucket[5m])))` |
| Dispatch throughput (stacked by template) | `dispatch.json` (C-T1) | `sum by (template) (rate(regatta_dispatch_subagents_total[1m]))` |
| PR lifecycle Sankey (stage → stage) | `pr-lifecycle.json` (C-T2 owns) | `sum by (from_stage, to_stage) (rate(regatta_pr_stage_transitions_total[1h]))` |
| PR stage p95 by stage | `pr-lifecycle.json` (C-T2) | `histogram_quantile(0.95, sum by (le, stage) (rate(regatta_pr_stage_duration_seconds_bucket[5m])))` |
| Cost-per-PR top-20 (log-warehouse panel) | `per-dag-cost.json` (extends OBS-A-T1) | log-query `SUM(usd_micro) GROUP BY pr_number ORDER BY 1 DESC LIMIT 20` |
| Failure-taxonomy pie | `failure-modes.json` (C-T4 owns) | `sum by (mode) (increase(regatta_dispatch_failure_total[24h]))` |

The "cost-vs-failure scatter" panel is deferred to Wave-D (operator surface wave) because it needs the OBS-A-T1 log warehouse → Grafana datasource link that A-T6 lands; filed in the same `[OBS-followup]` cost-per-agent rollup issue.

---

## §7 SLO YAML additions — 2 new SLOs

OpenSLO definitions at `slo/dispatch-subagent.yaml` + `slo/pr-lifecycle.yaml`. Sloth compiles to Prom recording + alert rules into `dashboards/prometheus/rules/`.

### SLO-5 — Subagent dispatch latency

```yaml
apiVersion: openslo/v1
kind: SLO
metadata:
  name: dispatch-subagent-latency
  displayName: Subagent dispatch p95 latency
spec:
  service: regatta
  indicator:
    metadata:
      name: dispatch-subagent-duration-p95
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            spec:
              source: prometheus
              query: |
                sum(rate(regatta_dispatch_subagent_duration_seconds_bucket{le="120"}[5m]))
        total:
          metricSource:
            spec:
              source: prometheus
              query: |
                sum(rate(regatta_dispatch_subagent_duration_seconds_count[5m]))
  objectives:
    - target: 0.95
      window: 7d
  description: 95% of subagent dispatches complete in under 2 minutes.
```

### SLO-6 — PR lifecycle stage p95

```yaml
apiVersion: openslo/v1
kind: SLO
metadata:
  name: pr-lifecycle-stage-p95
  displayName: PR lifecycle stage p95 duration
spec:
  service: regatta
  indicator:
    metadata:
      name: pr-stage-duration-p95
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            spec:
              source: prometheus
              query: |
                sum(rate(regatta_pr_stage_duration_seconds_bucket{le="3600"}[5m]))
        total:
          metricSource:
            spec:
              source: prometheus
              query: |
                sum(rate(regatta_pr_stage_duration_seconds_count[5m]))
  objectives:
    - target: 0.95
      window: 30d
  description: 95% of PR lifecycle stages complete in under 1 hour.
```

Runbooks live at `docs/operator/runbooks/dispatch-subagent-latency.md` + `docs/operator/runbooks/pr-lifecycle-stage-p95.md` (per parent §8 A3 — "operator runbook exists for every critical-tier alarm").

---

## §8 Wiring — minimal surface, OTel-idiomatic

| Item | New files | Modified files | Substrate event additions |
|---|---|---|---|
| C-T1 | `docs/operator/dashboards/dispatch.json` | `internal/orchestrator/spawner/spawner.go` | none |
| C-T2 | `internal/obs/prlifecycle/collector.go`, `internal/obs/prlifecycle/config.go`, `docs/operator/dashboards/pr-lifecycle.json` | `cmd/regatta/digest.go` (removes A-T4 placeholder for PRs-landed) | `pr_stage_transition` event (new kind, registered in `EventKind` enum) |
| C-T3 | none | `internal/cost/spend/writer.go` (A-T1 owner), `docs/operator/dashboards/per-dag-cost.json` (A-T1 owner) | none (cost spend already lands an event via A-T1) |
| C-T4 | `internal/orchestrator/spawner/failure_taxonomy.go`, `internal/orchestrator/spawner/testdata/failure_taxonomy/ci_failures_30d.txt`, `docs/operator/dashboards/failure-modes.json` | `internal/orchestrator/spawner/spawner.go` (calls classifier on Spawn exit-error path) | none |
| Cross | `slo/dispatch-subagent.yaml`, `slo/pr-lifecycle.yaml`, `docs/operator/runbooks/dispatch-subagent-latency.md`, `docs/operator/runbooks/pr-lifecycle-stage-p95.md` | none | none |

OTel SDK reuse: same `Config.Meter` pattern as OBS-A. No new SDK, no new exporter, no new transport — every meter call flows through the existing W6 OTLP pipe.

---

## §9 Risks (10) — adversarial red-team

### R1 — Cardinality blow-up via `pr_number` smuggled onto a counter

**Threat**: An implementer adds `pr_number` to the dispatch counter labels "because the per-PR drill would be easier."
**Mitigation**: Parent §2.2 AST-walk lint (`TestMetricCardinality_PRNumberLabelBanned`) already blocks this; this spec adds `task_id` to the banned label list in the same lint update.
**Verify**: `TestMetricCardinality_PRNumberLabelBanned` + `TestMetricCardinality_TaskIDLabelBanned` (new).

### R2 — Substrate `pr_stage_transition` event Append blocks PR-watch hot path

**Threat**: Synchronous substrate Append on every stage transition adds latency to PR polling and stalls the scheduler.
**Mitigation**: Channel-buffered, batched-100ms async writer in `internal/obs/prlifecycle/collector.go`. Buffer overflow drops to `regatta.pr.lifecycle.append_drop` counter (no tags); operator sees back-pressure as a non-zero series.
**Verify**: `TestPRLifecycle_AppendIsAsync` (load test asserts P99 of `Collect()` call < 1ms even when substrate Append is artificially slowed).

### R3 — Failure-taxonomy regex classifier latency on PR-fail path

**Threat**: Regex evaluation on a multi-MB CI failure log adds seconds to the fail-handling path.
**Mitigation**: Regex table runs against the last 8KB of the log only (failure signature lives near the end in 99% of cases). Property test sweeps the 30-d corpus and asserts P95 classify latency < 5ms.
**Verify**: `TestFailureTaxonomy_ClassifyLatencyP95Under5ms`.

### R4 — Subagent span attribute `model` leaks API key fragment

**Threat**: The `model` string is read from operator config; a typo or copy-paste could embed `sk-…` substrings, which then ship to OTLP collectors.
**Mitigation**: `sanitizeModelAttr(string) string` whitelists model name shape (`^[a-z0-9._-]{1,40}$`); anything outside the whitelist becomes the literal string `invalid_model`. Same sanitizer applies to `template` and `task_type`.
**Verify**: `TestDispatchSpan_ModelAttrSanitized` (table-driven; covers known-good models + 5 injection patterns).

### R5 — Per-PR cost double-counts on retry

**Threat**: A failed spend write retries, both the original and retry increment `regatta.pr.cost_usd_total`.
**Mitigation**: Idempotency via `event_token_spend` unique index on (`request_id`, `pr_number`); counter increment only on first-insert path (after the INSERT returns rows-affected = 1).
**Verify**: `TestPRCost_IdempotencyOnRetry` (writes same tuple 3×; asserts counter += 1, not 3).

### R6 — Stage-transition reordering causes negative duration

**Threat**: `ci_green_to_merge` event arrives before `pr_open_to_ci_green` is recorded; duration math underflows.
**Mitigation**: Monotone-stage invariant (§3.3); out-of-order events dropped to `regatta.pr.lifecycle.out_of_order` counter.
**Verify**: `TestPRLifecycle_OutOfOrderEventsDropped`.

### R7 — `dispatch.json` panels reference metric names that drift from emitted

**Threat**: Implementer renames a metric in code; dashboard JSON references the old name; panels go blank silently.
**Mitigation**: `TestDashboardMetricNames_MatchEmitted` (parent §8 A2) AST-walks every dashboard JSON, extracts metric refs, asserts each appears in the AST-discovered counter/histogram/gauge list.
**Verify**: `TestDashboardMetricNames_MatchEmitted` (existing; extended to cover the 3 new dashboards).

### R8 — Outcome enum drift between counter and span attribute

**Threat**: Span attribute uses `success`/`refused`/`error`/`timeout`; counter label uses `succeeded`/`refused`/`failed`/`timed_out`; SLI queries break.
**Mitigation**: Single `dispatchOutcome` typed enum in `internal/orchestrator/spawner/outcome.go` (existing package); span attr and counter label both read `.String()` on the same enum value. Lint test asserts no string literals match outcome shape outside the enum constructor.
**Verify**: `TestDispatchOutcome_SingleSourceOfTruth`.

### R9 — GitHub `gh` CLI rate-limit cascades silently

**Threat**: A burst of PR events trips the GitHub secondary rate limit; the collector backs off but the operator never notices.
**Mitigation**: `regatta.pr.lifecycle.gh_rate_limited` counter on every 403/429; SLO-6 burn-rate alert fires when PR-lifecycle data stops flowing.
**Verify**: `TestPRLifecycle_RateLimitCounterIncrements`.

### R10 — Wave-C dispatch races A-T1 on `internal/cost/spend/writer.go`

**Threat**: C-T3 implementer opens a PR before A-T1 merges; merge conflict + reviewer churn.
**Mitigation**: `feedback_shared_primitive_owner` pin — C-T3 dispatch prompt MUST cite A-T1 merge precondition. Roadmap §7 Wave-C table marks A-T1 as the shared owner of `writer.go` across both waves.
**Verify**: dispatch checklist item 6 in C-T3 acceptance criteria (PR body cites A-T1 shared-owner pin + waits for A-T1 merge).

### R11 — `failure_taxonomy` corpus drifts from real CI without anyone noticing

**Threat**: The `testdata/failure_taxonomy/ci_failures_30d.txt` corpus snapshots 2026-06-02 CI failures; 90 days later the failure distribution has shifted and the `other` bucket silently absorbs new modes.
**Mitigation**: `regatta.dispatch.failure{mode="other"}` rate ≥ 10% of total over 7 days triggers `[OBS-followup] Failure-taxonomy corpus refresh`. Operator runbook for the SLO names this trigger.
**Verify**: alarm rule lives in `dashboards/prometheus/rules/failure-modes.yaml`.

---

## §10 Test plan — 12+ test names (1-line godocs)

Test godocs are 1-line maximum per `feedback_test_godoc_one_line` (enforced by `scripts/doc-check.sh`).

1. `TestDispatchSpan_EmitsKindAndTemplate` — verify Spawn emits the dispatch span with required attributes.
2. `TestDispatchCounter_NilMeterFallback` — verify nil Config.Meter falls back to `otel.Meter("orchestrator/spawner")` without panic.
3. `TestDispatchSpan_CrossProductWithinBudget` — property test sweeps 200 dispatches across template × task_type and asserts cardinality stays under spec §2.2 cap.
4. `TestDispatchSpan_ModelAttrSanitized` — table-driven coverage of model-attr whitelist plus 5 injection patterns.
5. `TestDispatchOutcome_SingleSourceOfTruth` — AST-walk asserts no outcome-shaped string literals exist outside the typed enum.
6. `TestPRLifecycle_NilMeterFallback` — verify nil Config.Meter falls back to `otel.Meter("obs/prlifecycle")` without panic.
7. `TestPRLifecycle_OutOfOrderEventsDropped` — fixture covers stage-reorder and late-arrival merge.
8. `TestPRLifecycle_AppendIsAsync` — load test asserts Collect() P99 under 1ms even when substrate Append is artificially slowed.
9. `TestPRLifecycle_RateLimitCounterIncrements` — fake gh CLI returns 429; counter advances and operator-visible warn-log fires.
10. `TestPRCost_IdempotencyOnRetry` — write same (request_id, pr_number) 3 times; counter advances by 1 only.
11. `TestPRCost_LogEventCarriesFullAttribution` — log event contains pr_number, dag_id, operator_id, trace_id, usd, tokens.
12. `TestFailureTaxonomy_NilMeterFallback` — verify nil Config.Meter falls back without panic.
13. `TestFailureTaxonomy_KnownModesCoverage` — cross-references `mode` enum against the 30-d corpus, asserts ≥ 8 buckets observed.
14. `TestFailureTaxonomy_UnknownModeRouteToOther` — unparseable log routes to `other`; counter advances.
15. `TestFailureTaxonomy_ClassifyLatencyP95Under5ms` — property test sweeps the 30-d corpus, asserts P95 classify latency under 5ms.
16. `TestDashboardMetricNames_MatchEmitted` — extended coverage for `dispatch.json`, `pr-lifecycle.json`, `failure-modes.json`.
17. `TestDashboardJSON_LintsAgainstSchema` — Grafana JSON schema validation across the 3 new dashboards (A+ rubric).
18. `TestSLOYaml_ParsesAsOpenSLO` — `slo/dispatch-subagent.yaml` + `slo/pr-lifecycle.yaml` parse cleanly via Sloth's OpenSLO loader.
19. `TestMetricCardinality_TaskIDLabelBanned` — AST-walk asserts `task_id` never appears as a metric label (only as span attr).

---

## §11 A+ grade rubric

### B (floor — ships)

- B1. `make check` clean across all 4 PRs.
- B2. AST-walk lint passes (`pr_number`, `task_id`, `run_id`, `work_item_id` not on any metric label).
- B3. 4 dashboard JSONs checked in: `dispatch.json` (new), `pr-lifecycle.json` (new), `per-dag-cost.json` (extended), `failure-modes.json` (new).
- B4. 2 OpenSLO YAMLs checked in: `slo/dispatch-subagent.yaml`, `slo/pr-lifecycle.yaml`.
- B5. Release-notes fence + A+ scorecard in every PR body; `--body-file` form (no HEREDOC backtick traps).
- B6. C-T2 PR body shows non-zero `regatta_pr_stage_duration_seconds_count` series on a real PR (satisfies the OBS-D handoff gate that the parent spec §6.2 first-digest degraded contract names).

### A (target — expected)

- A1. Adversarial reviewer subagent clears each PR ("no unresolved findings").
- A2. Every metric in OBS-C has at least one Grafana panel referencing it AND at least one SLO recording rule OR alarm rule. Verify: `TestDashboardMetricNames_MatchEmitted` + `slo/*.yaml` cross-reference.
- A3. Operator runbooks exist for SLO-5 + SLO-6. Verify: `TestRunbook_ExistsForEveryCriticalAlarm` (parent §8 A3).
- A4. Cardinality budget audit clean — every new label (`kind`, `stage`, `from_stage`, `to_stage`, `mode`, `outcome`) documented in parent §2.2. Verify: `TestTagCardinality_LabelsHaveDocumentedBudget`.
- A5. Every named-but-deferred sub-decision filed as `[OBS-followup]` per `feedback_unaddressed_load_bearing`. This wave files: (a) cost-per-agent rollup recording rule (carried forward from parent §9 #3 — C-T3 ownership), (b) cost-vs-failure scatter panel (deferred to Wave-D), (c) failure-taxonomy verify-only LLM second-opinion (filed only if `other` > 10% sustained 7d), (d) failure-taxonomy corpus refresh trigger (R11). Verify: `gh issue list --label OBS-followup`.
- A6. T1 PR body shows one exemplar-drill demo: counter → trace exemplar `trace_id` → span carrying `task_id` + `model` + token counts. Verify: PR body screenshots or paste of the Jaeger drill URL.

### A+ (stretch — exceptional)

- A+1. `TestDashboardJSON_LintsAgainstSchema` exit 0 across the 3 new dashboards.
- A+2. Property test `TestDispatchSpan_CrossProductWithinBudget` sweeps 200 synthetic dispatches; zero cardinality-cap breach.
- A+3. Property test `TestFailureTaxonomy_ClassifyLatencyP95Under5ms` passes against the 30-d corpus.
- A+4. PR-lifecycle histogram populates on a real PR end-to-end (manual verification — `prom http GET /api/v1/query?query=regatta_pr_stage_duration_seconds_count` returns ≥ 1 series per stage).
- A+5. Mutation-verify: introduce a deliberate `pr_number` label on the dispatch counter in a local branch; `TestMetricCardinality_PRNumberLabelBanned` fails. Restore; lint passes. Recipe documented in T1 PR body.

---

## §12 Adversarial review section

A second-tier reviewer subagent re-runs this spec against the parent roadmap's §8 rubric + the four item-spec acceptance criteria. Three deliberate weaknesses for the reviewer to find or accept:

1. **Histogram bucket sizes are not pinned.** SLO-5 (subagent latency < 2 min) uses `le="120"` and SLO-6 (stage < 1hr) uses `le="3600"`. If the default histogram view does not emit those exact bucket boundaries, the Sloth rule will return NaN. **Mitigation**: the meter call MUST register an explicit `metric.WithExplicitBucketBoundaries(...)` view in OBS-A-T0a's setup; this spec inherits that view registration. If A-T0a did not land custom views, file `[OBS-followup] Histogram view boundaries for OBS-C SLOs` and ship SLO-5/SLO-6 as `[planned]` in the parent §5 table.
2. **Failure-taxonomy corpus is not version-controlled with a refresh cadence.** The 30-d snapshot ages out; R11 catches the drift via the `other`-rate alarm but operator habit needs the refresh to be a calendar action. **Mitigation accepted as deferred**: the alarm IS the refresh trigger; a calendar action without a signal is ceremony. Reviewer may push back.
3. **Per-PR cost roll-up ships as a Prom recording rule "preferred" with sqlite-view fallback.** If Prom cardinality bites at provision time, the fallback is unspecified. **Mitigation**: the follow-up issue body specifies the sqlite-view shape (`event_token_spend` rows aggregated by `pr_number`, materialized on PR-merge via a substrate fold). If reviewer demands the fallback shape inline in this spec, fold it into §4.3.

If the reviewer flags additional findings, fix inline OR cite as deferred with reopen-trigger per `feedback_adversarial_review`.

---

## §13 Follow-ups (inline; filed at merge)

1. `[OBS-followup] Cost-per-agent rollup` — Prom recording rule joining `event_token_spend` × dispatch trace tree on `trace_id`; multi-dimensional output (`pr_number` + `agent_id` + `task_type`) projected through Grafana template variable `$rollup_dim` into 3 views. Owner: C-T3 by default; reassign at follow-up triage if scope grows. Carried forward verbatim from parent §9 #3.
2. `[OBS-followup] Cost-vs-failure scatter panel` — Wave-D operator surface panel that joins `regatta_pr_cost_usd_micro_log_event_total` × `regatta_dispatch_failure_total` via `pr_number` (log-warehouse query, NOT Prom). Filed because the panel needs the OBS-A-T6 log-warehouse → Grafana datasource link.
3. `[OBS-followup] Failure-taxonomy verify-only LLM second-opinion` — filed only if `regatta_dispatch_failure_total{mode="other"}` exceeds 10% of total over 7 days. The LLM ships as an offline labelling tool that proposes new regex rules; operator reviews and merges as code.
4. `[OBS-followup] Failure-taxonomy corpus refresh` — alarm trigger (R11) above 10% `other`-rate fires a roll-the-corpus task; the new snapshot lives at `testdata/failure_taxonomy/ci_failures_<YYYY-MM-DD>.txt` with the old snapshot retained for diff.
5. `[OBS-followup] Histogram view bucket boundaries for OBS-C SLOs` — filed only if OBS-A-T0a did not land `metric.WithExplicitBucketBoundaries(...)` for the histogram views referenced by SLO-5 and SLO-6.

---

## §14 Comment sweep

Status: clean. This spec is prose; no source code introduced in this PR. Implementer PRs (C-T1, C-T2, C-T3, C-T4) will run `golangci-lint` + comment-sweep per `feedback_comments_discipline` before push.

---

## §15 Self-host filter

Every claim in this spec passes the filter "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?":

- T1 dispatch counter: yes — operator needs to see which subagent template is hot.
- T2 PR-lifecycle: yes — operator needs to see which stage is slow.
- T3 per-PR cost: yes — operator needs to see which PR ran up the bill.
- T4 failure taxonomy: yes — operator needs to see which failure mode is recurring.

Multi-tenant `tenant_id` swap is a 1-line lift at the Phase-X gate (parent §13). No additional surface deferred.

---

## §16 Items addressed

All 4 items from the wave dispatch:

- `obs-wave-c-c-t1-dispatch-subagents.md` — §2 (dispatch span + counter + histogram).
- `obs-wave-c-c-t2-pr-lifecycle.md` — §3 (stage histogram + transitions counter + substrate event).
- `obs-wave-c-c-t3-per-pr-cost-attribution.md` — §4 (log event + unlabeled aggregate counter).
- `obs-wave-c-c-t4-failure-taxonomy.md` — §5 (failure-mode counter + regex classifier + corpus).

---

```release-notes
none (internal — design spec only)
```
