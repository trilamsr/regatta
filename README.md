# regatta

Regatta reads your issue list, spawns Claude agents in isolated worktrees, opens PRs, gates them before you merge.

## Docker quickstart

```sh
cp .env.example .env && $EDITOR .env && docker compose up -d
```

`.env` needs `ANTHROPIC_API_KEY` (Claude API) and `GH_TOKEN` (PR ops).
Everything else is optional. The default compose stack boots with
`--ui=false` so a fresh install has no HMAC-key requirement; open
[`docker-compose.yml`](docker-compose.yml) and flip `--ui=true` +
set `REGATTA_HMAC_KEY=$(openssl rand -hex 32)` in `.env` once you
want the operator dashboard on `:8080`.

The stack brings up `regatta` (the orchestrator daemon), `prometheus`
(metrics), `grafana` (`:3000`, `admin/admin`), and `alertmanager`
(`:9093`). State persists in the `regatta-data` volume.

See [`docs/operator/docker-compose.md`](docs/operator/docker-compose.md)
for the full stack runbook and
[`docs/operator/container.md`](docs/operator/container.md) for the
single-container shape.

## How it works

1. Your repo declares its surface in `regatta.yaml` — spec adapter,
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

Threat model + trap catalog: [`docs/trap-catalog.md`](docs/trap-catalog.md) (moving to `docs/architecture.md` once that lands).

## Scope

Single-tenant, single-operator, single-repo, CLI-only. Multi-tenant
scoping, hosted SaaS, and RBAC are deferred to Phase X (open a request
if you need them).

- Fits: repos with machine-readable specs (issues / Linear / RFCs /
  `MILESTONES.md`) and a deterministic CI command.
- Requires: a human-merge layer (L6) — Regatta does not merge for you.
- Not for: repos without a machine-readable spec source, or teams
  that auto-merge without human review.

## Development

The native binary path is for iterating on `cmd/regatta` against a
checkout. Production runs inside a container per
[`docs/operator/container.md`](docs/operator/container.md).

```sh
brew install trilamsr/regatta/regatta
# or: go install github.com/trilamsr/regatta/cmd/regatta@latest
regatta init
regatta serve --repo . --db .regatta/state.db --spawner=claude
```

See [`docs/operator/quickstart.md`](docs/operator/quickstart.md) for
the full `init` -> `verify-repo-config` -> `serve` walkthrough.

To pull a new build into a running stack:

```sh
docker compose build regatta && docker compose up -d regatta
```

Only the `regatta` service is rebuilt; `prometheus`, `grafana`, and
`alertmanager` keep their state. The daemon does not self-restart
when the binary changes on disk — merged fixes stay dormant until
the rebuild step above.

## Repo layout

- [`docs/design.md`](docs/design.md) — full design
- [`docs/incidents.md`](docs/incidents.md) — AI-agent incident
  catalog with primary sources, root causes, and prevention patterns
- [`contracts/schemas/spec_adapter.go`](contracts/schemas/spec_adapter.go) —
  normative Go interface for `SpecAdapter`
- [`contracts/schemas/gate_result.schema.json`](contracts/schemas/gate_result.schema.json) —
  JSON Schema for the structured payload every gate emits
- [`contracts/schemas/work_item.schema.json`](contracts/schemas/work_item.schema.json) —
  JSON Schema for `WorkItem`
- [`contracts/schemas/regatta.v1.cue`](contracts/schemas/regatta.v1.cue) —
  CUE schema for `regatta.yaml`
- [`testdata/gates/l0/`](testdata/gates/l0/) — L0 fixture corpus
- [`testdata/gates/canary/`](testdata/gates/canary/) — canary
  archetype corpus + injection mechanism

## Why "regatta"

Work runs in **lanes** — one agent per lane, racing in parallel
toward done. A regatta is a fleet of boats racing in lanes; the
metaphor wrote itself.
