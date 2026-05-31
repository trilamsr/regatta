# Wedge: outcome-conditional DAG (CEL-predicated edges)

Prospective. Not on the milestone path. See
[`README.md`](./README.md).

## Thesis

The planner today joins feature nodes with a plain string list
(`DependsOnFeatures []string` in `internal/program/planner.go`) --
pure structural dependency, no runtime predicate. Real triage and
remediation flows need edges that activate on prior-node outputs:
"run remediation B only if scan A reports `severity == high`."
That's the gap between static DAG schedulers and full BPMN-style
engines. [Datadog's Bits AI
SRE](https://www.datadoghq.com/blog/building-bits-ai-sre/) is the
canonical real-world triage tree this wedge serves.

Maps to **Trap Catalog P1** (deterministic gate before AI gate,
load-bearing). Predicate evaluation is CEL -- deterministic, no
LLM re-call. The contrast is
[Magentic-One](https://www.microsoft.com/en-us/research/articles/magentic-one-a-generalist-multi-agent-system-for-solving-complex-tasks/),
where the orchestrator *is* an LLM that re-plans on every step;
regatta refuses that route by design.

## Prior art

| System | Condition language | Skip / cancel semantics |
|---|---|---|
| [Airflow `BranchPython` / `ShortCircuit`](https://airflow.apache.org/docs/apache-airflow/stable/core-concepts/dag-run.html) | Arbitrary Python; returns `task_id`(s) | Non-chosen siblings marked `skipped`; cascades via `trigger_rule`. `none_failed_min_one_success` preserves joins |
| [Argo Workflows `when:`](https://argo-workflows.readthedocs.io/en/latest/walk-through/conditionals/) | `govaluate` expression DSL over `{{steps.X.outputs.result}}` | Node marked `Omitted`; downstream `dependencies` treat omitted as satisfied |
| [AWS Step Functions `Choice`](https://docs.aws.amazon.com/step-functions/latest/dg/state-choice.html) | JSONPath + typed comparators (or JSONata) with `Default` branch | No skip — Choice transitions to exactly one `Next`; missing default is a runtime error |
| [Temporal child workflows](https://docs.temporal.io/develop/python/child-workflows) | Imperative Go / Python / Java with deterministic replay | No DAG to skip — control flow is code; non-spawned children simply never started |
| [Prefect 3](https://docs.prefect.io/v3/develop/control-flow) | Python `if` / `return` inside `@flow` | Returning early skips downstream; `state.is_skipped()` |
| [Dagster `DynamicOut`](https://docs.dagster.io/concepts/ops-jobs-graphs/dynamic-graphs) | Runtime mapping-key fan-out | Non-yielded keys never instantiated |
| [Snakemake `checkpoint`](https://snakemake.readthedocs.io/en/stable/snakefiles/rules.html#data-dependent-conditional-execution) | Python input-function reads checkpoint outputs and **re-evaluates** the DAG | DAG literally rebuilt post-checkpoint |
| [Nextflow `branch`](https://www.nextflow.io/docs/latest/operator.html#branch) | Groovy closure with labelled boolean clauses; `true:` as default | Items routed to one channel; others empty |
| [BPMN 2.0 gateways](https://www.omg.org/spec/BPMN/2.0/) | Expression on sequence flow (XOR / OR / AND); default flow required for XOR / OR | Token model — un-taken paths never get a token |
| [LangGraph `add_conditional_edges`](https://docs.langchain.com/oss/python/langgraph/graph-api) | Python router function `(state) -> next_node \| List[node]` | Non-returned successors not invoked; END is a legal target |
| [Tekton CelRun custom task](https://github.com/tektoncd/pipeline/issues/3149) (see [opensource.com walk-through](https://opensource.com/article/22/7/conditional-tekton-pipelines-cel)) | CEL expression as a pipeline `when:` clause | Skips the task; matches Tekton's standard skip semantics |
| [Argo CEL alternative discussion](https://github.com/argoproj/argo-workflows/issues/7824) | Community debate of CEL vs `expr` for `when:` | Same as Argo's existing `when:` -- Omitted |
| [Inngest AgentKit deterministic routing](https://agentkit.inngest.com/) | TypeScript code-based router as a first-class primitive | Returned successors run; others skipped |

## Patterns worth stealing

1. **Typed comparator DSL over raw code** (Step Functions). The
   `NumericEquals` / `StringEquals` / `IsPresent` / `IsNull`
   family is auditable, language-neutral, and diffs cleanly in
   PRs. Beats embedding Python.
2. **Explicit default branch is mandatory** (Step Functions,
   BPMN). Step Functions throws when no Choice matches without a
   `Default`; BPMN requires a default sequence flow on XOR / OR
   gateways. Avoids deadlock.
3. **`IsPresent` guard before comparison** (Step Functions). The
   documented idiom for missing-field safety:
   `And: [{IsPresent: true}, {StringEquals: "x"}]`. Solves the
   "condition references missing field" failure mode at the
   language level.
4. **Trigger-rule on the join node** (Airflow). Airflow's
   `none_failed_min_one_success` is the canonical fix for
   "diamond with one conditional arm": the join doesn't deadlock
   when one parent is skipped.
5. **Checkpoint plus re-plan** (Snakemake). The cleanest mental
   model for "the DAG itself depends on data": commit,
   re-evaluate, expand. Maps directly onto regatta's planner
   re-invocation.
6. **Deterministic replay boundary** (Temporal). Isolate
   non-determinism in activities; the branching code stays
   replay-safe. Lesson: **the decision reads a snapshotted value,
   never a fresh LLM call.**
7. **Labelled-branch closure** (Nextflow). `branch { small: v<10;
   large: v>10; other: true }` is the most readable conditional
   fan-out syntax in the field.
8. **[CEL](https://github.com/google/cel-spec/blob/master/doc/langdef.md)
   as the expression layer.** Google's Common Expression
   Language is memory-safe, side-effect-free, **terminating**,
   strongly typed, and gradually typed. Every property matters
   for a workflow predicate: edge guards cannot loop, mutate, or
   crash the scheduler.

## Failure modes

- **Missing field reference.** Argo `govaluate` throws at eval;
  Step Functions requires explicit `IsPresent`. Mitigation:
  type-check predicates against the upstream node's output
  schema at plan-validation time, not at runtime.
- **All branches skip -> deadlock.** Step Functions errors loudly;
  BPMN's required default flow prevents the case structurally
  (the normative clause lives in the members-only OMG PDF; see
  [Camunda's BPMN
  reference](https://docs.camunda.io/docs/components/modeler/bpmn/gateways/)
  for the public statement). Airflow's `all_success`
  cascade-skips silently -- famous footgun.
- **Races on N→1 conditional joins.** When several parallel
  parents each carry a predicate, the join's effective fire-rule
  depends on which parents resolved truthy. Make `trigger_rule`
  explicit per join.
- **Predicate references future field.** Snakemake forbids by
  construction (input-function re-eval); static DAGs with
  forward references silently never fire.

## Proposed minimal extension

Substrate note: the run journal proposed below is **superseded**
by the [unified substrate](./unified-substrate.md). Output
snapshots are `events WHERE kind='node_output'`. The CEL
evaluator becomes a `Decider` impl (`CELDecider`) feeding the
existing `RouteVerdicts`. The `Edge` struct below stays as-is --
it is the planner-side schema, not a storage table.

The current edge is implicit in `DependsOnFeatures []string`.
Make edges first-class with an optional predicate:

```go
type Edge struct {
    From      FeatureID `json:"from"`
    To        FeatureID `json:"to"`
    Predicate string    `json:"predicate,omitempty"` // CEL; empty = unconditional
    OnSkip    SkipMode  `json:"on_skip,omitempty"`   // "cascade" | "ignore" | "default"
}

type Feature struct {
    // ...existing fields...
    Edges       []Edge      `json:"edges,omitempty"`
    DefaultNext *FeatureID  `json:"default_next,omitempty"` // required iff any outgoing edge has a predicate
}
```

**Predicate language: CEL.** Not Python, not JSONPath, not a
bespoke DSL. CEL gives strong typing, termination guarantees, and
ready Go bindings (`cel-go`). Predicates read from a typed
`outputs.<from_node>.<field>` namespace whose schema is the
upstream node's declared output contract. Plan-time validation
rejects predicates that reference fields outside that contract,
killing the "missing field" failure mode before execution.

**Skip semantics:** borrow Airflow's join rules but make them
explicit per edge. `OnSkip = "cascade"` is the safe default;
`"ignore"` mirrors `none_failed_min_one_success`.

**Mandatory `default_next`** on any node with predicated outgoing
edges — borrowed verbatim from Step Functions and BPMN. The plan
validator rejects DAGs that can deadlock.

## Determinism with LLM outputs

CEL is deterministic, but the **inputs** are not. The fix is
Temporal's: snapshot the model output as the workflow's
authoritative value, then evaluate the predicate against the
snapshot. Concretely:

1. When a node completes, persist its output JSON into the run
   journal under a content-addressed key.
2. Edge evaluation reads from the journal, never from a live
   LLM re-call.
3. Re-runs of the planner replay the journal — same inputs,
   same predicate, same edge truth values.
4. Retries on the producing node create a new journal entry; the
   edge re-evaluates against the new snapshot. This matches
   Snakemake's checkpoint-then-re-evaluate.

Audit trail entry: *"edge B→C fired because
`outputs.scan.severity == 'high'` evaluated against journal
entry `sha256:…`."*

## Visualisation once 30% of edges are conditional

Adopt BPMN's glyph vocabulary without adopting BPMN's full
notation:

- Solid edge: unconditional dependency.
- Dashed edge with predicate label: conditional (LangGraph's
  Mermaid renderer does this).
- Diamond junction when one source has >=2 predicated outgoing
  edges (BPMN XOR).
- Dimmed grey for edges whose predicate evaluated false in a
  completed run (Argo "omitted" style).
- Hover-expand for predicate text -- never inline; CEL bloats the
  layout.

UX implication: viewers need a mode toggle between "planned graph"
(all edges visible, dashed where conditional) and "executed graph"
(only fired edges solid, others greyed). Same toggle Airflow's
Graph and Tree views give.

## Tradeoff: full DAG plus predicates vs. base DAG plus
planner-regenerated suffix

| Axis | Full DAG with predicates | Base DAG + re-plan suffix |
|---|---|---|
| Plan auditability | High — entire shape visible up front | Lower — suffix unknown until midstream |
| Predicate complexity | Bounded by CEL grammar | Unbounded (planner LLM regenerates) |
| Determinism / replay | Strong (snapshot + CEL) | Weak (LLM re-plan non-deterministic) |
| Failure surface | Missing-field, deadlock, race on join | Hallucinated next step, plan drift |
| Cost | One LLM call per node + cheap CEL evals | LLM call per node + LLM call per re-plan |
| Fit for triage | Excellent — small branch factor | Better for open-ended remediation |

Recommendation: **start with predicated edges (full DAG)**. The
project's grade rubric favors auditability and tool-checkable
plan validation. Reserve checkpoint-style re-planning for the
narrow case where the next-step set is itself unbounded ("scan
finds N issues, spawn N remediation features"). The two compose;
Snakemake supports both.

## Regatta extension points

- Extend `program_brief.json:child[].depends_on_features` from a
  list to a conditional DAG `{depends: [], gates: {gate_id:
  result_required}}`.
- `RouteVerdicts()` unpacks gate verdicts, evaluates conditionals,
  spawns or halts accordingly. Stays deterministic — load-bearing
  Trap Catalog P1 (no LLM routing).
- New run-journal table for output snapshots
  (`run_journal{run_id, node_id, output_json, digest,
  written_at}`).

## Trigger metric (when to adopt)

- First customer running a triage-shaped workflow (scan →
  remediate-if).
- OR ≥1 documented case of planner regenerating a different DAG
  on identical inputs in a way that the operator could not
  audit.

## Grade rubric

| Tier | Criterion |
|---|---|
| **B** | `Edge.Predicate` field implemented; CEL evaluation against in-memory output; plan-time syntax check; required `default_next` enforced. |
| **A** | All B + run journal persists output snapshots; predicate re-evaluates from journal on replay; field-presence validation against upstream output schema. |
| **A+** | All A + Mermaid visualisation with planned-vs-executed toggle + zero deadlock-able DAG admitted by validator (property-tested with a deadlock corpus). |

## References to existing repo state

- `internal/program/planner.go` — current `DependsOnFeatures`
  edge model the proposed `Edge` struct replaces.
- `contracts/schemas/program_brief.schema.json` — schema to
  extend.
- `docs/design.md` §Programs — `RouteVerdicts` already
  enforces deterministic routing over signed gate results;
  predicated edges are the same idea pushed into the
  inter-feature layer.
- `docs/incidents.md` P1.
