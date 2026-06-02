---
id: PHASE-AUTONOMY-W5
title: cost-cap autonomic enforcement — daily cap exceeded → regatta pause-all
lane: self-host
kind: feature
status: planned
gate: phase-autonomy-landing-3 (W1+W2+W3 merged)
source_ref: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md §11 W5
dependencies: PHASE-AUTONOMY-W2
linked_artifact: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md
---

Source brief: PHASE AUTONOMY amendment §11 W5 (Landing 3). W2 dependency: auto-merge must observe the pause flag.

## Scope

Extend `internal/cost` so that when the daily cap is exceeded, the substrate flips a `regatta_pause_all` flag and the scheduler tick halts dispatch within one loop. Auto-unpause at period boundary (UTC midnight by default; configurable) OR via operator `regatta resume-all`. `regatta status` shows pause state in plain text.

## Approach

- Adopt Vault's sealed/unsealed binary-flag UX shape (state + observable status).
- Adopt argo-workflows' `suspend`/`resume` command shape for the CLI.
- Build (~50 LoC): wire the existing `internal/cost/gate` cap-fired event to flip the substrate flag + add a guard at scheduler-tick that reads the flag before dispatching.
- Period-boundary clear is a cron-like check inside the scheduler tick — no new daemon.

## Acceptance criteria

- [planned] c1: Daily cap exceeded → substrate `regatta_pause_all=true` event emitted.
- [planned] c2: Scheduler tick reads the flag at the top of every loop; dispatch halts within one tick.
- [planned] c3: Period boundary (UTC midnight by default; configurable via `cost.period_boundary_tz`) clears the flag automatically.
- [planned] c4: `regatta resume-all` clears it on operator command; emits substrate event with `actor=<operator-gh-handle>`.
- [planned] c5: `regatta status` shows pause state in plain text.
- [planned] c6: Adversarial reviewer subagent posts.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c2 ship. (b) Default-on once `cost.daily_cap_usd` set. (c) Release-notes fence. |
| A (target) | B + (d) c3+c4+c5+c6. (e) Substrate event schema for `regatta_pause_all` documented in `docs/engineer/specs/2026-06-01-unified-substrate-design.md`. |
| A+ (stretch) | A + (f) Per-DAG pause (some DAGs halt while others continue) via label-set. (g) Property test: 100 random scheduler ticks under random pause/resume sequences; assert no dispatch fires when paused. (h) W4 self-improvement detector wired to detect pause-cycling (≥3 daily-cap hits in 7 days = self-improvement issue suggesting cap raise). |

## Cites

- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W5
- HashiCorp Vault sealed/unsealed semantics — adopted UX shape
- argo-workflows `suspend`/`resume` — adopted CLI shape
- k8s/kueue (Apache 2) — queue-pause reference
- `feedback_decision_priority` — operator UX: pause-before-burn is the load-bearing cost-control UX
- `feedback_research_design_principles` — adopt-first; pause semantics adopted, wiring built
