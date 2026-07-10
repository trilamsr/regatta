# Architecture

Reader: internal engineer (first-week onboarding) or auditor under
NDA scanning for tree shape.
Read time: 2 minutes.
Expires when: the top-level layout (`cmd/`, `contracts/`,
`internal/`, `testdata/`) changes shape.

This file is the tree map + read order. The product architecture
itself lives in `docs/design.md`; do not read both as parallel
sources.

## Read order

1. `README.md` - pitch, who-it-is-for, who-it-is-NOT-for, shipped
   subcommands.
2. `docs/design.md` - full product design (orchestrator, gate
   stack L0-L6, program layer, threat model).
3. `docs/incidents.md` - AI-agent incident catalog driving the trap
   patterns.
4. `TICKETS.md` - repo-resident tracker (per-task state, milestones).
5. `docs/engineer/specs/` + `docs/engineer/briefs/` - active design
   specs and unshipped briefs; `docs/engineer/CHANGELOG.md` for
   shipped decisions.

## Tree map

```
regatta/
  cmd/regatta/        # single binary; thin wiring; subcommands
  contracts/          # operator-facing surface (versioned)
    schemas/          # JSON Schema + CUE + Go pkg `schemas`
    prompts/          # signed agent prompts
    wire/             # plugin wire-protocol docs
  internal/           # private impl; default visibility
    gates/            # gate runners
    config/           # regatta.yaml load + repo audit
      validate/  verify/
    orchestrator/     # daemon: watcher / scheduler / spawner / reaper / state / adapter
    program/          # program layer (planner + handoff + route)
  testdata/           # all fixture corpora (single root)
  docs/
    design.md  incidents.md  architecture.md  CHANGELOG-releases.md
    engineer/         # engineer-scoped surfaces + specs + briefs + CHANGELOG
    operator/         # operator-scoped surfaces
  scripts/            # repo tooling (doc-check, etc.)
  research/           # raw investigation; out of doc-check scope
  .githooks/  .github/  .claude/
```

## Where does X live?

| Concept | Location |
|---|---|
| Operator-facing schema | `contracts/schemas/` |
| Signed agent prompt | `contracts/prompts/` |
| Plugin wire protocol | `contracts/wire/` |
| Gate runner code | `internal/gates/<name>/` |
| Gate fixture corpus | `testdata/gates/<name>/` |
| Daemon component | `internal/orchestrator/<role>/` |
| Program-layer code | `internal/program/` |
| Program fixtures | `testdata/program/` |
| Config load + repo audit | `internal/config/{validate,verify}/` |
| New design spec | `docs/engineer/specs/YYYY-MM-DD-<topic>-design.md` |
| Shipped-spec history | `docs/engineer/CHANGELOG.md` |
| Incident catalog | `docs/incidents.md` |
