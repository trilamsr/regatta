# Spec: MVP-4 W11 Blackboard — typed facts + reducers + CAS blobs

_Author: design subagent, 2026-06-01. Source-of-truth dossier: `memory/wedge_blackboard.md`. Builds on `docs/engineer/specs/2026-06-01-unified-substrate-design.md` (substrate v2 events log shipped via T-S1 #224). Sequencing reference: `memory/wedge_roadmap_assessment.md` MVP-4 row #6._

Blackboard is the shared-typed-fact store that lets parallel subagents publish + query state without prompt bloat. Per the wedge dossier, the primitive is **typed facts + reducers + CAS blobs**. Substrate v2 Wave 1 already shipped `kind=fact` events and the `RegisterPayloadValidator` dispatch — this spec adds three things the substrate deliberately deferred:

1. **CAS blobs primitive** (content-addressed storage keyed by sha256, referenced from facts via the substrate's `blob_digest` forward-fit column).
2. **Fact-kind registry + reducer dispatch** layered over the substrate's existing `kind=fact` event channel — `(topic, schema_version)` tuple-keyed reducer registration at boot.
3. **Fact-subscription API** (`TailFacts`) — polling-backed channel surface for agents to subscribe to a topic without re-folding the whole event log on every read.

Substrate provides the durable, signed, replay-protected fact channel. W11 makes facts **agent-addressable** (subscription) and **payload-bounded** (blobs for the ≥1 KiB tail).

---

## 1. Goal + non-goal

### IN scope (Wave W11)

- **Typed facts** — substrate `kind=fact` events bearing payload `{topic, schema_version, value, blob_refs []string}`. Schema-validated per `(topic, schema_version)` via the substrate's `RegisterPayloadValidator` dispatch (already shipped).
- **Reducers** — pluggable Go funcs registered at boot against `(topic, schema_version)` tuples. Reduce the append-only fact stream into a current-value view. Default reducer: LWW. Operator-defined merge funcs supported (set-union, write-once, custom).
- **CAS blobs primitive** — new `substrate_blobs` table (sqlite BLOB column, sha256 PK). Insert via `substrate.PutBlob`; read via `substrate.GetBlob`. Size cap config-driven (default 1 MiB; hard cap 16 MiB). Referenced from facts via `blob_refs []string` of sha256 digests.
- **Fact-subscription API** — `substrate.TailFacts(ctx, topic, since opaque-cursor) (<-chan Fact, error)`. Polling under the hood. Cursor opaque (rowid encoded).
- **Blob GC** — orphan-sweep job. Blobs unreferenced by any extant fact for >24h are deleted. Operator opt-out per-deployment via config.
- **OTel attribute conventions** — `regatta.blackboard.topic`, `regatta.blackboard.schema_version`, `regatta.blackboard.blob_count`, `regatta.blackboard.subscriber_id` on every fact-write + tail span. Reuses W6 SDK already wired into `AppendEvent`.

### OUT of scope (W11 v1)

- **Realtime push** — v1 is poll-only. No SQLite `LISTEN/NOTIFY` (not supported by sqlite), no goroutine-broadcast fan-out, no websocket. Subscribers poll on a config-driven interval (default 1s, bounded 100ms-10s).
- **Cross-tenant fact sharing** — every fact is scoped to its writer's `tenant_id` (inherited from the substrate event row). Cross-tenant queries are gated by W8 OPA RBAC, NOT by W11. W11 only enforces same-tenant reads via the substrate's existing `tenant_id` filter.
- **Inline fact validation beyond JSON-schema-style typed unmarshal** — semantic validation (e.g. "topic=schema.user_table can only be written by an agent with role=migration-author") is deferred to W8 OPA policies. W11 ships ONLY the typed-unmarshal hook through the substrate's existing `RegisterPayloadValidator`.
- **Fact retraction** — facts are append-only forever (substrate invariant). Supersession is handled by reducers (LWW gives a "latest-wins" effective retraction). No `DELETE FROM substrate_events`.
- **Blob deduplication across tenants** — sha256 is content-keyed but the blob row carries no tenant_id; deduplication is global. Encrypt-before-hash for tenant-sensitive payloads is a `[blackboard-followup]` for W8 multi-tenant cutover (mirrors substrate's F5).
- **Semantic / vector search over facts** — the original `wedge_blackboard.md` proposed `fact.semantic(query, k=5)`. v1 ships exact-topic lookup only; semantic search is a `[blackboard-followup]` requiring an embeddings stack (no proven local-OSS choice yet; out of scope per `feedback_research_design_principles`).

### What got smaller (deletion default per `feedback_deletion_default`)

This spec **deletes from the original `wedge_blackboard.md` data model** rather than adding:

- **Bespoke `facts` table removed** — substrate v2 already provides the append-only signed event log shape. W11 reuses `substrate_events WHERE kind='fact'` verbatim. **Saves: one table (`facts`), one migration, the `supersedes` FK, the `signed-by` HMAC column, the per-fact nonce, the cycle-check** — all already shipped by substrate T-S1 #224.
- **Bespoke `signature` + `written_by` columns removed** — substrate signs every event; W11 inherits the signing for free.
- **Bespoke `tenant_id` propagation removed** — substrate's `tenant_id` column carries forward.
- **Bespoke `TTL_at` column removed** — facts are append-only forever. Reducer-driven LWW gives effective expiry via supersession. No per-fact TTL field.
- **Bespoke `tags_json` column + index removed** — topics carry the same naming-convention burden (`"schema.user_table"`, `"files.touched"`) without an extra index. Tag-faceted queries land if/when a real use case forces it.
- **Bespoke read API (`fact.get`/`fact.list`/`fact.semantic`) collapsed to two** — `GetFact(topic)` (latest reducer output) + `TailFacts(topic, since)` (stream). Drop `fact.list(tag=...)` and `fact.semantic(query, k)` until a real consumer forces them.

Net: W11 adds **one new table (`substrate_blobs`)**, **one new package (`internal/orchestrator/blackboard/`)**, **one new migration (`0008_blackboard_blobs.sql`)**. Everything else reuses substrate primitives that already shipped.

> **Migration number note:** Migration #0007 was reserved for W8 `policy_revision` per amendment PR #311 (in flight). W11 takes #0008. See §3.3 + §9 pre-conditions.

---

## 2. In / Out — table form

| Capability | W11 ships | Why / deferred to |
|---|---|---|
| `kind=fact` event channel | Already shipped by substrate T-S1 | (n/a — substrate done) |
| Typed-payload validation per `(topic, schema_version)` | YES — registers `FactPayload` validator via substrate's `RegisterPayloadValidator(KindFact, ...)` | (n/a) |
| Reducer registry `(topic, schema_version) → ReducerFn` | YES — new `internal/orchestrator/blackboard/registry.go` | (n/a) |
| `GetFact(ctx, topic)` — latest reducer output | YES — folds `substrate_events WHERE kind='fact' AND key=$topic`, applies registered reducer | (n/a) |
| `TailFacts(ctx, topic, since cursor) (<-chan Fact, error)` | YES — polling under the hood | Realtime push deferred — v1 polls |
| Polling interval config | YES — `regatta.yaml: blackboard.tail_interval_ms` (default 1000, bounded 100-10000) | (n/a) |
| `PutBlob(ctx, tx, content) (sha256, error)` | YES — sqlite BLOB column, sha256 PK, 1 MiB default cap | (n/a) |
| `GetBlob(ctx, tx, sha256) ([]byte, error)` | YES | (n/a) |
| Blob size cap | YES — config-driven (`blackboard.blob_size_max_bytes`, default 1 MiB, hard cap 16 MiB) | Larger ⇒ deferred to S3 adapter (`[blackboard-followup]` F4) |
| Blob orphan-GC sweep job | YES — `internal/orchestrator/blackboard_gc/`, 24h grace, opt-out per deployment | (n/a) |
| OTel attributes on fact-write + tail | YES — `regatta.blackboard.topic`, `regatta.blackboard.schema_version`, `regatta.blackboard.blob_count`, `regatta.blackboard.subscriber_id` | (n/a) |
| Cross-tenant fact reads | NO | W8 OPA gates |
| Realtime push (SSE / websocket / LISTEN-NOTIFY) | NO | `[blackboard-followup]` F1; v2 if profiling shows polling waste |
| Tag-faceted queries | NO | `[blackboard-followup]` F2 if real use case lands |
| Semantic / vector search | NO | `[blackboard-followup]` F3 — needs embeddings stack |
| S3 / external blob backend | NO | `[blackboard-followup]` F4 — needs first ≥16 MiB blob ask |
| Fact retraction / DELETE | NO | Append-only invariant from substrate |
| Tenant-scoped encrypt-before-hash | NO | `[blackboard-followup]` F5 — mirrors substrate F5 |

---

## 3. Architecture

### 3.1 Fact = `substrate_events` row, no new table

A fact is one `substrate_events` row with `kind='fact'`. The `key` column holds the **topic** (namespaced string, e.g. `"schema.user_table"`, `"files.touched"`). The `payload_json` column holds:

```json
{
  "topic":          "<same as event.key — denormalized for read parity>",
  "schema_version": <int>,
  "value":          <topic-typed JSON value>,
  "blob_refs":      ["<sha256-hex>", ...]
}
```

**Typed validator** registers via the substrate's existing dispatch (no new code path):

```go
// internal/orchestrator/blackboard/payload.go
package blackboard

import (
    "encoding/json"
    "github.com/regatta-ai/regatta/internal/orchestrator/state/substrate"
)

type FactPayload struct {
    Topic         string          `json:"topic"`
    SchemaVersion int             `json:"schema_version"`
    Value         json.RawMessage `json:"value"`
    BlobRefs      []string        `json:"blob_refs,omitempty"`
}

func init() {
    substrate.RegisterPayloadValidator(substrate.KindFact, validateFact)
}

func validateFact(raw json.RawMessage) error {
    var p FactPayload
    if err := json.Unmarshal(raw, &p); err != nil {
        return substrate.ErrInvalidPayload
    }
    if p.Topic == "" {
        return substrate.ErrInvalidPayload
    }
    if p.SchemaVersion < 1 {
        return substrate.ErrInvalidPayload
    }
    for _, ref := range p.BlobRefs {
        if len(ref) != 64 { // sha256 hex is 64 chars
            return substrate.ErrInvalidPayload
        }
    }
    return nil
}
```

**Write path**: `blackboard.PutFact(ctx, tx, topic, schemaVersion, value, blobRefs)` constructs the `FactPayload`, sets `event.Key = topic`, calls `substrate.AppendEvent`. No new SQL primitive. The substrate's signing, replay-protection, supersedes-cycle-check, tenant_id propagation, and trace_id population all carry forward.

**`event.key`-as-topic denormalization** — the topic is stored in BOTH `event.key` (indexed; substrate's existing `idx_substrate_events_kind` covers `(run_id, kind, key, written_at DESC)`) AND `event.payload_json.topic` (for read parity). The validator asserts equality. Indexed lookups by topic are free; payload parsing is only on read.

### 3.2 Reducer = `(topic, schema_version) → ReducerFn` registry

Per the wedge dossier's "typed regions + reducers per key (LangGraph)" lesson, reducer is a property of the topic (not a column on the row). v1 ships ONE registry, registered at boot.

```go
// internal/orchestrator/blackboard/registry.go
package blackboard

import (
    "context"
    "github.com/regatta-ai/regatta/internal/orchestrator/state/substrate"
)

// ReducerFn folds a slice of facts (oldest → newest) into a current value.
// Implementations MUST be deterministic + pure (no I/O, no time).
type ReducerFn func(facts []Fact) (json.RawMessage, error)

// Fact is the typed projection of a substrate_events row with kind='fact'.
type Fact struct {
    EventID       string          // ULID
    RunID         string
    WorkItemID    string
    TenantID      string
    Topic         string
    SchemaVersion int
    Value         json.RawMessage
    BlobRefs      []string
    WrittenBy     string
    WrittenAt     int64
}

// Registry is the (topic, schema_version) → reducer table. Populated at boot
// via Register; sealed by Seal() called from Setup. Read-only after Seal.
type Registry struct {
    mu     sync.RWMutex
    sealed bool
    fns    map[registryKey]ReducerFn
}

type registryKey struct {
    Topic         string
    SchemaVersion int
}

// Register associates a reducer with (topic, schemaVersion). Must be called
// before Seal. Calling Register after Seal panics. Calling Register twice
// for the same (topic, schemaVersion) panics (no silent overwrite).
func (r *Registry) Register(topic string, schemaVersion int, fn ReducerFn) { /* … */ }

// Seal locks the registry. Called from Setup after all registrations.
func (r *Registry) Seal() { /* … */ }

// Resolve returns the reducer for (topic, schemaVersion). If none registered,
// returns the default LWW reducer (latest fact by (written_at, event_id) wins).
func (r *Registry) Resolve(topic string, schemaVersion int) ReducerFn { /* … */ }

// DefaultLWW is the fallback reducer when no explicit registration exists.
// Returns the value field of the latest fact (sorted by (written_at, event_id)).
// Returns nil + ErrNoFacts if facts is empty.
func DefaultLWW(facts []Fact) (json.RawMessage, error) { /* … */ }
```

**Why `(topic, schema_version)` key (not just `topic`):** schema migrations are inevitable. Bumping `schema_version` lets v1 + v2 reducers coexist during the migration window. The reducer for `("schema.user_table", 1)` may differ from `("schema.user_table", 2)` — e.g. v2 introduces a new merge strategy or normalizes value shape.

**Why sealed at boot, not mutable at runtime:** per the substrate's keyring-readonly pattern (`tools/lint-keyring-readonly`), runtime mutation of a security/correctness-critical registry is a foot-gun. Reducers determine the read-side answer; runtime mutation = silent data behavior change. Boot-time registration mirrors `sql.Register` + `image.RegisterFormat` from the std lib.

**Built-in reducers** ship in `internal/orchestrator/blackboard/reducers/`:
- `LWW` (default) — latest by `(written_at, event_id)`.
- `SetUnion` — for facts whose value is a JSON array; union of all elements across facts.
- `WriteOnce` — first fact wins; later facts are ignored (with optional log warning).
- `Append` — return the full ordered list (for use-cases where consumers want history).

Operator-defined reducers register via the same `Register` API.

### 3.3 CAS blobs primitive

A new table `substrate_blobs` (sqlite BLOB column, sha256 PK):

```sql
-- 0008_blackboard_blobs.sql
-- W11 blackboard CAS primitive. Owned by substrate package per spec §3.3.
-- Migration #0008 reserved per docs/engineer/specs/2026-06-01-w11-blackboard-design.md.
-- Migration #0007 owned by W8 policy_revision per amendment PR #311; W11 takes #0008.

CREATE TABLE substrate_blobs (
    digest        TEXT    NOT NULL PRIMARY KEY,         -- sha256 hex (64 chars)
    bytes         BLOB    NOT NULL,
    size_bytes    INTEGER NOT NULL,
    content_type  TEXT    NOT NULL DEFAULT 'application/octet-stream',
    created_at    INTEGER NOT NULL,                      -- unix ms UTC; orphan-GC grace anchor
    CHECK (length(digest) = 64
           AND digest NOT GLOB '*[^0-9a-f]*'),           -- lower-case sha256 hex only
    CHECK (size_bytes > 0 AND size_bytes <= 16777216)    -- hard cap 16 MiB
);

CREATE INDEX idx_substrate_blobs_created_at
    ON substrate_blobs(created_at);
```

**Why sqlite BLOB column, not S3 / external store:** per `feedback_research_design_principles` — substrate already ships with sqlite. Adding an S3 dependency for ≤1 MiB payloads is build-cost the wedge doesn't need. The schema's hard cap (16 MiB) keeps any single blob below sqlite's per-row recommended ceiling (~1 GiB; SQLITE_MAX_LENGTH; we leave 60× headroom). External-store adapter is `[blackboard-followup]` F4 once a real consumer needs >16 MiB.

**Go API (lives in `internal/orchestrator/state/substrate/blob.go`** — substrate owns the table because the substrate package owns `0008_blackboard_blobs.sql` per `feedback_migration_number_lock`):**

```go
package substrate

// PutBlob inserts content into substrate_blobs keyed by sha256(content).
// If the digest already exists, PutBlob is a no-op and returns the digest
// (content-addressed semantics — same bytes ⇒ same digest ⇒ idempotent).
// size_max is the per-deployment hard cap (config-driven; default 1 MiB; CHECK
// constraint hard-fails > 16 MiB at the SQL layer).
//
// Caller owns the *sql.Tx; PutBlob does NOT begin / commit.
func PutBlob(ctx context.Context, tx *sql.Tx, content []byte, sizeMax int) (digest string, err error)

// GetBlob reads the BLOB by digest. Returns ErrBlobNotFound if absent.
// Caller MUST verify the returned bytes hash to the expected digest if
// integrity matters (CAS is content-addressed; the row's PK is the digest,
// but defense-in-depth — re-hash on read for security-critical paths).
func GetBlob(ctx context.Context, tx *sql.Tx, digest string) ([]byte, error)

// ErrBlobNotFound is returned by GetBlob when no row matches.
var ErrBlobNotFound = errors.New("substrate: blob not found")

// ErrBlobTooLarge is returned by PutBlob when len(content) > sizeMax.
var ErrBlobTooLarge = errors.New("substrate: blob exceeds size cap")
```

**Config (`regatta.yaml`):**

```yaml
blackboard:
  blob_size_max_bytes: 1048576    # 1 MiB; clamped to [1, 16777216]
  tail_interval_ms:    1000       # 1s; clamped to [100, 10000]
  blob_gc_enabled:     true       # opt-out: false to disable orphan sweep
  blob_gc_grace_secs:  86400      # 24h; minimum 3600 (1h)
```

`PutBlob` reads the cap from the per-process `blackboard.Config` snapshot (loaded at `Setup`); see §3.6 for config lifecycle. The 16 MiB SQL CHECK is the **floor**, the config cap is the **ceiling**; ops can lower but never raise above 16 MiB without a migration.

### 3.4 Subscription API — `TailFacts`

```go
// internal/orchestrator/blackboard/tail.go
package blackboard

// TailFacts streams facts for a topic, starting AFTER the given cursor.
// since="" means "tail from now" (subscribe to future facts only).
// since="<cursor>" means "replay from this position forward, then continue tailing".
//
// Cursor is opaque (currently encodes substrate_events.rowid; do not parse).
// The returned channel closes when ctx is cancelled.
//
// Polling under the hood: SELECT … WHERE rowid > $cursor AND kind='fact' AND key=$topic
// every blackboard.tail_interval_ms (config-driven; default 1s). v1 has no LISTEN/NOTIFY.
//
// Errors are surfaced through a separate err channel (Go pattern: dual-channel
// for stream + terminal-error). The returned `errc` closes after `factc` closes.
func TailFacts(ctx context.Context, db *sql.DB, topic string, since Cursor) (factc <-chan Fact, errc <-chan error, err error)

// Cursor is opaque — exported as a type-aliased string so callers can persist it
// but cannot meaningfully construct one outside this package.
type Cursor string

// CursorEmpty represents "tail from now". TailFacts(... CursorEmpty ...) only
// streams facts inserted after the call.
const CursorEmpty Cursor = ""
```

**Polling design (the load-bearing v1 choice):**

```
poll_loop:
  for each tick (every tail_interval_ms):
    rows := SELECT id, run_id, work_item_id, tenant_id, key, payload_json,
                   written_by, written_at
            FROM substrate_events
            WHERE kind='fact'
              AND key = $topic
              AND rowid > $cursor
              AND tenant_id = $caller_tenant     -- prevents cross-tenant leak per W8 forward-fit
            ORDER BY rowid ASC
            LIMIT 100;                            -- bounded batch to keep tick fast
    for each row:
      cursor = max(cursor, rowid)
      factc <- decode(row)
    if ctx.Done(): break poll_loop
  close(factc)
  close(errc)
```

**Why polling, not LISTEN/NOTIFY:** sqlite has no native notify channel. Goroutine-broadcast (e.g. `sync.Cond`) would couple every writer to every reader process-wide and break the substrate's package-isolation. Polling is the **boring choice** that ships in v1; if profiling under real load shows >5% CPU spent in tail SELECTs, we revisit (`[blackboard-followup]` F1).

**Why bounded batch (LIMIT 100):** a backlogged subscriber rejoining after hours could otherwise yank 10K rows in one tick, blocking the channel + starving other subscribers. 100 is conservative — a 1000-rps fact stream produces 1000 rows/sec total across all subscribers, so a single-topic subscriber typically sees <10 rows/tick.

**Cursor opacity:** v1 encodes rowid. If we ever swap the backing query (e.g. by-`written_at` for cross-shard correctness), persisted cursors break unless they were opaque. Exporting `Cursor` as `type Cursor string` (with no exported constructors except `CursorEmpty`) keeps callers honest.

### 3.5 Fact GC + Blob GC

**Facts: append-only forever.** Substrate invariant; no `DELETE FROM substrate_events`. Reducer-driven LWW gives effective expiry — old facts remain in the log but are not visible through `GetFact`. Per-kind TTL+archival is `substrate-followup` F4 (cross-substrate concern, deferred from substrate Wave 1).

**Blobs: orphan-sweep GC.** A blob is **orphan** if no extant fact references its digest in `payload_json.blob_refs`. Orphans older than `blob_gc_grace_secs` (default 24h) are deleted.

```
-- internal/orchestrator/blackboard_gc/sweep.go
-- Runs in a separate goroutine started at Setup. Tick interval = blob_gc_grace_secs / 24
-- (default: hourly tick over 24h grace ⇒ a blob gets ~24 chances to be re-referenced
-- before deletion).

SELECT digest FROM substrate_blobs
WHERE created_at < $now_ms - $grace_secs * 1000
  AND digest NOT IN (
    SELECT DISTINCT json_extract(value, '$')
    FROM substrate_events,
         json_each(json_extract(payload_json, '$.blob_refs'))
    WHERE kind = 'fact'
      AND payload_json LIKE '%blob_refs%'
  )
LIMIT 100;
-- Delete each in its own short tx; bounded batch.
```

**Why mark-and-sweep, not ref-counting:** ref-counting requires every fact-write to bump a counter; that's an extra row mutation that the substrate's append-only contract forbids. Mark-and-sweep reads the fact log to compute live set; the `NOT IN` subquery is bounded by the substrate's `idx_substrate_events_kind` (kind='fact' partition). Sweep runs hourly + deletes in batches of 100 — a hostile or buggy producer cannot OOM the sweep.

**GC race defense (mirrors substrate R8 fix):** A blob inserted at T=0 might be referenced by a fact inserted at T=0+ε in the same producer tx, but the sweep query sees the blob FIRST (before the fact commits). The `grace_secs` window (default 24h) makes this race effectively impossible — sweep never deletes a blob whose `created_at > now_ms - grace_secs * 1000`. Even with the worst-case `grace_secs=3600` (1 hour, the minimum), a producer must take >1h between `PutBlob` and the referencing `PutFact` to lose the race. Such producers are buggy; the design rejects them.

**Operator opt-out:** `blackboard.blob_gc_enabled: false` disables the sweep goroutine entirely (e.g. for forensic deployments where blob history is part of audit). Documented in operator runbook.

### 3.6 Config lifecycle + tenant_id propagation

Config loads at `Setup`. After Setup, the config snapshot is read-only — same discipline as substrate's keyring. `tools/lint-blackboard-config-readonly.go` rejects mutation calls outside `Setup` (mirrors `tools/lint-keyring-readonly`).

`tenant_id` propagation: every `PutFact` call sets `event.TenantID = caller.TenantID`. `TailFacts` filters by `tenant_id = caller.TenantID`. Cross-tenant reads are impossible at the W11 API — even with a leaked cursor, the SELECT clause enforces tenant isolation. W8 OPA RBAC layers ABOVE this (e.g. denying `topic=schema.*` reads to non-migration roles).

---

## 4. Existing patterns reused (no bespoke invention)

Per `memory/feedback_research_design_principles`: every primitive cites a proven source. Where two systems collide, the rationale is documented.

| Primitive | Adopted from | What we take | Why not bespoke |
|---|---|---|---|
| Append-only signed fact log | Substrate v2 `substrate_events` (T-S1 #224) | All of it: ULID + chain + HMAC + nonce + tenant_id + supersedes + Kahn's-cycle-check | Already shipped; reinventing would duplicate 12 named tests + 5 indexes + 1 unique constraint |
| Typed payload validator dispatch | Substrate `RegisterPayloadValidator` (T-S1 §2.3) | `RegisterPayloadValidator(KindFact, validateFact)` registers from W11's `init()` | Substrate ships the dispatch pattern; W11 plugs in |
| Reducer-per-channel | [LangGraph reducers](https://langchain-ai.github.io/langgraph/concepts/low_level/#reducers) | `(topic, schema_version) → ReducerFn` map; sealed-at-boot | LangGraph proved typed reducers in production multi-agent graphs |
| Content-addressed BLOB storage | [Bazel CAS](https://bazel.build/remote/caching) + [Nix store](https://nixos.org/guides/nix-pills/nix-store-paths.html) + Git objects | sha256 PK, content-keyed dedup, integrity-on-read | Three proven systems, identical model |
| sqlite BLOB column | sqlite manual §[blob](https://www.sqlite.org/datatype3.html) | `bytes BLOB NOT NULL` + 16 MiB CHECK + index on `created_at` | sqlite already ships with substrate; no new dep |
| Boot-sealed registry | std lib `sql.Register`, `image.RegisterFormat`, `hash.Register*` | `Register(...)` panics after `Seal()`; no runtime mutation | Go stdlib pattern; reviewers + linters already familiar |
| Polling-based stream API | [Postgres LISTEN/NOTIFY fallback in pgx](https://pkg.go.dev/github.com/jackc/pgx/v5#Conn.WaitForNotification) (degraded mode) + Kafka consumer polling | `SELECT … WHERE rowid > $cursor` per tick; opaque cursor | Polling is the boring sqlite-compatible choice; matches Kafka semantics for cursor persistence |
| Mark-and-sweep GC with grace window | [Bazel remote-cache GC](https://github.com/buchgr/bazel-remote/blob/master/doc/gc.md) (`gcInterval` + LRU eviction); [Cassandra tombstone gc_grace_seconds](https://cassandra.apache.org/doc/latest/cassandra/operating/compaction/index.html#tombstone-gc) | `gc_grace_seconds` window prevents writer-vs-sweeper race | Both systems ship the identical knob shape at OSS scale; reinventing the timer + grace semantics has no upside |
| Cursor opacity (type-aliased string) | [GraphQL Relay cursor spec](https://relay.dev/graphql/connections.htm#sec-Cursor) + [AWS pagination tokens](https://docs.aws.amazon.com/AmazonECS/latest/APIReference/API_ListClusters.html#ECS-ListClusters-request-nextToken) | Opaque `Cursor` type; no constructors except `CursorEmpty` | Two industry-standard patterns; protects against backing-query swap |
| OTel attribute conventions | W6 GenAI semconv (`gen_ai.*`) + substrate's `regatta.*` prefix | `regatta.blackboard.topic`, `regatta.blackboard.schema_version`, `regatta.blackboard.blob_count`, `regatta.blackboard.subscriber_id` | Matches W6 namespace; reviewer can scan one prefix |

**Rejected alternatives (defended):**

- **CRDT store (e.g. Yjs, Automerge)** — overkill. v1 has no multi-master replication requirement; sqlite is single-writer.
- **Postgres LISTEN/NOTIFY for fan-out** — sqlite has no equivalent; would force a database migration. Out of scope.
- **Embedded Kafka / Redis streams** — cost-prohibitive for a single-host MVP-4 deployment; adds two operational dependencies for one streaming API.
- **Bespoke "facts" table** — substrate already ships the shape; reusing saves a migration + 5 indexes + the HMAC pipeline.

---

## 5. Risk register (R1-R10)

Per `feedback_adversarial_review`. Reviewer subagent MUST verify each before W11 dispatches.

| # | Risk | Defense |
|---|---|---|
| **R1** | **Blob storage growth unbounded.** Hostile or buggy producer floods `PutBlob` with random 1 MiB payloads ⇒ disk fills before GC sweeps. | Per-deployment `blob_size_max_bytes` cap (default 1 MiB). Hard SQL CHECK at 16 MiB. Rate-limit at the W8 RBAC API boundary (deferred to W8). Operator can lower `blob_size_max_bytes` to 0 to disable blob writes entirely. **No protection against a malicious authenticated writer pre-W8** — `[blackboard-followup]` F6 (rate-limit). |
| **R2** | **Schema version skew across processes.** Process A registers reducer for `(topic, 1)`, process B registers reducer for `(topic, 2)`. Writers emit v2 while a reader process only knows v1. Reader's `Resolve(topic, 2)` falls through to `DefaultLWW`, silently returning wrong shape. | (a) Registry exposes `Topics() map[string][]int` — caller can assert expected `(topic, schema_version)` tuples are registered at boot, panic if missing. (b) Forward-version migration recipe (mirrors substrate I3): ship readers with v1+v2 reducers BOTH registered for one release cycle before writers emit v2. (c) Test `TestBlackboard_RegistryAssertsAllVersions` exercises a boot-time `Registry.MustHave(topic, schemaVersion)` assertion. |
| **R3** | **Reducer cycle / infinite loop.** Operator-defined reducer calls back into `GetFact` recursively. | Reducers are pure: signature `ReducerFn func(facts []Fact) (json.RawMessage, error)` — no DB handle, no context, no I/O. The signature itself prevents reentrancy. `tools/lint-blackboard-reducer-purity.go` (new) — AST-checks that any function registered via `Registry.Register` does NOT reference `*sql.DB`, `*sql.Tx`, `os.`, `time.Now`, or `net.`. CI gate. |
| **R4** | **GC race — blob deleted before fact lands.** Producer calls `PutBlob` at T=0; GC sweep sees orphan at T=ε before producer's `PutFact` commits ⇒ blob deleted; subsequent `GetBlob` fails. | `blob_gc_grace_secs` window (default 86400s = 24h; minimum 3600s = 1h). Sweep query filters `WHERE created_at < $now_ms - $grace_secs * 1000` — blobs younger than the grace window are immune. Producer must `PutFact` within 24h of `PutBlob`. Buggy/lazy producers that take longer are out of scope; tracked as a config-level operator decision. `TestBlackboard_GCSpareesYoungOrphans` exercises a fact-write at T=23h59m + a sweep at T=24h ⇒ blob survives. |
| **R5** | **Subscription leak.** A subagent calls `TailFacts` then crashes / disconnects without cancelling ctx ⇒ goroutine + sql query loop forever. | `TailFacts` MUST take `ctx` as first arg (Go convention). Caller's ctx-cancel propagates. Defensive: `tail.go` registers a watchdog that closes the channel after `subscriber_idle_max_secs` (config, default 3600s) of zero received facts AND no consumer pulls. `tools/lint-blackboard-tail-ctx.go` (new) — AST-checks every `TailFacts` call site passes a cancellable ctx. CI gate. |
| **R6** | **Cursor staleness / out-of-order.** Substrate `rowid` is monotonic for the local writer but a future multi-writer story (W9 Temporal-backed) could break monotonicity. Persisted cursors would silently skip facts. | Cursor is opaque `type Cursor string` with no exported constructors. The encoding (currently rowid) is implementation-private. Document operator-runbook entry: "do not persist W11 cursors across major-version upgrades." `[blackboard-followup]` F7 (cursor-versioning) — required before W9 multi-writer cutover. |
| **R7** | **Reducer non-determinism.** Operator-defined reducer reads `time.Now()` or `rand.Int()` ⇒ different reader processes return different values for the same fact set ⇒ blackboard becomes non-deterministic for downstream consumers (CEL decider, replay, etc.). | Reducer purity lint (see R3). Plus: `TestBlackboard_ReducerDeterminism` runs every registered reducer 100× on the same input, asserts identical output. Failure ⇒ reducer is buggy; reviewer rejects. |
| **R8** | **Topic namespace collision.** Two unrelated wedges both write `"files.touched"` with different value shapes ⇒ reader confused. | Topic naming convention: `<owner>.<noun>` (e.g. `schema.user_table`, `migrate.files_touched`, `cost.spend_usd`). Operator runbook entry. `tools/lint-blackboard-topics.go` (new — A+-tier) enforces the prefix list in `regatta.yaml: blackboard.topic_prefixes`. Unknown prefix ⇒ write rejected at `validateFact` (deferred to A+ — B/A ship without lint, runbook is the v1 gate). |
| **R9** | **`json_each` performance.** GC sweep's `SELECT … FROM substrate_events, json_each(...)` is O(N) over the fact table on every tick. For 1M facts, this is a multi-second scan. | (a) Sweep query restricts to `kind='fact' AND payload_json LIKE '%blob_refs%'` — eliminates the 99% of facts without blob refs. (b) Sweep batches `LIMIT 100` orphans + deletes one-at-a-time in short txs. (c) Sweep runs once per hour by default — even a 10s scan is 0.3% of the hour. (d) `[blackboard-followup]` F8: materialized `blob_refs` index table if profiling shows the scan is the bottleneck. |
| **R10** | **CAS digest collision (sha256).** Two distinct contents hash to the same digest ⇒ `PutBlob` returns the same digest for content A and content B; `GetBlob` returns whichever landed first. Subsequent reads of "content B" return content A — silent data corruption. | sha256 collision probability is ~2^-256 per pair. Per the [academic consensus](https://www.iacr.org/cryptodb/data/conf.php?venue=tcc), this is effectively impossible. **Stronger defense:** `GetBlob` callers who need integrity assurance MUST re-hash the returned bytes (documented in godoc). `[blackboard-followup]` F9 — sha3-256 migration path if sha256 ever weakens. |

---

## 6. Test plan per task (B / A / A+ — tool-checkable per `feedback_grade_rubric`)

### T1 — Blobs primitive + migration 0008

**B-tier:**
- `TestMigration0008_AppliesAndCreatesSchema` — fresh DB → migrate → `substrate_blobs` table + index present; schema-version bump 7 → 8.
- `TestBlackboard_PutBlobRoundTrip` — `PutBlob(content)` returns sha256(content); `GetBlob(digest)` returns content.
- `TestBlackboard_PutBlobIdempotent` — same content twice ⇒ same digest; no UNIQUE-collision error; row count unchanged.
- `TestBlackboard_PutBlobRejectsOversize` — content > `sizeMax` ⇒ `ErrBlobTooLarge`; no row written.
- `TestBlackboard_GetBlobNotFound` — unknown digest ⇒ `ErrBlobNotFound`.

**A-tier:**
- `TestBlackboard_PutBlobRejectsAboveHardCap` — content > 16 MiB ⇒ SQL CHECK fires (defense-in-depth even if config raised).
- `TestBlackboard_DigestIsLowerCaseHex` — `PutBlob` returns lowercased hex; SQL CHECK enforces.

### T2 — Fact-kind registry + reducer dispatch

**B-tier:**
- `TestBlackboard_PutFactRoundTrip` — `PutFact(topic="files.touched", v1, value, nil)` writes substrate event with `kind='fact'`, `key=topic`; `GetFact(topic)` returns value via `DefaultLWW`.
- `TestBlackboard_RegistryRegisterAndResolve` — `Register("schema.t", 1, fn)` then `Resolve("schema.t", 1)` returns fn; `Resolve("schema.t", 2)` returns `DefaultLWW`.
- `TestBlackboard_RegistrySealRejectsLateRegister` — `Register` after `Seal` panics.
- `TestBlackboard_RegistryDuplicateRegisterPanics` — same `(topic, schemaVersion)` registered twice ⇒ panic.
- `TestBlackboard_FactPayloadValidator_RejectsEmptyTopic` — `validateFact` returns `ErrInvalidPayload` for empty topic.
- `TestBlackboard_FactPayloadValidator_RejectsBadBlobRef` — blob_ref with non-sha256-length ⇒ `ErrInvalidPayload`.

**A-tier:**
- `TestBlackboard_ReducerDeterminism` (R7) — every registered reducer run 100× on the same input; asserts identical output.
- `TestBlackboard_RegistryAssertsAllVersions` (R2) — `Registry.MustHave(topic, schemaVersion)` panics at boot if expected tuple is unregistered.
- `TestBlackboard_BuiltinReducers_SetUnion_WriteOnce_Append` — each built-in reducer exercised on a 5-fact sequence.

**A+-tier:**
- `TestBlackboard_ReducerPurityLint` — `tools/lint-blackboard-reducer-purity` rejects fixtures that call `time.Now`, `*sql.DB`, `os.*`, etc.

### T3 — TailFacts API + cursor semantics

**B-tier:**
- `TestBlackboard_TailFactsStreamsNewFacts` — `TailFacts(topic, CursorEmpty)` returns a channel; writes after the call land on the channel.
- `TestBlackboard_TailFactsCursorReplaysFromPosition` — write 3 facts; tail with cursor pointing before fact #2; stream emits #2 and #3 (not #1).
- `TestBlackboard_TailFactsCtxCancelClosesChannel` — cancel ctx ⇒ both channels close.
- `TestBlackboard_TailFactsFiltersByTopic` — writer emits topic A + B; tail for A only sees A.
- `TestBlackboard_TailFactsFiltersByTenant` — cross-tenant write ⇒ tail filtered by caller's tenant_id does NOT see it.

**A-tier:**
- `TestBlackboard_TailFactsBoundedBatch` — 1000 backlogged facts; first tick emits ≤100; subsequent ticks emit the rest.
- `TestBlackboard_TailFactsRespectsTailInterval` — interval=200ms; assert ticks happen at 200ms ± 50ms.
- `TestBlackboard_TailFactsCtxNoLeak` (R5) — N=100 ctx-cancelled `TailFacts` calls; goroutine count returns to baseline within 2s.

**A+-tier:**
- `TestBlackboard_TailCtxLintIntegrationCI` — `tools/lint-blackboard-tail-ctx` against full repo; asserts every `TailFacts` call site uses cancellable ctx.

### T4 — Blob GC sweep

**B-tier:**
- `TestBlackboard_GCSweepsOrphans` — insert blob, never reference; advance fake-clock past `gc_grace_secs`; sweep deletes.
- `TestBlackboard_GCSparesReferencedBlobs` — insert blob, write fact referencing digest; sweep does NOT delete.
- `TestBlackboard_GCSpareesYoungOrphans` (R4) — orphan blob with `created_at = now - grace + 1s` ⇒ sweep does NOT delete (grace window).
- `TestBlackboard_GCOptOut` — `blob_gc_enabled=false` ⇒ sweep goroutine not started; orphans survive forever.

**A-tier:**
- `TestBlackboard_GCBatchSize` — 500 orphans; first sweep deletes 100; next sweep deletes another 100 (bounded batch).
- `TestBlackboard_GCSurvivesPanicMidSweep` — induce a panic in the sweep loop; sweep goroutine recovers, logs, continues next tick.

### T5 — OTel attrs + docs

**B-tier:**
- `TestBlackboard_OTelSpanAttrs_PutFact` — fact-write inside an active span ⇒ span carries `regatta.blackboard.topic`, `regatta.blackboard.schema_version`, `regatta.blackboard.blob_count`.
- `TestBlackboard_OTelSpanAttrs_TailFacts` — `TailFacts` opens a span carrying `regatta.blackboard.subscriber_id` + `regatta.blackboard.topic`.

**A-tier:**
- `TestBlackboard_OperatorRunbookSectionsPresent` — grep-based test asserts `docs/operator/blackboard.md` has the required runbook sections (topic naming, GC opt-out, schema-version migration recipe).

---

## 7. Grade rubric

| Tier | Criterion | Tool check |
|---|---|---|
| **B** | `substrate_blobs` migration ships under `0008_blackboard_blobs.sql`; `PutBlob` + `GetBlob` + `PutFact` + `GetFact` + `TailFacts` shipped; `Registry.Register/Seal/Resolve` shipped; built-in `DefaultLWW` reducer ships; orphan-GC sweep job ships with opt-out; OTel attrs on fact-write + tail spans; **all B-tier tests in §6 pass**; no UPDATE/DELETE in blackboard package except the GC sweep's bounded DELETE on `substrate_blobs`. | `make check && go test ./internal/orchestrator/blackboard/... ./internal/orchestrator/blackboard_gc/... ./internal/orchestrator/state/substrate/...` passes; `grep -rE '\b(UPDATE\|DELETE)\b' internal/orchestrator/blackboard/` returns matches only in `_test.go` + the GC sweep file; schema-version 7 → 8. |
| **A** | All B + reducer-determinism test (R7) passes; schema-version-skew assertion test (R2) passes; bounded-batch test for tail + GC passes; ctx-cancel-no-leak test (R5) passes; operator runbook (`docs/operator/blackboard.md`) lands with topic-naming + GC opt-out + schema-version migration sections; built-in reducers `SetUnion`, `WriteOnce`, `Append` ship + tested. | A-tier tests pass; `docs/operator/blackboard.md` exists + `TestBlackboard_OperatorRunbookSectionsPresent` passes; `go test -run TestBlackboard_TailFactsCtxNoLeak ./...` passes. |
| **A+** | All A + `tools/lint-blackboard-reducer-purity` rejects impure reducers in CI; `tools/lint-blackboard-tail-ctx` enforces cancellable-ctx at call sites; `tools/lint-blackboard-topics` enforces prefix list; 1M-fact synthetic load test verifies p95 `GetFact` ≤ 20ms + p95 `TailFacts` tick ≤ 50ms (tag `-tags=load`, nightly); one downstream consumer (e.g. cost-governor's spend rollup, or a multi-agent refactor demo) uses `TailFacts` as the primary read path. | Three lint tools land in CI; `go test -run TestBlackboard_LoadP95 -tags=load ./...` reports p95 within budget; downstream-consumer PR merged citing W11 as data source. |

---

## 8. File-disjoint impl decomposition (preview only — full plan in T1+)

| # | Task | Files (exclusive write scope) | Effort | Depends on |
|---|---|---|---|---|
| **T1** | **Blobs primitive + migration 0008** | `internal/orchestrator/state/migrations/0008_blackboard_blobs.sql`; `internal/orchestrator/state/substrate/blob.go` + `blob_test.go`; `internal/orchestrator/state/migrate.go` (CurrentSchemaVersion 7 → 8) | M | Substrate W1 (#224) merged; W8 amendment PR #311 merged (claims #0007) |
| **T2** | **Fact-kind registry + reducer dispatch** | `internal/orchestrator/blackboard/{registry,reducers,payload,fact,get_fact,errors}.go` + matching `_test.go`; `internal/orchestrator/blackboard/reducers/{lww,set_union,write_once,append}.go` | M | T1 (uses `PutBlob`/`GetBlob`); substrate's `RegisterPayloadValidator` |
| **T3** | **TailFacts API + cursor semantics** | `internal/orchestrator/blackboard/{tail,cursor}.go` + `*_test.go` | M | T2 (uses `Fact` type + registry) |
| **T4** | **Blob GC sweep job** | `internal/orchestrator/blackboard_gc/{sweep,config}.go` + `_test.go` | S | T1 (DELETE on `substrate_blobs`); T2 (reads `payload_json.blob_refs`) |
| **T5** | **OTel attrs + operator docs + (A+) lints** | `internal/orchestrator/blackboard/otel.go`; `docs/operator/blackboard.md`; `tools/lint-blackboard-reducer-purity/main.go`; `tools/lint-blackboard-tail-ctx/main.go`; `tools/lint-blackboard-topics/main.go` (A+-only) | M | T1, T2, T3, T4 (cross-cut) |

**Disjointness verification:** T1 owns `substrate/blob.go` (one new file inside the substrate package, registered no validators). T2 owns the entire new `internal/orchestrator/blackboard/` package. T3 adds two files to that package (`tail.go`, `cursor.go`) — file-disjoint within the package. T4 owns the new `blackboard_gc/` package. T5 cross-cuts via new files only — `blackboard/otel.go` is a new file, `tools/lint-blackboard-*` are new directories. No path appears in two rows.

**Cross-task seam contracts (load-bearing — implementer MUST honour exactly):**

- T1 exports `substrate.PutBlob(ctx, tx, content, sizeMax) (digest, error)`, `substrate.GetBlob(ctx, tx, digest) ([]byte, error)`, `substrate.ErrBlobNotFound`, `substrate.ErrBlobTooLarge`. T2-T5 import these.
- T2 exports `blackboard.PutFact(ctx, tx, topic, schemaVersion, value, blobRefs) error`, `blackboard.GetFact(ctx, db, topic) (json.RawMessage, error)`, `blackboard.Fact` struct, `blackboard.Registry` + `Register/Seal/Resolve/MustHave`, `blackboard.ReducerFn` type, `blackboard.DefaultLWW`, plus the four built-in reducers. T3-T5 import these.
- T3 exports `blackboard.TailFacts(ctx, db, topic, since Cursor) (<-chan Fact, <-chan error, error)` + `blackboard.Cursor` type + `blackboard.CursorEmpty`. T4-T5 import for tail-span attrs.
- T4 exports `blackboard_gc.Run(ctx, db, cfg)` (background goroutine entrypoint). Wired from `cmd/regatta/serve.go` via T5.
- T2 ships `init()` registering `validateFact` via `substrate.RegisterPayloadValidator(substrate.KindFact, validateFact)`. T1 must NOT register a competing validator (substrate Wave 1 ships a placeholder validator for `KindFact` per spec §13 row #1 — T2 replaces it via a new registration; if substrate's dispatch panics on duplicate registration, T2 needs a `Re-Register`-shaped API; **followup spec question**, resolve before T2 dispatches).

**Concurrency cap (per `feedback_session_limit_dispatch`):** 5 tasks total. Recommended dispatch:
1. **Wave A:** T1 alone (spine — owns the new table + migration).
2. **Wave B:** T2 + T3 + T4 in parallel (peak 3 — within 3-4 cap). All branch off T1.
3. **Wave C:** T5 alone (cross-cut; lands last for runbook + lint CI).

---

## 9. Sequencing — when W11 ships

**Defended choice: W11 lands AFTER substrate Wave 1 (#224) is merged, and INDEPENDENTLY of W7 (UI), W8 (RBAC), W9 (replay).**

**Pre-conditions (hard):**

- Substrate Wave 1 merged to main — provides `kind=fact` event channel, `RegisterPayloadValidator`, `AppendEvent`, `Fold`, HMAC signing, tenant_id propagation, OTel attrs via W6. Without this W11 has no foundation. **(Already shipped via #224.)**
- Migration #0006 (substrate) applied — W11's migration #0008 depends on goose's sequential numbering. **(Already applied via #224.)**
- **W8 amendment PR #311 merged** — claims migration #0007 for `policy_revision`. W11 Wave A dispatch is GATED on #311 merge AND verification that #0008 is unallocated on `origin/main` at dispatch time.

**Pre-conditions (soft — not blockers):**

- W6 T3 (#209) — trace_id columns on `work_items` + `approval_events`. W11 reuses substrate's `trace_id` column on `substrate_events`, which is already populated. W6 T3 affects only legacy tables W11 doesn't touch. **Not a blocker.**

**Independent of (NOT a blocker):**

- **W7 (operator UI v2)** — W11 ships a pure-backend API. UI is a downstream consumer (e.g. a "facts panel" could land in W7 wave 2+). W11 ships before W7 needs it, or after — either ordering works.
- **W8 (OPA RBAC + multi-tenant)** — W11 propagates `tenant_id` via the substrate's existing column. W8 layers above: deny `topic=schema.*` writes to non-migration roles. W11 ships before W8; W8 adds policies later without touching W11.
- **W9 (replay + diff)** — replay reads the substrate event log. W11 is a producer + consumer of facts; W9 is a separate consumer of all events. No coupling. W11 + W9 can ship in either order.

**Sequencing implication:** W11 can dispatch as soon as substrate Wave 1 + W6 T3 are on main. **It is the SECOND wave of MVP-4** (after MVP-3 completes its 4 waves: substrate, W7, W8, W9). Roadmap row #6 in `wedge_roadmap_assessment.md` confirms MVP-4 slot.

**Why not earlier:** the wedge dossier classifies W11 as "med-high" build cost. Substrate Wave 1 took 3 tasks + ~2 weeks. W11 is 5 tasks + ~3 weeks. Pulling forward would crowd the MVP-3 critical path (W6/W7/W8/W9 ship the operator surface that unblocks pilot deployment; W11 unblocks the multi-repo-refactor demo which is a marketing wedge, not a credibility wedge).

**Why not later:** the wedge dossier flags **agent serialization on handoff.json** as the bottleneck the blackboard solves. Once MVP-3 ships, the multi-agent fleet refactor demo becomes the largest growth story. W11 unblocks that demo. Slipping past MVP-4 leaves a known bottleneck in place.

---

## 10. Deferred (not in W11 v1)

| Followup | What | Trigger |
|---|---|---|
| F1 | Realtime push (SSE / websocket / goroutine-broadcast fan-out) | Profiling under real load shows >5% CPU in tail SELECTs |
| F2 | Tag-faceted queries (`fact.list(tag=...)`) | Real consumer demands tag-based filtering |
| F3 | Semantic / vector search over facts (`fact.semantic(query, k=5)`) | Embeddings stack chosen (no current proven local-OSS candidate) |
| F4 | S3 / external blob backend | First ≥16 MiB blob ask from a real deployment |
| F5 | Tenant-scoped encrypt-before-hash for sensitive payloads | W8 multi-tenant cutover (mirrors substrate F5) |
| F6 | Rate-limit on `PutBlob` | W8 RBAC API boundary lands (post-W8 only) |
| F7 | Cursor versioning + migration recipe | W9 multi-writer story (Temporal-backed) lands |
| F8 | Materialized `blob_refs` index table | Profiling shows GC `json_each` scan is the bottleneck |
| F9 | sha3-256 / next-gen hash migration path | sha256 weakens (≥10y horizon) |
| F10 | Listen/notify backed `TailFacts` for Postgres adapter | First Postgres-backed adapter ships (W9 Temporal-backed deployments) |

Each must be filed BEFORE Wave A PR opens, with title prefix `[blackboard-followup]`. T1 PR body cites issue numbers.

---

## 11. Open questions for adversarial reviewer

1. **Validator re-registration (T1↔T2 seam).** Substrate W1's `RegisterPayloadValidator` ships a placeholder validator for `KindFact`. Does it panic on duplicate registration, or does it silently overwrite? T2's `init()` re-registers — if substrate panics, T2 needs a substrate-side API change. Resolve before T2 dispatches.

2. **GC sweep query scalability.** §3.5's sweep query uses `json_each` over the fact table. For deployments with ≥10M facts, the per-tick scan may exceed the hourly interval. Should the v1 ship a precomputed `blob_refs` index column on `substrate_events`, or rely on F8 followup? Recommended: ship `idx_substrate_events_blob_refs WHERE payload_json LIKE '%blob_refs%'` partial index in T1 + measure under load before deciding F8.

3. **`subscriber_idle_max_secs` watchdog default.** §3.5 / R5 mandate a watchdog that closes idle subscriber channels after N seconds. 3600s (1h) is conservative — a polling agent legitimately quiet for an hour gets surprise-disconnected. Suggested: 24h default, override per `TailFacts` call.

4. **Reducer state — should reducers see SCHEMA across versions?** §3.2 keys reducers by `(topic, schema_version)`. A migration from v1 to v2 may need a reducer that reads BOTH v1 and v2 facts and emits a unified v2 value. Current design forces this to be encoded in the v2 reducer (it folds the full fact list, including v1 rows). Confirm this is the intended pattern + document in operator runbook.

5. **Blob-ref denormalization vs `payload_json.blob_refs`.** §3.1 stores `blob_refs` inside `payload_json`. GC sweep uses `json_each` to extract them. Alternative: a `substrate_blob_refs(event_id, digest)` junction table populated on fact-write. Pro: O(1) GC scan via index. Con: extra table, extra write per fact, extra migration. Resolve in T1 spec freeze.

---

_Spec authority: `feedback_spec_pattern_authority` — implementer subagent deviation from this spec requires re-spawning design subagent. Open questions in §11 must be resolved by adversarial reviewer before Wave A dispatches. The 10 followup issues in §10 MUST be filed and cited in the Wave A PR body per `feedback_unaddressed_load_bearing` + `feedback_review_before_automerge`._
