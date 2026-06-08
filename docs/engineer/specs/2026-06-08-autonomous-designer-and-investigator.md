---
title: "Autonomous designer + auto-triggered root-cause investigator (minimize operator dispatch)"
status: draft
phase: s2-autonomy
summary: "Eliminate operator dispatch bottleneck for two recurring subagent roles. (1) New brief lands in docs/engineer/briefs/ -> regatta dispatches designer subagent that opens a draft-status spec PR. (2) Alarm-webhook fires -> regatta dispatches cavecrew-investigator subagent that attaches a root-cause report to the deduped issue. Both paths are throttled, cycle-broken, and override-gated. Operator only intervenes at irreversible decisions (branch protection, secret rotation, schema migration, GREEN-CLOCK sign-off)."
---

# Autonomous designer + auto-triggered investigator — Design Spec

Date: 2026-06-08
Trigger source: operator dispatch bottleneck retro (recurring designer/investigator manual spawns blocking parallel-implementer cap).
Memory rules in force: `feedback_decision_priority`, `feedback_default_simpler`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_dispatch_brief_only`, `feedback_no_signatures`, `feedback_adversarial_review_every_step`, `feedback_unaddressed_load_bearing`, `feedback_audit_main_before_implementing`, `feedback_trap_projection`.

```release-notes
[DOCS] Design spec for autonomous designer + auto-triggered investigator.
Brief-drop -> auto-designer (throttled 3/day, opt-out via frontmatter
auto_promote: false). Alarm-webhook fire -> auto-investigator (throttled
1/alarm-id/hr). Operator override gates irreversible actions. No code in
this PR; impl follows the §10 3-slice brief.
```

## §0 Closing trigger

Done when ALL of:

1. Slice 1 PR (briefs watcher) merges; dropping a brief into `docs/engineer/briefs/` produces a spec PR under `docs/engineer/specs/` within 30 minutes.
2. Slice 2 PR (alarm hook) merges; simulated alarm-webhook payload produces an investigator report attached to the filed issue within 5 minutes.
3. Slice 3 PR (dedup/throttle/cycle-breaker) merges; throttle counters expose Prometheus metrics that prove dispatch caps hold under storm conditions.
4. `make ci-check` green on every slice PR.
5. Operator turns the loop on for 7 consecutive days with zero accidental cost-explosion or spec-spam pages.

## §1 Problem

Two subagent dispatch paths consume disproportionate operator turns today:

**Designer dispatch.** Brief lands under `docs/engineer/briefs/`. Operator notices, opens dispatch template `docs/engineer/dispatch-templates/designer.md`, fills slug + scope + memory rules, spawns the subagent. The action is mechanical — brief text already names topic + scope — but it blocks the operator turn until the spec PR opens.

**Investigator dispatch.** Alarm-webhook (`internal/alarmwebhook/handler.go:312-355`) creates or comments on a deduped GitHub issue. Operator monitors the issue feed, dispatches `cavecrew-investigator` against the alert payload + affected files. Time-to-investigator is bounded by operator latency, not by what the system could do automatically.

Both paths are recurring + bounded + cheap to mechanize. Per `feedback_decision_priority` (UX > ease > performance), the operator-input minimization wins.

**Root cause** per `feedback_root_cause`: the dispatch step is a *transcription* — brief or alarm payload already carries the parameters the subagent needs. The operator is performing identity-function work. Eliminate the transcription, keep operator authority at irreversible decision points only.

Self-host filter (CLAUDE.md §Self-host filter): the sole internal operator dispatches regatta-the-binary at this repo unattended. Designer and investigator subagents fire every day during active development. Keep in scope.

## §2 Goal

Two new dispatch paths, both backed by existing harness primitives:

1. **Auto-designer**: new file under `docs/engineer/briefs/` -> dispatch designer subagent -> draft spec PR opens.
2. **Auto-investigator**: alarm-webhook fires -> dispatch `cavecrew-investigator` -> root-cause report comment attaches to the deduped issue.

Both paths obey: throttle (per-class daily cap), opt-out (frontmatter / label), cycle-breaker (subagent-spawned files do NOT re-trigger), and operator override (manual dispatch always wins).

Operator authority is preserved for:

- Irreversible: branch-protection downgrade, secret rotation, schema migration that rewrites prod data.
- Design intent: "should we even build X" (strategy-level scoping decisions).
- Sign-off: GREEN-CLOCK day-30 transition, customer release.

## §3 Auto-designer

### §3.1 Trigger

Scheduler scan (NOT fsnotify) every 5 minutes against `docs/engineer/briefs/`. Decision: scheduler scan keeps the design uniform with `internal/orchestrator/scheduler/scheduler.go` cadence, eliminates fsnotify dependency, survives FS-event drops on macOS/Linux mixed dev hosts.

Trigger predicate (all must hold):

1. File path matches `docs/engineer/briefs/[0-9]{4}-[0-9]{2}-[0-9]{2}-*.md` (date-prefixed brief slug).
2. Brief is committed to `origin/main` (`git log --diff-filter=A --pretty=%H -- <path>` returns a commit). Uncommitted local edits do NOT fire.
3. Brief frontmatter does NOT contain `auto_promote: false`.
4. No spec already exists under `docs/engineer/specs/` whose `summary:` or `Trigger source:` references this brief filename.
5. Daily throttle counter for class `designer.brief_promote` is below cap (3/day).

### §3.2 Action

Regatta dispatches the designer subagent with prompt body assembled from `docs/engineer/dispatch-templates/designer.md` preamble + per-dispatch payload:

- `<TOPIC>` = first H1 of the brief (or `title:` frontmatter if present).
- `<SPEC-SLUG>` = today's date + brief slug minus the brief's date prefix.
- `<SCOPE>` = derived from brief body §1-§2 if those sections exist; otherwise dispatched as "scope: see brief".
- `<PHASE>` = brief frontmatter `phase:` if set, else `s2-autonomy` default.
- `<MEMORY-RULES>` = `feedback_default_simpler, feedback_root_cause, feedback_deletion_default, feedback_dispatch_brief_only, feedback_no_signatures`.
- `<REFERENCES>` = empty unless brief frontmatter `references:` enumerates them.

Subagent output: spec lands at `docs/engineer/specs/<SPEC-SLUG>.md` with `status: draft` frontmatter, plus PR open against `origin/main` with `[DOCS]` release-notes prefix.

### §3.3 Throttle

In-memory counter keyed on `designer.brief_promote.YYYY-MM-DD`, persisted to BoltDB bucket `autodispatch_throttle` so restarts inherit the count. Cap = 3 spec dispatches per UTC day. Counter resets at UTC midnight via existing scheduler tick.

Beyond cap: log structured warning + skip; brief stays unpromoted until next day. Operator can override via manual dispatch (counter does not gate manual `designer.md` spawns).

### §3.4 Opt-out

Brief frontmatter:

```yaml
---
title: "..."
auto_promote: false
---
```

Skipped silently — no log noise, no counter bump. Use when brief is exploratory or paired with a deliberately-manual designer spawn.

### §3.5 Cycle-breaker

Files created BY a designer subagent (path matches `docs/engineer/specs/*.md`) MUST NOT re-trigger the watcher. Since the briefs watcher only fires on `briefs/` paths, this is structural — no shared dir between input and output. Adversarial check (§6): verify no subagent writes a *brief* as a side effect.

## §4 Auto-investigator

### §4.1 Trigger

Hook into `internal/alarmwebhook/handler.go::route` immediately after `h.Client.CreateIssue` or `h.Client.CommentOnIssue` succeeds (lines 343-353). Post-success hook fires only on `issue_created` action (NOT `comment_added`) — comment-add means the issue already exists and an investigator already attached on the original create.

Trigger predicate (all must hold):

1. `action == "issue_created"` (per `h.bump(ctx, name, severity, "issue_created")` at line 351).
2. Alert severity `>= warning` (filter out `info`-tier noise).
3. Throttle counter `investigator.alarm.<alertname>` below 1 dispatch per rolling 1-hour window.

### §4.2 Action

Regatta dispatches `cavecrew-investigator` subagent with prompt body:

- Alert payload (alertname, severity, labels, annotations) verbatim.
- Issue number + URL (from `CreateIssue` return value).
- Affected files: derived from alert label `code_path` or `service` if present; otherwise empty (investigator scans broadly).
- Instructions: produce 1-page report (root cause hypothesis + 3-5 file:line refs + 1 reproduction command); post as issue comment via `h.Client.CommentOnIssue(ctx, num, report)`.

Subagent runs in a fresh harness worktree under `.claude/worktrees/agent-<id>/` per CLAUDE.md §Worktree discipline. Read-only — investigator never writes code, never opens PRs. Findings land as issue comment + structured log line `autodispatch.investigator.completed alertname=... issue=... finding_count=N`.

### §4.3 Throttle

In-memory counter keyed on `investigator.alarm.<alertname>.<unix-hour-bucket>`, persisted to BoltDB bucket `autodispatch_throttle`. Cap = 1 investigator dispatch per alertname per 1-hour rolling window.

A storm of N firings of the same alertname produces ONE investigator. The dedup cache (`internal/alarmwebhook/dedup.go`) already collapses N issue-creates into one; the throttle here collapses N investigator-dispatches into one over the storm window.

### §4.4 Opt-out

Issue label `no-investigator` (added by operator after the fact, or pre-set on the alertname via AlertManager `annotations.regatta_no_investigator: "true"`). Investigator skips when the just-created issue carries this label or the alert payload's annotations include the opt-out key.

### §4.5 Cycle-breaker

Investigator subagent is read-only (§4.2). It cannot create issues, fire alerts, or modify files. The Claude-API spawn carries a system-prompt clause: "you are an investigator; do NOT call any tool that writes to disk, opens a PR, files an issue, or invokes a webhook". Enforcement: investigator's spawn config sets `allowed_tools` to a read-only allowlist (`Read`, `Bash` with grep/git read-only, `WebFetch`, `Glob`, `Grep`, MCP read-only tools). No `Write`, no `Edit`, no `gh issue create`, no `mcp__github__add_issue_comment` from the subagent — only the parent regatta process posts the final report.

Adversarial: see §6.2 for the "investigator-fires-alert" loop case.

## §5 Operator decision points (preserved manual)

Per `feedback_decision_priority` "NEVER ask user — spawn review subagent + decide", but irreversible actions stay manual:

| Action                                  | Reason kept manual                                                                                                   |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| Branch-protection downgrade             | Reverting takes a `gh api` call + audit trail; one-way until re-locked. Operator confirms via CLI.                   |
| Secret rotation                         | Wrong rotation order locks production out of GitHub / OTel collector. Operator runs `regatta secret rotate` by hand. |
| Schema migration rewriting prod data    | Migration N+1 logic must be operator-reviewed. Migration adds stay manual per `feedback_migration_number_lock`.      |
| Design intent ("should we build X")     | Strategy-level scoping is not mechanical. Operator opens the brief; auto-designer promotes only after that.          |
| GREEN-CLOCK day-30 transition           | 30-day-green is the soak gate before Phase-X reopen. Operator confirms metric review.                                |
| Customer / external release sign-off    | Operator owns release notes + version-bump cadence.                                                                  |

All other dispatch (designer on briefs, investigator on alarms) is autonomous. Manual dispatch is always available as override — the auto path never blocks the operator from dispatching the same subagent type.

## §6 Adversarial review

### §6.1 Spec spam (bad brief -> bad spec -> low signal)

**Risk**: operator drops a malformed brief; auto-designer produces a malformed spec PR; spec PRs accumulate as noise.

**Mitigation 1** — frontmatter gate. Briefs without `title:` OR without a first H1 are skipped (logged `autodispatch.designer.skipped reason=no-title`).

**Mitigation 2** — daily throttle cap (3/day). A bad-brief day caps the bad-spec output at 3 PRs, not N. Operator sees the noise within one day and can `auto_promote: false` the offending briefs.

**Mitigation 3** — spec PRs land as `status: draft` and `[DOCS]` release-notes. They do NOT trigger reviewer-verdict gate (CLAUDE.md `Reviewer-verdict gate` auto-skips `[DOCS]`). Bad specs sit in OPEN PRs until operator closes — they do NOT auto-merge.

**Residual risk**: A determined operator can still drop 100 briefs in a week, get 21 spec PRs (3/day * 7), and clutter the PR queue. Acceptable — operator chose to drop the briefs.

### §6.2 Investigator loop (investigator dispatches investigator)

**Risk**: investigator subagent posts an issue comment that triggers an alert that triggers another investigator.

**Mitigation 1** — read-only allowlist (§4.5). Investigator cannot post comments directly; only the parent regatta process does, and the parent does NOT fire an alert when posting an investigator report.

**Mitigation 2** — alertname throttle (1/hr). Even if a sibling alert fires from the same alertname within the hour, throttle short-circuits before subagent dispatch.

**Mitigation 3** — comment-action skip (§4.1). `comment_added` action does NOT fire the investigator hook. Only `issue_created` does. Repeated firings of the same alertname during a storm produce `comment_added` (per `internal/alarmwebhook/handler.go:330-337`), not `issue_created`.

**Residual risk**: investigator finds a bug, operator dispatches implementer to fix, implementer's fix lands but causes a NEW alertname to fire, which triggers a NEW investigator. This is the system working as designed — distinct alertnames are distinct root-cause domains. Acceptable.

### §6.3 Cost explosion (alarm storm -> N concurrent investigators)

**Risk**: 50 alertnames fire in 1 minute. Each fires a fresh investigator. 50 concurrent Claude API spawns blow the rate budget and incur runaway cost.

**Mitigation 1** — global investigator concurrency cap. New BoltDB counter `autodispatch_concurrent_investigators`; cap = 3 (matches CLAUDE.md §Dispatch "Cap parallel implementers at 3-4"). 4th simultaneous investigator dispatch waits in a queue with 5-minute timeout; on timeout, the investigator is skipped (logged) and the issue gets `[obs-alert] investigator skipped: concurrent cap` as a comment so the operator sees the gap.

**Mitigation 2** — per-alertname throttle (§4.3) already collapses storm of one alertname into 1 dispatch. Mitigation 1 covers the cross-alertname storm case.

**Mitigation 3** — daily global investigator cap. New counter `investigator.global.YYYY-MM-DD`, cap = 30/day. Hard ceiling — operator can raise via config but default protects against runaway.

**Residual risk**: a sophisticated attacker who can fire 30 distinct alertnames per day burns the budget for 24 hours. Self-host phase: alertmanager is internal-network-only behind the operator's VPN; not internet-reachable. Acceptable for now; revisit at Phase-X (external customer ask).

## §7 Implementation surface

Per `feedback_audit_main_before_implementing`, verify on `origin/main`:

- `internal/alarmwebhook/handler.go` — EXISTS; lines 312-355 carry the route function.
- `internal/orchestrator/scheduler/scheduler.go` — EXISTS; carries scheduler cadence.
- `docs/engineer/dispatch-templates/{designer,triage}.md` — EXIST; preamble blocks reusable verbatim.
- `internal/orchestrator/spawner/claude.go::defaultPromptBuilder` — EXISTS; current single-entry subagent spawn point.
- BoltDB usage — `internal/orchestrator/state/store.go` and similar own bucket conventions; reuse same library (`go.etcd.io/bbolt`).

New surface:

- `internal/autodispatch/` (new package): owns the throttle counters, BoltDB bucket schema `autodispatch_throttle`, the brief watcher tick, the alarm-hook entrypoint.
- `internal/autodispatch/designer.go` — brief watcher + designer spawn.
- `internal/autodispatch/investigator.go` — alarm-hook hook + investigator spawn (called from `handler.go::route` post-CreateIssue).
- `internal/autodispatch/throttle.go` — shared counter primitive.

Net code add: ~400 LOC. Per `feedback_deletion_default`, A+ defense: this is autonomy infrastructure (the deletion is operator-turn deletion, not LOC deletion). The 400 LOC removes ~10 operator turns/week of dispatch transcription. Long-term > short-term.

## §8 Out of scope

- **Auto-merge of designer-output specs.** Spec PRs land as `status: draft` + `[DOCS]`. Operator review still required before merge. Reopen-trigger: 30-day-green on the auto-designer accuracy metric (false-positive rate < 10%).
- **Investigator-driven auto-fix.** Investigator produces a *report*. Implementer dispatch stays operator-initiated. Reopen-trigger: investigator accuracy metric > 90% on a 50-issue corpus.
- **Auto-triage of GitHub issues filed by humans.** This spec covers brief-drop and alarm-fire only. Human-filed issues stay in the operator triage queue (`docs/engineer/dispatch-templates/triage.md`).
- **Multi-tenant throttle**. Single-operator self-host phase; one global throttle bucket suffices. Phase-X reopens if multi-tenant lands.

## §9 Acceptance

1. Drop `docs/engineer/briefs/2026-06-09-test-brief.md` (with `title:` and §1) on `origin/main`. Within 30 minutes, a PR opens against `origin/main` titled `[DOCS] Design spec for <topic>` with the spec body under `docs/engineer/specs/2026-06-09-test-brief.md` and `status: draft`.

2. POST to alarm-webhook with payload `{"alerts": [{"labels": {"alertname": "TestAlarm", "severity": "warning"}}]}`. Within 5 minutes, the auto-created GitHub issue gains a comment from the investigator subagent containing: a root-cause hypothesis sentence, 3-5 `file:line` refs, and 1 reproduction command.

3. Drop 5 briefs in 1 day. PRs open for the first 3; briefs 4-5 are skipped with log `autodispatch.designer.throttled count=3 cap=3`. Next UTC day, briefs 4-5 retry and succeed.

4. Fire 10 alarms with the same alertname in 5 minutes. 1 investigator dispatches; 9 are throttle-skipped with log `autodispatch.investigator.throttled alertname=X ttl_remaining=Ns`.

5. Operator manually dispatches a designer subagent for a brief that has `auto_promote: false`. Manual dispatch succeeds (throttle does NOT gate manual dispatches).

6. `make ci-check` green on all 3 slice PRs.

## §10 Implementer brief (3 slices)

Slices are file-disjoint and can dispatch in parallel per CLAUDE.md §Dispatch.

### Slice 1: brief watcher + designer dispatch

- **Files**: `internal/autodispatch/designer.go` (new), `internal/autodispatch/designer_test.go` (new), `cmd/regatta/serve.go` (wire watcher into scheduler tick).
- **Failing test FIRST** (`feedback_tdd_discipline`): `TestDesignerWatcher_PromotesNewBrief` — drops a brief under a tempdir briefs path, ticks the watcher, asserts a subagent spawn call with prompt body containing `<TOPIC>` extracted from the brief. Capture the failing output in the PR body.
- **Throttle**: BoltDB counter `designer.brief_promote.YYYY-MM-DD` cap 3/day. Counter resets at UTC midnight.
- **Opt-out**: brief frontmatter `auto_promote: false` skips.
- **Subagent spawn**: reuse existing spawn path `internal/orchestrator/spawner/claude.go::defaultPromptBuilder` — pass designer template + per-dispatch payload (§3.2).
- **Acceptance**: §9 cases 1, 3, 5.

### Slice 2: alarm hook + investigator dispatch

- **Files**: `internal/autodispatch/investigator.go` (new), `internal/autodispatch/investigator_test.go` (new), `internal/alarmwebhook/handler.go` (call investigator hook post-CreateIssue at line 351 boundary).
- **Failing test FIRST**: `TestInvestigatorHook_FiresOnIssueCreated` — stub `ghclient.Client`, route an alert through `Handler.route`, assert investigator hook called with alertname + issue number.
- **Throttle**: BoltDB counter `investigator.alarm.<alertname>.<unix-hour-bucket>` cap 1/hr.
- **Opt-out**: issue label `no-investigator` OR alert annotation `regatta_no_investigator: "true"`.
- **Subagent spawn**: invoke `cavecrew-investigator` via spawner with read-only allowed_tools allowlist (§4.5).
- **Concurrency cap**: global counter `autodispatch_concurrent_investigators` cap 3; 4th waits up to 5min then skips.
- **Acceptance**: §9 cases 2, 4.

### Slice 3: dedup + throttle + cycle-breaker primitives

- **Files**: `internal/autodispatch/throttle.go` (new), `internal/autodispatch/throttle_test.go` (new), `internal/autodispatch/cyclebreak.go` (new), `internal/autodispatch/cyclebreak_test.go` (new). Slice 1+2 import this slice's primitives.
- **Failing test FIRST**: `TestThrottle_DailyCapEnforced` — bumps counter past cap, asserts Reserve() returns false; `TestThrottle_PersistsAcrossRestart` — opens a fresh BoltDB, asserts prior count survives.
- **Cycle-breaker**: tag each subagent spawn with `autodispatch_origin=<class>` metadata; if the spawn process tries to fire another auto-dispatch class, parent rejects. (Pure defense-in-depth — read-only allowlist in §4.5 is the primary mitigation.)
- **Metrics**: Prometheus counters `regatta.autodispatch.spawned_total{class=designer|investigator,outcome=spawned|throttled|skipped}`. Wired through `internal/obs`.
- **Acceptance**: throttle persists across restart; metrics scrape green; storm-test (50 alarms in 5min) produces ≤3 concurrent investigators.

### Cross-slice

- All slices: `make pre-push-check`. `gh pr create --body-file <path>` per `feedback_pr_body_hygiene`. Release-notes prefix `[DOCS]` for spec-only changes; slices ship as `[FEAT]` since they add code.
- All slices: cite `feedback_dispatch_brief_only` in PR body — the prompts handed to the auto-spawned subagents are per-task briefs (designer template payload, investigator alert payload), NOT this full spec doc.
- Comment density: each new `.go` file MUST stay under 5% comment density (CLAUDE.md §CI gates). Exported godocs open with the symbol name and capture WHY in 1 sentence.
- Reviewer dispatch per `feedback_adversarial_review` — autodispatch is load-bearing (concurrency, persistent state, external API spawn). Reviewer-recommendation gate fires.
- Tracking issues per `feedback_unaddressed_load_bearing` for every reviewer finding not fixed inline.

## §11 Memory rule citations (PR body footer)

`feedback_decision_priority` `feedback_default_simpler` `feedback_root_cause` `feedback_deletion_default` `feedback_dispatch_brief_only` `feedback_no_signatures` `feedback_adversarial_review_every_step` `feedback_unaddressed_load_bearing` `feedback_audit_main_before_implementing` `feedback_trap_projection` `feedback_tdd_discipline` `feedback_pr_body_hygiene` `feedback_migration_number_lock`
