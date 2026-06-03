# Runbook — SLO-6 PR lifecycle stage p95

Alarm: `PRLifecycleStageLatencyHigh`. Source SLO: `slo/pr-lifecycle.yaml`
(p95 of `regatta_pr_stage_duration_seconds_bucket` <= 3600 s over a
rolling 30-day window, 5 % error budget). Compiled rules will live at
`dashboards/prometheus/rules/pr-lifecycle.yaml` once `make slo-compile`
runs against the new SLO YAML.

## What fires

The alert fires when burn-rate crosses one of the multi-window thresholds
Sloth renders from the SLO definition. PR lifecycle stages are bounded
by the closed enum:

`draft -> gates_running -> gates_pass -> awaiting_review -> awaiting_merge -> merged`

Any transition exceeding 1 hr at the p95 level burns budget.

## First 60 seconds

1. Open `docs/operator/dashboards/pr-lifecycle.json` heatmap. Identify
   which stage(s) the regression sits in via the stage template variable.
2. Cross-reference the substrate event log — query
   `SELECT * FROM substrate_events WHERE kind='pr_stage_transition'
   ORDER BY written_at DESC LIMIT 50` to see the recent transitions
   with their `pr_number` + `duration_seconds` payload values.
3. The slow stage tells the story:
   - `draft -> gates_running` slow: dispatch backlog or scheduler
     starvation.
   - `gates_running -> gates_pass` slow: CI runners or gate-decider
     latency.
   - `awaiting_review` slow: reviewer subagent queue depth.
   - `awaiting_merge` slow: branch-protection gate stuck on a missing
     check or a merge-queue stall.

## Per-stage diagnosis

### `awaiting_review` dominant

This is the most common cause of SLO-6 burn. Check
`regatta_dispatch_subagent_total{kind="reviewer",outcome="timeout"}` —
if reviewer dispatches are timing out, the reviewer queue is
backpressured by the implementer queue ahead of it.

### `gates_running` dominant

GitHub Actions runner shortage or a flaky required check. Check the
Actions queue depth directly; if the CI provider is hot, the only
operator action is to lower the dispatch concurrency until the queue
drains.

### `awaiting_merge` dominant

Branch-protection mismatch — a required check name in the protection
rules drifted from the workflow file. Open `gh api
repos/{owner}/{repo}/branches/main/protection` and reconcile against
the workflow names.

## If burn rate keeps climbing

1. Reduce dispatch concurrency at the wave-size knob.
2. Manually merge a few oldest PRs that are blocked on review-loop
   churn.
3. File a `[follow-up]` issue with the stage breakdown and the top-3
   `pr_number`s contributing to the burn.

## Related SLOs

- SLO-5 (`dispatch-subagent-latency`) — upstream signal; a dispatch
  regression often cascades into PR lifecycle.
