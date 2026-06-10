---
name: Scheduler parallel-cap enforcement + shipped-status filter
slug: 2026-06-09-scheduler-parallel-cap-enforcement
status: draft
phase: self-host-first
owner: tri@maydow.com
created: 2026-06-09
---

# Scheduler parallel-cap enforcement + shipped-status filter — Spec

Memory rules in force: `feedback_parallel_safety`, `feedback_recognize_session_end`, `feedback_default_simpler`, `feedback_decision_priority` (UX > ease > performance > best-practices).

```release-notes
[DOCS] specs: scheduler parallel-cap enforcement + shipped-status filter (#1169, #1172)
```

## §1 Problem

Live evidence from the 2026-06-09 autonomous loop:

1. **43 concurrent spawns in ~5s against a documented parallel cap of 3-4.**
   `CLAUDE.md` "Dispatch" §"Cap parallel implementers at 3-4" is a soft operator rule; the orchestrator enforces NO aggregate cap. The current scheduler only enforces **per-lane** ceilings via `Config.LaneCaps map[string]int` (`internal/orchestrator/scheduler/scheduler.go:89-91`) and an unbounded default lane (`""` key absent ⇒ unlimited per `internal/orchestrator/scheduler/scheduler_lane_cap.go:4`). When `ListSpawnable` returns dozens of items in one lane (or across many lanes with no caps set), `reserveFromSpawnable` (`internal/orchestrator/scheduler/scheduler_spawn.go:21`) walks the entire slice and reserves every one it can. Issue #1169 §A observed 43 spawns inside one ~5s tick, blowing past the cap that protects the shared Anthropic-API quota documented in `feedback_parallel_safety`.

2. **`tick.slow duration_ms=10656 work_items_evaluated=42` WARN.**
   `internal/obs/events.go:23` declares `EventTickSlow`. The scheduler's `Tick` (`internal/orchestrator/scheduler/scheduler.go:306-417`) runs the full gate chain — `gate_cost_cap`, `gate_approval`, `gate_cost`, `gate_l4` — over the **entire** `spawnable` slice before reaching `dispatch`. With 42 work-items in queue and a target cap of 3-4 actually dispatchable, ~38 candidates pay cost-gate + l4-gate evaluation cost for nothing. Measured: 10.6s per tick at 42 items. Each tick should bail at the cap, not the queue depth.

3. **18 `adapter.item_skipped reason=unknown_status value=done` WARNs at every boot (#1169 §B).**
   `internal/orchestrator/adaptersync/adaptersync.go:164-167` calls `mapAdapterStatus` (`internal/orchestrator/adaptersync/adaptersync.go:238-245`) which switches only on `schemas.StatusPlanned`. Any item the upstream GitHub adapter reports as `done | completed | shipped | closed | wontfix` falls into the default branch, gets logged as `unknown_status`, and is **skipped from staging but not marked terminal**. Re-fetched next boot, re-logged, re-skipped. Worse: when an item is re-fetched under a still-`planned` status (e.g. issue reopened then closed in a race), the orchestrator schedules an agent against an already-shipped phase. The WARN log line tells the operator "unknown status"; the actual semantics are "I know this status and it means done" — root cause is a missing case in `mapAdapterStatus`, not an unknown value.

Per `feedback_decision_priority`: the user-visible failure is (a) blown API quota + 503s and (b) dispatch of already-shipped work — both UX failures. Performance (`tick.slow`) is the third-order cost. The fix order matches: cap → status filter → backoff.

Per `feedback_recognize_session_end`: this spec is bounded — it does NOT chase Phase-X redesigns (Temporal, blackboard, multi-tenant). It closes the live failure mode with the smallest viable change at three discrete stages and stops.

## §2 Goals + non-goals

**Goals**

- **Autonomy quality**: dispatched agents must reflect the operator-documented cap so the loop survives a multi-hour run without quota blowups or already-shipped re-dispatch.
- **Cost predictability**: per-tick cost-gate / l4-gate model-token spend bounded by `ParallelCap`, not by queue depth. Per `feedback_default_simpler`: bail at the cap, do not evaluate then discard.
- **Observability honesty**: terminal-status items emit `DEBUG` (or no log), not `WARN`. WARN-spam masks the signal of items that genuinely have an unknown status.
- **TDD coverage**: per-stage failing test lands first, captures the production miss, then green.

**Non-goals**

- New transports (gRPC, NATS, Kafka).
- Temporal / blackboard / external workflow engine adoption.
- Per-tenant cap matrices, RBAC, billing-tier caps. Self-host phase: single operator, single repo, single cap.
- Adaptive autotuner (#1166 fix proposal 1) is **scoped to Stage 3 as deferred**; not in v1.
- Cross-lane fairness / weighted-round-robin. Stage 1 enforces aggregate ceiling only; existing per-lane caps still bind first.
- UI surfacing of the cap. Operator reads the value out of `regatta.yaml` or scheduler logs.

## §3 Design

Three discrete, independently-shippable stages. Each stage is a self-contained PR; Stage 2 does not depend on Stage 1 landing first; Stage 3 is deferred until a 30-day soak proves Stage 1+2 insufficient.

### §3.1 Stage 1 (smallest) — enforce `ParallelCap` at `scheduler.Tick`

Add one field to `scheduler.Config`:

```go
// ParallelCap is the aggregate ceiling across ALL lanes for one Tick.
// Zero (default) preserves current behavior (unlimited; lane caps only).
// When > 0, Tick short-circuits gate evaluation as soon as
// runningAgents + len(reserved) == ParallelCap.
ParallelCap int
```

Wired into `Tick` (`internal/orchestrator/scheduler/scheduler.go:306`):

1. After `gate_l0` (ListSpawnable, line 338-345) and BEFORE `gate_cost_cap` (line 349), compute `runningAgents` as `sum(occupancy across activeStates)` using the existing `CountAgentsByLane` primitive (line 379) — pulled forward one step.
2. If `ParallelCap > 0` and `runningAgents >= ParallelCap`, set `spawnable = nil` (or `spawnable[:0]`) and let subsequent gates no-op on an empty slice. Emits one structured log `scheduler.parallel_cap_saturated count=<n> cap=<m>` at INFO.
3. Else if `runningAgents + len(spawnable) > ParallelCap`, truncate `spawnable = spawnable[:ParallelCap-runningAgents]` after sort-stable ordering (existing `ListSpawnable` is already deterministic per `state.DB`). This caps **gate evaluation** at the budget, killing the `tick.slow duration_ms=10656` cost head.

Wiring in `cmd/regatta/serve.go`: read `regatta.yaml::scheduler.parallel_cap` (new key, default 4 per `CLAUDE.md`). Per `feedback_default_simpler`: one YAML key, one Config field, one short-circuit. No tier system, no per-lane override matrix, no autotuner.

Decision priority check: UX (no quota blowup) > ease (one field + one branch) > performance (truncate before gates) > best-practices (lane-cap layer unchanged). Stage 1 hits all four in the same direction.

### §3.2 Stage 2 — shipped-status filter at the adapter layer

Extend `mapAdapterStatus` (`internal/orchestrator/adaptersync/adaptersync.go:238-245`) to recognize the terminal set as a discrete signal, not "unknown":

```go
func mapAdapterStatus(s schemas.Status) (state.WorkItemStatus, terminal bool, ok bool) {
    switch s {
    case schemas.StatusPlanned:
        return state.WorkStatusPlanned, false, true
    case schemas.StatusDone, schemas.StatusCompleted,
         schemas.StatusShipped, schemas.StatusClosed, schemas.StatusWontfix:
        return state.WorkStatusDone, true, true  // explicit terminal
    default:
        return "", false, false                   // genuinely unknown
    }
}
```

Caller in `Sync` (line 164-167):

- `ok == false` keeps current WARN at `adapter.item_skipped reason=unknown_status` (genuine unknown — keep the signal).
- `terminal == true` swaps the WARN for a DEBUG `adapter.item_terminal id=<id> status=<value>` and skips the item from `staged`. The orchestrator's downstream scheduler already filters via `ListSpawnable`, so a terminal item never reaches `Tick` — but the **adapter** is the right enforcement point because the item should not be re-staged at all.
- Add `schemas.Status` constants for the missing terminal vocabulary in `contracts/schemas/` (Stage 2 prerequisite — same PR).

Observability follow-up (same PR): `internal/obs/events.go` adds `EventAdapterItemTerminal EventName = "adapter.item_terminal"` to the registry (line 155 vicinity) so dashboards can distinguish "skipped because shipped" from "skipped because broken". Per `feedback_default_simpler`: no new event-kind table row, no substrate-event mirror; the slog DEBUG line is the API.

Net effect: 18 boot-time WARNs collapse to 18 DEBUGs (or zero, with default log level INFO) and stop poisoning the operator dashboard's "recent WARN" panel.

### §3.3 Stage 3 (deferred / forward-fit) — adaptive backoff per #1166 fix proposal 1

Tracking only; not in v1. Forward-fit shape:

When N≥2 consecutive ticks emit the same exit-reason in `spawner` (e.g. `tool_denied`, `quota_exceeded`, `provider_503`), the scheduler pauses dispatch for an exponential window (initial 1 tick, doubling to a cap of 8 ticks) and emits `scheduler.backoff_engaged reason=<x> window_ticks=<n>`. Resumes on first successful agent completion or operator override (`regatta scheduler resume`).

Why deferred: (a) Stage 1+2 may make this unnecessary; (b) the trigger requires `spawner` exit-reason taxonomy that's still settling (cross-ref `2026-06-09-auto-friction-trackers.md` §2.1 introducing `agent_non_completed_exit`); (c) per `feedback_recognize_session_end`, do not pre-build for a hypothetical recurrence pattern — wait for the 30-day soak data.

Reopen trigger: a single autonomous loop after Stage 1+2 ships still exhibits ≥3 same-reason consecutive failures within a 1-hour window. Audit query: `gh issue list --label autonomy-blocked --created ">=$(date -d '30 days ago' -I)"`.

## §4 Acceptance

Per-stage failing test in `internal/orchestrator/scheduler/` lands FIRST (commit order in `git log --reverse` proves RED commit precedes implementation), captures RED output in PR body, then green.

### §4.1 Stage 1 acceptance

**`TestScheduler_TickHonorsParallelCap_WhenSpawnableLargerThanCap`** in `internal/orchestrator/scheduler/scheduler_test.go` (new file `scheduler_parallel_cap_test.go` if size warrants):

```
// TestScheduler_TickHonorsParallelCap_WhenSpawnableLargerThanCap asserts
// that with 43 spawnable items and ParallelCap=4, exactly 4 reservations
// land in one Tick and the cost-gate sees at most 4 candidates (#1169).
```

Setup:

- `state.DB` seeded with 43 planned work-items across 5 lanes, no per-lane cap set, `Config{ParallelCap: 4}`.
- Counting `CostGate.Evaluate` invocations via the existing fake `CostGate` interface (`internal/orchestrator/scheduler/scheduler.go:35-37`).

Assertions:

- `len(reserved) == 4`.
- `CostGate.Evaluate` call count `<= 4` (truncation must happen before cost-gate).
- One `scheduler.parallel_cap_saturated` log line with `count=4 cap=4`.
- Re-run `Tick` immediately with the 4 reserved still in `AgentSpawning`: `len(reserved) == 0`, no quota burn.

Sibling test **`TestScheduler_TickWithParallelCapZero_PreservesLaneCapBehavior`** locks the backward-compat path: `ParallelCap == 0` ⇒ existing lane-cap-only semantics, no behavioral change for currently-shipped configs.

### §4.2 Stage 2 acceptance

**`TestAdapterSync_TerminalStatusFiltered_DoesNotWarn`** in `internal/orchestrator/adaptersync/adaptersync_test.go`:

- Feed `schemas.Item{Status: schemas.StatusDone}`, `StatusCompleted`, `StatusShipped`, `StatusClosed`, `StatusWontfix` (5 items).
- Assert: `staged == 0` items reach `BatchUpsertWorkItems`; zero WARN lines containing `unknown_status`; 5 DEBUG lines `adapter.item_terminal`.

Sibling test **`TestAdapterSync_GenuinelyUnknownStatus_StillWarns`**: pass `schemas.Status("garbage")` and assert the WARN `reason=unknown_status` still fires — Stage 2 must not silence genuine adapter contract violations.

### §4.3 Stage 3 acceptance

Deferred. Reopen trigger documented in §3.3.

## §5 Out of scope

Explicit Phase-X items NOT addressed:

- **Temporal / Cadence / external workflow engine.** Self-host phase: single sqlite substrate, single binary. Re-evaluate per `feedback_decision_priority` long-term > short-term only after multi-tenant becomes a real ask.
- **Multi-tenant cap matrices** (`tenant_id` → `ParallelCap`). Single-operator phase; one global cap.
- **RBAC over the cap knob.** Operator-only YAML key.
- **Stripe / billing-tier caps.** No billing surface in self-host.
- **Sigstore / Rekor attestations on dispatched agents.** Out of scope; tracked separately under `2026-06-01-w10-sigstore-design.md`.
- **Blackboard architecture** for cross-agent coordination. The `feedback_self_host_filter` rule rejects this surface.
- **htmx / Svelte operator UI for cap tuning.** Single YAML key; operator edits the file.
- **Lane-priority queues** (HIGH/MED/LOW). Existing `ListSpawnable` ordering is deterministic; reopen if starvation surfaces post-Stage-1.
- **Autotuner closed-loop** (`2026-06-07-autotuner-closed-loop.md`). That spec adapts the cap from outcome data; this one enforces the static cap. Sequenced after.

## §6 References

- **GitHub issues**:
  - `#1166` — autonomy blocked: consecutive same-reason exits (fix proposal 1 = Stage 3 forward-fit).
  - `#1169` — scheduler ignores documented parallel cap (§A) + adapter logs `unknown_status=done` (§B). **Closed by Stages 1+2.**
  - `#1172` — sibling: `tick.slow duration_ms=10656` symptom of full-queue gate evaluation.
  - `#1177` — operator dashboard WARN-noise audit (downstream beneficiary of Stage 2's DEBUG-not-WARN flip).
- **CLAUDE.md rules cited**:
  - `feedback_parallel_safety` — "Cap parallel implementers at 3-4 (shared API quota dies at 5+)". The standard this spec enforces mechanically.
  - `feedback_recognize_session_end` — bounds the scope; Stage 3 deferred not built.
  - `feedback_default_simpler` — one Config field, one short-circuit, one switch-case extension. No tier system, no autotuner v1.
  - `feedback_decision_priority` — UX > ease > performance > best-practices: quota safety + already-shipped suppression are UX wins ranked above the `tick.slow` perf win.
- **Sibling specs**:
  - `docs/engineer/specs/2026-06-07-autotuner-closed-loop.md` — sequenced after; adapts the cap this spec enforces.
  - `docs/engineer/specs/2026-06-09-auto-friction-trackers.md` — supplies the `agent_non_completed_exit` event-kind Stage 3 will subscribe to.
- **Code refs (verified against worktree, must re-verify against `origin/main` at PR open per `feedback_cite_origin_main_not_local`)**:
  - `internal/orchestrator/scheduler/scheduler.go:89-91` — `Config.LaneCaps`.
  - `internal/orchestrator/scheduler/scheduler.go:306-417` — `Tick` step loop.
  - `internal/orchestrator/scheduler/scheduler.go:35-37` — `CostGate` seam used by Stage 1 test.
  - `internal/orchestrator/scheduler/scheduler_spawn.go:21` — `reserveFromSpawnable`.
  - `internal/orchestrator/scheduler/scheduler_lane_cap.go:4` — `laneHasCapacity` (untouched by this spec).
  - `internal/orchestrator/adaptersync/adaptersync.go:164-167` — WARN call site Stage 2 rewrites.
  - `internal/orchestrator/adaptersync/adaptersync.go:238-245` — `mapAdapterStatus` Stage 2 extends.
  - `internal/obs/events.go:23` — `EventTickSlow` declaration.
  - `internal/obs/events.go:155` — event registry Stage 2 appends `EventAdapterItemTerminal` to.
