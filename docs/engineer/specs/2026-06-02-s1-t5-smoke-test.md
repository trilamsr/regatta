# S1-T5 — self-host smoke test (Phase S1 acceptance gate)

**Status**: spec
**Owner**: self-host phase S1
**Brief**: [docs/engineer/briefs/2026-06-01-self-host-first.md](../briefs/2026-06-01-self-host-first.md) §3 S1-T5
**Memory cites**: `feedback_research_design_principles` · `feedback_decision_priority` · `feedback_grade_rubric` · `feedback_self_improvement` · `feedback_pr_body_file_only` · `feedback_test_godoc_one_line`

---

## 1. Problem

Phase S1 needs an acceptance gate that asserts the self-host loop wires together end-to-end. The four S1 prerequisites (T1 `regatta.yaml` + items dir, T2 cost-governor spawner callback, T3 boot-prompt converter, T4 cost-governor Wave 3) each tested their own seam. None of them assert the combined invariant: *given a fresh `regatta.yaml` + a single `[followup]`-labelled item in `.regatta/items/`, the orchestrator picks the item up, hands it to a spawner, observes the resulting PR, and transitions the work_item to merged*. Without that gate, the four T-tasks could each pass individually while the combined loop still has a wiring defect (importable but un-runnable).

S1-T5 ships that gate as a Go integration test under `tests/selfhost/` plus the fixture artefacts it consumes. The test fails on import — by construction the failing test must precede implementation per `feedback_tdd_discipline`. The fixture is byte-identical to the S1-T1 seed format so any drift in the seed schema breaks the smoke test first.

## 2. Scope (in)

1. **One Go test file** at `tests/selfhost_smoke_test.go` (alongside the existing `tests/selfhost_regatta_yaml_test.go` from S1-T1, same `package tests`). Single test function: `TestSelfHost_FollowupIntakeToPRMerge`. One line of godoc per `feedback_test_godoc_one_line`.
2. **Fixture tree** at `tests/testdata/selfhost_smoke/` containing:
   - `.regatta/items/SMOKE-001.md` — one `[followup]` work item referencing real open issue #92 (cross-restart-persistent brief replay). Status `planned`, lane `server`, single acceptance criterion.
   - No fixture `regatta.yaml` — the markdown adapter is constructed directly from `MarkdownCatalogConfig{Root: <fixture-dir>}`. The yaml-loader path is already covered by `selfhost_regatta_yaml_test.go`; doubling it here would test the parser, not the loop.
3. **In-process wiring** that mirrors `internal/program/end_to_end_v2_test.go`:
   - Open temp sqlite DB (`state.Open`).
   - Construct `MarkdownCatalog` adapter pointed at the fixture's `.regatta/items/`.
   - Wire `AdapterSync` + `BriefLoader` + `Scheduler` + `spawner.Stub` into an `orchestrator.Orchestrator`.
   - Drive `PollOnce` → `ScheduleOnce` → `stub.Complete(workItemID, prPayload)` once.
4. **Assertions** (all four MUST pass):
   - **A1 intake**: `db.GetWorkItem(ctx, "SMOKE-001")` returns a row with `Source == SourceAdapter` after `PollOnce`. This proves the markdown adapter parsed the fixture.
   - **A2 spawn**: `stub.Calls()` contains exactly one `spawner.Request` with `WorkItemID == "SMOKE-001"`. This proves the scheduler reserved the item and the orchestrator handed it to the spawner.
   - **A3 PR observation**: after `stub.Complete(ctx, "SMOKE-001", prPayload)` where `prPayload` is JSON `{"pr_url":"https://github.com/trilamsr/regatta/pull/SMOKE","completed":true}`, the latest journal entry for SMOKE-001 carries that exact PR URL string in `OutputJSON`. This proves the journal seam preserves the PR-opened signal end-to-end.
   - **A4 state transition**: `db.GetWorkItem(ctx, "SMOKE-001")` returns `Status == WorkStatusMerged` after Complete. This is the existing Stub.Complete contract — re-asserted here as the loop-level gate.

## 3. Scope (out)

- **Real `claude` subprocess**. `serve_claude_test.go` already exercises that seam; doubling here would add ~30 s of flake to every CI run for no new signal. Decision-priority: long-term > velocity; the smoke test value is *loop wiring correctness*, and the Stub spawner is the right tool for that question. Per `feedback_research_design_principles`, proven existing primitive (`spawner.Stub` in use across 12 test files) beats a custom mock.
- **Live GitHub API calls**. The smoke test runs offline. The PR URL in the journal is a synthetic literal; a future S2 test can promote this to a live `gh` invocation behind a `regatta_smoke_live=1` env gate when the operator wants to test against the real adapter.
- **Approval gate execution**. `human_merge` is the operator's manual step in the S1 brief §3 acceptance criterion ("operator merges (manual final step)"). The smoke test stops at `WorkStatusMerged` — the state the orchestrator drives to right before the gate engages.
- **Multi-tenant fields**. Single-tenant per brief §3 + Phase X deferral.
- **Pre-existing seed items**. `S1-SEED-001/002/003.md` already live in `.regatta/items/` and reference different issues; piggy-backing on those would couple the smoke test to whatever those seeds happen to look like next month. The fixture is independent.

## 4. File location decision

**Choice**: `tests/selfhost_smoke_test.go` (same `package tests` as the existing `tests/selfhost_regatta_yaml_test.go` from S1-T1).

Rationale:

- The `tests/` package already exists and already hosts a self-host integration test (`selfhost_regatta_yaml_test.go`). Adding a second file `tests/selfhost_smoke_test.go` follows the established convention — one file per self-host concern, all in one package.
- `cmd/regatta/` is binary-build territory; mixing a 200-line integration test in there would force the smoke test to import `main` indirectly (the `cmd/regatta/serve_claude_test.go` build-tag pattern). The `tests/` package is plain library territory — imports `internal/orchestrator`, `internal/orchestrator/adapter`, `internal/orchestrator/spawner`, `internal/program` like any other test.
- Fixture lives at `tests/testdata/selfhost_smoke/` — colocated with the test, namespaced from the existing yaml-validation fixture so the two cannot collide.

Alternatives considered + rejected:

- `internal/selfhost/smoke_test.go`: an `internal/` package with no production code is mis-organization.
- `tests/selfhost/smoke_test.go` (new subdir): would create a second package alongside the existing `package tests`, doubling import boilerplate for no signal.

## 5. Adversarial reviewer notes

Run an adversarial reviewer subagent on this spec before implementation. Expected findings checklist:

- **Q: Why a synthetic PR URL instead of `gh pr create --draft` against a sacrificial branch?** A: spec §3 covers this. The whole-loop wiring claim is testable without network; promoting to live `gh` is S2 territory and would gate every CI run on a network round trip.
- **Q: What stops the seed `.regatta/items/SMOKE-001.md` from drifting independently of the production `.regatta/items/*.md` schema?** A: by living under `tests/selfhost/testdata/`, the smoke test's fixture parses through the same `adapter.NewMarkdownCatalog` constructor that production uses. If the schema drifts, the test fails before merge.
- **Q: Single test, no table-driven cases?** A: yes — smoke tests are explicitly singular. The four assertions are the four properties of the loop; splitting them into four `t.Run` blocks would shadow a partial failure (A1 pass, A2 fail) under separate output lines. One test, four `t.Fatalf` checkpoints, fail at the first.
- **Q: What about the cost-governor wiring (S1-T2)?** A: the smoke test does NOT exercise the spend-callback path — `spawner.Stub` does not call the callback. A future S2 test (or extension of this one) can wire `spend.SpawnerCallback` once the cost-governor Wave 3 cleanup lands. This omission is intentional: smoke tests prove the spine, not every limb.
- **Q: Why fixture issue #92 specifically?** A: smallest open `[followup]`, scope estimate ~50 LoC + test, body is self-contained (no cross-issue references). Picking #80 (durable audit log, 800+ LoC scope) or #83 (composite index, requires bench harness) would lock the smoke fixture's prose against a much larger followup that would itself need a full design pass.

## 6. Acceptance rubric

Per `feedback_grade_rubric`. Scorecard MUST appear verbatim in the PR body.

### B (floor — ships)

- [ ] `tests/selfhost_smoke_test.go` exists, parses, builds.
- [ ] `tests/testdata/selfhost_smoke/.regatta/items/SMOKE-001.md` parses via `adapter.NewMarkdownCatalog` without error.
- [ ] `go test ./tests/...` exits 0.
- [ ] `bash scripts/doc-check.sh` exits 0 (no banned phrases, test godoc one line, link integrity).
- [ ] `make pre-push-check` exits 0.

### A (target — expected)

- [ ] B met.
- [ ] All four assertions (A1 intake / A2 spawn / A3 PR observation / A4 state transition) explicit + named in test output.
- [ ] Fixture references a real open `[followup]` issue (#92) so a reader can trace the smoke target back to a live work item.
- [ ] Test fails informatively when any one of the four assertions breaks — `t.Fatalf` messages name the assertion ID (`A1`, `A2`, `A3`, `A4`).
- [ ] Spec adversarial-reviewer subagent run before implementation; findings (if any) folded back into spec or test.
- [ ] PR body posts the scorecard verbatim per `feedback_grade_rubric` enforcement clause.

### A+ (stretch — exceptional)

- [ ] A met.
- [ ] Smoke test runs in under 1 s wall-clock on the CI runner (no sleep, no network, deterministic clock).
- [ ] Test file is < 200 LoC (anti-bloat — `feedback_deletion_default`).
- [ ] Spec doc + test cite the same six memory rules listed in §0, byte-identical token list, grep-checkable.

## 7. Implementation plan (TDD strict)

1. Write `tests/selfhost/smoke_test.go` skeleton with the four `t.Fatalf` checkpoints stubbed. Run `go test ./tests/selfhost/...` — expect compile failure (no fixture, missing imports). Capture output.
2. Add `tests/selfhost/testdata/regatta.yaml` + `tests/selfhost/testdata/.regatta/items/SMOKE-001.md`. Re-run — expect runtime failure (orchestrator not yet wired).
3. Fill in the orchestrator wiring (DB open, adapter, syncer, brief loader, scheduler, spawner.Stub, orchestrator.New). Re-run — expect A1 pass + A2 pass.
4. Add `stub.Complete(ctx, "SMOKE-001", prPayload)` plus the A3 + A4 assertions. Re-run — expect green.
5. Run `bash scripts/doc-check.sh`. Fix any banned phrases or godoc multi-line in the spec or test.
6. Run `make pre-push-check`. Fix any lint findings.
7. Open PR with body file containing the scorecard verbatim.

## 8. Open questions

None. Spec is implementation-ready.

## Resolution (2026-06-02)

Shipped via #348 (`feat(self-host): S1-T5 — end-to-end smoke test for the Phase S1 loop`). Stub spawner exit-zero path drives `SMOKE-001` to `WorkStatusMerged` under `go test -short -tags=smoke ./cmd/regatta/...`.
