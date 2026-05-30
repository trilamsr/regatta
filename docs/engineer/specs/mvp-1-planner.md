# MVP-1: Planner-as-DAG

Reader: internal engineer (or AI agent) implementing MVP-1.
Read time: 10 minutes.
Status: ready to implement.
Expires when: MVP-1 ships and this becomes the post-mortem reference.

## Acceptance (verbatim from `docs/design.md` §Phase 1)

> One program produces >=3 child PRs through unmodified L0-L6.

Concretely: a markdown_catalog item with `kind: program` and >=3
acceptance criteria flows through `regatta program plan` -> signed
`program_brief.json` -> orchestrator brief loader -> >=3 child
agents spawn -> >=3 PRs open against the target repo. All gates
(L0 today, L1-L5 deferred to MVP-2+) run unmodified.

## Scope

~600 LoC budget. The pieces:

1. **`internal/program/brief_loader.go`** -- polls
   `<repo>/.regatta/programs/*.json`, validates each via
   `programs.LoadAndValidate`, and upserts child WorkItem rows.
2. **`internal/orchestrator/state` schema migration** -- new
   columns on `work_items`: `kind`, `parent_program_id`,
   `depends_on_features` (JSON array). Migration in
   `internal/orchestrator/state/migrations/`.
3. **`internal/orchestrator/state.UpsertChild`** + accompanying
   `ChildFeaturesReady(parentProgramID) []WorkItem` query for
   the scheduler's DAG-respecting spawn order.
4. **`internal/program/planner.go` prompt loading** -- load
   `contracts/prompts/planner.md` from disk, verify SHA against
   `regatta.yaml`'s pinned value (when set), fall back to the
   embedded `defaultPlannerPrompt` constant when neither is
   present.
5. **`cmd/regatta/main.go` runProgramPlan output path** -- write
   the signed brief to `<repo>/.regatta/programs/<program_id>.json`
   instead of stdout when `--write` is passed. Stdout remains the
   default for stdout-piping workflows.
6. **`internal/orchestrator/orchestrator.go` PollOnce** -- call
   `BriefLoader.Sync(ctx)` before `adapter.List` each tick so the
   work_items table includes brief-derived rows.
7. **Tests** -- one end-to-end fixture under `testdata/program/`
   that pins the acceptance criterion: a 3-criterion program parent
   produces 3 child WorkItem rows that all reach `running`.

## Out of scope for MVP-1 (deferred to MVP-2)

- Handoff signature verification by the orchestrator. MVP-1
  workers write handoffs; the orchestrator does not yet read them
  to drive transitions (`RouteVerdicts` exists but is not wired).
- Re-run mismatch enforcement on `commands_run`.
- `program_id` injection in the spawner prompt. Today the spawner
  prompt template is one line (`spawner/claude.go:defaultPromptBuilder`);
  extending it lives with the rest of the prompt-as-code work in MVP-2.
- Per-criterion citation round-trip (`adapter/parse.go` TODO).

## Binding surface decision

`docs/rfcs/0001-mvp-1-program-publish.md`: brief reaches the
orchestrator via sqlite, not via SpecAdapter. Read this RFC
before reading the implementation files.

## Pre-flight state (landed in this PR)

- `WorkItem.kind` field added to `contracts/schemas/spec_adapter.go`
  + `work_item.schema.json`. Enum: `feature` (default), `program`.
- `markdown_catalog` adapter parses `kind:` frontmatter.
- `regatta program plan` guards `kind == program` at entry.
- `contracts/prompts/planner.md` extracted from embedded constant.

## Known weaknesses to NOT bridge in MVP-1

- **Trap-pattern check loose** (`handoff.go:184-205`): every
  claimed pattern is satisfied by any single non-inconclusive
  falsification. Schema agrees. Tightening requires schema bump +
  per-falsification `addresses_patterns: []string` field. Deferred
  to MVP-2 when handoff verification actually drives transitions.
- **`route.RouteVerdicts` unreachable from `serve`**: by design.
  MVP-1 stops at "child agents reach `running`"; verdict-driven
  transitions are MVP-2.

## Test recipe

End-to-end smoke (becomes the MVP-1 acceptance test):

```sh
# Set up a target repo
mkdir -p /tmp/regatta-mvp1-target/.regatta/items
cd /tmp/regatta-mvp1-target
git init -q && git commit --allow-empty -q -m init

cat > .regatta/items/PROG-1.md <<'EOF'
---
id: PROG-1
kind: program
title: smoke test program
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: add a foo function
- [planned] c2: add a bar function
- [planned] c3: add a baz function
EOF

# Plan
regatta program plan \
  --hmac-key-env=HMAC_KEY \
  --write \
  .regatta/items/PROG-1.md

# Serve one tick; expect 3 agents
regatta serve --spawner=claude --tick-once --repo .
sqlite3 .regatta/state.db 'SELECT count(*) FROM work_items WHERE parent_program_id IS NOT NULL'
# Expected output: 3
```

## File-by-file checklist for the implementer

| File | Change | Tests |
|---|---|---|
| `internal/program/brief_loader.go` | NEW: directory poll + validate + upsert | unit + fixture |
| `internal/orchestrator/state/migrations/0002_work_items_program.sql` | NEW: alter table | migration smoke |
| `internal/orchestrator/state/state.go` | NEW: `UpsertChild`, `ChildFeaturesReady` | unit |
| `internal/program/planner.go` | LoadPlannerPrompt(path, expectedSHA); fall back to embedded | unit (3 paths: disk, sha-pin, fallback) |
| `cmd/regatta/main.go:runProgramPlan` | `--write` flag; output path; SHA-pin read from regatta.yaml | unit |
| `internal/orchestrator/orchestrator.go:PollOnce` | call BriefLoader.Sync before adapter.List | adversarial: brief disappears mid-poll |
| `testdata/program/` | NEW: end-to-end fixture | acceptance smoke |
| `docs/operator/configure.md` | document `prompts.planner_sha` config field | n/a |

## Definition of done

1. End-to-end test recipe above passes.
2. `make ci-check` exit 0.
3. `go test -race ./...` clean.
4. CHANGELOG.md flipped to next dated section with MVP-1 entry.
5. Tagged v0.1.0 via release.yml.
