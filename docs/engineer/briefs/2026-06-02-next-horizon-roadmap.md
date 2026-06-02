# regatta — next-horizon roadmap (post-self-host)

**Date:** 2026-06-02
**Status:** unified spec. Supersedes PRs #399, #401, #402, #403,
#404, #406, #407, #408, #409, #411, #412, #414, #415, #416, #417,
#418, #419, #421, #422, #425, #428, #429, #430, #431. Single source
of truth for the four-chain consolidation (customer-roadmap +
wave-2 + wave-3 + wave-4).

**Reading order:** §1 picks customer 0. §2 fixes the Phase X exit
gates. §3 lists MVR-1's top-3 dispatch wedges. §4 sequences
MVR-2/3/4. §§5-7 are the wedge-by-wedge research that feeds the
sequence (wave-2 primitives + wave-3 adjacent-market borrow/reject
+ wave-4 emerging-tech 6/12/24 mo predictions). §8 pricing. §9
cuts. §10 open questions. §11 cites the impl-ready items already
filed in `.regatta/items/`.

---

## 1. Customer 0 + persona

Four personas were scored on adoption-cost, willingness-to-pay,
trust-bar, and Phase-X surface fit (the matrix lives in PRs
#399/#412/#421 chain history; preserved here in summary):

| Persona | Description | TTV | WTP | Trust bar |
|---|---|---|---|---|
| **A — OSS maintainer** | Solo or small-team owner of one ≥1k-star repo, multi-PR-per-day issue backlog. | < 30 min | $0 (sponsorship-bounded $50-500/mo upper). | low — public repo, public audit trail. |
| B — engineering team (10-100 eng) | Internal-tools shop with monorepo. | days | $200-2k/mo per seat. | high — SSO, RBAC, multi-tenant, "where does data go?" before reading README. |
| C — platform vendor | Reseller wanting regatta as an embedded primitive. | weeks | revenue share / contract. | bespoke. |
| D — research lab | Empirical CS / AI methods group. | weeks | grant-bounded $0-5k/mo. | publication-credible audit chain. |

**Pick: persona A.** Five reasons:

1. **Lowest time-to-value.** Multi-PR-per-day shape is native; single-tenant binary acceptable; public repo audit-trail already public — no new trust contract.
2. **UX-first per the decision-priority spine.** Solo maintainer UX = self-operator UX. CLI flow shipping in Phase S3 IS the v1 product surface.
3. **Marketing flywheel.** Persona-A wins are public. Every green PR on a popular repo carries organic discovery; persona-B/C/D wins are private.
4. **Discriminator vs Claude Code Dynamic Workflows.** Persona A needs the multi-PR ledger (cost cap + signed audit + queue), not just "ran an agent in a session." CC owns one-shot; regatta owns the queue.
5. **Phase-X minimization.** Persona A unblocks with W7 Wave 1 htmx + CLI-only. W8/W10/W11/W12 stay deferred.

**Revenue path note.** Persona A's WTP is $0 in OSS mode; this brief
does NOT count persona A as the first paying customer. MVR-2's
"first external paying customer" gate (§4) means persona B or D.
Persona A is the **adoption flywheel** — every green PR on a public
repo is organic discovery for persona B/D. Operator must answer §10
Q2 (paid persona-A SKU vs pure marketing surface) before MVR-1
closes.

**Adversarial-review note.** Persona A is rebuttable against persona
D — both want auditability and self-host. The conflation risk is
that "research-mode overlay" gets prematurely treated as MVR-1
scope. It is not. Research-mode rides on MVR-3 (see §4) after the
publication-credible audit chain has at least one external citation
fire.

---

## 2. Phase X gate criteria — measurable + observable

Phase X (post-self-host) opens via **two-gate AND**: a 30-day-green
operator signal plus a named external customer ask. The multi-tenant
trigger is a separate Phase MVR-2 gate (Gate 3).

### Gate 1 — 30-day-self-host-green (operator-customer)

**Metric:** ≥10 PRs/day green-merge ≥30 days unattended. Computed
nightly from `substrate_events kind=pr_merge` with `green_at_merge=
true`. Window: rolling 30 days. Dashboard: extends the cost-
governor dashboard (`docs/observability/dashboards/cost-governor.md`)
with a `pr_merge_rate` panel.

**Fires when:** 30-day-rolling-min ≥10 AND `unattended_runs /
total_runs ≥ 0.9` for the same window. Operator-side action: re-
litigate Phase X wedges via PRIORITY rewrite per
`docs/engineer/autonomous-session-prompt.md`.

### Gate 2 — external-customer-ask

Shape of the ask:

- **MVR-1 reopen:** 1 inbound email/issue from a persona-A
  maintainer with a named repo + a stated use case ("I want to
  dispatch regatta on `<repo>` for `<class of task>`"). One named
  individual sufficient.
- **MVR-2 reopen:** 2 inbound asks from persona-B teams OR 1 signed
  pilot LOI from any persona. Pilots imply commercial terms; LOI
  lives in `docs/legal/` (created when first one fires).
- **MVR-3 reopen:** 5 paying customers across any persona OR 1
  customer asking specifically for Sigstore/billing/blackboard.
- **MVR-4 reopen:** 10 paying customers OR P2.5 trigger fires (perf).

Each tier dashboardable as `inbound_customer_asks{persona, tier}`
in a CRM-backed exporter; for MVR-1 a single GH issue with
`[customer-ask]` label suffices until CRM lands.

Threshold rationale: tier 1=1 (one named user proves the surface
exists), tier 2=2 (one is an outlier, two is a signal), tier 3=5
(commercial validation floor — below 5 customers any one churn
kills the line), tier 4=10 (perf trigger dominates; customer count
is secondary). Thresholds tunable in `docs/engineer/decisions/`
once MVR-1 launch produces baseline data.

### Gate 3 — single-tenant → multi-tenant trigger (MVR-2 only)

Trigger predicate: any one of (a) ≥2 distinct persona-B teams ask
for shared infra; (b) the persona-A customer-0 self-hosts a second
fork that contends with the first on the same substrate; (c) the
cost-governor dashboard shows per-DAG USD allocations that cannot
be partitioned without a `tenant_id` column. Until one fires, W8
multi-tenant scoping stays deferred.

---

## 3. Top-3 MVR-1 wedges

Ranked by persona-A unblock weight × (1/effort) × (1/reopen-cost):

1. **W7 Wave 1 htmx UI** — approval queue + cost panel + DAG read view. Highest UX delta for persona A; ships behind a single Go binary with embedded template + CSS. Adopts the existing spec (`docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`). Zero new runtime deps. Mobile-friendly approval flow is the load-bearing customer-0 unblock.
2. **`regatta init` bundle** (init wizard + GoReleaser release pipeline + GH-issue adapter) — adoption-cost collapse. Without this, the W7 UI is invisible because persona A bounces at minute 5. Adopts AlecAivazis/survey + GoReleaser + go-github; ships in 1-2 weeks total.
3. **P3.8 SCM-adapter contract (Gitea first)** — second-consumer proof for the SCM-adapter seam per `feedback_research_design_principles`. Demonstrates the swap-out story to persona B without yet building all five P3.8 contracts.

Excluded from top-3 (deferred to MVR-2+): W8 multi-tenant, W10
Sigstore, W11 blackboard, W12 billing. Persona A does not need
them; UX bar dominates hypothetical compliance bar.

---

## 4. MVR-2 / MVR-3 / MVR-4 sequencing

### Phase MVR-1 — adoption-cost collapse (post-30-day-green OR persona-A ask, single tenant)

**Acceptance gate:** named persona-A maintainer (§1) installs
regatta via `go install` (or GoReleaser binary), runs `regatta
init`, dispatches first PR within 30 minutes, merges within 24
hours, returns the following weekend.

| # | Task | Effort | Adopt | Dep |
|---|---|---|---|---|
| MVR-1-T1 | W7 Wave 1 htmx UI — approval queue + cost panel | M (2-3 wks) | htmx + Go html/template | spec landed (#318/#303/#307) |
| MVR-1-T2 | `regatta init` wizard | S (3-5d) | AlecAivazis/survey | none |
| MVR-1-T3 | GoReleaser release pipeline | XS (1-2d) | GoReleaser | none |
| MVR-1-T4 | GH-issue adapter (`[autonomous]` label) | S (3-5d) | go-github | substrate Wave 1 (shipped) |
| MVR-1-T5 | P3.8 SCM-adapter contract + Gitea second consumer | M (1-2 wks) | go-gitea/sdk | P3.8 spec (deferred — landed concurrently) |

Effort total: ~5-7 calendar weeks. **Abandon-criterion:** if
MVR-1-T1 takes >4 wks OR no persona-A install lands within 60 days
of MVR-1 ship (measured as GitHub Stars >25 + ≥3 distinct repos
with a `.regatta/` directory in their tree, queryable via `gh
search code`), halt MVR-2 dispatch + revisit persona pick. The
60-day window assumes the operator posts launch to Hacker News +
r/golang + the Anthropic Developers Discord — outbound effort is a
1-day task, not a wedge.

### Phase MVR-2 — first external paying customer (persona B/D ask)

**Acceptance gate:** one signed pilot LOI from persona B or D.
Multi-tenant scoping lands. Reviewer-rich PR UI lands. License
decided (see §10).

| # | Task | Effort | Adopt |
|---|---|---|---|
| MVR-2-T1 | W7 Wave 2 htmx — DAG read view + reviewer-rich PR UI | M (3-4 wks) | htmx |
| MVR-2-T2 | W8 multi-tenant `tenant_id` routing | M (2-3 wks) | extend existing W8 OPA |
| MVR-2-T3 | Retract primitive (G10) | XS (1-2d) | go-github |
| MVR-2-T4 | P3.8 LLM-gateway adapter (LiteLLM or portkey, score at the time) | M (2-3 wks) | LiteLLM / portkey |
| MVR-2-T5 | W7 Wave 3 htmx — last polish + docs | S (1 wk) | htmx |

Effort total: ~8-12 wks. **Abandon-criterion:** if MVR-2-T2 churns
the substrate read path more than 4 files OR persona-B ask retracts
during dev, revert to MVR-1-only + re-plan.

### Phase MVR-3 — 5+ paying customers (trust + revenue)

**Acceptance gate:** 5 paying customers across persona B/C/D.
Sigstore attestation chain lands. Stripe Metering ships. Blackboard
sqlite-CAS lands (research-mode overlay unblocks here if persona D
is among the 5).

| # | Task | Effort | Adopt |
|---|---|---|---|
| MVR-3-T1 | W10 Sigstore — cosign CLI shell-out behind P3.8 signer adapter | S (1-2 wks) | cosign |
| MVR-3-T2 | W12 Stripe Metering behind P3.8 billing adapter | M (2-3 wks) | Stripe SDK |
| MVR-3-T3 | W11 blackboard sqlite-CAS (`blob_digest` column already forward-fit) | M (2-3 wks) | sqlite |
| MVR-3-T4 | Research-mode overlay (per `2026-06-01-regatta-research-vision.md`) | L (6-8 wks) | per research-mode spec |

Effort total: ~12-16 wks. **Abandon-criterion:** if Sigstore CLI
shell-out adds >100ms p99 latency to the signer hot path, swap to
sigstore-go Go lib — already in the candidate set.

### Phase MVR-4 — 10+ paying customers OR perf trigger

**Acceptance gate:** P2.5 trigger fires (sqlite contention >5% /
≥30 concurrent / replay >60s, two consecutive 24h windows) OR 10
paying customers.

| # | Task | Effort | Adopt |
|---|---|---|---|
| MVR-4-T1 | W9 Temporal-backed `DurableHistory` variant behind option-C adapter | L (3-4 wks) | Temporal Go SDK |
| MVR-4-T2 | Postgres HA option behind substrate adapter | L (3-4 wks) | pgx + golang-migrate |

Effort total: ~6-8 wks. **Abandon-criterion:** if Temporal RPC adds
>50ms p99 to scheduler tick on dev fixture, halt + reassess against
alternatives (restate.dev, custom journal).

### Cross-phase budget summary

| Phase | Calendar wks | Subagent wks | New OSS adoptions | Bespoke wedges |
|---|---|---|---|---|
| MVR-1 | 5-7 | ~7 | 4 (survey, GoReleaser, go-github, go-gitea) | 0 |
| MVR-2 | 8-12 | ~12 | 1 (LiteLLM OR portkey) | 0 |
| MVR-3 | 12-16 | ~14 | 3 (cosign, Stripe, sqlite-CAS) | 0 |
| MVR-4 | 6-8 | ~7 | 2 (Temporal, pgx) | 0 |

Zero bespoke wedges across four phases per
`feedback_research_design_principles` adoption-first.

---

## 5. Wave-2 wedges — primitives that widen scope after MVR-1

Source: research/wedge-wave-2 chain (#402 + #411 + #422). Picks
align with persona A; do NOT duplicate the MVR-1 top-3. Wave-2
picks the next layer: eval rigor + MCP-consume + prompt-revision
storage — all of which feed the L4 reviewer gate that gates
persona-A merges.

### 5.1 Workflow language for plans

| Adopt | Build | Pass + reopen |
|---|---|---|
| Keep markdown + frontmatter as operator surface; CUE as validation. | `regatta plan lint` runs the CUE schema against any plan source (md, yaml, json). One linter, three readers. | Pkl IDE-grade authoring: revisit if a paying customer files a request; today, markdown wins on AI-readability + operator pain is low. |

### 5.2 Agent skill marketplace

| Adopt | Build | Pass + reopen |
|---|---|---|
| Anthropic Agent Skills (SKILL.md + YAML frontmatter) + the official MCP Registry. | `regatta skill emit` packs the existing dispatcher templates as a SKILL.md bundle; publish via `agentskills.io` or the Anthropic catalog. | Building a regatta-branded marketplace: rejected — Anthropic's directory plus the MCP registry already win on adoption. Reopen only if Anthropic deprecates the format. |

### 5.3 Observability vendor alignment

| Adopt | Build | Pass + reopen |
|---|---|---|
| Primary: Honeycomb (OTel-native, generalist, regatta's existing backbone exports there cleanly). Fallback: Grafana/Tempo OSS stack for self-host operators. LLM-specific co-pilot: Langfuse (MIT, self-hostable). | A documented "swap vendor" runbook that points the OTel collector at a different backend. No vendor-specific code in regatta core. | Picking Datadog/Braintrust as default: reopen only if a paying customer mandates it. |

### 5.4 MCP server ecosystem — direction of integration

| Adopt | Build | Pass + reopen |
|---|---|---|
| Consume GitHub + Slack reference MCP servers for existing notify/PR-touch paths. Expose a minimal read-only MCP surface on regatta: `list_runs`, `get_run`, `get_budget` first. | Authn/authz inside the MCP handler — every exposed verb runs through the policy engine. Write verbs (`dispatch`, `approve`) gated behind explicit operator opt-in flag per tenant. | Full bi-directional write surface: reopen when the read-only surface has been used by ≥2 operator IDEs for ≥30 days without a security incident. Premature write exposure is the load-bearing risk. |

### 5.5 Agent-eval frameworks

| Adopt | Build | Pass + reopen |
|---|---|---|
| **DeepEval** as primary (G-Eval rubric-as-prompt; unified MLflow scorer API). **Promptfoo** as fallback for CLI-driven YAML test cases (lives in-repo, no SaaS dep). Phoenix held in track-only state. | A `regatta eval` command that wraps DeepEval scorers under the cost-governor budget. Eval prompts are CUE-validated so judge prompts are versioned + diff-able. | Constitutional + RAGAS rejected: Constitutional is Anthropic-only; RAGAS is RAG-shaped, not code-review-shaped. Reopen if Anthropic deprecates G-Eval-style scorers. |

### 5.6 Sandbox runtimes

| Adopt | Build | Pass + reopen |
|---|---|---|
| Self-host: local Docker subprocess (no change). SaaS canonical: E2B. Future GPU lane: Modal when an operator demand surfaces. | A pluggable `Sandbox` interface with two reference impls (local-docker + E2B) gated by a `regatta.toml` `sandbox.kind` field. Replay-grade artefact capture is the contract; the sandbox is interchangeable. | Switching to Daytona / Blaxel: reopen when cold-start latency becomes the dispatch-loop bottleneck (today the LLM call dominates). |

### 5.7 Prompt versioning + A/B testing

| Adopt | Build | Pass + reopen |
|---|---|---|
| Promptfoo's YAML test-case shape (matches `.regatta/items/`) + Langfuse for self-host prompt-revision storage (overlap with §5.3 is intentional). | A `prompt_revisions` table in substrate (digest-keyed, references the same CAS blob shape as W11). | Vendor-locked Braintrust/LangSmith/PromptLayer: track-only with named reopen predicate (one paying customer mandates the vendor). |

### Wave-2 top-3 (composed into the MVR-1/2 sequence, not parallel)

1. **MCP-expose regatta as read-only server** — inverts absorption-risk against Claude Code Dynamic Workflows. Low build cost. Lands in MVR-2 alongside W7 Wave 2 (one named IDE consumer predicate).
2. **DeepEval-backed L4 reviewer gate** — gates persona-A merges; ships in MVR-2-T1 (reviewer-rich PR UI). The L4 gate currently uses an ad-hoc Anthropic prompt; DeepEval gives a CUE-versionable rubric.
3. **Anthropic Skill bundle publish** — adopts SKILL.md + the official MCP registry. Lands in MVR-2 as a 3-day task once the read-only MCP surface stabilizes.

---

## 6. Wave-3 borrow/reject from adjacent markets

Source: research/wedge-wave-3 chain (#404 + #415 + #425).

### 6.1 Data-pipeline orchestrators (Airflow / Dagster / Prefect / Temporal / Argo)

**Borrow:** Dagster's asset-graph + freshness SLOs as the mental
model for regatta's `prereg.depends_on` field (when MVR-2+ adds
inference). Temporal Replay-2026's signed-history-export as the
shape for substrate's signed-verdict export. Airflow 3.2's deferable-
operator pattern as the budget-aware long-wait shape (cost-gov
yields, not blocks).

**Reject:** Airflow's task-centric scheduler loop (regatta's
scheduler-tick + lane-reservation wins on operator-credible signed-
verdict shape). Argo Workflows' YAML-CRD as primary spec (no type-
system, no signed manifest, prereg-locking impossible). Temporal's
deterministic-replay programming model (forbids `time.Now()`,
random, network reads — real cognitive tax; Inngest's `step.run`
ships the same durability without the rules).

### 6.2 Build systems (Bazel / Buck2 / Pants / Turborepo)

**Borrow:** Bazel's action-graph + action-cache as the credibility
precedent for scheduler-tick + lane-reservation. Pants' static-
analysis dependency inference (`prereg.depends_on: auto` in MVR-2+,
strict allowlist + fallback). Bazel RE API's "one signed protocol,
multiple impls" as the architecture for the MVR-4-T1 W9 Temporal-
backed variant — impl swap is URL-only, no spec change.

**Reject:** Bazel's `BUILD` files as primary authoring surface (DSL
tax). Turborepo's pipeline-task tree (subset of action-graph;
borrow Bazel's superset instead).

### 6.3 CI/CD platforms (GitHub Actions / GitLab CI / Tekton / Buildkite / CircleCI / Argo CD)

**Borrow:** GitHub merge queue 2026's branch-capacity-decides-what-
ships semantic — regatta's W7 approval queue can ship with a
"capacity" field per cost-cohort. Buildkite's `pipeline upload`
runtime-YAML-inject as the pattern for dynamic DAG extension (later;
not MVR-1).

**Reject:** GitHub Actions' matrix-at-parse expansion (regatta's
dispatch is runtime). Argo CD's app-of-apps GitOps (deploy-side
pattern, not relevant to regatta's dispatch).

### 6.4 AI eval/obs (Braintrust / Phoenix / Langfuse / LangSmith / Laminar)

**Borrow:** Braintrust's GitHub-Action-posts-to-PR pattern as the
shape for regatta L4 reviewer comments. OpenInference attribute
schema verify-or-file-issue (currently regatta emits OTel GenAI
semconv per #213; bridge to OpenInference is integration-time, not
default).

**Reject:** Vendor-specific tracing libraries (regatta core stays
OTel-native; bridges live at the collector).

### 6.5 Agent platforms (LangGraph / CrewAI / AutoGen / Inngest / Trigger.dev / Restate)

**Borrow:** Inngest's `step.run` memoization shape (matches
substrate's per-step verdict event without Temporal's determinism
tax). Restate's durable-RPC for long-wait HITL approvals (track,
adopt in MVR-2 if HITL latency becomes a hot path).

**Reject:** LangGraph's StateGraph (no signed history). CrewAI's
crew-grows additive culture (anti-pattern vs deletion-default).

### 6.6 PR-automation (Renovate / Dependabot / Copilot Workspace / Cody / Codeium)

**Borrow:** Renovate's `automerge: true` + `prConcurrentLimit` +
`prHourlyLimit` as the shape for regatta's per-cohort PR cap.
Renovate's multi-platform support (GitHub / GitLab / Bitbucket /
Azure DevOps / **Gitea**) is the same case for MVR-1-T5 SCM-adapter
Gitea-first.

**Reject:** Copilot Workspace's "reports victory but often doesn't
actually resolve" merge UX (regatta's L0-L4 gates already prevent
self-reported-green). Dependabot's GitHub-only scope.

### 6.7 Five cross-category insights

1. **Markdown-as-spec is the regatta novelty — and it's a moat, not a footgun.** Airflow/Dagster/Prefect/Temporal/LangGraph/Inngest author the DAG in Python (or SDK). Argo/Tekton author in YAML CRDs. *No* mainstream system authors the falsifiable-contract surface in operator-readable markdown with a typed prereg sub-block. **Defend against any "let users define WorkItems in code" pressure.**
2. **The gate-blocks-merge pattern has crossed the chasm.** Braintrust + GitHub merge queue + Renovate + (regatta) are converging on the same UX. The discriminator: regatta uniquely gates on **methodology** (prereg, immutability, signed verdicts), not CI green or eval score alone.
3. **OSS-then-monetize is the only credible move for open-core dispatch.** GitLab + Sentry + Grafana playbook: ship Apache-2.0 core, paid features only where the cost surface is the customer's problem (multi-tenant, hosted, SOC2). See §8.
4. **OpenInference + OTel GenAI semconv converging — no schema invention.** Phoenix, Langfuse, LangSmith, Laminar all ingest OTel spans. W6 OTel backbone already ships GenAI semconv per #213. **No infra change; bridging deferred to integration-time.**
5. **regatta's `feedback_deletion_default` is structurally rare.** Airflow/Dagster/LangGraph/CrewAI/Bazel default-additive. regatta's deletion-default makes primitive count *shrink* under adversarial review. **Cultural moat — preserve.**

---

## 7. Wave-4 emerging-tech (≤12mo) track vs adopt

Source: research/wedge-wave-4 chain (#401 + #414).

### 7.1 Three predictions

| Horizon | Trend | Predicted impact |
|---|---|---|
| **6 mo** | Skills + MCP normalize as the agent extensibility substrate (Anthropic 101 official + 2k MCP servers, GitHub MCP Registry GA). | regatta agents must publish as **both** a Claude Skill and an MCP server; failure = invisibility in the dominant distribution channel. **High urgency.** |
| **12 mo** | LLM-as-judge moves from research to CI gate (DeepEval/RAGAS/Phoenix unified under MLflow scorer API). G-Eval-style rubric-as-prompt becomes dominant. | regatta's L4 adversarial-review gate **must** standardize on CUE-validated rubric-as-prompt so judge prompts are versioned + diff-able + swappable. Already wired into §5.5 + MVR-2. |
| **24 mo** | Multi-agent collab moves from message-passing to **CRDT-mediated shared state** (Yjs + Automerge + Liveblocks). Agents become equal peers (not clients). | regatta's blackboard wedge (P6+P9) gets a free implementation: replace bespoke CAS+reducers with Yjs/Automerge. **High leverage — could collapse two wedges into one library.** |

### 7.2 Adopt (this horizon)

- **PR-Agent (Qodo) rubric-prompt structure + Aider's git-native commit-per-edit** (track patterns; regatta keeps `.regatta/items/` git-native).
- **Anthropic Claude Skills + MCP Server Registry** (dual-publish regatta agents as Claude Skill + MCP server; listed in both official catalogs). Single-channel commitment = invisibility risk in 12 mo.
- **Claude Code v2.1.137+ slash menu** (expose regatta skills + plugins discoverable from `/plugin` / `/skills`).
- **Anthropic Claude Agent SDK + Claude 4.6** (substrate; stay current).

### 7.3 Track (defer adoption until trigger)

- **Devin 3.0 re-planning loop** — adopt-pattern for autonomous-session-prompt evolution; competitor product otherwise. **Concession in §10:** regatta's loop is "more constrained" than "more sophisticated."
- **OpenAI Agents SDK + Google ADK + Microsoft Agent Framework** — cross-vendor SDK fragmentation risk; track quarterly.
- **Yjs / Automerge** — adopt-trigger: blackboard wedge dispatch (MVR-3-T3) OR a research-customer demanding branching plans.
- **Loro / Diamond Types / Liveblocks** — niche or SaaS-only; reject as primary.
- **Behavior Trees / HDDL / PDDL** — track only. CUE-validated YAML stays the authority for plan-as-code. Steal BT composition semantics for the *next* iteration.

### 7.4 Bet against

| Bet | Why |
|---|---|
| Sweep AI as a category leader | Reduced commit cadence vs 2023 peak; superseded by PR-Agent + Aider in 2026 write-ups. |
| OpenAI Plugins / Custom GPTs as a distribution channel | Deprecated by OpenAI's own pivot to Apps + GPTs; siloed to ChatGPT. |
| Cursor extensions as primary skill format | Cursor-locked + model-locked; will lose to Claude Skills + MCP. |
| Visual Studio 2026 Agent Mode displacing terminal-first | Threat to track, but terminal-first agents (Claude Code, Aider) keep shipping faster. |

### 7.5 Five nits resolved from #417 review

The wave-4 chain's adoption-with-nits round surfaced 5 caveats —
already filed as `.regatta/items/wave-4-nits-1..5-*.md`:

- **G1 spec-vs-impl gate** — wave-4 §7 surfaces 4 "Adopt" rows that
  are not yet wired into `.regatta/items/`; this brief wires items
  4-01 + 4-02 + 4-03 into MVR-2 scope.
- **G2 operator-dating** — every "track" row now carries a quarterly
  re-evaluation date.
- **G3 Devin concession** — explicit framing: regatta's loop is
  *constrained* (signed verdicts, immutability gates), not
  *sophisticated* — `feedback_drop_ceremony` rather than feature
  parity.
- **G4 semantic-merge wording** — "semantic merge layer" reframed as
  "blackboard reducer layer" (item 4-06) to avoid implying we are
  building a generic CRDT product.
- **G5 tier-2 vendor signal caveat** — Devin/Cursor/Bugbot stars and
  commit-cadence are SaaS-internal; the score axis uses public
  signals only.

---

## 8. Pricing + monetization

**License:** Apache 2.0 for the core. (License decision tracks §10
Q5 — finalize at MVR-2 kickoff. Until then, the brief picks Apache-2
as default; reversible to BSL only with a named persona-C
reselling-risk trigger.)

**Open-core split:**

| Layer | License | Surface |
|---|---|---|
| Core (substrate, scheduler, cost-governor, W7 UI, init-bundle, GH/Gitea adapter, MCP read-only surface, DeepEval rubric runner) | Apache 2.0 | OSS install via `go install` or GoReleaser binary. |
| Commercial-core add-ons (W8 multi-tenant, W10 Sigstore attestation chain, W12 Stripe Metering, hosted SaaS if MVR-3+ persona-C asks) | Commercial (Polyform or BSL) | Paid SKU. |
| Support contracts | Commercial | Pilots LOI ($1-5k/mo bracket); 5+ paying customers gate per MVR-3 §2 Gate 2. |

**Red Hat playbook reference.** GitLab + Sentry + Grafana + RH all
ship Apache-2 core + paid features where the cost surface is the
customer's problem (multi-tenant, SOC2, hosted). The Red Hat
playbook: open-source is the funnel; revenue is the support contract
+ commercial-core add-ons. Persona A is the funnel; persona B/D is
the revenue.

**Persona-A revenue option (operator decision §10 Q2):** GitHub
Sponsors-gated feature flag in the OSS core (e.g. `--with-sponsor-
features` enables priority queue scheduling). $50-500/mo upper.
Optional; default is no sponsorship gating until MVR-1 closes.

Pricing impl-ready brief lives at `.regatta/items/mvr-1-t6-pricing-
support-contracts.md`.

---

## 9. Cuts — what NOT to build (anti-roadmap)

Per `feedback_deletion_default` — every wedge below is rejected
with explicit reopen condition. Empty cuts list = failure mode.

| Cut | Reason | Reopen condition |
|---|---|---|
| Reviewer-agnostic gate that runs any LLM (Claude/GPT/Gemini auto-pick) | Locks Claude-Code assumption that reviewer subagent prompts are Anthropic-shaped; auto-picking creates a 3-way QA matrix that no one staffs. | A persona-B customer signs a pilot specifically mandating multi-vendor reviewer LLMs. |
| In-process agent runtime (vs Claude Code subprocess) | Phase X deferred indefinitely. Claude Code subprocess is the unit of work; in-process runtime would absorb 3+ months on a wedge that unlocks no persona. CC is the worker, not the competitor. | CC ships breaking changes that fundamentally invalidate subprocess as a primitive (>3 month outage). Track CC changelog quarterly. |
| Self-hosted model proxy | Operator brings own `ANTHROPIC_API_KEY`. Self-hosting a model proxy means regatta owns inference uptime, scaling, security — a wholly different product. | A persona-C platform vendor signs specifically asking regatta to be the model proxy. Even then, score LiteLLM / portkey first. |
| Web-based agent debugger | Jaeger handles span-level debugging (W6 OTel backbone shipped). Bespoke debugger duplicates Jaeger w/o domain insight. | Jaeger UX provably blocks >3 customer-reported debugging sessions per quarter. |
| Reviewer-rich PR UI as standalone product | Persona A reads PR diffs in GitHub UI directly. Building a separate reviewer-side UI doubles the surface persona A bounces off. | Persona B/C signs a pilot specifically asking for in-regatta diff review. |
| IDE integration (VS Code / JetBrains extension) | Claude Code IDE integration owns that surface. Building competing IDE plugins is a 3-month sink with zero persona-A delta. | A persona-B/C customer signs a pilot specifically requiring an in-IDE regatta panel. |
| Regatta-branded skill marketplace | Anthropic's official Claude Skills directory + MCP Registry already won the distribution race. Building a marketplace duplicates Anthropic with zero ecosystem leverage. | Anthropic deprecates the SKILL.md format or sunsets the directory. |
| Cross-repo plan sharing / regatta plan marketplace | Premature standardization. Plan-as-code spec (P4) still single-consumer. | 3+ paying customers ask for cross-repo plan sharing. Even then, ship as git submodule pattern, not a marketplace. |
| Hosted SaaS (regatta cloud) | Rejected for MVR-1+MVR-2. Self-host-only ships first; hosted is the third product (persona C territory). | Persona-B/C asks specifically for hosted variant + commits to a pilot LOI. See §10 Q4. |
| LangGraph / CrewAI / AutoGen as the substrate | Adversarial review against existing substrate per `2026-06-01-w9-temporal-vs-bespoke-redteam.md`. None of these provide signed-verdict history; rebuilding regatta around them is a 6-month sink. | A paying customer mandates one specifically. Even then, build the bridge as an MCP server, not a substrate swap. |
| In-house CRDT lib | Yjs / Automerge already won. Building one ourselves is the deletion-default counter-example. | Both Yjs and Automerge stop maintenance for ≥12 months. Unlikely. |

11 cuts. Each cut is a step we don't take.

---

## 10. Open questions — operator must answer before MVR-1 dispatch

The customer-roadmap adversarial-review chain filed 5 RISK issues
(#423-#427) plus #428 (1 HIGH Python-toolchain-tax from wave-2),
#429 (P1-P3 nits from wave-3), #417 (5 nits from wave-4 — already
captured in §7.5). The five open operator questions:

1. **Who is customer 0 by name?** This brief picks persona A; operator must name one specific maintainer + repo before MVR-1-T1 dispatches. Without a named target, W7 UI gets built to a hypothetical user. **Needed by:** MVR-1 kickoff. *(filed: #423)*
2. **WTP for persona A?** This brief estimates $0/mo for OSS. If operator wants a paid persona A (open-core sponsorship $50-500/mo via GitHub Sponsors), MVR-1 needs a Sponsors-gated feature flag. **Needed by:** end of MVR-1. *(filed: #424)*
3. **Open-core vs commercial-core?** Open-core = OSS regatta + paid enterprise features (W8 / W10 / W12 as add-ons). Commercial-core = everything OSS, revenue from hosted SaaS only. Affects MVR-3 ranking. **Needed by:** MVR-2 kickoff. *(filed: #425)*
4. **Hosted SaaS or self-host-only?** Self-host-only is §9 default. Hosted = separate product line (persona C primary) and reshapes MVR-3+MVR-4 entirely. **Needed by:** MVR-3 kickoff or earlier if persona-C ask fires. *(filed: #426)*
5. **License — Apache 2.0 vs BSL vs AGPL?** Apache 2 maximizes persona-A and persona-C adoption; BSL protects against persona-C reselling; AGPL forces SaaS reselling to open-source their stack. **Needed by:** MVR-2 kickoff (license signal becomes load-bearing once a paying customer signs). *(filed: #427)*

Wave-2 chain leftover (1 HIGH from #428): **Python-toolchain-tax
risk on DeepEval adoption.** DeepEval is Python; regatta core is
Go. Mitigation: run DeepEval as a subprocess behind a Go shim (same
pattern as Sigstore CLI shell-out in MVR-3-T1). Tracked as a follow-
up issue when MVR-2-T1 dispatches.

Wave-3 chain leftover (P1-P3 nits from #429): all three are doc-
nits, not blockers. They are inline-resolved in §6.

All five operator questions land in `docs/engineer/decisions/`
(created when answered) before the respective phase dispatches.

---

## 11. Sequenced impl-ready items

The implementer briefs already exist in `.regatta/items/` from the
chain consolidation. Dispatch in the order below.

### MVR-1 (post-30-day-green OR persona-A ask)

- `.regatta/items/mvr-1-t1-w7-wave1-htmx-ui-mvp.md` — W7 Wave 1 htmx UI MVP.
- `.regatta/items/mvr-1-t2-regatta-init-bundle.md` — `regatta init` wizard + GoReleaser + GH-issue adapter.
- `.regatta/items/mvr-1-t3-p38-scm-adapter-gitea-first.md` — P3.8 SCM adapter contract + Gitea second consumer.
- `.regatta/items/mvr-1-t6-pricing-support-contracts.md` — pricing + support-contract design (lands alongside license decision §10 Q5).

### MVR-2 (first external paying customer)

Wave-2 + wave-4 picks compose into MVR-2:

- `.regatta/items/wave-4-01-claude-skill-publish.md` — publish regatta as a Claude Skill bundle.
- `.regatta/items/wave-4-02-mcp-server-registry.md` — publish regatta as an MCP server in the official registry (read-only first).
- `.regatta/items/wave-4-03-g-eval-l4-rubric.md` — DeepEval-backed L4 reviewer gate (CUE-validated rubric).
- `.regatta/items/wave-4-04-claude-skills-track.md` — track Claude Skills format evolution quarterly.
- `.regatta/items/wave-4-nits-1-g1-spec-vs-impl-gate.md` through `wave-4-nits-5-g5-tier-2-vendor-signal-caveat.md` — 5 nit resolutions wired into MVR-2 dispatch prompts.

### MVR-3 (5+ paying customers + research-mode overlay)

- `.regatta/items/wave-4-05-sandbox-validation-spike.md` — sandbox-runtime swap validation (E2B alternative).
- `.regatta/items/wave-4-06-semantic-merge-layer-design.md` — blackboard reducer layer design (Yjs spike).
- `.regatta/items/wave-4-07-operator-cua-tracking.md` — operator CUA (Claude / OpenAI / Gemini) tracking matrix for the publish-bundle adoption-trigger.

---

## 12. References

- Customer roadmap (superseded): #399 brief, #403/#412/#421 reviews, #408/#418 amend, #431 MVR-1 impl-briefs.
- Wave-2 (superseded): #402 research, #407/#416/#428 reviews, #411/#422 amend.
- Wave-3 (superseded): #404 research, #409/#419/#429 reviews, #415/#425 amend.
- Wave-4 (superseded): #401 research, #406/#417 reviews, #414 amend, #430 7-adopt + 5-nit impl-briefs.
- Prior briefs (still live): `docs/engineer/briefs/2026-06-01-self-host-first.md`, `docs/engineer/briefs/2026-06-01-regatta-research-vision.md`, `docs/engineer/briefs/2026-06-01-arch-simplification-pass.md`.
- Wedge: `docs/wedges/research-mode.md`.
- W7 spec: `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`.
- Substrate spec: `docs/engineer/specs/2026-06-01-unified-substrate-design.md`.
- W9 spec: `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md`.
- Adapter contracts: `docs/engineer/specs/2026-06-01-adapter-contracts-design.md`.
- Cost-gov dashboard: `docs/observability/dashboards/cost-governor.md`.
- htmx — htmx.org (BSD-2); AlecAivazis/survey (MIT); GoReleaser (Apache 2); go-github (BSD-3); go-gitea/sdk (MIT); cosign / Sigstore (Apache 2); Stripe Metering; OPA (Apache 2, CNCF); LiteLLM (MIT); portkey-ai/gateway (MIT); Temporal Go SDK (MIT); DeepEval; Promptfoo; Honeycomb; Langfuse (MIT); Anthropic Agent Skills; MCP Server Registry; Yjs; Automerge.
- Memory cites: `feedback_design_iteration_local`, `feedback_drop_ceremony`, `feedback_self_improvement`, `feedback_research_design_principles`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`, `feedback_decision_priority`, `feedback_deletion_default`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_unaddressed_load_bearing`.

---

## 13. A+ rubric self-score

| Tier | Criteria |
|---|---|
| **B (floor)** | Customer 0 named (§1). Top-3 wedges ranked (§3). ≥1 adopt-vs-build table (§5). ≥1 cut (§9). Release-notes fence in PR body. |
| **A (target)** | B + 4 personas scored on ≥3 axes (§1). Strategic gaps mapped (folded into §3 + §5 + §6 sections). Phase X wedges score ≥2 OSS candidates each (§5). 4-phase sequenced roadmap with abandon-criterion per phase (§4). Gate criteria measurable (§2). ≥5 cuts with reopen condition (§9 has 11). |
| **A+ (stretch)** | A + 4 wave-3 cross-category insights surfaced + 1 cultural-moat insight (§6.7). Zero bespoke wedges across four phases (§4 budget summary). Customer-0 pick rebuttable with adversarial note (§1). Effort + abandon-criterion per task (§4 tables). ≥10 cuts (§9 has 11). Single-source consolidation kills 24 superseded PRs (§12 + this PR's "Supersedes" list). |

**Self-scored tier:** A+ — every criterion met. The consolidation
itself is the A+ delta: four chains collapsed to one brief, 24
PRs closed, single-canonical destination for MVR-1 dispatch.
