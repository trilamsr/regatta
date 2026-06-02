# Runbook — SLO-2 L4 gate latency

Alarm: `L4GateLatencyHigh`. Source SLO: `slo/l4-latency.yaml` (p95 of
`regatta_l4_latency_ms_bucket` ≤ 30 000 ms over a rolling 7-day
window, 1 % error budget). Compiled rules live at
`dashboards/prometheus/rules/l4-latency.yaml`.

## What fires

The alert fires when the budget burn-rate crosses one of four windows
(spec §5 SLO-2, Sloth-rendered from `tools/sloth/windows/7d.yaml`):

| Tier | Short window | Long window | Burn-rate threshold |
|---|---|---|---|
| page (critical) | 5 m | 1 h | 13.44× |
| page (critical) | 30 m | 6 h | 3.5× |
| ticket (warning) | 2 h | 1 d | 1.4× |
| ticket (warning) | 6 h | 3 d | 0.98× |

The 1 % budget is intentionally tight — `[OBS-followup] #1` is filed at
this PR's merge to widen after 30 days of real burn-rate data per spec
§5 SLO-2.

## First 60 seconds

1. Open `docs/operator/dashboards/slo.json` → "L4 gate — p95 vs 30 s".
   The red trace shows the burn window; the dashed line is the 30 s
   objective.
2. Open `docs/operator/dashboards/l4-gate.json` → bisect by `category`
   (the per-category stack panel). If one category dominates, that is
   the regression surface. If the stack is uniform, the cause is the
   shared model API or the cache.
3. Skip to the matching section below.

## Common causes

### Cache cold

The L4 cache populates per (input-hash, category) tuple. After a
deployment restart, the cache is empty; every L4 call hits the model
API end-to-end. Symptom: cache-hit ratio panel reads near 0 for the
first 10-30 minutes of a deployment.

Query: `rate(regatta_l4_cache_hits_total[5m]) / (rate(regatta_l4_cache_hits_total[5m]) + rate(regatta_l4_cache_misses_total[5m]))`.

Action: wait. Ratio should climb above 0.5 within 30 minutes of normal
traffic. If it stays below 0.5 after an hour, the cache backing store
is misconfigured — check `REGATTA_L4_CACHE_DIR` and disk space.

### Second-opinion fan-out

L4 fires a second-opinion call when the first verdict is `needs_review`.
A spike in `needs_review` doubles the per-invocation latency cost. The
second-opinion-fire-rate panel on `l4-gate.json` shows the rate
directly.

Query: `rate(regatta_l4_second_opinion_fired_total[5m])`.

Action: if the rate exceeds 10 % of total invocations for > 15 min,
the L4 prompt has drifted into uncertainty. Roll back the most recent
prompt change OR raise the confidence threshold via
`REGATTA_L4_CONFIDENCE_THRESHOLD`. The L4 maintainer owns the prompt;
escalate to tier 2 before changing thresholds in production.

### Model-API slowdown

The Anthropic API tail latency expands 5-10× for 30-60 minute periods.
The L4 latency histogram tracks the end-to-end call; an upstream tail
shows here first.

Query: `histogram_quantile(0.95, sum by (le) (rate(regatta_l4_latency_ms_bucket[5m])))` versus the Anthropic status page.

Action: confirm against status.anthropic.com. If the status page shows
elevated latency, acknowledge the alarm with a `INC-anthropic-{date}`
ticket reference (see "How to acknowledge"). The 1 % budget will burn
through in ~50 minutes during a real tail event; this is the trigger
condition for the budget-widen followup.

### Per-category regression

A single `category` label (≤ 12 enums per spec §3) shows elevated p95
while others stay flat. This is a category-specific prompt or model
regression.

Query: `histogram_quantile(0.95, sum by (category, le) (rate(regatta_l4_latency_ms_bucket[5m])))`.

Action: identify the category, check the L4 commit log for changes to
that category's prompt template, roll back if the change was recent.

## How to bisect by category

```bash
# Top-3 slowest categories over the last 30 minutes.
prom_query 'topk(3, histogram_quantile(0.95, sum by (category, le) (rate(regatta_l4_latency_ms_bucket[30m]))))'
```

If a category's p95 exceeds the SLO objective (30 s) on its own, the
SLO budget is being consumed by that category alone — fix or quarantine
that category, do not widen the SLO.

## How to acknowledge

The alert auto-resolves two evaluation cycles after the burn-rate
clears. To silence during a known upstream incident:

```bash
# Silence 1h, link to the Anthropic incident ID.
amtool silence add -a tree -c "anthropic-INC-5678" alertname=L4GateLatencyHigh -d 1h
```

Silences expire on their own. Always include a ticket reference — the
digest section "active silences" lists every silence + reason.

## Escalation

| Tier | Owner | Trigger |
|---|---|---|
| 1 | on-call (you) | first 30 min of page tier |
| 2 | L4 maintainer | page tier sustained > 30 min OR per-category regression |
| 3 | regatta maintainer | ticket tier > 24 h OR concurrent SLO-1 + SLO-2 fire |

## Post-incident

File a one-line entry on the weekly digest under "alarms that fired"
with: timestamp, tier, root cause, remediation. If the cause was a
new failure mode not covered above, add the section in the same PR as
the fix.

Per `[OBS-followup] #1`, every false-page (no operator action taken,
auto-resolved within 10 min) counts toward the 30-day budget-widen
trigger. Tag the digest entry with `false-page-candidate` when applicable.
