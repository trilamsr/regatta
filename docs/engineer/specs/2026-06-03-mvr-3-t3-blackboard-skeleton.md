---
title: "MVR-3-T3 Blackboard — sqlite-CAS blobs + fact subscriptions (skeleton-tier pre-fetch)"
status: skeleton-prefetch
summary: Pre-fetch skeleton for MVR-3-T3 typed-facts + reducers + sqlite-CAS-blobs wedge; full spec re-spawns when MVR-3 trigger fires (5 paying customers OR a customer demanding multi-agent fact-sharing). Locks scope, prior-art, risks, test plan, dep-order — substrate's `blob_digest` column already forward-fit per Wave 1.
---

# MVR-3-T3 Blackboard — sqlite-CAS + fact subscriptions (skeleton-tier pre-fetch)

_Author: design subagent, 2026-06-03. Skeleton-tier per `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 Phase MVR-3 row T3 (M, 2-3 wks, dep=sqlite). This spec is the pre-fetch contract; it does NOT dispatch implementer subagents._

Cites: `feedback_research_design_principles` (adopt sqlite-CAS over external KV), `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_deletion_default`, `feedback_migration_number_lock` (one migration; number pinned at dispatch), `feedback_spec_pattern_authority`.

Prior-art baseline: `docs/engineer/specs/2026-06-01-w11-blackboard-design.md` (50 KB Wave 1 design) is the source-of-truth for the full surface. Substrate Wave 1 (`docs/engineer/specs/2026-06-01-unified-substrate-design.md`) already shipped `kind=fact` events + the `blob_digest` forward-fit column + `RegisterPayloadValidator` dispatch. This skeleton re-litigates only the MVR-3 slice that the substrate deliberately deferred.

---

## 0. Scope (in / out)

### In scope (MVR-3-T3)

- **CAS-blobs primitive** — new `substrate_blobs(digest BLOB PK, body BLOB, created_at INTEGER)` table. Insert via `substrate.PutBlob(ctx, body) (digest, error)`; read via `substrate.GetBlob(ctx, digest)`. Size cap config-driven (default 1 MiB; hard cap 16 MiB).
- **Fact-kind registry + reducer dispatch** layered over substrate's `kind=fact` events. Registration at boot: `substrate.RegisterReducer(topic, schemaVer, fn)`. Default reducer = LWW. Operator-defined reducers: set-union, write-once, custom.
- **Fact-subscription API** — `substrate.TailFacts(ctx, topic, since cursor) (<-chan Fact, error)`. Polling backed (default 1s interval, bounded 100ms–10s).
- **Blob GC** — orphan sweep job; blobs unreferenced for >24h are deleted. Operator opt-out via config.
- **OTel attribute conventions** — `regatta.blackboard.{topic,schema_version,blob_count,subscriber_id}` on write + tail spans.

### Out of scope (MVR-3-T3)

- Real-time push (no SQLite LISTEN/NOTIFY, no websocket, no goroutine broadcast). Poll-only v1.
- Cross-tenant fact sharing (tenant_id-scoped via substrate; cross-tenant queries gated by W8 OPA, NOT W11).
- Semantic validation beyond JSON-typed unmarshal (e.g. "topic X requires role Y" — that is W8 OPA).
- Multi-region blob replication (single-binary sqlite-CAS v1; followup once a customer asks for HA).
- Blob compression (sqlite already pages efficiently; deferred until measured bloat).
- CRDT-grade conflict resolution (Yjs / Automerge — deferred until a research-customer demands branching plans, per roadmap §A1).

## 1. Prior art (cite version + license)

| Primitive | Adopted from | Version | License | What we take |
|---|---|---|---|---|
| Content-addressed storage | [git-objects model](https://git-scm.com/book/en/v2/Git-Internals-Git-Objects) | n/a | proven OSS pattern | sha256 digest as primary key; immutable rows; orphan-sweep GC |
| Reducer/blackboard pattern | [Erlang ETS](https://www.erlang.org/doc/man/ets.html) + [BlackboardAI](https://en.wikipedia.org/wiki/Blackboard_(design_pattern)) | n/a | proven pattern | Topic-keyed reducer dispatch; LWW default; pluggable merge funcs |
| sqlite as KV store | [sqlite BLOB column](https://www.sqlite.org/lang_blob.html) | 3.46+ | Public domain | BLOB primary key; `WITHOUT ROWID`; orphan GC via LEFT JOIN |
| Substrate event channel | `internal/substrate/` (regatta, Wave 1 shipped) | n/a | repo-internal | `kind=fact` events; `blob_digest` forward-fit column; `RegisterPayloadValidator` dispatch |
| Fact-subscription cursor | [PostgreSQL logical-replication slot](https://www.postgresql.org/docs/current/logicaldecoding.html) | n/a | docs ref | Opaque rowid cursor; replayable; no client-side state |

Rejected alternatives: Redis (adds infra surface to a single-binary regatta); embedded BadgerDB (sqlite is already in-tree); custom append-only file format (re-inventing sqlite WAL); Yjs/Automerge (research-customer trigger has not fired — defer per roadmap §A1).

## 2. Architecture (high-level)

```
internal/substrate/
  blobs.go           // PutBlob/GetBlob; sha256 PK; orphan-sweep job
  reducer.go         // RegisterReducer(topic, schemaVer, fn); dispatch at boot
  tail.go            // TailFacts(ctx, topic, since) <-chan Fact; polling
internal/blackboard/
  facade.go          // public agent-facing API; wraps substrate primitives
migrations/
  00NN_blackboard_blobs.sql  // CREATE TABLE substrate_blobs; number pinned in dispatch prompt
```

Reducer registration is boot-time only — runtime swap returns `ErrReducerFrozen` (same pattern as the signer adapter).

## 3. Key risks (≥6 named)

| # | Risk | Mitigation |
|---|---|---|
| R1 | Blob table grows unboundedly | Hard size cap (16 MiB per blob, configurable); orphan-sweep GC every 24h; per-tenant total-bytes cap (config, default 1 GiB) |
| R2 | Polling fan-out at scale (100 subscribers × 1s tick) | Subscriber multiplexing — one DB query per topic, fan-out in-process; benchmark in test plan |
| R3 | Reducer panic crashes substrate writer | `recover()` in reducer dispatch; panic logged + counted as `regatta.blackboard.reducer.panic` OTel metric; offending reducer auto-disabled |
| R4 | Schema-version drift (operator deploys v2 reducer; v1 facts in stream) | Reducer registered per `(topic, schema_version)` tuple; v1 facts replay through v1 reducer; v2 sees only v2 facts |
| R5 | Cross-tenant leak via blob digest (one tenant guesses another's digest) | GetBlob requires substrate `events.tenant_id` match in same tx (per substrate Wave 1 tenant-scoping) |
| R6 | Orphan-sweep deletes blob referenced by in-flight fact (race window) | Sweep query LEFT JOINs `events.blob_digest` + filters `created_at < now() - 24h` — race window > grace period |
| R7 | Migration number collision with parallel work | Number pinned in dispatch prompt per `feedback_migration_number_lock`; agent does not pick |
| R8 | sqlite WAL size growth from large blobs (>1 MiB per row stresses WAL checkpoint) | Default cap 1 MiB; hard cap 16 MiB; recommend WAL checkpoint config tune in operator runbook |
| R9 | Subscription cursor confusion (operator passes wrong cursor) | Cursor opaque (base64-encoded rowid + topic-hash); decode error returns ErrInvalidCursor; documented format |

## 4. Test plan (≥8)

1. `TestPutBlob_GetBlob_RoundTrip` — write + read; bytes identical; digest matches sha256.
2. `TestPutBlob_Idempotent` — same body twice; one row; same digest.
3. `TestPutBlob_RejectsOversize` — 17 MiB blob → ErrBlobTooLarge.
4. `TestReducer_LWW_Default` — three facts on one topic; reducer returns latest.
5. `TestReducer_SetUnion_Operator` — register set-union reducer; verify reduce semantics.
6. `TestReducer_PanicRecovered` — register reducer that panics; substrate writer survives; counter increments.
7. `TestTailFacts_Subscription` — subscribe; write fact; channel emits within 2 ticks.
8. `TestTailFacts_CursorReplay` — subscribe with stale cursor; channel replays from cursor.
9. `TestBlobGC_DeletesOrphans` — write blob; no fact references it; advance clock 25h; sweep deletes.
10. `TestBlobGC_PreservesReferenced` — write blob + fact with blob_ref; sweep preserves.
11. `TestBlackboard_TenantScoped` — tenant A writes; tenant B GetBlob with A's digest → ErrNotFound.
12. `BenchmarkTailFacts_100Subscribers` — fan-out p99 ≤ 1s tick.
13. `FuzzReducerDispatch` — random fact payloads + registered reducer must not panic outside the recover() boundary.

## 5. Dep order

1. **MUST be merged first:** Substrate Wave 1 (`docs/engineer/specs/2026-06-01-unified-substrate-design.md`) — `kind=fact` channel + `blob_digest` forward-fit + `RegisterPayloadValidator` dispatch.
2. **MUST be merged first:** S3-T2 substrate cutover phase B+C (`docs/engineer/specs/2026-06-02-s3-t2-substrate-cutover.md`) — facts must be on substrate before blackboard layers on top.
3. **SHOULD be merged first:** W8 OPA RBAC slim (`docs/engineer/specs/2026-06-02-s3-t1-w8-opa-slim.md`) — semantic validation hooks into OPA; v1 ships without OPA dep but adapter is wired.
4. **No dep on MVR-3-T1 / T2 / T4** — blackboard is orthogonal to signer, billing, research-mode.
5. **Trigger:** MVR-3 entry per roadmap §4 (5 paying customers OR a customer asking for multi-agent fact sharing).

## 6. Grade rubric (filled at dispatch time)

| Criterion | B (must) | A (should) | A+ (aspires) |
|---|---|---|---|
| `make check` clean | _filled at dispatch_ | _filled_ | _filled_ |
| ONE migration, number locked | _filled_ | _filled_ | _filled_ |
| Reducer panic does not crash writer | _filled_ | _filled_ | _filled_ |
| Tenant-scoping covers blob reads | _filled_ | _filled_ | _filled_ |
| Subscription p99 within tick budget | _filled_ | _filled_ | _filled_ |
| Deletion ledger | _filled_ | _filled_ | _filled_ |

## 7. What got smaller

Skeleton-tier defers real-time push + multi-region replication + CRDT semantics + blob compression to followups. MVR-3-T3 ships ONLY the sqlite-CAS-blobs + LWW-default reducer + polling subscription — minimum surface that lets parallel subagents share typed state without prompt-bloat. Yjs/Automerge stays deferred until a research-customer demands branching plans (per roadmap §A1 adopt-trigger).
