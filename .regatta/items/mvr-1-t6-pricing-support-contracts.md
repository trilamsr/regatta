---
id: MVR-1-T6
title: Pricing - priced-support contract template + price-ladder doc (Option A per #418)
lane: customer
kind: feature
status: planned
gate: mvr-1-kickoff (operator + lawyer time, parallel to MVR-1-T1 critical path)
source_ref: docs/engineer/specs/2026-06-02-customer-roadmap-amend-to-amend.md §1/§3.4/§5 (PR #418, Option A)
dependencies:
linked_artifact: docs/engineer/specs/2026-06-02-customer-roadmap-amend-to-amend.md
---

Source briefs: #418 §1 (Option A pick) + §3.4 (MVR-1-T6 row) + §5 (MVR-1 label updates) + §4 OSS-vs-paid table + #421 verdict.

Phase-MVR-1 wedge 4 of 4 per #421 ADOPT-WITH-AMENDMENTS verdict. Closes the L5 BLOCKER from PR #412: self-host-only + commercial-core + Apache 2.0 leaves MVR-2 with no priced revenue surface unless priced support is named.

## Scope

Doc the pricing surface for MVR-1/MVR-2 - NOT the implementation. Three deliverables, all prose:

- Priced-support contract template - what persona B/E signs to convert an LOI into a paid customer.
- Price-ladder doc - $5k/mo persona-B baseline + $10-20k/mo persona-E multi-client per #418 §3.1.
- OSS-vs-paid capability split note - per #418 §4, every wedge is OSS under Apache 2.0; the commercial surface is service-shaped (SLA + private-security-patch channel + configuration review + named-engineer office hours), NOT feature-shaped.

Three docs land under `docs/engineer/specs/`:

- `docs/engineer/specs/2026-06-XX-priced-support-contract-template.md`
- `docs/engineer/specs/2026-06-XX-priced-support-price-ladder.md`
- `docs/engineer/decisions/2026-06-XX-customer-roadmap-pricing.md` - the decision record per #408 §9.3, ratifying the Q3 + Q3.1 + Q4 + Q5 stubs.

## Approach

- Reuse-before-rebuild per `feedback_research_design_principles`: the Red Hat / GitLab Core / Elastic-pre-SSPL / HashiCorp Terraform-pre-BSL / Sentry-pre-BSL playbook is the proven shape. Pull the SLA + support-channel structure from a public RHEL or GitLab Core support contract template - operator/lawyer adapts, not invents.
- Lawyer-review required - this is a legal artefact, not a runtime artefact.
- Parallel to MVR-1-T1 critical path - operator/lawyer time, ~1 wk, off the subagent timeline per #418 §3.4.
- NO impl in this PR - W12 Stripe metering stays at MVR-3 ordering unchanged per #418 §5.

## Acceptance criteria

- [planned] c1: Priced-support contract template lands as `docs/engineer/specs/2026-06-XX-priced-support-contract-template.md` with the four service surfaces explicit (SLA, private patches, config review, office hours).
- [planned] c2: Price-ladder doc lands with persona-B + persona-E baselines per #418 §3.1; rebuttal triggers named (when does the ladder shift).
- [planned] c3: Decision record at `docs/engineer/decisions/2026-06-XX-customer-roadmap-pricing.md` ratifies Q3 + Q3.1 + Q4 + Q5 stubs per #408 §9.
- [planned] c4: OSS-vs-paid table from #418 §4 reproduced in the price-ladder doc - reader sees every wedge is OSS + commercial surface is service-shaped.
- [planned] c5: Lawyer review tagged on the contract template PR before merge; no AI signatures on the legal artefact.
- [planned] c6: Reviewer subagent spawned per `feedback_agent_pr_review`; reviewer comment cleared before automerge.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) Three docs land at the cited paths. (b) Contract template names the four service surfaces. (c) Price ladder reproduces the #418 §3.1 baselines. (d) Decision record ratifies the four stubs. (e) Release-notes fence in PR body. |
| A (target) | B + (f) Contract template cites at least one public precedent (e.g., RHEL / GitLab Core support contract) - reuse-before-rebuild per `feedback_research_design_principles`. (g) Price-ladder doc names rebuttal triggers + a quarterly re-score cadence. (h) Decision record links every stub to the source #408 / #418 cite. (i) Lawyer review note inline. |
| A+ (stretch) | A + (j) Adversarial reviewer subagent re-scores against this rubric. (k) Contract template + price-ladder are forkable - a future persona-E variant can be cut from the template without re-drafting. (l) The five #418 tracking-issue followups (L3 CLA / W7 Wave 2 / persona-E billing dep / decision-record landing / L8 aggregation) are linked + open as GH issues before this PR merges. (m) Effort lands inside the +1 wk parallel budget per #418 §3.4 (operator/lawyer time, off the subagent critical path). |

## Cites

- #418 §1 Option A pick
- #418 §3.1 (Q3 stub w/ $5k/mo + $10-20k/mo price ladder)
- #418 §3.4 (MVR-1-T6 row)
- #418 §4 (OSS-vs-paid table)
- #418 §5 (MVR-1 label additive)
- #408 §9.3 (decision-record landing)
- #421 verdict (Option A confirmed)
- `feedback_research_design_principles` - Red Hat / GitLab / Elastic precedent
- `feedback_decision_priority` - priced support is the bridge from $0 adoption to MVR-3 hosted SaaS
- `feedback_pr_body_file_only` + `feedback_pr_body_release_notes_mandatory` - PR body discipline
