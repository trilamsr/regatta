# Cost Governor (P8) Wave 1 — Implementer Task Breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-cost-governor-design.md`.
Authority: `feedback_spec_pattern_authority` — implementer deviation from any spec-mandated pattern (Gate concrete-type-no-interface per spec S5, Verdict + Request struct fields per spec §3.2, Reader "single SELECT with json_extract" per spec §3.5, CUE additive-only `#CostGovernor` per spec §3.6, scheduler step-0.6 ordering, UpperBound formula per spec §3.3, hardcoded pricing table per spec §3.8) MUST re-spawn the design subagent. NO implementer-chosen alternatives.

Design priority for every decision below (`feedback_decision_priority`): **UX → ease of use → best practices → execution speed → velocity**. Grade rubric (`feedback_grade_rubric`) inherited verbatim from spec §7 — each Wave 1 task carries the spec's B / A / A+ tool-checkable criteria.

---

## Wave overview

- **2 file-disjoint implementer tasks** (T1, T2) per spec §10 Wave 1. Both dispatch in **parallel** from main per `feedback_parallel_dispatch` — T1 imports `estimate.Estimator` (interface, T2-owned) but the interface contract is locked at spec §3.2 line 144 + spec §6 T2 — T1 can mock the interface in tests until T2 lands. Per `feedback_sequence_dependent_work`: T1 and T2 are file-disjoint AND the cross-import seam (Estimator interface) is contract-frozen at spec time, so PARALLEL is safe.
- **Prereqs (merged to main):**
  - W6 T1 #172 (OTel SDK setup) — for `cfg.Tracer` injection on `Gate` + `Reader`.
  - W6 T2 #169 (slog→OTel logs bridge) — for the `obs.EventCostSoftCapBreached` slog used by R10 mitigation.
  - W6 T5 #210 (Config.Tracer injection across 8 components) — Gate adopts the same pattern.
  - Substrate v2 Wave 1 — T-S1 #224 (event log + 0006 migration + HMAC sign) **merged**; T-S2 (CELDecider + gate_verdict) + T-S3 (lint tools + property tests) **assumed merged before this wave dispatches**. Cost-governor consumes `KindTokenSpend` + `KindBudgetReconciled` event kinds + the `DefaultTenantID` const, all already exported by `internal/orchestrator/state/substrate/event.go` since #224.
- **Sequence vs parallel:** T1 and T2 dispatch simultaneously. Both branch off `main`. The single cross-task import seam (`estimate.Estimator` interface owned by T2, consumed by T1) is contract-locked at spec §3.2 — T1 mocks the interface in its tests via an in-test stub, so T1 does NOT need T2 to merge first. Per `feedback_shared_primitive_owner`: T2 OWNS the interface. T1 imports `internal/cost/estimate.Estimator` (and `internal/cost/pricing.Lookup` + `internal/cost/pricing.ErrPricingMissing` — also T2-owned). T2 has ZERO T1 imports.
- **Migration phasing (`feedback_migration_number_lock`):** **NO new migration in Wave 1.** Cost data lives in existing substrate `events` rows under `kind='token_spend'` + `kind='budget_reconciled'` per spec §3.5 — both kinds already registered + reduced + signed by substrate v2 Wave 1 (#224). Migration #0007 is **reserved** for a future spec; cost-governor Wave 1 + Wave 2 do not need one. Wave 2's T3 adds dispatch-table entries to substrate's `validate.go` (open-extensible via T-S1's `RegisterPayloadValidator`) — still migration-free.
- **Concurrency cap (`feedback_session_limit_dispatch`):** 2 parallel implementers. Well under the 3-4 ceiling; no risk of session-limit cascade.
- **Deletion default (`feedback_deletion_default`):** Wave 1 is pure-addition (new `internal/cost/` package, additive CUE field, additive scheduler step). Per-PR `## Deletion default` section answers "what got smaller?":
  - **T1:** spec §10 S3 folds the original 6th task (T5 config + scheduler hook) into T1's PR — net 6 → 5 cost-gov tasks, one cross-PR coordination tax + one Wave-3 PR eliminated. Plus: post-W11 retirement issue filed for legacy `safety.spend_cap_usd` + `safety.spend_cap_usd_per_day` fields once operators migrate to `safety.cost.*`.
  - **T2:** spec §10 S1 + S2 cut `history` estimator (~80 LoC) AND `pricing_override_path` config surface (~50 LoC). The S2 cut **eliminates R14 (override-tampering surface) entirely** — risk slot reused for OTel cardinality. Refresh-via-code-change matches Helicone / Portkey / LiteLLM v1 shape.
- **Followup filing (`feedback_followup_filing_universal` + `feedback_unaddressed_load_bearing`):** every load-bearing named-but-deferred item in spec §2 (OOS list) + §9 (R-tier mitigations deferred) is filed as a `[cost-governor-followup]` issue PRE-MERGE; PR body cites the issue numbers. A7 rubric requires ≥ 13 issues filed across both PRs (per spec §7 A7).

---

## §1 File-disjoint table

| Task | Path (exclusive write scope) | Depends-on (Wave 1 + main) | Effort | TDD tests (count: named) |
| ---- | --------------------------- | -------------------------- | ------ | ------------------------ |
| T1   | `internal/cost/gate/{gate,verdict,scope}.go` + `*_test.go`; `internal/cost/spend/{reader,scope}.go` + `*_test.go`; `contracts/schemas/regatta.v1.cue` (additive `cost?: #CostGovernor` only); `internal/config/validate/cost.go` + `cost_test.go`; `internal/orchestrator/scheduler/scheduler.go` (insert step-0.6 + one private method; ≤ 30 LoC net delta); `internal/orchestrator/scheduler/scheduler_cost_gate_test.go` (NEW) | main; substrate v2 Wave 1 (#224 merged) | M-L | 20 named (B 12, A 4, A+ 4). Spec §6 T1 + §7 B/A/A+. |
| T2   | `internal/cost/estimate/{upper_bound,probe}.go` + `*_test.go`; `internal/cost/pricing/{anthropic,lookup}.go` + `*_test.go`; `internal/cost/pricing/testdata/` (optional fixture dir) | main | S | 8 named (B 4, A 2, reviewer-added 2). Spec §6 T2 + §7 B/A/A+. |

**Disjointness verification (`grep` at plan time):**
- T1 writes only to `internal/cost/gate/` + `internal/cost/spend/` + `internal/config/validate/cost*` + `internal/orchestrator/scheduler/scheduler{.go,_cost_gate_test.go}` + the additive CUE block.
- T2 writes only to `internal/cost/estimate/` + `internal/cost/pricing/`.
- No path appears in both rows. `internal/cost/` is split by sub-package (`gate/` + `spend/` for T1; `estimate/` + `pricing/` for T2). CUE schema is T1-only. Scheduler is T1-only. Pricing + estimator packages are T2-only.

**Cross-task seam contracts (load-bearing — implementer MUST honour exactly):**

- T1 exports `gate.Gate`, `gate.Verdict`, `gate.Request`, `gate.WorkItemScope`, `gate.Config`, `spend.Reader`, `spend.ScopeKey`, `spend.ScopeKind`, sentinels `gate.ErrCapExceeded`, `validate.ErrCostBlockEmpty`, `validate.ErrCostCapsAllZero`, `validate.ErrCostSoftPctOutOfRange`. These are the public seams Wave 2 T3 / T4 + Wave 3 T5 import.
- T2 exports `pricing.Row`, `pricing.Lookup`, `pricing.Anthropic` (the map), `pricing.ErrPricingMissing`, `estimate.Estimator` (interface), `estimate.UpperBound` (concrete type), `estimate.Hint` (struct), `estimate.Probe`, `estimate.NewProbe`. T1 imports `estimate.Estimator` + `pricing.Lookup` + `pricing.ErrPricingMissing` only.
- **Shared-primitive owner (`feedback_shared_primitive_owner`):** `estimate.Estimator` interface lives in T2 alongside the concrete `UpperBound`. T1 imports the interface; if T1 needs to refine the interface shape mid-implementation, STOP and coordinate via the design subagent — do NOT redefine the interface in T1's package.
- **T1 → T2 is the ONLY cross-import.** T2 has zero T1 imports — pure compute + one-time CLI probe.
- **NOT in Wave 1 (Wave 2 T3-owned):** `spend.TokenSpendPayload`, `spend.BudgetReconciledPayload`, `spend.CallRecord`, `spend.RecordCall`. T1's Reader unmarshals the substrate `payload_json` column via `json_extract` SQL only (single SELECT per spec §3.5) — no Go-struct dependency on the T3-owned typed payloads. This keeps T1 fully independent of T3 / Wave 2.
- Substrate kind constants + `DefaultTenantID` are ALREADY EXPORTED by `internal/orchestrator/state/substrate/event.go` (#224). T1 imports — does NOT redefine.

---

## §2 Task T1 — Gate seam + spend Reader + CUE config + validator + scheduler step-0.6 hook

### Scope

- **`internal/cost/gate/`** — NEW package. Files:
  - `gate.go` — `Gate` struct (spec §3.2 lines 138-145 verbatim: `cfg / pricing / spend / estim / tracer / log`); `NewGate(cfg Config, pricing pricing.Lookup, reader *spend.Reader, est estimate.Estimator, tracer trace.Tracer, log *slog.Logger) *Gate`; `Evaluate(ctx context.Context, req Request) (Verdict, error)` — single method per spec S5 (concrete type, no interface).
  - `verdict.go` — `Verdict` struct (spec §3.2 lines 148-156 verbatim: `Allow / Reason / USDEstimate / SoftCapBreached / DowngradeTo / CapDAGUSD / CapOperatorUSD`); `Request` struct (spec §3.2 lines 171-176: `WorkItemID / DAGID / OperatorID / TenantID / Model / EstHint`).
  - `scope.go` — `WorkItemScope` enrichment (annotation reader for `cost.allow_downgrade` per spec §2 in-scope #4 + R10 mitigation default-off opt-in).
- **`internal/cost/spend/`** — NEW package (Wave 1 reader-only; Wave 2 T3 adds writer + payload structs to this package). Files:
  - `reader.go` — `Reader` struct (spec §3.5 lines 297-303 verbatim); `NewReader(db *sql.DB, clock func() time.Time) *Reader`; `BudgetState(ctx, scope ScopeKey, period time.Duration) (USD float64, err error)` per spec §3.5 lines 305-309 — **single SELECT with `SUM(json_extract(payload_json, '$.usd'))` over `substrate_events WHERE kind='token_spend' AND tenant_id=? AND written_at >= ? AND json_extract(payload_json, '$.<scope-field>') = ?`** — no app-side loop, no `for rows.Next()`; `LastReconciliation(ctx, tenantID) (BudgetReconciledPayload, error)` per spec §3.5 lines 311-313 (returns latest row by `written_at` — substrate `lww` semantics inherited).
  - `scope.go` — `ScopeKey` struct (spec §3.5 lines 315-321 verbatim) + `ScopeKind` enum (`dag | operator | work_item | global`).
- **`contracts/schemas/regatta.v1.cue`** — ADDITIVE ONLY. Append `cost?: #CostGovernor` to `#Safety` (spec §3.6 lines 342-353) + add `#CostGovernor` definition with optional caps + `soft_pct *80 | int & >=50 & <=99` + `reconcile_interval *"1h" | "5m" | "15m" | "30m" | "6h" | "24h"` + `drift_alert_threshold_pct *10 | int & >=0 & <=100` + `usage_api_key_env *"ANTHROPIC_ADMIN_KEY" | string`. **Every existing `#Safety` field preserved byte-equal** — Wave 1 invariant is byte-equal MVP-2 behaviour when `safety.cost` unset (B2 rubric).
- **`internal/config/validate/cost.go`** — three validators wired into existing `Load`:
  - `ErrCostBlockEmpty` — reject `safety: { cost: {} }` (no caps configured ⇒ no-op overhead per I4).
  - `ErrCostCapsAllZero` — reject every cap set to 0 (R7 misconfig defense; error message names the exact field combination + the omit-to-disable workaround).
  - `ErrCostSoftPctOutOfRange` — soft_pct < 50 or > 99 ⇒ reject (CUE primary, validator backstop).
- **`internal/orchestrator/scheduler/scheduler.go`** — single insert:
  - Add `applyCostGovernor(ctx, spawnable []state.WorkItem) ([]state.WorkItem, error)` method mirroring `applyApprovalGates` (currently at line 481 — same filter-in-place signature shape).
  - Insert ONE call between `applyApprovalGates` (#114) and `reserveFromSpawnable` per spec §3.2 step 0.6.
  - When `Config.CostGate == nil` (cost unset) ⇒ `applyCostGovernor` returns the input slice byte-equal with NO substrate read, NO span allocation (spec §6 T1 `TestSchedulerTick_HookIsNoopWhenCostUnset` — I6 zero-overhead invariant).
  - Net LoC delta: ≤ 30 (spec §8 row 1).
  - `Scheduler.Config` gains optional `CostGate *gate.Gate` field (default nil = MVP-2 byte-equal).
- **`internal/orchestrator/scheduler/scheduler_cost_gate_test.go`** — NEW colocated test file; mirrors the shape of `scheduler_approval_gate_test.go`.

### Prereqs (cite spec sections)

- Spec §2 in-scope items #1 (package layout), #2 (scheduler tick seam), #5 (config surface), #6 (OTel attrs).
- Spec §3.2 — Gate struct + Verdict + Request signatures **verbatim** (load-bearing per `feedback_spec_pattern_authority`).
- Spec §3.5 — Reader struct + BudgetState SQL shape ("single SELECT with json_extract — no app-side loop").
- Spec §3.6 — CUE schema additions (backwards-compatible, every existing `#Safety` field preserved).
- Spec §3.7 — OTel attrs: `regatta.cost.usd_estimate`, `regatta.cost.cap_dag_usd`, `regatta.cost.cap_op_usd`, `regatta.cost.allow`, `regatta.cost.soft_breached`, plus W6 `regatta.work_item_id` / `regatta.dag_id` / `regatta.operator_id`.
- Spec §6 T1 — exhaustive named-test list (20 tests transcribed below).
- Spec §8 — file-disjoint table row 1 (T1 scope + seams).
- Spec §10 S3 — T5 (config + scheduler hook) FOLDED into T1's PR; do NOT split into a Wave-3 task.
- Spec §9 R7, R9, R10, R14 — misconfig defense, tenant-id forward-fit, soft-cap WARN-only default, OTel cardinality.

### Existing patterns to reuse (do NOT reinvent)

- **Approval-gate hook (mirror exactly):** `internal/orchestrator/scheduler/scheduler.go::Scheduler.applyApprovalGates` line 481. `applyCostGovernor` MUST have the same filter-in-place shape + nil-config no-op guard.
- **Approval-gate test fixture:** `internal/orchestrator/scheduler/scheduler_approval_gate_test.go`.
- **Substrate read:** `internal/orchestrator/state/substrate/fold.go::Fold` for kind-scoped query shape. Reader uses `SUM(json_extract(payload_json, '$.usd'))` over `kind='token_spend'` rows.
- **Substrate constants ALREADY EXPORTED:** `substrate.KindTokenSpend`, `substrate.KindBudgetReconciled`, `substrate.DefaultTenantID` — import, do NOT redefine.
- **WithTx pattern:** `internal/orchestrator/state/agents.go::WithTx` for the Reader's read tx (SQLite WAL snapshot).
- **Tracer pattern:** existing W6 T5 normalization — `cfg.Tracer` field on `Config`, fallback to `otel.Tracer("internal/cost/gate")` per W6 spec §3.4. NO `WithTracer(...)` setter (cardinality-prevention; matches spec §7 A8).
- **CUE schema extension:** `contracts/schemas/regatta.v1.cue` current `#Safety` block — ADDITIVE only; every existing field byte-equal.
- **Validator wiring:** `internal/config/validate/load.go` existing call chain — add cost-validator entries after CUE eval.
- **Approval-gate `WorkItem.Annotations` access:** existing pattern for reading work_item annotations (used by `cost.allow_downgrade` opt-in per R10).
- **HMAC reuse:** Reader does NOT sign — only reads. No new signing primitive.
- **Estimator interface:** import `internal/cost/estimate.Estimator` (T2-owned). T1's `Gate` field is `estim estimate.Estimator` (interface), populated by caller. NO compile-time import of `estimate.UpperBound` concrete.
- **Pricing import:** import `internal/cost/pricing.Lookup` (function value) + `pricing.ErrPricingMissing` sentinel. T1's `Gate` field is `pricing pricing.Lookup` (function type). One symbol only.

### TDD test list (named tests from spec §6 T1 — failing-output capture step required)

Per `feedback_tdd_discipline`: implementer writes each test first, runs `go test ./<pkg>/ -run <name> -v`, **captures failing output (paste into PR body)**, then implements. "Tests would have failed" is NOT acceptable.

**B-tier (12 named tests — spec §6 T1 + §7 B1):**
1. `TestGate_NoConfig_AllowsAll` — `safety.cost` unset → `Gate.Evaluate` returns `Allow=true` for any scope. Pins B2 byte-equal MVP-2 default.
2. `TestGate_PerDAGCap_DeniesOverBudget` — `per_dag_usd: 100`; recorded $95; estimate $10 → `Allow=false, Reason="cap_exceeded:dag:..."`.
3. `TestGate_PerDAGCap_AllowsUnderBudget` — recorded $80, estimate $10, cap $100 → `Allow=true`.
4. `TestGate_SoftCapBreached_WarnByDefault` — recorded $80, estimate $5, cap $100, `soft_pct=80`, NO annotation → `Allow=true, SoftCapBreached=true, DowngradeTo=""`. Pins R10 WARN-only default.
5. `TestGate_SoftCapBreached_DowngradeOnlyWithAnnotation` — same + `work_item.annotations.cost.allow_downgrade=true` → `DowngradeTo="claude-haiku-4-5"`. Pins R10 opt-in.
6. `TestGate_PrecedenceMostRestrictiveWins` — `per_dag_usd=100`, `per_operator_usd=50` → denial fires when EITHER cap would breach; e.g. recorded $48 operator + $10 estimate → operator-cap denial even though DAG has $92 headroom. Pins R-A2 precedence.
7. `TestGate_NilTracerFallsBackToGlobal` — `cfg.Tracer == nil` → resolves via `otel.Tracer("internal/cost/gate")`; no panic.
8. `TestGate_EmitsCostEvaluateSpan` — capture spans via test SpanRecorder; one `cost.evaluate` span per Evaluate call; attrs include all 5 `regatta.cost.*` + 3 W6 scope attrs per spec §3.7. A5 rubric pin.
9. `TestReader_BudgetState_SumOverWindow` — insert N substrate `token_spend` rows with `payload.usd` → `BudgetState` SUM matches.
10. `TestReader_BudgetState_PeriodWindow_ExcludesStale` — rows outside `period` window NOT counted.
11. `TestReader_LastReconciliation_LWWPerPeriod` — two `budget_reconciled` rows same `period_start`, latest `written_at` wins per substrate v2 §4.
12. `TestReader_FiltersOnTenantID` — Reader query includes `WHERE tenant_id = ?`; cross-tenant rows NOT counted. Pins R9 W8-forward-fit.

**A-tier (4 named tests — config validator + spec §7 A1):**
13. `TestCUEValidate_CostUnset_PassesAndDefaults` — `safety: {}` loads cleanly; `cost` is nil. Pins B8 backwards-compat.
14. `TestCUEValidate_EmptyCostBlock_Rejected` — `safety: { cost: {} }` → `ErrCostBlockEmpty`. Pins I4.
15. `TestCUEValidate_AllCapsZero_RejectedWithMessage` — every cap field 0 → `ErrCostCapsAllZero` with operator-friendly message naming the omit-to-disable workaround. Pins R7.
16. `TestCUEValidate_SoftPctOutOfRange_Rejected` — `soft_pct=49` → CUE rejects.

**A+-tier (4 named tests — scheduler integration + spec §7 A+1 property-sweep seed):**
17. `TestSchedulerTick_Step06_RunsBeforeReserve` — capture span tree; `cost.evaluate` spans appear between `gate.evaluate` (approval) and `reserveFromSpawnable`-internal spans.
18. `TestSchedulerTick_DeniedWorkItemStaysPlanned` — wi with over-cap estimate → `wi.Status` remains `planned` after Tick; next Tick re-evaluates.
19. `TestSchedulerTick_HookIsNoopWhenCostUnset` — `safety.cost == nil` → `applyCostGovernor` returns input slice byte-equal, no substrate read, no span. Pins I6 zero-overhead.
20. `TestSchedulerTick_SoftCapDowngrade_PassesModelOverride` — soft-cap-breached wi with `allow_downgrade=true` annotation → `reserveFromSpawnable` receives `Request.ModelOverride` set to `Verdict.DowngradeTo`.

Total: **20 named tests** (12 B + 4 A + 4 A+). PR body lists every test name + pasted failing-output excerpt for AT LEAST 6 representative cases (full set carried in the test files themselves).

### PR body skeleton

````
## Summary

Cost-governor Wave 1 T1 ships the gate seam, spend reader, CUE config
extension, validator, and scheduler step-0.6 wiring per
docs/engineer/specs/2026-06-01-cost-governor-design.md §3.2 §3.5 §3.6 §3.7.

- internal/cost/gate/ — Gate concrete type (no interface per spec S5);
  Gate.Evaluate(ctx, Request) (Verdict, error).
- internal/cost/spend/ — Reader.BudgetState (single SELECT with
  json_extract — no app-side loop per spec §3.5); LastReconciliation
  LWW per (tenant_id, period_start).
- contracts/schemas/regatta.v1.cue — additive `cost?: #CostGovernor`
  field on #Safety; every existing field byte-equal (B2).
- internal/config/validate/cost.go — ErrCostBlockEmpty,
  ErrCostCapsAllZero, ErrCostSoftPctOutOfRange (R7 + I4 mitigations).
- internal/orchestrator/scheduler/scheduler.go — applyCostGovernor
  method + step-0.6 insertion between applyApprovalGates and
  reserveFromSpawnable; ≤ 30 LoC net delta; no-op when
  Config.CostGate == nil (I6 zero-overhead invariant).

## Why

MVP-4 W11 P8 wedge Wave 1. Spec §3.2: pre-call deny at the scheduler
tick is the unique enforcement point that catches the *next* claude
session before any shell-setup / MCP-bring-up / system-init costs are
paid. SupervisorLimits remains the kill-already-running fallback per
spec §3.2. T5 folded into T1 per spec §10 S3 — config + scheduler
hook ship in this PR (one cross-PR coordination tax saved).

## Test plan

- [x] B-tier (12): TestGate_NoConfig_AllowsAll, TestGate_PerDAGCap_*,
       TestGate_SoftCapBreached_*, TestGate_PrecedenceMostRestrictiveWins,
       TestGate_NilTracerFallsBackToGlobal, TestGate_EmitsCostEvaluateSpan,
       TestReader_BudgetState_SumOverWindow, TestReader_BudgetState_PeriodWindow_ExcludesStale,
       TestReader_LastReconciliation_LWWPerPeriod, TestReader_FiltersOnTenantID.
- [x] A-tier (4): TestCUEValidate_CostUnset_PassesAndDefaults,
       TestCUEValidate_EmptyCostBlock_Rejected,
       TestCUEValidate_AllCapsZero_RejectedWithMessage,
       TestCUEValidate_SoftPctOutOfRange_Rejected.
- [x] A+-tier (4): TestSchedulerTick_Step06_RunsBeforeReserve,
       TestSchedulerTick_DeniedWorkItemStaysPlanned,
       TestSchedulerTick_HookIsNoopWhenCostUnset,
       TestSchedulerTick_SoftCapDowngrade_PassesModelOverride.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 6 reps>

## Deletion default

Pure-addition Wave 1, but **what got smaller**: spec §10 S3 folds T5
(config + scheduler hook) into THIS PR — net 6 → 5 cost-gov tasks,
one cross-PR coordination tax + one Wave-3 PR eliminated. Phase-D
(post-W11) drops `safety.spend_cap_usd` + `safety.spend_cap_usd_per_day`
legacy fields once operators migrate to `safety.cost.*` — tracking
issue filed pre-merge.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [cost-governor-followup] history estimator opt-in (#NNN; spec §10 S1)
- [cost-governor-followup] pricing_override_path config surface (#NNN; spec §10 S2)
- [cost-governor-followup] per-tenant + per-team budgets W8 cutover (#NNN; spec §2 OOS + R9)
- [cost-governor-followup] legacy safety.spend_cap_usd retirement after migration (#NNN)
- [cost-governor-followup] admin-key-vault integration (#NNN; spec §9 R15)

```release-notes
[FEATURE] cost-governor pre-call deny gate + spend reader + config surface (default-off; MVP-2 byte-equal when unset)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-cost-gov-t1. Branch off main:

  git fetch origin
  git checkout -b feat/cost-gov-t1-gate-reader origin/main

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-cost-governor-design.md.
Read ALL of: §2 (scope, in/out), §3.2 (Gate seam), §3.5 (Reader +
substrate hook), §3.6 (CUE config), §3.7 (OTel attrs), §6 T1 (named
test list), §7 (B/A/A+ rubric), §8 (file-disjoint table row 1), §10 S3
(T5 folded into T1; do NOT split), §9 R7/R9/R10/R14 (misconfig defense,
tenant forward-fit, soft-cap WARN default, cardinality).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (Gate concrete-type-no-interface, Verdict struct
fields, Request struct fields, Reader.BudgetState SQL "single SELECT
with json_extract", scheduler step-0.6 position between
applyApprovalGates and reserveFromSpawnable, CUE additive-only
#CostGovernor shape), STOP and report — do NOT pick an alternative
yourself. Re-spawn the design subagent.

# Scope (exclusive write paths — file-disjoint with T2)

- internal/cost/gate/gate.go
- internal/cost/gate/verdict.go
- internal/cost/gate/scope.go
- internal/cost/gate/gate_test.go
- internal/cost/gate/verdict_test.go
- internal/cost/spend/reader.go
- internal/cost/spend/scope.go
- internal/cost/spend/reader_test.go
- contracts/schemas/regatta.v1.cue              (ADDITIVE ONLY — append `cost?: #CostGovernor` to #Safety + add #CostGovernor block; every existing field byte-equal)
- internal/config/validate/cost.go
- internal/config/validate/cost_test.go
- internal/orchestrator/scheduler/scheduler.go  (insert step-0.6 + applyCostGovernor method; ≤ 30 LoC net delta)
- internal/orchestrator/scheduler/scheduler_cost_gate_test.go  (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT modify internal/cost/estimate/ or internal/cost/pricing/ — those are T2's exclusive scope.
- Do NOT define spend.TokenSpendPayload / spend.BudgetReconciledPayload / spend.CallRecord / spend.RecordCall — those are T3's payload-owner scope (Wave 2). Your Reader unmarshals the substrate payload column via json_extract SQL (per spec §3.5) — no app-side typed-struct dependency.
- Do NOT redefine substrate.KindTokenSpend / KindBudgetReconciled / DefaultTenantID — already exported by substrate/event.go (#224). Import.
- Do NOT change any existing #Safety field in regatta.v1.cue — additive only (B8 invariant).

If you discover a missing seam in an out-of-scope file, STOP and report — file a tracking issue per finding; do NOT edit out of scope (lesson from PR #209: out-of-scope edits get caught at review and need a separate issue).

# Patterns to reuse (do NOT reinvent)

- Approval-gate hook (mirror exactly): internal/orchestrator/scheduler/scheduler.go::Scheduler.applyApprovalGates at line 481. Your applyCostGovernor must have the same filter-in-place shape and same nil-config no-op guard.
- Approval-gate test pattern: internal/orchestrator/scheduler/scheduler_approval_gate_test.go.
- Substrate read: internal/orchestrator/state/substrate/fold.go::Fold for the kind-scoped query shape. Your Reader uses SUM(json_extract(payload_json, '$.usd')) over kind='token_spend' rows in the period window.
- WithTx: internal/orchestrator/state/agents.go::WithTx for a read tx (sql.LevelSnapshot under SQLite WAL).
- Tracer pattern: existing W6 T5 normalization — cfg.Tracer field on Config, fallback to otel.Tracer("internal/cost/gate"). NO WithTracer(...) setter (cardinality + uniform-injection per spec §7 A8).
- CUE schema extension: contracts/schemas/regatta.v1.cue current #Safety block — ADDITIVE only.
- Estimator interface: import internal/cost/estimate.Estimator (T2-owned interface). Do NOT import internal/cost/estimate.UpperBound concrete type; your Gate field is `estim estimate.Estimator` (interface) — caller wires the concrete impl.
- Pricing import: import internal/cost/pricing.Lookup (function value) + pricing.ErrPricingMissing sentinel. Your Gate field is `pricing pricing.Lookup` (function type). Two symbols only.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test below:
  1. Write the test file first.
  2. Run `go test ./<pkg>/ -run <TestName> -v`.
  3. CAPTURE the failing output (paste at least 6 representative samples into PR body's "Failing-test output (TDD capture)" section). "Tests would have failed" is NOT acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or per logical group; squash later if needed).

# Tests to land (20 named; spec §6 T1)

B-tier (12):
1.  TestGate_NoConfig_AllowsAll
2.  TestGate_PerDAGCap_DeniesOverBudget
3.  TestGate_PerDAGCap_AllowsUnderBudget
4.  TestGate_SoftCapBreached_WarnByDefault
5.  TestGate_SoftCapBreached_DowngradeOnlyWithAnnotation
6.  TestGate_PrecedenceMostRestrictiveWins
7.  TestGate_NilTracerFallsBackToGlobal
8.  TestGate_EmitsCostEvaluateSpan
9.  TestReader_BudgetState_SumOverWindow
10. TestReader_BudgetState_PeriodWindow_ExcludesStale
11. TestReader_LastReconciliation_LWWPerPeriod
12. TestReader_FiltersOnTenantID

A-tier (4):
13. TestCUEValidate_CostUnset_PassesAndDefaults
14. TestCUEValidate_EmptyCostBlock_Rejected
15. TestCUEValidate_AllCapsZero_RejectedWithMessage
16. TestCUEValidate_SoftPctOutOfRange_Rejected

A+-tier (4):
17. TestSchedulerTick_Step06_RunsBeforeReserve
18. TestSchedulerTick_DeniedWorkItemStaysPlanned
19. TestSchedulerTick_HookIsNoopWhenCostUnset
20. TestSchedulerTick_SoftCapDowngrade_PassesModelOverride

# Workflow after green

  1. Run `make pre-push-check` — confirm clean. If any lint / build / test fails, fix in this branch — do NOT skip hooks (--no-verify is banned).
  2. Re-run `go test ./internal/cost/... ./internal/orchestrator/scheduler/... ./internal/config/validate/... -v` and confirm every named test green.
  3. Push branch: `git push -u origin feat/cost-gov-t1-gate-reader`.
  4. File the 5 followup tracking issues (`[cost-governor-followup]` history estimator opt-in, pricing_override_path, per-tenant budgets W8, legacy spend_cap_usd retirement, admin-key-vault integration R15) and gather issue numbers. Coordinate with T2's followups — history-estimator + pricing_override_path are filed by whichever PR opens first; the second PR cites the existing issue numbers.
  5. Open PR via `gh pr create --base main --title "feat(cost): T1 gate + reader + config + scheduler step-0.6 hook" --body-file <path>` (NEVER heredoc per feedback_pr_lint_gates). Body MUST cite the 5 followup issue numbers.
  6. Spawn ONE adversarial reviewer subagent (per feedback_adversarial_review + feedback_agent_pr_review + feedback_simplify_reviewer) with hunt list:
     - Gate.Evaluate single-call correctness (no double-evaluation per Tick).
     - Reader SQL injection safety: json_extract scope fields parameterized, NOT string-interpolated.
     - Reader tenant_id filter present in EVERY query (R9 forward-fit).
     - applyCostGovernor returns INPUT slice byte-equal when Config.CostGate==nil (zero alloc, zero span — I6).
     - scheduler step-0.6 ordering: AFTER applyApprovalGates, BEFORE reserveFromSpawnable, BEFORE laneHasCapacity.
     - CUE additive-only: every existing #Safety field present + byte-equal (B8).
     - Validator surfaces operator-friendly error message naming the exact field (R7).
     - Soft-cap downgrade gated on `cost.allow_downgrade=true` annotation; default WARN-only (R10).
     - Span attrs match spec §3.7 list exactly; no extra cardinality (R14, A5).
     - Verdict struct fields exhaustive vs spec §3.2 lines 148-156.
     - Simplification opportunity: can applyCostGovernor reuse the approval-gate filter pattern via a shared helper? If yes, file a tracking issue — do NOT refactor in this PR.
     - Deletion default: PR body cites concrete shrinkage (S3 T5-fold + Phase-D legacy field retirement followup).
     - No AI signatures anywhere in commits / PR body / comments / code (feedback_no_signatures).
     - Reviewer format per finding: status prefix + severity (Risk-tier vs Polish).
  7. Apply reviewer findings inline (or file tracking issue + cite in PR body per feedback_unaddressed_load_bearing).
  8. Re-run `make pre-push-check`; force-push.
  9. Verify CI green (pr-lint, check-release-notes, check-tdd, build, test) BEFORE flipping automerge per feedback_review_before_automerge.
 10. Flip automerge ONLY after reviewer cleared the PR.

# Hygiene

- NO AI signatures anywhere (commits, PR body, comments, code) per feedback_no_signatures. No `Co-Authored-By: Claude` / `Generated with Claude Code` footers.
- Comments discipline per feedback_comments_discipline: WHY not WHAT; test-function godocs ≤ 1 line; sweep on every push.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 6 of the 20 tests (sample is fine — PR body carries full set).
- The 5 followup issue numbers filed (or cited from T2's PR).
- Adversarial reviewer verdict (APPROVE or full findings list with severities).
- One-line diff stat: files changed + LoC added/removed.

Begin now. NEVER pause for user input.
```

---

## §3 Task T2 — UpperBound estimator + Anthropic pricing table + claude-CLI probe

### Scope

- **`internal/cost/pricing/`** — NEW package. Files:
  - `anthropic.go` — `var Anthropic = map[string]Row{...}` per spec §3.8 lines 405-410. Initial rows: `claude-opus-4-7`, `claude-sonnet-4-7`, `claude-haiku-4-5` with `(input, cache_read, cache_creation, output)` USD-per-Mtok rates + `retired_after time.Time` zero value (still active). Source-of-truth: Anthropic pricing page; refresh runbook in Wave 3 T5 doc.
  - `lookup.go` — `Row` struct (spec §3.8 lines 412-418); `Lookup(model string) (Row, error)`; `ErrPricingMissing` sentinel. **Silent-zero is the Portkey trap (spec §3.1) — Lookup MUST hard-fail on unknown SKU.**
- **`internal/cost/estimate/`** — NEW package. Files:
  - `upper_bound.go` — `Estimator` interface (single method: `Estimate(ctx, model string, inputTokens int64, maxTokens int64) (USD float64, err error)`); `Hint` struct (planner-supplied optional `(InputTokens, MaxTokens)` override per spec §3.2 line 174); `UpperBound` concrete type implementing `Estimator` — formula per spec §3.3 line 182: `est_usd = (input_tokens × price_in / 1e6) + (max_tokens × price_out / 1e6)`. Pure function; deterministic; replay-safe; no map iteration, no `time.Now()`, no mutable global state.
  - `probe.go` — `Probe` struct + `NewProbe() (Probe, error)`; runs `claude --count-tokens` capability detection at process start ONCE; if CLI supports it returns ok + a token-counting function value; else returns heuristic-fallback `func(b []byte) int64 { return int64(len(b)/4) * 3/2 }` per spec §3.3 line 190 + R11 mitigation (the `× 3/2` is the I1 50%-safety-margin per spec §6 T2 `TestProbe_HeuristicFallbackAddsSafetyMargin`).

### Prereqs (cite spec sections)

- Spec §2 in-scope item #1 (`internal/cost/estimate/` + `internal/cost/pricing/` sub-packages).
- Spec §3.1 — adopted-OSS scan + Portkey silent-zero trap (hard-error on missing pricing row).
- Spec §3.3 — `upper_bound` chosen strategy + rationale (deterministic, conservative, cold-start-friendly); token-count source (`claude --count-tokens` probe + fallback heuristic).
- Spec §3.8 — pricing table verbatim (Row struct + Lookup function + ErrPricingMissing sentinel + refresh-runbook contract).
- Spec §6 T2 — exhaustive named-test list (6 tests + 2 reviewer-added).
- Spec §9 R1 — pricing-drift defense (retired-SKU handling).
- Spec §9 R11 — heuristic-fallback 50% safety margin.
- Spec §10 S1 — `history` strategy DROPPED (do NOT implement; tracking issue lands at impl-time).
- Spec §10 S2 — `pricing_override_path` DROPPED (do NOT implement override loader; refresh via code change matches Helicone/Portkey/LiteLLM v1).

### Existing patterns to reuse

- **Sentinel error:** stdlib `sql.ErrNoRows` shape. `ErrPricingMissing = errors.New("pricing missing for model")`; callers MUST hard-fail (spec §3.1 Portkey trap).
- **`os/exec` for the probe:** exact-call pattern lives in `internal/orchestrator/spawner/claude.go` (claude CLI invocation) — READ ONLY for env/arg shape, do NOT modify.
- **Property-test fuzzing:** `internal/orchestrator/state/substrate/reducer_property_test.go` (pgregory.net/rapid usage) — use for `TestEstimate_UpperBound_NeverUndercountsActual`.
- **Row struct shape:** spec §3.8 lines 412-418 verbatim — 4 USD rates (float64) + `RetiredAfter time.Time` (zero value = active).
- **No prior cost / pricing / estimator packages in this repo** — T2 is greenfield. Idiomatic Go pure-function design.

### TDD test list (named tests from spec §6 T2 + 2 reviewer-added)

Per `feedback_tdd_discipline`: same capture-failing-output discipline as T1.

**B-tier (4 named tests — spec §6 T2 + §7 B1, B7):**
1. `TestPricing_AllActiveSKUsHavePositiveRows` — every row in `Anthropic` map with `RetiredAfter.IsZero()` has all 4 rates > 0. Pins B7 Portkey-trap defense.
2. `TestPricing_Lookup_UnknownModelErrors` — `Lookup("gpt-4")` returns `ErrPricingMissing`. Hard-fail invariant.
3. `TestEstimate_UpperBound_Deterministic` — same `(input_tokens, max_tokens, model)` → same USD across 100 invocations. Pins replay-safety (W9 forward-fit).
4. `TestEstimate_UpperBound_NeverUndercountsActual` — fuzz: random `(input, max_tokens)`; `UpperBound(...) ≥ ActualCostFromKnownRows(input, output_tokens ≤ max_tokens)`. Pins conservative invariant.

**A-tier (2 named tests — probe robustness + spec §7 A-rubric):**
5. `TestProbe_CountTokensClaudeCLI_DetectsCapability` — probe runs `claude --count-tokens` on a stub; if CLI supports it, probe returns ok; else returns fallback heuristic. No panic on missing-flag claude binary.
6. `TestProbe_HeuristicFallbackAddsSafetyMargin` — heuristic mode active → estimator output ≥ 50% above raw `len(bytes)/4 × p_in + max_tokens × p_out`. Pins R11 / I1 mitigation.

**Reviewer-added (2 named tests — adversarial-pass findings #3 + #4):**
7. `TestPricing_RetiredSKURejected_IfStrictMode` — when `Row.RetiredAfter` is non-zero AND `time.Now() > RetiredAfter`, `Lookup` returns `ErrPricingMissing` (NOT a zero row). Pins R1 pricing-drift defense.
8. `TestEstimate_UpperBound_HintOverridesInputTokens` — when `estimate.Hint.InputTokens` is non-zero, it OVERRIDES the probe-supplied value; `Hint.MaxTokens` similarly. Pins the planner-supplied path used by T1's `Request.EstHint`.

Total: **8 named tests** (4 B + 2 A + 2 reviewer-added). PR body lists every test + pasted failing-output excerpt for AT LEAST 3 representative cases.

### PR body skeleton

````
## Summary

Cost-governor Wave 1 T2 ships the pricing table + UpperBound estimator
+ claude-CLI probe per docs/engineer/specs/2026-06-01-cost-governor-design.md
§3.3 §3.8.

- internal/cost/pricing/anthropic.go — hardcoded Go map of model SKU →
  (input, cache_read, cache_creation, output) USD-per-Mtok + retired_after.
  Refresh runbook in T1's docs/operator/cost-governor.md (Wave 3
  deliverable).
- internal/cost/pricing/lookup.go — Row struct + Lookup function value +
  ErrPricingMissing sentinel. Silent-zero is the Portkey trap (§3.1) — we
  hard-fail.
- internal/cost/estimate/upper_bound.go — Estimator interface (one
  method); UpperBound concrete type implementing it; deterministic +
  replay-safe (spec §3.3).
- internal/cost/estimate/probe.go — one-time `claude --count-tokens`
  capability probe at process start; heuristic fallback adds 50%
  safety margin (R11 / I1 mitigation).

`history` strategy + `pricing_override_path` cut per spec §10 S1 + S2 —
tracking issues filed.

## Why

MVP-4 W11 P8 wedge Wave 1 foundations. Upper-bound is the Waxell-$47K-
trap defense: predicted-mean undercounts in the worst case → spawning
continues past the actual cap → exactly the failure mode this wedge
exists to prevent. Upper-bound is also deterministic (W9 replay-safe)
and cold-start-friendly (no rolling-history dependency on call #1).

## Test plan

- [x] B-tier (4): TestPricing_AllActiveSKUsHavePositiveRows,
       TestPricing_Lookup_UnknownModelErrors,
       TestEstimate_UpperBound_Deterministic,
       TestEstimate_UpperBound_NeverUndercountsActual.
- [x] A-tier (2): TestProbe_CountTokensClaudeCLI_DetectsCapability,
       TestProbe_HeuristicFallbackAddsSafetyMargin.
- [x] Reviewer-added (2): TestPricing_RetiredSKURejected_IfStrictMode,
       TestEstimate_UpperBound_HintOverridesInputTokens.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 3 reps>

## Deletion default

Pure-addition Wave 1, but **what got smaller**: spec §10 S1 + S2 cut
`history` estimator (~80 LoC) AND `pricing_override_path` config surface
(~50 LoC). The S2 cut eliminates R14 (override-tampering surface)
entirely. Refresh-via-code-change matches Helicone / Portkey / LiteLLM v1.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [cost-governor-followup] history estimator opt-in (#NNN; spec §10 S1)
- [cost-governor-followup] pricing_override_path config surface (#NNN; spec §10 S2)
- [cost-governor-followup] Bedrock/Vertex first-class pricing (#NNN; spec §2 OOS)
- [cost-governor-followup] cross-fleet MCP attribution (#NNN; spec §2 OOS)
- [cost-governor-followup] pricing-table-drift autocheck script (#NNN; spec §7 A+5)

```release-notes
[FEATURE] cost-governor pricing table + UpperBound estimator + claude-CLI token-count probe
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-cost-gov-t2. Branch off main:

  git fetch origin
  git checkout -b feat/cost-gov-t2-estimator-pricing origin/main

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-cost-governor-design.md.
Read ALL of: §2 (scope), §3.1 (adopted-OSS scan — Portkey silent-zero
trap), §3.3 (estimator strategy + heuristic safety margin), §3.8
(pricing table), §6 T2 (named test list), §7 (B/A/A+ rubric), §8
(file-disjoint table row 2), §10 S1 + S2 (history estimator +
pricing_override_path DROPPED — do NOT implement), §9 R1 / R11
(pricing-drift defense, heuristic safety margin).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (Row struct fields, Lookup function value
returning Row+error, Estimator interface single-method shape, UpperBound
pure-function formula `(input × price_in / 1e6) + (max × price_out / 1e6)`,
heuristic-fallback × 1.5 safety multiplier, ErrPricingMissing hard-fail
on unknown SKU), STOP and report.

# Scope (exclusive write paths — file-disjoint with T1)

- internal/cost/pricing/anthropic.go
- internal/cost/pricing/lookup.go
- internal/cost/pricing/anthropic_test.go
- internal/cost/pricing/lookup_test.go
- internal/cost/pricing/testdata/                  (optional fixture dir)
- internal/cost/estimate/upper_bound.go
- internal/cost/estimate/probe.go
- internal/cost/estimate/upper_bound_test.go
- internal/cost/estimate/probe_test.go

You MUST NOT touch any other file. Specifically:
- Do NOT modify internal/cost/gate/ or internal/cost/spend/ — those are T1's exclusive scope.
- Do NOT modify internal/orchestrator/scheduler/scheduler.go — T1 owns the step-0.6 hook.
- Do NOT modify contracts/schemas/regatta.v1.cue — T1 owns the additive cost?: field.
- Do NOT implement `history` estimation strategy (spec §10 S1 DROPPED).
- Do NOT implement `pricing_override_path` loader (spec §10 S2 DROPPED).
- T2 has NO scheduler / spawner / config / substrate touch. Pure compute + one-time CLI probe.

If you find that the existing claude-CLI probe seam in internal/orchestrator/spawner/claude.go needs a tweak, STOP and report — file a tracking issue; do NOT edit out of scope.

# Patterns to reuse

- claude CLI invocation pattern: internal/orchestrator/spawner/claude.go (READ ONLY — for env / arg / exec shape).
- Property-test fuzzing: internal/orchestrator/state/substrate/reducer_property_test.go (pgregory.net/rapid pattern). Use for TestEstimate_UpperBound_NeverUndercountsActual.
- Sentinel error pattern: stdlib `sql.ErrNoRows` shape. `var ErrPricingMissing = errors.New("pricing missing for model")`; callers MUST hard-fail (spec §3.1 Portkey trap).
- Row struct shape: spec §3.8 lines 412-418 verbatim — 4 USD rates (float64) + RetiredAfter (time.Time). Zero value of RetiredAfter = active.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test below:
  1. Write the test file first.
  2. Run `go test ./internal/cost/<subpkg>/ -run <TestName> -v`.
  3. CAPTURE the failing output (paste at least 3 representative samples into PR body).
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (8 named; spec §6 T2 + 2 reviewer-added)

B-tier (4):
1. TestPricing_AllActiveSKUsHavePositiveRows
2. TestPricing_Lookup_UnknownModelErrors
3. TestEstimate_UpperBound_Deterministic
4. TestEstimate_UpperBound_NeverUndercountsActual

A-tier (2):
5. TestProbe_CountTokensClaudeCLI_DetectsCapability
6. TestProbe_HeuristicFallbackAddsSafetyMargin

Reviewer-added (2):
7. TestPricing_RetiredSKURejected_IfStrictMode  (R1 pricing-drift defense)
8. TestEstimate_UpperBound_HintOverridesInputTokens  (planner-supplied Hint precedence)

# Workflow after green

  1. Run `make pre-push-check` — confirm clean. NEVER skip hooks (--no-verify is banned).
  2. Re-run `go test ./internal/cost/estimate/... ./internal/cost/pricing/... -v` and confirm every named test green.
  3. Push branch: `git push -u origin feat/cost-gov-t2-estimator-pricing`.
  4. File the 5 followup tracking issues (`[cost-governor-followup]` history estimator opt-in, pricing_override_path, Bedrock/Vertex first-class, cross-fleet MCP attribution, pricing-table-drift autocheck script). Coordinate with T1's followups — history-estimator + pricing_override_path are filed by whichever PR opens first; cite numbers in both bodies.
  5. Open PR via `gh pr create --base main --title "feat(cost): T2 UpperBound estimator + Anthropic pricing table + claude-CLI probe" --body-file <path>` (NEVER heredoc per feedback_pr_lint_gates).
  6. Spawn ONE adversarial reviewer subagent (per feedback_adversarial_review + feedback_agent_pr_review + feedback_simplify_reviewer) with hunt list:
     - Lookup returns ErrPricingMissing for unknown SKU; silent-zero IMPOSSIBLE (Portkey trap B7).
     - Anthropic map: every active row has 4 positive rates (no zeros).
     - UpperBound formula correctness: `(input × p_in + max × p_out) / 1e6`; no floor; no rounding leak; division order avoids overflow at high token counts.
     - UpperBound determinism: no map iteration, no time.Now() reference, no global mutable state — fully replay-safe (W9 forward-fit).
     - UpperBound never-undercounts invariant: actual output ≤ max_tokens by definition, so input × p_in + max × p_out ≥ input × p_in + actual_out × p_out for all actual_out ∈ [0, max].
     - Probe: missing claude binary → no panic; missing --count-tokens flag → fallback heuristic; heuristic adds ≥ 50% safety margin (R11).
     - Retired SKU handling: when RetiredAfter is non-zero and time.Now() > RetiredAfter, Lookup MUST fail (not silently return a zero row).
     - Estimator interface: single method, no extension points — kept as interface for T1's mockability per spec §3.2; not a deviation from S5 "concrete-type-no-interface" (the impl is concrete; the seam is interfaced specifically to let T1 mock).
     - Simplification opportunity: can probe be ditched in favour of always-heuristic? Spec §3.3 mandates probe-first — file a tracking issue if you want to argue otherwise; do NOT cut from this PR.
     - Deletion default: PR body cites S1 + S2 cuts.
     - No AI signatures anywhere (feedback_no_signatures).
     - Reviewer format per finding: status prefix + severity (Risk-tier vs Polish).
  7. Apply reviewer findings inline (or file tracking issue + cite in PR body per feedback_unaddressed_load_bearing).
  8. Re-run `make pre-push-check`; force-push.
  9. Verify CI green BEFORE flipping automerge (feedback_review_before_automerge).
 10. Flip automerge ONLY after reviewer cleared the PR.

# Hygiene

- NO AI signatures anywhere (commits, PR body, comments, code) per feedback_no_signatures. No `Co-Authored-By: Claude` / `Generated with Claude Code` footers.
- Comments discipline per feedback_comments_discipline: WHY not WHAT; test-function godocs ≤ 1 line; sweep on every push.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 3 of the 8 tests.
- The 5 followup issue numbers filed (or cited from T1's PR).
- Adversarial reviewer verdict (APPROVE or full findings list with severities).
- One-line diff stat: files changed + LoC added/removed.

Begin now. NEVER pause for user input.
```

---

## §4 After Wave 1 — handoff to Wave 2 (T3 + T4) and Wave 3 (T5)

Wave 1 ships the **read side** of the cost-governor: a gate that queries substrate `token_spend` events but does NOT write them, and an estimator + pricing table that price upper-bound costs but do NOT record actuals. The data plane is closed by Wave 2 and the operator doc by Wave 3.

**Wave 2 — Data plane (T3 + T4 in parallel per spec §10 Wave 2):**
- **T3 — Spawner post-stream `token_spend` emission.** Owns `internal/cost/spend/{writer,payload}.go` (typed `TokenSpendPayload` + `BudgetReconciledPayload` + `CallRecord` + `RecordCall(ctx, tx, CallRecord) error`); adds entries to substrate `validate.go` dispatch table via T-S1's `RegisterPayloadValidator` (open-extensible — no T-S1 file edit); appends EXACTLY ONE line (`cost.spend.RecordCall(...)`) to W6 T4's `internal/orchestrator/spawner/genai.go` `result`-event handler.
- **T4 — Reconciler + Anthropic Cost API client (preferred) + Usage API fallback.** Owns `internal/cost/reconcile/{tick,client,window,backoff}.go` + canned JSON fixtures. Reads `spend.BudgetReconciledPayload` (T3-owned) + `gate.Verdict` (T1-owned). Hourly cron + 2-min jitter + exponential backoff + `retry-after` honour + drift detector + alert dedup.

**Shared-primitive owner (`feedback_shared_primitive_owner`):** T3 OWNS the typed payload structs `spend.TokenSpendPayload`, `spend.BudgetReconciledPayload`, `spend.CallRecord`, plus `spend.RecordCall` AND the substrate `validate.go` dispatch additions for `KindTokenSpend` + `KindBudgetReconciled` validators. T4 imports `spend.BudgetReconciledPayload` (T3-owned) and `gate.Verdict` (T1-owned) — never redefines either. T1's Reader uses `json_extract` SQL specifically to keep Wave 1 independent of T3's payload struct definitions; once T3 ships, T1's Reader MAY (Wave 3 polish) switch to the typed struct via a follow-up PR.

**Wave 3 — Operator doc (T5 dispatched alone per spec §10 Wave 3):**
- `docs/operator/cost-governor.md` covering env-var contract, scope precedence (most-restrictive-wins), drift-alert reading, soft-cap WARN-only semantics + opt-in downgrade, pricing-refresh runbook, OTel cardinality recommendation (R14 mitigation citing W6 R6 + `OTEL_TRACES_SAMPLER`), Cost API vs Usage API fallback semantics, dashboard query examples (Honeycomb / Grafana / Jaeger per A+4).
- `examples/full/regatta.yaml` adds a commented-out demo `cost:` block (operator copy-pastes to opt-in).

**Wave 1 followup-issue list (filed by T1 + T2 PR bodies) becomes Wave 2 + Wave 3 input:**
- `[cost-governor-followup] per-tenant + per-team budgets` → reads at W8 RBAC wave + 1.
- `[cost-governor-followup] history estimator opt-in (S1)` → optional follow-on; replay-safety constraint must be retained.
- `[cost-governor-followup] pricing_override_path config surface (S2)` → optional follow-on; threat-model the override-tampering surface (R14-equivalent) before enabling.
- `[cost-governor-followup] admin-key-vault integration (R15)` → reads at W8 secret-management wave.
- `[cost-governor-followup] pricing-table-drift autocheck (A+5)` → Wave 3 polish; nightly CI script.

**Boot-prompt refresh (`feedback_boot_prompt_per_wave_refresh`):** after Wave 1 merges, refresh `docs/engineer/autonomous-session-prompt.md` to cite T1 + T2 PR numbers in the cost-gov status section. Skip review (docs-only).

**Roadmap pre-fetch (`feedback_roadmap_pre_fetch`):** while Wave 1 is in-flight, pre-stage Wave 2 design notes (T3 + T4 already-spec'd; main session can dispatch the Wave 2 plan-subagent before Wave 1 merges if Wave 1 dispatches drain headroom).

---

## Adversarial-review pass (applied inline)

Reviewer subagent red-teamed this plan; 13 findings + fixes applied (no unresolved Risk-tier items remain — `feedback_adversarial_review` clearance).

1. **File-overlap risk between T1 and T2.**
   *Finding:* Both tasks add files under `internal/cost/`. Sub-package boundaries enforced?
   *Fix applied:* §1 table now lists each file path explicitly (no glob). T1 writes to `internal/cost/gate/` + `internal/cost/spend/` only; T2 writes to `internal/cost/estimate/` + `internal/cost/pricing/` only. Zero overlap verified by grep of file paths between the two rows.

2. **Shared-primitive seam: who owns `estimate.Estimator` interface?**
   *Finding:* T1's Gate has field `estim estimate.Estimator` — interface ownership direction matters; the import is one-way.
   *Fix applied:* T2 OWNS the interface (lives next to the concrete `UpperBound` impl in `internal/cost/estimate/upper_bound.go`). T1 imports `internal/cost/estimate.Estimator`. Documented in §1 cross-task seam contracts + both dispatch prompts. **This is the only T1 → T2 cross-import.** T2 has zero T1 imports.

3. **Retired-SKU handling not pinned by spec §6 T2.**
   *Finding:* Spec §3.8 line 417 documents `RetiredAfter` but spec §6 T2 has no test for "what happens when an active model retires while the table still has its row." Latent invariant.
   *Fix applied:* Added `TestPricing_RetiredSKURejected_IfStrictMode` to T2's test list (reviewer-added tier). Documented in §3 TDD list + dispatch prompt. Pins R1 pricing-drift defense.

4. **`Hint.InputTokens` precedence not pinned by spec §6 T2.**
   *Finding:* Spec §3.2 line 174 mentions `EstHint` (planner-supplied override) but no spec §6 test pins the precedence rule between Hint and probe-supplied values.
   *Fix applied:* Added `TestEstimate_UpperBound_HintOverridesInputTokens` to T2's test list (reviewer-added tier).

5. **Does T1 stub Estimator or import the real one?**
   *Finding:* Dispatch prompt could be read either way. If T1 stubs Estimator in tests but doesn't import T2 in prod code, who wires the concrete `UpperBound` at startup?
   *Fix applied:* Cross-task seam contracts clarify: T1's `Gate` field is typed `estimate.Estimator` (interface), populated by the caller (orchestrator startup at `cmd/regatta-serve` — already constructs the Scheduler). T1's gate-package tests use an interface mock. The orchestrator startup wiring adds ONE line to instantiate `gate.NewGate` with `&estimate.UpperBound{}` — counted in T1's ≤ 30 LoC scheduler delta budget.

6. **Does T2 need to know about substrate?**
   *Finding:* T2's only DB-side dependency is the pricing table (in-memory). The estimator is a pure function. Dispatch prompt should say so explicitly to prevent over-scoping.
   *Fix applied:* T2 dispatch prompt now states: "T2 has NO scheduler / spawner / config / substrate touch. Pure compute + one-time CLI probe."

7. **Migration number lock (`feedback_migration_number_lock`).**
   *Finding:* Plan must declare migration-number pinning to prevent parallel-PR collision.
   *Fix applied:* Wave overview now states: "**NO new migration in Wave 1.** Cost data lives in substrate events; migration #0007 is reserved for a future spec." Confirmed by reading spec §3.5: payloads ride existing substrate `events.kind` column — no new table. Both T1 and T2 are migration-free. Wave 2's T3 adds dispatch-table entries (open-extensible via T-S1's `RegisterPayloadValidator`) — still migration-free.

8. **Soft-cap downgrade target hardcoded?**
   *Finding:* `TestGate_SoftCapBreached_DowngradeOnlyWithAnnotation` expects `DowngradeTo="claude-haiku-4-5"`. Config-derived or hardcoded?
   *Fix applied:* Spec §3.6 has no downgrade-target config field in Wave 1; the downgrade target IS hardcoded as a Sonnet→Haiku mapping per spec §3.2. Documented inline in T1's scope as "downgrade target hardcoded; configurable downgrade-target table is a Wave 3+ polish (no spec §6 test against config-driven downgrade)." Filed as a Wave-3 polish followup.

9. **Simplification hunt (`feedback_simplify_reviewer`).**
   *Finding:* Both dispatch prompts must include "simpler way?" hunt items, not just risk-tier findings.
   *Fix applied:* Both dispatch prompts' reviewer hunt lists now include explicit "Simplification opportunity:" bullets — T1's looks for generic-able filter pattern reuse vs approval-gate; T2's looks for probe-ditch possibility (but spec §3.3 mandates probe-first, so any cut requires a follow-up issue, not in-PR removal).

10. **Followup-filing universality (`feedback_followup_filing_universal`).**
    *Finding:* Each PR body skeleton must file load-bearing followups pre-merge with explicit issue-number slots.
    *Fix applied:* Both PR body skeletons enumerate exact `[cost-governor-followup]` issue titles + placeholder `#NNN` slots. T1 files 5 (history estimator, pricing_override_path, per-tenant budgets W8, legacy spend_cap_usd retirement, admin-key-vault); T2 files 5 (history estimator, pricing_override_path, Bedrock/Vertex, cross-fleet MCP, pricing-drift autocheck). history-estimator + pricing_override_path appear in BOTH lists — the first PR to merge files the canonical issue; the second PR cites the existing number (coordinated by main session).

11. **Concurrency cap (`feedback_session_limit_dispatch`).**
    *Finding:* Plan must declare peak parallelism.
    *Fix applied:* Wave overview states: "2 parallel implementers — well under the 3-4 ceiling. No risk of session-limit cascade."

12. **A7 rubric ≥ 13 issues.**
    *Finding:* Spec §7 A7 requires ≥ 13 `[cost-governor-followup]` issues across the whole wedge.
    *Fix applied:* §4 handoff lists the 5 high-priority Wave 1 followups; the other 8+ (per-tenant budgets, Stripe webhook, predictive forecasting, mid-DAG kill+compensation, cache-aware budgeting, cross-fleet MCP attribution, Bedrock pricing, Pricing API auto-flag, backfill recipe, progress-gated renewal, spawner reconciliation outbox) get filed by Wave 2 + Wave 3 dispatches. Total cumulative target satisfied at wedge close.

13. **AI-signature guard (`feedback_no_signatures`).**
    *Finding:* Plan must guard against implementer subagents accidentally adding `Co-Authored-By` / `Generated with Claude Code` footers.
    *Fix applied:* Both dispatch prompts carry an explicit `# Hygiene` block citing `feedback_no_signatures` + `feedback_comments_discipline`. Plan itself contains zero AI signatures (verified by self-grep).

---

_Plan authority: this plan is a dispatch artifact only. The main session copy-pastes the §2 / §3 dispatch prompts into Agent tool calls. NO implementation, NO commit from this file. Wave 1 dispatch is unblocked the moment substrate Wave 1 (T-S1 ✅ #224, T-S2, T-S3) is fully merged to main per spec §depends-on; W6 T1 / T2 / T5 prereqs are already merged._
