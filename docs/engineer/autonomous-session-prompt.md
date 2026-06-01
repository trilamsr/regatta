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
1. #114 approval-gates Wave 1 — umbrella issue body has locked design decisions (HMAC token format, atomic decide tx, escalation semantics, fold ordering, etc.). Spec exists locally at docs/superpowers/specs/2026-05-31-mvp-approval-gates.md but is gitignored — recover via AGENTS.md recipe OR rebuild from #114 body. Wave 1 = migration 0004 (approvals + approval_events) + state ops + HMAC token issue/verify (reuses contracts/schemas/sign.go:macSum) + notifier adapter stub interface. File-disjoint, 4 parallel implementers.
2. #115 normalize logger-injection pattern to Config.Logger (#101 cleanup; mechanical ~15 call sites)
3. #117 --log-format=text|json flag (A+ tier for #101; small, single-file)
4. #99-#100 spawner determinism + reconciler (MVP-2 followups, load-bearing for approval-gates Wave 2)
5. Other open follow-ups (#82-#118) by load-bearing weight

Already shipped (do NOT redo):
- #98 scheduler default-fallback bug — merged PR #112 (commit 7dbcbab)
- #101 observability sweep — merged PR #113 (commit ab9f978)

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
- Approval-gates Wave 1+2 merged + Wave 3 dispatched
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
