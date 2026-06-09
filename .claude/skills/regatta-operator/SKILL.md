---
name: regatta-operator
description: Act as the human-in-the-loop operator of regatta running against a target repo. Monitor the running orchestrator, observe agent/PR/CI state by all available means, notice inefficiencies and bugs, and file issues + capture meta-lessons across the four surfaces (regatta-core, orchestrator, operator-loop, agent-prompt). Use when the user says "operate regatta", "babysit regatta", "regatta operator mode", "run regatta on <repo>", "watch the swarm", or any phrasing that asks Claude to drive a regatta session end-to-end. The skill takes a TARGET_REPO argument naming the GitHub repo regatta will work against; defaults to the in-tree test target. Containment first — every action is scoped, reversible, and logged.
---

# regatta-operator

Operator of `regatta` — the agent-orchestration binary. **GOAL = AUTONOMY.** The orchestrator is the worker; the human is the *exception path*, not the *default path*. This skill exists so the human stays OUT of the loop while regatta builds / reviews / designs / merges against a target repo. Skill's job: keep the conditions for autonomy intact + detect when autonomy breaks + auto-file the fix-request as a `[autonomous]`-labelled issue regatta can consume.

NOT this skill's job: write regatta code, merge PRs, or substitute for the orchestrator. If you find yourself doing the orchestrator's work, the autonomy loop is broken — file the breakage instead of papering over it.

### Autonomy mandate

- **Default state = silent.** In autonomous mode (operator said "autonomous" / "loop" / "keep watching"), skill emits one heartbeat per N hours (`heartbeat_interval`, default 4h) summarizing all iterations between heartbeats; per-phase narration is suppressed. In one-shot mode (operator said "snapshot" / "check" / "report"), skill narrates one line per phase per the run loop and hands back. The per-phase narration rule in the run loop applies to one-shot mode; the heartbeat rule applies to autonomous mode. Explicit operator request wins; default when ambiguous = one-shot.
- **Auto-act, don't ask.** Every finding routes to one of: (a) auto-filed `[autonomous]`-labelled issue (regatta consumes), (b) auto-comment on existing tracker issue (recurrence), (c) auto-updated baseline file (intended drift). `AskUserQuestion` is reserved for genuinely irreversible decisions only.
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
  spawn_label: '[autonomous]'                         # GH-issue label the adapter consumes
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

Read `$BASELINE_FILE` at pre-flight; write at hand-off:
```json
{ "session_id": "<id>", "ts": "<unix>",
  "metrics": { "build_green_rate_impl": 0.94, "tdd_order_rate_impl": 0.81, ... },
  "phase": "<active-roadmap-phase>", "green_clock_days": 7 }
```

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

PRIMARY USE: the orchestrator's OWN self-improve detector landed a change to its source; skill verifies the new binary is live and the canary suite still passes. SECONDARY USE: an operator (you-the-human or a one-shot skill invocation) makes a deliberate change. If neither, skip this section — autonomous loop runs without it. Default recipe applies to both:

1. **Checkpoint state DB** before edit. SQLite with WAL is multi-file (`-wal`, `-shm`); a bare `cp` of `$DB` while a writer is connected yields a corrupt snapshot. A bare `PRAGMA wal_checkpoint` + `cp` sequence also races — if the writer resumes between checkpoint and copy, the main DB and WAL drift. The safe primitive is `.backup`, which acquires the necessary locks internally:
   ```bash
   TS=$(date +%s)
   sqlite3 "$DB" ".backup '$DB.ckpt-$TS'"            # atomic, lock-aware, single-file output
   sqlite3 "$DB.ckpt-$TS" 'pragma integrity_check' | head -3
   ```
   `.backup` produces ONE file with no sidecars needed for restore. If `.backup` is unavailable (very old sqlite3 CLI), fall back to: stop the orchestrator first (`kill -TERM <PID>` + wait for exit), THEN `cp` all three of `$DB`, `$DB-wal`, `$DB-shm` together. Never `cp` while a writer is live.
2. **Edit in a worktree.** Operator-side edits MUST land in a separate working tree so the running binary's checkout stays untouched. Two reasons: (a) on rebuild the operator's worktree is the build input, and a clean baseline is recoverable via `git switch -`; (b) the running process holds its text segment in RAM, so source edits do not affect it until the next launch — editing in the binary's own checkout offers zero runtime benefit and adds risk of an unstaged change leaking into the next rebuild.
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
5. **Confirm the binary actually changed.** Restart picking up the old binary is the most common silent failure of this loop. `which $ORCH_BINARY` may resolve to a shim or shell alias — verify it matches the running PID's executable.
   - Resolve the running binary path. Linux: `readlink -f /proc/<PID>/exe`. macOS: `lsof -p <PID> -Fn` emits a TWO-line record per fd — `ftxt` then `n<path>` on the next line; parse with `awk '/^ftxt/{getline n; sub(/^n/,"",n); print n; exit}'`. Use THIS resolved path, not `$(which $ORCH_BINARY)` — `which` may resolve to a shim or a stale `$PATH` entry.
   - `go-install`: `go version -m "<resolved-path>" | head -5` — compare `mod` / `vcs.revision` / `vcs.time` lines pre/post. Requires the binary to embed module info (default with module mode + `go install`).
   - `docker`: `docker inspect --format '{{.Image}}' <container>` — must match the SHA you built in step 3.
6. **Stop the orchestrator BEFORE wiping agent worktrees.** Wiping while the orchestrator is mid-poll races the spawner: it may have just `mkdir`'d a new agent dir between your `git worktree list` and the `remove`, leaving an orphan. Sequence: graceful stop (step 4) → wipe → restart. Never wipe a live tree.

   The pre-check below confirms NO orchestrator process is running with the pre-flight PID. It does NOT prevent a different orchestrator instance from being launched by a parallel operator session between the check and the wipe — if that scenario is in scope, additionally verify the pre-flight `port` is unbound (`lsof -nP -iTCP:$PORT -sTCP:LISTEN | grep -q . && echo "port still bound" && exit 1`). The PID-identity check is sufficient for the single-operator self-host case (the rest of this skill's containment model).
   ```bash
   # Pre-check: orchestrator must be stopped — refuse if its PID still exists
   if kill -0 <PID> 2>/dev/null; then echo "$ORCH_BINARY still running; stop before wipe"; exit 1; fi

   # Empty-input safety: xargs with no input invokes the command once with literal {} on BSD/macOS;
   # GNU xargs needs --no-run-if-empty (or -r), BSD needs nothing because it has no -I-empty-skip.
   # Use `while read` to be portable.
   git worktree list | awk '/agent-/ {print $1}' \
     | while IFS= read -r path; do [ -n "$path" ] && git worktree remove --force "$path"; done
   find .claude/worktrees -maxdepth 1 -name 'agent-*' -type d -exec rm -rf {} + 2>/dev/null
   git worktree prune
   ```
7. **Replay a canary.** Re-trigger the same synthetic input you used before the edit. Two options:
   - Re-open the same sandbox-repo issue that triggered the prior run: `gh issue reopen <N> -R "$TARGET_REPO"` then add a comment "canary-replay-<unix-ts>".
   - File a fresh canary issue from a templated body that pins `canary: <reason> ts=<unix>` so subsequent diffs are mechanically correlatable.
8. **Observe.** `tail -F` on a log file breaks across restart when the file is recreated, and `journalctl -fu` can also drop if the journal buffer rotated. Prefer the channel that survives:
   - systemd: `journalctl --user -fu $SYSTEMD_UNIT` (or system unit). Pair with `journalctl --user -u $SYSTEMD_UNIT --since "1 min ago" --no-pager` after restart so you can see the boot lines you missed.
   - docker compose: `docker compose logs -f --since=1m $DOCKER_SERVICE` — `-f` reconnects on container restart; `--since` recovers boot lines.
   - go-install / bare process: redirect the process to a known file at launch (`$ORCH_BINARY serve >/var/log/$ORCH_BINARY.log 2>&1`) then `tail -F /var/log/$ORCH_BINARY.log`. After restart, re-run `tail -F` because the inode changed.
   - Across all three, also note: the first line after restart should include the version/build info from step 5.

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

## Containment rules (READ FIRST)

Regatta spawns real agents that open real PRs and burn real API quota.

1. **Sandbox target repo only.** Never point at production. Production-shaped name (matches `^(anthropics|google|microsoft|.*-prod|.*-production)/`) → require explicit confirmation via `AskUserQuestion` when available, else state the risk and require operator typed "confirm:<owner>/<name>" in chat before proceeding. Do NOT silently proceed.
2. **Worktree isolation for operator edits.** Any operator edit to orchestrator source goes through `EnterWorktree` when available; otherwise `git worktree add .claude/worktrees/<slug> -b <slug>` manually and `cd` into it. Never edit orchestrator source from a checkout that is also running the binary.
3. **No `--no-verify`, no force-push, no `gh pr merge --admin`.** Why: each bypasses a gate the orchestrator itself enforces. `--admin` overrides branch protection so a wedged target repo cannot be recovered without ops. `--no-verify` skips local checks that the worker would re-run anyway → just hides the failure. Force-push to an orchestrator-spawned branch loses the agent's heartbeat anchor and the reaper cannot reconcile.
4. **Parallel cap = 3.** If discovered `ParallelCap` > 3, recommend reducing in config BEFORE first launch (not at runtime — config is read at startup). Quota dies at 5+.
5. **Kill-switch FIRST.** First narration line MUST include the discovered kill command: `kill <PID>` (or `systemctl --user stop regatta` if discovered).
6. **No live mutation of target's `main`.** Regatta opens PRs; merge gate decides. Skill files findings + opens issues; does not merge.
7. **Token budget per loop.** Soft target ~20 tool calls per snapshot iteration. After 20, halt the current loop iteration, write what you have, hand back. Do NOT continue in the same iteration; spawn a subagent if more is needed.
8. **Pre-flight refusal.** If pre-flight discovery returns "unknown" for `port` OR `db` OR `pid`, do NOT proceed to snapshot. State "pre-flight incomplete" + the missing values; hand back to operator.

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

Cheap-first. Stop the moment a channel goes silent when it should be chatty — that silence is itself a finding.

| Channel | Command shape (uses pre-flight values) | What it tells you |
|---|---|---|
| Dashboard | `curl -sf "http://localhost:${PORT}/api/agents" \| jq` | Agent state, last heartbeat, current PR |
| GH PR sweep | `gh pr list -R "$TARGET_REPO" --json number,headRefName,state,mergeStateStatus,statusCheckRollup,isDraft -L 20` | What landed, what's stuck, CI flake pattern |
| Agent logs | `tail -F .claude/worktrees/agent-*/logs/*.log` (or whatever the spawner discovered) | Worker reasoning, prompt drift, tool-denial loops |
| Orchestrator logs | `journalctl -u $SYSTEMD_UNIT -f` if systemd; else `tail -F` discovered file; else attach to PID's fd/2 | State transitions, reaper events, prwatch warnings |
| State DB | `sqlite3 "$DB" 'select kind,count(*) from events group by kind'` | Event-vocabulary drift, idempotency failures |
| Heartbeat health | `gh pr view <N> --json mergeStateStatus,statusCheckRollup` | Pre-merge gate health per PR |
| Binary staleness | `go version -m "$(which regatta)"` or `docker inspect --format '{{.Image}}' <container>` | Confirm a rebuild actually took effect |
| State DB health | `sqlite3 "$DB" 'pragma integrity_check'` + `sqlite3 "$DB" "select max(ts) from events"` | Lock orphans, recent event freshness |

Open PR sweep FIRST (cheapest, public, no infra). Escalate inward only when ambiguous.

## Run loop

Five phases. One narration line per phase.

1. **Snapshot.** Pull current state from available channels in one parallel sweep. Diff against the previous snapshot kept in `$CLAUDE_JOB_DIR/regatta-snapshot.json` when `$CLAUDE_JOB_DIR` is set; otherwise `$TMPDIR/regatta-snapshot.json`. First iteration has no diff — state "cold start".
2. **Classify.** For each delta: expected progress / known-recurrence / new finding.
3. **Triage.** For each new finding: surface + severity (CRIT/HIGH/MED/LOW) + smallest reproducer. If HIGH+ AND blocks the running session → fix-in-place via worktree + small targeted PR. Otherwise → file issue.
4. **Act.** EITHER (a) file GitHub issue with surface prefix, (b) draft memory entry under `~/.claude/projects/-Users-treedesk-Desktop-Projects-regatta/memory/` if operator-loop lesson, OR (c) draft CLAUDE.md candidate rule if universal-agent lesson. NEVER both for the same finding — pick the surface.
5. **Pause.** One-sentence status hand-back. Autonomous looping is NOT default; only schedule via `ScheduleWakeup` when (a) the harness exposes it AND (b) the operator explicitly said "autonomous" / "loop" / "keep watching".

End every iteration with: `result: operator loop <N> — <findings> new, <issues> filed, <fixes> pushed, green-clock=<days>`.

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

## Hard nos (pre-flight self-check before every action)

Before any tool call that mutates state, ask: am I about to (a) merge a PR, (b) disable branch protection, (c) mutate target's `main`, (d) force-push, (e) `--admin` anything, (f) bypass a `scripts/check-*.sh`? If yes → STOP, file the situation as HIGH `[OPS]` finding, hand back.

- **Does not merge PRs.** Branch protection + automerge gate + human review decide.
- **Does not bypass any `scripts/check-*.sh`.** Gates are operator authority; bypassing turns the operator into the bug.
- **Does not edit regatta config from inside the running binary's checkout.** Use a worktree.
- **Does not silently switch target repos.** State + confirm.
- **Does not write multi-page status reports.** One-line snapshots, one-line deltas, one issue per finding. The CLAUDE.md ceremony rule applies here too.

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
