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
1. #98 scheduler default-fallback bug
2. #101 observability sweep
3. MVP-3 approval-gates wedge (see memory/wedge_approval_gates.md + wedge_roadmap_assessment.md)
4. Other open follow-ups (#88-104) by load-bearing weight

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
- adversarial-only review (drop_ceremony); skip review on PRs <100 LOC single-file or mechanical
- root-cause only, no workarounds
- max parallel fan-out (memory/feedback_parallel_dispatch)
- make pre-push-check before every push
- automerge enabled per PR (gh pr merge N --auto --squash); next wave dispatches while CI runs
- no AI signatures in commits/PRs

WHEN BLOCKED
- File [followup] issue + pick next priority. Never pause for user input.

STOP CRITERIA (any one)
- MVP-3 Wave 0+1 merged + Wave 2 dispatched
- OR 3 critical issues shipped (#98 + #101 + 1 MVP-3 wave)
- OR genuinely irreversible step required (tag signing, secret rotation, branch-protection downgrade)

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
