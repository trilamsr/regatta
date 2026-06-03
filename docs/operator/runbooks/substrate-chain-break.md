# Runbook — Substrate HMAC chain-break

Source spec: `docs/engineer/specs/2026-06-02-obs-wave-b-substrate-health.md` §4.
Alarm: `slo/substrate-chain-break.yaml` (critical-tier, zero-tolerance).
Dashboard: `docs/operator/dashboards/substrate-event-rate.json` (chain-break panel).

## Symptom
`SubstrateChainBreakDetected` critical-tier alarm fires.
`regatta_substrate_chain_break_total` incremented at least once.

## Severity
Critical. Any non-zero value pages. Chain-break is one of three signals:
1. **Tamper** — hostile or accidental row mutation.
2. **Corruption** — disk-level bit flip.
3. **Keyring rotation gap** — a key rotated mid-flight without coverage
   for older rows. (NB: the missing-key path explicitly does NOT bump
   this counter — spec §10 R9. If you see this alarm during a documented
   rotation, file a bug because the differentiation broke.)

## Locate the row
1. Check the WARN log entry: `journalctl -u regatta | grep 'chain.break_detected'`.
2. The log carries `event_id`, `event_kind`, `written_at_ms`, `sig_key_id`.
3. Pull the offending row: `sqlite3 state.db "SELECT * FROM substrate_events WHERE id='<event_id>'"`.

## Decide between forensic-preserve vs rollback
- **Forensic-preserve** (default for first occurrence): take a sqlite
  dump (`sqlite3 state.db ".backup forensic-$(date +%s).db"`) BEFORE
  any recovery action. The dump is the audit-trail evidence.
- **Rollback to last-known-good**: only after forensic dump. Substrate
  is append-only — there is no "rollback" in the row sense. The
  recovery is operational: stop the loop, decide whether the diverged
  row should be marked quarantined in a follow-up audit row.

## Verify the keyring
1. List the keys: `regatta keyring list`.
2. Check the `sig_key_id` from the WARN log is present in the keyring.
3. If missing: a key rotation dropped the key. Re-add from backup
   (NB: this is the missing-key path which should NOT have alarmed —
   re-verify the alarm rule).
4. If present: the key is the right one but the MAC mismatched — this
   is the tamper/corruption path.

## Recovery decision tree
- Single isolated break + forensic-preserve done → mark row quarantined,
  continue operations, file incident for root cause.
- Multiple breaks (> 3 in 24h) → halt the loop until root cause known.
  This is potentially active tampering.
- Break correlates with a recent kernel/disk/sqlite upgrade → file
  upstream bug, capture sqlite + driver versions.

## Follow-up
- File `[OBS-followup] B-T2 full-chain weekly sweep` if the broken row
  is older than 24h (sweeper window). Spec §10 R4.
- File `[INCIDENT]` issue with the WARN log + forensic dump path.
