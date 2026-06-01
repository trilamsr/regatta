# 0003. MVP-2 approval gates: HMAC-signed token + reaper + fold over an event log

Status: accepted
Date: 2026-05-31
Author: Tri Lam <tri@maydow.com>

## Context

Regulated and high-blast-radius work cannot run unattended end-to-end.
There are decision points where a human must approve before the
orchestrator proceeds (production migration, secret rotation, large
refund batch, irreversible delete). Pre-MVP-2 the platform had no
first-class way to pause execution at a human-decision point, notify a
reviewer set, resume on a signed-token callback, and produce an
append-only audit trail. Approval gates are the bridge from
"interesting demo" to "production-credible" and the unlock for the
compliance / regulated buyer segment.

The decision triggers from spec D5 fire: a new `regatta.yaml:gates[]`
type, a new `work_items.status` value (`awaiting_approval`), a new CLI
surface (`regatta approval decide` + `regatta approval list`), a new
keyring consumer (HMAC token mint/verify), and post-incident rule
codification of trap P2 (two-key approval on irreversible actions) and
P3 (trusted instructions only) -- both load-bearing here.

## Decision

Add a first-class `approval_gate` to MVP-2 with these locked decisions:

1. **Gate is a status modulator, not a new lifecycle.** Adds
   `planned -> awaiting_approval -> planned | rejected` transitions on
   `work_items`. Once approved, the item rejoins the universal queue
   unchanged; the scheduler hot path for non-gated work is untouched.
2. **Event-sourced approval state.** Canonical truth is the
   `approval_events` log (kinds: `requested`, `notified`, `decided`,
   `approved`, `rejected`, `timed_out`, `escalated`, `token_consumed`,
   `token_revoked`). The denormalised `approvals.status` is materialised
   by `fold(events)` and asserted byte-equal by property test.
3. **HMAC-signed single-use tokens.** Wire format
   `base64url(sig) "." base64url(payload)`. Payload is canonicalised
   JSON of `{kid, wi, aid, window, reviewer, jti}`. Signing reuses
   `contracts/schemas/sign.go:macSum` -- no new HMAC alg, no new key
   material, single canonicaliser. JTI = `base64url(crypto/rand.Read(16))`
   (mandatory; `math/rand` use is lint-gated). Verification order is
   constant-time HMAC FIRST, JSON unmarshal SECOND -- prevents parser-
   oracle attacks where a crafted payload triggers behaviour pre-auth.
4. **Reviewer set snapshot at request time, not decide time.** Mutating
   `regatta.yaml` after an approval is created does not change who can
   approve it. N-of-M quorum + `prevent_self_review` (identity-equality
   match against `approvals.requested_by`) are the sufficient RBAC
   primitives; team-scope is deferred.
5. **Escalation tier ladder.** `on_timeout=escalate` advances to the next
   tier, replays prior votes against the new quorum, revokes tier-N
   tokens (collision sink on `UNIQUE(approval_id, kind, token_jti)
   WHERE kind='token_consumed'`), and mints tier-N+1 tokens. If
   replayed votes satisfy the new quorum the escalation event is
   followed in-tx by the appropriate terminal event -- no second tick
   required.
6. **Reaper auto-approve on timeout.** `on_timeout ∈ {fail,
   auto_approve, escalate}`. `auto_approve` is config-gated to
   `risk_class=low` (config-load validator rejects `auto_approve` on
   any `medium|high` gate). High-blast classes (`delete_*`, `rotate_*`,
   `deploy_*`, large `refund_*` / `payout_*` / `withdraw_*`) MUST stay
   `fail` or `escalate`.
7. **Decide-path atomicity.** All five mutations (token_consumed event,
   decided event, denormalised approvals.status update on terminal,
   terminal kind event, work_items.status transition) execute in one
   `BEGIN IMMEDIATE ... COMMIT`. Failure rolls back the entire block;
   the token stays unconsumed; the operator may retry.
8. **Fold ordering is by event `id`, not `ts`.** The autoincrement
   monotonic `id` is the canonical fold sequence; wall-clock `ts` is
   advisory only and may drift. `TestFold_OrderByIdNotTs` pins this.
9. **One open approval per `(work_item_id, gate_name)`.** Belt:
   `UNIQUE(work_item_id, gate_name)` on `approvals`. Suspenders: the
   scheduler runs at most one `Tick` at a time, enforced by a
   process-level `flock` on the sqlite file and pool size 1.
10. **`fold ≡ status` property.** The denormalised
    `approvals.status` column matches `fold(approval_events).status`
    across every observable state. Property test
    `TestApprovalStatus_FoldEquivalence` runs 200 random event
    sequences, 5× repeats, asserts zero divergence.
11. **Notification adapter is interface-only in MVP.** Default impl is
    a `slog.Info("approval.notify_stub", ...)` no-op so the audit
    trail captures intent even with no real channel wired. Channel
    impls (Slack, PagerDuty, email) are separate per-channel PRs; they
    implement `Notifier` and register at startup. Construction is
    fail-closed: unregistered notifier kind in `regatta.yaml` errors
    out at startup, never silently falls through to stub.
12. **CLI surface (single binary).** `regatta approval decide --token
    <signed> --decision allow|deny [--reason <text>]` and
    `regatta approval list [--mine] [--format=table|json]`.

Alternatives considered + rejected:

- **Approve-via-PR-comment only.** Cheap but couples
  approval-lifecycle to GitHub's comment ordering + reaction semantics.
  Rejected -- token is the seam.
- **Decide-then-verify token shape (verify HMAC after JSON parse).**
  Rejected because it exposes the JSON parser to attacker-controlled
  bytes pre-auth. Constant-time HMAC FIRST is the only safe ordering.
- **Allow `approve_with_edits` decision variant.** Requires JSON-Patch
  validation against the typed outputs schema from RFC-0002. Deferred
  to MVP-3 alongside the same evaluator that ships with the
  conditional-DAG wedge.
- **Postgres-only backend for `approval_events`.** Out of scope; the
  in-DB sqlite log is sufficient for MVP audit. Off-host durability
  is tracked by issue #80.

## Consequences

- (+) Trap P2 + P3 enforcement seam is first-class: a single token
  carries the signed authorisation; the audit log is the unforgeable
  record of every decision; reviewer-distinct tokens kill the
  "Alice forwards her email to Bob" failure mode by construction.
- (+) Scheduler hot path unchanged for non-gated work. Gated work
  hits one extra sub-tick step that returns immediately when no
  pending approval exists.
- (+) Crash-recovery + reaper plumbing inherited from MVP-1.
  `approval_events` and `approvals` survive restart by construction
  (sqlite); the scheduler tick picks up where it left off.
- (-) New migration `0004_approvals.sql`, new package
  `internal/gates/approval/`, new CLI subcommands, new keyring
  consumer. Surface area is ~10 implementer subagents across 5 waves.
- (-) `auto_approve` policy is the single largest foot-gun. Mitigated
  by config-load validator (`V5`) but documented as the operator's
  responsibility in `docs/operator/approval-gates.md`.
- (-) Notification dispatch is interface-only in MVP. Operators
  without a real channel wired see the audit-trail capture but no
  external alert; the slog `approval.notify_stub` event is the
  documented bridge until a real Notifier ships.
- Activation triggers for follow-up RFCs: team-scope
  `prevent_self_review` (when role/membership backing store lands),
  off-host audit sink durability (issue #80), `approve_with_edits`
  variant (requires RFC-0002 typed outputs schema in MVP-3).

## Compliance

- **Schema:** migration
  `internal/orchestrator/state/migrations/0004_approvals.sql`
  defines `approvals` (with `UNIQUE(work_item_id, gate_name)` and
  `CHECK` constraint on actor charset
  `[a-zA-Z0-9_:.-]{1,128}`) and `approval_events`
  (with `UNIQUE(approval_id, kind, token_jti) WHERE
  kind='token_consumed'`). `TestMigrate_V3ToV4` in
  `internal/orchestrator/state/` asserts clean apply.
- **State ops:** `internal/orchestrator/state/approvals.go` +
  `approval_events.go` (+ `_test.go` per file) expose
  `CreateApproval`, `GetApprovalForWorkItem`, `AppendApprovalEvent`,
  `ListPendingApprovals`, `ListApprovalsTimedOutBefore`,
  `MarkApprovalDecided`. All writes go through
  `AppendApprovalEvent` first; status updates are derived from the
  fold and written in the same tx.
- **HMAC token:** `internal/canon/approval_token.go` +
  `_test.go` own mint + verify. Reuses
  `contracts/schemas/sign.go:macSum` verbatim.
  `TestJTI_UsesCryptoRand` pins `crypto/rand` (lint gate in
  `make ci-check` greps for `math/rand` in
  `internal/canon/approval_token.go` and `internal/gates/approval/`
  -- expected zero matches).
  `TestToken_RejectReplay -count=3` asserts UNIQUE-index collision
  on second consume. `TestToken_RejectWrongReviewer` asserts a
  forwarded URL fails verify when the CLI `--reviewer-id` flag does
  not match the signed `reviewer` field.
- **Gate handler:** `internal/gates/approval/gate.go` +
  `config.go` + `fold.go` (+ `_test.go` per file) implement the
  `Gate` interface, parallel to `internal/gates/l0/` and
  `internal/gates/security/`. `TestQuorum_TwoOfThree_Approves`,
  `TestQuorum_TwoOfThree_RejectsOnSplit`, `TestQuorum_PendingUntilQuorum`,
  `TestPreventSelfReview_IdentityOnly` cover the quorum + RBAC
  surface.
- **Reaper:** `internal/gates/approval/reaper.go` + `_test.go`
  sweep `ListApprovalsTimedOutBefore` per tick.
  `TestTimeout_FiresEscalationEvent`, `TestTimeout_AutoApprovePolicy`,
  `TestTimeout_EscalatePolicy`,
  `TestEscalation_ReplaysPriorVotes` cover all three policies +
  tier ladder. Clock is constructor-injected (`clock func() time.Time`);
  tests advance directly.
- **Decide-path atomicity:**
  `TestDecide_AtomicityOnMidTxConnError` injects
  `sql.ErrConnDone` at every statement index in the tx; for each
  injection asserts zero partial state (approvals row unchanged,
  zero events appended, work item status unchanged). Property-style
  sweep across the tx is the regression guard for spec §3.2.1.
- **Concurrent decide race:** `TestRace_QuorumExactlyMet` runs 3
  goroutines deciding simultaneously with quorum=3 under `-race`,
  `-count=10`; asserts exactly one row per reviewer, no
  double-count. `TestRace_PreventDoubleVote` asserts post-escalation
  re-mint kills prior tokens with `ErrTokenInvalid`.
- **CLI:** `cmd/regatta/approval_decide.go` +
  `cmd/regatta/approval_list.go` (+ `_test.go` per file) wire to
  `internal/gates/approval`. `TestApprovalList` asserts pending
  approvals listed; `TestApprovalDecide_HappyPath` and
  `TestApprovalDecide_AllSentinels` cover every error path
  (`ErrTokenInvalid`, `ErrUnverifiable`, `ErrTokenExpired`,
  `ErrTokenReplay`, `ErrUnknownKeyID`, `ErrNotReviewer`,
  `ErrSelfReview`).
- **Config invariants:**
  `internal/config/gates.go` + `_test.go` enforce the 11
  invariants from spec §5.5 (V1-V11) in one accumulating loader
  pass. `TestConfig_ApprovalGate_Invariants` is a single
  table-driven test with one row per invariant; the CUE schema
  (`contracts/schemas/regatta.v1.cue`) declares the structural
  shape and a `make ci-check` invariant test asserts Go + CUE
  reject the same fixtures.
- **Canary fixtures:** `testdata/program/PROG-APPROVAL.md` +
  `testdata/program/PROG-APPROVAL.brief.golden.json` exercise
  the happy-path + rejected-path through
  `internal/gates/approval/end_to_end_test.go`. `TestE2E_HappyPath`
  and `TestE2E_RejectedPath` cover full lifecycle audit emission
  and integration with the RFC-0002 conditional-DAG wedge
  (`on_skip=cascade` propagates).
- **Per-package coverage targets:** `internal/gates/approval`
  ≥ 85%, `internal/orchestrator/state` (new files only) ≥ 90%,
  enforced in `make ci-check`.

---

Numbering is monotonic; do not reuse a number. Once Status is
"accepted", do not edit; supersede with a new RFC that links back
via the "superseded by RFC-NNNN" status line.
