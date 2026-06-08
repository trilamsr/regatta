---
title: "cmd/regatta/serve.go residual per-subsystem split (cascade-rebase anchor reduction)"
status: draft
summary: "Continue the #737 pattern. Six wire_*.go siblings already exist (secrets, keyring, spec_adapter, web, alarm_webhook, spawner, scheduler, cost_cap, authz, prwatch, reconcile, merge_worker, itembody, tick_once, config, flags). runServe (cmd/regatta/serve.go) still inlines orchestrator construction, reaper wiring, evaluator/loader composition, the scheduler.New(...) call, and the clock literal. Four file-disjoint slices land wire_orchestrator.go / wire_reaper.go / wire_evaluator.go and shrink runServe to <100 LOC of pure orchestration (flag parse, signal/ctx, listener bind, shutdown order). Acceptance: regatta serve boots byte-equal pre/post."
---

# cmd/regatta/serve.go residual per-subsystem split — Design Spec

Date: 2026-06-08
Trigger source: `feedback_cascade_rebase_root_cause` — serve.go remains a parallel-wave anchor even after [#737](https://github.com/trilamsr/regatta/pull/737) (1068→394 LOC). Open shared-primitive owner scan in `CLAUDE.md::Dispatch` still names `cmd/regatta/serve.go` first.
Prior art: [#737](https://github.com/trilamsr/regatta/pull/737) (initial split), [#744](https://github.com/trilamsr/regatta/pull/744) (`serve_*.go`→`wire_*.go` rename).
Memory rules in force: `feedback_default_simpler`, `feedback_deletion_default`, `feedback_root_cause`, `feedback_cascade_rebase_root_cause`, `feedback_adversarial_review`, `feedback_audit_main_before_implementing`, `feedback_spec_pattern_authority`, `feedback_no_signatures`.

```release-notes
[DOCS] Design spec for residual cmd/regatta/serve.go split. Six wire_*.go
siblings already shipped via #737/#744. The runServe body still composes
orchestrator + reaper + evaluator + scheduler.New inline. Four file-
disjoint slices extract wire_orchestrator.go / wire_reaper.go /
wire_evaluator.go and shrink runServe to <100 LOC of pure boot
orchestration. No code in this PR.
```

## §0 Closing trigger

Done when ALL of:

1. Impl PR (separate from this spec) merges referencing this spec.
2. `wc -l cmd/regatta/serve.go` ≤ 100 (orchestration root only — flags, signal/ctx, listener bind, shutdown order).
3. `grep -n "orchestrator.New\|reaper.New\|program.NewEdgeEvaluator\|program.NewBriefLoader\|scheduler.New" cmd/regatta/serve.go` returns 0 matches.
4. `cmd/regatta serve --tick-once` boot trace before/after the split is byte-equal modulo timestamps (operator-observable contract preserved).
5. `make ci-check` green on impl PR.

## §1 Problem

Even after #737/#744, `cmd/regatta/serve.go::runServe` (399 LOC, lines 113-399) still inlines five subsystem compositions:

- **clock literal** (`serve.go:159` — `clock := time.Now`) — single composition-root wall-clock source threaded into every subsystem Config; comment is 5 lines.
- **evaluator + brief loader** (`serve.go:205-226` — `program.NewEdgeEvaluator()` + `program.NewBriefLoader(...)`) — 22 LOC; constructs the shared cel.Program cache for scheduler step-0 Eval + brief materialisation.
- **scheduler instance** (`serve.go:247-258` — `scheduler.New(db, scheduler.Config{...})` with 10 fields wired) — helpers exist in `wire_scheduler.go` (`buildApprovalGate`, `buildMergeWiring`, `outputsSchemaResolverFor`) but the final `scheduler.New(...)` composition lives in `runServe`.
- **orchestrator instance** (`serve.go:260-275` — `orchestrator.New(orchestrator.Config{...})` with 13 fields wired) — 16 LOC; pulls together adapter sync, brief loader, scheduler, spawner set, item body, repo paths, ticker cadences, and clock.
- **reaper instance + setter** (`serve.go:276-284`) — 9 LOC; `reaper.New(reaper.Config{...})` gated on `set.Worktrees != nil`.

Each new subsystem-touching PR (autotuner, BYOA, byom-providers, multi-target-repo, live-outcome) lands a Config field or Setter call here. Three of the last 15 PRs DIRTY-rebased on serve.go-adjacent diffs. Per `feedback_cascade_rebase_root_cause`, the design defect is the orchestrator-config god-call, not "normal merge math".

## §2 Goal

Shrink `serve.go::runServe` to ≤100 LOC of pure boot orchestration — flag parse, signal/ctx wiring, listener bind, shutdown order. Every subsystem construction lives in its own `wire_<subsystem>.go` sibling with an exported `buildXxx(cfg flags, deps Deps) (*Xxx, error)` (or `start*` for goroutine-owning helpers).

Self-host filter: cascade-rebase is a single-operator velocity tax. Keep in scope.

## §3 Pattern (authoritative)

Match the #737 pattern. Each new wire file:

1. Lives at `cmd/regatta/wire_<subsystem>.go` (snake-case noun, matches the subsystem package or composition role).
2. Exports ONE primary constructor `buildXxx(...) (*Xxx, error)` OR `startXxx(ctx, ...) func()` for goroutine-owning helpers (return a stop/wait-for-shutdown closure).
3. Imports only what the subsystem needs — no shared `serveContext` struct, no hidden globals. Pass primitives explicitly (db, clock, logger, repo paths).
4. Sibling test file `wire_<subsystem>_test.go` when the helper has non-trivial branching (e.g. nil-default fallback, error path).
5. Stays under 200 LOC. If a wire file would exceed 200 LOC, the subsystem is too coarse — split further (precedent: `wire_scheduler.go` 133 LOC + `wire_cost_cap.go` 72 LOC + `wire_merge_worker.go` 26 LOC).
6. Godoc on the exported helper opens with the symbol name and captures WHY in ≤1 sentence (`feedback_comments_lint_reconcile`).

`runServe` in `serve.go` stays the boot orchestration root only — its body is a linear sequence of `buildXxx` / `startXxx` calls + error returns + ctx wiring. No struct field plumbing inline.

## §4 Concerns extracted (already shipped, do NOT re-extract)

| Subsystem        | File                          | Primary symbol               |
|------------------|-------------------------------|------------------------------|
| flag parsing     | `wire_flags.go`               | `parseServeFlags`            |
| repo config root | `wire_config.go`              | `loadMarkdownCatalogRoot`    |
| secrets cache    | `wire_secrets.go`             | `buildSecretFetcherFromRepo` |
| brief keyring    | `wire_keyring.go`             | `loadBriefKeyringWithActive` |
| spec adapter     | `wire_spec_adapter.go`        | `buildSpecAdapter`           |
| spawner set      | `wire_spawner.go`             | `buildSpawner`               |
| item body loader | `wire_itembody.go`            | `buildItemBodyLoader`        |
| approval gate    | `wire_scheduler.go`           | `buildApprovalGate`          |
| rejection router | `wire_scheduler.go`           | `buildRejectionRouter`       |
| merge wiring     | `wire_scheduler.go`           | `buildMergeWiring`           |
| merge worker     | `wire_merge_worker.go`        | `startMergeWorker`           |
| outputs schemas  | `wire_scheduler.go`           | `outputsSchemaResolverFor`   |
| cost cap         | `wire_cost_cap.go`            | `buildCostCapEnforcer`       |
| cost reconciler  | `wire_reconcile.go`           | `startReconciler`            |
| authz (OPA)      | `wire_authz.go`               | `buildAuthorizer`            |
| review reconcile | `wire_authz.go`               | `startReviewReconciler`      |
| PR watcher       | `wire_prwatch.go`             | `startPRWatcher`             |
| listener + UI    | `wire_web.go`                 | `bootListener`               |
| alarm webhook    | `wire_alarm_webhook.go`       | `startAlarmWebhook`          |
| tick-once mode   | `wire_tick_once.go`           | `runTickOnce`                |

## §5 Concerns NOT yet extracted (this spec's scope)

| Inline today                                        | Target file                | Target symbol                                      |
|-----------------------------------------------------|----------------------------|----------------------------------------------------|
| `scheduler.New(db, scheduler.Config{...})` call     | `wire_scheduler.go`        | `buildScheduler(db, f, deps) *scheduler.Scheduler` |
| `orchestrator.New(orchestrator.Config{...})` call   | `wire_orchestrator.go` NEW | `buildOrchestrator(db, f, deps) *orchestrator.Orchestrator` |
| `reaper.New(reaper.Config{...})` + nil-guard        | `wire_reaper.go` NEW       | `attachReaper(o, set, db, clock, logger)`          |
| `program.NewEdgeEvaluator()` + `program.NewBriefLoader(...)` | `wire_evaluator.go` NEW | `buildEvaluator(briefsDir, db, costKey, costKeyID, logger) (Evaluator, *BriefLoader, error)` |
| `clock := time.Now` (with godoc justifying threading) | inline OK in `serve.go` | n/a — clock is the boot-level seam; one-liner stays |

Out of scope (#9): rewriting subsystem APIs. Wire helpers MUST be pure refactors — same struct fields, same nil/zero defaults, same error returns.

## §6 Implementer slices (4, file-disjoint, sequential — each depends on prior)

Slices are sequential because each pulls signature from the prior (`buildOrchestrator` consumes the `scheduler` returned by `buildScheduler`; `attachReaper` mutates the orchestrator returned by `buildOrchestrator`). Parallelisation is unsafe — implementer who dispatches in parallel hits a `serve.go` merge-conflict storm and contradicts the spec's purpose.

### Slice 1 — `buildScheduler` (extracts inline `scheduler.New(...)`)

- File: `cmd/regatta/wire_scheduler.go` (existing, append).
- Add: `buildScheduler(db *state.DB, f serveFlags, deps schedulerDeps) *scheduler.Scheduler` where `schedulerDeps` bundles `Evaluator`, `OutputsSchemas`, `Gate`, `GateResolver`, `CostCap`, `Clock`, `MergeCoordinator`, `MergeWorker`.
- `runServe` change: replace `serve.go:247-258` with `sched := buildScheduler(db, f, schedulerDeps{...})`. Net delta: -10 LOC inline + 14 LOC in wire file.
- Test: `wire_scheduler_test.go` (existing? — verify; if missing, add `TestBuildScheduler_DefaultsPropagate` asserting LaneCaps / LockTTL / Clock pass through).
- Acceptance: `regatta serve --tick-once` boot trace byte-equal modulo timestamps.

### Slice 2 — `buildOrchestrator` + `attachReaper` (NEW files)

- Files: `cmd/regatta/wire_orchestrator.go` (NEW), `cmd/regatta/wire_reaper.go` (NEW).
- `wire_orchestrator.go`: `buildOrchestrator(db *state.DB, f serveFlags, deps orchestratorDeps) *orchestrator.Orchestrator` — bundles `AdapterSync`, `BriefLoader`, `Scheduler`, `Spawner`, `ItemBody`, cadences (`PollInterval`, `TickInterval`, `HeartbeatInterval`, `LockTTL`), `RepoRoot`, `DBPath`, `Logger`, `Clock`. ~50 LOC including godoc + struct definition.
- `wire_reaper.go`: `attachReaper(o *orchestrator.Orchestrator, set spawnerSet, db *state.DB, clock func() time.Time, logger *slog.Logger)` — no-op when `set.Worktrees == nil`, otherwise calls `o.SetReaper(reaper.New(reaper.Config{...}))`. ~25 LOC.
- `runServe` change: replace `serve.go:260-284` with `o := buildOrchestrator(db, f, orchestratorDeps{...})` + `attachReaper(o, set, db, clock, slogger)`. Net delta: -25 LOC inline + 75 LOC across two wire files.
- Tests: `wire_orchestrator_test.go::TestBuildOrchestrator_ConfigPropagation` (assert 13 fields wired); `wire_reaper_test.go::TestAttachReaper_NilWorktreesNoOp` (assert no SetReaper call when set.Worktrees is nil).

### Slice 3 — `buildEvaluator` (NEW file)

- File: `cmd/regatta/wire_evaluator.go` (NEW).
- Symbol: `buildEvaluator(briefsDir string, db *state.DB, costKey []byte, costKeyID string, logger *slog.Logger) (*program.EdgeEvaluator, *program.BriefLoader, error)`.
- Constructs evaluator + brief loader as a single composed unit (the evaluator is passed INTO the loader config — splitting them across two files would re-introduce the inline composition seam this spec is removing).
- `runServe` change: replace `serve.go:192-226` (briefsDir mkdir + evaluator + loader) with `evaluator, loader, err := buildEvaluator(...)`. mkdir stays inline OR moves into buildEvaluator — implementer picks; default = move (mkdir is a buildEvaluator precondition).
- Test: `wire_evaluator_test.go::TestBuildEvaluator_LoaderShareEvaluator` (assert the same `*EdgeEvaluator` pointer is on `BriefLoader.Evaluator` and the returned evaluator — composition contract is "shared cache survives across ticks").

### Slice 4 — `runServe` shrink to <100 LOC orchestrator

- File: `cmd/regatta/serve.go` (edit).
- Sweep: with slices 1-3 merged, `runServe` body is:
  1. flag parse (`parseServeFlags`)
  2. logger init (`newLogHandler`)
  3. secrets boot (existing 6 lines)
  4. clock literal (1 line + godoc kept — clock IS the boot-level seam)
  5. preflight + signal ctx + db open (existing ~6 lines)
  6. `buildSpecAdapter` + `buildSpawner` + `buildEvaluator` + `buildApprovalGate` + `buildCostCapEnforcer` + `buildMergeWiring` + `buildScheduler` + `buildOrchestrator` + `attachReaper` (linear, ~12 lines)
  7. `o.SetRejectionRouter(buildRejectionRouter(...))` (1 line)
  8. `startPRWatcher` + `startMergeWorker` + `startReviewReconciler` + `o.Recover` (existing)
  9. `buildAuthorizer` + `bootListener` + listener serve goroutine + shutdown defer (existing)
  10. `startReconciler` + `startAlarmWebhook` (existing)
  11. tick-once branch + `o.Run(ctx)` (existing)
- Strip stale multi-line godoc whose justification now lives in the wire file. Keep: package godoc (lines 1-5), `defaultListenerAddr`, `listenerShutdownBudget`, `reconcilerShutdownBudget`, `defaultLogFormat`, `logFormatJSON`, `newLogHandler`, `logFormatFlag`, `laneCapsFlag`, `runServe`.
- Acceptance: `wc -l cmd/regatta/serve.go` ≤ 100. Add `cmd/regatta/wire_naming_test.go::TestServeFileSize` ceiling at 110 LOC (10 LOC slack for future imports).

## §7 Acceptance criteria

1. `wc -l cmd/regatta/serve.go` ≤ 100.
2. `grep -nE 'orchestrator\.New|reaper\.New|program\.NewEdgeEvaluator|program\.NewBriefLoader|scheduler\.New' cmd/regatta/serve.go` returns ZERO lines.
3. `regatta serve --tick-once` boot trace before/after the four-slice impl PR is byte-equal modulo `time.Now()` timestamps. Capture via `regatta serve --tick-once 2>&1 | grep -v '^\\d\\d:\\d\\d:\\d\\d'`.
4. `make ci-check` green on impl PR.
5. `git diff origin/main...HEAD -- cmd/regatta/` shows net LOC delta ≤ +20 (extractions should be near-zero-sum; godoc dedupe shrinks total).

## §8 Adversarial pass

Per `feedback_adversarial_review_every_step` this spec gets an adversarial read before dispatch. Risks the implementer MUST defend against in the impl PR body:

### Risk A — nil-pointer flow across composition order

`scheduler.New` receives `gate` + `gateResolver` + `costCapEnf` from preceding builders. `orchestrator.New` receives `syncer` + `loader` + `sched` + `set.Spawner`. Reaper attach reads `set.Worktrees`. If a builder is moved OUT OF the current top-down order — e.g. `buildScheduler` called before `buildApprovalGate` returns — `scheduler.Config.Gate` is nil and the first `Tick()` panics on gate evaluation.

Defense: implementer MUST keep the call sequence in `runServe` matching the CURRENT order in `serve.go:113-399`. Slice 4 is the ONLY slice that touches call order; it MUST diff-verify the sequence pre/post.

Test seam: `wire_naming_test.go::TestServeCallOrder` greps `runServe` body for the substring sequence `buildSpecAdapter`, `buildSpawner`, `buildEvaluator`, `buildApprovalGate`, `buildCostCapEnforcer`, `buildMergeWiring`, `buildScheduler`, `buildOrchestrator`, `attachReaper`. Reorder → test fails.

### Risk B — race conditions in init

The goroutines spawned in `runServe` (`secretCache.Run`, `watchSecretsExport`, listener serve goroutine, `startMergeWorker`, `startReconciler`, `startAlarmWebhook`, `startReviewReconciler`) all consume the ctx OR a logger pointer. Hoisting any of them into a wire file MUST keep ctx-cancellation as the sole shutdown path — no time-based shutdown, no shared bool flag.

Defense: every `start*` helper returns either `func()` (shutdown closure called via `defer`) or `<-chan struct{}` (done signal). No helper spawns a goroutine that outlives runServe.

### Risk C — test seam preservation

The existing `wire_*_test.go` files use construction-time assertions (config field propagation, nil-default behaviour). Extracting `buildOrchestrator` / `buildScheduler` MUST NOT break the existing constructors' callers in tests that bypass runServe.

Defense: implementer MUST `git grep -l 'orchestrator\.New\|scheduler\.New\|program\.NewBriefLoader' -- '*_test.go'` BEFORE slice 1, list every test file, and confirm none of them call `runServe`. Tests construct via package-level helpers directly — those stay untouched. The new wire helpers are additive; subsystem package constructors keep their existing signatures.

### Risk D — comment density drift

Current `serve.go` carries 5-line godocs on inline subsystems (clock, mergeWiring, costReconciler, rejectionRouter, alarmWebhook). Moving the subsystem out of serve.go MUST move the godoc with it — otherwise the wire file loses the WHY and `serve.go` keeps orphan-narration.

Defense: each slice's commit MUST move the inline godoc to the wire file (NOT duplicate it). Reviewer lens 9 (comment sweep) gates.

## §9 Out of scope

- Rewriting subsystem APIs (orchestrator.Config field names, scheduler.Config field names, reaper.Config field names stay byte-identical).
- Splitting wire files further (e.g. `wire_orchestrator_config.go` + `wire_orchestrator_setters.go`) — until a wire file exceeds 200 LOC, single-file-per-subsystem stays.
- Introducing a shared `serveContext` struct or DI container — explicit primitive passing is the pattern.
- Test-double `Clock` flag (`--clock-source`) — separate spec when a real test or operator need surfaces. The current `clock := time.Now` literal in serve.go is intentionally inline so the threading point is visible.

## §10 Dispatch brief (per `feedback_dispatch_brief_only`)

When the impl PR is dispatched, the implementer subagent gets the per-slice brief below — NOT the full spec doc.

### Brief, Slice 1 — `buildScheduler`

> Pattern authority: this spec, §3, §5, §6 (Slice 1), §8 (Risk B/D).
> Task: extract `cmd/regatta/serve.go:247-258` (`scheduler.New(db, scheduler.Config{...})`) into a new exported helper `buildScheduler(db *state.DB, f serveFlags, deps schedulerDeps) *scheduler.Scheduler` in `cmd/regatta/wire_scheduler.go` (existing file, append). `schedulerDeps` is a private struct bundling the 8 wired-in dependencies. TDD: failing test FIRST asserting `buildScheduler` returns a `*scheduler.Scheduler` whose LaneCaps + LockTTL + Clock match the input — capture failing output in PR body, then impl. `make ci-check` green. PR body release-notes prefix `[REFACTOR]`. Reviewer required (load-bearing, composition root).

### Brief, Slice 2 — `buildOrchestrator` + `attachReaper`

> Pattern authority: this spec, §3, §5, §6 (Slice 2), §8 (Risk A/B).
> Task: create `cmd/regatta/wire_orchestrator.go` (NEW) with `buildOrchestrator(db, f, deps orchestratorDeps) *orchestrator.Orchestrator` + create `cmd/regatta/wire_reaper.go` (NEW) with `attachReaper(o, set, db, clock, logger)`. Replace `serve.go:260-284`. Tests: `wire_orchestrator_test.go::TestBuildOrchestrator_ConfigPropagation` + `wire_reaper_test.go::TestAttachReaper_NilWorktreesNoOp`. Risk A defense: keep call order; do NOT reorder builders. `make ci-check` green. `[REFACTOR]`. Reviewer required.

### Brief, Slice 3 — `buildEvaluator`

> Pattern authority: this spec, §3, §5, §6 (Slice 3), §8 (Risk B/C).
> Task: create `cmd/regatta/wire_evaluator.go` (NEW) with `buildEvaluator(briefsDir, db, costKey, costKeyID, logger) (*program.EdgeEvaluator, *program.BriefLoader, error)`. Move briefsDir mkdir INTO buildEvaluator. Replace `serve.go:192-226`. Test: `wire_evaluator_test.go::TestBuildEvaluator_LoaderSharesEvaluator` (assert pointer identity — same evaluator on returned struct and inside loader.Config). `make ci-check` green. `[REFACTOR]`. Reviewer required.

### Brief, Slice 4 — `runServe` shrink

> Pattern authority: this spec, §3, §5, §6 (Slice 4), §7 (acceptance), §8 (Risk A/D).
> Task: with slices 1-3 merged, sweep `cmd/regatta/serve.go::runServe` to ≤100 LOC. Move inline godoc on hoisted subsystems to their wire files (do NOT duplicate). Add `cmd/regatta/wire_naming_test.go::TestServeFileSize` ceiling at 110 LOC. Add `wire_naming_test.go::TestServeCallOrder` greps `runServe` for the call-order substring sequence (§8 Risk A). Acceptance §7 ALL five conditions green. `[REFACTOR]`. Reviewer required.

## §11 Self-host filter check

Per `CLAUDE.md::Self-host filter`: does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended? YES — cascade-rebase storms on serve.go directly cost the operator velocity. Three of the last 15 PRs DIRTY-rebased on serve.go-adjacent diffs. Keep in scope.

No Phase-X tokens (`tenant_id`, `RBAC`, `Stripe`, `Sigstore`, `Rekor`, `blackboard`, `Temporal`) appear in this spec.

## §12 Rollback

Each slice is an independent revertable commit. If post-merge regression surfaces (boot trace diff, panic on first Tick, etc.), `git revert <slice-merge-sha>` restores the prior inline composition. The four slices DO NOT introduce data migrations or schema changes — rollback is mechanical.
