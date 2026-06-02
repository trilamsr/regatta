# regatta — architecture simplification pass (collapse-before-extend)

_Author: design subagent, 2026-06-01. Source-of-truth: parallel adversarial review of current regatta architecture + companion research-mode synthesis review. Cited reviewer findings are recorded in the consolidated session transcript; only the actionable headlines land here._

## 0. TL;DR

Regatta has ~19,000 prod LoC, ~26,000 test LoC, 31 internal packages, 9 in-flight specs (~4,900 lines of design markdown), 6 migrations, and 4 parallel schema-encoding layers (CUE + JSON Schema + Go types + substrate-payload-schemas). The substrate spec (`2026-06-01-unified-substrate-design.md`) catches that 4 wedges had each invented their own history table and collapses them; that same discipline applied to the rest of regatta deletes ~1,500 LoC of code + ~5,000 lines of speculative spec markdown + 6 interfaces with one implementation each. This brief is the deletion-default verdict applied to regatta itself.

Three rules govern this pass:

1. **No new internal package without two concrete consumers planned.** Single-consumer abstractions are anti-pattern per `PRINCIPLES.md` §2.
2. **No new swap-out adapter contract without a customer ask on file.** The adapter-contracts spec exhibits the failure mode this rule prevents.
3. **Substrate Wave 1 is the hard gate for MVP-3 cutover.** Any wedge in flight that reads `approvals` / `work_item_outputs` / `work_item_edges` directly must rebase onto `substrate_events` before merge.

## 1. Where regatta IS

- **Code surface**: 19,047 prod LoC, 26,140 test LoC, 31 internal packages, 9 subcommands.
- **Spec surface**: 12 spec files under `docs/engineer/specs/` (substrate, W6 OTel, W7 UI, W7 Wave-2, W8 OPA, W9 replay-diff, W9 Temporal-vs-bespoke, W10 Sigstore, W11 blackboard, W12 billing, adapter-contracts, cost-governor). At least 9 are in-flight design specs; the rest are decision documents that belong under `docs/rfcs/` after this pass.
- **Storage**: 6 migrations (0001-0004 shipped; 0005 reserved for W6 trace_id; 0006 reserved for substrate).
- **Interfaces**: 14 declared internal interfaces; counted manually, ~7 have a single concrete implementation today.
- **Schema encodings**: 4 parallel sources of truth (CUE for operator config, JSON Schema for wire format, hand-maintained Go structs, substrate-payload-schemas).
- **Gate stack**: L0 shipped (15/200 fixture corpus target); L1-L5 deferred; custom security gate skeleton (319 LoC, 3 of 4 declared tools not wired).

## 2. Where the drift accumulated

### 2.1 Substrate retroactively rewrites the MVP-1 + MVP-2 data layer

The substrate spec ships a single signed event log replacing `work_item_outputs` (mig 0003), `approvals` (mig 0004), `work_item_edges`, and per-agent `events` tables. Phase D ("Deprecate legacy tables") is explicitly NOT in the substrate spec's deliverable. W7 operator UI Wave 7.0-7.3 (14 tasks) currently dispatches against the **legacy** tables and carries a §3.10 "Data sources post-substrate — reconciliation" section to handle the discrepancy. Every PR shipped against the legacy read path is a PR that must be touched again at substrate cutover.

**Action:** Pause W7 + cost-gov + approval-UI dispatch until substrate Wave 1 lands. Shadow-assert until convergence. **Then delete `work_item_outputs.go` + `approvals.go` + `work_item_edges.go` + migrations 0003 + 0004.** ~1,500 LoC + 2 migrations + every "reconciliation" sub-section in W7 / cost / approval specs.

### 2.2 Adapter-contracts spec proposes 5 swap-out adapters with 0 second consumers

`2026-06-01-adapter-contracts-design.md` (~4,000 lines) proposes contracts for OTel exporter, OPA RBAC, Sigstore signer, Stripe billing, and LLM gateway — with hosted-vs-in-binary parallel impls, `sql.Register`-style registration, capability-detection, hot-swap, lifecycle, and per-adapter contract tests. **All five adapters have zero second consumers today:**

- OTel exporter: stdlib OTLP is the only impl planned per W6 spec §3.1.
- OPA RBAC: does not exist yet; W8 deferred.
- Sigstore: HMAC is the only signer shipping (`contracts/schemas/sign.go`).
- Stripe billing: MVP-4 W12.
- LLM gateway: one provider exists (`internal/program/provider_anthropic.go:1`); LiteLLM is P2.8, post-MVP-3.

This is the textbook `PRINCIPLES.md` §2 violation — pre-files contract tests for adapters with zero implementers.

**Action:** **Delete the adapter-contracts spec.** Replace with one paragraph in `PRINCIPLES.md` §2: "When an adapter needs swap-out, mint the interface in the caller's package; do not pre-file contract tests for hypothetical backends." Save: 4,000 lines of spec + 5 `internal/adapters/*` packages never created + 5 contract tests never written.

### 2.3 Parallel package hierarchies for single-consumer features

| Tree | Sub-packages | Consumers per package | Action |
|---|---|---|---|
| `internal/orchestrator/{adapter,adaptersync,lockfile}` | 3 (543 + 202 + 126 LoC) | 1 each (all consumed only by `orchestrator.go`) | Flatten into `internal/orchestrator/` |
| `internal/cost/{estimate,gate,pricing,reconcile,spend}` | 5 (67 + 190 + 293 + 352 + 954 LoC = 1,856 total) | 1 feature surface | Flatten into `internal/cost/` |
| `internal/obs/` + `internal/obs/otel/` | 2 | 1 consumes the other | Flatten into `internal/obs/` |

**Action:** Flatten all three trees. ~10 import statements per consumer dropped; 8 sub-packages → 3 packages.

### 2.4 Duplicate interfaces with one implementation each

| Interface | Declared in | Implementations | Action |
|---|---|---|---|
| `Estimator` | `internal/cost/gate/gate.go:52` AND `internal/cost/estimate/upper_bound.go:22` | Gate-side declares the seam to avoid an import cycle into `internal/cost/estimate`; estimate-side is the implementation. Two declarations of the same noun across sibling packages. | **Audit during the cost-package flatten:** if the flatten removes the import-cycle constraint, collapse the two declarations into one. If the constraint survives, keep both and document the seam-vs-impl split inline. Decision is a design-subagent task, not a main-thread pick. |
| `Keyring` | `internal/canon/approval_token.go` | One impl (HMAC), one consumer | Delete interface; concrete type. |
| `ModelClient` | `internal/program/planner.go:246` | One impl (`provider_anthropic.go`) | Delete interface; concrete type. |
| `InteractiveNotifier` | `internal/gates/approval/notify.go` | Zero impls since MVP-2 W1; W7.0 ships first caller — **if W7 slips, delete the interface entirely** | Track via tracking issue per `feedback_unaddressed_load_bearing`. |

### 2.5 Three-way planner fork (abandoned refactor)

`internal/program/planner.go` + `internal/program/planner_v2.go` + `internal/program/planner_stub.go`. Three implementations of the same concept = abandoned refactor smell.

**Action:** Pick one. Delete the other two. The pick-one decision is a design-subagent task per `feedback_spec_pattern_authority`; main thread does not guess.

### 2.6 Spec sprawl (9 in-flight, 3 should be live)

- **Already shipped (delete the in-flight copy if it duplicates an RFC):** approval-gates (RFC-0003), conditional-DAG (RFC-0002), planner-as-DAG (RFC-0001).
- **Active or next-wave (keep):** substrate, W6 OTel, W7 UI (post-substrate rebase).
- **Decision documents — move to `docs/rfcs/000N-*.md`:** W9 Temporal-vs-bespoke (this is a decision doc, not an implementation spec; the decision was Option D — defer to MVP-4 — but the doc still lives under `specs/`).
- **Delete:** adapter-contracts (per §2.2).
- **Park until prerequisites land:** W11 blackboard, W10 Sigstore, W12 billing (MVP-4 candidates, not active).

**Action:** Promote the 3 live specs to canonical `docs/engineer/specs/`; demote 3 specs to `docs/rfcs/`; delete the adapter-contracts spec; archive the duplicates that already live under RFCs.

### 2.7 Schema-encoding drift (4 sources of truth)

Today the same concept (e.g. `WorkItem`) is encoded in:

- CUE — `contracts/schemas/regatta.v1.cue` (operator-facing config validation)
- JSON Schema — `contracts/schemas/work_item.schema.json` (wire format)
- Go struct — `internal/orchestrator/state/work_items.go` (hand-maintained)
- Substrate-payload-schemas (forthcoming under substrate Wave 1) — yet another source

CUE + JSON Schema are justified (operator-facing vs wire-format = different consumers). Go structs as a third hand-maintained encoding is coordination debt, not a "delete one" problem.

**Action:** Add Go-type codegen from JSON Schema (one PR; one CI gate). Hand-maintained Go structs become generated. Substrate-payload-schemas live under `contracts/schemas/substrate/` AS the JSON Schema source — substrate is not a 4th source of truth, just the per-`kind` subset of one source.

## 3. Out of scope: MVP-0 "ship-the-binary minimum" proposal

An earlier draft of this brief included a "MVP-0: ship-the-binary minimum" section proposing to delete or defer ~10k LoC of already-shipped work (`internal/program/`, `internal/cost/`, `internal/gates/approval/`) to land the smallest end-to-end binary first. That proposal touches reversibility-before-optionality (`PRINCIPLES.md` §2) and code-deletion semantics that are independent of the research-mode unblock this brief drives. It belongs in its own brief with its own adversarial review.

**Status:** deferred to a future `2026-MM-DD-mvp-0-binary-minimum.md` brief if and when a maintainer commits to the retro-prune work. NOT in scope for this simplification pass.

The collapse work in §2 (substrate cutover + sub-package flatten + duplicate-interface resolution + three-way planner-fork pick-one + adapter-contracts spec delete + spec promote/demote/delete + schema-encoding-drift codegen) is independent of MVP-0 and stands on its own.

## 4. Re-sequenced MVP-3 (substrate-first)

Current state: W6 + W7 + W8 + W9 + substrate + cost-gov dispatching in parallel.

**Proposed:**

1. **Substrate Wave 1 first.** Hard gate on everything else.
2. **W6 OTel** after substrate (current dependency order anyway).
3. **W7 operator UI** after substrate cutover (NOT during). Eliminates the W7 §3.10 "reconciliation" complexity.
4. **W8 OPA** after substrate. Authorizer interface was already designed pre-W8 in W7 spec §3.6.4.
5. **Cost-gov Wave 3** as currently scoped.
6. **W9 take Option D** — defer to MVP-4, or delete entirely if substrate covers the durable-history use case. The `DurableHistory` interface had zero second consumer; substrate IS the durable history.

## 5. Counter-cases (defended complexity — do NOT touch)

- **L0 gate fixture corpus** + the L0-L6 numbered gate-stack ordering. P1 (deterministic-before-AI) and P3 (trusted-instructions-from-main-only) depend on gate *order*, not on a flat severity tag. **Do NOT flatten the numbered gate stack into a registry with tags.**
- **`internal/orchestrator/state/`** (2,173 LoC) is the right size for what it does — schema migrations, agent state machine, lock heartbeat, work-item lifecycle. The "many files" feel is SQL boilerplate; each table has its own `.go` and that is correct. The substrate sub-package (1,058 LoC) is the duplication risk, not the parent.
- **`internal/program/`** at 3,035 LoC houses planner + handoff + route + CEL decider + edge evaluator + reachability + brief loader. Each is a distinct concept; the file count is high but cohesion within `program/` is real. Re-examine after substrate cutover, not before.
- **Two-language schema split** (CUE for operator config + JSON Schema for wire format) is justified — different consumers. The fix is Go-type codegen, not "delete one".
- **`make check`** 60-second budget is load-bearing for agent velocity. Do not touch.

## 6. Roadmap impact (summary)

| Roadmap layer | Change |
|---|---|
| **MVP-1** (existing) | Unchanged scope. |
| **MVP-2** (existing) | Unchanged scope. |
| **MVP-3** (re-sequenced) | Substrate Wave 1 first → W6 OTel → W7 UI (post-substrate) → W8 OPA → Cost-gov Wave 3. W9 takes Option D. |
| **MVP-4** (existing) | W10 Sigstore + W11 blackboard + W12 billing. Unchanged scope. |
| **MVR-1** (new — post-MVP-4, optional) | Research-mode (per `2026-06-01-research-mode-extension-design.md`). Gated on substrate + W8 + W10 + W11 + this simplification pass. |

## 7. Bootstrap roadmap collapse (regatta-builds-regatta)

The original 7-stage bootstrap roadmap (reactive-janitor → doc/test surgeon → spec-implementer → gate-extender → spec-drafter → roadmap-proposer → self-modifying scheduler) collapses to **3 stages** that fit inside the existing autonomous-session-prompt flow, NOT as separate specs:

- **Stage 0** — regatta files own followup issues via a dedicated `regatta-bot` GitHub identity with `issues:write` only. Requires the TCB-enforcement L0 sub-gate with a written threat model attached. (Until that threat model exists, Stage 0 does not ship.)
- **Stage 1** — regatta runs doc-fix PRs + TDD regression tests against existing bug reports.
- **Stage 2** — regatta dispatches a wave per human-ratified spec, opens N parallel PRs with reviewer scorecards.

**Stages 3-6 are deleted.** Self-modifying scheduler violates Trap P11 (agent artifact pipelines as attack surface); spec-drafter + roadmap-proposer are hypothetical-future scaffolding forbidden by `feedback_drop_ceremony`.

## 8. Concrete next steps (this brief is actionable, not aspirational)

1. **File tracking issues** for each deletion candidate in §2 per `feedback_unaddressed_load_bearing`. One issue per package flatten, one per interface delete, one per spec promote/delete.
2. **Pause W7 + cost-gov + approval-UI dispatch** in the autonomous-session boot prompt. Add to `docs/engineer/autonomous-session-prompt.md` PRIORITY section.
3. **Land substrate Wave 1** as currently scoped.
4. **Shadow-assert** legacy + substrate writes to convergence on a representative DAG.
5. **Delete legacy tables + migrations 0003 + 0004** at cutover. One PR per table.
6. **Flatten the three sub-package trees** (orchestrator-adapter family, cost family, obs family). One PR per tree.
7. **Resolve duplicate interfaces** per §2.4. Design-subagent decides the pick-one location.
8. **Resolve three-way planner fork** per §2.5. Design-subagent decides which planner survives.
9. **Delete the adapter-contracts spec.** Replace with one paragraph in `PRINCIPLES.md` §2.
10. **Promote shipped-already specs to RFCs**; demote decision docs from `specs/` to `rfcs/`; delete duplicates.
11. **Add Go-type codegen from JSON Schema.** One PR; one CI gate.
12. **Add the new memory entries** `feedback_collapse_before_extend` and `feedback_research_mode_blockers` to `MEMORY.md`.
13. **Boot-prompt rule additions:**
    - "No new internal package without two concrete consumers planned."
    - "No new swap-out adapter contract without a customer ask on file."
    - "Substrate Wave 1 is the hard gate for MVP-3 cutover."

## 9. What got smaller

| Change | Smaller by |
|---|---|
| Delete adapter-contracts spec | ~4,000 lines of spec markdown; 5 `internal/adapters/*` packages never created; 5 contract tests never written |
| Delete legacy `work_item_outputs.go` + `approvals.go` + `work_item_edges.go` + mig 0003 + 0004 (at substrate cutover) | ~1,500 LoC + 2 migrations + every "reconciliation" sub-section in W7 / cost / approval specs |
| Flatten `orchestrator/{adapter,adaptersync,lockfile}` + `cost/{5 sub-packages}` + `obs/{2 sub-packages}` | 8 sub-packages → 3 packages; ~10 import statements per consumer dropped |
| Pick-one between three-way planner fork | 2 of 3 planner files deleted |
| Resolve `Estimator` seam-vs-impl split (audit during cost-package flatten; may collapse to one declaration if the import-cycle constraint survives the flatten) | Up to 1 interface declaration collapsed, contingent on the import-graph audit |
| Delete `Keyring` + `ModelClient` + (probably) `InteractiveNotifier` interfaces | 3 single-impl interfaces collapsed to concrete types |
| Promote/delete spec duplicates (approval, conditional-DAG, planner-as-DAG already in RFCs) | ~1,000 lines of duplicated markdown |
| Demote W9 Temporal-vs-bespoke from `specs/` to `rfcs/0004-*.md` | Spec count 9 → 6 (or fewer after deletes) |
| Bootstrap roadmap 7 stages → 3 stages | 4 stage specs never written |
| Original research-mode synthesis (parallel HypothesisAdapter / Workspace / Claim DSL / etc.) | 70% of the proposal cut by the adversarial reviewer; see `2026-06-01-research-mode-extension-design.md` §11 |

**Net delta:** ~1,500 LoC of code + ~5,000+ lines of speculative spec markdown + 6 single-impl interfaces. The discipline that caught "4 wedges invented their own history table" (substrate spec) is the same discipline applied to the rest of the repo: three live specs, not twelve; deletion before addition; no new interface without two concrete consumers.

**Honest accounting:** this PR itself adds ~1,072 lines of new doc content (wedge + 2 briefs + spec). Net of the speculative-doc cuts above, the PR is text-positive in the short term and (assuming the cuts land) text-negative across the next few PRs. The simplification-pass items above are commitments, not deletions-on-merge; their value is realized only when the follow-up PRs land.

## 10. References

- `PRINCIPLES.md` §2 (Reversibility before optionality), §4 (Do not police what you do not have)
- `MEMORY.md` `feedback_deletion_default`, `feedback_drop_ceremony`
- `docs/engineer/specs/2026-06-01-unified-substrate-design.md` — the consolidation discipline applied to one layer; this brief is that discipline applied to the rest
- `docs/engineer/specs/2026-06-01-adapter-contracts-design.md` — deletion candidate
- `docs/wedges/research-mode.md` — the wedge this brief unblocks
- `docs/engineer/briefs/2026-06-01-regatta-research-vision.md` — strategic vision dependent on this simplification pass
- `docs/engineer/specs/2026-06-01-research-mode-extension-design.md` — locked design that ships ONLY after this simplification pass lands
