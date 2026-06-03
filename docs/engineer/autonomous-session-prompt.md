# Autonomous Session Trigger Prompt

Copy-paste this prompt to bootstrap a fully autonomous regatta dev session. Designed for max velocity: subagent-heavy, decision-deferred-to-review, no user round-trips.

---

## Prompt

```
Continue regatta development autonomously. Operate INDEFINITELY in auto mode — execute don't ask, ship don't explain, stop only when externally interrupted. **Self-host-first; Phase S1+S2+S3 SHIPPED 2026-06-02 (96+ PRs merged in single autonomous session).** Direct path to autonomous self-improvement loop: Phase OBS-A (meter + dashboards + SLOs, ACTIVE) → Phase OBS-B/C/D (substrate + agent-loop telemetry + operator surface) → Phase AUTONOMY (#458 — 7 wedges closing the autonomous-loop gaps) → Phase DEPLOY (container + systemd/launchd, READY) → Phase GREEN-CLOCK (30-day-green trigger) → Phase MVR-1 (first external-customer wedges + DW-superset T7 strategy iface, gated on customer-0 interview) → Phase MVR-2 (paying-customer wedges + DW substrate bridge T6 + /workflows UI T7) → Phase MVR-3 (DW-superset capstone — script gate T5 + JS runtime T6 — makes regatta superset of Claude-Code Dynamic Workflows). External-buyer wedges (W8 multi-tenant, W10 Sigstore, W11 blackboard, W12 billing, P3.8 swap-out adapters, W9 Temporal-backed impl) stay Phase X until 30-day-green OR external-customer-ask fires. Never bottleneck on roadmap depth — pre-fetch next horizon per feedback_roadmap_pre_fetch when current wave drains. NEVER ask for clarification; decide via subagent + memory rules per feedback_decision_priority (UX > ease > performance > best-practices > speed > velocity). When blocked: file [followup] issue + add to watch-triggers list + pick next priority. Pause only for genuinely irreversible action (tag signing, secret rotation, branch-protection downgrade).

BOOT
1. cd /Users/treedesk/Desktop/Projects/regatta && git fetch && git pull --ff-only main
2. make check && bash scripts/cleanup-merged-branches.sh
3. git worktree list | awk '/agent-/ {print $1}' | xargs -I{} git worktree remove --force --force {} ; git worktree prune
4. gh pr list --state open  (note current state; in-flight PRs are normal)
5. Read MEMORY.md + AGENTS.md (auto-loaded). Specs in `docs/engineer/specs/` are canonical for execution.

PRIORITY (top-down, current direct path to autonomous self-improvement loop)

PHASE S — Self-host dogfood-ready core [COMPLETE]
  S1+S2+S3 shipped 2026-06-02. Acceptance: regatta dispatches itself on this repo end-to-end. Smoke test PASSED LIVE.

PHASE OBS-A — Observability Wave-A meter + dashboards + SLOs [ACTIVE]
  8 items in .regatta/items/obs-wave-a-*.md. Operator's autonomous loop shipping: #490 (MeterProvider + OTLP/Prom), #491 (gate_rejected events), #495 (cost.usd metrics), #496 (Meter fan-out), #502 (scheduler tick latency), #503 (OpenSLO YAML), #505 (digest cron). Target: all 8 items merged. Acceptance: Prom scraping regatta /metrics + 6 Grafana dashboards live + Sloth-compiled SLOs alerting.

PHASE OBS-B/C/D — Substrate health + agent-loop telemetry + operator surface [QUEUED]
  11 items in .regatta/items/obs-wave-{b,c,d}-*.md per #432 spec. Dispatch after OBS-A green. Acceptance: event-rate alarm + HMAC chain detector + divergence-audit dashboard + W9 replay-latency + PR-lifecycle stages + cost-per-PR + subagent-failure-taxonomy + `regatta status` TUI + daily digest + trigger-clock dashboard.

PHASE AUTONOMY — 7 wedges closing the autonomous-loop gaps [BLOCKED on OBS-A green]
  Per #458 spec. 7 items in .regatta/items/phase-autonomy-w{1..7}-*.md. Sequenced by layer:
    Layer 1 [obs→issue→merge]: W1 alarm-webhook · W2 auto-merge-on-gate-pass (c0 BLOCKS c2: ship intent/outbox + awaiting_merge recovery re-probe before any real `gh pr merge` — see #552, #551)
    Layer 2 [bootstrap]: W3 service-supervisor · W6 secret-credential-fetch
    Layer 3 [self-improvement]: W4 self-improvement-detector · W5 cost-cap-autonomic-enforcement · W7 PR-merge L4-as-review identity
  Total ~1100 LoC (+120 for W2 c0), ~10-14 days subagent-time. Acceptance: regatta serve runs unattended for 7 days dispatching + auto-merging without operator click.

PHASE DEPLOY — Production deploy of regatta-the-binary [READY]
  Container Stage 1+2+3 SHIPPED (#518 #534 #533 #536). Operator action required:
    Option A: docker compose up -d (Stage 2 — full obs stack)
    Option B: ./deploy/install-systemd.sh (Linux native)
    Option C: ./deploy/install-launchd.sh (macOS native)
  Env vars: ANTHROPIC_API_KEY · GH_TOKEN · REGATTA_BRIEF_HMAC_KEYS (optional, markdown-only).
  Acceptance: regatta serve running 24/7 against this repo.

PHASE GREEN-CLOCK — 30-day-green trigger [BLOCKED on DEPLOY]
  Metric: ≥10 PRs/day green-merge ≥30 consecutive days unattended. Each green-merge from regatta-the-binary increments the day-count. Operator intervention (manual merge) resets to day-0. Trigger fires → unlocks Phase X.

PHASE MVR-1 — First external-customer wedges [BLOCKED on GREEN-CLOCK OR external-customer-ask]
  Per #433 unified next-horizon roadmap + §14 DW-superset integration. 5 items in .regatta/items/mvr-1-*.md:
    MVR-1 T1: W7 Wave 1 htmx UI MVP
    MVR-1 T2: regatta init bundle (GoReleaser + GH-issue adapter)
    MVR-1 T3: P3.8 SCM adapter (Gitea first)
    MVR-1 T6: pricing — support contracts
    MVR-1 T7: strategy interface + concurrency-policy unify (DW-superset Wave A; pieces 1+5 from roadmap §14) — refactor only, parallel with T1, internal-velocity compound
  GATED on: customer-0 interview (#423 — must interview ≥3 OSS-maintainers-of-large-repos before dispatch). Estimated 7 weeks once customer-0 confirmed.

PHASE MVR-2 — First paying customer [DEFERRED until MVR-1 closes AND 1 signed pilot LOI from persona-B/D per roadmap §2 Gate 2 tier 2]
  Per #433 §4 + §14. Adds two DW-superset pieces alongside W7 Wave 2/3:
    MVR-2 T1: W7 Wave 2 htmx (DAG read view + reviewer-rich PR UI)
    MVR-2 T2: W8 multi-tenant tenant_id routing
    MVR-2 T3: Retract primitive (G10)
    MVR-2 T4: P3.8 LLM-gateway adapter (LiteLLM | portkey)
    MVR-2 T5: W7 Wave 3 polish + docs
    MVR-2 T6: substrate bridge for script-runs (DW-superset Wave B piece 4) — every script step writes kind=fact event, replay-grade
    MVR-2 T7: /workflows progress UI (DW-superset Wave A piece 6) — reuses W7 htmx scaffold
  Estimated 14 weeks.

PHASE MVR-3 — 5+ paying customers + DW-superset capstone [DEFERRED until 5 paying customers signed across persona B/C/D per roadmap §2 Gate 2 tier 3 OR week 24 of MVR-3 window closes]
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
- RISK followups #423 #424 #426 #427 (filed 2026-06-02 strategic-design closeout)
- OBS followups inline in `docs/engineer/specs/2026-06-02-observability-roadmap.md` (cost-per-agent integration + Wave-C rollup-shape)
- Open issue #15 superseded by #463 PR-watcher locked design (close on merge)
- Late-arriving open PRs to triage: #435 (FuzzToken_Verify) · #449 (cost-gov runbook) · #458 (PHASE AUTONOMY spec) · #463 (PR-watcher locked design) · #464 #465 (otel/bridge property test + bench)
- Architecture-review followups (filed 2026-06-02, boundary-gap + consistency set) — #553 canonical-JSON fork unify (HIGH, signing correctness) · #554 integer micro-USD money (HIGH, budget exactness) · #551 generalize external-side-effect intent/outbox · #550 gate-determinism reframe · #549 replay engine-version journal · #548 GDPR crypto-shred. #553+#554 are the two highest-value next PRs (real correctness bugs, well-bounded).
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

TOKEN ECONOMY (subagents do NOT inherit memory dir — these rules MUST be enforced via dispatch prompts)
- **Dispatch brief only** per `feedback_dispatch_brief_only`. Implementer subagents receive per-task brief (spec §12 style), NOT full spec doc. Main thread keeps full spec for cross-cutting Qs.
- **gh minimal fields** per `feedback_gh_minimal_fields`. Every `gh pr list/view` MUST pass explicit `--json` allowlist (default `number,state,mergeStateStatus,statusCheckRollup,isDraft,headRefName`) + `-L 20`. No bare `--json`.
- **No memory re-read** per `feedback_no_memory_reread`. Never `cat`/`Read` files under `memory/` — auto-loaded via MEMORY.md. Reference by `[[slug]]`. Exception: editing the memory file itself.
- **PR body cache per phase** per `feedback_pr_body_cache_per_phase`. ONE `gh pr view N --json number,title,body,comments,reviews` per review phase; pass as text to phase subagents. Re-fetch only on phase boundary OR explicit user ask.
- **Subagent ci-check compress** per `feedback_subagent_cicheck_compress`. Implementer reports `make ci-check 2>&1 | tee /tmp/cicheck.log | grep -E "^(FAIL|ok|---|Error|error:|PASS)" | tail -40` + exit code. 85-90% reduction. Main thread still re-runs full (~10% lie rate per `feedback_subagent_verification`).
- **ctx capture dedupe** per `feedback_ctx_capture_dedupe`. Before `ctx_batch_execute` on research/spec content: `ctx_search` first. Skip batch if recent (<24h) hit covers same content. Batch labels: content-distinct, not session-distinct.

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
