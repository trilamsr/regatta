# Runbook — Substrate divergence

Source spec: `docs/engineer/specs/2026-06-02-obs-wave-b-substrate-health.md` §5.
Alarm: `slo/substrate-divergence.yaml` (critical-tier, zero-tolerance).
Dashboard: `docs/operator/dashboards/substrate-event-rate.json` (divergence panel).

## Symptom
`SubstrateDivergenceDetected` critical-tier alarm fires.
`regatta_substrate_divergence_detected_total` incremented at least once.

## What divergence means
A row HMAC-verifies (so it has NOT been tampered) but when the recorded
verdict is replayed against the current engine, the replay produces a
DIFFERENT verdict. This is the silent-corruption signal — the loop
produced an answer the substrate cannot reproduce.

## Distinguish from chain-break (T2)
- T2 chain-break = row was mutated after write (HMAC fails).
- T3 divergence = row is intact (HMAC passes) but the replay path
  disagrees with the recorded verdict.

Both can fire together if the same row was tampered AND the replay
also drifts. The two signals are independent.

## Locate the audit row
1. The metric increments per row written to `substrate_divergence_audit`.
2. Pull the latest rows:
   ```
   sqlite3 state.db "SELECT id, detected_at, detector, store, primary_key,
                            diff_summary
                     FROM substrate_divergence_audit
                     WHERE repaired_at IS NULL
                     ORDER BY detected_at DESC LIMIT 10"
   ```
3. The `diff_summary` carries the JSON-path keys that disagreed.

## Triage by layer
- `detector='layer1_write'` — the writer wrote different bytes to
  legacy vs substrate. Suspect: a bug in the dual-write path
  (`internal/orchestrator/state/approvals_shadow.go`).
- `detector='layer1_read'` — substrate returned empty while legacy
  had data, or vice versa. Suspect: a migration race or a partial
  backfill.
- `detector='layer2_test'` — divergence found by the periodic
  consistency test. Suspect: pre-existing inconsistency that
  the spot-check just discovered.
- `detector='layer3_cron'` — divergence found by the nightly cron.
  Worst case — likely production data has been wrong for hours.

## Recovery decision tree
- Single isolated divergence → mark as repaired with operator
  decision documented in `repair_action`.
- Multiple divergences (> 3 in 24h) → halt the loop until root cause known.
- Divergence correlates with a recent code change → revert the change,
  then walk back through diffed rows.

## Follow-up
- File `[INCIDENT]` issue with the diff_summary + recent deployment list.
- If divergence rate is non-zero but trending toward zero, file
  `[OBS-followup] B-T3 follow-up — divergence backlog cleared`.
