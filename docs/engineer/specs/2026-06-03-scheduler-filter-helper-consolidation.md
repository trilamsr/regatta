---
title: "Scheduler filter-helper consolidation (closes #251)"
status: shipped
summary: "Consolidate `applyApprovalGates` / `applyCostGovernor` / `applyL4Gate` into shared `filter.Apply` helper. No runtime change. Landed via #698."
---

# Scheduler filter-helper consolidation (closes #251)

Owner: scheduler  
Status: shipped (#251 / #698)  
Closes: #251 once implementer wave lands

```release-notes
[DOCS] Spec for #251 — consolidate `applyApprovalGates` / `applyCostGovernor`
(and `applyL4Gate`) into a shared `filter.Apply` helper. No runtime change.
```

## §1 Problem statement

Three sibling methods in `package scheduler` share an identical
filter-in-place + resolver-callback + gate-evaluate shape, duplicating
nil-short-circuit, kept-slice allocation, per-wi resolver miss, evaluate
call, fail-closed warn-drop, and final return:

- `applyApprovalGates` — `internal/orchestrator/scheduler/scheduler_approval_gate.go:25-80` (~55 LoC body)
- `applyCostGovernor` — `internal/orchestrator/scheduler/scheduler_cost_gate.go:23-55` (~32 LoC body)
- `applyL4Gate` — `internal/orchestrator/scheduler/scheduler_l4_gate.go:32-69` (~37 LoC body)

All three live in the same package (`internal/orchestrator/scheduler`) — no
cross-package constraint. Each has a slightly different verdict type and a
gate-specific side effect (mark-rejected DB transition for approval,
downgrade callback for cost, emit-gate-rejected event for L4), so the helper
must accept the side effect as a callback rather than try to unify it.

#251 originated as an adversarial-review finding from PR #250 (T1
cost-governor) and was deferred per `feedback_spec_pattern_authority` until a
third gate-pass arrived. L4 (W5) is that third pass — the abstraction is now
forced.

## §2 Proposed shape

Introduce `internal/orchestrator/scheduler/filter/filter.go`:

```go
package filter

import (
    "context"

    "github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Pass is a single gate-pass over a spawnable slice. Resolve maps wi to a
// per-wi scope (or `gated=false` to skip); Evaluate decides keep/drop and
// runs any side effect via callbacks closed over by the caller.
//
// Returning an error halts the tick — reserve this for failures that would
// silently retry a terminal wi (e.g. mark-rejected DB transition lost).
// Per-wi fail-closed (warn + drop) belongs INSIDE Evaluate.
type Pass[Scope any] struct {
    Name     string
    Resolve  func(state.WorkItem) (scope Scope, gated bool)
    Evaluate func(ctx context.Context, wi state.WorkItem, scope Scope) (keep bool, err error)
}

// Apply runs one pass over spawnable and returns the kept subset. Disabled
// passes (caller pre-checks gate/resolver nil and skips Apply entirely) are
// the caller's responsibility — Apply has no gate-nil short-circuit.
func Apply[Scope any](ctx context.Context, p Pass[Scope], spawnable []state.WorkItem) ([]state.WorkItem, error) {
    kept := make([]state.WorkItem, 0, len(spawnable))
    for _, wi := range spawnable {
        scope, gated := p.Resolve(wi)
        if !gated {
            kept = append(kept, wi)
            continue
        }
        keep, err := p.Evaluate(ctx, wi, scope)
        if err != nil {
            return nil, err
        }
        if keep {
            kept = append(kept, wi)
        }
    }
    return kept, nil
}
```

Generic over `Scope` so each gate keeps its own scope type
(`approval.GateConfig`, `costgov.Scope`, `l4.Input`). `Evaluate` returns
`(keep, err)` rather than a tri-state verdict — the caller closes over its
own logger/DB/downgrade callback inside `Evaluate` and folds the
keep/warn-drop/side-effect logic there. This keeps the helper unaware of
verdict shapes while still extracting the loop scaffolding.

Caller site after migration (approval, abbreviated):

```go
func (s *Scheduler) applyApprovalGates(ctx context.Context, tc *tickCtx, sp []state.WorkItem) ([]state.WorkItem, error) {
    if s.cfg.Gate == nil || s.cfg.GateResolver == nil {
        return sp, nil
    }
    return filter.Apply(ctx, filter.Pass[approval.GateConfig]{
        Name:    "approval",
        Resolve: s.cfg.GateResolver,
        Evaluate: func(ctx context.Context, wi state.WorkItem, cfg approval.GateConfig) (bool, error) {
            res, err := s.evaluateApproval(ctx, wi, cfg)  // helper holds span + log
            if err != nil { /* warn, return false, nil */ }
            switch res { /* Proceed→true; Reject→markRejected + false; Pause→false */ }
        },
    }, sp)
}
```

## §3 Migration path

Strict 3-step sequence; each step ships as ONE commit so revert is clean:

a. **Introduce `internal/orchestrator/scheduler/filter/` package** with
   `filter.go` (the helper above) + `filter_test.go` (unit + property
   tests per §5). Zero call-site change. New code only.

b. **Adapt each gate to the `Pass[Scope]` interface, smallest delta.**
   Per gate: extract the inner-loop body into a private
   `(s *Scheduler) evaluate<Gate>(ctx, wi, scope) (keep bool, err error)`
   method, then rewrite `apply<Gate>` to construct a `Pass` and call
   `filter.Apply`. The nil short-circuit stays at the call site (`applyX`
   wrapper) — it lives OUTSIDE the helper per §2 to keep `Apply` total.
   Order: approval → cost → l4 (smallest blast radius first; each commit
   ships green tests).

c. **No scheduler.go change.** The `tickPhases` slice still calls
   `s.applyApprovalGates / applyCostGovernor / applyL4Gate` —
   `scheduler.go:365-389` is untouched.

Out of scope: `applyCostCap` (no per-wi loop — operates on the whole
slice via aggregate budget check; different shape, would distort the
abstraction).

## §4 What gets SMALLER

Per-gate LoC after migration (excluding godocs):

| Gate                | Before (loop body) | After (closure body + Pass construct) | Delta |
|---------------------|---|---|---|
| `applyApprovalGates`| ~50 | ~32 | -18 |
| `applyCostGovernor` | ~28 | ~16 | -12 |
| `applyL4Gate`       | ~32 | ~20 | -12 |
| `filter.Apply`      | 0   | +20 (new) | +20 |

**Net deletion: ~22 LoC. GREENLIGHT — marginal but justified because the
abstraction is forced by a third call site (the trigger from #251).**

Beyond raw LoC, the consolidation:
- Removes `kept := make([]state.WorkItem, 0, len(spawnable))` + `for _, wi
  := range spawnable` boilerplate from three places.
- Future gate-pass (W12 billing-meter, hypothetical) ships as a single
  `Pass` literal — no new file in `scheduler/`.

If review during impl shows net delta is < +5 / -10 LoC (e.g. closures
balloon the call sites), **STOP and re-spec** — the abstraction is not
paying for itself.

## §5 Test plan

Land in step (a), before any call-site change:

- `TestApply_KeepsAllWhenAllGated` — every wi `gated=true`, `Evaluate→keep=true` → output equals input order.
- `TestApply_SkipsUngated` — `Resolve→gated=false` → wi kept verbatim, `Evaluate` never called (counter assertion).
- `TestApply_DropsOnEvaluateFalse` — half the wi `keep=false` → kept subset preserves input order, dropped wi absent.
- `TestApply_HaltsOnEvaluateError` — first error short-circuits; returns `(nil, err)` wrapping the cause.
- `TestApply_EmptySpawnable` — empty input → empty output, no panic, `Evaluate` never called.
- `TestApply_PreservesOrder` — interleaved keep/drop pattern (e.g. `T F T F T`) → kept slice order matches input.
- `FuzzApply_OrderIndependentVerdict` — property: for any spawnable + any pure `Evaluate`, `Apply(Apply(x))` equals `Apply(x)` (idempotence) and reordering input + reapplying yields the same kept *set*.
- `TestApplyComposition` — chaining two `Pass` values: `Apply(p2, Apply(p1, sp))` matches the union of their kept predicates; verifies the call sites' sequential composition (`gate_approval → gate_cost → gate_l4`) loses no info.

Property-test note: composition order DOES matter for side-effects (mark-rejected fires only if approval keeps the wi), so the property tests cover *pure-keep equivalence*, not side-effect ordering. Side-effect order is asserted by the existing per-gate `_test.go` files unchanged.

Existing tests untouched (per #251 acceptance: "Test coverage held by the existing per-pass _test.go files").

## §6 A+ grade rubric scorecard template

Implementer copies into PR body and re-rates per
`feedback_grade_rubric` / `feedback_agent_pr_review`:

```
- [ ] B-tier: filter.Apply compiles, all gate _test.go pass — evidence: <Test*> / <file:line>
- [ ] B-tier: nil short-circuit preserved at call site (Gate/Resolver nil → spawnable byte-equal) — evidence: <Test*>
- [ ] A-tier: net LoC delta ≤ -15 (deletion ≥ 15 lines) — evidence: `git diff --stat origin/main...HEAD` paste
- [ ] A-tier: filter.Apply has zero scheduler-package import (no leaky abstraction) — evidence: <file:line>
- [ ] A-tier: property test FuzzApply_OrderIndependentVerdict lands and passes ≥ 10s — evidence: <Test*>
- [ ] A-tier: side-effect order preserved (mark-rejected, downgrade callback, gate_rejected emit) — evidence: <Test*> in per-gate file
- [ ] A+-tier: composition test asserts gate_approval → gate_cost → gate_l4 ordering still kept-set-equal under reorder — evidence: <Test*>
- [ ] A+-tier: zero new banned-phrase violations, `make ci-check` green local — evidence: paste tail
- [ ] A+-tier: hot-path benchmark (`BenchmarkTick`) within ±2% of baseline (generics shouldn't cost) — evidence: paste before/after
Claimed tier: <B|A|A+>
```

## §7 Open questions / risks

1. **Generic monomorphization cost on hot path.** `Apply` runs every tick
   on every spawnable wi. Go generics generally compile to per-instantiation
   code, so cost should be zero — but the spec gates this on a
   `BenchmarkTick` regression check (§6 A+ row). Cannot decide without
   running the benchmark on the implementer's branch.

2. **Should `Apply` accept `[]Pass[Scope]` instead of one?** A batched
   variant would let scheduler.go collapse `tickPhases`' three `gate_*`
   entries into one. Deferred — different `Scope` types per gate kill the
   slice typing without `any` boxing, which forfeits the
   monomorphization win. Revisit if a fourth gate arrives.

3. **`applyCostCap` inclusion.** It's a whole-slice budget gate, not a
   per-wi filter — including it would distort `Apply`'s contract. Left
   out of scope. If the cost-cap evolves into per-wi soft caps, reopen
   #251 with the new shape.
