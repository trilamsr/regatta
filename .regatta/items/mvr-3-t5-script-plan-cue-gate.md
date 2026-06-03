---
id: MVR-3-T5
title: script-plan CUE gate - validate LLM-authored DAGs at L0-L6 before runtime accepts
lane: customer
kind: program
status: planned
gate: mvr-3-entry (5 paying customers OR 1 strategic logo OR persona-C inbound)
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md §4 MVR-3-T5 + §14 DW-superset Wave B piece 3
dependencies: MVR-2-T6 (substrate bridge for script-runs), W5 cost-cap (L6)
linked_artifact: docs/engineer/specs/2026-06-02-mvr-3-t5-script-plan-cue-gate.md
---

Source brief: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-3-T5 + §14 DW-superset wedge integration (piece 3, Wave B).

Phase-MVR-3 capstone of the DW-superset wedge. LLM emits a script plan DAG (Claude-Code Dynamic-Workflows style); regatta validates it via CUE schema plus the L0-L6 gate chain before the runtime accepts it. Catches LLM hallucination + plan-shaped injection at the gate layer, not at exec time.

## Scope

In:

- Accept JSON/YAML DAG from LLM (`internal/scriptplan.Validate`).
- CUE schema `contracts/schemas/script-plan.v1.cue` enforces shape + closed action enum + cardinality bound.
- L0-L6 gates wired: L0 ref/merge, L1 cost-estimate, L2 schema, L3 cycle, L4 LLM second-opinion, L5 capability allowlist, L6 cost-cap (reuses W5).
- Emit `script_plan_accepted{plan_id}` or `script_plan_rejected{rule, reason}` to substrate.
- `regatta script validate <plan.json>` CLI - exit 0 OK, non-zero with explicit rule-name on fail.
- Hook into `internal/orchestrator/scheduler/scheduler.go` so script-mode dispatches call the validator before persist.

Out (Phase X, explicit reopen-trigger):

- Plan fuzzing harness (followup issue at dispatch time).
- Multi-tenant per-tenant plan quota (reopen on first persona-C inbound).
- v2 schema migration (reopen on first breaking change need).

Self-host filter: script-mode is internal velocity (autonomous loop writes its own plans) before it is customer-facing. The gate ships when the autonomous loop itself dispatches a script-plan; until then, the validator runs in shadow mode and rejections are logged-only.

## Approach

- CUE language (Apache 2.0) - schema + constraint validator. Reuses the established `contracts/schemas/regatta.v1.cue` pattern (#467 + `internal/config/validate/load.go`).
- L0-L6 gate framework already in place under `internal/gates/` and `contracts/schemas/gate_result.go`.
- Substrate emit uses the existing `kind=fact` event topic from MVR-2-T6 (`workflow.<run_id>.<step>`).
- L4 second-opinion reuses the S2-T2 adversarial-L4-gate prompt loader pattern (`internal/gates/l4/prompts/`).

## Dispatch order

W5 cost-cap (L6 dependency, in flight) → MVR-2-T6 substrate bridge (event topic) → L4 gate (S2-T2 reviewer infra) → this item.

## Acceptance criteria

- [planned] c1: `internal/scriptplan.Validate()` returns `(*Plan, error)`; CUE schema `contracts/schemas/script-plan.v1.cue` enforces shape + closed action enum + cardinality; `cue vet` exits 0.
- [planned] c2: L0-L6 gates wired in first-fail-wins order matching spec §4; all 7 rule-coded exit codes (1-7) emitted by `regatta script validate <plan.json>` CLI.
- [planned] c3: Substrate emits `script_plan_accepted{plan_id}` and `script_plan_rejected{rule, reason}` on the `workflow.<run_id>.<step>` topic from MVR-2-T6.
- [planned] c4: Scheduler hook in `internal/orchestrator/scheduler/scheduler.go` calls the validator before persist; shadow-mode default with enforce-mode behind config flag (cutover criteria per spec §8 R14).
- [planned] c5: p95 latency <1s for 100-step plan including L4 call; reviewer-subagent cleared per `feedback_review_before_automerge` before automerge.
