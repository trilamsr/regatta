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
4. `PRINCIPLES.md` + `STYLE.md` + `AGENTS.md` - how we work.
5. `docs/superpowers/specs/` - design specs for in-flight or recent
   structural changes.

## Tree map

```
regatta/
  cmd/regatta/        # single binary; thin wiring; subcommands
  contracts/          # operator-facing surface (versioned)
    schemas/          # JSON Schema + CUE + Go pkg `schemas`
    prompts/          # signed agent prompts (populated MVP-1+)
    wire/             # plugin wire-protocol docs (when first plugin lands)
  internal/           # private impl; default visibility
    gates/            # gate runners (L0 shipped; L1-L5 deferred;
      l0/  security/   # security/ is a custom gate, not in the numbered stack)
    config/           # regatta.yaml load + repo audit
      validate/  verify/
    orchestrator/     # daemon: watcher / scheduler / spawner / reaper / state / adapter
    program/          # MVP-1 program layer (planner + handoff + route)
  testdata/           # all fixture corpora (P6 single root)
    gates/{l0,canary,security}/  program/handoffs/
  docs/
    design.md  incidents.md
    superpowers/specs/   # design specs for restructures + features
    rfcs/  operator/  auditor/  engineer/  # persona-scoped surfaces
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
| New design spec | `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md` |
| Incident catalog | `docs/incidents.md` |
