# 0002. MVP-2 outcome-conditional DAG: CEL-predicated edges over an append-only outputs journal

Status: accepted
Date: 2026-05-31
Author: Tri Lam <tri@maydow.com>

## Context

MVP-1 shipped a fully static DAG: every dependency is structural
("complete before spawn"), expressed as a `depends_on_features`
array on each child feature in `program_brief.json`. Real triage /
remediation work needs the next axiom -- edges that fire based on
what an upstream node produced ("run the deep-scan only if the
cheap-scan said `severity=high`"). Without conditional edges, the
planner either has to predict the answer (impossible) or both
downstream agents always spawn (waste).

The decision triggers from spec D5 fire: a schema-version bump
(`ProgramBrief.schema_version=2`), new internal contract surfaces
(`Edge`, `OutputsSchema`, journal API), and a new persistence
shape (two relational tables). Trap P1 is load-bearing:
routing predicates must be deterministic, never LLM-routed --
determinism, auditability, and replay all derive from this.

## Decision

Add outcome-conditional edges to MVP-2 W1 as a backward-compatible
extension of the universal queue:

1. **First-class `Edge` objects** with optional CEL predicates
   over upstream output JSON. Empty predicate = unconditional
   (back-compat lowering of `depends_on_features`).
2. **CEL via `github.com/google/cel-go`** as the predicate
   language -- terminating, sandboxed, gradually typed against
   declared output schemas. No LLM ever evaluates an edge.
3. **`ProgramBrief.schema_version=2`** with `Edges[]`,
   `DefaultNext`, and `OutputsSchema` per feature. v1 briefs
   continue to load read-only; the loader sniffs the version and
   mechanically lowers v1 deps to unconditional edges.
4. **Mandatory `default_next`** on any node with ≥1 outgoing
   predicated edge. The plan validator rejects any conditional
   fan-out missing a default at `BriefLoader.LoadAndVerifyBrief`
   time, not at execution time.
5. **Append-only outputs journal** in `work_item_outputs`
   (content-addressed by `sha256(canonical JSON)`, one row per
   `(work_item_id, attempt_no)`). Predicate re-evaluation reads
   the journal, never the LLM. The spawner's `AppendOutput` and
   the `status=merged` transition execute in a single sqlite
   transaction -- hard contract, not best-effort.
6. **First-class `work_item_edges` table** (not a JSON column),
   indexed on `(from_id, fired)` and `(to_id, fired)`. Edges
   carry durable `fired ∈ {pending, true, false, skipped}`.
7. **Plan validator runs at ingest time** (not at scheduler tick).
   Predicate compilation errors, unknown-field references,
   type mismatches, missing defaults, unknown edge targets, and
   cycles on the edge union all reject the brief loudly.
8. **Kahn-style cycle check at plan-validate**, applied to the
   union of unconditional + predicated edges. Predicates do not
   break cycle detection -- a conditional cycle is still a cycle.
9. **Join semantics (locked decisions 16-18 in spec):**
   - **OR-join (decision 16):** a node spawns the moment ≥1
     inbound edge fires true; pending siblings do not block.
     AND-join is deferred to MVP-3.
   - **Independent fan-out (decision 17):** every true predicate
     activates its target; mutual exclusion is the operator's
     responsibility, documented in operator guide.
   - **One-way import (decision 18):** `internal/program` may
     import `internal/orchestrator/state`; the reverse is
     forbidden. Eliminates cycle risk between the program-side
     evaluator and state-side row types.

Alternatives considered + rejected:

- **JSONPath or bespoke DSL:** loses cel-go's
  termination/sandboxing/type-checking and forces us to maintain
  a parser. Rejected.
- **JSON column for edges on `work_items`:** the scheduler
  queries `(from_id, fired)` every tick; a JSON column forces
  O(rows) scans where a relational index is O(1). Rejected.
- **Mid-run edge re-evaluation:** allowing predicates to re-fire
  if the upstream is retried makes replay non-deterministic.
  Journal is append-only; the scheduler only evaluates edges
  for nodes not yet spawned. Rejected.
- **AND-join in MVP-2:** OR-join matches Step Functions / BPMN
  exclusive-merge default and minimises triage latency; AND-join
  modelable as a sentinel feature. Deferred to MVP-3.

## Consequences

- (+) Static-DAG operators (v1 briefs) see zero behaviour change.
  The scheduler's `NOT EXISTS (SELECT 1 FROM work_item_edges
  WHERE to_id = w.id)` clause is the fast path for v1 work items.
- (+) Determinism property holds: edge re-evaluation against the
  journaled output JSON produces identical `fired` tables across
  replays. `regatta program replay <program_id>` (CLI deferred)
  is a pure-function check over the data model.
- (+) Operators read "what fired?" with plain SQL on
  `work_item_edges`. Slog events
  (`edge.fired`, `edge.skipped`, `edge.default_fallback`,
  `edge.cascade_skip`, `brief.rejected_predicate`) cover every
  transition.
- (-) Two new tables (`work_item_edges`, `work_item_outputs`),
  one new migration (`0003`), one new dependency (cel-go pinned
  in `go.mod`). cel-go API drift across minor versions is risk;
  golden corpus test guards CI.
- (-) Outputs journal grows append-only across retries. A vacuum
  CLI (`regatta gc --before=<ts>`) is a follow-up; per-program
  100 MB disk warning is the MVP backstop.
- (-) Operator confusion when an edge doesn't fire is the largest
  UX risk. Slog edge events include the journal SHA, predicate
  text, and a truncated `out` map; a `regatta program edges
  <program_id>` CLI is tracked as a follow-up.
- Activation triggers for follow-up RFCs: AND-join semantics
  (MVP-3 blackboard wedge), `regatta program replay` CLI surface,
  vacuum CLI for the journal.

## Compliance

- **Schema:** migration
  `internal/orchestrator/state/migrations/0003_work_item_edges_and_outputs.sql`
  defines `work_item_outputs` (content-addressed, `UNIQUE(work_item_id,
  attempt_no)` only -- no `UNIQUE(content_sha)` so byte-identical
  payloads across different work items do not collide) and
  `work_item_edges` (`UNIQUE(program_id, from_id, to_id)`,
  `fired ∈ {pending, true, false, skipped}`). `TestMigrate_V2ToV3`
  in `internal/orchestrator/state/` asserts clean apply.
- **Edge evaluator:** `internal/program/edge_evaluator.go` owns
  the cel-go env + compiled-program cache, keyed by
  `(programID, edgeID)`. The package may import
  `internal/orchestrator/state` (one-way, per decision 18); the
  reverse import is forbidden and enforced by a grep gate in
  `make ci-check`. Evaluator imports MUST NOT include any
  model-client dependency -- trap P1 is verifiable by inspection.
- **Plan validation:** `internal/program/planner_v2.go`
  `ValidateV2` rejects every failure mode listed in
  `wedge_conditional_dag.md`. Each rejection returns a typed
  sentinel from `internal/orchestrator/errors.go`
  (`ErrPredicateCompile`, `ErrPredicateUnknownField`,
  `ErrPredicateTypeMismatch`, `ErrEdgeMissingDefault`,
  `ErrEdgeUnknownTarget`; cycles continue to use the existing
  `ErrFeatureCycle`). `TestValidateV2_RejectsAll` enumerates
  every sentinel as a named sub-test, each asserting
  `errors.Is(err, Err...)`.
- **Determinism:** `TestEdgeEval_Deterministic` in
  `internal/program/` runs 50 random v2 briefs (n≤6 features,
  m≤10 edges) replayed 3× against a fixed journal; `fired`
  tables must be identical across runs. `TestCanon_RoundTrip`
  in the same package pins canonical JSON byte-equality across
  key reorder + whitespace + unicode NFC.
- **Spawner atomicity:** `internal/orchestrator/spawner/spawner.go`
  `AppendOutput` and `status=merged` execute in one sqlite
  transaction. `TestSpawnerComplete_AtomicJournalAndMerge`
  asserts no observable window where the work item is merged but
  the journal entry is missing.
- **Scheduler:** the extended `ListSpawnable` query in
  `internal/orchestrator/scheduler/scheduler.go` handles three
  shapes (no inbound edges, ≥1 inbound `fired=true`, all inbound
  resolved with ≥1 `on_skip=ignore`). `TestListSpawnable`
  property tests cover the truth table from spec §3.7.
- **Back-compat:** `TestBriefLoader_V1Compat` in
  `internal/program/` loads every v1 brief in `testdata/`
  unmodified. `TestV2_LowerThenRoundTrip` lowers a v1 brief
  through the v2 path and asserts behaviour-equivalent
  `ListSpawnable` output across 200 ticks. CI fails on drift.
- **Per-package coverage targets:** `internal/program` ≥ 85%,
  `internal/orchestrator/state` (new files only) ≥ 90%, enforced
  in `make ci-check`.

---

Numbering is monotonic; do not reuse a number. Once Status is
"accepted", do not edit; supersede with a new RFC that links back
via the "superseded by RFC-NNNN" status line.
