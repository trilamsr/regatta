# MVP-3 W9 — Replay + Diff Harness — Implementer Task Breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md` (#292, MERGED). Read fully §3 Architecture (3.1–3.7), §5 Risk register (R1–R10), §8 File-disjoint implementation preview (T1–T6), §9 Sequencing.

Authority: `feedback_spec_pattern_authority` — implementer deviation from this spec MUST re-spawn the design subagent.

Decision priority (`feedback_decision_priority`): UX → ease → performance → best-practices → speed → velocity; long-term > short-term. UX gate is the operator clicking "replay" in the W7 UI and watching diff facts stream in over the same 5 s TailFacts cadence they already use for approvals. Every scope-trim defends that gate.

**Substrate-choice locked: Option C hybrid** — DurableHistory Go interface with substrate-default impl; Temporal-backed impl gated behind the P2.5 trigger as a design-only stub. Source: `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md` (locked predecessor, cited in spec preamble). **DO NOT re-litigate Temporal-vs-bespoke at the implementer layer.** Deviations on this choice route through the design subagent per `feedback_spec_pattern_authority`.

---

## Wave overview

- **6 file-disjoint implementer tasks** per spec §8. T1 (DurableHistory interface + substrate-default impl), T2 (Replay() + diff harness), T3 (operator UI replay button + bg job), T4 (OTel attrs + non-determinism quarantine), T5 (P2.5 trigger metrics + alert), T6 (Temporal-backed impl design doc; markdown stub only).
- **Dispatch shape — single wave with internal gating:**
  - **Wave A (parallel):** T1 + T4 + T5 + T6 dispatch in parallel. T1 owns the interface + substrate impl; T4 writes its OTel helpers + `nondeterministic.Mark` quarantine API against the import path T1 reserves (no runtime dep — T4 ships span-attr helpers + the quarantine mark API in disjoint files); T5 ships metrics in a disjoint file; T6 is markdown-only.
  - **Wave B (sequential after T1 merges):** T2 (Replay engine + diff harness; imports `DurableHistory` from T1).
  - **Wave C (sequential after T2 + W7 Wave 1 T4/T6 land):** T3 (operator UI replay button + bg job; imports T2's Replay engine + W7's `internal/web/` scaffold + W8 OPA Authorizer middleware).
- **Hard prereqs (merged to main before dispatch):**
  - W6 OTel T1–T5 — shipped (PRs #172, #169, #209, T4, #210). Replay reads `trace_id` from substrate event; `Config.Tracer` injection seam reused by T4.
  - Substrate Wave 1 (T-S1 + T-S2 + T-S3) — `docs/engineer/plans/2026-06-01-substrate-w1-tasks.md` dispatch pending; **T1 cannot dispatch until T-S1/T-S2/T-S3 merge.**
  - W7 Wave 7.0 T1 (#263 HTTP listener) shipped; W7 Wave 1 T4/T6 (#268 plan execution) **must merge before T3 dispatches.**
  - W8 OPA RBAC — spec shipped (#266); **impl must merge before T3 dispatches** (T3's POST handler takes `Principal` per W7 §3.6.4 and the W8 Authorizer middleware gates at the boundary).
- **Migration number lock (`feedback_migration_number_lock`):** **ZERO new SQL migrations.** Substrate spec §2.1 (`substrate_events` with `tenant_id`, `payload_json`, `supersedes`, signing columns, `idx_substrate_events_kind`) + W6 §3.5 (`trace_id`, `span_id` columns) cover every column W9 needs. If any implementer surfaces a load-bearing schema need, the migration number is whatever substrate W1 finalises +1 (pinned at dispatch time, not implementer-picked); the design subagent must be re-spawned BEFORE writing it.
- **Concurrency cap:** peak 4 parallel implementers in Wave A (T1 + T4 + T5 + T6); then T2 alone in Wave B; then T3 alone in Wave C. Well within `feedback_dispatch_strategy` 10-lane cap.
- **Deletion default (`feedback_deletion_default`) — what gets smaller in W9 v1:**
  - **No parallel `history` table.** Substrate `events` is the only journal. The brief considered a regatta-owned journal separate from substrate; spec §4 collapses storage onto substrate. Net SQL delta: zero migrations.
  - **No new `regatta replay` CLI in v1.** The brief named `regatta replay <run_id> --from=<node>`; v1 ships the operator-UI trigger only. CLI lands as a thin shim in a v2 follow-up. Saves ~200 LoC of CLI parsing + flag plumbing.
  - **Temporal SDK dependency kept out of v1 go.mod.** T6 is design-only markdown; no Temporal import lands until §3.6 trigger fires. Build weight: zero.
  - **`PinSet` declared but unimplemented.** The interface signature accepts edit-and-replay knobs (v2 doesn't break the API); v1's substrate impl rejects non-zero `PinSet` with `ErrUnsupported`. Saves ~80 LoC of v1 pin-loader scaffolding.
  - **No bespoke LLM-variance detector.** Spec §3.3 non-determinism quarantine via `nondeterministic.Mark(ctx, reason)` collapses R1 + R4 into one mechanism (re-executors that touch clock/rand/network self-mark; LLM-touching events are read verbatim from `supersedes` chain and report `Match` by definition). Saves a parallel "is-this-LLM-output" heuristic that would have shipped to address R4 separately.
- **Phase positioning:** W9 v1 ships interface + substrate impl + diff harness + UI trigger + metrics. **OUT (v2 follow-ups, filed pre-dispatch per §8 below):** CLI shim, partial replay (`--from=<node>`), edit-and-replay (`--pin-model=<model>`), Temporal impl, cross-tenant admin UX, 1M-event load test, W10 attestation chain.

---

## §1 File-disjoint table

| Task   | Path (exclusive write scope) | Depends-on (Wave 1 + main) | Effort | TDD tests (count: B/A/A+) |
| ------ | ---------------------------- | -------------------------- | ------ | ------------------------- |
| **T1** | `internal/history/durable_history.go` (NEW — interface + `ReplayOpts` + `PinSet` + `ReplayedEvent` + `DiffResult` + `DiffVerdict` types); `internal/history/substrate_impl.go` (NEW — substrate-default impl: `Append` + `Tail` + `Replay`); `internal/history/errors.go` (NEW — `ErrCrossTenant`, `ErrUnsupported` sentinels); `internal/history/doc.go` (NEW — one-line package godoc); `internal/history/durable_history_test.go` (NEW); `internal/history/substrate_impl_test.go` (NEW) | main + Substrate W1 merged | M | 8 (B 5, A 3) |
| **T2** | `internal/history/replay.go` (NEW — Replay engine streaming ReplayedEvent values); `internal/history/diff.go` (NEW — `Diff(orig, replayed, reducer) DiffResult` reducer-aware comparator); `internal/history/reexecutor.go` (NEW — `RegisterReExecutor` registry + four deterministic re-executors for `node_output` / `approval_event` / `gate_verdict` / `budget_reconciled`); `internal/history/replay_test.go` (NEW); `internal/history/diff_test.go` (NEW); `internal/history/reexecutor_test.go` (NEW) | T1 merged | M | 7 (B 5, A 2) |
| **T3** | `internal/uiserver/replay.go` (NEW — POST `/runs/{run_id}/replay` + GET `/runs/{run_id}/replay/{job_id}` handlers + background-job goroutine); `internal/uiserver/templates/replay_progress.tmpl` (NEW); `internal/uiserver/replay_test.go` (NEW) | T1 + T2 merged + W7 Wave 1 T4 + T6 merged + W8 Authorizer middleware shipped | M | 6 (B 4, A 2) |
| **T4** | `internal/history/otel.go` (NEW — span open/close helpers; attr setters for `regatta.replay.run_id`, `regatta.replay.original_trace_id`, `regatta.replay.divergence_count`, `regatta.replay.event_kind`, `regatta.replay.event_id`, `regatta.replay.nondeterministic`); `internal/history/nondeterministic.go` (NEW — `nondeterministic.Mark(ctx, reason)` quarantine API + active-span attr setter); `internal/history/otel_test.go` (NEW); `internal/history/nondeterministic_test.go` (NEW) | main (parallel with T1) | S | 3 (B 2, A 1) |
| **T5** | `internal/history/metrics.go` (NEW — three OTel meter instruments: `regatta.history.sqlite_contention_pct` gauge, `regatta.history.concurrent_programs` gauge, `regatta.history.replay_recovery_seconds` histogram); `internal/history/metrics_test.go` (NEW) | main (parallel with T1) | S | 5 (B 4, A 1) |
| **T6** | `internal/history/temporal/README.md` (NEW — design-only stub: trigger thresholds, swap procedure, dual-write window, v2 impl PR contract). **No Go code.** | main (parallel with T1) | XS | 0 (markdown only) |

**Total v1 effort estimate** (per spec §8): ~2.4 K LoC. T1 ≈ 700 + T2 ≈ 900 + T3 ≈ 400 + T4 ≈ 150 + T5 ≈ 200 + T6 = 0 (markdown).

### Disjointness verification (grep at plan time)

- T1's `internal/history/` files: `durable_history.go`, `substrate_impl.go`, `errors.go`, `doc.go`. T2's `internal/history/` files: `replay.go`, `diff.go`, `reexecutor.go`. T4's `internal/history/` files: `otel.go`, `nondeterministic.go`. T5's `internal/history/` files: `metrics.go`. All filename-disjoint by enumeration.
- T3 owns the only writes under `internal/uiserver/replay*` + `internal/uiserver/templates/replay_progress.tmpl`. No overlap with W7 Wave 1's `internal/web/` (different package; T3 lives in `internal/uiserver/` per spec §3.5).
- T6 lives under `internal/history/temporal/` (sub-package directory); zero overlap with T1/T2/T4/T5 which live at `internal/history/` directly.
- **Verdict: ZERO file overlap across T1–T6.** Per `feedback_plan_subagent_dup_files`, every dispatch prompt below cites the EXACT output path slug per task; implementers cannot accidentally collide on a new file because the prompt enumerates the full exclusive write list.

---

## Cross-task seam contracts (load-bearing — implementers MUST honour exactly)

These are the exports each task surfaces. Pinning them at plan time prevents the "T2 redesigns `DurableHistory` because T1's signature wasn't obvious" failure mode (per `feedback_shared_primitive_owner`).

### T1 exports (consumed by T2 + T3 + T4)

```go
// internal/history/durable_history.go
package history

// DurableHistory is the v1 read+replay surface; substrate-default impl
// ships in substrate_impl.go. Temporal-backed impl is design-only (T6).
type DurableHistory interface {
    Append(ctx context.Context, runID string, ev substrate.Event) error
    Tail(ctx context.Context, runID string, since string) (<-chan substrate.Event, io.Closer, error)
    Replay(ctx context.Context, runID string, opts ReplayOpts) (<-chan ReplayedEvent, io.Closer, error)
}

type ReplayOpts struct {
    TenantID     string                // required; substrate.DefaultTenantID for single-tenant
    IncludeKinds []substrate.EventKind // empty = all except KindHeartbeat
    FromNodeID   string                // reserved; v1 rejects non-empty (v2 partial-replay)
    PinOverride  PinSet                // reserved; v1 rejects non-zero (v2 edit-and-replay)
}

type PinSet struct {
    ModelID   string
    Seed      int64
    PromptSHA string
}

type ReplayedEvent struct {
    Original substrate.Event // journaled row, signature verified
    Replayed substrate.Event // re-derived; same id, recomputed payload_json
    Diff     DiffResult
}

type DiffResult struct {
    Verdict       DiffVerdict
    Reason        string   // empty for match; named cause for divergent/skipped
    DivergentKeys []string // empty for match/skipped
}

type DiffVerdict string

const (
    Match         DiffVerdict = "match"
    Divergent     DiffVerdict = "divergent"
    ReplaySkipped DiffVerdict = "replay_skipped"
)
```

```go
// internal/history/errors.go
package history

var (
    ErrCrossTenant = errors.New("history: cross-tenant event in fold")
    ErrUnsupported = errors.New("history: v1 does not support this ReplayOpts field")
)
```

### T2 exports (consumed by T3)

```go
// internal/history/reexecutor.go
package history

// RegisterReExecutor binds an EventKind to a function that re-derives
// the event payload given the journaled fold. Mirrors substrate
// T-S1's RegisterPayloadValidator pattern (init()-time registration).
func RegisterReExecutor(kind substrate.EventKind, fn ReExecutor)

type ReExecutor func(ctx context.Context, ev substrate.Event, fold []substrate.Event) (json.RawMessage, error)
```

```go
// internal/history/diff.go
package history

// Diff compares replayed payload vs original payload per event,
// reducer-aware (lww vs append per substrate spec §4). Canonical-JSON
// byte-equality via the existing substrate sign helper.
func Diff(orig, replayed substrate.Event, reducer substrate.ReducerStrategy) DiffResult
```

```go
// internal/history/replay.go
// No new exports beyond the DurableHistory.Replay impl; replay.go
// implements the streaming engine that pipes events through the
// re-executor registry + Diff.
```

### T3 exports (consumed by `internal/uiserver` mux at boot)

```go
// internal/uiserver/replay.go
package uiserver

// RegisterReplayRoutes mounts POST /runs/{run_id}/replay +
// GET /runs/{run_id}/replay/{job_id} on the supplied mux with the
// W8 Authorizer middleware + W7 Wave 1 T4's CSPMiddleware chained.
// Background-job ctx derives from the orchestrator root ctx; shutdown
// cancels in-flight jobs cleanly (R8).
func RegisterReplayRoutes(mux *http.ServeMux, deps ReplayDependencies)

type ReplayDependencies struct {
    History    history.DurableHistory
    DB         *state.DB
    Tracer     trace.Tracer  // W6 Config.Tracer injection seam
    Authorizer w8opa.Authorizer  // W8 OPA gate
    Clock      func() time.Time
}
```

### T4 exports (consumed by T2's Replay engine + every re-executor)

```go
// internal/history/otel.go
package history

// StartReplaySpan opens the root replay span (kind=internal) with
// regatta.replay.run_id + regatta.replay.original_trace_id pre-set.
// Caller defers span.End. SetDivergenceCount is called just before
// End to record the final count (§3.7).
func StartReplaySpan(ctx context.Context, tr trace.Tracer, runID, originalTraceID string) (context.Context, trace.Span)

func SetDivergenceCount(span trace.Span, count int)

// StartReExecuteSpan opens the per-event child span with
// regatta.replay.event_kind + regatta.replay.event_id pre-set.
func StartReExecuteSpan(ctx context.Context, tr trace.Tracer, kind substrate.EventKind, eventID string) (context.Context, trace.Span)
```

```go
// internal/history/nondeterministic.go
package history

// Mark records that the active replay span observed a non-deterministic
// source (clock, rand, network). Diff harness reads the attr and
// downgrades the event to ReplaySkipped with Reason=reason (§3.3).
func Mark(ctx context.Context, reason string)

// IsMarked is called by T2's Diff path to read the attr off the span.
// Returns (reason, true) if Mark fired during this re-execute; ("", false) otherwise.
func IsMarked(ctx context.Context) (string, bool)
```

### T5 exports (consumed by T1's substrate impl write-path + T2's Replay engine)

```go
// internal/history/metrics.go
package history

// MetricsRecorder wires the three P2.5 trigger instruments. T1's
// substrate_impl.go observes sqlite_contention_pct on write-side
// AppendEvent retries; T2's replay.go observes replay_recovery_seconds
// on Replay completion; the orchestrator observes concurrent_programs
// (instrument registered here; observation registered by orchestrator
// poll loop — out of W9 scope, [w9-followup] F6 doc PR cites the
// observation site).
type MetricsRecorder struct { ... }

func NewMetricsRecorder(meter metric.Meter) (*MetricsRecorder, error)

func (r *MetricsRecorder) ObserveContention(ctx context.Context, blocked bool)
func (r *MetricsRecorder) ObserveReplayRecovery(ctx context.Context, dur time.Duration)
```

### Owner declarations (per `feedback_shared_primitive_owner`)

- **T1 is OWNER of:** `DurableHistory` interface, `ReplayOpts`, `PinSet`, `ReplayedEvent`, `DiffResult`, `DiffVerdict`, `ErrCrossTenant`, `ErrUnsupported`. T2/T3/T4 import these by name; signature changes block their PRs.
- **T2 is OWNER of:** `RegisterReExecutor` registry shape + the four v1 re-executors + `Diff` comparator.
- **T3 is OWNER of:** `RegisterReplayRoutes` + the progress-page template.
- **T4 is OWNER of:** `StartReplaySpan`, `SetDivergenceCount`, `StartReExecuteSpan`, `Mark`, `IsMarked`. T2 imports `Mark`/`IsMarked` for the quarantine path; T2 imports `StartReplaySpan`/`StartReExecuteSpan` for span management.
- **T5 is OWNER of:** `MetricsRecorder` + the three meter-instrument names + units. Alert thresholds live in operator config (out of W9 code scope; [w9-followup] F6 doc PR appends to `docs/operator/observability.md`).
- **T6 is OWNER of:** the design-only stub at `internal/history/temporal/README.md`. Concrete impl PR lands ONLY when the §3.6 trigger fires.

**Shared primitive: `DurableHistory` interface is T1-owned.** T2, T3, T4 consume it by name; any deviation requires re-spawning T1's design subagent per `feedback_spec_pattern_authority`.

---

## §2 Task T1 — DurableHistory interface + substrate-default impl

### Scope (exclusive write paths)

- `internal/history/durable_history.go` (NEW) — interface + types per cross-task seam.
- `internal/history/substrate_impl.go` (NEW) — `Append` thin-wraps `substrate.AppendEvent`; `Tail` SELECTs `WHERE run_id=? AND id > ? ORDER BY written_at, id` at caller cadence using `idx_substrate_events_kind`; `Replay` opens a substrate read tx, folds `substrate.Fold(runID, kind)` for every kind in `opts.IncludeKinds`, merges into journal-ordered stream, pipes through `runReExecutor(event)` (registry owned by T2; T1 calls via the registered hook). Cross-tenant safety: rejects with `ErrCrossTenant` if any folded event's `TenantID != opts.TenantID`.
- `internal/history/errors.go` (NEW) — `ErrCrossTenant`, `ErrUnsupported` sentinels.
- `internal/history/doc.go` (NEW) — one-line package godoc per `feedback_comments_discipline`.
- `internal/history/durable_history_test.go` (NEW) + `internal/history/substrate_impl_test.go` (NEW) — TDD tests below.

### Prereqs (cite spec sections)

- Spec §3.1 — DurableHistory interface signatures verbatim; v1 reserved-field rejection semantics.
- Spec §3.2 — `ReplayedEvent` + `DiffResult` shapes.
- Spec §4 — reducer-aware fold contract (substrate's existing `defaultReducer(kind)`).
- Spec §5 R7 — cross-tenant defence in depth.
- Spec §6 T1 — B-tier + A-tier test names.
- Spec §8 T1 — file scope.
- Substrate spec §2.1 + §2.3 + §5 — `substrate_events` schema, `Fold` semantics, `Verify` signature gate.

### Existing patterns reused (do NOT reinvent — `feedback_research_design_principles`)

- `substrate.AppendEvent` (substrate spec §2.2) — Append delegates verbatim; identical idempotency (`substrate.ErrReplay` on UNIQUE collision).
- `substrate.Fold(runID, kind)` (substrate spec §2.3) — Replay folds journal-order; T1 does NOT introduce a new read primitive.
- `substrate.Verify` (substrate spec §5 + T-S1) — every folded event signature-verified before re-execution. T1 calls `Verify`; does NOT re-implement.
- `substrate.RegisterPayloadValidator` pattern (substrate T-S1) — T1 ships an analogous registry hook for T2's `RegisterReExecutor`; T2 owns the registry, T1 only calls the registered function.
- `idx_substrate_events_kind` (substrate spec §2.1) — Tail uses this index; no new index.
- W6 trace_id column (W6 §3.5) — T1's substrate impl reads `trace_id` off the journaled row; T4 sets it as `regatta.replay.original_trace_id` on the replay span.

### TDD test list (with failing-output capture step)

Per `feedback_tdd_discipline`: each test ships first; implementer runs `go test ./internal/history/... -run <name> -v`, captures failing output, pastes into PR body, then implements.

**B-tier (spec §6 T1 + spec §7 B-rubric):**

1. `TestHistory_AppendDelegatesToSubstrate` — Append wraps `substrate.AppendEvent`; same idempotency (`substrate.ErrReplay` on UNIQUE collision).
2. `TestHistory_TailStreamsNewEvents` — Tail emits events written after `since` cursor; closes channel on ctx cancel.
3. `TestHistory_ReplayFoldsAllKinds` — Replay streams one ReplayedEvent per folded event across all `IncludeKinds`.
4. `TestHistory_ReplayRejectsCrossTenant` — folded events with mismatched tenant_id ⇒ `ErrCrossTenant` (R7).
5. `TestHistory_ReplayRejectsReservedFields` — `opts.FromNodeID != ""` ⇒ `ErrUnsupported`; `opts.PinOverride != (PinSet{})` ⇒ `ErrUnsupported`.

**A-tier (spec §6 T1 + spec §7 A-rubric):**

6. `TestW9_ReplayDuringActiveRunReadsSnapshot` (R2) — start a Replay; concurrently AppendEvent; assert Replay only sees the pre-snapshot events.
7. `TestW9_ReplaySpanCarriesOriginalTraceID` (R6) — replay span has `regatta.replay.original_trace_id` equal to the substrate event's `trace_id`; replay's own trace_id is distinct. (Imports T4's `StartReplaySpan`.)
8. `TestW9_ReplayJobShutsDownCleanly` (R8) — `goleak.VerifyNone` after ctx cancel mid-stream; no leaked goroutines.

### PR body skeleton

```
## Summary

T1 ships `internal/history/` package with the `DurableHistory` Go
interface + the substrate-default implementation per
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md §3.1
§3.2 §5 §6 T1 §8 T1.

- internal/history/durable_history.go: DurableHistory interface +
  ReplayOpts + PinSet + ReplayedEvent + DiffResult + DiffVerdict.
- internal/history/substrate_impl.go: substrate-default impl —
  Append (thin wrap), Tail (idx_substrate_events_kind), Replay
  (substrate.Fold + reducer-aware merge + per-event re-execute via
  T2's registry hook).
- internal/history/errors.go: ErrCrossTenant + ErrUnsupported sentinels.
- internal/history/doc.go: one-line package godoc.

## Why

MVP-3 W9 v1 substrate-choice locked Option C hybrid (red-team spec).
T1 owns the seam (substrate spec §13 cross-task-seam pattern) so T2,
T3, T4 import a stable interface. Cross-tenant defence in depth at
the interface layer (R7) — W8 OPA middleware is the upstream gate;
T1 is the in-depth guard.

## Test plan

- [x] B-tier: TestHistory_AppendDelegatesToSubstrate,
       TestHistory_TailStreamsNewEvents,
       TestHistory_ReplayFoldsAllKinds,
       TestHistory_ReplayRejectsCrossTenant,
       TestHistory_ReplayRejectsReservedFields.
- [x] A-tier: TestW9_ReplayDuringActiveRunReadsSnapshot,
       TestW9_ReplaySpanCarriesOriginalTraceID,
       TestW9_ReplayJobShutsDownCleanly (goleak).
- [x] make pre-push-check clean.
- [x] doc-check diff vs origin/main: every new exported godoc ≤ 1 line
       (per feedback_comments_discipline).
- [x] Zero new SQL migrations (verify: `git diff --name-only
       origin/main...HEAD -- 'internal/orchestrator/state/migrations/*.sql'`
       empty).

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Deletion default

T1 is pure addition for the new internal/history/ package (~700 LoC
across 4 .go files + 2 _test.go files). What gets smaller:
- ZERO new SQL migrations (substrate spec §2.1 + W6 §3.5 already
  ship every column W9 needs).
- ZERO new storage primitive (substrate.Fold + AppendEvent are the
  only journal primitives; T1 wraps, does not re-implement).
- ZERO new sign/verify code (substrate.Verify owns the gate).
- PinSet declared but unimplemented saves ~80 LoC of v1 pin-loader
  scaffolding deferred to v2.

## A+ Rubric Scorecard

<paste verbatim — required per feedback_grade_rubric>

## Followups

- F1–F8 — pre-filed per §8 of this plan + spec §10. T1 PR body cites
  every issue number.

```release-notes
none
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w9-t1. Branch off main:
`git checkout -b feat/w9-t1-durable-history main`.

Dispatch in PARALLEL with T4 + T5 + T6 (file-disjoint per the plan's
§1 table). T2 dispatches AFTER your PR merges to main.

# Spec authority

Source-of-truth spec:
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md.
Read ALL of: §3.1 (DurableHistory interface signatures — verbatim;
v1 reserved-field rejection semantics), §3.2 (ReplayedEvent +
DiffResult shapes), §4 (reducer-aware fold contract), §5 R2 R6 R7 R8
(invariants you defend), §6 T1 (named tests), §8 T1 (file scope).
Substrate spec §2.1 §2.3 §5 §13 (Fold + AppendEvent + Verify +
RegisterPayloadValidator pattern).

Plan: docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md §2 +
the cross-task seam contracts section — your exports MUST match
those signatures byte-for-byte; T2 + T3 + T4 import by name.

Substrate-choice locked: Option C hybrid (DurableHistory Go
interface + substrate-default impl; Temporal impl is T6 markdown
stub only). DO NOT re-litigate this choice.

Per feedback_spec_pattern_authority: if you want to deviate from
any spec-mandated pattern — DurableHistory method set, ReplayOpts
field set, reserved-field rejection semantics, ErrCrossTenant sentinel
name, substrate.Fold-as-source-of-truth — STOP and re-spawn the
design subagent. Do NOT pick an alternative yourself.

# Scope (exclusive write paths — file-disjoint with T2/T3/T4/T5/T6)

EXACT output path slugs (per feedback_plan_subagent_dup_files):
- internal/history/durable_history.go (NEW)
- internal/history/substrate_impl.go (NEW)
- internal/history/errors.go (NEW)
- internal/history/doc.go (NEW; one-line package godoc per
  feedback_comments_discipline)
- internal/history/durable_history_test.go (NEW)
- internal/history/substrate_impl_test.go (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT create internal/history/replay.go, diff.go, reexecutor.go
  (T2's domain).
- Do NOT create internal/history/otel.go, nondeterministic.go (T4).
- Do NOT create internal/history/metrics.go (T5).
- Do NOT create internal/history/temporal/* (T6).
- Do NOT create internal/uiserver/replay* (T3).
- Do NOT add a goose migration. Substrate spec §2.1 + W6 §3.5 cover
  every column W9 needs (per plan migration-number-lock).
- Do NOT modify internal/orchestrator/substrate/ or internal/canon/.

# Patterns to reuse (feedback_research_design_principles)

- substrate.AppendEvent — Append wraps verbatim (idempotency,
  signature gate).
- substrate.Fold(runID, kind) — Replay folds journal-order; do NOT
  introduce a new read primitive.
- substrate.Verify — every folded event signature-verified before
  re-execute.
- idx_substrate_events_kind — Tail uses this index.
- W6 trace_id column — substrate event row exposes trace_id; T4
  reads it for the replay span attr.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test:
  1. Write test file first.
  2. Run `go test ./internal/history/... -run <name> -v`.
  3. CAPTURE failing output (paste into PR body's "Failing-test output
     (TDD capture)" section). "Tests would have failed" is NOT
     acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or logical group; squash later).

# Tests to land (8 named; spec §6 T1)

B-tier:
1. TestHistory_AppendDelegatesToSubstrate
2. TestHistory_TailStreamsNewEvents
3. TestHistory_ReplayFoldsAllKinds
4. TestHistory_ReplayRejectsCrossTenant
5. TestHistory_ReplayRejectsReservedFields (FromNodeID + PinOverride)

A-tier:
6. TestW9_ReplayDuringActiveRunReadsSnapshot (R2 — sqlite WAL
   snapshot isolation)
7. TestW9_ReplaySpanCarriesOriginalTraceID (R6 — imports T4's
   StartReplaySpan; if T4 not yet merged, gate with a build tag or
   stub the helper inline behind an interface)
8. TestW9_ReplayJobShutsDownCleanly (R8 — goleak.VerifyNone)

# Workflow after green

  1. Run `make pre-push-check` — confirm clean.
  2. Run `go test ./... -race` end-to-end. Any failure outside your
     scope means you broke a transitive caller — STOP and report.
  3. doc-check diff vs origin/main:
     `git diff origin/main -- 'internal/history/**/*.go' | grep -E
     '^\+(?!\+)' | grep '^// '` — every new godoc on exported funcs
     MUST be ≤ 1 line (per feedback_comments_discipline +
     feedback_pr_lint_gates). Trim multi-line godocs to one line
     BEFORE pushing.
  4. Banned-phrase pre-push grep — run `bash scripts/doc-check.sh`;
     gate on the banned-phrase list in
     ~/.claude/projects/-Users-treedesk-Desktop-Projects-regatta/memory/feedback_doc_check_banned_phrases.md.
     Do NOT inline literal token list in this prompt or in the PR body
     — the script + memory file are the canonical source (per
     feedback_doc_check_banned_phrases).
  5. NO AI signatures — do NOT add Co-Authored-By footers or
     "Generated with" trailers (per feedback_no_signatures).
  6. Verify release-notes fence in PR body BEFORE push:
     `grep -E '^\`\`\`release-notes' <body-file>` — must match
     EXACTLY ONCE (per feedback_pr_body_release_notes_fence). Common
     drift: release-note (singular), releasenotes, release_notes.
  7. Push branch.
  8. Open PR with:
     `gh pr create --base main --title "feat(w9-t1): internal/history
     DurableHistory interface + substrate-default impl" --body-file
     <path>`. USE --body-file (per feedback_pr_lint_gates), NEVER
     heredoc.
  9. Paste the A+ Rubric Scorecard VERBATIM in the PR body (per
     feedback_grade_rubric).
 10. After PR opens, spawn ONE adversarial reviewer subagent (per
     feedback_adversarial_review + feedback_agent_pr_review) with
     hunt list:
     - DurableHistory method signatures byte-equal to spec §3.1
       (paste both; assert zero drift).
     - ReplayOpts reserved-field rejection covers both FromNodeID
       (non-empty string) AND PinOverride (non-zero PinSet) via
       reflect.DeepEqual.
     - Cross-tenant rejection covers BOTH the opts.TenantID mismatch
       case AND the per-event TenantID drift case (defence in depth
       R7).
     - sqlite WAL snapshot isolation actually fires (R2) — test
       inspects sqlite_master.journal_mode or asserts the read tx
       isolation level explicitly.
     - goleak.VerifyNone catches the long-running Tail goroutine if
       it doesn't release on ctx cancel (R8).
     - ZERO new SQL migrations — `git diff origin/main
       internal/orchestrator/state/migrations/` returns empty.
     - Reviewer uses OK:/ISSUE: per item.
 11. Apply reviewer findings; re-run make pre-push-check; re-run
     doc-check; force-push.
 12. Enable automerge ONLY after reviewer cleared + every Risk-tier
     finding fixed inline OR filed as tracking issue cited in PR body
     (per feedback_review_before_automerge +
     feedback_unaddressed_load_bearing).

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 5 of the 8 tests.
- Adversarial reviewer verdict (APPROVE | findings list).
- One-line diff stat.
- A+ Rubric Scorecard verbatim.
- Confirmation: ZERO new SQL migrations.
```

---

## §3 Task T2 — Replay engine + diff harness + re-executor registry

### Scope (exclusive write paths)

- `internal/history/replay.go` (NEW) — `Replay` engine streaming `ReplayedEvent` via the substrate fold + per-event re-execute + Diff comparison. Memory bound: O(1) per replay job (R3 — events flow through one at a time).
- `internal/history/diff.go` (NEW) — `Diff(orig, replayed, reducer) DiffResult`. Canonical-JSON byte-equality via the existing `substrate.CanonicalJSON` helper. Reducer-aware: lww kinds compare head; append kinds compare per-pair in journal order.
- `internal/history/reexecutor.go` (NEW) — `RegisterReExecutor` registry + four deterministic re-executors:
  - `node_output` — payload re-derived from upstream supersedes + work_item_inputs fold.
  - `approval_event` — payload re-derived from approvals state machine fold (deterministic per inputs).
  - `gate_verdict` — payload re-derived from `CELDecider.Decide` over the journaled Snapshot (substrate spec T-S2).
  - `budget_reconciled` — payload re-derived from `token_spend` SUM over the journaled fold window.
  - The three non-deterministic kinds (`token_spend`, `fact`, `heartbeat`) go in an explicit `noReExecutorKinds` allow-list.
- `internal/history/replay_test.go` + `internal/history/diff_test.go` + `internal/history/reexecutor_test.go` (NEW).

### Prereqs (cite spec sections)

- Spec §3.1 — Replay method contract from T1's interface.
- Spec §3.3 — re-executor registry pattern (mirrors substrate `RegisterPayloadValidator`) + quarantine semantics.
- Spec §3.4 — Diff comparison contract (reducer-aware, canonical-JSON byte-equality).
- Spec §5 R1 R3 R4 R9 — quarantine, OOM-stable streaming, LLM-variance false-positive, registry parity test.
- Spec §6 T2 — B-tier + A-tier test names.
- Spec §8 T2 — file scope.
- Substrate spec §2.2 T-S2 — `CELDecider.Decide` shape for `gate_verdict` re-executor.
- Substrate spec T-S1 — `RegisterPayloadValidator` pattern that `RegisterReExecutor` mirrors.

### Existing patterns reused

- `substrate.CanonicalJSON` (contracts/schemas/sign.go) — Diff uses this; do NOT reimplement.
- `substrate.RegisterPayloadValidator` init() pattern — `RegisterReExecutor` mirrors structure verbatim.
- `CELDecider.Decide` (substrate T-S2) — `gate_verdict` re-executor calls this; same CEL program over the journaled Snapshot.
- T4's `nondeterministic.Mark` + `IsMarked` — Diff reads `IsMarked(ctx)` to downgrade events to `ReplaySkipped`.
- T4's `StartReExecuteSpan` — Replay engine opens a child span per event via T4's helper.

### TDD test list

**B-tier (spec §6 T2):**

1. `TestW9_DiffMatchOnIdenticalPayload` — same canonical-JSON ⇒ `Match`.
2. `TestW9_DiffDivergentNamesKeys` — payload diverges on `{a: 1}` vs `{a: 2}` ⇒ `Divergent` with `DivergentKeys=["a"]`.
3. `TestW9_DiffReducerAware` — append-reducer kinds compare per-pair; lww-reducer kinds compare only head.
4. `TestW9_QuarantineMarksNondeterministic` (R1) — fake re-executor calls `nondeterministic.Mark(ctx, "test")`; assert `DiffVerdict == ReplaySkipped` and `Reason == "test"`.
5. `TestW9_LLMNodeOutputReplaysAsMatch` (R4) — LLM-generated `node_output` re-executor reads from `supersedes` chain verbatim; no LLM client call (`httptest` server with `t.Fatal` on any request); reports `Match`.

**A-tier (spec §6 T2):**

6. `TestW9_DiffSummaryPerKind` — multi-kind run produces per-kind divergence summary.
7. `TestW9_ReExecutorRegistryCoversAllKinds` (R9) — every `substrate.EventKind` constant is registered OR in `noReExecutorKinds` allow-list; mirrors substrate T-S3's `TestSubstrate_EventKindEnumMatchesSQLCheck` pattern.

### PR body skeleton

```
## Summary

T2 ships the Replay engine + diff harness + re-executor registry per
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md §3.3
§3.4 §5 §6 T2 §8 T2.

- internal/history/replay.go: streaming Replay engine; O(1) memory
  per job (R3).
- internal/history/diff.go: reducer-aware canonical-JSON byte-equality
  comparator; quarantine downgrade via T4's IsMarked.
- internal/history/reexecutor.go: RegisterReExecutor registry +
  four v1 deterministic re-executors (node_output, approval_event,
  gate_verdict, budget_reconciled); explicit noReExecutorKinds
  allow-list for the three non-deterministic kinds.

## Why

MVP-3 W9 v1. Closes the replay → fix → re-run loop with reducer-
aware diff. Quarantine via T4's Mark API collapses R1 (clock/rand/
network) + R4 (LLM variance) into a single mechanism: deterministic
re-executors stay deterministic; non-determinism self-marks; LLM-
touching node_output reads verbatim from supersedes (no re-call).

## Test plan

- [x] B-tier: TestW9_DiffMatchOnIdenticalPayload,
       TestW9_DiffDivergentNamesKeys,
       TestW9_DiffReducerAware,
       TestW9_QuarantineMarksNondeterministic,
       TestW9_LLMNodeOutputReplaysAsMatch.
- [x] A-tier: TestW9_DiffSummaryPerKind,
       TestW9_ReExecutorRegistryCoversAllKinds.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Deletion default

T2 adds the Replay engine + diff harness + registry (~900 LoC). What
gets smaller:
- ZERO bespoke LLM-variance detector — quarantine via T4's Mark
  collapses R1 + R4 into one mechanism.
- ZERO new canonical-JSON helper — substrate.CanonicalJSON is the
  single comparator.
- ZERO new CEL evaluator — gate_verdict re-executor calls the same
  CELDecider.Decide from substrate T-S2.

## A+ Rubric Scorecard

<paste verbatim — required per feedback_grade_rubric>

```release-notes
none
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w9-t2. Branch off main AFTER T1 merges:
`git fetch origin && git checkout -b feat/w9-t2-replay-diff main`.

T2 is SEQUENTIAL after T1. DO NOT dispatch until T1 PR is merged to
main; T2 imports DurableHistory + ReplayedEvent + DiffResult from T1.

# Spec authority

Source-of-truth spec:
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md.
Read ALL of: §3.3 (re-executor registry + quarantine — verbatim;
nondeterministic.Mark(ctx, reason) API contract), §3.4 (Diff
contract; reducer-aware; canonical-JSON byte-equality), §5 R1 R3 R4
R9 (quarantine, OOM-stable streaming, LLM-variance, registry
parity), §6 T2 (named tests), §8 T2 (file scope). Substrate spec §4
(ReducerStrategy + defaultReducer), §2.2 T-S2 (CELDecider.Decide),
§13 T-S1 (RegisterPayloadValidator pattern).

Plan: docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md §3 +
cross-task seam contracts — your exports MUST match those signatures
byte-for-byte; T3 imports by name.

Substrate-choice locked: Option C hybrid. DO NOT re-litigate.

Per feedback_spec_pattern_authority: if you want to deviate (e.g.
ship a non-reducer-aware Diff, swap canonical-JSON for a structural
comparator, change the RegisterReExecutor signature, add an LLM
heuristic outside the quarantine mechanism), STOP and re-spawn the
design subagent.

# Scope (exclusive write paths — file-disjoint with T1/T3/T4/T5/T6)

EXACT output path slugs (per feedback_plan_subagent_dup_files):
- internal/history/replay.go (NEW)
- internal/history/diff.go (NEW)
- internal/history/reexecutor.go (NEW)
- internal/history/replay_test.go (NEW)
- internal/history/diff_test.go (NEW)
- internal/history/reexecutor_test.go (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT modify internal/history/durable_history.go, substrate_impl.go,
  errors.go, doc.go (T1's domain — already merged when you dispatch).
- Do NOT create internal/history/otel.go, nondeterministic.go (T4).
- Do NOT create internal/history/metrics.go (T5).
- Do NOT create internal/uiserver/replay* (T3).
- Do NOT add a goose migration.

# Patterns to reuse (feedback_research_design_principles)

- substrate.CanonicalJSON — Diff comparator; do NOT reimplement.
- substrate.RegisterPayloadValidator init() pattern —
  RegisterReExecutor mirrors verbatim.
- CELDecider.Decide (substrate T-S2) — gate_verdict re-executor calls.
- T4's nondeterministic.Mark + IsMarked — Diff reads IsMarked to
  downgrade to ReplaySkipped. (If T4 not yet merged when you start,
  vendor the API surface inline behind a build tag; remove the tag
  after T4 merges.)
- T4's StartReExecuteSpan — Replay opens child spans via this helper.

# Workflow steps (TDD discipline)

For each named test: write test → capture failing output → implement
→ commit. Paste failing output into PR body.

# Tests to land (7 named; spec §6 T2)

B-tier:
1. TestW9_DiffMatchOnIdenticalPayload
2. TestW9_DiffDivergentNamesKeys
3. TestW9_DiffReducerAware
4. TestW9_QuarantineMarksNondeterministic (R1)
5. TestW9_LLMNodeOutputReplaysAsMatch (R4 — httptest server with
   t.Fatal on any LLM endpoint hit)

A-tier:
6. TestW9_DiffSummaryPerKind
7. TestW9_ReExecutorRegistryCoversAllKinds (R9 — mirrors substrate
   T-S3 enum-parity pattern)

# Workflow after green

  1. make pre-push-check clean.
  2. go test ./... -race clean.
  3. doc-check diff (≤1-line godocs on exports).
  4. Banned-phrase pre-push grep: `bash scripts/doc-check.sh` (script
     + feedback_doc_check_banned_phrases memory file are the canonical
     source — do NOT inline literal token list).
  5. NO AI signatures (feedback_no_signatures).
  6. Release-notes fence grep BEFORE push: `grep -E
     '^\`\`\`release-notes' <body-file>` must match EXACTLY ONCE
     (feedback_pr_body_release_notes_fence).
  7. gh pr create --base main --title "feat(w9-t2): Replay engine +
     diff harness + reexecutor registry" --body-file <path>.
  8. A+ Scorecard verbatim in PR body.
  9. Spawn adversarial reviewer with hunt list:
     - RegisterReExecutor signature matches substrate
       RegisterPayloadValidator pattern (init()-time registration).
     - Diff reducer-aware: lww=head-only; append=per-pair journal order.
     - Quarantine downgrade fires for ANY nondeterministic.Mark call
       during re-execute, regardless of reason string.
     - LLM node_output re-executor demonstrably reads supersedes chain
       (test uses httptest server with t.Fatal on any request).
     - Registry parity test (R9) lists EVERY current
       substrate.EventKind in either registered or noReExecutorKinds.
     - Replay memory bound is O(1) per job (no slice accumulation).
     - Reviewer uses OK:/ISSUE: per item.
 10. Apply findings; force-push; automerge after reviewer cleared.

# Return format

PR URL, failing-test outputs (≥4 of 7), reviewer verdict, diff stat,
A+ Scorecard verbatim.
```

---

## §4 Task T3 — Operator UI replay button + background job

### Scope (exclusive write paths)

- `internal/uiserver/replay.go` (NEW) — `RegisterReplayRoutes(mux, deps)` mounts:
  - `POST /runs/{run_id}/replay` — W8 OPA gate at middleware (Principal.TenantID must match run.TenantID; defence in depth at T1's `opts.TenantID` check); spawns background replay job (goroutine, ctx tied to job_id); returns 303 to GET progress page.
  - `GET /runs/{run_id}/replay/{job_id}` — HTML progress page; W7 TailFacts `hx-trigger="every 5s"` polls substrate for `kind=fact AND key LIKE 'w9.replay.{job_id}.%'`; renders per-kind divergence summary table.
  - Background-job writer: drains `DurableHistory.Replay` channel; each ReplayedEvent writes a `substrate.AppendEvent(kind=fact, key="w9.replay.{job_id}.{event_id}", payload={verdict, reason, divergent_keys})`. Terminal `key="w9.replay.{job_id}.complete"` when channel drains. Defer block calls `span.End` + flushes diff-fact writer (R8).
- `internal/uiserver/templates/replay_progress.tmpl` (NEW) — HTML template; references W7 layout.tmpl partials; htmx `hx-get="/runs/{{.RunID}}/replay/{{.JobID}}" hx-trigger="every 5s"`.
- `internal/uiserver/replay_test.go` (NEW).

### Prereqs (cite spec sections)

- Spec §3.5 — operator UI seam; route table verbatim; background-job ctx derivation.
- Spec §3.6.4 W7 — Principal forward-compat parameter shape (W7 spec).
- Spec §5 R7 R8 — cross-tenant defence + shutdown cleanliness.
- Spec §6 T3 — named tests.
- Spec §8 T3 — file scope.
- W7 Wave 1 T4 (`internal/web/`) merged — Templates + CSP middleware reused.
- W7 Wave 1 T6 — approval handler patterns the W9 handler mirrors.
- W8 OPA Authorizer middleware merged — gates `POST /runs/{run_id}/replay`.

### Existing patterns reused

- `internal/uiserver/` scaffold (W7 Wave 7.0 T1, #263 merged) — T3 extends the existing mux.
- W7 Wave 1 T4's `Templates.RegisterFunc` (W7 plan §1 cross-task seam) — T3 registers any new template funcs via this hook; does NOT touch W7's template parser.
- W7 TailFacts `every 5s` hx-trigger (W7 spec §3.4) — progress page reuses the existing polling primitive.
- W8 Authorizer middleware — T3 wraps its handler chain via the existing W8 middleware composition.
- `substrate.AppendEvent` (substrate spec §2.2) — background job writes diff facts via this.

### TDD test list

**B-tier (spec §6 T3):**

1. `TestW9_POSTReplayReturns303` — handler spawns job; returns 303 to progress page URL.
2. `TestW9_POSTReplayRejectsCrossTenantPrincipal` — Principal.TenantID ≠ run.TenantID ⇒ 403 (W8 middleware gate fires).
3. `TestW9_ProgressPageRendersDiffFacts` — substrate has facts under `w9.replay.<job_id>.*`; progress page lists them.
4. `TestW9_ProgressPageRendersComplete` — terminal fact `w9.replay.<job_id>.complete` ⇒ "complete" badge.

**A-tier (spec §6 T3):**

5. `TestW9_BackgroundJobWritesFactsViaSubstrate` — replay job's diff facts persist as `kind=fact` events with the expected `key` shape.
6. `TestW9_ProgressPagePollingRespectsW7Cadence` — `hx-trigger="every 5s"` per W7 §3.4.

### PR body skeleton

```
## Summary

T3 ships the operator-UI replay trigger + background job + progress
page per docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md
§3.5 §6 T3 §8 T3.

- internal/uiserver/replay.go: POST /runs/{run_id}/replay (W8 OPA
  gate; spawns bg job; 303 redirect) + GET
  /runs/{run_id}/replay/{job_id} (W7 TailFacts 5s polling; renders
  per-kind divergence summary).
- internal/uiserver/templates/replay_progress.tmpl: htmx-driven
  progress page; references W7 layout.tmpl.

## Why

MVP-3 W9 v1 closes the operator-driven replay loop: click "replay"
in the UI → bg job streams diff facts → progress page polls substrate
at W7's existing 5s cadence. Defence in depth on cross-tenant (W8
middleware upstream; T1's opts.TenantID check downstream). R8
shutdown cleanliness verified via goleak.

## Test plan

- [x] B-tier: TestW9_POSTReplayReturns303,
       TestW9_POSTReplayRejectsCrossTenantPrincipal,
       TestW9_ProgressPageRendersDiffFacts,
       TestW9_ProgressPageRendersComplete.
- [x] A-tier: TestW9_BackgroundJobWritesFactsViaSubstrate,
       TestW9_ProgressPagePollingRespectsW7Cadence.

## Failing-test output (TDD capture)

<paste — required per feedback_tdd_discipline>

## Deletion default

T3 adds ~400 LoC across 2 .go + 1 .tmpl file. What gets smaller:
- ZERO new polling primitive (W7 TailFacts hx-trigger reused).
- ZERO new HTTP listener (W7 Wave 7.0 mux extended).
- ZERO new fact-storage shape (substrate.AppendEvent with
  kind=fact + key='w9.replay.<job_id>.<event_id>').

```release-notes
none
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w9-t3. Branch off main AFTER T1 + T2 + W7
Wave 1 T4 + W7 Wave 1 T6 + W8 Authorizer middleware all merge:
`git fetch origin && git checkout -b feat/w9-t3-operator-ui main`.

T3 is SEQUENTIAL — last to dispatch. Pre-dispatch verify on main:
- internal/history/durable_history.go exists (T1).
- internal/history/replay.go exists (T2).
- internal/web/server.go exists (W7 T4).
- internal/web/approval.go exists (W7 T6).
- internal/uiserver/ has W8 Authorizer middleware integration.

# Spec authority

Source-of-truth spec:
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md §3.5
§5 R7 R8 §6 T3 §8 T3. W7 spec §3.4 (TailFacts cadence) +
§3.6.4 (Principal forward-compat). W8 spec §3.6 (Authorizer
middleware shape).

Plan: docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md §4 +
cross-task seam contracts.

Substrate-choice locked: Option C hybrid. DO NOT re-litigate.

Per feedback_spec_pattern_authority: deviations from the route
table, ctx-derivation chain, or W7 TailFacts cadence require
re-spawning the design subagent.

# Scope (exclusive write paths — file-disjoint with T1/T2/T4/T5/T6)

EXACT output path slugs (per feedback_plan_subagent_dup_files):
- internal/uiserver/replay.go (NEW)
- internal/uiserver/templates/replay_progress.tmpl (NEW)
- internal/uiserver/replay_test.go (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT modify internal/history/* (T1/T2/T4/T5).
- Do NOT modify internal/web/* (W7 domain).
- Do NOT modify cmd/regatta/serve.go.
- Do NOT add a goose migration.

# Patterns to reuse (feedback_research_design_principles)

- internal/uiserver scaffold (W7 Wave 7.0 #263) — extend existing mux.
- W7 TailFacts hx-trigger="every 5s" (W7 §3.4) — progress page reuses.
- W8 Authorizer middleware — wrap T3 handler chain via existing
  composition; do NOT introduce a new auth seam.
- substrate.AppendEvent kind=fact — bg job writes diff facts via this.
- T2's history.Replay channel — drain in bg job; one diff fact per
  ReplayedEvent.

# Workflow steps (TDD discipline)

For each named test: write test → capture failing output → implement
→ commit. Paste failing output into PR body.

# Tests to land (6 named; spec §6 T3)

B-tier:
1. TestW9_POSTReplayReturns303
2. TestW9_POSTReplayRejectsCrossTenantPrincipal (W8 middleware fires)
3. TestW9_ProgressPageRendersDiffFacts
4. TestW9_ProgressPageRendersComplete

A-tier:
5. TestW9_BackgroundJobWritesFactsViaSubstrate
6. TestW9_ProgressPagePollingRespectsW7Cadence

# Workflow after green

  1. make pre-push-check clean.
  2. doc-check diff (≤1-line godocs).
  3. Banned-phrase pre-push: `bash scripts/doc-check.sh` (canonical
     source — do NOT inline literal token list per
     feedback_doc_check_banned_phrases).
  4. NO AI signatures.
  5. Release-notes fence grep: `grep -E '^\`\`\`release-notes'
     <body-file>` must match EXACTLY ONCE.
  6. gh pr create --base main --title "feat(w9-t3): operator UI
     replay button + background job" --body-file <path>.
  7. A+ Scorecard verbatim.
  8. Spawn adversarial reviewer with hunt list:
     - POST handler returns 303 (NOT 200 + JSON; UX gate per spec).
     - Cross-tenant 403 fires at W8 middleware layer AND T1's
       opts.TenantID check (defence in depth R7).
     - Background-job ctx derives from orchestrator root ctx (R8 —
       shutdown cancels in-flight; goleak clean).
     - Progress page hx-trigger is "every 5s" (W7 §3.4 byte-equal,
       not "5000ms" or "every 5 seconds").
     - Diff-fact key shape is "w9.replay.<job_id>.<event_id>" (no
       drift; downstream queries are key-prefix-bound).
     - Terminal fact key "w9.replay.<job_id>.complete" present.
     - Reviewer uses OK:/ISSUE: per item.
  9. Apply findings; force-push; automerge after reviewer cleared.

# Return format

PR URL, failing outputs, reviewer verdict, diff stat, A+ Scorecard.
```

---

## §5 Task T4 — OTel attrs + non-determinism quarantine

### Scope (exclusive write paths)

- `internal/history/otel.go` (NEW) — `StartReplaySpan` + `SetDivergenceCount` + `StartReExecuteSpan` helpers. Span attrs: `regatta.replay.run_id`, `regatta.replay.original_trace_id`, `regatta.replay.divergence_count` (root), `regatta.replay.event_kind`, `regatta.replay.event_id`, `regatta.replay.nondeterministic` (children).
- `internal/history/nondeterministic.go` (NEW) — `Mark(ctx, reason)` sets `regatta.replay.nondeterministic=true` + `regatta.replay.nondeterministic_reason=reason` on the active span; `IsMarked(ctx) (string, bool)` reads back.
- `internal/history/otel_test.go` + `internal/history/nondeterministic_test.go` (NEW).

### Prereqs (cite spec sections)

- Spec §3.3 — quarantine API contract (`nondeterministic.Mark(ctx, reason)`).
- Spec §3.7 — replay span tree shape + attr names verbatim.
- Spec §5 R6 — `regatta.replay.original_trace_id` from `substrate_events.trace_id` column (W6 §3.5).
- Spec §6 T4 — named tests.
- Spec §8 T4 — file scope.
- W6 §3.3 — `Config.Tracer trace.Tracer` injection seam (T4 does NOT introduce a new tracer construction site).

### Existing patterns reused

- W6 `Config.Tracer` injection pattern (W6 §3.3, T5) — T4 takes `trace.Tracer` as a parameter; never constructs one.
- `go.opentelemetry.io/otel/trace` stdlib — `trace.SpanFromContext(ctx)` is the active-span read primitive for `Mark`/`IsMarked`.
- W6 trace_id column (`substrate_events.trace_id`) — T4's `StartReplaySpan` reads the original trace_id off the parent caller's event and stamps it as `regatta.replay.original_trace_id`.

### TDD test list

**B-tier (spec §6 T4):**

1. `TestW9_ReplaySpanAttrs` — root replay span carries `regatta.replay.run_id`, `regatta.replay.original_trace_id`, `regatta.replay.divergence_count` (last set just before `span.End`).
2. `TestW9_NondeterministicSpanAttr` — `Mark(ctx, "reason")` sets `regatta.replay.nondeterministic=true` + `regatta.replay.nondeterministic_reason="reason"` on the active span; `IsMarked(ctx)` returns `("reason", true)`.

**A-tier (spec §6 T4):**

3. `TestW9_DivergenceCountAccumulates` — multi-event run with mixed verdicts; final `divergence_count` equals count of `Divergent` verdicts.

### PR body skeleton

```
## Summary

T4 ships OTel span helpers + the non-determinism quarantine API per
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md §3.3
§3.7 §6 T4 §8 T4.

- internal/history/otel.go: StartReplaySpan + SetDivergenceCount +
  StartReExecuteSpan helpers; attrs match spec §3.7 verbatim.
- internal/history/nondeterministic.go: Mark(ctx, reason) +
  IsMarked(ctx) — quarantine API; T2's Diff reads IsMarked to
  downgrade events to ReplaySkipped.

## Why

MVP-3 W9 v1. Closes R1 (clock/rand/network non-determinism) + R6
(W6 trace_id replay span correctness) via a single mechanism:
re-executors that touch non-deterministic sources self-Mark; diff
harness downgrades; replay span carries original trace_id for OTel
backend jump.

## Test plan

- [x] B-tier: TestW9_ReplaySpanAttrs, TestW9_NondeterministicSpanAttr.
- [x] A-tier: TestW9_DivergenceCountAccumulates.

## Failing-test output (TDD capture)

<paste — required per feedback_tdd_discipline>

## Deletion default

T4 adds ~150 LoC across 2 .go + 2 _test.go files. What gets smaller:
- ZERO new tracer construction (W6 Config.Tracer injection reused).
- ZERO new quarantine mechanism (single Mark/IsMarked API
  collapses R1 + R4 — LLM variance handled via T2's verbatim-read
  re-executor, no parallel detector).

```release-notes
none
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w9-t4. Branch off main (PARALLEL with T1 +
T5 + T6): `git checkout -b feat/w9-t4-otel-quarantine main`.

# Spec authority

Source-of-truth spec:
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md §3.3
§3.7 §5 R6 §6 T4 §8 T4. W6 §3.3 §3.5 (Config.Tracer + trace_id
column).

Plan: docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md §5 +
cross-task seam contracts.

Substrate-choice locked: Option C hybrid. DO NOT re-litigate.

Per feedback_spec_pattern_authority: deviations from the attr names
(spec §3.7) or the Mark/IsMarked signature require re-spawning the
design subagent.

# Scope (exclusive write paths)

- internal/history/otel.go (NEW)
- internal/history/nondeterministic.go (NEW)
- internal/history/otel_test.go (NEW)
- internal/history/nondeterministic_test.go (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT create internal/history/durable_history.go (T1).
- Do NOT create internal/history/replay.go, diff.go, reexecutor.go (T2).
- Do NOT create internal/history/metrics.go (T5).
- Do NOT add a goose migration.

# Patterns to reuse (feedback_research_design_principles)

- W6 Config.Tracer injection (W6 §3.3) — take trace.Tracer as a
  parameter; do NOT construct.
- trace.SpanFromContext(ctx) — active-span read for Mark/IsMarked.
- go.opentelemetry.io/otel/trace stdlib semconv for attr keys.

# Workflow steps (TDD discipline)

For each test: write → capture failing → implement → commit.

# Tests to land (3 named; spec §6 T4)

B-tier:
1. TestW9_ReplaySpanAttrs (verbatim §3.7 attr names + types)
2. TestW9_NondeterministicSpanAttr

A-tier:
3. TestW9_DivergenceCountAccumulates

# Workflow after green

  1. make pre-push-check clean.
  2. doc-check diff (≤1-line godocs).
  3. `bash scripts/doc-check.sh` banned-phrase pre-push (canonical
     source — do NOT inline literal token list per
     feedback_doc_check_banned_phrases).
  4. NO AI signatures (feedback_no_signatures).
  5. Release-notes fence grep: must match exactly once.
  6. gh pr create --base main --title "feat(w9-t4): OTel replay span
     attrs + nondeterministic quarantine API" --body-file <path>.
  7. A+ Scorecard verbatim.
  8. Spawn adversarial reviewer with hunt list:
     - Attr names byte-equal to spec §3.7 (paste both lists).
     - Mark/IsMarked is goroutine-safe (multiple re-executes in
       parallel under one replay).
     - regatta.replay.nondeterministic_reason captures the FIRST
       Mark call only (subsequent calls are idempotent — verify).
     - Reviewer uses OK:/ISSUE: per item.
  9. Apply findings; automerge after reviewer cleared.

# Return format

PR URL, failing outputs, reviewer verdict, diff stat, A+ Scorecard.
```

---

## §6 Task T5 — P2.5 trigger metrics + alert

### Scope (exclusive write paths)

- `internal/history/metrics.go` (NEW) — `MetricsRecorder` with three OTel meter instruments:
  - `regatta.history.sqlite_contention_pct` — gauge; % of scheduler ticks blocked on `database is locked` over a 24-h window; observation site is T1's substrate impl write-path (T1's `Append` retry loop calls `ObserveContention(ctx, blocked)`).
  - `regatta.history.concurrent_programs` — gauge; `work_items WHERE state='running' AND kind='program'` row count; observation registered at orchestrator poll loop (out of W9 code scope; [w9-followup] F6 doc PR cites the observation site).
  - `regatta.history.replay_recovery_seconds` — histogram; end-to-end Replay duration for a single `run_id`; observation site is T2's `Replay` engine on channel drain.
- `internal/history/metrics_test.go` (NEW).

### Prereqs (cite spec sections)

- Spec §3.6 — P2.5 trigger metric table verbatim (names, aggregation, thresholds).
- Spec §5 R10 — contention metric must measure writer-side only (not reader); replay is read-only so cannot inflate.
- Spec §6 T5 — named tests.
- Spec §8 T5 — file scope; alert-rule doc append is [w9-followup] F6 (out of code PR scope).

### Existing patterns reused

- W6 `metric.Meter` injection seam (W6 §3.3) — `NewMetricsRecorder(meter metric.Meter)`; do NOT construct meter.
- W6 prior metric instruments (substrate spec §10 #5 `regatta.substrate.append_total`) — naming convention `regatta.<package>.<metric>` followed.

### TDD test list

**B-tier (spec §6 T5):**

1. `TestW9_SqliteContentionMetricEmits` — induce `database is locked` via concurrent writers; metric counter increments.
2. `TestW9_ConcurrentProgramsMetricEmits` — 30 running program rows; gauge reads 30 when the orchestrator observation hook fires (test stubs the observation site).
3. `TestW9_ReplayRecoveryHistogramEmits` — Replay finishes; histogram records the duration.
4. `TestW9_ReplayDoesNotInflateContentionMetric` (R10) — start 10 concurrent Replays; assert `regatta.history.sqlite_contention_pct` stays at 0 (no writes blocked).

**A-tier (spec §6 T5):**

5. `TestW9_AlertFiresAfterTwoWindows` — synthetic two-window threshold breach ⇒ alert handler invoked (alert handler is a test double; production handler is operator-config per [w9-followup] F6).

### PR body skeleton

```
## Summary

T5 ships the three P2.5 trigger metric instruments per
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md §3.6
§5 R10 §6 T5 §8 T5.

- internal/history/metrics.go: MetricsRecorder with
  sqlite_contention_pct (gauge, writer-side only), concurrent_programs
  (gauge, orchestrator-observed), replay_recovery_seconds
  (histogram).
- Observation sites: T1 substrate impl Append-retry loop for
  contention; T2 Replay engine on channel drain for recovery;
  orchestrator poll loop for concurrent_programs ([w9-followup]
  F6 doc PR cites this site).

## Why

MVP-3 W9 v1 ships the metrics infrastructure that powers the P2.5
trigger. v1 ships substrate-default impl only; the trigger flip to
Temporal is design-only in T6.

## Test plan

- [x] B-tier: TestW9_SqliteContentionMetricEmits,
       TestW9_ConcurrentProgramsMetricEmits,
       TestW9_ReplayRecoveryHistogramEmits,
       TestW9_ReplayDoesNotInflateContentionMetric (R10).
- [x] A-tier: TestW9_AlertFiresAfterTwoWindows.

## Failing-test output (TDD capture)

<paste — required per feedback_tdd_discipline>

## Deletion default

T5 adds ~200 LoC. What gets smaller:
- ZERO new meter construction (W6 metric.Meter injection reused).
- Alert thresholds are operator-config (not code-config), so a future
  threshold tweak is a doc-only PR.

```release-notes
none
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w9-t5. Branch off main (PARALLEL with T1 +
T4 + T6): `git checkout -b feat/w9-t5-metrics main`.

# Spec authority

Source-of-truth spec:
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md §3.6
§5 R10 §6 T5 §8 T5.

Plan: docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md §6 +
cross-task seam contracts.

Substrate-choice locked: Option C hybrid. DO NOT re-litigate.

Per feedback_spec_pattern_authority: metric names + units + aggregation
type are spec-locked; deviations require re-spawning the design
subagent.

# Scope (exclusive write paths)

- internal/history/metrics.go (NEW)
- internal/history/metrics_test.go (NEW)

You MUST NOT touch any other file. Do NOT create the other
internal/history/ files or any orchestrator hook; the orchestrator
observation site is [w9-followup] F6's doc PR scope.

# Patterns to reuse (feedback_research_design_principles)

- W6 metric.Meter injection (W6 §3.3) — take meter as parameter.
- substrate spec naming convention regatta.<package>.<metric>.

# Workflow steps (TDD discipline)

For each test: write → capture failing → implement → commit.

# Tests to land (5 named; spec §6 T5)

B-tier:
1. TestW9_SqliteContentionMetricEmits
2. TestW9_ConcurrentProgramsMetricEmits (orchestrator observation site
   stubbed)
3. TestW9_ReplayRecoveryHistogramEmits
4. TestW9_ReplayDoesNotInflateContentionMetric (R10 — assert
   contention metric is writer-side only)

A-tier:
5. TestW9_AlertFiresAfterTwoWindows (synthetic threshold breach;
   alert handler is a test double)

# Workflow after green

  1. make pre-push-check clean.
  2. doc-check diff (≤1-line godocs).
  3. `bash scripts/doc-check.sh` banned-phrase pre-push (canonical
     source per feedback_doc_check_banned_phrases — do NOT inline
     literal token list).
  4. NO AI signatures.
  5. Release-notes fence grep: must match exactly once.
  6. gh pr create --base main --title "feat(w9-t5): history.MetricsRecorder
     P2.5 trigger instruments" --body-file <path>.
  7. A+ Scorecard verbatim.
  8. Spawn adversarial reviewer with hunt list:
     - Metric names byte-equal to spec §3.6 table.
     - Units match (gauge percent; gauge count; histogram seconds).
     - Contention metric is writer-side only (R10).
     - Threshold values are NOT hardcoded — they are operator-config
       (alert handler is a test double in v1).
     - Reviewer uses OK:/ISSUE: per item.
  9. Apply findings; automerge after reviewer cleared.

# Return format

PR URL, failing outputs, reviewer verdict, diff stat, A+ Scorecard.
```

---

## §7 Task T6 — Temporal-backed impl design doc (markdown stub only)

### Scope (exclusive write paths)

- `internal/history/temporal/README.md` (NEW) — design-only stub. **No Go code.**

Content (mandatory sections):
- **Trigger thresholds** — cite spec §3.6 table verbatim; reference T5's three metric instruments.
- **Swap procedure** — how an operator flips `history.backend: temporal` in `regatta.yaml` once the trigger fires; what changes in `Append` / `Tail` / `Replay` write paths.
- **Dual-write window** — substrate writer continues in parallel during the Temporal-flip window (locked red-team §7 + spec §5 R5); N-day cooldown after which substrate writer stops; flip-back is zero-loss within the window.
- **v2 impl PR contract** — exact set of files the impl PR is expected to add (`temporal/history.go`, `temporal/worker.go`, etc.); explicit "Temporal SDK dependency adds ~12 MB to go.mod" note; mandatory cleanup if the trigger de-fires (delete the impl + back to substrate-only).
- **Acceptance criteria for unblocking the impl PR** — the §3.6 trigger condition that fired + a 24-h-window confirmation post + sign-off from the operator on-call.

### Prereqs (cite spec sections)

- Spec §3.6 — trigger thresholds.
- Spec §5 R5 — dual-write window contract.
- Spec §8 T6 — file scope (markdown-only).
- Locked red-team spec §4 + §7 — Temporal swap procedure design.

### Existing patterns reused

- `internal/orchestrator/temporal/README.md` shape (if it exists in the locked red-team spec; otherwise mirror substrate spec's design-stub style).
- N/A for code patterns — T6 ships zero Go code.

### TDD test list

T6 ships zero code. No tests. **Markdown gates only:**

- `bash scripts/doc-check.sh` exits 0 (link integrity + banned-phrase lint per `feedback_doc_check_banned_phrases`).
- `bash scripts/stale-todo.sh` exits 0 (no stale TODO without owner).

### PR body skeleton

```
## Summary

T6 ships the Temporal-backed DurableHistory impl design-only stub per
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md §3.6
§5 R5 §8 T6.

- internal/history/temporal/README.md: design stub — trigger
  thresholds (cites T5 metrics), swap procedure, dual-write window
  (R5), v2 impl PR contract, acceptance criteria.

## Why

MVP-3 W9 v1 keeps Temporal SDK out of go.mod. Build weight: zero.
Concrete impl PR lands ONLY when the §3.6 trigger fires (any of three
conditions for two consecutive 24-h windows). The stub pre-stages the
swap so a future operator-driven flip is a low-surprise PR.

## Test plan

- [x] bash scripts/doc-check.sh exits 0.
- [x] bash scripts/stale-todo.sh exits 0.
- [x] No Go file added; v1 go.mod unchanged.

## Deletion default

T6 adds one markdown file (~200 LoC). What gets smaller:
- ZERO Go code; ZERO Temporal SDK dep in go.mod; ZERO build-weight
  increase. Concrete impl deferred until trigger fires.

```release-notes
none
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w9-t6. Branch off main (PARALLEL with T1 +
T4 + T5): `git checkout -b docs/w9-t6-temporal-stub main`.

# Spec authority

Source-of-truth spec:
docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md §3.6
§5 R5 §8 T6. Locked red-team spec
docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md §4
+ §7 (Option C lock-in budget; dual-write window).

Plan: docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md §7.

Substrate-choice locked: Option C hybrid. DO NOT re-litigate. DO NOT
ship Go code; v1 keeps Temporal SDK out of go.mod (deletion-default
gate).

# Scope (exclusive write paths)

- internal/history/temporal/README.md (NEW; markdown-only)

You MUST NOT touch any other file. Specifically:
- Do NOT add internal/history/temporal/*.go.
- Do NOT modify go.mod or go.sum.
- Do NOT add a goose migration.

# Mandatory sections in the README

1. Trigger thresholds — cite spec §3.6 table verbatim.
2. Swap procedure — operator flips history.backend in regatta.yaml.
3. Dual-write window — spec §5 R5 contract (N-day cooldown,
   zero-loss flip-back).
4. v2 impl PR contract — exact file set + Temporal SDK weight note.
5. Acceptance criteria — trigger fired + 24-h-window confirmation +
   operator sign-off.

# Workflow

  1. Write the README.
  2. Run `bash scripts/doc-check.sh` — exit 0 (link integrity +
     banned-phrase lint; canonical source per
     feedback_doc_check_banned_phrases).
  3. Run `bash scripts/stale-todo.sh` — exit 0.
  4. NO AI signatures.
  5. Release-notes fence grep: `grep -E '^\`\`\`release-notes'
     <body-file>` matches exactly once (per
     feedback_pr_body_release_notes_fence).
  6. gh pr create --base main --title "docs(w9-t6): Temporal-backed
     DurableHistory impl design stub" --body-file <body>.
  7. Doc-only PR — no reviewer subagent, no A+ scorecard required
     per feedback_review_proportional (trivial doc strip).
  8. Enable automerge: gh pr merge <PR-NUM> --auto --squash.

# Return format

PR URL, doc-check exit code, stale-todo exit code, automerge enabled.
```

---

## §8 Followup issue templates (pre-enumerated)

Per `feedback_unaddressed_load_bearing` + `feedback_parallel_dup_followups`: all eight follow-ups from spec §10 are pre-enumerated here so each implementer cites the same issue numbers in their PR body. **File these issues BEFORE T1 dispatches.**

| # | Title | Why deferred | Re-enable when |
|---|---|---|---|
| F1 | `[w9-followup]` `regatta replay <run_id>` CLI shim | v1 is UI-triggered only; CLI is a thin wrapper that the v2 partial-replay knob requires anyway | v2 partial-replay PR |
| F2 | `[w9-followup]` Partial replay `--from=<node>` semantics + impl | `ReplayOpts.FromNodeID` reserved field; v1 rejects non-empty with `ErrUnsupported` | Operator request signal OR v2 wave begins |
| F3 | `[w9-followup]` Edit-and-replay `--pin-model=<model>` (PinSet impl) | `ReplayOpts.PinOverride` reserved field; v1 rejects non-zero with `ErrUnsupported` | v2 wave (after F2) |
| F4 | `[w9-followup]` Cross-tenant replay UX (admin-scope Principal) | v1 rejects cross-tenant in both layers (R7); admin-scope replay needs a W8 policy bundle extension | W8 multi-tenant cutover |
| F5 | `[w9-followup]` Temporal-backed `DurableHistory` impl + dual-write window | §3.6 P2.5 trigger; §8 T6 design-only stub in v1 | Any §3.6 condition holds two consecutive 24-h windows |
| F6 | `[w9-followup]` P2.5 trigger operator runbook + orchestrator observation hook | Doc append to `docs/operator/observability.md` + orchestrator `concurrent_programs` observation site wiring | Alongside T5 merge in v1 (doc PR) |
| F7 | `[w9-followup]` 1M-event load test in CI (A+3 perf budget) | Build-tag `load`; not pre-merge | A+ tier polish wave |
| F8 | `[w9-followup]` Replay-result attestation in W10 chain | W10 supply-chain wedge consumes `DiffResult` to sign "replay verified" attestations | MVP-4 W10 |

**Filing command** (run before T1 dispatches):

```bash
for issue in F1 F2 F3 F4 F5 F6 F7 F8; do
  gh issue create --label w9-followup --title "..." --body "..."
done
```

Implementer PR bodies cite the resulting issue numbers in their "Followups" section per `feedback_unaddressed_load_bearing`.

---

_Plan authority: `feedback_spec_pattern_authority` — implementer subagent deviation from the spec or this plan requires re-spawning the design subagent. The 8 follow-up issues in §8 MUST be filed and cited in each implementer's PR body per `feedback_unaddressed_load_bearing` + `feedback_review_before_automerge`._
