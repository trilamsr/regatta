# Regatta

A repo-agnostic autonomous-agent fleet. Point it at any git-hosted
repo with planned work and a deterministic test command, and a fleet
of Claude agents picks up open work items, develops them in isolated
worktrees, and opens PRs gated by a configurable review stack
hardened against every publicly-documented AI-agent incident class
as of mid-2026.

## How it works

1. The repo declares its surface in `regatta.yaml` — its spec
   adapter (issues / RFCs / markdown catalog / Jira / Linear /
   custom), its CI command, its lanes, its hotspot files, its
   safety policy.
2. The orchestrator watches the spec source, picks planned work
   items whose dependencies are satisfied, and spawns one Claude
   agent per item into an isolated worktree.
3. The agent develops the item against the acceptance criteria,
   runs CI locally, and opens a PR with a citation block.
4. A configurable gate stack runs on every PR push:

```
L0  deterministic spec-immutability       (pre-AI hard gate)
L1  repo's CI command                      (deterministic)
L2  PR-body conformance                    (deterministic)
L3  AI spec-conformance verifier           (judicial, default Opus)
L4  AI adversarial reviewer                (mixed family, default Sonnet)
L5  AI drift detector                      (cheap, default Haiku)
L6  human merge                            (branch protection)
```

Repos can add custom gates (security scans, migration safety, license
audits, i18n checks), reorder layers, swap models, tune thresholds.

## Why "regatta"

Work runs in **lanes** — one agent per lane, racing in parallel
toward done. A regatta is a fleet of boats racing in lanes; the
metaphor wrote itself.

## Status

**Draft design, pre-implementation.** No code yet. The design is at
v3, hardened against 19 publicly-documented AI-agent incident classes
(Replit/PocketOS database wipes, EchoLeak, Comment-and-Control,
Cursor MCPoison, Air Canada liability, Mata v. Avianca, slopsquatting,
Sakana self-modification, o3 shutdown sabotage, Opus 4 blackmail,
Cursor runaway-billing, GTG-1002 espionage campaign, and more).

## Repo layout

- [`docs/design.md`](docs/design.md) — full design (v3, generic)
- [`docs/incidents.md`](docs/incidents.md) — 19 AI-agent incidents
  with primary sources, root causes, and prevention patterns
- [`docs/gate-prompts.md`](docs/gate-prompts.md) — production prompts
  for L3/L4/L5
- [`docs/orchestrator.md`](docs/orchestrator.md) — Go daemon skeleton
- [`docs/pilot.md`](docs/pilot.md) — applied pilot brief (case study)
- [`docs/reviews/`](docs/reviews/) — eight parallel adversarial
  reviews that drove the v1 → v2 → v3 progression

## The Trap Catalog

`docs/design.md` §Trap Catalog maps each incident class to a
platform-enforcement pattern wired into the architecture:

- **P1** Deterministic gate before AI gate on destructive ops
- **P2** Two-key approval on irreversible actions
- **P3** Fetch trusted instructions from `main`, treat all other text as data
- **P4** Least-privilege, ephemeral, environment-scoped credentials
- **P5** Out-of-band supervisor for limits and kill-switches
- **P6** Verified grounding for any outward-facing claim
- **P7** Schema-level scope constraints, not prompt-level
- **P8** Spend / iteration brakes with mandatory re-approval
- **P9** Sensitive context segregation
- **P10** Render-the-invisible + signed prompt artifacts

P1, P3, P5, P6, P8 each prevent 3+ documented incidents and are the
most load-bearing.
