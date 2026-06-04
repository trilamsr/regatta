**Status: Phase MVR-1 self-host. Single-tenant, single-operator, single-repo, CLI-only. Schemas and fixture-corpus contracts are normative; the binary tracks them.**

# Regatta

A repo-agnostic autonomous-agent fleet. Point it at any git-hosted
repo with planned work and a deterministic test command - Regatta
picks up open work items, develops them in isolated worktrees, and
opens PRs gated by a configurable six-layer review stack hardened
against every publicly-documented AI-agent incident class as of
mid-2026.

## Start here

- **Operators**: [`docs/operator/`](docs/operator/) (begin at
  `quickstart.md`).
- **Security auditors**: [`docs/auditor/`](docs/auditor/) (begin at
  `threat-model.md`).
- **Internal engineers and AI agents**: [`AGENTS.md`](AGENTS.md) +
  [`docs/engineer/`](docs/engineer/).

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
- Teams that need a hosted multi-tenant SaaS today. Regatta is
  single-tenant, single-operator, self-hosted; multi-tenant scoping
  is deferred to Phase X.

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
- [`contracts/schemas/spec_adapter.go`](contracts/schemas/spec_adapter.go) - normative Go
  interface for `SpecAdapter` (with custom-adapter wire protocol)
- [`contracts/schemas/gate_result.schema.json`](contracts/schemas/gate_result.schema.json)
  - JSON Schema for the structured payload every gate emits
- [`contracts/schemas/work_item.schema.json`](contracts/schemas/work_item.schema.json) -
  JSON Schema for `WorkItem`
- [`contracts/schemas/regatta.v1.cue`](contracts/schemas/regatta.v1.cue) - CUE schema for
  `regatta.yaml`
- [`testdata/gates/l0/`](testdata/gates/l0/) - L0 spec-immutability
  fixture corpus (the contract)
- [`testdata/gates/canary/`](testdata/gates/canary/) - canary
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
  under `testdata/gates/l0/` is the contract; `go test ./internal/gates/l0`
  exercises pass / fail / edge sweeps plus unit-level normalization.
- **`regatta verify-repo-config`.** Pre-flight audit of a GitHub repo
  against the P2 canonical recipe - branch protection
  (`required_approving_review_count>=2`, `require_code_owner_reviews`,
  `require_last_push_approval`, `dismiss_stale_reviews`,
  `enforce_admins`), CODEOWNERS presence, and the
  `/codeowners/errors` silent-ignore catcher. Requires `GITHUB_TOKEN`.
- **`regatta validate-config`.** CUE-validates `regatta.yaml` against
  the embedded `contracts/schemas/regatta.v1.cue`. Multi-error output enumerates
  every offending field with `file:line` positions instead of eliding
  with `(and N more errors)`.
- **L4 adversarial reviewer gate.** `internal/gates/l4/` runs at
  scheduler step 0.7 — Anthropic adapter + tolerant JSON parser +
  per-category model selection + LRU findings cache + second-opinion
  loop + auto-fix patch mode + SIGHUP prompt-template hot-reload.
- **Approval-gate stack.** HMAC-token mint + reaper + multi-key
  rotation + `regatta keys` CLI. CLI flow: `regatta approval list` /
  `regatta approval decide --id X --approve`.
- **Cost governor.** Pre-call USD+token caps, Anthropic Usage/Cost
  API reconciliation, drift alarms, soft-cap warn mode, pricing-table
  boot validator. Runbook: `docs/engineer/runbooks/cost-governor-incidents.md`.
- **W6 OTel backbone.** SDK + slog bridge + scheduler/spawner/gate
  spans + GenAI semantic conventions + Jaeger E2E.
- **W8 OPA Authorizer (single-tenant).** Rego policy embed + atomic
  store swap + SIGHUP/fsnotify hot-reload. Multi-tenant scoping
  deferred to Phase X.
- **W9 substrate-default `DurableHistory`.** Append/replay/diff over
  `substrate_events`. Temporal-backed variant deferred to Phase X.

- **Orchestrator skeleton.** `regatta serve` runs a daemon backed by
  a sqlite state store (`modernc.org/sqlite`) that implements the
  responsibilities in `docs/design.md` §Orchestrator shape:
  the SpecWatcher (markdown_catalog adapter reading
  `<root>/.regatta/items/*.md`), the Scheduler with sorted-lock
  hotspot acquisition and per-lane concurrency caps, the
  `ClaudeSpawner` that shells `claude` per work item into per-agent
  worktrees (`internal/orchestrator/spawner/claude.go`), the
  approval-gate Reaper, PRWatcher
  (`internal/orchestrator/prwatch/`), and RejectionRouter
  (`internal/orchestrator/rejectionrouter/`). The state machine in
  `docs/design.md` §State, persistence, recovery is enforced in
  `internal/orchestrator/state`; crash recovery requeues dead agents
  on startup. `--tick-once` runs a single poll+schedule cycle for CI
  smoke tests. CanaryInjector, SupervisorLimits (cgroups / rlimits),
  and LessonCapture are deferred to follow-up commits.

## Next steps

1. **Expand the L0 fixture corpus toward the 200-fixture target.**
   Currently at ~88 fixtures under `testdata/gates/l0/`.
2. **`regatta validate-spec`.** Connect to the configured `SpecAdapter`,
   list ready work items, surface NFC + invisible-glyph cleanliness,
   verify the dependency DAG.
3. **SupervisorLimits.** The `ClaudeSpawner` shells `claude` per
   work item into per-agent worktrees (`internal/orchestrator/spawner/claude.go`),
   but cgroups (Linux) / rlimits (macOS) are not wired yet.
4. **PRWatcher + RejectionRouter.** Drive agents past `running` so
   the rest of the state machine (`pr_open` through `done`) gets
   exercised.
5. **Gate runners for L3 / L5.** L4 (adversarial reviewer) shipped via the
   Phase S2-T2 wave (`internal/gates/l4/`); L3 spec-conformance and L5 drift
   detection still pending.

## Why "regatta"

Work runs in **lanes** — one agent per lane, racing in parallel
toward done. A regatta is a fleet of boats racing in lanes; the
metaphor wrote itself.
