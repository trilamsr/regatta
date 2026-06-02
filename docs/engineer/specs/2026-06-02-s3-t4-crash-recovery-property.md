# S3-T4 — Crash-Recovery Property Test Design Spec

Status: ready for review
Date: 2026-06-02
Author: design subagent <tri@lumalabs.ai>
Binding brief: `docs/engineer/briefs/2026-06-01-self-host-first.md` §3 S3-T4
Depends on (must be merged to main before S3-T4 dispatches):
  - Substrate Wave 1 (event log + HMAC + migration 0005) — SHIPPED
  - W9 substrate-default `DurableHistory` impl (#328) — spec landed; replay+diff harness reused as the verification primitive

Memory rules in force: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_pr_body_file_only`, `feedback_test_godoc_one_line`, `feedback_deletion_default`, `feedback_doc_check_banned_phrases`, `feedback_pr_body_release_notes_fence`, `feedback_unaddressed_load_bearing`, `feedback_spec_pattern_authority`.

---

## §1 Goal + non-goal

### 1.1 Goal

Prove — via 2 000 randomized fault-injection cases — that for any crash point inside a scheduler tick, the substrate event log plus scheduler state machine recover to a state semantically indistinguishable from the no-crash baseline. Concretely: `recover(crash@k) → tick(N+1)` reaches the same reducer-folded final state as `tick(N) → tick(N+1)` with no crash.

The acceptance bar that S3-T4 raises:

- No double-spawn — `work_item.state` never transitions out of `pending` twice for the same id.
- No lost work_item — every wi that was eligible pre-tick is either spawned or still `pending` post-recovery.
- No phantom spawn — no wi is `spawning`/`running` whose substrate trail lacks a matching `node_output` write.

### 1.2 Non-goal

- Not a chaos-engineering harness (no kernel-level kills, no fs corruption).
- Not a sqlite durability test (we trust WAL fsync; substrate W1 already pins it).
- Not a Jepsen-style multi-node consensus test (regatta is single-process; one scheduler, one sqlite file).
- Not a mutation-testing run (S2-T4 owns that).

---

## §2 In / Out

### IN

- One new test file `internal/orchestrator/scheduler/scheduler_crash_recovery_property_test.go`.
- One small fault-injection seam: `scheduler.WriteHook` (function pointer; nil-default no-op) consulted before every substrate append inside `Tick`. Same shape as the existing `DowngradeHook`.
- Reuse W9 substrate-default `DurableHistory.Replay` (#328) as the post-crash state reader.
- Reuse `pgregory.net/rapid` already vendored at `internal/orchestrator/state/cycle_check_property_test.go`.

### OUT

- Process-level kill (`os.Exit`). Rejected — see §3.3.
- A new package. The seam + test fit inside `internal/orchestrator/scheduler/`.
- Production wiring of `WriteHook` beyond test injection. The hook stays nil in prod.

---

## §3 Architecture

### 3.0 Relation to existing `scheduler_reserve_crash_test.go`

The existing single-case panic test at `internal/orchestrator/scheduler/scheduler_reserve_crash_test.go` exercises *one* crash point inside `TransitionAgentTx` and asserts the `defer tx.Rollback()` safety net holds. S3-T4 generalises that test pattern from one fixed crash site to 200 randomized crash points spanning every substrate `Append` inside `Tick`. The existing test stays — it is a focused regression for the rollback path; S3-T4 is the property-level invariant.

### 3.1 Prior art adopted (≥2 OSS, per `feedback_research_design_principles`)

| Pattern | Source | What we adopt |
|---|---|---|
| Property-based test driver | [`pgregory.net/rapid`](https://github.com/flyingmutant/rapid) — already vendored, already used in `internal/orchestrator/state/cycle_check_property_test.go` + `internal/orchestrator/state/substrate/reducer_property_test.go` | Generator combinators (`rapid.IntRange`, `rapid.SliceOfN`), shrink-on-failure, `rapid.Check` test entry. Adoption cost = zero new deps. |
| Error-injection at I/O boundary | [etcd's `failpoint`](https://github.com/etcd-io/gofail) — production-tested error-injection seam used to crash etcd at every persistent-state write | Inject at the substrate `Append` boundary, not at the syscall boundary. One hook, one error sentinel, deterministic seed-replayable. |
| Replay-as-oracle | [Foundation DB simulation testing](https://apple.github.io/foundationdb/testing.html) — drives the system, snapshots state at deterministic checkpoints, re-runs from snapshot, asserts equivalence | Our checkpoint = the substrate event log itself (append-only, HMAC-signed, already the source of truth post-W9). Our re-run = replay+diff harness from #328. |
| `std/quick.Check` shape | [Go std library](https://pkg.go.dev/testing/quick) | Considered. Rejected — `quick` has no shrinking, no labels, no rapid-style seeded reproduction. `rapid` strictly dominates for our use. |

Adoption-first holds: the only new code is glue (the seam + the assertion); the property-driver, the fault-injection idea, and the replay-as-oracle are all borrowed.

### 3.2 Property statement (the test invariant)

For any scheduler tick `N` over a queue `Q` of pending work items, for any crash point `k ∈ [0, writes_per_tick(N))`:

```
recover( crash_at(tick(N), k) ) → tick(N+1)  ≡_state  tick(N) → tick(N+1)
```

where `≡_state` is reducer-folded equivalence over the substrate event log restricted to kinds `{node_output, approval_event, gate_verdict, heartbeat}` plus the scheduler-owned `work_items` table rows for `state`, `lane`, `attempt`.

Three derived sub-properties (each asserted independently in the test body so failures pinpoint the violation):

- **P-NoDouble** — for every wi id, `count(state-transition pending→spawning) ≤ 1` across the crash-and-recover trace.
- **P-NoLoss** — `set(eligible-pre-tick) ⊆ set(spawned-after-recovery) ∪ set(still-pending-after-recovery)`.
- **P-NoPhantom** — for every wi where post-recovery `state ∈ {spawning, running}`, there exists a `node_output` event with matching `work_item_id` in the substrate log (i.e. the substrate trail is consistent with the table state).

### 3.3 Fault-injection primitive — PICK: error-injection at every substrate `Append` boundary

**Decision: error-injection, not process kill.**

Why not `os.Exit`/process kill:

- The Go test runner can't observe state from a killed process — the assertion harness would have to re-exec the binary and reattach to sqlite, which (a) doubles runtime per case and (b) demands a separate `main`-package shim that doesn't otherwise exist.
- Crash points inside a tick are between sqlite-WAL fsyncs, not between processes. sqlite already guarantees WAL durability past fsync — what we need to test is the application-level state machine's ability to resume from a partially-written substrate trail, not the OS's.
- `failpoint`/`gofail`-style in-process error injection is the OSS-validated pattern for this exact failure mode (cited §3.1).

The seam:

```go
// scheduler.go (production code — one new field, one default-nil)
type Scheduler struct {
    // … existing fields …
    // WriteHook fires before every substrate Append inside Tick.
    // Returning a non-nil error aborts the tick mid-flight, simulating
    // a crash at write index k. Nil-default; production wires nil.
    WriteHook func(writeIndex int) error
}
```

Total production surface added: one field, one nil-default check. T1 threads a local `writeIndex int` counter through `Tick`'s existing inner loop and passes it to the hook. The counter is hook-local — not exported, not OTel-exposed, not part of the production observability surface.

**Crash semantics**: `WriteHook` returning `errCrashSim` causes `Tick` to return early via the existing error-return path. No partial-state cleanup runs (that is the point — we're testing recovery, not graceful shutdown). The sqlite transaction wrapping the in-flight `Append` rolls back, leaving the substrate log in exactly the state it was in before write `k`.

### 3.4 Test runner — 200 crash points × 10 ticks = 2 000 cases

**Sizing decision:**

- `numCrashPoints = 200` per tick — matches brief. Drawn via `rapid.IntRange(0, writes_per_tick-1)` with shrinking enabled so failures collapse to the smallest reproducer.
- `N = 10` consecutive ticks per case — long enough to exercise multi-tick recovery (a crash in tick 7 followed by recovery, then ticks 8-10 must still converge), short enough to keep per-case wallclock bounded.
- Total cases = 2 000. Each case independently seeds a `t.TempDir()` sqlite + fresh substrate log + fresh scheduler.

**Runtime budget — PICK: ≤90 s wallclock on CI, gated by PHASE-S-RELAX**

- Per `feedback_gate_relaxation_phase_s` (ACTIVE during self-host window), heavy property runs are halved on default CI. The test ships with two modes:
  - default (`go test ./...`): 200 cases, ~30 s. Asserts the property holds on the smaller sample.
  - `-tags=property_full` (nightly + pre-release): 2 000 cases, ≤90 s. Full per-brief sample.
- Per-case budget = 90 s / 2 000 = 45 ms. Sqlite-in-tempdir setup is ~5 ms; 10 ticks × ~2 ms = 20 ms; replay+diff = ~10 ms; overhead 10 ms. Fits.
- The test calls `t.Parallel()` and rapid's per-goroutine state is independent, so wallclock scales with `GOMAXPROCS`. CI runs `GOMAXPROCS=4` → expected ≈25 s.

If wallclock regresses past budget post-merge, the followup is filed under `docs/engineer/followups.md` per `feedback_unaddressed_load_bearing` and gated behind the property_full tag, not skipped.

### 3.5 Replay+diff verification harness (W9 reuse)

Verification piggy-backs on the W9 spec's `DurableHistory.Replay(ctx, runID, opts)` substrate-default impl (#328 §3.1). The test asserts equivalence by:

1. **Baseline run** — execute `tick(N)` and `tick(N+1)` on a fresh substrate; capture `baseline := DurableHistory.Replay(ctx, runID, {until: end})`.
2. **Crash run** — on a separate fresh substrate, execute `tick(N)` with `WriteHook = crashAt(k)`; the tick errors mid-way. Open a new scheduler against the same sqlite + substrate (this is the "recovery" — the new scheduler is the recovered process); run `tick(N+1)`; capture `recovered := DurableHistory.Replay(ctx, runID, {until: end})`.
3. **Diff** — assert `diff(baseline, recovered) == ∅` per the W9 diff harness (`spec §3.4`, reducer-aware comparison restricted to the kinds in §3.2).
4. **P-NoDouble / P-NoLoss / P-NoPhantom** — assert the three sub-properties directly against `recovered`'s reducer-folded state. Diffing alone would catch any of them, but the explicit sub-property assertions give a focused failure label per `rapid.T.Logf`.

If #328 has not merged when S3-T4 dispatches, T1 of this spec lands a 30-line inline replay-fold helper that mirrors W9's substrate-default impl; we re-converge on the W9 impl in a followup.

### 3.6 File-disjoint task breakdown

One implementer subagent owns the spec. The work splits into 3 file-disjoint tasks per `feedback_plan_subagent_dup_files`:

| Task | File | Scope |
|---|---|---|
| T1 — fault-injection + clock seam | `internal/orchestrator/scheduler/scheduler.go` (existing) | Add `WriteHook func(writeIndex int) error` and `NowFunc func() time.Time` fields to `Scheduler` struct + nil-default checks + writeIndex threading inside `Tick`. ≤30 LoC delta. |
| T2 — property test | `internal/orchestrator/scheduler/scheduler_crash_recovery_property_test.go` (new) | The 2 000-case driver, sub-property asserts, baseline+crash harness. ~250 LoC including test fixtures. One-line godoc per `feedback_test_godoc_one_line`. |
| T3 — followup file + nightly tag | `docs/engineer/followups.md` (existing) + `.github/workflows/nightly.yml` (existing) | Append followup line for `property_full` budget regression; add `-tags=property_full` step to nightly workflow. ≤15 LoC delta. |

T1 + T2 sit under the same package (`scheduler`) but in different files; rebase order T1 → T2 → T3.

---

## §4 Risk register

### R1 — Non-determinism in scheduler.Tick masks recovery bugs

Tick currently reads `time.Now()` for heartbeat stamps. A crash-recovery test that uses real clocks will see clock drift between baseline and crash runs, producing false-positive diffs.

**Mitigation**: T1 adds one nil-default `NowFunc func() time.Time` field on `Scheduler` (same shape as `WriteHook`; nil → `time.Now().UTC()`). Test injects a monotonically-incrementing fake. No equivalent seam exists today — verified by `grep -rn NowFunc internal/` returning empty — so the spec owns introducing it. Production wires nil; ≤6 LoC delta in `scheduler.go`.

### R2 — `WriteHook` field grows into a general-purpose interceptor

A function-pointer hook is the smallest seam; the temptation will be to extend it to `ReadHook`, `LockHook`, etc.

**Mitigation**: spec pins the hook as test-only; `feedback_spec_pattern_authority` blocks extension without a fresh design spawn. Add a `// WriteHook is test-only; see docs/engineer/specs/2026-06-02-s3-t4-crash-recovery-property.md.` godoc on the field.

### R3 — 2 000 cases × 10 ticks runs flaky on CI under contention

GitHub-hosted runners are noisy; the 90 s budget may flip flaky.

**Mitigation**: PHASE-S-RELAX gating + `property_full` tag means the 2 000-case run only fires on nightly + pre-release. Default CI runs the 200-case quick-mode. Sub-property asserts (not just diff) fail fast — a flake on diff alone re-runs once via `go test -count=2` in the workflow; sub-property failures never re-run.

### R4 — Replay-fold helper drift if W9 #328 lands after S3-T4

T2's inline 30-line helper must converge on W9's substrate-default impl.

**Mitigation**: T2 imports `internal/orchestrator/state/substrate/fold.go` (already lands in W1) for the reducer, so the helper is ≤30 lines of glue not a parallel implementation. If #328 ships after S3-T4, a 1-PR followup swaps the helper for the W9 `DurableHistory.Replay` call. Followup line filed at PR time per `feedback_unaddressed_load_bearing`.

### R5 — Sqlite WAL checkpoint mid-test corrupts the property's premise

If sqlite WAL-checkpoints during a crash run, the "rollback on uncommitted tx" assumption may not hold for events that crossed checkpoint boundaries.

**Mitigation**: test opens sqlite via `sql.Open("sqlite", path)` then issues `db.Exec("PRAGMA wal_autocheckpoint = 0")` before running ticks. The substrate package does not currently expose a WAL-knob helper (verified: `grep -rn wal_autocheckpoint internal/` empty), so T2 issues the PRAGMA directly from the test setup helper. No production change.

### R6 — `errCrashSim` sentinel leaks into production error paths

A test sentinel returned from `WriteHook` must never match production error-equality checks (`errors.Is`).

**Mitigation**: the sentinel is package-private to the test file (`var errCrashSim = errors.New("crash-sim")`). The hook field returns `error`; production wires `nil`. No production codepath constructs or checks `errCrashSim`.

### R7 — Hook called inside a sqlite transaction may leave locks held on test goroutine exit

If `WriteHook` returns an error mid-transaction and the test goroutine exits without rolling back, the sqlite connection pool may report locked-rows on the next case.

**Mitigation**: `Tick`'s existing error-return path already wraps writes in `tx, err := db.BeginTx(...); defer tx.Rollback()` — the rollback fires on hook-induced error identical to any other error. The test uses `t.Cleanup(func() { db.Close() })` so connections never leak across cases.

---

## §5 Grade rubric

### B (floor — ships)

- T1 + T2 land. 200-case mode passes on every PR; the assertion catches a hand-injected bug (e.g. deleting the `tx.Rollback()` line) and fails-loud.
- One-line godoc on the test function per `feedback_test_godoc_one_line`.
- PR body includes a ```release-notes``` fence and the A+ scorecard.
- Followup line filed if any A+ rubric item is deferred.

### A (target — expected)

- All B items.
- T3 lands: nightly workflow runs `property_full` mode (2 000 cases) and uploads the rapid-shrunk failure seed as a CI artefact on failure.
- The seam (`WriteHook`) is the only new production surface — no test-only flags on production types, no `if testing.Testing()` branches.
- Sub-property asserts (P-NoDouble / P-NoLoss / P-NoPhantom) fire independently of the W9 diff harness, so a future W9 spec churn does not silently break this test.

### A+ (stretch — exceptional)

- All A items.
- Test harness factored so it can extend to other tick-driven state machines (cost-governor reconciler, approval-reaper) by passing the state-machine under test as a `TickFunc func(ctx context.Context) error`.
- Rapid seed + shrunk minimal reproducer logged on every failure (rapid does this by default; test calls `rt.Logf("seed=%v", rapid.Seed())` so the seed lands in the test output stream too).
- Followup filed: cost-governor + approval-reaper crash-recovery property tests using the same harness (1 followup line each, not a new spec).

---

## §6 Sequencing

- **Pre**: W9 substrate-default impl (#328) ideally merged. If not, T2 ships the inline helper; followup converges.
- **S3-T4 dispatch**: T1 → T2 → T3 in three commits on one branch; one PR.
- **Post**: PR merges with PHASE-S-RELAX default CI mode (200 cases); first nightly run validates the 2 000-case `property_full` mode.

No spec dependency follows S3-T4 — it is a leaf in the Phase S graph.

---

## §7 Deferred (named-but-not-shipped per `feedback_unaddressed_load_bearing`)

- Cost-governor crash-recovery property test — same harness, different state machine. Filed as followup at PR time.
- Approval-reaper crash-recovery property test — same harness, different state machine. Filed as followup at PR time.
- Multi-process crash recovery (regatta restarts mid-tick) — not in S3-T4 scope; deferred to Phase X if/when regatta gains a daemon-restart story.
- Sqlite-file corruption recovery — out of scope; substrate W1 trusts sqlite WAL fsync per its own spec.

## Resolution (2026-06-02)

Shipped across #366 (`feat(scheduler): WriteHook fault-injection seam`), #382 (`test(scheduler): crash-recovery property test (rapid 200/2000 cases)`), #391 (`ci(crash-recovery): nightly 2000-case property sweep + make target`), and #394 (`test(cost+approval): crash-recovery property tests + factor golden-DB clone`). Property harness now covers scheduler, cost-governor, and reaper state machines.
