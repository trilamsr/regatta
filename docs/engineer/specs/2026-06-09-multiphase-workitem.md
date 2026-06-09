---
title: "Multi-phase WorkItem (#1083)"
status: design
phase: self-host-s2
issue: 1083
summary: "`schemas.WorkItem` is single-phase: one acceptance set per dispatch. Real roadmap items (e.g. #832 — Phase A R6+R7+R8, Phase B R9+R10+R11, Phase C Autotuner) are inherently multi-phase, and today the operator either splits them by hand into sibling issues or accepts a single conflated PR (cascade-rebase + scope-creep guaranteed). Proposal: model each phase as its own sub-WorkItem (`<parent>/<phase>`) linked through the existing `work_items.parent_program_id` column. `parseIssueBody` learns to split on `## Phase <name>` blocks; the scheduler projects each phase as an independent candidate; lane cap, preconditions, and PR-title suffix apply per phase. No new schema columns. Single-phase issues continue to round-trip unchanged."
date: 2026-06-09
---

# Multi-phase WorkItem (#1083) — Spec

Memory rules in force: `feedback_default_simpler`, `feedback_no_signatures`, `feedback_cite_origin_main_not_local`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_tdd_discipline`.

```release-notes
[DOCS] specs: multi-phase WorkItem design (#1083)
```

## §1 Problem

`schemas.WorkItem` (`contracts/schemas/spec_adapter.go`) carries exactly one `AcceptanceCriteria []Criterion` and one `Status`. Per `git show origin/main:contracts/schemas/spec_adapter.go | sed -n '37,50p'` (head `ca71046d`):

```
39: type WorkItem struct {
40: 	ID                 WorkItemID   `json:"id"`
41: 	Kind               WorkItemKind `json:"kind,omitempty"` // "feature" (default) | "program" routes through planner
42: 	Title              string       `json:"title"`
43: 	Body               string       `json:"body,omitempty"`
44: 	AcceptanceCriteria []Criterion  `json:"acceptance_criteria"`
45: 	Dependencies       []WorkItemID `json:"dependencies,omitempty"` // topological order; cycles MUST surface as ErrDependencyCycle on List
46: 	Lane               LaneID       `json:"lane,omitempty"`         // empty = default lane
47: 	Status             Status       `json:"status"`
```

Many real roadmap items are multi-phase by design. Concrete trigger: `gh issue view 832 --json title,body` returns the operator-authored "Dispatch sequence" block:

```
1. Phase A (after MVR-1-T4 ships + ≥30 autonomous PRs merged): R6 latency-outlier + R7 cost-outlier + R8 rework-cycle. 1 wk impl.
2. Phase B (after R6-R8 fire 10+ times): R9 success-pattern + R10 priority-thrash + R11 cap-thrash. 2 wk impl.
3. Phase C (after Phase B): Autotuner — closed-loop write-back to regatta.yaml + dispatch templates.
```

`parseIssueBody` (`git show origin/main:internal/orchestrator/adapter/githubissues/parse.go | sed -n '56,60p'`) lifts exactly one `## Acceptance criteria` section per body — no notion of phase exists in the adapter projection, in the schema, or in the scheduler.

End-to-end consequence for #832 and every issue that follows the same shape:

- Path A — operator splits by hand into sibling issues (`#832-A`, `#832-B`, `#832-C`) and carries the precondition graph in prose. Brittle. The Phase B "depends on R6-R8 firing 10+ times" precondition has nowhere mechanical to live.
- Path B — operator labels #832 `autonomous`. Scheduler dispatches it as one work item. Implementer builds A+B+C in one branch; PR becomes huge; cascade-rebase per `feedback_cascade_rebase_root_cause`; reviewer cannot scope the adversarial pass; A+ rubric defaults to B-tier.

The root cause is the schema, not the operator workflow. Per `feedback_root_cause`, fix at the contract surface and let the adapter / scheduler / PR-title layer ride.

## §2 Design

**Model each phase as its own sub-WorkItem, linked to the parent via the existing `work_items.parent_program_id` column. No new schema columns; no new edge tables.**

### §2.1 Schema delta

`contracts/schemas/spec_adapter.go::WorkItem` gains a single optional field:

```go
Phases []Phase `json:"phases,omitempty"`
```

where:

```go
type Phase struct {
    Name               string       `json:"name"`                // "A", "B", "C", "research", "design", ... — bytes from the `## Phase <name>` heading
    AcceptanceCriteria []Criterion  `json:"acceptance_criteria"`
    Dependencies       []WorkItemID `json:"dependencies,omitempty"` // sub-WorkItem IDs the phase blocks on; resolved per §2.3
}
```

Backwards-compatible rule: when `Phases` is empty, the top-level `AcceptanceCriteria` is canonical and behavior is byte-equal pre/post (the single-phase path). When `Phases` is non-empty, the top-level `AcceptanceCriteria` is **ignored at dispatch time** (per acceptance c2 in #1083) — the adapter MAY still populate it as a flat union for diagnostics, but the scheduler MUST NOT project it.

### §2.2 Persistence

The `work_items` table already carries `parent_program_id TEXT` per `git show origin/main:internal/orchestrator/state/migrations/0002_work_items.sql`:

```
12:    parent_program_id    TEXT,
...
22: CREATE INDEX idx_work_items_parent ON work_items(parent_program_id);
```

That column was sized for the program → feature relationship (`KindProgram`) and already has the `idx_work_items_parent` index. We reuse it: each phase becomes a row in `work_items` with `id = <parent_id>/<phase>` and `parent_program_id = <parent_id>`. The parent row itself carries `Kind = KindProgram` (existing enum, `git show origin/main:contracts/schemas/spec_adapter.go | sed -n '55,60p'`) and an empty `AcceptanceCriteria` after fan-out.

**Impedance mismatch — `schemas.WorkItem` has no `ParentProgramID` field**: `git show origin/main:contracts/schemas/spec_adapter.go | grep -n ParentProgramID` returns empty. The schema contract is INTENTIONALLY parent-link-free; parent linkage lives only in the persistence layer (`internal/orchestrator/state/work_items.go::WorkItem` has the `ParentProgramID` column; the schema contract does not). The adapter is the bridge: `adaptersync.Syncer.Sync` projects each fan-out row through `state.UpsertWorkItem` directly, passing `ParentProgramID` as a sibling argument, NOT via the schema. The schema's job is "what the issue body said"; the state package's job is "how the orchestrator stitches it". The implementer MUST NOT add `ParentProgramID` to `schemas.WorkItem` — that would couple the contract surface to internal persistence and break the schema-vs-state separation that #1083 c4 explicitly preserves.

No new migration. No new column. No new edge table. Per `feedback_default_simpler` and `feedback_deletion_default`, this is the smallest viable shape; the persistence struct in `git show origin/main:internal/orchestrator/state/work_items.go | sed -n '56,70p'` needs zero new fields.

### §2.3 Adapter projection

`parseIssueBody` (`internal/orchestrator/adapter/githubissues/parse.go`) learns one new rule:

- If the body contains one or more `## Phase <name>` headings, treat each `## Phase <name>` block as a sub-projection. Inside each block, the existing `## Acceptance criteria` regex (`criteriaHeadRE`, line 41) applies; the body between the `## Phase` heading and the next `## Phase` (or EOF) is the phase scope.
- Optional `## Preconditions` subsection per phase, format identical to the existing `## Acceptance criteria` bullet shape, except the bullet text is interpreted as a `WorkItemID` reference (e.g. `- depends_on: #832/A`). Cross-issue references (`#NNN/X`) are preserved verbatim; the scheduler resolves them in §2.4.
- If at least one `## Phase` heading is present, the top-level `## Acceptance criteria` (if any) is treated as documentation; it is NOT projected onto the `Phases[0]` row. This is the inverse of c2 in #1083 and matches the "phases override flat acceptance" rule above.
- If no `## Phase` heading exists, the parser path is byte-equal pre/post — single-phase issues round-trip unchanged.

Adapter `ListReady` / `Get` fan out the projection: the parent `schemas.WorkItem` carries `Kind = KindProgram` and `Phases` populated; the adapter's projection layer (`internal/orchestrator/adapter/githubissues/adapter.go` lines 146-170 and 244-260) walks `Phases` and synthesizes one child `schemas.WorkItem` per phase with:

- `ID = <parent>/<phase>`
- `Kind = KindFeature`
- `Title = <parent.Title> — Phase <name>`
- `Body = <parent.Body>` (the full issue body; the implementer relies on top-level context)
- `AcceptanceCriteria = phase.AcceptanceCriteria`
- `Dependencies = phase.Dependencies` (resolved via §2.4)
- `Status = StatusPlanned`
- `Source = <parent.Source>` (unchanged — same issue, same SHA)

`adaptersync.Syncer.Sync` then calls `state.UpsertWorkItem` once per child row plus once per parent. The parent row's `parent_program_id` is empty (it is the parent); the child rows carry `parent_program_id = <parent_id>`.

**PR-title generation seam**: `internal/orchestrator/spawner/claude.go::defaultPromptBuilder` (lines 266-267 on `origin/main`) emits `req.WorkItemID` in the prompt prose but does NOT itself build the PR title; the claude child agent writes the title at `gh pr create` time per its own dispatch template. The implementer MUST extend the dispatch template (`docs/engineer/dispatch-templates/implementer.md`) with a §X "Phase-suffixed PR titles" rule: when `req.WorkItemID` contains `/`, the agent suffixes the PR title with ` — Phase <name>` (e.g. `[FEAT] BUG-832 — Phase research:`). The seam is the dispatch template, NOT the spawner code; the spawner stays role-agnostic. Reviewer flagged this as missing — added here.

### §2.4 Scheduler projection

`internal/orchestrator/scheduler/scheduler.go` already iterates `state.ListSpawnable`; each row is a candidate. Sub-phase rows behave like any other row:

- `id = <parent>/<phase>` is the dispatch identifier.
- Lane cap counts each phase against its lane (phases of one parent MAY be in the same lane; the lane cap throttles them naturally).
- `depends_on_features` already exists on `state.WorkItem` (`git show origin/main:internal/orchestrator/state/work_items.go | sed -n '57,70p'`). Phase B's `depends_on = ["#832/A"]` rides this column — no new edge mechanism.
- Precondition check (the join in `state.ListSpawnable` against `depends_on_features` not-yet-`merged`) gates Phase B until Phase A's row reaches `WorkStatusMerged`.

PR titles get a phase suffix from the dispatch path:

- The spawner prompt builder (`internal/orchestrator/spawner/claude.go::defaultPromptBuilder`) already injects work-item id. When `id` contains `/`, the PR title template adds the phase suffix: `[FEAT] ... (closes #832 Phase A)` — the phase token is the substring after the last `/` in the `id`.
- The branch name retains the orchestrator-pinned `regatta/agent-<N>` shape (per `feedback_keep_orchestrator_branch_name`); the semantic phase belongs in the title, not the branch.

### §2.5 What "research → design → implementation → verification" looks like

#1083 asks the spec to model `research → design → implementation → verification` as the canonical phase sequence. Concretely:

- `## Phase research` — sub-WorkItem `<parent>/research`. Acceptance: prior-art audit + OSS-survey + adversarial-input survey land at a brief path. Dispatch dispatches a research-mode subagent.
- `## Phase design` — sub-WorkItem `<parent>/design`. Acceptance: spec lands under `docs/engineer/specs/` with the 7 H2 sections. Depends on `research`. Dispatch dispatches a designer subagent.
- `## Phase implementation` — sub-WorkItem `<parent>/implementation`. Acceptance: spec criteria satisfied; tests TDD-ordered; PR merged. Depends on `design`. Dispatch dispatches an implementer.
- `## Phase verification` — sub-WorkItem `<parent>/verification`. Acceptance: live-validation observation window (per `feedback_validate_before_ship`) — e.g. soak smoke for ≥30 min, regression count zero. Depends on `implementation`.

The phase names are not enum-pinned in the schema — `Phase.Name` is free-text bytes from the heading. The four-phase template is a convention the operator (or a roadmap-discovery brief) writes into the issue body; the schema is agnostic. This preserves `feedback_default_simpler`: no closed enum, no hypothetical-drift abstraction. If 80% of multi-phase issues converge on the four-phase template, codify later.

**Phase-name character set is validated, not free-anything**: `parseIssueBody` enforces `Phase.Name` matches the regex `^[a-z0-9][a-z0-9_-]*$` (same shape as the existing `id_prefix` convention, e.g. `RESEARCH-DELTA-001`). Specifically: a phase name MUST start with a letter or digit, MAY contain lowercase letters, digits, underscore, or hyphen, MUST NOT contain `/` (the parent/child separator), MUST NOT contain whitespace, MUST NOT contain `:` (collides with markdown reference-link syntax), and MUST NOT be empty. Headings that violate produce a parser error (the work item fails to project, the operator sees `parse.invalid_phase_name` WARN with the offending heading echoed). The constraint protects the PR-title-suffix path (no shell-escaping needed), the `<parent>/<phase>` id grammar (no embedded slashes), and the substrate event payload (no JSON-key ambiguity).

### §2.6 Backwards compatibility + byte-equal pin

- Single-phase issues (no `## Phase` heading) round-trip unchanged: `Phases` empty, top-level `AcceptanceCriteria` populated, scheduler dispatches one row per work item.
- Pre-existing multi-phase issues (like #832 today) only get fanned out after the operator edits the body to add `## Phase <name>` headings. Until then they project as single-phase (one acceptance set, one dispatch).
- Per the byte-equal-refactor-pin convention: the single-phase path is the byte-equal pre/post claim. The implementing PR MUST ship a regression-fixture test (table-driven `parseIssueBody` cases) covering specifically these 3 real single-phase issues that are pinned in `internal/orchestrator/adapter/githubissues/parse_test.go` as of `origin/main` `f68d35e`: (a) the `TestParseIssueBody_AcceptanceCriteria_OneCheckbox` fixture, (b) the `TestParseIssueBody_AcceptanceCriteria_MultilineCheckboxes` fixture, and (c) the `TestParseIssueBody_NoAcceptanceCriteriaHeading` fixture. The test asserts `bytes.Equal(serialize(parse(body, preChange)), serialize(parse(body, postChange)))` for each. The PR body MUST include the full `go test -run ...` output showing pre/post outputs identical.

**Note on JSON marshaling**: `Phases []Phase \`json:"phases,omitempty"\`` is `omitempty`, but `AcceptanceCriteria []Criterion` on `schemas.WorkItem` is currently `json:"acceptance_criteria"` (no omitempty per `contracts/schemas/spec_adapter.go` line 44). An empty `[]Criterion` marshals as `"acceptance_criteria": []` regardless. The byte-equal claim holds at the *parser-projection* level (the `Phases` field stays nil → omitted), but at the *full JSON document* level there is no change either, because the pre-change marshaling ALSO emitted `"acceptance_criteria": []` for issues with no checkboxes. The implementer's regression-fixture test must compare full marshaled JSON, not just the parsed-struct fields, to catch any silent ordering drift.

### §2.7 Why not new schema

`work_item_edges` (`git show origin/main:internal/orchestrator/state/work_item_edges.go | head -20`) already models cross-WorkItem edges with named relationships. A naive proposal would add `kind=phase_of` rows to `work_item_edges` and leave `parent_program_id` for programs. Rejected per `feedback_default_simpler`:

- Two competing parent linkages (column + edge row) for the same semantic ("this WorkItem belongs to that parent") splits reads and writes.
- `idx_work_items_parent` is already indexed; phase rollups (e.g. "all phases of #832") run as one indexed SELECT today, no join needed.
- `Kind = KindProgram` cleanly distinguishes "this row dispatches" (KindFeature children) from "this row routes through fan-out" (KindProgram parent), reusing the existing planner-routing semantics.

Per `feedback_deletion_default` the design adds one schema field (`Phases []Phase`) on the contract and zero columns on persistence. Subtraction: the implicit single-issue-equals-single-dispatch coupling goes away.

## §3 Acceptance

1. `contracts/schemas/spec_adapter.go::WorkItem` gains `Phases []Phase` (optional, `json:"phases,omitempty"`); zero-value preserves byte-equal JSON for single-phase items. Round-trip test in `contracts/schemas/spec_adapter_test.go` covers both empty-Phases and populated-Phases cases.
2. `internal/orchestrator/adapter/githubissues/parse.go::parseIssueBody` extracts one sub-projection per `## Phase <name>` heading, each carrying its own `## Acceptance criteria` (and optional `## Preconditions`) subsection. When no `## Phase` heading exists, the projection is byte-equal pre/post (regression-fixture test ≥3 existing single-phase bodies).
3. `internal/orchestrator/adapter/githubissues/adapter.go` fans the parent `schemas.WorkItem` out into one parent row (`Kind = KindProgram`, empty `AcceptanceCriteria`) plus N child rows (`Kind = KindFeature`, `id = <parent>/<phase>`, `parent_program_id = <parent>`).
4. `internal/orchestrator/scheduler/scheduler.go` projects each child phase row as an independent dispatch candidate. Lane cap applies per child. Phase B with `depends_on = ["#832/A"]` only becomes spawnable after `#832/A` reaches `WorkStatusMerged` (covered by `scheduler_phases_test.go`).
5. PR-title template suffixes the phase name when the dispatched `id` contains `/`. Single-phase items keep the existing title shape (byte-equal).
6. New regression test `internal/orchestrator/scheduler/scheduler_phases_test.go` covers: (a) single-phase issue → one candidate, unchanged behavior; (b) 3-phase issue with phase A unconditional + phase B depends_on `#832/A` → only A spawns first; (c) phase A reaches merged → B becomes eligible on the next tick.
7. New parser test `internal/orchestrator/adapter/githubissues/parse_phases_test.go` covers: (a) 3-phase issue → 3 sub-projections; (b) malformed `## Phase` block (no acceptance subsection) → SkipReason `bad_phase_section` (new); (c) `## Phase` heading at the wrong nesting level (### / #) is NOT projected as a phase.
8. `make check` and `make ci-check` pass.
9. Red-then-green commit order per `feedback_tdd_discipline`: the RED tests for items 2, 6, 7 land first; impl green after.

## §4 Out of scope

- Closed enum on `Phase.Name`. Per `feedback_default_simpler`, the four-phase research/design/implementation/verification template is operator convention, not a schema-enforced enum. Reopen if 80% of multi-phase issues converge on the four template AND a reviewer-finding demands enum-time validation.
- Multi-phase support in non-`github_issues` adapters (`markdown_catalog`, `gitlab_issues`, `linear`, `custom`). Schema is adapter-agnostic; per-adapter parser extension lands case-by-case. Track separately if/when added.
- Cross-issue phase dependencies beyond the existing `depends_on_features` mechanism. The scheduler precondition path is already present; phases ride it.
- Automatic fan-out for issues that ALREADY exist on `main` with operator-authored sibling issues (`#832-A`, `#832-B`). Operator may re-consolidate manually; no migration path.
- Dispatch-template differentiation by phase name. Implementer subagent for `## Phase research` could route through `docs/engineer/dispatch-templates/designer.md` instead of `implementer.md`; defer to follow-up `#NNN` if/when the four-phase template is empirically dominant.
- UI surface (dashboard) for phase rollups. Persistence query is one indexed SELECT — UI can ride on top later.
- Phase-level cost cap (per-phase token budget). Cost gate operates at the work-item-id level today; phase rows ride that path unchanged.

## §5 Adversarial pass

Independent reviewer (cavecrew-reviewer or equivalent) MUST be spawned in a fresh slot BEFORE the implementer PR's `Reviewer-recommendation: APPROVE` token lands, per `feedback_no_self_tagged_approve` and `feedback_adversarial_review_every_step`. Likely findings the reviewer should hunt:

- **Byte-equal claim**: the single-phase round-trip MUST be mechanically validated, not asserted in prose. Per the byte-equal-refactor-pin convention, the implementing PR ships `parse_byte_equal_test.go` or extends the existing parse_test.go with explicit pre/post fixtures from ≥3 representative single-phase issues currently on `main`. Operator escape `<!-- byte-equal-justified: <reason> -->` is NOT permitted here — the byte-equal claim is the contract.
- **Phase-id collision**: `<parent>/<phase>` is a sub-id; what if a phase name is empty / contains `/` / collides with a sibling? Parser MUST reject empty phase names and embedded `/` via `SkipReason = bad_phase_section`. Test the failure mode.
- **Reopen trigger drift**: `## Reopen trigger` per phase is sketched in §2.3 but not specified. Defer to follow-up — phase-level reopen triggers are not load-bearing for #1083 acceptance c1-c4. The implementer MUST NOT inline a half-feature; file a follow-up issue if reviewer escalates.
- **Cascade-soft semantics**: when the parent program is archived but child phases are in-flight, the existing cascade-soft rule (`state/work_items.go` package godoc lines 1-8: "archived parents do not kill in-flight child agents") covers this. Reviewer should grep `work_items.go` package doc to confirm.
- **Adapter projection determinism**: per `internal/orchestrator/adapter/githubissues/adapter.go` package godoc ("deterministic projection, LLM inference forbidden, spec §6.2"), the phase-splitting MUST be regex-based, not LLM-inferred. Cite the rule in the implementer brief.
- **Verdict expected**: APPROVE iff items above are addressed in the PR diff or filed as follow-ups. REVISE on any unaddressed HIGH. Tracking-issue pattern per `feedback_reviewer_findings_to_issues`.

Self-included adversarial sections do NOT satisfy the gate (per `feedback_adversarial_review_every_step`). The reviewer spawn happens at implementer-PR time, not at spec-PR time; this spec's own PR is docs-only and qualifies for `[DOCS]` release-notes — but the reviewer-verdict gate still applies because `docs/engineer/specs/*.md` is on the load-bearing-doc surface.

## §6 Implementer brief

Per `feedback_dispatch_brief_only` — paste-ready scope for the implementer subagent. Reference this spec for cross-cutting Qs; do NOT re-dump full spec text into the dispatch prompt.

```
Scope: Add Phases []Phase to schemas.WorkItem (optional, byte-equal pre/post when nil).
       Teach parseIssueBody to extract `## Phase <name>` sub-projections.
       Fan out adapter projection into 1 parent (KindProgram) + N child (KindFeature) rows
       linked via existing work_items.parent_program_id column.
       Teach scheduler to project each child phase row as an independent candidate; lane cap +
       depends_on_features apply per phase. PR-title suffix `Phase <name>` when id contains `/`.
       NO new schema columns, NO new edge table — reuse parent_program_id + idx_work_items_parent.

Files:
  - contracts/schemas/spec_adapter.go         (+Phase type, +Phases field)
  - contracts/schemas/spec_adapter_test.go    (round-trip JSON)
  - internal/orchestrator/adapter/githubissues/parse.go (phase-block regex + per-phase acceptance lift)
  - internal/orchestrator/adapter/githubissues/parse_phases_test.go (NEW — RED first)
  - internal/orchestrator/adapter/githubissues/parse_byte_equal_test.go (NEW — single-phase byte-equal pin)
  - internal/orchestrator/adapter/githubissues/adapter.go (fan-out in ListReady + Get)
  - internal/orchestrator/scheduler/scheduler_phases_test.go (NEW — RED first; covers c4 of #1083)
  - cmd/regatta/wire_pr_title.go (or wherever PR-title template lives — verify at impl time;
    grep origin/main for `closes #` template literal)

TDD order:
  1) Land RED: parse_phases_test.go + parse_byte_equal_test.go + scheduler_phases_test.go.
  2) Implement schemas.Phase + Phases field.
  3) Implement parseIssueBody phase-block extraction.
  4) Implement adapter fan-out.
  5) Implement scheduler per-phase projection.
  6) Implement PR-title suffix.
  7) Green commit; capture RED output of each test in PR body per feedback_tdd_discipline.

make ci-check exit: 0
Reviewer dispatch: YES (load-bearing — schema contract + adapter contract + scheduler hot path).
  Spawn cavecrew-reviewer (or equivalent) in a fresh slot BEFORE writing
  `Reviewer-recommendation: APPROVE` per feedback_no_self_tagged_approve.
  PR body footer MUST carry Reviewer-agent-id + Reviewer-recommendation: APPROVE
  (bare, not in a code block) per check-reviewer-verdict.sh.

NO new schema migration. NO new SQL column. NO new edge-table row kind.
Reuse work_items.parent_program_id + idx_work_items_parent.

NO automerge (per feedback_no_implementer_automerge).
End with `gh pr ready <N>` + operator-merge handoff.

Branch: keep orchestrator-pinned regatta/agent-<N> per feedback_keep_orchestrator_branch_name.
PR title: `[FEAT] schema: multi-phase WorkItem (closes #1083)`.
Release-notes fence: `[FEAT] orchestrator: multi-phase WorkItem dispatch (closes #1083)`.
```

## §7 Reopen trigger

Reopen this spec when ANY of:

- A non-`github_issues` adapter (markdown_catalog, gitlab_issues, linear, custom) lands multi-phase support and the schema needs per-adapter discriminators.
- The four-phase research/design/implementation/verification template empirically dominates ≥80% of multi-phase issues (count via `gh issue list --label autonomous --search "## Phase" -L 100`) AND a reviewer-finding demands enum-time validation on `Phase.Name`.
- Phase-level cost cap or dispatch-template-per-phase becomes load-bearing for an external customer ask (Phase X gate, per `docs/engineer/briefs/2026-06-01-self-host-first.md`).
- The byte-equal single-phase round-trip regression-fixture begins flaking (would indicate parser drift; treat as `feedback_double_fail_root_cause`).
- Operator workflow consistently bypasses fan-out by re-introducing sibling sub-issues (`#832-A`, `#832-B`, ...) — signals fan-out UX is worse than manual split.
