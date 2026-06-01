# Spec: Unified Substrate — events log (v2, post-review)

_Author: design subagent, 2026-06-01 (v2 re-spec). Source-of-truth: `docs/wedges/unified-substrate.md` (thesis) + `docs/superpowers/briefs/2026-05-31-mvp-3-next-level.md` §5 thread #1. Adversarial review v1: `docs/superpowers/reviews/2026-06-01-unified-substrate-review.md` (8 Risk + 7 Important + 5 Simplifications)._

This spec locks the schema, migration plan, reducer contract, signing strategy, tenant propagation, performance budget, and grade rubric for the **unified events log** — one primitive (`substrate_events`) — that collapses the bespoke "history" tables proposed by `cost-governor.md`, `approval-gates.md`, `conditional-dag.md`, and `blackboard.md`. MVP-2 shipped `approvals`, `approval_events`, `work_item_outputs`, `work_item_edges`, and `events` (per-agent) under the bespoke shape. The substrate ships behind a feature flag, **mirrors** legacy writes via a phased read-side cutover (NO mid-tx dual-write), then deprecates legacy tables after the cutover proves stable.

---

## Changes from v1 (this section addresses the review verdict)

1. **(R1)** Migration renamed `0005_substrate.sql` → `0006_substrate.sql`. W6 OTel spec owns 0005 (`trace_id` columns); we verified `internal/orchestrator/state/migrations/` ships 0001–0004; 0005 is uncommitted but W6's spec assigns it; substrate takes 0006.
2. **(R2)** Atomic dual-write claim dropped. Replaced with **events-log-as-additive-mirror, phased read-side cutover** (Phase A reads legacy, Phase B reads both flag-gated for shadow assertion, Phase C reads substrate-only after cutover-by-flag). No new transaction-threading refactor across packages.
3. **(R3)** `tenant_id TEXT NOT NULL` (no SQL `DEFAULT`). Default tenant constant lives in code (`substrate.DefaultTenantID = "default"`). Backfill migration step explicitly fills the column for any pre-substrate row at substrate-table creation (substrate is new — no pre-existing rows — but the constraint shape carries forward).
4. **(R4)** Sequencing fixed: **substrate ships in MVP-3 Wave 1, AFTER W6** (T1 + T2 + T5 must be merged first). Schema adds `trace_id` and `span_id` columns referencing W6's resource-attribute conventions.
5. **(R5)** Perf budget demoted from "500 writes/sec sustained" to "500 writes/sec target" with napkin math defending a ~13K writes/sec single-writer ceiling. Added **Task 0** (benchmark) to the implementation plan; required to land before Task 1's PR opens for review.
6. **(R6)** Reducer for `token_spend` changed from `set-union` to `append`. SUM over filtered window is the budget computation. Documented in §4 why set-union collapses legitimate retries.
7. **(R7)** Supersedes-cycle defense changed from depth-5 walk to **Kahn's cycle-check at write time** (single tx, insert-then-verify-or-rollback). Cites the existing pattern at `internal/orchestrator/state/work_items_query.go` shipped under issues #90 / #177.
8. **(R8)** CAS GC race replaced by **ref-counted mark-and-sweep w/ generation counter + `gc_grace_seconds` (default 3600s)**. Never delete a blob whose `created_at > now() - gc_grace_seconds`. CAS shipped in a separate wedge — not Wave 1.
9. **(S1)** Wave 1 ships **events log only**. Policies primitive deferred to W8 RBAC wave. Blobs primitive deferred to W11 blackboard wave. The substrate spec body retains forward-fit columns (`blob_digest`, `schema_version`) but does NOT ship a CAS table, policy table, or GC cron in Wave 1.
10. **(S3)** `KindLockHeld` removed from event-kind enum. Locks stay on the existing mutable `locks` table; substrate is for append-only event history.
11. **(S5)** `Decider` 3-impl interface dropped. Wave 1 ships `CELDecider` as a concrete type only. Interface extracted when the second impl forces it.
12. **Important findings I1–I7** each addressed inline or as `[substrate-followup]` tracking issues cited in the implementer PR body.
13. **Wave 1 task count: 6 → 3** file-disjoint subagent tasks (excluding Task 0 benchmark gate).

---

## 1. Prior art adopted (no bespoke invention)

Per `memory/feedback_research_design_principles`, every primitive cites a proven OSS source. Where two systems collide, the dossier's "why this one" is the tiebreaker.

| Primitive | Adopted from | What we take | Why not bespoke |
|---|---|---|---|
| Append-only signed event log | [Temporal event history](https://docs.temporal.io/workflows) | History = audit, state = `fold(events)`, never row mutation | Temporal proves the model at scale (37 tables → audit replay); reinventing the journal is a known anti-pattern |
| Event ULID + chain | [AWS Step Functions execution history](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-amazon-states-language.html) | Monotonic IDs + `previous_event_id` chain for replay determinism | Step Functions ships this in production; we copy the ID + chain semantics |
| Reducer-per-channel | [LangGraph reducers](https://langchain-ai.github.io/langgraph/concepts/low_level/#reducers) | `lww` / `append` / `write-once` strategy enum per `key` | LangGraph proved typed reducers in production multi-agent graphs |
| HMAC canonical signing | `contracts/schemas/sign.go` (this repo) | JCS-flavored canonicalization, HMAC-SHA256, `key_id` keyring | Already shipping, already proven, already audited |
| One CEL decider | `internal/program/route.go::RouteVerdicts` (this repo) + [`cel-go`](https://github.com/google/cel-go) | Deterministic Go function over signed verdicts; no LLM in the routing path | Already shipping (Trap P1); generalises to `CELDecider` concrete type |
| Cycle-check on write | `internal/orchestrator/state/work_items_query.go` Kahn's-sort impl (this repo) | Insert-then-Kahn'sValidate in single tx, rollback on cycle | Already shipping under issue #90 (cycle check) / #177 (test fixture); reuse pattern verbatim |

**Rejected alternatives (defended below):** Postgres LISTEN/NOTIFY for fan-out (we're sqlite, single-host MVP); CRDT store (overkill, no concurrent multi-writer requirement at MVP-3); embedded Kafka (cost prohibitive, single-host MVP).

**Deferred to later waves (NOT Wave 1):** `substrate_policies` (W8 RBAC wave); `substrate_blobs` CAS (W11 blackboard wave); HumanDecider + VerifierDecider impls; the `Decider` interface itself (S5).

---

## 2. Locked schema (Wave 1 ships ONE table)

### 2.1 `substrate_events` — single append-only signed log

**Go struct (`internal/orchestrator/state/substrate/event.go`):**

```go
// Event is one row in the substrate event log. Append-only. State for
// any wedge is fold(events WHERE kind=X) — never a row mutation.
//
// Signed: every event carries an HMAC over (id, kind, key, payload,
// written_by, schema_version, trace_id, span_id, nonce) using
// contracts/schemas/sign.go.
type Event struct {
    ID             string           // ULID; primary key + monotonic for replay
    RunID          string           // DAG run correlation
    WorkItemID     string           // optional; "" for run-level events
    TenantID       string           // NOT NULL; defaults to substrate.DefaultTenantID in code (R3)
    TraceID        string           // W6 OTel correlation; "" if not in a span (R4)
    SpanID         string           // W6 OTel correlation; "" if not in a span (R4)
    Kind           EventKind        // enum below — schema-validated per kind
    Key            string           // namespace for kind=fact|node_output; "" otherwise
    PayloadJSON    json.RawMessage  // small inline payload (≤1 KiB); per-kind validated
    BlobDigest     string           // forward-fit (S1): empty in Wave 1; W11 fills it
    Supersedes     string           // prior event ID; same-run FK + Kahn's-validated (R7)
    WrittenBy      string           // signed-by principal (agent id, system, operator)
    WrittenAt      int64            // unix milliseconds UTC (process-local monotonic per I2)
    SchemaVersion  int              // per-kind schema version (forward-compat per I3)
    Nonce          string           // 16-byte hex; replay protection (per I5: signed + columned)
    SigAlg         string           // per S2: column, not JSON (was signature_json)
    SigKeyID       string
    SigMAC         string
}

type EventKind string

const (
    KindNodeOutput        EventKind = "node_output"        // replaces work_item_outputs
    KindFact              EventKind = "fact"               // blackboard fact write (W11 consumer)
    KindApprovalEvent     EventKind = "approval_event"     // replaces approval_events.kind=*
    KindTokenSpend        EventKind = "token_spend"        // LLM call spend record (per call)
    KindBudgetReconciled  EventKind = "budget_reconciled"  // Anthropic Usage API cron
    KindGateVerdict       EventKind = "gate_verdict"       // signed CELDecider output
    KindHeartbeat         EventKind = "heartbeat"          // running-agent liveness
)
// Note (S3): KindLockHeld / KindLockReleased deliberately omitted.
// Locks have a mutable lifecycle; they live on the existing `locks` table.
```

**sqlite DDL (migration `0006_substrate.sql`, addresses R1):**

```sql
-- 0006_substrate.sql
-- Substrate event log. Wave 1 of MVP-3, lands AFTER W6's 0005_trace_id_columns
-- (which adds trace_id columns to work_items + approval_events).
-- This migration depends on 0005 being applied; goose will refuse to apply
-- 0006 if 0005 is missing.

CREATE TABLE substrate_events (
    id              TEXT    NOT NULL PRIMARY KEY,             -- ULID (26 chars Crockford-base32)
    run_id          TEXT    NOT NULL,
    work_item_id    TEXT,                                      -- NULL allowed (run-level)
    tenant_id       TEXT    NOT NULL,                          -- R3: NOT NULL, no SQL DEFAULT
    trace_id        TEXT    NOT NULL DEFAULT '',               -- R4: W6 OTel correlation (32 hex)
    span_id         TEXT    NOT NULL DEFAULT '',               -- R4: W6 OTel correlation (16 hex)
    kind            TEXT    NOT NULL,
    key             TEXT    NOT NULL DEFAULT '',
    payload_json    TEXT    NOT NULL DEFAULT '{}',
    blob_digest     TEXT    NOT NULL DEFAULT '',               -- S1 forward-fit; W11 CAS fills it
    supersedes      TEXT    NOT NULL DEFAULT '',               -- prior event id; '' if first
    written_by      TEXT    NOT NULL
                    CHECK (length(written_by) > 0
                           AND length(written_by) <= 128
                           AND written_by NOT GLOB '*[^a-zA-Z0-9_:.-]*'),
    written_at      INTEGER NOT NULL,                          -- unix ms UTC
    schema_version  INTEGER NOT NULL DEFAULT 1,
    nonce           TEXT    NOT NULL,                          -- 16-byte hex; signed + columned (I5)
    sig_alg         TEXT    NOT NULL,                          -- S2: column not json
    sig_key_id      TEXT    NOT NULL,
    sig_mac         TEXT    NOT NULL,
    CHECK (kind IN ('node_output','fact','approval_event','token_spend',
                    'budget_reconciled','gate_verdict','heartbeat')),
    CHECK (length(payload_json) <= 1024),
    CHECK (trace_id = '' OR length(trace_id) = 32),
    CHECK (span_id  = '' OR length(span_id)  = 16),
    -- Same-run supersedes (R7): supersedes must reference an existing row in
    -- the same run. FK enforces existence; Kahn's-validate in AppendEvent
    -- enforces acyclicity.
    FOREIGN KEY (supersedes) REFERENCES substrate_events(id)
);

CREATE INDEX idx_substrate_events_kind
    ON substrate_events(run_id, kind, key, written_at DESC);
CREATE INDEX idx_substrate_events_wi
    ON substrate_events(work_item_id, kind, written_at DESC)
    WHERE work_item_id IS NOT NULL;
CREATE INDEX idx_substrate_events_tenant
    ON substrate_events(tenant_id, kind, written_at DESC);
CREATE INDEX idx_substrate_events_supersedes
    ON substrate_events(supersedes)
    WHERE supersedes != '';
CREATE INDEX idx_substrate_events_trace
    ON substrate_events(trace_id)
    WHERE trace_id != '';

-- I5 replay-protection: per (run_id, written_by, nonce). Bounds collision
-- surface to a single writer per run; cross-writer collisions are impossible.
CREATE UNIQUE INDEX uq_substrate_events_nonce
    ON substrate_events(run_id, written_by, nonce);
```

**Per-`kind` payload validation** is done via Go typed unmarshal per S4 — no JSON Schema files. Dispatch table in `internal/orchestrator/state/substrate/validate.go` calls `json.Unmarshal` into a per-kind typed struct (`NodeOutputPayload`, `FactPayload`, etc.). Unmarshal failure ⇒ `ErrInvalidPayload`.

**`EventKind` enum ↔ SQL CHECK parity test** (N1): `TestSubstrate_EventKindEnumMatchesSQLCheck` introspects both lists; fails on mismatch.

### 2.2 `CELDecider` — single concrete type, no interface (S5)

`internal/program/cel_decider.go`:

```go
// CELDecider evaluates a CEL predicate against a Snapshot and emits a signed
// gate_verdict event. Wave 1 is the only impl; HumanDecider / VerifierDecider
// will land in later waves and force an interface extraction at that time.
type CELDecider struct {
    Program cel.Program  // pre-compiled at plan-time (rejects malformed CEL)
}

type Snapshot struct {
    RunID      string
    WorkItemID string
    TenantID   string
    TraceID    string
    SpanID     string
    Inputs     map[string]json.RawMessage  // from substrate WHERE kind=node_output, snapshot iso
    Outputs    map[string]json.RawMessage  // outgoing edge candidates
}

type GateResult struct {
    Pass   bool
    Reason string
}

// Decide evaluates the CEL predicate and emits a signed gate_verdict event.
// Snapshot is captured under read-snapshot isolation (sqlite WAL provides this
// for the duration of the BEGIN/COMMIT) — addresses #17 in §10.
func (c *CELDecider) Decide(ctx context.Context, s Snapshot) (GateResult, error) {
    /* … */
}
```

No `Decider` interface ships in Wave 1. When HumanDecider lands (Wave 2 of W2 approval-gates wedge), the interface is extracted then.

### 2.3 Substrate Go API (`internal/orchestrator/state/substrate/`)

```go
// AppendEvent inserts one row and verifies invariants in a single tx:
// (a) per-kind payload typed-unmarshal validates;
// (b) HMAC signs (id, kind, key, payload, written_by, schema_version,
//     trace_id, span_id, nonce);
// (c) supersedes FK + same-run check via Kahn's validate;
// (d) UNIQUE(run_id, written_by, nonce) collision = ErrReplay;
// (e) lastWrittenAt monotonicity check in process (I2).
func AppendEvent(ctx context.Context, tx *sql.Tx, e Event) error

// Fold returns events for a given (run, kind) ordered by (written_at, id).
// Read happens under WAL snapshot isolation.
func Fold(ctx context.Context, db *sql.DB, runID string, kind EventKind) ([]Event, error)

// DefaultTenantID is the per-process constant (R3): no SQL DEFAULT, code default.
const DefaultTenantID = "default"
```

**Transaction discipline:** `AppendEvent` takes an explicit `*sql.Tx`. Callers wrap with `db.WithTx(ctx, func(tx) error { ... })` per the existing pattern (`internal/orchestrator/state/agents.go::WithTx`). No mid-package transaction-threading refactor required because **no legacy caller needs to dual-write** (R2). Substrate is a NEW table; existing tables are untouched in Wave 1.

---

## 3. Migration plan — phased read-side cutover (NO atomic dual-write, addresses R2)

**Core insight (R2 fix):** The original spec required mid-transaction dual-write across `approvals.go`, `events.go`, `work_item_outputs.go`. That refactor crosses 3+ packages, requires `WithinTx` to be threaded through every caller (spawner / reaper / scheduler / adaptersync), and sqlite WAL forbids nested transactions. We drop the dual-write contract entirely.

**Replacement design:** Substrate is an **additive, write-side-independent mirror**. Every event-producing site that wants to participate in the substrate ALSO calls `AppendEvent` — but the legacy write and the substrate write are **independent transactions**. Reconciliation handles partial failures (substrate has it, legacy doesn't; or vice versa). The substrate is **read-only mirror until cutover-by-flag**.

### Phase A — Substrate ships dark, no readers (default `SUBSTRATE_ENABLED=false`)

- Migration `0006_substrate.sql` creates `substrate_events`.
- Go package `internal/orchestrator/state/substrate/` exposes `AppendEvent` and `Fold`.
- **No writers, no readers**. The table exists but contains zero rows on every deployment.
- Exercised only by unit tests + the benchmark (Task 0).
- Wave 1 ships at this phase; cutover phases below are MVP-3 Wave 2+ deliverables.

### Phase B — Shadow-write (flag `SUBSTRATE_SHADOW_WRITE=true`)

- Producer callsites that opt in (approval-gates wedge first, by isolation) call `AppendEvent` AFTER their legacy write succeeds. Two independent transactions.
- Read paths still hit legacy tables ONLY. Substrate is shadow.
- Reconciliation cron (Task 3) periodically asserts `fold(substrate WHERE kind=approval_event AND run_id=X) ≈ SELECT * FROM approval_events WHERE run_id=X`. Divergence triggers an audit issue, NOT a runtime failure (substrate is shadow, not source-of-truth).
- A producer site that fails to call `AppendEvent` (post-legacy-write crash) is detected by the reconciliation cron's "substrate is missing rows legacy has" check. Reconciliation cron BACKFILLS via re-signing (the legacy row carries enough data; signing key is the per-process secret).

### Phase C — Cutover (flag `SUBSTRATE_READ_FROM=substrate` for the migrated wedge)

- ONE wedge at a time (lowest-risk first: `node_output` reads, by Phase A grade-B criterion).
- Read paths flip to substrate. Legacy writes continue.
- Tests: `TestSubstrate_CutoverNoRegression_NodeOutput` — every MVP-2 e2e suite passes with `SUBSTRATE_READ_FROM=substrate node_output`.
- Rollback: flip the flag back. Legacy tables still being written ⇒ rollback is zero-loss.

### Phase D — Deprecate (later MVP-3 wave; NOT this spec's deliverable)

Per-table-drop migrations land one-at-a-time after a release cycle of `SUBSTRATE_READ_FROM=substrate` being default. Out of scope for this spec.

---

## 4. Reducer contract per `kind`

Per `memory/feedback_spec_pattern_authority`: spec dictates strategy per `kind`. Reducer override via policies-table is OUT OF SCOPE for Wave 1 (S1: policies primitive deferred). Strategies are hardcoded.

| `kind` | Strategy | Fold semantics | Why |
|---|---|---|---|
| `node_output` | `lww` (latest by `written_at`, tie-break by ULID) | Most-recent output per `(work_item_id, attempt_no)` wins | Replaces `work_item_outputs` shape verbatim |
| `fact` | `lww` default; per-key override deferred to W11 (S1) | Most-recent fact per `key` wins | W11 blackboard wave will ship policy-driven reducer override |
| `approval_event` | `append` | State machine: pending → approved \| rejected \| timed_out via fold | Each transition is a distinct event row |
| `token_spend` | `append` (R6 — changed from set-union) | **Budget = SUM(spend_usd) over filtered window** | Set-union collapses legitimate retries: provider re-bills with corrected token count = a new distinct record, not a duplicate. Idempotency at append layer is enforced by `UNIQUE(run_id, written_by, nonce)` — duplicate idempotency keys collapse to ErrReplay BEFORE the append, not by reducer logic. (R6 detail below.) |
| `budget_reconciled` | `lww` per `(tenant_id, period_start)` | Most-recent Usage-API row wins | Reconciliation is a correction, not history |
| `gate_verdict` | `append`; `RouteVerdicts` consumes most-recent per `(work_item_id, gate_name)` | Signed verdict chain | Existing `RouteVerdicts` semantics preserved |
| `heartbeat` | `lww` per `(work_item_id)` | Most-recent timestamp wins | Liveness is a single value |

**Why `append` for `token_spend` (R6 detail):** Set-union over `(work_item_id, llm_call_id)` was wrong for two reasons. (1) `SUM(spend_usd)` is the only aggregation budget enforcement needs; set-union would require additional dedupe-aware aggregation. (2) Legitimate retry semantics: when Anthropic's Usage API reconciles and emits a re-billed value for the same `llm_call_id` (e.g. corrected token count), set-union dedupe by `llm_call_id` would either double-count or silently drop the correction. The `append` strategy is correct: each spend record is a distinct event. Duplicate-write protection is at the `nonce` layer (the writer must mint a fresh nonce for each event). Reconciliation events emit `kind='budget_reconciled'` with `lww` semantics — that's the channel for corrections.

**Reducer strategy enum** lives in `internal/orchestrator/state/substrate/reducer.go`:

```go
type ReducerStrategy string
const (
    StrategyLWW       ReducerStrategy = "lww"
    StrategyAppend    ReducerStrategy = "append"
    StrategyWriteOnce ReducerStrategy = "write-once"
)
```

Per-kind defaults are hardcoded in `defaultReducer(kind)`. Policy-driven override is deferred to W11 (S1).

---

## 5. HMAC signing strategy

**Reuse `contracts/schemas/sign.go` verbatim.** Per `feedback_research_design_principles`: signing is already proven on `gate_result.schema.json` + `handoff.schema.json`.

### Canonicalization per row

The HMAC input is `CanonicalJSON(payload)` where `payload` is the map:

```json
{
  "id":              "<ulid>",
  "run_id":          "<run>",
  "work_item_id":    "<wi or empty>",
  "tenant_id":       "<tenant>",
  "trace_id":        "<hex or empty>",
  "span_id":         "<hex or empty>",
  "kind":            "<event-kind>",
  "key":             "<key or empty>",
  "payload_json":    <raw json or empty object>,
  "blob_digest":     "<sha256: prefix or empty>",
  "supersedes":      "<prior id or empty>",
  "written_by":      "<principal>",
  "written_at":      <int>,
  "schema_version":  <int>,
  "nonce":           "<16-byte hex>"
}
```

Sign with the same `key_id` the writer already uses (handoff signer / gate decider). One keyring, one rotation policy.

### Replay-protection nonce (per I5)

Every event carries a 16-byte hex `nonce` column. The signature INCLUDES the nonce. The `UNIQUE(run_id, written_by, nonce)` index makes replay structurally impossible.

**Verifier invariant (I5 fix):** `Verify(e)` MUST assert `e.Nonce == signedPayload["nonce"]`. Mismatch is `ErrUnverifiable`. Without this check, a hostile writer who held the key once could replay with a fresh column-nonce but unchanged signed-nonce — passing verification. `TestSubstrate_NonceMismatchRejected` is in the grade-B rubric.

### Schema-version forward migration (I3)

Forward-version migration recipe:
1. Bump `schema_version` constant for the affected kind only.
2. Ship a versioned canonicalization helper `CanonicalJSONV(v any, schemaVersion int) ([]byte, error)`.
3. Run one release cycle where BOTH the v1 and v2 verifiers exist (writers may emit v1 OR v2).
4. After the release cycle, writers can emit v2 only.
5. Operator runbook entry: "before deploying a kind-payload schema change, confirm all reader processes have been upgraded ≥1 release."

`[substrate-followup]` issue: "schema_version v2 migration recipe — first real bump."

### Verifier keyring trust-on-first-use defense (#13 in §10)

Keyring is loaded at process Setup and is **read-only after Setup**. Lint `tools/lint-keyring-readonly.go` rejects any `KeyringSet` call outside `init` / `Setup`. No runtime keyring mutation ⇒ no compromised-process key-injection attack.

---

## 6. Tenant_id propagation (W8 RBAC forward-fit, addresses R3)

`tenant_id TEXT NOT NULL` (no SQL `DEFAULT` per R3). The Go constant `substrate.DefaultTenantID = "default"` is the value writers use on single-tenant deployments. The schema does NOT silently fill the column — the Go-level constant is a deliberate, auditable choice.

**Propagation rules:**
- Spawner sets `tenant_id` from `WorkItem.TenantID` (forward-fit column on `work_items` — to be added in a sibling migration when W8 RBAC ships).
- All `AppendEvent` calls require `tenant_id` parameter. `AppendEventForRun(ctx, tx, run)` resolves it from `run.TenantID`, falling back to `substrate.DefaultTenantID` ONLY at the resource-attribute level.
- Read paths: `Fold(ctx, runID, kind)` does not filter by tenant_id (within-run is already tenant-bounded by spawner). Cross-run reads (audit, billing) MUST filter; lint rule catches.

**Single→multi-tenant cutover runbook:** When an operator turns on multi-tenant, every pre-cutover row carries `tenant_id='default'`. Substrate provides a CLI: `regatta substrate retag-tenant --run-prefix=<X> --tenant=<Y>` that re-tags rows + re-signs them with the operator key. Operator runbook entry mandates audit-before-retag.

`[substrate-followup]` issue: "tenant_id retag helper for single→multi-tenant cutover."

**Lint:** `tools/lint-substrate-queries.go` rejects any SQL string in `internal/` that selects from `substrate_events` without a `WHERE` clause containing `kind = ?`. For cross-run reads (no `run_id = ?` clause in the WHERE), the lint also requires `tenant_id = ?` (per reviewer §8). False-positive bound: within-run reads with `WHERE run_id = ?` are explicitly allowed to omit `tenant_id`.

---

## 7. Performance budget (addresses R5)

**Targets (NOT contracts):** numbers below are *targets*. The grade-B rubric requires `BenchmarkAppendEvent` (Task 0) lands clearing the **single-row HMAC + canonical-JSON portion** ≤ 500 ns/op. The 10M-row synthetic load test is a grade-A+ gate, runs nightly, NOT pre-merge.

| Operation | Target (p95) | Defense |
|---|---|---|
| `AppendEvent` HMAC+canonical-JSON only (no INSERT) | ≤ 500 ns/op | Napkin: HMAC-SHA256 over 1 KiB ≈ 2 µs single-core; CanonicalJSON ≈ 20 µs uncached. Optimization path: pre-canonicalize at caller (see follow-up). |
| `AppendEvent` end-to-end (incl. sqlite INSERT, WAL) | ≤ 5 ms | sqlite WAL insert single-writer ≈ 50–100 µs; HMAC+canon ≈ 25 µs; total ~150 µs. 5 ms is conservative. |
| `Fold(run, kind)` (typical: ≤100 events) | ≤ 10 ms | `idx_substrate_events_kind` covers `(run_id, kind, key, written_at DESC)`. |
| Writes/sec sustained | ≥ 500 writes/sec **target** | Napkin: HMAC (2 µs) + canonical JSON (20 µs) + sqlite WAL insert (50 µs) = ~75 µs/write ⇒ ~13K writes/sec single-writer ceiling. 500/sec is 4% of ceiling — comfortable. |

**Napkin math defense for the 13K/sec ceiling:**
- HMAC-SHA256 over a ~500-byte canonical-JSON payload, Go `crypto/hmac` on modern x86: ~2 µs.
- `CanonicalJSON` round-trips through `encoding/json` (parse) → recursive sort → emit: ~20 µs for a ~500-byte payload (measured in `contracts/schemas/sign_bench_test.go`-class benches).
- sqlite WAL INSERT with 5 secondary indices: ~50 µs single-writer.
- Total per-write CPU: ~75 µs ⇒ 1 / 75e-6 ≈ 13,300 ops/sec single-core single-writer.
- 500 writes/sec is the *target*; the ceiling is ~26× headroom. Comfortable.

**Pre-canonicalization fast-path follow-up:** A `AppendEventCanonicalized(ctx, tx, e Event, preCanonPayload []byte)` API can skip CanonicalJSON when the caller has already canonicalized (e.g. spawner builds the payload from a typed struct it already controls). `[substrate-followup]` issue: "AppendEvent fast-path for pre-canonicalized payloads."

**Task 0 (benchmark) gate:** `BenchmarkAppendEvent` MUST land BEFORE Task 1's PR opens for review. If the benchmark fails to clear 500 ns/op for the HMAC+canon portion alone, the spec is re-evaluated (likely by demoting `lru-cached canonical-JSON encoder` from optimization to baseline).

---

## 8. Failure modes (addresses R7, R8, I1, I2, I3, I5)

| Failure | Mitigation |
|---|---|
| **Events log unbounded growth** | TTL+GC cron is OUT OF SCOPE Wave 1 (no policies primitive). Wave 1 deployments rely on operator-mediated archival per run. `[substrate-followup]` issue: "per-kind TTL cron + archive_audit_outbox." |
| **CAS orphan blobs** | OUT OF SCOPE Wave 1 — no blob storage. When W11 ships the CAS, GC is **ref-counted mark-and-sweep with `gc_grace_seconds`** (R8): never delete a blob whose `created_at > now() - gc_grace_seconds` (default 3600s). The `blob_digest` column on `substrate_events` is the reference graph. |
| **Signature drift across schema_version bumps** | `schema_version` is signed (§5). Forward-version migration recipe (I3) requires both readers to be upgraded before writers emit new version. |
| **Supersedes cycle** | **Kahn's cycle-check at write time** (R7): `AppendEvent` runs INSERT inside a tx, then runs a Kahn's-style topo-sort over the supersedes graph restricted to `(run_id = ?)`. If the new edge creates a cycle, the tx rolls back with `ErrSupersedesCycle`. Reuses the pattern at `internal/orchestrator/state/work_items_query.go` (shipped under issues #90 / #177). Foreign key `supersedes REFERENCES substrate_events(id)` rules out forward-references; same-run check is an app-layer assertion in `AppendEvent`. |
| **Process clock regression** (I2) | `AppendEvent` tracks `lastWrittenAt` per process (sync.Mutex-guarded). New writes where `written_at < lastWrittenAt` fail with `ErrClockRegression`. **No SQL CHECK** — the v1 CHECK was incoherent (sqlite CHECKs don't see session state). |
| **Hostile subagent forges signature** | HMAC key is per-writer. Without the secret, no signature passes. Plus keyring-readonly defense (§5). |
| **Nonce column ≠ signed nonce** (I5) | Verifier asserts column-nonce == signed-payload-nonce. `TestSubstrate_NonceMismatchRejected` — grade B. |
| **ULID cross-host collision** | Wave 1 is single-host (single sqlite writer). Cross-host writes are W9-deferred. `[substrate-followup]` issue: "UUIDv7 vs ULID for post-W9 multi-host deployments." (I4) |
| **Reducer-strategy change after rows exist** (#15 in §10) | Wave 1 hardcodes reducers per-kind — no policy-driven override exists yet. When W11 ships policy-driven reducer override, the spec MUST add: "reducer strategy is write-once per `key` once the first event lands. Change requires explicit re-fold script." `[substrate-followup]` issue. |
| **`KindLockHeld` mismatch** (I1, S3) | Resolved by dropping `lock_held` from the enum. Locks stay on the existing mutable `locks` table. |

---

## 9. Grade rubric (B / A / A+ — tool-checkable per `feedback_grade_rubric`)

| Tier | Criterion | Tool check |
|---|---|---|
| **B** | `substrate_events` ships under migration `0006_substrate.sql`; `AppendEvent` + `Fold` + `CELDecider` shipped; `BenchmarkAppendEvent` passes ≤ 500 ns/op for HMAC+canon; **replay-protection nonce test passes** (promoted from A+ per reviewer §grade-rubric-feedback); `TestSubstrate_NonceMismatchRejected` (I5) passes; supersedes cycle-check via Kahn's-sort passes property tests; no UPDATE / DELETE in any substrate caller. | `make check && go test ./internal/orchestrator/state/substrate/...` passes; `go test -run TestSubstrate_ReplayProtection ./...` passes; `go test -run TestSubstrate_NonceMismatchRejected ./...` passes; `go test -run TestSubstrate_SupersedesCycleRejected ./...` passes; `grep -rE '\b(UPDATE|DELETE)\b' internal/orchestrator/state/substrate/` returns zero matches in non-test files. |
| **A** | All B + per-`kind` typed payload validation rejects malformed payloads; Phase-B shadow-write reconciliation cron lands; `TestSubstrate_EventKindEnumMatchesSQLCheck` (N1) passes; `TestSubstrate_NoUpdateDelete` script lints substrate callers via AST (replaces v1's undefined `tools/mutation-test-append-only.sh` per reviewer §grade-rubric-feedback). | `go test -run TestSubstrate_KindValidation ./...` passes; reconciliation cron lands as `internal/orchestrator/substrate_recon/`; AST-lint passes. |
| **A+** | All A + cross-`kind` query lint rejects unfiltered reads (`tools/lint-substrate-queries.go`); partition pruning verified on 10M-row synthetic load (p95 fold ≤ 10ms); Phase-C cutover for `node_output` lands and one MVP-3 wedge consumes substrate as read source-of-truth. | `tools/lint-substrate-queries.go` passes in CI; `go test -run TestSubstrate_LoadP95 -tags=load ./...` reports p95 ≤ 10ms; cutover PR merged for `node_output`. |

---

## 10. Adversarial red-team

Per `feedback_adversarial_review`. Reviewer subagent MUST verify each before Wave 1 dispatches.

1. **Signature replay across runs.** Adversary captures a valid event from run X and replays it into run Y. **Defense:** signature includes `run_id`; cross-run replay fails. `TestSubstrate_CrossRunReplayRejected`.

2. **Double-fold race.** Two readers fold concurrently while a writer appends. **Defense:** sqlite WAL snapshot isolation; appends never block reads. `TestSubstrate_ConcurrentFoldReadSnapshot`.

3. **Supersedes cycle (R7 fix).** Adversary inserts events A→B→A. **Defense:** `AppendEvent` runs Kahn's-sort over the supersedes graph for `(run_id = ?)` inside the insert tx; cycle ⇒ rollback. Property-tested (`cycle_check_property_test.go` extending the work_items pattern).

4. **CAS orphan blob.** OUT OF SCOPE Wave 1 (no blobs). R8 fix recorded in §8.

5. **Tenant leak via blob_digest collision.** OUT OF SCOPE Wave 1. `[substrate-followup]` issue: "tenant-scoped encrypt-before-hash helper — required before W8 multi-tenant cutover."

6. **Policy precedence tie.** OUT OF SCOPE Wave 1 (no policies primitive).

7. **ULID clock skew across processes (I2).** Process A and B mint IDs across clock skew. **Defense:** sort folds by `(written_at, id)`. `written_at` is the substrate's authoritative time. **Cross-process monotonicity** is bounded by sqlite's single-writer model in Wave 1.

8. **Atomic dual-write divergence (R2 — REMOVED).** No dual-write exists in v2. Shadow-write divergence is handled by reconciliation cron (§3 Phase B).

9. **Schema-version bump breaks in-flight verifications (I3).** Forward-version migration recipe in §5 mandates both verifiers ship before writers emit new version. `[substrate-followup]`: first real bump executes the recipe.

10. **Nonce exhaustion.** Adversary spams events with random nonces. **Defense:** writer is authenticated (HMAC); rate-limit at API boundary (W8 RBAC, post-Wave-1).

11. **Reducer override via ACL bypass (R6 fix).** Adversary corrupts budget via duplicate writes. **Defense:** `token_spend` uses `append` reducer; idempotency at `nonce` layer (`UNIQUE(run_id, written_by, nonce)` collision = ErrReplay). Reconciliation events emit `budget_reconciled` (lww), not by rewriting `token_spend`.

12. **Migration rollback.** Production rolls back past 0006. **Defense:** drop `substrate_events`; legacy tables continue functioning (substrate is additive, no legacy table altered). Operator runbook: rollback past 0006 = drop table + restore from snapshot.

13. **Verifier-side keyring TOFU (#13 new).** **Defense:** keyring is read-only post-Setup; lint enforces. `TestKeyringReadOnly`.

14. **Cross-process ULID collision (#14 new).** Two writers on same host generate ULIDs in lockstep. Probability is ~2^-80 per millisecond per pair. **Defense:** PK collision ⇒ `INSERT` fails ⇒ writer retries with fresh ULID. Bounded retry storm via exponential backoff. `[substrate-followup]` issue: "UUIDv7 alternative" (I4).

15. **Reducer-strategy change after rows exist (#15 new).** OUT OF SCOPE Wave 1 (no policy-driven reducer override). When W11 ships override, spec must add re-fold tooling. `[substrate-followup]`.

16. **Fold with concurrent supersedes (#16 new).** Two writers both supersede event E concurrently. Both succeed. Fold head is non-deterministic. **Defense:** Wave 1 hardcodes tiebreaker — `(written_at DESC, id DESC)`. Deterministic given clock + ULID monotonicity.

17. **Decider determinism under concurrent reads (#17 new).** Snapshot isolation level. **Defense:** Snapshot is captured under sqlite WAL read-snapshot isolation; `CELDecider.Decide` MUST begin a read tx, fold, eval, emit verdict, commit — all in one tx. Documented in `CELDecider` godoc.

---

## 11. Sequencing decision: ship AFTER W6, NOT in parallel (addresses R4)

**Defended choice: ship the substrate in MVP-3 Wave 1, AFTER W6 (OTel) lands.** W6 owns:
- Migration `0005_trace_id_columns.sql` (substrate uses 0006).
- The OTel SDK setup + resource-attribute conventions (`regatta.tenant_id`, `trace_id`, `span_id`).
- The `trace_id` columns on `work_items` and `approval_events` that the substrate references.

**Why "after," not "parallel":** the substrate event-log schema embeds `trace_id` + `span_id` columns whose semantics are defined by W6. If W6 changes the trace-id encoding (32 hex vs ULID), the substrate would need a follow-up migration. Shipping substrate AFTER W6 ensures the substrate schema inherits W6's resource-attribute conventions correctly. Also: W9 spec (`2026-06-01-w9-temporal-vs-bespoke-redteam.md`) explicitly states W6 "locks the events table shape the substrate impl reads from" (§1).

**Pre-conditions for Wave 1 dispatch:**
1. W6's T1 (otelsdk setup), T2 (span constants), and T5 (trace_id columns via migration `0005_trace_id_columns.sql`) are merged to main.
2. `BenchmarkAppendEvent` (Task 0) lands clearing ≤ 500 ns/op.

**Wave 1 task count:** 3 file-disjoint subagent tasks (per S1 simplification — events only). See §13.

**Wave 2 (NOT this spec):** W7 (UI) reads substrate; W8 ships `substrate_policies`; W11 ships `substrate_blobs`. Each is a separate spec.

---

## 12. Followup issues (cited per `feedback_unaddressed_load_bearing`)

Each must be filed BEFORE Wave 1 PR opens, with title prefix `[substrate-followup]`. PR body cites issue numbers.

| # | Title | Why deferred | Pre-condition for which wave |
|---|---|---|---|
| F1 | `[substrate-followup]` AppendEvent fast-path for pre-canonicalized payloads | Optimization, not blocker | Wave 2 cost-governor (hot path) |
| F2 | `[substrate-followup]` schema_version v2 migration recipe — first real bump | No v2 exists yet | First kind-payload change after Wave 1 |
| F3 | `[substrate-followup]` tenant_id retag helper for single→multi-tenant cutover | Pre-W8 deployments use 'default' | W8 RBAC cutover |
| F4 | `[substrate-followup]` per-kind TTL cron + archive_audit_outbox | Requires `policies` primitive (deferred) | W11 blackboard wave |
| F5 | `[substrate-followup]` tenant-scoped encrypt-before-hash helper for sensitive payloads | Requires multi-tenant signal | Before W8 multi-tenant cutover |
| F6 | `[substrate-followup]` UUIDv7 vs ULID for post-W9 multi-host deployments | Single-host MVP, ULID is sufficient | Post-W9 multi-host story |
| F7 | `[substrate-followup]` substrate_policies primitive (W8 RBAC) | S1: deferred from Wave 1 | W8 RBAC wedge |
| F8 | `[substrate-followup]` substrate_blobs CAS + ref-counted GC (R8 fix lands here) | S1: deferred from Wave 1 | W11 blackboard wedge |
| F9 | `[substrate-followup]` HumanDecider / VerifierDecider + Decider interface extraction | S5: only CELDecider in Wave 1 | Wave 2 approval-gates / MVP-4 verifier wedge |
| F10 | `[substrate-followup]` reducer-strategy change re-fold tooling | Requires policy-driven override (deferred) | W11 blackboard wedge |

---

## 13. Subagent task split (3 file-disjoint, addresses S1)

Per `feedback_parallel_dispatch` + `feedback_session_limit_dispatch` (cap 3-4 concurrent): Wave 1 ships in **3 file-disjoint subagent tasks**, plus Task 0 as a pre-Wave-1 benchmark gate.

| # | Task | Files | Owner | Depends on |
|---|---|---|---|---|
| **0** | **`BenchmarkAppendEvent` perf gate** (R5) — lands BEFORE Wave 1 PRs open for review | `internal/orchestrator/state/substrate/event_bench_test.go` (skeleton; impl follows in Task 1) | Z | W6 T5 merged |
| 1 | **Substrate schema + storage primitives** (OWNER of `AppendEvent`, `Fold`, signing integration) | `internal/orchestrator/state/substrate/{event,fold,sign,validate,reducer}.go` + `_test.go`; `internal/orchestrator/state/migrations/0006_substrate.sql`; `tools/lint-keyring-readonly.go` | A | W6 T5 (provides 0005); Task 0 benchmark passes |
| 2 | **CELDecider concrete type + gate_verdict emission** | `internal/program/cel_decider.go` + `_test.go`; substrate-side `kind=gate_verdict` payload struct | B | Task 1 (uses `AppendEvent`) |
| 3 | **Query lint + nonce-mismatch test + cycle-check property test + ULID enum-parity test** | `tools/lint-substrate-queries.go`; `internal/orchestrator/state/substrate/{nonce_test,cycle_property_test,enum_parity_test}.go` | C | Task 1 |

Wedge-dossier updates (cost-governor.md, approval-gates.md, conditional-dag.md, blackboard.md) are docs-only PRs; ship as a separate batch after Task 3 lands (per `feedback_review_proportional`).

**Estimated wave count: 1.** Tasks 1, 2, 3 are file-disjoint and dispatch in parallel after Task 0 passes. Task 1 is the spine; Tasks 2 + 3 import from Task 1's PR pre-merge via the `feedback_shared_primitive_owner` pattern.

**Tasks deferred to later waves (NOT this spec's deliverables):**
- Task 4 (Wave 2 of W2): HumanDecider — when approval-gates needs it.
- Task 5 (W8 wedge): `substrate_policies` table + reducer override.
- Task 6 (W11 wedge): `substrate_blobs` CAS + ref-counted GC.
- Task 7 (W11 wedge): TTL cron + archive_audit_outbox.

---

## 14. Open questions for adversarial reviewer (v2)

1. **Phase-B reconciliation cron divergence threshold.** §3 Phase B says divergence opens an audit issue. What's the divergence-percentage threshold? Suggested: ANY non-zero divergence between substrate and legacy after a 60-second settle window opens an issue.

2. **Read-snapshot isolation level for `CELDecider.Decide`.** §10 #17 mandates one tx for fold + eval + verdict-emit. sqlite WAL provides snapshot isolation but only within a single tx — is the implementation pattern documented in `CELDecider` godoc?

3. **Kahn's-sort scope for cycle check.** §8 + §10 #3 restrict the Kahn's-sort to `(run_id = ?)`. Should it ALSO restrict to events with `kind` matching the new event's kind? Or is cross-kind supersedes legitimate? (Current spec: cross-kind supersedes is allowed; reviewer to confirm.)

4. **Task 0 benchmark hardware target.** §7 + Task 0 require 500 ns/op for HMAC+canon. On what hardware? Suggested: GitHub Actions `ubuntu-latest` 4-vCPU x86_64.

5. **Per-kind payload typed structs location.** §2.1 says typed structs live in `internal/orchestrator/state/substrate/`. Should they live in `contracts/schemas/substrate/` for cross-package consumption? (Current spec: in-package; reviewer to confirm scope.)

---

_Spec authority: `feedback_spec_pattern_authority` — implementer subagent deviation from this spec requires re-spawning design subagent. Open questions in §14 must be resolved by adversarial reviewer before Wave 1 dispatches. The 10 followup issues in §12 MUST be filed and cited in the Wave 1 PR body per `feedback_unaddressed_load_bearing` + `feedback_review_before_automerge`._
