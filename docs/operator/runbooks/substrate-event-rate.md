# Runbook — Substrate event-rate

Source spec: `docs/engineer/specs/2026-06-02-obs-wave-b-substrate-health.md` §3.
SLO: `slo/substrate-event-rate.yaml` (SLO-3, warn-tier).
Dashboard: `docs/operator/dashboards/substrate-event-rate.json`.

## Symptom
`SubstrateEventRateAnomaly` warn-tier alarm fires. The substrate `Append`
event-rate either stalled to zero or stormed past 2× the 24h trailing P95.

## Quiescence guard already applied
The alarm `AND`s with `regatta_cost_cap_state != 1`. If the operator
paused the loop via W5 cost-cap, the alarm is suppressed automatically.
If it fires anyway, the suppression is bypassed — investigate.

## Stall triage (rate dropped to zero)
1. Check writer goroutine liveness:
   `journalctl -u regatta | grep -i 'substrate.append'`.
2. Check sqlite WAL contention: `sqlite3 state.db 'PRAGMA wal_checkpoint(PASSIVE);'`.
3. Check disk: `df -h $(dirname state.db)` — full disks silently block writes.
4. Check the scheduler tick: if SLO-1 is also firing, the upstream
   trigger stopped — not the substrate.
5. Recovery: if the writer goroutine is wedged, `systemctl restart regatta`
   is safe (substrate is crash-recoverable; no row is half-written).

## Storm triage (rate > 2× baseline)
1. Identify the kind: open the dashboard "Events/sec by kind" panel.
2. If `kind=heartbeat` storms: a scheduler-tick loop or runaway agent.
   Check `regatta_scheduler_tick_count`.
3. If `kind=token_spend` storms: an LLM dispatcher in a retry loop.
   Check `regatta_llm_calls_total`.
4. If `kind=gate_verdict` storms: a CEL evaluator loop.
   Check `regatta_cel_decider_invocations_total`.
5. Recovery: pause the offending agent via `regatta agent stop <id>`
   then root-cause the loop.

## Escalation
If neither stall nor storm triage finds a cause within 30 minutes,
this is the first sighting of an unknown failure mode. File `[INCIDENT]`
issue with the dashboard screenshot + `journalctl --since "1 hour ago"`
attached.

## Related signals
- SLO-1 (scheduler tick): if both fire, upstream cause is the scheduler.
- Cost-cap state: if `regatta_cost_cap_state == 1`, the suppression is
  active; alarm should not have fired. File a bug.
