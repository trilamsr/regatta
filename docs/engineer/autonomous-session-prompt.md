# Autonomous Session Trigger Prompt

Copy-paste this prompt to bootstrap a fully autonomous regatta dev session. Designed for max velocity: subagent-heavy, decision-deferred-to-review, no user round-trips.

---

## Prompt

```
Continue regatta development autonomously. Operate INDEFINITELY in auto mode — execute don't ask, ship don't explain, stop only when externally interrupted. Drive toward MVP-3 wave completion → MVP-4 wedges (W10 Sigstore / W11 blackboard / W12 billing per docs/engineer/briefs/2026-05-31-mvp-3-next-level.md) → Phase 2 P2.x adopt-when-needed triggers → Phase 3 P3.x conditional adoption. Never bottleneck on roadmap depth — pre-fetch next horizon per feedback_roadmap_pre_fetch when current wave drains. NEVER ask for clarification; decide via subagent + memory rules per feedback_decision_priority (UX > ease > best-practices > speed > velocity). When blocked: file [followup] issue + add to watch-triggers list + pick next priority. Pause only for genuinely irreversible action (tag signing, secret rotation, branch-protection downgrade).

BOOT
1. cd /Users/treedesk/Desktop/Projects/regatta && git fetch && git pull --ff-only main
2. make check && bash scripts/cleanup-merged-branches.sh
3. git worktree list | awk '/agent-/ {print $1}' | xargs -I{} git worktree remove --force --force {} ; git worktree prune
4. gh pr list --state open  (note current state; in-flight PRs are normal)
5. Read MEMORY.md + AGENTS.md (auto-loaded). Round-2 specs are canonical (v2 supersedes v1; review files in docs/superpowers/ describe v1 issues now fixed and are not load-bearing for execution).

PRIORITY (top-down, skip if blocked)
1. **MVP-3 W7.0 listener prereq finish** — T2 (`internal/orchestrator/state/dbtest/query_counter`) + T3 (lift `decideTx` → `internal/gates/approval/decide.DecideTx`) dispatched 2026-06-01 (background agents in `.claude/worktrees/agent-aa696079445f3df46` + `agent-a285cfa1f06e1de89`); confirm their PRs landed. Then dispatch T1 (`cmd/regatta/serve.go` HTTP listener + `CallbackRoute()` impl) — solo dispatch since T1 imports T3's `approval.DecideTx`. Plan: `docs/engineer/plans/2026-06-01-w7-prereq-listener.md` (PR #236 merged).
2. **MVP-3 W7 Wave 1 (T4-T7)** — once W7.0 lands, dispatch T4 (`internal/web/` scaffold + CSP + healthz) + T5 (vendored Tailwind + htmx + `build-tailwind`) + T7 (`Principal` + cookie-bound HMAC + CSRF + Origin) in parallel; T6 (`/approve/*` handlers + templates + 8 KiB diff cap) dispatches LAST (depends on T4+T7 merge). Reference plan at `docs/engineer/plans/2026-06-01-w7-operator-ui-w0-w1-tasks.md` (1757 LoC — written but un-committed; dedupe vs PR #236 plan before dispatching).
3. **Cost-governor (P8) Wave 2** — T3 (spawner post-stream `token_spend` emission) + T4 (reconciler + Anthropic Cost+Usage API) per cost-governor plan §4 handoff. Wave 1 SHIPPED (T1 #250 + T2 #246). Wave 2 ungated post-#250 merge.
4. Open follow-up issues (~50+) by load-bearing weight — A+ tier rubric checkboxes from earlier waves (mutation testing, fuzz, key-rotation drill, etc.). Plus #194 (reaper-escalate gate.Evaluate mint+notify gap) + 8 substrate followups #216-#223. Spawn a triage subagent (≤5 trivial PRs/session cap) to sweep. Per feedback_session_2026_05_31_lessons.
5. **MVP-3 W8 OPA RBAC + multi-tenant** — plugs into W7's `Authorizer` interface seam (designed pre-W8 in W7 spec §3.6.4 — one-file impl swap, no re-architecture). Substrate `policies` primitive ships in W8 (deferred from substrate W1 per spec §13). Dispatch AFTER W7 land.
6. **MVP-3 W9 replay+diff harness** — ships AFTER W6/W7/W8 land; substrate-default `DurableHistory` impl, Temporal-backed impl gated behind refined P2.5 trigger — spec `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md` picks option C (hybrid).
7. **MVP-4 wedges + P3.8 swap-out adapters** — W10 Sigstore / W11 blackboard (substrate blobs primitive) / W12 billing per `docs/engineer/briefs/2026-05-31-mvp-3-next-level.md`. P3.8 adapters spec at `docs/engineer/specs/2026-06-01-adapter-contracts-design.md`; trigger = first customer ask for hosted backend.

Already shipped (do NOT redo) — confirm via `git log --oneline origin/main -40`:
- #98 #101 #115 #116 (Wave 0 prereqs)
- **MVP-2 W1-W5 approval-gates SHIPPED** (#114 umbrella) — #123 #126 #127 (W1) · #143 #144 (W2) · #154 #155 #168 (W3) · #170 (W4 program-plan) · #191 (W5 E2E lifecycle) · #197 (W5 log-format) · #203 (W5 reaper auto_approve fix)
- **MVP-3 W6 OTel backbone COMPLETE** (#159 umbrella) — T1 #172 (SDK setup) · T2 #169 (slog bridge) · T3 #209 (migration 0005 trace_id) · T4 #213 (GenAI semconv parser) · T5 #210 (Config.Tracer × 8 components) · T6+T7 #215 (Jaeger E2E + operator doc)
- Cleanup + sweep: #160 (cel-go) #163 (single-NewReaper) #164 (kid sentinels) #166 (state dedupe) #177 (Kahn cycle-check)
- #99 spawner orphan-journal reconcile (#186) · #88 scheduler single-tx reservation (#171) · #180 atomicWriteBrief 8 MiB cap · #184 Jaeger compose scaffold (W6 T6 pre-position)
- **2026-06-01 design specs landed**: unified-substrate, W7 operator web UI, W9 Temporal-vs-bespoke red-team (option C hybrid picked), adapter contracts (5 swap-out), cost-governor P8 (#211)
- **2026-06-01 plans landed**: substrate W1 implementer task breakdown (#212)
- **2026-06-01 MVP-3 substrate W1 T-S1 SHIPPED**: #224 (event log primitive + 0006 migration + HMAC sign + Kahn cycle-check); followups F1-F4, F6-F8, F10 filed as #216-#223
- **2026-06-01 cost-governor followups filed**: #225-#228 (R-A3 admin-key vault, soft-cap opt-in, W6-T4 tx-export, sampler-customization E2E)
- **2026-06-01 boot-prompt extension**: #208 — indefinite-mode framing + 3 new feedback rules in MEMORY.md
- **2026-06-01 session B SHIPPED**: substrate W1 finish (#232 T-S2 + #233 T-S3 + #237 property-test Makefile) · cost-governor Wave 1 (#246 T2 + #250 T1) · W7.0 listener plan #236 · W6 spec semconv bump #230 (closes #178)
- **2026-06-01 session B followups filed**: cost-gov #238 #239 #240 #242 #243 #247 #248 #249 #251 · substrate #234 — dup pairs #241 #244 #245 #252 closed
- **2026-06-01 session B lessons**: [[feedback_agent_tree_spillage]] (stash before reset when primary tree dirty from agents) · [[feedback_parallel_dup_followups]] (pre-file shared followups main-thread) · [[feedback_plan_subagent_dup_files]] (specify exact output path in plan dispatch)

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
- Implementer subagent dispatch prompts MUST require an explicit "A+ Rubric Scorecard" section in the PR body — each B/A/A+ criterion from the spec marked PASS/FAIL/N-A with one-line evidence + claimed tier. Reviewer subagent independently re-scores. Automerge precondition: scorecard posted (feedback_a_plus_scorecard_required + feedback_agent_pr_review).
- Unaddressed load-bearing items in PR body → file tracking issues + cite numbers in PR before merge (memory/feedback_unaddressed_load_bearing)
- Research + design: prefer adopting proven OSS over reimplementation. Priority order: user experience first, then quality bar matching reference systems, then ecosystem conventions, then long-term repo + user benefit (memory/feedback_research_design_principles). Every design-subagent prompt must cite this rule.
- Spec deviations require design-subagent re-spawn (memory/feedback_spec_pattern_authority); never let implementer pick alternative
- **W9 substrate-choice locked = option C hybrid** (memory/wedge_roadmap_assessment §"Substrate + W9 substrate-choice locked 2026-06-01"): ship W9 against `DurableHistory` Go interface, default impl on substrate `events`, Temporal-backed impl gated behind refined P2.5 trigger (sqlite contention >5% OR ≥30 concurrent OR replay-recovery >60s — any one, two consecutive 24h windows). W9 ships AFTER W6/W7/W8 land; substrate ships BEFORE W7. Never re-litigate during implementer dispatch.
- root-cause only, no workarounds
- max parallel fan-out (memory/feedback_parallel_dispatch)
- Cap parallel implementer subagents at 3-4 per feedback_session_limit_dispatch; shared API quota dies at 5+. Heavy-context sessions reduce cap to 2-3.
- make pre-push-check before every push
- Pre-fetch next-horizon brief when current wave drains (≤2 unblocked items remaining) per feedback_roadmap_pre_fetch
- Audit dep-graph before parallel dispatch; sequence chained-output work; parallelize file-disjoint only per feedback_sequence_dependent_work
- Default to deletion over addition; every PR answers "what got smaller?"; adversarial reviewer enforces ≥1 deletion proposal per feedback_deletion_default
- no AI signatures in commits/PRs
- Pre-push grep banned phrases per `feedback_doc_check_banned_phrases` — 11-token list lives in `scripts/doc-check.sh`. Every spec/plan/PR-body subagent dispatch MUST cite this rule. Reword hits to falsifiable claims (version pin, benchmark, named reference).

WHEN BLOCKED
- File [followup] issue + pick next priority. Never pause for user input.

STOP CRITERIA — indefinite mode
- Continue until externally interrupted (user signal) OR genuinely irreversible action required (tag signing, secret rotation, branch-protection downgrade, force-push to main)
- Per-session soft-stop on context-budget pressure: if approaching context limit mid-wave, finish the current implementer-subagent batch + checkpoint progress in MEMORY.md/observation_record_event, then end-of-turn cleanly (no half-applied state)
- Wave-finish checkpoints are NOT stop signals — immediately pre-fetch next horizon (per feedback_roadmap_pre_fetch) and dispatch next wave's design subagent
- Watch-triggers list: blocked items file as [followup] GH issues with trigger conditions (e.g. "unblock when X merges") in PR body; loop back when trigger fires; never deadlock waiting

Begin BOOT. After boot, pick highest priority + dispatch design subagent.
```

---

## Why this shape

- **No "should I" — only "spawn subagent who decides"**. Main thread is router, not approver.
- **Memory-bound rules**: don't re-explain; cite the file. Agent reads memory on boot.
- **Stop criteria are concrete**: agent knows when to land vs continue.
- **Escape valve named**: blocked → file issue → pick next. No deadlock on one item.
- **Genuine irreversibility named explicitly**: tag signing, secrets, protection downgrade. Everything else proceeds.
- **Indefinite by design**: STOP CRITERIA bounds the per-session soft-stop only; the prompt never says "we're done" because the roadmap is infinite. Pre-fetch keeps queue full.
- **Latitude is bounded by quality gates, not by stop signals**: adversarial review + B/A/A+ rubric + deletion-default enforce quality regardless of how indefinite the session runs.

## When to update this prompt

- New memory entry added → cite in RULES if load-bearing
- New gate added to `make` → reference if pre-push-relevant
- Priorities shift → reorder PRIORITY section
- Drop_ceremony adds/removes items → adjust RULES brevity
