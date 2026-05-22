**Status: Draft design, pre-implementation. Schemas and fixture-corpus contracts are normative; the binary follows.**

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

- [`docs/design.md`](docs/design.md) — full design
- [`docs/incidents.md`](docs/incidents.md) — AI-agent incident
  catalog with primary sources, root causes, and prevention patterns
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
platform-enforcement pattern wired into the architecture. The 13
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
| P11 | Agent-artifact release pipelines are themselves attack surface |
| P12 | Inbound vulnerability signals default-escalate |
| P13 | Judge-LLM lineage isolation + read-only metric channel |

P1, P3, P5, P6, P8, P10 each prevent 3+ documented incidents and are
the most load-bearing.

## Shipped

- **L0 spec-immutability gate.** `cmd/regatta l0 <diff>` reads a
  unified diff, runs invisible-glyph stripping + NFC + criterion
  byte-equality, and emits a `GateResult` JSON. The fixture corpus
  under `gates/l0/testdata/` is the contract; `go test ./internal/l0`
  exercises pass / fail / edge sweeps plus unit-level normalization.
- **`regatta verify-repo-config`.** Pre-flight audit of a GitHub repo
  against the P2 canonical recipe - branch protection
  (`required_approving_review_count>=2`, `require_code_owner_reviews`,
  `require_last_push_approval`, `dismiss_stale_reviews`,
  `enforce_admins`), CODEOWNERS presence, and the
  `/codeowners/errors` silent-ignore catcher. Requires `GITHUB_TOKEN`.
- **`regatta validate-config`.** CUE-validates `regatta.yaml` against
  the embedded `schemas/regatta.v1.cue`. Multi-error output enumerates
  every offending field with `file:line` positions instead of eliding
  with `(and N more errors)`.

- **Orchestrator skeleton.** `regatta serve` runs a daemon backed by
  a sqlite state store (`modernc.org/sqlite`) that implements three of
  the nine responsibilities in `docs/design.md` §Orchestrator shape:
  the SpecWatcher (markdown_catalog adapter reading
  `<root>/.regatta/items/*.md`), the Scheduler with sorted-lock
  hotspot acquisition and per-lane concurrency caps, and the
  AgentSpawner (currently a stub that records spawn calls). The state
  machine in `docs/design.md` §State, persistence, recovery is
  enforced in `internal/orchestrator/state`; crash recovery requeues
  dead agents on startup. `--tick-once` runs a single poll+schedule
  cycle for CI smoke tests. PRWatcher, RejectionRouter,
  CanaryInjector, SupervisorLimits, Reaper, and LessonCapture are
  deferred to follow-up commits.

## Next steps

1. **Expand the L0 fixture corpus toward the 200-fixture target.**
2. **`regatta validate-spec`.** Connect to the configured `SpecAdapter`,
   list ready work items, surface NFC + invisible-glyph cleanliness,
   verify the dependency DAG.
3. **Production AgentSpawner.** Replace the stub with a real worktree
   + `claude --resume` launcher; wire SupervisorLimits (cgroups on
   Linux, rlimits on macOS).
4. **PRWatcher + RejectionRouter.** Drive agents past `running` so
   the rest of the state machine (`pr_open` through `done`) gets
   exercised.
5. **Gate runners for L3 / L4 / L5.** Anthropic SDK clients with
   prompt-caching, structured-output enforcement.

## Why "regatta"

Work runs in **lanes** — one agent per lane, racing in parallel
toward done. A regatta is a fleet of boats racing in lanes; the
metaphor wrote itself.
