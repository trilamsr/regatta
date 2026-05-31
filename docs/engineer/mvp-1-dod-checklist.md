# MVP-1 Definition-of-Done Checklist

Per spec §6. Tick each item as the PR series progresses. Anything
left unchecked blocks the v0.1.0 tag.

## Merge-time (per PR)
- [ ] `go test -race ./...` green
- [ ] `make ci-check` exit 0
- [ ] Paired `_test.go` for every new file

## Series-complete
- [ ] `regatta program plan --write` then `regatta serve --tick-once`
      yields exactly 3 work_items WHERE parent_program_id='PROG-1'
- [ ] Exactly 3 agents WHERE state='running' for those work_item_ids
- [ ] Zero events at `slog.LevelWarn` or higher during happy-path run.
      Happy path means: adapter returns a stable item set across both
      ticks (nothing disappears), brief verifies on first read, no
      tombstones fire. Tombstone WARN events (`adapter.tombstoned`,
      `brief.tombstoned`, `child.cascade_archived`,
      `child.dependency_archived`) are *expected* in adversarial
      scenarios; they are only "noise" in the steady-state baseline.
- [ ] CHANGELOG.md updated with MVP-1 entry
- [ ] `docs/engineer/specs/mvp-1-planner.md` deleted (superseded)

## Grade-A (production-trustable) checks
- [ ] `grep -rn 'errors.New(' internal/orchestrator/ internal/program/`
      returns only `internal/orchestrator/errors.go`
- [ ] Every slog WARN path enumerated below is emitted by at least one test:
  - [ ] `brief.rejected` (HMAC fail, sha mismatch, parse error)
  - [ ] `brief.tombstoned` (file disappeared)
  - [ ] `adapter.tombstoned` (item disappeared)
  - [ ] `child.cascade_archived` (parent archived); emitted by `AdapterSync.CascadeArchiveChildren` caller
  - [ ] `child.dependency_archived` (depends_on target tombstoned)
- [ ] Adversarial tests green:
  - [ ] Brief disappears mid-poll
  - [ ] AdapterSync fails; fail-fast
  - [ ] Stale flock; reclaim succeeds
  - [ ] HMAC rotation; old and new keys coexist in keyring
  - [ ] Tombstoned dep; auto-cascade child
- [ ] DAG property test: 200 random DAGs n<=8, reserved set == topological-ready set
- [ ] Operator docs include: program plan walkthrough, flock troubleshooting, slog event reference
- [ ] Per-package coverage thresholds (run `go test -cover`):
  - [ ] `internal/orchestrator/state` >= 90%
  - [ ] `internal/program` >= 85%
  - [ ] `cmd/regatta` >= 70%

## Grade-A+ (stretch)
- [ ] Mutation test: comment out version-check in `state/migrate.go`,
      `TestMigrate_DowngradeResistance` must fail
- [ ] MTTD a rejected brief <= 60 seconds using only `journalctl | grep`;
      operator runbook in [`docs/operator/quickstart.md`](../operator/quickstart.md)
      timed externally
- [ ] Operator recovery procedure for corrupted `.regatta/state.db`
      documented and tested in [`docs/operator/quickstart.md`](../operator/quickstart.md)

## Decision -> file mapping

| Decision (per spec "Locked decisions") | Implementation site |
|---|---|
| 1. Universal queue | `state.work_items` table; `migrations/0002_work_items.sql` |
| 2. Two writers, one reader | `adaptersync.Sync` + `program.BriefLoader.Sync` write; `scheduler.Tick` reads |
| 3. Scheduler join | `state.ListSpawnable` SQL in `work_items_query.go` |
| 4. Cascade-soft | `state.CascadeArchiveChildren`; only touches work_items |
| 5. Cascade snapshot | `program.BriefLoader.Sync` -> `acceptance_json` per child |
| 6. Sign-then-persist | `program.LoadAndVerifyBrief`; Validate + VerifySignature before upsert |
| 7. slog WARN rejections | `brief_loader.go` `slog.Warn("brief.rejected", ...)` |
| 8. DAG enforce + cycle check | `state.ListSpawnable` deps clause + `state.CycleCheck` |
| 9. pollStartedAt cutoff | `orchestrator.PollOnce` captures, passes to both syncs |
| 10. Fail-fast PollOnce | `orchestrator.PollOnce` returns on first error |
| 11. Flock | `internal/orchestrator/lockfile/lockfile.go` |
| 12. sqlite stays | `state.go` `_ "modernc.org/sqlite"` driver |
| 13. TDD + library-first | every step in the plan; `goose` + `gofrs/flock` deps |
