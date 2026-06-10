---
name: adapter-terminal-status-log-level
slug: 2026-06-09-adapter-terminal-status-log-level
status: draft
phase: self-host-first
owner: trilamsr@gmail.com
created: 2026-06-09
---

# Adapter terminal-status recognition + log-level downgrade

_Author: design session, 2026-06-09. Closes GH #1173. Stage 2 of the scheduler-cap-enforcement spec (`2026-06-09-scheduler-cap-enforcement.md`, sibling in this PR). Source: operator boot-log noise observed during regatta-operator skill session 4 this session._

## 1. Observed

Every regatta boot fires a burst of WARN-level adapter skips for items that are in fact terminal in the source-of-truth spec/issue file. Captured during regatta-operator skill session 4 (2026-06-09), the relevant log signature repeats 18 times per boot:

```
level=WARN msg=adapter.item_skipped id=<id> reason=unknown_status value=done
level=WARN msg=adapter.item_skipped id=<id> reason=unknown_status value=done
... (16 more, one per terminal work-item in the adapter snapshot)
```

The items DO carry `status: done` in their spec/issue source — the adapter just doesn't recognise `done` as a terminal token, drops them into the `unknown_status` bucket, and logs WARN per item. On a fresh worktree boot the noise is 18 lines; in long-running sessions where the spec corpus has accumulated terminal items, it scales linearly with the historical terminal-item count.

**Operator impact** (observed this session, not hypothetical):

- WARN floor on every boot — the operator dashboard's WARN-counter widget renders a non-zero badge before any real work has started, training the operator to ignore WARN as noise (the exact regression `feedback_double_fail_root_cause` warns against — alert fatigue masks real defects).
- Dashboard alerting (`internal/web/`) treats `adapter.item_skipped` WARN as a benign-re-dispatch candidate and emits an operator notification per item. 18 notifications per boot is a paper-cut that punishes the operator for the adapter's own classification gap.
- The condition is benign — the scheduler skips re-spawning agents for items it considers terminal regardless, so no work is lost. Only the log volume + dashboard noise is the harm.

## 2. Root cause

`internal/orchestrator/adaptersync/adaptersync.go::mapAdapterStatus` (verified at lines 238-245 on this worktree's HEAD) recognises exactly one input token:

```go
func mapAdapterStatus(s schemas.Status) (state.WorkItemStatus, bool) {
    switch s {
    case schemas.StatusPlanned:
        return state.WorkStatusPlanned, true
    default:
        return "", false
    }
}
```

The accompanying godoc declares the design intent: _"Only `planned` maps; in_progress / done belong to the agent state-machine and never originate from adapters."_ That premise held at the time it was written — adapters were expected to publish only fresh, unstarted work — but the spec/issue adapter has since been allowed to surface already-terminal items (because the on-disk source-of-truth file IS the spec/issue, and a closed issue still appears in the adapter's snapshot pass). Today, `done` arrives at `mapAdapterStatus` from real adapter input and lands in the `default` branch.

The call site at line 164-168 wraps the unknown bucket in a blanket WARN:

```go
status, ok := mapAdapterStatus(it.Status)
if !ok {
    s.log.Warn("adapter.item_skipped", "id", id, "reason", "unknown_status", "value", string(it.Status))
    continue
}
```

The WARN was originally a legitimate canary — "an adapter just emitted a status I don't understand, so a downstream contract may be drifting, fail loud so the operator notices." That signal is still valuable for genuinely novel tokens. The defect is that the bucket now misclassifies KNOWN-terminal statuses (`done`, etc.) as novel, drowning the real signal in benign noise. The fix is to teach `mapAdapterStatus` the terminal vocabulary AND split the log level: known-terminal = DEBUG (operator can opt-in to verify the adapter saw the item), genuinely-unknown = WARN (canary preserved).

## 3. Fix — smallest viable change

One-line-per-status extension to `mapAdapterStatus`, plus a log-level split at the call site. Total change: ~12 LOC.

### 3a. Extend the recognised-terminal set

In `internal/orchestrator/adaptersync/adaptersync.go::mapAdapterStatus`, add explicit cases for the terminal-token vocabulary the spec/issue adapter actually emits:

```go
func mapAdapterStatus(s schemas.Status) (state.WorkItemStatus, bool) {
    switch s {
    case schemas.StatusPlanned:
        return state.WorkStatusPlanned, true
    case schemas.StatusDone, schemas.StatusCompleted, schemas.StatusShipped,
        schemas.StatusClosed, schemas.StatusWontfix:
        return state.WorkStatusDone, true
    default:
        return "", false
    }
}
```

The set `done | completed | shipped | closed | wontfix` covers the terminal tokens observed in the regatta spec/issue corpus on this worktree. Adapters that emit a not-yet-in-the-set terminal token still hit the `default` branch and fire the WARN canary — Stage 3 of the scheduler-cap spec captures the RFC mechanism for closing that gap (deferred per §5 below).

`schemas.Status*` constants for the new tokens MAY need adding to `contracts/schemas/` — verify which exist at impl time; missing ones land as one-liner const declarations in the same PR.

### 3b. Split log level at the call site

Adjust `internal/orchestrator/adaptersync/adaptersync.go` line ~164-168 so KNOWN-terminal statuses log at DEBUG with a distinct event name, and only TRULY-unknown statuses keep WARN:

```go
status, ok := mapAdapterStatus(it.Status)
if !ok {
    s.log.Warn("adapter.item_skipped", "id", id, "reason", "unknown_status", "value", string(it.Status))
    continue
}
if status == state.WorkStatusDone {
    s.log.Debug("adapter.item_terminal", "id", id, "value", string(it.Status))
    continue
}
```

Operator-facing payoff: WARN floor on boot drops from 18 lines to 0 in the steady state, while the canary for genuinely-novel adapter statuses is preserved. Dashboard alerting in `internal/web/` continues to surface real `adapter.item_skipped` WARN events at full fidelity.

### 3c. Parent linkage

This is Stage 2 of the scheduler-cap-enforcement spec (`docs/engineer/specs/2026-06-09-scheduler-cap-enforcement.md`, sibling in this PR). Stage 1 caps concurrent runners; Stage 2 stops the cap-decision logs from being drowned by terminal-status WARN noise. Cite parent in the impl PR body.

## 4. Test surface

Unit-test file: `internal/orchestrator/adaptersync/adaptersync_terminal_status_test.go` (new file; co-located with existing `adaptersync_test.go` which already exercises the unknown-status path at lines 167-174 and 430-452 — the new file inherits the same harness).

Required assertions:

- **Per-token DEBUG**: for each of `done`, `completed`, `shipped`, `closed`, `wontfix`, assert the adapter emits exactly one `adapter.item_terminal` record at DEBUG level with `value=<token>` and ZERO `adapter.item_skipped` records.
- **Unknown still WARN**: assert that an unrecognised token (e.g. `frobnicated`) emits `adapter.item_skipped` at WARN with `reason=unknown_status` and `value=frobnicated`. This protects the canary from regressing as the recognised set grows.
- **No WARN floor**: integration assertion that a fixture with N terminal items emits ZERO WARN records (the bug-1173 regression case).

Test harness pattern follows the existing injected-logger setup at `adaptersync_test.go:430-452` (slog handler capturing records into a slice, then assertions over the slice). TDD order per `CLAUDE.md` §TDD: failing test FIRST, capture RED output in PR body, then impl, then green.

## 5. Out of scope

- **Status-vocabulary RFC across adapters.** A formal contract that pins the full terminal-token vocabulary and forbids adapter-specific spellings is a larger surface that the current single-adapter self-host loop does not need. Deferred to Phase X with reopen-trigger: ≥2 adapters disagree on a status token (e.g. the spec adapter emits `done` while a future GitHub-projects adapter emits `Done` or `closed-as-completed` and the union breaks `mapAdapterStatus` again). File the RFC issue at that trigger, not now. Per `feedback_default_simpler` — three similar lines beat a premature abstraction; wait for the second adapter, not the lint.
- **Schema-level enum tightening for `schemas.Status`.** Whether `schemas.Status` should be a closed enum (validated at adapter ingress) vs the current open string type is a separate design question. Out of scope here; tracked under the Stage 3 RFC trigger above.
- **Web-dashboard alert-routing changes.** This brief fixes the source of the WARN noise; whether `internal/web/` should additionally batch/dedupe `adapter.item_skipped` notifications is independent and tracked separately.

## 6. Citations

- **Issue**: GH #1173 — "18 WARN adapter.item_skipped lines fire at every regatta boot" (parent issue this brief closes).
- **Spec parent**: `docs/engineer/specs/2026-06-09-scheduler-cap-enforcement.md` (sibling in this PR) — Stage 2.
- **Rule**: `feedback_default_simpler` — smallest viable fix is one-line-per-status + log-level split; the cross-adapter RFC is deferred to its concrete trigger.
- **Code refs** (verified against this worktree's HEAD):
  - `internal/orchestrator/adaptersync/adaptersync.go:238-245` — `mapAdapterStatus` (the function that drops `done` into the unknown bucket).
  - `internal/orchestrator/adaptersync/adaptersync.go:164-168` — call site that emits the WARN.
  - `internal/orchestrator/adaptersync/adaptersync_test.go:167-174, 430-452` — existing unknown-status test harness the new test file inherits.
- **Evidence**: regatta-operator skill session 4 (this session, 2026-06-09) — 18 `level=WARN msg=adapter.item_skipped ... reason=unknown_status value=done` lines captured at boot.
