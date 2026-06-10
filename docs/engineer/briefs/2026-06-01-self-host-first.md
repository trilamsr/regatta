# regatta — self-host-first roadmap reorder

_Author: design session, 2026-06-01. Source: operator decision — prioritize regatta dispatching regatta-the-binary at regatta-the-repo before any external-buyer scope. Supersedes the rank ordering in `2026-05-31-mvp-3-next-level.md` §4 until external-buyer trigger fires._

## 1. The reorder rule

**Self-host first.** Every wedge filtered by one question: does the sole operator (lumalabs internal) need this to dispatch regatta-the-binary at regatta-the-repo unattended?

- **Keep** → in-scope for max-velocity phase.
- **Defer** → moved to Phase X, ships when an external paying customer asks.

Self-host = single-tenant, single-operator, single-repo, deterministic CI (`make ci-check`), human-merge enforced by GitHub branch protection. No RBAC needed (one operator). No billing needed (no customer). No multi-tenant scoping needed (one tenant). No htmx UI needed (CLI works for solo operator). No Sigstore attestation chain needed (operator trusts own git history). No metered-billing webhook needed (no invoice recipient).

## 2. What we have today (v0.2.0, 2026-05-31)

Already self-host-capable in isolation:

- `ClaudeSpawner` shells `claude` per work item into per-agent worktree (`internal/orchestrator/spawner/claude.go`).
- Markdown spec adapter parses `WorkItem` from on-disk briefs (`internal/orchestrator/adapter/markdown.go`).
- `regatta serve --tick-once` loop ticks scheduler → spawner → gates.
- CLI approval flow: `regatta approval list` / `regatta approval decide --id X --approve` (`cmd/regatta/approval_*.go`).
- Cost governor Waves 1+2 — pre-call USD+token caps, Anthropic Usage API reconciliation.
- Substrate W1 — `substrate_events` append-only log + CELDecider.
- W6 OTel backbone — spans on scheduler/spawner/gates.
- W7.0 listener prereq + W8 T1 OPA authorizer interface (latter shipped 2026-06-01).

Missing for unattended self-dispatch: a `regatta.yaml` pointing at this repo + spend-callback wiring + boot-prompt→brief converter + adversarial-reviewer-as-gate. That is the whole of Phase S1.

## 3. Phases

### Phase S1 — dogfood-ready core _(1-2 weeks)_

**Acceptance gate**: regatta dispatches itself on a real `autonomous`-labeled GH issue in this repo, opens a PR, gates run, operator clicks merge.

#### 3.1. Adapter pin: `github_issues` (amended 2026-06-09)

The original S1-T1 entry deferred GH-vs-markdown to §8 Q1; the markdown
adapter landed first as the self-host pin. PR #1206 shipped the
`regatta-operator` skill, whose FEED phase files wedges directly to GH
issues (label `autonomous`) — that is now the live backlog the operator
already maintains (#1196, #1094, #1092, #1163, ~20 ready wedges as of
2026-06-09). The orchestrator reading markdown_catalog would leave every
FEED write uneaten.

The flip: `spec_adapter.type: github_issues` is the self-host default
going forward. `markdown_catalog` remains schema-supported for archived
briefs + Phase-X buyers but is no longer the self-host pin. GH issues do
not require public ingress for this — the orchestrator pulls issues
outbound via the GitHub API (no webhook inbound), so the single-operator
/ no-public-surface premise of §1 holds. The Phase-S filter still
applies, with the `autonomous` label as the mechanical intake gate.

| # | Task | Status | Effort |
|---|---|---|---|
| S1-T1 | `regatta.yaml` for this repo — `github_issues` adapter against `label:autonomous` (flipped from markdown_catalog 2026-06-09 per §3.1) | SHIPPED | S |
| S1-T2 | Close #282 — wire `spend.SpawnerCallback` into `cmd/regatta/serve.go::buildSpawner` | FILED | XS |
| S1-T3 | Boot-prompt → work_item brief converter — script that turns `autonomous-session-prompt.md` PRIORITY entries into GH issues the github_issues adapter ingests (was: markdown briefs) | NEW | S |
| S1-T4 | Cost-governor Wave 3 (T5+T6+T7 docs) — already next on PRIORITY; ships caps-spend-if-Claude-loops safety | IN FLIGHT | S |
| S1-T5 | Self-host smoke test — end-to-end fixture: regatta picks one `autonomous` issue → PR → green gates → operator merges | NEW | M |

Effort total: ~5-7 days subagent-time.

### Phase S2 — trust-the-loop _(2-3 weeks)_

**Acceptance gate**: operator leaves `regatta serve` running overnight against `[autonomous]` queue without watching every PR. Adversarial review catches the bad PRs; cost caps stop runaway spend; replay-diff lets the operator re-run a flaky decision deterministically the next morning.

| # | Task | Status | Effort |
|---|---|---|---|
| S2-T1 | W9 replay+diff harness, substrate-default `DurableHistory` impl ONLY (skip Temporal-backed variant) — promoted from MVP-3 rank #4 to rank #2 for self-host | SPECCED | M |
| S2-T2 | Adversarial reviewer subagent → first-class gate L4 — bake the prompt-side reviewer into `internal/gates/`; today it lives only in Claude Code dispatch prompts | NEW | M |
| S2-T3 | Followup-issue auto-triage — regatta reads its own `followup`-tagged GH issues, self-files plan briefs back as new GH issues (was: markdown adapter directory; updated per §3.1 adapter flip) | NEW | S |
| S2-T4 | Mutation testing on cost-governor + scheduler (top 2 A+ rubric items from prior waves) | FILED | S |

Effort total: ~10-15 days subagent-time.

### Phase S3 — durability _(3-4 weeks)_

**Acceptance gate**: regatta survives crashes, key rotations, schema migrations, and adversarial brief edits without operator hand-holding.

| # | Task | Status | Effort |
|---|---|---|---|
| S3-T1 | W8 T-remaining — OPA Authorizer impl + policy hot-reload. SKIP multi-tenant `tenant_id` propagation (deferred to Phase X). Slim W8 by ~60%. | SPECCED | M |
| S3-T2 | Substrate Phase B+C cutover — shadow-write + read-from-substrate for cost-gov + approvals only. Skip everything-else cutover. | SPECCED | M |
| S3-T3 | Key-rotation drill — A+ rubric item from substrate W1; document recovery procedure in `docs/operator/quickstart.md` | FILED | S |
| S3-T4 | Crash-recovery property test — 200 random crash-points × scheduler tick; assert no double-spawn, no lost work_item | NEW | S |

Effort total: ~15-20 days subagent-time.

**Self-host complete = end of S3.** Total: ~5-7 calendar weeks at current 3-4 lane parallel pace.

## 4. Phase X — deferred until external buyer

Specced and partly built, but every line cuts time-to-self-host. Re-enter scope on the first external-customer ask, not before.

| Wedge | Reason deferred |
|---|---|
| W7 Waves 1-3 htmx web UI | CLI approval flow already works for solo operator; htmx UI is enterprise-pilot polish |
| W8 multi-tenant `tenant_id` scoping | One operator, one tenant. Authorizer interface stays single-tenant-default. |
| W10 Sigstore attestation chain | No downstream verifier yet. Operator trusts own git history. |
| W11 blackboard shared state | One concurrent operator. No shared-state contention. |
| W12 metered billing + Stripe webhook | No invoice recipient. |
| P3.8 swap-out adapters (5 contracts) | No customer asking for hosted-backend variants. |
| W9 Temporal-backed `DurableHistory` impl | Substrate-default impl covers self-host replay needs; refined P2.5 trigger (sqlite >5% contention OR ≥30 concurrent OR replay >60s, two consecutive 24h windows) has not fired. |
| Reviewer-rich PR UI | Solo operator reads PR diffs in GH directly. |

## 5. What changes vs. prior brief

`2026-05-31-mvp-3-next-level.md` §4 ranks: W6 → W7 → W8 → W9 → W10 → W11 → W12. Driver: enterprise-pilot demo bar.

This brief overrides for the self-host window:

- **W6** — already shipped, no change.
- **W7** — full deferral to Phase X. CLI works. (-1 wave of work)
- **W8** — slim to authorizer-only, defer tenant scoping. (-60% of W8)
- **W9** — promote to Phase S2 rank #1; substrate-default impl only. (kept; Temporal variant deferred)
- **W10 / W11 / W12** — full deferral to Phase X. (-3 waves)
- **P3.8 adapters** — full deferral to Phase X.
- **NEW** — 4 small tasks (S1-T1, S1-T3, S1-T5, S2-T2, S2-T3) not in prior roadmap; together ≤10 days.

Net: prior MVP-3+MVP-4 scope ~6-12 weeks calendar → self-host scope ~5-7 weeks calendar. ~40% scope reduction.

## 6. Decision-priority self-check

Per `memory/feedback_decision_priority` (UX → ease → performance → best-practices → speed → velocity; long-term > short-term):

- **UX (sole operator)** — CLI > htmx UI for solo. Replay > Sigstore for solo debugging. Self-dispatch loop > multi-tenant ergonomics.
- **Ease** — fewer wedges, slimmer W8, no UI build pipeline. -40% scope.
- **Performance** — substrate-default replay impl is faster than Temporal RPC roundtrip for self-host scale.
- **Best-practices** — adversarial-reviewer-as-gate (S2-T2) hardens unattended dispatch; mutation testing (S2-T4) hardens cost-gov.
- **Speed** — 5-7 wks vs 6-12 wks.
- **Long-term** — Phase X is **deferred not abandoned**. Specs stay tracked. First external-customer ask reopens scope without rework — every Phase X wedge already has a locked spec.

## 7. Trigger to exit self-host phase

Move Phase X items back into the active roadmap on **either**:

1. First external customer signs a paid pilot agreement (any wedge they specifically ask for unblocks).
2. Operator measures self-host loop running ≥30 days unattended at ≥10 PRs/day green-merge rate (self-host hardened — open external scope).

Neither has fired. Stay self-host-first until one does.

## 8. Open questions

- **GH-issue adapter vs markdown adapter for S1-T1**: GH issues give richer state (assignees, projects), but markdown is already implemented + works against the boot-prompt directly. Decision: ship markdown adapter first (S1-T1a); GH-issue adapter as S2 stretch (S1-T1b). _Update 2026-06-09 (see §3.1):_ flipped to github_issues. The regatta-operator skill (PR #1206) writes wedges to GH issues as part of its FEED phase; keeping markdown_catalog left every operator-filed wedge uneaten by the orchestrator. GH issues do not require public ingress (orchestrator pulls outbound), so the single-operator no-public-surface premise of §1 is preserved.
- **Boot-prompt converter scope (S1-T3)**: minimum-viable = converts the PRIORITY block into N briefs. Stretch = round-trips updates back into the prompt when waves drain. Decision: ship MV in S1; round-trip in S2 if Time.
- **Adversarial-reviewer-as-gate (S2-T2) model choice**: Sonnet 4.6 in main session, Opus 4.7 in design subagent. For gate use, default Sonnet 4.6 (cost) with `regatta.yaml: gates.l4.model` escape hatch.

## 9. Bootstrap for next session

Replace the PRIORITY block of `docs/engineer/autonomous-session-prompt.md` with the S1 → S2 → S3 ordering from §3. Pre-fetch S2 once S1 drains to ≤2 unblocked items per `memory/feedback_roadmap_pre_fetch`.
