# Autonomous Session Trigger Prompt

Copy-paste this prompt to bootstrap a fully autonomous regatta dev session. Designed for max velocity: subagent-heavy, decision-deferred-to-review, no user round-trips.

---

## Prompt

```
Continue regatta development autonomously. Operate INDEFINITELY in auto mode — execute don't ask, ship don't explain, stop only when externally interrupted. **Self-host-first.** Drive toward regatta dispatching regatta-the-binary at regatta-the-repo unattended (per docs/engineer/briefs/2026-06-01-self-host-first.md): Phase S1 (dogfood-ready core) → S2 (trust-the-loop) → S3 (durability). External-buyer wedges (W7 htmx UI, W8 multi-tenant scoping, W10 Sigstore, W11 blackboard, W12 billing, P3.8 adapters, W9 Temporal-backed impl) are Phase X — deferred until external customer ask or 30-day self-host-green trigger fires. Never bottleneck on roadmap depth — pre-fetch next horizon per feedback_roadmap_pre_fetch when current wave drains. NEVER ask for clarification; decide via subagent + memory rules per feedback_decision_priority (UX > ease > performance > best-practices > speed > velocity). When blocked: file [followup] issue + add to watch-triggers list + pick next priority. Pause only for genuinely irreversible action (tag signing, secret rotation, branch-protection downgrade).

BOOT
1. cd /Users/treedesk/Desktop/Projects/regatta && git fetch && git pull --ff-only main
2. make check && bash scripts/cleanup-merged-branches.sh
3. git worktree list | awk '/agent-/ {print $1}' | xargs -I{} git worktree remove --force --force {} ; git worktree prune
4. gh pr list --state open  (note current state; in-flight PRs are normal)
5. Read MEMORY.md + AGENTS.md (auto-loaded). Specs in `docs/engineer/specs/` are canonical for execution.

PRIORITY (top-down, skip if blocked) — driven by docs/engineer/briefs/2026-06-01-self-host-first.md §3 + docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md

PHASE S — Self-host loop SHIPPED (do NOT redo). S1+S2+S3 closed in 2026-06-02 autonomous session (56-PR batch + strategic-design closeout). Verify via `git log --oneline origin/main -80`.

PHASE OBS — Observability Wave-A impl-ready. 8 items at `.regatta/items/obs-wave-a-*.md` per spec `docs/engineer/specs/2026-06-02-observability-roadmap.md` (#432). Dispatch next session. Wave-B/C/D items at `.regatta/items/obs-wave-{b,c,d}-*.md` (11 items) queued behind Wave-A.

PHASE MVR-1 — Next-horizon roadmap CONVERGED (#433 brief `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md`, 565 lines). 4 impl-ready items at `.regatta/items/mvr-1-*.md`. Phase X gate criteria specified: 30-day-green (≥10 PRs/day green-merge ≥30 days unattended) OR external-customer-ask. Wave-4 items at `.regatta/items/wave-4-*.md` queued.

PHASE X — Locked behind 30-day-green trigger OR external-customer ask. Specs ready: W7 htmx UI, W8 multi-tenant, W10 Sigstore, W11 blackboard, W12 billing, P3.8 adapters, W9 Temporal-backed impl. **DO NOT dispatch implementer agents on Phase X items until gate fires.** Reopen Phase X via single-line trigger event in MEMORY.md.

OPEN FOLLOWUPS (sweep when between phase items, ≤5 trivial PRs/session cap)
- RISK followups #423 #424 #426 #427 (filed 2026-06-02 strategic-design closeout)
- OBS followups: cost-per-agent integration `[OBS-followup]` #3; 2 inline FOLLOWUP markers in `docs/engineer/specs/2026-06-02-observability-roadmap.md`
- A+ rubric checkboxes from prior waves — fuzz, mutation testing, key-rotation drill
- Session-C followups #272 #273 #274 #275 #276 #277 (cost-gov) + #265 (approval)

Already shipped (do NOT redo) — confirm via `git log --oneline origin/main -40`:
- **MVP-3 W6 OTel backbone COMPLETE** (#159 umbrella) — T1 #172 (SDK setup) · T2 #169 (slog bridge) · T3 #209 (migration 0005 trace_id) · T4 #213 (GenAI semconv parser) · T5 #210 (Config.Tracer × 8 components) · T6+T7 #215 (Jaeger E2E + operator doc)
- **MVP-3 W7.0 listener prereq COMPLETE** — T1 #263 (HTTP listener + CallbackRoute impl) · T2 #255 (dbtest.QueryCounter) · T3 #253 (DecideTx lift)
- **MVP-3 substrate W1 COMPLETE** — T-S1 #224 (event log + 0005 migration + HMAC) · T-S2 #232 (CELDecider + gate_verdict validator) · T-S3 #233 (lint + property + adversarial) · #237 property-test Makefile
- **Cost-governor (P8) Wave 1+2 SHIPPED** — Wave 1: T1 #250 (gate + reader + scheduler hook) + T2 #246 (UpperBound estimator + pricing). Wave 2: T3 amendment #270 (onResult seam) + T3 primary #283 (token_spend writer + validator) + T4 #279 (reconciler + Cost/Usage API)
- **MVP-4 specs SHIPPED** — W8 OPA RBAC #266 · W10 Sigstore #284 · W11 blackboard #281 · W12 billing #280 · cost-gov Wave 3 plan #267
- **2026-06-01 design specs landed**: unified-substrate, W7 operator web UI, W9 Temporal-vs-bespoke red-team (option C hybrid picked), adapter contracts (5 swap-out), cost-governor P8 (#211)
- **2026-06-01 session C fixes**: #269 (decideTx pre-check race; closes #206) · #285 (lift t.Skip(#194))
- **2026-06-01 session C followups filed**: cost-gov #272 #273 #274 #275 #276 #277 #282 · approval #265
- **2026-06-01 boot-prompt extensions**: #208 (indefinite-mode framing + 3 new rules) · #256 (post-session-B refresh) · #262 (require A+ scorecard) · #271 (doc-check banned-phrase pre-push) · #264 (rubric sentinel clarification)
- **2026-06-02 self-host-first reorder SHIPPED**: #320 (brief + PRIORITY rewrite, S1→S2→S3 phasing, Phase X deferral list)
- **2026-06-02 Phase-S gate relaxation SHIPPED**: #322 (CI: drop windows matrix · property-test 200→50 · go-check -short · stale-todo 30d · prose-dup engineer-doc skip) · #323 (dispatch rules: reviewer auto-skip on docs/CI · scorecard scope · load-bearing slim) · #324 (followup: check-tdd downgrade considered+rejected, reopen-trigger documented). All toggles grep-able via `PHASE-S-RELAX` marker; restore at 30-day-green OR external-customer trigger per memory/feedback_gate_relaxation_phase_s
- **2026-06-02 Phase S1 SHIPPED** (self-host dogfood-ready core): T1 #334 regatta.yaml + .regatta/items · T2 #294 spawner-callback wiring · T3 #331 boot-prompt→items + #368 gh-followup→items · T5 #348 self-host smoke test · plus #327 Notifier contract · #332 reaper revocation · #335 scheduler perf · #336 doc-check fence-strip · #347 BASE_SHA drift · #371 reviewer-tag regex
- **2026-06-02 Phase S2 SHIPPED** (trust-the-loop): T1 #350 W9 DurableHistory substrate · T2 #351+#370+#373+#380+#381+#385+#387+#388 L4 gate (interface, scheduler-wire, Anthropic adapter, second-opinion, cache, auto-fix, prompt-SIGHUP, per-category) · T3 #368 followup-triage · T4 #372 mutation infra · #375 severity extract · #377 fold≡state property test
- **2026-06-02 Phase S3 SHIPPED** (durability): T1 #367 W8 OPA hot-reload · T2 #369+#378 substrate Phase B+C cutover (approvals) · T3 #379+#389+#393+#395 multi-key + rotate-CLI + brief re-sign + recovery · T4 #366+#382+#391+#394 crash-recovery WriteHook seam + property runner + nightly + cost/reaper extension
- **2026-06-02 Sweeps SHIPPED**: #80 audit · #83 composite index · #89 batched UPSERTs · #92 brief replay · #95 materializePending · #133 notify adapter · #182 approval E2E · #187 perf · #195 #198 contract · #238 #239 #240 cost-gov · #78 acceptance drift · #79 brief re-sign · #383 #384 crash-recovery extensions
- **2026-06-02 Self-improvement SHIPPED**: #326 dispatch templates · #337 boot-prompt rules · #349 templates patch · #396 reload-readiness gate · #397 operator getting-started doc. 56 PRs merged in ~6hr autonomous session. Phase X (W7/W8-multi-tenant/W10/W11/W12/Temporal-W9) deferred — locked behind 30-day-green OR external-customer trigger.
- **2026-06-02 Observability roadmap CONVERGED (#432)** — `docs/engineer/specs/2026-06-02-observability-roadmap.md` + 8 Wave-A items + 11 Wave-B/C/D items + 6 Grafana dashboard JSONs + `regatta status` TUI mockup §6.1.1 + cost-per-agent followup `[OBS-followup]` #3. Verdict ADOPT-WITH-NITS; Wave-C rollup-shape FOLLOWUP inline.
- **2026-06-02 Next-horizon roadmap CONVERGED (#433)** — `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` (565 lines) + 16 impl-ready items (`.regatta/items/mvr-1-*.md` + `.regatta/items/wave-4-*.md`). Verdict ADOPT after final review. License counter-discussion + SCM gate + 5 wave-4 nits closed; 2 FOLLOWUP inline (fleet-mgmt + plugin-API). 4 RISK followups filed: #423 #424 #426 #427.
- **2026-06-02 Strategic-design trail consolidation** — 29 prior strategic-design PRs CLOSED as superseded by #432/#433 per memory/feedback_design_iteration_local (local-iterate-then-converge supersedes serial PR trail). 8 memory rules added session-extension: `feedback_design_iteration_local` (LATEST).

WORKFLOW per item — use templates at `docs/engineer/dispatch-templates/`. Substitute variables; do NOT inline-repeat preamble.
1. Design subagent → spec — `designer.md` (rubric, OSS-first, self-host filter)
2. Adversarial reviewer on spec → fix findings — `reviewer.md`
3. Plan subagent → plan — `designer.md` (plans are spec-shaped)
4. Parallel implementer subagents on file-disjoint tasks — `implementer.md` (worktree + TDD + scorecard + release-notes + doc-check)
5. Adversarial reviewer per wave → fix → merge — `reviewer.md`
6. Land / defer / reject decisions on issues + stale PRs — `triage.md`

Templates encode load-bearing preamble: worktree-first, TDD failing-first, adversarial reviewer, A+ scorecard, doc-check banned phrases, release-notes fence, no-signatures, memory cites, PHASE-S-RELAX conditions. Cite memory rules in dispatch prompts via the templates' `<MEMORY-RULES>` variable.

RULES (memory-bound; do not re-derive)
- Subagents do everything: design, plan, impl, review, doc, PR-body drafting, issue filing, debugging. Main thread = dispatcher + integrator.
- Decisions: NEVER ask user. Spawn review subagent + decide based on memory/feedback_decision_priority (UX > ease > best-practices > speed > velocity).
- **W9 substrate-choice locked = option C hybrid, self-host scope = substrate-default impl ONLY** (memory/wedge_roadmap_assessment §"Substrate + W9 substrate-choice locked 2026-06-01" + self-host-first brief §3 S2-T1): ship W9 against `DurableHistory` Go interface, default impl on substrate `events`. Temporal-backed impl is Phase X — gated behind refined P2.5 trigger (sqlite contention >5% OR ≥30 concurrent OR replay-recovery >60s — any one, two consecutive 24h windows) AND external customer ask. W9 promoted ahead of W7/W8 for self-host loop closure. Never re-litigate during implementer dispatch.
- **Self-host-first filter** (per docs/engineer/briefs/2026-06-01-self-host-first.md §1): every wedge filtered by "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?". Keep → in scope. Defer → Phase X. Single-tenant, single-operator, single-repo, CLI-only, deterministic CI, human-merge via GH branch protection. No RBAC for tenancy. No billing. No htmx UI. No Sigstore. No blackboard. Reopen Phase X on external customer ask OR 30-day-green trigger.
- root-cause only, no workarounds (memory/feedback_root_cause)
- max parallel fan-out (memory/feedback_parallel_dispatch)
- Cap parallel implementer subagents at 3-4 per feedback_session_limit_dispatch; shared API quota dies at 5+. Heavy-context sessions reduce cap to 2-3.
- make pre-push-check before every push
- Pre-fetch next-horizon brief when current wave drains (≤2 unblocked items remaining) per feedback_roadmap_pre_fetch
- Audit dep-graph before parallel dispatch; sequence chained-output work; parallelize file-disjoint only per feedback_sequence_dependent_work
- Default to deletion over addition; every PR answers "what got smaller?"; adversarial reviewer enforces ≥1 deletion proposal per feedback_deletion_default
- PHASE-S-RELAX active on reviewer + scorecard + load-bearing gates — template files encode current conditions; restore at 30-day-green OR external-customer trigger per memory/feedback_gate_relaxation_phase_s.
- **Test/Fuzz/Benchmark godocs 1 line max** per `feedback_test_godoc_one_line`. `scripts/doc-check.sh` test-godoc gate rejects multi-line. Recurring agent-failure 2026-06-02.
- **`gh pr create` / `gh pr edit` MUST use `--body-file`** per `feedback_pr_body_file_only`. HEREDOC bodies escape backticks and silently break the release-notes fence. Recurring agent-failure 2026-06-02.
- **Comment-noise gate trip-traps** per #333 followup — reviewer-tag regex over-matches reviewer-Request / reviewer-JSON prose; banner-comment regex rejects `# --- Section ---`. Dodge: hyphenate or lowercase, replace banners with plain `# Section.`.

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

- New memory entry added → cite in RULES if load-bearing OR update template `<MEMORY-RULES>` defaults
- New gate added to `make` → reference if pre-push-relevant
- Priorities shift → reorder PRIORITY section
- Drop_ceremony adds/removes items → adjust RULES brevity
- Dispatch preamble drift detected → update `docs/engineer/dispatch-templates/*.md` instead of inlining rules back into boot prompt
