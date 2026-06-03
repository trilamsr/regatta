---
title: "Fix #195 — Persist per-JTI `token_minted` rows so reaper revocation is reachable"
status: shipped
summary: "Persist per-JTI `token_minted` rows so reaper revocation reaches the event log. Landed via #332."
---

# Fix #195 — Persist per-JTI `token_minted` rows so reaper revocation is reachable

Date: 2026-06-02
Issue: [#195](https://github.com/trilamsr/regatta/issues/195)
Status: shipped (#332)

## Problem

`gate.createApprovalAndNotify` and `gate.notifyEscalatedTier` mint one token per
reviewer via `canon.MintToken` (returning `(wire, jti)` pairs) but persist only
the `jti_count` integer in the `notified` event payload. The per-JTI
`token_jti` column on `approval_events` is never populated by the gate. The
reaper's `outstandingJTIs(events)` filter (`e.TokenJTI != ""`) therefore
returns `[]`, and the revocation branch is dead code in production. Spec
§3.3.1.3 ("revoke outstanding tokens via single-use UNIQUE on
(approval_id, kind, token_jti)") is not met.

## Decision: A — persist one `token_minted` event per JTI

### Adversarial review summary

Two candidate fixes were considered:

- **A) Persist per-JTI `token_minted` rows.** One audit row per JTI; reaper
  revocation becomes reachable.
- **B) Delete the reaper revocation branch.** Admit the path is dead; remove
  `outstandingJTIs` + `revoked_jtis` payload + the per-JTI `token_consumed`
  loop.

Choice **A** wins on `feedback_decision_priority` (long-term > short-term;
UX/ease/best-practices over speed) and `feedback_research_design_principles`
(proven OSS pattern > build-from-scratch).

| Lens | A (persist) | B (delete) |
|---|---|---|
| Security primitive | revocation works | replay window = full decision_window |
| Spec §3.3.1.3 contract | met | broken; spec edit required |
| Existing reaper tests | already assume `token_minted` rows | must delete passing tests |
| Audit trail completeness | every JTI traceable | mint→consume traceable, but revoke gap unaudited |
| Code surface | +1 event-kind constant + 1 helper | −~30 LoC in reaper |
| Long-term reversibility | trivial to delete later | re-adding requires migration |

Rejection of B is decisive: it weakens a documented security primitive for a
−30 LoC payoff. The reaper tests `TestReaper_EscalatePolicy_*` already seed
`token_minted` rows and assert `revoked_jtis` count + per-JTI `token_consumed`
rows — the wire format and the revocation semantics are settled; only the
producer side (the gate) is missing.

### Solution

1. Add `EventKindTokenMinted = "token_minted"` to `audit.go` constants
   (already referenced as a string literal by `reaper_test.go` and
   `outstandingJTIs` doc comment).
2. In `createApprovalAndNotify`, after `mintReviewerTokens` returns the
   `jtis` slice, append one `token_minted` row per JTI inside the same
   recording flow. Each row carries `TokenJTI = jti`, `Actor = "orchestrator"`,
   `Kind = "token_minted"`, empty payload `{}`.
3. Same in `notifyEscalatedTier` for the post-escalation tier's tokens.
4. Append the `token_minted` rows BEFORE the `notified` row so a fold sees the
   mint markers in the same temporal slot as the `requested` → `notified`
   pair. Atomicity is best-effort here: spec §3.2.1 already tolerates an
   `AppendApprovalEvent`-loop sequence (each call is its own statement); the
   reaper's idempotency contract (`isTerminal` + single-use UNIQUE) absorbs a
   partial mint trail.

### Out of scope

- Single-tx wrap of the `requested + N×token_minted + notified` sequence. The
  spec contract already allows per-event commits; bundling would require
  exposing a tx-aware `recordEvent` variant and is orthogonal to #195.
- New `payload.jtis` array in the existing `notified` event. The per-row column
  is the canonical home; the `jti_count` integer in payload is kept for
  backwards-compat reads.

## Test plan

### TDD strict (failing test FIRST)

`TestGate_PersistsTokenMintedPerJTI` (gate_test.go): asserts that after
`Evaluate` creates an approval, the event log contains N `token_minted` rows
where N = len(reviewers), each carrying a distinct non-empty `TokenJTI`.
**Pre-fix**: this test FAILS — zero `token_minted` rows exist. Failing
output captured in the PR body.

### Revocation E2E (A-grade)

`TestGate_ReaperRevokesMintedTokensOnEscalate` (gate_test.go): drives the full
loop — gate mints, reaper escalates, asserts `outstandingJTIs` was non-empty
inside the sweep (proxied by N `token_consumed` rows with
`reason=escalated` appearing in the log).

### Property test (A+ grade)

`TestReaperMintInvariant_Property` (reaper_property_test.go or extended
`fold_property_test.go`): for any pre-escalate gate run with K reviewers, the
post-escalate event log contains exactly K `token_consumed`-with-
`reason=escalated` rows whose JTIs are a permutation of the K JTIs minted by
the gate. Uses `testing/quick`.

## Rollout / migration

No schema changes. No data migration: pre-existing approvals predating the fix
will continue to have no `token_minted` rows; reaper revocation on those rows
remains a no-op (graceful degradation — they age out via `decision_window`).

## References

- Spec §3.3.1.3 (token revocation contract)
- Spec §5.7 (audit-trail recordEvent helper)
- `feedback_root_cause` — fix root cause (missing producer), no symptom
  suppression (do not weaken the reaper)
- `feedback_decision_priority` — long-term security primitive > short-term LoC
- `feedback_tdd_discipline` — failing test FIRST

## Resolution (2026-06-02)

Shipped via #332 (`fix(approval-gates): persist per-JTI token_minted rows so reaper revocation reaches the event log`). Closes #195; reaper revocation branch is now reachable.
