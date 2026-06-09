---
title: "Scheduler file-scope collision check (#1065)"
status: design
phase: self-host-s2
issue: 1065
summary: "Add an optional `FileScopeExtractor` to `scheduler.Config`; when set, `reserveFromSpawnable` defers a candidate whose declared file scope overlaps any active agent on the same lane, closing the cascade-rebase storm path observed 2026-06-08 when agents 11+13+14 (#1061 #1063 #1064) all targeted `internal/orchestrator/spawner/claude.go`."
date: 2026-06-09
---

# Scheduler file-scope collision check — Spec

Memory rules in force: `feedback_default_simpler`, `feedback_cascade_rebase_root_cause`, `feedback_parallel_safety`, `feedback_no_signatures`, `feedback_cite_origin_main_not_local`, `feedback_spec_pattern_authority`.

```release-notes
[DOCS] specs: scheduler file-scope collision design (#1065)
```

## §1 Problem

2026-06-08 dogfood: operator filed 7 `autonomous`-labeled issues (#1058-#1064) covering distinct orchestrator-improvement scopes. `regatta serve` (PID 31758) spawned all 7 in a single 30s adapter poll cycle against `lane: server`. Adversarial review (subagent `aa42da89d96e38d32`) confirmed #1061 + #1063 + #1064 all touch the same shared anchor `internal/orchestrator/spawner/claude.go`. Without manual intervention the result is the cascade-rebase storm pattern captured in `feedback_cascade_rebase_root_cause`: first-to-merge wins, sibling PRs hit DIRTY merge state, operator absorbs N rebases.

`CLAUDE.md` (origin/main) anchored rule (per `feedback_parallel_safety`):

> **File-disjoint only in parallel; sequence chained-output work.**

The scheduler today does not model file scope. `internal/orchestrator/scheduler/scheduler.go::Tick` (verified `git ls-tree origin/main internal/orchestrator/scheduler/scheduler.go`) sequences `gate_l0 → gate_cost_cap → gate_approval → gate_cost → gate_l4 → dispatch → persist`. The `dispatch` step calls `reserveFromSpawnable(ctx, tc, spawnable, occupancy)` whose only structural admission control is `occupancy[lane] < LaneCaps[lane]` (see `scheduler_lane_cap.go`). No projection of "which files will this work item touch" exists; `state.WorkItem` (verified `git ls-tree origin/main internal/orchestrator/state/work_items.go`) carries:

```
type WorkItem struct {
    ID, Kind, Title, Lane, Status, ParentProgramID string
    DependsOnFeatures []string
    AcceptanceJSON string
    Source WorkItemSource
    LastSeenAt, CreatedAt, UpdatedAt time.Time
}
```

No `FileScope` field; no extractor seam; lane is the only structural concurrency knob. When N issues share lane AND overlap on a shared anchor, the scheduler spawns them concurrently and the cascade is downstream of a design gap, not a routing accident.

Root cause per `feedback_root_cause`: the scheduler's structural admission model has one axis (lane) when the cascade-rebase failure mode needs two (lane + file scope). Adding a file-scope check at the dispatch boundary is the upstream fix; downstream rebase mitigations (auto-rebase workers, sibling-stack rebase tooling) treat the symptom.

## §2 Design

### §2.1 Where does file-scope live?

Two options considered:

- **(a) Issue body extraction (operator-declared)** — parse a `File scope` block, or fall back to regex over the acceptance-criteria section for backticked paths under `cmd|internal|scripts|docs|contracts/...`. Declaration lands BEFORE the scheduler ticks. Source of truth lives on the issue, visible to operator.
- **(b) PR-diff post-hoc analysis** — wait for the agent to push, then compare diffs. Rejected: by the time a diff exists the scheduler has already spawned, the collision has already happened, and the rebase storm is in flight. Too late.

**Adopt (a).** Source of truth is the issue body, which the operator already authors. The default adapter extractor (`github_issues`) regexes acceptance criteria; bespoke adapters supply their own.

### §2.2 Schema

Add a typed field to `state.WorkItem`:

```go
// FileScope is the predicted set of repo-relative paths or glob patterns
// the work item is expected to touch. NIL or empty = unbounded scope;
// the scheduler treats unbounded as "may touch anything" and refuses to
// co-schedule it with any other work item on the same lane.
// Populated by the adapter via Config.FileScopeExtractor when set; the
// JSON column is `file_scope_json` (TEXT, NULL default) — fold-in
// migration N+1.
FileScope []string
```

Add per-source resolver seams to `scheduler.Config` (matches `OutputsSchemaResolver` per-source precedent — see §5):

```go
// FileScopeExtractorByAdapter returns the predicted file paths/globs a
// work item will touch when WorkItem.Source == "adapter". nil disables
// the collision check for adapter-sourced items (pre-#1065 behavior —
// lane-cap-only). Non-nil + empty-slice return = unbounded scope
// (treated as "always collides on this lane").
FileScopeExtractorByAdapter func(state.WorkItem) []string

// FileScopeExtractorByBrief returns the predicted file paths/globs a
// work item will touch when WorkItem.Source == "brief" (or any non-
// adapter source). nil disables the collision check for brief-sourced
// items.
FileScopeExtractorByBrief func(state.WorkItem) []string
```

`reserveFromSpawnable` dispatches by `wi.Source`. Both seams nil ⇒ feature globally disabled (byte-equal pre-#1065).

Default `github_issues` adapter-side extractor (lives at `internal/orchestrator/adapter/githubissues/file_scope.go`):

1. If issue body contains `## File scope` H2, parse the following fenced block as one path/glob per line.
2. Else: regex `` `(cmd|internal|scripts|docs|contracts|Makefile\.d|\.github)/[^`]+` `` over the acceptance-criteria section.
3. Return empty slice when no matches (caller treats as unbounded).

### §2.3 Scheduler invariant

At most ONE in-flight agent per overlapping file-scope pattern per lane. Wired in the existing `dispatch` step:

```
reserveFromSpawnable(ctx, tc, spawnable, occupancy):
  for each candidate wi in spawnable:
    if occupancy[wi.Lane] >= LaneCaps[wi.Lane]: skip            # existing
    ext := extractorFor(cfg, wi.Source)                          # per-source dispatch
    if ext != nil:
      cand := ext(wi)
      if len(cand) == 0:
        log "scheduler.file_scope_unbounded" INFO with
          work_item_id=wi.ID, lane=wi.Lane, source=wi.Source     # §3 required
      for each active agent a on wi.Lane:
        aExt := extractorFor(cfg, a.WorkItem.Source)
        active := aExt(a.WorkItem)
        if overlaps(cand, active, cfg.SharedAnchors):
          log "scheduler.file_collision_deferred" with
            work_item_id=wi.ID, colliding_agent_id=a.ID,
            colliding_files=intersection
          metric scheduler_file_collision_deferred_total++
          continue OUTER                                         # defer to next tick
    reserve wi

extractorFor(cfg, source):
  if source == "adapter": return cfg.FileScopeExtractorByAdapter
  return cfg.FileScopeExtractorByBrief
```

`overlaps([]string, []string) bool`:

- Empty/nil on either side → `true` (unbounded scope collides with everything; see §2.5).
- Else: for each pair `(p, q)` use `github.com/gobwas/glob` (pinned at `v0.2.3`; verified `git -C $REPO grep -E 'gobwas/glob' go.mod` returns `github.com/gobwas/glob v0.2.3 // indirect` — promote to direct in the implementer PR). Compile each pattern via `glob.Compile(p, '/')` and call `g.Match(q)` both directions, plus `prefixOverlap(p, q)`. `prefixOverlap` handles the common case of a glob `internal/orchestrator/**` overlapping a literal `internal/orchestrator/spawner/claude.go` after normalizing trailing `/**`. `filepath.Match` is rejected for this seam because its `*` does not cross `/` and `**` is unsupported, breaking the dominant declared-scope shape (`internal/**/spawner.go`).

Complexity per tick: `O(C · A · F²)` where C = candidates, A = active agents, F = max file-scope size. Empirically C+A ≤ 8 and F ≤ 10 — negligible vs an SQLite gate-pass (§5 adversarial: covered).

### §2.4 New observability

- slog event `scheduler.file_collision_deferred` with attrs `work_item_id`, `colliding_agent_id`, `lane`, `colliding_files`.
- slog event `scheduler.file_scope_unbounded` (INFO) with attrs `work_item_id`, `lane`, `source` — emitted exactly once per `(work_item_id, tick)` when the extractor returns nil/empty. Required per §3 c-criterion.
- metric `regatta_scheduler_file_collision_deferred_total{lane}` — counter, lane label.
- Existing `scheduler.dispatched` event gains attr `file_scope_count` (always emitted; `0` when extractor nil).

### §2.5 Degenerate / failure modes

1. **Unbounded scope (nil/empty FileScope)** — treated as "may touch anything"; deferred against any active agent on the same lane. The required `scheduler.file_scope_unbounded` INFO event (§3) makes this visible. Operator override: leave BOTH `FileScopeExtractorByAdapter` and `FileScopeExtractorByBrief` nil to disable the check globally.
2. **Operator override** — add `scheduler.disable_file_scope_check: true` to `regatta.yaml`. When true, `New(...)` sets BOTH extractor fields to `nil` regardless of resolver wiring. Logged at orchestrator construction as `scheduler.file_scope_check_disabled` WARN.
3. **Shared anchor that no work item declares** — two work items declare disjoint scopes (`internal/web/` and `cmd/regatta/`) but both touch `CLAUDE.md` or `Makefile.d/ci.mk`. The collision check does not catch this — declaration honesty is a pre-condition. Mitigation: §2.6 (anchor-list cross-product).
4. **FileScopeExtractor panic** — recovered by the dispatch step; treated as nil for that candidate (logged WARN); does not halt the tick.

### §2.6 Shared-anchor cross-product

For known-god-file anchors that every PR touches in practice, `scheduler.shared_anchors` lives in `regatta.yaml`. **Default (non-empty) ships in `contracts/schemas/regatta.cue`:**

```
scheduler.shared_anchors: [
  "CLAUDE.md",
  "Makefile",
  "Makefile.d/*",
  "docs/engineer/specs/README.md",
]
```

An empty default re-introduces cascade risk (the 2026-06-08 storm hit `internal/orchestrator/spawner/claude.go` plus repeated CLAUDE.md touches); operators may override by setting an empty list explicitly, but the shipped default is non-empty.

**Decision rule (resolved here — pick A, conservative):** the collision check treats a candidate as overlapping every active agent on the same lane IFF the candidate's lane is the same AND **at least one side** (candidate scope OR active-agent scope) names a shared anchor pattern. The mirror form ("both sides declare the same shared anchor → defer") is rejected: one declaration is sufficient, matching the "godfile blocks everything" intent and preserving the conservative posture per `feedback_default_simpler`. Rationale: a single PR touching `CLAUDE.md` is enough to serialize the lane, since any sibling PR that lands first will force a rebase on the CLAUDE.md change.

Match semantics: each shared-anchor entry is fed to the same `gobwas/glob` compiler as §2.3; literal entries (`CLAUDE.md`, `Makefile`) match by direct string equality after pattern compilation; glob entries (`Makefile.d/*`) match via `g.Match(path)`.

### §2.7 Reference prior pattern

Mirrors `scheduler.Config.HotspotResolver` (lock-name resolver, existing), `scheduler.Config.CostGateResolver` (per-wi gate resolver, existing), and `OutputsSchemaResolver` (per-source resolver — direct precedent for dual extractor seams). Same shape: nil → identity / disabled; non-nil → wired into `reserveFromSpawnable`. No new architectural concept introduced.

## §3 Acceptance

1. `state.WorkItem` gains `FileScope []string` field with JSON tag `file_scope`; migration N+1 adds `file_scope_json TEXT NULL` to `work_items`; `state.upsertWorkItem` marshals on write and unmarshals on read. Migration number pinned by implementer brief (§6).
2. `scheduler.Config` gains BOTH `FileScopeExtractorByAdapter func(state.WorkItem) []string` and `FileScopeExtractorByBrief func(state.WorkItem) []string` (nil-default, opt-in), plus `SharedAnchors []string` populated from `regatta.yaml`.
3. `scheduler_reserve.go::reserveFromSpawnable` defers a candidate when its extracted scope overlaps the scope of any active agent on the same lane; deferral is silent in metrics if extractor nil; counted otherwise.
4. `regatta.yaml` schema gains `scheduler.disable_file_scope_check: bool` (default `false`) — when `true`, `New(...)` nil's both extractor fields. `scheduler.shared_anchors: [string]` ships with non-empty default `["CLAUDE.md", "Makefile", "Makefile.d/*", "docs/engineer/specs/README.md"]` (§2.6).
5. Default `github_issues` adapter extractor lives at `internal/orchestrator/adapter/githubissues/file_scope.go`; parses `## File scope` block first, falls back to acceptance-criteria backtick regex.
6. New regression test in `internal/orchestrator/scheduler/scheduler_test.go`:
   - 3 candidates same lane same single-file scope → 1 spawn first tick, 1 spawn second tick (after first completes), 1 spawn third tick.
   - 3 candidates same lane disjoint scopes → 3 spawns same tick.
   - 1 candidate nil scope + 1 active agent any scope same lane → defer.
   - 1 candidate scope overlap with active on DIFFERENT lane → spawn (lane gate independent).
   - Extractor panic on one candidate → that candidate skipped, others proceed.
7. New event `scheduler.file_collision_deferred` emitted with `work_item_id`, `colliding_agent_id`, `lane`, `colliding_files`.
8. New metric `regatta_scheduler_file_collision_deferred_total{lane}` exposed via OTel meter.
9. **REQUIRED** empty-scope mitigation event `scheduler.file_scope_unbounded` (INFO level) emitted once per `work_item_id` per tick window when the extractor returns nil or empty, with attrs `work_item_id`, `lane`, `source` (`WorkItem.Source` — `adapter` / `brief`). A test in `internal/orchestrator/scheduler/scheduler_file_scope_test.go` asserts the event fires exactly once per `(work_item_id, tick)` for an unbounded-scope candidate; missing emission fails CI.
10. **Per-source extractor routing** — `scheduler.Config` ships TWO extractor seams, not one:
    - `FileScopeExtractorByAdapter func(state.WorkItem) []string` — invoked when `WorkItem.Source == "adapter"`.
    - `FileScopeExtractorByBrief func(state.WorkItem) []string` — invoked when `WorkItem.Source == "brief"` (and any other non-`adapter` source).
    Mirrors `OutputsSchemaResolver` (per-source resolver) precedent called out in §5. `reserveFromSpawnable` dispatches by `wi.Source`; both code paths MUST have regression-test coverage in `scheduler_file_scope_test.go` (one adapter-sourced + one brief-sourced collision fixture). A single-resolver design is explicitly rejected; serve.go wires both, and `cmd/regatta/serve.go` test (or a new integration test) covers the wiring.
11. TDD order per `feedback_tdd_discipline`: failing test commits land FIRST; PR body captures RED output; impl + green follow.
12. `make ci-check` exits 0 at PR HEAD.
13. Independent reviewer subagent dispatched per `feedback_no_self_tagged_approve`; verdict pasted at §5 + `Reviewer-agent-id:` in PR footer.

## §4 Out of scope

- **PR-diff post-hoc scope inference** — rejected at §2.1 (too late to prevent the cascade).
- **Automatic shared-anchor learning** — operator-declared list in `regatta.yaml`; auto-detection (e.g. "files touched by >N% of recent PRs") deferred. File follow-up issue if anchors-list maintenance burden surfaces.
- **Cross-lane file collision** — out of scope: lanes are the primary concurrency partition; cross-lane sharing of a god-file is rare in practice and would require lane-cap=1 globally to mitigate. Reopen if observed.
- **Auto-rebase worker** — downstream symptom mitigation; treats cascade after it occurs. Distinct concern; not this spec. Reference `feedback_cascade_rebase_address_immediately`.
- **Sibling-stack rebase tooling** — covered by `feedback_rebase_onto_for_sibling_stacks`; orthogonal.
- **Spawner-side hotspot lock unification** — existing `HotspotResolver` covers DB-level locks (`internal/orchestrator/state/locks.go`). File-scope collision is admission-time, not lock-time; collapsing both into one resolver is plausible but rejected per `feedback_default_simpler` until a second concrete need surfaces.

## §5 Adversarial pass

Reviewer dispatch PENDING — to be paste-filled by independent `cavecrew-reviewer` subagent in a fresh slot before the PR moves out of draft, per `feedback_no_self_tagged_approve` + `feedback_adversarial_review_every_step`. The author-drafted candidate edge cases below are reviewer fodder, NOT a substitute for the independent pass:

- **Declaration drift** (declared scope ≠ actual diff): operator declares `internal/web/` but agent edits `cmd/regatta/serve.go`. The collision check is blind. Mitigation: `verify` job in CI can compare PR diff against declared scope and emit a `pr-lint.file_scope_drift` warning; not in this spec. Tracker required when drift first observed.
- **Cascade re-introduction via shared anchor** (CLAUDE.md, Makefile, generated `docs/engineer/specs/README.md`): §2.6 ships a non-empty `scheduler.shared_anchors` default (`CLAUDE.md`, `Makefile`, `Makefile.d/*`, `docs/engineer/specs/README.md`) — reviewer should hunt residual cascade risk beyond that list (e.g. `cmd/regatta/serve.go` as a composition root) and decide whether to extend before ship.
- **Performance** (`O(C·A·F²)` glob match per tick): plausible problem at high C+A; reviewer must confirm worst-case bound under self-host load (C+A ≤ 8 observed). Reject premature caching unless reviewer benchmarks show >5ms tick overhead. Compile globs once per tick (cache `glob.Glob` per pattern string).
- **Empty-scope-collides-with-everything is surprising**: handled by required §3 c-criterion (`scheduler.file_scope_unbounded` INFO event); listed here only as the originating concern.
- **Glob library mandated**: §2.3 mandates `github.com/gobwas/glob v0.2.3` (already in `go.mod` as indirect — promote to direct in the implementer PR). `filepath.Match` is rejected (`**` unsupported, `*` does not cross `/`); reviewer must verify the import promotion and confirm no fallback to `filepath.Match` slipped in.
- **Per-source extractor routing**: handled by required §3 c-criterion (dual seams `FileScopeExtractorByAdapter` + `FileScopeExtractorByBrief`) — reviewer must confirm both code paths have regression-test coverage, not just the adapter side.

## §6 Implementer brief

```
Scope: Add scheduler.Config.FileScopeExtractor seam + state.WorkItem.FileScope field + default
       github_issues extractor + reserveFromSpawnable collision check. Opt-in only; nil
       extractor preserves pre-#1065 behavior byte-equal.

Files:
  internal/orchestrator/state/work_items.go             (add FileScope field)
  internal/orchestrator/state/migrations/0022_file_scope.sql  (re-audit at impl-PR-open time)
  internal/orchestrator/scheduler/scheduler.go          (Config: dual per-source extractor fields + SharedAnchors)
  internal/orchestrator/scheduler/scheduler_reserve.go  (collision check + per-source dispatch + unbounded INFO event)
  internal/orchestrator/scheduler/scheduler_file_scope.go  (new — gobwas/glob overlap helper, shared-anchor check)
  internal/orchestrator/scheduler/scheduler_file_scope_test.go (new — collision regression: adapter-sourced + brief-sourced + unbounded INFO + shared-anchor)
  internal/orchestrator/scheduler/scheduler_test.go     (extend dispatch test fixtures)
  internal/orchestrator/adapter/githubissues/file_scope.go      (new — default adapter extractor)
  internal/orchestrator/adapter/githubissues/file_scope_test.go (new)
  cmd/regatta/serve.go                                  (wire BOTH extractors; honor disable_file_scope_check; load shared_anchors)
  contracts/schemas/regatta.cue                         (scheduler.disable_file_scope_check / shared_anchors default list)
  go.mod / go.sum                                       (promote github.com/gobwas/glob to direct)
  docs/engineer/pointers.md                             (add this spec under scheduler section)

TDD order:
  1) Land RED: scheduler_file_scope_test.go + extender of scheduler_test.go covering
     §3.6 fixtures. Run `go test ./internal/orchestrator/scheduler/ -run FileScope`,
     paste failing output in PR body.
  2) Land migration + WorkItem field + Config field + adapter extractor.
  3) Land reserveFromSpawnable wiring + serve.go opt-in. Green.

migration N (pinned at spec-author time via `git ls-tree origin/main internal/orchestrator/state/migrations/ | tail -3`):
  HEAD (origin/main, 2026-06-09):
    0019_work_items_run_id.sql
    0020_approval_events_run_id.sql
    0021_substrate_kind_tool_call.sql
  Next free = 0022. Implementer MUST re-audit at impl-PR-open time per
  `feedback_migration_number_lock` (sibling specs may consume 0022 first); the dispatch prompt
  re-pins the number at that point. Spec-time pin is a hint, not a contract.

Glob library: `github.com/gobwas/glob v0.2.3` is verified in `go.mod` as indirect (run
  `git -C $REPO grep -E 'gobwas/glob' go.mod` to re-verify). Implementer PR MUST promote it to
  direct via `go get github.com/gobwas/glob@v0.2.3` + commit the `go.mod` / `go.sum` updates.
  `filepath.Match` is forbidden for this seam — see §2.3 mandate.

make ci-check exit: 0
Reviewer dispatch: YES — load-bearing scheduler change. cavecrew-reviewer subagent in fresh slot.
  Paste verdict in spec §5 + PR body `Reviewer-agent-id:` + `Reviewer-recommendation:`.

PR title: [FEAT] scheduler: file-scope collision check (closes #1065)
Release-notes prefix: [FEAT]
NO automerge per feedback_no_implementer_automerge — end with `gh pr ready <N>`.
```

## §7 Reopen trigger

This spec is `status: design` until the implementer PR lands. After ship, reopen when:

- A new cascade-rebase incident lands in retro with ≥3 simultaneous DIRTY PRs on a single anchor file (per `feedback_cascade_rebase_address_immediately`) — extend §2.6 shared-anchor handling.
- Declaration drift incident lands (declared scope ≠ PR diff produces a cascade) — wire the `pr-lint.file_scope_drift` warning called out in §5.
- `regatta_scheduler_file_collision_deferred_total` consistently >0 in a 7-day window for a single `work_item_id` (work item permanently stuck behind a collision) — reopen to design starvation backoff.
- Operator runs `scheduler.disable_file_scope_check: true` for >24h — collision check is over-rejecting; reopen for false-positive analysis.
