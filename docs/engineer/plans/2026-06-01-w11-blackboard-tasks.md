# MVP-4 W11 Blackboard — Implementer Task Breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-w11-blackboard-design.md` (#281 merged).
Authority: `feedback_spec_pattern_authority` — implementer deviation from any spec-mandated pattern (T1 owns the `substrate_blobs` migration + `PutBlob`/`GetBlob` API per spec §3.3; T2 owns the entire `internal/orchestrator/blackboard/` package + `RegisterPayloadValidator(KindFact, validateFact)` registration per §3.1 + §3.2; T3 owns `TailFacts` + `Cursor` per §3.4; T4 owns the `blackboard_gc/` package per §3.5; T5 wires OTel attrs + operator runbook + A+ lint binaries per §3.7 + §7 row A+) MUST re-spawn the design subagent. NO implementer-chosen alternatives.

Design priority for every decision below (`feedback_decision_priority`): **UX → ease of use → performance → best practices → execution speed → velocity**; long-term over short-term. Grade rubric (`feedback_grade_rubric`) inherited verbatim from spec §7 — each task carries the spec's B / A / A+ tool-checkable criteria.

---

## Wave overview

- **5 file-disjoint implementer tasks** (T1, T2, T3, T4, T5) per spec §8.
- **Two-wave dispatch:**
  - **Wave A (parallel):** T1, T2, T3, T4 all branch off `main`. File-disjoint per §1 below. Concurrency cap 4 — under the 10-lane ceiling (`feedback_dispatch_strategy`).
  - **Wave B (sequential):** T5 branches off Wave A merged main; depends on T1/T2/T3/T4 exports (OTel attr surfaces) being on main.
- **Prereqs (merged to main):**
  - Substrate Wave 1 (#224) — provides `kind=fact` event channel, `RegisterPayloadValidator(kind, fn)` open-extension hook, `AppendEvent`, `Fold`, HMAC signing, `tenant_id` propagation, OTel attrs via W6. **Already shipped.**
  - Migration #0006 (`0006_substrate.sql`) applied. **Already shipped.**
  - W6 T3 (#209) — `trace_id` columns. Substrate already populates `trace_id`; W11 inherits. **Soft prereq; not a blocker.**
- **Migration phasing (`feedback_migration_number_lock`):** Migration **#0008** is LOCKED for `0008_blackboard_blobs.sql` (owned by T1). Migration #0007 is owned by W8 `policy_revision` per amendment PR #311 (in flight) — W11 takes #0008 to avoid collision. T2/T3/T4 add NO migrations — they layer over substrate's existing event log. T5 adds NO migration. Implementers MUST NOT renumber. If an implementer believes a different number is needed, STOP and re-spawn design.
- **W11 Wave A dispatch sequencing (HARD GATE):** Wave A dispatch is GATED on (a) W8 amendment PR #311 merge to `main`, AND (b) verification at dispatch time that `internal/orchestrator/state/migrations/0008_*.sql` is unallocated on `origin/main` (run `ls internal/orchestrator/state/migrations/` and confirm no `0008_*.sql` exists). If either fails, STOP — do NOT dispatch.
- **Concurrency cap (`feedback_dispatch_strategy`):** Wave A peaks at 4 parallel implementers. Wave B is solo. Well under the 10-lane operator ceiling.
- **Open spec question to resolve before Wave A (`feedback_spec_pattern_authority` per spec §11):** validator re-registration semantics. Substrate v2 Wave 1 ships a placeholder validator for `KindFact`; T2's `init()` re-registers `validateFact`. If `RegisterPayloadValidator` panics on duplicate registration, T2 needs an upstream substrate-side API addition (`Re-Register`-shaped) **before T2 dispatches**. Plan-time grep against substrate `validate.go` MUST confirm the panic-on-duplicate behaviour; if it panics, file a substrate-side spec amendment first.
- **Deletion default (`feedback_deletion_default`) — what got smaller across this wave:**
  - **No bespoke `facts` table** — substrate's `kind='fact'` event channel is the storage. Saves: one table, one migration, one HMAC pipeline, one `supersedes` FK, one nonce column, one cycle-check (all already shipped by substrate T-S1 #224).
  - **No bespoke `signature`, `written_by`, `tenant_id`, `TTL_at`, `tags_json` columns** — all inherited from substrate.
  - **Read API collapsed from 3 to 2** — `GetFact(topic)` + `TailFacts(topic, since)`. Drops `fact.list(tag=...)` + `fact.semantic(query, k)` until a real consumer forces them (deferred to `[blackboard-followup]` F2, F3).
  - **Sweep, not ref-counting** — GC uses mark-and-sweep over the fact log. Saves one column on every fact row + one mutation per write.
  - Net per spec §1: **one new table (`substrate_blobs`), one new package (`internal/orchestrator/blackboard/`), one new migration (`0008_blackboard_blobs.sql`)**. Plus one auxiliary package (`internal/orchestrator/blackboard_gc/`) for the sweep job. T5 adds one operator doc + three optional A+-tier lint binaries.
- **Followup filing (`feedback_unaddressed_load_bearing`):** every load-bearing named-but-deferred item in spec §10 (F1-F10) is filed as a `[blackboard-followup]` issue **PRE-MERGE of Wave A's first PR**. T1's PR body cites every issue number. §7 below enumerates the 10 templates.

---

## §1 File-disjoint table

| Task | Path (exclusive write scope) | Depends-on (Wave + main) | Effort | TDD tests (count: named) |
| ---- | ---------------------------- | ------------------------ | ------ | ------------------------ |
| **T1 — Blobs primitive + migration #0008** | `internal/orchestrator/state/migrations/0008_blackboard_blobs.sql` (NEW; spec §3.3 DDL verbatim); `internal/orchestrator/state/substrate/blob.go` (NEW; `PutBlob` + `GetBlob` + `ErrBlobNotFound` + `ErrBlobTooLarge`) + `blob_test.go`; `internal/orchestrator/state/migrate.go` (CurrentSchemaVersion bump 7 → 8, ONE-LINE delta) + `migrate_test.go` (assert new version) | Substrate W1 (#224) merged; migration #0006 applied; W8 amendment PR #311 merged (#0007) | M | **7 named** (B 5, A 2). Spec §6 T1 verbatim. |
| **T2 — Fact-kind registry + reducer dispatch** | `internal/orchestrator/blackboard/` NEW package: `payload.go`, `payload_test.go`, `registry.go`, `registry_test.go`, `fact.go`, `errors.go`, `get_fact.go`, `get_fact_test.go`; `internal/orchestrator/blackboard/reducers/` NEW sub-package: `lww.go`, `set_union.go`, `write_once.go`, `append.go`, plus matching `_test.go` siblings | T1 (uses `substrate.PutBlob` indirectly via `blob_refs` validation); substrate's `RegisterPayloadValidator` open-extension hook (T-S1 #224) | M | **9 named** (B 6, A 2, A+ 1). Spec §6 T2 verbatim. |
| **T3 — TailFacts API + cursor semantics** | `internal/orchestrator/blackboard/tail.go` (NEW), `tail_test.go`; `internal/orchestrator/blackboard/cursor.go` (NEW), `cursor_test.go` | T2 (imports `Fact` type + `Registry`) | M | **9 named** (B 5, A 3, A+ 1). Spec §6 T3 verbatim. |
| **T4 — Blob orphan GC sweep job** | `internal/orchestrator/blackboard_gc/` NEW package: `sweep.go`, `sweep_test.go`, `config.go`, `config_test.go` | T1 (DELETE on `substrate_blobs`); T2 (reads `payload_json.blob_refs`) | S | **6 named** (B 4, A 2). Spec §6 T4 verbatim. |
| **T5 — OTel attrs + operator runbook + (A+) lints** | `internal/orchestrator/blackboard/otel.go` (NEW; attr constants + helpers for `regatta.blackboard.topic`, `regatta.blackboard.schema_version`, `regatta.blackboard.blob_count`, `regatta.blackboard.subscriber_id`); `internal/orchestrator/blackboard/otel_test.go`; `docs/operator/blackboard.md` (NEW); `tools/lint-blackboard-reducer-purity/main.go` (NEW; A+ only); `tools/lint-blackboard-tail-ctx/main.go` (NEW; A+ only); `tools/lint-blackboard-topics/main.go` (NEW; A+ only); plus `_test.go` siblings for each lint binary; one-line wire-in changes to `internal/orchestrator/blackboard/{fact,get_fact,tail}.go` (≤ 3 LoC per file) to set the otel attrs on already-open spans | T1, T2, T3, T4 merged | M | **6 named** (B 2, A 1, A+ 3). Spec §6 T5 verbatim. |

**Total: 37 named tests across 5 tasks.**

### Disjointness verification (`grep` at plan time)

- T1 writes only to `migrations/0008_blackboard_blobs.sql`, `substrate/blob.go` + `blob_test.go`, and a ONE-LINE bump in `state/migrate.go`. T1 does NOT touch the `blackboard/` or `blackboard_gc/` packages.
- T2 owns the entire new `internal/orchestrator/blackboard/` package (registry + reducers + payload + get_fact + fact + errors). T2 does NOT touch `substrate/`, does NOT touch `migrations/`, does NOT touch `blackboard_gc/`, does NOT touch the files T3 owns (`tail.go` + `cursor.go`).
- T3 adds two new files to `internal/orchestrator/blackboard/`: `tail.go` + `cursor.go`. File-disjoint with T2 within the same Go package; both file sets compile against a single `package blackboard` declaration. T3 does NOT touch T2's files.
- T4 owns the entire new `internal/orchestrator/blackboard_gc/` package. T4 does NOT touch `blackboard/`, does NOT touch `substrate/`, does NOT touch `migrations/`.
- T5 adds new files in three locations: `blackboard/otel.go` (new file in T2's package — disjoint with T2's existing files), `docs/operator/blackboard.md` (new), `tools/lint-blackboard-*/main.go` (three new directories, A+ only). The ≤ 3-LoC wire-in changes in `fact.go`/`get_fact.go`/`tail.go` are SEAM additions only (calls into `blackboard/otel.go` helpers). T5 lands AFTER Wave A merges, so those edits land on top of T2/T3 commits — no parallel-edit conflict.

**Cross-task seam verification:** the only file modified by two tasks is `internal/orchestrator/blackboard/{fact,get_fact,tail}.go`, and that overlap is sequential (T2 + T3 land first; T5's ≤ 3-LoC wire-in adds attribute calls on the already-open spans). Sequential edits, not parallel. No file appears in two parallel-wave rows.

### Cross-task seam contracts (load-bearing — implementer MUST honour exactly)

- **T1 exports** `substrate.PutBlob(ctx context.Context, tx *sql.Tx, content []byte, sizeMax int) (digest string, err error)`, `substrate.GetBlob(ctx context.Context, tx *sql.Tx, digest string) ([]byte, error)`, `substrate.ErrBlobNotFound`, `substrate.ErrBlobTooLarge`. T2/T3/T4/T5 import these. Caller owns the `*sql.Tx` — `PutBlob` does NOT begin/commit.
- **T2 exports** `blackboard.PutFact(ctx, tx, topic, schemaVersion, value, blobRefs) error`, `blackboard.GetFact(ctx, db, topic) (json.RawMessage, error)`, `blackboard.Fact` struct (11 fields per spec §3.2 lines 153-165), `blackboard.Registry` + `Register/Seal/Resolve/MustHave`, `blackboard.ReducerFn` type, `blackboard.DefaultLWW`, the four built-in reducers (`reducers.LWW`, `reducers.SetUnion`, `reducers.WriteOnce`, `reducers.Append`), `blackboard.ErrNoFacts`, `blackboard.ErrInvalidPayload`. T3 imports `Fact` + `Registry`. T4 reads `payload_json.blob_refs` via SQL `json_each` — NO Go-struct import dependency on T2 for the GC sweep (T4 is SQL-side; closes the symmetric concern that mirrors cost-gov W2's Reader/Writer split).
- **T2 ships `init()`** registering `validateFact` via `substrate.RegisterPayloadValidator(substrate.KindFact, validateFact)`. This REPLACES the placeholder validator that substrate Wave 1 ships for `KindFact` per spec §13 row #1. **Open spec question (§11 #1):** confirm at dispatch time whether `RegisterPayloadValidator` panics on duplicate registration. If it panics, T2's dispatch is BLOCKED until a substrate-side amendment lands a `ReRegisterPayloadValidator` API (separate doc-only spec amendment PR; ~30 LoC change in `substrate/validate.go`). Plan-time check: `grep -n "RegisterPayloadValidator\|duplicate" internal/orchestrator/state/substrate/validate.go`.
- **T3 exports** `blackboard.TailFacts(ctx, db, topic, since Cursor) (factc <-chan Fact, errc <-chan error, err error)`, `blackboard.Cursor` (type-aliased string with no exported constructors except `CursorEmpty`), `blackboard.CursorEmpty`. T5 imports for tail-span attrs.
- **T4 exports** `blackboard_gc.Run(ctx context.Context, db *sql.DB, cfg Config) error` (long-loop entrypoint), `blackboard_gc.Sweep(ctx, db, cfg) (deleted int, err error)` (one pass for testing), `blackboard_gc.Config` (HTTPClient stub NOT applicable; fields: `DB`, `Clock func() time.Time`, `GraceSecs int`, `BatchSize int`, `Enabled bool`, `Tracer`, `Logger`). Wired from `cmd/regatta/serve.go` via T5's operator-runbook wire-in (one-line invocation; if `Enabled=false`, the goroutine is not started).
- **T5 exports** the OTel attr-key constants `blackboard.AttrTopic`, `blackboard.AttrSchemaVersion`, `blackboard.AttrBlobCount`, `blackboard.AttrSubscriberID`, plus three lint binaries (A+ only). The ≤ 3-LoC wire-in calls live in `fact.go`/`get_fact.go`/`tail.go` and use `trace.SpanFromContext(ctx).SetAttributes(...)` against the already-open span. NO new spans opened by T5 — attrs land on T2/T3's spans.
- **Cyclic-import check (plan-time invariant):** `blackboard` imports `substrate`. `blackboard_gc` imports `substrate` (for the DELETE statement) AND `blackboard` (for the `KindFact` constant via re-export, OR direct from substrate). `substrate` does NOT import `blackboard`. `substrate` does NOT import `blackboard_gc`. Run `go list -deps ./internal/orchestrator/blackboard/... ./internal/orchestrator/blackboard_gc/...` after Wave A merges to confirm.

---

## §2 Task T1 — Blobs primitive + migration #0008

### Scope

- **`internal/orchestrator/state/migrations/0008_blackboard_blobs.sql`** — NEW. DDL verbatim from spec §3.3 lines 215-232:
  - `substrate_blobs` table: `digest TEXT NOT NULL PRIMARY KEY` (64-char lower-case sha256 hex, enforced via SQL CHECK), `bytes BLOB NOT NULL`, `size_bytes INTEGER NOT NULL`, `content_type TEXT NOT NULL DEFAULT 'application/octet-stream'`, `created_at INTEGER NOT NULL` (unix ms UTC).
  - `CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*')` — lower-case sha256 hex only.
  - `CHECK (size_bytes > 0 AND size_bytes <= 16777216)` — hard cap 16 MiB.
  - `CREATE INDEX idx_substrate_blobs_created_at ON substrate_blobs(created_at)` — orphan-GC sweep selectivity.
  - File-header comment cites spec §3.3 + "Migration #0008 reserved per plan §1 — DO NOT renumber" + "Migration #0007 owned by W8 policy_revision per amendment PR #311".
- **`internal/orchestrator/state/substrate/blob.go`** — NEW. Go API per spec §3.3 lines 239-261:
  - `PutBlob(ctx context.Context, tx *sql.Tx, content []byte, sizeMax int) (digest string, err error)`:
    1. Validate `len(content) > 0`.
    2. Validate `len(content) <= sizeMax` ⇒ else return `ErrBlobTooLarge` (NO row written).
    3. Compute `digest = hex.EncodeToString(sha256.Sum256(content)[:])` (lower-case).
    4. INSERT … ON CONFLICT(digest) DO NOTHING (idempotent — same content twice ⇒ same row).
    5. Return digest + nil. Caller owns tx; PutBlob does NOT begin/commit.
  - `GetBlob(ctx context.Context, tx *sql.Tx, digest string) ([]byte, error)`:
    1. SELECT bytes FROM substrate_blobs WHERE digest = ?.
    2. Return `ErrBlobNotFound` on no rows.
    3. Return bytes + nil.
  - Sentinels: `ErrBlobNotFound = errors.New("substrate: blob not found")`, `ErrBlobTooLarge = errors.New("substrate: blob exceeds size cap")`.
- **`internal/orchestrator/state/substrate/blob_test.go`** — NEW. 7 named tests (see below).
- **`internal/orchestrator/state/migrate.go`** — ONE-LINE delta: `CurrentSchemaVersion` constant bumps from 7 → 8. NO other change. Migration runner already auto-applies `0008_*.sql` by filename ordering.
- **`internal/orchestrator/state/migrate_test.go`** — add `TestMigration0008_AppliesAndCreatesSchema` (B-tier) asserting fresh DB → migrate → schema-version is 8 + `substrate_blobs` + `idx_substrate_blobs_created_at` exist via `sqlite_master` introspection.

### Prereqs (cite spec sections)

- Spec §1 in-scope item #1 (CAS blobs primitive).
- Spec §3.3 — sqlite BLOB column DDL + Go API signatures verbatim.
- Spec §3.6 — config lifecycle (`blackboard.blob_size_max_bytes`, default 1 MiB, hard cap 16 MiB).
- Spec §6 T1 — exhaustive named-test list (7 tests transcribed below).
- Spec §7 B/A — B requires migration + PutBlob + GetBlob + ErrBlobNotFound + ErrBlobTooLarge; A adds hard-cap-from-SQL-CHECK enforcement + lower-case-hex assertion.
- Spec §9 R1 (blob storage growth → sizeMax cap), R4 (GC race → defended via grace window in T4, but T1's `created_at` column is the load-bearing input), R10 (sha256 collision → documented in godoc; nothing for T1 to enforce).

### Existing patterns to reuse (do NOT reinvent)

- **Substrate migration sequence:** `migrations/0001_*.sql` through `migrations/0006_substrate.sql` — copy the file-header comment shape (date + spec authority + DO-NOT-RENUMBER warning).
- **Migration runner:** `internal/orchestrator/state/migrate.go::ApplyAll(ctx, db) error` already walks `migrations/*.sql` in lexicographic order. T1 needs ONLY the `CurrentSchemaVersion` bump; no other migrate.go change.
- **Test fixture for fresh DB:** `internal/orchestrator/state/substrate/helpers_test.go::newTestDB(t)` — opens an in-memory sqlite, applies all migrations. Use verbatim.
- **sha256 hashing:** `crypto/sha256` + `encoding/hex`. Standard lib; no external dep.
- **ON CONFLICT idempotent insert:** existing substrate event-log writer uses `INSERT … ON CONFLICT(run_id, written_by, nonce) DO NOTHING` for the replay-protection UNIQUE. Mirror that pattern for `substrate_blobs` (`ON CONFLICT(digest) DO NOTHING`).
- **Error sentinel naming:** `ErrBlobNotFound`, `ErrBlobTooLarge` — mirrors substrate's `ErrReplay`, `ErrInvalidPayload`, `ErrCycleDetected`.

### TDD test list (named tests from spec §6 T1; failing-output capture step required)

Per `feedback_tdd_discipline`: implementer writes each test first, runs `go test ./internal/orchestrator/state/substrate/ -run <TestName> -v`, **captures failing output (paste into PR body)**, then implements. "Tests would have failed" is NOT acceptable.

**B-tier (5 named tests; spec §6 T1 + §7 B):**

1. `TestMigration0008_AppliesAndCreatesSchema` — fresh DB → migrate → `substrate_blobs` table + `idx_substrate_blobs_created_at` index present; schema-version bump 7 → 8.
2. `TestBlackboard_PutBlobRoundTrip` — `PutBlob(content)` returns `hex(sha256(content))`; subsequent `GetBlob(digest)` returns identical bytes.
3. `TestBlackboard_PutBlobIdempotent` — same content twice ⇒ same digest; row count unchanged (assert via `SELECT COUNT(*) FROM substrate_blobs`); no UNIQUE-collision error surfaces.
4. `TestBlackboard_PutBlobRejectsOversize` — content `len > sizeMax` ⇒ returns `ErrBlobTooLarge`; assert ZERO rows written via row count.
5. `TestBlackboard_GetBlobNotFound` — unknown digest ⇒ returns `ErrBlobNotFound`; nil byte slice.

**A-tier (2 named tests; spec §6 T1 A-tier + §7 A):**

6. `TestBlackboard_PutBlobRejectsAboveHardCap` — bypass the config cap by passing `sizeMax=20*1024*1024`; content of 17 MiB triggers the SQL CHECK fault (defense-in-depth — even if an operator misconfigures `sizeMax`, the 16 MiB hard cap holds at the SQL layer).
7. `TestBlackboard_DigestIsLowerCaseHex` — write content; assert returned digest matches `^[0-9a-f]{64}$`; assert row's stored digest column matches; assert that an attempt to INSERT an upper-case digest hand-rolled into SQL triggers the CHECK constraint.

Total T1: **7 named tests**. PR body lists every name + pasted failing-output excerpt for AT LEAST 4 representative cases.

### PR body skeleton — T1

````
## Summary

W11 blackboard T1: ships the CAS blobs primitive per
docs/engineer/specs/2026-06-01-w11-blackboard-design.md §3.3.

- internal/orchestrator/state/migrations/0008_blackboard_blobs.sql — NEW.
  substrate_blobs table (sha256 PK + BLOB column + 16 MiB hard cap +
  created_at index for GC sweep selectivity).
- internal/orchestrator/state/substrate/blob.go — NEW. PutBlob /
  GetBlob / ErrBlobNotFound / ErrBlobTooLarge. Caller owns tx; PutBlob
  ON CONFLICT(digest) DO NOTHING for content-addressed idempotency.
- internal/orchestrator/state/migrate.go — ONE-LINE delta:
  CurrentSchemaVersion 7 → 8.

Migration #0008 is LOCKED per docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md
§1. Migration #0007 is owned by W8 policy_revision per amendment PR #311.
T2/T3/T4 add NO migrations.

## Why

Per spec §3.3: blackboard facts reference variable-length payloads
(file diffs, structured outputs) too large to inline in the substrate
event log. CAS keying by sha256 gives free dedup; the 1 MiB default
config cap (16 MiB SQL hard cap) keeps single rows well under sqlite's
per-row ceiling. External-store adapter is [blackboard-followup] F4
once a real consumer needs >16 MiB.

## Test plan

- [x] TestMigration0008_AppliesAndCreatesSchema
- [x] TestBlackboard_PutBlobRoundTrip
- [x] TestBlackboard_PutBlobIdempotent
- [x] TestBlackboard_PutBlobRejectsOversize
- [x] TestBlackboard_GetBlobNotFound
- [x] TestBlackboard_PutBlobRejectsAboveHardCap
- [x] TestBlackboard_DigestIsLowerCaseHex
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 4 reps>

## Deletion default

T1 does NOT add a bespoke "blob_refs" junction table — references live
in payload_json.blob_refs on the substrate_events row. Saves one
table, one migration, two columns per fact row. GC sweep reads
blob_refs via SQL json_each on the existing fact log. Tracked vs
F8 followup (materialized index if profiling demands).

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [blackboard-followup] F1 — Realtime push (SSE / websocket / goroutine broadcast) (#NNN)
- [blackboard-followup] F2 — Tag-faceted fact queries (#NNN)
- [blackboard-followup] F3 — Semantic / vector search over facts (#NNN)
- [blackboard-followup] F4 — S3 / external blob backend for >16 MiB (#NNN)
- [blackboard-followup] F5 — Tenant-scoped encrypt-before-hash (#NNN)
- [blackboard-followup] F6 — Rate-limit on PutBlob (#NNN; post-W8)
- [blackboard-followup] F7 — Cursor versioning before W9 multi-writer (#NNN)
- [blackboard-followup] F8 — Materialized blob_refs index if json_each becomes a bottleneck (#NNN)
- [blackboard-followup] F9 — sha3-256 migration path (#NNN)
- [blackboard-followup] F10 — Listen/notify-backed TailFacts on Postgres adapter (#NNN)

```release-notes
[FEATURE] substrate CAS blobs primitive (PutBlob/GetBlob + migration #0008 substrate_blobs table)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w11-t1. Branch: `feat/w11-t1-blobs-primitive`
off main.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w11-blackboard-design.md.
Read ALL of: §1 (scope, in/out, what got smaller), §3.3 (DDL +
PutBlob/GetBlob API verbatim), §3.6 (config lifecycle), §6 T1 (named
test list), §7 (B/A/A+ rubric), §8 (file-disjoint row T1 + seams), §9
R1 (storage growth defense), R4 (GC race window — T4 enforces; T1
populates created_at), R10 (sha256 collision — godoc only).

Plan: docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md §2 (this
task).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (T1 OWNS migration #0008 + PutBlob + GetBlob +
ErrBlobNotFound + ErrBlobTooLarge; sqlite BLOB column; sha256 PK with
lower-case hex CHECK; 16 MiB hard cap as SQL CHECK; caller-owns-tx
contract; ON CONFLICT DO NOTHING idempotency), STOP and report — do
NOT pick an alternative yourself. Re-spawn the design subagent.

# Migration number lock (feedback_migration_number_lock)

Migration #0008 is LOCKED. File MUST be named exactly
`0008_blackboard_blobs.sql`. Do NOT renumber under any circumstance.
Migration #0007 is owned by W8 policy_revision per amendment PR #311
— W11 explicitly takes #0008 to avoid collision. If you believe a
different number is needed, STOP and re-spawn design.

# Pre-flight verification

Before starting, run:

  ls internal/orchestrator/state/migrations/
  grep -n "CurrentSchemaVersion" internal/orchestrator/state/migrate.go
  grep -n "func newTestDB" internal/orchestrator/state/substrate/helpers_test.go

Confirm: 0007 is the latest migration on `origin/main` (W8 amendment
PR #311 merged); CurrentSchemaVersion is 7; `0008_*.sql` does NOT yet
exist; newTestDB helper exists. If any fails (especially if #311 has
NOT merged), STOP and report — Wave A is gated on #311 per plan §1.

# Scope (exclusive write paths)

- internal/orchestrator/state/migrations/0008_blackboard_blobs.sql  (NEW)
- internal/orchestrator/state/substrate/blob.go                     (NEW)
- internal/orchestrator/state/substrate/blob_test.go                (NEW)
- internal/orchestrator/state/migrate.go                            (ONE-LINE delta: CurrentSchemaVersion 7 → 8)
- internal/orchestrator/state/migrate_test.go                       (add ONE new test: TestMigration0008_AppliesAndCreatesSchema)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/orchestrator/blackboard/  — that is T2/T3/T5's scope.
- Do NOT touch internal/orchestrator/blackboard_gc/  — that is T4's scope.
- Do NOT modify any existing migration file.
- Do NOT modify any existing substrate test fixture.

# Patterns to reuse (do NOT reinvent)

- Substrate migration file-header comment: copy from
  migrations/0006_substrate.sql (date + spec authority cite +
  DO-NOT-RENUMBER warning).
- ApplyAll migration runner: existing in migrate.go; no change needed
  beyond the CurrentSchemaVersion bump.
- newTestDB(t) helper: substrate/helpers_test.go — opens in-memory
  sqlite and applies all migrations.
- ON CONFLICT idempotency: existing substrate event-log writer.
- sha256 + hex.EncodeToString: std lib only.
- Error sentinels: mirror substrate's ErrReplay / ErrInvalidPayload
  naming style.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each of the 7 named tests:
  1. Write the test file first.
  2. Run `go test ./internal/orchestrator/state/substrate/ -run <TestName> -v`
     (or `./internal/orchestrator/state/` for the migrate test).
  3. CAPTURE the failing output. Paste at least 4 representative samples
     into the PR body's "Failing-test output (TDD capture)" section.
     "Tests would have failed" is NOT acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or logical group; squash later).

# Tests to land (7 named — spec §6 T1)

1. TestMigration0008_AppliesAndCreatesSchema
2. TestBlackboard_PutBlobRoundTrip
3. TestBlackboard_PutBlobIdempotent
4. TestBlackboard_PutBlobRejectsOversize
5. TestBlackboard_GetBlobNotFound
6. TestBlackboard_PutBlobRejectsAboveHardCap
7. TestBlackboard_DigestIsLowerCaseHex

# Workflow after green

  1. Run `make pre-push-check` — confirm clean. NEVER skip hooks
     (`--no-verify` is banned per feedback_pr_lint_gates).
  2. Re-run `go test ./internal/orchestrator/state/... -v` and confirm
     all 7 named tests green.
  3. Sweep introduced comments per feedback_comments_discipline
     (WHY not WHAT; godocs ≤ 1 line on test funcs).
  4. Run `bash scripts/doc-check.sh` and `bash scripts/stale-todo.sh`
     — both exit 0.
  5. Grep your diff and PR body against the banned-phrase token list
     in `scripts/doc-check.sh` (per `feedback_doc_check_banned_phrases`
     — 11-token marketing-prose lint). Reword any hit to a falsifiable
     claim before push.
  6. File the 10 [blackboard-followup] issues per the plan §7 list
     BEFORE opening this PR. Gather issue numbers.
  7. Push branch.
  8. Open PR via `gh pr create --base main --title "feat(w11): T1 blobs primitive + migration #0008" --body-file <path>` (NEVER heredoc per feedback_pr_lint_gates). PR body MUST cite the 10 followup issue numbers + end with the release-notes fence below.
  9. Verify the PR body's last line is the closing ` ``` ` of the
     release-notes fence (per feedback_pr_body_release_notes_fence).
     `gh pr view <NUM> --json body --jq .body | tail -3` MUST show:
       ```release-notes
       [FEATURE] substrate CAS blobs primitive ...
       ```
 10. Spawn ONE adversarial reviewer subagent (per
     feedback_adversarial_review + feedback_agent_pr_review) with the
     hunt list below. The reviewer measures the PR against the A+
     scorecard per feedback_grade_rubric.
 11. Apply reviewer findings inline OR file a tracking issue per
     feedback_unaddressed_load_bearing and cite the issue in the PR body.
 12. Re-run `make pre-push-check`; force-push if needed.
 13. Verify CI green (pr-lint, check-release-notes, check-tdd, build,
     test) BEFORE flipping automerge per feedback_review_before_automerge.
 14. Flip automerge ONLY after reviewer cleared the PR.

# Adversarial reviewer hunt list

- Migration filename is exactly `0008_blackboard_blobs.sql`. Not
  `0008_blobs.sql`, not `0008_substrate_blobs.sql`. Match plan §1.
- DDL CHECK constraints match spec §3.3 verbatim: digest is 64 lower-case
  hex chars; size_bytes is (0, 16777216].
- PutBlob does NOT begin/commit tx. Caller owns. Confirm via signature
  + test that wraps tx around PutBlob+GetBlob+ROLLBACK and verifies the
  blob did not persist.
- ErrBlobTooLarge fires BEFORE the INSERT. Zero rows written on oversize.
- ON CONFLICT(digest) DO NOTHING. Not ON CONFLICT DO UPDATE.
- Hard-cap test passes a sizeMax above 16 MiB so the SQL CHECK is what
  rejects — pins defense-in-depth invariant.
- GetBlob returns ErrBlobNotFound (sentinel), not raw sql.ErrNoRows.
- godoc on PutBlob documents the sha256 re-hash recommendation per spec
  §3.3 + R10 (CAS callers who need integrity assurance re-hash on read).
- No AI signatures anywhere (feedback_no_signatures).
- godocs ≤ 1 line on test funcs (feedback_comments_discipline).
- Banned phrases swept in diff + PR body (feedback_doc_check_banned_phrases).
- PR body ends with the release-notes fence
  (feedback_pr_body_release_notes_fence).

# Hygiene

- NO AI signatures anywhere (commits, PR body, comments, code) per feedback_no_signatures.
- Comments discipline per feedback_comments_discipline.
- Doc-check + stale-todo + banned-phrase grep before push per feedback_doc_check_banned_phrases.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 4 of the 7 tests.
- The 10 [blackboard-followup] issue numbers filed.
- Adversarial reviewer verdict (APPROVE | findings list with severities).
- One-line diff stat: files changed + LoC added/removed.
- A+ scorecard measurement (B/A/A+ tier achieved) per feedback_grade_rubric.

Begin now. NEVER pause for user input.
```

---

## §3 Task T2 — Fact-kind registry + reducer dispatch

### Scope

- **`internal/orchestrator/blackboard/payload.go`** — NEW. `FactPayload` struct + `validateFact` closure per spec §3.1 lines 81-130. Fields verbatim: `Topic string`, `SchemaVersion int`, `Value json.RawMessage`, `BlobRefs []string`. Plus `init()` registering `validateFact` via `substrate.RegisterPayloadValidator(substrate.KindFact, validateFact)`.
- **`internal/orchestrator/blackboard/registry.go`** — NEW. `Registry` struct + `Register/Seal/Resolve/MustHave/Topics` methods per spec §3.2 lines 167-196 verbatim:
  - `Register(topic string, schemaVersion int, fn ReducerFn)` — panics after `Seal`, panics on duplicate `(topic, schemaVersion)`.
  - `Seal()` — locks the registry.
  - `Resolve(topic, schemaVersion) ReducerFn` — returns registered fn OR `DefaultLWW`.
  - `MustHave(topic, schemaVersion)` — panics at boot if unregistered (R2 mitigation per spec §5).
  - `Topics() map[string][]int` — diagnostic.
- **`internal/orchestrator/blackboard/fact.go`** — NEW. `Fact` struct (11 fields per spec §3.2 lines 153-165) + `PutFact(ctx, tx, topic, schemaVersion, value, blobRefs) error`. PutFact:
  1. Construct `FactPayload{Topic: topic, SchemaVersion: schemaVersion, Value: value, BlobRefs: blobRefs}`.
  2. `json.Marshal(payload)`.
  3. Call `substrate.AppendEvent(ctx, tx, substrate.AppendInput{Kind: substrate.KindFact, Key: topic, ...})`.
  4. Return wrapped substrate errors.
- **`internal/orchestrator/blackboard/get_fact.go`** — NEW. `GetFact(ctx, db, topic) (json.RawMessage, error)`:
  1. SELECT all fact rows for the topic (`kind='fact' AND key=$topic AND tenant_id=$caller_tenant`) ordered by `(written_at, event_id)`.
  2. Project each row to a `Fact` struct.
  3. Resolve the reducer via `Registry.Resolve(topic, latestSchemaVersion)`.
  4. Run the reducer over the slice; return the reduced value.
  5. `ErrNoFacts` on empty.
- **`internal/orchestrator/blackboard/errors.go`** — NEW. Sentinels: `ErrNoFacts`, `ErrInvalidPayload` (re-export from substrate for caller convenience), `ErrRegistrySealed`, `ErrDuplicateRegistration`.
- **`internal/orchestrator/blackboard/reducers/`** — NEW sub-package. One file per reducer per spec §3.2 lines 202-207:
  - `lww.go` — `LWW`: latest fact by `(written_at, event_id)`. ALSO ships under the alias `DefaultLWW` in the parent package (re-export).
  - `set_union.go` — `SetUnion`: JSON array union across all facts.
  - `write_once.go` — `WriteOnce`: first fact wins.
  - `append.go` — `Append`: full ordered list.
- **`internal/orchestrator/blackboard/*_test.go`** — 9 named tests below.

### Prereqs (cite spec sections)

- Spec §1 in-scope items "Typed facts" + "Reducers".
- Spec §3.1 — `kind=fact` event row shape + `FactPayload` JSON + `validateFact` verbatim.
- Spec §3.2 — Registry API + `Fact` struct + built-in reducer list + boot-sealed-not-mutable rationale.
- Spec §6 T2 — exhaustive named-test list (9 tests; B/A/A+ tiers).
- Spec §7 B/A/A+ — rubric criteria.
- Spec §8 — file-disjoint row T2 + cross-task seam contracts (registry exports).
- Spec §9 R2 (schema-version skew → MustHave assertion + dual-version recipe), R3 (reducer cycle → purity by signature + lint), R7 (non-determinism → determinism test).
- Spec §11 #1 — substrate validator re-registration question (resolve before dispatch).

### Existing patterns to reuse (do NOT reinvent)

- **Substrate AppendEvent:** `internal/orchestrator/state/substrate/event.go::AppendEvent(ctx, tx, AppendInput)` — exported by T-S1 #224. Use verbatim; do NOT write directly to `substrate_events`.
- **RegisterPayloadValidator:** T-S1 #224's open-extension hook. Pattern: `func init() { substrate.RegisterPayloadValidator(substrate.KindFact, validateFact) }`. Confirm at dispatch time that substrate handles duplicate registration; if it panics, BLOCKED.
- **Boot-sealed registry pattern:** mirrors `sql.Register`, `image.RegisterFormat`, `hash.Register*` from the std lib. Sync via `sync.RWMutex`; panic on misuse; reviewers + linters already familiar.
- **Reducer purity contract:** signature `ReducerFn func(facts []Fact) (json.RawMessage, error)` — no `*sql.DB`, no `*sql.Tx`, no `context.Context`, no `time.Now`. The signature ITSELF prevents reentrancy. T5 ships the lint binary that AST-checks this property; T2's tests only assert the signature.
- **Substrate kind constants:** `substrate.KindFact` — exported by T-S1 #224.
- **Tenant scoping:** every `GetFact` SELECT filters `tenant_id = caller.TenantID`. Mirror substrate's reader pattern.

### TDD test list (named tests from spec §6 T2; failing-output capture step required)

**B-tier (6 named tests; spec §6 T2 + §7 B):**

1. `TestBlackboard_PutFactRoundTrip` — `PutFact(topic="files.touched", v=1, value, nil)` writes one substrate event with `kind='fact'`, `key=topic`; `GetFact(topic)` returns value via `DefaultLWW`.
2. `TestBlackboard_RegistryRegisterAndResolve` — `Register("schema.t", 1, fn)` then `Resolve("schema.t", 1)` returns fn; `Resolve("schema.t", 2)` returns `DefaultLWW`.
3. `TestBlackboard_RegistrySealRejectsLateRegister` — `Register` after `Seal` panics with `ErrRegistrySealed` (or equivalent panic message).
4. `TestBlackboard_RegistryDuplicateRegisterPanics` — same `(topic, schemaVersion)` registered twice ⇒ panic with `ErrDuplicateRegistration`.
5. `TestBlackboard_FactPayloadValidator_RejectsEmptyTopic` — `validateFact` returns `substrate.ErrInvalidPayload` for `{topic: "", ...}`.
6. `TestBlackboard_FactPayloadValidator_RejectsBadBlobRef` — blob_ref with non-64-char length ⇒ `substrate.ErrInvalidPayload`.

**A-tier (2 named tests; spec §6 T2 A-tier + §7 A):**

7. `TestBlackboard_ReducerDeterminism` (R7) — every registered reducer (LWW, SetUnion, WriteOnce, Append) runs 100× on the same 10-fact input; asserts identical output across all 100 runs.
8. `TestBlackboard_RegistryAssertsAllVersions` (R2) — `Registry.MustHave("topic.x", 2)` panics at boot when only `("topic.x", 1)` is registered. Pins schema-version-skew defense.

**A+-tier (1 named test; spec §6 T2 A+ + §7 A+):**

9. `TestBlackboard_BuiltinReducers_SetUnion_WriteOnce_Append` — each built-in reducer exercised on a 5-fact sequence; asserts exact output shape.

Total T2: **9 named tests**. PR body lists every name + pasted failing-output excerpt for AT LEAST 5 representative cases.

### PR body skeleton — T2

````
## Summary

W11 blackboard T2: ships the fact-kind registry + reducer dispatch
layer per docs/engineer/specs/2026-06-01-w11-blackboard-design.md
§3.1 + §3.2.

- internal/orchestrator/blackboard/payload.go — FactPayload struct +
  validateFact + init() registering against substrate's
  RegisterPayloadValidator(KindFact, ...) open-extension hook.
- internal/orchestrator/blackboard/registry.go — Registry +
  Register/Seal/Resolve/MustHave/Topics; boot-sealed pattern from
  sql.Register; panic-on-misuse for safety-critical map.
- internal/orchestrator/blackboard/fact.go — PutFact wraps
  substrate.AppendEvent with the typed payload.
- internal/orchestrator/blackboard/get_fact.go — GetFact folds the
  fact log + applies the resolved reducer.
- internal/orchestrator/blackboard/reducers/ — four built-in reducers:
  LWW (default), SetUnion, WriteOnce, Append.

## Why

Per spec §3.2: reducer is a property of the topic, not a column on
the row. (topic, schema_version) key lets v1+v2 readers coexist
during migration windows. Boot-sealed registry mirrors std lib safety
posture for security/correctness-critical maps. Per spec §3.1: typed
payload validation reuses substrate's existing dispatch — no new SQL
primitive, no new HMAC pipeline, no second nonce.

## Test plan

- [x] TestBlackboard_PutFactRoundTrip
- [x] TestBlackboard_RegistryRegisterAndResolve
- [x] TestBlackboard_RegistrySealRejectsLateRegister
- [x] TestBlackboard_RegistryDuplicateRegisterPanics
- [x] TestBlackboard_FactPayloadValidator_RejectsEmptyTopic
- [x] TestBlackboard_FactPayloadValidator_RejectsBadBlobRef
- [x] TestBlackboard_ReducerDeterminism (pins R7)
- [x] TestBlackboard_RegistryAssertsAllVersions (pins R2)
- [x] TestBlackboard_BuiltinReducers_SetUnion_WriteOnce_Append
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 5 reps>

## Deletion default

T2 ELIMINATES the bespoke "facts" table that wedge_blackboard.md
originally proposed. Substrate's kind='fact' event channel is the
storage — saves one table, one migration, one HMAC pipeline, one
nonce column, one supersedes FK. The read API collapses from three
calls (fact.get / fact.list / fact.semantic) to two (GetFact +
TailFacts; TailFacts is T3). Tag-faceted + semantic queries deferred
to F2 + F3 followups until a real consumer demands them.

## Followup issues filed (per feedback_unaddressed_load_bearing)

(T1 PR filed the 10 [blackboard-followup] issues; cite the numbers here.)

```release-notes
[FEATURE] blackboard typed-fact registry + reducer dispatch (PutFact/GetFact + four built-in reducers)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w11-t2. Branch: `feat/w11-t2-fact-registry` off
main.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w11-blackboard-design.md.
Read ALL of: §1 (scope), §3.1 (FactPayload + validator + init pattern
verbatim), §3.2 (Registry API + Fact struct + built-in reducers + sealed-at-boot
rationale), §6 T2 (named test list), §7 (B/A/A+ rubric), §8 (file-disjoint
row T2 + cross-task seam contracts), §9 R2 (schema-version skew),
R3 (reducer cycle by signature), R7 (non-determinism).

Plan: docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md §3 (this task).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (T2 OWNS the entire blackboard/ package +
FactPayload + Registry + Fact struct + PutFact + GetFact + four built-in
reducers + init() registering via substrate.RegisterPayloadValidator;
boot-sealed registry that panics on duplicate or post-Seal registration;
reducer purity-by-signature contract), STOP and report — do NOT pick
an alternative. Re-spawn the design subagent.

# Pre-flight verification (resolves spec §11 #1)

Before starting, run:

  grep -n "RegisterPayloadValidator\|registerPayloadValidator" internal/orchestrator/state/substrate/validate.go
  grep -n "KindFact" internal/orchestrator/state/substrate/*.go
  grep -n "func AppendEvent" internal/orchestrator/state/substrate/event.go

Confirm:
- RegisterPayloadValidator exists, exported, takes (kind, fn) — DOES NOT panic on duplicate registration (silent overwrite is the desired behaviour for our re-registration use case). If substrate panics on duplicate ⇒ STOP and report — your dispatch is BLOCKED until a substrate-side amendment lands.
- KindFact constant exists.
- AppendEvent signature is (ctx, tx, AppendInput) returning error.

# Scope (exclusive write paths — file-disjoint with T1/T3/T4)

- internal/orchestrator/blackboard/payload.go         (NEW)
- internal/orchestrator/blackboard/payload_test.go    (NEW)
- internal/orchestrator/blackboard/registry.go        (NEW)
- internal/orchestrator/blackboard/registry_test.go   (NEW)
- internal/orchestrator/blackboard/fact.go            (NEW)
- internal/orchestrator/blackboard/get_fact.go        (NEW)
- internal/orchestrator/blackboard/get_fact_test.go   (NEW)
- internal/orchestrator/blackboard/errors.go          (NEW)
- internal/orchestrator/blackboard/reducers/lww.go         (NEW)
- internal/orchestrator/blackboard/reducers/set_union.go   (NEW)
- internal/orchestrator/blackboard/reducers/write_once.go  (NEW)
- internal/orchestrator/blackboard/reducers/append.go      (NEW)
- internal/orchestrator/blackboard/reducers/*_test.go      (NEW; one per reducer)

You MUST NOT touch any other file. Specifically:
- Do NOT create tail.go or cursor.go — those are T3's exclusive scope.
- Do NOT touch internal/orchestrator/blackboard/otel.go (yet) — that is T5's exclusive scope.
- Do NOT touch internal/orchestrator/blackboard_gc/  — that is T4's scope.
- Do NOT touch internal/orchestrator/state/substrate/  — T1 owns the blob.go addition.
- Do NOT modify any existing substrate validator / DDL / kind enum.

# Patterns to reuse (do NOT reinvent)

- substrate.AppendEvent — write API. Caller passes *sql.Tx; do NOT
  open or commit. Use AppendInput shape from T-S1 #224.
- substrate.RegisterPayloadValidator(KindFact, validateFact) — open-extension
  hook. init() block.
- sql.Register-style sealed registry: sync.RWMutex; panic on misuse.
- substrate.KindFact constant.
- Reducer purity: signature ReducerFn func(facts []Fact) (json.RawMessage, error)
  is the entire contract. NO ctx, NO *sql.DB, NO *sql.Tx, NO time.Now.
  T5 ships the AST lint; T2 ships the signature.

# Workflow steps (TDD discipline)

For each of the 9 named tests:
  1. Write the test first.
  2. Run `go test ./internal/orchestrator/blackboard/... -run <TestName> -v`.
  3. Capture failing output. Paste at least 5 representative samples
     into the PR body.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or logical group).

# Tests to land (9 named — spec §6 T2)

B1. TestBlackboard_PutFactRoundTrip
B2. TestBlackboard_RegistryRegisterAndResolve
B3. TestBlackboard_RegistrySealRejectsLateRegister
B4. TestBlackboard_RegistryDuplicateRegisterPanics
B5. TestBlackboard_FactPayloadValidator_RejectsEmptyTopic
B6. TestBlackboard_FactPayloadValidator_RejectsBadBlobRef
A1. TestBlackboard_ReducerDeterminism                         (pins R7)
A2. TestBlackboard_RegistryAssertsAllVersions                 (pins R2)
A+1. TestBlackboard_BuiltinReducers_SetUnion_WriteOnce_Append

# Workflow after green

  1. Run `make pre-push-check` — confirm clean.
  2. Re-run `go test ./internal/orchestrator/blackboard/... -v`.
  3. Sweep comments per feedback_comments_discipline.
  4. Run `bash scripts/doc-check.sh` and `bash scripts/stale-todo.sh`.
  5. Grep banned phrases in diff + PR body; reword on hit.
  6. Push branch.
  7. Open PR with `gh pr create --base main --title "feat(w11): T2 fact-kind registry + reducer dispatch" --body-file <path>`. PR body cites T1's 10 followup issue numbers + ends with the release-notes fence.
  8. Verify release-notes fence with `gh pr view <NUM> --json body --jq .body | tail -3`.
  9. Spawn ONE adversarial reviewer subagent (hunt list below). Reviewer measures vs A+ scorecard per feedback_grade_rubric.
 10. Apply findings inline OR file tracking issue + cite per feedback_unaddressed_load_bearing.
 11. Verify CI green before flipping automerge.
 12. Flip automerge ONLY after reviewer cleared.

# Adversarial reviewer hunt list

- FactPayload field shapes EXACT match to spec §3.1 lines 81-89.
  JSON tags verbatim ("topic", "schema_version", "value", "blob_refs,omitempty").
- validateFact rejects: empty topic, schema_version < 1, blob_ref of
  wrong length (≠ 64), unparseable JSON. Each failure returns
  substrate.ErrInvalidPayload (NOT a new local sentinel).
- Registry.Register PANICS (not returns error) on duplicate AND on
  post-Seal calls. Mirror std lib safety posture. Test asserts the
  panic value type.
- Registry.Resolve returns DefaultLWW (not nil, not panic) for unregistered
  tuples. Pins the silent-fallback contract.
- PutFact does NOT begin or commit tx. Caller owns. Confirm via test
  that ROLLBACK after PutFact returns clean DB.
- GetFact filters by tenant_id = caller.TenantID. No cross-tenant leak.
  Test asserts a cross-tenant write is invisible to GetFact.
- Reducer signature: `func(facts []Fact) (json.RawMessage, error)`.
  NO ctx, NO *sql.DB, NO *sql.Tx. The signature is the contract.
- Built-in reducers in reducers/ subpackage. LWW also re-exported as
  blackboard.DefaultLWW for caller convenience.
- Cyclic-import check: `go list -deps ./internal/orchestrator/blackboard/...`
  shows no edge to internal/orchestrator/blackboard_gc/ or to spawner/.
- No AI signatures anywhere (feedback_no_signatures).
- godocs ≤ 1 line on test funcs (feedback_comments_discipline).
- Banned phrases swept (feedback_doc_check_banned_phrases).
- PR body ends with release-notes fence (feedback_pr_body_release_notes_fence).

# Hygiene

- NO AI signatures anywhere (feedback_no_signatures).
- Comments discipline per feedback_comments_discipline.
- Doc-check + stale-todo + banned-phrase grep before push.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 5 of the 9 tests.
- Adversarial reviewer verdict + severities.
- One-line diff stat.
- A+ scorecard measurement per feedback_grade_rubric.

Begin now. NEVER pause for user input.
```

---

## §4 Task T3 — TailFacts API + cursor semantics

### Scope

- **`internal/orchestrator/blackboard/tail.go`** — NEW. `TailFacts(ctx, db, topic, since Cursor) (factc <-chan Fact, errc <-chan error, err error)` per spec §3.4 lines 278-302 verbatim:
  - Polling loop per tick (every `blackboard.tail_interval_ms`, default 1s, bounded 100ms-10s).
  - SELECT bounded by `LIMIT 100` per tick (spec §3.4 line 318) — backlogged-subscriber DoS defense.
  - Tenant-scoped (`WHERE tenant_id = $caller_tenant`).
  - Cursor encodes `substrate_events.rowid`; advances monotonically per row.
  - On ctx cancel: closes `factc` then `errc`.
- **`internal/orchestrator/blackboard/cursor.go`** — NEW. `Cursor` (type-aliased string with no exported constructors); `CursorEmpty` constant ("" → "tail from now"). Internal helpers `encodeCursor(rowid) Cursor` + `decodeCursor(c Cursor) (rowid int64, err error)` unexported.
- **`internal/orchestrator/blackboard/tail_test.go`** + **`cursor_test.go`** — 9 named tests below.

### Prereqs (cite spec sections)

- Spec §1 in-scope item "Fact-subscription API".
- Spec §3.4 — `TailFacts` signature + polling design + bounded-batch + cursor opacity + Go dual-channel pattern verbatim.
- Spec §3.6 — `blackboard.tail_interval_ms` config bounds (100-10000).
- Spec §6 T3 — exhaustive named-test list (9 tests; B/A/A+ tiers).
- Spec §7 B/A/A+ — rubric criteria.
- Spec §8 — file-disjoint row T3 + cross-task seam contracts (Cursor + TailFacts exports).
- Spec §9 R5 (subscription leak → ctx-cancel + idle watchdog + lint), R6 (cursor staleness → opacity).

### Existing patterns to reuse (do NOT reinvent)

- **Go dual-channel pattern:** stream channel + separate err channel + terminal nil-receive. Standard pattern; see `database/sql.Rows`.
- **Polling under sqlite:** substrate's reader / Fold uses `rowid` as the natural-order cursor. Reuse the same column.
- **Tenant scoping:** mirror substrate's reader pattern (`WHERE tenant_id = $caller_tenant`).
- **Cursor opacity via type-aliased string:** mirrors GraphQL Relay cursor spec + AWS pagination tokens; no exported constructors keeps callers honest if the backing query swaps.
- **Bounded batch via SQL LIMIT:** mirrors Kafka consumer poll semantics; substrate's Fold already uses `LIMIT` for batched reads.
- **Config:** `blackboard.Config` snapshot (T5 ships the config struct; T3 reads `cfg.TailIntervalMS` via a getter or constructor param). Until T5 lands the wired-up config, T3 takes `tailInterval time.Duration` as a parameter or pulls from a package-level var with a default of 1s (whichever the implementer judges cleaner — preference: explicit `cfg.TailIntervalMS` reader from a small `internal/orchestrator/blackboard/config.go` helper that T5 later replaces; document the seam).

### TDD test list (named tests from spec §6 T3; failing-output capture step required)

**B-tier (5 named tests; spec §6 T3 + §7 B):**

1. `TestBlackboard_TailFactsStreamsNewFacts` — `TailFacts(topic, CursorEmpty)` returns channel; writes after the call land on the channel.
2. `TestBlackboard_TailFactsCursorReplaysFromPosition` — write 3 facts; tail with cursor pointing before fact #2; stream emits #2 + #3 (not #1).
3. `TestBlackboard_TailFactsCtxCancelClosesChannel` — cancel ctx ⇒ both `factc` + `errc` close (assert via `for range` exits).
4. `TestBlackboard_TailFactsFiltersByTopic` — writer emits topic A + topic B; tail for A only sees A.
5. `TestBlackboard_TailFactsFiltersByTenant` — cross-tenant write ⇒ tail filtered by caller's tenant_id does NOT see it.

**A-tier (3 named tests; spec §6 T3 A-tier + §7 A):**

6. `TestBlackboard_TailFactsBoundedBatch` — 1000 backlogged facts; first tick emits ≤ 100; subsequent ticks emit the rest.
7. `TestBlackboard_TailFactsRespectsTailInterval` — interval=200ms; assert ticks happen at 200ms ± 50ms (mock clock or `time.Since` budget).
8. `TestBlackboard_TailFactsCtxNoLeak` (R5) — N=100 ctx-cancelled `TailFacts` calls; `runtime.NumGoroutine()` returns to baseline within 2s.

**A+-tier (1 named test; spec §6 T3 A+ + §7 A+):**

9. `TestBlackboard_TailCtxLintIntegrationCI` — `tools/lint-blackboard-tail-ctx` (delivered in T5) against the full repo; asserts every `TailFacts` call site uses a cancellable ctx. Lint binary itself is T5; T3 ships the test that exercises it once T5 lands (T3 may stub-fail the test until T5 merges OR ship it under a `//go:build a_plus` build tag so it doesn't gate CI before T5 wires the binary).

Total T3: **9 named tests**. PR body lists every name + pasted failing-output excerpt for AT LEAST 5 representative cases.

### PR body skeleton — T3

````
## Summary

W11 blackboard T3: ships TailFacts polling API + opaque Cursor type
per docs/engineer/specs/2026-06-01-w11-blackboard-design.md §3.4.

- internal/orchestrator/blackboard/tail.go — TailFacts(ctx, db, topic,
  since) returns (factc, errc, err); polling under the hood at
  blackboard.tail_interval_ms; LIMIT-100 per tick; tenant-scoped.
- internal/orchestrator/blackboard/cursor.go — Cursor (type-aliased
  string with no exported constructors except CursorEmpty); opacity
  protects against backing-query swap.

## Why

Per spec §3.4: sqlite has no LISTEN/NOTIFY equivalent. Polling is the
sqlite-compatible choice; if profiling under real load shows >5% CPU
in tail SELECTs, we revisit via [blackboard-followup] F1. LIMIT 100
per tick blocks a backlogged subscriber from yanking 10K rows and
starving others. Cursor opacity protects persisted cursors against a
future multi-writer swap (W9 — F7).

## Test plan

- [x] TestBlackboard_TailFactsStreamsNewFacts
- [x] TestBlackboard_TailFactsCursorReplaysFromPosition
- [x] TestBlackboard_TailFactsCtxCancelClosesChannel
- [x] TestBlackboard_TailFactsFiltersByTopic
- [x] TestBlackboard_TailFactsFiltersByTenant
- [x] TestBlackboard_TailFactsBoundedBatch
- [x] TestBlackboard_TailFactsRespectsTailInterval
- [x] TestBlackboard_TailFactsCtxNoLeak (pins R5)
- [x] TestBlackboard_TailCtxLintIntegrationCI (A+; gated on T5 lint binary)
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 5 reps>

## Deletion default

T3 does NOT ship a goroutine-broadcast fan-out, a sync.Cond wakeup
queue, or a websocket bridge. Polling + cursor is one SELECT per tick
per subscriber — the simplest correct shape. Realtime push is F1
deferred to v2 if profiling demands. Cursor type-aliased string with
no exported constructors keeps the persisted-cursor migration story
trivial — callers can ONLY persist what TailFacts handed them.

## Followup issues filed (per feedback_unaddressed_load_bearing)

(T1 PR filed the 10 [blackboard-followup] issues; cite the numbers here.)

```release-notes
[FEATURE] blackboard TailFacts polling subscription API + opaque cursor type
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w11-t3. Branch: `feat/w11-t3-tail-facts` off main.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w11-blackboard-design.md.
Read ALL of: §1 (scope), §3.4 (TailFacts signature + polling design +
bounded-batch + cursor opacity verbatim), §3.6 (tail_interval_ms config
bounds), §6 T3 (named test list), §7 (B/A/A+ rubric), §8 (file-disjoint
row T3 + seam contracts), §9 R5 (subscription leak — ctx-cancel + lint),
R6 (cursor staleness — opacity).

Plan: docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md §4 (this task).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (T3 OWNS tail.go + cursor.go in
internal/orchestrator/blackboard/; polling-based stream; LIMIT-100 per
tick; tenant-scoped SELECT; Cursor type-aliased string with no exported
constructors except CursorEmpty; dual-channel Go pattern), STOP and
report — do NOT pick an alternative. Re-spawn the design subagent.

# Pre-flight verification

  ls internal/orchestrator/blackboard/      # confirm T2 has landed Fact + Registry, or you are branching off T2 stub
  grep -n "type Fact " internal/orchestrator/blackboard/fact.go
  grep -n "kind=fact" internal/orchestrator/state/substrate/*.go

Confirm Fact struct + KindFact + kind='fact' rows are queryable. If T2
has not merged, branch off T2's open PR (`git fetch && git checkout -b
feat/w11-t3-tail-facts origin/feat/w11-t2-fact-registry`); after T2 lands,
rebase onto main.

# Scope (exclusive write paths — file-disjoint with T1/T2/T4)

- internal/orchestrator/blackboard/tail.go        (NEW)
- internal/orchestrator/blackboard/tail_test.go   (NEW)
- internal/orchestrator/blackboard/cursor.go      (NEW)
- internal/orchestrator/blackboard/cursor_test.go (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT modify fact.go / get_fact.go / registry.go / payload.go — those are T2's exclusive scope.
- Do NOT create reducers/ files — that is T2's scope.
- Do NOT create otel.go — that is T5's exclusive scope.
- Do NOT touch internal/orchestrator/blackboard_gc/ — that is T4's scope.

# Patterns to reuse (do NOT reinvent)

- Go dual-channel pattern: stream channel + err channel; close both
  on ctx-cancel.
- substrate.Fold's rowid-as-cursor pattern.
- Tenant-scoped SELECT: mirror substrate reader's tenant_id filter.
- Bounded batch via SQL LIMIT 100.
- Cursor opacity via type Cursor string + only CursorEmpty exported.
- Config interim: ship `internal/orchestrator/blackboard/config.go`
  helper with `TailIntervalMS` defaulting to 1000 (bounded 100-10000);
  T5 later wires the operator config struct into this helper.

# Workflow steps (TDD discipline)

For each of the 9 named tests:
  1. Write the test first.
  2. Run `go test ./internal/orchestrator/blackboard/ -run <TestName> -v`.
  3. Capture failing output. Paste at least 5 representative samples.
  4. Implement.
  5. Re-run; confirm pass.

# Tests to land (9 named — spec §6 T3)

B1. TestBlackboard_TailFactsStreamsNewFacts
B2. TestBlackboard_TailFactsCursorReplaysFromPosition
B3. TestBlackboard_TailFactsCtxCancelClosesChannel
B4. TestBlackboard_TailFactsFiltersByTopic
B5. TestBlackboard_TailFactsFiltersByTenant
A1. TestBlackboard_TailFactsBoundedBatch
A2. TestBlackboard_TailFactsRespectsTailInterval
A3. TestBlackboard_TailFactsCtxNoLeak                  (pins R5)
A+1. TestBlackboard_TailCtxLintIntegrationCI           (A+; build-tag gated until T5)

# Workflow after green

  1. make pre-push-check clean.
  2. go test ./internal/orchestrator/blackboard/... -v.
  3. Sweep comments.
  4. doc-check + stale-todo + banned-phrase grep.
  5. Push.
  6. gh pr create --base main --title "feat(w11): T3 TailFacts polling API + opaque cursor" --body-file <path>.
  7. Verify release-notes fence at PR body tail.
  8. Spawn adversarial reviewer (hunt list below).
  9. Apply findings or file tracking.
 10. CI green before automerge.

# Adversarial reviewer hunt list

- TailFacts signature EXACT match to spec §3.4 lines 286-293: returns
  (factc <-chan Fact, errc <-chan error, err error). NOT a single
  channel of (Fact, error) pairs. NOT a callback-based API.
- LIMIT 100 per tick. Magic number is the spec's load-bearing v1
  choice (spec §3.4 line 318); cite the spec line in the SQL comment.
- ctx-cancel closes factc THEN errc (order matters for the Go for-range pattern).
- Tenant_id filter on every SELECT. Cross-tenant write test in B5.
- Cursor opacity: NO exported helpers like NewCursor / ParseCursor.
  Only CursorEmpty. Type is `type Cursor string` for persistence shape.
- Polling interval bounds: 100ms ≤ interval ≤ 10000ms (10s). Clamp at
  read; reject out-of-range with a config error (NOT a silent clamp).
- Goroutine leak test (A3) asserts runtime.NumGoroutine() delta < 5
  across 100 cancelled subscribers within 2s.
- A+ lint test build-tag-gated until T5 lands the lint binary —
  document the gate (`//go:build a_plus`) so CI doesn't fail before
  T5 merges.
- No AI signatures (feedback_no_signatures).
- godocs ≤ 1 line on test funcs.
- Banned phrases swept.
- Release-notes fence at PR body tail.

# Hygiene

- No AI signatures. Comments discipline. Doc-check + stale-todo + banned-phrase grep before push.

# Return format

- PR URL.
- Pasted failing output for at least 5 of 9 tests.
- Reviewer verdict + severities.
- One-line diff stat.
- A+ scorecard measurement.

Begin now. NEVER pause for user input.
```

---

## §5 Task T4 — Blob orphan GC sweep job

### Scope

- **`internal/orchestrator/blackboard_gc/sweep.go`** — NEW. Sweep loop per spec §3.5 lines 340-359:
  - `Sweep(ctx, db, cfg) (deleted int, err error)` — one pass:
    1. SELECT digests where `created_at < now_ms - grace_secs * 1000` AND digest NOT IN (the live-set subquery using `json_each(payload_json, '$.blob_refs')` filtered by `kind='fact' AND payload_json LIKE '%blob_refs%'`).
    2. LIMIT 100 (spec §3.5 + R9 mitigation).
    3. DELETE each in its own short tx.
  - `Run(ctx, db, cfg) error` — long-loop driver. Tick interval = `grace_secs / 24` (default: hourly tick across 24h grace ⇒ orphan gets ~24 chances). Goroutine recovery on panic (R-defense; logs + continues next tick).
  - Span emission: each `Sweep` opens one `blackboard.gc.sweep` span with attrs `regatta.blackboard.gc.deleted` + `regatta.blackboard.gc.scanned`.
- **`internal/orchestrator/blackboard_gc/config.go`** — NEW. `Config` struct: `DB *sql.DB`, `Clock func() time.Time`, `GraceSecs int` (default 86400; min 3600), `BatchSize int` (default 100), `Enabled bool` (operator opt-out per spec §3.5 line 361), `Tracer`, `Logger`.
- **`internal/orchestrator/blackboard_gc/{sweep,config}_test.go`** — 6 named tests below.

### Prereqs (cite spec sections)

- Spec §1 in-scope item "Blob GC".
- Spec §3.5 — sweep query verbatim + mark-and-sweep rationale + GC race defense + operator opt-out.
- Spec §3.6 — `blackboard.blob_gc_enabled`, `blob_gc_grace_secs` config fields.
- Spec §6 T4 — exhaustive named-test list (6 tests; B/A tiers).
- Spec §7 B/A — rubric criteria.
- Spec §8 — file-disjoint row T4 + cross-task seam contracts (`Run` + `Sweep` exports).
- Spec §9 R4 (GC race — grace window defense; T1 populated `created_at`, T4 enforces the filter), R9 (json_each performance — bounded batch + partial LIKE-filter), R-recovery (panic-mid-sweep → goroutine recovers + continues).

### Existing patterns to reuse (do NOT reinvent)

- **Bazel remote-cache GC + Cassandra `gc_grace_seconds`:** identical mark-and-sweep + grace-window pattern (per spec §4 "Existing patterns reused" table row 8). Don't reinvent.
- **Bounded batch via SQL LIMIT 100:** spec §3.5 line 354 + §9 R9 mitigation.
- **Per-DELETE short tx:** prevents long-running locks on substrate_blobs; mirrors substrate's reader pattern.
- **Goroutine panic recovery:** standard Go pattern; `defer func() { if r := recover(); r != nil { log.Error("sweep panic", ...) } }()` wrapping the tick body. Continues next tick.
- **Operator opt-out:** if `cfg.Enabled == false`, `Run` returns immediately. NO goroutine started.

### TDD test list (named tests from spec §6 T4; failing-output capture step required)

**B-tier (4 named tests; spec §6 T4 + §7 B):**

1. `TestBlackboard_GCSweepsOrphans` — insert blob, never reference; advance fake-clock past `grace_secs`; `Sweep()` deletes the row.
2. `TestBlackboard_GCSparesReferencedBlobs` — insert blob, write fact whose payload references the digest; `Sweep()` does NOT delete.
3. `TestBlackboard_GCSpareesYoungOrphans` (R4) — orphan blob with `created_at = now - grace + 1s` ⇒ `Sweep()` does NOT delete (grace window holds).
4. `TestBlackboard_GCOptOut` — `cfg.Enabled = false` ⇒ `Run()` returns immediately; no goroutine started; orphans survive forever.

**A-tier (2 named tests; spec §6 T4 A-tier + §7 A):**

5. `TestBlackboard_GCBatchSize` — 500 orphans; first `Sweep()` deletes 100; subsequent calls each delete another 100 (bounded batch).
6. `TestBlackboard_GCSurvivesPanicMidSweep` — induce a panic in the sweep loop (e.g. via a poisoned mock DB on the second DELETE); goroutine recovers, logs at ERROR, continues on next tick (assert via test-clock advance + second tick completes).

Total T4: **6 named tests**. PR body lists every name + pasted failing-output excerpt for AT LEAST 4 representative cases.

### PR body skeleton — T4

````
## Summary

W11 blackboard T4: ships the blob orphan GC sweep job per
docs/engineer/specs/2026-06-01-w11-blackboard-design.md §3.5.

- internal/orchestrator/blackboard_gc/sweep.go — Sweep + Run.
  Mark-and-sweep over substrate_blobs filtered by created_at younger
  than grace_secs AND digest NOT IN live-set extracted from
  payload_json.blob_refs via json_each. LIMIT 100; per-DELETE short tx.
- internal/orchestrator/blackboard_gc/config.go — Config struct with
  operator opt-out (Enabled=false ⇒ goroutine not started).

## Why

Per spec §3.5: ref-counting requires a counter column on substrate_blobs +
a mutation per fact write — substrate's append-only contract forbids
that. Mark-and-sweep reads the fact log to compute the live set; bounded
batch + per-DELETE short tx keeps a hostile producer from OOMing the
sweep. Grace window (24h default, 1h min) makes the
PutBlob-then-PutFact race effectively impossible.

## Test plan

- [x] TestBlackboard_GCSweepsOrphans
- [x] TestBlackboard_GCSparesReferencedBlobs
- [x] TestBlackboard_GCSpareesYoungOrphans (pins R4)
- [x] TestBlackboard_GCOptOut
- [x] TestBlackboard_GCBatchSize
- [x] TestBlackboard_GCSurvivesPanicMidSweep
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 4 reps>

## Deletion default

T4 does NOT add a ref-count column on substrate_blobs. Saves one
column + one mutation per fact write + one consistency invariant (counter
must equal actual references). Mark-and-sweep reads the log; eventual
consistency through the grace window is the chosen tradeoff. Operator
opt-out is one config bool — no separate disable-GC daemon, no
backup-and-restore-from-disabled-state recipe.

## Followup issues filed (per feedback_unaddressed_load_bearing)

(T1 PR filed the 10 [blackboard-followup] issues; cite the numbers here,
particularly F8 — materialized blob_refs index if json_each becomes the bottleneck.)

```release-notes
[FEATURE] blackboard orphan-blob GC sweep job (operator opt-out via blackboard.blob_gc_enabled)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w11-t4. Branch: `feat/w11-t4-blob-gc` off main.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w11-blackboard-design.md.
Read ALL of: §1 (scope), §3.5 (sweep query verbatim + mark-and-sweep
rationale + GC race defense + opt-out), §3.6 (config fields), §6 T4
(named test list), §7 (B/A rubric), §8 (file-disjoint row T4 + seams),
§9 R4 (GC race), R9 (json_each performance).

Plan: docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md §5 (this task).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (T4 OWNS the entire blackboard_gc/ package;
mark-and-sweep over substrate_blobs filtered by grace window AND digest
NOT IN live-set; bounded LIMIT-100 batch; per-DELETE short tx; operator
opt-out via Enabled=false; hourly tick by default; grace_secs ≥ 3600),
STOP and report — do NOT pick an alternative. Re-spawn the design subagent.

# Pre-flight verification

  ls internal/orchestrator/state/migrations/0008_blackboard_blobs.sql
  grep -n "substrate_blobs" internal/orchestrator/state/substrate/blob.go

Confirm T1 has merged (0008 migration + PutBlob exist). If T1 has not
merged, branch off T1's PR and rebase after T1 lands.

# Scope (exclusive write paths — file-disjoint with T1/T2/T3)

- internal/orchestrator/blackboard_gc/sweep.go         (NEW)
- internal/orchestrator/blackboard_gc/sweep_test.go    (NEW)
- internal/orchestrator/blackboard_gc/config.go        (NEW)
- internal/orchestrator/blackboard_gc/config_test.go   (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/orchestrator/blackboard/ — that is T2/T3/T5's scope.
- Do NOT touch internal/orchestrator/state/substrate/ — T1 owns the blob.go.
- Do NOT modify any migration file.
- Do NOT wire the goroutine into cmd/regatta/serve.go — T5 owns the wire-in.

# Patterns to reuse (do NOT reinvent)

- Mark-and-sweep + grace_seconds: Bazel remote-cache GC + Cassandra
  gc_grace_seconds. Don't reinvent the timer or semantics.
- SQL LIMIT 100 per sweep call.
- Per-DELETE short tx. NO long-running tx wrapping the entire sweep.
- json_each(payload_json, '$.blob_refs') to extract live set from
  kind='fact' rows; filter by payload_json LIKE '%blob_refs%' for
  selectivity per spec §9 R9.
- Goroutine panic-recovery wrapper around the tick body.

# Workflow steps (TDD discipline)

For each of the 6 named tests:
  1. Write the test first.
  2. Run `go test ./internal/orchestrator/blackboard_gc/ -run <TestName> -v`.
  3. Capture failing output. Paste at least 4 representative samples.
  4. Implement.
  5. Re-run; confirm pass.

# Tests to land (6 named — spec §6 T4)

B1. TestBlackboard_GCSweepsOrphans
B2. TestBlackboard_GCSparesReferencedBlobs
B3. TestBlackboard_GCSpareesYoungOrphans            (pins R4)
B4. TestBlackboard_GCOptOut
A1. TestBlackboard_GCBatchSize
A2. TestBlackboard_GCSurvivesPanicMidSweep

# Workflow after green

  1. make pre-push-check clean.
  2. go test ./internal/orchestrator/blackboard_gc/ -v.
  3. Sweep comments.
  4. doc-check + stale-todo + banned-phrase grep.
  5. Push.
  6. gh pr create --base main --title "feat(w11): T4 blob orphan GC sweep" --body-file <path>.
  7. Verify release-notes fence.
  8. Spawn adversarial reviewer.
  9. Apply findings or file tracking.
 10. CI green before automerge.

# Adversarial reviewer hunt list

- Sweep query EXACT match to spec §3.5 lines 344-355: kind='fact',
  payload_json LIKE '%blob_refs%' (selectivity), json_each extraction,
  LIMIT 100.
- Grace window: created_at < now_ms - grace_secs * 1000. NOT ≤;
  off-by-one matters for young-orphan test (B3).
- grace_secs minimum 3600 (1h). Clamp at config-load; reject below.
- Per-DELETE short tx. NOT one tx for all 100 deletes. NOT autocommit
  without an explicit tx (sqlite default fine, but verify code does
  not open a long tx).
- Operator opt-out: cfg.Enabled = false ⇒ Run returns nil immediately.
  NO goroutine started. Test asserts goroutine count delta == 0.
- Panic recovery: defer { recover } at the tick-body scope (NOT the
  long-loop scope, otherwise one panic kills the goroutine permanently).
- Tick interval = grace_secs / 24 default (hourly by default;
  rationale: orphan gets ~24 chances to be re-referenced before deletion).
- Span attrs: regatta.blackboard.gc.deleted + regatta.blackboard.gc.scanned.
  No work_item / dag cardinality.
- Cyclic-import check: blackboard_gc imports substrate (for blob table).
  Does NOT import blackboard (no Go-struct dependency; SQL-side only —
  payload_json.blob_refs extraction is pure SQL).
- No AI signatures. godocs ≤ 1 line. Banned phrases swept. Release-notes fence.

# Hygiene

- No AI signatures. Comments discipline. Doc-check + stale-todo +
  banned-phrase grep before push.

# Return format

- PR URL.
- Pasted failing output for at least 4 of 6 tests.
- Reviewer verdict + severities.
- One-line diff stat.
- A+ scorecard measurement.

Begin now. NEVER pause for user input.
```

---

## §6 Task T5 — OTel attrs + operator runbook + (A+) lints

### Scope

- **`internal/orchestrator/blackboard/otel.go`** — NEW. OTel attr-key constants + helpers per spec §3.7:
  - `AttrTopic = attribute.Key("regatta.blackboard.topic")`.
  - `AttrSchemaVersion = attribute.Key("regatta.blackboard.schema_version")`.
  - `AttrBlobCount = attribute.Key("regatta.blackboard.blob_count")`.
  - `AttrSubscriberID = attribute.Key("regatta.blackboard.subscriber_id")`.
  - Helper: `SetFactWriteAttrs(ctx, topic, schemaVersion, blobCount)` — invokes `trace.SpanFromContext(ctx).SetAttributes(...)`.
  - Helper: `SetTailAttrs(ctx, topic, subscriberID)` — same.
- **`internal/orchestrator/blackboard/otel_test.go`** — NEW. Tests verify attrs land on the right spans.
- **`internal/orchestrator/blackboard/fact.go`** — ONE-LINE wire-in: call `SetFactWriteAttrs(ctx, topic, schemaVersion, len(blobRefs))` inside `PutFact` after the substrate write succeeds.
- **`internal/orchestrator/blackboard/get_fact.go`** — ONE-LINE wire-in: call `SetTailAttrs(ctx, topic, "")` inside `GetFact` (no subscriber id for sync gets).
- **`internal/orchestrator/blackboard/tail.go`** — ONE-LINE wire-in: call `SetTailAttrs(ctx, topic, subscriberID)` inside the `TailFacts` opening span. subscriberID generated as ULID per call.
- **`docs/operator/blackboard.md`** — NEW. Operator runbook with the three sections enumerated in spec §6 T5 A-tier:
  - **Topic naming convention** — `<owner>.<noun>`; enforced via T5 A+ lint optionally; otherwise runbook is the v1 gate.
  - **GC opt-out** — `blackboard.blob_gc_enabled: false` for forensic deployments; explain consequences (orphans survive forever; manual cleanup recipe).
  - **Schema-version migration recipe** — dual-register reducers for `(topic, v1)` and `(topic, v2)`, ship one release, then writers can emit v2 (mirrors substrate I3).
  - Plus: cursor persistence warning ("do not persist cursors across major-version upgrades"); blob size cap config (default 1 MiB, hard cap 16 MiB); reducer purity contract.
- **`tools/lint-blackboard-reducer-purity/main.go`** — NEW. A+-tier. Go AST analyser: walks the call graph from any function registered via `Registry.Register`; rejects if the closure references `*sql.DB`, `*sql.Tx`, `os.*`, `time.Now`, `rand.*`, `net.*`. Wired into CI via `tools/lint-blackboard-reducer-purity_test.go`.
- **`tools/lint-blackboard-tail-ctx/main.go`** — NEW. A+-tier. AST analyser: asserts every `blackboard.TailFacts(...)` call site passes a `context.Context` that can be cancelled (heuristic: traces back to `context.WithCancel`, `context.WithTimeout`, or an incoming `ctx context.Context` argument).
- **`tools/lint-blackboard-topics/main.go`** — NEW. A+-tier. AST + config analyser: walks every `blackboard.PutFact(...)` call site; extracts the topic literal; asserts its prefix is in `blackboard.topic_prefixes` config list. Unknown prefix ⇒ lint fail.
- **`tools/lint-blackboard-*/main_test.go`** — fixture-based tests per spec §6 T5 A+ — each lint binary has its own pass-fixture + fail-fixture pair.

### Prereqs (cite spec sections)

- Spec §1 in-scope item "OTel attribute conventions".
- Spec §3.7 — attr names verbatim + W6 SDK reuse note.
- Spec §6 T5 — exhaustive named-test list (6 tests; B/A/A+ tiers).
- Spec §7 A+ — three lint binaries.
- Spec §8 — file-disjoint row T5 + cross-task seam contracts.
- Spec §9 R3 (reducer cycle → purity lint), R5 (subscription leak → tail-ctx lint), R8 (topic namespace collision → topics lint).

### Existing patterns to reuse (do NOT reinvent)

- **W6 attribute set:** `internal/obs/otel/` already wires the OTel SDK + GenAI semconv attrs. T5 attrs use the same `attribute.Key` + `trace.SpanFromContext(ctx).SetAttributes(...)` pattern.
- **Existing repo lint binaries:** `tools/lint-keyring-readonly/` + (any substrate lint shipped by T-S3 #224's adversarial pass). Mirror the layout: `main.go` walks `go/ast` over a target directory, prints findings, exits 1 on hit.
- **CI wire-in:** `make check` already invokes every `tools/lint-*` binary. T5 only needs to ship the binaries — Makefile auto-discovers (or T5 adds three lines for explicit invocation; check existing pattern).
- **Operator runbook layout:** `docs/operator/` siblings (e.g. `cost-governor.md` once W2 lands). Mirror section headers + tone.

### TDD test list (named tests from spec §6 T5; failing-output capture step required)

**B-tier (2 named tests; spec §6 T5 + §7 B):**

1. `TestBlackboard_OTelSpanAttrs_PutFact` — fact-write inside an active span ⇒ span carries `regatta.blackboard.topic`, `regatta.blackboard.schema_version`, `regatta.blackboard.blob_count`. Use `tracetest.SpanRecorder` to capture.
2. `TestBlackboard_OTelSpanAttrs_TailFacts` — `TailFacts` opens a span carrying `regatta.blackboard.subscriber_id` + `regatta.blackboard.topic`.

**A-tier (1 named test; spec §6 T5 A-tier + §7 A):**

3. `TestBlackboard_OperatorRunbookSectionsPresent` — grep-based: `docs/operator/blackboard.md` contains required H2 sections — "Topic naming convention", "GC opt-out", "Schema-version migration recipe", "Cursor persistence", "Blob size caps", "Reducer purity".

**A+-tier (3 named tests; spec §6 T5 A+ + §7 A+):**

4. `TestBlackboard_ReducerPurityLint` — `tools/lint-blackboard-reducer-purity` accepts a pure fixture; rejects a fixture that calls `time.Now`, references `*sql.DB`, or imports `os`.
5. `TestBlackboard_TailCtxLint` — `tools/lint-blackboard-tail-ctx` accepts call sites with cancellable ctx; rejects fixtures passing `context.Background()` directly.
6. `TestBlackboard_TopicsLint` — `tools/lint-blackboard-topics` accepts topics with allowed prefix; rejects unknown prefix.

Total T5: **6 named tests**. PR body lists every name + pasted failing-output excerpt for AT LEAST 4 representative cases.

### PR body skeleton — T5

````
## Summary

W11 blackboard T5: ships OTel attribute conventions, operator runbook,
and (A+) three lint binaries per
docs/engineer/specs/2026-06-01-w11-blackboard-design.md §3.7 + §7 A+.

- internal/orchestrator/blackboard/otel.go — attr-key constants
  (regatta.blackboard.{topic, schema_version, blob_count, subscriber_id})
  + SetFactWriteAttrs / SetTailAttrs helpers.
- internal/orchestrator/blackboard/{fact, get_fact, tail}.go —
  ≤ 3-LoC wire-in calls on already-open spans.
- docs/operator/blackboard.md — operator runbook (topic naming + GC
  opt-out + schema-version migration recipe + cursor persistence +
  blob size cap + reducer purity).
- tools/lint-blackboard-reducer-purity/ — A+; rejects impure reducers.
- tools/lint-blackboard-tail-ctx/ — A+; rejects non-cancellable ctx
  passed to TailFacts.
- tools/lint-blackboard-topics/ — A+; enforces topic prefix allowlist.

## Why

Per spec §3.7: attribute conventions reuse the W6 SDK already wired
into AppendEvent — reviewer scans one prefix (regatta.blackboard.*).
Per spec §9 R3/R5/R8: the three lint binaries make a class of mistakes
(impure reducers, leaking subscribers, ambiguous topic namespaces)
IMPOSSIBLE to land — Wave 2/3 review surface shrinks because the
gates are automatic.

## Test plan

- [x] TestBlackboard_OTelSpanAttrs_PutFact
- [x] TestBlackboard_OTelSpanAttrs_TailFacts
- [x] TestBlackboard_OperatorRunbookSectionsPresent
- [x] TestBlackboard_ReducerPurityLint (A+)
- [x] TestBlackboard_TailCtxLint (A+)
- [x] TestBlackboard_TopicsLint (A+)
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 4 reps>

## Deletion default

T5 makes three failure modes UNREACHABLE at CI time. Saves: every
future PR's review burden of checking reducer purity + tail-ctx
cancellability + topic prefix discipline. Lint binaries are pure
additions but the review surface they retire is meaningfully larger
than their LoC footprint.

## Followup issues filed (per feedback_unaddressed_load_bearing)

(T1 filed the 10 [blackboard-followup] issues; cite numbers here.)

```release-notes
[FEATURE] blackboard OTel attrs (regatta.blackboard.*) + operator runbook + reducer-purity / tail-ctx / topics lint binaries
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w11-t5. Branch: `feat/w11-t5-otel-docs-lints`
off main AFTER T1+T2+T3+T4 have all merged. Do NOT branch before then.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w11-blackboard-design.md.
Read ALL of: §1, §3.7 (OTel attr names verbatim), §6 T5 (named test
list), §7 A+ (three lint binaries + load test — load test out of T5
scope; A+ lint binaries only), §8 (file-disjoint row T5 + ≤ 3-LoC
wire-in seams), §9 R3 (reducer purity → lint), R5 (subscription leak
→ tail-ctx lint), R8 (topic namespace → topics lint).

Plan: docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md §6 (this task).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (T5 OWNS otel.go in blackboard/ + the operator
runbook + the three A+ lint binaries; attr names exactly
"regatta.blackboard.topic", "regatta.blackboard.schema_version",
"regatta.blackboard.blob_count", "regatta.blackboard.subscriber_id";
≤ 3-LoC wire-in into T2's fact.go + get_fact.go + T3's tail.go),
STOP and report — do NOT pick an alternative.

# Pre-flight verification

  ls internal/orchestrator/blackboard/         # T2 + T3 files present
  ls internal/orchestrator/blackboard_gc/      # T4 files present
  ls internal/orchestrator/state/migrations/0008_blackboard_blobs.sql

Confirm all of T1/T2/T3/T4 have merged. If any has not, do NOT proceed
— Wave B is sequenced after Wave A.

# Scope (exclusive write paths)

- internal/orchestrator/blackboard/otel.go                   (NEW)
- internal/orchestrator/blackboard/otel_test.go              (NEW)
- internal/orchestrator/blackboard/fact.go                   (≤ 3-LoC delta — add SetFactWriteAttrs call inside PutFact)
- internal/orchestrator/blackboard/get_fact.go               (≤ 3-LoC delta — add SetTailAttrs call inside GetFact)
- internal/orchestrator/blackboard/tail.go                   (≤ 3-LoC delta — add SetTailAttrs call inside TailFacts)
- docs/operator/blackboard.md                                (NEW)
- tools/lint-blackboard-reducer-purity/main.go               (NEW; A+)
- tools/lint-blackboard-reducer-purity/main_test.go          (NEW; A+)
- tools/lint-blackboard-reducer-purity/testdata/             (NEW; pass + fail fixtures)
- tools/lint-blackboard-tail-ctx/main.go                     (NEW; A+)
- tools/lint-blackboard-tail-ctx/main_test.go                (NEW; A+)
- tools/lint-blackboard-tail-ctx/testdata/                   (NEW; pass + fail fixtures)
- tools/lint-blackboard-topics/main.go                       (NEW; A+)
- tools/lint-blackboard-topics/main_test.go                  (NEW; A+)
- tools/lint-blackboard-topics/testdata/                     (NEW; pass + fail fixtures)
- Makefile (or scripts/ci/*.sh)                              (wire-in for three lint binaries IF auto-discovery is not present; check existing pattern first; one line per binary)

You MUST NOT touch any other file. Specifically:
- Do NOT modify registry.go / payload.go / errors.go / reducers/ — those are T2's scope and the otel attrs land via a helper called from fact.go.
- Do NOT touch internal/orchestrator/blackboard_gc/ — T4 owns. T4's GC spans get their attrs from substrate's existing attrs; T5 does NOT add new ones.

# Patterns to reuse (do NOT reinvent)

- W6 attribute set: attribute.Key + trace.SpanFromContext(ctx).SetAttributes.
- Existing lint binary layout in tools/ — see tools/lint-keyring-readonly/ for the canonical shape.
- Operator runbook layout: mirror docs/operator/cost-governor.md (if shipped) for tone + H2 section structure.

# Workflow steps (TDD discipline)

For each of the 6 named tests:
  1. Write the test first.
  2. Run `go test ./internal/orchestrator/blackboard/ -run <TestName> -v`
     (or `go test ./tools/lint-blackboard-*/ -v` for the lint tests).
  3. Capture failing output. Paste at least 4 representative samples.
  4. Implement.
  5. Re-run; confirm pass.

# Tests to land (6 named — spec §6 T5)

B1. TestBlackboard_OTelSpanAttrs_PutFact
B2. TestBlackboard_OTelSpanAttrs_TailFacts
A1. TestBlackboard_OperatorRunbookSectionsPresent
A+1. TestBlackboard_ReducerPurityLint
A+2. TestBlackboard_TailCtxLint
A+3. TestBlackboard_TopicsLint

# Workflow after green

  1. make pre-push-check clean.
  2. go test ./... -v (sample the affected pkgs).
  3. Sweep comments.
  4. doc-check + stale-todo + banned-phrase grep.
  5. Push.
  6. gh pr create --base main --title "feat(w11): T5 OTel attrs + operator runbook + reducer/tail-ctx/topics lints" --body-file <path>.
  7. Verify release-notes fence.
  8. Spawn adversarial reviewer.
  9. Apply findings or file tracking.
 10. CI green before automerge.

# Adversarial reviewer hunt list

- Attr names EXACT match to spec §3.7: regatta.blackboard.topic,
  regatta.blackboard.schema_version, regatta.blackboard.blob_count,
  regatta.blackboard.subscriber_id. No drift.
- Wire-in diffs ≤ 3 LoC per file. Verify via `git diff origin/main --
  internal/orchestrator/blackboard/{fact,get_fact,tail}.go | grep -c "^+" `.
- Runbook H2 sections match the test grep verbatim.
- Three lint binaries each have pass + fail fixtures under testdata/.
- Lint binary CI wire-in present (Makefile or scripts/ci/).
- Reducer-purity lint walks the call graph (not just the registered
  function body — closures matter).
- Tail-ctx lint heuristic documented in godoc; accepts WithCancel,
  WithTimeout, WithDeadline, or an incoming ctx arg.
- Topics lint reads `blackboard.topic_prefixes` from the config; rejects
  unknown prefix at the PutFact call site (NOT at runtime — lint-time).
- No AI signatures. godocs ≤ 1 line. Banned phrases swept. Release-notes fence.

# Hygiene

- No AI signatures. Comments discipline. Doc-check + stale-todo +
  banned-phrase grep before push.

# Return format

- PR URL.
- Pasted failing output for at least 4 of 6 tests.
- Reviewer verdict + severities.
- One-line diff stat.
- A+ scorecard measurement.

Begin now. NEVER pause for user input.
```

---

## §7 Followup issue templates (file BEFORE Wave A — T1 PR body cites the issue numbers)

Per `feedback_unaddressed_load_bearing`: every load-bearing named-but-deferred item in spec §10 is filed as a `[blackboard-followup]` issue **PRE-MERGE of Wave A's first PR**. Below is the verbatim template for each — T1's implementer files all ten before pushing.

**F1 — Realtime push (SSE / websocket / goroutine broadcast)**

> **Title:** [blackboard-followup] F1 — Realtime fact-push alternative to polling TailFacts
>
> **Body:**
> Per spec §10 F1. v1 ships polling-only TailFacts (default 1s interval). If profiling under real load shows >5% CPU spent in tail SELECTs, evaluate goroutine-broadcast fan-out (sync.Cond), SSE bridge, or per-tenant pub/sub adapter.
>
> Trigger: profiling shows >5% CPU in tail SELECT loops.
> Scope: design subagent re-spawn; new spec doc.
> Out-of-scope for W11 v1.

**F2 — Tag-faceted fact queries**

> **Title:** [blackboard-followup] F2 — Tag-faceted fact queries (fact.list(tag=...))
>
> **Body:**
> Per spec §1 OOS + §10 F2. Original wedge_blackboard.md proposed tag_json column + index. v1 collapses tag-facet to topic-prefix convention (`<owner>.<noun>`). Land tag-indexed queries when a real consumer demands them.
>
> Trigger: first internal consumer files an issue with concrete use case.
> Scope: new migration (add tags_json column + index) + read API addition.

**F3 — Semantic / vector search over facts**

> **Title:** [blackboard-followup] F3 — Semantic / vector search (fact.semantic(query, k))
>
> **Body:**
> Per spec §1 OOS + §10 F3. Requires embeddings stack — no proven local-OSS choice yet (research/research_design_principles bar).
>
> Trigger: local-OSS embeddings stack reaches production readiness (e.g. ONNX + a stable model).
> Scope: new package + embedding column + ANN index.

**F4 — S3 / external blob backend for >16 MiB**

> **Title:** [blackboard-followup] F4 — S3 / external blob backend for blobs >16 MiB
>
> **Body:**
> Per spec §1 OOS + §10 F4. SQL CHECK hard-caps single blobs at 16 MiB. Larger payloads require an external-store adapter (S3-compatible).
>
> Trigger: first deployment files an issue requesting >16 MiB payloads.
> Scope: new adapter package + config + migration to store digest+URL instead of bytes.

**F5 — Tenant-scoped encrypt-before-hash**

> **Title:** [blackboard-followup] F5 — Tenant-scoped encrypt-before-hash for blob payloads
>
> **Body:**
> Per spec §1 OOS + §10 F5. sha256 PK is content-addressed and globally deduplicated — two tenants with identical content share the row. For tenant-sensitive payloads, encrypt before hash so identical plaintexts hash differently per tenant.
>
> Trigger: W8 multi-tenant cutover lands; first tenant flags sensitive payload concern.
> Scope: new KMS wrapper + per-tenant key + migration.

**F6 — Rate-limit on PutBlob**

> **Title:** [blackboard-followup] F6 — Rate-limit on PutBlob to defend against hostile producers
>
> **Body:**
> Per spec §9 R1 + §10 F6. A malicious authenticated writer can flood PutBlob with 1 MiB payloads pre-W8. Per-deployment sizeMax cap is the v1 defense; rate-limit lands post-W8 once RBAC API boundary exists.
>
> Trigger: W8 RBAC API boundary ships.
> Scope: middleware at the W8 API boundary; per-(tenant, role) token bucket.

**F7 — Cursor versioning before W9 multi-writer**

> **Title:** [blackboard-followup] F7 — Cursor versioning + migration recipe for W9 multi-writer
>
> **Body:**
> Per spec §9 R6 + §10 F7. v1 cursor encodes rowid (opaque). W9's multi-writer story (Temporal-backed) may break rowid monotonicity. Persisted cursors would silently skip facts.
>
> Trigger: W9 multi-writer adapter spec opens.
> Scope: new Cursor encoding (e.g. (written_at, event_id) tuple); migration recipe for persisted cursors; operator runbook update.

**F8 — Materialized blob_refs index if json_each becomes a bottleneck**

> **Title:** [blackboard-followup] F8 — Materialized blob_refs index column on substrate_events
>
> **Body:**
> Per spec §9 R9 + §10 F8. v1 GC sweep uses `json_each(payload_json, '$.blob_refs')` to extract the live set. For >10M facts the per-tick scan may exceed the hourly tick interval. Land a precomputed index column / junction table if profiling demands.
>
> Trigger: GC sweep tick exceeds 10s on a real deployment.
> Scope: new migration (add blob_refs_index column OR substrate_blob_refs junction table); rewrite sweep query.

**F9 — sha3-256 migration path**

> **Title:** [blackboard-followup] F9 — sha3-256 / next-gen hash migration path
>
> **Body:**
> Per spec §9 R10 + §10 F9. sha256 collision is effectively impossible at v1; sha3-256 migration path lands if sha256 ever weakens (≥10y horizon).
>
> Trigger: cryptographic consensus shifts; sha256 deprecation surfaced in NIST guidance.
> Scope: dual-digest column on substrate_blobs; migration tool that rehashes content.

**F10 — Listen/notify-backed TailFacts on Postgres adapter**

> **Title:** [blackboard-followup] F10 — Postgres LISTEN/NOTIFY-backed TailFacts for non-sqlite adapters
>
> **Body:**
> Per spec §3.4 (polling rationale) + §10 F10. sqlite has no LISTEN/NOTIFY equivalent — v1 polls. When the first Postgres-backed adapter ships (W9 Temporal-backed), evaluate native LISTEN/NOTIFY for tail.
>
> Trigger: first Postgres adapter spec opens.
> Scope: TailFacts adapter interface; pgx-based listener implementation; cursor semantics review.

---

_Plan authority: `feedback_spec_pattern_authority` — implementer subagent deviation from this plan or the spec it cites requires re-spawning the design subagent. The 10 followup issues in §7 MUST be filed and cited in Wave A's first PR body per `feedback_unaddressed_load_bearing` + `feedback_review_before_automerge`. Open spec question §11 #1 (validator re-registration) MUST be resolved before T2 dispatches._
