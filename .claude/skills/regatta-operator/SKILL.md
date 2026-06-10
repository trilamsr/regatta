---
name: regatta-operator
description: Act as the human-in-the-loop operator of regatta running against a target repo. Two co-equal primary responsibilities — (1) FEED the orchestrator by picking next wedges from roadmap / milestones / ready-labeled issues / briefs / specs and filing them as orchestrator-consumable issues, and (2) OBSERVE the running orchestrator for errors / inefficiencies / stuck agents and file findings + capture meta-lessons across the four surfaces (regatta-core, orchestrator, operator-loop, agent-prompt). Loop is unbounded by design — exits only on wedge-queue exhaustion + quiet observation OR operator interrupt OR recurring-trap fire. Use when the user says "operate regatta", "babysit regatta", "regatta operator mode", "run regatta on <repo>", "watch the swarm", or any phrasing that asks Claude to drive a regatta session end-to-end. The skill takes a TARGET_REPO argument naming the GitHub repo regatta will work against; defaults to the in-tree test target. Containment first — every action is scoped, reversible, and logged.
---

# regatta-operator

Operator of `regatta` — the agent-orchestration binary. **GOAL = AUTONOMY.** The orchestrator is the worker; the human is the *exception path*, not the *default path*. This skill exists so the human stays OUT of the loop while regatta builds / reviews / designs / merges against a target repo. Skill's job: keep the conditions for autonomy intact + detect when autonomy breaks + auto-file the fix-request as a `autonomous`-labelled issue regatta can consume.

NOT this skill's job: write regatta code, merge PRs, or substitute for the orchestrator. If you find yourself doing the orchestrator's work, the autonomy loop is broken — file the breakage instead of papering over it.

### Autonomy mandate

- **Default state = silent.** In autonomous mode (operator said "autonomous" / "loop" / "keep watching"), skill emits one heartbeat per N hours (`heartbeat_interval`, default 4h) summarizing all iterations between heartbeats; per-phase narration is suppressed. In one-shot mode (operator said "snapshot" / "check" / "report"), skill narrates one line per phase per the run loop and hands back. The per-phase narration rule in the run loop applies to one-shot mode; the heartbeat rule applies to autonomous mode. Explicit operator request wins; default when ambiguous = one-shot.
- **Auto-act, don't ask.** Every finding routes to one of: (a) auto-filed `autonomous`-labelled issue (regatta consumes), (b) auto-comment on existing tracker issue (recurrence), (c) auto-updated baseline file (intended drift). `AskUserQuestion` is reserved for genuinely irreversible decisions only.
- **Self-improve detector first.** Before filing a finding, search for an open issue regatta's self-improve detector already filed for the same root cause. If exists, auto-comment + bump counter. NEVER file duplicate.
- **Autonomy regression = HIGH severity.** Any signal that the loop is going DOWN (build-green rate dropping, review-catch rate dropping, recurrence counter climbing, the orchestrator falling back to operator-prompts) is a HIGH `[OPS]` or `[AGENT]` finding, auto-filed.
- **Green-clock signal.** Track ≥10 PRs/day green-merge consecutive days. Day count resets on any operator manual merge OR `--admin` override. Heartbeat reports `green-clock=<N>` so the operator can see autonomy compounding without reading details.

## Inputs

Skill argument: `TARGET_REPO=<owner>/<name>`. If unset, ask via `AskUserQuestion` when available; otherwise prompt the operator in the next message and stop. Persist the choice for the session.

Do NOT silently switch target repos mid-session. Switching invalidates the observation baseline.

## Target config (per-target YAML — keeps skill portable across orchestrators)

This skill is written against `regatta` but works for any agent-orchestrator-vs-target-repo setup. Per-target literals live in `.claude/skills/regatta-operator/targets/<owner>-<name>.yaml`. Missing file = use the regatta defaults below. Treat the YAML as authoritative for THIS target; defaults are fallback only.

```yaml
# .claude/skills/regatta-operator/targets/<owner>-<name>.yaml
orchestrator:
  binary: regatta                                     # process name to pgrep / inspect
  process_match: 'regatta serve'                      # ps -ef | grep <this>
  branch_prefix: 'regatta/agent-'                     # spawner-assigned branch prefix
  state_db_glob: '*.db *.sqlite *.sqlite3'            # find candidates
  poll_interval_cfg_key: 'PollInterval|TickInterval'  # git grep key
  parallel_cap_cfg_key: 'ParallelCap|MaxConcurrent'
  systemd_unit: regatta                               # journalctl --user -u <unit>
  docker_service: regatta                             # docker compose logs -f <service>
  spawn_label: autonomous                             # GH-issue label the adapter consumes (no brackets — brackets are TITLE convention, not label name; closes #1167)
target:
  ci_provider: github-actions                         # github-actions | gitlab-ci | buildkite | circleci
  ci_query_cmd: |                                     # must return PR -> status mapping
    gh pr list -R "$TARGET_REPO" --json number,statusCheckRollup,mergeStateStatus -L 50
  roadmap_paths:                                      # filter findings against active phase
    - ROADMAP.md
    - docs/engineer/autonomous-session-prompt.md
    - docs/*/roadmap*.md
  design_paths:                                       # designer-canary inputs
    - docs/engineer/specs/
    - docs/engineer/briefs/
    - docs/rfcs/
  defer_labels: [phase-x, phase-x-forward-fit, wontfix, deferred]   # exclude from finding sweeps
findings_repo:                                        # WHICH repo gets the issue file
  orchestrator_findings: orchestrator-source-repo     # [CORE]/[ORCH]/[OPS]/[AGENT] always file against the orchestrator's own GH repo
  target_findings: target-repo                        # findings about the TARGET (canary failed = bug in target code) file against target
roles:                                                # subtypes for [AGENT] findings
  - impl                                              # implementer
  - rev                                               # reviewer
  - des                                               # designer
  - tri                                               # triage
canaries:                                             # known-input fixtures for regression detection
  CANARY_DIR_impl: .claude/skills/regatta-operator/canaries/impl/
  CANARY_DIR_rev:  .claude/skills/regatta-operator/canaries/rev/
  CANARY_DIR_des:  .claude/skills/regatta-operator/canaries/des/
  CANARY_DIR_tri:  .claude/skills/regatta-operator/canaries/tri/
  # Bootstrap: directories are empty by default. Skill auto-creates them at first
  # run and emits a `[OPS]` finding "canary suite empty for role=<r>" with a
  # template the orchestrator can fill via its self-improve detector. Empty dir
  # = canary section SKIPPED for that role, NOT an error. Forward-fit phase.
baseline_file: .claude/skills/regatta-operator/baselines/<owner>-<name>.json
```

Refer to config values via the `$ORCH_BINARY`, `$BRANCH_PREFIX`, `$CI_QUERY_CMD`, etc. names below. When the YAML is missing, the regatta defaults shown above apply — DO NOT bake new literals into the skill body.

## Pre-flight (REQUIRED before first snapshot)

Run discovery FIRST. Hardcoded values are bugs.

```bash
# Find running orchestrator + its port (substitute $ORCH_BINARY / $PROCESS_MATCH from target YAML;
# defaults: $ORCH_BINARY=regatta, $PROCESS_MATCH='regatta serve')
pgrep -l "$ORCH_BINARY" || ps -ef | grep "[${ORCH_BINARY:0:1}]${ORCH_BINARY:1}"
lsof -nP -iTCP -sTCP:LISTEN -p <PID> 2>/dev/null      # listening port (portable)
ss -lntp 2>/dev/null | grep "$ORCH_BINARY"            # Linux
netstat -anv -p tcp 2>/dev/null | grep LISTEN | grep "$ORCH_BINARY"   # macOS

# Find state DB ($STATE_DB_GLOB defaults to '*.db *.sqlite *.sqlite3')
find . -maxdepth 4 \( -name '*.db' -o -name '*.sqlite*' \) 2>/dev/null | head -5

# Find log destination — try $SYSTEMD_UNIT user, then system, then docker compose
git grep -nE 'log\.Open|os\.Stderr|journal|slog\.New' cmd/ internal/ 2>/dev/null | head -10
journalctl --user -u "$SYSTEMD_UNIT" -n 1 --no-pager 2>/dev/null \
  || journalctl -u "$SYSTEMD_UNIT" -n 1 --no-pager 2>/dev/null \
  || docker compose logs --tail=1 "$DOCKER_SERVICE" 2>/dev/null \
  || echo "no managed log; tail process stderr fd"

# Discover poll interval + parallel cap from config keys named in target YAML
git grep -nE "$POLL_INTERVAL_CFG_KEY" cmd/ internal/ 2>/dev/null
git grep -nE "$PARALLEL_CAP_CFG_KEY" cmd/ internal/ 2>/dev/null

# Build method (docker / docker-compose / go-install / systemd)
ps -o command= -p <PID> | head -1
docker ps --filter "ancestor=$ORCH_BINARY" --format '{{.Names}}' 2>/dev/null
ls Dockerfile docker-compose*.y*ml 2>/dev/null
which "$ORCH_BINARY" && go version -m "$(which "$ORCH_BINARY")" | head -5

# Discover active roadmap phase from $ROADMAP_PATHS (filter findings against this)
for p in $ROADMAP_PATHS; do grep -nE '^(P[0-9]+|PHASE)[[:space:]].*(\[IN-FLIGHT\]|\[BLOCKED\])' "$p" 2>/dev/null; done | head -20

# Load baseline file for regression detection (default missing = cold start, write one at session end)
test -f "$BASELINE_FILE" && jq '.' "$BASELINE_FILE" || echo 'cold-start: no baseline'
```

State the discovered values in the first narration line:
`pre-flight: pid=<N> port=<P> db=<path> poll=<interval> cap=<N> build=<docker|go-install|systemd> kill=<signal+cmd> phase=<active-roadmap-phase> baseline=<exists|cold>`

If ANY of pid/port/db is unknown, refuse the snapshot loop. Unknown `poll` → "is it stuck?" diagnoses are low-confidence. Unknown `build` → tight-loop section is unavailable. Unknown `phase` → roadmap-filter is disabled (warn, don't block).

## Polling cadence (read this BEFORE diagnosing "stuck")

The orchestrator polls its work source (GH issues, internal queue, etc.) on `$POLL_INTERVAL`. It does NOT receive webhooks by default. This means:

- **Lag** = up to `$POLL_INTERVAL` seconds between source change and orchestrator noticing. Expected. NOT a bug.
- **Stuck** = ≥ 3 × `$POLL_INTERVAL` with no equivalent state-transition event in logs (e.g. `prwatch.*` / `scheduler.poll.*`). File `[ORCH]` finding.
- **Silent stall** = poll fires but no state transitions over multiple intervals despite open work. File `[CORE]` finding.

When `$POLL_INTERVAL` is unknown, "is it stuck?" diagnoses are low-confidence — say so in narration.

Tracking issues for polling-vs-webhook design improvements live under `[CORE]`.

## Output-quality channels (per-role; primary signal for autonomy health)

The goal is autonomy. The orchestrator is healthy when its OUTPUT is good — green builds, real reviews, mergeable designs. Watching state transitions without watching output is watching the wrong thing.

Every channel below produces a metric. Each session's metrics are written to `$BASELINE_FILE` at hand-off. Next session compares vs baseline.

**Regression thresholds.** Each metric carries its own absolute or relative bound (see table). A finding fires on ANY single metric crossing its bound — not all simultaneously. To avoid alarm storms when multiple metrics regress in the same session (often a single root cause), bundle co-firing regressions into ONE `[AGENT][<role>]` umbrella issue per role per session. Recurrence rule still applies: comment on existing umbrella, never new.

| Metric | Source | Auto-action on regression |
|---|---|---|
| `build_green_rate_impl` | `$CI_QUERY_CMD` → count `statusCheckRollup=SUCCESS` / total per implementer agent | file `[AGENT][impl]` if rate drops ≥ 0.10 vs baseline (= 10% relative regression) |
| `tdd_order_rate_impl` | per merged PR, first commit subject of `git log --reverse --format=%s <base>..<head>` matches POSIX-ERE `^(test\|red\|RED\|FAIL\|\[TEST\])` (parentheses group the alternation; only first commit subject is tested) | file `[AGENT][impl]` if rate drops ≥ 0.10 vs baseline |
| `comment_density_impl` | regatta: `bash scripts/check-comment-density.sh`. Generic: `cloc --csv --quiet <PR-diff>` → comment / total | file `[AGENT][impl]` if density ≥ 0.05 on new files (= 5%) |
| `mock_vs_real_ratio_impl` | regatta: `bash scripts/check-mock-vs-real.sh`. Generic: `grep -cE 'mock\|stub\|fake' *_test.go` / total test lines per merged PR | file `[AGENT][impl]` if ratio ≥ 0.70 |
| `scope_creep_impl` | merged-PR LoC + file-count rolling avg | file `[AGENT][impl]` if either dimension ≥ 1.50× baseline (= 50% regression) |
| `review_catch_rate_rev` | sample N merged PRs/session, dispatch independent reviewer-replay, diff findings | file `[AGENT][rev]` if catch rate drops ≥ 0.10 vs baseline |
| `review_false_positive_rate_rev` | reviewer findings that got dismissed inline | file `[AGENT][rev]` if rate rises ≥ 0.10 vs baseline |
| `review_approve_no_finding_rev` | APPROVE recommendations with zero findings | file `[AGENT][rev]` if rate ≥ 0.30 absolute (any session) |
| `spec_to_impl_conversion_des` | designer specs that landed mergeable impl within K=3 dispatches | file `[AGENT][des]` if conversion drops ≥ 0.10 vs baseline |
| `spec_churn_des` | specs re-edited > 2 times before impl dispatch | file `[AGENT][des]` if rate rises ≥ 0.10 vs baseline |
| `triage_misclass_tri` | findings ended up needing different surface than triaged | file `[AGENT][tri]` if rate rises ≥ 0.10 vs baseline |
| `green_clock_days` | consecutive days ≥10 PRs merged unattended (no operator `--admin`, no manual merge) | heartbeat report only |
| `recurrence_counter_<root>` | open issue with same `<root>` keyword | comment on existing issue, never new |

Read `$BASELINE_FILE` at pre-flight; write at hand-off. Schema: `{session_id, ts, metrics:{<name>: <value>, ...}, phase, green_clock_days}`. Cold start = no baseline → no regression alerts until N≥2 sessions exist.

## Per-role canary suite

Per-role known-input fixtures live under `$CANARY_DIR_impl` / `$CANARY_DIR_rev` / `$CANARY_DIR_des` / `$CANARY_DIR_tri`. Canaries are RE-PLAYED on a schedule, not human-invoked. Default cadence = once per heartbeat (4h). Each canary fixture is a directory with:
- `input.md` — issue body or PR diff to feed in.
- `expected.json` — structured expected output (PR shape / finding lines / spec key sections / surface label).
- `meta.yaml` — `last_run`, `last_result`, `seeded_bug_line` (for `rev/`), `expected_K_dispatches` (for `des/`).

**Empty `$CANARY_DIR_<role>`** = canary section SKIPPED for that role. Skill emits ONE `[OPS]` finding "canary suite empty for role=<r>" with a fixture template at first run, then suppresses for subsequent sessions. Empty ≠ error.

**Replay path.** Each canary is replayed by injecting `input.md` into the orchestrator's normal intake — the SAME path the orchestrator already consumes (e.g. file a target-repo issue with `$SPAWN_LABEL`, or POST to the orchestrator's internal queue endpoint if exposed). Do NOT bypass intake by directly invoking the spawner — the goal is to test the live loop, not the sandbox-only code path. Tag the input body with `canary:<role>:<fixture>:<ts>` so the output is mechanically correlatable.

**Divergence vs replay.** Replay = inject input + wait for orchestrator output. Divergence = `expected.json` does not match the captured output. ALL divergences auto-file ONE `[AGENT][<role>]` finding per canary per session against `$ORCH_SOURCE_REPO`; do not file per-divergence + per-replay separately.

Skipping canaries because "the orchestrator is busy" is a containment fail. Canaries replay regardless; the orchestrator is supposed to be busy.

## Roadmap awareness (filter findings against active phase)

Findings against deferred / phase-x / wontfix work waste tracker space. Before filing, check:

```bash
# Issue's labels (target repo)
gh issue view <N> -R "$TARGET_REPO" --json labels --jq '.labels[].name' \
  | grep -E "$(printf '%s' "$DEFER_LABELS" | tr ' ' '|')" && echo "skip: deferred"

# Active roadmap phase (per $ROADMAP_PATHS)
grep -E '^P[0-9]+[[:space:]].*\[IN-FLIGHT\]' $ROADMAP_PATHS 2>/dev/null | head -3
```

Findings against the active phase: file. Findings against deferred phases: capture in `$BASELINE_FILE` only; do not file. Findings against unknown phase: warn + file with `phase=unknown` tag.

When MULTIPLE phases are marked `[IN-FLIGHT]` simultaneously (e.g. P0 + P3), treat the LOWEST-numbered phase as primary; the others are "in-flight-parallel". Findings against any in-flight phase are filed. The primary is what the heartbeat reports as `phase=P<lowest>`.

**Where the finding is filed.** Two repos in play: `$ORCH_SOURCE_REPO` (where the orchestrator's own code lives — for regatta self-host this == `$TARGET_REPO`) and `$TARGET_REPO` (the workload). Routing per the YAML `findings_repo` block:
- `[CORE] / [ORCH] / [OPS] / [AGENT]` findings → `$ORCH_SOURCE_REPO` (these are orchestrator-improvement issues).
- Findings about TARGET code (canary tripped a real bug in target) → `$TARGET_REPO`.
- Recurrence search runs in BOTH repos before filing.

## Tight feedback loop (edit → rebuild → restart → canary → observe)

Default recipe when the orchestrator's self-improve detector lands a source change, or when an operator edits deliberately. Skip in steady autonomous state.

1. **Checkpoint state DB** before edit. WAL DBs are multi-file; never `cp` live. Use `.backup` (atomic, lock-aware, single-file). Fallback when `.backup` is unavailable: stop orchestrator first, then `cp` all three of `$DB` / `$DB-wal` / `$DB-shm` together.
   ```bash
   TS=$(date +%s)
   sqlite3 "$DB" ".backup '$DB.ckpt-$TS'"
   sqlite3 "$DB.ckpt-$TS" 'pragma integrity_check' | head -3
   ```
2. **Edit in a worktree.** Running process holds its text segment in RAM — source edits don't affect it until next launch. Editing the running checkout offers zero runtime benefit and risks unstaged leak into rebuild. Worktree gives clean `git switch -` baseline.
3. **Build verification.** Compile errors surface immediately; startup errors only surface after restart. Always run the compile check FIRST so you don't conflate them.
   - `go-install` build: `go build "./cmd/$ORCH_BINARY"` (does NOT install) → on success `go install "./cmd/$ORCH_BINARY"`.
   - `docker` build: `docker build -t "$ORCH_BINARY:dev" .` then capture image SHA — comparing this SHA to the running container's image SHA tells you if the rebuild actually changed anything.
   - `docker-compose` build: `docker compose build "$DOCKER_SERVICE"` then `docker compose images "$DOCKER_SERVICE"`.
4. **Graceful restart by default.** Hard kill mid-write corrupts the state DB silently.

   | Strategy | Command | When |
   |---|---|---|
   | Graceful (default) | `kill -TERM <PID>` then wait for exit; or `systemctl --user restart $SYSTEMD_UNIT`; or `docker compose restart $DOCKER_SERVICE` | Always try first |
   | Hard kill (last resort) | `kill -KILL <PID>` | Only if graceful does not exit within ~30s. Before KILL, try `kill -ABRT <PID>` — the Go runtime dumps all goroutine stacks to stderr on SIGABRT regardless of `signal.Notify` registration. SIGQUIT is intercepted ONLY when the Go binary registers it; assume the orchestrator does not, so SIGQUIT will be silently ignored unless target YAML says otherwise. |

   After restart, re-run `sqlite3 "$DB" 'pragma integrity_check'`. Lock-orphan signature: `sqlite3` returns `database is locked` despite no other process — `lsof "$DB"` finds it.
5. **Confirm the binary changed.** Restart picking up the old binary is this loop's most common silent failure. Resolve the running binary by PID (NOT `which`, which may shim): Linux `readlink -f /proc/<PID>/exe`; macOS `lsof -p <PID> -Fn | awk '/^ftxt/{getline n; sub(/^n/,"",n); print n; exit}'`. Compare against step-3 build output: `go version -m <path>` (go-install) or `docker inspect --format '{{.Image}}' <container>` (docker).
6. **Stop the orchestrator BEFORE wiping agent worktrees.** Wiping live races the spawner's `mkdir`. PID-identity check is sufficient for single-operator self-host; for parallel-operator scenarios also verify the port is unbound (`lsof -nP -iTCP:$PORT -sTCP:LISTEN | grep -q . && exit 1`).
   ```bash
   if kill -0 <PID> 2>/dev/null; then echo "$ORCH_BINARY still running"; exit 1; fi
   git worktree list | awk '/agent-/ {print $1}' \
     | while IFS= read -r path; do [ -n "$path" ] && git worktree remove --force --force "$path"; done
   find .claude/worktrees -maxdepth 1 -name 'agent-*' -type d -exec rm -rf {} + 2>/dev/null
   git worktree prune
   ```
7. **Replay a canary** — see per-role canary suite section. Single-operator-shot: re-open prior canary issue or file fresh issue tagged `canary:<role>:<fixture>:<ts>`.
8. **Observe through restart.** `tail -F` breaks on inode change; `journalctl -fu` drops on journal rotation. Survivor channels: `journalctl --user -fu $SYSTEMD_UNIT` (systemd, pair with `--since "1 min ago"` to catch boot), `docker compose logs -f --since=1m $DOCKER_SERVICE` (docker, reconnects on restart), or redirect-to-file + re-`tail -F` after restart (bare process). First post-restart line should carry the build info from step 5.

### Sandbox target-repo reset (between iterations)

DANGEROUS if `$TARGET_REPO` drifted. Re-read the value FIRST, refuse if it does not match the pre-flight value.

```bash
# Confirm target hasn't drifted
test "$TARGET_REPO" = "$PREFLIGHT_TARGET" || { echo "TARGET drifted; abort"; exit 1; }

# Close all OPEN PRs from orchestrator-spawned branches ($BRANCH_PREFIX prefix).
# Use `while read` instead of `xargs -I{}` — BSD xargs invokes the command once with literal `{}`
# on empty input, which would call `gh pr close {} -R ...` and corrupt state.
gh pr list -R "$TARGET_REPO" --state open --json number,headRefName -L 50 \
  | jq -r --arg p "$BRANCH_PREFIX" '.[] | select(.headRefName | startswith($p)) | .number' \
  | while IFS= read -r n; do
      [ -n "$n" ] || continue
      gh pr close "$n" -R "$TARGET_REPO" -d || break   # break on first failure (rate-limit, perms)
      sleep 1                                          # gentle on secondary rate limit
    done

# Delete any leftover $BRANCH_PREFIX* refs (paranoia after -d). Same empty-input guard.
gh api "repos/$TARGET_REPO/git/refs/heads/regatta" --jq '.[].ref' 2>/dev/null \
  | while IFS= read -r ref; do
      [ -n "$ref" ] || continue
      case "$ref" in
        refs/heads/${BRANCH_PREFIX}*) gh api -X DELETE "repos/$TARGET_REPO/git/$ref" || break ;;
        *) echo "skip non-$BRANCH_PREFIX ref: $ref" ;;            # never delete operator work
      esac
    done

# Local agent worktrees (separate from target repo). Orchestrator MUST be stopped before this.
git worktree list | awk '/agent-/ {print $1}' \
  | while IFS= read -r path; do [ -n "$path" ] && git worktree remove --force "$path"; done
git worktree prune
```

NEVER use any deletion command without the `TARGET_REPO != PREFLIGHT_TARGET` guard. NEVER delete branches that don't match the `$BRANCH_PREFIX` prefix — operator-authored work on the sandbox repo is NOT in scope.

## Containment rules

The orchestrator spawns real agents that open real PRs and burn real API quota.

1. **Sandbox target only.** Production-shaped name → require typed `confirm:<owner>/<name>` (or `AskUserQuestion` when available) before proceeding.
2. **Parallel cap = 3.** If discovered `$PARALLEL_CAP_CFG_KEY` > 3, reduce in config BEFORE first launch (config is read at startup). Quota dies at 5+.
3. **Kill-switch in first narration line.** `kill <PID>` (or `systemctl --user stop $SYSTEMD_UNIT` when discovered).
4. **Token budget per loop.** ~20 tool calls per snapshot iteration; finish current phase then hand back. Spawn subagent if larger sweep needed.
5. **Pre-flight refusal.** Unknown `port` OR `db` OR `pid` → "pre-flight incomplete" + hand back.

### Pre-action self-check (every tool call)

Before any mutating tool call, ask: am I about to (a) merge a PR, (b) disable branch protection, (c) mutate target's `main`, (d) force-push, (e) `--admin` anything, (f) bypass a `scripts/check-*.sh`, (g) edit orchestrator source from the running binary's checkout? If yes → STOP, file HIGH `[OPS]` finding, hand back.

WHY these are forbidden: `--admin` overrides branch protection (wedged target unrecoverable without ops); `--no-verify` hides failures the worker re-runs anyway; force-push to `$BRANCH_PREFIX*` loses the agent's heartbeat anchor (reaper cannot reconcile); editing the running binary's checkout risks unstaged leak into the next rebuild. Worktree isolation per the tight-feedback-loop section is the canonical path for any operator edit.

## Activation

**Explicit triggers.** The user says any of:
- "operate regatta", "regatta operator mode"
- "babysit the swarm", "watch regatta"
- "run regatta on <repo>"
- `/regatta-operator` or any direct invocation

**Implicit triggers.** Self-offer (don't start) when:
- User mentions a long-running regatta session and asks "how's it going"
- User pastes a dashboard screenshot showing ≥2 in-flight agents
- User asks "what should regatta do next"

## The four surfaces

Every observation maps to **exactly one** of these. Title prefix is mandatory:

| Surface | Prefix | Examples |
|---|---|---|
| `regatta-core` | `[CORE]` | binary bugs, state-machine defects, schema drift, CI gates that fire wrong, polling/transport design |
| `orchestrator` | `[ORCH]` | dispatch logic, parallel-cap accounting, reaper, prwatch, spawner |
| `operator-loop` | `[OPS]` | CLAUDE.md rules, dispatch templates, boot prompts, this skill |
| `agent-prompt` | `[AGENT]` | implementer/reviewer/designer/triage prompt drift, worker-side trap repetition |

Misclassifying a finding wastes a fix — a prompt-drift bug shipped as a `[CORE]` patch never closes. When in doubt, file under `[OPS]` and note "surface uncertain" — triage gate sorts it.

## Observation channels

Cheap-first (PR sweep before logs before DB). Silence on a channel that should be chatty is itself a finding.

| Channel | Command shape (uses pre-flight values) | What it tells you |
|---|---|---|
| Dashboard health | `curl -sf "http://localhost:${PORT}/healthz" \| jq` | JSON: orchestrator status (ok/degraded), db, heartbeat freshness, brief presence |
| Dashboard agents | `curl -sf "http://localhost:${PORT}/ui/panels/agents"` (HTML) | Agent state, last heartbeat, current PR. Note: regatta UI is HTML panel, not JSON `/api/agents`; for structured data use the State DB row or `/ui/drawer/agent/<id>` |
| GH PR sweep | `gh pr list -R "$TARGET_REPO" --json number,headRefName,state,mergeStateStatus,statusCheckRollup,isDraft -L 20` | What landed, what's stuck, CI flake pattern |
| Agent logs | `tail -F .claude/worktrees/agent-*/logs/*.log` (or whatever the spawner discovered) | Worker reasoning, prompt drift, tool-denial loops |
| Orchestrator logs | `journalctl -u $SYSTEMD_UNIT -f` if systemd; else `tail -F` discovered file; else attach to PID's fd/2 | State transitions, reaper events, prwatch warnings |
| State DB | `sqlite3 "$DB" 'select kind,count(*) from events group by kind'` (distroless containers lack `sqlite3` — exec via sidecar: `docker run --rm -v <volume>:/data alpine sh -c 'apk add -q sqlite && sqlite3 /data/regatta.db "..."'` OR mount volume to host + run host-side sqlite3) | Event-vocabulary drift, idempotency failures |
| Heartbeat health | `gh pr view <N> --json mergeStateStatus,statusCheckRollup` | Pre-merge gate health per PR |
| Binary staleness | `go version -m "$(which regatta)"` or `docker inspect --format '{{.Image}}' <container>` | Confirm a rebuild actually took effect |
| State DB health | `sqlite3 "$DB" 'pragma integrity_check'` + `sqlite3 "$DB" "select max(ts) from events"` (distroless: sidecar pattern same as State DB row above) | Lock orphans, recent event freshness |


## Wedge sourcing (FEED responsibility — co-equal w/ OBSERVE)

The operator's first job is to keep the orchestrator's intake non-empty. Empty intake = idle agents = wasted parallel headroom. Pick the next wedge from the sources below in priority order; stop at the first non-empty source per tick.

Sources, highest → lowest:

1. **Ready-labeled backlog.** `gh issue list -R "$ORCH_SOURCE_REPO" --label "$SPAWN_LABEL" --state open --json number,title,labels,body -L 20`. Already triaged + scoped; safe to dispatch as-is. The orchestrator's poller picks these up on its next tick.
2. **Brief-ready specs.** `git ls-tree -r origin/main docs/engineer/briefs/ docs/engineer/specs/` — frontmatter `status: ready` (specs) OR brief w/ explicit `## Acceptance criteria` block. File an issue w/ brief link + `$SPAWN_LABEL` so orchestrator consumes.
3. **Milestones.** `gh api "repos/$ORCH_SOURCE_REPO/milestones" --jq '.[] | select(.state=="open") | {number,title,due_on}'` → for the soonest-due open milestone, `gh issue list -R "$ORCH_SOURCE_REPO" --milestone <N> --state open --label "ready" --json number,title -L 20`. Pick one; if it has a brief, dispatch directly; if not, dispatch a designer-subagent issue first (`[OPS] design brief: <topic>` w/ `$SPAWN_LABEL`).
4. **Roadmap active-phase items.** Walk `$ROADMAP_PATHS` for the `[IN-FLIGHT]` phase block: `for p in $ROADMAP_PATHS; do [ -f "$p" ] && grep -nE '^(P[0-9]+|PHASE).*\[IN-FLIGHT\]' "$p"; done` ( `[ -f ]` guard is REQUIRED — unquoted glob expansion fails-loud under `zsh` `nomatch` when a path pattern matches nothing; `[ -f ]` skips silently. ERE pipe is bare `|`, NOT `\|`). Pick the topmost item w/ no linked open PR + no linked open issue. File as a brief-stub issue.
5. **Self-improve detector backlog.** `[OPS]` / `[AGENT]` findings the skill already filed this session that are scoped + still open AND have ≥2 INDEPENDENT observations (separate agents / separate iterations / separate root-cause traces — NOT 2 comments on the same issue). Recurrence-only counts (single root cause seen N times on one agent) bump the existing issue's occurrence counter per recurrence rule; they do not become new wedges. Circuit-breaker: if a source-5 wedge produces a finding that itself becomes a source-5 candidate w/in 3 ticks, halt source-5 sourcing for the session + file `[OPS]` finding "recursive wedge loop detected".

**Wedge dispatch shape.** Every operator-filed wedge issue MUST have: (a) title w/ surface prefix `[CORE]/[ORCH]/[OPS]/[AGENT]`, (b) `## Brief` linking to a doc OR inline 5–15 line scope, (c) `## Acceptance criteria` bulleted list — these are the test-derivable specs the orchestrator's implementer subagent will write the failing test against per CLAUDE.md `feedback_tdd_discipline` (RED commit first; brief + criteria MUST be specific enough that a failing test compiles + fails on `main` for the right reason), (d) `## File scope` glob list (use w/ shared-primitive owner audit per CLAUDE.md), (e) `$SPAWN_LABEL` label applied. Wedges touching load-bearing surfaces (per CLAUDE.md reviewer-verdict gate path list) carry `## Downstream review` note: "PR will require independent reviewer per `feedback_no_self_tagged_approve` — `Reviewer-agent-id:` + `Reviewer-recommendation: APPROVE` mandatory in PR body footer". Operator does not enable automerge per `feedback_no_implementer_automerge`.

**Audit main before filing** (per CLAUDE.md `feedback_audit_main_before_implementing`). Before filing ANY wedge: `git ls-tree -r origin/main --name-only | grep -E '<expected-path>'` AND `gh pr list -R "$ORCH_SOURCE_REPO" --search "in:title <wedge-keyword>" --state all -L 5 --json number,state,mergedAt`. If shipped → skip wedge + close source item w/ "shipped in #<PR>". Wastes orchestrator dispatch otherwise.

**Self-host filter applies.** Mechanism: skip a candidate iff `gh issue view <N> --json labels --jq '.labels[].name'` returns ANY label appearing in `$defer_labels` (target YAML, default `[phase-x, phase-x-forward-fit, wontfix, deferred]`). Distinct from `check-phase-x-leak.sh` which gates spec frontmatter in source files — that gate runs in CI on the orchestrator-source repo; this filter runs on issue / spec / roadmap CANDIDATES before they become wedges.

**Roadmap-empty ≠ ship anything.** If sources 1–4 are all empty AND source 5 is empty, do NOT manufacture work. Mark the queue exhausted; exit predicate fires after N quiet ticks.

## Run loop

Two co-equal primary phases per tick: FEED + OBSERVE. Three support phases. One narration line per phase.

1. **FEED.** Top up to queue-healthy in one tick — file up to `max(0, $PARALLEL_CAP - $UNCLAIMED)` wedges per §Wedge sourcing, not one. `$UNCLAIMED` = `gh issue list -R "$ORCH_SOURCE_REPO" --label "$SPAWN_LABEL" --state open --search "no:assignee" --json number -L 50 | jq length` (open + `$SPAWN_LABEL` + no assignee — GH search supports `no:assignee` but NOT `no:linked-pr`; over-count when an issue has a linked PR is acceptable false-positive — orchestrator's own dedupe handles it, and the issue auto-closes when the PR merges). Single-wedge-per-tick under-fills when agents consume faster than the operator tick interval; top-up matches throughput. Files as `$SPAWN_LABEL`-labeled issues in `$ORCH_SOURCE_REPO`. Skip entirely if `$UNCLAIMED >= $PARALLEL_CAP`.
2. **OBSERVE.** Snapshot current state from available channels in one parallel sweep. Diff against previous snapshot kept in `$CLAUDE_JOB_DIR/regatta-snapshot.json` when `$CLAUDE_JOB_DIR` is set; otherwise `$TMPDIR/regatta-snapshot.json`. First iteration has no diff — state "cold start". Per-role canary replay due → run inside this phase per heartbeat cadence.
3. **Classify + triage.** For each delta + each finding: expected progress / known-recurrence / new finding. New finding gets surface + severity (CRIT/HIGH/MED/LOW) + smallest reproducer. If HIGH+ AND blocks the running session → fix-in-place via worktree + small targeted PR. Otherwise → file issue. Every defect filed = future wedge for FEED phase next tick.
4. **Act.** EITHER (a) file GitHub issue w/ surface prefix, (b) draft memory entry under `~/.claude/projects/-Users-treedesk-Desktop-Projects-regatta/memory/` if operator-loop lesson, OR (c) draft CLAUDE.md candidate rule if universal-agent lesson. NEVER both for the same finding — pick the surface.
5. **Continue OR exit.** Exit predicates (in priority): (a) operator interrupt, (b) recurring-trap fire per `feedback_trap_projection` → hand back for root-cause, (c) wedge queue empty AND zero new observations across `QUIET_TICKS=3` consecutive ticks. Otherwise schedule next tick via `ScheduleWakeup` when (i) harness exposes it AND (ii) operator said "autonomous" / "loop" / "keep watching"; else hand back one-shot.

End every iteration with: `result: operator loop <N> — <wedges-filed> fed, <findings> new, <issues> filed, <fixes> pushed, green-clock=<days>, quiet-streak=<K>/3`.

## Recurrence rule (CRITICAL — don't spam tracker)

Same root cause hits ≥ 2 agents / iterations → exactly ONE tracker issue, bump occurrence counter via PR-comment on that issue. NEVER file a second issue for the same root cause. Conflicts with naive "Nth occurrence" loops; this rule wins.

Implementation: before filing, search BOTH `$ORCH_SOURCE_REPO` and `$TARGET_REPO` (the orchestrator's self-improve detector may have filed against either): `for R in "$ORCH_SOURCE_REPO" "$TARGET_REPO"; do gh issue list -R "$R" --search "<root-cause-keyword>" --state all -L 5 --json number,title,state; done`. If an open issue matches → comment with "occurrence N: <PR or agent ref>". If a closed issue matches → reopen with "regression detected: <PR ref>". If none match → file in the repo per the routing rule above.

This rule beats the generic "repeat trap" trigger. `feedback_trap_projection` says recurring trap is a structural defect — N tracker issues is itself the trap.

## Meta-learning triggers

- **Repeat trap.** Same prompt-drift / same CI gate / same orchestrator race firing on ≥2 agents → ONE issue (see recurrence rule), then file `[OPS]` lesson candidate about whether the gate/prompt needs a structural fix.
- **Silent failure mode.** Agent goes idle without an error event → `[CORE]` event-vocabulary gap. Reaper should have caught it.
- **Operator surprise.** YOU were surprised by what regatta did (or didn't do) → `[OPS]` lesson candidate. Surprise = unwritten rule.
- **PR pattern shift.** Mock-vs-real ratio crosses 70% (read from `[BUG-1088]` gate output if wired, else skip), comment density crosses 5%, or any `check-*.sh` gate fires more this session than last → `[OPS]` if signal is real, `[CORE]` if gate is wrong.
- **Subagent over-claim.** Subagent report spot-checked and wrong → `[AGENT]` prompt fix. Per `feedback_validate_before_ship`.

After 3 findings in a session, write session-end summary under `$CLAUDE_JOB_DIR/regatta-session-<date>.md` (or `$TMPDIR` fallback) with surface breakdown + top-1 candidate rule to codify next session.

## Things this skill notes but does not fix

These are durable-design items. File as `[CORE]` issues when relevant; do NOT touch in operator session.

- **Polling vs webhooks.** Regatta polls GH. Sub-`poll_interval` latency is unreachable without a transport change. Improvement ladder: (1) ETag conditional GET → 304s don't count rate-limit, (2) adaptive poll backoff, (3) GH events API single stream, (4) smee.io / webhook-relay hybrid (push notifies, regatta still pulls detail) — no public ingress, (5) full webhook + tunnel + HMAC + dedup store. Self-host phase: (1) + (2) is the smallest win; (4) is the right next step when latency complaint surfaces.

## Bottleneck-resolution loop

A bottleneck = a finding that blocks further observation (orchestrator wedged, dispatch loop runaway, auth precondition unresolvable, port unbound). Standard "file issue + move on" does not apply — the next snapshot is meaningless until the bottleneck clears.

When a finding is flagged `bottleneck=true`:

1. **STOP observation.** Halt the run loop; further snapshots add noise, not signal.
2. **File the issue normally** (with surface prefix + `$SPAWN_LABEL`).
3. **Spawn adversarial reviewer subagent** with the finding body. Reviewer hunts: is the proposed fix the right shape? Smallest? Reversible? Per `feedback_adversarial_review_every_step`.
4. **Apply reviewer's narrowest fix** in a worktree. If reviewer says BLOCK, ask `AskUserQuestion` (this IS an exception to auto-act — bottlenecks are irreversible-shaped).
5. **Verify in the live stack.** Rebuild + restart per the post-merge cycle below; replay the canary that hit the bottleneck. Verify in the live stack via the bounded CI poll above; never use unbounded `until SUCCESS` loops.
6. **If still bottlenecked → repeat** from step 3 with a fresh reviewer. Track iteration count; ≥3 attempts without resolution = escalate to operator via `AskUserQuestion`.
7. **Resolved → close the issue** + write a follow-up canary fixture in `$CANARY_DIR_<role>` so the bottleneck cannot silently regress.
8. **Resume observation** from a clean snapshot. Discard the pre-bottleneck baseline — orchestrator behavior pre/post-fix is not comparable.

## Post-merge rebuild-and-observe (regular cycle)

When a PR merges into `$ORCH_SOURCE_REPO`, the running orchestrator is now STALE relative to main. The skill must rebuild + restart + observe at every merge to confirm the new binary does not regress autonomy. Cadence: every merge to the orchestrator-source repo, OR every heartbeat if multiple merges land between heartbeats.

```bash
# 1. Pull latest. Refuse on unclean tree.
git -C "$ORCH_CHECKOUT" fetch origin main && \
  git -C "$ORCH_CHECKOUT" diff --quiet HEAD origin/main || \
  { git -C "$ORCH_CHECKOUT" status -s; echo "dirty; abort"; exit 1; }
git -C "$ORCH_CHECKOUT" pull --ff-only origin main

# 2. Build (per build-method; see tight-loop step 3).
docker compose --env-file "$ENV_FILE" build "$DOCKER_SERVICE"
NEW_SHA=$(docker compose --env-file "$ENV_FILE" images "$DOCKER_SERVICE" -q)

# 3. Graceful restart (see tight-loop step 4).
docker compose --env-file "$ENV_FILE" up -d "$DOCKER_SERVICE"

# 4. Confirm binary changed (see tight-loop step 5).
RUNNING_SHA=$(docker inspect --format '{{.Image}}' "$DOCKER_SERVICE" | cut -d: -f2 | head -c12)
[ "$NEW_SHA" = "$RUNNING_SHA" ] || { echo "binary unchanged"; exit 1; }

# 5. Smoke-watch 60s for the spawn-loop bottleneck pattern.
# If >K=5 agent.exited events arrive within 30s with same fingerprint → flag bottleneck.
docker compose --env-file "$ENV_FILE" logs --since=30s "$DOCKER_SERVICE" \
  | grep -c 'agent.exited' | awk '{ if ($1 > 5) print "bottleneck: spawn loop"; else print "ok" }'

# 6. Replay all per-role canaries (see canary section).
# 7. Write metrics → $BASELINE_FILE.
# 8. Resume normal observation.
```

If any step fails: bottleneck-resolution loop fires.

## Operator-delegated merge

Default per CLAUDE.md `feedback_no_implementer_automerge`: skill does NOT enable automerge, does NOT merge PRs. The operator owns the merge button. This holds when the human is sitting at the keyboard.

BUT: in autonomous operation (operator said "loop" / "autonomous" / "keep going" / "merge when green") the human has explicitly delegated merge authority for THIS session. In that mode, after ALL conditions are met for a PR the skill itself opened:

1. Independent adversarial reviewer returned `Reviewer-recommendation: APPROVE` (real subagent ID in PR body, NOT self-tagged — per `feedback_no_self_tagged_approve`). Reviewer prompt uses the five-lens prompt at `docs/engineer/dispatch-templates/reviewer.md` (defects + simplification + refactor + comments + organization), not defect-only.
2. `gh pr view <N> --json state,mergeStateStatus,statusCheckRollup` shows `state=OPEN`, `mergeStateStatus=CLEAN` (not BLOCKED / DIRTY / UNSTABLE / UNKNOWN / HAS_HOOKS). Pick latest-by-`completedAt` per name; entries with `completedAt=null` are PENDING → wait, do not merge. Required: every name has ≥1 entry AND latest is `SUCCESS`.
3. **Skill-opened PR detection.** Skill writes `Skill-session-id: <session>` into the PR body footer when opening AND prepends `feat/skill-` / `fix/skill-` / `chore/skill-` to the branch name. Merge gate requires BOTH (token match AND branch prefix); either alone fails. Operator-authored PRs lack both.

Skill executes `gh pr merge <N> --squash --delete-branch`, logs the merge in `$BASELINE_FILE` with merging-skill-session ID so the green-clock counter advances, then runs the post-merge rebuild-and-observe cycle. The local branch (if present) is pruned via `git branch -D <branch>` after the remote delete; if any local worktree is still on the branch, switch it to `main` first OR skip the local-prune step + emit `[OPS]` finding "skill-branch leak: <name>".

Hard refusals (operator delegation does NOT extend to):
- `--admin` flag (overrides branch protection).
- `--auto` flag (enables automerge per `feedback_no_implementer_automerge`; skill executes the actual merge, never schedules it).
- `--merge` (non-squash commit) or `--rebase` (rewrites linear history) — skill is squash-only.
- Force-merge through DIRTY / BLOCKED / UNSTABLE / HAS_HOOKS / UNKNOWN state.
- Merging a PR the skill did NOT open (failed token+prefix detection above).
- Force-push on a merge conflict — always REBASE locally, never overwrite remote.
- Merging into `$TARGET_REPO`'s `main` when `$TARGET_REPO` != `$ORCH_SOURCE_REPO`. Target-side merges always require operator (no reliable in-skill mechanism to scope canary-fix vs broader change; safer-default until labels-as-marker land).

Bottleneck-resolution loop fix PRs follow the same gate. Skill-opened PRs only; never operator-authored PRs.

### Bounded CI poll (mandatory pattern)

**Failure mode.** Open-ended `until CLEAN; sleep; done` loops silently hang on FAILED CI runs. The PR sits BLOCKED, the gate condition (`mergeStateStatus=CLEAN`) is never reached, the loop polls forever, and the failure is never reported to the operator — the bottleneck-resolution loop never triggers. Observed self-evidence: regatta-operator skill session 5 hit this trap on PRs #1183 / #1184 / #1185 (Jun 9, 2026). Three PRs sat BLOCKED while the skill silently polled.

**Mandatory pattern.** Every CI poll loop MUST:

1. Check for ANY failure conclusion at each tick and break on first failure.
2. Report the failure summary back to the operator within 1 tick.
3. Cap total iterations to `MAX_TICKS=10` with an explicit bounded counter (`i=0; while [ $i -lt 10 ]`) and hand-back message on cap (never poll indefinitely, never use bare `until ... done`).
4. Use REST-backed `gh pr checks <N>` for per-tick status reads — NOT GraphQL `gh pr view --json statusCheckRollup`. `gh pr checks` is REST under the hood and unifies BOTH GitHub Actions check_runs AND legacy commit-status entries (e.g. `pr-lint`-style statuses) into one `bucket` field (`pass`/`fail`/`pending`/`skipping`/`cancel`) — matching what the operator visually sees in the PR UI. Avoid the raw `gh api repos/.../commits/<sha>/check-runs` endpoint: it returns ONLY check_runs and silently omits commit-status entries, so legacy statuses look "missing" and the poll declares CLEAN prematurely. The GraphQL `statusCheckRollup` field nests deeply and costs ~15+ rate-limit units per call; `gh pr checks` costs ~3 units in the REST core bucket. A 4-PR sweep every 60s for 18 ticks via GraphQL burns ~1080 units; the same sweep via `gh pr checks` burns ~216 units — ~5× headroom. 2026-06-10 session depleted the 5000/hr GraphQL quota in one autonomous run, forcing a 26-min `ScheduleWakeup` pause. Reserve GraphQL `gh pr view --json mergeStateStatus,state` for the single-shot terminal merge-gate check (one call per PR, end of poll session).
5. Re-fetch `headRefOid` every 3 ticks (or check `gh pr view --json updatedAt`) — if the PR force-pushes mid-poll, a stale SHA polls forever against an abandoned commit and never observes the new CI runs. The bounded counter + `gh pr checks <N>` form below sidesteps this entirely (no SHA pinned client-side; `gh` resolves PR → current head on every call).

```bash
# Bounded, fail-fast CI poll. Uses gh pr checks (REST-backed, unifies check_runs + statuses).
# No client-side SHA pin — gh resolves <N> to the current head each tick, so force-pushes are seen.
i=0
while [ $i -lt 10 ]; do
  i=$((i+1))
  CHECKS=$(gh pr checks <N> --json name,state,bucket 2>/dev/null)
  fails=$(echo "$CHECKS" | jq -r '[.[]|select(.bucket=="fail" or .bucket=="cancel")|.name]|join(",")')
  if [ -n "$fails" ]; then echo "FAIL on PR <N>: $fails"; break; fi
  pending=$(echo "$CHECKS" | jq -r '[.[]|select(.bucket=="pending")]|length')
  if [ "$pending" = "0" ]; then echo "CLEAN on PR <N>"; break; fi
  sleep 60
done
[ $i -ge 10 ] && echo "MAX_TICKS=10 reached on PR <N>; handing back to operator"
```

`gh pr checks --json` fields (verified against `gh` 2.x): `bucket`, `completedAt`, `description`, `event`, `link`, `name`, `startedAt`, `state`, `workflow`. `bucket` is the categorized signal (`pass`/`fail`/`pending`/`skipping`/`cancel`) — use it, not raw `state`/`conclusion`, because it normalizes check_runs `conclusion` and status `state` into one field. Failure detection feeds the bottleneck-resolution loop.

## Reviewer prompt shape

Every reviewer subagent the skill dispatches uses the five-lens prompt at `docs/engineer/dispatch-templates/reviewer.md` (defects + simplification + refactor + comments + organization). Defect-only reviews are forbidden as default — they missed 134 LOC of redundancy on this skill's own development. Template is authoritative; this skill defers.

## Hand-off

When ending a session (operator interrupt or natural stop): produce ONE summary block with:
- target repo
- session duration
- pre-flight values (pid, port, db, poll, cap)
- findings by surface (count + 1-line each)
- issues filed (numbers)
- fixes pushed (PR numbers)
- top candidate rule for next codification round

That summary is the entire session output the operator needs to keep. Everything else is intermediate state.
