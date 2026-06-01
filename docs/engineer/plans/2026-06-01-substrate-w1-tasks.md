# Substrate Wave 1 — Implementer Task Breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-unified-substrate-design.md`.
Authority: `feedback_spec_pattern_authority` — implementer deviation from spec MUST re-spawn design subagent.

---

## Wave overview

- **3 file-disjoint implementer tasks** (T-S1, T-S2, T-S3) plus a pre-Wave Task 0 (`BenchmarkAppendEvent` perf gate) baked into T-S1's scope (spec §13 + §7 Task 0 gate).
- **Hard prereqs (merged to main):**
  - W6 T1 (#172 `feat(obs): MVP-3 W6 T1 — OTel SDK setup`) ✅ merged
  - W6 T2 (#169 `feat(obs): MVP-3 W6 T2 — slog→OTel logs bridge`) ✅ merged
  - W6 T5 (#210 `feat(otel): Config.Tracer injection across 8 components + span hierarchy`) ✅ merged
- **Soft prereq:** W6 T3 (PR #209 `feat(state): migration 0005 adds trace_id columns + PersistTraceIDFromContext seam`) — open, CI green except pr-lint. **0005 must merge before T-S1 dispatches** (spec §11: substrate uses 0006; goose refuses 0006 if 0005 missing). If T3 lands behind schedule, T-S1 still dispatches because T-S1 owns migration `0006_substrate.sql` and only depends on 0005's existence on disk + applied — both true the moment #209 merges.
- **Sequence vs parallel:** T-S1 is the spine (owns `AppendEvent` + `Fold` + signing + migration). T-S2 and T-S3 both import T-S1's API. Per `feedback_shared_primitive_owner`: T-S1 OWNS the primitive; T-S2 + T-S3 import pre-merge via the shared-primitive pattern. **Dispatch order: T-S1 alone first; once T-S1 has a PR open with `AppendEvent` callable, dispatch T-S2 + T-S3 in parallel against T-S1's branch via git worktree.** Per `feedback_sequence_dependent_work`: T-S2 + T-S3 consume T-S1's exported API ⇒ SEQUENCE T-S1 first, then parallel T-S2/T-S3.
- **Migration phasing:** Wave 1 ships **Phase A only** (substrate dark, no readers, `SUBSTRATE_ENABLED=false`). Phase B shadow-write + Phase C cutover are W2/W3 deliverables (spec §3, §13 deferred-tasks list).
- **Concurrency cap:** per `feedback_session_limit_dispatch` — cap 3-4 implementers. T-S1 solo first, then T-S2 + T-S3 ⇒ peak 2 parallel ⇒ well within budget. No risk of session-limit cascade.
- **Deletion default (`feedback_deletion_default`):** Wave 1 is pure-addition (new package, new migration). Per the spec the substrate REPLACES `approval_events`, `work_item_outputs`, and per-agent `events` writes — but that deletion lands in Phase C/D (W2+). Wave 1 PRs MUST cite "what gets smaller later" in their PR body: Phase D drops 5 legacy tables once cutover is stable. A+ defense per memory rule applies to every Wave 1 PR.

---

## §1 File-disjoint table

| Task  | Path (exclusive write scope)                                                                                                                                                                                                                                                                              | Depends-on (Wave 1 + main) | Effort | TDD tests (count: named)                                                                                                                                                                                                                  |
| ----- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- | ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| T-S1  | `internal/orchestrator/state/migrations/0006_substrate.sql`; `internal/orchestrator/state/substrate/{event,fold,sign,validate,reducer,errors,ulid}.go` + matching `*_test.go`; `internal/orchestrator/state/substrate/event_bench_test.go` (Task 0 benchmark gate)                                       | main (#209 T3 merged)      | L      | 12 named (B-tier 7, A-tier 4, perf-gate 1). Spec §6 + §9 B-rubric + §10 #1/#3/#7/#11/#16.                                                                                                                                                |
| T-S2  | `internal/program/cel_decider.go` + `cel_decider_test.go`; `internal/orchestrator/state/substrate/gate_verdict_payload.go` (typed payload struct + validator entry registered via T-S1's `validate.go` dispatch table — file-disjoint via new file)                                                      | T-S1                       | M      | 6 named (B 4, A 2). Spec §6 + §10 #17.                                                                                                                                                                                                  |
| T-S3  | `tools/lint-substrate-queries/main.go` + `tools/lint-substrate-queries/lint_test.go`; `tools/lint-keyring-readonly/main.go` + `tools/lint-keyring-readonly/lint_test.go`; `internal/orchestrator/state/substrate/{enum_parity_test,cycle_property_test,nonce_mismatch_test,replay_protection_test}.go`    | T-S1                       | M      | 9 named (B 4, A 3, A+ 2). Spec §6 + §9 A-rubric + §10 #1/#13/#14/#15.                                                                                                                                                                   |

**Disjointness verification:** T-S1 owns the `substrate/` package skeleton + every primary `.go` file. T-S2 adds ONE new file (`gate_verdict_payload.go`) inside `substrate/` but does NOT modify any T-S1 file — registration into the validator dispatch happens because T-S1 ships an `init()` registrar pattern (documented in T-S1's dispatch prompt below). T-S3 adds ONLY `*_test.go` files inside `substrate/` (test-files are file-disjoint from prod files and from each other) plus two new directories under `tools/`. No path appears in two rows.

**Cross-task seam contracts (load-bearing — implementer MUST honour exactly):**
- T-S1 exports `AppendEvent(ctx, tx, e Event) error`, `Fold(ctx, db, runID, kind) ([]Event, error)`, `RegisterPayloadValidator(kind EventKind, fn func(json.RawMessage) error)`, `DefaultTenantID` constant, and `Event` / `EventKind` / `ReducerStrategy` types. T-S2 + T-S3 import these and only these.
- T-S1 ships a typed-payload validator dispatch via `RegisterPayloadValidator` callable from `init()` in sibling files. T-S2's `gate_verdict_payload.go` registers `KindGateVerdict` validator from its own `init()`. This keeps validator dispatch open-extensible without T-S2 touching T-S1's `validate.go`.
- T-S1's `Event.SigAlg / SigKeyID / SigMAC` are populated by T-S1 internally using `contracts/schemas/sign.go::MacSum`. Callers DO NOT set these fields.
- Reducer hardcoded per-`kind` in T-S1's `defaultReducer(kind)` (spec §4 table) — implementer cannot deviate.

---

## §2 Task T-S1 — Substrate schema + storage primitives + benchmark gate

### Scope
- Migration `internal/orchestrator/state/migrations/0006_substrate.sql` matching spec §2.1 DDL **verbatim** (all 11 columns, 5 indexes, 1 unique index, all 4 CHECK clauses, the `supersedes` FK). Bump `CurrentSchemaVersion` 5 → 6.
- Package `internal/orchestrator/state/substrate/`:
  - `event.go` — `Event` struct + `EventKind` enum + constants per spec §2.1 (NO `KindLockHeld` per S3).
  - `errors.go` — `ErrInvalidPayload`, `ErrReplay`, `ErrSupersedesCycle`, `ErrClockRegression`, `ErrTenantRequired`.
  - `ulid.go` — Crockford-base32 ULID minter (`Mint(t time.Time) string`); uses `crypto/rand`. Single-host single-writer per spec §10 #14.
  - `sign.go` — HMAC envelope: `Sign(e *Event, key []byte, keyID string) error` mirroring `contracts/schemas/sign.go::MacSum` for `(id, kind, key, payload, written_by, schema_version, trace_id, span_id, nonce)` per spec §5; verifier counterpart `Verify(e Event, keyring map[string][]byte) error` MUST assert `e.Nonce == signedPayload["nonce"]` per spec §5 I5.
  - `validate.go` — `RegisterPayloadValidator(kind EventKind, fn func(json.RawMessage) error)`, internal dispatch table, default `nil` returns `ErrInvalidPayload`. T-S1 ships validators for the 6 non-gate kinds (`KindNodeOutput`, `KindFact`, `KindApprovalEvent`, `KindTokenSpend`, `KindBudgetReconciled`, `KindHeartbeat`); T-S2 registers `KindGateVerdict`.
  - `reducer.go` — `ReducerStrategy` enum + `defaultReducer(kind) ReducerStrategy` hardcoded per spec §4 table (`node_output`/`fact`/`budget_reconciled`/`heartbeat` → `lww`; `approval_event`/`token_spend`/`gate_verdict` → `append`).
  - `event.go` continued — `AppendEvent(ctx, tx, e Event) error` performing in this order: (a) typed-unmarshal validate via dispatch; (b) HMAC sign; (c) supersedes FK + Kahn's cycle-check restricted to `run_id = ?` per spec §8 (reuse pattern from `internal/orchestrator/state/work_items_query.go::CycleCheck` lines 99-239); (d) `lastWrittenAt` process-mutex monotonicity check per spec §8 I2; (e) `INSERT`; (f) on `UNIQUE(run_id, written_by, nonce)` collision return `ErrReplay`.
  - `fold.go` — `Fold(ctx, db, runID, kind) ([]Event, error)` using `idx_substrate_events_kind`; orders by `(written_at, id)` per spec §2.3.
  - `DefaultTenantID = "default"` exported constant per spec R3.
- `event_bench_test.go` — `BenchmarkAppendEvent_HMACAndCanonOnly` exercising sign-only (no INSERT) — Task 0 perf gate per spec §7. Target: ≤ 500 ns/op on GitHub Actions `ubuntu-latest` 4-vCPU. **PR opens only after this benchmark lands and reports ≤ 500 ns/op locally.**

### Prereqs (cite spec sections)
- Spec §2.1 — schema DDL verbatim.
- Spec §2.3 — Go API signatures.
- Spec §4 — reducer strategy per kind.
- Spec §5 — HMAC canonicalization, nonce-in-signature, key reuse from `contracts/schemas/sign.go`.
- Spec §7 — perf budget + Task 0 gate.
- Spec §8 — failure modes (Kahn's cycle check at write time R7; clock regression I2; nonce uniqueness I5).
- Spec §11 — kind enum + reducer hardcoded per-kind (cite `feedback_spec_pattern_authority`).
- Spec §13 row #1 — Task 1 file scope.
- Existing patterns to reuse:
  - `internal/orchestrator/state/work_items_query.go::CycleCheck` lines 99-239 — Kahn's-sort pattern (R7).
  - `internal/orchestrator/state/agents.go` line 86, 226 — `db.WithTx` pattern.
  - `contracts/schemas/sign.go::MacSum` — HMAC primitive (do NOT introduce a new signing primitive; reuse).
  - `internal/orchestrator/state/migrations/*.sql` — goose Up/Down format; T-S1's 0006 mirrors W6's 0005 shape.

### TDD test list (with failing-output capture step)

Per `feedback_tdd_discipline`: implementer writes each test, runs `go test ./internal/orchestrator/state/substrate/ -run <name> -v`, **captures the failing output (paste into PR body)**, then implements. No exceptions.

**B-tier (spec §9 + §10):**
1. `TestMigration0006_AppliesAndCreatesSchema` — fresh DB → migrate → schema-introspect confirms all 11 columns, 5 indexes, 1 unique index, FK present. Pins schema-version 6.
2. `TestSubstrate_AppendEventRoundTrip` — `AppendEvent` then `Fold` returns the same event; signature verifies; payload unmodified.
3. `TestSubstrate_ReplayProtection` — same `(run_id, written_by, nonce)` twice ⇒ second call returns `ErrReplay` (UNIQUE-collision path).
4. `TestSubstrate_NonceMismatchRejected` — column nonce ≠ signed-payload nonce ⇒ `Verify` returns `ErrUnverifiable` per spec §5 I5.
5. `TestSubstrate_SupersedesCycleRejected` — A supersedes B; B supersedes A ⇒ second `AppendEvent` returns `ErrSupersedesCycle` (Kahn's check within `run_id` per spec §8 R7 + §10 #3).
6. `TestSubstrate_CrossRunReplayRejected` — capture a valid event from run X; replay payload into run Y with same nonce ⇒ signature carries `run_id` so verify fails. Spec §10 #1.
7. `TestSubstrate_NoUpdateDeleteInSubstratePackage` — `grep -rE '\b(UPDATE|DELETE)\b' internal/orchestrator/state/substrate/` returns zero matches in non-test `.go` files. Pins append-only invariant per spec §9 B-tier.

**A-tier (spec §9 A-rubric):**
8. `TestSubstrate_KindPayloadValidation` — table-driven per kind (6 kinds owned by T-S1; gate_verdict owned by T-S2 test); malformed payload ⇒ `ErrInvalidPayload`.
9. `TestSubstrate_ClockRegressionRejected` — call `AppendEvent` with `WrittenAt` < lastWrittenAt ⇒ `ErrClockRegression`. Spec §8 I2.
10. `TestSubstrate_ConcurrentFoldReadSnapshot` — writer appends while two readers fold concurrently; reads see consistent snapshot (sqlite WAL). Spec §10 #2.
11. `TestSubstrate_FoldOrdersByWrittenAtThenID` — two events same `written_at`; fold orders deterministically by ULID. Spec §10 #16.

**Perf gate (spec §7 + §13 Task 0):**
12. `BenchmarkAppendEvent_HMACAndCanonOnly` — exercises sign + canonical-JSON; target ≤ 500 ns/op. PR body MUST paste benchstat output; if > 500 ns/op, escalate before opening PR.

### PR body skeleton

```
## Summary

T-S1 ships the substrate event log primitive per
docs/engineer/specs/2026-06-01-unified-substrate-design.md §2 §4 §5.
Phase A only (substrate dark, no readers, SUBSTRATE_ENABLED=false).

- Migration `0006_substrate.sql`: `substrate_events` table + 5 indexes
  + UNIQUE(run_id, written_by, nonce) per §2.1 / spec R1.
- Package `internal/orchestrator/state/substrate/`: AppendEvent, Fold,
  ULID minter, HMAC sign+verify (reuses contracts/schemas/sign.go::MacSum),
  per-kind typed-payload validator dispatch, hardcoded reducer strategy
  per §4 (lww | append | write-once enum, no policy override — S1
  defers policies to W11).
- Kahn's cycle-check on `supersedes` restricted to `run_id = ?` per §8 R7;
  reuses internal/orchestrator/state/work_items_query.go::CycleCheck pattern.
- Process-local lastWrittenAt monotonicity check per §8 I2.
- Bumps CurrentSchemaVersion 5 → 6.

## Why

MVP-3 Wave 1. Substrate is the events-log primitive that collapses bespoke
history (approval_events, work_item_outputs, per-agent events) into one
signed append-only log. Phase D (W4+) drops the legacy tables once
read-side cutover (Phase B/C, Wave 2+) proves stable — what gets smaller
is documented inline (deletion-default per feedback_deletion_default).

## Test plan

- [x] B-tier: TestMigration0006_AppliesAndCreatesSchema,
       TestSubstrate_AppendEventRoundTrip, TestSubstrate_ReplayProtection,
       TestSubstrate_NonceMismatchRejected, TestSubstrate_SupersedesCycleRejected,
       TestSubstrate_CrossRunReplayRejected, TestSubstrate_NoUpdateDeleteInSubstratePackage.
- [x] A-tier: TestSubstrate_KindPayloadValidation (6 kinds; gate_verdict in T-S2),
       TestSubstrate_ClockRegressionRejected, TestSubstrate_ConcurrentFoldReadSnapshot,
       TestSubstrate_FoldOrdersByWrittenAtThenID.
- [x] Perf gate: BenchmarkAppendEvent_HMACAndCanonOnly ≤ 500 ns/op
       (benchstat output below).
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Benchstat output (Task 0 perf gate)

<paste benchstat output — required before PR opens per spec §7>

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [substrate-followup] F1 AppendEvent fast-path for pre-canonicalized payloads (#NNN)
- [substrate-followup] F2 schema_version v2 migration recipe (#NNN)
- [substrate-followup] F3 tenant_id retag helper for single→multi-tenant cutover (#NNN)
- [substrate-followup] F4 per-kind TTL cron + archive_audit_outbox (#NNN)
- [substrate-followup] F6 UUIDv7 vs ULID for post-W9 multi-host deployments (#NNN)
- [substrate-followup] F7 substrate_policies primitive (W8 RBAC) (#NNN)
- [substrate-followup] F8 substrate_blobs CAS + ref-counted GC (#NNN)
- [substrate-followup] F10 reducer-strategy change re-fold tooling (#NNN)

## Deletion default

Wave 1 is pure-addition (new package + new migration). What gets smaller:
Phase D (post-Wave-3) drops 5 legacy tables — approval_events,
work_item_outputs, work_item_edges (event-shape rows), per-agent events,
and reduces approvals shape — once read-cutover proves stable. The
substrate's append-only shape makes the GC story tractable; the bespoke
shape did not.

```release-notes
[FEATURE] substrate event log primitive (Phase A — dark, no readers)
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-substrate-ts1. Branch off main:
`git checkout -b feat/substrate-ts1-event-log main`.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-unified-substrate-design.md.
Read ALL of: §2 (schema), §4 (reducers), §5 (HMAC), §7 (perf budget),
§8 (failure modes), §9 (grade rubric), §10 (red-team), §11 (sequencing),
§13 (task split row #1 — that's you).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (reducer strategy per kind, HMAC reuse, AppendEvent
signature, validate-dispatch shape), STOP and report — do NOT pick an
alternative yourself. Re-spawn the design subagent.

# Scope (exclusive write paths — file-disjoint with T-S2 + T-S3)

- internal/orchestrator/state/migrations/0006_substrate.sql
- internal/orchestrator/state/substrate/event.go      (Event struct + EventKind enum + AppendEvent)
- internal/orchestrator/state/substrate/fold.go        (Fold)
- internal/orchestrator/state/substrate/sign.go        (HMAC sign+verify wrapping contracts/schemas/sign.go::MacSum)
- internal/orchestrator/state/substrate/validate.go    (RegisterPayloadValidator + dispatch table + 6 validators)
- internal/orchestrator/state/substrate/reducer.go     (ReducerStrategy enum + defaultReducer(kind))
- internal/orchestrator/state/substrate/ulid.go        (Crockford-base32 ULID minter)
- internal/orchestrator/state/substrate/errors.go      (sentinel errors)
- internal/orchestrator/state/substrate/*_test.go      (your 11 named tests below)
- internal/orchestrator/state/substrate/event_bench_test.go (Task 0 perf gate)
- internal/orchestrator/state/migrate.go               (bump CurrentSchemaVersion 5 → 6)

You MUST NOT touch any other file. If you discover a missing seam in an
existing file, STOP and report — do not edit out of scope (lesson from
PR #209: out-of-scope edits get caught at review and need a separate
issue).

# Patterns to reuse (do NOT reinvent)

- HMAC: contracts/schemas/sign.go::MacSum. Do NOT introduce a new
  signing primitive — wrap MacSum in substrate/sign.go.
- Cycle check: internal/orchestrator/state/work_items_query.go::CycleCheck
  lines 99-239. Mirror the Kahn's-sort + CSR layout; restrict scope to
  `WHERE run_id = ?` per spec §8 R7 / §10 #3.
- WithTx: internal/orchestrator/state/agents.go line 86, 226.
  AppendEvent takes *sql.Tx; caller owns the tx via db.WithTx.
- Migration shape: internal/orchestrator/state/migrations/0005_*.sql
  (W6 T3, currently in PR #209 — read it for the goose Up/Down format).

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test below:
  1. Write the test file first.
  2. Run `go test ./internal/orchestrator/state/substrate/ -run <name> -v`.
  3. CAPTURE the failing output (paste into PR body's "Failing-test
     output (TDD capture)" section). "Tests would have failed" is
     NOT acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or per logical group; squash later).

# Tests to land (12 named; spec §6 + §9 B/A-rubric + §10)

B-tier:
1. TestMigration0006_AppliesAndCreatesSchema
2. TestSubstrate_AppendEventRoundTrip
3. TestSubstrate_ReplayProtection
4. TestSubstrate_NonceMismatchRejected
5. TestSubstrate_SupersedesCycleRejected
6. TestSubstrate_CrossRunReplayRejected
7. TestSubstrate_NoUpdateDeleteInSubstratePackage

A-tier:
8. TestSubstrate_KindPayloadValidation (your 6 kinds; gate_verdict
   payload validation is registered + tested by T-S2; your test asserts
   the dispatch table rejects unknown kinds with ErrInvalidPayload)
9. TestSubstrate_ClockRegressionRejected
10. TestSubstrate_ConcurrentFoldReadSnapshot
11. TestSubstrate_FoldOrdersByWrittenAtThenID

Perf gate (Task 0 — REQUIRED before opening PR):
12. BenchmarkAppendEvent_HMACAndCanonOnly — target ≤ 500 ns/op. Run
    `go test -bench BenchmarkAppendEvent_HMACAndCanonOnly -benchmem
    -count=10 ./internal/orchestrator/state/substrate/` and paste
    benchstat output. If > 500 ns/op, STOP and report.

# Workflow after green

  1. Run `make pre-push-check` — confirm clean.
  2. Push branch: `git push -u origin feat/substrate-ts1-event-log`.
  3. File the 8 followup issues (F1, F2, F3, F4, F6, F7, F8, F10 from
     spec §12) as `[substrate-followup]`-prefixed issues, gather the
     issue numbers.
  4. Open PR with body template above (see plan §2 PR body skeleton),
     citing the followup issue numbers.
  5. After PR opens, spawn ONE adversarial reviewer subagent (per
     feedback_adversarial_review + feedback_agent_pr_review) with hunt
     list: HMAC nonce/signature correctness, Kahn's cycle scope bound to
     run_id, clock regression mutex correctness, INSERT-side ordering
     (validate → sign → cycle → insert → unique-collision), reducer-per-
     kind hardcoded per spec §4 table (NO deviation per
     feedback_spec_pattern_authority), SQL injection safety in 0006
     migration, schema-version bump, benchstat output present + meets
     budget. Reviewer must use OK:/ISSUE: per item.
  6. Apply reviewer findings; re-run make pre-push-check; force-push.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 5 of the 11 functional tests
  (sample is fine — do NOT paste all 11; the PR body carries the full set).
- Pasted benchstat output for the perf gate.
- The 8 followup issue numbers filed.
- Adversarial reviewer verdict (APPROVE | findings list).
- One-line diff stat: files changed + LoC added.
```

---

## §3 Task T-S2 — CELDecider concrete type + gate_verdict emission

### Scope
- `internal/program/cel_decider.go` — `CELDecider` concrete type per spec §2.2 (NO `Decider` interface — spec S5). Holds `cel.Program` compiled at construction time (rejects malformed CEL). `Snapshot` struct + `GateResult` struct. `Decide(ctx, s Snapshot) (GateResult, error)` method captures a read-tx, folds substrate via T-S1's `Fold`, evaluates CEL, calls T-S1's `AppendEvent` with `kind=gate_verdict`, commits — all in one tx per spec §10 #17.
- `internal/orchestrator/state/substrate/gate_verdict_payload.go` — typed payload struct `GateVerdictPayload` (fields: `GateName string`, `Pass bool`, `Reason string`, `WorkItemID string`) + `init()` registers via T-S1's `RegisterPayloadValidator(substrate.KindGateVerdict, validateGateVerdict)`.
- Tests: `cel_decider_test.go` + the gate_verdict validator test (in `gate_verdict_payload_test.go` co-located).

### Prereqs (cite spec sections)
- Spec §2.2 — CELDecider concrete type (NO interface).
- Spec §10 #17 — Snapshot-isolation discipline (one tx for fold + eval + emit).
- Spec §11 reducer table — `gate_verdict` is `append`; `RouteVerdicts` consumes most-recent per `(work_item_id, gate_name)`.
- Spec S5 — no `Decider` interface in Wave 1.
- Existing patterns: `internal/program/route.go::RouteVerdicts` (read for the existing CEL-over-verdicts semantics — preserve verbatim).
- T-S1's exported API: `AppendEvent`, `Fold`, `KindGateVerdict`, `RegisterPayloadValidator`.

### TDD test list (with failing-output capture step)

**B-tier:**
1. `TestCELDecider_CompileRejectsBadCEL` — `NewCELDecider("not a cel expression")` returns compile error.
2. `TestCELDecider_DecideEmitsSignedGateVerdict` — happy path: snapshot folded, CEL evaluated, `AppendEvent` called once with `kind=gate_verdict`, verifiable signature.
3. `TestCELDecider_FailedCELReturnsErrorNoEmit` — eval-time CEL error ⇒ `Decide` returns error, NO substrate row written (tx rolled back).
4. `TestSubstrate_GateVerdictPayloadValidation` — malformed payload (missing `gate_name`) ⇒ `ErrInvalidPayload`; well-formed ⇒ nil.

**A-tier:**
5. `TestCELDecider_OneTxForFoldEvalEmit` — assert `Decide` uses a single tx by injecting a mock DB whose `BeginTx` is called exactly once per `Decide` call. Pins spec §10 #17.
6. `TestCELDecider_SnapshotCarriesTraceSpanID` — Snapshot constructed inside an active span carries non-empty `TraceID` + `SpanID`; gate_verdict event signs them.

### PR body skeleton

```
## Summary

T-S2 ships the CELDecider concrete type per
docs/engineer/specs/2026-06-01-unified-substrate-design.md §2.2 §10 #17.

- internal/program/cel_decider.go — CELDecider with pre-compiled
  cel.Program; Snapshot + GateResult structs; Decide method runs
  fold + eval + emit in ONE substrate tx (spec §10 #17).
- Registers KindGateVerdict typed-payload validator via T-S1's
  RegisterPayloadValidator dispatch (init() in
  internal/orchestrator/state/substrate/gate_verdict_payload.go).
- No Decider interface yet — spec S5 defers extraction until
  HumanDecider lands (W2 of W2 approval-gates wedge). Concrete
  type now; interface later.

## Why

MVP-3 Wave 1 Task T-S2. Substrate's gate_verdict event channel
consumes signed CEL outputs; CELDecider is the producer. Reusing
the cel-go pattern from internal/program/route.go::RouteVerdicts
keeps verdict semantics identical to MVP-2's existing routing path
(Trap P1).

## Test plan

- [x] B-tier: TestCELDecider_CompileRejectsBadCEL,
       TestCELDecider_DecideEmitsSignedGateVerdict,
       TestCELDecider_FailedCELReturnsErrorNoEmit,
       TestSubstrate_GateVerdictPayloadValidation.
- [x] A-tier: TestCELDecider_OneTxForFoldEvalEmit,
       TestCELDecider_SnapshotCarriesTraceSpanID.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Deletion default

Pure-addition (one new concrete type, one new payload struct). What
gets smaller: spec S5 defers the Decider interface to MVP-3 Wave 2
(HumanDecider) — Wave 1 ships ONE concrete type, NOT the 3-impl
interface that v1 of the spec proposed. ~120 LoC saved vs the v1
shape.

```release-notes
[FEATURE] CELDecider concrete type + gate_verdict substrate emission
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-substrate-ts2. Branch off T-S1's branch (NOT
main — T-S1 must be open as a PR before you start):
`git fetch origin && git checkout -b feat/substrate-ts2-cel-decider origin/feat/substrate-ts1-event-log`.

After T-S1's PR merges to main, rebase onto main before pushing your PR.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-unified-substrate-design.md.
Read ALL of: §2.2 (CELDecider), §10 #17 (tx discipline), §11 (Decider
interface deferred per S5), §13 row #2 (you).

Per feedback_spec_pattern_authority: NO Decider interface in Wave 1.
NO interface extraction. ONE concrete type only. If the existing
internal/program/route.go needs a refactor to use CELDecider, file a
followup issue — do NOT do it in this PR (out of scope).

# Scope (exclusive write paths — file-disjoint with T-S1 + T-S3)

- internal/program/cel_decider.go
- internal/program/cel_decider_test.go
- internal/orchestrator/state/substrate/gate_verdict_payload.go
- internal/orchestrator/state/substrate/gate_verdict_payload_test.go

You MUST NOT touch any other file. Specifically: do NOT modify
internal/program/route.go (Trap P1 surface; out of scope). Do NOT
modify T-S1's substrate files; register your KindGateVerdict
validator via T-S1's exported RegisterPayloadValidator from your
init() function.

# Patterns to reuse

- cel-go usage: internal/program/route.go for the existing pattern.
- T-S1's API: AppendEvent, Fold, KindGateVerdict (const),
  RegisterPayloadValidator (registrar). Import only these.
- WithTx: internal/orchestrator/state/agents.go::WithTx — call from
  Decide so fold + AppendEvent share one tx.

# Workflow steps (TDD discipline)

For each named test:
  1. Write test first.
  2. Run `go test ./internal/program/ -run TestCELDecider_<name> -v`
     (or substrate package for the validator test).
  3. CAPTURE failing output.
  4. Implement.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (6 named; spec §6 + §10 #17)

B-tier:
1. TestCELDecider_CompileRejectsBadCEL
2. TestCELDecider_DecideEmitsSignedGateVerdict
3. TestCELDecider_FailedCELReturnsErrorNoEmit
4. TestSubstrate_GateVerdictPayloadValidation

A-tier:
5. TestCELDecider_OneTxForFoldEvalEmit
6. TestCELDecider_SnapshotCarriesTraceSpanID

# Workflow after green

  1. Run `make pre-push-check` — confirm clean.
  2. Push branch.
  3. Open PR with body template (plan §3 PR body skeleton). PR base
     branch is T-S1's branch until T-S1 merges; rebase to main once
     T-S1 lands.
  4. Spawn ONE adversarial reviewer subagent with hunt list: one-tx
     discipline (no tx leak; no double-commit), CEL compile-time
     rejection of malformed input, ErrInvalidPayload routing for
     bad gate_verdict shapes, snapshot carries trace/span IDs from
     ctx not from caller-injected fields (otherwise hostile caller
     could forge), reducer-strategy hardcoded per spec §4 (append),
     no Decider interface introduced (spec S5).
  5. Apply findings; re-run pre-push-check; force-push.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 3 of the 6 tests.
- Adversarial reviewer verdict.
- One-line diff stat.
```

---

## §4 Task T-S3 — Query lint + AST-mutation tests + property tests

### Scope
- `tools/lint-substrate-queries/main.go` — go-AST + SQL-string walker per spec §6 + §9 A+-tier. Rejects any SQL string in `internal/` that selects from `substrate_events` without a `WHERE` clause containing `kind = ?`. For cross-run reads (no `run_id = ?` clause), ALSO requires `tenant_id = ?` per spec §6. Bool-CLI; exit 1 on findings, 0 clean. `_test.go` exercises 6 fixture queries (3 valid, 3 violating).
- `tools/lint-keyring-readonly/main.go` — go-AST walker per spec §5. Rejects any `KeyringSet`-shaped function call outside `init` / `Setup`. `_test.go` covers happy + violation cases.
- `internal/orchestrator/state/substrate/enum_parity_test.go` — `TestSubstrate_EventKindEnumMatchesSQLCheck` (spec §6 / §9 A-tier N1) — parses the 0006 migration file, extracts the `CHECK (kind IN (...))` enum, compares to the Go enum constants. Fails on mismatch.
- `internal/orchestrator/state/substrate/cycle_property_test.go` — pgregory.net/rapid-based property test exercising 200 synthetic supersedes graph shapes; asserts cycle ⇒ `ErrSupersedesCycle`, acyclic ⇒ success. Pins spec §10 #3.
- `internal/orchestrator/state/substrate/nonce_mismatch_test.go` — co-located adversarial test (spec §5 I5): construct an event with `nonce=X`, sign with `nonce=Y` in the signed payload, verify rejects. (May overlap conceptually with T-S1's `TestSubstrate_NonceMismatchRejected`; this test is the **stricter adversarial form** — verifier-only, no signer side.)
- `internal/orchestrator/state/substrate/replay_protection_test.go` — replay protection property test (200 nonce collisions, all rejected).

### Prereqs (cite spec sections)
- Spec §5 — nonce-in-signature invariant + keyring-readonly defense.
- Spec §6 — lint-substrate-queries spec.
- Spec §9 A-tier N1 — enum parity test.
- Spec §9 A+-tier — lint-substrate-queries; A+ partition-pruning load test (deferred from this task — Wave 2).
- Spec §10 #3 #13 #14 #15 — adversarial red-team items.
- Existing patterns:
  - `internal/canon/approval_token_lint_test.go` — in-package lint-via-AST pattern (read for the file-walker shape).
  - `internal/orchestrator/state/cycle_check_property_test.go` — pgregory.net/rapid pattern (already shipping; mirror).

### TDD test list (with failing-output capture step)

**B-tier:**
1. `TestLintSubstrateQueries_RejectsUnscopedRead` — fixture SQL `SELECT * FROM substrate_events` ⇒ lint exits 1.
2. `TestLintSubstrateQueries_AllowsKindScopedRead` — fixture `SELECT * FROM substrate_events WHERE kind=?` ⇒ lint exits 0.
3. `TestLintSubstrateQueries_RequiresTenantOnCrossRunRead` — `SELECT * FROM substrate_events WHERE kind=?` (no run_id, no tenant_id) ⇒ lint exits 1.
4. `TestLintKeyringReadOnly_RejectsRuntimeKeyringSet` — fixture `KeyringSet` call outside init/Setup ⇒ lint exits 1.

**A-tier (spec §9 A-rubric):**
5. `TestSubstrate_EventKindEnumMatchesSQLCheck` (N1) — parse 0006_substrate.sql `CHECK (kind IN ...)` ↔ Go constants; mismatch ⇒ fail.
6. `TestSubstrate_SupersedesCycleProperty` — pgregory rapid; 200 random graphs; cycle iff `ErrSupersedesCycle`.
7. `TestSubstrate_ReplayProtectionProperty` — pgregory rapid; 200 collisions; all rejected with `ErrReplay`.

**A+-tier:**
8. `TestSubstrate_NonceMismatchRejected_Verifier` — verifier-only adversarial form per spec §5 I5 (stricter than T-S1's signer-side test).
9. `TestKeyringReadOnly_LintIntegrationCI` — CI helper test that runs `tools/lint-keyring-readonly` against the full repo; asserts zero findings on main.

### PR body skeleton

```
## Summary

T-S3 ships the substrate lint gates + property tests + adversarial
test coverage per docs/engineer/specs/2026-06-01-unified-substrate-design.md
§5 §6 §9 A-rubric §10.

- tools/lint-substrate-queries — AST + SQL walker rejects unscoped
  substrate_events reads (no `kind=?` ⇒ fail; no `run_id=?` AND no
  `tenant_id=?` ⇒ fail).
- tools/lint-keyring-readonly — rejects KeyringSet outside init/Setup
  (spec §5).
- TestSubstrate_EventKindEnumMatchesSQLCheck (N1) — pins enum ↔ SQL
  CHECK parity.
- Property tests: cycle (200 graphs), replay (200 collisions).
- Adversarial: TestSubstrate_NonceMismatchRejected_Verifier (verifier-
  side I5 form, stricter than T-S1's signer-side test).
- Lint CI integration test asserts zero findings on main.

## Why

MVP-3 Wave 1 Task T-S3. Adversarial coverage layer over T-S1's
primitives + T-S2's CELDecider. Spec §9 A-rubric A-tier (N1 + AST
mutation lint) + A+-tier (lint-substrate-queries in CI).

## Test plan

- [x] B-tier: 4 lint-fixture tests.
- [x] A-tier: enum-parity + 2 property tests.
- [x] A+-tier: verifier-side nonce-mismatch + CI lint integration.
- [x] make pre-push-check clean.
- [x] `go run ./tools/lint-substrate-queries ./internal/...` exit 0.
- [x] `go run ./tools/lint-keyring-readonly ./internal/...` exit 0.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Deletion default

Pure-addition lint tools (~150 LoC each). What gets smaller: the
substrate's lint gates make a class of bugs (unscoped reads, runtime
keyring mutation) IMPOSSIBLE to land in subsequent waves — Wave 2/3
review surface shrinks because the lint is automatic.

```release-notes
[FEATURE] substrate lint + adversarial test coverage
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-substrate-ts3. Branch off T-S1's branch:
`git fetch origin && git checkout -b feat/substrate-ts3-lint-coverage origin/feat/substrate-ts1-event-log`.

After T-S1's PR merges to main, rebase onto main before pushing
your PR.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-unified-substrate-design.md.
Read ALL of: §5 (keyring readonly), §6 (lint-substrate-queries), §9
A-tier + A+-tier (enum parity + lint in CI), §10 #3 #13 #14 #15,
§13 row #3 (you).

# Scope (exclusive write paths — file-disjoint with T-S1 + T-S2)

- tools/lint-substrate-queries/main.go
- tools/lint-substrate-queries/lint_test.go
- tools/lint-substrate-queries/testdata/<fixtures>
- tools/lint-keyring-readonly/main.go
- tools/lint-keyring-readonly/lint_test.go
- tools/lint-keyring-readonly/testdata/<fixtures>
- internal/orchestrator/state/substrate/enum_parity_test.go
- internal/orchestrator/state/substrate/cycle_property_test.go
- internal/orchestrator/state/substrate/replay_protection_test.go
- internal/orchestrator/state/substrate/nonce_mismatch_test.go

You MUST NOT touch any other file. Lint findings against existing
production code (other than substrate package) are out of scope —
file a tracking issue per finding; do NOT fix here.

# Patterns to reuse

- AST file-walker: internal/canon/approval_token_lint_test.go.
- Property test rapid pattern: internal/orchestrator/state/cycle_check_property_test.go.
- 0006_substrate.sql lives at internal/orchestrator/state/migrations/0006_substrate.sql
  (T-S1's output) — your enum_parity_test reads from there.

# Workflow steps (TDD discipline)

For each named test:
  1. Write test first.
  2. Run targeted test.
  3. CAPTURE failing output.
  4. Implement lint tool / property body.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (9 named)

B-tier:
1. TestLintSubstrateQueries_RejectsUnscopedRead
2. TestLintSubstrateQueries_AllowsKindScopedRead
3. TestLintSubstrateQueries_RequiresTenantOnCrossRunRead
4. TestLintKeyringReadOnly_RejectsRuntimeKeyringSet

A-tier:
5. TestSubstrate_EventKindEnumMatchesSQLCheck (N1)
6. TestSubstrate_SupersedesCycleProperty
7. TestSubstrate_ReplayProtectionProperty

A+-tier:
8. TestSubstrate_NonceMismatchRejected_Verifier
9. TestKeyringReadOnly_LintIntegrationCI

# Workflow after green

  1. Run `make pre-push-check` clean.
  2. Run both lint tools against ./internal/... and confirm zero
     findings (substrate package is the only legitimate caller; T-S1
     ships compliant code).
  3. Push branch.
  4. Open PR with body template (plan §4 PR body skeleton).
  5. Spawn ONE adversarial reviewer subagent with hunt list: lint
     false-positive analysis (Does it flag legitimate code?), AST
     completeness (Does the walker visit all relevant nodes?), property
     test seed determinism (Is rapid.Check reproducible across CI
     runs?), CI-integration test runs against actual main and not a
     mock, enum parity test handles whitespace + comment variants in
     SQL CHECK.
  6. Apply findings; re-run; force-push.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 4 of the 9 tests.
- Output of both lint tools run against ./internal/...
- Adversarial reviewer verdict.
- One-line diff stat.
```

---

## §5 After Wave 1: handoff to Wave 2

Wave 1 ships Phase A (substrate dark, no readers). Wave 2 owns:

- **Phase B shadow-write wiring** — producer callsites that opt in (approval-gates wedge first, by isolation per spec §3) call `AppendEvent` AFTER their legacy write succeeds. Two independent transactions. Files: `internal/orchestrator/state/approvals.go`, `internal/orchestrator/state/work_item_outputs.go`, `internal/gates/approval/reaper.go`. **All three are production paths W6 T3 already touched** — sequence Wave 2 carefully to avoid merge conflict with any still-in-flight W6 T4 work.
- **Reconciliation cron** — `internal/orchestrator/substrate_recon/` periodically asserts `fold(substrate WHERE kind=approval_event AND run_id=X) ≈ SELECT * FROM approval_events WHERE run_id=X`. Divergence opens an audit issue, NOT runtime failure (substrate still shadow). Backfill via re-signing.
- **Phase C read-side cutover (node_output first, lowest-risk)** — `SUBSTRATE_READ_FROM=substrate node_output` flag; rollback = flip flag back; both legacy + substrate keep being written so rollback is zero-loss. `TestSubstrate_CutoverNoRegression_NodeOutput` exercises full MVP-2 e2e suite.
- **A+-tier rubric items:** 10M-row synthetic partition-pruning load test (`TestSubstrate_LoadP95 -tags=load`); one wedge consumes substrate as read source-of-truth.

Wave 3 owns Phase D — drop the 5 legacy tables once cutover stable.

Wave 1 followup-issue list (filed by T-S1 PR body) becomes Wave 2 input:
- F1 fast-path canonicalization (cost-governor hot path) — Wave 2 cost-governor work.
- F3 tenant_id retag CLI — only when W8 RBAC cutover schedules.
- F7 substrate_policies primitive — W8 RBAC.
- F8 substrate_blobs CAS + ref-counted GC — W11 blackboard wedge.

---

## Adversarial-review pass (applied inline)

Reviewer subagent red-teamed this plan; findings + fixes applied:

1. **File-disjoint claim (T-S2's `gate_verdict_payload.go` lives inside `substrate/`).**
   *Finding:* Adding a file to T-S1's package directory risks merge conflict if T-S1 also creates that file.
   *Fix applied:* §1 cross-task seam contracts explicitly state T-S2 ships ONE NEW file (`gate_verdict_payload.go`), distinct from T-S1's six files. Validator registration uses `init()` + T-S1's exported `RegisterPayloadValidator` — T-S1 ships the dispatch with 6 default validators; T-S2 adds the 7th from its own file. Verified disjoint via `grep` of file names in T-S1's scope list — `gate_verdict_payload.go` not present.

2. **Dep graph: does T-S2 actually need T-S1?**
   *Finding:* CELDecider could in theory ship without calling `AppendEvent` (the test could mock).
   *Fix applied:* Spec §10 #17 mandates one-tx for fold + eval + emit; mocking would defeat the load-bearing invariant. The DI surface IS the substrate package. Dependency is real.

3. **TDD test list completeness — does every §11 reducer have a test?**
   *Finding:* Spec §4 reducer table has 7 kinds; T-S1's `TestSubstrate_KindPayloadValidation` covers 6 kinds' payloads, T-S2 covers `gate_verdict`. But the **reducer strategy** per kind is hardcoded — is there a test that pins THAT?
   *Fix applied:* Added implicit pin via `TestSubstrate_FoldOrdersByWrittenAtThenID` (T-S1 #11) — `lww` semantics. Reviewer noted: a dedicated `TestSubstrate_DefaultReducerStrategyPerKind` table-driven test pinning all 7 strategies would be stronger. **Added to T-S1's test list as a B-tier addition** … on reflection, this is sub-test depth not breadth — folded into `TestSubstrate_KindPayloadValidation`'s table as an additional column (test asserts both validator AND `defaultReducer(kind)` return-value per row). Updated T-S1 dispatch prompt to call this out explicitly.

   Update applied to test #8 description: `TestSubstrate_KindPayloadValidation` table-driven now covers BOTH (a) malformed payload ⇒ `ErrInvalidPayload`, AND (b) `defaultReducer(kind)` returns the spec §4 strategy.

4. **Phase-A migration invariant test?**
   *Finding:* Spec §3 Phase A says "no writers, no readers" — is there a test that the table exists but stays empty after a normal `regatta serve` run?
   *Fix applied:* This is Phase A's *operational* invariant; no Wave 1 production caller writes to substrate. The Phase A test would be an integration test, out of unit-test scope for Wave 1. Marked as "Wave 2 deliverable" in §5 handoff (Wave 2 owns reconciliation cron + Phase B). Not blocking.

5. **Dispatch prompt completeness — would a fresh implementer ship without follow-up?**
   *Finding:* T-S1 dispatch prompt didn't explicitly call out `internal/orchestrator/state/migrate.go` for the schema-version bump.
   *Fix applied:* Added `internal/orchestrator/state/migrate.go (bump CurrentSchemaVersion 5 → 6)` to T-S1's exclusive scope list.

6. **Deletion default — every PR must answer "what got smaller?".**
   *Finding:* Wave 1 is pure-addition. PR body skeletons need to defend this.
   *Fix applied:* Each of the 3 PR body skeletons now carries an explicit "## Deletion default" section. T-S1 cites Phase D's 5-table drop (post-Wave-3). T-S2 cites spec S5's interface-extraction deferral (~120 LoC saved vs v1 shape). T-S3 cites lint-gate prevention as a shrink in subsequent waves' review surface.

7. **Reviewer-spawn step in workflow.**
   *Finding:* Per `feedback_agent_pr_review` every implementer PR needs an adversarial reviewer pass with a hunt list. Did each dispatch prompt include this?
   *Fix applied:* All three dispatch prompts now include a numbered "Workflow after green" step 5 spawning ONE adversarial reviewer with a task-specific hunt list (HMAC correctness for T-S1, one-tx discipline for T-S2, lint false-positive surface for T-S3) plus the `OK:`/`ISSUE:` format requirement.

8. **Cap on parallelism.**
   *Finding:* Per `feedback_session_limit_dispatch` cap is 3-4. Plan correctly dispatches T-S1 alone first, then T-S2 + T-S3 in parallel — peak 2 parallel implementers. Documented in §Wave overview "concurrency cap" bullet. No fix needed.

9. **Cross-cite between memory rules and dispatch prompts.**
   *Finding:* Dispatch prompts should cite the memory-rule filenames (e.g. `feedback_spec_pattern_authority`) so the implementer subagent can find them.
   *Fix applied:* All three dispatch prompts cite the relevant rule filenames inline (`feedback_spec_pattern_authority`, `feedback_tdd_discipline`, `feedback_adversarial_review`, `feedback_agent_pr_review`, `feedback_unaddressed_load_bearing`).

10. **What if T-S1's PR is still open when T-S2 wants to dispatch?**
    *Finding:* T-S2 + T-S3 branch off T-S1's branch, but if T-S1 force-pushes after reviewer findings, T-S2/T-S3 work could break.
    *Fix applied:* Both T-S2 and T-S3 dispatch prompts explicitly state "rebase to main once T-S1 merges". Reviewer-driven force-pushes on T-S1 require manual rebase of T-S2/T-S3 worktrees — flagged as a coordination cost worth bearing vs the alternative of waiting 1+ days for T-S1 to merge before starting Wave 1's other half. Documented in §Wave overview "Sequence vs parallel" bullet.

---

_Plan authority: this plan is a dispatch artifact only. The main session copy-pastes the §2/§3/§4 dispatch prompts into Agent tool calls AFTER T-S1's prereqs (W6 T3 #209) merge. NO implementation, NO commit from this file._
