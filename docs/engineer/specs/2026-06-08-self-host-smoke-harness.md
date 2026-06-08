---
title: "Self-host loop validation smoke harness (closes #864)"
status: draft
phase: self-host
summary: "Operator-runnable smoke harness that proves the autonomous loop closes end-to-end (issue → adapter → scheduler → spawner → PR → merge) against a freshly installed regatta service. Six steps, one signal per step, six failure-mode diagnostic ladders. Pure documentation — no new prod code; no implementer task."
date: 2026-06-08
---

# Self-host loop validation smoke harness — Spec

Closes: [#864](https://github.com/trilamsr/regatta/issues/864) — `[autonomous] smoke test: regatta self-host loop validation`.

Depends on: `#882` (ghclient ENOENT fix for distroless container; merged) + `[autonomous]` label consumption path in `internal/orchestrator/adapter/githubissues/adapter.go`.

Memory rules in force: `feedback_default_simpler`, `feedback_decision_priority`, `feedback_root_cause`, `feedback_no_signatures`, `feedback_deletion_default`, `feedback_pr_body_hygiene`, `feedback_watch_pr_until_merged`.

```release-notes
[DOCS] Spec for #864 — runnable smoke harness that proves the self-host
autonomous loop closes after `regatta install-service`. Six steps + one
pin signal per step + six failure-mode diagnostic ladders. Pure docs;
no new prod code; no implementer task. Operator-runnable checklist.
```

## §0 Closing trigger

Done when ALL of:

1. This spec lands on `main` (PR mentioning `closes #864`).
2. Operator has run the harness once end-to-end against a real `regatta install-service` deployment and recorded the result (pass/fail + diagnostics) in a comment on #864 OR a follow-up runbook PR.
3. The smoke-test no-op PR opened by the spawned worker has `mergedAt != null` AND `state == MERGED` within the 10-minute wall-clock budget specified in §5.

`#864` stays OPEN until (2) is recorded. The spec PR alone is NOT sufficient — running the harness is the acceptance signal, not authoring it.

## §1 Problem

Issue #864 enumerates three loose acceptance criteria — "regatta picks up issue", "projects to work_item", "scheduler tick logs projection" — but ships no runnable harness or pin signal. After `#882` landed (ghclient ENOENT under distroless) and the `[autonomous]` label-consumption path went green in `internal/orchestrator/adapter/githubissues/adapter.go`, the operator can in principle run a real smoke. In practice, the operator does NOT know:

- Which exact log line confirms each step fired.
- What the 10-minute wall-clock budget breaks down to per step.
- Which diagnostic to consult when step N stalls (gh auth? CUE config? rate limit? distroless missing binary? scheduler MinPoll skew?).
- What `pass` means in falsifiable terms ("PR merged" → which PR? whose merge button?).

Without those four answers, every smoke attempt either degrades into ad-hoc shell archaeology or quietly passes on cached state from a prior run. The S1-T5 unit smoke (`tests/selfhost/smoke_test.go`, shipped #348) covers the in-process wiring but stops at `WorkStatusMerged` with a Stub spawner and a synthetic PR URL — it does not exercise `regatta install-service`, real `gh`, real Claude subprocess, or real GitHub merge. This spec fills that gap with a manual operator harness; no Go test, no CI integration.

## §2 Scope

### In scope

1. Six-step harness — one operator action per step, one pin signal per step.
2. Six failure-mode diagnostic ladders — what to check when step N stalls past its per-step budget.
3. Falsifiable pass criterion — synthetic `[autonomous]` issue is auto-merged within 10 minutes.
4. Operator runbook prose, suitable for direct paste into `#864` comment or a follow-up PR body.

### Out of scope

- **Multi-tenant smoke**: Phase X. Single-tenant single-operator per `CLAUDE.md` self-host filter.
- **Production-volume soak**: separate spec when external customer LOI lands. This harness fires once per `install-service` regression; not a continuous probe.
- **CI integration**: harness is operator-runnable, not CI-runnable. Reasoning: real `gh issue create` + real Claude subprocess + real GH merge are network-coupled, identity-coupled, cost-coupled. Promoting to CI would gate every push on a 10-minute network round trip with no new signal beyond the unit smoke.
- **Auto-generated harness script**: defer. Per `feedback_default_simpler`, the six-step checklist is the simplest viable artefact. Wrap-in-bash later only if the operator runs the harness ≥3 times in a session and notices the same 6 commands.
- **Substrate event audit**: the unit smoke already covers journal seam preservation; the operator-facing pass signal is the merged PR, not the journal row.

## §3 Harness design

### Pre-flight (one-time, NOT part of the 10-minute budget)

```bash
# 1. Confirm install-service is running.
systemctl --user status regatta            # Linux user-mode
# OR
launchctl print gui/$UID/com.regatta.serve # macOS user-mode
# OR
sudo systemctl status regatta              # Linux system-mode

# 2. Confirm gh CLI is on PATH for the service user (#882 root cause).
sudo -u regatta gh auth status              # system-mode
# OR
gh auth status                              # user-mode

# 3. Confirm regatta.yaml resolves the github_issues adapter.
regatta doctor --json | jq '.checks[] | select(.name | contains("adapter"))'

# 4. Snapshot scheduler tick cadence + adapter MinPoll.
journalctl --user -u regatta -n 50 | grep -E 'scheduler\.|adapter\.'
```

All four must succeed before running the §4 harness. Pre-flight failure is NOT a smoke failure — it is an install-service regression and belongs in the install-service runbook, not here.

### Step table

| # | Operator action | Pin signal | Per-step budget |
|---|-----------------|-----------|-----------------|
| 1 | `gh issue create --label autonomous --title '[autonomous] smoke-test no-op 2026-06-08' --body-file /tmp/smoke-body.md` (body content per §3.1) | `gh issue list --label autonomous --state open` shows the new issue number; record as `$SMOKE_ISSUE` | <1 min |
| 2 | Wait for adapter to project the issue into `work_items` | `journalctl --user -u regatta` shows `adapter.empty_list` BEFORE step 1 then NO `adapter.empty_list` AFTER step 1 within MinPoll window (default 30s per spec `2026-06-04-mvr-1-t4-github-issues-adapter-impl.md` §1.1). Confirm via `regatta status --json` shows the new work_item ID | <1 min (MinPoll bound) |
| 3 | Wait for scheduler to dispatch | `journalctl --user -u regatta` shows `scheduler.gates_pass_enqueued` with `work_item_id` matching `$SMOKE_ISSUE` projection. Source: `internal/orchestrator/scheduler/scheduler_gates_pass.go:36` | <30s after step 2 |
| 4 | Wait for spawner to launch worker | `regatta status` shows one new agent row in `running` state with work_item_id matching step 3 | <30s after step 3 |
| 5 | Wait for worker to open PR | `gh pr list --state open --head 'regatta/agent-*' --json number,headRefName,title,url -L 10` shows a new PR whose body contains `closes #$SMOKE_ISSUE` | <5 min after step 4 |
| 6 | Wait for L4 review + automerge | `gh pr view $SMOKE_PR --json state,mergedAt,mergeStateStatus` shows `state=MERGED` AND `mergedAt != null` per `feedback_watch_pr_until_merged` | <3 min after step 5 |

Total budget: <10 min wall-clock from step 1 to step 6.

### §3.1 Smoke issue body (`/tmp/smoke-body.md`)

```
<!--regatta:
id: smoke-test-2026-06-08
lane: self-host
kind: refactor
-->

# Smoke test for autonomous loop closure

## Acceptance criteria
- [ ] worker opens a PR closing this issue with a no-op change

## Trigger
Manual operator smoke against `regatta install-service`. Re-run when the
install-service codepath, github_issues adapter, scheduler dispatch path,
or L4 review identity changes.

```release-notes
[CHORE] smoke-test no-op — closes #SMOKE_ISSUE
```
```

The worker is instructed (via the dispatch prompt from `regatta.yaml`) to land a trivial-by-construction no-op PR: append a single empty line to `docs/engineer/smoke-test-log.md` (file created if absent) AND no other diff. The diff is intentionally byte-trivial so L4 review never blocks; this is a smoke for loop wiring, not for code quality.

### §3.2 Signal grep cheatsheet

```bash
SMOKE_ISSUE=<from step 1>

# Step 2 — adapter consumed.
journalctl --user -u regatta --since '5 min ago' | grep -E 'adapter\.(empty_list|duplicate_id|item_skipped|tombstoned)'
# Healthy: no warnings after step 1.

# Step 3 — scheduler enqueued.
journalctl --user -u regatta --since '5 min ago' | grep 'scheduler.gates_pass_enqueued'
# Healthy: one line, work_item_id matches the projection.

# Step 4 — running agent.
regatta status --json | jq '.agents[] | select(.status=="running")'

# Step 5 — PR opened.
gh pr list --search "in:body \"closes #$SMOKE_ISSUE\"" --json number,url,state -L 5

# Step 6 — merged.
gh pr view $SMOKE_PR --json state,mergedAt,mergeStateStatus
```

## §4 Failure-mode diagnostic ladders

One ladder per step; each ladder enumerates the top-3 root causes ordered by observed frequency in this repo's history.

### Step 1 fails (issue create returns non-zero)

1. `gh auth status` — token expired or scope missing (`repo` required).
2. Rate limit: `gh api rate_limit | jq '.resources.core'`. If `remaining < 100`, wait `reset`.
3. Label `autonomous` missing on the target repo: `gh label list | grep autonomous`. Create via `gh label create autonomous --description "regatta will auto-consume this issue as a work_item" --color 0E8A16`.

### Step 2 fails (adapter never projects)

1. **`gh` binary missing on service PATH** (root cause of `#882`): `sudo -u <service-user> which gh`. Empty → install-service runbook regression; do NOT patch around it.
2. **`MinPoll` skew**: adapter default is 30s (per `2026-06-04-mvr-1-t4-github-issues-adapter-impl.md` §1.1). Check `regatta.yaml` `spec_adapter.min_poll`. If >2 min, raise step-2 budget OR shorten the cap.
3. **Selector mismatch**: `regatta.yaml` `spec_adapter.selector` includes a clause that excludes the smoke issue's labels (e.g. `label:source:operator` AND smoke issue lacks that label). Validate via `gh issue list --search "<selector>"` parity with adapter.
4. **CUE config error**: `regatta doctor --json | jq '.checks[] | select(.status != "ok")'`. Common: `spec_adapter.repo.owner` empty.

### Step 3 fails (scheduler never enqueues)

1. **L4 gate blocked**: `journalctl --user -u regatta` shows `scheduler.l4_gate_blocked`. Smoke issues with trivial bodies sometimes trip L4's "missing-acceptance-criteria" heuristic. Mitigation: confirm `## Acceptance criteria` header present in body verbatim (§3.1 fixture).
2. **Cost cap throttled**: log shows `scheduler.cost_cap_throttled` or `scheduler.cost_gate_denied`. Check `regatta cost status`; raise cap or wait for window reset.
3. **Approval gate held**: log shows `EventApprovalDecided` with `outcome=blocked`. Smoke runs should bypass approval (no human gate); confirm `regatta.yaml` `approval.required` is `false` for `kind: refactor`.

### Step 4 fails (no running agent appears)

1. **Spawner config error**: `regatta status` shows agent in `failed` state. Check `journalctl` for `scheduler.materialize_failure` (source: `internal/orchestrator/scheduler/scheduler_spawn.go:60`).
2. **Worker binary missing**: configured spawner (claude, codex, custom) not on PATH for service user.
3. **Worktree creation failure**: disk full OR permissions error under `$XDG_STATE_HOME/regatta/worktrees`. Inspect with `df -h`, `ls -la`.

### Step 5 fails (worker runs but no PR)

1. **Worker exit non-zero**: `regatta status --json | jq '.agents[] | .last_output_tail'`. Check for CUE-plan compile error, missing dep, or worker prompt parity failure (see `CLAUDE.md` worker-prompt-parity gate).
2. **PR-watch seam lag**: `internal/orchestrator/prwatch` polls GH head SHA on tick. Wait one more tick OR force `regatta refresh prs` if implemented.
3. **`gh pr create` failed under service identity**: identity setup regression. Verify `regatta-bot` PAT is valid via `sudo -u <service-user> gh pr list -L 1`.

### Step 6 fails (PR opens but never merges)

1. **L4 reviewer-bot blocked**: PR body footer missing `Reviewer-recommendation: APPROVE` (per `CLAUDE.md` reviewer-verdict gate). Smoke worker prompt should include the verdict; if absent, treat as worker-prompt regression.
2. **Branch protection check missing**: `gh pr checks $SMOKE_PR` shows a required check pending or failing. For a no-op smoke, only `pr-lint` should run; if other checks gate, branch protection drifted.
3. **`mergeStateStatus=DIRTY`**: smoke PR collided with a concurrent merge to `main`. Rebase via `gh pr update-branch $SMOKE_PR`, re-watch.

## §5 Acceptance

The harness PASSES iff, within 10 minutes of step 1:

- `gh pr view $SMOKE_PR --json state,mergedAt,mergeStateStatus` returns `state == "MERGED"` AND `mergedAt != null` AND `mergeStateStatus == "CLEAN"`.
- The merged PR's diff is exactly the no-op described in §3.1 (one trailing-newline append to `docs/engineer/smoke-test-log.md`, no other files touched).

Any other outcome is a FAIL; the operator records the failing step + diagnostic ladder hit + log excerpts in the #864 comment.

## §6 Adversarial reviewer notes

Run an adversarial reviewer subagent on this spec before closing #864. Expected findings checklist:

- **Q: Why not promote to a `tests/selfhost_smoke_live_test.go` build-tagged Go test gated by `REGATTA_SMOKE_LIVE=1`?** A: §2 covers this. Real GH + real Claude + real merge would make every CI run network-coupled + identity-coupled + cost-coupled. The S1-T5 in-process smoke (`#348`) already gates the wiring; the operator harness adds the install-service end of the loop, which is by definition off-CI.
- **Q: 10-minute budget — is it tight enough to catch slow-loop regressions and loose enough to absorb GH API jitter?** A: 30s adapter MinPoll + 30s scheduler tick + ~3min spawned-worker no-op + ~2min L4 + ~1min automerge ≈ 7min mean, with 3min headroom for GH API jitter. Tighter risks false negatives on a healthy install; looser stops catching slow-tick regressions.
- **Q: Why does the smoke require an actual no-op diff instead of a worker that just comments "I see you" and closes the issue?** A: the loop terminates on `mergedAt != null`. A comment-only worker never opens a PR and never traverses steps 5–6, so the smoke would miss the automerge + L4 + branch-protection path — the half of the loop most likely to drift between install-service regressions.
- **Q: What stops a stale `[autonomous]` issue from a prior smoke from being re-consumed and spawning duplicate workers?** A: The github_issues adapter's dedup-key marker (`<!-- regatta-dedup-key: <hex> -->` per `2026-06-04-mvr-1-t4-github-issues-adapter-impl.md` §3.5) is written to the issue body on first consumption. Replay is a no-op. If the operator manually deletes the marker, that is a deliberate "re-run smoke against unchanged install" gesture; document via runbook, not via spec gate.
- **Q: Why no Phase-X `tenant_id` seam mentioned?** A: spec is `phase: self-host` (per `CLAUDE.md` self-host filter). The `tenant_id` forward-fit lives at the adapter + scheduler seams already (per `2026-06-04-mvr-1-t4-github-issues-adapter-impl.md`); this smoke does not exercise it because single-tenant per Phase-S brief.
- **Q: Where does the "configured spawner" decision land if the operator uses Codex / Cursor / GPT-5 instead of Claude?** A: §4 step-4 ladder cites "claude, codex, custom" generically. The spawner-agnosticism is covered by spec `2026-06-07-bring-your-own-agent.md`; this smoke validates loop wiring regardless of which spawner is configured, provided the worker binary is on PATH.

## §7 Out of scope

- Multi-tenant smoke variant (Phase X; reopen-trigger: external customer LOI).
- Production-volume soak (separate spec; reopen-trigger: 30-day-green on this single-shot harness across ≥3 install-service regressions).
- Auto-generated harness wrapper script (defer until §4 runbook is run ≥3 times; per `feedback_default_simpler`).
- Smoke for non-`github_issues` adapters (`markdown_catalog`, future Gitea / GitLab adapters — separate per-adapter smoke once those land).

## §8 Closes

`#864` closes when (1) this spec merges AND (2) the operator records a passing run per §0 trigger criteria. Until both are recorded, #864 stays OPEN even though the spec is on `main`.
