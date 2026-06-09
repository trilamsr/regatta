---
status: design
phase: self-host-s2
issue: 1092
summary: Persist `schemas.WorkItem.Body` into `work_items` so adapter-fetched issue context survives to dispatch and `orchestrator.item_body_missing` stops firing every tick when no filesystem brief exists.
---

# Item-body persistence — design spec (#1092)

## 1. Problem

Issue title: "orchestrator.item_body_missing warning fires every tick because work_items.body column doesn't exist."

Today the body-loader path is filesystem-only. `buildItemBodyLoader` in `cmd/regatta/wire_itembody.go` (lines 16-28) rescans `<repoRoot>/.regatta/items/*.md` per call and returns `(body, true)` only when a brief file matches the dispatched work-item id. `ScheduleOnce` (`internal/orchestrator/orchestrator_schedule.go`, lines 74-83) treats a miss as a WARN:

```
74:  var itemBody string
75:  if o.cfg.ItemBody != nil {
76:      if body, ok := o.cfg.ItemBody(ctx, a.WorkItemID); ok {
77:          itemBody = body
78:      } else {
79:          o.log.Warn("orchestrator.item_body_missing",
80:              string(obs.KeyWorkItemID), a.WorkItemID,
81:              string(obs.KeyAgentID), a.ID,
82:          )
83:      }
84:  }
```

For the GitHub-issues adapter path, the issue body IS available upstream — `internal/orchestrator/adapter/githubissues/adapter.go` (lines 164 and 252) constructs `schemas.WorkItem{... Body: p.Body ...}` from `contracts/schemas/spec_adapter.go:43` (the canonical contract: `Body string \`json:"body,omitempty"\``). adaptersync then calls `state.UpsertWorkItem(ctx, ...)`, but `internal/orchestrator/state/work_items.go` declares a persistence struct (`type WorkItem struct`, lines 56-70) with NO `Body` field, and `internal/orchestrator/state/work_items_upsert.go::UpsertWorkItem` (lines 16-67) never writes one. The `work_items` table itself (defined in `internal/orchestrator/state/migrations/0002_work_items.sql`, lines 7-20) has columns `id, kind, title, lane, status, parent_program_id, depends_on_features, acceptance_json, source, last_seen_at, created_at, updated_at` and no `body`.

End-to-end result: the adapter fetches `Body`; adaptersync silently discards it at the persistence boundary; the orchestrator falls back to the filesystem brief loader; when no `.regatta/items/<id>.md` exists the loader returns `false` and `ScheduleOnce` emits the WARN every tick that work item is ready. Operator-visible symptoms during the 2026-06-08 docker soak:

- `time=... level=WARN msg=orchestrator.item_body_missing work_item_id=BUG-1058 agent_id=1` on every tick where a ready item has no brief file.
- Worker prompt receives empty `ItemBody` (the WARN does NOT block dispatch; the agent is spawned without the issue context the implementer needs).
- Operator workflow today is "drop a markdown brief in `.regatta/items/`" — but the GitHub-issues adapter path is the strategic happy path for self-host. Adapter bodies arrive on every Sync and should be usable without a parallel manual filesystem step.

Citations verified against `git ls-tree origin/main` at `f68d35e6` (latest at time of writing):

```
git -C $R show origin/main:internal/orchestrator/orchestrator_schedule.go        | sed -n '74,83p'
git -C $R show origin/main:cmd/regatta/wire_itembody.go                          | sed -n '16,28p'
git -C $R show origin/main:internal/orchestrator/state/work_items.go             | sed -n '56,70p'
git -C $R show origin/main:internal/orchestrator/state/work_items_upsert.go      | sed -n '12,67p'
git -C $R show origin/main:internal/orchestrator/state/migrations/0002_work_items.sql
git -C $R show origin/main:contracts/schemas/spec_adapter.go                     | sed -n '37,50p'
git -C $R show origin/main:internal/orchestrator/adapter/githubissues/adapter.go | sed -n '160,170p;248,256p'
```

Note: the issue title is correct in spirit ("body column doesn't exist") but the WARN fires from the filesystem-brief loader, not the SQLite SELECT. Fixing the WARN requires extending the persistence chain so the adapter-fetched body becomes the loader's authoritative source.

## 2. Approach options

### Option A — schema migration: add `body TEXT NOT NULL DEFAULT '' COLLATE BINARY` to `work_items`

New goose migration file `internal/orchestrator/state/migrations/0022_work_items_body.sql` (next available number — `0021_substrate_kind_tool_call.sql` is the current head). The migration adds the column; in the same PR:

- `state.WorkItem` gains `Body string`.
- `state.UpsertWorkItem` extends the INSERT column list, the UPDATE SET clause, and the placeholder bindings in `internal/orchestrator/state/work_items_upsert.go`.
- The shared SELECT projection (`selectWorkItemsCols` in `internal/orchestrator/state/work_items_query.go`) gains `body` and `scanWorkItems` reads it.
- `ScheduleOnce` is rewired to consult state when the filesystem loader returns `false`: try `o.cfg.ItemBody(...)`; on miss, `o.db.GetWorkItem(ctx, a.WorkItemID)` and use `wi.Body` if non-empty; emit the WARN only when BOTH miss.

Pros:
- Cheapest at runtime — adapter populates Body once at Sync, every later read pulls from the same row already fetched.
- Survives orchestrator restart with no GitHub re-fetch.
- Drift-resistant — the column is in the SELECT projection and the struct, no future caller can quietly drop the field.
- Matches established codebase pattern — every prior column-introducing change went through a new numbered `.sql` file with goose `Up`/`Down` fences (see `0019_work_items_run_id.sql`, `0020_approval_events_run_id.sql`).
- Preserves the filesystem-brief escape hatch — operators can still drop `.regatta/items/<id>.md` to override the adapter-supplied body, and the file wins.

Cons:
- DB size grows by ~average-issue-body per tracked item. At self-host scale (single operator, single repo, <1000 work items expected) the absolute ceiling is well under ~50 MB — non-load-bearing.
- One-time ALTER TABLE on first start. SQLite handles `ADD COLUMN` in O(1) (schema-version bump, no row rewrite). Must be documented in the release-notes block.
- Issue bodies go stale between syncs. Acceptable: re-Sync overwrites on every poll cycle (currently every 30 s).

### Option B — lazy adapter `Get` from `ScheduleOnce` at dispatch time

Leave the schema alone. When the filesystem loader misses, call `adapter.Get(ctx, schemas.WorkItemID(a.WorkItemID))` inline, use the returned `Body`.

Pros:
- Zero schema change.
- Bodies always fresh.

Cons:
- Extra GitHub REST call per dispatched item per tick. Default 30 s tick × ~5 ready items = ~600 calls/h. We just spent #888 narrowing the same surface; this regresses it.
- Failure mode is strictly worse: API error → either dispatch with empty body (same bug) or block dispatch (new bug). Current code at least dispatches.
- New coupling — `orchestrator` now reaches into the `adapter` at dispatch time, not just at sync time. Today `ScheduleOnce` reads only from `state` (clean separation). Option B blurs it.
- Increased dispatch latency (network round-trip on the hot path).
- Doesn't address the root: the body we already fetched at Sync is still discarded. We are re-fetching to dodge the persistence gap.

### Option C — in-memory LRU keyed by work_item_id between `Sync` and `ScheduleOnce`

Add a process-lifetime LRU. `adaptersync` populates on Sync; `ScheduleOnce` reads on dispatch.

Pros:
- Zero schema change.
- No extra API round-trips during steady state.

Cons:
- Lost on restart. Soak operator bounces the binary repeatedly during the docker-soak; every restart triggers a re-fetch storm.
- New tunable (size, TTL) the operator has to reason about. Self-host filter (`feedback_default_simpler`) rejects new knobs absent demand signal.
- New invalidation problem: any code path that mutates the row without updating the cache desyncs. Cache invalidation is hard; we add it instead of removing it.
- Same "data we already fetched is discarded" critique as B — the cache is a band-aid over the gap between adapter and state, not a fix.
- Pure-addition PR (`feedback_deletion_default` — nothing gets smaller).

## 3. Design (Recommendation)

**Option A — schema migration `0022_work_items_body.sql` + struct/upsert/select threading + ScheduleOnce composite read.**

Defense:

1. **Fixes the root cause stated in #1092.** The issue title literally is "work_items.body column doesn't exist." A and only A removes the discrepancy between the adapter contract (`schemas.WorkItem.Body string`) and the persistence shape (no `body` column). Per `feedback_root_cause`.
2. **Cheapest at runtime.** Body persists once per Sync, served from the same row read already done for `Identifier`/`Title`/`Status`. No new round-trip, no new cache, no new tunable.
3. **Smallest LoC delta.** One `.sql` file, one struct field, three SQL touches (INSERT cols, UPDATE cols, SELECT cols), one `ScheduleOnce` composite-read tweak. Plus tests. Options B and C ship more code for a worse outcome.
4. **Matches the established pattern.** Goose `.sql` files are the only column-introducing pattern in this codebase (`0001`–`0021` all use `-- +goose Up`/`-- +goose Down` fences). Reviewers can diff against `0019_work_items_run_id.sql` as ground truth.
5. **Survives restart.** Self-host operator restarts the binary daily; Options B and C re-fetch on every restart, A does not.
6. **Preserves the filesystem-brief escape hatch.** The `ItemBody` callback stays as the highest-priority source for operator overrides; state.Body is consulted only on filesystem miss; the WARN fires only when BOTH miss. Operator workflow today does not regress.
7. **Drift-resistant.** Future refactors cannot quietly drop `Body` from the persistence path — it is in the column list, the struct, and the test asserts non-empty round-trip.

## 4. Migration plan (Option A)

Next migration number: `0022`. The current head on `origin/main` is `0021_substrate_kind_tool_call.sql` (verified via `git ls-tree origin/main internal/orchestrator/state/migrations/`). Goose runs migrations in lexicographic order; there is one historical gap (`0008` is absent and intentionally so per the existing ladder), so the next slot is `0022` not `0008`.

File: `internal/orchestrator/state/migrations/0022_work_items_body.sql`. Skeleton (shape, NOT code, per "DO NOT write code"):

- `-- +goose Up` / `-- +goose StatementBegin`
- `ALTER TABLE work_items ADD COLUMN body TEXT NOT NULL DEFAULT '' COLLATE BINARY;`
- `-- +goose StatementEnd`
- `-- +goose Down` / `-- +goose StatementBegin`
- `SELECT 1;` (forward-only convention — matches every other migration in this repo, see `0002_work_items.sql:26-30`).
- `-- +goose StatementEnd`

In-Go touchpoints in the same PR:

- `internal/orchestrator/state/work_items.go` — add `Body string` to `type WorkItem struct` between `Title` and `Lane` (mirrors the contract's field order).
- `internal/orchestrator/state/work_items_upsert.go::UpsertWorkItem` — extend the UPDATE SET clause (line ~36) and INSERT column list / placeholders (line ~52) to include `body`.
- `internal/orchestrator/state/work_items_query.go` (`selectWorkItemsCols` / `scanWorkItems`) — extend the SELECT projection and the rowscan to surface `Body`.
- `internal/orchestrator/state/work_items_batch_upsert.go` — same column threading as `UpsertWorkItem` for the bulk path (sibling implementation by inspection of file names; verify in the implementing PR).
- `internal/orchestrator/adapter/githubissues/adapter.go` — no change. Already populates `WorkItem.Body` from `p.Body` at lines 164 and 252.
- `contracts/schemas/spec_adapter.go` — no change. `Body string` already declared at line 43.
- `internal/orchestrator/orchestrator_schedule.go` (lines 74-83) — change the composite-read order:

  1. If `o.cfg.ItemBody != nil` and returns `(body, true)` → use it (filesystem-brief override stays highest-priority).
  2. Else attempt `wi, err := o.db.GetWorkItem(ctx, a.WorkItemID)`; if `err == nil` and `wi.Body != ""` → use `wi.Body`.
  3. Else emit `orchestrator.item_body_missing` WARN and proceed with empty body (preserve current never-block-dispatch semantics).

Backout: revert PR. The migration is forward-only; `ALTER TABLE ... DROP COLUMN` works on SQLite ≥3.35 (the project pins newer) but is not required — the column is unused after revert and the disk cost is bounded.

Operator first-start behavior: `ADD COLUMN` is O(1) in SQLite. First `Sync` after upgrade populates `body` for all currently-tracked items. Brief-file users see no change — the filesystem path stays the highest-priority source.

Telemetry: keep the WARN, narrow its trigger. The event's existence is load-bearing for operator observability of the (now-rarer) "we have neither a brief file nor a tracked body" case. Call out the narrowed trigger in the release-notes block so operators do not assume the WARN was suppressed.

## 5. Acceptance criteria

1. After upgrade, `orchestrator.item_body_missing` slog events drop to zero during a 1-hour docker-soak run with the GitHub-issues adapter tracking at least one open `autonomous` issue (no `.regatta/items/<id>.md` brief present).
2. When the filesystem brief loader returns a body, that body is dispatched (filesystem-brief override remains highest-priority — regression-test).
3. When neither the filesystem loader nor `state.WorkItem.Body` has content, the WARN fires once per tick per item (current behavior preserved).
4. `PRAGMA table_info(work_items)` on a freshly-migrated DB lists `body` with type `TEXT`, `NOT NULL`, default `''`, `COLLATE BINARY`.
5. Existing DBs survive migration with no data loss; pre-existing rows have `body=''` until the next adapter Sync populates them.
6. Re-running migrations is a no-op (goose's standard version-table guard).
7. `make check` and `make ci-check` pass.
8. PR production LoC delta under ~150 lines (sanity check on the "smallest surface" claim; if it grows beyond that, re-open Options B/C).

## 6. Test plan

TDD order — failing test FIRST, then implementation. Each test's RED output captured in the PR body per `feedback_tdd_discipline`.

1. **Migration unit test** — extend `internal/orchestrator/state/migrate_test.go` (or a sibling `work_items_body_migration_test.go` mirroring `work_items_run_id_migration_test.go`): open a fresh DB, run all migrations, assert `PRAGMA table_info(work_items)` includes `body TEXT NOT NULL DEFAULT '' COLLATE BINARY`. RED before `0022_work_items_body.sql` lands.
2. **Idempotency test** — re-run migrations; assert no error, column-count unchanged. Goose's version table handles this; the test pins the contract.
3. **Legacy-DB test** — same pattern as `internal/orchestrator/state/work_items_run_id_migration_test.go`: seed a pre-`body` row via raw SQL, run migrations, assert the existing row survives with `body=''` and every other column intact.
4. **UpsertWorkItem round-trip** — extend `internal/orchestrator/state/work_items_upsert_test.go`: call `UpsertWorkItem` with `WorkItem{Body: "hello"}`, call `GetWorkItem`, assert `Body == "hello"`. RED before the SELECT projection extends.
5. **Empty-body round-trip** — `UpsertWorkItem` with `Body: ""`, read back as `""` (not NULL). Pins the `NOT NULL DEFAULT ''` choice.
6. **adaptersync persists Body** — extend `internal/orchestrator/adaptersync/adaptersync_test.go`: drive a fake `SpecAdapter` returning one `schemas.WorkItem{Body: "issue text"}`; run one `Sync`; query `state.GetWorkItem`; assert `Body == "issue text"`. RED until the persistence path is end-to-end.
7. **ScheduleOnce composite-read** — extend `internal/orchestrator/orchestrator_test.go`:
   - Sub-case A: filesystem `ItemBody` returns `(body, true)` → that body dispatched, no state lookup needed (verify with a counting fake `db.GetWorkItem`), no WARN.
   - Sub-case B: `ItemBody` misses, `state.WorkItem.Body != ""` → `Body` dispatched, no WARN.
   - Sub-case C: both miss → WARN fires, dispatch proceeds with empty body (preserves current semantics).
   - Use the existing slog-capture helper to assert event counts. RED before `ScheduleOnce` rewires.
8. **Mutation gate** — deliberately comment out the SELECT-clause `body` projection and confirm the round-trip + composite-read tests fail. Validates that the tests exercise the column, not just the struct field.

Integration coverage:

- Soak smoke (follow-up, non-blocking): run docker-compose harness for ≥30 min with the GitHub-issues adapter tracking ≥1 open `autonomous` issue and no `.regatta/items/<id>.md` brief; grep stderr for `item_body_missing`; assert zero.

## 7. Open questions

1. **Body length cap.** GitHub issue bodies can technically reach 1 MB (REST limit). The filesystem loader already caps at 256 KB (`MaxItemBodyBytes` in `cmd/regatta/wire_itembody.go:31`). Should `UpsertWorkItem` enforce the same cap on write to keep the SELECT projection cheap, with a `state.work_items.body.oversize_truncated` slog event on truncation? Default proposal: yes, mirror `MaxItemBodyBytes` so filesystem and adapter paths agree.
2. **Filesystem vs state precedence — is "file wins" the right default?** A "file always wins" rule lets the operator override a stale or wrong adapter body by dropping a markdown file. Alternative: "state wins, file is fallback" so operators stop maintaining briefs once the adapter is reliable. Default proposal: file wins (preserves today's escape hatch; smaller behavior delta).
3. **Per-adapter body coverage.** Today only `githubissues` populates `WorkItem.Body`. The `markdown_catalog` adapter (`internal/orchestrator/adapter/markdown.go`) may or may not — verify in the implementing PR and document. Adapters that omit `Body` land items with `body=''` and the warning surface stays the same as today (no regression).
4. **AdapterSync write coverage.** Confirm `adaptersync.Syncer.Sync` is the only call site for `UpsertWorkItem` on the adapter path; if `BriefLoader` (the brief-source path, `source=brief`) also calls into the persistence layer, decide whether brief-source rows also persist `Body` or whether the brief-file content remains canonical. Default proposal: brief-source rows persist `Body=""` (the file is canonical for that source); the composite read in `ScheduleOnce` resolves the actual dispatched body.

## 8. Out of scope

- Body truncation in the prompt builder (separate concern — prompt token budget; upstream of `ScheduleOnce`).
- Push notifications / webhook-driven body refresh; periodic re-Sync owns staleness.
- Bodies for non-`github_issues` adapters beyond verifying the contract is upheld (case-by-case; track separately if/when added).
- Operator-facing knob to disable body persistence (no demand signal; self-host filter rejects new toggles).
- DB vacuuming / file-size policy (orthogonal; the body column is a small contributor in the self-host single-repo regime).
- Encryption at rest for issue bodies (self-host loop runs on the operator's own machine — no multi-tenant boundary; host disk encryption covers it).
- Reworking the filesystem-brief loader (`buildItemBodyLoader`) or its 256 KB cap (this spec preserves it as the override path).

## 9. Citations

All references verified against `git ls-tree origin/main` (head `f68d35e6`). Reviewers should re-run these:

- `internal/orchestrator/orchestrator_schedule.go:74-83` — the WARN site and the file-loader call. Command: `git -C $R show origin/main:internal/orchestrator/orchestrator_schedule.go | sed -n '74,83p'`.
- `cmd/regatta/wire_itembody.go:16-80` — the filesystem `ItemBody` callback, including `MaxItemBodyBytes` (line 31) and the WARN sub-events `item_body_loader.readdir_failed` / `item_body_loader.symlink_skipped` / `item_body_loader.oversize_skipped`.
- `internal/orchestrator/state/work_items.go:56-70` — `type WorkItem struct` with no `Body` field. Command: `git -C $R show origin/main:internal/orchestrator/state/work_items.go | sed -n '56,70p'`.
- `internal/orchestrator/state/work_items_upsert.go:12-67` — `UpsertWorkItem` INSERT/UPDATE column lists omit `body`. Command: `git -C $R show origin/main:internal/orchestrator/state/work_items_upsert.go | sed -n '12,67p'`.
- `internal/orchestrator/state/migrations/0002_work_items.sql:7-20` — `work_items` table definition. Command: `git -C $R show origin/main:internal/orchestrator/state/migrations/0002_work_items.sql`.
- Migration head: `0021_substrate_kind_tool_call.sql`. Command: `git -C $R ls-tree origin/main internal/orchestrator/state/migrations/ | awk '{print $4}' | sort | tail -1`.
- `contracts/schemas/spec_adapter.go:43` — `Body string \`json:"body,omitempty"\``. Command: `git -C $R show origin/main:contracts/schemas/spec_adapter.go | sed -n '37,50p'`.
- `internal/orchestrator/adapter/githubissues/adapter.go:164, 252` — `Body: p.Body` populated from `parseIssueBody`. Command: `git -C $R show origin/main:internal/orchestrator/adapter/githubissues/adapter.go | sed -n '160,170p;248,256p'`.

Numeric / version claims are paired with the commands that produced them. No OSS-prior-art license claims in this spec.

## 10. Adversarial

Independent reviewer (cavecrew-reviewer or equivalent) MUST be spawned BEFORE the APPROVE token lands on the implementer PR. Likely findings:

- Migration ordering: the new `body` column MUST be added BEFORE any reader code is changed; backfill MUST be idempotent (re-run safe). Tests cover both an empty-body and a backfilled-body row.
- Body size: GitHub issue bodies can be ≤65536 chars but adapters truncate at 32k by convention. Migration uses `TEXT` (sqlite unlimited); loader truncates on read for OTel attribute hygiene.
- Stale-cache: when the upstream issue body is edited mid-loop, the cache MUST refresh on next adapter poll (already covered by the existing `adapter.ListReady` upsert path; no new code).

## 11. Implementer brief

Slug: `item-body-cache`.
Migration N: pinned at dispatch time; one new migration adds `work_items.body TEXT NOT NULL DEFAULT ''`.
File scope:

- EDIT `internal/orchestrator/state/migrations/NNN_work_items_body.sql` — add column + backfill from existing filesystem cache where present.
- EDIT `cmd/regatta/wire_itembody.go::buildItemBodyLoader` — query `work_items.body` first; fall back to filesystem only when the column is empty.
- EDIT `internal/adapter/github_issues/adapter.go::ListReady` — populate `work_items.body` on upsert.
- ADD `internal/orchestrator/state/work_items_body_test.go` — covers backfill + read-after-upsert.

Independent reviewer required (load-bearing — adapter contract + migration). Per `feedback_no_self_tagged_approve`: spawn fresh-slot reviewer BEFORE the APPROVE token lands. `Reviewer-agent-id:` + `Reviewer-recommendation: APPROVE` in PR body footer (bare, not in a code block).

## 12. Reopen trigger

Reopen this spec when ANY of:

- A non-GitHub adapter (Gitea, Linear, JIRA) lands and `work_items.body` semantics need to be per-adapter.
- Body size routinely exceeds the truncation threshold (operator complaint or dashboard surface need).
- The filesystem-cache fallback is removed (would require this spec's read path to be the sole source of truth).
