---
title: "S2-T1 — W9 Replay+Diff Harness, Substrate-Default DurableHistory Impl Spec"
status: shipped
shipped_at: 2026-06-04
phase: x-forward-fit
summary: "Phase S2-T1: substrate-default DurableHistory impl only. Compares against Temporal-backed alternative for rationale; the Temporal variant itself is Phase-X per brief §4."
---

# S2-T1 — W9 Replay+Diff Harness, Substrate-Default `DurableHistory` Impl Spec

Status: ready for review
Date: 2026-06-02
Author: design subagent (Opus 4.7, pre-fetch wave per `feedback_roadmap_pre_fetch`)
Binding brief: `docs/engineer/briefs/2026-06-01-self-host-first.md` §3 S2-T1 (Phase S2, rank #1).
Parent design (already on main): `docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md`.
Locked red-team (already on main): `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md` — Option C hybrid PICKED; substrate-default first; Temporal-backed Phase X per self-host brief §4.

Memory in force: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_decision_priority`, `feedback_adversarial_review`, `feedback_cross_doc_link_phasing`, `feedback_deletion_default`, `feedback_unaddressed_load_bearing`, `feedback_doc_check_banned_phrases`, `feedback_pr_body_release_notes_fence`.

---

## §0 Why this spec exists (and why it is narrow)

The parent W9 design spec covers both substrate-default and Temporal-backed `DurableHistory` variants (the locked Option C hybrid). The self-host-first brief §4 **defers the Temporal-backed variant to Phase X** until an external customer or the P2.5 trigger fires. This spec is the operational sub-spec for Phase S2-T1: the substrate-default implementation, scope-frozen to what self-host needs, with every Temporal hook removed from the v1 critical path.

What this spec adds beyond the parent design:

1. Adopted prior-art shape locked into the interface signature (Temporal `WorkflowReplayer` API echoed verbatim, plus AWS Step Functions Redrive semantic for resume-on-divergence; §2).
2. A frozen file-disjoint task breakdown (≥3 tasks; §7) ready for implementer dispatch — narrower than parent §8 (drops Temporal stub T6 entirely; Phase X re-introduces it).
3. A B/A/A+ rubric matched to self-host acceptance (single-tenant; one operator; sqlite; no Temporal binary; §6).
4. Cross-doc link phasing decision: this spec only forward-links to docs already on `origin/main` (parent design, red-team spec, brief, substrate spec, W6 OTel spec). No new sibling docs are introduced in this PR. Safe per `feedback_cross_doc_link_phasing`.

What this spec deliberately does NOT re-litigate:

- The Temporal vs. bespoke trade. Decided in the red-team. Phase X.
- The interface admitting a future Temporal impl. Already in the parent design `DurableHistory` shape; this spec inherits that shape unchanged so Phase X drops in without an API break.
- Operator UI replay route (`POST /runs/{run_id}/replay`). Deferred to Phase X per the brief: the self-host operator triggers replay via CLI (introduced in §7 T3 below, scope-bounded to a thin shim).

---

## §1 In / Out

### IN (this spec authorizes implementer dispatch for)

1. `internal/history/` Go package exporting `DurableHistory` interface + `ReplayedEvent` + `ReplayOpts` + `PinSet` + `DiffResult` + `DiffVerdict` + sentinels (`ErrCrossTenant`, `ErrUnsupported`). Signature is the parent design §3.1 verbatim — no drift permitted.
2. Substrate-default impl: reads `substrate_events` via `substrate.Fold`; verifies signatures via `substrate.Verify`; emits `ReplayedEvent` stream.
3. Replay engine + per-`EventKind` re-executor registry (4 deterministic kinds: `node_output`, `approval_event`, `gate_verdict`, `budget_reconciled`).
4. Diff harness — reducer-aware structural diff over canonical-JSON payloads.
5. CLI shim — `regatta replay <run_id>` reads run, streams `ReplayedEvent`, prints per-kind summary, exits non-zero on any `Divergent`. CLI-only trigger for v1 self-host (no UI).
6. P2.5 trigger metrics emit-only (§3.6 of parent design) — metrics ship; alert rule is operator-config in `regatta.yaml`.

### OUT (Phase X — re-enter on external buyer trigger per self-host brief §4)

- Temporal-backed impl (no Temporal SDK in `go.mod`; no stub markdown either — parent design §8 T6 is the design home if Phase X re-opens).
- Operator web UI replay route (`POST /runs/{run_id}/replay` + progress page). Parent design §3.5 owns the spec; defer the code until W7 web UI re-enters Phase X scope.
- Multi-tenant replay UX (cross-tenant Principal). Defence-in-depth tenant_id check stays (R7 in parent §5); admin-scope cross-tenant UX deferred.
- Partial replay (`--from=<node>`), edit-and-replay (`--pin-model=<model>`). Reserved fields on `ReplayOpts` per parent §3.1; v1 returns `ErrUnsupported`.
- Replay-result attestation in W10. MVP-4.

---

## §2 Prior art adopted (research-first per `feedback_research_design_principles`)

The decision priority in force is **UX → ease → performance → best-practices → speed → velocity** (`feedback_decision_priority`); the design-time refinement is **UX → the leading existing impl → best-practices → long-term** (`feedback_research_design_principles`). The interface signature is anchored on shapes operators already know.

| Prior art | What we adopt | What we drop | Source |
|---|---|---|---|
| Temporal Go SDK `worker.WorkflowReplayer` | Method-set shape — a typed interface with `Replay*` methods that stream a single workflow task's expected outputs given a journal. Implementations live behind one interface so users can swap backends without rewriting callers. | The `RegisterWorkflow*` family (worker-bootstrap concerns regatta doesn't have — substrate already routes events by `EventKind`, not by user-registered code). Also dropped: `ReplayWorkflowExecution` which reads from a live Temporal service over RPC; substrate is in-process. | `https://pkg.go.dev/go.temporal.io/sdk/worker#WorkflowReplayer` (godoc, sdk-go v1.44.1). |
| AWS Step Functions `RedriveExecution` | Resume-from-failure semantic — replay re-evaluates only the steps that did not succeed; passing steps are not re-executed. Maps to our reducer-aware diff: `lww`-kinds that already settled compare against journaled head and short-circuit `Match`; `append`-kinds re-evaluate per pair. | The "redrive billing" surface (each redrive counts as a new state-transition charge) — irrelevant locally. | `https://docs.aws.amazon.com/step-functions/latest/dg/concepts-redrive-executions.html`. |
| Inngest step memoization | Step-as-memoized-unit — once a step output is journaled, replay returns the journaled bytes verbatim; only un-memoized steps execute. Adopted as the "LLM `node_output` replays as `Match` by reading `supersedes` chain verbatim" rule (parent design R4). | The "each step is a separate HTTP request" execution model — substrate is in-process Go. | `https://www.inngest.com/docs/learn/how-functions-are-executed`. |
| substrate `events` table (W1 SHIPPED — migration 0006) | Single source of truth for replay input. `substrate.Fold(runID, kind)` is the read primitive; `substrate.Verify` validates signatures pre-replay; `idx_substrate_events_kind` is the index Replay walks. **Zero new SQL migrations** — `feedback_deletion_default` honored. | n/a (we adopt all of it). | `internal/orchestrator/state/migrations/0006_substrate.sql` + `internal/orchestrator/state/substrate/fold.go`. |

**Score** (the rule: ≥2 OSS replay primitives before bespoke):

- Temporal `WorkflowReplayer` — interface-shape match: 1.
- Step Functions Redrive — semantic-match for resume-on-divergence: 1.
- Inngest memoization — semantic-match for the LLM-output replay rule: 1.

Total: 3 prior-art adoptions, 0 bespoke primitives invented. Bespoke layer is the per-`EventKind` re-executor registry — and even that mirrors substrate's own `RegisterPayloadValidator` pattern (parent design §3.3), so it is not a new pattern in the codebase.

---

## §3 `DurableHistory` interface — substrate-default impl

Inherited verbatim from parent design §3.1. The signature below is the load-bearing contract:

```go
package history

import (
    "context"
    "io"

    "github.com/<repo>/internal/orchestrator/state/substrate"
)

type DurableHistory interface {
    Append(ctx context.Context, runID string, ev substrate.Event) error
    Tail(ctx context.Context, runID string, since string) (<-chan substrate.Event, io.Closer, error)
    Replay(ctx context.Context, runID string, opts ReplayOpts) (<-chan ReplayedEvent, io.Closer, error)
}

type ReplayOpts struct {
    TenantID     string
    IncludeKinds []substrate.EventKind
    FromNodeID   string  // reserved; v1 rejects non-empty (Phase X partial-replay)
    PinOverride  PinSet  // reserved; v1 rejects non-zero (Phase X edit-and-replay)
}

type PinSet struct {
    ModelID   string
    Seed      int64
    PromptSHA string
}

type ReplayedEvent struct {
    Original substrate.Event
    Replayed substrate.Event
    Diff     DiffResult
}

type DiffResult struct {
    Verdict       DiffVerdict
    Reason        string
    DivergentKeys []string
}

type DiffVerdict string

const (
    Match         DiffVerdict = "match"
    Divergent     DiffVerdict = "divergent"
    ReplaySkipped DiffVerdict = "replay_skipped"
)
```

**One-line signature for the dispatch acknowledgment**:

```go
type DurableHistory interface{ Append(ctx,runID,ev) error; Tail(ctx,runID,since) (<-chan Event, io.Closer, error); Replay(ctx,runID,opts) (<-chan ReplayedEvent, io.Closer, error) }
```

**Substrate impl rules** (no drift from parent §3.1):
- `Append` thin-wraps `substrate.AppendEvent`; same `UNIQUE(run_id, written_by, nonce)` idempotency.
- `Tail` SELECTs `WHERE run_id=? AND id > ? ORDER BY written_at, id` at caller cadence; uses `idx_substrate_events_kind`.
- `Replay` opens a read tx (sqlite WAL snapshot per parent R2), folds each kind in `opts.IncludeKinds`, merges journal-ordered, runs `runReExecutor(event)` per row, emits `ReplayedEvent`.
- Cross-tenant defence: every folded row's `TenantID` must equal `opts.TenantID` or `ErrCrossTenant` aborts the stream.

**Reducer awareness in Replay** (parent §3.4):
- `lww` (`node_output`, `fact`, `budget_reconciled`, `heartbeat`) — emit head only.
- `append` (`approval_event`, `token_spend`, `gate_verdict`) — emit per-pair in journal order.

**Non-determinism quarantine** (parent §3.3):
- v1 re-executors NEVER read clock / rand / network. If any does, `nondeterministic.Mark(ctx, "<reason>")` fires; diff downgrades verdict to `ReplaySkipped`. Quarantine fire indicates a re-executor bug; tracking issue auto-files.

---

## §4 Diff algorithm — structural diff over canonical-JSON

**Decision**: structural diff over canonical-JSON bytes (substrate `contracts/schemas/sign.go::CanonicalJSON`), keyed by JSON-path.

**Why this, not a semantic-diff library**:
- substrate already canonicalizes for signing; identical bytes ⇒ `Match` is a 1-line byte compare; no new library, no new code path.
- divergent bytes ⇒ walk the JSON tree via the in-repo `internal/canon` traversal helper (W6 already uses it); emit `DivergentKeys` as JSON-path strings (`$.outputs.token_count`, `$.gate.verdict`).
- non-deterministic kinds short-circuit before reaching diff (see §3 quarantine).

**Why not a third-party diff** (e.g. `jsondiff`):
- The substrate canonical-JSON form is already idempotent — byte-equality covers the `Match` case cheaply.
- JSON-path traversal for divergent-keys reporting is ~80 LoC in-repo; pulling a dep would add a ~2k LoC tree dependency with its own canonicalization rules that may disagree with substrate's. `feedback_deletion_default` rules: every addition needs A+ defense; this one fails it.
- A future Phase X follow-up MAY swap in `jsondiff` if reviewers find the in-repo traversal under-covers nested-array divergence; reserved as `[s2-t1-followup]`.

**Aggregation per run**: per-kind divergence count + per-run divergence count. Surface via:
- CLI: ANSI-coloured table per kind, exit-code = total divergences (`exit 0` ⇔ all `Match` or `ReplaySkipped`; `exit 1` ⇔ ≥1 `Divergent`).
- substrate fact: `kind=fact, key="w9.replay.<job_id>.summary", payload={per_kind:[...], total_divergent:N}`. Phase X UI reads this fact.

---

## §5 Substrate event-kind replay coverage (which kinds replay; which skip)

Inherits parent §3.3 verbatim. Encoded here for implementer's mechanical checklist:

| `EventKind` | Reducer | Replay behaviour | Re-executor source |
|---|---|---|---|
| `node_output` | `lww` | Re-derive from upstream `supersedes` + journaled work_item_inputs fold; LLM-generated payloads read verbatim from supersedes (parent R4) | `internal/history/reexec_node_output.go` |
| `approval_event` | `append` | Re-derive from approval state machine fold over journaled pending → approved/rejected/timed_out transitions | `internal/history/reexec_approval.go` |
| `gate_verdict` | `append` | Re-derive via `CELDecider.Decide` over the journaled `Snapshot` — substrate spec §10 #17 contract: one tx for fold + eval + emit; replay rebuilds Snapshot and re-evaluates the same CEL program | `internal/history/reexec_gate_verdict.go` |
| `budget_reconciled` | `lww` | Re-derive from `token_spend` SUM over the journaled fold window | `internal/history/reexec_budget.go` |
| `token_spend` | `append` | Skip — payload rides actual provider response (non-deterministic source = network) | quarantine list |
| `fact` | `lww` | Skip — payload is operator-injected or reducer-timing-dependent | quarantine list |
| `heartbeat` | `lww` | Skip — payload is wall-clock | quarantine list |

**Enum parity invariant**: a test (parent design R9 — `TestW9_ReExecutorRegistryCoversAllKinds`) asserts every `substrate.EventKind` constant has either a registered re-executor or is in the explicit `quarantineKinds` allow-list. Adding a new kind without either move ⇒ test fails. The list shipping is exactly `{KindTokenSpend, KindFact, KindHeartbeat}`.

---

## §6 Grade rubric (B / A / A+)

Per `feedback_grade_rubric`. Each tier tool-checkable; PR scorecard cites verbatim.

### B — floor (ships)

- **B1.** Round-trip identity for one DAG: a 4-node linear DAG with all-deterministic kinds replays with `divergence_count == 0`. Verify: `TestS2T1_LinearDAGReplaysRoundTripIdentity`.
- **B2.** `DurableHistory` interface + substrate impl exported per §3 signature. Verify: `go test ./internal/history/... -count=1` green; `go vet ./internal/history/...` clean.
- **B3.** Cross-tenant rejection (defence in depth, R7 of parent). Verify: `TestS2T1_CrossTenantReplayRejected`.
- **B4.** Reserved-field rejection (`FromNodeID`, `PinOverride`). Verify: `TestS2T1_ReservedFieldsReturnErrUnsupported`.
- **B5.** Zero new SQL migrations. Verify: `git diff --name-only origin/main...HEAD -- 'internal/orchestrator/state/migrations/*.sql'` returns empty.
- **B6.** PR body carries `release-notes` fence + scorecard verbatim. Verify: `grep -E '^```release-notes' <body-file>` matches once; scorecard block grep-matches the rubric IDs above.

### A — target (expected)

- **A1.** Structural diff across 2 runs of the same DAG (run A: clean; run B: one node_output payload byte-flipped post-write) reports exactly one `Divergent` event with the correct JSON-path key. Verify: `TestS2T1_StructuralDiffAcrossTwoRuns`.
- **A2.** Re-executor registry covers all 7 substrate `EventKind` constants (4 registered + 3 quarantined). Verify: `TestS2T1_ReExecutorRegistryCoversAllKinds`.
- **A3.** Adversarial-reviewer subagent finds zero unaddressed issues per `feedback_adversarial_review`. Verify: reviewer transcript posted to PR body or filed as `[s2-t1-followup]`.
- **A4.** CLI replay surface (`regatta replay <run_id>`) prints per-kind summary table + exits non-zero on any `Divergent`. Verify: `TestS2T1_CLIReplayExitsNonZeroOnDivergence` + `TestS2T1_CLIReplaySummaryTable`.
- **A5.** OTel attrs on replay span tree match parent design §3.7 verbatim (`regatta.replay.run_id`, `regatta.replay.original_trace_id`, `regatta.replay.divergence_count`, `regatta.replay.nondeterministic`). Verify: `TestS2T1_ReplaySpanAttrs`.
- **A6.** Every named-but-deferred sub-decision is filed as `[s2-t1-followup]` per `feedback_unaddressed_load_bearing` (Phase X operator UI route; partial replay `--from=<node>`; edit-and-replay `--pin-model`; W10 attestation hook; `jsondiff`-library escape hatch; Phase X Temporal-backed impl). Verify: `gh issue list --label s2-t1-followup` returns ≥ 6.

### A+ — stretch (exceptional)

- **A+1.** Replay survives schema migration: a fixture journaled under `substrate_events.schema_version=1` replays correctly after the table is migrated to `schema_version=2` (forward-only column add). Verify: `TestS2T1_ReplaySurvivesSchemaMigration` — load fixture, run goose up to a +1 migration that adds a nullable column, run replay, assert `divergence_count == 0`. (Note: this test fixtures a forward-compat migration; no v1 production migration ships, per B5.)
- **A+2.** Property test (`pgregory.net/rapid`) — 100 synthetic substrate fixtures of deterministic-kind events; assert `DiffVerdict == Match` for every event (replay determinism). Verify: `TestS2T1_ReplayDeterminismProperty` exit 0 with rapid seed pin.
- **A+3.** p95 replay-recovery < 60 s on a 1000-event history (matches parent §3.6 P2.5 trigger threshold). Verify: `BenchmarkS2T1_Replay1000Events` reports p95 < 60 s on GitHub Actions `ubuntu-latest` 4-vCPU.

---

## §7 File-disjoint task breakdown (≥3 tasks; ready for implementer dispatch)

Per `feedback_dispatch_strategy` (parallel default) + `feedback_plan_subagent_dup_files` (each task names exact paths). Four tasks, file-disjoint write scopes.

| Task | Path (exclusive write scope) | Depends-on | Effort | Owner role |
|---|---|---|---|---|
| **T1 — interface + substrate impl** | `internal/history/durable_history.go` (interface + types per §3); `internal/history/substrate_impl.go` (Replay/Tail/Append substrate-default); `internal/history/errors.go` (`ErrCrossTenant`, `ErrUnsupported`); `internal/history/durable_history_test.go` | substrate Wave 1 (SHIPPED) | M | spine — ships first; T2/T3/T4 import its types only |
| **T2 — replay engine + diff harness + re-executors** | `internal/history/replay.go` (engine); `internal/history/diff.go` (canonical-JSON byte-compare + JSON-path divergent-keys); `internal/history/reexecutor.go` (`RegisterReExecutor` registry mirroring substrate `RegisterPayloadValidator`); `internal/history/reexec_node_output.go`; `internal/history/reexec_approval.go`; `internal/history/reexec_gate_verdict.go`; `internal/history/reexec_budget.go`; `internal/history/{replay,diff,reexecutor}_test.go` | T1 | M | dispatches in parallel against T1 branch via `feedback_shared_primitive_owner` |
| **T3 — CLI shim** | `cmd/regatta/replay.go` (cobra subcommand `regatta replay <run_id>`); `cmd/regatta/replay_test.go` | T1, T2 | S | new file; does NOT touch existing `cmd/regatta/*` subcommands |
| **T4 — OTel attrs + quarantine + P2.5 metrics** | `internal/history/otel.go` (span open/close, attr helpers); `internal/history/nondeterministic.go` (`Mark(ctx, reason)`); `internal/history/metrics.go` (3 OTel meter instruments per parent §3.6); `internal/history/{otel,nondeterministic,metrics}_test.go` | T1, T2, W6 T5 (SHIPPED) | S | dispatches in parallel with T2 |

**Total v1 effort**: ~1.8 K LoC (Option A line from the red-team minus Temporal hooks).
- T1 ≈ 600 LoC.
- T2 ≈ 800 LoC (registry 200 + 4 re-executors 200 + diff 250 + replay engine 150).
- T3 ≈ 200 LoC.
- T4 ≈ 200 LoC.

**Cross-task seam contracts** (load-bearing — implementer MUST honour exactly):
- T1 exports the §3 type set + sentinels. T2/T3/T4 import these and only these.
- T2's `RegisterReExecutor(kind, fn)` registry mirrors substrate T-S1's `RegisterPayloadValidator` pattern: re-executors register from their own files' `init()`. Open-extensible without touching `reexecutor.go`.
- T3 takes a `DurableHistory` value via the existing `cmd/regatta` DI pattern (`regatta.yaml` parse + substrate handle hand-off); T3 does NOT introduce a new construction site for `DurableHistory`.
- T4 takes a `trace.Tracer` injected via the W6 `Config.Tracer` pattern; T4 does NOT introduce a new tracer construction site.
- Metric names in T4 match parent §3.6 verbatim: `regatta.history.sqlite_contention_pct`, `regatta.history.concurrent_programs`, `regatta.history.replay_recovery_seconds`.

**Shared-primitive owner** (per `feedback_shared_primitive_owner`): T1 owns the type set. Any T2/T3/T4 implementer who needs a type not in T1's exports MUST re-spawn the design subagent per `feedback_spec_pattern_authority` rather than add a type inline.

**Dispatch wave**: 1 wave. T1 lands first (10-20 min of substrate). T2 + T3 + T4 dispatch in parallel against T1's branch once T1's PR merges. The brief's §3 effort estimate (M, ~10-15 days subagent-time for entire S2) absorbs this comfortably.

---

## §8 Adversarial-reviewer subagent — load-bearing finding sweep

Per `feedback_adversarial_review` + `feedback_agent_pr_review`. Adversarial subagent ran on this spec; findings + resolutions inline:

| # | Finding | Severity | Resolution |
|---|---|---|---|
| F1 | Spec calls `gate_verdict` reducer `append` but parent §3.4 also calls it `append`. Confirm substrate matches. | medium | Verified: `internal/orchestrator/state/substrate/reducer.go::defaultReducer` returns `append` for `KindGateVerdict`. No drift. |
| F2 | A+1 (schema migration) ships a fixture migration. Does that violate B5 (zero new SQL migrations)? | high | **No** — the A+1 test fixtures a forward-compat migration inside the test helper directory (`internal/history/testdata/migrations/`), never in `internal/orchestrator/state/migrations/`. Production migration set unchanged. Clarified in §6 A+1 note. |
| F3 | CLI shim (T3) was OUT in parent §1.2 (deferred to v2). This spec promotes it back. Why? | high | Self-host brief §4 defers the operator UI to Phase X; the operator has to trigger replay somehow. CLI is the cheapest surface and the brief permits it (operator runs `regatta` directly). The CLI scope is bounded: read-only replay of a complete run; no `--from`, no `--pin-model`. Reserved fields stay reserved. F1 of parent §10 is closed by T3 here. |
| F4 | A+3 perf budget (p95 < 60s on 1000 events) uses the parent §3.6 P2.5 trigger threshold. Is that the right bar for a 1000-event run? | low | The P2.5 threshold is per-run; 1000-event is a representative single-run size. Same number deliberately, so the perf-budget regression alarm and the P2.5 trigger alarm fire on the same wall-clock. Documented in §6 A+3. |
| F5 | OTel attrs (A5) duplicate parent §3.7 — risk of drift if parent updates. | medium | Spec defers to parent §3.7 verbatim; A5 test asserts the parent's named keys are present, no extras. Drift in the parent forces a re-spec per `feedback_spec_pattern_authority`. |
| F6 | The "Phase X" exit clause is named in §0/§1 but no concrete re-entry trigger is defined. | medium | Re-entry trigger inherited from self-host brief §4 + parent §3.6: external customer ask OR any P2.5 condition holds two consecutive 24-h windows. Both gates already documented upstream. No new clause needed. |
| F7 | `jsondiff` escape hatch is a `[s2-t1-followup]` per A6, but the spec doesn't say when to pull it. | low | Trigger: ≥3 `Divergent` events in production replay where the in-repo JSON-path traversal reports an under-covered key. Threshold filed into the follow-up issue body. |
| F8 | No banned-phrase sweep referenced. Doc-check will fail. | high | Pre-push sweep added to §10 sequencing per `feedback_doc_check_banned_phrases`. The spec itself contains no banned tokens (verified at draft time). |

All findings either resolved in this revision or accepted with explicit rationale. Reviewer transcript will be attached to the PR per A3.

---

## §9 Risk register (delta vs. parent §5)

Parent §5 R1–R10 inherit verbatim. Substrate-default scope tightens two risks:

- **R5 (Temporal swap-back risk)** — n/a in this spec; no Temporal code lands. Re-enters in Phase X.
- **R7 (cross-tenant leak)** — single-tenant self-host means leak surface is operator-machine-local (one tenant); the `opts.TenantID` defence-in-depth check still ships (single-tenant default `tenant_id='default'`) so Phase X multi-tenant cutover doesn't have to retrofit it.

New risk introduced by the CLI shim (T3):

- **R11 — CLI replay run-id confusion**: operator pastes wrong `run_id`; replay reports `Match` on an unrelated run + no error fires.
  - Mitigation: CLI prints `run_id`, `kind_counts`, `tenant_id` header BEFORE running diff; operator confirms via `--yes` flag or interactive y/N. Default is interactive.
  - Verify: `TestS2T1_CLIReplayConfirmsBeforeRunning`.

---

## §10 Sequencing

```
                   S2-T1 substrate-default impl land sequence:

   ┌──────────────────────────────────────────────────────────┐
   │ Substrate Wave 1   T-S1 + T-S2 + T-S3   (SHIPPED)        │
   │   → substrate_events table + Fold + Verify + CELDecider  │
   └──────────────────────────────────────────────────────────┘
                              │
                              ▼
   ┌──────────────────────────────────────────────────────────┐
   │ W6 OTel T1–T5 (SHIPPED)                                  │
   │   → trace_id column + Config.Tracer injection            │
   └──────────────────────────────────────────────────────────┘
                              │
                              ▼
   ┌──────────────────────────────────────────────────────────┐
   │ T1 — DurableHistory interface + substrate impl           │
   │   PR opens with §6 scorecard verbatim + this spec link   │
   │   Reviewer adversarial pass per A3                       │
   └──────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
       ┌──────────┐    ┌──────────┐    ┌──────────┐
       │ T2 — re- │    │ T3 — CLI │    │ T4 — OTel│
       │ play +   │    │ shim     │    │ + quaran │
       │ diff +   │    │          │    │ + metrics│
       │ re-execs │    │          │    │          │
       └──────────┘    └──────────┘    └──────────┘
              │               │               │
              └───────────────┼───────────────┘
                              ▼
                       S2-T1 complete
                  → S2-T2 reviewer-as-gate
                  → S2-T3 followup auto-triage
                  → S2-T4 mutation testing
```

**Pre-push gates** (every PR):
1. `make check` green.
2. `make pr-lint` green per `feedback_pr_lint_gates` (separate from `make check`).
3. Banned-phrase grep clean per `feedback_doc_check_banned_phrases` (11-token denylist; see memory entry for the canonical list).
4. PR body carries `release-notes` fence + scorecard verbatim per `feedback_pr_body_release_notes_fence`.

**No automerge until** reviewer subagent transcript posted AND every Risk-tier finding either resolved or filed as `[s2-t1-followup]` per `feedback_review_before_automerge`.

---

## §11 Deferred sub-decisions (filed pre-T1-merge per `feedback_unaddressed_load_bearing`)

Each MUST be filed as `[s2-t1-followup]` BEFORE T1's PR opens. PR body cites issue numbers.

| # | Title | Why deferred | Re-enable when |
|---|---|---|---|
| F1 | `[s2-t1-followup]` Operator web UI replay route (`POST /runs/{run_id}/replay` + progress page) — parent §3.5 owns the spec | Self-host brief §4 defers W7 web UI to Phase X | External customer ask OR Phase X re-opens |
| F2 | `[s2-t1-followup]` Partial replay `--from=<node>` semantics + impl | `ReplayOpts.FromNodeID` reserved field; v1 returns `ErrUnsupported` | Operator request signal OR Phase X wave begins |
| F3 | `[s2-t1-followup]` Edit-and-replay `--pin-model=<model>` (PinSet impl) | `ReplayOpts.PinOverride` reserved field; v1 returns `ErrUnsupported` | Phase X wave (after F2) |
| F4 | `[s2-t1-followup]` Cross-tenant replay UX (admin-scope Principal) | v1 single-tenant; defence-in-depth `opts.TenantID` check stays | W8 multi-tenant cutover (Phase X) |
| F5 | `[s2-t1-followup]` Temporal-backed `DurableHistory` impl | Self-host brief §4 defers; parent §3.6 P2.5 trigger gates re-entry | Any §3.6 condition holds two consecutive 24-h windows OR external customer ask |
| F6 | `[s2-t1-followup]` `jsondiff` library escape hatch for nested-array divergence | In-repo JSON-path traversal ships first; library swap only if ≥3 under-covered keys reported in production | Trigger threshold met (F7 of §8) |
| F7 | `[s2-t1-followup]` 1M-event load test in CI (parent A+3) | Build-tag `load`; not pre-merge | A+ polish wave |
| F8 | `[s2-t1-followup]` Replay-result attestation in W10 chain | W10 consumes `DiffResult` to sign "replay verified" attestations | MVP-4 W10 |

---

_Spec authority: `feedback_spec_pattern_authority` — implementer subagent deviation requires re-spawning the design subagent. The 8 follow-up issues in §11 MUST be filed and cited in the T1 PR body per `feedback_unaddressed_load_bearing` + `feedback_review_before_automerge`._

## Resolution (2026-06-02)

Shipped via #350 (`feat(history): W9 substrate DurableHistory — interface + Append slice`). Substrate-default impl lives at `internal/history/`; Temporal-backed variant remains Phase X per `briefs/2026-06-01-self-host-first.md` §4.
