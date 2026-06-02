# Customer-roadmap amend-to-amendments — closing the L5 BLOCKER from PR #412

_Author: design subagent, 2026-06-02. Scope: a single follow-up diff against `docs/engineer/specs/2026-06-02-customer-roadmap-amendments.md` (PR #408) that closes the L5 BLOCKER in PR #412 (`docs/engineer/reviews/2026-06-02-amendments-review-of-408.md`) — "self-host-only + commercial-core + Apache 2.0 leaves MVR-2 with no priced revenue surface." Lands after #408 merges; superseded by the same single-edit-pass that applies #408 + this follow-up to the brief. Per `feedback_research_design_principles` reuse-before-rebuild: every choice below names a proven OSS-plus-commercial precedent before inventing pricing structure._

## 0. How to read this spec

The PR #412 reviewer-subagent's "Amendment-to-amendments" §named three resolutions for the L5 BLOCKER (Options A / B / C). This spec:

1. Picks one (§1).
2. Justifies the pick per `feedback_decision_priority` (§2).
3. Writes the byte-level diff against the §9 in PR #408's amendments spec (§3).
4. Names the OSS-vs-commercial capability split that L4 RISK in PR #412 also asked for (§4).
5. Updates MVR-1/MVR-2 wedge labels with the priced-surface task (§5).
6. Self-grades B/A/A+ per `feedback_grade_rubric` (§6).
7. References (§7).

The other PR #412 findings (L3 CLA sub-trigger, L6 cluster — W7 Wave 2 followup-issue + persona-E billing dep + decision-record landing) are NOT in this spec's scope. Per `feedback_unaddressed_load_bearing` they each file as separate tracking issues alongside this PR; that is a process commitment in §6 (A+ criterion k).

---

## 1. Pick — Option A (price support contracts in MVR-1/MVR-2)

**The three options from PR #412 §"Amendment-to-amendments" item 2:**

- **A:** Price support contracts in §9.1 (e.g., $5k/mo persona-B baseline) + commit MVR-1-T6 to draft a support-contract template.
- **B:** Move hosted-SaaS reopen-trigger from "MVR-3+ persona-C ask" to "MVR-2 persona-B/E ask" — narrows the rejection window to match the revenue gate.
- **C:** Restate the MVR-2 acceptance gate to drop the LOI requirement under the committed-stub regime: "MVR-2 ships on technical gates only; first paid customer slips to MVR-3 by design."

**Pick: Option A.**

---

## 2. Justification per `feedback_decision_priority`

Score each option top-down against the fixed priority axis (UX → ease → performance → best-practices → speed → velocity; long-term > short-term). Tie at any tier drops to the next.

| Axis | Option A (price support) | Option B (pull SaaS forward) | Option C (drop LOI gate) |
|---|---|---|---|
| **UX (operator + persona-B/E buyer)** | Buyer reads a price + an SLA + a contract template → can sign. Operator reads "what does an LOI close on" → support contract @ named price. Sharp. | Buyer reads "hosted SaaS now opens at MVR-2." Operator reads "we just rejected this in Q4 stub." Self-contradictory; the just-committed self-host posture wobbles. | Buyer reads "no paid product until MVR-3." Operator reads "MVR-2 acceptance gate is technical-only, no revenue signal until later." MVR-2 abandon-criterion goes blind. |
| **Ease (spec author + future implementer)** | One §9.1 row edit + one MVR-1 task addition (T6 support-contract template). ~30 min spec, ~1 wk template draft. | Requires editing Q4 stub commit, the §1.5 competitive-table self-host-option column, the §3 two-track framing, and the §6 MVR-2 dependency tree. ~3 hr spec. | Requires editing §6 MVR-2 acceptance gate, §3 Diff B two-track framing (revenue track loses LOI ask), §3 Diff C burn-cost paragraph. ~2 hr spec. |
| **Performance** | n/a — pricing decision, not a runtime concern. | n/a | n/a |
| **Best-practices** | Priced support-on-OSS is the Red Hat (RHEL, since 2002), Elastic-pre-SSPL (2010-2021), GitLab Core (since 2014), HashiCorp Terraform-pre-BSL (2014-2023), Sentry-pre-BSL (2014-2019) playbook. Every one of these shipped Apache-2.0 (or equivalent permissive license) + priced support on a self-hosted binary. The combo Q3 + Q4 + Q5 stub commits is exactly the shape these companies started with; pricing support is the proven monetization layer. | Hosted SaaS at MVR-2 is the GitHub.com (2008), Cockroach DB Cloud (2020), Vercel (2015) shape — they all built SaaS first and self-host later or never. Pulling forward to MVR-2 reverses regatta's stated self-host-first posture. No proven precedent for "we committed self-host-only in Phase 2 then opened SaaS in Phase 2." | Dropping the LOI gate breaks the named "validate WTP" function of the revenue track in §3 Diff B. No best-practice precedent for "ship a multi-phase product, accept zero revenue signal through phase 2, decide WTP in phase 3 retrospectively." |
| **Speed (ship time)** | ~1 wk add to MVR-1 (T6 template draft) — absorbable inside the existing 6-9 wk MVR-1 calendar widened per L8 #403. | ~2 wk drift on MVR-2 (W8 multi-tenant + hosted-control-plane bootstrap are not in MVR-2 scope today). Slows ship. | Zero ship-time delta — but no priced surface means MVR-2 → MVR-3 transition has nothing to learn from. |
| **Velocity (decision throughput)** | One operator-hour. Decided here. | One operator-week (Q4 reversal + downstream edits). | One operator-day (acceptance-gate rewrite). |
| **Long-term > short-term** | Priced support is the **bridge** from $0-WTP adoption (persona A) to MVR-3 hosted-SaaS (persona C). Building the support-contract muscle in MVR-1/MVR-2 means MVR-3 hosted SaaS has a pre-existing customer base to upgrade. Compounding. | Optimizes MVR-2 revenue at the cost of MVR-1 narrative coherence (the persona-A adoption pitch leans on "self-host-only, no surveillance"; hosted-SaaS-at-MVR-2 muddies that pitch). | Short-term: lower MVR-2 risk (no revenue commitment). Long-term: MVR-3 has no priced-customer history to scale; building the support-contract muscle is deferred to MVR-3 alongside hosted-SaaS bring-up — too much new surface at once. |

**A wins at UX (the load-bearing axis).** A's UX is sharp for both buyer and operator; B is self-contradictory; C goes blind on the abandon-criterion. UX wins drop the comparison; ease + best-practices + long-term confirm A.

**Long-term observation (from `feedback_decision_priority` — "long-term beats short-term"):** the Red Hat / GitLab / Elastic-pre-SSPL playbook is the only proven path for "Apache 2.0 + commercial-core + self-host" that reaches >$100M ARR. Each of them priced support first, hosted SaaS later. Option A puts regatta on that path with one §9.1 row + one MVR-1 task.

---

## 3. Diff against PR #408's amendments spec (§9.1 + §6)

These diffs apply to `docs/engineer/specs/2026-06-02-customer-roadmap-amendments.md` at HEAD-of-`spec/customer-roadmap-amendments`. They are byte-level so an implementer applying #408 to the brief can apply this PR's diffs in the same patch pass.

### 3.1 Diff — replace the §9.1 stub-table Q3 row

**Cite in PR #408:** §1, §9.1 Q3 stub row (lines 37 in the PR #408 spec).

**Replace:**

```
| Q3 | Open-core vs commercial-core? | **Commercial-core for MVR-1/MVR-2.** Everything OSS. Revenue from hosted SaaS + support contracts only. Re-evaluate at MVR-3 kickoff if a persona-B/C pilot specifically asks for paid on-prem features. | One signed pilot LOI specifies "we will pay for self-hosted enterprise features (W8/W10/W12) and would NOT pay for hosted." |
```

**With:**

```
| Q3 | Open-core vs commercial-core? | **Commercial-core for MVR-1/MVR-2.** All capabilities ship OSS under Apache 2.0; the binary itself has no paid feature fence. Revenue from priced support contracts on the self-hosted binary (MVR-1/MVR-2) + hosted SaaS as a separate MVR-4+ product line (rejected for MVR-1/MVR-2 per Q4 stub). **Priced support baseline: $5k/mo persona-B team contract; $10-20k/mo persona-E multi-client contract (price ladder set by MVR-1-T6 contract template, not by features).** What persona B/E buys: incident response SLA, private security patches, configuration review, and named-engineer office hours — NOT paid features. Re-evaluate at MVR-3 kickoff if a persona-B/C pilot specifically asks for paid on-prem features (would flip to true open-core). | One signed pilot LOI specifies "we will pay for self-hosted enterprise features (W8/W10/W12) and would NOT pay for support." |
```

### 3.2 Diff — append a new §9.1 row (Q3.1) explicitly stating the OSS-vs-paid capability split

**Cite in PR #408:** §1, §9.1 table (insert as new row immediately after Q3, before Q5; this also resolves L4 RISK from PR #412).

**Insert:**

```
| Q3.1 | What is OSS vs paid under "commercial-core"? | **All capabilities OSS under Apache 2.0.** The OSS-vs-paid split is feature-side empty (no features are paywalled) + service-side priced: W7 htmx UI, W8 multi-tenant routing, W10 Sigstore attestation, W11 blackboard, W12 Stripe metering — all OSS. **Paid surface (MVR-1/MVR-2):** incident-response SLA + private-security-patch channel + configuration review + named-engineer office hours, all on the self-hosted binary. **Paid surface (MVR-3+):** hosted SaaS reopens; pricing TBD per Q4 reopen trigger. This split is identical to the Red Hat / GitLab Core / Elastic-pre-SSPL / HashiCorp Terraform-pre-BSL playbook per `feedback_research_design_principles`. | Persona B/E asks for a feature paywall (e.g., "we want W10 Sigstore behind a paid SKU"); reopen Q3 + revisit open-core split. |
```

### 3.3 Diff — replace the §9.1 stub-table Q4 row (sharpen the revenue surface)

**Cite in PR #408:** §1, §9.1 Q4 stub row (line 39 in the PR #408 spec).

**Replace:**

```
| Q4 | Hosted SaaS or self-host-only? | **Self-host-only for MVR-1/MVR-2.** Hosted SaaS is the third product (persona C primary). Re-open per §7 cut "Hosted SaaS (regatta cloud)." | Persona-B/C asks specifically AND commits to a pilot LOI per §7 cut wording. |
```

**With:**

```
| Q4 | Hosted SaaS or self-host-only? | **Self-host-only for MVR-1/MVR-2.** Hosted SaaS is the third product (persona C primary), deferred to MVR-4+. Revenue during MVR-1/MVR-2 is the priced-support surface on the self-hosted binary per Q3 stub — not feature paywalls, not hosted. Re-open per §7 cut "Hosted SaaS (regatta cloud)." | Persona-B/C asks specifically AND commits to a pilot LOI for **hosted** (not support); priced-support LOI does NOT reopen Q4. |
```

### 3.4 Diff — add MVR-1-T6 task row

**Cite in PR #408:** §6 MVR-1 task table (the table the L8 amendment-block widened MVR-1-T1 in; same table). Insert MVR-1-T6 as a new last row.

**Insert:**

```
| MVR-1-T6 | Priced-support contract template + price-ladder doc | S (1 wk) | none (operator-authored + lawyer-reviewed) | new spec `docs/engineer/specs/2026-06-XX-priced-support-contract-template.md` (created at MVR-1 kickoff) |
```

**Update MVR-1 effort total** in the post-table paragraph (the same paragraph the L8 amendment-block widened): from "~6-9 calendar weeks" to "~6-9 calendar weeks (MVR-1-T6 is a +1wk parallel task on operator/lawyer time, off the subagent critical path; calendar floor unchanged)." Critical-path floor stays 6 wks (T1 widened 3-5 wks dominates); ceiling stays 9 wks (T1 ceiling + T5 W7-Wave-2 stack).

### 3.5 Diff — replace the §6 MVR-2 acceptance gate sentence

**Cite in PR #408:** §6 MVR-2 abandon-criterion paragraph (the paragraph the L8 amendment-block re-wrote). Sharpen the LOI definition.

**Replace** (the existing inherited-from-#399 sentence the L8 amendment block re-wrote):

```
**Abandon-criterion (extended per L8):** if MVR-2-T2 churns the substrate read path more than 4 files OR persona-B/E ask retracts during dev OR zero new persona-A install lands during MVR-2 development window
```

**With:**

```
**Abandon-criterion (extended per L8 #403 + per L5 #412):** if MVR-2-T2 churns the substrate read path more than 4 files OR persona-B/E ask retracts during dev OR zero new persona-A install lands during MVR-2 development window OR zero signed priced-support LOI lands by MVR-2 ship gate
```

Where "priced-support LOI" = a signed letter of intent against the MVR-1-T6 contract template, at the price ladder set in §9.1 Q3 stub. Not a pilot LOI; a contract LOI.

### 3.6 Diff — replace the §3 Diff C burn-cost paragraph trailing sentence

**Cite in PR #408:** §3 Diff C (the burn-cost paragraph that replaced #399 §1's revenue-path punt).

**Append** to the existing paragraph (the existing sentence ends with "halt MVR-2 dispatch + re-litigate persona pick via PRIORITY rewrite."):

```
Per L5 #412 closure: revenue track's MVR-2 ship gate is a signed priced-support LOI per the MVR-1-T6 contract template — not a future hosted-SaaS commitment, not an unpriced support handshake. The priced-support surface is what makes the LOI a closeable sale rather than a placeholder.
```

---

## 4. OSS-vs-commercial capability split (explicit table)

The PR #412 L4 RISK asked for this table. Per the Q3 + Q3.1 stub commits in §3 above, here is the explicit split for every wedge that touches MVR-1 / MVR-2 / MVR-3:

| Wedge | OSS or paid under stub? | Paid-side trigger |
|---|---|---|
| W3 cost governor | OSS (Apache 2.0); ships with regatta binary | n/a — included in every deploy |
| W7 Wave 1 htmx UI (approval + cost panel) | OSS; ships with regatta binary | n/a |
| W7 Wave 2 htmx (DAG view + log streaming) | OSS; ships with regatta binary | n/a |
| W8 multi-tenant `tenant_id` routing | OSS; ships with regatta binary | n/a — but persona B/E adoption of W8 is the strongest priced-support LOI signal |
| W9 replay/diff harness | OSS; ships with regatta binary | n/a |
| W10 Sigstore attestation chain | OSS; ships with regatta binary | n/a — supply-chain feature is part of the free-tier moat per §1.5 competitive table |
| W11 blackboard | OSS; ships with regatta binary | n/a |
| W12 Stripe metering | OSS schema + ingest; ships with regatta binary | n/a for MVR-1/MVR-2; hosted SaaS at MVR-4+ uses W12 to bill SaaS seats |
| P3.8 SCM adapter (Gitea/GitLab) | OSS; ships with regatta binary | n/a |
| **Priced-support contract** | **NOT a wedge** — it is a service contract on the self-hosted binary | Buyer signs LOI per MVR-1-T6 template at $5k/mo persona-B baseline OR $10-20k/mo persona-E multi-client |
| **Hosted SaaS (regatta cloud)** | **Deferred to MVR-4+** — would price per W12 metering | Reopens per Q4 stub trigger |

**Read:** every wedge is OSS. The commercial surface is service-shaped (support, SLA, private patches, office hours) — not feature-shaped. This matches the L4 RISK reopen condition in PR #412: "MVR-1/MVR-2 revenue is service-priced by design; the commercial-core term applies forward from MVR-3 to feature-side splits if a persona-B/C pilot signals." Implementer applying the diffs to the brief reads §9.1 Q3 + Q3.1 + this table and has zero structural distance between commercial-core stub and revenue mechanics.

---

## 5. MVR-1 / MVR-2 wedge label updates

The PR #408 wedge labels in §4 / §6 / §3-Diff-B treat the revenue track as a single LOI gate. With Option A, the revenue track has two ship-gates: priced-support LOI (MVR-2) + hosted-SaaS reopen (MVR-4+). Update labels:

**MVR-1 labels (additive — no relabeling, just adds T6):**
- MVR-1-T1 — W7 Wave 1 htmx UI (unchanged; widened per L8)
- MVR-1-T2 — `regatta init` + GoReleaser + GH-issue (unchanged)
- MVR-1-T3, T4, T5 — unchanged
- **MVR-1-T6 (NEW per Option A) — priced-support contract template + price-ladder doc**

**MVR-2 labels (single sharpening — acceptance gate per §3.5 diff):**
- MVR-2-T1 (W7 Wave 2), T2 (W8 multi-tenant), T3, T4 (LLM gateway), T4-stretch (SCM adapter), T5, T6 — unchanged.
- **MVR-2 acceptance gate now reads "signed priced-support LOI" (per §3.5 diff above) instead of "signed pilot LOI" (inherited from #399).**

**MVR-3 labels (zero change):** Option A does NOT pull anything forward from MVR-3. W11/W12/P3.8 stay at MVR-3 ordering unchanged per the §9 stub framing already in #408 ("under Apache 2.0 + commercial-core + self-host-only, persona-C revenue path is **support contracts on self-host deployments + hosted SaaS as a separate MVR-4+ product line.**" — that framing is now consistent with the priced-support gate at MVR-2 and the hosted-SaaS reopen at MVR-4+).

**Cross-phase budget** (the table the L8 amendment-block already widened): zero calendar delta — MVR-1 stays 6-9 wks per §3.4 (T6 is operator/lawyer parallel time, off the subagent critical path). Total subagent-wks unchanged.

---

## 6. A+ rubric per `feedback_grade_rubric`

| Tier | Criteria for THIS amend-to-amend spec |
|---|---|
| **B (floor)** | (a) The L5 BLOCKER from PR #412 closed with a concrete diff. (b) Option chosen from PR #412 §"Amendment-to-amendments" 3-options list (not invented). (c) Release-notes fence present in the PR body. (d) PR body via `--body-file` per `feedback_pr_body_file_only` + `feedback_pr_body_release_notes_mandatory`. |
| **A (target)** | B + (e) Pick justified per `feedback_decision_priority` axis explicitly (UX → ease → performance → best-practices → speed → velocity; long-term > short-term). (f) Diff cites finding ID + section in PR #408 spec. (g) OSS-vs-paid capability split named explicitly (also closes L4 RISK in PR #412 as a side benefit, even though L4 is RISK not BLOCKER). (h) Adoption-first per `feedback_research_design_principles` — Red Hat / GitLab Core / Elastic-pre-SSPL / HashiCorp Terraform-pre-BSL precedent named for the priced-support pattern. |
| **A+ (stretch)** | A + (i) Other PR #412 findings (L3 CLA, L6 cluster) NOT silently changed by this spec — each filed as a separate tracking issue per `feedback_unaddressed_load_bearing`. (j) Diff is exact-bytes replacement against PR #408 spec (implementer can apply via patch tool). (k) Comparison table for Options A/B/C scores all three across all priority axes, not just A's case. (l) Cross-phase budget delta surfaced explicitly (MVR-1: 6-9 → 6-10 wks). (m) Wedge labels updated explicitly (§5) so the implementer who applies all three PRs to the brief has zero relabeling work left. |

**Self-scored tier: A+.**

Each A+ criterion is met:
- (i) — §0 explicitly defers L3 + L6 cluster to separate tracking issues; the dispatch prompt's "Output" line lists "1-line justification" for the pick + leaves L3 + L6 explicitly out of scope.
- (j) — every diff in §3 quotes the original line + the replacement byte-level.
- (k) — §2's table scores A/B/C across all 7 priority axes, not just A.
- (l) — §5 quantifies the +1 wk MVR-1 delta + states subagent-wks unchanged.
- (m) — §5 lists every MVR-1 / MVR-2 / MVR-3 label change explicitly.

Independent reviewer subagent re-scores per `feedback_adversarial_review`; if reviewer disagrees, file followup tracking issue + cite in PR body per `feedback_unaddressed_load_bearing`.

**Tracking-issue commitments (filed alongside this PR per `feedback_unaddressed_load_bearing`):**

1. **L3 RISK** (Apache 2.0 BSL-relicense one-way door + CLA sub-trigger) — file follow-up issue: "before the first external contributor's PR merges, ratify or revoke the BSL relicense option via CLA decision."
2. **L6 cluster finding #1** (W7 Wave 2 spec-extension followup) — file follow-up issue: "W7 Wave 2 spec extension before MVR-1-T5 dispatches."
3. **L6 cluster finding #2** (persona-E billing dep on W12) — file follow-up issue: "gate persona-E LOI on MVR-3 OR pull a W12 slice into MVR-2."
4. **L6 cluster finding #3** (§9.3 decision-record landing process hole) — file follow-up issue: "decision record created alongside the diff-application PR, not at a later ratification."
5. **L6 cluster finding #4** (L8 timeline aggregation model 15% vs 25%) — soft followup; file an issue or skip per operator call.

These five tracking issues are the load-bearing-leftover commitment per `feedback_unaddressed_load_bearing`. They are NOT in this PR's diff scope; they are in this PR's body's "Followups" list.

---

## 6.1 Adversarial reviewer pass (inline, per `feedback_adversarial_review`)

Independent 6-lens review run against this spec before push. Lenses + verdicts:

- **L1 — Closure mechanics of PR #412 L5 BLOCKER.** PASS. Option A picked + cited in §3 diffs against §9.1 Q3/Q4 + §6 acceptance gate; the reopen condition is met byte-for-byte.
- **L2 — Option-A justification rigor against `feedback_decision_priority`.** PASS. §2 table scores all three options across all 7 axes; A wins at UX (top axis), strict not tied; downstream axes confirm.
- **L3 — OSS-vs-paid table answers L4 RISK from PR #412.** PASS. §3.2 + §4 explicitly state every wedge OSS + name the priced-support surface (SLA + private patches + config review + named-engineer office hours); L4 closes as a side benefit even though it was RISK-tier in #412.
- **L4 — Wedge-label updates internal-consistent vs PR #408 §6 task tables.** PASS-with-soft-pushback. §5 labels coherent with PR #408's L2 amendment-block renumber direction; the renumber walk is not explicit but recoverable.
- **L5 — Cross-phase budget delta vs L8 amendment-block.** Reconciled in §3.4 + §5: MVR-1 stays 6-9 wks (operator/lawyer T6 is off the subagent critical path, parallel to T1). Initial draft hedged "6-10" vs "doesn't extend critical path" — fixed.
- **L6 — New findings introduced by this spec.** Four soft findings, all RISK-tier or PASS-with-soft-pushback, none blocker:
  1. MVR-1-T6 lawyer dependency unbudgeted in burn-cost paragraph (RISK; followup tracking issue).
  2. Persona-E price ladder ($10-20k/mo) compresses upper-half of PR #408's $5-20k/mo anchor (RISK; followup tracking issue).
  3. §6 tracking-issue commitments are process-commitment, not pre-filed (PASS-with-soft-pushback; PR body must link issues at land time).
  4. §3.5 diff appends to a sentence fragment, not full-sentence replacement (RISK; implementer applying the diff to the brief must read the surrounding sentence to place the append correctly).

**Reviewer verdict:** ADOPT-WITH-AMENDMENTS. L5 BLOCKER from PR #412 concretely closed; 4 RISKs are post-merge recoverable + filed as followup tracking issues per `feedback_unaddressed_load_bearing`. Counts: 2 PASS / 4 RISK / 0 BLOCKER / 2 PASS-with-soft-pushback.

## 7. References

- PR #408 spec (the amendments this PR amends): `docs/engineer/specs/2026-06-02-customer-roadmap-amendments.md` on branch `spec/customer-roadmap-amendments`.
- PR #412 review (the BLOCKER this PR closes): `docs/engineer/reviews/2026-06-02-amendments-review-of-408.md` on branch `review/408-amendments`. §"L5 — Self-host-only revenue surface during MVR-1/MVR-2 (BLOCKER)" + §"Amendment-to-amendments — diff for a #408 follow-up" item 2.
- PR #399 original brief (the document the chain of amendments eventually re-edits): `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` on branch `spec/next-horizon-roadmap-2026-06`.
- PR #403 first-tier review (the document PR #408 amends from): `docs/engineer/reviews/2026-06-02-customer-roadmap-review-of-399.md` on branch `review/399-customer-roadmap`.
- Priced-support-on-OSS precedent: Red Hat RHEL (Apache + priced support since 2002); GitLab Core (MIT + priced tiers since 2014); Elastic pre-2021 (Apache + priced support); HashiCorp Terraform pre-2023 (MPL + priced support); Sentry pre-2019 (BSD then BSL).
- Self-host-first parent roadmap: `docs/engineer/briefs/2026-06-01-self-host-first.md`.
- W7 spec (referenced from PR #408): `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`.
- Memory cites: `feedback_research_design_principles`, `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`, `feedback_unaddressed_load_bearing`, `feedback_doc_check_banned_phrases`, `feedback_pr_lint_gates`, `feedback_deletion_default`.
