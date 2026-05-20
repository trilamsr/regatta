**Status: Draft design, pre-implementation (v3.1). Schemas and fixture-corpus contracts are normative; the binary follows.**

# Regatta

A repo-agnostic autonomous-agent fleet. Point it at any git-hosted
repo with planned work and a deterministic test command — Regatta
picks up open work items, develops them in isolated worktrees, and
opens PRs gated by a configurable six-layer review stack hardened
against every publicly-documented AI-agent incident class as of
mid-2026.

## Who this is for

- Teams whose work is **machine-readable** (issues / Jira / Linear /
  RFCs / `MILESTONES.md`) — Regatta's value tracks how falsifiable
  your "done" is.
- Teams whose CI is a **deterministic command** that exits non-zero
  on failure (`make test`, `npm test`, `cargo test`, a GH workflow).
- Teams comfortable with **autonomous PR authorship** under a strict
  AI-gate stack plus mandatory human merge.

## Who this is NOT for

- Repos without a machine-readable spec source. Regatta won't invent
  one — encode "done" first.
- Repos that auto-merge without human review. Regatta requires a
  human-merge layer (L6); rubber-stamping breaks the safety model.
- Teams that need to ship today. This is a design doc, not a binary.

## How it works

1. The repo declares its surface in `regatta.yaml` — spec adapter,
   CI command, lanes, hotspots, safety policy.
2. The orchestrator watches the spec source, picks ready items, and
   spawns one Claude agent per item into an isolated worktree.
3. The agent develops the item against acceptance criteria, runs CI,
   and opens a PR with a citation block.
4. The gate stack runs on every PR push:

```
L0  spec-immutability      deterministic, hard-block
L1  repo CI                deterministic
L2  PR-body conformance    deterministic
L3  spec-conformance       AI (Opus 4.7), judicial
L4  adversarial reviewer   AI (Sonnet 4.6)
L5  drift detector         AI (Haiku 4.5)
L6  human merge            branch protection
```

Repos add custom gates, swap models, tune thresholds via the same
config.

## Repo layout

- [`docs/design.md`](docs/design.md) — full design (v3.1, ~5300 words)
- [`docs/incidents.md`](docs/incidents.md) — 19 AI-agent incidents
  with primary sources, root causes, and prevention patterns
- [`schemas/spec_adapter.go`](schemas/spec_adapter.go) — normative Go
  interface for `SpecAdapter` (with custom-adapter wire protocol)
- [`schemas/gate_result.schema.json`](schemas/gate_result.schema.json)
  — JSON Schema for the structured payload every gate emits
- [`schemas/work_item.schema.json`](schemas/work_item.schema.json) —
  JSON Schema for `WorkItem`
- [`schemas/regatta.v1.cue`](schemas/regatta.v1.cue) — CUE schema for
  `regatta.yaml`
- [`gates/l0/testdata/`](gates/l0/testdata/) — L0 spec-immutability
  fixture corpus (the contract)
- [`gates/canary/testdata/`](gates/canary/testdata/) — canary
  archetype corpus + injection mechanism

## The Trap Catalog

`docs/design.md` §Trap Catalog maps each documented incident to a
platform-enforcement pattern wired into the architecture. The 10
patterns:

| ID | Pattern |
|---|---|
| P1 | Deterministic gate before AI gate on destructive ops |
| P2 | Two-key approval on irreversible actions |
| P3 | Trusted instructions from `main` only; all other text is data |
| P4 | Least-privilege, ephemeral, environment-scoped credentials |
| P5 | Out-of-band supervisor for limits and kill-switches |
| P6 | Verified grounding for any outward-facing claim |
| P7 | Schema-level scope constraints, not prompt-level |
| P8 | Spend / iteration brakes with mandatory re-approval |
| P9 | Sensitive context segregation |
| P10 | Invisible-glyph normalization + signed prompt artifacts |

P1, P3, P5, P6, P8 each prevent 3+ documented incidents and are the
most load-bearing.

## Next steps

Pre-implementation, the contract is the schemas + the fixture-corpus
READMEs + the design doc. Concrete next milestones:

1. **L0 implementation.** Pure Go, ~200 lines, passes the
   `gates/l0/testdata/` corpus (target: 200 fixtures).
2. **`regatta validate-config` + `regatta validate-spec`.** Two CLI
   commands; minimal Go binary; CUE validation under the hood.
3. **Orchestrator skeleton.** Daemon, sqlite state, scheduler with
   sorted-lock acquisition, agent spawner.
4. **Gate runners for L3 / L4 / L5.** Anthropic SDK clients with
   prompt-caching, structured-output enforcement.

## Why "regatta"

Work runs in **lanes** — one agent per lane, racing in parallel
toward done. A regatta is a fleet of boats racing in lanes; the
metaphor wrote itself.
