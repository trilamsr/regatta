# Wedge: blackboard (shared agent state)

Prospective. Not on the milestone path. See
[`README.md`](./README.md).

## Thesis

Concurrent subagents on a large refactor or fleet migration need
to share findings -- file paths, API contracts, schema decisions,
partial results -- without stuffing every prompt with everything.
[Cemri et al. 2025, "MAST: A Taxonomy of Multi-Agent System
Failures"](https://arxiv.org/pdf/2503.13657) finds **36.9% of
multi-agent failures come from inter-agent misalignment driven by
stale or invisible shared state.** That's the operator-pain
anchor. The classical blackboard pattern (HEARSAY-II, BB1),
updated with LangGraph's typed-channels-with-reducers contract
and Bazel's content-addressed payload split, is the right shape.

Maps to **Trap Catalog P6** (verified grounding for any
outward-facing claim) and **P9** (sensitive context segregation).

### Defensibility under Dynamic Workflows

Among the five wedges, this is the most defensible against
Claude Code Dynamic Workflows. Dynamic Workflows' intermediate
state lives as script-local variables in the orchestration JS --
it does not survive a process restart, an operator handoff, or a
cross-run join. Typed shared state with provenance is structurally
incompatible with a session-local script. The ranking matrix in
the README understates this; reconsider whether this wedge
deserves an earlier wave.

## Classical roots and what they taught us

- [HEARSAY-II](https://websites.nku.edu/~foxr/CSC425/hearsay2.pdf)
  (CMU, 1980): global hypothesis space, anonymous publish,
  schema-typed regions, opportunistic activation. Knowledge
  sources only knew the blackboard schema, never each other.
- [BB1](http://www-ksl.stanford.edu/projects/BB1/bb1.html)
  (Hayes-Roth, 1985): added a second, *control* blackboard so
  the system reasoned opportunistically about its own
  scheduling.
- What worked: anonymous publish, schema-typed regions,
  opportunistic activation.
- What broke: scheduler hand-tuning, unbounded hypothesis growth
  (no GC), no provenance for retraction, single-machine
  semantics.

## Modern parallels

| System | Typed keys | Mutability | Schema | Read cost | Merge model |
|---|---|---|---|---|---|
| [LangGraph](https://docs.langchain.com/oss/python/langgraph/graph-api) | Yes (TypedDict) | Reducer-merged | Yes | O(channel) | Reducer function |
| [Inngest AgentKit shared state](https://agentkit.inngest.com/concepts/agents) | Yes (TS types) | Reducer-merged | Yes | O(channel) | Reducer function |
| [mem0 multi-agent memory](https://mem0.ai/blog/multi-agent-memory-systems) | Yes (typed namespaces) | Mutable + history | Yes | O(query) | Vendor-managed |
| [CrewAI](https://docs.crewai.com/concepts/processes) | No | Append-only | No | O(history) | Implicit order |
| [AutoGen GroupChat](https://microsoft.github.io/autogen/stable/user-guide/core-user-guide/design-patterns/group-chat.html) | No | Append-only | No | O(N·turns) | None — total order |
| [Temporal workflows](https://docs.temporal.io/workflows) | Yes (workflow vars) | Mutable | Yes | Query API | Last-write |
| [Erlang ETS](https://www.erlang.org/doc/man/ets.html) | Yes (table + key) | Mutable | Optional | O(1) hash | Per-op |
| [Bazel remote cache](https://bazel.build/remote/caching) | Content hash | Write-once | Digest | O(1) | Identity (CAS) |
| [CRDT store](https://crdt.tech/) | Yes | Mergeable | Yes | O(key) | Type-defined |

## Most directly relevant prior art

[Salemi et al. 2025, *LLM-based Multi-Agent Blackboard System for
Information Discovery in Data Science*](https://arxiv.org/abs/2510.01285).
A central agent posts a request, subordinates self-select if they
can contribute, and **all agents operate autonomously, responding
to requests posted on the blackboard.** Reports 13-57% relative
improvement on three data-discovery benchmarks (KramaBench,
modified DS-Bench, DA-Code) versus RAG and master-slave baselines.
Scope the claim to those benchmarks; the broader fleet-generalisation
result is not yet proven.

## Patterns worth stealing

1. **Typed regions plus reducers per key** (LangGraph). Write
   conflicts become a *schema* problem, not a runtime problem. An
   `Annotated[list, operator.add]` declaration bakes a
   commutative-merge contract into the schema.
2. **Content-addressed payloads, typed metadata** (Bazel, Nix).
   Small facts in `value_json`; large blobs in a content-addressed
   store keyed by `sha256`.
3. **Anonymous publish-subscribe** (HEARSAY-II, Salemi et al.).
   Agents post against keys or tags; they never address each
   other.
4. **Append-only fact log plus materialised view** (Temporal
   event history). Durable, crash-replayable, query-friendly.
5. **Write-once-per-`(key, writer)` with vector clocks.**
   Sidesteps last-write-wins data loss when two agents finish
   near-simultaneously.
6. **Capability-scoped reads and writes** (ETS
   `public`/`protected`/`private`). Subagents declare which keys
   or tags they may touch.
7. **Opportunistic control blackboard** (BB1). Surface "facts
   that are now stale" and "facts blocking node N" as first-class
   observable state.
8. **Selective read API, never dump-all.** Refuses the AutoGen
   prompt-bloat trap.

## Failure modes

- **Prompt bloat.** N agents x M facts -> quadratic context.
  Agents *query*; never auto-inject.
- **Stale reads.** Fact written at T1, consumed at T5,
  invalidated at T3 by another writer. Mitigation: version
  numbers plus read-your-writes per `dag_run_id`.
- **Write conflicts.** Two agents discover the same schema,
  propose different shape. Mitigation: reducer per key; if no
  reducer, fail loudly with both candidates surfaced for the
  operator.
- **Unbounded growth.** Every retry, every speculative branch
  posts facts. Mitigation: TTL + per-run scope GC + maximum
  facts per key.
- **Tampering.** A hostile or buggy subagent overwrites a
  high-value fact. Mitigation: provenance row, append-only
  history, key ACLs, signed-by-writer hash.
- **Schema drift across runs.** Key namespace per
  `WorkItem.kind`, registry of known keys, lint to enforce.

## Proposed data model

Substrate note: the `facts` and `blobs` tables below are
**superseded** by the [unified substrate](./unified-substrate.md).
Facts ship as `events WHERE kind='fact'` (with `key`, `supersedes`,
`written_by` carried over verbatim). Reducers move into
`policies WHERE kind='reducer'`; key ACLs into
`policies WHERE kind='acl'`. The `blobs` content-addressed store
is shared substrate, not blackboard-specific. The columns below
are the `spec_json` shapes for those policy rows and the
`payload_json` shape for fact events.

A single-table shape conflates two concerns. Split metadata from
payload, Bazel-style:

```sql
CREATE TABLE facts (
    fact_id        TEXT PRIMARY KEY,        -- ulid
    dag_run_id     TEXT NOT NULL,
    key            TEXT NOT NULL,            -- namespaced: "schema.user_table", "files.touched"
    value_json     TEXT,                     -- small payload; null if artifact_digest set
    artifact_digest TEXT,                    -- sha256 into blobs (Bazel CAS)
    written_by     TEXT NOT NULL,            -- work_item_id
    written_at     INTEGER NOT NULL,
    supersedes     TEXT,                     -- fact_id of prior version (append-only)
    ttl_at         INTEGER,
    tags_json      TEXT,                     -- ["api-contract", "draft"]
    reducer        TEXT,                     -- "lww" | "set-union" | "append" | "write-once"
    schema_version INTEGER NOT NULL,
    UNIQUE (dag_run_id, key, written_by, schema_version)
);
CREATE INDEX facts_run_key ON facts(dag_run_id, key, written_at DESC);
CREATE INDEX facts_tags    ON facts(dag_run_id, tags_json);

CREATE TABLE blobs (
    digest       TEXT PRIMARY KEY,
    bytes        BLOB,
    size         INTEGER,
    content_type TEXT
);
```

Why no in-place `version` column: it hides whether updates are
LWW, set-union, or write-once. Make the merge contract
**explicit in the row** (`reducer`) and drop in-place mutation
entirely. Append plus a `supersedes` pointer gives a free audit
trail and lets the operator diff "what changed mid-run."

## Read API (three orthogonal modes, never auto-injected)

- `fact.get(key)` — latest non-superseded value, typed key
  lookup, O(1).
- `fact.list(tag=…)` — tag-faceted scan, cheap with the covering
  index.
- `fact.semantic(query, k=5)` — only when the agent does not know
  the key namespace. Backed by an optional FTS5 or embedding
  view over `value_json`, not the canonical store.

Tradeoff: typed keys are deterministic but demand a shared
vocabulary (publish a key registry, validate in CI). Semantic
search rescues agents from vocabulary mismatch at the cost of
non-determinism -- opt-in per WorkItem only. Never *push* facts
into prompts; agents *pull*. Sidesteps the AutoGen O(N^2) growth
trap.

## Garbage collection

- Default scope per `dag_run_id`: drop on run terminal state plus
  a retention window (e.g. 7 days for forensics).
- TTL column: speculative or draft facts expire in minutes;
  decisions persist for the run lifetime.
- Promotion: facts tagged `persistent` survive run GC and move
  to a separate `lessons` table (ties into `learn-from-mistakes`).
- Cap: `max_facts_per_(run, key)` rejects pathological writers;
  surfaces in the control blackboard.

## Security and provenance

- `written_by` is mandatory and HMAC-signed over `(fact_id, key,
  value_json | digest, written_by)` using a per-run secret the
  orchestrator injects into each subagent's env. A hostile
  subagent cannot forge another's signature without exfiltrating
  the secret.
- Append-only plus `supersedes` — tampering requires writing a
  *new* row; the old row remains queryable. Diff = audit.
- **Key ACLs in `WorkItem.kind` manifest** — each subagent
  declares `reads: […]` and `writes: […]`. Orchestrator rejects
  out-of-scope writes at the API boundary, not at runtime trust.
- Content-addressed blobs are verifiable — anyone can rehash and
  detect substitution (Nix property).
- Capability-scoped reads via a `visibility` column:
  `private` / `protected` / `public`.

## Regatta extension points

- Optional `SharedState` interface added to the orchestrator
  (deferred — gates on observed need per adopt-when-needed).
- `contracts/schemas/handoff.schema.json` already carries
  inter-feature data with HMAC; the blackboard is the
  append-only log of that handoff plus per-run scope and key
  registry.
- New table `feature_artifacts{feature_id, artifact_key, payload,
  signature}` for structured inter-feature grounding.

## Trigger metric (when to adopt)

- First customer running a refactor or migration with ≥4
  concurrent subagents that share file or schema findings.
- OR documented case where two subagents redid the same
  discovery work and the operator could not deduplicate it
  through handoff alone.

## Grade rubric

| Tier | Criterion |
|---|---|
| **B** | `facts` table + typed `fact.get` / `fact.list` API + HMAC provenance + per-run GC. |
| **A** | All B + content-addressed `blobs` table + explicit `reducer` column + append-only `supersedes` chain + key ACL enforcement at API boundary. |
| **A+** | All A + key registry validated in CI + `fact.semantic` opt-in mode + lessons promotion path + zero forged-signature paths (fuzz-tested with hostile subagent harness). |

## References to existing repo state

- `contracts/schemas/handoff.schema.json` — existing typed
  inter-feature handoff; blackboard generalises it.
- `docs/design.md` §Programs §Audit — the same HMAC mechanism is
  reused for fact provenance.
- `docs/incidents.md` P6 and P9.
