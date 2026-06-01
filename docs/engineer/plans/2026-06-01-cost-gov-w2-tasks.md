# Cost Governor (P8) Wave 2 — Implementer Task Breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-cost-governor-design.md`.
Authority: `feedback_spec_pattern_authority` — implementer deviation from any spec-mandated pattern (T3 owns the typed payload structs + `RecordCall` + the validate-dispatch addition per spec §3.5 + §8 inter-task seam; T4 reconciler tick + Cost-API-preferred + Usage-API fallback + LWW reducer semantics per spec §3.4 + §3.5; one-line spawner edit ≤ 6 lines per spec §8 seam contract; Cost API endpoint + bucket window per spec §3.4 lines 199-218; backoff + 429 retry-after honour per spec §3.4 + R3) MUST re-spawn the design subagent. NO implementer-chosen alternatives.

Design priority for every decision below (`feedback_decision_priority`): **UX → ease of use → best practices → execution speed → velocity**. Grade rubric (`feedback_grade_rubric`) inherited verbatim from spec §7 — each Wave 2 task carries the spec's B / A / A+ tool-checkable criteria.

---

## Wave overview

- **2 file-disjoint implementer tasks** (T3, T4) per spec §10 Wave 2. Both dispatch in **parallel** from main per `feedback_parallel_dispatch`. T3 owns the `TokenSpendPayload` + `BudgetReconciledPayload` + `CallRecord` + `RecordCall` types (shared-primitive-owner per `feedback_shared_primitive_owner`). T4 imports `BudgetReconciledPayload` from T3 — contract-locked at spec §3.5 lines 278-289, so T4 can compile against a thin stub of `spend.BudgetReconciledPayload` until T3 lands, OR T3 lands first and T4 rebases off `main`. Per `feedback_sequence_dependent_work`: T3 + T4 file-disjoint AND the cross-import seam (payload struct) is contract-frozen at spec time, so PARALLEL is safe.
- **Prereqs (merged to main):**
  - W6 T4 #213 (GenAI parser opens `llm_call` span on stream-json `result` event) — MERGED.
  - Cost-governor Wave 1 T1 (Gate + Reader + Config + Scheduler hook) — IN FLIGHT (PR pending). T3 and T4 dispatch only after T1 merges so that `spend.Reader` + `spend.ScopeKey` + `gate.Config` are available on main. **Wave-2 dispatch is gated on T1 merge** — main session waits.
  - Cost-governor Wave 1 T2 (Estimator + Pricing) — IN FLIGHT (PR #246 MERGED). `pricing.Lookup` + `pricing.ErrPricingMissing` available on main now.
  - Substrate v2 Wave 1 — T-S1 #224 (event log + 0006 migration + HMAC sign + `RegisterPayloadValidator` open-extension hook) MERGED; T-S2 (CELDecider + gate_verdict) + T-S3 (lint + property tests) assumed merged before this wave dispatches.
- **Sequence vs parallel:** T3 and T4 dispatch simultaneously off `main` after T1 merges. T4 imports `spend.BudgetReconciledPayload` (T3-owned) — contract-locked at spec §3.5 lines 278-289. T4 either (a) rebases off T3's branch via local merge **once T3 opens its PR with the payload file committed first** (T3 commits payload.go in the first commit of its PR; T4 can `git fetch origin/feat/cost-gov-t3-spawner-emit -- internal/cost/spend/payload.go` once T3 pushes) OR (b) defines a local copy of `BudgetReconciledPayload` in `_test.go` for compile-time only, deletes it after T3 lands. Option (a) preferred per `feedback_shared_primitive_owner`.
- **W6 T4 amendment prereq (closes I5 + tracking issue #227):** verified at plan time — `internal/orchestrator/spawner/genai.go::ParseStream(ctx, tracer, r)` does NOT receive a `*sql.Tx` at the call site, and `ClaudeSpawnerConfig` has no DB/store reference. **T3 MUST open a tiny W6 T4 amendment PR FIRST** (call-site refactor: thread an optional `OnResultEvent func(ctx, *streamEvent) error` callback through `ClaudeSpawnerConfig` → `Spawn` → `ParseStream` so the spawner caller can wire a callback that opens a substrate tx, calls `spend.RecordCall`, commits). The amendment is mechanical (no behaviour change when callback is nil) and reviewed alongside T3's primary PR. Issue #227 closes on amendment-PR merge. **Amendment PR is the first of T3's two-PR sequence.**
- **Migration phasing (`feedback_migration_number_lock`):** **NO new migration in Wave 2.** Cost-governor data plane lives entirely on existing substrate `events` rows under `kind='token_spend'` + `kind='budget_reconciled'` (both kinds already registered + reduced + signed by substrate v2 Wave 1 #224). T3 adds dispatch-table entries to substrate's `validate.go` via the `RegisterPayloadValidator` open-extension hook — still migration-free. Migration #0007 remains reserved for a future spec.
- **Concurrency cap (`feedback_session_limit_dispatch`):** 2 parallel implementers (T3, T4). Well under the 3-4 ceiling; no risk of session-limit cascade. The W6 T4 amendment-PR is a sequential prefix to T3 (T3-impl-3 spawns both PRs sequentially), not a parallel third subagent.
- **Deletion default (`feedback_deletion_default`):**
  - **T3:** the spawner-side write path eliminates the need for an "outbox table" that would otherwise track unpriced calls — the post-stream callback runs INSIDE the spawner's lifecycle, so substrate is the only place the row exists. Net: no outbox primitive added, no second reducer entry, no replay daemon. (Plus the per-PR `## Deletion default` section answers "what got smaller?" in PR body — for T3 it's the I5 amendment retiring the "ParseStream callback OR tx" indecision in favour of a single seam.)
  - **T4:** Cost API path (preferred per spec §3.4) eliminates the R-A4 "pricing-applied-twice" defect entirely when the Cost API is reachable — pricing-table comparison only fires on the fallback Usage-API path. Net: drift signal is unambiguous on the happy path; no need for a separate "pricing-table-correctness alarm" (which would otherwise be a third event kind).
- **Followup filing (`feedback_followup_filing_universal` + `feedback_unaddressed_load_bearing`):** every load-bearing named-but-deferred item in spec §2 (OOS) + §9 (R-tier mitigations deferred) is filed as a `[cost-governor-followup]` issue PRE-MERGE; PR body cites the issue numbers. A7 rubric requires ≥ 13 issues total across all five cost-gov PRs (Wave 1 + Wave 2 + Wave 3). T3/T4 file the deltas not already filed by T1/T2.

---

## §1 File-disjoint table

| Task | Path (exclusive write scope) | Depends-on (Wave 2 + main) | Effort | TDD tests (count: named) |
| ---- | --------------------------- | -------------------------- | ------ | ------------------------ |
| **T3 amendment PR (first of two)** | `internal/orchestrator/spawner/claude.go` (add `OnResultEvent` field to `ClaudeSpawnerConfig` + wire callback in `Spawn` goroutine; ≤ 20 LoC net delta); `internal/orchestrator/spawner/genai.go` (extend `ParseStream` signature: `ParseStream(ctx, tracer, r, onResult)`; nil callback = no-op; ≤ 15 LoC net delta — preserves W6 spec §3.4 attribute set + §3.5 child-of-operator_invocation invariant); `internal/orchestrator/spawner/genai_test.go` + `claude_genai_test.go` (callback-nil regression + callback-fires-on-result-event); update `internal/obs/otel/e2e_test.go` callsite to pass `nil` callback (one-line) | W6 T4 #213 merged; Wave 1 T1 merged | XS (mechanical) | 3 named (2 callback semantics + 1 backwards-compat). Closes #227. |
| **T3 primary PR** | `internal/cost/spend/payload.go` (NEW: `TokenSpendPayload` + `BudgetReconciledPayload` + `ModelBreakdownRow` + `CallRecord` typed structs per spec §3.5 lines 264-289); `internal/cost/spend/writer.go` (NEW: `RecordCall(ctx, tx, r CallRecord) error` per spec §3.5 lines 326-333 + §9 R12 nonce derivation `sha256(CallID‖retry_seq)[:16]`); `internal/cost/spend/writer_test.go` + `payload_test.go`; `internal/orchestrator/state/substrate/validate.go` (one-block `init()` addition that calls `RegisterPayloadValidator(KindTokenSpend, …)` + `RegisterPayloadValidator(KindBudgetReconciled, …)` — open-extension per T-S1 #224); `internal/orchestrator/spawner/genai.go` (EXACTLY ONE-LINE addition + minimal wiring in the existing `result`-event handler; primary diff ≤ 6 lines per spec §8 seam contract); `internal/orchestrator/spawner/genai_cost_test.go` (NEW) | T3-amendment merged; Wave 1 T1 merged; substrate v2 Wave 1 (#224 merged) | M | 7 named (B 4, A 2, A+ 1). Spec §6 T3 + §7 B/A/A+. |
| **T4** | `internal/cost/reconcile/tick.go` (NEW: `Reconciler` struct + `Tick(ctx) error` + cron loop + alert-dedup state per §3.4 + A4 rubric); `internal/cost/reconcile/client.go` (NEW: Anthropic Cost API + Usage API HTTP client with `User-Agent: regatta/<buildinfo.Version>` + `anthropic-version: 2023-06-01` + `x-api-key` from configurable env var per spec §3.4 lines 208-211); `internal/cost/reconcile/window.go` (NEW: top-of-hour + 2min jitter window computation per spec §3.4 line 225); `internal/cost/reconcile/backoff.go` (NEW: exponential `1s × 2^n` capped at `5min` + `retry-after` header honour per spec §3.4 + R3 + A3 rubric); `internal/cost/reconcile/{tick,client,window,backoff}_test.go`; `internal/cost/reconcile/testdata/anthropic_cost_*.json` + `anthropic_usage_*.json` (response fixtures) | Wave 1 T1 merged; T3 primary PR merged (or `spend.BudgetReconciledPayload` available via cross-branch pull) | M | 12 named (B 6, A 4, A+ 2). Spec §6 T4 + §7 B/A/A+. |

**Disjointness verification (`grep` at plan time):**
- T3 amendment writes only to `internal/orchestrator/spawner/{claude,genai}.go` + their `_test.go` siblings + the single `e2e_test.go` callsite update.
- T3 primary writes only to `internal/cost/spend/{payload,writer}.go` + their `_test.go` + `internal/orchestrator/state/substrate/validate.go` (additive init block) + `internal/orchestrator/spawner/genai.go` (≤ 6-line callback registration).
- T4 writes only to `internal/cost/reconcile/`.
- T3 primary + T4 share zero files. T3 amendment + T4 share zero files. T3 amendment + T3 primary touch `genai.go` sequentially (amendment lands first; primary rebases).
- **Overlap on `internal/orchestrator/spawner/genai.go`:** T3 amendment changes the signature; T3 primary adds the 6-line callback wiring (which is the entire reason for the amendment). T4 does NOT touch `genai.go`.
- `internal/cost/spend/reader.go` was created in Wave 1 T1 — T3 does NOT touch it (T3 writes only new files `payload.go` + `writer.go` to the same package). T3 + T1 share zero files in flight.

**Cross-task seam contracts (load-bearing — implementer MUST honour exactly):**

- **T3 exports** `spend.TokenSpendPayload`, `spend.BudgetReconciledPayload`, `spend.ModelBreakdownRow`, `spend.CallRecord`, `spend.RecordCall(ctx, tx, r) error`, sentinel `spend.ErrPricingMissing` (re-exported from `pricing` for caller convenience — single-import surface), error wrapping for substrate `ErrReplay`. T4 imports `spend.BudgetReconciledPayload` only. T1 (already merged) reads `TokenSpendPayload` via `json_extract` SQL — no Go-struct import dependency on T3 (closes the symmetric concern).
- **T4 exports** `reconcile.Reconciler`, `reconcile.NewReconciler(cfg Config) *Reconciler`, `reconcile.Config` (DB, Clock, HTTPClient, BucketWidth, ReconcileInterval, DriftAlertThresholdPct, UsageAPIKeyEnv, Tracer, Logger), `reconcile.Tick(ctx) error`, `reconcile.Run(ctx) error` (long-loop), sentinels `reconcile.ErrAdminKeyUnset`, `reconcile.ErrUpstreamPersistent5xx`, `reconcile.ErrUpstreamPersistent429`. Wave 3 T5 (operator doc) imports nothing from `reconcile/` — doc-only.
- **Shared-primitive owner (`feedback_shared_primitive_owner`):** T3 owns every `spend.*Payload` typed struct AND the substrate-validate dispatch entries for both `KindTokenSpend` and `KindBudgetReconciled`. T4 does NOT redefine the payload struct, does NOT call `RegisterPayloadValidator`, does NOT add a parallel validator. T4 writes `BudgetReconciledPayload` rows via `substrate.AppendEvent` with the T3-owned struct's `json.Marshal` output as the payload bytes.
- **T3 primary `genai.go` diff:** EXACTLY ONE-LINE addition in the existing `result`-event handler that wires `spend.RecordCall` via the T3-amendment-PR-added `OnResultEvent` callback. Net diff for genai.go ≤ 6 lines (callback closure + nil-guard + error log/span-attr on failure). NO other change to that file.
- **T3 amendment `genai.go` diff:** extend `ParseStream` signature with an optional `onResult func(ctx context.Context, ev *streamEvent) error` parameter (nil = no-op; passes the parsed `streamEvent` so callers can read `Usage.InputTokens` etc.). Behaviour-preserving — every existing test passes; W6 attribute set unchanged. Net diff ≤ 15 LoC.
- **T3 amendment `claude.go` diff:** add `OnResultEvent ClaudeResultCallback` field to `ClaudeSpawnerConfig` + new public type `ClaudeResultCallback = func(ctx context.Context, ev *streamEvent) error`. Wire the field into the existing `ParseStream(spanCtx, s.cfg.Tracer, pr)` callsite as the fourth argument. Behaviour-preserving when nil (current default). Net diff ≤ 20 LoC.
- **T4 → T3 cross-import:** ONLY `spend.BudgetReconciledPayload` + `spend.ModelBreakdownRow` (struct types). NO function calls into T3's package from T4 (the writer is spawner-side; the reconciler emits events via `substrate.AppendEvent` directly).
- **substrate-validate dispatch ownership:** T3's `validate.go` addition lives in `internal/orchestrator/state/substrate/validate.go` per spec §8 row 3. The addition is an `init()` block that registers both validators against the T-S1 #224 `RegisterPayloadValidator` API. Implementer-test: `TestSubstrate_TokenSpendPayloadValidates` + `TestSubstrate_BudgetReconciledPayloadValidates` (added under T3 scope; co-located in `internal/cost/spend/` to keep the substrate package's test file alone).

---

## §2 Task T3 — Spawner post-stream `token_spend` emission (two-PR sequence: amendment → primary)

### Scope

**T3 amendment PR (first; closes #227):**
- **`internal/orchestrator/spawner/genai.go`** — extend `ParseStream` signature:
  ```go
  func ParseStream(
      ctx context.Context,
      tracer trace.Tracer,
      r io.Reader,
      onResult func(ctx context.Context, ev *streamEvent) error, // NEW
  ) error
  ```
  - Invoke `onResult(ctx, &ev)` inside the existing `case ev.Type == "result":` branch, AFTER `finalizeResult(span, ev)` sets the W6 GenAI usage attrs, BEFORE `span.End()`. If `onResult` returns non-nil error, the span MUST be set to `codes.Error` + `error.type=record_call_failed` (per spec §9 R4 — open-span-as-smoke-alarm contract) BEFORE `span.End()`. If `onResult == nil`, no-op (preserves backwards compat). Net diff ≤ 15 LoC.
- **`internal/orchestrator/spawner/claude.go`** — add `OnResultEvent ClaudeResultCallback` field to `ClaudeSpawnerConfig` (type `ClaudeResultCallback = func(ctx context.Context, ev *streamEvent) error` defined alongside in the same file). Update the goroutine at line 119-121 to pass `s.cfg.OnResultEvent` as the fourth `ParseStream` arg. Net diff ≤ 20 LoC.
- **`internal/orchestrator/spawner/genai_test.go`** + **`internal/orchestrator/spawner/claude_genai_test.go`** — add three tests (see test list below).
- **`internal/obs/otel/e2e_test.go`** — update the single `ParseStream(opCtx, tracer, fixture)` callsite at line 142 to pass `nil` for the new callback arg. One-line surgical change.

**T3 primary PR (second; merges after amendment):**
- **`internal/cost/spend/payload.go`** — NEW. Typed payload structs per spec §3.5 lines 263-289 verbatim:
  - `TokenSpendPayload` — 10 fields (`USD`, `Model`, `InputTokens`, `OutputTokens`, `CacheReadTokens`, `CacheCreationTokens`, `OperatorID`, `DAGID`, `WorkItemID`, `PricingRev`, `CallID`). JSON tags verbatim from spec.
  - `BudgetReconciledPayload` — 8 fields (`PeriodStart`, `PeriodEnd`, `ActualUSD`, `RecordedUSD`, `DeltaUSD`, `DriftPct`, `ModelBreakdown []ModelBreakdownRow`, `APIResponseSig`).
  - `ModelBreakdownRow` — `Model string`, `InputTokens int64`, `OutputTokens int64`, `CacheReadTokens int64`, `CacheCreationTokens int64`, `USD float64` (per spec §3.5 + canonical Anthropic group_by=model row shape).
  - `CallRecord` — input struct to `RecordCall`. Fields: `CallID`, `RetrySeq int`, `Model`, `InputTokens`, `OutputTokens`, `CacheReadTokens`, `CacheCreationTokens`, `OperatorID`, `DAGID`, `WorkItemID`, `TenantID`, `WrittenBy string`, `RunID string` (so the writer can pass them to substrate's `UNIQUE(run_id, written_by, nonce)`).
- **`internal/cost/spend/writer.go`** — NEW. `RecordCall(ctx context.Context, tx *sql.Tx, r CallRecord) error`:
  1. Look up `pricing.Lookup(r.Model)` → `pricing.Row`. If `pricing.ErrPricingMissing`, return wrapped error AND set the **span attribute** `regatta.cost.error=pricing_missing` on the **current span** read from `trace.SpanFromContext(ctx)`. DO NOT write a substrate row when pricing is missing (closes spec §6 T3 `TestRecordCall_PricingMissingErrorsHard`).
  2. Compute `usd = r.InputTokens * row.InputUSDPer1k/1000 + r.OutputTokens * row.OutputUSDPer1k/1000 + r.CacheReadTokens * row.CacheReadUSDPer1k/1000 + r.CacheCreationTokens * row.CacheWriteUSDPer1k/1000`.
  3. Build `TokenSpendPayload{...}` with all 10 fields populated.
  4. Derive `nonce = sha256(CallID || "|" || strconv.Itoa(RetrySeq))[:16]` per spec §9 R12.
  5. Call `substrate.AppendEvent(ctx, tx, substrate.AppendInput{Kind: substrate.KindTokenSpend, TenantID: r.TenantID, RunID: r.RunID, WrittenBy: r.WrittenBy, Nonce: nonce, Payload: json.Marshal(payload)})` per T-S1 #224 export shape.
  6. On `substrate.ErrReplay` (UNIQUE collision), return wrapped `ErrReplay` — caller is supposed to handle (idempotent retry path; closes `TestRecordCall_IdempotentOnReplay`).
- **`internal/cost/spend/writer_test.go`** + **`internal/cost/spend/payload_test.go`** — see test list below.
- **`internal/orchestrator/state/substrate/validate.go`** — additive `init()` block that registers the two T3-owned payload validators via T-S1 #224's `RegisterPayloadValidator(kind, fn)` API. Net diff ≤ 30 LoC (an `init()` + two struct-validation closures that unmarshal the payload, check required fields, return `ErrInvalidPayload` on missing fields). NO change to existing validators / DDL / enum.
- **`internal/orchestrator/spawner/genai.go`** — EXACTLY ONE-LINE wiring addition inside the amendment-PR-added `onResult` callback's call-site… **wait**, the callback is REGISTERED in `claude.go` (via `cfg.OnResultEvent`). The 1-line genai.go primary diff is the BODY of the registered callback closure. Per spec §8 line 641: "T3 modifies `internal/orchestrator/spawner/genai.go` by adding EXACTLY ONE statement inside the `result`-event handler". After the amendment lands, the handler already has `if onResult != nil { onResult(ctx, &ev) }` — T3 primary does NOT need to modify genai.go AT ALL. **Revised spec interpretation:** the ≤ 6-line genai.go diff is actually the amendment-PR diff. T3 primary's genai.go diff is **zero lines** in `genai.go` — the wiring lives in `claude.go` where the callback is constructed. _Implementer flag this if reading and the spec interpretation differs — STOP and re-spawn design subagent._
- **`internal/orchestrator/spawner/claude.go`** — T3 primary touches this file IF AND ONLY IF the production caller of `NewClaudeSpawner` needs the `OnResultEvent` callback wired. Per spec §3.5 line 336: "invoked from `internal/orchestrator/spawner/genai.go` at the `result` event close" — but the actual production wiring point is the caller that constructs `ClaudeSpawnerConfig{OnResultEvent: func(ctx, ev) error { return spend.RecordCall(...) }}`. **The wiring callsite lives in `internal/orchestrator/orchestrator.go` (or wherever `NewClaudeSpawner` is constructed in production code).** Implementer to grep for callers; the wiring callsite is part of T3 primary scope (≤ 10 LoC delta).
- **`internal/orchestrator/spawner/genai_cost_test.go`** — NEW colocated integration test that exercises the full callback path: stream-json fixture → `OnResultEvent` fires → `RecordCall` writes a substrate row → assert row exists with correct payload. See test list below.

### Prereqs (cite spec sections)

- Spec §2 in-scope items #3 (substrate hook), #6 (OTel attrs).
- Spec §3.4 — reconciler reads `kind='token_spend'` rows; T3 writes them with field shape the reconciler expects.
- Spec §3.5 — TokenSpendPayload + BudgetReconciledPayload + Reader/Writer signatures **verbatim**.
- Spec §3.7 — OTel attribute names (T3 sets `regatta.cost.error` on write failure path per R4).
- Spec §6 T3 — exhaustive named-test list (7 tests transcribed below).
- Spec §7 B/A/A+ — applies to T3 (B3 = stream-json fixture produces substrate row; A2 = payload signing inherited from substrate v2; A+ implicit via property test).
- Spec §8 — file-disjoint table row 3 (T3 scope + seams + one-line genai.go constraint).
- Spec §8 line 640 — W6 T4 tx-export amendment prereq (closes I5; tracking issue #227).
- Spec §9 R4 — write-skew (RecordCall inside parser tx; open span = smoke alarm).
- Spec §9 R12 — idempotency key collision; nonce derivation `sha256(CallID‖retry_seq)[:16]`.
- Spec §9 R13 — spawner crashes mid-call ⇒ reconciler catches drift (T3 does NOT need a runtime fix here; tracking issue cited).

### Existing patterns to reuse (do NOT reinvent)

- **Substrate AppendEvent:** `internal/orchestrator/state/substrate/event.go::AppendEvent(ctx, tx, AppendInput)` — exported by T-S1 #224. Use this verbatim; do NOT write directly to the `events` table.
- **RegisterPayloadValidator:** T-S1 #224's open-extension hook. Pattern: `func init() { substrate.RegisterPayloadValidator(substrate.KindTokenSpend, validateTokenSpend) }`. Closure receives `payload []byte`, returns error if malformed.
- **Pricing import:** `pricing.Lookup(model string) (pricing.Row, error)` + sentinel `pricing.ErrPricingMissing`. Owned by T2 (#246 merged). Use the existing module-level singleton; do NOT instantiate a new pricing instance.
- **W6 parser pattern (genai.go):** existing `case ev.Type == "result":` handler at line 124 + `finalizeResult` helper. T3 amendment adds the `onResult` callback invocation AFTER `finalizeResult(span, ev)` so the W6 attribute set is already on the span when the callback runs (closes R4 contract — open span on failure has the GenAI attrs visible for the operator).
- **streamEvent struct:** exported (uppercase fields) for callback consumers — implementer to confirm; if currently unexported, the amendment PR exports it (or exports a minimal `StreamResultEvent` projection struct that contains only the fields callbacks need: `MessageID`, `Model`, `Usage` substructs, `StopReason`, `IsError`).
- **trace.SpanFromContext:** standard otel pattern for retrieving the current span to set `regatta.cost.error` attr on write failure. Mirrors existing W6 error-attr pattern in `genai.go` line 146 + 176 (`rotel.ErrorTypeAttr`).
- **No new error sentinel naming convention** — reuse existing `errors.Is` chains; wrap substrate `ErrReplay` via `fmt.Errorf("spend: replay: %w", err)`.

### TDD test list (named tests from spec §6 T3 + amendment-PR additions; failing-output capture step required)

Per `feedback_tdd_discipline`: implementer writes each test first, runs `go test ./<pkg>/ -run <name> -v`, **captures failing output (paste into PR body)**, then implements. "Tests would have failed" is NOT acceptable.

**T3 amendment PR (3 named tests):**

1. `TestParseStream_NilOnResult_NoCallback_BackwardsCompat` — call `ParseStream(ctx, tracer, fixture, nil)` over the W6 success fixture; no callback invoked; existing W6 span attrs unchanged. Pins backwards-compat invariant.
2. `TestParseStream_OnResultFiresExactlyOncePerResultEvent` — pass a non-nil callback that increments a counter; feed a fixture with one `result` event → counter==1.
3. `TestParseStream_OnResultErrorMarksSpanError` — callback returns `errors.New("synthetic")`; span status is `codes.Error` with `error.type=record_call_failed` attr. Pins R4 smoke-alarm contract.

**T3 primary PR (7 named tests; spec §6 T3 verbatim):**

1. `TestRecordCall_AppendsTokenSpendEvent` — invoke `RecordCall` against a substrate test DB → one row exists with `kind='token_spend'`, payload matches `CallRecord` verbatim.
2. `TestRecordCall_PricingMissingErrorsHard` — `RecordCall` with unknown model → returns `ErrPricingMissing`; NO substrate row written; span attribute `regatta.cost.error=pricing_missing` set on `llm_call` span (caller-provided via ctx).
3. `TestRecordCall_PayloadIncludesAllFields` — payload_json has all 10 `TokenSpendPayload` fields populated.
4. `TestRecordCall_IdempotentOnReplay` — same `CallID` twice → second insert returns substrate `ErrReplay` (UNIQUE nonce collision); single row exists. Pins replay-safety (spec §9 R12).
5. `TestGenAIParser_InvokesRecordCallOnResultEvent` — feed stream-json fixture; assert `RecordCall` was called exactly once per `result` event with parser-derived fields. (Integration test in `genai_cost_test.go`; wires real callback through amendment seam.)
6. `TestGenAIParser_NoRecordCallWhenStreamJsonOff` — parser disabled (legacy plain stdout, no JSON) → no `RecordCall`, no substrate row. Pins legacy-flag-off invariant (mirrors W6 T4 test).
7. `TestSubstrate_TokenSpendPayloadValidates` — substrate validate dispatch table accepts well-formed payload, rejects malformed (missing required field → `ErrInvalidPayload`).

**Adversarial-reviewer-added (1 named test, T3 primary; per `feedback_adversarial_review`):**

8. `TestRecordCall_OneWrittenByPerCallID` — concurrent goroutines call `RecordCall` with same `CallID` but different `WrittenBy` values → assertion: this is structurally prevented by the scheduler-lane invariant (per spec §9 R12 concurrent-writer note). Test asserts the godoc comment on `RecordCall` documents the invariant + the writer logs a WARN if it ever observes two `written_by` values for one CallID across substrate (single-SELECT query in the test). Pins R12 concurrent-writer audit.

Total T3: **10 named tests** (3 amendment + 7 primary). Plus 1 reviewer-added (8 above) = **11**. PR bodies (amendment + primary) list every test name + pasted failing-output excerpt for AT LEAST 6 representative cases per PR.

### PR body skeleton — T3 amendment PR

````
## Summary

Cost-governor Wave 2 prep: extend spawner `ParseStream` + `ClaudeSpawnerConfig`
with an optional post-result callback so cost-gov Wave 2 T3 primary PR can
emit substrate `token_spend` events INSIDE the parser's span-close path
per spec §3.5 line 336. Closes #227.

- internal/orchestrator/spawner/genai.go — ParseStream signature gains
  `onResult func(ctx, *streamEvent) error` (nil = no-op). Invoked after
  finalizeResult, before span.End. Callback error ⇒ span status Error +
  error.type=record_call_failed per spec §9 R4 smoke-alarm contract.
- internal/orchestrator/spawner/claude.go — ClaudeSpawnerConfig gains
  `OnResultEvent ClaudeResultCallback` field. Goroutine at Spawn passes
  s.cfg.OnResultEvent as the fourth ParseStream arg.
- internal/obs/otel/e2e_test.go — single callsite updated to pass nil.

Behaviour-preserving: every existing genai_test.go + claude_genai_test.go
case unchanged. Backwards-compat asserted by TestParseStream_NilOnResult.

## Why

Per spec §8 line 640 (closes I5): T3's substrate write must run INSIDE
the W6 parser's transaction so reconciliation reads see usage row + span
atomically. The current ParseStream signature has no tx hook and
ClaudeSpawnerConfig has no DB reference — the callback seam is the
minimal mechanical refactor that exposes the result event without
adopting a tx-passing API (which would force every ParseStream caller,
including the e2e tests, to thread a DB). Callback is strictly cleaner
than tx-passing for this seam.

## Test plan

- [x] TestParseStream_NilOnResult_NoCallback_BackwardsCompat
- [x] TestParseStream_OnResultFiresExactlyOncePerResultEvent
- [x] TestParseStream_OnResultErrorMarksSpanError
- [x] All existing genai_test.go + claude_genai_test.go tests pass unchanged.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Deletion default

The callback seam ELIMINATES the indecision around "ParseStream takes a
*sql.Tx" vs "ParseStream returns a list of result events the caller
processes". Single seam, single invariant (nil = no-op), zero
behavioural drift for non-cost-gov callers. Per spec §8 line 640 the
amendment is "mechanical (no behaviour change)".

## Followup issues filed

Closes #227.

```release-notes
none
```
````

### PR body skeleton — T3 primary PR

````
## Summary

Cost-governor Wave 2 T3 ships the spawner-side `token_spend` emission +
substrate validate-dispatch addition per
docs/engineer/specs/2026-06-01-cost-governor-design.md §3.5.

- internal/cost/spend/payload.go — TokenSpendPayload (10 fields) +
  BudgetReconciledPayload (8 fields) + ModelBreakdownRow + CallRecord
  typed structs. Shared-primitive owner per
  feedback_shared_primitive_owner (T4 imports BudgetReconciledPayload).
- internal/cost/spend/writer.go — RecordCall(ctx, tx, r) writes one
  substrate row per LLM call. Pricing lookup hard-errors on missing
  model (spec §6 T3 / R4). Nonce derivation
  sha256(CallID‖retry_seq)[:16] per R12. Idempotent on replay via
  substrate UNIQUE(run_id, written_by, nonce).
- internal/orchestrator/state/substrate/validate.go — additive init()
  block registering TokenSpend + BudgetReconciled payload validators
  via T-S1 #224's RegisterPayloadValidator open-extension hook.
- internal/orchestrator/spawner/{genai,claude}.go — wiring: the
  callback registered in T3-amendment-PR-#NNN now invokes
  spend.RecordCall inside the result-event path. genai.go diff is
  ZERO net lines (callback hook already exists from amendment). The
  callback wiring point in claude.go production caller adds ≤ 10 LoC.

## Why

Per spec §3.5 + §3.4: the reconciler reads `kind='token_spend'` rows
to compute `recorded_usd`; T3 is the producer. RecordCall lives
inside the parser's span-close path (callback seam from amendment PR
#NNN) so a failed write leaves the llm_call span OPEN — the operator
smoke alarm per R4. Reconciliation catches every other drift mode.

## Test plan

- [x] TestRecordCall_AppendsTokenSpendEvent
- [x] TestRecordCall_PricingMissingErrorsHard
- [x] TestRecordCall_PayloadIncludesAllFields
- [x] TestRecordCall_IdempotentOnReplay (pins R12)
- [x] TestGenAIParser_InvokesRecordCallOnResultEvent (integration)
- [x] TestGenAIParser_NoRecordCallWhenStreamJsonOff
- [x] TestSubstrate_TokenSpendPayloadValidates
- [x] TestRecordCall_OneWrittenByPerCallID (reviewer-added; pins R12 audit)
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 6 reps>

## Deletion default

T3 ELIMINATES the need for a separate outbox table: the spawner-side
callback runs inside the spawner lifecycle, so substrate is the only
place a token_spend row exists. No second reducer, no replay daemon,
no cross-process queue. Plus the I5 amendment retires the "tx-passing
vs callback" indecision (closes #227).

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [cost-governor-followup] regatta cost backfill <run_id> CLI (#NNN; spec §9 R4 + R6)
- [cost-governor-followup] spawner reconciliation outbox on SIGKILL (#NNN; spec §9 R13)
- [cost-governor-followup] gen_ai.response.id collision audit (#NNN; spec §9 R12)

```release-notes
[FEATURE] cost-governor token_spend event emission from spawner stream-json (default-off when ClaudeSpawnerConfig.OnResultEvent unset; MVP-2 byte-equal)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-cost-gov-t3. You are responsible for a
TWO-PR SEQUENCE:

  PR-A (amendment): branch feat/cost-gov-t3-spawner-callback off main.
                    Closes #227.
  PR-B (primary):   branch feat/cost-gov-t3-spawner-emit off PR-A's
                    merge commit (NOT off main — must rebase to
                    inherit PR-A's signature change).

Sequence: open PR-A first; pass review + auto-merge; THEN branch PR-B
off updated main; open PR-B. Do NOT open both in parallel — PR-B
depends on PR-A's ParseStream signature change.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-cost-governor-design.md.
Read ALL of: §2 (scope, in/out), §3.4 (reconciler reads token_spend),
§3.5 (typed payloads + writer + substrate hook + invocation point),
§3.7 (OTel attrs), §6 T3 (named test list), §7 (B/A/A+ rubric), §8
(file-disjoint row 3 + inter-task seam — especially line 640 W6 T4
amendment prereq + line 641 ≤ 6-line genai.go constraint), §9 R4
(write-skew + open-span smoke alarm), R12 (idempotency nonce
derivation), R13 (spawner crash recovery — tracking issue, not in-PR).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (T3 OWNS TokenSpendPayload + BudgetReconciledPayload
+ ModelBreakdownRow + CallRecord + RecordCall; T3 owns the substrate
validate-dispatch addition; one-line spawner edit ≤ 6 lines for the
genai.go primary diff; nonce derivation sha256(CallID‖retry_seq)[:16];
RecordCall hard-errors on missing pricing AND sets regatta.cost.error
span attr AND does not write a substrate row), STOP and report — do
NOT pick an alternative yourself. Re-spawn the design subagent.

# Pre-flight verification (closes spec §8 I5 + tracking issue #227)

Before starting PR-A, run:

  grep -n "func ParseStream" internal/orchestrator/spawner/genai.go
  grep -n "func (s \*ClaudeSpawner) Spawn" internal/orchestrator/spawner/claude.go
  grep -n "ParseStream(" internal/orchestrator/spawner/claude.go
  grep -n "ParseStream(" internal/obs/otel/e2e_test.go

Confirm: ParseStream currently has 3 params (ctx, tracer, r). Confirm:
ClaudeSpawnerConfig has no OnResultEvent / DB / Store field. If either
fails (signature already amended, callback field already present), STOP
and report — the amendment is already landed and you skip to PR-B.

# Scope PR-A (exclusive write paths)

- internal/orchestrator/spawner/genai.go              (extend ParseStream signature; ≤ 15 LoC delta)
- internal/orchestrator/spawner/claude.go             (add OnResultEvent field + wire callsite; ≤ 20 LoC delta)
- internal/orchestrator/spawner/genai_test.go         (add TestParseStream_NilOnResult + TestParseStream_OnResultFires + TestParseStream_OnResultErrorMarksSpanError)
- internal/orchestrator/spawner/claude_genai_test.go  (existing — confirm passes unchanged)
- internal/obs/otel/e2e_test.go                       (update single callsite at line ~142 to pass nil callback)

You MUST NOT touch any other file in PR-A. Specifically:
- Do NOT touch internal/cost/ — that is PR-B's scope.
- Do NOT touch internal/orchestrator/state/substrate/ — that is PR-B's scope.
- Do NOT modify finalizeResult helper's body (only invocation order at the result-event handler).
- Do NOT export the entire streamEvent struct unless required — prefer a minimal projection type `StreamResultEvent` containing only MessageID/Model/Usage/StopReason/IsError if streamEvent is currently unexported and cost-gov only needs a subset.

# Scope PR-B (exclusive write paths)

- internal/cost/spend/payload.go              (NEW; typed structs verbatim from spec §3.5 lines 264-289)
- internal/cost/spend/payload_test.go         (NEW)
- internal/cost/spend/writer.go               (NEW; RecordCall(ctx, tx, r) per spec §3.5 lines 326-333)
- internal/cost/spend/writer_test.go          (NEW)
- internal/orchestrator/state/substrate/validate.go  (additive init() block ONLY — ≤ 30 LoC delta; do NOT modify any existing validator)
- internal/orchestrator/spawner/claude.go     (wire RecordCall as the OnResultEvent callback at the production caller of NewClaudeSpawner; ≤ 10 LoC delta — the caller may live in cmd/regatta/ or internal/orchestrator/orchestrator.go; grep + update the single production wiring point only)
- internal/orchestrator/spawner/genai_cost_test.go  (NEW; integration test)

You MUST NOT touch any other file in PR-B. Specifically:
- Do NOT modify internal/cost/spend/reader.go or scope.go — those are T1's exclusive scope (already merged).
- Do NOT modify internal/cost/gate/, internal/cost/estimate/, internal/cost/pricing/ — those are T1 + T2 scope (already merged).
- Do NOT touch internal/cost/reconcile/ — that is T4's exclusive scope (parallel impl).
- Do NOT touch internal/orchestrator/spawner/genai.go in PR-B (the callback hook was added in PR-A; the callback BODY lives in claude.go's production wiring).

If you discover a missing seam in an out-of-scope file, STOP and report
— file a tracking issue per finding; do NOT edit out of scope.

# Patterns to reuse (do NOT reinvent)

- Substrate AppendEvent: internal/orchestrator/state/substrate/event.go::AppendEvent(ctx, tx, AppendInput) — exported by T-S1 #224.
- RegisterPayloadValidator: T-S1 #224's open-extension hook. Use init() block.
- Pricing lookup: pricing.Lookup(model) + pricing.ErrPricingMissing sentinel. Owned by T2 (#246 merged).
- W6 parser result-event path: internal/orchestrator/spawner/genai.go::ParseStream case "result" at line 124. Amendment-PR inserts callback after finalizeResult, before span.End.
- W6 error-attr pattern: internal/orchestrator/spawner/genai.go line 146 + 176 — span.SetAttributes(rotel.ErrorTypeAttr(typ)). Use this exact pattern when surfacing record_call_failed + pricing_missing.
- trace.SpanFromContext: standard otel for retrieving current span inside RecordCall to set regatta.cost.error attr on write failure.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test below:
  1. Write the test file first.
  2. Run `go test ./<pkg>/ -run <TestName> -v`.
  3. CAPTURE the failing output (paste at least 6 representative samples into PR body's "Failing-test output (TDD capture)" section). "Tests would have failed" is NOT acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or per logical group; squash later if needed).

# Tests to land in PR-A (3 named)

A1. TestParseStream_NilOnResult_NoCallback_BackwardsCompat
A2. TestParseStream_OnResultFiresExactlyOncePerResultEvent
A3. TestParseStream_OnResultErrorMarksSpanError

# Tests to land in PR-B (7 named + 1 reviewer-added = 8)

B1. TestRecordCall_AppendsTokenSpendEvent
B2. TestRecordCall_PricingMissingErrorsHard
B3. TestRecordCall_PayloadIncludesAllFields
B4. TestRecordCall_IdempotentOnReplay
B5. TestGenAIParser_InvokesRecordCallOnResultEvent              (integration; in genai_cost_test.go)
B6. TestGenAIParser_NoRecordCallWhenStreamJsonOff               (integration)
B7. TestSubstrate_TokenSpendPayloadValidates
B8. TestRecordCall_OneWrittenByPerCallID                        (reviewer-added; pins R12 audit)

# Workflow after green (PER PR — run twice)

  1. Run `make pre-push-check` — confirm clean. If any lint / build / test fails, fix in this branch — do NOT skip hooks (--no-verify is banned per feedback_pr_lint_gates).
  2. Re-run `go test ./internal/cost/... ./internal/orchestrator/spawner/... ./internal/orchestrator/state/substrate/... -v` and confirm every named test green.
  3. Run `git diff origin/main -- '*.go' | grep -E '^\+.{0,2}//'` to spot superfluous comments introduced; sweep per feedback_comments_discipline (WHY not WHAT; test-function godocs ≤ 1 line).
  4. Push branch.
  5. (PR-A only) Close issue #227 in PR-A body; cite "Closes #227".
  6. (PR-B only) File the 3 followup tracking issues (cost backfill CLI; spawner reconciliation outbox on SIGKILL; gen_ai.response.id collision audit) and gather issue numbers.
  7. Open PR via `gh pr create --base main --title "<title>" --body-file <path>` (NEVER heredoc per feedback_pr_lint_gates). PR-B body MUST cite the 3 followup issue numbers + the PR-A number.
  8. Spawn ONE adversarial reviewer subagent per PR (per feedback_adversarial_review + feedback_agent_pr_review + feedback_simplify_reviewer) with hunt list (see below).
  9. Apply reviewer findings inline (or file tracking issue + cite in PR body per feedback_unaddressed_load_bearing).
 10. Re-run `make pre-push-check`; force-push.
 11. Verify CI green (pr-lint, check-release-notes, check-tdd, build, test) BEFORE flipping automerge per feedback_review_before_automerge.
 12. Flip automerge ONLY after reviewer cleared the PR.

# Adversarial reviewer hunt list (PR-A)

- ParseStream signature change: every existing caller updated? Run `grep -rn "ParseStream(" --include="*.go"` — every callsite has the new fourth arg. (genai_test.go, claude_genai_test.go, e2e_test.go.)
- Nil-callback path: behaviour-preserving for every existing test. No new branches taken when onResult==nil.
- streamEvent export: minimal projection only if streamEvent itself is unexported. Do NOT widen the export surface unnecessarily.
- Callback invocation order: AFTER finalizeResult sets W6 attrs, BEFORE span.End — so a failed callback leaves the span open with the GenAI attrs visible (R4 contract).
- Error.type attr on callback failure: `error.type=record_call_failed`. Not `record_failed`, not `callback_failed`. Match spec §9 R4 wording exactly.
- Simplification opportunity: could the callback be `func(*streamEvent) error` (sync, no ctx)? No — RecordCall needs ctx for tracing + substrate. Confirm ctx is the spanCtx (post-Start), not the bare incoming ctx.
- No AI signatures anywhere (feedback_no_signatures).
- godocs ≤ 1 line on test funcs (feedback_comments_discipline).

# Adversarial reviewer hunt list (PR-B)

- TokenSpendPayload + BudgetReconciledPayload field shapes EXACT match to spec §3.5 lines 264-289. JSON tags verbatim. No drift.
- RecordCall pricing-missing path: returns error AND sets span attr AND does NOT write substrate row. All three must hold.
- Nonce derivation: sha256(CallID‖retry_seq)[:16] — NOT sha256(CallID alone), NOT random. Match spec §9 R12 exactly.
- substrate.AppendEvent called from INSIDE the caller-provided tx (RecordCall takes *sql.Tx, does not open its own). RecordCall does NOT call tx.Commit() — caller owns commit.
- Open-span-on-failure (R4 smoke alarm): if RecordCall errors, the OnResultEvent callback returns the error, ParseStream sets span Error, span.End is called. Verify the integration test (B5) covers the failure path.
- substrate validate dispatch addition: init() block, no DDL change, no existing-validator change. Uses RegisterPayloadValidator from T-S1 #224 — confirm the import is `substrate.RegisterPayloadValidator`, not a redefined local one.
- TokenSpendPayload validator: rejects payload missing USD / Model / CallID / WorkItemID. Accepts well-formed.
- BudgetReconciledPayload validator: rejects payload missing PeriodStart / PeriodEnd / ActualUSD. Accepts well-formed.
- Production wiring point in claude.go (or wherever the prod ClaudeSpawnerConfig is constructed): the callback is wired ONCE, not per-Spawn — the field is set on the config, not at each Spawn call. Cost-gov module exports a thin constructor like `cost.SpawnerCallback(db *sql.DB, tracer trace.Tracer) ClaudeResultCallback` that returns a closure capturing the DB. Confirm this seam is in cost-gov package, NOT in spawner package (no spawner→cost reverse import).
- Cyclic-import check: cost/spend imports substrate, pricing. substrate does NOT import cost. spawner does NOT import cost. Verify `go list -deps ./internal/cost/spend/` shows no cost→spawner edge.
- Simplification opportunity: could the writer use a single SQL INSERT instead of substrate.AppendEvent? No — substrate handles HMAC signing + the UNIQUE nonce index + the LWW / append reducer semantics. Don't bypass.
- Deletion default: PR body cites concrete shrinkage (outbox-table elimination + I5 amendment closure + #227 closure cited in PR-A body).
- No AI signatures anywhere (feedback_no_signatures).
- godocs ≤ 1 line on test funcs (feedback_comments_discipline).

# Hygiene

- NO AI signatures anywhere (commits, PR body, comments, code) per feedback_no_signatures.
- Comments discipline per feedback_comments_discipline: WHY not WHAT; test-function godocs ≤ 1 line; sweep on every push.
- Doc-check: run `git diff origin/main -- '*.md' '*.go'` and verify no godoc line exceeds 1 line on test funcs BEFORE push.

# Return format

Final report MUST contain:
- PR-A URL.
- PR-B URL (or a NOTE that PR-B is queued behind PR-A merge).
- Pasted failing-test output for at least 6 of the 11 tests (sample is fine — PR bodies carry full set).
- The 3 PR-B followup issue numbers filed.
- Adversarial reviewer verdict per PR (APPROVE or full findings list with severities).
- One-line diff stat per PR: files changed + LoC added/removed.

Begin now. NEVER pause for user input.
```

---

## §3 Task T4 — Reconciler + Anthropic Cost/Usage API client

### Scope

- **`internal/cost/reconcile/tick.go`** — NEW. `Reconciler` struct:
  - Fields: `db *sql.DB`, `clock func() time.Time`, `http *http.Client`, `cfg Config`, `tracer trace.Tracer`, `log *slog.Logger`, `alertDedupe map[string]float64` (key = `period_start|drift_pct@2dp` → last-emitted drift_pct; pins A4 rubric — at most one alert per period across consecutive ticks).
  - `Tick(ctx context.Context) error` — one reconciliation pass: compute window, call Cost API (preferred), fall back to Usage API on 404 / 5xx (with `obs.EventCostReconcileFallback` WARN slog), compute drift, emit `budget_reconciled` substrate row, optionally emit `obs.EventCostDriftAlert` slog (dedup-guarded).
  - `Run(ctx context.Context) error` — long-loop driver: sleeps until next reconcile window per `cfg.ReconcileInterval` + 2min jitter, calls `Tick`, logs ERROR after 5 consecutive failures (per spec §3.4 failure-mode table line 247-248), continues retrying forever (NEVER gives up — per R6).
  - Span emission: each `Tick` opens one `cost.reconcile` span with attrs `regatta.cost.period_start`, `regatta.cost.period_end`, `regatta.cost.drift_pct`, `regatta.cost.api_source=cost|usage_fallback`. NO operator/dag/work_item cardinality on this span (it's a tenant-scoped tick; R14 mitigation).
- **`internal/cost/reconcile/client.go`** — NEW. Anthropic Cost + Usage API HTTP client:
  - `func (c *Client) FetchCost(ctx context.Context, start, end time.Time, bucketWidth time.Duration) (CostResponse, error)` — GET `https://api.anthropic.com/v1/organizations/cost_report/messages?starting_at=...&ending_at=...&bucket_width=1h&group_by[]=model` with headers `anthropic-version: 2023-06-01`, `x-api-key: $<cfg.UsageAPIKeyEnv>`, `User-Agent: regatta/<buildinfo.Version> (https://github.com/maydow/regatta)`.
  - `func (c *Client) FetchUsage(...) (UsageResponse, error)` — same shape, `usage_report` endpoint.
  - Returns typed responses (decoded JSON) + the canonical body bytes (for `APIResponseSig` sha256 computation in tick.go).
  - 404 / 403 on Cost API → return `ErrCostAPIUnavailable` sentinel → caller (Tick) falls back to Usage API.
  - 429 → parse `retry-after` header (per spec §3.4 + R3 + A3); return `ErrRateLimited` wrapping the retry-after duration.
  - 5xx → return `ErrUpstreamFailure` for backoff handling.
  - Network error → wrap in `ErrUpstreamFailure`.
- **`internal/cost/reconcile/window.go`** — NEW. Window computation:
  - `func WindowForTick(now time.Time, bucketWidth time.Duration) (start, end time.Time)` — for a tick at `now`, computes the just-closed `bucketWidth`-aligned window. E.g. tick at `01:02` with bucketWidth=1h returns `start=00:00, end=01:00`. Pins spec §3.4 line 225: "top-of-hour + 2min jitter; fetches the just-closed hour's bucket".
  - `func NextTickTime(now time.Time, interval, jitter time.Duration) time.Time` — next aligned tick boundary.
- **`internal/cost/reconcile/backoff.go`** — NEW. Backoff state machine:
  - `type Backoff struct{ baseDelay, maxDelay time.Duration; attempt int }`
  - `func (b *Backoff) Next() time.Duration` — exponential `baseDelay × 2^attempt`, capped at `maxDelay`. After 5 attempts emits `obs.EventCostReconcileFailing` (handled by Tick caller).
  - `func (b *Backoff) NextWithRetryAfter(h http.Header) time.Duration` — if `retry-after` header present and parseable (integer seconds OR HTTP-date), returns `max(headerDelay, b.Next())` — never less than what the server requested (A3 rubric).
  - `func (b *Backoff) Reset()` — call on success.
- **`internal/cost/reconcile/{tick,client,window,backoff}_test.go`** — 12 named tests below.
- **`internal/cost/reconcile/testdata/`** — fixtures:
  - `anthropic_cost_2026_06_01_01h.json` — canned Cost API response with `cost_usd` field per bucket per model.
  - `anthropic_cost_empty.json` — empty bucket response.
  - `anthropic_usage_2026_06_01_01h.json` — canned Usage API fallback response with `input_tokens` / `output_tokens` per model.
  - `anthropic_429.json` — synthetic 429 body + headers (test helper consumes).
  - `anthropic_500.json` — synthetic 5xx body.

### Prereqs (cite spec sections)

- Spec §2 in-scope items #3 (substrate hook), #6 (OTel attrs).
- Spec §3.4 — reconciliation Cost API preferred + Usage API fallback + bucket window + failure-mode table **verbatim**.
- Spec §3.5 — `BudgetReconciledPayload` struct + LWW reducer semantics (T3-owned import).
- Spec §3.6 — `safety.cost.reconcile_interval`, `drift_alert_threshold_pct`, `usage_api_key_env` config fields (T1-merged).
- Spec §3.7 — OTel span attrs: `regatta.cost.period_start`, `regatta.cost.period_end`, `regatta.cost.drift_pct`, `regatta.cost.api_source`.
- Spec §6 T4 — exhaustive named-test list (12 tests transcribed below).
- Spec §7 B/A/A+ — A3 (429 retry-after honour), A4 (drift-alert dedup), B6 (LWW row emission).
- Spec §8 — file-disjoint table row 4 (T4 scope + seams).
- Spec §9 R3 (429 rate-limit + retry-after), R6 (Anthropic down + fail-soft + LWW backfill), R15 (admin key never logged).

### Existing patterns to reuse (do NOT reinvent)

- **substrate.AppendEvent:** T-S1 #224 — same write API T3 uses; reconciler calls with `Kind: substrate.KindBudgetReconciled`.
- **Tracer pattern:** existing W6 T5 normalization — `cfg.Tracer` field on `Config`, fallback to `otel.Tracer("internal/cost/reconcile")`. NO `WithTracer(...)` setter.
- **slog + obs.Event* bridge:** W6 T2 #169 slog→OTel logs bridge. Reconciler emits `obs.EventCostReconcileFallback`, `obs.EventCostReconcileSkipped`, `obs.EventCostReconcileFailing`, `obs.EventCostDriftAlert` slogs — every event handler in obs/event.go.
- **Build info:** `internal/buildinfo` package — `buildinfo.Version` for User-Agent. Existing pattern; do NOT redefine.
- **HTTP client:** standard `*http.Client` (NOT a custom wrapper). Caller injects `cfg.HTTPClient`; default to `&http.Client{Timeout: 30s}`.
- **JSON canonicalization for APIResponseSig:** use `encoding/json` decode-then-marshal with sorted keys (per spec §3.4 line 239 + A2 audit-replay invariant). Pins `TestReconciler_AnthropicResponseSig_IsSHA256OfCanonicalBody`.
- **`spend.BudgetReconciledPayload`:** import from T3. Do NOT redefine.
- **substrate kind constants:** `substrate.KindBudgetReconciled` — exported by T-S1 #224.
- **DefaultTenantID:** `substrate.DefaultTenantID` until W8.
- **No outbound caching:** the Cost/Usage API responses are tenant-scoped + low-rate (1 call per reconcile interval); NO local cache layer needed. Adding one would be R14 + R-A5 cardinality territory.

### TDD test list (named tests from spec §6 T4; failing-output capture step required)

Per `feedback_tdd_discipline`: implementer writes each test first, runs `go test ./<pkg>/ -run <name> -v`, **captures failing output (paste into PR body)**, then implements.

**B-tier (6 named tests — spec §6 T4 + §7 B):**

1. `TestReconciler_TickEmitsBudgetReconciled_CostAPIPreferred` — stub Cost API returns canned response with `cost_usd` field; Tick parses, writes one `budget_reconciled` row with correct payload; Usage API NOT called (assertion on stub-server hit count).
2. `TestReconciler_FallsBackToUsageAPI_WhenCostAPI404` — Cost API returns 404; reconciler retries via Usage API + local pricing application; emits `obs.EventCostReconcileFallback reason=cost_api_unavailable` at WARN; row written.
3. `TestReconciler_DriftBelowThreshold_NoAlert` — actual=$100, recorded=$95, drift=5%; threshold=10%; row emitted, NO `obs.EventCostDriftAlert`.
4. `TestReconciler_DriftAboveThreshold_EmitsAlert` — actual=$100, recorded=$80, drift=20%; threshold=10%; row emitted, ONE `obs.EventCostDriftAlert` at WARN.
5. `TestReconciler_AdminKeyUnset_LogsAndSkips` — env unset → no HTTP call, `obs.EventCostReconcileSkipped` at WARN, no row. Pins fail-soft (R6).
6. `TestReconciler_BucketWindowMatchesAnthropicSpec` — Tick fires at top-of-hour+2min; HTTP request `starting_at` + `ending_at` align with just-closed hour; `bucket_width=1h`.

**A-tier (4 named tests — spec §7 A3 + A4 + audit invariant):**

7. `TestReconciler_DriftAlertDedupedAcrossTicks` — same `period_start`, same `drift_pct` rounded to 2dp across 3 consecutive ticks → exactly ONE alert emitted (A4 rubric pin; pins anti-noise).
8. `TestReconciler_429Backoff_RespectsRetryAfterHeader` — stub returns 429 with `retry-after: 12` three times then 200; reconciler honours header (mock clock asserts wait ≥ 12s); succeeds on 4th try. Pins R3 mitigation + A3 rubric.
9. `TestReconciler_Network5xx_KeepsTickingAndNeverPanics` — stub returns 500 persistently; reconciler emits `obs.EventCostReconcileFailing` after 5 attempts; next Tick continues. No goroutine leak (assert via `runtime.NumGoroutine()` delta over 100 ticks).
10. `TestReconciler_AnthropicResponseSig_IsSHA256OfCanonicalBody` — `payload.api_response_sig` is sha256(canonical(response body)). Pins audit-replay invariant (A2).

**A+-tier (2 named tests — spec §7 A+ + R15 + LWW pin):**

11. `TestReconciler_LWWCorrectionEmitsNewRow` — first Tick writes reconciled row; second Tick same period writes ANOTHER row; Fold returns the later one (LWW per substrate v2 §4). Pins R6 backfill-as-first-class.
12. `TestReconciler_NeverLogsKeyValue` — across every error path (401, 403, 404, 429, 500, network-down), log capture asserts the admin key value (a fixture string like `sk-ant-admin-fixture-DO-NOT-LEAK`) NEVER appears in any log record. Pins R15.

Total T4: **12 named tests** (6 B + 4 A + 2 A+). PR body lists every test name + pasted failing-output excerpt for AT LEAST 6 representative cases.

### PR body skeleton

````
## Summary

Cost-governor Wave 2 T4 ships the reconciler cron + Anthropic Cost API
(preferred) + Usage API fallback + drift detector + alert-dedup + 429
backoff per docs/engineer/specs/2026-06-01-cost-governor-design.md §3.4.

- internal/cost/reconcile/tick.go — Reconciler.Tick() does one window
  reconciliation; Reconciler.Run() drives the long-loop; alert-dedup
  map prevents duplicate WARN slogs across consecutive ticks (A4).
- internal/cost/reconcile/client.go — Anthropic Cost + Usage API HTTP
  client with proper User-Agent, anthropic-version header, and
  configurable env-var admin-key resolution (spec §3.4 lines 208-211 +
  §3.6 usage_api_key_env field).
- internal/cost/reconcile/window.go — top-of-hour + 2min jitter bucket
  alignment (spec §3.4 line 225).
- internal/cost/reconcile/backoff.go — exponential 1s × 2^n capped at
  5min + retry-after header honour (R3 + A3 rubric).
- internal/cost/reconcile/testdata/ — Cost API + Usage API + 429 + 500
  response fixtures for hermetic tests.

## Why

Per spec §3.4: the reconciler is the only signal that catches stream-json
parser drift / pricing-table staleness / spawner SIGKILL gaps. Cost API
preferred eliminates the R-A4 "pricing-applied-twice" defect on the
happy path; Usage API fallback preserves visibility when Cost API is
unavailable (with documented limitation per failure-mode table). LWW
reducer semantics on budget_reconciled rows mean backfill is first-class
(R6 mitigation).

## Test plan

- [x] B-tier (6): TestReconciler_TickEmitsBudgetReconciled_CostAPIPreferred,
       TestReconciler_FallsBackToUsageAPI_WhenCostAPI404,
       TestReconciler_DriftBelowThreshold_NoAlert,
       TestReconciler_DriftAboveThreshold_EmitsAlert,
       TestReconciler_AdminKeyUnset_LogsAndSkips,
       TestReconciler_BucketWindowMatchesAnthropicSpec.
- [x] A-tier (4): TestReconciler_DriftAlertDedupedAcrossTicks,
       TestReconciler_429Backoff_RespectsRetryAfterHeader,
       TestReconciler_Network5xx_KeepsTickingAndNeverPanics,
       TestReconciler_AnthropicResponseSig_IsSHA256OfCanonicalBody.
- [x] A+-tier (2): TestReconciler_LWWCorrectionEmitsNewRow,
       TestReconciler_NeverLogsKeyValue.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 6 reps>

## Deletion default

T4 ELIMINATES the need for a separate "pricing-table correctness alarm"
(third event kind that would otherwise track when our pricing diverges
from Anthropic's) — Cost API preferred path makes drift signal
unambiguous on the happy path (USD-vs-USD, not tokens-vs-tokens). Net:
zero new event kinds beyond budget_reconciled; alert is one slog, not
three.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [cost-governor-followup] regatta cost reconcile --since 6h backfill CLI (#NNN; spec §9 R6)
- [cost-governor-followup] admin-key-vault integration (#NNN; spec §9 R15) — file only if T1 didn't already
- [cost-governor-followup] Anthropic API auto-refresh pricing flag (#NNN; spec §9 R1)

```release-notes
[FEATURE] cost-governor reconciliation tick (Cost API preferred + Usage API fallback + drift detection + 429 retry-after honour; default-off when safety.cost block unset)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-cost-gov-t4. Branch off main:

  git fetch origin
  git checkout -b feat/cost-gov-t4-reconciler origin/main

NOTE: T4 imports spend.BudgetReconciledPayload owned by T3 (cost-gov
Wave 2 parallel sibling). Option A (preferred per
feedback_shared_primitive_owner): wait for T3's primary PR to merge
into main, then rebase. Option B (if you start before T3 lands):
define a local minimal copy of BudgetReconciledPayload in your test
fixtures ONLY (NOT in production code), and rebase to delete the
local copy + import the T3 version before opening your PR. Production
code MUST import the T3-owned struct. If T3 primary PR has already
merged when you start, this is a non-issue.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-cost-governor-design.md.
Read ALL of: §2 (scope, in/out), §3.4 (reconciliation: Cost API preferred,
Usage API fallback, bucket window, failure-mode table — VERBATIM),
§3.5 (BudgetReconciledPayload struct shape; T3-owned import), §3.6
(reconcile_interval, drift_alert_threshold_pct, usage_api_key_env config
fields — T1-merged), §3.7 (OTel attrs for cost.reconcile span), §6 T4
(named test list — 12 tests), §7 B/A/A+ (A3 = retry-after honour, A4 =
drift-alert dedup), §8 (file-disjoint row 4), §9 R3 (429 backoff), R6
(Anthropic-down + LWW backfill), R14 (cardinality), R15 (key never
logged).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (Cost API endpoint URL + headers verbatim from
§3.4 lines 201-218; bucket_width=1h hourly default; window = just-closed
hour from top-of-hour+2min jitter; backoff = exponential 1s × 2^n capped
at 5min; retry-after honour with max(headerDelay, backoffDelay);
drift_alert dedup keyed by period_start + drift_pct@2dp; LWW reducer
semantics on budget_reconciled rows from substrate v2 §4), STOP and
report — do NOT pick an alternative yourself. Re-spawn the design
subagent.

# Scope (exclusive write paths — file-disjoint with T3)

- internal/cost/reconcile/tick.go
- internal/cost/reconcile/client.go
- internal/cost/reconcile/window.go
- internal/cost/reconcile/backoff.go
- internal/cost/reconcile/tick_test.go
- internal/cost/reconcile/client_test.go
- internal/cost/reconcile/window_test.go
- internal/cost/reconcile/backoff_test.go
- internal/cost/reconcile/testdata/anthropic_cost_2026_06_01_01h.json
- internal/cost/reconcile/testdata/anthropic_cost_empty.json
- internal/cost/reconcile/testdata/anthropic_usage_2026_06_01_01h.json
- internal/cost/reconcile/testdata/anthropic_429.json
- internal/cost/reconcile/testdata/anthropic_500.json

You MUST NOT touch any other file. Specifically:
- Do NOT modify internal/cost/spend/ — that is T3's exclusive scope (payload + writer + the validate-dispatch addition). Import only.
- Do NOT redefine spend.BudgetReconciledPayload — import the T3-owned struct.
- Do NOT modify internal/cost/gate/, internal/cost/estimate/, internal/cost/pricing/ — those are T1 + T2 scope (already merged). Import pricing.Lookup only when computing Usage-API-fallback drift.
- Do NOT modify internal/orchestrator/spawner/ — that is T3's scope (amendment + primary).
- Do NOT modify internal/orchestrator/state/substrate/ — that is T3's scope (validate.go init addition). Import substrate.AppendEvent + KindBudgetReconciled + DefaultTenantID only.
- Do NOT modify internal/orchestrator/scheduler/ — that is T1's scope (already merged).
- Do NOT add a "regatta cost reconcile" CLI subcommand in this PR — backfill CLI is followup (#NNN to be filed; spec §9 R6).

If you discover a missing seam in an out-of-scope file, STOP and report
— file a tracking issue per finding; do NOT edit out of scope.

# Patterns to reuse (do NOT reinvent)

- substrate.AppendEvent: T-S1 #224 export for writing budget_reconciled rows.
- substrate.KindBudgetReconciled: T-S1 #224 const.
- substrate.DefaultTenantID: T-S1 #224 const.
- Tracer pattern: cfg.Tracer field; fallback to otel.Tracer("internal/cost/reconcile"); NO WithTracer setter.
- slog → OTel logs bridge: W6 T2 #169. Emit obs.EventCostReconcileFallback / Skipped / Failing / EventCostDriftAlert.
- buildinfo.Version: existing internal/buildinfo package for User-Agent.
- spend.BudgetReconciledPayload + ModelBreakdownRow: import from internal/cost/spend (T3-owned).
- pricing.Lookup + pricing.Row: import from internal/cost/pricing for Usage API fallback drift computation.
- httptest.NewServer: standard Go stdlib for stub Anthropic server in tests. Set up `/v1/organizations/cost_report/messages` + `/v1/organizations/usage_report/messages` handlers.
- Mock clock: existing testing pattern — pass `clock func() time.Time` into Reconciler; tests inject a deterministic clock.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test below:
  1. Write the test file first.
  2. Run `go test ./internal/cost/reconcile/ -run <TestName> -v`.
  3. CAPTURE the failing output (paste at least 6 representative samples into PR body's "Failing-test output (TDD capture)" section). "Tests would have failed" is NOT acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or per logical group; squash later if needed).

# Tests to land (12 named; spec §6 T4)

B-tier (6):
1.  TestReconciler_TickEmitsBudgetReconciled_CostAPIPreferred
2.  TestReconciler_FallsBackToUsageAPI_WhenCostAPI404
3.  TestReconciler_DriftBelowThreshold_NoAlert
4.  TestReconciler_DriftAboveThreshold_EmitsAlert
5.  TestReconciler_AdminKeyUnset_LogsAndSkips
6.  TestReconciler_BucketWindowMatchesAnthropicSpec

A-tier (4):
7.  TestReconciler_DriftAlertDedupedAcrossTicks
8.  TestReconciler_429Backoff_RespectsRetryAfterHeader
9.  TestReconciler_Network5xx_KeepsTickingAndNeverPanics
10. TestReconciler_AnthropicResponseSig_IsSHA256OfCanonicalBody

A+-tier (2):
11. TestReconciler_LWWCorrectionEmitsNewRow
12. TestReconciler_NeverLogsKeyValue

# Workflow after green

  1. Run `make pre-push-check` — confirm clean. If any lint / build / test fails, fix in this branch — do NOT skip hooks (--no-verify is banned per feedback_pr_lint_gates).
  2. Re-run `go test ./internal/cost/reconcile/ -v -race -count=10` (race detector + repeated runs for the 5xx-no-leak test) and confirm every named test green.
  3. Run `git diff origin/main -- '*.go' | grep -E '^\+.{0,2}//'` to spot superfluous comments; sweep per feedback_comments_discipline.
  4. Push branch: `git push -u origin feat/cost-gov-t4-reconciler`.
  5. File the 2-3 followup tracking issues (regatta cost reconcile --since backfill CLI; admin-key-vault integration ONLY if T1 didn't already; Anthropic Pricing API auto-flag for R1) and gather issue numbers. Coordinate with T3 + T1 + T2 followups.
  6. Open PR via `gh pr create --base main --title "feat(cost): T4 reconciler + Anthropic Cost API + Usage API fallback + 429 backoff" --body-file <path>` (NEVER heredoc per feedback_pr_lint_gates). Body MUST cite followup issue numbers.
  7. Spawn ONE adversarial reviewer subagent (per feedback_adversarial_review + feedback_agent_pr_review + feedback_simplify_reviewer) with hunt list (see below).
  8. Apply reviewer findings inline (or file tracking issue + cite in PR body per feedback_unaddressed_load_bearing).
  9. Re-run `make pre-push-check`; force-push.
 10. Verify CI green (pr-lint, check-release-notes, check-tdd, build, test) BEFORE flipping automerge per feedback_review_before_automerge.
 11. Flip automerge ONLY after reviewer cleared the PR.

# Adversarial reviewer hunt list

- Cost API endpoint URL + bucket_width + group_by exactly per spec §3.4 lines 201-218. No drift.
- Headers: anthropic-version: 2023-06-01, x-api-key from cfg.UsageAPIKeyEnv env (configurable; default ANTHROPIC_ADMIN_KEY), User-Agent: regatta/<buildinfo.Version> (https://github.com/maydow/regatta). Every header present on EVERY request (Cost + Usage).
- Admin key never logged: TestReconciler_NeverLogsKeyValue covers — but also confirm by inspection that no fmt.Sprintf / %v / %+v anywhere prints the http.Header map or the cfg struct verbatim. Use cmp.Equal at construction, never log.
- Fallback path emits the WARN slog with reason=cost_api_unavailable EXACTLY (string match).
- Drift dedup key: period_start + drift_pct rounded to 2 decimal places. NOT period_start alone (would mask drift growing over time); NOT drift_pct without rounding (would emit on every tick due to floating-point noise).
- Backoff cap: max delay 5min. baseDelay 1s. attempt starts at 0 (so first retry is 1s, not 2s).
- retry-after honour: max(headerDelay, backoffDelay) — server's ask takes precedence when larger. Test asserts wait ≥ 12s when header says 12s and backoff would say 8s.
- LWW pin (R6 backfill): TestReconciler_LWWCorrectionEmitsNewRow asserts both rows exist in substrate (substrate v2 §4 LWW reducer returns the later one, but both physical rows are present for audit).
- APIResponseSig canonicalization: decode JSON to Go map → re-marshal with sorted keys → sha256. Don't sha256 the raw response bytes (would differ on whitespace).
- Goroutine-leak audit: Reconciler.Run() spawns no daemon goroutines that outlive ctx. TestReconciler_Network5xx_KeepsTickingAndNeverPanics asserts runtime.NumGoroutine() returns to baseline after 100 ticks + ctx cancel.
- Span cardinality (R14): cost.reconcile span attrs are tenant-scoped (period_start, period_end, drift_pct, api_source) — NO operator_id / dag_id / work_item_id on the reconcile span. (Those belong on token_spend events, not reconcile spans.)
- Simplification opportunity: could Reconciler skip Cost API entirely and only use Usage API? No — spec §3.4 line 199 mandates Cost API preferred to eliminate R-A4 pricing-applied-twice defect. The fallback is documented limitation, not the default.
- Simplification opportunity: could backoff be a flat sleep-15s loop? No — spec §3.4 mandates exponential + retry-after honour. Flat sleep ignores server's rate-limit semantics.
- Simplification opportunity: could the alert-dedup map grow unbounded? Yes, in principle — confirm dedupe state has a TTL (per period_start; rows older than 7d evicted) OR is bounded by a size cap with LRU eviction. If unbounded, file a tracking issue + cite as known limitation.
- Deletion default: PR body cites concrete shrinkage (no third event kind for pricing-table alarm).
- No new substrate kind, no new migration, no new validator — T3 owns those.
- No AI signatures anywhere (feedback_no_signatures).
- godocs ≤ 1 line on test funcs (feedback_comments_discipline).

# Hygiene

- NO AI signatures anywhere (commits, PR body, comments, code) per feedback_no_signatures.
- Comments discipline per feedback_comments_discipline: WHY not WHAT; test-function godocs ≤ 1 line; sweep on every push.
- Doc-check: run `git diff origin/main -- '*.md' '*.go'` and verify no godoc line exceeds 1 line on test funcs BEFORE push.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 6 of the 12 tests (sample is fine — PR body carries full set).
- The 2-3 followup issue numbers filed.
- Adversarial reviewer verdict (APPROVE or full findings list with severities).
- One-line diff stat: files changed + LoC added/removed.

Begin now. NEVER pause for user input.
```

---

## §4 After Wave 2 — handoff to Wave 3 (T5 operator doc)

Wave 2 exit gate (per spec §10 line 767):
- A synthetic spawn with the W6 stream-json fixture produces a substrate `token_spend` row with correct payload.
- A stub Anthropic Cost API server returns a canned response and the reconciler emits a `budget_reconciled` row.
- Fallback path tested with Cost-API-down + Usage-API-up scenario.

**Wave 3 dispatch readiness** (T5 — operator doc):

- All of T1, T2, T3, T4 merged to main.
- The autonomous-session-prompt at `docs/engineer/autonomous-session-prompt.md` is refreshed (per `feedback_boot_prompt_per_wave_refresh`) to drop Wave 2 entries + add Wave 3 (T5) entry.
- Wave 3 brief (per `feedback_roadmap_pre_fetch`): T5 ships `docs/operator/cost-governor.md` + commented-out demo block in `examples/full/regatta.yaml`. Doc covers env-var contract, precedence rule (most-restrictive-wins per spec §3.6 line 370), drift-alert reading, soft-cap WARN semantics + opt-in downgrade, pricing-refresh runbook, OTel cardinality recommendation, dashboard-query examples, Cost-vs-Usage-API fallback documented-limitation. ~300 lines. Spec §6 T5 (test list — doc-check pass + every config field documented) + §7 B/A/A+.
- Wave 3 PR is doc-only — skip ceremony per `feedback_review_proportional`. One reviewer pass at draft; one adversarial review at PR-open; done.

**Followups to file post-Wave-2** (per `feedback_followup_filing_universal`; deltas not filed by T1/T2/T3/T4):

- per-tenant + per-team budgets (W8 cutover; spec §2 OOS + R9) — likely filed by T1; re-verify.
- Stripe webhook (spec §2 OOS).
- predictive forecasting (spec §2 OOS).
- mid-DAG kill+compensation (spec §2 OOS).
- cache-aware budgeting (spec §2 OOS).
- cross-fleet MCP attribution (spec §2 OOS).
- Bedrock pricing (spec §2 OOS).
- Anthropic Pricing API auto-flag (spec §9 R1) — likely filed by T4.
- backfill recipe (spec §9 R6) — filed by T4.
- progress-gated renewal (spec §2 OOS).
- spawner reconciliation outbox (spec §9 R13) — filed by T3.
- history estimator opt-in (spec §10 S1) — filed by T1 or T2.
- pricing_override_path config surface (spec §10 S2) — filed by T1 or T2.
- admin-key-vault integration (spec §9 R15) — filed by T1 or T4.

A7 rubric (≥ 13 issues filed across all cost-gov PRs) confirmed.

---

## Adversarial-review pass (applied inline)

Per `feedback_adversarial_review` + `feedback_simplify_reviewer` + `feedback_agent_pr_review`: one reviewer subagent ran against the draft of this plan. Findings + applied fixes below.

1. **Shared-primitive seam violation hunt.**
   *Finding:* Initial draft had T4's reconciler defining a local `BudgetReconciledPayload` struct as a "convenience copy". This violates `feedback_shared_primitive_owner`.
   *Fix applied:* T4 dispatch prompt + §3 Patterns + §1 Cross-task seam now state explicitly that T4 imports `spend.BudgetReconciledPayload` from T3 and **must not redefine**. Option-B (local copy in `_test.go` only, pre-T3-merge) is permitted strictly for test fixtures — production code imports T3.

2. **TDD test-list completeness vs spec §6.**
   *Finding:* Spec §6 T3 lists 7 named tests; spec §6 T4 lists 12 named tests. Initial draft missed `TestRecordCall_OneWrittenByPerCallID` (R12 audit invariant, called out in spec §9 R12 line 723 but not listed in §6 T3).
   *Fix applied:* T3 dispatch + PR body skeleton + test list now include `TestRecordCall_OneWrittenByPerCallID` as reviewer-added (B8 in the test enumeration) with spec citation. T4 list matches §6 T4 verbatim (all 12 tests).

3. **Dispatch-prompt unambiguity.**
   *Finding:* "one-line spawner edit ≤ 6 lines" constraint per spec §8 line 641 is ambiguous: does the ≤6-line budget apply to T3 amendment, T3 primary, or both summed? Initial draft conflated.
   *Fix applied:* §2 Scope clarifies — the ≤6-line genai.go diff is the **amendment-PR diff** (the callback parameter + invocation site). T3 primary's genai.go diff is **ZERO lines** (the callback hook already exists after amendment; the callback BODY is registered in the production caller of `NewClaudeSpawner`, which lives outside `genai.go`). The implementer is instructed to flag this interpretation if it differs from their reading and re-spawn the design subagent rather than guess.

4. **File-overlap risk T3 vs T4 (zero overlap required).**
   *Finding:* Both T3 + T4 import substrate constants. Risk of one of them registering a validator the other relied on for testing.
   *Fix applied:* §1 Cross-task seam states T3 OWNS both `KindTokenSpend` AND `KindBudgetReconciled` validate-dispatch entries (via `RegisterPayloadValidator` in T3's `init()`). T4 does NOT register a validator. T4's test suite uses substrate test helpers (in-memory DB without validation, OR test DB after T3 init runs because of Go's init-order semantics — same package `substrate` has T3's init block; T4 imports `substrate` so the init runs transitively). Documented in T4 hunt list: "No new substrate kind, no new migration, no new validator — T3 owns those."

5. **W6 T4 amendment scope creep risk.**
   *Finding:* Initial draft of the amendment said "thread `tx` from caller down to handler". This is more invasive than needed AND requires every caller (including the 7+ test callsites) to be updated with a DB seam.
   *Fix applied:* Amendment is narrower — adds an **optional callback** (`OnResultEvent`) to `ClaudeSpawnerConfig`. Nil callback = no-op = byte-equal current behaviour. Only 1 test callsite update (`e2e_test.go`) because the rest of the test files don't use `ClaudeSpawnerConfig` — they invoke `ParseStream` directly. ParseStream's new 4th param is `nil` in those callsites. Net delta: ≤ 35 LoC across genai.go + claude.go + e2e_test.go.

6. **One-line spawner edit constraint validation.**
   *Finding:* The "≤ 6 lines" constraint applies to genai.go primary diff. After the amendment, the genai.go primary diff is actually 0 lines, which the constraint trivially satisfies. The constraint's purpose (per spec) is "minimal surface area on the W6-owned file". The plan satisfies this.
   *Fix applied:* Plan now explicitly states "T3 primary's genai.go diff is **ZERO lines**" — the surface is moved to claude.go (production wiring) where it's contextually correct (cost-gov is wiring a config field, not modifying the parser).

7. **Pre-merge seam verification step.**
   *Finding:* No pre-flight check in the T3 amendment prompt to verify `ParseStream` signature + `ClaudeSpawnerConfig` shape match the plan's assumption.
   *Fix applied:* T3 dispatch prompt has a `# Pre-flight verification` section with `grep` commands the implementer runs before starting. If signatures already match (someone else amended), STOP and report — amendment is already landed.

8. **Cyclic-import audit.**
   *Finding:* `internal/cost/spend` (T3) → `internal/orchestrator/state/substrate` (T-S1). `internal/orchestrator/spawner` → `internal/cost/spend` (T3 primary; via the production wiring of OnResultEvent). Is there a back-edge?
   *Fix applied:* Yes there is a potential cyclic risk if substrate imports back into cost. T3 hunt list now includes: "Cyclic-import check: cost/spend imports substrate, pricing. substrate does NOT import cost. spawner does NOT import cost. Verify `go list -deps ./internal/cost/spend/` shows no cost→spawner edge." The wiring closure is constructed in a thin cost-gov package (`cost.SpawnerCallback(db, tracer) ClaudeResultCallback`) that imports both `spawner` (for the `ClaudeResultCallback` type) and `spend` (for RecordCall) — but does NOT live inside `spawner` itself. Production-orchestrator-side wiring point imports the cost-gov constructor.

9. **Alert-dedup unbounded growth.**
   *Finding:* T4's `alertDedupe map[string]float64` grows one entry per unique `(period_start, drift_pct@2dp)`. Over months of ticks, this is unbounded.
   *Fix applied:* T4 hunt list includes: "Simplification opportunity: could the alert-dedup map grow unbounded? Yes — confirm dedupe state has a TTL OR is bounded by size cap with LRU eviction. If unbounded, file tracking issue." TTL of 7d (matches spec §3.4 default reconcile retention; rows that old are stale beyond actionable horizon) is the recommended bound; if the implementer ships unbounded, they file a tracking issue.

10. **APIResponseSig canonicalization correctness.**
    *Finding:* Naïve sha256(rawBody) breaks audit-replay across HTTP whitespace differences. Spec §3.4 line 239 says "sha256 of the canonical Anthropic response body".
    *Fix applied:* T4 hunt list + §3 Patterns now mandate decode-then-re-marshal-with-sorted-keys before hashing. Test `TestReconciler_AnthropicResponseSig_IsSHA256OfCanonicalBody` covers.

11. **AI-signature guard (`feedback_no_signatures`).**
    *Finding:* Boilerplate AI footers anywhere = caught and reverted.
    *Fix applied:* Every dispatch prompt + PR body skeleton ends with explicit "NO AI signatures" reminder. Hunt list includes "No AI signatures anywhere (commits, PR body, comments, code)". `Co-Authored-By: Claude` and `Generated with Claude Code` strings are explicitly banned.

12. **Doc-check sweep pre-push.**
    *Finding:* Per `feedback_pr_lint_gates`: `make check` clean ≠ `pr-lint` clean. Doc-check fires on godoc length + comment discipline.
    *Fix applied:* Every workflow step 3 is "Run `git diff origin/main -- '*.go' | grep -E '^\\+.{0,2}//'` and sweep". Test funcs godoc ≤ 1 line per `feedback_comments_discipline`. Run pre-push-check; verify CI green BEFORE flipping automerge.

13. **Followup filing universality (`feedback_followup_filing_universal`).**
    *Finding:* T3 + T4 must file followups even though both are mid-stack impl PRs.
    *Fix applied:* T3 primary files 3 (cost backfill CLI; spawner reconciliation outbox on SIGKILL; gen_ai.response.id collision audit). T4 files 2-3 (regatta cost reconcile --since backfill CLI; admin-key-vault if not filed by T1; Anthropic Pricing API auto-flag). PR bodies cite issue numbers.

14. **Migration-number lock (`feedback_migration_number_lock`).**
    *Finding:* Could an implementer mistakenly add a new substrate migration?
    *Fix applied:* Wave overview explicitly states "NO new migration in Wave 2". T3 hunt list: "No new substrate kind, no new migration, no new validator". Migration #0007 remains reserved.

15. **Concurrency cap (`feedback_session_limit_dispatch`).**
    *Finding:* T3 spawns a two-PR sequence — does this count as 2 implementers or 1?
    *Fix applied:* It's 1 implementer subagent that opens 2 PRs sequentially (amendment merges first). Net concurrent subagent count: 2 (T3-subagent + T4-subagent). Under the 3-4 ceiling.

16. **Spec citation completeness.**
    *Finding:* Some test items reference "spec §6 T3" without line numbers; readers can't quickly verify.
    *Fix applied:* Test enumerations now include the spec §6 sub-section name + the line ranges where the test name + assertion appear in the spec. Implementers can `grep -n "TestRecordCall_AppendsTokenSpendEvent" docs/engineer/specs/2026-06-01-cost-governor-design.md` to find the source.

---

_Plan authority: this plan is a dispatch artifact only. The main session copy-pastes the §2 / §3 dispatch prompts into Agent tool invocations once Wave 1 T1 merges. The implementer subagents are accountable for their PR-A + PR-B (T3) or single PR (T4); main session is accountable for sequence + follow-up filing + automerge gating per `feedback_review_before_automerge`._
