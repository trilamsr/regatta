# Adversarial review of PR #399 — next-horizon customer roadmap

_Reviewer: independent adversarial subagent, 2026-06-02. Scope: 8-lens review of `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` (branch `spec/next-horizon-roadmap-2026-06`). Run per `feedback_adversarial_review` — edge cases + refactor + risk + simplification; never auto-approve. Reviewer is not the author + did not draft the brief._

## Findings summary

| Lens | Topic | Verdict |
|---|---|---|
| L1 | Customer-0 persona realism | RISK |
| L2 | Top-3 wedge selection | RISK |
| L3 | Phase X gate criteria | PASS |
| L4 | Adopt-vs-build coverage | PASS |
| L5 | Cuts list (§7) | RISK |
| L6 | Pricing + monetization | BLOCKER |
| L7 | Competitive position | BLOCKER |
| L8 | Timeline realism | RISK |

**Closing verdict: ADOPT-WITH-AMENDMENTS.** Two BLOCKERs (L6, L7) and four RISKs must be addressed in a follow-up PR before MVR-1 dispatches. The brief is otherwise A-tier — adopt-vs-build coverage is honest, the gate criteria are tool-checkable, and the 4-phase sequence is internally consistent. Amendment list in §"Amendments".

PASS / RISK / BLOCKER count: **1 PASS-only lens, 4 RISK, 2 BLOCKER, 1 PASS** (L3 + L4 = PASS; L1, L2, L5, L8 = RISK; L6, L7 = BLOCKER).

---

## L1 — Customer-0 persona realism (RISK)

**Brief claim:** §1 picks persona A = "OSS maintainer of a single large repo" (langchain, prefect, dagster, temporal, n8n, langflow). Justifies on time-to-value, UX-shape match, marketing flywheel, CC discriminator, Phase-X minimization.

**Adversarial read:** The brief itself admits in §1 closing paragraph that "persona A's WTP is $0 … revenue from persona A is sponsorship-bounded ($50-500/mo at the upper bound)." That is not a customer. That is a **traffic source.** The brief then defers actual revenue to MVR-2 (persona B/D). The implicit thesis — "free users at scale produce paid users" — is a flywheel hypothesis without a stated conversion rate, latency, or attribution telemetry.

Counter-personas the brief dismisses too quickly:

- **Persona B (internal-platform team, 50-500 eng).** Brief grades WTP "high" + says trust bar is "a different product surface." But the §2 blocker list shows G11/G12/G13 — the multi-tenant/Sigstore/billing items — are all listed P2. The "different product surface" framing inflates the gap: tenant_id routing on a single sqlite is one column + one OPA policy update (the brief itself says so in §3 W8). The 60% of the W7 UI work is the same for A and B. Moving to B-first costs less than the brief implies and shortens time-to-revenue from ~Q3 (MVR-2) to ~Q1.
- **Persona E (not in §1) — AI consulting firm / agent-fleet integrator.** Firms like Galileo, Arcee, Sourcegraph-Cody integrators, the long tail of "we ship agent infra for our clients" boutiques. They have the multi-PR-per-day problem on **other people's repos** + are buying tooling now. WTP is $5-20k/mo per seat. The brief silently absorbs them under persona C (platform vendors) but C is gated MVR-3, which means persona E is implicitly deferred 6+ months.

**Cite:** §1 lines 11-22, §1 line 34 ("revenue path"). The persona-A pick is internally consistent with a "marketing-first" play but the brief never says "marketing-first" — it says "customer 0." Conflating reach with revenue is the failure mode.

**Alternative:** explicit two-track framing — persona A as **adoption track** (no revenue ask, retention metric is GH stars + repos-with-`.regatta/`), persona B/E as **revenue track** (LOI metric, $-denominated). Both run in parallel from MVR-1; W7 UI investment serves both because it ships pre-tenant. Persona-A wins seed persona-B trust on `[regatta-dispatched]` PRs visible on the public GH timeline.

**Reopen condition:** brief is amended to (a) rename "persona A = customer 0" to "persona A = adoption track + persona B/E = revenue track" or (b) explicitly accept "we are building free-tier infra for 9 months pre-revenue" and quantify the burn cost in §9.

---

## L2 — Top-3 wedge selection (RISK)

**Brief claim:** §4 ranks 11 wedges; top-3 = W7 Wave 1 htmx + NEW init/GoReleaser/GH-issue bundle + P3.8 SCM Gitea.

**Adversarial read on each pick:**

1. **W7 htmx (rank 1).** PASS on the technology pick — htmx + Go html/template is the right choice for a single-binary product. RISK: the brief assumes the W7 spec is "shipped." Per §6 MVR-1-T1 effort = 2-3 wks, but the spec line cites "#318/#303/#307" as dependencies — those are spec PRs not implementation. Real implementation effort for approval queue + cost panel + DAG read view (Wave 1 only) is 4-6 wks at observed regatta velocity, not 2-3. See L8.

2. **NEW init/GoReleaser/GH-issue bundle (rank 2).** PASS on the bundle composition. **Concern**: the brief frames `regatta init` as a wizard but never specifies what `regatta init` outputs — is it just a `regatta.yaml`, or does it also seed `.regatta/items/` + register a webhook + configure gates.l4.model? If "wizard" means "5-prompt survey" the effort is XS, not S. If it means "discover repo conventions + propose gate config + auto-detect cost cap" the effort is M. The brief picks S (3-5d) without scoping. Implementer will pick the easy interpretation.

3. **P3.8 SCM adapter Gitea-first (rank 3).** **RISK — questionable pick.** Brief justifies "GH shape closer, lower porting cost" — but the second-consumer-proof argument is met equally well by GitLab + GitLab has 30M users vs Gitea's ~100k. The actual customer signal: every named persona-A example user in §1 (langchain, prefect, dagster, temporal, n8n, langflow) is on GitHub. Persona-A adoption is **not** unblocked by Gitea. GitLab unblocks more **persona-B** internal-platform teams (GitLab is the second-most-common SCM in 50-500 eng orgs after GitHub Enterprise). Brief picks Gitea for engineering convenience (closer GH shape), not customer leverage. This violates `feedback_decision_priority` (UX over ease).

**Counter-pick for rank 3:** swap Gitea for **W7 Wave 2 DAG-view + log-streaming** — that's a higher persona-A retention lever (visible "regatta doing work right now"), more than a second-SCM consumer that no current persona-A example user needs. If a second SCM consumer is required for the adapter contract, score it but defer adapter selection to MVR-2 when a persona-B SCM signal arrives.

**Cite:** §3 lines 134-143 (Gitea scoring), §4 line 195 (rank 3 placement).

**Reopen condition:** brief is amended to either (a) demote Gitea to MVR-2 + promote W7 Wave 2 to rank 3, or (b) name one specific persona-A example user blocked by GitHub-only — without that name, Gitea-first is speculation.

---

## L3 — Phase X gate criteria (PASS)

**Brief claim:** §5 names three triggers — 30-day-green (≥10 PRs/day, 90% unattended), external-customer-ask (tiered 1/2/5/10), single→multi-tenant (≥2 distinct tenant_ids over 7d).

**Adversarial read:** all three gates are dashboardable + tool-checkable per `feedback_grade_rubric` A-tier. The thresholds have stated rationale (§5 line 234). The 30-day window is right-sized — short enough that no real customer waits, long enough that flake gets smoothed. The tier-1=1 / tier-2=2 / tier-3=5 / tier-4=10 ladder is conservative + non-arbitrary.

Minor caveat — not a RISK — the brief defines `pr_merge_rate` panel as "extends cost-governor dashboard" without spec'ing the SQL. That's implementer-task scope not brief-scope. Acceptable.

**Verdict:** PASS. This is the strongest section of the brief.

---

## L4 — Adopt-vs-build coverage (PASS)

**Brief claim:** §3 scores ≥2 OSS candidates per wedge per `feedback_research_design_principles`. Zero bespoke wedges across four phases (§6 cross-phase budget summary).

**Adversarial spot-check:**

- W7 htmx: 5 candidates scored (htmx, shadcn, Streamlit, FastAPI+HTMX, plain html/template). Compliant.
- W8 multi-tenant: 4 candidates (OPA, Cedar, SPIRE, Casbin). Compliant.
- W10 Sigstore: 4 candidates (cosign CLI, sigstore-go, GH Action, custom OIDC+Rekor). Compliant.
- W11 blackboard: 5 candidates (sqlite-CAS, CozoDB, golog, Automerge, etcd/Consul). Compliant.
- W12 billing: 5 candidates (Stripe, Lago, Orb, OpenMeter, self-hosted reconciler). Compliant.
- `regatta init`: 4 candidates (survey, huh, pterm, hand-rolled). Compliant.
- Release pipeline: 3 candidates (GoReleaser, hand-rolled Makefile, ko). Compliant.
- SCM adapter: 4 candidates (Gitea, GitLab, Bitbucket, sourcehut). Compliant.

Lock-in column present for every score table. Adoption-cost honest (htmx scored "low," shadcn scored "high" — author isn't pre-loading the answer).

**Verdict:** PASS. `feedback_research_design_principles` compliance is total across the brief.

---

## L5 — Cuts list / anti-roadmap (RISK)

**Brief claim:** §7 lists 10 cuts. Per `feedback_deletion_default` every cut has a reopen condition.

**Adversarial spot-check on each:**

- **Cut 5 "Reviewer-rich PR UI as standalone product"** is partially **contradicted by §6 MVR-2-T1** which lists "W7 Wave 2 htmx — DAG read view + reviewer-rich PR UI." The cut says "rejected" + reopen requires a persona-B/C pilot; the roadmap ships it in MVR-2 unconditionally. **Internal inconsistency.** Either the cut is wrong, or MVR-2-T1 is wrong. Reviewer's guess: the cut intends "reviewer-rich PR UI as a *standalone product, separate from regatta web UI*" — but that's not what the text says. The wording must clarify.
- **Cut 1 "Reviewer-agnostic gate"** — defensible. Anthropic-shaped prompts ARE locked-in; auto-pick across vendors is a 3x QA burden no team will staff. Reopen condition crisp.
- **Cut 7 "Memory/RAG as core"** — defensible. Mem0/Zep/Cognee/LangMem all live; bespoke RAG is YAGNI for customer-0.
- **Cut 9 "Marketplace of reusable plans"** — defensible. Supply-chain risk argument is honest.
- **Cut 10 "Hosted SaaS (regatta cloud)"** — defensible for MVR-1+2 but reopen condition is "persona-B/C asks specifically + LOI" which contradicts §1's persona-B-as-MVR-2 framing. If persona B IS MVR-2's target customer + persona B asks for hosted SaaS, that's the default ask not the exception. Reopen condition needs tightening to "asks for hosted SaaS instead of self-host, AND commits LOI."

**Load-bearing-cut audit:** none of the 10 cuts is load-bearing for persona A. But **cut 5 (reviewer-rich PR UI)** is internally contradicted by MVR-2-T1 — that's the load-bearing inconsistency.

**Cite:** §7 cut 5 vs §6 MVR-2-T1 (line 266).

**Reopen condition:** cut 5 wording clarified to "as a standalone product separate from in-regatta web UI" OR MVR-2-T1 line is rephrased to "reviewer-rich PR UI inside regatta web UI" with explicit cross-ref to cut 5.

---

## L6 — Pricing + monetization (BLOCKER)

**Brief claim:** §9 Q3 ("open-core vs commercial-core") + Q4 ("hosted SaaS or self-host-only") + Q5 ("Apache 2.0 vs BSL vs AGPL") punted to "decision needed by MVR-2 kickoff." §1 line 34 punts persona-A WTP to "operator must decide in §9 Q2."

**Adversarial read:** A roadmap brief that defers four pricing-shaped decisions to a future PR is not a roadmap — it is a research note. The brief explicitly orders four phases (MVR-1 through MVR-4) without knowing:

1. Whether the product is open-core (W8/W10/W12 are paid-only) or commercial-core (all OSS, hosted-only revenue).
2. Whether hosted SaaS is on the table.
3. Which license — Apache 2.0 (max persona-C adoption + zero SaaS-resale protection), BSL (medium adoption + SaaS-resale protection), or AGPL (forces re-OSS of SaaS stacks).

Each license choice **inverts the MVR-3 wedge selection.** Under Apache 2.0, persona C is the dominant revenue path → W11 blackboard + W12 metering + W3.8 LLM-gateway rise to MVR-2. Under BSL, persona C self-hosts → those wedges stay MVR-3. Under AGPL, persona C is repelled → persona B/D dominate.

Building W7 + init + GoReleaser + Gitea (MVR-1 = ~7 subagent weeks) is partly fine pre-decision because those are persona-A-shaped (zero pricing implication for free OSS users). But MVR-2 cannot start until Q3+Q5 land. The brief acknowledges this in §9 ("decision needed by MVR-2 kickoff") but doesn't enforce it as a phase gate. The MVR-2 acceptance gate (§6 line 262) lists "License decided" — that's good. But the **brief itself** should not be ADOPTED with this gap; it should be amended to **resolve Q3+Q5 in §6 before MVR-2 lands**, not at MVR-2 kickoff.

**Severity:** BLOCKER. Without a license decision the WHOLE adopt-vs-build calculus for W11/W12/P3.8 flips. The brief gives the *appearance* of a roadmap; it actually defers the load-bearing strategic call.

**Cite:** §1 line 34, §9 Q2/Q3/Q4/Q5 (lines 343-346).

**Alternative:** add a §9.5 "decision deadline" subsection that orders Q3+Q5 BEFORE MVR-1 dispatch (not MVR-2 kickoff) — because the W7 spec implicitly assumes Apache 2.0 (e.g., embedding htmx assets requires license compatibility check that the brief skipped). Spend one operator week scoring the three licenses against persona A/B/C/D pull, then commit. Even a stub answer ("Apache 2.0 for OSS path + commercial license available on request") is better than four parallel open Qs.

**Reopen condition:** brief is amended to commit a license + open-core-vs-commercial-core direction in §9, even if the answer is "Apache 2.0 + hold open-core decision to MVR-2." Punting both is the failure mode.

---

## L7 — Competitive position (BLOCKER)

**Brief claim:** §1 line 29 names "Claude Code Dynamic Workflows" as the discriminator. §1 line 22 "persona A needs the multi-PR ledger (cost cap + signed audit + queue), not just 'ran an agent in a session.'"

**Adversarial read:** That is the only competitive frame in the entire brief. **The brief contains zero analysis of Devin, Sweep AI, Cursor BugBot, Copilot Workspace, OpenHands, CodeRabbit, or Gitar** — every one of which is a named regatta competitor in `docs/design.md` line "Devin, Cursor agent mode, Copilot Workspace, Aider — each ships an...". The design doc has a full comparison table; the customer-roadmap brief silently inherits zero of that work.

This is a strategic-brief failure because:

1. Persona A (langchain, prefect, etc.) is **already being courted by Sweep AI** (they have public GitHub bot integrations on similar repos). The brief's "marketing flywheel" thesis requires regatta to win against Sweep on a public repo where Sweep is currently posting PRs. The brief never names Sweep.
2. **Copilot Workspace** ships GitHub-native, free for OSS maintainers. Persona A's `regatta init` cost (3-5d operator time + GoReleaser install + `[autonomous]` label setup) competes against Copilot Workspace's "click a button" cost. The brief never compares.
3. **Devin** at $500/mo is the WTP anchor persona B uses to evaluate regatta-the-paid-tier. Brief estimates persona B WTP at $2-10k/mo per team without referencing the Devin-shaped market clearing price.
4. **CodeRabbit** owns the review-quality narrative; the brief's "reviewer-rich PR UI" (MVR-2-T1) collides directly. Brief never names CodeRabbit.

The discriminator-vs-CC argument is fine but **CC is not the most-likely customer-0 competitor.** For persona A, the competitor IS Sweep + Copilot Workspace. For persona B/E, the competitor IS Devin + CodeRabbit + Cursor BugBot. The brief picks the easy strawman (CC owns sessions, regatta owns queues) and skips the hard fight.

**Severity:** BLOCKER. A roadmap without competitive grounding is dispatch-ready only if the competitive landscape is unchanged from the design.md table — which is from MVP-3 era and predates current Sweep/Copilot Workspace push. Regenerate the table fresh against the post-self-host horizon.

**Cite:** §1 line 29 (sole competitive frame); `docs/design.md` competitive table (not referenced in the brief at all).

**Alternative:** add §1.5 "Competitive position" subsection with a 7-column table (regatta + 6 competitors) scored on: (a) persona-A free-tier offer (b) persona-A install friction (c) persona-B WTP anchor (d) cost-cap primitive (e) signed audit chain (f) self-host option (g) license shape. Cite design.md and update the table for 2026-Q2 state. Pull the existing design.md table forward as the starting point — `feedback_research_design_principles` says reuse before rebuild.

**Reopen condition:** brief is amended with a §1.5 explicit competitive table covering at minimum {Devin, Sweep, Cursor BugBot, Copilot Workspace, CodeRabbit, Claude Code Dynamic Workflows}. Without this, the persona-A pick is uncalibrated against the actual market.

---

## L8 — Timeline realism (RISK)

**Brief claim:** §6 cross-phase budget — MVR-1 = 5-7 calendar wks (~7 subagent wks), MVR-2 = 8-12 wks (~12 subagent wks), MVR-3 = 12-16 wks (~14 subagent wks), MVR-4 = 6-8 wks (~7 subagent wks). Zero bespoke wedges.

**Adversarial read:**

- **MVR-1-T1 (W7 Wave 1 htmx) = 2-3 wks.** Optimistic. W7 Wave 1 is "approval queue + cost panel + DAG read view" per §4 rank-1 row. Recent regatta velocity on shipped W6+W8+W9 specs ran 4-6 wks each at the implementation phase (per recent merged PRs visible on `main`). 2-3 wks assumes one implementer at full parallelism, no spec churn, no operator review loops. Realistic: 4-5 wks. **Brief should widen to 3-5 wks** + flag the abandon-criterion (currently ">4 wks") as too tight.
- **MVR-1-T2 (`regatta init` wizard) = 3-5d.** Acceptable if scope is strict 5-prompt survey. RISK per L2 above — scope is undefined.
- **MVR-2-T2 (W8 multi-tenant tenant_id routing) = 2-3 wks.** Acceptable for the column add + OPA-policy update. Does not include migration of historical single-tenant data — brief silently assumes greenfield deploys. If a real persona-B deploy needs migration from a single-tenant regatta install, add 1-2 wks.
- **MVR-3-T4 (research-mode overlay) = 6-8 wks.** This is "per research-mode spec" — the research-mode spec (`docs/wedges/research-mode.md`) is itself a wedge thesis, not an implementation plan. 6-8 wks is a placeholder; honest answer is "unknown until research-mode plan lands." Brief should flag this as L (unknown), not L (6-8 wks).
- **Cross-phase total** = MVR-1 through MVR-4 = ~40 subagent weeks = ~10 months at one implementer or ~5 months at 2x parallel. **Realistic with the L8 widenings = 50-60 subagent weeks** = 12-15 months at 1x. The brief understates by ~25%.

**Abandon-criterion audit:** §6 has per-phase abandon-criteria — solid per `feedback_grade_rubric`. But the criteria are file-count-based ("churns the substrate read path more than 4 files") and time-based (">4 wks"), not customer-signal-based. Better criterion: "if MVR-1 ships and zero persona-A example users from §1 install within 60 days, halt MVR-2 + revisit persona pick." The brief actually says this in §6 line 258 — that's the right criterion. Apply it more broadly to MVR-2 + MVR-3.

**Cite:** §6 line 252 (MVR-1-T1 effort), §6 line 258 (abandon-criterion form), §6 line 304 (cross-phase budget).

**Reopen condition:** brief is amended to widen MVR-1-T1 to 3-5 wks, flag MVR-3-T4 as effort=unknown (not 6-8 wks), and apply the persona-install-count abandon-criterion to MVR-2 + MVR-3.

---

## Amendments — diff for post-#399 follow-up PR

If the operator chooses ADOPT-WITH-AMENDMENTS, the implementer assigned the follow-up PR should apply these specific diffs:

1. **§1.5 NEW subsection — competitive position.** 7-row table scored against {Devin, Sweep AI, Cursor BugBot, Copilot Workspace, CodeRabbit, Claude Code Dynamic Workflows, regatta}. Reuse the design.md table as a starting point + refresh for 2026-Q2. **Resolves L7 BLOCKER.**

2. **§1 paragraph rename** — split "persona A = customer 0" into "persona A = adoption track" and "persona B/E = revenue track" with explicit two-track framing. Add **persona E (AI-consulting/agent-fleet integrator)** to the §1 candidate table. **Resolves L1 RISK.**

3. **§4 rank 3 swap** — demote Gitea SCM adapter to MVR-2, promote W7 Wave 2 DAG read view to rank 3. Alternative: name one specific persona-A example user blocked by GitHub-only and keep Gitea. **Resolves L2 RISK.**

4. **§9 Q3 + Q5 commit** — even a stub commit ("Apache 2.0 + hold open-core decision to MVR-2") is sufficient. Punting all 4 Qs to MVR-2 kickoff is the failure mode. **Resolves L6 BLOCKER.**

5. **§7 cut 5 wording fix** — clarify "reviewer-rich PR UI as standalone product" excludes the W7 Wave 2 reviewer-rich PR UI (which is *inside* regatta), so MVR-2-T1 doesn't contradict §7. **Resolves L5 RISK.**

6. **§6 effort widening** — MVR-1-T1 = 3-5 wks (not 2-3), MVR-3-T4 = effort=unknown (not 6-8 wks), apply persona-install-count abandon-criterion to MVR-2 + MVR-3. **Resolves L8 RISK.**

7. **§5 — no change.** L3 PASS.

8. **§3 — no change.** L4 PASS.

Estimated amendment-PR effort: 1-2 operator-days. Should land before MVR-1-T1 dispatches per the L6 BLOCKER timing.

---

## Closing

**Verdict: ADOPT-WITH-AMENDMENTS.** The brief is structurally sound (clear phases, honest adopt-vs-build, tool-checkable gates, deletion-default cuts list). The amendments are scoped + low-effort. Without the amendments — specifically without L6 (pricing decision) and L7 (competitive table) — the brief understates the strategic uncertainty + ships dispatch-ready material against a market it hasn't measured.

Reviewer subagent signs off pending the 6-amendment follow-up PR.

References:
- Brief under review: `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md`
- Competitive context (referenced but not cited in brief): `docs/design.md`
- Memory cites: `feedback_adversarial_review`, `feedback_research_design_principles`, `feedback_decision_priority`, `feedback_deletion_default`, `feedback_grade_rubric`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`, `feedback_unaddressed_load_bearing`, `feedback_test_godoc_one_line`
