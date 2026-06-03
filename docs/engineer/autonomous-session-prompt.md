# Autonomous Session Trigger Prompt

Copy-paste this prompt to bootstrap a fully autonomous regatta dev session. Designed for max velocity: subagent-heavy, decision-deferred-to-review, no user round-trips.

---

## Prompt

```
Continue regatta development autonomously. Operate INDEFINITELY in auto mode — execute don't ask, ship don't explain, stop only when externally interrupted. **Self-host-first; Phase S1+S2+S3 SHIPPED 2026-06-02 (96+ PRs merged in single autonomous session).** Next horizon: Phase OBS (observability wave-A impl from #432 converged spec) + Phase MVR-1 (next-horizon customer wedges from #433 converged brief) — both gated behind the 30-day-green-trigger OR external-customer-ask criteria per docs/engineer/briefs/2026-06-01-self-host-first.md. External-buyer wedges (W7 htmx UI, W8 multi-tenant scoping, W10 Sigstore, W11 blackboard, W12 billing, P3.8 adapters, W9 Temporal-backed impl) stay Phase X until trigger fires. Never bottleneck on roadmap depth — pre-fetch next horizon per feedback_roadmap_pre_fetch when current wave drains. NEVER ask for clarification; decide via subagent + memory rules per feedback_decision_priority (UX > ease > performance > best-practices > speed > velocity). When blocked: file [followup] issue + add to watch-triggers list + pick next priority. Pause only for genuinely irreversible action (tag signing, secret rotation, branch-protection downgrade).

BOOT
1. cd /Users/treedesk/Desktop/Projects/regatta && git fetch && git pull --ff-only main
2. make check && bash scripts/cleanup-merged-branches.sh
3. git worktree list | awk '/agent-/ {print $1}' | xargs -I{} git worktree remove --force --force {} ; git worktree prune
4. gh pr list --state open  (note current state; in-flight PRs are normal)
5. Read MEMORY.md + AGENTS.md (auto-loaded). Specs in `docs/engineer/specs/` are canonical for execution.

PRIORITY (top-down, skip if blocked) — driven by docs/engineer/briefs/2026-06-01-self-host-first.md §3 +docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md (#433) + docs/engineer/specs/2026-06-02-observability-roadmap.md (#432)

**STATUS 2026-06-02 PM: Phase S1+S2+S3 SHIPPED.** All 13 numbered entries below are CLOSED — see "Already shipped" block. Listed here only because `cmd/boot-prompt-to-items` consumes the numbered shape; treat as historical reference, NOT a fresh dispatch queue. Next horizons (Phase OBS / Phase MVR-1) live in narrative form below, gated behind merge of #432/#433 + 30-day-green trigger respectively.

PHASE S1 — dogfood-ready core (acceptance: regatta dispatches itself on a real [autonomous]-labeled issue → opens PR → green gates → operator merges) — ALL ITEMS SHIPPED
1. **S1-T2 — close #282 spawner-callback wiring** — SHIPPED #294. Wired `spend.SpawnerCallback` into `cmd/regatta/serve.go::buildSpawner`.
2. **S1-T4 — Cost-governor Wave 3 dispatch** — SHIPPED. T5+T6+T7 trio + reconcile boot validator (#440 #441 #451 #452 #461) per plan #267.
3. **S1-T1 — regatta.yaml for THIS repo** — SHIPPED #334. Markdown adapter + `.regatta/items/` scaffold; default markdown per brief §8.
4. **S1-T3 — boot-prompt → work_item brief converter** — SHIPPED #331 (boot-prompt→items) + #368 (gh-followup→items).
5. **S1-T5 — self-host smoke test** — SHIPPED #348. End-to-end fixture: regatta picks one `[followup]` issue → PR → green gates → operator merges.

PHASE S2 — trust-the-loop (acceptance: leave `regatta serve` running overnight; adversarial-reviewer catches bad PRs, cost caps stop runaway spend, replay-diff debugs flaky decisions) — ALL ITEMS SHIPPED
6. **S2-T1 — W9 replay+diff harness, substrate-default `DurableHistory` impl ONLY** — SHIPPED #350. Per spec `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md` option C, substrate path only.
7. **S2-T2 — adversarial reviewer as first-class L4 gate** — SHIPPED #351+#370+#373+#380+#381+#385+#387+#388. Anthropic adapter + second-opinion + cache + auto-fix + prompt-SIGHUP + per-category.
8. **S2-T3 — followup-issue auto-triage** — SHIPPED #368. `[followup]`-tagged GH issues self-file as `.regatta/items/`.
9. **S2-T4 — mutation testing on cost-governor + scheduler** — SHIPPED #372 (infra) + #454 (reaper tier-comparison helpers).

PHASE S3 — durability (acceptance: survives crashes, key rotations, schema migrations without operator hand-holding) — ALL ITEMS SHIPPED
10. **S3-T1 — W8 T-remaining slim** — SHIPPED #367 (OPA hot-reload) + #448 (wire OPAAuthorizer into serve). Multi-tenant `tenant_id` propagation deferred to Phase X.
11. **S3-T2 — substrate Phase B+C cutover** — SHIPPED #369+#378 (approvals only) + #442 (reconcile.Run wired into serve).
12. **S3-T3 — key-rotation drill + recovery doc** — SHIPPED #379+#389+#393+#395 (multi-key + rotate-CLI + brief re-sign + recovery).
13. **S3-T4 — crash-recovery property test** — SHIPPED #366+#382+#391+#394 (WriteHook seam + property runner + nightly + cost/reaper extension).

PHASE OBS — Observability wave-A. Spec at `docs/engineer/specs/2026-06-02-observability-roadmap.md` (#432 MERGED 2026-06-02 20:48 UTC). First impl PRs already landed: #436 (EventCostReconcileFailing OTel ERROR severity), #438 (OTEL_TRACES_SAMPLER env honor), #442 (reconcile.Run wired into serve), #448 (OPAAuthorizer wired). Remaining wave-A items dispatch next session — pull from `.regatta/items/obs-wave-a-*.md` once #433 merges (those items currently live in PR #433's diff). Wave-B/C/D queued behind wave-A.

PHASE MVR-1 — Next-horizon customer wedges. Unified brief at `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` (#433 MERGED — gate now stands on 30-day-self-host-green OR named persona-A inbound only). 4 impl-ready items: MVR-1-T1 (W7 wave-1 htmx UI), MVR-1-T2 (regatta init bundle), MVR-1-T5 (P3.8 SCM adapter — Gitea/GitLab), MVR-1-T6 (priced-support contract template). All gated `mvr-1-entry`: 30-day-self-host-green OR named persona-A inbound. **DO NOT dispatch MVR-1 implementers until gate fires.** Triage 4 RISK followups #423 #424 #426 #427 in parallel (persona qualify · sales-channel · precedent counter-evidence · pricing-unit pin).

PHASE X — Locked behind 30-day-green trigger OR external-customer ask. Specs ready: W7 htmx UI · W8 multi-tenant scoping · W10 Sigstore #284 · W11 blackboard #281 · W12 billing #280 · P3.8 adapters · W9 Temporal-backed impl. **DO NOT dispatch implementer agents on Phase X items until gate fires.** Reopen via single-line trigger event in MEMORY.md.

OPEN FOLLOWUPS (sweep when between phase items, ≤5 trivial PRs/session cap)
- RISK followups #423 #424 #426 #427 (filed 2026-06-02 strategic-design closeout)
- OBS followups inline in `docs/engineer/specs/2026-06-02-observability-roadmap.md` (cost-per-agent integration + Wave-C rollup-shape)
- Open issue #15 superseded by #463 PR-watcher locked design (close on merge)
- Late-arriving open PRs to triage: #435 (FuzzToken_Verify) · #449 (cost-gov runbook) · #458 (PHASE AUTONOMY spec) · #463 (PR-watcher locked design) · #464 #465 (otel/bridge property test + bench)
- Recurring A+ rubric checkboxes — fuzz, mutation testing extensions, key-rotation drill extensions

Already shipped (do NOT redo) — confirm via `git log --oneline origin/main -120`. Per feedback_boot_prompt_per_wave_refresh, entries >2 waves old are pruned; older shipped wedges live in git history only.

- **2026-06-02 Phase S1 SHIPPED** (self-host dogfood-ready core): T1 #334 regatta.yaml + .regatta/items · T2 #294 spawner-callback wiring · T3 #331 boot-prompt→items + #368 gh-followup→items · T5 #348 self-host smoke test · plus #327 Notifier contract · #332 reaper revocation · #335 scheduler perf · #336 doc-check fence-strip · #347 BASE_SHA drift · #371 reviewer-tag regex
- **2026-06-02 Phase S2 SHIPPED** (trust-the-loop): T1 #350 W9 DurableHistory substrate · T2 #351+#370+#373+#380+#381+#385+#387+#388 L4 gate (interface, scheduler-wire, Anthropic adapter, second-opinion, cache, auto-fix, prompt-SIGHUP, per-category) · T3 #368 followup-triage · T4 #372 mutation infra · #375 severity extract · #377 fold≡state property test
- **2026-06-02 Phase S3 SHIPPED** (durability): T1 #367 W8 OPA hot-reload · T2 #369+#378 substrate Phase B+C cutover (approvals) · T3 #379+#389+#393+#395 multi-key + rotate-CLI + brief re-sign + recovery · T4 #366+#382+#391+#394 crash-recovery WriteHook seam + property runner + nightly + cost/reaper extension
- **2026-06-02 Sweeps + Self-improvement SHIPPED**: #80 audit · #83 composite index · #89 batched UPSERTs · #92 brief replay · #95 materializePending · #133 notify adapter · #182 approval E2E · #187 perf · #195 #198 contract · #238 #239 #240 cost-gov · #78 acceptance drift · #79 brief re-sign · #383 #384 crash-recovery extensions · #326 dispatch templates · #337 boot-prompt rules · #349 templates patch · #396 reload-readiness gate · #397 operator getting-started doc. ~56 PRs merged in 2026-06-02 morning wave.
- **2026-06-02 PM wave SHIPPED** (post 18:30 UTC, ~20 PR batch): obs wiring #436 (EventCostReconcileFailing OTel ERROR severity) · #438 (OTEL_TRACES_SAMPLER env honor) · #442 (reconcile.Run wired into serve) · #448 (OPAAuthorizer + hot-reload wired into serve); cost/pricing hardening #440 (boot validator + known-bad fixture) · #441 (soft-cap warn-mode explicit ack) · #451 (empty-table guard + zero-rate defense) · #452 (reconcile_failing attempt_count fix) · #454 (reaper tier-comparison mutation tests) · #461 (sampler-customization E2E); approvals contract #455 (approval_list.v1.json + schema-check) · #459 (nil reviewer_set orEmpty shim) · #460 (canon.VerifyToken reviewer-from-claim derivation) · #462 (approvals.go 95% coverage); refactors #434 (retire BudgetReconciledPayload stub) · #443 (consolidate ApprovalGateConfig with approval.Config) · #444 (reaper fold helpers via recordEvent) · #450 (reconcile/appender_test spend pkg import) · #453 (typed TransitionWorkItem; drop raw-SQL CAS).
- **2026-06-02 Strategic-design CONVERGENCE**: #432 MERGED observability-roadmap converged spec (consolidates #400 #405 #410 #413 #420). #433 MERGED next-horizon roadmap unified brief — 4 MVR-1 impl-ready items + 16 wave-4 items + 4 RISK followups (#423 #424 #426 #427). 29 prior strategic-design PRs CLOSED as superseded per `feedback_design_iteration_local`. Boot-prompt refresh PR #437 CLOSED (superseded by this refresh).

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
