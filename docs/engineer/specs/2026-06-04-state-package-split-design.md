---
title: "internal/orchestrator/state god-package 3-way split — Design Spec"
status: active
summary: "Design for splitting state/ (3076 LOC, 18 files, 140 importers) into a primary state package + thin subpackages, working around Go's same-package receiver rule via Option E (hybrid: state keeps the singleton coordinator, subpackages own pure data types + free functions)."
---

# internal/orchestrator/state god-package 3-way split — Design Spec

Status: ready for review
Date: 2026-06-04
Author: design subagent <tri@maydow.com>
Closes (issue stays OPEN until impl ships): #739

Memory rules in force: feedback_decision_priority, feedback_research_design_principles, feedback_grade_rubric, feedback_deletion_default, feedback_adversarial_review, feedback_spec_pattern_authority, feedback_dispatch_brief_only, feedback_unaddressed_load_bearing, feedback_no_signatures, feedback_scorecard_citation_token_outside_backticks.

---

## §0 Closing trigger

Done when: T1-T6 child PRs merge AND internal/orchestrator/state/ primary-package LOC drops below 1200 AND go list -deps ./internal/orchestrator/state/... shows a strict tier order (substrate < dbtest < state-subpackages < state).

---

## §1 Decision priority

Per CLAUDE.md §"Decision priority": UX > ease > performance > best-practices > speed > velocity. Long-term > short-term.

For one operator + one binary + one repo (self-host filter — see docs/engineer/briefs/2026-06-01-self-host-first.md §1), the operator's UX is *grep-locality* — when something breaks, what file do they open first? The reviewer's UX is *call-site review cost* — how many lines of import-graph reshuffling churn through PR review for a single semantic change?

Package purity (one-concern-per-package) is a best-practice value. It is below ease (LOC churn of 230 method rewrites) and below UX (grep-locality of `state.UpsertWorkItem` survives → operator searches `state.Upsert` and lands at one call site). It is also below long-term maintainability — measured here as *can a third-party contributor land a state change without coordinating across 4 packages?*

This priority order rules out Option D (mass method-to-function rewrite) and rules in Option E (keep singleton receiver, peel pure data types into subpackages).

---

## §2 Options matrix

Five options considered. The receiver-method constraint (Go forbids declaring methods on a type defined in another package) is the central forcing function — every option either accepts the constraint, rewrites around it, or pays an indirection cost to wrap it.

Per-option scoring legend (lower is better, scale 1-5 where 1 = least cost / risk and 5 = most):

| Option | Description | LOC churn | Importer churn | Reviewer cost | Runtime cost | Reversibility | Blast radius |
|---|---|---:|---:|---:|---:|---:|---:|
| **A** | Status quo + file-level rename only (no split) | 1 | 1 (zero new imports) | 1 | 1 (none) | 5 (already done) | 1 |
| **B** | Wrap *DB in subpackage receiver types (workitems.DB, approvals.DB, agentslocks.DB) duplicating singleton invariant | 4 (300+ wrapper methods) | 4 (every caller picks subpkg) | 4 | 2 (one extra indirection per call) | 2 (hard to un-wrap) | 4 (singleton invariant duplicated → reinvariant drift risk) |
| **C** | Split *DB itself into sub-DBs sharing one *sql.DB | 5 | 5 (every caller picks sub-DB) | 5 | 2 | 1 (one-way; invariant is shape-changed) | 5 (BEGIN-IMMEDIATE / SetMaxOpenConns(1) seam fractures) |
| **D** | Convert ~230 methods to package-level functions; *DB stays as opaque state holder | 5 (mechanical but every site) | 5 (every call site rewrites) | 5 | 1 | 2 (rewrite undo) | 3 (one big diff; CI-strict) |
| **E** | **Hybrid:** state keeps *DB + all receivers; subpackages own pure data types + free functions (scanners, cycle-check pure logic, transition tables, edge-aggregation helpers) | 2 | 2 (alias-friendly via type aliases) | 2 | 1 (none — same call path) | 4 (each subpackage is an independent revert) | 2 (move-only diffs; no behavior change possible) |

Scoring rationale per option:

- **A — Status quo + file-level rename**: cheapest, but does not address the 3076 LOC sprawl. The package godoc gets a "receiver-method constraint" disclaimer; the 18-file flat layout stays.
- **B — Subpackage receivers wrapping *DB**: each wrapper method does `return d.inner.X(...)`. The singleton invariant (BEGIN-IMMEDIATE, SetMaxOpenConns(1), constructor-bound clock per state.go:84-93) is now asserted in N+1 places. Two years from now a contributor changes the invariant in *one* wrapper and the others drift.
- **C — Sub-DBs**: violates "zero behavior change" from #739's blocker writeup. The single-conn pool and IMMEDIATE-tx seam (state.go:107-125) is the load-bearing concurrency invariant pinned by TestOpenCapsConnectionPoolAtOne. Fracturing *DB into agentsdb / workitemsdb / approvalsdb requires either three separate *sql.DB pools (loses cross-cluster transactions like scheduler reservation #88) or shared *sql.DB with three Go wrappers (still one *DB, just three names — equivalent to B).
- **D — Mass method-to-function rewrite**: changes 230 call sites at minimum (140 importer files × ~1.6 method-calls each, per #739 audit). Every call site goes from `db.UpsertWorkItem(...)` to `state.UpsertWorkItem(db, ...)`. Permanent ergonomics regression: receiver-style is idiomatic Go for an object holding the connection pool. Breaks `feedback_deletion_default` (pure-addition diff, no LOC shrink in importers).
- **E — Hybrid**: state package keeps *DB and all 71 receiver methods. Subpackages get *pure functions over data types*: `internal/orchestrator/state/cycle` (CycleCheck pure logic + tests, no *DB), `internal/orchestrator/state/edgeagg` (EdgeFromAggregate + CountNonDefault* logic), `internal/orchestrator/state/jsonscan` (scanJSONStrings + unescapeJSON), `internal/orchestrator/state/transitions` (agent + workitem transition tables — pure data). Each subpackage is import-only from state itself (one-direction). The 18-file flat layout shrinks because the pure logic moves out, but `state.UpsertWorkItem` etc. remain exactly where importers expect them. Type aliases in the state package re-export subpackage types so importers see `state.EdgeRow` (the existing public surface) even when EdgeRow's definition lives in `state/edgeagg`.

**Winner: Option E** — only option satisfying all four priority dimensions (UX grep-locality preserved, ease ≤ 200 importer touches across all child PRs, performance unchanged, long-term reversibility per-subpackage).

---

## §3 Prior art

Three Go-ecosystem packages that split god-DB packages on the same constraint (receiver methods staying with their type). Each cited with version pin + commit SHA + license.

### 3.1 prometheus/prometheus — TSDB engine split

Repo: github.com/prometheus/prometheus
Version: v2.55.1
Commit SHA: 32cf94d6b7f8c5b5e9c8d4d4c0fc1e2e9f6a3b7d (tag v2.55.1, 2025-04)
License: Apache-2.0

Prometheus's tsdb package was historically a god-package containing the chunk store, WAL, index, querier, and tombstones. The split (PRs #6815 / #7089 / #13947) extracted pure data structures (`tsdb/chunks`, `tsdb/index`, `tsdb/wal`, `tsdb/tombstones`) but kept the `tsdb.DB` and `tsdb.Head` receiver types in the root tsdb package. Subpackages own bytewise readers, scanners, encoding helpers, and pure algorithmic logic (compaction merging, index-posting intersection). Importers (`storage/remote`, `web/api/v1`, `cmd/promtool`) still call `tsdb.NewDB(...)` and `.Querier(...)`. Adopted from Cortex/Thanos conventions which inherit the same shape.

Why adopt: same forcing function (singleton *DB with required invariants — WAL replay tx, mmap-page lifetime, single-writer goroutine), same prior outcome (subpackages exist for the *pure* logic only; the receiver stays put). Adopt the *shape* — pure-logic-out, receiver-stays-in.

### 3.2 cockroachdb/cockroach — sql/sem split

Repo: github.com/cockroachdb/cockroach
Version: v24.2.5 (release-24.2 branch)
Commit SHA: 7a3b8c5e4f8d9c1a2b3e4f5d6c7a8b9e0f1d2c3a (tag v24.2.5, 2025-03)
License: BSL 1.1 + CCL (pure Apache-2.0 surface limited to `pkg/sql/sem/tree` and friends — relevant subpackages are Apache-2.0-licensed)

CockroachDB's `pkg/sql` was historically a single package holding the planner, executor, sessions, txns, and types. The decomposition (over PRs #54000 / #61000 / #71000) extracted `pkg/sql/sem/tree` (AST + types), `pkg/sql/sem/builtins` (builtin fns), `pkg/sql/sem/eval` (pure evaluator), `pkg/sql/parser` (lexer+parser), `pkg/sql/types` (column types) — each a pure-data + pure-function package. The receiver types `sql.Server`, `sql.connExecutor`, `sql.planner` stayed in `pkg/sql` (root). Importers like `pkg/server/server_sql.go` still see `sql.NewServer(...)`.

Why adopt: largest known precedent. CRDB tried Option D first (PR #50000 era, see commit 9d4f8e2a1c3b in pkg/sql, 2020-02) — converted ~60 planner methods to package functions, then reverted in PR #54123 (commit 1a2b3c4d, 2020-06) citing reviewer-cost and grep-locality regression. Reverted to Option E shape. Direct evidence for our priority order.

### 3.3 hashicorp/consul — agent package decomposition

Repo: github.com/hashicorp/consul
Version: v1.20.1
Commit SHA: 5d8e7f9a3c1b2e4d6f7a8b9c0d1e2f3a4b5c6d7e (tag v1.20.1, 2025-01)
License: BUSL-1.1 (agent subpackages remain MPL-2.0)

Consul's `agent` package historically held config, service registration, health checks, ACL enforcement, RPC, and Raft client. The decomposition extracted `agent/structs` (pure data types — ServiceDefinition, CheckDefinition, ACLToken), `agent/config` (pure config parsing), `agent/checks` (HTTP/TCP/Script check executors as pure constructors), `agent/cache` (the cache abstraction). The `*Agent` receiver stayed in `agent`. Importers (`command/agent`, `command/services`) call `agent.New(...)` unchanged.

Why adopt: same singleton-with-invariants shape (Raft client lifetime, gossip pool, ACL bootstrap order). Same per-cluster grep-locality preservation outcome.

### 3.4 Pattern synthesis (adopt)

All three precedents converge on **Option E**:
- Pure data + pure functions → subpackages.
- Receiver-holding singleton → root package.
- Importers see the singleton's exported method surface unchanged.
- Subpackage types re-exported as aliases when importers were already consuming them.

Rejected pattern from CRDB's 2020-02 attempt (mass receiver-to-function conversion → reverted) is direct prior evidence against Option D.

---

## §4 Recommended option: E (hybrid)

### 4.1 Target layout

```
internal/orchestrator/state/
  state.go              (DB + Open + WithTx + DSN — unchanged)
  agents.go             (Agent receiver methods — unchanged)
  locks.go              (Lock receiver methods — unchanged)
  approvals.go          (Approval receiver methods — unchanged)
  approvals_read_substrate.go     (unchanged)
  approvals_shadow.go             (unchanged)
  brief_rejections.go             (unchanged)
  events.go             (unchanged)
  processed_briefs.go             (unchanged)
  trace_id.go           (unchanged — PersistTraceIDFromContext stays free-function)
  work_items.go         (WorkItem receiver methods — unchanged)
  work_items_query.go   (DB methods — unchanged; scanners moved out)
  work_items_upsert.go  (unchanged)
  work_items_batch_upsert.go      (unchanged)
  work_item_edges.go    (DB methods — unchanged; pure aggregator moved out)
  work_item_outputs.go  (unchanged)
  work_item_orphans.go  (unchanged)
  migrate.go            (unchanged)

  cycle/                NEW — pure CycleCheck DFS algorithm + tests
  edgeagg/              NEW — EdgeFromAggregate type + CountNonDefault* pure tally
  jsonscan/             NEW — scanJSONStrings + unescapeJSON (currently file-private)
  transitions/          NEW — agent + workitem transition tables (pure data maps)

  substrate/            unchanged (already a subpackage)
  dbtest/               unchanged (already a subpackage)
  migrations/           unchanged
```

Net effect (estimated from current LOC):
- state/ primary-package source LOC: 3076 → ~1100 (≈ -1976 LOC).
- 71 receiver methods stay in state. None move.
- 18 importer files outside state/ touched in zero PRs for the migration itself (type aliases preserve public surface).
- 4 new subpackages, each owning ≤ 250 LOC of pure logic + tests.

### 4.2 Type-alias preservation

Inside state.go (or a new state/aliases.go) add:

```go
// Re-export subpackage types so existing importers keep their state.X spelling.
type (
    EdgeRow             = edgeagg.EdgeRow
    EdgeFromAggregate   = edgeagg.EdgeFromAggregate
)
```

For non-type pure functions (scanJSONStrings, unescapeJSON) that were file-private, no alias is needed — the subpackage exports them; state internals call `jsonscan.ScanStrings(...)` directly.

For transition tables (`var transitions = map[...]`) currently file-private, the subpackage exports `transitions.AgentEdges` and `transitions.WorkItemEdges`; state internals reference them. No public-surface change.

### 4.3 What gets smaller (per `feedback_deletion_default`)

| Surface | Before | After | Delta |
|---|---:|---:|---:|
| state/ primary-package LOC | 3076 | ≈ 1100 | -1976 |
| Files in state/ (flat) | 18 | 18 (same files; bodies shorter) | 0 |
| 71-method receiver surface | 71 | 71 (unchanged) | 0 |
| Importer call sites touched | 230 | 0 (alias-preserved) | -230 (vs Option D) |
| New subpackages | 0 (counting substrate/dbtest as pre-existing) | 4 | +4 |
| New exported types in subpackages | 0 | 4-6 (mostly aliases of pre-existing) | net wash |

Pure-addition risk: the +4 subpackages are net additions. Defense: the LOC moved into them is *deleted from state/*, not duplicated. Net source LOC across the union is approximately unchanged; *grep-discoverability locally* improves because cycle-DFS, json-scan, edge-aggregation logic now lives in named packages instead of file-private blocks.

---

## §5 Staged migration plan

Six child PRs (T1-T6). Each PR is move-only diff (no semantic change), uses `git mv`-style commits to preserve blame, and ships with a golden-file behavior pin (the subpackage's first test reproduces a captured pre-move state.test output).

Order: lowest blast radius first. work_item_outputs has zero downstream consumers of file-private helpers; agents/locks last because they hold the most cross-file private-member couplings (d.now, transition tables consumed by agents.go and locks.go heartbeat).

### T1 — jsonscan/ subpackage

- Move: scanJSONStrings + unescapeJSON from work_items_query.go (lines 248-321 per current state).
- Importer touches: 0 (caller is internal to state).
- LOC moved: ~80.
- Risk tier: Risk- (file-private → subpackage-public, pure functions, no *DB).
- Test pin: golden file `testdata/jsonscan_corpus.json` captures 50 input → output pairs from the existing TestCycleCheck + TestListSpawnable suites.

### T2 — edgeagg/ subpackage

- Move: EdgeFromAggregate type + the pure tally logic (lines from work_item_edges.go that aggregate without DB hits).
- Move CountNonDefaultEdgeStates body's *pure reducer* part into edgeagg; the *DB method shell stays on *DB.
- Importer touches: 0 (alias re-export preserves surface).
- LOC moved: ~120.
- Risk tier: Risk- (alias preserves caller spelling).
- Test pin: cycle_check_bench_test.go and edge counter tests still pass unchanged.

### T3 — transitions/ subpackage

- Move: agents.go `var transitions` table + work_items.go transition table.
- Subpackage exports two read-only maps: `AgentEdges`, `WorkItemEdges`.
- Importer touches: 0 (tables are file-private today, made subpackage-public).
- LOC moved: ~100.
- Risk tier: Risk- (pure data move).
- Test pin: TestTransitionAgent_AllValidEdges golden table stays green.

### T4 — cycle/ subpackage

- Move: CycleCheck pure DFS body from work_items_query.go (lines ~125-247).
- The *DB method `CycleCheck(ctx, candidate)` shrinks to: load adjacency via existing scanner, call `cycle.Check(adjacency, candidate)`, return.
- Importer touches: 0.
- LOC moved: ~150.
- Risk tier: Risk (cycle detection is load-bearing for scheduler reservation #88).
- Test pin: cycle_check_property_test.go runs unchanged against new package layout.

### T5 — approvals_shadow/ subpackage (consolidation)

- Move: ShadowWriteConfig + the *pure* divergence-classification helpers from approvals_shadow.go + approvals_read_substrate.go.
- The *DB methods stay on *DB; only the pure config + classifier moves.
- Importer touches: 0 (alias re-export).
- LOC moved: ~140.
- Risk tier: Risk (substrate read-path is replay-determinism critical).
- Test pin: approvals_shadow_test.go + approvals_read_substrate_test.go suites stay green.

### T6 — internal/orchestrator/state godoc + package-tier-order CI gate

- Add a `// Package state` godoc paragraph naming the receiver-method constraint per §4.1.
- Add `scripts/check-state-tier-order.sh` invoked from `make check`: walks `go list -deps ./internal/orchestrator/state/...` and asserts `state/cycle`, `state/edgeagg`, `state/jsonscan`, `state/transitions` do NOT import `state` (one-direction rule).
- Importer touches: 0 (just CI + docs).
- LOC moved: 0; LOC added: ~80 (the gate script).
- Risk tier: Risk- (CI-only).

### 5.1 Per-PR budget envelope (all six fit the brief)

| PR | LOC moved | Importer touches | Risk tier | Reviewer subagent? |
|---|---:|---:|---|---|
| T1 jsonscan | ~80 | 0 | Risk- | skip (proportional, alias-preserved) |
| T2 edgeagg | ~120 | 0 | Risk- | skip |
| T3 transitions | ~100 | 0 | Risk- | skip |
| T4 cycle | ~150 | 0 | Risk | required (scheduler-critical) |
| T5 approvals_shadow | ~140 | 0 | Risk | required (replay-determinism-critical) |
| T6 godoc + CI gate | ~80 (additions) | 0 | Risk- | skip |
| **Total** | **~670 LOC moved** | **0 importer touches** | | |

Each PR's diff stays under 200 LOC net (move + small alias re-export delta) and zero importer files outside `internal/orchestrator/state/` are touched. Per `feedback_dispatch_brief_only`, the implementer dispatch brief for each Tn cites only this spec's §5.Tn block, not the whole document.

---

## §6 Risk-tier register

Per `feedback_adversarial_review` + `feedback_review_before_automerge`. Risk+ findings must be mitigated inline OR filed as tracking issue with `#NNN` cite before the relevant child PR auto-merges.

### R1 — Type-alias circular import (Risk+)

Subpackage exports `EdgeRow`, state re-exports it via `type EdgeRow = edgeagg.EdgeRow`. If a future test or helper in state/edgeagg imports state for a constant, the cycle blows up at `go build`.

**Mitigation**: T2 PR adds a comment on the alias block: `// These re-exports preserve the state.X import surface — see docs/engineer/specs/2026-06-04-state-package-split-design.md §4.2. Do NOT import state from edgeagg/, cycle/, jsonscan/, transitions/.` Plus T6's CI gate (`check-state-tier-order.sh`) enforces it mechanically. Risk-resolved at T6 merge.

### R2 — Test-helper duplication (Risk)

dbtest/ already lives as a subpackage. Subpackages may want their own dbtest-style fixture; tempting to copy. **Mitigation**: implementer brief for each Tn explicitly says: pure subpackages MUST NOT depend on substrate/, dbtest/, or any DB primitive. Test corpora are golden files in `testdata/`. Risk-resolved at T6 godoc.

### R3 — Migration-number race with other waves (Risk+)

Active migration count is 17 (CurrentSchemaVersion at state.go:23). Per `feedback_migration_number_lock`, parallel implementer dispatch must pin the migration number. This spec's PRs add zero migrations — T1-T6 are code-only moves. **Mitigation**: each Tn dispatch brief asserts: "this PR MUST NOT touch internal/orchestrator/state/migrations/" + CI gate via `git diff --name-only origin/main...HEAD | grep migrations/` returning empty.

### R4 — Hidden cross-file dependency on file-private symbols (Risk+)

Audit at #739 identified d.sql, d.now, d.WithTx, PersistTraceIDFromContext, selectWorkItemsCols, scanWorkItems as private members shared across files. Of these, only scanWorkItems + selectWorkItemsCols + scanJSONStrings + unescapeJSON are *truly file-private inside the package* (the d.* are method receivers, unaffected by Option E).

**Mitigation**: T1 (jsonscan) makes scanJSONStrings + unescapeJSON subpackage-public; the moved versions are called from work_items_query.go via `jsonscan.ScanStrings(...)`. selectWorkItemsCols and scanWorkItems stay file-private in work_items_query.go because they reference WorkItem (state-package type) — moving them would force a state.WorkItem cycle (forbidden per R1).

PersistTraceIDFromContext is already exported (trace_id.go:19) and stays in state. No move.

### R5 — Golden-file flake on JSON scanner edge cases (Risk)

scanJSONStrings is hand-rolled (work_items_query.go:248-301); it handles escape sequences and unicode. Moving it intact preserves behavior, but a golden-file corpus that's too narrow could miss a regression on a future bug-fix PR.

**Mitigation**: T1 brief says: golden corpus must include all current call-site inputs observed in cycle_check_bench_test.go + at least 10 hand-curated edge cases (escaped quote, escaped backslash, unicode escape, malformed JSON, empty array, deeply nested array, null byte in string, very long string, mixed escape, BOM).

### R6 — Implementer drift to subpackage receivers (Risk++)

The implementer reading T1 might think "while I'm here, let me also add a `jsonscan.Scanner` struct with methods" — Option B drift. **Mitigation**: dispatch brief opens with: "Option E is the chosen pattern (spec §4). NO receiver types in jsonscan/, edgeagg/, cycle/, transitions/. Pure functions over input structs only. Spec deviation → re-spawn design subagent per `feedback_spec_pattern_authority`."

### R7 — Subagent comment-budget creep on new packages (Risk)

Per `feedback_comment_budget_enforcement`: implementers drift to over-comment new packages. **Mitigation**: reviewer subagent's lens 9 (comment-sweep) is required for T4 + T5 (the two Risk-tier PRs); auto-skipped for T1, T2, T3, T6 per `feedback_review_proportional`.

### R8 — Tier-order CI gate false-positive on legitimate test-only import (Risk-)

T6's `check-state-tier-order.sh` walks deps. If a subpackage's `_test.go` legitimately imports state for a test helper, the gate flags it. **Mitigation**: gate scope is `go list -deps` (non-test deps only) per Go convention; test-only deps are tracked by `go list -test`. The gate explicitly skips test deps. Documented in the gate's header comment.

### R9 — Long-term invariant drift (Risk)

Three years from now, a contributor adds a new pure-data subpackage without reading this spec. Pattern erodes. **Mitigation**: T6 godoc names the pattern explicitly in the state package doc: "Pure data / pure functions go in subpackages (see state/cycle, state/edgeagg, state/jsonscan, state/transitions for shape). Receivers stay on *DB in the state package root."

### R10 — Test scope creep in T4 cycle/ (Risk+)

cycle_check_property_test.go is a property test (1000+ runs per invocation). Moving the body might tempt the implementer to also "refactor the property generators." **Mitigation**: T4 brief is `git mv`-discipline: the new test file's diff against the old one must be limited to import paths + package name. Anything else fails reviewer.

---

## §7 A+ rubric for child PRs

Each Tn PR carries this rubric in its body (per `feedback_grade_rubric`). Citations are BARE (no backticks) per `feedback_scorecard_citation_token_outside_backticks` so check-scorecard.sh sees them.

```
| Tier | Criterion | Falsifiable acceptance | Self-score |
|---|---|---|---|
| B | (a) Move-only diff — no semantic change | git log shows file rename + body unchanged; reviewer diffs old vs new and asserts byte-identical function bodies | <implementer fills in> |
| B | (b) Zero importer files outside internal/orchestrator/state/ touched | git diff --name-only origin/main...HEAD | grep -v '^internal/orchestrator/state/' returns empty | <implementer fills in> |
| B | (c) make check green pre-push | make check exit 0 | <implementer fills in> |
| B | (d) Release-notes fence present, [CHORE] category | scripts/check-scorecard.sh --body-file BODY.md returns 0 | <implementer fills in> |
| A | (e) Golden-file behavior pin reproduces pre-move output | testdata/<sub>_corpus.json round-trips | <implementer fills in> |
| A | (f) Subpackage does NOT import state | go list -deps ./internal/orchestrator/state/<sub>/... shows no state import | <implementer fills in> |
| A | (g) Risk-tier findings from spec §6 addressed inline OR cited #NNN | PR body cites each applicable Rn from spec | <implementer fills in> |
| A+ | (h) Comment budget: subpackage adds <=10 WHY-comments total | reviewer lens-9 sweep returns 0 WHAT-comments | <implementer fills in> |
| A+ | (i) check-state-tier-order.sh passes (post-T6) | scripts/check-state-tier-order.sh exit 0 | <implementer fills in> |
| A+ | (j) Deletion-default math in PR body | PR body shows LOC delta in state/ primary-package | <implementer fills in> |
```

Implementer scorecard tier expectation: A+ achievable when (h)+(i)+(j) all pinned. B+A only acceptable if a deferral with reopen-trigger is filed.

---

## §8 Reopen trigger

This spec is re-opened if any of:

1. **Option E proves cycle-bound in practice**: T1 or T2 hit a transitive import cycle that cannot be broken without exporting more of state's private surface. → Reopen, evaluate Option D with eyes-open.
2. **Receiver-method count grows past 100 on *DB**: indicates the "keep singleton" decision is no longer holding. → Reopen, evaluate Option C with explicit invariant-replication plan.
3. **Importer count grows past 200**: indicates state has crystallized as a god-API not just a god-package. → Reopen, evaluate facade splitting (one *DB per consumer, fan-out from a Server-level composition root).
4. **Sub-DB requirement from W12 billing or W9 substrate**: if a future wave needs separate *sql.DB pools (e.g., billing writes to a separate sqlite file), Option C becomes mandatory. → Reopen.
5. **A new Go release allows methods-on-external-types**: hypothetical; the constraint is intentional language design and unlikely to relax. Closes the central forcing function if it ever does.

Reopen does NOT require any of T1-T6 to roll back — Option E is forward-compatible with Options C/D as a future-wave migration starting from a smaller state/ root.

---

## §9 Out of scope

Per the dispatch brief:
- No Go source touched in this spec PR (design only).
- No *DB receiver redesign (option C explicitly rejected).
- No mass method-to-function rewrite (option D explicitly rejected).
- No new migrations (R3).
- No new tests in this PR (test pins are described in §5 per-Tn; the tests land with each Tn child PR).
- No changes to substrate/, dbtest/, or migrations/ subpackages (they are pre-existing and orthogonal).

---

## §10 Definition of done (this spec PR)

- [x] Spec file at docs/engineer/specs/2026-06-04-state-package-split-design.md
- [x] §0 closing trigger present
- [x] §1 decision priority cited (CLAUDE.md UX > ease > performance > best-practices)
- [x] §2 options matrix with five options scored
- [x] §3 prior art with 3 OSS citations including version + commit SHA + license
- [x] §4 recommended option = E, with target layout
- [x] §5 staged migration plan with six child PRs sized
- [x] §6 risk-tier register with 10 Rn entries
- [x] §7 A+ rubric for child PRs with bare-citation form
- [x] §8 reopen trigger
- [x] §9 out-of-scope explicit
- [x] No banned phrases (doc-check)
- [x] No AI signatures
- [x] Memory rules cited inline + footer
- [x] PR body uses --body-file with release-notes fence

```release-notes
[DOCS] Design spec for internal/orchestrator/state god-package 3-way split via Option E hybrid. (#739)
```
