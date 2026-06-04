# Autonomous Session Trigger Prompt

Copy-paste this prompt to bootstrap a fully autonomous regatta dev session. Designed for max velocity: subagent-heavy, decision-deferred-to-review, no user round-trips.

---

## Prompt

```
Continue regatta development autonomously. Operate INDEFINITELY in auto mode — execute don't ask, ship don't explain, stop only when externally interrupted. **Self-host-first; Phase S1+S2+S3 + OBS-A/B/C/D + PHASE-AUTONOMY W1-W7 ALL SHIPPED through 2026-06-03 (~150+ PRs merged across autonomous sessions). Autonomy-loop structurally CLOSED — regatta can dispatch + L4-review + auto-merge + cost-throttle + self-improvement-detect + alarm-webhook → file issue → close loop. Operator action: `regatta install-service` once + watch.** Direct path to first paying customer: Phase DEPLOY (container + systemd/launchd, READY — operator install pending) → Phase GREEN-CLOCK (30-day-green trigger — counts after deploy) → Phase MVR-1 (first external-customer wedges + DW-superset T7 strategy iface — specs landed for T1/T2/T3/T5/T6, gated on customer-0 interview) → Phase MVR-2 (paying-customer wedges + DW substrate bridge T6 + /workflows UI T7 — skeleton specs landed) → Phase MVR-3 (DW-superset capstone — script gate T5 + JS runtime T6 — makes regatta superset of Claude-Code Dynamic Workflows; skeleton specs in flight). External-buyer wedges (W8 multi-tenant, W10 Sigstore, W11 blackboard, W12 billing, P3.8 swap-out adapters, W9 Temporal-backed impl) stay Phase X until 30-day-green OR external-customer-ask fires. Never bottleneck on roadmap depth — pre-fetch next horizon per feedback_roadmap_pre_fetch when current wave drains. NEVER ask for clarification; decide via subagent + memory rules per feedback_decision_priority (UX > ease > performance > best-practices > speed > velocity). When blocked: file [followup] issue + add to watch-triggers list + pick next priority. Pause only for genuinely irreversible action (tag signing, secret rotation, branch-protection downgrade).

BOOT
1. cd /Users/treedesk/Desktop/Projects/regatta && git fetch && git pull --ff-only main
2. make check && bash scripts/cleanup-merged-branches.sh
3. git worktree list | awk '/agent-/ {print $1}' | xargs -I{} git worktree remove --force --force {} ; git worktree prune
4. gh pr list --state open  (note current state; in-flight PRs are normal)
5. Read MEMORY.md + AGENTS.md (auto-loaded). Specs in `docs/engineer/specs/` are canonical for execution.

PRIORITY (top-down, current direct path: deploy regatta-the-binary → green-clock → first paying customer)

PHASE S — Self-host dogfood-ready core [COMPLETE 2026-06-02]
  S1+S2+S3 shipped. Acceptance: regatta dispatches itself on this repo end-to-end. Smoke test PASSED LIVE. (Detail in git history — pruned per feedback_boot_prompt_per_wave_refresh.)

PHASE OBS-A/B/C/D — Observability full stack [COMPLETE 2026-06-03]
  All 4 waves (meter+dashboards+SLOs · substrate health · agent-loop telemetry · operator surface) shipped or queued for merge. Acceptance: Prom scraping regatta /metrics + Grafana dashboards live + Sloth-compiled SLOs alerting + event-rate alarm + HMAC chain detector + divergence-audit + W9 replay-latency + PR-lifecycle stages + cost-per-PR + subagent-failure-taxonomy + `regatta status` TUI + daily digest + trigger-clock dashboard. Per #432 spec.

PHASE AUTONOMY — 7 wedges closing the autonomous-loop gaps [COMPLETE 2026-06-03]
  Per #458 spec. All 7 wedges landed or queued: W1 alarm-webhook · W2 auto-merge-on-gate-pass (intent/outbox + awaiting_merge recovery re-probe) · W3 service-supervisor · W6 secret-credential-fetch · W4 self-improvement-detector · W5 cost-cap-autonomic-enforcement · W7 PR-merge L4-as-review identity (#589). Acceptance MET (structural): regatta serve can run unattended dispatching + L4-reviewing + auto-merging + cost-throttling + alarm-webhooking → file issue → close loop. Operator action: one `regatta install-service` invocation + watch.

PHASE DEPLOY — Production deploy of regatta-the-binary [READY — operator install pending]
  Container Stage 1+2+3 SHIPPED. Operator action required (ONE of):
    Option A: docker compose up -d (Stage 2 — full obs stack)
    Option B: regatta install-service --system (Linux native)
    Option C: regatta install-service (macOS native)
  Env vars: ANTHROPIC_API_KEY · GH_TOKEN · REGATTA_BRIEF_HMAC_KEYS (optional, markdown-only).
  Acceptance: regatta serve running 24/7 against this repo. **This is the next operator-side gate — every downstream phase blocks on it.**

PHASE GREEN-CLOCK — 30-day-green trigger [BLOCKED on DEPLOY]
  Metric: ≥10 PRs/day green-merge ≥30 consecutive days unattended. Each green-merge from regatta-the-binary increments the day-count. Operator intervention (manual merge) resets to day-0. Trigger fires → unlocks Phase X. **Day-0 starts only after Phase DEPLOY operator action.**

PHASE MVR-1 — First external-customer wedges [SPECS LANDED · IMPL BLOCKED on GREEN-CLOCK OR external-customer-ask]
  Per #433 unified next-horizon roadmap + §14 DW-superset integration. Specs landed 2026-06-03:
    MVR-1 T1: operator-console v5.1 (SUPERSEDES htmx W7 Wave 1) — spec #701 (docs/engineer/specs/2026-06-02-operator-console-design.md) + S0 substrate-prereqs plan (docs/engineer/plans/2026-06-03-operator-console-s0-substrate-prereqs.md). Dual-principal (regatta-self + human) SvelteKit console; 5 slices, ~26 wk v1; S0 unblocks S1-S4 by adding runs registry + run_id columns + tool_call substrate kind + gh-checks poller + prwatch DIRTY. Original W7 htmx #601 retired (3-phase rip planned in S1).
    MVR-1 T2: regatta init bundle (GoReleaser + GH-issue adapter) — #590
    MVR-1 T3: P3.8 SCM adapter (Gitea first) — #603
    MVR-1 T5: CUE gate — #602
    MVR-1 T6: goja runtime — #604
    MVR-1 T7: strategy interface + concurrency-policy unify (DW-superset Wave A; pieces 1+5 from roadmap §14) — refactor only, parallel with T1, internal-velocity compound
  GATED on: customer-0 interview (interview ≥3 OSS-maintainers-of-large-repos before dispatch — original tracker #423 closed; re-open via search `gh issue list --search 'customer-0' --state open` if a successor tracker exists). Estimated 7 weeks once customer-0 confirmed.

PHASE MVR-2 — First paying customer [SKELETON SPECS LANDED #670 · IMPL DEFERRED until MVR-1 closes AND 1 signed pilot LOI from persona-B/D per roadmap §2 Gate 2 tier 2]
  Per #433 §4 + §14. Adds two DW-superset pieces alongside W7 Wave 2/3:
    MVR-2 T1: W7 Wave 2 htmx (DAG read view + reviewer-rich PR UI)
    MVR-2 T2: W8 multi-tenant tenant_id routing
    MVR-2 T3: Retract primitive (G10)
    MVR-2 T4: P3.8 LLM-gateway adapter (LiteLLM | portkey)
    MVR-2 T5: W7 Wave 3 polish + docs
    MVR-2 T6: substrate bridge for script-runs (DW-superset Wave B piece 4) — every script step writes kind=fact event, replay-grade
    MVR-2 T7: /workflows progress UI (DW-superset Wave A piece 6) — reuses W7 htmx scaffold
  Estimated 14 weeks.

PHASE MVR-3 — 5+ paying customers + DW-superset capstone [SKELETON SPECS IN FLIGHT (T1-T4 separate PR) · IMPL DEFERRED until 5 paying customers signed across persona B/C/D per roadmap §2 Gate 2 tier 3 OR week 24 of MVR-3 window closes]
  Per #433 §4 + §14. Two new DW pieces alongside Sigstore/Stripe/blackboard/research-mode:
    MVR-3 T1: W10 Sigstore (cosign behind signer adapter)
    MVR-3 T2: W12 Stripe Metering behind billing adapter
    MVR-3 T3: W11 blackboard sqlite-CAS
    MVR-3 T4: research-mode overlay
    MVR-3 T5: script-plan gate adapter (DW-superset Wave B piece 3) — L0-L6 + CUE validates LLM-emitted DAG before runtime accepts
    MVR-3 T6: LLM-authored JS runtime via goja (DW-superset Wave C piece 2) — pure-Go ES5.1+, sandboxed bridge (spawn/fanout/gather only, no FS/eval/net)
  Estimated 20 weeks. T6 is the customer-facing capstone — regatta becomes superset of Claude-Code Dynamic Workflows with gates + substrate replay + signed handoffs DW lacks.

PHASE X — External-buyer wedges [DEFERRED]
  P3.8 swap-out adapters · W9 Temporal-backed DurableHistory. Specs in main. DO NOT dispatch implementers. Reopen on: external-customer-ask OR 30-day-green trigger. (W8/W10/W11/W12 moved into MVR-2/MVR-3 above.)

OPEN FOLLOWUPS (sweep when between phase items, ≤5 trivial PRs/session cap)

<!-- BEGIN auto-priority -->
- #753 [PERF] scheduler/filter: bench generic monomorphization cost (#704 R2)
- #758 [CI-followup] add scorecard pre-validation in implementer dispatch + pre-push hook
- #759 [followup] comment-density: 238 existing prod .go files exceed 5% (G4 scoped gate-add-only)
- #760 [G3-followup] migrate 10 polling time.Sleep sites to testutil.Eventually
- #766 [DESIGN-D-followup] operator-console UI roadmap — amend with minimal-input principle
- #773 [703-followup] R3 GetWorkItem transient error in orphan recheck: no backoff, next tick hammers same orphan
- #776 [703-followup] R2 fetchWorkItemForRecheck fail-open vs fail-closed semantics
- #777 [velocity-followup] gopls LSP diagnostic noise from cross-worktree files
- #778 [velocity-followup] pre-PR scorecard pre-validation hook
- #779 [velocity-followup] document --theirs vs --ours rebase strategy in dispatch templates
<!-- END auto-priority -->

- RISK followups — strategic-design closeout: #423 #424 #426 #427 all CLOSED 2026-06-03; no open RISK trackers remain in this slice.
- ~80 followup tracking issues filed across PHASE-AUTONOMY + OBS wave drains (#573-#588, #606-#667) — sweep by tier (correctness > consistency > docs)
- Architecture-review followups — #551 generalize external-side-effect intent/outbox · #548 GDPR crypto-shred. (#549 #550 #553 #554 CLOSED 2026-06-03.)
- Recurring A+ rubric checkboxes — fuzz, mutation testing extensions, key-rotation drill extensions

Already shipped (do NOT redo) — confirm via `git log --oneline origin/main -200`. Per feedback_boot_prompt_per_wave_refresh, entries >2 waves old are pruned; older shipped wedges live in git history only.

<!-- BEGIN auto-shipped -->
- #693 feat(lint-substrate-queries): walk BinaryExpr concat chains for substrate queries (#234)
- #694 perf(substrate): F1 fast-path skips signedPayload round-trip (#216)
- #695 [CHORE] dispatch-templates: harden scorecard backtick + release-notes-fence traps
- #696 fix(test): absorb linux CI variance in spawner span-recorder poll
- #697 fix(scheduler): re-check approval gate in reservation loop (closes #167)
- #698 refactor(scheduler): consolidate apply{Approval,Cost,L4}Gate into shared filter.Apply (#251)
- #699 [FEAT] obs/lint: Tracer+Meter pair grep-invariant gate (closes #509)
- #701 [DOCS] operator console v5.1 spec + v2 backlog (customer-0 dual-principal)
- #707 [CHORE] docker-compose UI default + boot prompt wave refresh
- #708 [FEATURE] operator-console S0 plan + MVR-1-T1 pointer update (boot-prompt + roadmap)
- #710 [CHORE] collapse multi-line Test/Fuzz/Benchmark godocs
- #711 [DOCS] specs: status frontmatter + skeleton-prefetch/superseded sections
- #712 [CHANGE] prwatch: delete deprecated BranchRenameThreshold const alias
- #713 [DOCS] boot-prompt purge + check-memory-citations gate + 5 broken slug cite fixes
- #716 [CHORE] add scripts/worktree-gc.sh — dry-run-default GC for merged agent worktrees
- #717 [REFACTOR] unify GitHubClient/GHClient under internal/ghclient.Client
- #718 [REFACTOR] collapse otel.Meter literals via obs.Meter factory (Wave B2)
- #721 [CHANGE] health: delete W7.0 plaintext shim — /healthz always returns JSON
- #723 [CI] scripts/check-phase-x-leak.sh — mechanical self-host filter gate
- #732 [DOCS] purge bare TBD placeholders + add check-tbd gate
- #733 [REFACTOR] testutil: add Eventually helper + migrate 5 high-flake polling sites
- #734 [REFACTOR] Wave E: inline single-impl interfaces (LLMClient/EventSource/CohortReader/BridgeOption); delete dead estimate.Estimator
- #737 [REFACTOR] split cmd/regatta/serve.go (1068→394 LOC) into per-subsystem wire_*.go siblings
- #740 [CI] add htmx to Phase-X leak token list (#724)
- #742 [CHORE] scripts/gen-boot-status.sh: auto-regen boot-prompt shipped/priority blocks from gh
- #743 [DOCS] dispatch-templates: enforce comment-zero-by-default + MED-sev reviewer sweep
- #744 [CHORE] cmd/regatta: rename serve_*.go wiring siblings to wire_*.go (#738)
- #745 [CI] check-memory-citations: ship CI-portable fixture (closes #714)
- #746 [DOCS] reconcile getting-started + w7 spec with JSON-only /healthz (closes #722)
- #747 [CI] check-scorecard.sh: count backtick-wrapped citations (closes #741)
- #748 [FIX] substrate: wrap schemas.ErrUnverifiable to match docstring (closes #715)
- #749 [CI] check-tbd: tighten HTML-comment whitelist (closes #735)
- #750 [CHORE] scripts/worktree-gc: detect squash-merged via git cherry (closes #719)
- #751 [DOCS] mvr-2-t6 spec: replace active tenant validator with DefaultTenantID seam (closes #725)
- #752 [REFACTOR] testutil+tests: EventuallyT + AssertStable; close 3 #733-skipped polling sites
- #754 [FIX] scheduler: rev L4GateResolver to L4Scope shape + cross-pass property (closes #704)
- #755 [CI] obs/lint: close 3 Tracer/Meter pair gaps + stale-guard test (closes #706)
- #756 [FIX] authz/reload: handle root-Remove before shouldHandleEvent filter (#702)
- #757 [FIX] substrate fast-path: pointer→value HMAC key cache; drop SigMAC alias (closes #700)
- #761 [DOCS] design-c: sequenced cutover spec for BudgetReconciledPayload float deprecation (#709)
- #762 [REFACTOR] consolidate test-DB helpers into internal/testutil/statetest
- #763 [CI] scripts/check-comment-density: enforce 5pct ceiling on new prod .go in PR diff
- #764 [DOCS] specs: state/ god-package split design (Option E hybrid) (#739)
- #765 [DOCS] specs: operator-console UI phase-S roadmap (S1->S2->S3)
- #767 [DOCS] design-b: approvals_shadow cutover sequence (#720 blocker)
- #768 [REFACTOR] substrate Sweeper/SweepFullChain + health heartbeat pool: caller-injected *sql.DB (G2)
- #770 [CI] add check-no-bare-sleep gate; annotate 10 polling sites (#760)
- #771 [CI] check-release-notes-local: add REFACTOR + FIX to allowlist
- #774 [FIX] cost-backfill: 4 adversarial-review findings (R1-R4) (closes #705)
- #775 [FIX] scheduler: cost+L4 orphan recheck, getter-missing warn, preserve reserved-on-err (closes #703 R1,R2,R4)
<!-- END auto-shipped -->

- **2026-06-03 Wave SHIPPED** (~60+ PRs across PHASE-AUTONOMY + OBS final drains + MVR specs + memory/scorecard infra + post-merge reliability+UX polish):
  - PHASE-AUTONOMY W1-W7: all 7 wedges IMPL landed. W7 L4-as-review identity #589. W3 supervisor #597 (`regatta install-service` + healthz + sd_notify).
  - OBS Wave A/B/C/D: all 4 waves IMPL shipped.
  - MVR-1 specs: T1 htmx UI #601 · T2 init bundle #590 · T3 SCM adapter #603 · T5 CUE gate #602 · T6 goja runtime #604 · T7 strategy iface refactor pending dispatch.
  - MVR-2 skeleton specs: 7 (T1-T7) via #670.
  - MVR-3 skeleton specs: 4 (T1-T4) landed.
  - Memory consolidation #594 (CLAUDE.md + boot prompt RULES expanded).
  - Scorecard CI gate #669 (machine-checkable rubric — per-criterion citation enforced in pr-lint).
  - Critical-path cascade fixes: scheduler/orchestrator/Makefile splits + auto-gen specs README #583 · cost+resume CLI db.Close defer #668 (closes #649) · substrate manual_merge + operator_intervention event kinds #665 (#659).
  - **Operator-UX polish wave (post-autonomy-loop closure):**
    - #690 install-service healthz port respect operator config (closes #667)
    - #689 `--public-url` flag for reverse-proxy Origin check (closes #304)
    - #691 reloader F-HR8 + uncovered watch-root re-Add bug (closes #365)
    - #692 `regatta cost backfill <run_id>` recovery CLI (closes #272)
    - #693 substrate AST-concat lint for built SQL strings (closes #234)
    - #694 substrate fast-path 47× perf, 213× fewer allocs (closes #216)
    - #696 flaky `TestClaudeSpawn_StreamJsonOpens...` deadline bump 2s→10s
    - #697 scheduler gate re-check at reservation loop (closes #167)
    - #698 scheduler filter.Apply consolidation refactor (closes #251)
    - #699 Tracer+Meter pair grep-invariant CI gate (closes #509)
  - **Dispatch-template hardening:** #688 worktree /tmp/clone trap (closes #188) · #695 scorecard-backtick + release-notes-fence traps.
  - **Issue triage:** ~80 followup tracking issues filed (#573-#588, #606-#667, #700-#706). 33+ closed via PRs or moved to reopen-trigger state.
  - **Adversarial-review backfill:** 6 retroactive reviews on load-bearing PRs from this wave → 22 Risk+ findings → 6 followup issues #700 #702 #703 #704 #705 #706.
  - **New feedback memories** (3 this session): `feedback_ci_timeout_orphan_test_goroutine` · `feedback_release_notes_fence_missing` · `feedback_scorecard_citation_token_outside_backticks` (reinforced).
  - Self-host loop structurally complete. Phase DEPLOY READY (Docker stage 2 compose covers full obs stack). Operator action: set `REGATTA_HMAC_KEY` + `docker compose up -d` OR `regatta install-service` on bare host.
- **2026-06-02 Strategic-design CONVERGENCE**: #432 MERGED observability-roadmap converged spec (consolidates #400 #405 #410 #413 #420). #433 MERGED next-horizon roadmap unified brief — 4 MVR-1 impl-ready items + 16 wave-4 items + 4 RISK followups (#423 #424 #426 #427). 29 prior strategic-design PRs CLOSED as superseded per `feedback_design_iteration_local`.

WORKFLOW per item — use templates at `docs/engineer/dispatch-templates/`. Substitute variables; do NOT inline-repeat preamble.
1. Design subagent → spec — `designer.md` (rubric, OSS-first, self-host filter)
2. Adversarial reviewer on spec → fix findings — `reviewer.md`
3. Plan subagent → plan — `designer.md` (plans are spec-shaped)
4. Parallel implementer subagents on file-disjoint tasks — `implementer.md` (worktree + TDD + scorecard + release-notes + doc-check)
5. Adversarial reviewer per wave → fix → merge — `reviewer.md`
6. Land / defer / reject decisions on issues + stale PRs — `triage.md`

Templates encode load-bearing preamble: worktree-first, TDD failing-first, adversarial reviewer, A+ scorecard, doc-check banned phrases, release-notes fence, no-signatures, memory cites, PHASE-S-RELAX conditions. Cite memory rules in dispatch prompts via the templates' `<MEMORY-RULES>` variable.

RULES (canonical — repo-tracked at CLAUDE.md; this section adds autonomous-loop-only rules)

The bulk of agent rules now live in repo-root `CLAUDE.md` (auto-loaded by every agent in this tree). Read it once at session start. The block below ONLY captures rules specific to the indefinite-autonomous-loop mode that wouldn't make sense in a one-off dev session.

- Subagents do everything: design, plan, impl, review, doc, PR-body drafting, issue filing, debugging. Main thread = dispatcher + integrator.
- **W9 substrate-choice locked = option C hybrid, self-host scope = substrate-default impl ONLY** (memory/wedge_roadmap_assessment §"Substrate + W9 substrate-choice locked 2026-06-01" + self-host-first brief §3 S2-T1): ship W9 against `DurableHistory` Go interface, default impl on substrate `events`. Temporal-backed impl is Phase X — gated behind refined P2.5 trigger (sqlite contention >5% OR ≥30 concurrent OR replay-recovery >60s — any one, two consecutive 24h windows) AND external customer ask. W9 promoted ahead of W7/W8 for self-host loop closure. Never re-litigate during implementer dispatch.
- PHASE-S-RELAX no relaxations active — full-gate posture across all templates. Reopen-trigger: next gate-relaxation window (pre-launch hardening freeze OR customer-pilot mode). Archived rule at `archive/feedback_gate_relaxation_phase_s.md` in operator memory.
- **Comment-noise gate trip-traps** per #333 followup — reviewer-tag regex over-matches reviewer-Request / reviewer-JSON prose; banner-comment regex rejects `# --- Section ---`. Dodge: hyphenate or lowercase, replace banners with plain `# Section.`.

AUTONOMOUS-LOOP CADENCE
- **Dispatch discipline** (`feedback_dispatch_discipline`): 3 loops — (1) parallelize by default, sequence on dep-graph; (2) status-report cadence after every wave-dispatch + ~3 subagent completions + when wave drains to ≤2 lanes; (3) GH-API throttle — batch `gh pr list --json` polls, ls-remote over fetch.
- **Status report cadence** (`feedback_status_report_cadence`): surface report after wave-dispatch, every ~3 completions, when wave drains ≤2, on blocker dropping active count below floor.
- **No idle wait** (`feedback_no_idle_wait`) [soft, 2026-06-02]: avoid redundant wakeups while agents in flight. Minimum-N-agent floors optional — apply only when operator restates per-session.
- **Anticipate starvation** (`feedback_anticipate_starvation`): keep ≥N active by pre-fetching next-horizon work. Priority for idle slots: adversarial reviews → spec drafts → followup triage → next-wave dispatch.
- **Roadmap pre-fetch** (`feedback_roadmap_pre_fetch`): when current wave ≥80% shipped OR <2 unblocked items, spawn design-subagent for next-horizon brief at `docs/engineer/briefs/YYYY-MM-DD-<topic>.md`.
- **Indefinite autonomy** (`feedback_indefinite_autonomy`): do NOT stop at PHASE GREEN-CLOCK or any milestone. After autonomy closes, continue designing + building. Pull from `[followup]` issues when waves drain. Halt only on externally-irreversible action.
- **TaskCreate usage** (`feedback_task_create_usage`): use for ≥4 discrete dispatches, multi-wave roadmap, crash-prone work. Skip for single-pass audits, 1-2 step atomic edits, Q&A.
- **Boot-prompt per-wave refresh** (`feedback_boot_prompt_per_wave_refresh`): after wave merges, edit PRIORITY + "Already shipped" sections; open `docs(engineer):` PR with automerge. Drop entries >2 waves old.
- **Self-improvement** (`feedback_self_improvement`): when session friction observed (slow ops, repeated lookups, ambiguous dispatch prompts), self-diagnose root cause + ship fix in same session. Authority granted 2026-06-02.

AUTOMERGE GATING
- **Review before automerge** (`feedback_review_before_automerge`): automerge fires ONLY when (1) independent reviewer ran on current head (not stale rev) AND (2) every Risk-tier+ finding addressed (inline-fix OR tracking issue #).
- **Review every step** (`feedback_review_every_step`): pipeline gate at design draft, roadmap, plan, impl. Each iterates edit-in-place + re-review → ADOPT.
- **Post-automerge CI monitor** (`feedback_post_automerge_ci_monitor`): after `gh pr merge --auto`, CI may fail post-rebase OR DIRTY merge-state may surface silently. Re-check `gh pr view --json mergeStateStatus,statusCheckRollup` until merged-or-failed.
- **Agent load-bearing → issues** (`feedback_agent_load_bearing_to_issues`): subagent findings NOT addressed in own PR → main thread files tracking issue, never leaves as PR comment.

WORKTREE / GIT HYGIENE (long-session)
- **Agent tree spillage** (`feedback_agent_tree_spillage`): harness sometimes drops agents into primary tree instead of worktree. Stash primary before reset; verify `.claude/worktrees/agent-<id>/` matches before edits.
- **Git ops speed** (`feedback_git_ops_speed`): periodic `git gc`; bulk-delete stale branches; batch `gh pr list --json`; ls-remote over fetch; classifier-overhead tax is real (1-3s/Bash invisible-but-real).
- **Stale worktree GC**: `make worktree-gc` (dry-run) / `make worktree-gc-apply` removes agent worktrees whose branch is merged into `origin/main`. Skips primary checkout, cwd, and any worktree on `main`. Default mode is dry-run — destructive `--apply` is opt-in.

NOTE: All other agent rules (token economy, identity, comments, CI gates, TDD, reviewer, worktree basics, dispatch caps, decision priority, root cause, deletion default, drop ceremony, self-host filter, branch protection) live in `CLAUDE.md` at repo root.

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