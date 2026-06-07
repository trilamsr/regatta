---
title: "Scheduler.pollAdaptersHonouringMinPoll vs adaptersync.Syncer.Sync unification (closes #888)"
status: draft
summary: "Unify the two adapter.List call sites so a real adapter wired into the composition root cannot be polled twice per MinPoll window. Recommendation: Option A — adaptersync.Syncer honours MinPoll itself; scheduler stops polling adapters. Trigger fires the moment serve.go wires a real adapter into scheduler.Config.Adapters."
---

# Scheduler ↔ adaptersync MinPoll unification — Design Spec

Date: 2026-06-07
Closes (issue stays OPEN until impl ships): [#888](https://github.com/trilamsr/regatta/issues/888)
Trigger source: reviewer finding on [#885](https://github.com/trilamsr/regatta/pull/885) (closes #847)
Memory rules in force: `feedback_default_simpler`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_decision_priority`, `feedback_adversarial_review`, `feedback_unaddressed_load_bearing`, `feedback_no_signatures`, `feedback_spec_pattern_authority`.

```release-notes
[DOCS] Design spec for #888 — unify scheduler.pollAdaptersHonouringMinPoll
and adaptersync.Syncer.Sync so a real adapter wired into the composition
root cannot be polled twice per MinPoll window. Recommendation: Option A
(adaptersync owns MinPoll cadence; scheduler stops polling adapters).
No code in this PR; impl lands when serve.go wires a real adapter into
scheduler.Config.Adapters.
```

## §0 Closing trigger

Done when ALL of:

1. Impl PR (separate from this spec) merges referencing this spec.
2. After impl, `grep -n "ad\.List\|adapter\.List" internal/orchestrator/{scheduler,adaptersync}/` shows exactly ONE production call site (in `adaptersync/adaptersync.go`).
3. `cmd/regatta/serve.go` constructs at most ONE Syncer per physical adapter; `scheduler.Config.Adapters` is either empty (test-only seam) or removed.
4. `make ci-check` green on impl PR.

## §1 Problem

`Scheduler.pollAdaptersHonouringMinPoll` (`internal/orchestrator/scheduler/scheduler.go:446-458`) calls `ad.List(ctx)` per registered `schemas.SpecAdapter` and DROPS the result. The seam exists only to gate per-adapter cadence — adaptersync owns the actual mirror-into-state side effect (per the godoc on the same function).

`adaptersync.Syncer.Sync` (`internal/orchestrator/adaptersync/adaptersync.go:107`) also calls `s.adapter.List(ctx)`. The Syncer is wired through `Orchestrator.PollOnce` (`internal/orchestrator/orchestrator_poll.go:28`) which fires every `PollInterval` tick — with NO MinPoll-aware gating.

Today:

- `cmd/regatta/serve.go:193` builds `adaptersync.Config{Adapter: ad, ...}` from the boot adapter `ad`.
- `cmd/regatta/serve.go:243` builds `scheduler.Config{...}` with NO `Adapters:` field — production `scheduler.Config.Adapters` is empty.

So the scheduler's MinPoll gate runs on an empty slice. No regression today. Only the package test wires `Adapters`.

The reviewer's `#888` claim: when next-wave composer wires the SAME physical `ad` into BOTH seams, the rate-budgeted source (e.g. `github_issues` at MinPoll=30s) gets two `List` calls per tick — one through the scheduler's MinPoll gate (every MinPoll), one through PollOnce (every PollInterval, no MinPoll gate). If `PollInterval < MinPoll`, the syncer alone busts the budget; if both seams are wired, doubling stacks on top.

Root cause: TWO components claim cadence ownership over the same primitive. Per `feedback_root_cause`, fix the ownership ambiguity at design time, not the duplicate-call symptom at runtime.

## §2 Goal

ONE call site for `adapter.List` per physical adapter. ONE component owns MinPoll cadence. Composition root cannot accidentally double-poll by wiring an adapter into both seams.

Self-host filter: adapter rate budget directly affects the sole operator (GitHub 5000-req/hr cap). Keep in scope.

## §3 Options

### Option A — adaptersync owns MinPoll cadence; scheduler stops polling adapters

`Scheduler.pollAdaptersHonouringMinPoll`, `Config.Adapters`, and `Scheduler.lastPoll` are deleted (~25 LOC + ~5 LOC config field + ~3 LOC init). `Syncer` grows a `MinPollInterval` honour-gate (or inherits `Adapter.Capabilities().MinPollInterval` directly). `Orchestrator.PollOnce` calls `adapterSync.Sync(ctx, pollStartedAt)`; the Syncer no-ops the `adapter.List` call when its own `lastPoll` is inside the budget window — short-circuiting BEFORE the network call.

**Pros**

- Single ownership: the component that consumes List results owns the cadence.
- Net deletion (~30 LOC) per `feedback_deletion_default`.
- No new injection seam — `Capabilities()` already exists on `schemas.SpecAdapter` (verified in scheduler.go:449).
- Tombstone semantics unaffected: skipped tick leaves prior state intact, identical to the existing "transient empty list" branch (adaptersync.go:117-120).
- Existing scheduler `#847` test for MinPoll-honouring becomes a Syncer test — same coverage, different package.

**Cons**

- Adaptersync now carries per-adapter state (`lastPoll time.Time`). Single-adapter Syncer means one `time.Time` field on `Syncer`, not a slice. Concurrency: `PollOnce` is flock-serialized (`orchestrator_poll.go:20`) so no mutex needed.
- The "scheduler enforces per-adapter cadence" seam contemplated in `#847` becomes orphaned. Since serve.go never wired it, this is unrealized scope — deletion is the right call.

### Option B — inject adaptersync.Syncer as scheduler's Adapters consumer

Scheduler retains `Config.Adapters []schemas.SpecAdapter` + `pollAdaptersHonouringMinPoll`, but replaces the discard-result `ad.List(ctx)` body with `adapterSync.Sync(ctx, now)`. `Orchestrator.PollOnce` stops calling `adapterSync.Sync`; cadence flows scheduler → syncer.

**Pros**

- Preserves the `#847` design (scheduler owns cadence, syncer owns side-effects).
- Symmetric to other scheduler-owned cadences (gate poll, lock TTL).

**Cons**

- Adds a new direction of dependency: scheduler → adaptersync. Today the package graph is `orchestrator → {scheduler, adaptersync}`; B requires `scheduler → adaptersync`, which is a topology change without a paying user.
- More moving parts (a new `Adapters` slot AND a new SyncerCallback field) for one prod adapter.
- Doesn't shrink anything; pure addition. Fails `feedback_deletion_default` unless coupled with deletion elsewhere.
- The "scheduler enforces cadence for things it doesn't own the result of" pattern is already odd; B doubles down.

## §4 Recommendation

**Option A.**

Decision-priority chain (CLAUDE.md): UX > ease > performance > best-practices > speed > velocity. Both options solve the duplicate-poll risk for the operator (UX tie). Option A is easier to maintain (single state machine), shrinks LOC, removes an unrealized seam, and matches the `feedback_default_simpler` rule ("three similar lines beat a premature abstraction" — `scheduler.Config.Adapters` is the premature abstraction; it was speculatively added in #885 but never wired in production).

`feedback_deletion_default`: Option A answers "what got smaller?" with a concrete number (~30 LOC + one Config field + one runtime invariant). Option B is pure addition.

## §5 Scope (in)

5.1 Delete `Scheduler.Config.Adapters` field + `Scheduler.lastPoll []time.Time` + `Scheduler.pollAdaptersHonouringMinPoll(ctx)` + the `Tick` call site that invokes it (`scheduler.go:338`).

5.2 Move MinPoll-honouring logic into `adaptersync.Syncer`:
- Add `Syncer.lastPoll time.Time` (zero-value = "never polled, fire on next call").
- `Sync` reads `s.adapter.(schemas.SpecAdapter).Capabilities().MinPollInterval`; if `!s.lastPoll.IsZero() && now.Sub(s.lastPoll) < minPoll`, return nil immediately. Update `lastPoll` on List-error too (same semantics as current scheduler.go:454 — flapping rate-limited adapter must not re-tried inside the budget window).
- Use `pollStartedAt` as `now` (already threaded from `PollOnce`).

5.3 Update the `SpecAdapter` interface declared locally in `adaptersync.go:24-26` to add the `Capabilities()` method — OR import `schemas.SpecAdapter` directly if no import cycle exists. Audit: `adaptersync` already imports `schemas` (line 17), so direct reuse is the simpler form.

5.4 Migrate the existing `#847` MinPoll-honouring scheduler test (`internal/orchestrator/scheduler/scheduler_test.go::TestScheduler_*MinPoll*`) into `internal/orchestrator/adaptersync/adaptersync_test.go`. Same assertion shape; new SUT.

5.5 No change to `cmd/regatta/serve.go:193` (adaptersync wiring) or `cmd/regatta/serve.go:243` (scheduler wiring drops the never-wired `Adapters:` slot if a partial wave landed it in the interim — sanity check on impl day).

## §6 Scope (out)

6.1 Adapter Capabilities() shape changes. Keep `MinPollInterval time.Duration` exactly as today (no new fields).

6.2 Multi-adapter Syncer support. Today one Syncer wraps one adapter; broadening to N is a separate spec (Phase-X — external multi-source ask required).

6.3 Adapter-level retry / backoff inside MinPoll. Existing `lastPoll` update on error covers the flapping case; richer backoff (jittered, capped) deferred.

6.4 Replacing PollOnce's flock-then-Sync structure. Out — orthogonal.

6.5 Removing the `schemas.SpecAdapter` interface. It is a public contract; only the scheduler-side consumer changes.

## §7 Back-compat invariants

7.1 Empty `scheduler.Config.Adapters` (today's production state) behaves identically: no scheduler-side adapter polls, syncer drives the single List per tick — same as today.

7.2 Operator-observable behavior of the single-adapter prod config (github_issues, MinPoll=30s, PollInterval=15s) goes from "list every 15s" to "list every max(15s, 30s) = 30s" — STRICTLY BETTER for the rate budget. This is the bug fix `#847` aimed for but only delivered for an unwired seam.

7.3 No CUE schema change, no migration, no audit-event payload change.

7.4 Telemetry: existing `scheduler.adapter_poll_failed` slog warn moves to `adaptersync.adapter_poll_failed`. Operator-grep impact: documented in PR body, NOT a back-compat break (slog warn shape is not a contract).

## §8 Composition-root delta

`cmd/regatta/serve.go` already does the right thing:

- Line 193: `adaptersync.New(adaptersync.Config{Adapter: ad, DB: db, Logger: slogger})` — no change.
- Line 243-254: `scheduler.New(db, scheduler.Config{...})` — no `Adapters:` slot today. If a parallel wave adds one before this spec lands, the impl PR must DROP it as part of the unification.

Impl-day sanity check: `grep -n "Adapters:" cmd/regatta/serve.go` must return empty after the change.

## §9 Failing tests (TDD scaffold)

Each test name pins behaviour; impl PR lands the RED commit first per `feedback_tdd_discipline`.

9.1 `TestSyncer_HonoursMinPollInterval_SkipsInsideBudget` (adaptersync) — adapter with `MinPollInterval=30s`. Sync at t=0 fires List; Sync at t=10s must NOT fire List (assert via a counting fake); Sync at t=31s fires List again.

9.2 `TestSyncer_HonoursMinPollInterval_FirstCallAlwaysFires` (adaptersync) — zero-value `lastPoll` fires on first Sync regardless of MinPoll.

9.3 `TestSyncer_HonoursMinPollInterval_UpdatesLastPollOnError` (adaptersync) — adapter `List` returns rate-limit error. Sync returns error wrapped; `lastPoll` advances so the next Sync inside the budget window short-circuits and does NOT retry-into-the-throttle. Mirrors the scheduler's prior `#847` semantics (scheduler.go:453-456 update-on-error).

9.4 `TestSyncer_HonoursMinPollInterval_ZeroMinPollAlwaysFires` (adaptersync) — adapter `Capabilities().MinPollInterval == 0` skips the gate (today's default for non-rate-budgeted adapters).

9.5 `TestScheduler_NoLongerOwnsAdapterPoll` (scheduler) — `Scheduler.Tick` runs with NO adapter-poll invocation; assert via no `ad.List` call on a counting fake registered through any remaining seam OR (after deletion) compile-time absence of `Config.Adapters`. Final shape decided during impl.

9.6 `TestOrchestrator_PollOnce_FiresSyncerOnce` (orchestrator) — PollOnce within one MinPoll window calls Syncer's `Sync` once at minimum; the assertion is that the wrapped `adapter.List` call count equals one regardless of how many ticks fire inside the window. This is the issue-#888 acceptance test — the invariant that proves the duplicate-poll risk is eliminated.

RED-output capture in PR body per CLAUDE.md TDD rule.

## §10 Acceptance criteria

10.1 All §9 tests pass; PR-body shows §9.1 + §9.3 + §9.6 RED-then-GREEN commit sequence (`git log --reverse` ordering).

10.2 `make ci-check` green.

10.3 `grep -n "ad\.List(\|adapter\.List(" internal/orchestrator/{scheduler,adaptersync}/*.go` returns exactly ONE production hit, inside `adaptersync.go`.

10.4 `grep -n "Adapters:" cmd/regatta/serve.go` returns empty.

10.5 Net LOC change is negative (deletion-default).

10.6 No `Co-Authored-By` / AI signatures in commits or PR body.

10.7 Adversarial reviewer subagent runs on the impl PR; CRITICAL/HIGH findings either fixed inline or filed as tracker issues before automerge.

## §11 Risk + adversarial pass

- **Risk**: Syncer-side MinPoll cadence is invisible to operators expecting "every PollInterval, the queue refreshes from upstream". **Mitigate**: document in `docs/operator/configure.md` "Adapter cadence" subsection — actual refresh interval is `max(PollInterval, adapter.MinPollInterval)`. Out-of-band followup F1.

- **Risk**: Multi-Syncer future (if 6.2 reopens) makes `lastPoll time.Time` insufficient. **Mitigate**: when that spec lands, refactor to `lastPoll map[string]time.Time` keyed by adapter-id. Today's single-adapter shape is intentionally simpler per `feedback_default_simpler`.

- **Risk**: PollOnce skipping Sync inside MinPoll means `BriefLoader.Sync` (the second step in PollOnce) still fires every tick — operator might assume "PollOnce is a no-op inside MinPoll". **Mitigate**: only the `adapter.List` call short-circuits; BriefLoader continues unaffected. Document in §11.

- **Risk**: Skipped adapter Sync means tombstone cutoff timestamp never advances inside the MinPoll window. **Mitigate**: this is the SAME behavior the current empty-`Config.Adapters` path delivers today (scheduler never advanced anything tombstone-relevant; tombstones come from Syncer.Sync only). No regression.

- **Risk**: Test-only `scheduler.Config.Adapters` consumers (the existing #847 test) break on field deletion. **Mitigate**: 9.4 migrates the test to adaptersync. Use `git grep -l "scheduler.Config{.*Adapters:" --` before impl to enumerate sites.

## §12 Implementer brief (paste into dispatch prompt)

> Task: implement #888 per `docs/engineer/specs/2026-06-07-scheduler-adaptersync-unification.md`. Option A (deletion + Syncer-owned cadence).
>
> Files in scope (file-disjoint w/ any parallel wave):
> - `internal/orchestrator/scheduler/scheduler.go` (delete `Config.Adapters`, `lastPoll`, `pollAdaptersHonouringMinPoll`, Tick call site)
> - `internal/orchestrator/scheduler/scheduler_test.go` (delete #847 MinPoll tests OR migrate to adaptersync per §9.4)
> - `internal/orchestrator/adaptersync/adaptersync.go` (add `lastPoll time.Time`, MinPoll short-circuit, Capabilities() consumption)
> - `internal/orchestrator/adaptersync/adaptersync_test.go` (add §9.1-9.4 + §9.6)
> - `cmd/regatta/serve.go` (sanity-only; assert no `Adapters:` slot in `scheduler.Config{...}`)
>
> TDD: RED commit first for §9.1 + §9.3 + §9.6. Capture RED output in PR body.
> CI: `make pre-push-check`. Subject line: `[FIX] adaptersync: honour MinPoll inside Sync (closes #888)`.
> Reviewer dispatch: required (concurrency + public API). Skip-predicate `feedback_review_proportional` does NOT apply — this is a load-bearing wiring change.

## §13 Out-of-band followups

- F1: doc update — `docs/operator/configure.md` "Adapter cadence" subsection. File as separate issue after impl ships.
- F2: multi-adapter Syncer (deferred; Phase-X external-customer trigger). Not filed today.
- F3: structured `lastPoll` telemetry counter (`regatta.adaptersync.skipped_inside_minpoll_total`) so operators can see when cadence is rate-budget-bound. File as separate issue.
