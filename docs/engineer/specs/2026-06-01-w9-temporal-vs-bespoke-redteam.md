---
title: "W9 replay+diff harness — Temporal vs. bespoke red-team"
status: active
summary: "W9 replay+diff harness, option C (hybrid): `DurableHistory` Go interface, substrate-default impl, Temporal-backed impl behind refined P2.5 trigger. Ships AFTER W6/W7/W8 land."
---

# W9 Replay+Diff Harness — Temporal vs. Bespoke Red-Team

**Status**: Red-team analysis answering the open question from
`docs/superpowers/briefs/2026-05-31-mvp-3-next-level.md` §6 #5
(Temporal/Inngest could absorb the bottom — should W9 ship on
Temporal day-1?).

**Author**: red-team subagent, 2026-06-01.

**Picked option**: **C — hybrid, substrate-default with a
`DurableHistory` adapter seam that admits a Temporal backend
when the P2.5 trigger fires.**

---

## 0. Reading guide

§1 enumerates the four options. §2 scores each on nine axes. §3
defends the trigger metric — when does bespoke become
unacceptable? §4 sketches the `DurableHistory` interface. §5
contrasts Temporal's versioning patches vs. bespoke
model-pin+seed+prompt-hash. §6 enumerates failure modes per
option. §7 computes cost-of-being-wrong both directions. §8 is
the recommendation with prior-art citations. §9 is the
sequencing block diagram. §10 lists five follow-up questions for
the next-wave reviewer.

---

## 1. Decision options

### Option A — Bespoke on the substrate `events` log

Build W9 entirely on the unified-substrate `events` table
proposed in `docs/wedges/unified-substrate.md`. Replay walks
`events WHERE run_id=X ORDER BY written_at`, re-executes each
node from its journaled inputs, diffs against the original
`node_output` rows. The existing `work_item_outputs` table
(migration `0003_work_item_edges_and_outputs.sql`) is already
the durable history for MVP-2's conditional DAG; W9 promotes
that journal into an addressable replay log.

### Option B — Temporal-only

Adopt `go.temporal.io/sdk` as the durable-execution substrate.
Every regatta DAG run becomes a Temporal Workflow; every
work-item invocation becomes an Activity. `tctl workflow show`
+ `worker.WorkflowReplayer` provide the replay; semantic-diff
sits on top of `client.GetWorkflowHistory`. Migrate
`work_item_outputs` → Temporal Event History; deprecate
`scheduler.Tick()`.

### Option C — Hybrid, substrate-default + Temporal-backend adapter (**RECOMMENDED**)

Ship W9 against a `DurableHistory` Go interface. Default
implementation reads/writes the substrate `events` table.
Second implementation (gated behind the P2.5 trigger from
`design.md` row 804: ≥30 concurrent programs OR sqlite
contention >5%) wraps `temporal.Client.GetWorkflowHistory` +
`worker.WorkflowReplayer`. Operators stay on sqlite-bespoke
until they hit the pain; then they flip a config flag and the
same `regatta replay <run_id>` CLI works against Temporal Cloud
or self-hosted Temporal Server. Semantic-diff layer is shared.

### Option D — Defer W9 to MVP-4

Skip W9 entirely until the substrate (W6 OTel + W11
blackboard) is mature. Justification: the `events` log shape
W9 depends on is being load-bearingly redesigned right now
(W6 spec freezes 2026-06-01; substrate dossier postdates the
brief by hours). Building W9 on a moving table is wasted work.

---

## 2. Scoring matrix

Build-cost LOC is "first ship, gates clean, with adversarial
review passes." All numbers are red-team estimates, not
commitments — the implementer subagent will overshoot 20-30%.

| Axis | A (bespoke / substrate) | B (Temporal-only) | C (hybrid + adapter) | D (defer) |
|---|---|---|---|---|
| **Build-cost LOC** | ~1.8k (replay engine 600 + pin loader 250 + semantic differ 500 + CLI 200 + tests 250) | ~3.5k (Temporal SDK bootstrap 400 + workflow/activity wrappers 900 + history-fetch wrapper 200 + semantic differ 500 + worker bootstrap 600 + ops runbook 200 + migration 700) | ~2.4k (A baseline + `DurableHistory` interface + 2 impls 300 + adapter tests 300) | ~0 now, ~2.4k later (= C, deferred) |
| **Operator UX** | `regatta replay <run_id> --from=<node>`; single binary; sqlite file. **Best UX for pilots.** | Operator runs Temporal Server (3 containers: history, matching, frontend) + a Postgres OR Cassandra; or pays Temporal Cloud ($25-100/mo entry + per-action billing). Plus a worker process. **5× ops footprint for MVP pilots.** | Default = A's UX. Opt-in Temporal flips on for scale-out tenants. **Same UX as A at MVP; B's ceiling at scale.** | UX-neutral now (no replay capability); 6-month gap where ops can't reproduce failures — **regression: today's MVP-2 spec promised W9 in MVP-3 to close pilot debugging gap.** |
| **Multi-host failover** | None. Single sqlite writer; new orchestrator host loses in-flight runs unless rebuilt from journal. SPOF (design.md §2 gap inventory). | First-class. Temporal History service handles failover, task-queue polling across workers, sticky-on-host failover. The reason Temporal exists. | Failover gated behind the Temporal flip. Until the flip, same as A. **Honest:** the adapter doesn't conjure failover — it's the trigger to migrate, not a polyfill. | Same SPOF as A; nothing changes. |
| **Model-pin support** | Bespoke `pin_model`/`pin_seed`/`pin_prompt_sha` columns in journal entry — explicit, regatta-owned, evolves with model-versioning policy. **Native fit for AI workflows.** | Temporal has no concept of "model version." We stuff into `Memo` or `Search Attributes`; activities re-read on replay. Works but the schema is upside down — Temporal's invariant is "activity output is replayed from history," but **we want the model call re-executed under different pins**. We have to write activity code that reads the pin from history-as-input. Doable, awkward. | A's native pin support, Temporal-backend re-implements pins via custom `Memo` on the workflow. Adapter contract requires both impls expose `PinSet(run_id)` and `PinsAt(run_id, node_id)`. | N/A. |
| **Semantic-diff difficulty** | Easy. Both sides are JSON rows from the same `events` table; diff library walks acceptance-criteria deltas. Already factored in `internal/canon` canonicalization (canonical JSON → byte-equal SHAs). | Medium. Diff needs to extract activity outputs from `history.History` protos, decode the `Payloads` (DataConverter unwrap), re-canonicalize. One extra encoding layer per diff call. | A's path for default; B's path when Temporal-backed. Diff library accepts an iterator over (`node_id`, `OutputJSON`, `ContentSHA`); both backends produce that iterator. | N/A. |
| **Lock-in risk** | Low. The journal schema is regatta-owned; the only "vendor" is sqlite/Postgres. | High. Temporal's workflow code is non-portable: `workflow.GetVersion`, `workflow.ExecuteActivity`, `workflow.Sleep`, `Signal`/`Update` semantics. Migrating off Temporal = rewrite every workflow. | Low. The `DurableHistory` interface is the seam; Temporal lives behind it. Worst case we delete the Temporal impl and stay on substrate. **Lock-in is bounded to one adapter file.** | N/A (no commitment). |
| **Swap-out adapter feasibility** | N/A — no adapter, no swap. | N/A — Temporal IS the substrate. | **Designed for swap.** Five-method interface, default + Temporal impl ship together; Restate impl or Inngest impl can be added later without touching W9 callers. | N/A. |
| **Replay-determinism semantics** | Pin-based: `(model_id, seed, prompt_sha, input_sha) → ContentSHA` is the contract. Re-run with same pins = same output (modulo provider non-determinism, which we accept as a known limit). Operator changes the pin (`--pin-model claude-4.7`) to drive the diff. | History-based: Temporal replays from `Event History` events. **Activities return cached results on replay.** To re-run with a different model, you START A NEW WORKFLOW with a new model param; you don't "replay the same workflow with a different pin." Temporal's replay is for crash-recovery determinism, not "what would a new model say?" diff. | A's semantics in both backends. Temporal backend uses `Reset` + new run, not in-place replay. | N/A. |
| **Failure-mode count** | 4 (see §6.A) | 9 (see §6.B) | 5 (= A's 4 + adapter-drift 1) | 1 (forensic gap) |

**Reading**: B's only categorical win is multi-host failover.
A wins or ties on the other eight. C costs +600 LOC over A and
buys an exit ramp for the one axis where A loses. D forfeits
the pilot debugging story we already promised.

---

## 3. Trigger metric — when does sqlite-bespoke become unacceptable?

`design.md` row 804 already defines P2.5 (Temporal adoption):

> **P2.5 (Temporal)** — Sustain ≥30 concurrent programs. Use
> Temporal workflow history as the second tamper-evident
> timeline alongside the audit sink. 3-day human approval
> pauses survive across restart. **Cannot:** Run without
> Postgres.

We refine the trigger into three measurable conditions, **any
one of which** crosses the line:

1. **Sqlite write contention >5%** of scheduler ticks blocked
   on `database is locked` over a 24-h window. (Measured via
   `pragma_busy_timeout` counter + `tick.completed`
   `duration_ms` p99 regression vs. baseline.)
2. **Concurrent programs ≥30** at steady state. (Measured via
   `work_items WHERE state='running' AND kind='program'` row
   count.)
3. **Replay-recovery time >60s** for a single run_id with
   ≥1000 journal entries — i.e., the bespoke replayer's linear
   scan starts to feel slow to operators.

When any condition holds for two consecutive 24-h windows, the
operator flips the `durable_history.backend: temporal` flag in
`regatta.yaml`. New runs land in Temporal; old runs continue
to replay from sqlite via the adapter (no destructive
migration — substrate stays read-accessible).

**Why three conditions, not just "30 concurrent"**: row 804's
threshold is concurrency-based, but contention manifests
earlier on busy single-tenant deployments (the cost-governor
reconciler + scheduler tick + audit appender all serialize on
the writer). The replay-recovery time is the UX-shaped trigger
that operators will notice before the metrics.

---

## 4. `DurableHistory` adapter sketch

Five methods, Go interface, lives at
`internal/durhist/durable_history.go`. Default impl wraps the
substrate `events` table; Temporal impl wraps
`client.NewClient` + `client.GetWorkflowHistory`.

```go
// Package durhist abstracts the durable-execution backend for
// W9 replay+diff. Default impl reads/writes the substrate
// events table; Temporal impl wraps client.GetWorkflowHistory
// + worker.WorkflowReplayer. Callers (replay CLI, diff engine)
// depend only on this interface — never on the concrete
// backend. The seam is one file so we can delete the Temporal
// impl without touching callers (lock-in budget = 1 file).
package durhist

import (
    "context"
    "io"
)

// HistoryEntry is the lowest-common-denominator shape across
// substrate events and Temporal history events. Both backends
// project into this; the diff/replay engines never see backend
// types. ContentSHA is canonical-JSON sha256 — same contract
// as state.OutputJournalEntry.
type HistoryEntry struct {
    NodeID     string
    AttemptNo  int
    OutputJSON []byte // canonical form
    ContentSHA string
    Pins       PinSet // model+seed+prompt_sha; opaque to backend
    ProducedAt int64  // unix ms
}

type PinSet struct {
    ModelID    string
    Seed       int64
    PromptSHA  string
    InputSHA   string
}

type DurableHistory interface {
    // Append records a node output. Idempotent on
    // (run_id, node_id, attempt_no). Returns the canonical
    // ContentSHA so callers can compare without re-hashing.
    Append(ctx context.Context, runID string, e HistoryEntry) (string, error)

    // Replay streams entries for run_id in journal order. The
    // returned closer releases backend resources (sqlite stmt
    // or Temporal history iterator). Caller drives semantic
    // diff over the stream — no whole-history materialization.
    Replay(ctx context.Context, runID string) (iter HistoryIterator, close io.Closer, err error)

    // Pin reads the pin set that produced the latest output
    // at node_id. Used by `regatta replay --from=<node>
    // --pin-model=...` to compute the override delta.
    Pin(ctx context.Context, runID, nodeID string) (PinSet, error)

    // Fork starts a new run that inherits the prefix of
    // runID up to nodeID (exclusive), then re-executes from
    // nodeID under newPins. Returns the new run_id.
    // Substrate impl: copy rows + new run_id. Temporal impl:
    // Reset workflow + StartNewExecution with override Memo.
    Fork(ctx context.Context, runID, nodeID string, newPins PinSet) (newRunID string, err error)

    // VerifyReplay re-executes runID against the journaled
    // pins, returns true if every re-derived ContentSHA
    // matches the journaled ContentSHA. Operator-facing
    // determinism check; powers `regatta replay --verify`.
    VerifyReplay(ctx context.Context, runID string) (ok bool, mismatches []NodeID, err error)
}

type HistoryIterator interface {
    Next(ctx context.Context) (HistoryEntry, bool, error)
}
```

Five methods, two backends, one diff engine. The CLI command
`regatta replay <run_id> --from=<node> --pin-model claude-4.7`
calls `Pin(run_id, node)`, mutates `Pins.ModelID`, calls
`Fork(...)`, then `VerifyReplay(new_run_id)` and finally pipes
both `Replay` streams into the semantic differ.

**Why these five and not more**: every method is operator-
observable (CLI surface ↔ method 1:1). `Append` powers the
producing side; `Replay` powers the read-back; `Pin` exposes
the override seam; `Fork` is the diff-driver; `VerifyReplay`
is the audit check. Anything else (`Cancel`, `Signal`,
`Update`) is a Temporal-shaped concept that doesn't belong in
the replay-harness contract — they live in the orchestrator,
not in durhist.

---

## 5. Replay-determinism comparison

### Temporal's workflow-versioning patches

Temporal's determinism contract is: **workflow code is replayed
from event history; activity results are memoized.** If you
change workflow code in a way that diverges from the recorded
history, the replayer panics with `NondeterministicError`. The
documented fix (Go SDK docs §"Patching with GetVersion"):

```go
v := workflow.GetVersion(ctx, "Step1", workflow.DefaultVersion, 1)
if v == workflow.DefaultVersion {
    err = workflow.ExecuteActivity(ctx, ActivityA, data).Get(ctx, &result1)
} else {
    err = workflow.ExecuteActivity(ctx, ActivityC, data).Get(ctx, &result1)
}
```

Each `GetVersion` call is a permanent branch in the code. Over
time the workflow accretes a forest of branches; the docs
recommend cleaning them up "gradually" but require that all
old executions exit retention before removal. For long-running
workflows that's months. Worker Versioning is the proposed
escape hatch (pin workers to deployment versions) but it
requires Temporal Server ≥ a recent version, breaks task-queue
routing semantics, and is still being stabilized as of 2026.

**The mismatch**: Temporal's versioning protects *code* across
runs. **Regatta's W9 requires versioning the *model* within a
re-execution of the same run.** Two different problems. Forcing
W9 into Temporal's versioning model means encoding the
model-pin in the workflow code as a `GetVersion` branch — which
explodes code-branch count linearly in model count.

### Bespoke pin-based replay (design.md P2.5 narrative)

The bespoke contract is:

```
ContentSHA = sha256(canonical_json(
    (model_id, seed, prompt_sha, input_sha) -> output_json
))
```

A replay with `--pin-model=claude-4.7 --pin-seed=42` produces a
NEW journal entry under the new pins, sharing a `run_id` but
distinguished by an `attempt_no` (already in
`work_item_outputs` migration 0003) AND a `pin_set_id` column
W9 adds. The differ compares journal entry `attempt_no=1`
(original pins) vs `attempt_no=2` (new pins) for the same
`node_id`. Code never branches per model — the code is the
LLM invocation, parameterized by pins. The model identity is
data, not control flow.

**The win**: pin-based replay is the right abstraction for AI
workflows specifically. Temporal optimized for the case where
workflow code evolves and activities are deterministic
modulo retries; W9 lives in the inverted case: code stays put,
the LLM is the non-deterministic input. Pin-based replay
matches the inversion.

---

## 6. Failure modes per option

### Option A (bespoke / substrate) — 4 failure modes

1. **Sqlite contention at scale**: write throttle at ~500 ops/s
   on single-writer; addressed by P2.5 → Postgres or the
   Temporal flip.
2. **Replay drift across `VACUUM`**: sqlite vacuum rewrites
   rowids but not content_sha; we already canonicalize, so
   ContentSHA is vacuum-stable. **Risk reduced to: vacuum
   during a replay locks the writer, replay hangs.** Mitigation
   already in `internal/canon`.
3. **Schema migration breaks old journals**: adding columns to
   `work_item_outputs` later requires a backfill. Mitigated by
   nullable-default-add pattern (consistent with the
   tenant_id discipline in brief §6 #7).
4. **Model-version-after-fact replay** ("what would Claude 4.8
   have said?" when 4.8 wasn't released at original run time):
   the journal stores `model_id`; replay with a future model
   is allowed and produces a new ContentSHA. **No failure mode
   here — by design.**

### Option B (Temporal-only) — 9 failure modes

1. **Temporal-server SPOF for single-tenant pilots**: the
   minimum production deployment is history + matching +
   frontend + Postgres or Cassandra. Five containers. Pilots
   running on a single VM can't ship.
2. **Event-History size limit (4 MB / transaction; cloud
   docs §"Event History transaction size limit")**: a single
   workflow accreting ~3-4 K events triggers
   `ContinueAsNew` requirements. W9's "show me the whole
   run history for diff" hits this hard for long-running
   programs.
3. **Per-workflow Update limit: 2000 total updates in
   history** (cloud docs §"Per Workflow Execution Update
   limits"). Regatta DAGs with >2000 work-items break.
4. **Payload size: 2 MB per single request**. LLM outputs >
   2 MB (long PR descriptions, generated specs) need
   external blob storage + payload codec. Adds CAS to the
   activity path even though we already have one.
5. **Version-mismatch deadlock**: `GetVersion` branches not
   maintained → in-flight workflows can't progress on
   redeployed workers. Documented Temporal pain.
6. **Pricing exit cost (Temporal Cloud)**: actions-per-second
   is the billed unit (cloud docs §"Actions per second");
   500 APS soft cap, auto-scales but billed per action.
   Burst-y regatta workloads (parallel-subagent fan-out)
   inflate APS unpredictably. Migration off Cloud = rehost
   the open-source server (Postgres + Cassandra ops).
7. **Replay semantics mismatch with model-pin diff**: see §5.
8. **Workflow definition is non-portable Go code**: rewriting
   `internal/orchestrator/scheduler` as Temporal workflows
   means every test changes shape. ~3 K LOC refactor.
9. **Operator-onboarding overhead**: every pilot ops
   engineer must learn `tctl` AND `regatta`. Doubles the
   learning curve.

### Option C (hybrid) — 5 failure modes

1-4 from Option A.
5. **Adapter drift**: substrate impl and Temporal impl evolve
   apart; semantic-diff produces different results under the
   two backends. Mitigation: shared conformance test suite
   that runs both impls against a fixture set on every PR.
   ~150 LOC of test infra.

### Option D (defer) — 1 failure mode

1. **Forensic gap**: no replay capability in MVP-3 ships
   breaks the pilot-debugging story brief §2 promised. Six
   months of unreproducible failures during the most
   sensitive phase (pilot adoption).

---

## 7. Cost of being wrong

### If we pick A or C and need to pivot to Temporal later

The `DurableHistory` interface (C) reduces this to: write the
Temporal impl behind the same five methods, dual-run for a
window, flip the flag, decommission the substrate impl per
run_id. **Estimated migration cost: ~600 LOC + one ops
runbook.** This is exactly the P2.5 trigger path design.md
already planned for. Pure-A makes this harder — caller code
has direct sqlite queries — but the substrate dossier already
pushes for the abstraction.

### If we pick B and Temporal Inc. shifts pricing

Self-host Temporal Server. Cost: 5-container Helm chart, one
Postgres or Cassandra, dedicated ops oncall. Burden falls on
every operator deploying regatta. **The exit is "everyone gets
a free Cassandra to operate"** — net loss vs. sqlite-bespoke.

### If we pick B and Temporal has a Sequoia-led pivot to enterprise-only

Two-year EOL on the OSS path is the documented industry risk
(Cockroach, HashiCorp, Elastic precedent). Regatta would be
locked to a fork or scramble to swap. Our differentiator
(operator-surface UX, AI-labor-specific gates) is unaffected,
but the substrate forces a re-platform mid-roadmap.

### If we pick D (defer) and a pilot churns over a debuggability gap

Lose the pilot, lose 6 months of MVP-3 momentum. The exit is
"build W9 anyway, late." Worst expected value.

**Bayesian read**: A's downside is bounded by C's adapter
investment. B's downside is unbounded (vendor pricing + ops
weight). C costs +600 LOC over A to buy the optionality. C
strictly dominates A by the optionality value. C dominates B
on UX-for-pilots. C dominates D on capability-now.

---

## 8. Recommendation (≤500 words)

**Adopt Option C: ship W9 on the substrate `events` log as the
default `DurableHistory` implementation, with a Temporal-
backed implementation gated behind P2.5 trigger
(`design.md` row 804).**

**Prior-art defense**:

- **Temporal docs §"Patching with GetVersion"** confirm
  Temporal's versioning was built for workflow-code evolution,
  not model-pin diffing. We don't need that abstraction; we
  do need pin-replay (§5). Adopting Temporal forces us to
  re-encode our problem in Temporal's shape — a category
  error.
- **Restate's journal model** (`docs.restate.dev/concepts/
  durable_execution`) validates that "replay from a journal,
  skip completed steps, resume from where we left off" is
  the right primitive — and is exactly what `work_item_outputs`
  already does in MVP-2. We are not building from scratch;
  we are extending a journal that already exists and is
  already content-addressed.
- **Inngest's memoization model**
  (`inngest.com/docs/learn/how-functions-are-executed`) shows
  the step-id+result hash pattern is industry-accepted —
  pin-based replay is the AI-labor specialization of the same
  shape.
- **`feedback_research_design_principles.md`** says "no
  proven equivalent exists for the exact shape" justifies
  bespoke; the shape here (model-pin replay over an
  acceptance-criteria differ) **has no proven equivalent** in
  Temporal/Inngest/Restate. They all assume activity outputs
  are replayed verbatim; W9 explicitly wants to *re-execute
  the LLM under different pins*.
- **`wedge_roadmap_assessment.md` Temporal absorption risk**:
  "Regatta sits one abstraction above Temporal." C honors
  that — we ship the higher abstraction (pin-replay +
  semantic-diff + CLI), and admit Temporal as a backend
  *under* our abstraction when scale demands it. We do not
  absorb into Temporal; we accept it as a swappable engine.
- **`design.md` P2.5** already names Temporal as the
  scale-out backend. C operationalizes that promise instead
  of either pre-committing (B) or deferring forever (D).

**Why not pure-A**: the adapter seam costs ~600 LOC up front
and removes the migration cliff at P2.5 trigger time. The
substrate dossier already requires this abstraction; W9 is
the natural place to land it.

**Why not B**: 5-container minimum deployment + 2 MB payload
cap + workflow-versioning-not-model-versioning + non-portable
workflow code add up to a 3.5 K LOC commitment with a vendor
exit cost that exceeds the bespoke cost. Operators don't want
a second control plane to learn during pilot adoption.

**Why not D**: forfeits the MVP-3 capability promise; pilot
debugging story regresses.

**Confidence**: high on direction (C), medium on exact
adapter shape (§4 is the proposed sketch — adversarial
reviewer should hunt edge cases on `Fork` semantics, see §10
question 2).

---

## 9. Sequencing — block diagram of dependencies

```
                      MVP-3 sequence (Wave order):
                      
   ┌──────────────────────────────────────────────────────────┐
   │ W6  OTel + GenAI semconv observability backbone          │
   │  └─ Locks the events table shape (substrate dossier      │
   │     §migration path step 3 — node_output events)         │
   └──────────────────────────────────────────────────────────┘
                              │
                              ▼
       ┌──────────────────────┴───────────────────────┐
       ▼                                              ▼
   ┌─────────────────────┐                  ┌──────────────────┐
   │ W7  operator web UI │                  │ W8  RBAC + tenant│
   │  (read-only DAG +   │                  │  (tenant_id col +│
   │   approvals + cost) │                  │   OPA policy)    │
   └─────────────────────┘                  └──────────────────┘
       │                                              │
       └────────────────────┬─────────────────────────┘
                            ▼
              ┌─────────────────────────────┐
              │ W9  replay + diff harness   │ ← THIS SPEC
              │  - DurableHistory iface     │
              │  - substrate-default impl   │
              │  - semantic differ          │
              │  - regatta replay CLI       │
              │  - Temporal impl: file-only │
              │    skeleton, P2.5 gated     │
              └─────────────────────────────┘
                            │
                            ▼ (MVP-4)
              ┌─────────────────────────────┐
              │ W10 in-toto / Sigstore      │
              │  (uses W9 ContentSHA chain) │
              └─────────────────────────────┘
```

**W9 ships AFTER W6/W7/W8** because:

1. **W6 locks the `events` shape** the substrate impl reads
   from. Building W9 first means building against a moving
   table — same wasted-work risk D names.
2. **W7's UI is the primary read surface** for diff output
   (semantic diff in side-by-side view). W7-first lets W9
   wire into the UI without a second pass.
3. **W8's tenant_id column** must be present in `events`
   before W9 caches anything tenant-scoped. Adding tenancy
   after-the-fact to a content-addressed log is destructive.

**The Temporal-backed impl ships as a stubbed-out skeleton
file** (`durhist_temporal.go` with method stubs returning
`ErrNotImplemented`) **at the same time as the default impl,
so the seam is exercised but the SDK dependency is build-
tag-gated**. Operators don't pay the Temporal SDK build
weight until they flip the trigger.

---

## 10. Open follow-up questions for next-wave reviewer

1. **Fork semantics**: when `Fork(run_id, node_id, newPins)`
   is called, do downstream-of-node_id edges re-fire under
   the new pins, or does the fork halt at node_id+1 and
   require an explicit `--from=<later_node> --continue`?
   Affects CLI UX and journal layout.
2. **PinSet immutability**: should `PinSet` include
   `agent_id` (which subagent ran the node)? Argument for:
   reproducing "Claude-instance-N said X" requires agent
   identity. Argument against: agent_id is non-deterministic
   process artifact; pinning it locks reproducibility to a
   specific worker host.
3. **Semantic-diff scope**: does the differ emit byte-level
   JSON diff, AST diff (per work_item.kind typed schema), or
   acceptance-criteria-delta only? Brief §4 W9 promised
   "acceptance-criteria delta"; this means each work_item
   kind needs a registered differ. Where does the registry
   live?
4. **Temporal-backend conformance suite**: what is the
   minimum fixture set the conformance suite must run
   against both backends before the Temporal flip can be
   declared production-ready? Suggest: 50 historical run_ids
   from at least one regulated pilot, replay all under both
   backends, ContentSHA must match for non-LLM nodes.
5. **`VerifyReplay` cost ceiling**: re-executing every node
   under journaled pins is O(N) LLM calls per replay-verify.
   For a 200-node program at $0.05/call ≈ $10/verify. Should
   `VerifyReplay` accept a `--budget-usd` cap and bail at
   threshold, or default to sampling (verify N random nodes
   + all gate-decision nodes)? Cost-governor wedge should
   own the cap definition.

---

## Appendix — citations

- Temporal Go SDK versioning + patching:
  `https://docs.temporal.io/develop/go/versioning`
- Temporal Cloud limits (4 MB event-history transaction,
  2000 update cap, 2 MB payload):
  `https://docs.temporal.io/cloud/limits`
- Temporal Go testing + WorkflowReplayer:
  `https://docs.temporal.io/develop/go/testing-suite`
- Temporal Event History encyclopedia entry:
  `https://docs.temporal.io/encyclopedia/event-history`
- Restate durable-execution journal model:
  `https://docs.restate.dev/concepts/durable_execution`
- Inngest step-memoization model:
  `https://www.inngest.com/docs/learn/how-functions-are-executed`
- Cloudflare Durable Objects (rejected as W9 substrate —
  single-region routing model is wrong shape for a
  multi-host regatta deployment):
  `https://developers.cloudflare.com/durable-objects/`
- Existing regatta journal:
  `internal/orchestrator/state/migrations/0003_work_item_edges_and_outputs.sql`
  + `internal/orchestrator/state/work_item_outputs.go`
- Substrate dossier `events`/`policies`/`blobs` shape:
  `docs/wedges/unified-substrate.md`
- design.md P2.5 trigger:
  `docs/design.md` row 804 (`P2.5 (Temporal)` —
  "Sustain ≥30 concurrent programs").
- Brief open question:
  `docs/superpowers/briefs/2026-05-31-mvp-3-next-level.md`
  §6 #5.
- Memory: `feedback_research_design_principles.md` bespoke
  heuristics; `wedge_roadmap_assessment.md` Temporal
  absorption risk.
