---
title: "Provider-halt gate (N consecutive credit_exhausted) — Design Spec"
status: design
phase: self-host-s2
summary: "Stop the dispatch loop when N consecutive agent.exited events carry exit_reason=provider_credit_exhausted inside a sliding window, instead of burning retry cycles indefinitely when Anthropic credits run out. Builds on the spawner exit_reason classifier (closed #1063, PR #1104). Defaults: N=3 consecutive in 5min window; halve to 1 under stricter operator config. Halt-state is a process-local in-memory counter mirrored to a substrate event for observability; release is operator-issued `regatta dispatch resume` plus an auto-release after a configurable cool-off (default 60min). Lands as a new tick-level pre-gate ahead of approval/cost/l4 — earliest possible drop point so no per-wi work runs while halted."
---

# Provider-halt gate — Design Spec

Status: design
Date: 2026-06-08
Author: design subagent
Tracks: #1096 (this gate), #1063 (exit_reason classifier dependency, MERGED in PR #1104)
Cross-ref: `internal/orchestrator/spawner/exit_reason.go` (classifier), `internal/orchestrator/scheduler/scheduler.go` (Tick step loop), `internal/orchestrator/scheduler/scheduler_cost_gate.go` (cost-cap pattern — closest analog), `docs/engineer/specs/2026-06-02-phase-autonomy-w5-cost-cap-autonomic-enforcement.md` (sibling tick-level halt primitive).

Memory rules in force: `feedback_decision_priority`, `feedback_default_simpler`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_adversarial_review`, `feedback_adversarial_review_every_step`, `feedback_unaddressed_load_bearing`, `feedback_spec_pattern_authority`, `feedback_no_signatures`, `feedback_validate_before_ship`, `feedback_no_self_tagged_approve`.

---

## §1 Problem

2026-06-08 dogfood session: operator left the regatta daemon dispatching against the `operator-docker-soak` worktree overnight. The Anthropic account hit the credit cap mid-loop. Every subsequent spawn produced a `Credit balance is too low` exit within seconds, the spawner classified it as `provider_credit_exhausted` (per #1063), but the scheduler kept happily picking the next spawnable work item, the next, the next — burning ~$20 of orchestrator-side overhead (CI build minutes for the harness, GitHub API requests for prwatch sweeps, sqlite churn, telemetry export) over several hours producing zero forward progress. The work items also got their retry counters bumped, so several started backing off legitimately even though the failure was 100% external.

Symptom inventory:

- `agent.exited exit_reason=provider_credit_exhausted` log lines: ~200 in a 4-hour window, all back-to-back.
- Cost gate (#W5) does NOT fire on provider-credit failures — it tracks regatta's own spend ledger, which lags Anthropic's truth by hours and doesn't see external account state.
- L4 reviewer gate runs AFTER the work item is reserved + the model is called — too late by definition for credit-balance failures.
- Approval gate is HITL — orthogonal.
- The exit_reason classifier (#1063) stamps the reason but nothing consumes it.

Adversarial framing: a single `provider_credit_exhausted` exit is recoverable (rate spike, transient API drift, a fresh credit top-up between exit and next tick). The pathology is the *streak* — when N back-to-back attempts all hit the same provider-credit signature, the operator's wallet is empty and no number of retries inside the window will fix it. The gate must distinguish a streak from a flake.

Closes the loop: spawner classifies (#1063, shipped) → tick-level gate counts streaks and halts dispatch (#1096, this spec) → operator surface (`regatta dispatch status` + `regatta dispatch resume`) makes the halt observable + releasable.

## §2 Design (Goal)

Operators can answer "is the dispatch loop currently making any forward progress, or is it just retrying against a dead provider?" with `regatta dispatch status` and release the halt with `regatta dispatch resume` once credits are topped up — without the daemon needing a restart, without losing the pending queue, and without any work item retry counter being bumped while the halt is active.

Single conjunct: `N consecutive agent.exited with exit_reason=provider_credit_exhausted inside window W ⇒ dispatch loop halted ⇒ Tick is a no-op until release condition fires`.

## §3 Scope (in)

3.1 New scheduler-level gate `applyProviderHalt` running BEFORE `gate_l0` (ListSpawnable) in the `Tick` step loop. When halted, the gate returns early with `reserved=nil, err=nil` — no spawnable lookup, no per-wi work, no event writes (other than the halt-active heartbeat described in §4.4).

3.2 New package `internal/orchestrator/scheduler/providerhalt/` exporting:

- `Counter` — process-local sliding-window counter keyed on `(exit_reason, timestamp)`. Records via `Record(reason ExitReason, at time.Time)`; reports via `Streak(now time.Time) int` (count of consecutive `provider_credit_exhausted` entries inside window W ending at `now`, where "consecutive" means no other exit reason has been recorded between them).
- `Gate` — wraps `Counter` with the halt-state machine: `Evaluate(now time.Time) HaltState` returns `{Halted bool, Since time.Time, StreakCount int, AutoReleaseAt time.Time}`.
- `Release(reason ReleaseReason)` — clears the streak counter and emits a substrate event (`KindDispatchResumed`).

3.3 Spawner-side seam: `internal/orchestrator/spawner/claude.go` already emits `agent.exited` with `exit_reason`. Add a `ProviderHaltRecorder` interface (`Record(reason ExitReason, at time.Time)`) to `spawner.Config`; spawner calls it from the same code path that emits the structured log (line ~422). Nil recorder = no-op so existing tests stay byte-equal.

3.4 New CLI subcommands under `cmd/regatta/dispatch.go`:

- `regatta dispatch status` — reads halt state (via a read-only state.DB query on the substrate KindDispatchHalted / KindDispatchResumed event tail) and prints `halted | live`, streak count, halted-since, auto-release-at.
- `regatta dispatch resume` — sends a release intent via a sqlite row (see §4.2) that the scheduler polls on its next Tick.

3.5 New substrate event kinds added to `internal/orchestrator/state/substrate/event.go`:

- `KindDispatchHalted` — payload `{streak_count int, window_seconds int, first_exit_at, last_exit_at time.Time, halted_at time.Time, auto_release_at time.Time}`. Emitted once per halt transition (not per blocked Tick).
- `KindDispatchResumed` — payload `{halted_at, resumed_at time.Time, release_reason "operator" | "auto_cooldown" | "config_disable"}`. Emitted once per release transition.

3.6 New `regatta.yaml` block under `dispatch:`:

```yaml
dispatch:
  provider_halt:
    enabled: true                # default true
    consecutive_exits: 3         # default 3
    window: 5m                   # default 5min
    auto_release_after: 1h       # default 60min; 0 = require operator resume
    strict_mode: false           # default false; true → consecutive_exits=1
```

3.7 OTel: counter `regatta.dispatch.provider_halt.transitions_total{direction=halt|resume,reason=...}` + gauge `regatta.dispatch.halted{value=0|1}`. Reuses `obs.MeterScopeScheduler`.

## §4 Halt state schema

Three layers, narrowest-to-widest:

### §4.1 In-memory (authoritative for the running daemon)

`providerhalt.Gate` owns a `sync.Mutex`-guarded struct:

```go
type haltState struct {
    halted          bool
    haltedAt        time.Time
    autoReleaseAt   time.Time   // zero = manual-only
    streakCount     int
    firstExitAt     time.Time   // start of the streak
    lastExitAt      time.Time   // most recent provider_credit_exhausted
}
```

Plus a bounded ring of the last `N` exit records (size = `consecutive_exits` from config; cap at 16 to bound memory regardless of misconfig). The ring is the ONLY mutable surface; `Streak()` derives the count from a single linear walk. No background goroutine — eviction happens lazily inside `Record` and `Evaluate`.

Rationale: provider-halt is a *crash-safe-enough* primitive. A daemon restart resets the counter to zero, which is the operator-friendly default (a restart is an implicit "I checked, retry") and matches the cost-gate's own restart semantics. The streak window is short (5min) — even if we wanted persistence, a restart longer than W invalidates the streak anyway.

### §4.2 Operator-resume intent: single sqlite row

A new table `dispatch_resume_intents (id INTEGER PRIMARY KEY, requested_at TIMESTAMP NOT NULL, consumed_at TIMESTAMP)` — the CLI inserts one row with `consumed_at=NULL`; the next scheduler Tick reads the latest unconsumed row, calls `Gate.Release(ReleaseReasonOperator)`, then CAS-updates `consumed_at`. CAS-on-NULL guarantees idempotence if the operator runs `resume` twice in quick succession.

Why a row not a flag-on-agent-state: keeps the resume signal out of the agents lifecycle (the gate is upstream of any agent); makes `regatta dispatch resume` work even when no agents exist; gives us a free audit log of resume actions.

### §4.3 Substrate events (observability, NOT source of truth)

`KindDispatchHalted` + `KindDispatchResumed` emitted on transitions only. The CLI `dispatch status` reads the tail of these to render a transcript; it MUST NOT use them to compute the live state (the daemon's in-memory counter is the authority — the event log can lag if the substrate writer is backed up).

### §4.4 Per-Tick halt heartbeat (operator UX, not state)

While halted, the scheduler emits one `slog.Info("scheduler.provider_halt_blocking_tick", streak_count, since, auto_release_at)` per Tick at a **rate-limited** cadence: at most once per 30s. Operator running `journalctl -f` sees a steady "still halted" pulse without log spam. Not a substrate event — pure observability.

## §5 Defaults: window + N

Per problem-statement guidance, defaults shipped as:

- `consecutive_exits: 3` — three credit_exhausted exits with no other reason between them.
- `window: 5m` — the streak's first and last exit MUST land within 5 minutes of each other. A gap >5min between exit i and exit i+1 resets the streak (single transient exit "recovers" automatically by virtue of any non-credit_exhausted exit OR a 5-minute idle gap).
- `auto_release_after: 1h` — once halted, the gate auto-releases after 60min wall-clock with release_reason=auto_cooldown. Operator can override to 0 (never auto-release; require `regatta dispatch resume`) for stricter dogfood profiles.
- `strict_mode: true` — overrides `consecutive_exits` to 1. A single provider_credit_exhausted halts dispatch immediately. Use case: paid customer-facing soak runs where one wasted spawn is one too many.

Anti-patterns (NOT defaults):

- Time-based "any 5 credit_exhausted in 5min" — non-consecutive matching incorrectly halts on a legitimate flake mixed with successful runs.
- Exponential window growth — over-engineered for a binary halt/live signal.
- Per-lane halt — credit exhaustion is account-global; per-lane resolution leaks model-specific complexity into a provider-level gate.

## §6 Halt-release behavior

Three release paths, mutually exclusive per halt:

1. **Operator-issued `regatta dispatch resume`** — release_reason=operator. Always available, even before `auto_release_after` elapses. The CLI inserts the `dispatch_resume_intents` row; the next Tick (≤1 sched-tick latency, ~5s worst case) consumes it. CLI exits 0 once the row is inserted — it does NOT block on Tick consumption (the dispatch loop is by design async).
2. **Auto-cool-off** — release_reason=auto_cooldown. When `now >= autoReleaseAt`, the gate auto-releases on the next Tick. This is the "operator went to sleep, credits topped up via auto-billing overnight" case.
3. **Config disable** — release_reason=config_disable. Setting `dispatch.provider_halt.enabled: false` and SIGHUP'ing the daemon clears any active halt. Used for "I know what I'm doing, get out of the way" overrides.

On release, the streak counter resets to zero. The next provider_credit_exhausted exit starts a fresh streak. No "cool-down with reduced N" — strict binary state.

Rejected: GitHub-webhook-driven release ("payment received" webhook). Outside scope of self-host; binds regatta to a specific billing provider; can be added later via the same substrate event seam.

## §7 Acceptance criteria

- **c1** — Given `consecutive_exits=3, window=5m`, when the spawner emits 3 `agent.exited` events with `exit_reason=provider_credit_exhausted` in 4min with no other-reason exit in between, then the next `scheduler.Tick(ctx)` returns `(nil, nil)`, `ListSpawnable` is NOT called, and a single `KindDispatchHalted` substrate event is appended.
- **c2** — Given a halted state, when an operator runs `regatta dispatch resume`, then within 2 Tick intervals the gate releases, a single `KindDispatchResumed` event with `release_reason=operator` is appended, and the next Tick proceeds through the full step loop including `ListSpawnable`.
- **c3** — Given `consecutive_exits=3` and 2 `provider_credit_exhausted` exits followed by 1 `completed` exit, when a 3rd `provider_credit_exhausted` arrives, then the streak count is 1 (the `completed` reset it), no halt fires, and no substrate event is emitted.
- **c4** — Given a halted state with `auto_release_after=1h`, when 60min wall-clock elapses (sealed test clock advances), then the next Tick releases with `release_reason=auto_cooldown` and a `KindDispatchResumed` event lands.
- **c5** — Given `enabled=false`, the gate is a no-op: `Record` is never called, `Evaluate` returns `Halted=false`, no substrate events emitted regardless of exit streak. Zero per-Tick overhead — single nil-check, identical hot path to the cost-gate's disabled short-circuit.
- **c6** — Given `strict_mode=true`, a single `provider_credit_exhausted` exit halts dispatch on the next Tick (effective `consecutive_exits=1` regardless of config block value).
- **c7** — Streak detection is "consecutive in chronological order of `Record(at)` arguments". A clock-skewed `Record` call (timestamp older than the previous record) is ignored (warn-log, do not abort). Bound the ring by `consecutive_exits` capped at 16 entries to prevent unbounded growth on misconfig.
- **c8** — `regatta dispatch status` reads the latest `KindDispatchHalted` / `KindDispatchResumed` events from the substrate tail and prints the derived state. Live daemon in-memory state and substrate-derived state MUST agree at steady state (within one Tick of any transition).

## §8 Test plan

Failing tests land FIRST per `feedback_tdd_discipline`. Coverage matrix:

### Unit (`internal/orchestrator/scheduler/providerhalt/`)

- `TestCounter_StreakResetByOtherReason` — covers c3.
- `TestCounter_StreakHonorsWindow` — 3 credit_exhausted exits but exit 1 and exit 3 are 6min apart → streak=2 (the trailing two, exit 1 falls out of window).
- `TestCounter_ClockSkewRecordIgnored` — covers c7's skew branch.
- `TestGate_EvaluateHaltsAtThreshold` — covers c1 (in-memory layer only).
- `TestGate_EvaluateAutoReleasesAfterCooldown` — covers c4.
- `TestGate_ReleaseEmitsResumeEvent` — verifies one event per transition (idempotent — second Release call no-ops).
- `TestGate_DisabledShortCircuits` — covers c5 (zero events, zero allocs via `testing.AllocsPerRun`).
- `TestGate_StrictModeOverridesThreshold` — covers c6.

### Integration (`internal/orchestrator/scheduler/scheduler_provider_halt_gate_test.go`)

- `TestTick_HaltedGateSkipsListSpawnable` — wires Gate into a real Scheduler, fakes a halted state, asserts `db.ListSpawnable` is never called (mock with call counter).
- `TestTick_ResumeIntentConsumedByGate` — inserts a `dispatch_resume_intents` row via the test DB, advances Tick, asserts gate is released + row is CAS-marked consumed + a second Tick doesn't double-release.
- `TestSpawnerRecordsExitReason` — verifies the spawner→ProviderHaltRecorder wiring fires on agent.exited (closes the integration gap that would otherwise let unit tests pass while production wiring drifts).

### End-to-end (`cmd/regatta/dispatch_test.go`)

- `TestDispatchStatus_PrintsHaltedState` — golden test against `regatta dispatch status` output, halted + live + auto-release-pending variants.
- `TestDispatchResume_InsertsIntent` — `regatta dispatch resume` inserts a row; second invocation pre-consumption is idempotent (does NOT insert a duplicate); post-consumption inserts a fresh row.

### Property / fuzz

- `FuzzCounterStreak` — random sequences of `(reason, timestamp)` records; invariant: `Streak()` never exceeds the number of consecutive `provider_credit_exhausted` entries in the input, ending at the most recent record, all inside `window`. Standard go fuzz harness, seeded with the unit-test inputs.

### TDD order verification

`git log --reverse fix/1096-provider-halt-gate -- internal/orchestrator/scheduler/providerhalt/` MUST show RED commit first, GREEN commit second. PR body MUST include the RED output (per `feedback_tdd_discipline`).

## §9 Out of scope (defer / Phase-X)

- **Provider-rate-limit halt** — same gate shape, different exit_reason. Trivially additive (parameterize the watched reason set) but YAGNI for v1: rate limits are usually self-curing within minutes via Anthropic's backoff; credit exhaustion is the only reason that needs human action. File as followup if a 24h dogfood window sees ≥3 rate-limit storms.
- **Multi-provider halt independence** — when regatta gains OpenAI / Gemini backends, the halt-gate counter must key on `(provider, exit_reason)` not just `exit_reason`. Defer until the second provider lands. The `providerhalt.Counter` API is shaped to accept this extension without a breaking change.
- **Webhook-driven release** — see §6 rejection.
- **Cross-daemon halt coordination** — if two regatta daemons share an Anthropic account, halt on daemon A does NOT inform daemon B. Cross-ref `2026-06-08-cross-daemon-shared-cost-ledger.md` — the same ledger could carry halt state. Defer; single-operator-single-daemon is the self-host norm.
- **Operator pager notification on halt transition** — substrate event is enough for v1; chat-notifier integration (`2026-06-08-chat-notifier-integration.md`) can subscribe in a follow-up.
- **Per-model halt** — different Anthropic plans / models have separate credit pools? Unverified. If true, key the gate by `(model, exit_reason)`. Investigate via empirical Anthropic API behavior in a separate research wedge.

## §10 Open questions

- **Q1** — Should `auto_release_after=0` mean "never auto-release" or "release immediately"? Spec picks NEVER (manual-only) because the immediate-release interpretation collapses the gate into a single-shot warn. Confirm via operator preference.
- **Q2** — Should the streak counter reset on a daemon restart? Spec picks YES (in-memory only) per §4.1 rationale. The cost-gate, the prwatch backoff, and the recheck backoff all reset on restart — consistency over persistence.
- **Q3** — Should `regatta dispatch resume` accept a `--reason <text>` flag that lands in the substrate event payload? Adds operator-audit value; no impl cost beyond a flag pass-through. Recommend YES; mark as a polish item not a v1 blocker.
- **Q4** — Should the heartbeat log (§4.4) be every 30s or every Tick (which is typically 5s in dogfood)? 30s is the spec default; tunable via `dispatch.provider_halt.heartbeat_seconds` if operators report it's too sparse / too noisy.
- **Q5** — Does the classifier need a new signature for `Your credit balance has been exhausted` (longer-form Anthropic message)? Spot-check upstream Anthropic error catalog before merge. If yes, file as a one-line `exit_reason.go` addition — does NOT block this spec.

## §11 Risks + mitigations

- **R1 — Halt-on-flake** — a single misclassification (e.g. an internal-server-error response that happens to contain the substring `insufficient_credits` in a longer message) could contribute to a false streak. Mitigation: classifier signatures are case-folded substring matches on a 4KB tail (`classifyHaystackCap`); the three current signatures (`Credit balance is too low`, `credit_balance_low`, `insufficient_credits`) are narrow enough to make false positives rare. Adversarial review MUST hunt for English error-message overlap.
- **R2 — Stuck-halted** — operator's `regatta dispatch resume` works, but the daemon is wedged so it never consumes the intent row. Mitigation: heartbeat log (§4.4) makes the wedge visible; the next-tick-consumption model means a healthy daemon clears the intent within 1 Tick of any healthy Tick happening; persistent failure is a separate orchestrator-health issue. Auto-release-after acts as a backstop.
- **R3 — Schema drift** — adding two substrate event kinds touches the substrate event enum; ALL consumers of the event tail (dashboards, replay-diff, `regatta dispatch status`) must handle the new kinds. Mitigation: substrate readers default to ignore-unknown-kind per existing event-enum contract; a missing handler is a missing surface, not a crash.
- **R4 — Test flake on clock-based windows** — sliding-window assertions are notorious for flake. Mitigation: ALL clock reads thread through `Config.Clock`, tests pin a fake clock via the same `time.Now`-injection seam used by Scheduler + spawner; zero `time.Sleep` in any test (per `check-no-bare-sleep`).

## §12 Implementer brief (dispatch-ready)

Slug: `provider-halt-gate`
Migration N: pinned at dispatch time; one new migration adds the `dispatch_resume_intents` table.
File scope:

- ADD `internal/orchestrator/scheduler/providerhalt/{counter,gate,counter_test,gate_test}.go`.
- ADD `internal/orchestrator/scheduler/scheduler_provider_halt_gate.go` (analog of `scheduler_cost_gate.go`).
- ADD `internal/orchestrator/scheduler/scheduler_provider_halt_gate_test.go`.
- EDIT `internal/orchestrator/scheduler/scheduler.go` `Tick`: insert `gate_provider_halt` step BEFORE `gate_l0`.
- EDIT `internal/orchestrator/spawner/spawner.go` `Config`: add `ProviderHaltRecorder` seam; spawner calls `Record` from the agent.exited emission path.
- ADD substrate event kinds (KindDispatchHalted, KindDispatchResumed) to `internal/orchestrator/state/substrate/event.go`.
- ADD `internal/orchestrator/state/migrations/NNN_dispatch_resume_intents.sql`.
- ADD `cmd/regatta/dispatch.go` for `dispatch status` + `dispatch resume`.
- EDIT `internal/orchestrator/state/agents.go` only if the resume-intent CAS helper lives there (else colocate with the migration).

Independent reviewer required (load-bearing — scheduler hot path + new CLI verbs). Per `feedback_no_self_tagged_approve`: spawn fresh-slot reviewer BEFORE the APPROVE token lands. `Reviewer-agent-id:` + `Reviewer-recommendation: APPROVE` in PR body footer (bare, not in a code block).

No automerge from implementer; `gh pr ready <N>` only.

## §13 Adversarial

Independent reviewer (cavecrew-reviewer or equivalent) MUST be spawned BEFORE the APPROVE token lands on any implementer PR. Findings:

- Counter-state lost across restarts: tolerable by design (operator credit-load is the upstream fix, not in-memory persistence).
- Window race under concurrent agent.exited bursts: counter MUST guard with a mutex; covered in `counter_test.go::TestRace_ConcurrentRecord`.
- Auto-release cool-off vs operator-manual resume: cool-off path MUST emit KindDispatchResumed with `actor=auto`; operator path with `actor=<id>`. Substrate audit must distinguish.

## §14 Reopen trigger

Reopen this spec when ANY of:

- A second provider's credit-exhausted signal lands (BYOM): the `provider_credit_exhausted` enum value becomes per-provider and the gate keys by `(provider, account)`.
- Operator overrides cool-off > 5x per week: indicates default window is wrong.
- A non-credit `exit_reason` recurs N≥3 consecutive and should also halt (e.g. `provider_rate_limited` storm).
