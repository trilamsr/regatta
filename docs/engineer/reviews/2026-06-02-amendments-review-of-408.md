# Adversarial review of PR #408 — customer-roadmap amendments

_Reviewer: independent adversarial subagent (second tier), 2026-06-02. Scope: 6-lens review of `docs/engineer/specs/2026-06-02-customer-roadmap-amendments.md` (branch `spec/customer-roadmap-amendments`, PR #408) — the amendments spec that closes the adversarial-review findings in PR #403 against the customer-roadmap brief in PR #399. Run per `feedback_adversarial_review` — edge cases + refactor + risk + simplification; never auto-approve. Reviewer is not the amendments author + did not draft #399 or #403._

## Findings summary

| Lens | Topic | Verdict |
|---|---|---|
| L1 | #403 BLOCKER closure (L6 pricing + L7 competitive) | PASS-with-followup |
| L2 | #403 RISK closure (L1 persona + L2 top-3 + L5 cuts + L8 timeline) | PASS-with-followup |
| L3 | Apache 2.0 license pick defensibility | RISK |
| L4 | Commercial-core scope (what stays OSS vs paid) | RISK |
| L5 | Self-host-only revenue surface during MVR-1/MVR-2 | BLOCKER |
| L6 | New findings introduced by the amendments | RISK |

**Closing verdict: ADOPT-WITH-AMENDMENTS.** The amendments concretely close every #403 BLOCKER + RISK with byte-level diffs (PASS on closure mechanics). Three new findings surface from the amendments themselves: a license-stub one-way door (L3), an unresolved commercial-core capability split (L4), and an internally inconsistent revenue surface during MVR-1/MVR-2 (L5 BLOCKER). The L5 BLOCKER is closable with two new sentences in §9; not a blocker for merging #408 as a spec, but a blocker before any implementer applies the diffs to the brief.

PASS / RISK / BLOCKER count: **0 PASS-only / 3 RISK / 1 BLOCKER / 2 PASS-with-followup**.

---

## L1 — #403 BLOCKER closure (PASS-with-followup)

**Audit of each #403 BLOCKER:**

**L6 (pricing + monetization, BLOCKER in #403).** Amendment §1 replaces #399 §9 wholesale. Commits stub answers for Q3 (commercial-core), Q4 (self-host-only), Q5 (Apache 2.0) with named rebuttal triggers. Q1/Q2 remain open with phase-bounded decision deadlines. **Closure mechanics: PASS.** The stub-commit pattern (rebuttable with explicit signal, not a unilateral lock-in) is the right shape per `feedback_decision_priority` long-term > short-term — A+ criterion (l) per the spec's own self-grading rubric. Followup risks deferred to L3 + L4 + L5 below.

**L7 (competitive position, BLOCKER in #403).** Amendment §2 inserts new §1.5 with 8-row competitive table (regatta + Devin + Sweep AI + Cursor BugBot + Copilot Workspace + CodeRabbit + OpenHands + Claude Code Dynamic Workflows). #403's reopen condition required "{Devin, Sweep, Cursor BugBot, Copilot Workspace, CodeRabbit, Claude Code Dynamic Workflows}" — amendment over-delivers with OpenHands added. Table columns score: free-tier offer, install friction, WTP anchor, cost-cap primitive, signed audit chain, self-host option, license shape, regatta delta. Delta synthesis covers all four personas. **Closure mechanics: PASS.** Reuse of `docs/design.md` table per `feedback_research_design_principles` is cited explicitly.

**Both BLOCKERs concretely closed by the amendments themselves.** No new BLOCKER under this lens.

---

## L2 — #403 RISK closure (PASS-with-followup)

**Audit of each #403 RISK:**

**L1 (persona realism, RISK in #403).** Amendment §3 has three diffs: (A) candidate-persona table extended with persona E (AI-consulting/agent-fleet integrator); (B) "Customer 0 pick" subsection replaced by "Customer-0 split — two tracks running in parallel" (adoption track = persona A, revenue track = persona B OR persona E); (C) burn-cost paragraph replaces the $0-WTP punt. All three #403 reopen conditions ("rename customer 0 to adoption track + revenue track", "add persona E", "quantify burn cost") are met verbatim. **Closure mechanics: PASS.**

**L2 (top-3 wedge selection, RISK in #403).** Amendment §4 swaps rank 3 from "Gitea SCM adapter" to "W7 Wave 2 htmx — DAG read view + log streaming." Five diff blocks update §4 top-3 list, §4 prioritization-matrix rows, §3 P3.8 verdict, §6 MVR-1 task table row T5, and §6 MVR-2 table insertion of MVR-2-T4-stretch. #403's reopen condition (a) "demote Gitea to MVR-2 + promote W7 Wave 2 to rank 3" is met. **Closure mechanics: PASS** with one followup flagged in L6 below (new rank-3 lacks a landed spec).

**L5 (cut #5 wording, RISK in #403).** Amendment §5 replaces #399 §7 cut #5 row with "Reviewer-rich PR UI **as standalone product separate from the in-regatta web UI**" plus a NOTE explicitly excluding the W7 Wave 2 / W7 Wave 3 in-regatta reviewer surface. #403's reopen condition (cut wording clarified OR MVR-2-T1 rephrased) is met by the wording-clarification arm. **Closure mechanics: PASS.**

**L8 (timeline realism, RISK in #403).** Amendment §6 has six diffs: MVR-1-T1 widened 2-3 → 3-5 wks; MVR-1 abandon-criterion threshold widened to >6 wks; MVR-2 abandon-criterion extended with persona-install-count; MVR-3-T4 flagged effort=unknown; MVR-3 abandon-criterion adds the >12 wk halt; cross-phase budget table replaced with widened numbers. **Partial close.** #403 finding said "brief understates by ~25%"; amendment commits to ~15% as "a calibrated mid-point — abandon-criterion catches over-runs." The 25% figure was the reviewer's calibrated estimate against shipped W6/W8/W9 velocity (4-6 wks observed vs 2-3 estimated per task ≈ 50% per-task widening); aggregate widening to 15% is a soft down-revision without engaging the underlying velocity argument. **Closure mechanics: PASS-with-soft-pushback** — the abandon-criterion mechanism does absorb the residual delta, so the practical risk is bounded, but the framing ("we commit to 15% as a calibrated mid-point") asserts calibration without showing the work. Followup: implementer applying the diffs should either cite the per-task widening that aggregates to 15% or accept 25%.

**All four RISKs closed with concrete diffs.** Soft pushback on L8 framing only.

---

## L3 — Apache 2.0 license pick defensibility (RISK)

**Amendment claim:** §9 Q5 stub commits Apache 2.0 for MVR-1. Rebuttal trigger: "One persona-C platform vendor publicly ships regatta-as-a-service without contribution back; revisit BSL at MVR-3. AGPL stays rejected (repels persona-C entirely)."

**Adversarial read:** Apache 2.0 is a **defensible PICK** but the **rebuttal trigger is a one-way door**, and the amendments don't flag the one-way-ness.

Three counter-arguments:

1. **BSL relicense path requires CLA infra regatta does not have.** Sentry, Cockroach, Elastic, HashiCorp, Confluent, Redis — every one of them relicensed from a permissive baseline to BSL or SSPL and every one of them paid a community-pain cost measured in months. The relicense is mechanically feasible only when either (a) a CLA is in place from Day 0 OR (b) every external contributor since Day 0 agrees retroactively. Regatta currently has neither. By MVR-3 (the named "revisit BSL" trigger point) the brief expects multiple external contributors via the persona-A adoption flywheel. Reaching back to relicense each contribution is the Sentry playbook's worst phase. The stub's "revisit BSL at MVR-3" is **trapdoor-shaped**, not trigger-shaped, unless CLA lands alongside the first external PR. The amendment does not mention CLA.
2. **AGPL is not symmetric with BSL for self-host-only roadmaps.** Stub framing: "AGPL stays rejected (repels persona-C entirely)." That framing is correct **only if persona-C revenue is the dominant MVR-3 path.** Under the committed stub (commercial-core + self-host-only + hosted-SaaS-rejected-MVR-1/2), the dominant MVR-2 path is persona-B/E support contracts on self-hosted deployments. AGPL repels nobody on self-host paths because nobody is offering regatta-as-a-service yet. The amendment forecloses AGPL based on a downstream persona-C calculus that doesn't fire until MVR-3, while binding the license choice now.
3. **Sentry-style BSL-on-Day-0 is the proven moat for self-host-with-revenue-protection plays.** Cockroach started BSL Day 0 + relicensed to Apache 2.0 later (one-way, easy direction). Apache 2.0 → BSL is the hard direction. The Sentry/Cockroach playbook says: if you're seriously considering revenue protection, start with the more-restrictive license and relax later when the moat no longer matters. Amendments commit to the asymmetric-risk side.

**Severity:** RISK (not BLOCKER) because the stub is **rebuttable** by construction — operator can edit the §9 decision record before MVR-3 + before external contributions accumulate. But the rebuttal window is bounded by external-contribution accrual + nobody is tracking it.

**Cite:** Amendment §1 (§9.1 Q5 row, §9.4 "Why stub instead of decide").

**Reopen condition:** add a Q5 sub-trigger: "before the first external contributor's PR merges, ratify or revoke the BSL relicense option via CLA decision." Without that sub-trigger, "revisit BSL at MVR-3" is theoretical.

---

## L4 — Commercial-core scope (RISK)

**Amendment claim:** §9 Q3 stub commits "Commercial-core for MVR-1/MVR-2. Everything OSS. Revenue from hosted SaaS + support contracts only."

**Adversarial read:** "Everything OSS" is underspecified. The W7/W8/W10/W11/W12 wedges all ship under MVR-1/2/3 per the original brief. Under commercial-core "everything OSS", the per-capability commercial split is:

| Capability | OSS or commercial under stub? | Amendment cite |
|---|---|---|
| W7 htmx UI | OSS (no commercial fence stated) | unclear |
| W8 multi-tenant tenant_id routing | OSS (no commercial fence stated) | unclear |
| W10 Sigstore attestation | OSS (no commercial fence stated) | unclear |
| W11 blackboard | OSS (no commercial fence stated) | unclear |
| W12 Stripe metering | OSS schema; commercial billing terms? | unclear |
| Support contracts (named revenue path) | commercial, but unpriced | partially specified |
| Hosted SaaS (named revenue path) | rejected MVR-1/MVR-2 | rejected |

The stub says "everything OSS" and names two revenue paths (hosted SaaS + support contracts). Hosted SaaS is **rejected** for MVR-1/MVR-2 per Q4 stub. Support contracts are named but **unpriced** — the amendments don't say what a support contract costs, who sells it, or which capabilities trigger one. The persona-B WTP anchor in §1.5 ($2-10k/mo team seat) is for **regatta the binary**, not for a support contract.

This leaves "commercial-core for MVR-1/MVR-2" with no priced commercial surface during MVR-1/MVR-2. The L1 burn-cost paragraph (Diff C) admits this implicitly: "operator is funding from the lumalabs envelope through MVR-2." So the answer is: **commercial-core during MVR-1/MVR-2 means zero revenue by design; the commercial term applies forward from MVR-3.**

That answer is defensible — but it should be stated plainly in §9, not buried in the L1 burn-cost paragraph two sections away.

**Severity:** RISK. Implementer applying the diffs will read §9 commercial-core stub + assume there's a paid SKU somewhere. The L1 burn-cost paragraph is structurally distant from the §9 commitment.

**Cite:** Amendment §1 (§9.1 Q3 stub row), Amendment §3 Diff C (L1 burn-cost paragraph).

**Reopen condition:** add a sentence to §9.1 Q3 stub: "MVR-1/MVR-2 revenue is $0 by design; the commercial-core term applies forward from MVR-3 when hosted SaaS OR priced support contracts are unblocked." This eliminates the structural distance between the commercial-core stub and the burn-cost acknowledgment.

---

## L5 — Self-host-only revenue surface during MVR-1/MVR-2 (BLOCKER)

**Amendment claim:** §9 Q4 stub commits self-host-only for MVR-1/MVR-2. Combined with Q3 (commercial-core, "Everything OSS"), this yields the surface:

- Adoption track (persona A): $0 WTP by design.
- Revenue track (persona B/E): self-hosted regatta binary.
- Hosted SaaS: rejected for MVR-1/MVR-2.
- Paid enterprise features: rejected under commercial-core ("everything OSS").
- Support contracts: named but unpriced.

**Adversarial read:** the revenue track has **no priced surface during MVR-1/MVR-2**. Persona B/E signs an LOI for what? The original #399 brief (§6 MVR-2 acceptance gate) says "one signed pilot LOI from persona B or D." LOI = letter of intent for **what**? Under the committed stub, the answer is "a future support contract whose price we haven't set" OR "a future hosted-SaaS engagement after MVR-3 unblocks it." Neither is a closeable sale during MVR-2.

This is an **internal inconsistency** introduced by the combination of three amendment commitments:

1. Q3 stub: commercial-core, everything OSS, revenue from "hosted SaaS + support contracts only."
2. Q4 stub: self-host-only for MVR-1/MVR-2; hosted SaaS is rejected during this window.
3. §6 MVR-2 acceptance gate (inherited from #399): "one signed pilot LOI from persona B or D."

Under (1) + (2), the only legal MVR-2 revenue surface is support contracts. Support contracts are not priced + the amendment doesn't name a buyer-decision process for them. So MVR-2 cannot meet its acceptance gate under the committed stubs.

Three resolutions, each requiring a §9 amendment edit:

- **Option A:** Price support contracts in §9.1 (e.g., $5k/mo persona-B baseline) + commit MVR-1-T6 to draft a support-contract template.
- **Option B:** Move hosted SaaS reopen-trigger from "MVR-3+ persona-C ask" to "MVR-2 persona-B/E ask" — narrows the rejection window to match the revenue gate.
- **Option C:** Restate the MVR-2 acceptance gate to drop the LOI requirement under the committed-stub regime: "MVR-2 ships on technical gates only; first paid customer slips to MVR-3 by design."

All three are tractable; the amendment ships none of them. The implementer applying the diffs to the brief will hit this inconsistency on first read.

**Severity:** BLOCKER. The amendments otherwise pass closure mechanics for #403's BLOCKERs (PASS L1 above), but the L6 stub commitments introduce a new internal inconsistency that blocks MVR-2 from meeting its own acceptance gate. Without a §9 resolution, MVR-2 is dispatch-blocked under the committed stub.

**Cite:** Amendment §1 (§9.1 Q3 + Q4 stubs); original brief §6 MVR-2 acceptance gate (inherited, not amended).

**Reopen condition:** apply one of Options A/B/C above + cite the choice in §9. Without that, the amendments structurally block MVR-2 even though they close #403's BLOCKERs.

---

## L6 — New findings introduced by the amendments themselves (RISK)

Four findings beyond L3/L4/L5:

1. **Spec-readiness regression on new rank 3.** Amendment §4 Diff (MVR-1-T5 replacement) reads "W7 Wave 2 htmx — DAG read view + log streaming … spec extension to W7 (file followup; co-design with W7 Wave 1 implementer)." The demoted Gitea task had a landed spec (P3.8 SCM-adapter contract); the promoted W7 Wave 2 has only a file-followup commitment. Per `feedback_unaddressed_load_bearing`, every load-bearing leftover needs a tracking issue. The amendment does not name the issue number. Net spec-readiness regresses from the rank-3 swap even though customer leverage improves. **Followup required:** file an issue for the W7 Wave 2 spec extension before MVR-1-T5 dispatches; cite in PR body.

2. **Persona-E revenue path blocked by MVR-3 feature.** Amendment §3 Diff A persona-E row requires "multi-client tenant isolation" (W8 multi-tenant, MVR-2-T2) and "per-client billing surface" (consulting firms invoice clients separately — that's W12 Stripe metering, MVR-3-T2). MVR-2-T2 lands W8; MVR-2 does not land W12. So a persona-E LOI signed during MVR-2 is blocked on a MVR-3 feature for the buyer's own ops surface. The two-track framing (Diff B) treats persona B and persona E symmetrically, but persona-E's revenue path has a one-phase delay against persona-B's. **Followup required:** either gate persona-E LOI signing on MVR-3 OR pull W12-billing forward to MVR-2 for the per-client billing slice only.

3. **§9.3 decision-record landing has a process hole.** Stub commits land "in `docs/engineer/decisions/2026-06-XX-customer-roadmap-pricing.md` (created when the first stub is ratified) before MVR-1 dispatch." But §9.1 says stubs are committed NOW (in the spec). "Created when the first stub is ratified" lets the implementer skip the file creation. The decision record should be created in the same PR that applies these diffs to the brief, not deferred to a separate "ratification" event. **Followup required:** amendment to §9.3 — "decision record created alongside the diff-application PR, not at a later ratification."

4. **Sub-soft-pushback on L8 timeline framing.** Amendment §6 commits 15% aggregate widening as "a calibrated mid-point" against L8's 25% finding. The L8 finding cited 4-6 wks observed vs 2-3 estimated per task (≈50% per-task widening); aggregating to 15% requires either (a) a tighter aggregation model than per-task averaging OR (b) accepting that some tasks are tighter than the per-task widening implies. The amendment asserts (a) without showing the model. **Closure remains PASS on mechanics** (abandon-criterion absorbs residual delta); soft followup is to either show the aggregation work OR accept 25% as the cross-phase widening.

**Severity:** RISK (cluster of four). None individually is a blocker; the cluster signals the amendments need a 30-min cleanup pass before brief-application.

---

## Synthesis — does PR #408 ship?

**Yes, with amendments to the amendments.** The amendments concretely close every #403 finding (BLOCKER + RISK) with byte-level diffs that an implementer can apply mechanically. The closure mechanics are A+ shape: stub-commit pattern for L6, prior-art reuse for L7, two-track framing for L1, customer-signal-driven rank for L2, wording-disambiguation for L5, abandon-criterion-bounded widening for L8. The amendments self-grade A+ per their own rubric; this reviewer agrees on the closure-mechanics grade.

The amendments **introduce three new findings** that need attention before any implementer applies the diffs to the brief:

- **L3 (RISK):** Apache 2.0 stub is defensible but the BSL-relicense rebuttal trigger is a one-way door without a CLA decision sub-trigger.
- **L4 (RISK):** "Commercial-core, everything OSS" leaves the OSS-vs-paid capability split unspecified.
- **L5 (BLOCKER):** Self-host-only + commercial-core + hosted-SaaS-rejected combo means MVR-2 has no priced revenue surface; MVR-2 acceptance gate (one LOI) cannot be met under the committed stubs.

**L5 is the load-bearing finding.** L3 and L4 are recoverable post-merge. L5 blocks MVR-2 dispatch under the committed-stub regime; one of Options A/B/C in L5 above must land in §9 before the diffs apply to the brief.

---

## Amendment-to-amendments — diff for a #408 follow-up

If the operator chooses ADOPT-WITH-AMENDMENTS on #408, the spec author (or implementer applying these diffs to the brief) should apply:

1. **§9.1 Q3 stub — add MVR-1/MVR-2 revenue-surface sentence.** "MVR-1/MVR-2 revenue is $0 by design; the commercial-core term applies forward from MVR-3 when hosted SaaS OR priced support contracts unblock." **Resolves L4 RISK.**

2. **§9.1 Q4 OR Q3 — one of Options A/B/C from L5 above.** Either price support contracts in MVR-1/MVR-2 (A), pull hosted-SaaS reopen into MVR-2 (B), or restate the MVR-2 acceptance gate (C). **Resolves L5 BLOCKER.**

3. **§9.1 Q5 stub — add CLA sub-trigger.** "Before the first external contributor's PR merges, ratify or revoke the BSL relicense option via CLA decision." **Resolves L3 RISK.**

4. **§9.3 decision-record landing — drop the "ratification" deferral.** "Decision record created alongside the diff-application PR, not at a later ratification." **Resolves L6 finding #3.**

5. **§4 / MVR-1-T5 — name the W7 Wave 2 spec-extension followup issue.** File the issue, cite the number. **Resolves L6 finding #1.**

6. **§3 Diff A persona-E row — flag the W12-billing-surface dependency.** Either gate persona-E LOI on MVR-3 OR pull a W12 slice into MVR-2. **Resolves L6 finding #2.**

7. **§6 Diff F — show the aggregation model OR accept 25%.** Optional; L8 closure is on mechanics regardless. **Resolves L6 finding #4 (soft).**

Estimated amendment-to-amendments effort: 30 operator-minutes for items 1, 3, 4, 5, 7; 2 operator-hours for items 2 and 6 (require a decision, not just a wording fix).

---

## Closing

**Verdict: ADOPT-WITH-AMENDMENTS.** The amendments close #403's findings with mechanical precision (PASS L1 + L2). The new findings (L3 + L4 + L5 + L6 cluster) are scoped + tractable; L5 is the load-bearing one and is closable with a single §9 edit. PR #408 should merge once the L5 BLOCKER's Option A/B/C choice is committed; L3 + L4 + L6 followups can land as either edits-in-#408 or a #408-follow-up PR per `feedback_unaddressed_load_bearing`.

PASS / RISK / BLOCKER count: **0 PASS-only / 3 RISK / 1 BLOCKER / 2 PASS-with-followup (L1 + L2)**.

Reviewer subagent signs off pending the L5 resolution.

References:
- Amendments under review: `docs/engineer/specs/2026-06-02-customer-roadmap-amendments.md`
- Original brief: `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` (PR #399)
- First-tier review: `docs/engineer/reviews/2026-06-02-customer-roadmap-review-of-399.md` (PR #403)
- Memory cites: `feedback_adversarial_review`, `feedback_research_design_principles`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`, `feedback_decision_priority`, `feedback_unaddressed_load_bearing`, `feedback_deletion_default`
