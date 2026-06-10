---
name: compose-container-name-strategy
slug: 2026-06-09-compose-container-name-strategy
status: draft
phase: self-host-first
owner: trilamsr@gmail.com
created: 2026-06-09
---

# Compose container-name strategy — per-worktree isolation

_Author: design session, 2026-06-09. Closes GH #1174. Source: operator pain observed across regatta-operator skill sessions 3-5 this session._

## 1. Observed pain

`docker-compose.yml` pins fixed `container_name:` on every service (`regatta-init`, `regatta`, `regatta-prometheus`, `regatta-grafana`, `regatta-alertmanager` — lines 38, 51, 92, 125, 150). Docker requires container names to be globally unique on a host, so two worktrees of the same repo cannot run their compose stacks side-by-side: the second `docker compose up` fails immediately with `Conflict. The container name "/regatta" is already in use`. The fix today is destructive cleanup:

```
docker rm -f regatta regatta-grafana regatta-prometheus regatta-init regatta-alertmanager
```

**Frequency (this session)**: 3 separate `docker compose up` failures across regatta-operator skill sessions 3, 4, and 5 — each one triggered after the operator switched worktrees and tried to bring up the obs stack against the new branch. Each recovery cost the operator one full manual `docker rm -f` sweep across five fixed names plus a re-`up`. Per-worktree compose isolation is the whole point of the compose `--project-name` flag (and the `COMPOSE_PROJECT_NAME` env var), but explicit `container_name:` lines defeat it because compose stops generating `<project>_<service>_<n>` names when a literal is pinned.

## 2. Two options + tradeoff

### Option A — drop `container_name:` lines, let compose generate `<project>_<service>_<n>`

Remove all five `container_name:` lines (38, 51, 92, 125, 150). Compose falls back to its default naming scheme: `<project>-<service>-<n>` (compose v2 hyphen form), where `<project>` defaults to the directory name and is overridable via `--project-name <name>` or `COMPOSE_PROJECT_NAME=<name>`.

- **Pro**: per-worktree isolation works automatically. Worktree `.claude/worktrees/spec-1169-scheduler-cap/` brings up `spec-1169-scheduler-cap-regatta-1`; worktree `fix-1170-revert-pay-as-you-go-default/` brings up `fix-1170-revert-pay-as-you-go-default-regatta-1`. Both stacks coexist. Operator running parallel debugs of two branches never has to choose which one to kill.
- **Pro**: aligns with the compose-native UX path — operators who learn compose elsewhere already expect this naming.
- **Con**: operator-facing container names get longer and project-scoped. Existing docs (`docs/operator/docker-compose.md`, the file-header quickstart at line 30, any `docker logs regatta` snippet) break and need a one-time sweep. Operators who type `docker logs regatta` from muscle memory get `No such container` and have to learn `docker compose logs regatta` instead.

### Option B — keep `container_name:`, add a pre-`up` cleanup script

Keep the five fixed-name lines. Add a `docker compose up` wrapper (or a Makefile target) that runs `docker rm -f regatta regatta-grafana regatta-prometheus regatta-init regatta-alertmanager` before bringing the stack up. Document the cleanup-before-up sequence in `docs/operator/docker-compose.md`.

- **Pro**: short container names preserved. `docker logs regatta` keeps working. No doc sweep.
- **Con**: cleanup is destructive across worktrees. An operator running a long-debug stack on worktree A and then running `docker compose up` from worktree B has stack A silently killed mid-debug. The whole reason compose has `--project-name` is to avoid exactly this footgun — Option B re-implements the footgun and papers over it with a destructive script.
- **Con**: the script grows every time a service is added to the compose file; the cleanup list and the compose file drift independently. Mechanical enforcement (a check that the rm-list matches the compose `container_name:` set) would have to be added on top.

## 3. Recommendation — Option A

Drop the five `container_name:` lines. Operator UX over short-name muscle memory: parallel-worktree compose stacks are a load-bearing self-host workflow (operator debugs branch A while reviewer subagent works branch B in a second compose stack), and Option B kills that workflow.

Compose project name gives operators full control of the prefix:

```
COMPOSE_PROJECT_NAME=regatta docker compose up -d   # back to short(ish) names: regatta-regatta-1
docker compose --project-name dev up -d             # arbitrary prefix
```

Operator-facing docs update once (`docs/operator/docker-compose.md` + the quickstart at lines 29-33 of `docker-compose.yml`). Long-term UX > one-time doc sweep.

Per `feedback_decision_priority` (UX > ease > performance > best-practices > speed > velocity; long-term > short-term) and `feedback_default_simpler` (pick the simplest viable option; don't pre-build a cleanup script + drift-check lint for hypothetical short-name preservation when removing five lines and updating one doc page solves it).

## 4. Citations

- **Rule**: `feedback_decision_priority` — UX > ease, long-term > short-term. Option A trades a one-time doc sweep for permanent per-worktree compose isolation.
- **Rule**: `feedback_default_simpler` — Option B requires a cleanup script plus a drift-check lint to keep the script in sync with the compose file. Option A is line-deletion.
- **Evidence**: regatta-operator skill sessions 3, 4, 5 (this session, 2026-06-09) — three separate `docker compose up` collisions after worktree switching, each recovered by manual `docker rm -f` across five fixed names.
- **Lines that change** (`/Users/treedesk/Desktop/Projects/regatta/docker-compose.yml`):
  - line 38 — `container_name: regatta-init`
  - line 51 — `container_name: regatta`
  - line 92 — `container_name: regatta-prometheus`
  - line 125 — `container_name: regatta-grafana`
  - line 150 — `container_name: regatta-alertmanager`
- **Doc surfaces that update once**:
  - `docs/operator/docker-compose.md` — any `docker logs regatta` / `docker exec regatta` snippets switch to `docker compose logs regatta` / `docker compose exec regatta`.
  - `docker-compose.yml` quickstart header (lines 29-33) — add `COMPOSE_PROJECT_NAME` guidance.

## 5. Out of scope

- **Full split into `docker-compose.dev.yml` / `docker-compose.prod.yml`**. Multi-file compose with environment-specific overrides is a larger surface that the current single-operator self-host loop does not need. Deferred to Phase X with reopen-trigger: first external customer ask for a production-vs-dev separation, OR the single compose file accumulates more than two environment-conditional `${VAR:-default}` blocks. Tracked: file at that trigger, not now.
- **Migrating the obs stack to a separate compose file** (`docker-compose.obs.yml`) so that `regatta` itself can boot without Prometheus/Grafana/AlertManager. Distinct concern from container naming; out of scope for this brief.
- **Renaming the bridge network** (`regatta-net`, line 164). Networks are also project-scoped by compose; the fixed name is harmless because compose prefixes it automatically. No change needed.
