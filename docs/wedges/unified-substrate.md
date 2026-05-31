# Meta-dossier: unified substrate

Prospective. Not on the milestone path. Read this before reading
any individual wedge -- it changes how the others' data models
land.

## Thesis

Each of the five wedge dossiers proposes a data model in
isolation. Read together, four of them invent a variant of the
same append-only signed event log, four invent a variant of the
same scoped policy table, and two reinvent content-addressed
storage. Regatta already ships the primitive that subsumes them
(`RouteVerdicts` over signed `GateResult`s in `internal/program/`
per `docs/design.md` §Programs).

This dossier collapses the wedge tables into **three primitives,
one decider, one artifact** -- without losing a single feature.
The collapse drops projected build cost by roughly half and keeps
every defensibility claim from the per-wedge dossiers intact.

## Redundancy across the five wedges

| Wedge proposed | Shape | Subsumed by |
|---|---|---|
| `cost-governor`: extend `events` for token spend | append-only signed | events log |
| `approval-gates`: `approval_events` table | append-only signed | events log |
| `conditional-dag`: `run_journal{node_id, output_json, digest}` | append-only signed | events log |
| `blackboard`: `facts` table with `supersedes` | append-only signed | events log |
| `cost-governor`: `budget{scope_kind, scope_id, limit}` | hierarchical scoped policy | policies |
| `approval-gates`: `approvals{scope, reviewer_set, quorum}` | hierarchical scoped policy | policies |
| Existing: lane caps + hotspot locks (`regatta.yaml`) | hierarchical scoped policy | policies |
| `blackboard`: key ACLs + reducers | scoped policy | policies |
| `conditional-dag`: CEL predicate on edge | decide(snapshot) → route | decider |
| `approval-gates`: human decision | decide(snapshot) → route | decider |
| Existing: `RouteVerdicts` over signed gate results | decide(snapshot) → route | decider |
| `blackboard`: `blobs` content-addressed store | content-addressed payload | shared CAS |
| `conditional-dag`: implicit `output_json` >1KB | content-addressed payload | shared CAS |

## The three primitives

### `events` -- one append-only signed log

```
events(
    id              ulid PRIMARY KEY,
    run_id          text NOT NULL,
    work_item_id    text,
    kind            text NOT NULL,        -- enum below
    key             text,                  -- namespace for kind=fact
    payload_json    text,                  -- small inline payload
    blob_digest     text,                  -- → blobs CAS for large payload
    supersedes      text,                  -- prior event id; append-only chain
    written_by      text NOT NULL,
    written_at      integer NOT NULL,
    schema_version  integer NOT NULL,
    signature       text NOT NULL          -- HMAC over (id, kind, payload|digest, written_by)
)
INDEX events_kind ON events(run_id, kind, key, written_at DESC)
```

`kind` enum (extensible):

| `kind` | Producer | Reader | Replaces |
|---|---|---|---|
| `node_output` | work-item completion | conditional-dag decider; replay | `run_journal` |
| `fact` | subagent `fact.put(key, value)` | subagent `fact.get/list/semantic` | blackboard `facts` |
| `approval_event` | notifier callback | approval-gate state fold | `approval_events` |
| `token_spend` | LLM call wrapper | budget materialized view | extension of existing `events` |
| `budget_reconciled` | Anthropic Usage API cron | budget reconciliation | new |
| `gate_verdict` | decider impl | `RouteVerdicts` | existing `GateResult`, re-shaped |
| `lock_held` | scheduler hotspot acquisition | scheduler | existing locks table (de-normalised) |
| `heartbeat` | running agent | reaper | existing |

State for any wedge is `fold(events WHERE kind=X)`. Never a row
mutation. Audit, replay, and provenance fall out of the table
shape.

### `policies` -- one scoped policy table

```
policies(
    id                ulid PRIMARY KEY,
    scope_kind        text NOT NULL,        -- operator|tenant|dag|work_item|lane|key
    scope_id          text NOT NULL,
    kind              text NOT NULL,        -- enum below
    spec_json         text NOT NULL,        -- kind-specific schema
    precedence_rank   integer NOT NULL,     -- explicit, not join-order
    schema_version    integer NOT NULL,
    created_at        integer NOT NULL,
    revoked_at        integer
)
INDEX policies_scope ON policies(scope_kind, scope_id, kind) WHERE revoked_at IS NULL
```

`kind` enum:

| `kind` | spec_json shape | Replaces |
|---|---|---|
| `budget` | `{limit_usd, soft_pct, period, meter_pool}` | cost-governor `budget` |
| `approval` | `{reviewer_set, quorum, payload_schema, on_timeout, escalation_chain}` | approval-gates `approvals` (policy half) |
| `lane_cap` | `{max_concurrency}` | existing `regatta.yaml:lanes[]` |
| `hotspot_lock` | `{lease_seconds, max_age_seconds}` | existing locks config |
| `reducer` | `{strategy: "lww"\|"set-union"\|"append"\|"write-once"}` | blackboard `facts.reducer` column |
| `acl` | `{reads, writes, visibility}` | blackboard key ACL |
| `ttl` | `{kind_filter, ttl_seconds}` | per-wedge TTL configs |

Precedence is explicit (`precedence_rank`), never join-order. The
LiteLLM team-vs-user bug cannot recur because the schema names
the rule.

### `blobs` -- one content-addressed store

```
blobs(
    digest        text PRIMARY KEY,        -- sha256
    bytes         blob NOT NULL,
    size          integer NOT NULL,
    content_type  text
)
```

Any payload >1KB in `events` lives here, keyed by digest. Nix /
Bazel verifiability property is free.

## The one decider

`internal/program/route.go` already ships `RouteVerdicts` --
deterministic Go function over signed `GateResult`s. Generalise
its input through a `Decider` interface:

```go
type Decider interface {
    Kind() string                                            // "cel" | "human" | "verifier"
    Decide(ctx context.Context, snapshot Snapshot) (GateResult, error)
}

type Snapshot struct {
    RunID      string
    WorkItemID string
    Inputs     map[string]json.RawMessage  // from events WHERE kind=node_output
    Outputs    map[string]json.RawMessage
}
```

Three impls cover the wedges:

- `CELDecider` -- conditional-dag CEL predicate evaluation.
- `HumanDecider` -- approval-gates token callback resolves to a
  signed `GateResult` with `verdict ∈ {approve, reject,
  approve_with_edits}`.
- `VerifierDecider` -- LLM judge on a sandboxed task; load-bearing
  for the "verifier node" candidate in the README backlog.

All three feed the same `RouteVerdicts` downstream. Each emits a
`gate_verdict` event with HMAC signature; the replay path is one
codepath.

## The one artifact

Plan envelopes (per `plan-as-code.md`) carry WorkItem arrays plus
typed inputs. JSON is the canonical form (CUE-validated). YAML is
a human view rendered from / parsed to JSON. The runtime planner
emits the same JSON envelope to `.regatta/runs/<run_id>/plan.json`
that authored plans live in at `.regatta/plans/<name>.yaml`. One
schema, two writers, one executor.

## What does NOT collapse

- **Notifier abstraction** (Slack / email / webhook / CLI) --
  vendor-specific adapters; stays separate per `approval-gates.md`.
- **CEL evaluator** -- language runtime; `cel-go` import lives in
  `CELDecider`.
- **CUE plan validator** -- declarative-config runtime; lives in
  the plan loader.
- **Anthropic Usage API cron** -- network-bound; lives in the
  reconciler that produces `budget_reconciled` events.

These are the wedge-specific *handlers*. The unified substrate
removes the per-wedge *storage*.

## Migration path

The dossiers do not need to change their feature claims; they
need to change their "Proposed data model" sections to point
here. Concretely:

1. **`cost-governor.md`** -- delete the `budget` table; declare
   `policies WHERE kind='budget'`. Delete the bespoke materialised
   view; declare a view over `events WHERE kind='token_spend'`.
2. **`approval-gates.md`** -- delete `approvals` and
   `approval_events`. Declare `policies WHERE kind='approval'`
   for the gate policy, `events WHERE kind='approval_event'` for
   the log. State is still `fold(events)`.
3. **`conditional-dag.md`** -- delete `run_journal`. Output
   snapshots are `events WHERE kind='node_output'`. CEL evaluator
   becomes a `Decider` impl.
4. **`blackboard.md`** -- delete `facts` and `blobs`. Facts are
   `events WHERE kind='fact'`. Blobs share the substrate's
   `blobs` table. Reducers move into `policies WHERE kind='reducer'`.
   Key ACLs move into `policies WHERE kind='acl'`.
5. **`plan-as-code.md`** -- unchanged. The plan envelope is
   already the right shape.

The five wedge dossiers keep their prior-art surveys, failure
modes, security analyses, trigger metrics, and grade rubrics.
Only the data-model sections change.

## Cost of collapse

- **Schema validation per `kind`.** A per-`kind` validator runs at
  write time. Already needed in the per-table proposals; same
  total complexity, one dispatch point instead of five.
- **Single hot table.** `events` carries every kind. Partition by
  `(run_id, kind)`; per-kind TTL via `policies WHERE kind='ttl'`.
  Same write volume regardless of table count.
- **Looser SQL-layer typing.** `payload_json` varies by kind.
  Mitigation: per-kind Go types with `json.RawMessage` at the
  storage boundary, typed structs at the API boundary. Same
  invariant the per-wedge proposals assumed.
- **Query discipline.** Every read must filter on `kind`.
  Lint-checkable; failing to filter is a static-analysis
  finding, not a runtime bug.

## What defensibility looks like under the collapse

Every claim in the per-wedge dossiers' "Defensibility under
Dynamic Workflows" sections holds:

- Cross-session durability -- `events` is on-disk; Dynamic
  Workflows' in-memory script variables are not.
- Cross-operator audit -- `signature` column + per-operator
  partitioning; Dynamic Workflows have no operator concept.
- Deterministic replay -- the journal IS the events table; same
  argument as the per-wedge proposal, fewer moving parts.
- Reduced abstraction surface -- harder for a vendor to absorb a
  control plane that is "one event log + one policy table" than a
  bundle of feature-specific stores.

The wedge ranking matrix in `README.md` does not change. The
build-cost column drops one notch for every wedge that previously
introduced a bespoke table:

| Wedge | Build cost before | Build cost after |
|---|---|---|
| Cost governor | low | low |
| Approval gates | low | very low |
| Plan-as-code | low | low |
| Conditional DAG | medium | low |
| Blackboard | medium-high | medium |

## Trigger to adopt the unified substrate

The substrate is not its own feature -- it is the way the other
features should ship. Adopt the substrate **before** the second
wedge lands. Concretely: once `cost-governor` or `approval-gates`
clears its trigger and enters Phase 2, build the substrate first
and land the wedge against it. Otherwise the first wedge ships a
bespoke table that the second has to rip out.

## Grade rubric (for the substrate itself)

| Tier | Criterion |
|---|---|
| **B** | `events`, `policies`, `blobs` tables shipped; `Decider` interface in `internal/program/`; one wedge migrated against the substrate. |
| **A** | All B + per-`kind` schema validators wired at write; per-`kind` TTL enforced through `policies WHERE kind='ttl'`; mutation-tested append-only invariant. |
| **A+** | All A + cross-`kind` query lint (no read without `kind` filter); partition pruning verified on a 10M-row synthetic load; substrate carries every existing wedge feature with zero per-wedge tables remaining. |

## References

- `docs/design.md` §Programs -- `RouteVerdicts` is the existing
  primitive the `Decider` interface generalises.
- `contracts/schemas/gate_result.schema.json` -- existing signed
  verdict shape; one canonical form for every `Decider` output.
- `contracts/schemas/handoff.schema.json` -- existing HMAC
  signature mechanism; reused unchanged for `events.signature`.
- Each wedge dossier in this directory carries a one-line
  cross-link back to this dossier from its "Proposed data model"
  section.
