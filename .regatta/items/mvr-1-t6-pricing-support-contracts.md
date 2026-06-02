---
id: MVR-1-T6
title: Pricing - priced-support contract template + price-ladder doc (Option A per #418)
lane: customer
kind: feature
status: planned
gate: mvr-1-kickoff (operator + lawyer time, parallel to MVR-1-T1 critical path)
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md §8 (pricing + monetization) + §10 Q5 (license) + §11 dispatch list
dependencies:
linked_artifact: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md
---

Source brief: the unified next-horizon roadmap at `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §8 (open-core split + Red Hat playbook) + §10 Q5 (license decision) + §11 dispatch list.

Phase-MVR-1 wedge 4 of 4. Names the priced-support surface that bridges Apache-2-core to MVR-3 revenue.

## Scope

Doc the pricing surface for MVR-1/MVR-2 - NOT the implementation. Three deliverables, all prose:

- Priced-support contract template - what persona B/E signs to convert an LOI into a paid customer.
- Price-ladder doc - $5k/mo persona-B baseline + $10-20k/mo persona-E multi-client (anchor numbers from the superseded customer-roadmap chain, preserved as the operator+lawyer starting point).
- OSS-vs-paid capability split note - per §8 of the unified brief, the core is Apache 2.0; the commercial surface combines commercial-core add-ons (W8/W10/W12) plus service-shaped contracts (SLA + private-security-patch channel + configuration review + named-engineer office hours).

Three docs land under `docs/engineer/specs/`:

- `docs/engineer/specs/2026-06-XX-priced-support-contract-template.md`
- `docs/engineer/specs/2026-06-XX-priced-support-price-ladder.md`
- `docs/engineer/decisions/2026-06-XX-customer-roadmap-pricing.md` - the decision record per #408 §9.3, ratifying the Q3 + Q3.1 + Q4 + Q5 stubs.

## Approach

- Reuse-before-rebuild per `feedback_research_design_principles`: the Red Hat / GitLab Core / Elastic-pre-SSPL / HashiCorp Terraform-pre-BSL / Sentry-pre-BSL playbook is the proven shape. Pull the SLA + support-channel structure from a public RHEL or GitLab Core support contract template - operator/lawyer adapts, not invents.
- Lawyer-review required - this is a legal artefact, not a runtime artefact.
- Parallel to MVR-1-T1 critical path - operator/lawyer time, ~1 wk, off the subagent timeline.
- NO impl in this PR - W12 Stripe metering stays at MVR-3 per the unified brief §4 MVR-3-T2.

## Acceptance criteria

- [planned] c1: Priced-support contract template lands as `docs/engineer/specs/2026-06-XX-priced-support-contract-template.md` with the four service surfaces explicit (SLA, private patches, config review, office hours).
- [planned] c2: Price-ladder doc lands with persona-B + persona-E baselines; rebuttal triggers named (when does the ladder shift).
- [planned] c3: Decision record at `docs/engineer/decisions/2026-06-XX-customer-roadmap-pricing.md` ratifies the §10 Q2 / Q3 / Q4 / Q5 stubs of the unified brief.
- [planned] c4: OSS-vs-paid table reproduced in the price-ladder doc - reader sees the Apache-2 core + commercial-core add-ons + service-shaped contract surface split per §8.
- [planned] c5: Lawyer review tagged on the contract template PR before merge; no AI signatures on the legal artefact.
- [planned] c6: Reviewer subagent spawned per `feedback_agent_pr_review`; reviewer comment cleared before automerge.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) Three docs land at the cited paths. (b) Contract template names the four service surfaces. (c) Price ladder reproduces the #418 §3.1 baselines. (d) Decision record ratifies the four stubs. (e) Release-notes fence in PR body. |
| A (target) | B + (f) Contract template cites at least one public precedent (e.g., RHEL / GitLab Core support contract) - reuse-before-rebuild per `feedback_research_design_principles`. (g) Price-ladder doc names rebuttal triggers + a quarterly re-score cadence. (h) Decision record links every stub to §8 + §10 of the unified brief. (i) Lawyer review note inline. |
| A+ (stretch) | A + (j) Adversarial reviewer subagent re-scores against this rubric. (k) Contract template + price-ladder are forkable - a future persona-E variant can be cut from the template without re-drafting. (l) Tracking issues filed for any §8 / §10 leftover (CLA + W7 Wave 2 persona-E billing + decision-record landing) before this PR merges. (m) Effort lands inside the +1 wk parallel budget (operator/lawyer time, off the subagent critical path). |

## Cites

- `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §8 (open-core + Red Hat playbook) + §10 Q5 (license) + §11 dispatch list
- `feedback_research_design_principles` - Red Hat / GitLab / Elastic precedent
- `feedback_decision_priority` - priced support is the bridge from $0 adoption to MVR-3 hosted SaaS
- `feedback_pr_body_release_notes_mandatory` - PR body discipline
