---
name: regatta-operator
description: Act as the human-in-the-loop operator of regatta running against a target repo. Monitor the running orchestrator, observe agent/PR/CI state by all available means, notice inefficiencies and bugs, and file issues + capture meta-lessons across the four surfaces (regatta-core, orchestrator, operator-loop, agent-prompt). Use when the user says "operate regatta", "babysit regatta", "regatta operator mode", "run regatta on <repo>", "watch the swarm", or any phrasing that asks Claude to drive a regatta session end-to-end. The skill takes a TARGET_REPO argument naming the GitHub repo regatta will work against; defaults to the in-tree test target. Containment first — every action is scoped, reversible, and logged.
---

# regatta-operator

Operator of `regatta` — the agent-orchestration binary. Your job is to **run** regatta against a target repo, watch it work, and turn what you see into issues, learnings, and small targeted fixes. NOT to write regatta code in this session.

## Inputs

Skill argument: `TARGET_REPO=<owner>/<name>`. If unset, ask via `AskUserQuestion` when available; otherwise prompt the operator in the next message and stop. Persist the choice for the session.

Do NOT silently switch target repos mid-session. Switching invalidates the observation baseline.

## Pre-flight (REQUIRED before first snapshot)

Run discovery FIRST. Hardcoded values are bugs.

```bash
# Find running regatta + its port
pgrep -fl 'regatta serve'                          # PID + cmdline
lsof -nP -iTCP -sTCP:LISTEN -p <PID> 2>/dev/null   # listening port
# OR fallback:
ss -lntp 2>/dev/null | grep regatta || \
  netstat -anv -p tcp | grep LISTEN | grep regatta

# Find state DB
find . -maxdepth 4 -name '*.db' -o -name '*.sqlite*' 2>/dev/null | head -5
# Or check serve config:
git grep -nE 'state.*\.db|sqlite|StatePath' cmd/ internal/ | head -10

# Find log destination
git grep -nE 'log\.Open|os\.Stderr|journal|slog\.New' cmd/regatta/serve.go | head -10
# Try systemd, fall back to stderr capture
journalctl -u regatta -n 1 --no-pager 2>/dev/null || \
  echo "no systemd; tail wherever serve.go writes"

# Discover poll interval + parallel cap
git grep -nE 'PollInterval|TickInterval|ParallelCap|MaxConcurrent' internal/ cmd/
```

State the four discovered values in the first narration line:
`pre-flight: pid=<N> port=<P> db=<path> poll=<interval> cap=<N>`

If ANY value is unknown, state "unknown" — do NOT guess. Unknown poll-interval means you cannot distinguish lag from stuck (next section).

## Polling cadence (read this BEFORE diagnosing "stuck")

Regatta polls GitHub. It does NOT receive webhooks. This means:

- **Lag** = up to `poll_interval` seconds between GH state change and regatta noticing. Expected. NOT a bug.
- **Stuck** = ≥ 3 × `poll_interval` with no `prwatch.*` or equivalent event in logs. File `[ORCH]` finding.
- **Silent stall** = poll runs but no state transitions over multiple intervals despite open work. File `[CORE]` finding.

When you cannot find a `PollInterval` config or the value is unknown, treat all "is it stuck?" diagnoses as low-confidence and say so in narration.

Tracking issues for polling-vs-webhook design improvements live under the `[CORE]` surface (see "Things this skill notes but does not fix" below).

## Containment rules (READ FIRST)

Regatta spawns real agents that open real PRs and burn real API quota.

1. **Sandbox target repo only.** Never point at production. Production-shaped name (matches `^(anthropics|google|microsoft|.*-prod|.*-production)/`) → require explicit confirmation via `AskUserQuestion` when available, else state the risk and require operator typed "confirm:<owner>/<name>" in chat before proceeding. Do NOT silently proceed.
2. **Worktree isolation for operator edits.** Any operator edit to regatta source goes through `EnterWorktree` when available; otherwise `git worktree add .claude/worktrees/<slug> -b <slug>` manually and `cd` into it. Never edit regatta source from a checkout that is also running the binary — the running process holds stale state.
3. **No `--no-verify`, no force-push, no `gh pr merge --admin`.** Why: each bypasses a gate regatta itself enforces. `--admin` overrides branch protection so a wedged target repo cannot be recovered without ops. `--no-verify` skips local checks that the worker would re-run anyway → just hides the failure. Force-push to a regatta-spawned branch loses the agent's heartbeat anchor and the reaper cannot reconcile.
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
| Regatta logs | `journalctl -u regatta -f` if systemd; else `tail -F` discovered file; else attach to PID's fd/2 | State transitions, reaper events, prwatch warnings |
| State DB | `sqlite3 "$DB" 'select kind,count(*) from events group by kind'` | Event-vocabulary drift, idempotency failures |
| Heartbeat health | `gh pr view <N> --json mergeStateStatus,statusCheckRollup` | Pre-merge gate health per PR |

Open PR sweep FIRST (cheapest, public, no infra). Escalate inward only when ambiguous.

## Run loop

Five phases. One narration line per phase.

1. **Snapshot.** Pull current state from available channels in one parallel sweep. Diff against the previous snapshot kept in `$CLAUDE_JOB_DIR/regatta-snapshot.json` when `$CLAUDE_JOB_DIR` is set; otherwise `$TMPDIR/regatta-snapshot.json`. First iteration has no diff — state "cold start".
2. **Classify.** For each delta: expected progress / known-recurrence / new finding.
3. **Triage.** For each new finding: surface + severity (CRIT/HIGH/MED/LOW) + smallest reproducer. If HIGH+ AND blocks the running session → fix-in-place via worktree + small targeted PR. Otherwise → file issue.
4. **Act.** EITHER (a) file GitHub issue with surface prefix, (b) draft memory entry under `~/.claude/projects/-Users-treedesk-Desktop-Projects-regatta/memory/` if operator-loop lesson, OR (c) draft CLAUDE.md candidate rule if universal-agent lesson. NEVER both for the same finding — pick the surface.
5. **Pause.** One-sentence status hand-back. Autonomous looping is NOT default; only schedule via `ScheduleWakeup` when (a) the harness exposes it AND (b) the operator explicitly said "autonomous" / "loop" / "keep watching".

End every iteration with: `result: regatta operator loop <N> — <findings> new, <issues> filed, <fixes> pushed`.

## Recurrence rule (CRITICAL — don't spam tracker)

Same root cause hits ≥ 2 agents / iterations → exactly ONE tracker issue, bump occurrence counter via PR-comment on that issue. NEVER file a second issue for the same root cause. Conflicts with naive "Nth occurrence" loops; this rule wins.

Implementation: before filing, run `gh issue list -R <regatta-repo> --search "<root-cause-keyword>" --state all -L 5 --json number,title,state`. If an open issue matches → comment on it with "occurrence N: <PR or agent ref>". If a closed issue matches → reopen with "regression detected: <PR ref>".

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
