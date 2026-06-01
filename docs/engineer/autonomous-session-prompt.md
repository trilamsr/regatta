# Autonomous Session Trigger Prompt

Copy-paste this prompt to bootstrap a fully autonomous regatta dev session. Designed for max velocity: subagent-heavy, decision-deferred-to-review, no user round-trips.

---

## Prompt

```
Continue regatta development autonomously. Operate in auto mode — NEVER ask for clarification, decide via subagent + memory rules per feedback_decision_priority (UX > ease > best-practices > speed > velocity). When blocked: file [followup] issue + pick next priority. Never wait, never pause for user input unless a stop-criteria item or genuinely irreversible action (tag signing, secret rotation, branch-protection downgrade) is reached.

BOOT
1. cd /Users/treedesk/Desktop/Projects/regatta && git fetch && git pull --ff-only main
2. make check && bash scripts/cleanup-merged-branches.sh
3. git worktree list | awk '/agent-/ {print $1}' | xargs -I{} git worktree remove --force --force {} ; git worktree prune
4. gh pr list --state open  (note current state; in-flight PRs are normal)
5. Read MEMORY.md + AGENTS.md (auto-loaded). Recover docs/superpowers/ per AGENTS.md recipe if needed.

PRIORITY (top-down, skip if blocked)
1. **NEXT WEDGE — pick from memory/wedge_roadmap_assessment.md**. MVP-2 W1 (approval-gates) just shipped; the control-plane-for-AI-labor lineup ranks the next bets. Top candidates:
   - **cost-governor (P8)** — per-DAG/operator USD+token caps; Helicone/Portkey/Anthropic Usage API patterns. See `memory/wedge_cost_governor.md`. Pairs naturally with MVP-3 W6 OTel backbone for spend instrumentation.
   - **conditional-DAG (P1+P6)** — CEL-predicated edges + journal snapshot; Step Functions/BPMN deadlock-prevention. See `memory/wedge_conditional_dag.md` + `memory/wedge_blackboard.md`.
   - **plan-as-code (P3+P4)** — .regatta/plans/*.yaml; CircleCI compile-time + Tekton result-refs; CUE-validated. See `memory/wedge_plan_as_code.md`.
   Spawn design subagent on the chosen wedge spec FIRST; spec lands in `docs/superpowers/specs/`.
2. MVP-3 W6 OTel observability backbone — umbrella #159. Wave 1 partial shipped (T1 #172 + T2 #169). Remaining: T5 Config.Tracer injection across 8 components. Wave 2 = T3 migration 0005 trace_id columns + T4 spawner stream-json GenAI semconv parser. Wave 3 = T6 docker-compose Jaeger E2E (scaffold pre-positioned at examples/observability/docker-compose.yml via #184) + T7 operator observability doc. Spec at `docs/superpowers/specs/2026-05-31-mvp-3-w6-otel-backbone.md` (gitignored — recover via AGENTS.md recipe or rebuild from #159 body). **Pre-written dispatch prompts for T3+T4+T5 at `docs/superpowers/dispatch/2026-05-31-mvp-3-w6-wave1-finish.md`** (gitignored; recover by rebuild from spec §5 task table). Paste straight into 3 parallel `Agent(isolation: worktree)` calls; cap at 3-4 parallel per `feedback_session_limit_dispatch`.
3. **MVP-2 approval-gates followup cleanup** — load-bearing followups that ship after the wedge but before declaring it stable. Current open `mvp-2` / `approval-gates` followups (NOT blockers; ship after MVP-2 closeout):
   - **#193** reaper auto_approve event-order vs fold-of-events disagreement (W5 finding — auto_approve resolves as rejected via gate path)
   - **#194** reaper escalate path does not mint/notify tier-N+1 reviewers (W5 finding — production seam missing)
   - **#195** minted token JTIs never land in approval_events; reaper revocation is dead code (W5 finding)
   - **#133** wire state.Approval → notify.Request adapter (Wave 2)
   - **#140** add CHECK on approval_events.kind once enum locked (Wave 5)
   - **#145** consolidate reaper fold helpers with fold.go (post A2+A2b merge)
   - **#146** route reaper events through audit.recordEvent for slog/row byte-equality
   - **#147** mutation-survival test on tier-comparison logic (A+ rubric)
   - **#153** JSON contract for 'approval list --format=json' (Wave 3 A5 follow-up)
   - **#156** consolidate ApprovalGateConfig + sentinels with approval.Config
   - **#157** consolidate CLI foldResult into internal/gates/approval/fold.go
   - **#165** typed state.TransitionWorkItem (collapse raw-SQL CAS in scheduler.go)
   - **#167** pending-agent-for-gated-wi consolidation
   - **#132** FuzzToken_Verify parser fuzz target (A+ rubric)
   - **#131** reuse contracts/schemas:macSum (kill duplicate tokenMAC)
   - **#134** TestNotifier_InterfaceContract table-driven contract test (A+ rubric)
   - **#139** state pkg coverage ≥90% OR re-word rubric to per-method coverage
4. Open follow-up issues (~50 total) by load-bearing weight. Spawn a triage subagent (≤5 trivial PRs/session cap) to sweep. Per feedback_session_2026_05_31_lessons.
5. MVP-3 next-level — brief at docs/superpowers/briefs/2026-05-31-mvp-3-next-level.md. After W6 lands: W7 operator web UI (rank #2; Temporal-UI pattern via Go embed.FS + htmx; scope-disciplined to approval flow + read-only DAG + cost panel). W8 OPA RBAC + multi-tenant (rank #3).

Already shipped (do NOT redo) — confirm via `git log --oneline origin/main -40`:
- #98 #101 #115 #116 (Wave 0 prereqs)
- **#114 MVP-2 approval-gates wedge SHIPPED** (umbrella closed via Wave 5 E2E PR):
  - Wave 1 — #123 (HMAC token) #126 (state migration + ops) #127 (notifier interface + stub)
  - Wave 2 — #143 (reaper sweep + escalation) #144 (gate handler + fold + config)
  - Wave 3 — #154 (config loader + CUE schema) #155 (CLI decide+list) #168 (A4 scheduler tick gate-pass)
  - Wave 5 — #161 (operator runbook) #191 (E2E lifecycle: happy + reject + timeout-fail)
  - Wave 5 closeout — this PR (E2E auto_approve + escalate + concurrent race + fold-vs-state-machine property test; closes #182 + #114)
- #159 MVP-3 W6 T1 — #172 (OTel SDK setup)
- #159 MVP-3 W6 T2 — #169 (slog→OTel logs bridge)
- Cleanup + sweep: #160 (cel-go) #163 (single-NewReaper) #164 (kid sentinels) #166 (state dedupe) #170 (program-plan test) #177 (Kahn cycle-check)

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
- adversarial review on EVERY PR before automerge fires; main session OR implementer subagent may enable automerge once reviewer-cleared AND all Risk-tier+ findings addressed (inline-fixed or filed as cited followup issues) per memory/feedback_review_before_automerge
- Unaddressed load-bearing items in PR body → file tracking issues + cite numbers in PR before merge (memory/feedback_unaddressed_load_bearing)
- Research + design: prefer adopting proven OSS over reimplementation. Priority order: user experience first, then quality bar matching reference systems, then ecosystem conventions, then long-term repo + user benefit (memory/feedback_research_design_principles). Every design-subagent prompt must cite this rule.
- Spec deviations require design-subagent re-spawn (memory/feedback_spec_pattern_authority); never let implementer pick alternative
- root-cause only, no workarounds
- max parallel fan-out (memory/feedback_parallel_dispatch)
- make pre-push-check before every push
- no AI signatures in commits/PRs

WHEN BLOCKED
- File [followup] issue + pick next priority. Never pause for user input.

STOP CRITERIA (any one)
- Next-wedge spec landed (cost-governor / conditional-DAG / plan-as-code) + Wave 1 dispatched
- OR MVP-3 W6 Wave 1 finished (T1+T2+T5) + Wave 2 dispatched
- OR MVP-3 W7 (operator web UI) design spec drafted + reviewed
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
