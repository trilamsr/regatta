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
filed in `.regatta/items/`. §12 references. §13 self-scores against
the B/A/A+ rubric.

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

Numbering note: §3's three top-3 wedges expand into the five MVR-1
table rows here — rank-1 (W7 UI) → T1, rank-2 (init bundle) → T2/T3/T4
(wizard + GoReleaser + GH-issue adapter, dispatched as one program),
rank-3 (SCM adapter) → T5. The impl-ready item filenames in `.regatta/
items/` preserve the rank-3 nomenclature (`mvr-1-t3-p38-scm-adapter-*`).

**Honest week estimate: 6 calendar weeks** (range 5-7).
Composition: T1 2-3 wks + T2/T3/T4 dispatched as one program 2 wks
+ T5 1-2 wks. Basis: per-task adopt-row in `.regatta/items/mvr-1-
t1-w7-wave1-htmx-ui-mvp.md`, `mvr-1-t2-regatta-init-bundle.md`,
`mvr-1-t3-p38-scm-adapter-gitea-first.md`. Subagent-week vs
calendar-week diverges because T2/T3/T4 fan out under one
dispatch.

**Abandon-criterion:** if MVR-1-T1 takes >4 wks OR no persona-A
install lands within 60 days of MVR-1 ship (measured as GitHub
Stars >25 + ≥3 distinct repos with a `.regatta/` directory in
their tree, queryable via `gh search code`), halt MVR-2 dispatch +
revisit persona pick. The 60-day window assumes the operator posts
launch to Hacker News + r/golang + the Anthropic Developers
Discord — outbound effort is a 1-day task, not a wedge.

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

**Honest week estimate: 12 calendar weeks** (range 8-12). Basis:
T1 3-4 wks + T2 2-3 wks + T3 ≤1 wk + T4 2-3 wks + T5 ~1 wk.
Per-task estimates cite the wave-2 + wave-4 impl-ready items
(`.regatta/items/wave-4-01-claude-skill-publish.md`,
`wave-4-02-mcp-server-registry.md`, `wave-4-03-g-eval-l4-
rubric.md`) plus W8 multi-tenant scoping per OPA extension already
in-tree.

**Abandon-criterion (load-bearing):** **MVR-2 abandons if no first
paid customer (persona B or D pilot LOI signed) by week 18 of the
MVR-2 window** — that is, 6 weeks past the 12-week effort
estimate. Sub-criteria: if MVR-2-T2 churns the substrate read path
more than 4 files OR persona-B ask retracts during dev, revert to
MVR-1-only + re-plan. The first-paid-customer gate matches §2 Gate
2 tier 2 (one signed LOI suffices); without it MVR-2 is building
to a hypothetical buyer.

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

**Honest week estimate: 16 calendar weeks** (range 12-16). Basis:
T1 1-2 wks + T2 2-3 wks + T3 2-3 wks + T4 6-8 wks (research-mode
overlay is the load-bearing line item). Cite: `docs/wedges/
research-mode.md` + `2026-06-01-regatta-research-vision.md`.

**Abandon-criterion:** **MVR-3 abandons if fewer than 5 paying
customers across persona B/C/D by week 24 of the MVR-3 window**
(matches §2 Gate 2 tier 3 floor). Sub-criteria: if Sigstore CLI
shell-out adds >100ms p99 latency to the signer hot path, swap to
sigstore-go Go lib — already in the candidate set. Research-mode
overlay (T4) gets its own abandon trigger: if no persona-D citation
in 90 days post-launch, demote T4 to track-only.

### Phase MVR-4 — 10+ paying customers OR perf trigger

**Acceptance gate:** P2.5 trigger fires (sqlite contention >5% /
≥30 concurrent / replay >60s, two consecutive 24h windows) OR 10
paying customers.

| # | Task | Effort | Adopt |
|---|---|---|---|
| MVR-4-T1 | W9 Temporal-backed `DurableHistory` variant behind option-C adapter | L (3-4 wks) | Temporal Go SDK |
| MVR-4-T2 | Postgres HA option behind substrate adapter | L (3-4 wks) | pgx + golang-migrate |

**Honest week estimate: 8 calendar weeks** (range 6-8). Basis: T1
3-4 wks + T2 3-4 wks, dispatched in parallel where possible. Cite:
`docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md`.

**Abandon-criterion:** **MVR-4 abandons if neither P2.5 perf
trigger fires nor 10 paying customers materialize by week 16 of the
MVR-4 window.** Sub-criteria: if Temporal RPC adds >50ms p99 to
scheduler tick on dev fixture, halt + reassess against alternatives
(restate.dev, custom journal).

### Cross-phase budget summary

| Phase | Honest wks | Range | Subagent wks | New OSS adoptions | Bespoke wedges |
|---|---|---|---|---|---|
| MVR-1 | 6 | 5-7 | ~7 | 4 (survey, GoReleaser, go-github, go-gitea) | 0 |
| MVR-2 | 12 | 8-12 | ~12 | 1 (LiteLLM OR portkey) | 0 |
| MVR-3 | 16 | 12-16 | ~14 | 3 (cosign, Stripe, sqlite-CAS) | 0 |
| MVR-4 | 8 | 6-8 | ~7 | 2 (Temporal, pgx) | 0 |

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

**License:** Apache 2.0 for the core (provisional pending §10 Q5
final decision at MVR-2 kickoff).

Counter-licenses considered:

- **BSL (Business Source License).** Trigger to switch: a named
  persona-C reseller appears with no contracting interest. BSL caps
  the reselling vector but costs persona-A adoption (BSL is not
  OSI-approved; many corporate procurement filters reject it). Net:
  pay a measurable adoption tax to defend against a hypothetical
  reselling vector. Defer until trigger fires. (Elastic / Sentry /
  HashiCorp Terraform precedent.)
- **AGPL.** Trigger to switch: a hosted-SaaS competitor appears with
  no contracting interest. AGPL forces re-distribution of any
  hosted-fork's modifications. Costs persona-B adoption (corporate
  legal teams reflexively reject AGPL even for non-network-served
  use). Defer until trigger fires. (MongoDB pre-SSPL / Grafana
  precedent.)
- **Source-available only (no OSI license).** Considered + rejected:
  collapses persona-A funnel entirely; the §6.7 cultural moat
  insight #3 ("OSS-then-monetize is the only credible move for
  open-core dispatch") depends on a true-OSI license at the core
  layer.

Apache 2.0 is the maximalist-adoption pick at the core. Commercial-
core add-ons (W8 / W10 / W12) live under a separate restrictive
license (Polyform or BSL) — that is where reselling-risk gets
fenced, NOT at the core.

**Open-core split:**

| Layer | License | Surface |
|---|---|---|
| Core (substrate, scheduler, cost-governor, W7 UI, init-bundle, GH/Gitea adapter, MCP read-only surface, DeepEval rubric runner) | Apache 2.0 | OSS install via `go install` or GoReleaser binary. |
| Commercial-core add-ons (W8 multi-tenant, W10 Sigstore attestation chain, W12 Stripe Metering, hosted SaaS if MVR-3+ persona-C asks) | Commercial (Polyform or BSL) | Paid SKU. |
| Support contracts | Commercial | Pilots LOI ($1-5k/mo bracket); 5+ paying customers gate per MVR-3 §2 Gate 2. |

**Commercial-core feature list — what is paid.** Adoption-first
(`feedback_research_design_principles`): a feature is paid only if
the *cost surface is the customer's problem*, not the operator's.
Seven candidates evaluated; four paid + three OSS.

| Feature | Tier | Justification | Reopen if |
|---|---|---|---|
| **W8 multi-tenant scoping** (org/tenant isolation in scheduler + blackboard + cost-governor) | Paid (commercial-core) | Multi-tenant = the persona-B/D ask. Single-tenant operators (persona A) never need it; carrying the isolation tax on the OSS path penalizes the funnel. GitLab EE precedent (group-level isolation = paid). | A persona-A user needs tenant scoping for personal-fork separation (unlikely — git submodules cover this). |
| **SSO / SAML / SCIM** (auth bridge for corp IdPs) | Paid (commercial-core) | Procurement gate for persona-B/D. Solo persona-A users sign in via GitHub OAuth (free). The SSO tax pattern is the canonical open-core paywall — GitLab, Sentry, Grafana, Tailscale all gate SSO at the paid tier. `sso.tax` exists as a meme for a reason; we accept the meme to fund the funnel. | A persona-A inbound asks for GitHub SSO Enterprise specifically (still rare). |
| **Audit-log SaaS** (immutable, queryable, retained event stream w/ export to S3/Splunk/Datadog) | Paid (commercial-core) | SOC2 + ISO27001 compliance evidence is a persona-B/D procurement line-item. Self-host operators get the raw event stream OSS; SaaS-retention + query UI + connectors is the paid layer. Sentry's audit-log gating is the precedent. | Persona-A users need >30-day retention locally (covered by sqlite OSS + `regatta audit export`). |
| **W12 billing reconciler / Stripe Metering integration** | Paid (commercial-core) | Customer-facing usage-billing is a persona-C reseller need (charge their downstream users). Persona-A/B run on `ANTHROPIC_API_KEY` directly + see Anthropic's own bill — no reconciler need. Stripe Metering integration costs are entirely on the reseller's revenue path. | A persona-B asks for internal-cost chargeback (deferred; W3 cost-governor OSS covers visibility). |
| **W10 Sigstore artifact attestation chain** | **OSS (core)** | Supply-chain provenance benefits *everyone* — making it paid would chill the funnel + cede ground to Tekton Chains / SLSA-default tooling. Sigstore itself is OSI; gating verification creates the exact "open-core wrapping OSI" anti-pattern that killed Chef / Puppet adoption. Cost surface is the operator's (CI signing keys), not regatta's. | Persona-C reseller asks for *multi-tenant* attestation aggregation (then W10-multi-tenant becomes the paid wrapper, not W10 itself). |
| **Hosted analytics dashboard** | **OSS (core)** | The W7 htmx UI is the wedge; gating analytics behind a paid dashboard splits the persona-A funnel UX. Grafana's lesson: gating dashboards killed enterprise edition until they un-gated the core. Ship a single OSS dashboard; hosted SaaS becomes paid only when hosting (uptime) is the cost surface. | Hosted SaaS ships (§10 Q4) — at that point, the *hosted* dashboard is paid, but self-host stays OSS. |
| **Priority support / SLA** | Paid (support contracts) | Already in the table above ($1-5k/mo). Not a code feature — a contractual response-time guarantee. Sells alongside any commercial-core SKU. | N/A — standard tier. |

Cross-ref: W8 (`docs/engineer/briefs/2026-04-w8-multi-tenant-
scoping.md`), W10 (W10 attestation brief), W12 (`mvr-1-t6-pricing-
support-contracts.md` for SKU price brackets). Tier shifts at MVR-3
kickoff per §10 Q3 operator answer.

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
| Bespoke SBOM generator | `syft` (Anchore, Apache 2) ships an SPDX/CycloneDX-emitting CLI; GoReleaser already wires syft-on-release via the `sboms:` block. Persona-B/D procurement asks for SBOMs read CycloneDX directly — building our own is pure duplication. | syft drops Go module support OR a customer demands a non-CycloneDX format syft does not emit. |
| Bespoke license-compliance scanner | `go-licenses` (Google, Apache 2) + `licensei` (Bánzai Cloud, Apache 2) cover the Go-module compliance ask; `scancode-toolkit` covers the deeper source-level case. License-allowlist enforcement lives in CI as a pre-merge gate, not as a regatta verb. | A persona-B/C reseller signs a pilot specifically requiring in-substrate license attribution alongside Sigstore attestation. Even then, wrap go-licenses output into the W10 bundle — do not re-implement. |
| Bespoke skill-version registry | Anthropic Skills format already carries `version:` in the front-matter; pinning happens at the dispatch-prompt layer (operator pins `skill@1.2.3` in the autonomous-session-prompt). regatta-side version DB duplicates the Skills directory's own metadata. | Anthropic drops the `version:` field from the Skills front-matter OR a customer demands cross-skill compatibility resolution (semver-solver shape) that the directory does not provide. |

14 cuts. Each cut is a step we don't take.

### 9.1 Fleet-management stack — adopt/reject

Self-host single-tenant is the MVR-1+MVR-2 scope (row 9 hosted-
SaaS reject above). Three named fleet primitives evaluated; all
three **rejected** under current scope per
`feedback_decision_priority` (UX > best-practices) and
`feedback_research_design_principles` (adoption-first — no proven
OSS need until persona-B/C fleet ask fires).

| Primitive | Decision | Reason | Reopen condition |
|---|---|---|---|
| **SPIFFE / Spire** (workload identity) | Reject | Single-tenant self-host has one trust boundary (operator's CI runner). GitHub OIDC + a 30-line `id_token` verifier covers MVR-1+MVR-2. Spire's runtime + control-plane cost outweighs benefit at <100 nodes. | A persona-B/C signs a pilot requiring cross-cluster workload identity (mTLS between regatta substrates in separate VPCs) OR W10 attestation chain needs SPIFFE IDs as the subject claim. |
| **Spinnaker** (deploy pipeline) | Reject | Regatta ships as `go install` + GoReleaser binary (§6.4). Spinnaker is a 7-service JVM stack — the deploy primitive is heavier than what it deploys. GitHub Actions + GoReleaser is the proven OSS path. | A persona-C reseller signs a pilot requiring blue/green or canary rollout of regatta substrates across N customer clusters from a single control plane. |
| **HashiCorp Nomad** (workload scheduler) | Reject | Regatta's scheduler IS the wedge (W1 substrate + W2 conditional-DAG + W3 cost-governor). Adopting Nomad as the lower-layer scheduler creates two schedulers; the wedge dissolves. Single-tenant runs on a single host or a single K8s namespace — no scheduler needed below regatta. | A persona-C asks specifically to run regatta on Nomad-managed infra AND `kubectl` is rejected by their stack. Even then, build Nomad as a substrate target (a W1 backend), not a scheduler-below-regatta. |

All three reopen on the same gate: a named persona-B/C contract
with the fleet ask in writing. Self-host single-tenant ships first;
fleet management is MVR-3+ territory.

### 9.2 Plugin / extension API — pick

**Pick: MCP server is the extension surface. CLI-only at the
substrate; no plugin-as-binary registry.**

Three options considered; MCP wins on adoption-first
(`feedback_research_design_principles`) — the MCP ecosystem
already ships the IDE→dispatch surface that regatta gets for free.

| Option | Verdict | Precedent |
|---|---|---|
| **MCP server (IDE → dispatch)** | **Adopt.** Regatta exposes `regatta-mcp` (read-only first per cuts row 10; read-write behind `--write` at MVR-2). Any MCP client (Claude Code, Cursor, Cody, VS Code Copilot once MCP lands) dispatches DAGs without a regatta-shipped IDE plugin. | Anthropic MCP servers directory (filesystem, github, postgres, slack — all OSS, registry-listed). MCP is the de-facto IDE↔tool protocol post-2025. |
| **Plugin-as-binary (git-style `git-foo` on PATH)** | Reject. Git's PATH-discovery model works for stateless subcommands but leaks regatta-internal cost-governor + blackboard surfaces to arbitrary binaries. Versioning + signature-verification cost outweighs benefit when MCP already serves the same need. | Git plugins (`git-lfs`, `git-crypt`) and `kubectl` plugins via krew show the pattern works — both pay a meaningful trust + version-drift tax (krew's manifest-signing is mandatory). |
| **Pure CLI (no extension surface)** | Reject. Closes off the IDE→dispatch wedge entirely. Persona A's #1 UX ask (W7 research) is "dispatch from where I already am." Pure CLI forces context-switch to terminal. | N/A — chosen-against. |

**Reopen for plugin-as-binary:** a persona-C reseller asks for a
regatta-native plugin registry AND MCP cannot satisfy their use
case (e.g. long-running daemon, not request/response). File a
tracking issue at that point.

<!-- FOLLOWUP-RESOLVED 2026-06-02: §9.1 fleet-mgmt + §9.2 plugin-API closed in-place. -->

### 9.3 Rollback strategy — regatta auto-merges a bad PR, main breaks

When regatta dispatches a PR that passes all gates, auto-merges,
and breaks `main` (test red post-merge, telemetry regression, or
operator-tagged), the rollback path is a **first-class regatta
verb**, not a manual git-revert dance.

**CLI shape.** `regatta rollback --to <good-sha>` materializes a
rollback PR off the bad-merged commit:

1. Branch from the bad-merged sha (`HEAD` if the bad PR was the
   last merge) named `rollback/<short-bad-sha>`.
2. `git revert --no-edit <bad-merge-sha>` produces the inverse
   diff. Multi-commit bad-merges roll back as a range.
3. Open the rollback PR with title `revert: <bad-pr-title>
   (#<bad-pr-num>)` and a body that names the trigger ("CI red
   post-merge", "operator-tagged", "telemetry regression
   `<metric>`").
4. The rollback PR enters the **same gate pipeline** as any other
   regatta PR — make check, pr-lint, reviewer subagent, automerge
   policy. No force-push. No bypass.

**Why gates stay on the rollback PR.** GitHub branch-protection on
`main` is unchanged. The rollback PR carries no privilege beyond a
normal PR; gate-pass is required. This is load-bearing: if
rollback bypassed gates, every auto-rollback becomes an unsigned
trust hole. Operator can manually-merge if a gate is itself the
regression (e.g. CI infra outage), but the manual path is a
separate verb, not the default.

**W9 substrate replay.** W9 `DurableHistory` (substrate Wave-1,
see `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-
redteam.md`) re-runs the gate verdicts against the rollback PR.
Replay confirms the revert diff produces a green gate set on the
pre-bad-merge sha — a double-check that the revert is genuinely a
no-op against history, not a new defect with a misleading commit
message.

**Precedents.**

- **Renovate auto-rollback** — Renovate's `rollbackPrs: true`
  policy auto-files a downgrade PR when an upgrade is later
  flagged as regression; the rollback PR re-runs CI like any
  other. Reference: Renovate docs, `rollbackPrs` setting.
- **Aviator merge-queue revert** — Aviator's merge queue auto-files
  a `revert: <pr-title>` PR when post-merge CI fails on the queue's
  base branch, then re-enqueues it through the normal queue gate.
  Reference: Aviator docs, "auto-revert on merge failure".

Both precedents share the shape: **revert is a PR, gates stay on,
no force-push**. regatta inherits the same invariant.

<!-- FOLLOWUP: how does regatta DETECT a bad merge? Three candidate signals — (1) CI red on `main` post-merge (cheap, narrow); (2) operator-tagged via GH comment `@regatta rollback` (manual but covers every defect class); (3) production telemetry-driven (error-rate spike on a named metric within N minutes of merge; requires customer telemetry wiring). Recommended MVR-1 default: (1) + (2); (3) defers to MVR-3 when customer telemetry pipelines exist. File a `[FOLLOWUP] rollback detection signal pick` issue before MVR-1-T1 dispatch; without a chosen signal, `regatta rollback` is operator-invoked only — acceptable for MVR-1, load-bearing for MVR-2+. -->

### 9.4 Secret-rotation design — beyond #379 HMAC rotation

PR #379 landed multi-key keyring rotation for the HMAC signing
key. The same shape extends to every other long-lived credential
regatta holds. Pattern: **multi-key window during cutover, accept
both old and new for a bounded period, sweep the old key after**.

**ANTHROPIC_API_KEY rotation.**

| Surface | Design |
|---|---|
| Storage | Env var read at process start; held in-memory; never written to logs or substrate. |
| Rotation flow | Operator issues new key in Anthropic console, sets `ANTHROPIC_API_KEY_NEXT` env var, restarts (or signals SIGHUP for hot-reload variant). |
| Multi-key window | Both `ANTHROPIC_API_KEY` (current) and `ANTHROPIC_API_KEY_NEXT` (next) accepted by the HTTP client for **7 days** (matches the conservative end of the #379 HMAC window). After 7 days operator promotes NEXT → CURRENT and unsets NEXT. |
| Hot-reload | MVR-2 ships SIGHUP-driven reload (dispatcher re-reads env vars from sidecar config); MVR-1 ships restart-only. Drill operator-doc lives at `docs/operations/secret-rotation.md` (created when MVR-1-T2 lands). |

**GH_TOKEN rotation.**

| Surface | Design |
|---|---|
| Forms accepted | GitHub App installation token (default for MVR-2+), org-level PAT (acceptable for MVR-1 single-tenant), classic PAT (rejected — too broad). |
| Rotation flow | Same SIGHUP / restart pattern as ANTHROPIC_API_KEY. GitHub App tokens auto-rotate hourly via the installation-token API; the rotation flow handles app-private-key rotation, not access-token rotation. |
| Multi-key window | App private keys: 7-day overlap. Org-level PATs: 24-hour overlap (PATs are coarser-grained; long overlap widens the blast radius if leaked). |
| Org-level vs PAT vs App | Persona-A defaults to user PAT. Persona-B defaults to GH App with per-installation scoping. Dispatcher reads `GH_AUTH_KIND={pat,app}` to dispatch to the right token-mint path. |

**Sigstore cosign key rotation (W10).**

| Surface | Design |
|---|---|
| Generation | `cosign generate-key-pair` produces `cosign.key` + `cosign.pub`; the private key is encrypted with a passphrase held in operator-side KMS or sealed-secret store. |
| Rotation flow | `cosign rotate-key` (or generate a new pair) → re-sign the bundle of in-flight attestations with the new key → publish both old and new public keys to the regatta `cosign.pub` set during the overlap window. |
| Multi-key window | **30-day overlap**: any verifier accepts either key for 30 days post-rotation. Longer than HMAC/ANTHROPIC because attestation chains are read by downstream consumers who pin the public key in their own infra. |
| Re-sign cost | Bundle re-signing is O(open attestations), bounded by W10's retention window. For MVR-3 launch volumes (≤10k attestations), re-sign is a minutes-scale batch job — not a hot-path concern. |

**Multi-key window summary.**

| Key | Window | Trigger |
|---|---|---|
| HMAC (already shipped, #379) | 7 days | manual via `regatta key rotate` |
| ANTHROPIC_API_KEY | 7 days | operator console + env-var swap |
| GH_TOKEN (PAT) | 24 hours | operator |
| GH_TOKEN (App private key) | 7 days | operator |
| Sigstore cosign | 30 days | operator + bundle re-sign |

**Precedents.**

- **HashiCorp Vault** — Vault's transit-engine key-rotation
  pattern supports `min_decryption_version` < `latest_version`,
  letting both old and new keys decrypt during cutover.
  Reference: Vault docs, `transit/keys/<name>/config` API.
- **AWS Secrets Manager** — Secrets Manager's rotation lambda
  pattern keeps the current secret `AWSCURRENT` and the new
  secret `AWSPENDING` until rotation completes, then atomically
  swaps. Reference: AWS Secrets Manager rotation user guide.

Both precedents share regatta's invariant: **dual-key acceptance
during cutover, atomic promotion after**. No flag-day rotation.

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

### 10.6 Customer-0 interview followup — persona pick is unvalidated until ≥3 maintainer interviews land

The persona-A pick in §1 is **desk research**, not validated
demand, per #421 RISK #423. Surfacing this explicitly so the
unvalidated-demand risk is not buried in the brief.

**Load-bearing action item.** Identify and interview **≥3 real
OSS-maintainers-of-large-repos** BEFORE MVR-1 implementer
dispatch. Record each interview (notes + transcript) and analyze
for:

- Does the maintainer's pain match the W7 UI + `regatta init`
  surface §3 ranks #1 and #2?
- Is "multi-PR-per-day" their real shape, or a desk-research
  artifact?
- Would they install a Go binary that dispatches Claude Code
  against their repo? (Trust contract test.)
- What is their actual time budget — does the "< 30 min TTV" claim
  in §1 match the maintainer's day?
- Stated WTP — confirm or refute §1's $0 + sponsorship-bounded
  estimate. §10 Q2 depends on this answer.

**Candidate interview targets** (operator-side outreach effort):

- **langchain** — Harrison Chase. Largest agent-orchestration repo
  by stars; persona-A archetype if the project shape weren't SaaS-
  flavored.
- **prefect** — Jeremiah Lowin. Workflow-orchestration maintainer;
  closest mental-model overlap with regatta's DAG primitives.
- **dagster** — Nick Schrock. Asset-graph + freshness-SLO author
  (cited in §6.1); direct relevance to MVR-2+ inference work.
- **temporal** — Maxim Fateev. Durable-execution maintainer; the
  W9 redteam concluded against Temporal but the maintainer's view
  of self-host pain is load-bearing.
- **n8n** — Jan Oberhauser + team. Workflow tool with strong
  self-host adoption; persona-A funnel comparison.
- **langflow** — visual-DAG agent maintainer; persona-A overlap on
  the OSS-of-large-repo axis.

Operator picks at least 3 from the candidate set (or substitutes
others with comparable repo scale ≥10k stars + multi-PR-per-day
shape).

**Reopen trigger (gates MVR-1 dispatch).** **MVR-1 implementer
dispatch happens ONLY after ≥3 maintainer interviews are recorded
+ analyzed; otherwise the persona pick remains unvalidated and W7
UI risks being built to a hypothetical user.** This is the
strongest gate in §10 because it bounds every downstream estimate
in §4.

If fewer than 3 interviews land within 30 days of brief acceptance,
the operator either (a) widens the candidate list or (b) accepts
desk-research-only and explicitly notes the validation gap in the
MVR-1 dispatch prompt. Default is (a).

Interview artifacts land in `docs/engineer/research/customer-0-
interviews/` (created when first one lands) — one file per
interview, with verbatim quotes and a per-interview verdict on
whether the persona pick survives contact with the maintainer.

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
| **A (target)** | B + 4 personas scored on ≥3 axes (§1). Strategic gaps mapped (folded into §3 + §5 + §6 sections). Phase X wedges score ≥2 OSS candidates each (§5). 4-phase sequenced roadmap with abandon-criterion per phase (§4). Gate criteria measurable (§2). ≥5 cuts with reopen condition (§9 has 14). |
| **A+ (stretch)** | A + 4 wave-3 cross-category insights surfaced + 1 cultural-moat insight (§6.7). Zero bespoke wedges across four phases (§4 budget summary). Customer-0 pick rebuttable with adversarial note (§1). Effort + abandon-criterion per task (§4 tables). ≥10 cuts (§9 has 14). Single-source consolidation kills 24 superseded PRs (§12 + this PR's "Supersedes" list). |

**Self-scored tier:** A+ — every criterion met. The consolidation
itself is the A+ delta: four chains collapsed to one brief, 24
PRs closed, single-canonical destination for MVR-1 dispatch.

---

## Update — 2026-06-02 PM

PM-wave merge sweep landed ~30 PRs while this brief was open. The
sections below are status-only; strategic content above is
unchanged. Reading order: shipped → in-flight → pending.

### Shipped today (does not change MVR-1/2/3/4 sequence)

These merges land under the **pre-MVR-1 self-host hardening**
window — they are not MVR-1 dispatch wedges (no W7 UI / `regatta
init` / Gitea adapter merged), but they reduce risk on §4's
abandon-criteria and clear §9.4's secret-rotation invariants ahead
of MVR-1 kickoff.

**§9.4 secret-rotation (HMAC chain shipped end-to-end):**
- #79 / #393 — `regatta keys re-sign-briefs` subcommand for HMAC key rotation.
- #379 — multi-key HMAC keyring parser for the rotation drill.
- #389 — `keys` subcommand tree (list/rotate/retire) + retire pre-flight.
- #395 — `regatta keys recover` + operator key-rotation runbook.
- #374 — S3-T3 spec for the rotation drill + operator recovery (docs).

The HMAC row in §9.4's multi-key-window table is now
operator-runnable end-to-end; only ANTHROPIC_API_KEY + GH_TOKEN +
cosign rows remain on paper (those land with MVR-2-T2 + MVR-3-T1).

**L4 reviewer-gate substrate (feeds §5.5 + MVR-2-T1):**
- #370 — wire L4 adversarial gate at scheduler step 0.7.
- #373 — anthropic adapter + tolerant parser + 7-fixture table.
- #375 — extract shared severity parser to `internal/gates/severity`.
- #380 — reviewer-disagreement second-opinion loop.
- #381 — in-memory LRU findings cache (#357).
- #385 — auto-fix patch mode (unified-diff on findings).
- #387 — prompt template hot-reload (SIGHUP + fsnotify).
- #388 — per-category model selection (#355).

The L4 gate substrate now carries the shape DeepEval will plug
into at MVR-2-T1 (§5.5 picks DeepEval; the rubric loader + severity
parser + cache + hot-reload are the seams).

**Cost-governor hardening (reduces §4 MVR-1 abandon-risk):**
- #434 — retire local `BudgetReconciledPayload` stub (#275).
- #440 — boot validator + known-bad fixture for rollback runbook (#290).
- #441 — soft-cap warn mode requires explicit ack (#226).
- #442 — wire `reconcile.Run` into production startup (#276).
- #445 / #447 / #451 — pricing empty-table guard + Lookup-time zero-rate defense.
- #450 — `reconcile/appender_test` import fix.
- #452 — `cost.reconcile_failing` attempt_count reflects real attempts (#439).
- #461 — cost-governor sampler-customization E2E (#228).
- #372 — gremlins mutation CI for cost + scheduler (S2-T4 wave 1).
- #454 — mutation-survival tests for reaper tier-comparison helpers (#147).

**State / approval substrate (feeds MVR-2-T2 + §9.1 rollback):**
- #133 / #386 — wire `state.Approval` → `notify.Request` adapter.
- #156 / #443 — consolidate `ApprovalGateConfig` with `approval.Config`.
- #369 — S3-T2 Phase B approvals shadow-write seam (migration 0009).
- #378 — S3-T2 Phase C approvals read-from-substrate seam (migration 0011).
- #377 — property fold(events)≡state-machine (1000 checks).
- #382 — S3-T4 T2 crash-recovery property test (rapid 200/2000 cases).
- #391 — nightly 2000-case property sweep + make target.
- #394 — crash-recovery property tests + factor golden-DB clone.
- #453 — typed `TransitionWorkItem`; drop raw-SQL CAS.
- #455 — `approval_list.v1.json` contract + schema-check.
- #456 / #457 / #459 — `approval_list` nil reviewer_set orEmpty shim + doc surface.
- #460 — `canon.VerifyToken` derives reviewer from claim when expectReviewer empty (#305).
- #462 — drive `approvals.go` coverage to 95% via branch tests (#139).
- #144 / #444 — reaper consolidate fold helpers + route events via recordEvent.

**Observability + W8 OPA (feeds §5.3 + W8 commercial-core):**
- #432 — observability roadmap converged spec (consolidates #400 #405 #410 #413 #420).
- #436 — surface `EventCostReconcileFailing` on OTel ERROR severity.
- #438 — honor `OTEL_TRACES_SAMPLER` env in obs/otel setup (#174).
- #448 — wire OPAAuthorizer + hot-reload into serve (#364).
- #396 — gate OnStart on SIGHUP-handler readiness (resolves signal: hangup).

**Brief / followup substrate (feeds §10 + meta-workflow):**
- #80 / #390 — durable brief-rejection sink via substrate events.
- #78 / #392 — warn on criteria drift for in-flight children.
- #95 / #376 — pin reserve continue-on-error contract.
- #145 / #146 / #444 — reaper fold-helper consolidation.
- #175 / #465 — bridge per-record overhead bench + <5us guard.
- #333 / #371 — tighten reviewer_tag regex to stop over-matching prose.
- #368 — GH `[followup]` issues → `work_item` briefs (S2-T3).

**Operator docs:**
- #397 — getting-started: clone → first PR walkthrough + README index refresh.
- #398 — record 2026-06-02 autonomous-session shipped PRs.

### Pending — MVR-1 dispatch wedges untouched

The §3/§4 MVR-1 top-3 wedges remain pending; none merged in the
PM wave:

- **MVR-1-T1 — W7 Wave 1 htmx UI.** Not started. Spec landed in §11 (#318/#303/#307 referenced); implementer dispatch gated on §10.6 customer-0 interview completion (≥3 maintainer interviews).
- **MVR-1-T2 — `regatta init` wizard.** Not started.
- **MVR-1-T3 — GoReleaser release pipeline.** Not started.
- **MVR-1-T4 — GH-issue adapter (`[autonomous]` label).** Not started.
- **MVR-1-T5 — P3.8 SCM-adapter + Gitea second consumer.** Not started.
- **MVR-1-T6 — Pricing + support-contract design.** Item filed in `.regatta/items/mvr-1-t6-pricing-support-contracts.md`; awaits §10 Q5 license decision.

### Pending — §10 operator decisions

All five §10 questions (#423–#427) remain open. §10.6 customer-0
interview gate (≥3 maintainer interviews) remains the load-bearing
block on MVR-1 implementer dispatch. None of today's PM-wave
merges resolve §10 — the wave hardened substrate, not strategy.

### Net read

Today's wave is **pre-MVR-1 substrate hardening** — the L4
reviewer-gate, cost-governor, approval-state, and HMAC-rotation
seams that MVR-1/MVR-2 plug into. The MVR-1 wedge surface (W7 UI
/ init bundle / Gitea adapter) is still untouched and remains
gated on §10.6 interview completion + §10 Q1–Q5 operator answers.
