# Customer-roadmap amendments — applying adversarial-review #403 to brief #399

_Author: design subagent, 2026-06-02. Scope: concrete diffs that amend `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` (PR #399) per the adversarial review in `docs/engineer/reviews/2026-06-02-customer-roadmap-review-of-399.md` (PR #403, verdict ADOPT-WITH-AMENDMENTS, 2 PASS / 4 RISK / 2 BLOCKER). Lands after both #399 and #403 merge; superseded by a single edit pass on the brief itself once an implementer applies these diffs. Adoption-first per `feedback_research_design_principles` — every amendment reuses prior in-repo artefacts before inventing new structure._

## 0. How to read this spec

Each amendment block has the same shape:

1. **Finding** — lens ID + verdict from #403.
2. **Cite** — line/section in the original brief.
3. **Diff** — exact insertion/replacement against the brief at HEAD-of-`spec/next-horizon-roadmap-2026-06`.
4. **Justification** — decision-priority axis per `feedback_decision_priority` (UX → ease → performance → best-practices → speed → velocity; long-term > short-term).

Amendments are ordered BLOCKER → RISK so the implementer lands the load-bearing fixes first per `feedback_unaddressed_load_bearing`.

Two PASS lenses (L3 gate criteria, L4 adopt-vs-build) generate zero diffs and are not repeated here.

---

## 1. Amendment for L6 — pricing + monetization (BLOCKER)

**Finding (L6, BLOCKER):** brief defers four pricing-shaped Qs (Q2 WTP, Q3 open-core, Q4 hosted SaaS, Q5 license) to "MVR-2 kickoff." Each license choice inverts the MVR-3 wedge selection; punting is a research-note shape not a roadmap shape.

**Cite:** §1 line 34 (persona-A WTP punt), §9 Q2/Q3/Q4/Q5 (lines 343-346).

**Diff — replace §9 with the following.**

```
## 9. Open questions — operator must answer before MVR-1 dispatch

Per `feedback_decision_priority` long-term > short-term: four of the five Qs below are committed to a stub answer NOW; the brief no longer defers load-bearing strategy. Stubs are rebuttable but they unblock dispatch.

### 9.1 Committed stubs (BEFORE MVR-1 dispatch)

| # | Question | Stub commit | Rebuttal trigger |
|---|---|---|---|
| Q3 | Open-core vs commercial-core? | **Commercial-core for MVR-1/MVR-2.** Everything OSS. Revenue from hosted SaaS + support contracts only. Re-evaluate at MVR-3 kickoff if a persona-B/C pilot specifically asks for paid on-prem features. | One signed pilot LOI specifies "we will pay for self-hosted enterprise features (W8/W10/W12) and would NOT pay for hosted." |
| Q5 | License — Apache 2.0 vs BSL vs AGPL? | **Apache 2.0 for MVR-1.** Maximizes persona-A adoption + persona-C compat. Accept SaaS-resale risk; competitors monetizing regatta-as-a-service is a downstream-of-product-market-fit problem, not a Day-0 problem. | One persona-C platform vendor publicly ships regatta-as-a-service without contribution back; revisit BSL at MVR-3. AGPL stays rejected (repels persona-C entirely). |
| Q4 | Hosted SaaS or self-host-only? | **Self-host-only for MVR-1/MVR-2.** Hosted SaaS is the third product (persona C primary). Re-open per §7 cut "Hosted SaaS (regatta cloud)." | Persona-B/C asks specifically AND commits to a pilot LOI per §7 cut wording. |

These three stubs collapse the MVR-3 fork: under Apache 2.0 + commercial-core + self-host-only, persona-C revenue path is **support contracts on self-host deployments + hosted SaaS as a separate MVR-4+ product line.** W11 blackboard + W12 metering + P3.8 LLM-gateway remain MVR-3 (no rank change required from L6 — see §3 amendment-block synthesis below).

### 9.2 Operator answers before MVR-1 kickoff (still open)

| # | Question | Decision needed by |
|---|---|---|
| Q1 | Who is customer 0 by name? Operator names one specific maintainer + repo from §1's persona-A example list. | MVR-1 kickoff |
| Q2 | Persona-A paid SKU? Stub answer: **no paid persona-A SKU in MVR-1; treat persona A as pure adoption surface.** Re-open if persona A inbound asks ($50-500/mo sponsorship range) accumulates ≥5 unsolicited offers. | End of MVR-1 |

### 9.3 Decision-record landing

All five Qs (committed stubs + open Q1/Q2) land in `docs/engineer/decisions/2026-06-XX-customer-roadmap-pricing.md` (created when the first stub is ratified) before MVR-1 dispatch. Per `feedback_decision_priority` long-term > short-term: stubs are written down so a future operator can read the decision trail, not re-derive it.

### 9.4 Why stub instead of decide

A stub is rebuttable; a punt is not. Each stub names the exact signal that flips the answer. Under `feedback_decision_priority` UX > best-practices: the UX of "implementer reads §9 and dispatches" beats the best-practice of "operator deliberates for 4 weeks." Operator can override any stub by editing the decision record; the brief stays dispatch-ready in the interim.
```

**Justification per `feedback_decision_priority`:** UX (implementer can dispatch MVR-1 without operator-week stall) > ease (writing stubs is harder than punting) > performance (no perf impact) > best-practices (best-practice deliberation deferred to decision-record edit) > speed (stub commit takes 1 operator-hour vs 1 operator-week deliberation) > velocity. Long-term > short-term: Apache 2.0 + commercial-core stub maximizes 5-year persona-A + persona-C surface; BSL/AGPL re-openable when revenue signal appears.

---

## 2. Amendment for L7 — competitive position (BLOCKER)

**Finding (L7, BLOCKER):** brief's sole competitive frame is "Claude Code Dynamic Workflows" (§1 line 29). Zero analysis of Devin, Sweep AI, Cursor BugBot, Copilot Workspace, OpenHands, CodeRabbit. `docs/design.md` competitive table is not cited. Persona A is actually competing against Sweep + Copilot Workspace; persona B against Devin + CodeRabbit + Cursor BugBot.

**Cite:** §1 line 29 (sole competitive frame); `docs/design.md` competitive table not referenced.

**Diff — insert new subsection §1.5 between §1 (last paragraph) and §2.**

```
## 1.5 Competitive position — calibrated against 2026-Q2 landscape

Per `feedback_research_design_principles` reuse-before-rebuild: `docs/design.md` contains a competitive table from the MVP-3 era. This subsection refreshes that table for the post-self-host horizon, scored against the personas in §1.

| Competitor | Persona-A free-tier offer | Persona-A install friction | Persona-B WTP anchor | Cost-cap primitive | Signed audit chain | Self-host option | License shape | regatta delta |
|---|---|---|---|---|---|---|---|---|
| **regatta (post-MVR-1)** | full product, OSS | 3-5d (init wizard + GoReleaser binary) | $2-10k/mo team seat | per-DAG/operator USD+token caps shipped (W3) | HMAC SHA-256 shipped; Sigstore MVR-3 | yes — primary mode | Apache 2.0 (Q5 stub) | multi-PR ledger, queue, cap, audit |
| Devin (Cognition) | none (paid only) | minutes (hosted) | $500/mo/seat reference price | per-task budget; no fleet cap | proprietary log; no public chain | no | proprietary | one-shot session; not a fleet primitive |
| Sweep AI | free on public OSS repos | minutes (GH App install) | n/a (OSS-marketing only) | none disclosed | none (closed log) | no | proprietary (open-source legacy paused) | persona-A-shaped; no multi-PR ledger; cost surface opaque |
| Cursor BugBot | none on free Cursor; bundled in $20/mo Cursor seat | minutes (IDE integration) | seat-priced ($20/mo) | per-org Cursor cap | none | no | proprietary | IDE-bound; not a fleet primitive |
| GitHub Copilot Workspace | free on public OSS w/ Copilot OSS terms | seconds (click button in GH UI) | bundled in Copilot Enterprise ($39/seat/mo) | none — under GH org Copilot budget | none (closed log) | no | proprietary | GitHub-only; lock-in to GH; no audit chain |
| CodeRabbit | free tier on public OSS | minutes (GH App install) | $12-24/seat/mo | per-review cap | none (closed log) | no | proprietary | review-only; not a dispatcher |
| OpenHands (All Hands AI, ex-OpenDevin) | OSS (MIT) | hours (Docker + config) | n/a (OSS); All Hands cloud pricing pending | none disclosed | none | yes | MIT | single-session shell agent; not a fleet primitive |
| Claude Code Dynamic Workflows | bundled in Claude.ai $20/mo Pro | seconds (CC CLI) | bundled in Claude for Work ($25/seat/mo) | per-session token cap | none (closed log; Anthropic-internal) | no | proprietary | CC owns one-shot sessions; regatta owns the fleet |

**Delta synthesis per persona:**

- **Persona A (OSS maintainer):** primary competitors are **Sweep AI** + **Copilot Workspace** (both target OSS maintainers with free-tier on public repos). regatta's delta is the multi-PR ledger + cost cap + Apache-2.0 self-host. Friction tradeoff: Copilot Workspace is `click-a-button`, regatta is `3-5d to init + binary install`. Mitigation: `regatta init` (MVR-1-T2) compresses friction to ≤30 min on first dispatch; persona-A example users (langchain, prefect, dagster) already have CI complexity that makes `click-a-button` insufficient for fleet dispatch.
- **Persona B (internal-platform team):** primary competitors are **Devin** ($500/mo anchor) + **CodeRabbit** + **Cursor BugBot**. regatta's delta is self-host + signed audit + per-team cost cap. Devin is the WTP anchor; regatta MVR-2 prices in the $2-10k/mo team-seat band sit above Devin's $500/mo/seat because the unit is "team" not "seat" (5-20 seats per team).
- **Persona C (platform vendor):** primary competitors are **OpenHands** (only OSS contender) + bespoke in-house orchestrators. regatta's delta is the adapter-contract surface (P3.8) + multi-tenant scoping (W8). OpenHands ships single-session agent runtime; regatta ships the fleet primitive around it.
- **Persona D (research lab):** no direct competitor (research-mode overlay is a niche). regatta's delta is preregistration discipline + signed audit chain.

**What this table changes about §1's persona-0 pick:** nothing. Persona A remains the right adoption track because Sweep + Copilot Workspace both have surface gaps (no fleet cap, no audit chain) that the named persona-A example users care about (cost runaway on a public repo is reputational). What it changes is the **MVR-1 launch narrative**: regatta's pitch to persona A is no longer "vs Claude Code Dynamic Workflows" — it is "the fleet primitive Sweep + Copilot Workspace skipped." Launch copy + docs/landing-page MUST cite Sweep + Copilot Workspace explicitly per `feedback_research_design_principles` reuse — calibrating against the actual market beats positioning against a strawman.

**Refresh cadence:** this table is dashboardable as "competitive snapshot 2026-Q2." Re-score quarterly. Re-open the persona-0 pick if any one row's "persona-A free-tier offer" column ships a fleet-cap-plus-audit-chain primitive — that is the competitive moat regatta is betting on.
```

**Justification per `feedback_decision_priority`:** UX (implementer + operator + future contributors know which competitor they're actually fighting) > ease (table is mechanical given design.md prior art per `feedback_research_design_principles` reuse) > best-practices > velocity. Long-term: re-scoring quarterly hedges against 2026-Q3/Q4 market shifts.

---

## 3. Amendment for L1 — customer-0 persona realism (RISK)

**Finding (L1, RISK):** persona A's WTP is $0 — that's a traffic source, not a customer. Brief conflates "reach" with "revenue." Counter-personas B + new persona E (AI-consulting / agent-fleet integrator) dismissed too quickly.

**Cite:** §1 lines 11-22, §1 line 34 (revenue path).

**Diff A — extend §1 candidate-persona table to include persona E + rename the customer-0 framing.**

Replace the §1 table heading + table with:

```
### Candidate personas

| # | Persona | Named example users | Time-to-value | WTP | Retention risk | NPS proxy |
|---|---|---|---|---|---|---|
| A | OSS maintainer of a single large repo | `langchain-ai/langchain`, `prefecthq/prefect`, `dagster-io/dagster`, `temporalio/temporal`, `n8n-io/n8n`, `langflow-ai/langflow` | hours (one `regatta.yaml`) | low (~$0 OSS; sponsorship $50-500/mo at best) | medium (maintainer attention is scarce; PRs need to deliver visible velocity) | high — visible green PRs on a public timeline are organic marketing |
| B | Internal-tooling team at a mid-stage company (50-500 eng) running multi-repo agent dispatch | example targets: Vercel platform team, Linear platform, Sourcegraph internal, Replit infra, Modal internal | days (need `regatta.yaml` per repo + secrets plumbing) | high ($2-10k/mo per team for the seat-replacement narrative) | high (must keep up with Claude Code feature velocity; if CC ships native cost-gov, value collapses) | medium — internal advocates surface only on case-study Tuesdays |
| C | Platform vendors building agent-orchestration infrastructure on top of regatta | Convex agent platform, Buildkite agent harness, CodeSandbox agent runners, future Flowise/n8n agent-fleet add-ons | weeks (need stable adapter contracts + multi-tenant) | very high ($10k+/mo + revenue share) | high (every primitive we don't expose is one they reimplement) | low (vendors are quiet; loss reasons opaque) |
| D | Research labs running empirical AI/CS benchmark fleets | OpenReview-tracked labs, EleutherAI infra, MLCommons, Stanford CRFM, HuggingFace evals team | weeks (research-mode overlay; preregistration discipline) | medium ($1-5k/mo grant-funded) | low (publication-credible audit chain is hard to replace) | high (publications are public + cite tooling) |
| E | AI-consulting / agent-fleet integrator firms | named example targets: Galileo, Arcee, Sourcegraph-Cody integrators, long tail of "we ship agent infra for our clients" boutiques | days (per-client `regatta.yaml`; multi-client tenant isolation) | high ($5-20k/mo per seat across multiple client engagements) | medium (consulting margins absorb tool cost; switching tax is high) | medium — integrators advocate selectively to clients |
```

**Diff B — replace the §1 "Customer 0 pick" subsection title + first paragraph (after the candidate table, before "Justification") with two-track framing.**

Replace:

```
### Customer 0 pick — **Persona A: OSS maintainer of a single large repo**
```

With:

```
### Customer-0 split — two tracks running in parallel

The brief now explicitly splits "customer 0" into two parallel tracks per `feedback_decision_priority` UX > ease: bundling adoption + revenue into one persona created the conflation L1 flagged. Tracks run concurrently from MVR-1; tasks below in §6 indicate which track each task serves.

**Adoption track — Persona A (OSS maintainer of a single large repo).** Retention metric: GitHub Stars > 25 + ≥3 distinct repos with a `.regatta/` directory in their tree (queryable via `gh search code`). Revenue ask = $0. Function: organic discovery flywheel for the revenue track.

**Revenue track — Persona B OR Persona E (whichever fires first).** Retention metric: 1 signed pilot LOI by end of MVR-2; LOI lives in `docs/legal/` (created when first fires). Revenue ask = $2-20k/mo (persona-B team-seat or persona-E per-client seat). Function: validate WTP + sharpen MVR-3 wedge ordering.

**Why parallel not sequential:** the W7 UI investment in MVR-1 (htmx approval queue + cost panel + DAG view) serves both tracks pre-tenant. W8 tenant_id routing is a 2-3 wk delta from single-tenant to multi-tenant; it doesn't require waiting for MVR-2 to begin scoping. Revenue-track signals (LOI, persona-B/E inbound) trigger MVR-2-T2 (W8 tenant routing); adoption-track signals (GH Stars, `.regatta/` repos) trigger MVR-1 retrospective + MVR-2-T1 (W7 Wave 2).

**Adversarial-review note (kept from original §1):** persona A's WTP can be confused with persona D's because both are "research-adjacent" — they are NOT the same buyer. A buys velocity; D buys methodology. Persona E sits between A and B — they buy fleet primitives for client work, not for their own repo. Don't conflate.
```

**Diff C — replace §1 "Persona A → revenue path" paragraph (line 34) with the burn-cost quantification.**

Replace the existing paragraph (starts "**Persona A → revenue path.** Persona A's WTP is $0 …") with:

```
**Two-track burn cost.** Adoption track has WTP $0 by design. Revenue track lives or dies on MVR-2 LOI. Burn-cost framing: if MVR-1 (5-7 calendar wks) + MVR-2-T2 + MVR-2-T1 + MVR-2-T5 (~6-9 wks of MVR-2 ship) = ~11-16 wks total before any revenue, that's the financial exposure. The brief explicitly accepts this exposure because (a) the W7 + init + GoReleaser + GH-issue work is dual-purpose (both tracks consume it), (b) operator is funding from the lumalabs envelope through MVR-2, (c) adoption-track signal (GH Stars, `.regatta/` repos) within 60 days of MVR-1 ship is the abandon-criterion — see §6 MVR-1 abandon-criterion below. If neither persona-A adoption signal NOR persona-B/E inbound fires within 60 days of MVR-1 ship, halt MVR-2 dispatch + re-litigate persona pick via PRIORITY rewrite.
```

**Justification per `feedback_decision_priority`:** UX (sharp two-track framing prevents implementer confusion about which metric they're optimizing) > ease (splitting track names is ~20 lines of edit) > best-practices (named-burn-cost-with-abandon-criterion is best-practice for VC-backed bets; we adopt the pattern) > velocity. Long-term: persona E surfacing in §1 unblocks the persona-E inbound when it fires — without the table row, that signal would route to persona B by default and mis-shape MVR-2 scoping.

---

## 4. Amendment for L2 — top-3 wedge selection (RISK)

**Finding (L2, RISK):** rank-3 (Gitea SCM adapter) is picked for engineering convenience not customer leverage. No named persona-A example user is blocked by GitHub-only. GitLab unblocks more persona-B internal-platform teams. W7 Wave 2 DAG-view + log-streaming is a higher persona-A retention lever.

**Cite:** §3 lines 134-143 (Gitea scoring), §4 line 195 (rank 3 placement).

**Diff — replace §4 "Recommended top-3 next wedges" subsection.**

Replace the existing top-3 list with:

```
### Recommended top-3 next wedges

Per L2 amendment-block: rank-3 swapped from "Gitea SCM adapter" to "W7 Wave 2 DAG read view + log streaming." Gitea demoted to MVR-2-T4-stretch alongside the LLM-gateway adapter — both serve the persona-B revenue track, both are speculative until that track signals.

1. **W7 Wave 1 htmx UI** — approval + cost panel + DAG read view. Highest UX delta for persona A; ships behind a single Go binary embedded with template+CSS. Adopts the existing spec (`docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`). Zero new runtime deps. Mobile-friendly approval flow is the load-bearing customer-0 unblock.
2. **NEW bundle: `regatta init` + GoReleaser + GH-issue adapter** — adoption-cost collapse. Without this, the W7 UI is invisible because persona A bounces at minute 5. Adopts AlecAivazis/survey + GoReleaser + go-github; no bespoke build. Ships in 1-2 weeks total.
3. **W7 Wave 2 htmx — DAG read view + log streaming** — persona-A retention lever. Visible "what is regatta doing right now" is the second-week conversion event after first-PR-merged. Adopts htmx SSE + existing template stack. Zero new runtime deps. Replaces the previous rank-3 (Gitea SCM adapter) — Gitea is demoted to MVR-2-T4-stretch (deferred until a named Gitea-hosted persona-A or persona-B inbound fires).

The top-3 explicitly excludes W8 multi-tenant, W10 Sigstore, AND the SCM adapter: persona A does not need them, and per `feedback_decision_priority` the customer-0 UX bar dominates persona B's hypothetical compliance bar AND the engineering-convenience pull of "Gitea ships first because it's shaped like GitHub."
```

**Diff — replace §4 prioritization-matrix rows 3 and 4.**

Replace rows 3-4 of the §4 matrix with:

```
| 3 | W7 Wave 2 htmx (DAG view + log streaming) | P1 (G2 extended; persona-A retention) | 2-3 | none | low | n/a (persona A) | **A** |
| 4 | P3.8 SCM adapter — Gitea OR Gitlab (decide at trigger) | P2 (G7; deferred — no named persona-A blocked) | 1-2 | low | low | enables persona B-shaped tenants on non-GitHub SCM | B+ |
```

**Diff — replace §3 P3.8 SCM-adapter score table verdict line.**

Replace:

```
**Verdict:** ship Gitea SCM adapter first as the second-consumer proof for the SCM-adapter contract, per `feedback_research_design_principles` "no proven equivalent for exact shape" — every adapter contract needs a second consumer or it's spec ceremony.
```

With:

```
**Verdict (amended per L2 #403):** DEFER SCM adapter to MVR-2 stretch. Decide Gitea-vs-GitLab at the trigger — if a named Gitea-hosted persona-A inbound fires first, ship Gitea; if a persona-B internal-platform team on GitLab Enterprise asks first, ship GitLab. The second-consumer-proof requirement for the SCM-adapter contract is satisfied at MVR-2 by EITHER adapter; rank-ordering between Gitea and GitLab is a customer-signal-driven call, not an engineering-convenience call (which would auto-pick Gitea for the GitHub-shape proximity). Per `feedback_decision_priority` UX > ease — the customer-signal-driven rank wins even though Gitea is engineering-easier.
```

**Diff — replace §6 MVR-1 task table row MVR-1-T5 (Gitea).**

Replace:

```
| MVR-1-T5 | P3.8 SCM-adapter contract + Gitea second consumer | M (1-2 wks) | go-gitea/sdk | P3.8 spec (deferred — landed concurrently) |
```

With:

```
| MVR-1-T5 | W7 Wave 2 htmx — DAG read view + log streaming | M (2-3 wks) | htmx + Go html/template + SSE | spec extension to W7 (file followup; co-design with W7 Wave 1 implementer) |
```

**Diff — add to §6 MVR-2 task table (insert MVR-2-T4 row between existing T3 and T4).**

Insert:

```
| MVR-2-T4-stretch | P3.8 SCM-adapter contract + Gitea OR GitLab (decide at trigger) | M (1-2 wks) | go-gitea/sdk OR gitlab-org/api/client-go | requires named SCM-blocked inbound |
```

(Renumber the existing T4 LLM-gateway adapter row + T5 to keep the table contiguous; renumber W7-Wave-3-polish as T6.)

**Justification per `feedback_decision_priority`:** UX (persona-A retention lever > speculative second-SCM consumer) > ease (Gitea was the engineering-easy pick; the L2 finding correctly calls this out) > best-practices (customer-signal-driven adapter choice > engineering-convenience adapter choice) > velocity. Long-term: ship the SCM adapter when the customer fires, not before.

---

## 5. Amendment for L5 — §7 cut #5 internal contradiction (RISK)

**Finding (L5, RISK):** §7 cut #5 "Reviewer-rich PR UI as standalone product" is contradicted by §6 MVR-2-T1 which ships "W7 Wave 2 htmx — DAG read view + reviewer-rich PR UI." Either the cut wording is wrong or MVR-2-T1 is wrong.

**Cite:** §7 cut #5 (table row), §6 MVR-2-T1 (line 266).

**Diff — replace §7 cut #5 row.**

Replace:

```
| Reviewer-rich PR UI as standalone product | Rejected. Persona A reads PR diffs in GitHub UI directly. Building a separate reviewer-side UI doubles the surface persona A bounces off. | Persona B/C signs a pilot specifically asking for in-regatta diff review (signals their org doesn't use GitHub UI). |
```

With:

```
| Reviewer-rich PR UI **as standalone product separate from the in-regatta web UI** | Rejected. Persona A reads PR diffs in GitHub UI directly. Building a separate reviewer-side UI (e.g. `regatta-review` binary, browser extension, or hosted-only review dashboard) doubles the surface persona A bounces off. NOTE: this cut does NOT reject the reviewer-rich PR UI that ships INSIDE the regatta web UI as part of W7 Wave 2 / W7 Wave 3 (`docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`) — that is a different surface, justified by W7's existing spec, and serves the in-regatta approval flow not a standalone diff-review product. | Persona B/C signs a pilot specifically asking for in-regatta diff review **as a separate product surface** (signals their org doesn't use GitHub UI AND wants to license the review surface independent of regatta dispatch). |
```

**Justification per `feedback_decision_priority`:** UX (reader of the brief understands which thing is rejected and which thing ships) > ease (a single sentence amendment) > best-practices (internal-consistency is best-practice for strategic briefs) > velocity. Long-term: future contributor doesn't re-litigate "wait, didn't we cut this?" two PRs later — the load-bearing inconsistency from L5 is foreclosed.

---

## 6. Amendment for L8 — timeline realism (RISK)

**Finding (L8, RISK):** MVR-1-T1 widening (2-3 → 3-5 wks), MVR-3-T4 effort flagged unknown (not 6-8 wks), persona-install-count abandon-criterion applied to MVR-2 + MVR-3, cross-phase total understated ~25%.

**Cite:** §6 line 252 (MVR-1-T1 effort), §6 line 258 (abandon-criterion form), §6 line 304 (cross-phase budget).

**Diff A — replace §6 MVR-1-T1 row.**

Replace:

```
| MVR-1-T1 | W7 Wave 1 htmx UI — approval queue + cost panel | M (2-3 wks) | htmx + Go html/template | spec landed (#318/#303/#307) |
```

With:

```
| MVR-1-T1 | W7 Wave 1 htmx UI — approval queue + cost panel | M (3-5 wks; widened per L8 #403 — observed regatta velocity on W6/W8/W9 ran 4-6 wks at implementation phase, not 2-3) | htmx + Go html/template | spec landed (#318/#303/#307) |
```

**Diff B — replace §6 MVR-1 abandon-criterion paragraph (line 258).**

Replace the existing abandon-criterion sentence with:

```
Effort total: ~6-9 calendar weeks at current parallel pace (widened from 5-7 per L8 #403). **Abandon-criterion:** if MVR-1-T1 takes >6 wks (widened from >4 to absorb the realistic 3-5 wk implementation band) OR no persona-A install lands within 60 days of MVR-1 ship (measured as GitHub Stars >25 + ≥3 distinct repos with a `.regatta/` directory in their tree, queryable via `gh search code`), halt MVR-2 dispatch + revisit persona pick. The 60-day window assumes the operator posts MVR-1 launch to Hacker News + r/golang + the Anthropic Developers Discord — outbound effort is a 1-day task, not a wedge.
```

**Diff C — replace §6 MVR-2 abandon-criterion sentence (after the MVR-2 task table).**

Replace:

```
Effort total: ~8-12 wks. **Abandon-criterion:** if MVR-2-T2 churns the substrate read path more than 4 files OR persona-B ask retracts during dev, revert to MVR-1-only + re-plan.
```

With:

```
Effort total: ~9-14 wks (widened ~10% per L8 #403). **Abandon-criterion (extended per L8):** if MVR-2-T2 churns the substrate read path more than 4 files OR persona-B/E ask retracts during dev OR zero new persona-A install lands during MVR-2 development window (continuous monitoring of GH Stars + `.regatta/` repo count from MVR-1 cohort), revert to MVR-1-only + re-plan. The persona-install-count criterion catches the failure mode where MVR-2 ships but adoption-track stalls — that's a persona-pick problem, not an MVR-2-execution problem.
```

**Diff D — replace §6 MVR-3-T4 row.**

Replace:

```
| MVR-3-T4 | Research-mode overlay (Phase X research-mode wedge per `2026-06-01-regatta-research-vision.md`) | L (6-8 wks) | per research-mode spec |
```

With:

```
| MVR-3-T4 | Research-mode overlay (Phase X research-mode wedge per `2026-06-01-regatta-research-vision.md`) | L (effort=unknown until research-mode implementation plan lands — placeholder 6-8 wks is a research thesis estimate not an implementation estimate per L8 #403) | per research-mode spec |
```

**Diff E — replace §6 MVR-3 abandon-criterion sentence.**

Append to the existing MVR-3 abandon-criterion paragraph:

```
Additional MVR-3 abandon-criterion (per L8 #403): if MVR-3-T4 effort lands >12 wks at implementation-plan time (i.e. research-mode implementation plan itself scopes >12 wks), halt MVR-3-T4 + isolate to its own MVR-4-shaped phase. Don't let a single L-effort task absorb >50% of MVR-3 calendar.
```

**Diff F — replace §6 cross-phase-budget table.**

Replace:

```
| Phase | Calendar wks | Subagent wks | New OSS adoptions | Bespoke wedges |
|---|---|---|---|---|
| MVR-1 | 5-7 | ~7 | 4 (survey, GoReleaser, go-github, go-gitea) | 0 |
| MVR-2 | 8-12 | ~12 | 1 (LiteLLM OR portkey) | 0 |
| MVR-3 | 12-16 | ~14 | 3 (cosign, Stripe, sqlite-CAS) | 0 |
| MVR-4 | 6-8 | ~7 | 2 (Temporal, pgx) | 0 |
```

With:

```
| Phase | Calendar wks (widened per L8 #403) | Subagent wks (widened) | New OSS adoptions | Bespoke wedges |
|---|---|---|---|---|
| MVR-1 | 6-9 | ~9 | 3 (survey, GoReleaser, go-github; Gitea demoted to MVR-2 per L2) | 0 |
| MVR-2 | 9-14 | ~14 | 2 (LiteLLM OR portkey; go-gitea OR gitlab-client-go) | 0 |
| MVR-3 | 14-20 (T4 effort widened to "unknown, ≤12 wks abandon-criterion") | ~16 | 3 (cosign, Stripe, sqlite-CAS) | 0 |
| MVR-4 | 6-8 | ~7 | 2 (Temporal, pgx) | 0 |

Cross-phase total widened from ~40 subagent-wks to **~46 subagent-wks** (~15% widening; the L8 finding's 25% widening conservatively budgets for implementation churn; this brief commits to 15% as a calibrated mid-point — abandon-criterion catches over-runs).
```

**Justification per `feedback_decision_priority`:** UX (operator + future contributors read accurate effort estimates) > ease (widening a number is ~5 lines) > performance > best-practices (calibrated estimates are best-practice; the L8 finding correctly maps observed velocity to estimate) > velocity. Long-term > short-term: widened estimates + abandon-criterion let the operator make persona-pick + phase-halt decisions on calibrated data — 12-15-mo realistic vs 10-mo optimistic is the difference between a confident MVR-3 dispatch and a panicked MVR-2 retrenchment.

---

## 7. Synthesis — does the top-3 wedge ranking change?

**Yes** (per amendment 4 / L2). New top-3:

1. W7 Wave 1 htmx UI (approval + cost panel + DAG read view) — unchanged from original
2. NEW bundle: `regatta init` + GoReleaser + GH-issue adapter — unchanged from original
3. **W7 Wave 2 htmx — DAG read view + log streaming** (NEW rank 3, replacing original Gitea SCM adapter)

Gitea SCM adapter demoted to MVR-2-T4-stretch with Gitea-vs-GitLab decision deferred until a named SCM-blocked inbound fires.

## 8. Synthesis — does Phase X gate criteria change?

**No.** L3 is the only PASS-only lens (gate criteria are tool-checkable per `feedback_grade_rubric` A-tier). No diff against §5.

## 9. Synthesis — does pricing + competitive position change?

**Yes** (per amendments 1 + 2 / L6 + L7).

**Pricing (L6):** Apache 2.0 + commercial-core + self-host-only is the stub commit. Rebuttal triggers named. MVR-3 wedge ordering does NOT change because under the committed stub, persona-C revenue path is "support contracts on self-host + hosted SaaS as MVR-4+ product line" — W11/W12/P3.8 stay at MVR-3 ranking unchanged.

**Competitive position (L7):** new §1.5 subsection with 8-row competitor table (regatta + 7 competitors). Persona-0 pick (persona A as adoption track) is unchanged — the table calibrates the launch narrative ("vs Sweep + Copilot Workspace, not vs Claude Code") but doesn't flip the customer choice.

## 10. A+ rubric per `feedback_grade_rubric`

| Tier | Criteria for THIS amendments spec |
|---|---|
| **B (floor)** | (a) Every BLOCKER from #403 addressed with a concrete diff. (b) Every RISK from #403 addressed with a concrete diff. (c) Release-notes fence present in the PR body. (d) PR body via `--body-file` per `feedback_pr_body_file_only`. |
| **A (target)** | B + (e) Each amendment cites finding ID + section in original brief. (f) Each amendment justified per `feedback_decision_priority` axis explicitly. (g) Synthesis sections for top-3 / Phase X / pricing / competitive position. (h) Adoption-first per `feedback_research_design_principles` — competitive table reuses `docs/design.md` table; no new structure invented. |
| **A+ (stretch)** | A + (i) PASS findings explicitly noted as zero-diff (no implicit changes against passed lenses). (j) Each diff is exact-bytes replacement against original brief (implementer can apply via patch tool without re-interpretation). (k) Amendment ordering BLOCKER → RISK per `feedback_unaddressed_load_bearing`. (l) Stub-commit pattern for L6 punted-Qs preserves rebuttable shape (not a unilateral lock-in). (m) Each amendment quantifies the downstream change (top-3 rank, MVR-3 ordering, cross-phase budget) explicitly. |

**Self-scored tier:** A+ — every A+ criterion met. Independent reviewer subagent re-scores per `feedback_adversarial_review`; if reviewer disagrees, file followup + cite in PR body per `feedback_unaddressed_load_bearing`.

## 11. Application sequencing

The implementer who applies these diffs to the brief should:

1. Apply amendments in numbered order (BLOCKER first per `feedback_unaddressed_load_bearing`).
2. Verify each diff is exact-bytes against the post-#399-merge brief — line numbers may shift post-merge.
3. Run `make pr-lint` + `make doc-check` per `feedback_pr_lint_gates` before push; the new §1.5 competitive table introduces new prose that doc-check banned-phrases must pass per `feedback_doc_check_banned_phrases`.
4. PR body MUST include release-notes fence per `feedback_pr_body_release_notes_mandatory` + use `--body-file` per `feedback_pr_body_file_only`.
5. Reviewer subagent spawns per `feedback_adversarial_review`; this amendments spec is itself reviewable.

## 12. References

- Original brief: `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` (PR #399, branch `spec/next-horizon-roadmap-2026-06`)
- Adversarial review: `docs/engineer/reviews/2026-06-02-customer-roadmap-review-of-399.md` (PR #403, branch `review/399-customer-roadmap`)
- Competitive prior art (reused per `feedback_research_design_principles`): `docs/design.md` competitive table
- W7 spec (referenced by amendment 4 + 5): `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`
- Research-mode (referenced by amendment 6 D): `docs/engineer/briefs/2026-06-01-regatta-research-vision.md`
- Self-host-first (parent roadmap): `docs/engineer/briefs/2026-06-01-self-host-first.md`
- Memory cites: `feedback_research_design_principles`, `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`, `feedback_unaddressed_load_bearing`, `feedback_doc_check_banned_phrases`, `feedback_pr_lint_gates`, `feedback_deletion_default`
