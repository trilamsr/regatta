# Runbook — SLO-5 dispatch-subagent latency

Alarm: `DispatchSubagentLatencyHigh`. Source SLO: `slo/dispatch-subagent.yaml`
(p95 of `regatta_dispatch_subagent_duration_seconds_bucket` <= 120 s over
a rolling 7-day window, 5 % error budget). Compiled rules will live at
`dashboards/prometheus/rules/dispatch-subagent.yaml` once `make slo-compile`
runs against the new SLO YAML.

## What fires

The alert fires when burn-rate crosses one of the multi-window thresholds
Sloth renders from the SLO definition (page tier wakes on-call; ticket
tier surfaces in the daily digest).

## First 60 seconds

1. Open `docs/operator/dashboards/dispatch.json` panel "Subagent latency
   p95 (s) by kind". Identify which `kind` (implementer / reviewer /
   designer / triage) carries the regression.
2. Open Jaeger / OTLP backend and drill from the counter exemplar
   `trace_id` to a slow span. The span attributes carry `task_id`,
   `model`, `input_tokens`, `output_tokens` — read them for shape.
3. Cross-reference the failure-taxonomy panel
   (`regatta_pr_failure_total{taxonomy=...}`) — a spike in `timeout` or
   `cost_cap` is the proximate cause; `reviewer_block` indicates a
   reviewer-loop regression rather than a dispatch-side problem.

## Per-kind diagnosis

### `implementer` dominant

Implementer subagents do most of the per-PR work. A spike here usually
indicates either (a) a model regression, (b) an upstream cost-cap
throttle, or (c) a test/lint loop that retries until the dispatch budget
exhausts. Check `regatta_dispatch_subagent_total{kind="implementer",outcome="timeout"}`
first.

### `reviewer` dominant

Reviewer subagents are typically much faster than implementers. A spike
here often signals (a) the reviewer is being given larger diffs than the
A-tier rubric specifies, or (b) the reviewer template recently changed.
Check the most recent `template` enum change in spawner deploys.

### `designer` dominant

Designer subagents read more context. A spike usually indicates an
overlong spec or roadmap doc in the input window. Check the input-token
attribute on the span.

### `triage` dominant

Triage should be the fastest path. Spike here is anomalous — escalate.

## If burn rate keeps climbing

1. Cap the dispatch concurrency at the orchestrator's wave-size knob.
2. If a single `model` value dominates the slow tail, fall back to the
   previous model revision.
3. File a `[follow-up]` issue with the trace_id + slowest span dump.

## Related SLOs

- SLO-6 (`pr-lifecycle-stage-p95`) — downstream signal; a dispatch
  spike often shows up here too.
- Cost SLOs — `regatta.cost.usd` aggregate panel.
