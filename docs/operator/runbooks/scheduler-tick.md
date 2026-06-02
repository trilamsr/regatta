# Runbook — SLO-1 scheduler-tick latency

Alarm: `SchedulerTickLatencyHigh`. Source SLO: `slo/scheduler-tick.yaml`
(p95 of `regatta_scheduler_tick_latency_ms_bucket` ≤ 5 000 ms over a
rolling 7-day window, 5 % error budget). Compiled rules live at
`dashboards/prometheus/rules/scheduler-tick.yaml`.

## What fires

The alert fires when the budget burn-rate crosses one of four windows
(spec §5 SLO-1, Sloth-rendered from `tools/sloth/windows/7d.yaml`):

| Tier | Short window | Long window | Burn-rate threshold |
|---|---|---|---|
| page (critical) | 5 m | 1 h | 13.44× |
| page (critical) | 30 m | 6 h | 3.5× |
| ticket (warning) | 2 h | 1 d | 1.4× |
| ticket (warning) | 6 h | 3 d | 0.98× |

Page tier wakes the on-call. Ticket tier surfaces in the daily digest.

## First 60 seconds

1. Open the SLO dashboard — `docs/operator/dashboards/slo.json` →
   "Scheduler tick — p95 vs 5 s objective". The red trace shows the
   burn window; the dashed line is the 5 s objective.
2. Check the per-step breakdown — `docs/operator/dashboards/scheduler-tick.json`
   panel "Tick step breakdown (stacked)". The 8 named steps are
   `dispatch`, `gate_l0`, `gate_l4`, `gate_approval`, `gate_cost`,
   `reaper`, `fold`, `persist`.
3. If one step dominates the stack: that is the regression. Skip to the
   per-step section below. If the stack is flat-but-elevated: skip to
   "fan-out + queue depth".

## Per-step diagnosis

### `gate_l4` dominant

The L4 gate p95 latency SLO (SLO-2) is the next layer down — check
`SchedulerTickLatencyHigh` against `L4GateLatencyHigh`. If both fire,
work the L4 runbook first: `docs/operator/runbooks/l4-latency.md`.
A model-provider tail latency surfaces here before the L4 SLO trips
because tick latency = sum of step latencies.

Query: `histogram_quantile(0.95, sum by (le) (rate(regatta_scheduler_tick_step_duration_ms_bucket{step="gate_l4"}[5m])))`.

### `persist` dominant

The substrate event-append path is contending. Check disk IOPS on the
substrate volume + the SQLite write-ahead log size. Restart the
substrate process only after capturing the WAL size; a clean restart
fsyncs and zeroes the WAL, hiding the symptom.

Query: `histogram_quantile(0.95, sum by (le) (rate(regatta_scheduler_tick_step_duration_ms_bucket{step="persist"}[5m])))`.

### `gate_cost` dominant

A cost reconciler call is timing out against the Anthropic Cost/Usage
API. Check `regatta_cost_reconciler_request_duration_seconds` and the
Anthropic status page. Cost gate failures fall open per spec §11 — the
tick keeps moving, but the latency drag still hits SLO-1.

### `dispatch` dominant

Spawner queue depth is high. Check `regatta_spawner_queue_depth` and
the `spawner` process worker count. The default worker pool is 4; raise
to 8 via `REGATTA_SPAWNER_WORKERS=8` and observe for 10 minutes before
escalating.

### Flat-but-elevated

Every step shows a small bump. Two causes:
- Substrate read amplification (cache cold after restart). Check
  `regatta_substrate_read_cache_hits_total` ratio against
  `regatta_substrate_read_cache_misses_total`; ratio < 0.5 over 5 min
  means the cache is warming. Wait 10 minutes for warm.
- Host CPU saturation. Check `node_cpu_seconds_total{mode="idle"}` —
  below 20 % across the tick window points at noisy-neighbour or a
  runaway agent. Identify the agent via `regatta_agent_cpu_seconds_total`
  top-1 and decide whether to cap or terminate.

## Fan-out + queue depth

If no single step dominates but tick count drops while latency rises,
the scheduler is serializing on a single hot work-item. Check
`regatta_orchestrator_work_items_active` by `lane` — a single lane at
high count with the rest at zero means the dispatcher picked a
work-item the gates keep rejecting. Inspect the work-item via
`regatta status` and either escalate the gate verdict or move the
item to `quarantined`.

## How to acknowledge

The alert auto-resolves when the burn-rate drops below threshold for
two consecutive evaluation cycles (Sloth default 1 minute → 2 minute
clearance). To silence during a known planned incident:

```bash
# Silence for 1 hour, label with operator + ticket ID.
amtool silence add -a tree -c "INC-1234" alertname=SchedulerTickLatencyHigh -d 1h
```

Silences expire on their own. Do not silence without a ticket reference
— the digest section "active silences" gets noisy fast.

## Escalation

| Tier | Owner | Trigger |
|---|---|---|
| 1 | on-call (you) | first 30 min of page tier |
| 2 | platform lead | page tier sustained > 30 min |
| 3 | regatta maintainer | ticket tier > 24 h OR two page tiers in 7 d |

Tier-3 also gets paged if both SLO-1 and SLO-2 are firing simultaneously
— that points at a shared substrate or model-provider regression, not
a per-component bug.

## Post-incident

File a one-line entry on the weekly digest under "alarms that fired"
with: timestamp, tier, root cause, remediation. If the root cause was
a missing per-step branch in this runbook, edit this file in the same
PR as the fix; do not let runbook coverage drift behind reality.

If false-page rate exceeds 1 per week, file `[OBS-followup]` against
the SLO-1 entry in `slo/scheduler-tick.yaml` to widen the budget or
extend the window.
