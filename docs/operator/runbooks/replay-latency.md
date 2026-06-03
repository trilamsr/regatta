# Runbook — Replay latency

Source spec: `docs/engineer/specs/2026-06-02-obs-wave-b-substrate-health.md` §6.
SLO: `slo/replay-latency.yaml` (SLO-5, warn + critical).
Dashboard: `docs/operator/dashboards/substrate-event-rate.json`.

## Symptom
`ReplayLatencyHigh` fires at one of two tiers:
- **warn (P95 > 30s over 10m)**: early-warning. Scheduler tick SLO-1
  (5s P95) starves at five consecutive ticks if replay sits at 30s.
- **critical (P95 > 60s)**: replay-tail starves the resume path.
  Operator notices on manual restart.

## Triage
1. Identify the slow program_kind: dashboard "Replay P50/P95/P99 by
   program_kind" panel. If only one kind is slow, the regression is
   scoped; if all, the substrate-spine is the bottleneck.
2. Check sqlite size:
   `ls -lh state.db state.db-wal state.db-shm`.
3. Check sqlite stats:
   `sqlite3 state.db "PRAGMA stats;"`.

## Common causes + recovery

### sqlite WAL bloat
Symptom: `state.db-wal` is multi-GB.
Recovery: `sqlite3 state.db "PRAGMA wal_checkpoint(TRUNCATE);"`.
Prevention: file `[OBS-followup]` to wire periodic checkpoint into
the substrate writer goroutine.

### Page-cache cold
Symptom: first replay after restart is slow; subsequent fast.
Recovery: nothing to do — second-replay fast is OK. The SLO P95 is
warm-cache; if P95 is slow with hot cache, this is a real regression.

### Substrate compaction trigger
Symptom: replay slow proportional to event-log size; no other signal.
Recovery: run the substrate compactor if available, else file an
issue for the compactor design.

### Hybrid-fallback toggle
The W9 `DurableHistory` interface allows alternative implementations.
If the substrate impl is slow and a hybrid impl is available, swap
via config — see `internal/history/`.

## Threshold rationale
- 30s warn is half the critical 60s threshold. The scheduler tick SLO
  (5s P95) starves at five consecutive ticks if replay sits at 30s;
  the warn fires a tier before SLO-1 follows.
- 60s critical is the loop has resumed, but slowly enough that an
  operator notices.

## Wave-B exit re-tune
At Wave-B exit (week-1 of digests), the B-T4 author re-reads the live
week-1 P95 median:
- < 5s → drop warn to 10s (single-commit YAML edit).
- 5-25s → keep 30s warn.
- ≥ 25s → file `[OBS-followup] B-T4 re-tune warn threshold`.

## Follow-up
- File `[INCIDENT]` issue with the dashboard screenshot + sqlite stats.
- If P95 ≥ 25s on a recurring basis, file
  `[OBS-followup] B-T4 re-tune warn threshold`.
