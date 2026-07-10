# Tickets

_Exported from Linear on 2026-07-09. Going forward this file is the source of truth; Linear tracker is retired._

Legend: Priority — U=Urgent, H=High, M=Medium, L=Low, ·=None.

Total: 75 tickets across 10 milestones.

- M1 — Operator usable self-host: 5
- M2 — Polling efficiency + cost: 6
- M3 — CI simplification: 3
- M4 — Orchestrator resilience: 10
- M5 — Adapter expansion (Linear-first): 1
- M6 — UX polish: 7
- M7 — Agent prompt + review discipline: 6
- M8 — Reviewer-finding burndown: 7
- M9 — Multi-phase work shapes (deferred): 29
- M10 — Backlog / unclassified: 1

## M1 — Operator usable self-host

### MAY-20 — F1: read-write dashboard actions (kill / retry / withdraw / force-merge)

- Priority: U
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-20/f1-read-write-dashboard-actions-kill-retry-withdraw-force-merge

> **Migrated from** [GH #1250](<https://github.com/themaydow/regatta/issues/1250>) · Created 2026-06-10 · GH labels: severity:high, kind:slice, priority:p0, scope:ux

Umbrella: GH #1249 (now Linear M6 wedge issue).

## Problem

Dashboard is purely read-only. Every recovery action requires shelling to CLI or raw `sqlite3` queries. Stuck agent → no UI action available. From audit §3 F1.

## Acceptance criteria

* \[planned\] AC1: dashboard exposes… (truncated, use `get_issue` for full description)

---

### MAY-21 — F2: live notification on CI failure (Slack/OS push + retry-on-flake)

- Priority: U
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-21/f2-live-notification-on-ci-failure-slackos-push-retry-on-flake

> **Migrated from** [GH #1251](<https://github.com/themaydow/regatta/issues/1251>) · Created 2026-06-10 · GH labels: severity:high, kind:slice, priority:p0, state:blocked, scope:ux

Umbrella: GH #1249.

## Problem

Operator enables automerge → CI flakes post-automerge → PR stuck in OPEN/BLOCKED for hours. No live notification. From audit §3 F2 + recurring per `feedback_watch_pr_until_merged`.

## Acceptance criteria

* \[planned\] AC1: desktop n… (truncated, use `get_issue` for full description)

---

### MAY-40 — F6: failure-aware empty states + above-the-fold incidents bar

- Priority: U
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-40/f6-failure-aware-empty-states-above-the-fold-incidents-bar

> **Migrated from** [GH #1255](<https://github.com/themaydow/regatta/issues/1255>) · GH labels: severity:high, kind:slice, priority:p0, scope:ux

Umbrella: #1249

## Problem

Empty state, healthy idle, DB down, adapter misconfigured — all render the same italic gray "no work items found". Operator cannot triage at-a-glance. Highest-leverage single fix per adversarial §6. From audit §3 F6.

## Acceptance criteria

* \[planned\] AC1: each dashboar… (truncated, use `get_issue` for full description)

---

### MAY-22 — F3: regatta login OAuth + collapse 6-mechanism auth jungle

- Priority: H
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-22/f3-regatta-login-oauth-collapse-6-mechanism-auth-jungle

> **Migrated from** [GH #1252](<https://github.com/themaydow/regatta/issues/1252>) · Created 2026-06-10 · GH labels: severity:high, kind:slice, priority:p1, scope:ux

Umbrella: GH #1249.

## Problem

First-run requires 3-6 env vars. HMAC has 3 env var variants for one key (`REGATTA_HMAC_KEY` / `_KEY_ENV` / `_KEYRING`). Approval keyring is parallel surface w/ confusingly similar names. No `regatta login` device-flow. Onboarding-killer. From audit… (truncated, use `get_issue` for full description)

---

### MAY-85 — BUG-1081: introduce L6.2 session-batch merge — operator clicks once to merge N green PRs in topo-sorted order

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-85/bug-1081-introduce-l62-session-batch-merge-operator-clicks-once-to

> **Migrated from** [GH #1081](<https://github.com/themaydow/regatta/issues/1081>) · GH labels: autonomous

## Symptom

The 2026-06-08 dogfood session ended with 7 PRs that all required individual operator click-merge. Each click required: read PR body, glance at CI status, check reviewer-id, click merge button, click delete-branch, return to monitor. \~30s × 7 = \~3.5min of pure burden. Across N parallel agents merging M PRs each across W weeks… (truncated, use `get_issue` for full description)

---

## M2 — Polling efficiency + cost

### MAY-39 — F7: polling reduction (ETag + gh checks --watch + GraphQL bundle)

- Priority: H
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-39/f7-polling-reduction-etag-gh-checks-watch-graphql-bundle

> **Migrated from** [GH #1256](<https://github.com/themaydow/regatta/issues/1256>) · GH labels: severity:medium, kind:slice, priority:p1, state:blocked, scope:perf

Umbrella: #1249

## Problem

Polling intervals 5-30s eat 5000/hr GraphQL quota. Depletes in single session per 2026-06-10 incident. From audit §3 F7.

## Acceptance criteria

* \[planned\] AC1: tracked under issue #1232 (ETag + `gh checks --watch` + GraphQL bundle synthesis)
* \[plan… (truncated, use `get_issue` for full description)

---

### MAY-41 — F5: per-agent live spend + halt-on-cap

- Priority: H
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-41/f5-per-agent-live-spend-halt-on-cap

> **Migrated from** [GH #1254](<https://github.com/themaydow/regatta/issues/1254>) · GH labels: severity:medium, kind:slice, priority:p1, scope:ux

Umbrella: #1249

## Problem

Operator sees `Today: $12` 30s-lagging aggregate. Cannot tell which agent is burning, cannot halt the burner. Discovery happens in next-day reconciliation. From audit §3 F5.

## Acceptance criteria

* \[planned\] AC1: `Agent.spend_micros` running counter updated on every … (truncated, use `get_issue` for full description)

---

### MAY-51 — [CORE] polling reduction: ship gh pr checks --watch + ETag + GraphQL bundle (supersedes #1229 #1230 #1231 after adversarial review)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-51/core-polling-reduction-ship-gh-pr-checks-watch-etag-graphql-bundle

> **Migrated from** [GH #1232](<https://github.com/themaydow/regatta/issues/1232>) · GH labels: autonomous

## Surface

`[CORE]` — operator-loop + prwatch + dashboard polling reduction

## Synthesis of 3 adversarial reviews on #1229 #1230 #1231

Three adversarial reviewers converged on the same conclusion: **ETag conditional-GET + smaller cadence/swap fixes win 80-95% of the polling-cost reduction at 5% of the engineering cost** — webhooks (#122… (truncated, use `get_issue` for full description)

---

### MAY-53 — [CORE] web/dashboard: replace htmx every-5s polling with SSE push (5 panels → 1 long-lived stream)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-53/core-webdashboard-replace-htmx-every-5s-polling-with-sse-push-5-panels

> **Migrated from** [GH #1230](<https://github.com/themaydow/regatta/issues/1230>) · GH labels: autonomous

## Problem

Five dashboard panels in `internal/web/templates/layout.tmpl:38-90` use htmx `hx-trigger="load, every 5s"` (3 panels) and `every 10s` (2 panels) to full-refresh innerHTML. At a quiet hour this is \~45 HTTP round-trips/min per open dashboard tab — each one fully re-renders Go templates server-side even when the underlying state-… (truncated, use `get_issue` for full description)

---

### MAY-54 — [CORE] operator skill: replace bounded CI poll with GitHub webhook wake-on-event (smee.io relay)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-54/core-operator-skill-replace-bounded-ci-poll-with-github-webhook-wake

> **Migrated from** [GH #1229](<https://github.com/themaydow/regatta/issues/1229>) · GH labels: autonomous

## Problem

The regatta-operator skill's bounded-CI-poll pattern (`.claude/skills/regatta-operator/SKILL.md:455-484`) sleeps the operator session 60s/tick × up to 10 ticks × N PRs in a merge wave. Session 2026-06-10 burned \~27 min of operator wall-clock waiting for 4 PRs to clear CI. Same trap motivated MAX_TICKS=10 cap in session 5 (PRs … (truncated, use `get_issue` for full description)

---

### MAY-56 — [CORE] spawner: retry-on-provider_credit_exhausted (lose 12 agents/session to subscription rate-limit)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-56/core-spawner-retry-on-provider-credit-exhausted-lose-12-agentssession

> **Migrated from** [GH #1216](<https://github.com/themaydow/regatta/issues/1216>) · GH labels: autonomous

## Surface

`[CORE]` — regatta-core spawner / provider gateway

## Finding

Live operator session (2026-06-10) observed `exit_reason=provider_credit_exhausted` × 12+ across spawned claude subprocesses. Pattern: subscription (`CLAUDE_CODE_OAUTH_TOKEN`) hits per-minute rate-limit during burst dispatch; affected agents exit `exit_code=1` mid-… (truncated, use `get_issue` for full description)

---

## M3 — CI simplification

### MAY-33 — [wedge] CI simplification 2026-06-10: delete > add (empirical-not-marketing)

- Priority: U
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-33/wedge-ci-simplification-2026-06-10-delete-add-empirical-not-marketing

> **Migrated from** [GH #1263](<https://github.com/themaydow/regatta/issues/1263>) · GH labels: kind:wedge, priority:p0, scope:ci

## Umbrella

Tracks delivery of CI/gate simplification wedges identified via 2-adversary audit (deletion-angle + refactor-angle). Audit driven by empirical observation that PR #1248 cycle = 80.7min wall-clock, but CI itself = 8.9min (11%); remaining 89% is reviewer-loop + body-snapshot-lag empty-commit ritual.

**Aut… (truncated, use `get_issue` for full description)

---

### MAY-274 — [CI] Demote test (macos-latest) from blocking required check

- Priority: H
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-274/ci-demote-test-macos-latest-from-blocking-required-check

## Problem

`test (macos-latest)` runs on every PR as a required check. macos runners are slow (cold start) and rarely catch OS-specific bugs (last real catch: never in 90d session log). Adds ~3min wall to every merge.

## Fix

Demote to nightly: remove from required-checks branch protection. Keep workflow running on push to main (catches regressions post-merge).

## Acceptance

- [ ] `.github/branch-protection.yml` (or settings) drops `test (ma… (truncated, use `get_issue` for full description)

---

### MAY-44 — [CI] dev-velocity: measure gate ROI + fast-path doc PRs + squash trivial TDD + retire dead gates

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-44/ci-dev-velocity-measure-gate-roi-fast-path-doc-prs-squash-trivial-tdd

> **Migrated from** [GH #1247](<https://github.com/themaydow/regatta/issues/1247>) · GH labels: autonomous

## Surface

`[CI]` — Makefile.d/ + scripts/check-\*.sh + workflows

## Finding

2026-06-10 operator feedback (live): "dev process so slow."

Empirical (lens 4 measurement + this-session observation):

* 31 `check-*.sh` scripts in `make check` (verified `ls scripts/check-*.sh | wc -l`).
* Local `make check` 5-10 min wall-clock; CI `verify-g… (truncated, use `get_issue` for full description)

---

## M4 — Orchestrator resilience

### MAY-122 — w9-replay-diff T2: Replay engine + diff harness + re-executor registry

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-122/w9-replay-diff-t2-replay-engine-diff-harness-re-executor-registry

> **Source plan**: [2026-06-01-w9-replay-diff-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/history/replay.go` (NEW… (truncated, use `get_issue` for full description)

---

### MAY-125 — w9-replay-diff T3: Operator UI replay button + background job

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-125/w9-replay-diff-t3-operator-ui-replay-button-background-job

> **Source plan**: [2026-06-01-w9-replay-diff-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/uiserver/replay.go` (NE… (truncated, use `get_issue` for full description)

---

### MAY-127 — w9-replay-diff T4: OTel attrs + non-determinism quarantine

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-127/w9-replay-diff-t4-otel-attrs-non-determinism-quarantine

> **Source plan**: [2026-06-01-w9-replay-diff-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/history/otel.go` (NEW):… (truncated, use `get_issue` for full description)

---

### MAY-129 — w9-replay-diff T5: P2.5 trigger metrics + alert

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-129/w9-replay-diff-t5-p25-trigger-metrics-alert

> **Source plan**: [2026-06-01-w9-replay-diff-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/history/metrics.go` (NE… (truncated, use `get_issue` for full description)

---

### MAY-132 — w9-replay-diff T6: Temporal-backed impl design doc (markdown stub only)

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-132/w9-replay-diff-t6-temporal-backed-impl-design-doc-markdown-stub-only

> **Source plan**: [2026-06-01-w9-replay-diff-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w9-replay-diff-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w9-replay-diff-harness-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/history/temporal/README… (truncated, use `get_issue` for full description)

---

### MAY-68 — BUG: orchestrator worktrees leak onto host filesystem via :/repo bind-mount

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-68/bug-orchestrator-worktrees-leak-onto-host-filesystem-via-repo-bind

PARTIALLY MITIGATED — repo-integrity fixed, host-fs residual remains.

Shipped (commit 8ea6655): `.regatta/worktrees/` added to `.gitignore` → worktrees no longer leak into version control.

RESIDUAL (low priority): docker-compose binds `${REPO_PATH:-.}:/repo` (docker-compose.yml:78); worktrees created inside the container at `/``repo/.regatta/worktrees/agent-``N/` still persist on the HOST filesystem after container teardown (26MB found in 2026… (truncated, use `get_issue` for full description)

---

### MAY-58 — [ORCH] reaper: sweep terminal-agent worktrees + remote refs (disk-leak prevention)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-58/orch-reaper-sweep-terminal-agent-worktrees-remote-refs-disk-leak

> **Migrated from** [GH #1213](<https://github.com/themaydow/regatta/issues/1213>) · GH labels: autonomous

## Surface

`[ORCH]` — orchestrator reaper

## Finding

The reaper (`internal/orchestrator/reaper/reaper.go`) handles agent-process death + state-DB lifecycle transitions, but does NOT sweep:

1. The host filesystem worktree `.regatta/worktrees/agent-<N>/` — accumulates across restarts; survives PR merge + agent done.
2. The orchestrator-p… (truncated, use `get_issue` for full description)

---

### MAY-60 — [PLAN] Next-3 unblocker wave: #1098 #1096 #1094 #1093 #1092 (post #1202 #1203)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-60/plan-next-3-unblocker-wave-1098-1096-1094-1093-1092-post-1202-1203

> **Migrated from** [GH #1205](<https://github.com/themaydow/regatta/issues/1205>) · GH labels:

## Context

Meta-reviewer (`a4f5211da019d7073`) flagged top-3 wave (#1061/#1062/#1065) as recency-bias pick. KILL'd #1062. Shipped #1061/#1065. Better next-3 candidates:

## Ranked

1. **#1098** — `tick.started`/`tick.completed` INFO every 5s drowns operator log signal. Live operator-UX pain, sub-50-LoC fix.
2. **#1096** — `agent.exited` `Credit bala… (truncated, use `get_issue` for full description)

---

### MAY-69 — BUG: parallel UI PR storm caused 4 rebase cycles on internal/web/dashboard.go + _test.go

- Priority: ·
- Status: In Progress
- Assignee: Tri Lam
- Linear: https://linear.app/themaydow/issue/MAY-69/bug-parallel-ui-pr-storm-caused-4-rebase-cycles-on

SHIPPED — PR #1289 merged 2026-06-21 (squash). Split the 1071-line internal/web/dashboard.go god-file (the cascade-rebase anchor: 14+ UI PRs all touched it → 4+ rebase cycles) into dashboard.go (304, core: shared types/routes/helpers) + 6 per-concern files (agents 125, workitems 248, pipeline 157, events 101, spend 125, soak 79), all package web. 29 test funcs relocated to matching _test.go, none added/dropped. MOVE-ONLY verified: 59 prod symbol… (truncated, use `get_issue` for full description)

---

### MAY-87 — [ADOPT] Operator soak harness — merge→rebuild→restart→drain loop (consolidates MAY-87/93/94)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-87/adopt-operator-soak-harness-mergerebuildrestartdrain-loop-consolidates

ADOPT-NOT-BUILD consolidation (2026-06-20 autonomous re-audit): <issue id="f2854e26-ecbb-4e37-ba5e-c2de4e1213b2" href="https://linear.app/themaydow/issue/MAY-87/bug-1079-regatta-serve-doesnt-self-restart-on-binary-update-operator">MAY-87</issue> (self-restart on binary update), [[<issue id="04d16fb9-1132-485c-82fa-3ea95372bea3" href="https://linear.app/themaydow/issue/MAY-93/bug-1071-no-first-class-pauseresume-cli-for-in-flight-agents-session">M… (truncated, use `get_issue` for full description)

---

## M5 — Adapter expansion (Linear-first)

### MAY-92 — BUG-1072: markdown_catalog adapter unwired; operator cannot point regatta at filesystem roadmap docs

- Priority: ·
- Status: In Progress
- Assignee: Tri Lam
- Linear: https://linear.app/themaydow/issue/MAY-92/bug-1072-markdown-catalog-adapter-unwired-operator-cannot-point

ALREADY SHIPPED — closed not-dispatched (2026-06-21 audit-before-implement). The markdown_catalog adapter is fully wired on main since #872 (commit bf90a0c): adapter impl internal/orchestrator/adapter/markdown.go (NewMarkdownCatalog, List/Get/UpdateStatus/Capabilities); dispatch by regatta.yaml::spec_adapter.type in cmd/regatta/wire_spec_adapter.go:18-46 (markdown_catalog = default branch L41-45); YAML root resolution wire_flags.go:82-89 + wire_… (truncated, use `get_issue` for full description)

---

## M6 — UX polish

### MAY-37 — F9: regatta agents withdraw/kill/retry CLI commands

- Priority: H
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-37/f9-regatta-agents-withdrawkillretry-cli-commands

> **Migrated from** [GH #1258](<https://github.com/themaydow/regatta/issues/1258>) · GH labels: severity:medium, kind:slice, priority:p1, scope:ux

Umbrella: #1249

## Problem

Stuck agent → `regatta agents list` → grep ID → `sqlite3 regatta.db "UPDATE ..."` → restart serve. 4 steps + raw SQL. From audit §3 F9.

## Acceptance criteria

* \[planned\] AC1: `regatta agents withdraw <id>` CLI command
* \[planned\] AC2: `regatta agents kill <id>` (SI… (truncated, use `get_issue` for full description)

---

### MAY-42 — F4: obs.WorkerLoop per-goroutine heartbeat wrapper

- Priority: H
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-42/f4-obsworkerloop-per-goroutine-heartbeat-wrapper

> **Migrated from** [GH #1253](<https://github.com/themaydow/regatta/issues/1253>) · GH labels: severity:high, kind:slice, priority:p1

Umbrella: #1249

## Problem

Silent-stall heartbeat decoupling — heartbeat alive while worker goroutine wedged on synchronous IO = false-OK. #1218 + #1227 both shipped fixes in last 24h for same class of bug. Root cause architectural. From audit §3 F4.

## Acceptance criteria

* \[planned\] AC1: `obs.WorkerLoop(… (truncated, use `get_issue` for full description)

---

### MAY-43 — [wedge] UX audit Phase-S 2026-06-10: 12 wedges + 8 quick wins

- Priority: H
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-43/wedge-ux-audit-phase-s-2026-06-10-12-wedges-8-quick-wins

> **Migrated from** [GH #1249](<https://github.com/themaydow/regatta/issues/1249>) · GH labels: kind:wedge, priority:p1, scope:ux

## Umbrella

Tracks delivery of UX-audit wedges identified in PR #1248. The audit catalogs 12 ranked findings (F1-F12) + 8 quick wins (QW1-8) across 7 UX categories; this umbrella tracks slice issues for each.

**Authoritative doc**: `docs/engineer/briefs/2026-06-10-ux-audit.md` (merged in #1248)

**Headline conclusi… (truncated, use `get_issue` for full description)

---

### MAY-34 — F12: phone-readable triage view (single-column + incidents-feed)

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-34/f12-phone-readable-triage-view-single-column-incidents-feed

> **Migrated from** [GH #1261](<https://github.com/themaydow/regatta/issues/1261>) · GH labels: severity:low, kind:slice, priority:p2, scope:ux

Umbrella: #1249

## Problem

Operator off-laptop cannot triage. Mobile drawer = 100% width + grid cramped underneath. No above-the-fold "is anything broken?" summary. From audit §3 F12.

## Acceptance criteria

* \[planned\] AC1: below 768px collapse all panels to single "incidents + agents" feed sorted… (truncated, use `get_issue` for full description)

---

### MAY-35 — F11: Escape closes drawer + keyboard nav

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-35/f11-escape-closes-drawer-keyboard-nav

> **Migrated from** [GH #1260](<https://github.com/themaydow/regatta/issues/1260>) · GH labels: severity:low, kind:slice, priority:p2, scope:ux

Umbrella: #1249

## Problem

Drawer has no Escape close. No `/` search. No arrow-key list nav. Power operator opens drawer w/ click, must mouse-back. From audit §3 F11.

## Acceptance criteria

* \[planned\] AC1: Escape key closes drawer
* \[planned\] AC2: focus trap inside drawer + restore focus to tri… (truncated, use `get_issue` for full description)

---

### MAY-36 — F10: group regatta serve --help flags by category

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-36/f10-group-regatta-serve-help-flags-by-category

> **Migrated from** [GH #1259](<https://github.com/themaydow/regatta/issues/1259>) · GH labels: severity:low, kind:slice, priority:p2, scope:ux

Umbrella: #1249

## Problem

First-time operator scans `regatta serve --help`, sees wall of 18 flags, no idea which 3 matter. From audit §3 F10.

## Acceptance criteria

* \[planned\] AC1: modify `parseServeFlags()` help text to emit grouped sections
* \[planned\] AC2: groups: `# Database` / `# Timing` … (truncated, use `get_issue` for full description)

---

### MAY-38 — F8: operator-vocabulary rename (L0/L4/substrate → operator terms)

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-38/f8-operator-vocabulary-rename-l0l4substrate-operator-terms

> **Migrated from** [GH #1257](<https://github.com/themaydow/regatta/issues/1257>) · GH labels: severity:medium, kind:slice, priority:p2, scope:ux

Umbrella: #1249

## Problem

"L0 gate", "L4 gate", "substrate", "ProgramBrief", "rejection_count" — useful for code review, hostile for operator UX. Operator reads "L4 gate verdict" and shrugs. From audit §3 F8.

Per round-1 adversarial review: effort estimate is M-L not S — term leaks beyond UI temp… (truncated, use `get_issue` for full description)

---

## M7 — Agent prompt + review discipline

### MAY-275 — [FOLLOWUP] Fix PIPESTATUS sampling in CI-check compress pattern (false make-check-clean reports)

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-275/followup-fix-pipestatus-sampling-in-ci-check-compress-pattern-false

## Problem

The CLAUDE.md `feedback_subagent_cicheck_compress` pattern instructs:

```
make check 2>&1 | tee /tmp/cicheck.log | grep -E "^(FAIL|ok|---|Error|error:|PASS)" | tail -40
```

Implementers paired with `echo "exit=$?"` get TAIL's exit code, NOT make's. Pipeline w/o `set -o pipefail` masks the lead command's exit. Session 2026-06-21 PR #1313 reviewer caught a false "make check exit=0" claim because the gate had actually failed.

## Fix
… (truncated, use `get_issue` for full description)

---

### MAY-46 — [OPS] .github/ISSUE_TEMPLATE/{wedge,reviewer-finding}.yml + skill FEED rewire

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-46/ops-githubissue-templatewedgereviewer-findingyml-skill-feed-rewire

> **Migrated from** [GH #1242](<https://github.com/themaydow/regatta/issues/1242>) · GH labels: autonomous

## Surface

`[OPS]` — `.github/ISSUE_TEMPLATE/`

## Finding

Adversarial-reviewed velocity audit — survivor G (scoped — drop helper script).

Operator filing reviewer findings, wedges, post-merge audit issues = remembering exact title prefix + body sections. Operator composes HEREDOC each time; subagents drift; `#1220` classifier trips on … (truncated, use `get_issue` for full description)

---

### MAY-55 — [OPS] classifier: batch-mode permits reviewer-token paste; singular blocks (~30min loss/session)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-55/ops-classifier-batch-mode-permits-reviewer-token-paste-singular-blocks

> **Migrated from** [GH #1220](<https://github.com/themaydow/regatta/issues/1220>) · GH labels: autonomous

## Surface

`[OPS]` — operator-loop classifier (Claude Code auto-mode)

## Finding

PR #1207 (prwatch exit-4 fix) was authored by an implementer subagent; reviewer (separate `cavecrew-fix-prwatch-exit4` subagent) returned APPROVE. Operator (main thread) attempted to paste the reviewer's `Reviewer-agent-id:` + `Reviewer-recommendation: APPR… (truncated, use `get_issue` for full description)

---

### MAY-59 — [OPS] regatta-operator skill: pre-flight must verify spec_adapter.type matches FEED target

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-59/ops-regatta-operator-skill-pre-flight-must-verify-spec-adaptertype

> **Migrated from** [GH #1212](<https://github.com/themaydow/regatta/issues/1212>) · GH labels: autonomous

## Surface

`[OPS]` — operator-loop skill

## Finding

The `regatta-operator` skill (`.claude/skills/regatta-operator/SKILL.md`) FEED phase writes wedges as GitHub issues with the `autonomous` label, but the skill does NOT pre-flight-check `regatta.yaml::spec_adapter.type` against this assumption. Per live 2026-06-10 session:

* Prior to P… (truncated, use `get_issue` for full description)

---

### MAY-61 — [FOLLOW-UP #1203] check-prompt-parity: enforce gist ≤80 chars + slug suffix on anchored-rule lines

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-61/follow-up-1203-check-prompt-parity-enforce-gist-80-chars-slug-suffix

> **Migrated from** [GH #1204](<https://github.com/themaydow/regatta/issues/1204>) · GH labels:

## Context

PR #1203 stripped 2 anchored-slug rule bodies from `defaultPromptBuilder` per BUG-1061. Per meta-review, c2 of the original issue (`scripts/check-prompt-parity.sh` extension to validate `≤120-char` body length) was punted to follow-up — `feedback_default_simpler` says don't pre-build for drift until it recurs.

## Trigger

File a third PR… (truncated, use `get_issue` for full description)

---

### MAY-70 — BUG: pr-lint workflow uses stale body snapshot — auto-empty-commit on body edit

- Priority: ·
- Status: In Progress
- Assignee: Tri Lam
- Linear: https://linear.app/themaydow/issue/MAY-70/bug-pr-lint-workflow-uses-stale-body-snapshot-auto-empty-commit-on

ALREADY SHIPPED by PR #1271 (commit 567e86b) — closed not-dispatched (2026-06-21 audit). pr-lint stale-body-snapshot is fixed: all 4 body-reading gates (pr-lint-release-notes, pr-lint-tdd, pr-lint-reviewer-verdict, pr-lint-byte-equal-pin) do a live `gh pr view --json body` fetch with SNAPSHOT_BODY fallback AND trigger on `pull_request: types: [edited]`. A body edit now fires a fresh run reading the live body — the manual empty-commit ritual is o… (truncated, use `get_issue` for full description)

---

## M8 — Reviewer-finding burndown

### MAY-28 — [REVIEWER #1266] LOW stale-gate-refs in 29 shipped/draft specs (banned-phrase + comment-density)

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-28/reviewer-1266-low-stale-gate-refs-in-29-shippeddraft-specs-banned

> **Migrated from** [GH #1277](<https://github.com/themaydow/regatta/issues/1277>) · GH labels: severity:low, kind:slice, priority:p2, scope:ci

Round-7 reviewer (`ae2e7a463f9b5b7fa`) found 39 total stale refs to deleted gates across docs/. Active dispatch templates + operator surfaces fixed inline in #1266. Remaining 29 specs/plans defer to this issue per `feedback_drop_ceremony`.

Files w/ stale refs (status):

* 8 active specs (chat-notifier,… (truncated, use `get_issue` for full description)

---

### MAY-29 — [REVIEWER #1266] LOW stale-doc: chat-notifier spec §6.4 cites deleted check-comment-density gate

- Priority: M
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-29/reviewer-1266-low-stale-doc-chat-notifier-spec-64-cites-deleted-check

> **Migrated from** [GH #1274](<https://github.com/themaydow/regatta/issues/1274>) · GH labels: severity:low, kind:slice, priority:p2, scope:ci

Reviewer round-3 (`ac894f962a809528f`) flagged `docs/engineer/specs/2026-06-08-chat-notifier-integration.md:177` still cites deleted `check-comment-density` gate as CI-green requirement.

Inline fix in #1266 triggers `check-spec-sections` strict mode — spec lacks 3 canonical H2 sections (Design, Out of … (truncated, use `get_issue` for full description)

---

### MAY-62 — [REVIEWER #1163] MED bug: recurrence rule summary omits reopen-closed-issue path

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-62/reviewer-1163-med-bug-recurrence-rule-summary-omits-reopen-closed

> **Migrated from** [GH #1198](<https://github.com/themaydow/regatta/issues/1198>) · GH labels: autonomous

## Retro review finding

`.claude/skills/regatta-operator/SKILL.md` recurrence rule summary (early in file) says "Same root cause hits ≥2 agents/iterations → exactly ONE tracker issue, bump occurrence counter via PR-comment."

The full recurrence rule documented later includes a reopen path: "If a closed issue matches → reopen with 'regres… (truncated, use `get_issue` for full description)

---

### MAY-63 — [REVIEWER #1163] HIGH bug: docker inspect uses service name not container identity — binary-staleness check fails when container_name != service_name

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-63/reviewer-1163-high-bug-docker-inspect-uses-service-name-not-container

> **Migrated from** [GH #1197](<https://github.com/themaydow/regatta/issues/1197>) · GH labels: autonomous

## Retro review finding

`.claude/skills/regatta-operator/SKILL.md` tight feedback loop step 5 says:

```bash
docker inspect --format '{{.Image}}' "$DOCKER_SERVICE"
```

`$DOCKER_SERVICE` is the docker-compose service name. `docker inspect` takes a container or image name. When the compose file pins `container_name: regatta` explicitly (wh… (truncated, use `get_issue` for full description)

---

### MAY-64 — [REVIEWER #1163] HIGH bug: git API endpoint returns single ref, not array — agent-branch deletion will fail

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-64/reviewer-1163-high-bug-git-api-endpoint-returns-single-ref-not-array

> **Migrated from** [GH #1196](<https://github.com/themaydow/regatta/issues/1196>) · GH labels: autonomous

## Retro review finding

Retro three-lens reviewer on PR #1163 (regatta-operator skill v1; merged with stale REVISE token per #1189) flagged HIGH bug:

`.claude/skills/regatta-operator/SKILL.md §Sandbox target-repo reset` calls:

```bash
gh api "repos/$TARGET_REPO/git/refs/heads/regatta" --jq '.[].ref'
```

GitHub REST `GET /repos/{owner}/… (truncated, use `get_issue` for full description)

---

### MAY-65 — [REVIEWER #1186] MED risk: bounded poll loop entry assumes CI ran; BLOCKED-state edge case untested

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-65/reviewer-1186-med-risk-bounded-poll-loop-entry-assumes-ci-ran-blocked

> **Migrated from** [GH #1194](<https://github.com/themaydow/regatta/issues/1194>) · GH labels: autonomous

## Retro review finding

Loop entry condition `until [ mergeStateStatus = CLEAN ]` assumes CI ran successfully. On BLOCKED state (CI timeout / resource exhaustion), `gh pr checks <N>` may return empty output or error. The failure-detection grep won't fire on empty output; the loop depends entirely on it. No fallback to iteration-cap timeou… (truncated, use `get_issue` for full description)

---

### MAY-66 — [REVIEWER #1186] MED bug: bounded-poll subsection forward-references itself + section order backwards

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-66/reviewer-1186-med-bug-bounded-poll-subsection-forward-references

> **Migrated from** [GH #1193](<https://github.com/themaydow/regatta/issues/1193>) · GH labels: autonomous

## Retro review finding

Retro three-lens reviewer on PR #1186 found:

Step 5 of regatta-operator skill bottleneck-resolution loop says "via the bounded CI poll **above**", but the bounded-poll subsection appears 62 lines BELOW step 5 in the rendered skill file.

Readers following numbered steps hit a dangling anchor + information is prese… (truncated, use `get_issue` for full description)

---

## M9 — Multi-phase work shapes (deferred)

### MAY-119 — w8-opa-rbac T5: OTel attrs + audit event + property tests + operator doc + Makefile target

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-119/w8-opa-rbac-t5-otel-attrs-audit-event-property-tests-operator-doc

> **Source plan**: [2026-06-01-w8-opa-rbac-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w8-opa-rbac-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/authz/otel.go` (NEW): `setAuthzAttrs(span, decision, tenant, action… (truncated, use `get_issue` for full description)

---

### MAY-134 — w10-sigstore T1: Sign + Verify wrapper + sigstore-go SDK adoption

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-134/w10-sigstore-t1-sign-verify-wrapper-sigstore-go-sdk-adoption

> **Source plan**: [2026-06-01-w10-sigstore-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w10-sigstore-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/sign/sigstore/sign.go` (NEW): `Sign(ctx, artifa… (truncated, use `get_issue` for full description)

---

### MAY-139 — w10-sigstore T2: CI workflow signs every release artifact via GitHub OIDC keyless

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-139/w10-sigstore-t2-ci-workflow-signs-every-release-artifact-via-github

> **Source plan**: [2026-06-01-w10-sigstore-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w10-sigstore-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md>)
> **Status at plan time**: queued

## Scope

* `.github/workflows/release.yml`: extend release job — add… (truncated, use `get_issue` for full description)

---

### MAY-140 — w10-sigstore T3: Policy-bundle Verify integration (W8 loader hookup)

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-140/w10-sigstore-t3-policy-bundle-verify-integration-w8-loader-hookup

> **Source plan**: [2026-06-01-w10-sigstore-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w10-sigstore-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/gates/authz/loader.go` (W8-created): ADD `sigst… (truncated, use `get_issue` for full description)

---

### MAY-142 — w10-sigstore T4: Pricing-table Verify integration

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-142/w10-sigstore-t4-pricing-table-verify-integration

> **Source plan**: [2026-06-01-w10-sigstore-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w10-sigstore-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/cost/pricing/pricing_verify.go` (NEW): per spec… (truncated, use `get_issue` for full description)

---

### MAY-143 — w10-sigstore T5: Local-dev fallback keys + regatta sign CLI + build-tag gate

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-143/w10-sigstore-t5-local-dev-fallback-keys-regatta-sign-cli-build-tag

> **Source plan**: [2026-06-01-w10-sigstore-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w10-sigstore-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md>)
> **Status at plan time**: queued

## Scope

* `cmd/regatta/sign.go` (NEW): cobra root for `regatta sign… (truncated, use `get_issue` for full description)

---

### MAY-146 — w10-sigstore T6: OTel + audit events + plan-as-code loader stub + operator docs

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-146/w10-sigstore-t6-otel-audit-events-plan-as-code-loader-stub-operator

> **Source plan**: [2026-06-01-w10-sigstore-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w10-sigstore-tasks.md>)
> **Spec**: [docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/phase-x/2026-06-01-w10-sigstore-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/sign/sigstore/otel.go` (NEW): `VerifyWithSpan(c… (truncated, use `get_issue` for full description)

---

### MAY-148 — w11-blackboard T1: Blobs primitive + migration #0008

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-148/w11-blackboard-t1-blobs-primitive-migration-0008

> **Source plan**: [2026-06-01-w11-blackboard-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w11-blackboard-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w11-blackboard-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/orchestrator/state/migrations/0008_blackboard_blobs.sql… (truncated, use `get_issue` for full description)

---

### MAY-150 — w11-blackboard T2: Fact-kind registry + reducer dispatch

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-150/w11-blackboard-t2-fact-kind-registry-reducer-dispatch

> **Source plan**: [2026-06-01-w11-blackboard-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w11-blackboard-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w11-blackboard-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/orchestrator/blackboard/payload.go` (NEW): `FactPayload… (truncated, use `get_issue` for full description)

---

### MAY-151 — w11-blackboard T3: TailFacts API + cursor semantics

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-151/w11-blackboard-t3-tailfacts-api-cursor-semantics

> **Source plan**: [2026-06-01-w11-blackboard-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w11-blackboard-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w11-blackboard-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/orchestrator/blackboard/tail.go` (NEW): `TailFacts(ctx,… (truncated, use `get_issue` for full description)

---

### MAY-152 — w11-blackboard T4: Blob orphan GC sweep job

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-152/w11-blackboard-t4-blob-orphan-gc-sweep-job

> **Source plan**: [2026-06-01-w11-blackboard-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w11-blackboard-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w11-blackboard-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/orchestrator/blackboard_gc/sweep.go` (NEW): per spec §3… (truncated, use `get_issue` for full description)

---

### MAY-153 — w11-blackboard T5: OTel attrs + operator runbook + (A+) lints

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-153/w11-blackboard-t5-otel-attrs-operator-runbook-a-lints

> **Source plan**: [2026-06-01-w11-blackboard-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w11-blackboard-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w11-blackboard-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w11-blackboard-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/orchestrator/blackboard/otel.go` (NEW): OTel attr-key c… (truncated, use `get_issue` for full description)

---

### MAY-154 — w12-billing T1: Billing-period rollup job

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-154/w12-billing-t1-billing-period-rollup-job

> **Source plan**: [2026-06-01-w12-billing-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w12-billing-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w12-billing-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w12-billing-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/billing/event/payload.go` (NEW): typed payloads per spec §3.7 lines… (truncated, use `get_issue` for full description)

---

### MAY-155 — w12-billing T2: Stripe adapter + idempotency

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-155/w12-billing-t2-stripe-adapter-idempotency

> **Source plan**: [2026-06-01-w12-billing-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w12-billing-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w12-billing-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w12-billing-design.md>)
> **Status at plan time**: queued

## Scope

* `go.mod` + `go.sum`: vendor `github.com/stripe/stripe-go/v76` (pin patch, no … (truncated, use `get_issue` for full description)

---

### MAY-156 — w12-billing T3: Invoice markdown template

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-156/w12-billing-t3-invoice-markdown-template

> **Source plan**: [2026-06-01-w12-billing-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w12-billing-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w12-billing-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w12-billing-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/billing/invoice/template.md.tmpl` (NEW): spec §3.4 lines 205-228 ve… (truncated, use `get_issue` for full description)

---

### MAY-157 — w12-billing T4: regatta billing close CLI

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-157/w12-billing-t4-regatta-billing-close-cli

> **Source plan**: [2026-06-01-w12-billing-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w12-billing-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w12-billing-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w12-billing-design.md>)
> **Status at plan time**: queued

## Scope

* `cmd/regatta/billing.go` (NEW): `regatta billing close --period YYYY-MM [--te… (truncated, use `get_issue` for full description)

---

### MAY-158 — w12-billing T5: Operator UI billing tab

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-158/w12-billing-t5-operator-ui-billing-tab

> **Source plan**: [2026-06-01-w12-billing-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w12-billing-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w12-billing-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w12-billing-design.md>)
> **Status at plan time**: queued

## Scope

* `internal/web/billing.go` (NEW): per spec §3.6 lines 280-313:
  * `RegisterBi… (truncated, use `get_issue` for full description)

---

### MAY-159 — w12-billing T6: OTel + operator doc

- Priority: L
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-159/w12-billing-t6-otel-operator-doc

> **Source plan**: [2026-06-01-w12-billing-tasks.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/plans/2026-06-01-w12-billing-tasks.md>)
> **Spec**: [docs/engineer/specs/2026-06-01-w12-billing-design.md](<https://github.com/themaydow/regatta/blob/main/docs/engineer/specs/2026-06-01-w12-billing-design.md>)
> **Status at plan time**: queued

## Scope

* `docs/operator/billing.md` (NEW; \~300 lines):
  * Close ritual end-to-end (C… (truncated, use `get_issue` for full description)

---

### MAY-101 — [RESEARCH-DELTA] SPRT early-stop on K=10 repro cron (Task 5 amend)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-101/research-delta-sprt-early-stop-on-k10-repro-cron-task-5-amend

> **Migrated from** [GH #1030](<https://github.com/themaydow/regatta/issues/1030>) · GH labels: autonomous

## Delta 3.7 — SPRT early-stop on K=10 repro

Spec §4.1 hardcodes K=10 seeds. If first 3 seeds wildly confirm or wildly refute, remaining 7 are wasted budget.

**Adopt**: SPRT (Wald 1945); impl over gonum/stat distuv ([https://github.com/gonum/gonum](<https://github.com/gonum/gonum>)) v0.15.1, BSD-3. gonum is NOT in go.mod today; lands as … (truncated, use `get_issue` for full description)

---

### MAY-102 — [RESEARCH-DELTA] adversarial-thesis subagent role (Task 9)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-102/research-delta-adversarial-thesis-subagent-role-task-9

> **Migrated from** [GH #1029](<https://github.com/themaydow/regatta/issues/1029>) · GH labels: autonomous

## Delta 3.6 — Adversarial-thesis subagent role

CLAUDE.md `feedback_adversarial_review_every_step` says every load-bearing artifact gets an adversarial pass. Spec §3 has four gate runners but no role for an adversarial subagent that tries to *break* the thesis itself (find counter-examples, propose null-hypothesis interpretations, hunt un… (truncated, use `get_issue` for full description)

---

### MAY-103 — [RESEARCH-DELTA] in-toto/SLSA envelope on KindReproVerdict (Task 0 amend)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-103/research-delta-in-totoslsa-envelope-on-kindreproverdict-task-0-amend

> **Migrated from** [GH #1028](<https://github.com/themaydow/regatta/issues/1028>) · GH labels: autonomous

## Delta 3.5 — in-toto / SLSA envelope shape for `KindReproVerdict`

Spec §2.3 defines `KindReproVerdict` payload as flat JSON object signed by HMAC. Verifiable-evidence consumers (external auditors, future supply-chain checks) expect a predicate-shaped envelope.

**Adopt**: in-toto Attestation Framework v1.0 ([https://github.com/in-toto/a… (truncated, use `get_issue` for full description)

---

### MAY-23 — [RESEARCH-DELTA] thesis-graveyard query primitive (Task 7)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-23/research-delta-thesis-graveyard-query-primitive-task-7

> **Migrated from** [GH #1024](<https://github.com/themaydow/regatta/issues/1024>) · GH labels: autonomous

## Delta 3.1 — Thesis-graveyard query primitive

Spec covers `CriterionStateRefuted` as terminal state. No primitive lets a new thesis query "has this hypothesis shape been tried + refuted before?" before dispatch. Without it, loop re-walks dead branches.

**Adopt**: `claude-mem` corpus (already in repo plugin set per `.claude/settings.jso… (truncated, use `get_issue` for full description)

---

### MAY-24 — [RESEARCH-DELTA] base-rate prior consultation gate (Task 1 amend + implementer.md)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-24/research-delta-base-rate-prior-consultation-gate-task-1-amend

> **Migrated from** [GH #1025](<https://github.com/themaydow/regatta/issues/1025>) · GH labels: autonomous

## Delta 3.2 — Base-rate prior consultation gate

Spec has no rule requiring a thesis to consult prior runs before dispatch. Adversarial-review-every-step rule in CLAUDE.md applies post-dispatch; base-rate is a pre-dispatch hook.

**Adopt**: same `claude-mem` substrate as delta 3.1; LangGraph checkpointers (reference pattern only). No new … (truncated, use `get_issue` for full description)

---

### MAY-25 — [RESEARCH-DELTA] OTel traceparent + seed propagation through Spawner (Task 5 amend)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-25/research-delta-otel-traceparent-seed-propagation-through-spawner-task

> **Migrated from** [GH #1026](<https://github.com/themaydow/regatta/issues/1026>) · GH labels: autonomous

## Delta 3.3 — OTel `traceparent` + seed propagation through Spawner

Spec §4.1 step 3 runs K=10 fresh seeds via `Spawner.Spawn`. No mention of how `{trace_id, rng_seed, model_sha, prompt_sha}` propagates into each subagent invocation. Without it, K=10 cron cannot be replayed bit-exact.

**Adopt**: OpenTelemetry Go SDK v1.30.0, Apache-2.0 … (truncated, use `get_issue` for full description)

---

### MAY-26 — [RESEARCH-DELTA] content-addressed LLM memo cache (Task 8)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-26/research-delta-content-addressed-llm-memo-cache-task-8

> **Migrated from** [GH #1027](<https://github.com/themaydow/regatta/issues/1027>) · GH labels: autonomous

## Delta 3.4 — Content-addressed LLM memo cache

Spec has no memoization layer. Identical sub-queries (canary-extraction prompts repeated across K=10 seeds, repeated leakage scans against same `train_manifest_sha`) re-spend LLM tokens.

**Adopt**: Bazel REAPI CAS digest shape (`sha256:<hex>/<size>`) over substrate row; impl trivial in SQLi… (truncated, use `get_issue` for full description)

---

### MAY-27 — [RESEARCH-DELTA] in-toto/SLSA envelope on KindReproVerdict (Task 0 amend)

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-27/research-delta-in-totoslsa-envelope-on-kindreproverdict-task-0-amend

> **Migrated from** [GH #1028](<https://github.com/themaydow/regatta/issues/1028>) · GH labels: autonomous

## Delta 3.5 — in-toto / SLSA envelope shape for `KindReproVerdict`

Spec §2.3 defines `KindReproVerdict` payload as flat JSON object signed by HMAC. Verifiable-evidence consumers (external auditors, future supply-chain checks) expect a predicate-shaped envelope.

**Adopt**: in-toto Attestation Framework v1.0 ([https://github.com/in-toto/a… (truncated, use `get_issue` for full description)

---

### MAY-82 — BUG-1084: orchestrator dispatches implementer only; research-then-build shapes (RESEARCH-DELTA + wedges) can't designer-first

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-82/bug-1084-orchestrator-dispatches-implementer-only-research-then-build

> **Migrated from** [GH #1084](<https://github.com/themaydow/regatta/issues/1084>) · GH labels: autonomous

## Symptom

Today scheduler dispatches `implementer`-role agents only. Some roadmap items mandate a **research/design step first** — e.g. #832 body says *"Audit Sloth SLO surface first.* `slo/*.yaml` *may already alert on latency/cost outliers — if so, R6+R7 are duplicate work. Reject any rule that reinvents an existing alert primitive."*
… (truncated, use `get_issue` for full description)

---

### MAY-83 — BUG-1083: WorkItem schema is single-phase; multi-phase roadmap items (e.g. #832) conflate into one dispatch

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-83/bug-1083-workitem-schema-is-single-phase-multi-phase-roadmap-items-eg

> **Migrated from** [GH #1083](<https://github.com/themaydow/regatta/issues/1083>) · GH labels: autonomous

## Symptom

WorkItem schema (`contracts/schemas/work_item.schema.json` + `contracts/schemas/spec_adapter.go::WorkItem`) is single-phase: one acceptance set, one dispatch, one PR. Many real roadmap items are multi-phase by design — e.g. #832 explicitly states *"Phase A: R6+R7+R8 (1wk). Phase B: R9+R10+R11 (2wk). Phase C: Autotuner."* Each p… (truncated, use `get_issue` for full description)

---

### MAY-84 — BUG-1082: orchestrator ignores ## Reopen trigger + ## Why deferred sections; wedge issues dispatched as ready

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-84/bug-1082-orchestrator-ignores-reopen-trigger-why-deferred-sections

> **Migrated from** [GH #1082](<https://github.com/themaydow/regatta/issues/1082>) · GH labels: autonomous

## Symptom

Today the github_issues adapter (`internal/orchestrator/adapter/githubissues/parse.go::parseIssueBody`) projects WorkItems from `## Acceptance criteria` bullets only. Issues that carry `## Reopen trigger`, `## Why deferred`, `## Dispatch sequence`, or `## Phase A/B/C` sections are ignored — the orchestrator treats them as ready… (truncated, use `get_issue` for full description)

---

## M10 — Backlog / unclassified

### MAY-72 — BUG: harness Agent tool default isolation=worktree for file-mutating subagents

- Priority: ·
- Status: Backlog
- Assignee: —
- Linear: https://linear.app/themaydow/issue/MAY-72/bug-harness-agent-tool-default-isolationworktree-for-file-mutating

> **Migrated from** [GH #1153](<https://github.com/themaydow/regatta/issues/1153>) · GH labels: autonomous

## Discovery

Session 2026-06-09: \~10 builder subagents fanned out concurrently against shared \`.claude/worktrees/operator-docker-soak\`. 4 died from \`git checkout\` HEAD-clobber collisions. Lost subagent token budget + 3+ rebase cycles.

Filed as #1123. Memory note \`feedback_subagent_shared_worktree_collision\` codified.

## Root caus… (truncated, use `get_issue` for full description)

---

