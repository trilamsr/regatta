---
id: MVR-1-T3
title: P3.8 SCM adapter - Gitea first (second-consumer proof for SCM-adapter contract)
lane: customer
kind: feature
status: planned
gate: mvr-2-stretch (named SCM-blocked persona-A or persona-B inbound; demoted from MVR-1-T5 per #408 L2)
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md §3 SCM-adapter table + amendments §4 (PR #408, L2 demote) + amend-to-amend §3.1/§4 (PR #418, OSS row)
dependencies: MVR-1-T2
linked_artifact: docs/engineer/specs/2026-06-01-adapter-contracts-design.md
---

Source briefs: #399 §3 SCM-adapter score table + §6 MVR-1-T5 + #408 §4 L2 amendment (demote to MVR-2-T4-stretch + decide-at-trigger) + #418 §4 OSS-vs-paid row (Gitea/GitLab both OSS) + #421 verdict (Gitea-vs-GitLab pick verified per #418).

Phase-MVR-1 wedge 3 of 4 per #421 ADOPT-WITH-AMENDMENTS verdict, BUT demoted to MVR-2-stretch per #408 L2 RISK closure. Closes G7 (SCM beyond GitHub) - low severity since most OSS lives on GH; lands when a named inbound fires.

## Scope

Ship a second SCM adapter alongside the existing go-github path, behind the P3.8 SCM-adapter contract, so the contract has a proven second consumer per `feedback_research_design_principles`. Pick at trigger: Gitea (engineering-easier per #399 score table) OR GitLab (broader persona-B fit per #408 L2) - decision deferred until a named SCM-blocked inbound fires.

Two child tasks under this item:

- P3.8 SCM-adapter contract - extract the existing go-github surface in `internal/orchestrator/adapter` (or wherever PR-open / PR-merge / label / comment touches GH directly) into a `schemas.SCMAdapter` interface.
- Second adapter implementation - Gitea (go-gitea/sdk, MIT) OR GitLab (gitlab-org/api/client-go, MIT). Pick at trigger per #408 L2.

## Approach

- Reuse: existing go-github code is the first consumer of the contract; no extraction is bespoke design - the contract shape is dictated by what already ships.
- Gitea-vs-GitLab: per #418 §4 both are OSS rows; per #408 L2 customer-signal-driven (not engineering-convenience). Default if both inbounds fire simultaneously: Gitea (lower porting cost per #399 §3 SCM table).
- Status `planned` on-disk but gate-blocked semantically per `gate:` field above; implementer reads `gate:` + does NOT dispatch until trigger fires.

## Acceptance criteria

- [planned] c1: `schemas.SCMAdapter` interface lands with the methods today's go-github code uses (open-PR, comment, label set, status check); existing GH path implements the interface unchanged externally.
- [planned] c2: Either go-gitea/sdk OR gitlab-client-go implementation passes the same orchestrator integration test the GH adapter passes.
- [planned] c3: `regatta.yaml` accepts an `scm:` block selecting `github | gitea | gitlab`; default unchanged (`github`).
- [planned] c4: The second adapter is the second-consumer-proof of the contract per `feedback_research_design_principles` - documented in the PR body, not just inferred.
- [planned] c5: Reviewer subagent spawned + cleared per `feedback_agent_pr_review`.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) Contract extracted; GH path unchanged externally. (b) Second adapter implementation passes the same integration test. (c) Release-notes fence in PR body. (d) Trigger documented - PR body cites the named inbound that fired the dispatch. |
| A (target) | B + (e) `regatta.yaml` config validates at load time (CUE or `validator/v10`). (f) Both adapters share property tests (round-trip label state, idempotent PR-open). (g) The contract spec lands as `docs/engineer/specs/2026-06-XX-scm-adapter-contract.md` cited from `docs/engineer/specs/2026-06-01-adapter-contracts-design.md`. (h) Per-adapter cost surface tracked (different SCMs price API calls differently). |
| A+ (stretch) | A + (i) Adversarial reviewer subagent re-scores against this rubric. (j) Contract leaves room for a third SCM (Bitbucket / sourcehut) without re-shaping. (k) Trigger-customer pilots the adapter end-to-end on their repo; merged-PR count > 5 within first 30 days of adapter ship. (l) Effort lands inside #408 cross-phase budget (1-2 wks). |

## Cites

- #399 §3 SCM-adapter score table (Gitea ADOPT-FIRST original verdict)
- #408 §4 L2 amendment (DEFER + decide-at-trigger)
- #418 §4 OSS-vs-paid row (Gitea/GitLab both OSS)
- #421 final verdict (Gitea-vs-GitLab pick verified)
- `feedback_research_design_principles` - second-consumer proof for adapter contract
- `feedback_decision_priority` - customer-signal-driven > engineering-convenience
