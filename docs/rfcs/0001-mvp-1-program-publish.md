# 0001. MVP-1 program publish: planner output reaches the orchestrator via sqlite, not via SpecAdapter

Status: accepted
Date: 2026-05-30
Author: Tri Lam <tri@maydow.com>

## Context

MVP-1 (per `docs/design.md` §Phase 1) requires that a parent
`WorkItem{kind: program}` flows through `regatta program plan` into a
signed `program_brief.json`, and that the orchestrator subsequently
spawns one agent per child feature listed in the brief.

The orchestrator polls work via the `SpecAdapter` interface
(`contracts/schemas/spec_adapter.go`). That interface today has
`List`, `Get`, `UpdateStatus`, and `Capabilities` -- there is no
`Create`. So a brief produced by the planner cannot enter the
spawn queue through the adapter without a new contract method.

The decision triggers from spec D5 fire here: this is a
schema-version-adjacent change (new field already added in this
PR: `WorkItem.kind`) and a new internal contract surface
(child-work-item creation path).

## Decision

Child work items reach the orchestrator via the **sqlite state
store**, not via the spec adapter. Specifically:

1. `regatta program plan` writes the signed `program_brief.json`
   to disk (today's behavior; unchanged).
2. The orchestrator gains a `BriefLoader` that polls
   `<repo>/.regatta/programs/*.json`, validates each brief through
   `programs.LoadAndValidate`, and upserts each `feature` as a
   child `WorkItem` row in `state.work_items` with
   `parent_program_id` and `depends_on_features` populated.
3. The Scheduler treats child work items like any other work item
   for spawn purposes; the only difference is that
   `parent_program_id IS NOT NULL` rows have their
   `acceptance_criteria` byte-equal to the brief's
   `parent_criteria` subset that maps via `features[].fulfills`.

Alternatives considered + rejected:

- **Add `Create(WorkItem) error` to `SpecAdapter`**: every adapter
  would need a write surface. GitHub Issues, Jira, Linear all
  support issue creation, but the failure modes (rate limits,
  partial creation under network partition) leak into the
  orchestrator hot path. Rejected -- spawn-loop velocity matters
  more than spec-source uniformity for child features.

- **Write children back to markdown_catalog source files**: forces
  the markdown_catalog adapter to mutate operator-owned files mid-
  run; breaks the read-only-pre-publish invariant. Rejected.

- **Skip persistence; route brief through the spawner directly**:
  no recovery across restart; design.md §State requires durable
  parentage. Rejected.

## Consequences

- (+) Adapter contract stays read-only for the spec source.
  Operators never see Regatta-authored work items show up in their
  issue tracker.
- (+) Crash recovery is uniform: every spawnable unit lives in
  `state.work_items`; `Recover` already handles the orphan case.
- (+) The planner remains spec-adapter-agnostic; works against
  markdown_catalog today, GitHub Issues later, with no per-adapter
  branching.
- (-) `state.work_items` needs new columns: `parent_program_id`,
  `kind`, `depends_on_features` (JSON array). Migration lands with
  MVP-1.
- (-) The auditor surface gains a new read path:
  `.regatta/programs/*.json` is now a load-bearing artifact and
  must appear in `regatta verify-repo-config` checks for
  CODEOWNERS coverage. Activation trigger for a follow-up RFC if
  audit needs deepen.

## Compliance

- New contract test in `internal/program/`: round-trip a
  `program_brief.json` through `BriefLoader` + state.UpsertChild,
  assert that the resulting `state.work_items` rows pass
  `programs.CoverageCheck` against the parent.
- `docs/engineer/mvp-1-spec.md` lists this RFC as the binding
  surface decision; spec changes that contradict this require
  superseding RFC.
- `regatta verify-repo-config` is extended (in MVP-1) to fail
  closed if `.regatta/programs/` exists but is not covered by
  CODEOWNERS.
