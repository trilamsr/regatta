# MVP-3 W9 — Replay + Diff Harness Design Spec

Status: ready for review
Date: 2026-06-01
Author: design subagent <tri@maydow.com>
Binding brief: `docs/engineer/briefs/2026-05-31-mvp-3-next-level.md` §4 W9
Locked predecessor: `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md` — Option C (hybrid, substrate-default `DurableHistory` interface, Temporal-backed impl gated behind refined P2.5 trigger) is PICKED. This spec does NOT re-litigate Temporal-vs-bespoke; it operationalises Option C.
Depends on (must be merged to main before W9 dispatches):
  - W6 OTel T1–T5 — shipped (substrate inherits trace_id / span_id seam from PR #209)
  - W7 operator web UI Wave 7.0 — #268 plan pending; T1 HTTP listener shipped (#263)
  - W8 OPA RBAC — spec shipped (#266); impl pending
  - Substrate Wave 1 (`docs/engineer/plans/2026-06-01-substrate-w1-tasks.md`) — T-S1 + T-S2 + T-S3 merged

Memory rules in force: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_deletion_default`, `feedback_migration_number_lock`, `feedback_spec_pattern_authority`, `feedback_doc_check_banned_phrases`, `feedback_pr_body_release_notes_fence`, `feedback_unaddressed_load_bearing`.

---

## §1 Goal + non-goal

### 1.1 Goal

Operators can deterministically replay a DAG run from the substrate `events` log, diff the replayed payload against the original payload per event, and trigger replay from the operator web UI. The replay → fix → re-run loop is closed in MVP-3 without parallel history infrastructure: substrate events are the only source of truth.

### 1.2 Non-goal

- **Cross-tenant replay.** The reviewer who triggers replay sees only events whose `tenant_id` matches their Principal (W8 OPA gate); cross-tenant replay UX is deferred. Single-tenant deployments (W8 default `tenant_id='default'`) are unaffected.
- **Partial replay.** v1 ships full-run replay only. `regatta replay <run_id> --from=<node>` (named in the brief) is deferred to a v2 follow-up; the `DurableHistory.Replay` iterator surface admits it later without API break.
- **Edit-and-replay.** v1 ships read-only replay (re-execute under the journaled inputs). The brief's `--pin-model claude-4.7` knob is a v2 follow-up — Fork() is named in the interface (§3.1) but its concrete write path is deferred to the v2 wave.
- **Temporal-backed impl.** v1 ships the `DurableHistory` interface + substrate-default impl only. Temporal impl is a design-only §8 T6 stub; concrete code lands ONLY when the P2.5 trigger (§3.6) fires.

---

## §2 In / Out

### IN

1. New package `internal/history/` exporting the `DurableHistory` Go interface (§3.1) + the substrate-default implementation (§3.1) + `ReplayedEvent` struct (§3.2) + `ReplayOpts` struct (§3.1).
2. Replay engine (`internal/history/replay.go`) that streams events from `DurableHistory.Replay(...)`, re-derives each event's expected payload via a per-`EventKind` re-executor registry, and emits a `ReplayedEvent` per input event.
3. Diff harness (`internal/history/diff.go`) that compares replayed payload against original payload per event, reducer-aware (LWW vs append per substrate spec §4), emitting one of `{match, divergent, replay_skipped}` per event.
4. Operator UI seam: `POST /runs/{run_id}/replay` route handler (`internal/uiserver/replay.go`) that spawns a background replay job + a `GET /runs/{run_id}/replay/<job_id>` progress page that polls `TailFacts` (W7 streaming primitive — currently `every 5s` `hx-trigger` per W7 §3.4) for replay progress facts.
5. P2.5 trigger metrics — `regatta.history.sqlite_contention_pct`, `regatta.history.concurrent_programs`, `regatta.history.replay_recovery_seconds` exported via OTel meter (defined in §3.6); alert fires when any condition holds for two consecutive 24-h windows.
6. OTel attrs on replay spans: `regatta.replay.run_id`, `regatta.replay.original_trace_id`, `regatta.replay.divergence_count` (§3.7).
7. Non-determinism quarantine: re-executors that observe caller-injected randomness (clock, rand, network) set `regatta.replay.nondeterministic=true` on the active span; diff harness downgrades those events to `replay_skipped` (§3.3).

### OUT

- Cross-tenant replay (W8 OPA gate filters out events where Principal.TenantID ≠ event.TenantID; cross-tenant trigger surface deferred per §1.2).
- Partial replay (`--from=<node>`), edit-and-replay (`--pin-model=<model>`), and the `regatta replay` CLI subcommand. v1 is operator-UI-triggered only; the CLI lands as a thin shim in a v2 follow-up.
- Temporal-backed `DurableHistory` impl (design-only stub in §8 T6; gated behind §3.6 trigger).
- Parallel `history` table — substrate `events` is the only source of truth; substrate spec §2.1 + §4 already define the storage shape and reducer semantics. **Zero new SQL migrations** in this spec.
- Cost-budget for replay re-execution — `VerifyReplay` cost ceiling is a `[w9-followup]` issue (red-team open question 5); v1 replay is read-only (re-derives payloads in-process, no LLM calls), so cost is bounded by the substrate event count.

---

## §3 Architecture

### 3.1 `DurableHistory` Go interface — substrate-default implementation

Package `internal/history/` ships ONE interface and ONE concrete impl in v1. The interface admits a Temporal-backed impl later (§3.6) — that impl is design-only in §8 T6.

```go
// Package history abstracts the durable-execution backend for W9
// replay + diff. v1 default impl reads from the substrate events
// table; Temporal-backed impl lands ONLY when the P2.5 trigger
// fires (§3.6). The seam is one file (durable_history.go) so a
// future Temporal impl can be deleted without touching callers
// (lock-in budget = 1 file, per the locked red-team §4 sketch).
package history

import (
    "context"
    "io"
)

// ReplayOpts is the read knob set. v1 supports only TenantID
// (mandatory for W8 OPA gating) and IncludeKinds (defaults to all
// non-heartbeat kinds — heartbeats are liveness-only and contribute
// no replay signal). Partial-replay knobs (FromNodeID, PinOverride)
// are reserved fields; v1 implementations reject non-empty values
// with ErrUnsupported.
type ReplayOpts struct {
    TenantID      string         // required; substrate.DefaultTenantID for single-tenant
    IncludeKinds  []substrate.EventKind  // empty = all except KindHeartbeat
    FromNodeID    string         // reserved; v1 rejects non-empty (v2 partial-replay)
    PinOverride   PinSet         // reserved; v1 rejects non-empty (v2 edit-and-replay)
}

// PinSet is the v2 edit-and-replay override shape. Declared in
// v1 so the interface signature does not break when v2 lands.
// v1 ReplayOpts rejects non-zero PinSet with ErrUnsupported.
type PinSet struct {
    ModelID   string
    Seed      int64
    PromptSHA string
}

// DurableHistory is the v1 read+replay surface. Append is the
// existing substrate seam (substrate.AppendEvent — DurableHistory
// wraps it for symmetry with the future Temporal impl, which
// will route Append through worker.RegisterWorkflow). Tail is
// the streaming-read primitive the operator UI polls. Replay is
// the diff-driver: it streams ReplayedEvent values pairing the
// original payload with the re-derived payload per event.
type DurableHistory interface {
    // Append records one event in the durable backend. v1 impl
    // delegates to substrate.AppendEvent; identical idempotency
    // semantics (UNIQUE(run_id, written_by, nonce) collision ⇒
    // substrate.ErrReplay). Provided here so a future Temporal
    // impl owns the same write surface.
    Append(ctx context.Context, runID string, ev substrate.Event) error

    // Tail streams new events for runID written since `since`
    // (ULID cursor). Channel closes when ctx cancels. v1 impl
    // polls substrate at the read cadence the caller configures
    // via opts; the operator UI passes TailFacts' 5s cadence per
    // W7 §3.4. The returned io.Closer releases the backend
    // iterator (substrate prepared-statement handle for v1).
    Tail(ctx context.Context, runID string, since string) (<-chan substrate.Event, io.Closer, error)

    // Replay streams ReplayedEvent values for runID — one per
    // event in the substrate fold, in journal order. Each emitted
    // value pairs (original event payload) with (re-derived
    // payload via the per-kind re-executor registry — §3.3).
    // Diff harness consumes the stream and emits {match,
    // divergent, replay_skipped} per event (§3.4).
    //
    // Reducer-aware: events whose substrate.defaultReducer is
    // `append` (token_spend, approval_event, gate_verdict) are
    // replayed in journal order; events whose reducer is `lww`
    // (node_output, fact, budget_reconciled, heartbeat) are
    // replayed at their journaled head (the substrate.Fold
    // tiebreaker (written_at DESC, id DESC) is preserved).
    //
    // ctx cancellation closes the channel; the io.Closer
    // releases backend resources.
    Replay(ctx context.Context, runID string, opts ReplayOpts) (<-chan ReplayedEvent, io.Closer, error)
}
```

**v1 substrate-default impl** (`internal/history/substrate_impl.go`):

- `Append` thin-wraps `substrate.AppendEvent` (substrate package owns sign + cycle-check + monotonicity invariants; history layer adds no new write-side semantics).
- `Tail` SELECTs from `substrate_events WHERE run_id=? AND id > ? ORDER BY written_at, id` at the caller's polling cadence. Uses `idx_substrate_events_kind` (substrate spec §2.1).
- `Replay` opens a substrate read tx, folds `substrate.Fold(runID, kind)` for every kind in `opts.IncludeKinds`, merges into a single journal-ordered stream, and pipes each event through `runReExecutor(event)` to produce the `ReplayedEvent`. Reducer-aware merge: kind-by-kind ordering preserves the substrate spec §4 contract.
- Cross-tenant safety: `Replay` rejects with `ErrCrossTenant` if any folded event's `TenantID != opts.TenantID`. W8 OPA gate enforces this upstream at the HTTP handler (§3.5), but the interface defends in depth.

**No new SQL migration.** This package adds zero migrations — substrate spec §2.1 already ships `substrate_events` with `tenant_id`, `trace_id`, `span_id`, `payload_json`, `supersedes`, signing columns, and the `idx_substrate_events_kind` index Replay folds against.

### 3.2 `ReplayedEvent` shape

```go
// ReplayedEvent pairs one substrate event with its replay result.
// Original is the journaled row verbatim; Replayed is the payload
// the re-executor re-derived under journaled inputs. Diff is the
// per-kind comparison result (§3.4).
//
// When the re-executor cannot replay an event deterministically
// (clock, rand, network — §3.3), Replayed.PayloadJSON is empty
// and Diff.Verdict == ReplaySkipped with Diff.Reason naming the
// non-determinism source.
type ReplayedEvent struct {
    Original  substrate.Event       // journaled row, signature verified
    Replayed  substrate.Event       // re-derived; same id, recomputed payload_json
    Diff      DiffResult            // §3.4
}

type DiffResult struct {
    Verdict       DiffVerdict       // match | divergent | replay_skipped
    Reason        string            // empty for match; named cause for divergent/skipped
    DivergentKeys []string          // JSON-path keys that disagreed; empty for match/skipped
}

type DiffVerdict string

const (
    Match          DiffVerdict = "match"
    Divergent      DiffVerdict = "divergent"
    ReplaySkipped  DiffVerdict = "replay_skipped"
)
```

### 3.3 Replay invariants + non-determinism quarantine

**Invariant:** same input (substrate events for a `run_id`) → same output, **assuming the per-kind re-executor is deterministic**. The substrate spec already mandates content-addressed signatures (§5 nonce-in-signature; cycle-check on supersedes; clock-regression rejection); replay extends the contract: given identical inputs, the re-executor produces identical outputs.

**Re-executor registry** (`internal/history/reexecutor.go`):

```go
// RegisterReExecutor binds an EventKind to a function that re-derives
// the event payload given the journaled inputs (the prior fold state
// + the event's supersedes chain). v1 ships re-executors for the four
// deterministic kinds:
//   - node_output       : payload re-derived from upstream supersedes
//                          + work_item_inputs fold
//   - approval_event    : payload re-derived from approvals state
//                          machine fold (pending → approved | rejected
//                          | timed_out, deterministic per inputs)
//   - gate_verdict      : payload re-derived from CELDecider.Decide
//                          over the journaled Snapshot (substrate
//                          spec §10 #17 — one tx for fold + eval +
//                          emit; replay rebuilds the Snapshot from
//                          fold and re-evaluates the same CEL program)
//   - budget_reconciled : payload re-derived from token_spend SUM
//                          over the journaled fold window
//
// The three non-deterministic kinds (token_spend, fact, heartbeat)
// are reducer='append' or 'lww' with caller-injected randomness
// (token_spend rides actual provider response; fact rides reducer
// timing; heartbeat rides wall clock). They are marked
// regatta.replay.nondeterministic=true and emitted as ReplaySkipped.
//
// Mirrors substrate.RegisterPayloadValidator init() pattern (T-S1
// substrate spec §13 cross-task seam): re-executors register from
// their own files. Open-extensible without touching this file.
func RegisterReExecutor(kind substrate.EventKind, fn ReExecutor)

type ReExecutor func(ctx context.Context, ev substrate.Event, fold []substrate.Event) (json.RawMessage, error)
```

**Quarantine span attr** (§3.7): re-executors that detect caller-injected randomness — concretely, any call into `time.Now()`, `crypto/rand`, or a network operation during re-derivation — call `nondeterministic.Mark(ctx, "<reason>")`. The diff harness reads the attr from the span and downgrades the event's `DiffVerdict` to `ReplaySkipped` with `Reason` set to the marked cause. v1's re-executors NEVER use these sources (re-derivation reads only the substrate fold), so a quarantine fire indicates a re-executor bug → tracking issue auto-filed.

### 3.4 Diff harness — reducer-aware comparison

```go
// Diff compares replayed payload vs original payload for one event.
// Reducer-aware: LWW kinds (substrate spec §4) compare only the
// current head; append kinds (token_spend, approval_event,
// gate_verdict) compare in journal-order pairs.
//
// Comparison uses canonical-JSON byte-equality (substrate's existing
// CanonicalJSON from contracts/schemas/sign.go) — identical bytes ⇒
// match; otherwise structural diff names the divergent JSON-path
// keys. LLM-output-variance false-positives (R4) are bounded by
// quarantine (§3.3): events whose payload includes provider-side
// randomness are ReplaySkipped before reaching Diff.
func Diff(orig, replayed substrate.Event, reducer substrate.ReducerStrategy) DiffResult
```

**Reducer awareness:**
- `lww` (node_output, fact, budget_reconciled, heartbeat) — Diff compares the head only (substrate.Fold ordering tiebreaker (written_at DESC, id DESC)). Same head ⇒ match.
- `append` (approval_event, token_spend, gate_verdict) — Diff is per-pair in journal order. The pair-wise verdict aggregates: divergent if ANY pair diverges; match if ALL pairs match. Skipped if any non-skip pair Diverges but the run also has any skipped pair (skipped wins for forensic clarity).

**Output**: `{match, divergent, replay_skipped}` per event, aggregated per kind, summarised per run.

### 3.5 Operator UI seam — `/runs/{run_id}/replay`

The operator web UI (W7) is the v1 trigger surface. W7 Wave 7.0 owns the HTTP listener primitive (PR #263, shipped); W9 adds two routes:

```
POST /runs/{run_id}/replay
  - W8 OPA gate: Principal.TenantID must match run.TenantID
  - Spawns a background replay job (Goroutine, ctx tied to the
    job_id) that calls DurableHistory.Replay(...) + drains the
    channel into per-event diff facts written via
    substrate.AppendEvent(kind=fact, key="w9.replay.<job_id>.<event_id>",
    payload={verdict, reason, divergent_keys})
  - Returns 303 to GET /runs/{run_id}/replay/{job_id}

GET /runs/{run_id}/replay/{job_id}
  - HTML progress page; W7 TailFacts seam (every 5s hx-trigger
    per W7 §3.4) polls substrate_events WHERE kind='fact' AND
    key LIKE 'w9.replay.<job_id>.%' AND run_id=?, renders the
    per-kind divergence summary as a table.
  - Replay job writes a terminal fact key='w9.replay.<job_id>.complete'
    when the channel drains; UI shows "complete" + final summary.
```

Background-job ctx: derived from `cmd/regatta`'s root ctx (the same one the orchestrator uses). On `regatta serve` shutdown, in-flight jobs cancel cleanly; the substrate fact log retains partial-progress records so a re-trigger picks up only the missing kinds.

**Per W7 §3.6.4** (Principal forward-compat): the W9 handler takes `Principal` as a parameter; W8's OPA gate runs at the middleware layer (W8 spec §3.6) and rejects cross-tenant trigger before the W9 handler ever runs. W9 defends in depth via `DurableHistory.Replay`'s `opts.TenantID` check (§3.1).

### 3.6 P2.5 trigger refinement — metrics + alert

The locked red-team spec §3 defines three trigger conditions. W9 ships the metrics infrastructure that powers them:

| Metric (OTel meter `regatta.history`) | Aggregation | Trigger threshold |
|---|---|---|
| `regatta.history.sqlite_contention_pct` | gauge, % of scheduler ticks blocked on `database is locked` over a 24-h window | > 5 % for two consecutive 24-h windows |
| `regatta.history.concurrent_programs` | gauge, `work_items WHERE state='running' AND kind='program'` row count | ≥ 30 at steady state for two consecutive 24-h windows |
| `regatta.history.replay_recovery_seconds` | histogram, end-to-end Replay duration for a single `run_id` with ≥ 1000 journal entries | p95 > 60 s for two consecutive 24-h windows |

The metrics live in `internal/history/metrics.go`; the alert rule lives in `docs/operator/observability.md` (W6 T7 owns that file — W9 appends a §"P2.5 trigger" section via `[w9-followup]` doc PR).

When any condition holds, the operator flips `history.backend: temporal` in `regatta.yaml`. v1 ships a single backend (substrate-default); the config flag is reserved but unimplemented. **The Temporal-backed impl is design-only in §8 T6 — no code lands until the trigger fires.**

### 3.7 OTel attrs on replay spans

Per W6 §3.3 (Config.Tracer injection pattern), W9 emits a `replay` span tree:

```
replay        (kind=internal, attrs: regatta.replay.run_id,
                                     regatta.replay.original_trace_id,
                                     regatta.replay.divergence_count)
  ├─ replay.fold       (kind=internal, attrs: substrate.events.read_count)
  ├─ replay.reexecute  (kind=internal, attrs: regatta.replay.event_kind,
                                              regatta.replay.event_id,
                                              regatta.replay.nondeterministic?)
  └─ replay.diff       (kind=internal, attrs: regatta.replay.diff.verdict)
```

`regatta.replay.original_trace_id` is read from the substrate event's `trace_id` column (W6 §3.5 — the W3C 32-hex value). Operators jump from a replay span to the original run's trace in their OTel backend via this seam.

`regatta.replay.divergence_count` is the sum of `divergent` verdicts emitted by the diff harness for the run. The replay-job goroutine sets it on the root `replay` span just before the span closes.

---

## §4 Existing patterns reused (deletion default)

Per `feedback_deletion_default` + the locked red-team §8: W9 v1 introduces **no new storage primitive**, **no new sign/verify primitive**, and **no new HTTP listener**.

| Concern | Existing primitive reused | Why no bespoke |
|---|---|---|
| Event log read | `substrate.Fold(runID, kind)` (substrate spec §2.3) | Substrate is the only journal; W9 reads, never writes new schema |
| Trace-context propagation | `Config.Tracer trace.Tracer` injection (W6 §3.3, T5) | Mandated DI pattern — replay opens spans via injected tracer |
| Trace ↔ row join | `substrate_events.trace_id` column (W6 §3.5) | Replay span attrs read from this column |
| Operator UI listener | `internal/uiserver/` HTTP scaffold (W7 Wave 7.0, PR #263) | Replay routes ride the existing mux |
| Background polling cadence | W7 TailFacts `every 5s` hx-trigger (W7 §3.4) | Progress page reuses the W7 streaming primitive |
| Tenant gating | W8 OPA Authorizer (W8 §3.1) + Principal forward-compat (W7 §3.6.4) | Cross-tenant replay rejected upstream; W9 defends in depth |
| Reducer semantics | `substrate.ReducerStrategy` + `defaultReducer(kind)` (substrate spec §4) | Diff harness is reducer-aware; no new strategy enum |
| Signature verify | `substrate.Verify` (substrate spec §5 + T-S1) | Replay verifies every folded event before re-execution |
| CEL evaluation (gate_verdict re-exec) | `CELDecider.Decide` (substrate spec §2.2, T-S2) | Replay re-evaluates the same CEL program over the journaled Snapshot |

**What got smaller** (deletion-default per `feedback_deletion_default`):

- **No parallel `history` table.** v1 of the brief considered a `regatta-owned journal` separate from substrate; this spec collapses replay storage onto substrate.
- **No new `regatta replay` CLI in v1.** The brief named `regatta replay <run_id> --from=<node>`; v1 ships the operator-UI trigger only (the CLI lands as a thin shim in v2 once `--from=<node>` partial replay is supported). Saves ~200 LoC of CLI parsing + flag plumbing for v1.
- **Temporal SDK dependency kept out of v1 go.mod.** §8 T6 is design-only; no Temporal import lands until the §3.6 trigger fires. Build weight: zero.
- **No new SQL migrations.** Substrate spec §2.1 + W6 §3.5 already ship every column W9 needs.
- **`PinSet` declared but unimplemented.** The interface signature accepts edit-and-replay knobs (so v2 doesn't break the API); v1's substrate impl rejects non-zero `PinSet` with `ErrUnsupported`. ~80 LoC of pin-loader scaffolding deferred to v2.

---

## §5 Risk register (R1–R10)

### R1 — Non-determinism in re-executors (clock, rand, network)

**Threat**: A re-executor reads `time.Now()` or `crypto/rand` or makes a network call; replay produces a different payload than the original → diff harness reports false-positive divergence.
**Mitigation**: §3.3 quarantine: re-executors that detect these sources call `nondeterministic.Mark(ctx, "<reason>")`; diff harness downgrades the event to `ReplaySkipped`. v1 re-executors never call these sources (re-derivation reads only the substrate fold) — quarantine fires only on a re-executor bug.
**Verify**: `TestW9_QuarantineMarksNondeterministic` — fake re-executor calls `nondeterministic.Mark(ctx, "test")`; assert `DiffVerdict == ReplaySkipped` and `Reason == "test"`.

### R2 — Replay-during-active-run race

**Threat**: Operator triggers replay while the run is still writing new events to substrate. Replay folds a partial journal; diff reports divergence against events that don't exist yet.
**Mitigation**: `DurableHistory.Replay` reads under sqlite WAL snapshot isolation (substrate spec §10 #2 — `TestSubstrate_ConcurrentFoldReadSnapshot`); the snapshot is captured at tx-begin time. Diff is run against the snapshot. If the operator wants a fresh snapshot, they re-trigger. UI label: "snapshot at <ts>" on the progress page.
**Verify**: `TestW9_ReplayDuringActiveRunReadsSnapshot` — start a Replay, then concurrently AppendEvent; assert Replay only sees the pre-snapshot events.

### R3 — OOM on long histories

**Threat**: A 1M-event run folds entirely into memory; replay OOMs.
**Mitigation**: `DurableHistory.Replay` is a streaming channel — events flow through the diff harness one at a time. The substrate prepared-statement handle is the only persistent state. Diff results are written back to substrate as facts (§3.5), not accumulated in memory. Memory bound: O(1) per replay job.
**Verify**: `TestW9_Replay1MEventsMemoryStable` (build-tag `load`) — synthetic 1M-event fixture; assert `runtime.MemStats.HeapInuse` delta < 100 MiB across the replay.

### R4 — Divergence false-positive on LLM output variance

**Threat**: An LLM-touching event (e.g. `node_output` whose payload contains LLM-generated prose) is re-derived deterministically from substrate inputs — but the original event's payload was the LLM's actual response. Replay produces the substrate-derived value; diff reports divergence even though the inputs match.
**Mitigation**: v1 re-executors do NOT re-call LLMs (read-only replay, §1.2). For `node_output` events whose payload was LLM-generated, the re-executor reads the journaled payload verbatim from `supersedes` chain (no re-derivation) and reports `Match` by definition. Edit-and-replay (v2, §1.2) is where LLM re-call lives; only then does divergence become semantically meaningful.
**Verify**: `TestW9_LLMNodeOutputReplaysAsMatch` — fixture with a journaled LLM `node_output`; assert replay reports `Match` and the re-executor does NOT call any LLM client.

### R5 — Temporal swap-back risk after trigger fires then de-fires

**Threat**: Operator flips to Temporal under load; load subsides; operator wants to flip back to substrate. Temporal-written events live in Temporal Event History only; substrate has a gap.
**Mitigation**: §3.6 + locked red-team §7 — the substrate impl continues writing in parallel during a Temporal-flip window (`dual-write` mode); only after a documented N-day cooldown does the substrate writer stop. Flip-back is zero-loss within that window. v1 ships only the substrate-default; the flip semantic is design-only in §8 T6.
**Verify**: Documented in operator runbook (`[w9-followup]` doc PR appends to `docs/operator/observability.md`). No v1 test (Temporal impl absent).

### R6 — Substrate trace_id mismatch (W6 spec §R8)

**Threat**: W6 spec §R8 flagged "Replay-time span replay correctness": a replayed `work_item` opens a span under a NEW trace_id; the original `trace_id` in the substrate row no longer points at the replayed lifecycle.
**Mitigation**: §3.7 — replay spans carry `regatta.replay.original_trace_id` (read from `substrate_events.trace_id`), NOT a rewritten value. The replay span tree is a SEPARATE trace; cross-link via the attribute. Operator runbook documents the join: "find original trace via `replay.original_trace_id` attr; replay trace is the active span tree."
**Verify**: `TestW9_ReplaySpanCarriesOriginalTraceID` — start a replay within a span; assert the replay root span has `regatta.replay.original_trace_id` equal to the substrate event's trace_id and that the replay's own trace_id is distinct.

### R7 — Cross-tenant replay leak

**Threat**: A Principal whose `TenantID = T1` triggers replay for a run whose events carry `TenantID = T2`; the W8 OPA gate is misconfigured (allow-all bundle); replay leaks T2's events into T1's UI.
**Mitigation**: Defence in depth — W8 OPA gate filters at the HTTP boundary (W8 §3.6); W9's `DurableHistory.Replay` re-checks `opts.TenantID` against every folded event and returns `ErrCrossTenant` on mismatch. Two layers must both fail for a leak.
**Verify**: `TestW9_CrossTenantReplayRejected` — fold events with mixed tenant_id, set `opts.TenantID` to one tenant; assert `ErrCrossTenant`.

### R8 — Operator UI replay-job leak on shutdown

**Threat**: Background replay job's goroutine outlives `regatta serve` shutdown; partial diff facts land in substrate after the shutdown ctx cancels; orphan span never closes.
**Mitigation**: §3.5 job ctx derives from the root orchestrator ctx; shutdown cancels in-flight jobs. The replay job's defer block calls `span.End()` and the diff-fact writer flushes pending writes before returning. `goleak.VerifyNone` in test suite.
**Verify**: `TestW9_ReplayJobShutsDownCleanly` — start a Replay; cancel root ctx mid-stream; assert goroutine returns within 1s and no diff facts are written after cancel.

### R9 — Re-executor registry drift across kinds

**Threat**: substrate adds a new `EventKind` (e.g. via the §8 of substrate spec follow-ups F4 TTL cron); W9's re-executor registry doesn't know about it; replay silently skips events of the new kind.
**Mitigation**: An `enum_parity_test` (mirrors substrate T-S3's `TestSubstrate_EventKindEnumMatchesSQLCheck`) asserts every `substrate.EventKind` constant has either a registered re-executor OR is in an explicit `noReExecutorKinds` allow-list. Adding a new kind without either move ⇒ test fails. Allow-list ships with `KindHeartbeat`, `KindFact`, `KindTokenSpend` (the three non-deterministic kinds).
**Verify**: `TestW9_ReExecutorRegistryCoversAllKinds`.

### R10 — Sqlite contention metric ↔ replay-recovery metric double-counting

**Threat**: Replay itself causes sqlite read contention; replay-recovery metric measures wall-clock including contention; contention metric and recovery metric correlate; P2.5 trigger fires spuriously.
**Mitigation**: §3.6 — `regatta.history.sqlite_contention_pct` measures **writer-side** lock contention (`database is locked` on AppendEvent), not reader contention. Replay is read-only; it cannot inflate this metric. The recovery metric is independent.
**Verify**: `TestW9_ReplayDoesNotInflateContentionMetric` — start 10 concurrent Replays; assert `regatta.history.sqlite_contention_pct` stays at 0 (no writes blocked).

---

## §6 Test plan per task (B / A / A+ tier)

Per `feedback_grade_rubric`. Implementer captures failing-test output before writing impl per `feedback_tdd_discipline`. Tests are listed by §8 Task ID.

### T1 — `DurableHistory` interface + substrate-default impl

**B-tier:**
- `TestHistory_AppendDelegatesToSubstrate` — Append wraps `substrate.AppendEvent`; same idempotency (`substrate.ErrReplay` on UNIQUE collision).
- `TestHistory_TailStreamsNewEvents` — Tail emits events written after `since` cursor; closes channel on ctx cancel.
- `TestHistory_ReplayFoldsAllKinds` — Replay streams one ReplayedEvent per folded event across all `IncludeKinds`.
- `TestHistory_ReplayRejectsCrossTenant` — folded events with mismatched tenant_id ⇒ `ErrCrossTenant` (R7).
- `TestHistory_ReplayRejectsNonEmptyFromNodeID` / `TestHistory_ReplayRejectsNonZeroPinOverride` — v1 reserved fields return `ErrUnsupported`.

**A-tier:**
- `TestW9_ReplayDuringActiveRunReadsSnapshot` (R2).
- `TestW9_ReplaySpanCarriesOriginalTraceID` (R6).
- `TestW9_ReplayJobShutsDownCleanly` + `goleak.VerifyNone` (R8).

### T2 — Replay engine + diff harness

**B-tier:**
- `TestW9_DiffMatchOnIdenticalPayload` — same canonical-JSON ⇒ `Match`.
- `TestW9_DiffDivergentNamesKeys` — payload diverges on `{a: 1}` vs `{a: 2}` ⇒ `Divergent` with `DivergentKeys=["a"]`.
- `TestW9_DiffReducerAware` — append-reducer kinds compare per-pair; lww-reducer kinds compare only head.
- `TestW9_QuarantineMarksNondeterministic` (R1) — fake re-executor calls `nondeterministic.Mark`; assert `ReplaySkipped`.
- `TestW9_LLMNodeOutputReplaysAsMatch` (R4) — LLM-generated node_output replays without LLM call, reports Match.

**A-tier:**
- `TestW9_DiffSummaryPerKind` — multi-kind run produces per-kind divergence summary.
- `TestW9_ReExecutorRegistryCoversAllKinds` (R9).

### T3 — Operator UI replay button + background job

**B-tier:**
- `TestW9_POSTReplayReturns303` — handler spawns job, returns 303 to progress page.
- `TestW9_POSTReplayRejectsCrossTenantPrincipal` — Principal.TenantID ≠ run.TenantID ⇒ 403.
- `TestW9_ProgressPageRendersDiffFacts` — substrate has facts under `w9.replay.<job_id>.*`; progress page lists them.
- `TestW9_ProgressPageRendersComplete` — terminal fact `w9.replay.<job_id>.complete` ⇒ "complete" badge.

**A-tier:**
- `TestW9_BackgroundJobWritesFactsViaSubstrate` — replay job's diff facts persist as `kind=fact` events with the expected `key` shape.
- `TestW9_ProgressPagePollingRespectsW7Cadence` — `hx-trigger="every 5s"` per W7 §3.4.

### T4 — OTel attrs + non-determinism quarantine

**B-tier:**
- `TestW9_ReplaySpanAttrs` — root replay span carries `regatta.replay.run_id`, `regatta.replay.original_trace_id`, `regatta.replay.divergence_count` (the last is set just before span.End).
- `TestW9_NondeterministicSpanAttr` — quarantined event's `replay.reexecute` child span has `regatta.replay.nondeterministic=true`.

**A-tier:**
- `TestW9_DivergenceCountAccumulates` — multi-event run with mixed verdicts; divergence_count equals the count of `Divergent` verdicts.

### T5 — P2.5 trigger metric + alert

**B-tier:**
- `TestW9_SqliteContentionMetricEmits` — induce `database is locked`; metric counter increments.
- `TestW9_ConcurrentProgramsMetricEmits` — 30 running program rows; gauge reads 30.
- `TestW9_ReplayRecoveryHistogramEmits` — Replay finishes; histogram records the duration.
- `TestW9_ReplayDoesNotInflateContentionMetric` (R10).

**A-tier:**
- `TestW9_AlertFiresAfterTwoWindows` — synthetic two-window threshold breach ⇒ alert handler invoked.

### T6 — Temporal-backed impl (design-only stub)

v1 ships zero code for T6. No tests. The §8 T6 deliverable is a markdown stub in `internal/history/temporal/README.md` documenting the trigger and the swap procedure. **An impl PR lands only when the §3.6 trigger fires.**

### A+ (cross-cutting)

- A+1. `TestW9_Replay1MEventsMemoryStable` (load test, build-tag `load`) — R3.
- A+2. Property test (`pgregory.net/rapid`) — 100 synthetic substrate fixtures (deterministic kinds only); assert Replay produces `Match` for every event (replay determinism). Pins R1's design-time claim.
- A+3. p95 replay-recovery < 60 s on a 1000-event history. Verify via `BenchmarkReplay1000Events` reporting p95 below the threshold.

---

## §7 Grade rubric

Per `feedback_grade_rubric`. Three tiers, tool-checkable.

### B (floor — ships)

- B1. `DurableHistory` interface + substrate-default impl land per §3.1; B-tier tests in §6 pass. Verify: `go test ./internal/history/... -count=1`.
- B2. Operator UI `/runs/{run_id}/replay` POST + GET handlers ship per §3.5; B-tier tests in §6 T3 pass.
- B3. Cross-tenant replay rejected at both W8 layer + W9 defence-in-depth layer (R7). Verify: `TestW9_CrossTenantReplayRejected` + `TestW9_POSTReplayRejectsCrossTenantPrincipal`.
- B4. Zero new SQL migrations. Verify: `git diff --name-only origin/main...HEAD -- 'internal/orchestrator/state/migrations/*.sql'` returns empty.
- B5. PR body carries `release-notes` fence per `feedback_pr_body_release_notes_fence`. Verify: `grep -E '^```release-notes' <body-file>` matches exactly once.
- B6. Replay against a simple linear DAG fixture (4 deterministic node_output events) reports `divergence_count=0`. Verify: `TestW9_LinearDAGReplaysWithZeroDivergence`.

### A (target — expected)

- A1. B + adversarial-reviewer subagent finds zero unaddressed issues per `feedback_adversarial_review`. Verify: reviewer transcript posted to PR body.
- A2. Replay against a branching DAG (≥1 gate_verdict + ≥2 downstream node_outputs) reports `divergence_count=0` on clean replay; with injected non-determinism (one re-executor calls `nondeterministic.Mark`) reports the affected event as `ReplaySkipped`. Verify: `TestW9_BranchingDAGReplaysWithoutDivergence` + `TestW9_QuarantineMarksNondeterministic`.
- A3. OTel attrs on replay spans match §3.7 verbatim. Verify: `TestW9_ReplaySpanAttrs` + `TestW9_NondeterministicSpanAttr` + `TestW9_DivergenceCountAccumulates`.
- A4. P2.5 trigger metrics emit per §3.6. Verify: T5 B-tier tests pass.
- A5. Every named-but-deferred sub-decision is filed as `[w9-followup]` per `feedback_unaddressed_load_bearing` (CLI shim, partial replay `--from=<node>`, edit-and-replay `--pin-model=<model>`, Temporal-backed impl, operator-runbook trigger doc, A+3 perf budget). Verify: `gh issue list --label w9-followup` returns ≥ 6.
- A6. Re-executor registry covers all current substrate `EventKind` constants (registered OR explicit skip-list). Verify: `TestW9_ReExecutorRegistryCoversAllKinds`.

### A+ (stretch — exceptional)

- A+1. Property test sweeps 100 deterministic-kind substrate fixtures and asserts `DiffVerdict == Match` for every event. Verify: `TestW9_ReplayDeterminismProperty` exit 0 with rapid seed pin.
- A+2. p95 replay-recovery < 60 s on a 1000-event history (mirrors the §3.6 P2.5 trigger threshold). Verify: `BenchmarkReplay1000Events` reports p95 < 60 s on GitHub Actions `ubuntu-latest` 4-vCPU.
- A+3. Memory stable across a 1M-event replay (R3). Verify: `TestW9_Replay1MEventsMemoryStable` (build-tag `load`) — `runtime.MemStats.HeapInuse` delta < 100 MiB.

---

## §8 File-disjoint implementation decomposition (preview)

Per the locked red-team §4: five methods, two backends, one diff engine. v1 lands the interface + substrate impl + diff harness + UI + metrics; the Temporal impl is a design-only stub.

| Task | Path (exclusive write scope) | Depends-on | Effort |
|---|---|---|---|
| **T1** | `internal/history/durable_history.go` (interface + types); `internal/history/substrate_impl.go` (substrate-default impl); `internal/history/errors.go`; `internal/history/durable_history_test.go` | substrate Wave 1 merged | M |
| **T2** | `internal/history/replay.go` (Replay engine); `internal/history/diff.go` (Diff harness); `internal/history/reexecutor.go` (registry + 4 deterministic re-executors); `internal/history/{replay,diff,reexecutor}_test.go` | T1 | M |
| **T3** | `internal/uiserver/replay.go` (POST + GET handlers); `internal/uiserver/templates/replay_progress.tmpl`; `internal/uiserver/replay_test.go` | T1, T2, W7 Wave 7.0 (#268 plan pending), W8 Authorizer (W8 spec §3.1, impl pending) | M |
| **T4** | `internal/history/otel.go` (span open/close, attr helpers); `internal/history/nondeterministic.go` (quarantine mark); `internal/history/otel_test.go` | T1, T2, W6 T5 shipped | S |
| **T5** | `internal/history/metrics.go` (3 OTel meter instruments per §3.6); `internal/history/metrics_test.go`; `[w9-followup]` doc PR appends §"P2.5 trigger" to `docs/operator/observability.md` | T1, W6 T1 shipped | S |
| **T6** | `internal/history/temporal/README.md` (design-only stub) — documents the trigger metric thresholds, the swap procedure, the dual-write window, and the v2 impl PR contract. **No Go code.** Concrete impl PR lands ONLY when the §3.6 trigger fires. | — | XS (markdown only) |

**Total v1 effort**: ~2.4 K LoC per the locked red-team §2 estimate (C-option), distributed as T1 ≈ 700 + T2 ≈ 900 + T3 ≈ 400 + T4 ≈ 150 + T5 ≈ 200 + T6 = 0 (markdown).

**Cross-task seam contracts** (load-bearing — implementer MUST honour exactly):
- T1 exports `DurableHistory`, `ReplayOpts`, `PinSet`, `ReplayedEvent`, `DiffResult`, `DiffVerdict`, plus sentinels `ErrCrossTenant`, `ErrUnsupported`. T2/T3/T4/T5 import these and only these.
- T2's `RegisterReExecutor(kind, fn)` registry mirrors substrate T-S1's `RegisterPayloadValidator` pattern (substrate plan §1): re-executors register from their own files' `init()`. Open-extensible.
- T3 takes `Principal` per W7 §3.6.4; the W8 OPA Authorizer gates at middleware; T3 does NOT introduce any new auth seam.
- T4 takes a `trace.Tracer` injected via the existing `Config.Tracer` pattern (W6 §3.3); T4 does NOT introduce a new tracer construction site.
- T5's metric names + units match §3.6 verbatim; alert thresholds are operator-config, not code-config.

---

## §9 Sequencing — W9 lands after W6 / W7 Wave 1 / W8 / Substrate Wave 1

Per the locked red-team §9 + substrate spec §11:

```
                MVP-3 sequence (Wave order):

   ┌──────────────────────────────────────────────────────────┐
   │ W6  OTel + GenAI semconv observability backbone          │
   │   T1-T5 SHIPPED (PRs #172, #169, #209, T4, #210)         │
   │   → Replay reads trace_id from substrate event           │
   │   → Config.Tracer injection seam reused                  │
   └──────────────────────────────────────────────────────────┘
                              │
                              ▼
   ┌──────────────────────────────────────────────────────────┐
   │ Substrate Wave 1   T-S1 + T-S2 + T-S3                    │
   │   → events table is the only journal                     │
   │   → CELDecider concrete type for gate_verdict re-exec    │
   │   → AppendEvent + Fold + ReducerStrategy + signing       │
   └──────────────────────────────────────────────────────────┘
                              │
                              ▼
       ┌──────────────────────┴───────────────────────┐
       ▼                                              ▼
   ┌────────────────────────┐               ┌──────────────────┐
   │ W7 Wave 7.0 SHIPPED    │               │ W8 spec SHIPPED  │
   │   #263 HTTP listener   │               │   #266; impl     │
   │ W7 #268 plan pending   │               │   pending        │
   │   → /runs/<id>/replay  │               │   → Authorizer   │
   │     rides existing mux │               │     gates W9 UI  │
   └────────────────────────┘               └──────────────────┘
       │                                              │
       └────────────────────┬─────────────────────────┘
                            ▼
              ┌─────────────────────────────┐
              │ W9  replay + diff harness   │ ← THIS SPEC
              │  T1 DurableHistory iface +  │
              │     substrate-default impl  │
              │  T2 Replay() + diff harness │
              │  T3 operator UI replay      │
              │  T4 OTel + quarantine spans │
              │  T5 P2.5 trigger metrics    │
              │  T6 Temporal stub (design-  │
              │     only; no code in v1)    │
              └─────────────────────────────┘
                            │
                            ▼ (MVP-4)
              ┌─────────────────────────────┐
              │ W10 in-toto / Sigstore      │
              │   uses W9 DiffResult        │
              │   to attest replay verdicts │
              └─────────────────────────────┘
```

**Pre-conditions for W9 dispatch:**

1. W6 T1–T5 merged to main — DONE (PRs cited above).
2. Substrate Wave 1 (T-S1 + T-S2 + T-S3) merged to main — substrate spec is locked; plan in `docs/engineer/plans/2026-06-01-substrate-w1-tasks.md`; dispatch pending.
3. W7 Wave 7.0 PRs (#263 listener + #268 plan execution) merged to main.
4. W8 impl shipped — Authorizer middleware available for W9's T3 handler.

**Wave count for W9**: 1 dispatch wave. T1 ships first (spine; owns interface + substrate impl). T2/T3/T4/T5 dispatch in parallel against T1's branch via the substrate-plan `feedback_shared_primitive_owner` pattern. T6 ships as a markdown PR alongside T5 (zero code).

---

## §10 Deferred (named-but-not-shipped per `feedback_unaddressed_load_bearing`)

Each MUST be filed as `[w9-followup]` BEFORE T1's PR opens. PR body cites issue numbers.

| # | Title | Why deferred | Re-enable when |
|---|---|---|---|
| F1 | `[w9-followup]` `regatta replay <run_id>` CLI shim | v1 is UI-triggered only; CLI is a thin wrapper that the v2 partial-replay knob requires anyway | v2 partial-replay PR |
| F2 | `[w9-followup]` Partial replay `--from=<node>` semantics + impl | `ReplayOpts.FromNodeID` reserved field; v1 rejects non-empty with `ErrUnsupported` | Operator request signal OR v2 wave begins |
| F3 | `[w9-followup]` Edit-and-replay `--pin-model=<model>` (PinSet impl) | `ReplayOpts.PinOverride` reserved field; v1 rejects non-zero with `ErrUnsupported` | v2 wave (after F2) |
| F4 | `[w9-followup]` Cross-tenant replay UX (admin-scope Principal) | v1 rejects cross-tenant in both layers (R7); admin-scope replay needs a W8 policy bundle extension | W8 multi-tenant cutover |
| F5 | `[w9-followup]` Temporal-backed `DurableHistory` impl + dual-write window | §3.6 P2.5 trigger; §8 T6 design-only stub in v1 | Any §3.6 condition holds two consecutive 24-h windows |
| F6 | `[w9-followup]` P2.5 trigger operator runbook | Documented in `docs/operator/observability.md`; W6 T7 owns that file | Alongside T5 in v1 (doc PR) |
| F7 | `[w9-followup]` 1M-event load test in CI (A+3 perf budget) | Build-tag `load`; not pre-merge | A+ tier polish wave |
| F8 | `[w9-followup]` Replay-result attestation in W10 chain | W10 supply-chain wedge consumes `DiffResult` to sign "replay verified" attestations | MVP-4 W10 |

---

_Spec authority: `feedback_spec_pattern_authority` — implementer subagent deviation from this spec requires re-spawning the design subagent. The 8 follow-up issues in §10 MUST be filed and cited in the T1 PR body per `feedback_unaddressed_load_bearing` + `feedback_review_before_automerge`._
