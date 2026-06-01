# Autonomous Session Trigger Prompt

Copy-paste this prompt to bootstrap a fully autonomous regatta dev session. Designed for max velocity: subagent-heavy, decision-deferred-to-review, no user round-trips.

---

## Prompt

```
Continue regatta development autonomously.

BOOT
1. cd /Users/treedesk/Desktop/Projects/regatta && git fetch && git pull --ff-only main
2. make check && make cleanup-branches
3. gh pr list --state open  (expect 0)
4. Read MEMORY.md + AGENTS.md (auto-loaded). Recover docs/superpowers/ per AGENTS.md recipe if needed.

PRIORITY (top-down, skip if blocked)
1. #114 approval-gates remaining waves — Wave 1 + 2 + much of 3 already shipped (see "Already shipped" below). Remaining: Wave 3 A4 (scheduler integration — extend tick w/ gate-pass per spec §3.1), Wave 4 (e2e + ops runbook), Wave 5 (operator doc + property tests). Spec at docs/superpowers/specs/2026-05-31-mvp-approval-gates.md (gitignored — recover via AGENTS.md recipe or rebuild from #114 body).
2. Open follow-up issues #115-#148 by load-bearing weight — many are A+ tier rubric checkboxes from earlier waves (mutation testing, fuzz, key-rotation drill, etc.). Triage via `gh issue list --state open` and pick highest UX impact per feedback_decision_priority.
3. MVP-3 next-level — design brief written at docs/superpowers/briefs/2026-05-31-mvp-3-next-level.md (gitignored). Top-3 wedges: W6 OTel + GenAI semconv observability backbone, W7 operator web UI, W8 OPA RBAC + multi-tenant. W6 unblocks W7+W9+W10+W12; spawn design subagent on W6 spec FIRST. Brief §8 carries a ready-to-paste design-subagent bootstrap prompt.
4. #99-#100 spawner determinism + reconciler (MVP-2 followups, load-bearing for approval-gates Wave 3 A4 scheduler integration)

Already shipped (do NOT redo):
- #98 scheduler default-fallback bug — merged PR #112 (commit 7dbcbab)
- #101 observability sweep — merged PR #113 (commit ab9f978)
- #114 approval-gates Wave 1 — merged PRs #123 (HMAC token), #126 (migration + state ops), #127 (notifier interface + stub)
- #114 approval-gates Wave 2 — merged PRs #143 (reaper + escalation), #144 (gate handler + fold + config)
- #114 approval-gates Wave 3 partial — A5 (CLI decide/list) + A7 (config loader + CUE bump) dispatched, may have merged; check `gh pr list --state merged --search "Wave 3"`

WORKFLOW per item
1. Spawn design subagent → spec (w/ grade rubric per feedback_grade_rubric)
2. Spawn adversarial reviewer on spec → fix findings
3. Spawn plan subagent → plan
4. Spawn parallel implementer subagents on file-disjoint tasks
5. Spawn adversarial reviewer per wave → fix → merge

RULES (memory-bound; do not re-derive)
- Subagents do everything: design, plan, impl, review, doc, PR-body drafting, issue filing, debugging. Main thread = dispatcher + integrator.
- Decisions: NEVER ask user. Spawn review subagent + decide based on memory/feedback_decision_priority (UX > ease > best-practices > speed > velocity).
- TDD strict (failing test FIRST, capture output)
- adversarial review on EVERY PR pre-automerge (memory/feedback_review_before_automerge) — automerge only AFTER reviewer clears blocking findings. Implementer subagents MUST NOT enable automerge themselves.
- Unaddressed load-bearing items in PR body → file tracking issues + cite numbers in PR before merge (memory/feedback_unaddressed_load_bearing)
- Research + design: adopt proven OSS over build-from-scratch; UX > best-in-class > best-practices > long-term benefit (memory/feedback_research_design_principles). Every design-subagent prompt must cite this rule.
- Spec deviations require design-subagent re-spawn (memory/feedback_spec_pattern_authority); never let implementer pick alternative
- root-cause only, no workarounds
- max parallel fan-out (memory/feedback_parallel_dispatch)
- make pre-push-check before every push
- no AI signatures in commits/PRs

WHEN BLOCKED
- File [followup] issue + pick next priority. Never pause for user input.

STOP CRITERIA (any one)
- Approval-gates Wave 5 merged (full HITL flow shippable) OR MVP-3 W6 (OTel backbone) Wave 1 merged + Wave 2 dispatched
- OR 3 critical PRs shipped this session
- OR genuinely irreversible step required (tag signing, secret rotation, branch-protection downgrade)
- OR context budget tight + Wave-mid (don't leave half-applied state)

Begin BOOT. After boot, pick highest priority + dispatch design subagent.
```

---

## Why this shape

- **No "should I" — only "spawn subagent who decides"**. Main thread is router, not approver.
- **Memory-bound rules**: don't re-explain; cite the file. Agent reads memory on boot.
- **Stop criteria are concrete**: agent knows when to land vs continue.
- **Escape valve named**: blocked → file issue → pick next. No deadlock on one item.
- **Genuine irreversibility named explicitly**: tag signing, secrets, protection downgrade. Everything else proceeds.

## When to update this prompt

- New memory entry added → cite in RULES if load-bearing
- New gate added to `make` → reference if pre-push-relevant
- Priorities shift → reorder PRIORITY section
- Drop_ceremony adds/removes items → adjust RULES brevity
