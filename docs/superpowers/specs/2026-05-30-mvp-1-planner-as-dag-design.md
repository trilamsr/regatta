# MVP-1 Planner-as-DAG — Design Spec

Status: ready for implementation
Date: 2026-05-30
Author: Tri Lam <tri@maydow.com>
Supersedes: `docs/engineer/specs/mvp-1-planner.md` (reconciles spec wording to code reality)
Binding RFC: `docs/rfcs/0001-mvp-1-program-publish.md`

## Context

MVP-1 acceptance (verbatim from `docs/design.md` §Phase 1): "One program produces ≥3 child PRs through unmodified L0-L6."

Concretely: a `markdown_catalog` item with `kind: program` and ≥3 acceptance criteria flows through `regatta program plan` → signed `program_brief.json` → orchestrator brief loader → ≥3 child agents spawn → ≥3 PRs open against the target repo. L0 runs unmodified; L1-L5 deferred to MVP-2+.

The original spec at `docs/engineer/specs/mvp-1-planner.md` assumed `work_items` table existed with a `kind` column to ALTER. Code reality: no `work_items` table; the orchestrator polls work items live from `SpecAdapter.List` each tick. This spec reconciles to that reality and expands scope (per user direction "reliable MVP, parallel-agent build, no budget ceiling") to a universal-queue architecture.

## Locked decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | **Universal queue**: new `state.work_items` table is single source of truth | UX: one query answers "what does regatta think exists?" Long-term: scheduler stays adapter-agnostic |
| 2 | `AdapterSync` + `BriefLoader` both write work_items; scheduler reads only | Clean seam; adapter swap doesn't touch scheduler |
| 3 | **Scheduler join** (not materialize): `ListSpawnable` = `work_items LEFT JOIN agents` query, reserves directly into agents in one tx | Eliminates orphan class by construction; no recover-sweep code |
| 4 | **Tombstone + cascade-soft**: missing item → status=archived; running children finish naturally | Audit trail intact; mid-run agents not killed |
| 5 | **Cascade snapshot**: child carries own `acceptance_json` at upsert | Parent archive doesn't invalidate in-flight validation |
| 6 | **Sign-then-persist**: brief must pass `programs.LoadAndValidate` before children upsert | Clean invariant: rows in work_items are valid |
| 7 | **slog WARN events** for rejections (no `brief_rejections` table) | Aligns with audit-log deferral to MVP-3+; operator uses `journalctl \| grep` |
| 8 | **DAG enforce in MVP-1**: blocked children wait until upstream `status=merged`; cycle detection at upsert | Closes issue #25 inline; avoids parallel-spawn merge conflicts |
| 9 | **Tombstone keyed by source + `pollStartedAt`** (no meta tick table) | Reuses existing time-based pattern; no new write-hotspot |
| 10 | **Fail-fast PollOnce**: any sync error → return early, retry next tick | Atomic-tick invariant; matches existing PollOnce semantics |
| 11 | **Flock on `.regatta/state.db.lock`** with stale-PID reclaim | Prevents concurrent PollOnce; gofrs/flock + pid-in-content + `kill -0` check |
| 12 | **sqlite stays** | Single-binary, no infra, file-local state; driver-swap surface documented for future HA |
| 13 | **TDD discipline** + library-first | red→green→refactor; goose for migrations, gofrs/flock for locking |

## §1 Architecture

```
markdown file (PROG-1.md, kind=program)
   │
   ├─► SpecAdapter.List ───► AdapterSync.Sync ──┐
   │                                            │
   │                                            ▼
   │                                  state.work_items
   │                                            ▲
   │                                            │
   └─► regatta program plan ─signed brief──► BriefLoader.Sync
                                  │
                                  ▼
                       .regatta/programs/*.json
                                            │
                                            │
                          Scheduler.Tick ───┘
                          (work_items LEFT JOIN agents)
                                            │
                                            ▼
                                  state.agents (reserved)
                                            │
                                            ▼
                                  Spawner.Spawn (stub)
                                            │
                                            ▼
                                  state.agents (running)
```

**Two writers** to `state.work_items`: `AdapterSync` (source=adapter) and `BriefLoader` (source=brief).
**One reader**: `Scheduler.ListSpawnable` (join with agents, deps-satisfied filter).
**Tick sequence**: `flock acquire → AdapterSync → BriefLoader → Scheduler.Tick → flock release`. Fail-fast on first error.

## §2 Components

### 2.1 Migrations (`internal/orchestrator/state/migrations/`)
- `0001_initial.sql` — current `schema.sql` content verbatim
- `0002_work_items.sql` — new `work_items` table
- `migrate.go` — `pressly/goose` embed.FS wrapper; returns typed `ErrSchemaTooNew` on downgrade
- Tests: empty→v2, v1→v2, downgrade-resistance

### 2.2 `state.work_items` table + Go API

```sql
CREATE TABLE work_items (
    id                   TEXT PRIMARY KEY,
    kind                 TEXT NOT NULL,           -- feature | program
    title                TEXT NOT NULL,
    lane                 TEXT NOT NULL,
    status               TEXT NOT NULL,           -- planned | running | pr_open | merged | archived | blocked
    parent_program_id    TEXT,                    -- NULL for top-level
    depends_on_features  TEXT NOT NULL DEFAULT '[]',
    acceptance_json      TEXT NOT NULL DEFAULT '[]',
    source               TEXT NOT NULL,           -- adapter | brief
    last_seen_at         INTEGER NOT NULL,        -- unix seconds
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);
CREATE INDEX idx_work_items_status ON work_items(status);
CREATE INDEX idx_work_items_parent ON work_items(parent_program_id);
```

Go API (split across `work_items_upsert.go` + `work_items_query.go`):
- `UpsertWorkItem(ctx, WorkItem, source string, seenAt time.Time) error`
- `TombstoneBySource(ctx, source string, before time.Time) (archivedIDs []WorkItemID, err error)`
- `CascadeArchiveChildren(ctx, parentID WorkItemID) error`
- `ListSpawnable(ctx) ([]WorkItem, error)` — join, deps-satisfied
- `CycleCheck(ctx, newItem WorkItem) error` — returns `ErrCycleDetected`
- `ListByParent(ctx, parentID WorkItemID) ([]WorkItem, error)`
- `GetWorkItem(ctx, id WorkItemID) (WorkItem, error)`

### 2.3 `AdapterSync` (`internal/orchestrator/adaptersync/`)
- `Sync(ctx, pollStartedAt time.Time) (changed int, err error)`
- Calls `adapter.List`; upserts each with `source=adapter, last_seen_at=pollStartedAt`
- Then `TombstoneBySource(ctx, "adapter", pollStartedAt)` archives missing items
- Cascade-archive children of any archived program

### 2.4 `BriefLoader` (`internal/program/brief_loader.go`)
- Constructor takes `fs.FS` (for `fstest.MapFS` in tests), `Clock`, `*state.DB`, HMAC key
- `Sync(ctx, pollStartedAt time.Time) (loaded int, err error)`
- Glob `*.json`; skip `*.tmp`
- For each: `programs.LoadAndValidate(path, hmacKey)`:
  - Error → `slog.Warn("brief.rejected", "path", p, "reason", err.Error())`; continue
  - Success → for each `features[]`: `UpsertWorkItem(child, source=brief, last_seen_at=pollStartedAt)` with `parent_program_id` + `depends_on_features` + `acceptance_json` snapshot populated
- Then `TombstoneBySource(ctx, "brief", pollStartedAt)`
- Tombstoned dep detection: any child whose `depends_on_features` references an archived ID → `slog.Warn("child.dependency_archived", ...)` + cascade-archive the child

### 2.5 `planner.LoadPlannerPrompt` (mod `internal/program/planner.go`)
- `LoadPlannerPrompt(path string, expectedSHA string) (string, error)`
- Read file at path; compute SHA256
- If `expectedSHA != ""` and mismatch → `ErrBriefSHAMismatch`
- If path missing → fall back to embedded `defaultPlannerPrompt`

### 2.6 `runProgramPlan --write` (mod `cmd/regatta/main.go`)
- New `--write` flag: writes signed brief to `<repo>/.regatta/programs/<program_id>.json` via temp+rename
- Read `prompts.planner_sha` from `regatta.yaml`; pass to `LoadPlannerPrompt`
- Target exists + different content → `ErrTargetExists` unless `--force`
- Stdout remains default when `--write` absent

### 2.7 Flock (`internal/orchestrator/lockfile/`)
- gofrs/flock wrapper; writes pid into lockfile content
- `Acquire(path string) (*Lock, error)` — returns `ErrLockHeld` if active; reclaims if pid-not-running (`kill -0`)
- `(*Lock).Release()` removes file

### 2.8 Scheduler rewire (mod `internal/orchestrator/scheduler/scheduler.go`)
- Replace `adapter.List` reads with `state.ListSpawnable`
- SQL: `SELECT w.* FROM work_items w LEFT JOIN agents a ON w.id = a.work_item_id WHERE w.status='planned' AND a.id IS NULL AND (w.depends_on_features='[]' OR NOT EXISTS (SELECT 1 FROM json_each(w.depends_on_features) WHERE value NOT IN (SELECT id FROM work_items WHERE status='merged')))`
- Reserve + lock acquire in single tx; lock-fail → rollback reservation (item simply not in this tick's reserved set; retries next tick)

### 2.9 Orchestrator wire (mod `internal/orchestrator/orchestrator.go`)
- `PollOnce(ctx)`:
  1. `lock, err := lockfile.Acquire(...)` — defer Release
  2. `pollStartedAt := o.clock.Now().UTC()`
  3. `adaptersync.Sync(ctx, pollStartedAt)` — return on error
  4. `briefLoader.Sync(ctx, pollStartedAt)` — return on error
  5. `scheduler.Tick(ctx)` — return on error
- `ScheduleOnce` unchanged (operates on reserved agent IDs from Tick)

### 2.10 E2E + fixture
- `testdata/program/PROG-1.md` (3 independent acceptance criteria, no inter-deps)
- `testdata/program/PROG-1.brief.golden.json`
- `internal/program/end_to_end_test.go` — tmpdir, plan --write, serve --tick-once, assert 3 work_items + 3 running agents

## §3 Data flow + error handling

### Error policy per step
- **AdapterSync fails**: PollOnce returns early (fail-fast). Next tick retries.
- **BriefLoader: one bad brief**: `slog.Warn("brief.rejected")`; continue loop with other briefs.
- **BriefLoader: HMAC key missing**: hard error, abort sync. Operator misconfig — fail loud.
- **Scheduler.Tick: lock acquire fails for a candidate**: rollback that reservation; item retries next tick. Other candidates in same Tick unaffected.
- **Spawn fails**: existing behavior — agent crashed → pending. Unchanged.

### Concurrency
- `state.work_items` writes via `BEGIN IMMEDIATE`. Single-writer guaranteed via flock.
- Tombstone: `UPDATE ... WHERE last_seen_at < ? AND source = ?` — per-source, atomic.

### Test seams
- `BriefLoader` constructor takes `fs.FS` — production: `os.DirFS(repoRoot)`; tests: `fstest.MapFS`
- `Clock` interface in `internal/orchestrator/clock/` — production: `SystemClock`; tests: `FakeClock`
- Typed error sentinels in `internal/orchestrator/errors.go` — `ErrBriefSHAMismatch`, `ErrHMACInvalid`, `ErrTargetExists`, `ErrLockHeld`, `ErrSchemaTooNew`, `ErrCycleDetected`

## §4 Testing strategy

### Unit tests per component

**state.work_items**
- Round-trip upsert
- Tombstone source-scoped (rows with source='adapter' untouched when BriefLoader tombstones)
- Cascade-soft (parent archive ≠ kill child agent)
- Child completes after parent archived → merge transition recorded
- Dependency-satisfaction query
- Join with agents (planned + no agent + deps satisfied)
- DAG cycle detection at upsert (A→B, B→A → second upsert returns `ErrCycleDetected`)

**AdapterSync.Sync**
- Empty / 3 appear / 1 disappears tombstone / re-appears un-tombstone
- Item upserted after `pollStartedAt` within same tick → NOT tombstoned (requires clock injection)

**BriefLoader.Sync** (uses `fstest.MapFS`)
- Signed brief → 3 children with acceptance snapshot
- Tampered brief → 0 children + 1 captured WARN log; `errors.Is(err, ErrHMACInvalid)`
- Brief deleted between ticks → children tombstoned
- Tombstoned dep → auto-tombstone child + WARN log
- HMAC key rotation: old-key briefs reject + new-key briefs accept in same tick
- Crash between sign and persist → reopen DB, assert zero partial child rows
- `*.tmp` files skipped

**planner.LoadPlannerPrompt**
- All 4 paths; `errors.Is(err, ErrBriefSHAMismatch)` for sha mismatch

**runProgramPlan --write**
- Error path: target exists, no --force → `ErrTargetExists`
- Stdout unchanged when --write absent
- (Atomic-rename happy path NOT tested — POSIX guarantees it)

**Scheduler.Tick**
- 3 children, no deps → all 3 reserved
- c2 deps c1 → c1 only until c1 merged
- Lock acquire fails → reservation rolled back, no agents row created

### Adversarial / integration
- Brief disappears mid-poll (fstest.MapFS swap between BriefLoader and Scheduler) → scheduler sees no rows for that program
- AdapterSync fails → PollOnce returns early (fail-fast)
- Two PollOnce concurrent → second returns `ErrLockHeld`
- Stale flock: kill process holding lock, leave file → next PollOnce reclaims after pid-not-running check
- HMAC key wrong → all briefs rejected, WARN logs emitted, no children upserted

### End-to-end acceptance
- Fixture: `testdata/program/PROG-1.md`, 3 independent criteria
- `regatta program plan --write` → `regatta serve --tick-once`
- Assertions:
  - exactly 3 rows in `work_items` WHERE `parent_program_id='PROG-1'`
  - exactly 3 rows in `agents` WHERE `state='running'` matching those work_item_ids
  - stub spawner Spawn called 3 times
  - zero slog WARN events captured

### Migration
- Empty DB → v1 → v2: schema matches golden
- v1 DB → v2: `work_items` table exists, version=2
- Downgrade-resistance: v2 DB opened by simulated v1 binary → `errors.Is(err, ErrSchemaTooNew)`

### Property + fuzz
- DAG join property test: random DAGs n≤8, 1000 cases, `rapid` or `testing/quick`. Assert `ListSpawnable` returns exactly the topological-ready set.
- `BriefLoader.Parse` fuzz target: malformed YAML/markdown/JSON shouldn't panic; either parse cleanly or return typed error.

### Observability
- Captured `slog` handler in tests asserts WARN events fire at every reject/tombstone/cascade path enumerated in §6 checklist.

### Race
- `go test -race ./internal/orchestrator/... ./internal/program/...`
- 10 concurrent `Scheduler.Tick` goroutines on shared DB → no double-reservation

## §5 File-by-file delivery + parallel decomposition

### Wave 0 — Foundation (A0; serial; blocks all)

| Path | Status |
|---|---|
| `internal/orchestrator/errors.go` | NEW: typed sentinels |
| `internal/orchestrator/clock/clock.go` | NEW: `Clock`, `SystemClock`, `FakeClock` |
| Decision recorded: logging = `log/slog`; new state methods follow `(ctx context.Context, ...) (..., error)` convention | DOC |

Total agents counting A0: **12**, 7 waves.

### Wave 1 — Migrations + flock (parallel)

| # | Path | Owner |
|---|---|---|
| 1 | `internal/orchestrator/state/migrations/0001_initial.sql` (extract) | A1 |
| 2 | `internal/orchestrator/state/migrations/0002_work_items.sql` | A1 |
| 3 | `internal/orchestrator/state/migrate.go` + test | A1 |
| 4 | `go.mod` / `go.sum` (add `pressly/goose` + `gofrs/flock`) | A1 |
| 5 | `internal/orchestrator/lockfile/lockfile.go` + test | A2 |

**Wave 1.5 — Goose smoke gate**: A1 lands first; `go test ./internal/orchestrator/state/...` green; then Wave 2 unblocks.

### Wave 2 — work_items API (split per cohesion)

| # | Path | Owner |
|---|---|---|
| 6 | `internal/orchestrator/state/work_items.go` (interface stub) | A3 lands first |
| 7 | `internal/orchestrator/state/work_items_upsert.go` + test | A3 |
| 8 | `internal/orchestrator/state/work_items_query.go` + test | A4 (parallel after stub) |

### Wave 3 — Syncs (parallel)

| # | Path | Owner |
|---|---|---|
| 9 | `internal/orchestrator/adaptersync/adaptersync.go` + test | A5 |
| 10 | `internal/program/brief_loader.go` + test | A6 |
| 11 | `internal/program/planner.go` (`LoadPlannerPrompt`) + test | A7 |

### Wave 4 — Scheduler rewire

| # | Path | Owner |
|---|---|---|
| 12 | `internal/orchestrator/scheduler/scheduler.go` + test | A8 |

### Wave 5 — Orchestrator wire + CLI

| # | Path | Owner |
|---|---|---|
| 13 | `internal/orchestrator/orchestrator.go` + adversarial test | A9 |
| 14 | `cmd/regatta/main.go` (`runProgramPlan --write`) + test | A10 (after A7) |

### Wave 6 — E2E + docs (parallel)

| # | Path | Owner |
|---|---|---|
| 15 | `internal/program/end_to_end_test.go` | A11 |
| 16 | `testdata/program/PROG-1.md`, `testdata/program/PROG-1.brief.golden.json` | A11 |
| 17 | `docs/operator/configure.md`, `docs/operator/quickstart.md` | A12 |
| 18 | `docs/engineer/mvp-1-dod-checklist.md` (cross-cutting checklist) | A12 |

**Total (excluding A0): 11 agents across waves 1-6. With A0: 12 agents, 7 waves.**

### Merge-conflict mitigations
- Wave 0 lands typed errors + clock first; no agent invents own
- A1 has sole authority on `go.mod` / `go.sum`; other waves needing deps request via PR review
- Wave 2 stub commits first, freezing the work_items API surface for Wave 3+ imports

## §6 Definition of done + grade rubric

### Merge-time DoD (per PR in the series)
1. Wave's tests + adversarial cases green: `go test -race ./...`
2. `make ci-check` exit 0 (lint + vet + build)
3. New files include paired `_test.go` (TDD evidence)
4. Spec at `docs/engineer/specs/mvp-1-planner.md` reconciled before final wave merges

### Series-complete DoD (after Wave 6)
5. Acceptance test passes: `regatta program plan --write` then `regatta serve --tick-once` → exactly 3 `work_items` rows with `parent_program_id=PROG-1`, exactly 3 `agents` rows status=`running`, zero slog WARN events
6. CHANGELOG.md flipped to next dated section with MVP-1 entry

### Release readiness (separate gate)
7. Tag v0.1.0 via release.yml after main is green + manual operator smoke

### Grade rubric

**B (ships, minimum bar)** — *compiles, tests pass, ships*
- All merge-time + series-complete DoD items
- `make ci-check` clean across all PRs in series
- E2E acceptance test green

**A (target)** — *production-trustable*
- B +
- Typed error sentinels for all new boundary errors. Verify: `grep -r 'errors.New' internal/orchestrator/ internal/program/` returns only the sentinel file
- Structured slog WARN events at all paths enumerated in `docs/engineer/mvp-1-dod-checklist.md`:
  - `brief.rejected` (HMAC fail, sha mismatch, parse error)
  - `brief.tombstoned` (file disappeared)
  - `adapter.tombstoned` (item disappeared)
  - `child.cascade_archived` (parent archived)
  - `child.dependency_archived` (depends_on target tombstoned)
  - `parent.criteria_changed` (snapshot divergence detected)
- Adversarial tests green: brief disappears mid-poll, AdapterSync fail-fast, stale flock reclaim, HMAC rotation, tombstoned-dep auto-archive
- DAG join property test: 1000 random DAGs n≤8, reserved set == topological-ready set
- Operator docs: program plan walkthrough + flock troubleshooting + slog event reference
- Per-package coverage: `internal/orchestrator/state` ≥90%, `internal/program` ≥85%, `cmd/regatta` ≥70%

**A+ (exceptional, stretch)** — *qualitatively different*
- A +
- Fuzz target on `BriefLoader.Parse` runs 5min in CI with zero panics
- Mutation testing on migration runner downgrade-blocking code: commenting out version-check fails downgrade-resistance test
- `repo-consistency-loop` skill across 9 lenses, zero unresolved findings
- Operator UX walkthrough in `docs/operator/quickstart.md`: scripted "diagnose a rejected brief" using only `journalctl | grep`

### Risks

| # | Risk | Mitigation |
|---|---|---|
| 1 | goose runner bug | Wave 1.5 smoke gate |
| 2 | goose schema-version drift across PRs | A1 sole authority on migration numbering |
| 3 | sqlite write contention | BEGIN IMMEDIATE serializes; single-orchestrator only |
| 4 | flock cross-platform (no Windows) | Accepted gap; darwin+linux only |
| 5 | e2e flake | `--tick-once` synchronous; retry-3-times gate before declaring real flake |
| 6 | HMAC key rotation UX | Manual re-run plan; MVP-2 follow-up |
| 7 | `acceptance_json` snapshot staleness | On parent re-upsert with changed criteria, emit `parent.criteria_changed` WARN |
| 8 | slog as only rejection record | Operator docs note log shipping required for retention; MVP-3 audit table closes |
| 9 | Clock skew in stale-PID reclaim | pid-in-lockfile + `kill -0` (not mtime); same-host assumption documented |
| 10 | Test pollution from leftover lockfile | `t.Cleanup` in every flock test; CI clears `/tmp/regatta-*` before each job |
| 11 | Wave 0 errors not adopted by downstream waves | PR review: `grep errors.New` outside sentinel file blocks merge |

## Out of scope (deferred to MVP-2+)

- Handoff signature verification driving transitions (`RouteVerdicts` exists but stays unwired)
- Re-run mismatch enforcement on `commands_run`
- `program_id` injection in spawner prompt
- Per-criterion citation round-trip in `adapter/parse.go`
- Real `ClaudeSpawner` with worktree + SupervisorLimits (issue #14)
- PRWatcher driving running→pr_open→gates_running (issue #15)
- RejectionRouter K=3 escalation (issue #16)
- Reaper teardown (issue #19)
- Adapter orphan-adoption (issue #45)
- Postgres support / multi-orchestrator HA
- Windows support
