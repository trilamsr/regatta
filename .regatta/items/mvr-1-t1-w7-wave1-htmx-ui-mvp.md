---
id: MVR-1-T1
title: W7 Wave 1 htmx UI MVP - operator dashboard (approval + cost panel)
lane: customer
kind: feature
status: planned
gate: mvr-1-entry (30-day-self-host-green OR named persona-A inbound)
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md §3 (top-3 rank 1) + §4 MVR-1-T1 + §11 dispatch list
dependencies: S2-T1
linked_artifact: docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md
---

Source brief: the unified next-horizon roadmap at `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §3 (top-3 rank 1) + §4 MVR-1-T1 + §11 dispatch list. Effort-band 3-5 wks per the consolidation's MVR-1 sequencing.

Phase-MVR-1 wedge 1 of 4. Smallest UI cut that closes the persona-A mobile-approval + cost-cap-reset + dashboard-URL blockers.

## Scope

Ship the minimum operator dashboard so persona-A maintainer can review + approve + reset cost gates from a phone. Adopt the htmx + Go html/template stack already specced in #318 / #303 / #307; embed CSS via `embed.FS`; zero JS toolchain.

Three surfaces only:

- Approval queue - list pending HITL nodes + decide (approve / deny) inline.
- Cost panel - per-DAG + per-operator USD/token spend, with a reset-the-cap action wired to the cost-governor (W3).
- Read-only "what is regatta doing" home page - last 20 substrate events + currently-running DAGs.

Out of scope for Wave 1: DAG read view + log streaming (deferred to MVR-2-T1 per #408 L2 amendment), reviewer-rich PR UI (W7 Wave 3), multi-tenant scoping (W8).

## Approach

- Reuse spec: `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` is the design source-of-truth. Implementer dispatches against the spec, not from scratch.
- New runtime deps: zero. htmx is a single 14kB JS file served from `embed.FS`.
- Single binary - the regatta binary serves the UI on `regatta serve --ui`.

## Acceptance criteria

- [planned] c1: Approval queue lists pending HITL nodes from substrate; operator can decide each from a mobile browser without a CLI shell.
- [planned] c2: Cost panel reads W3 cost-governor state + exposes a reset-the-cap button gated by OPA RBAC.
- [planned] c3: Home page shows last 20 substrate events + currently-running DAGs; refresh polls every 5s via htmx SSE or polling-trigger.
- [planned] c4: All HTML served from embedded templates; binary size delta under 2MB vs pre-W7 build.
- [planned] c5: Reviewer subagent spawned per `feedback_adversarial_review`; reviewer comment posted on the PR before automerge.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) Three surfaces ship behind one Go binary. (b) Zero new runtime deps beyond htmx + html/template. (c) Mobile-readable layout (no horizontal scroll at 375px). (d) OPA gate enforced on the reset-cap action. (e) Release-notes fence in PR body. |
| A (target) | B + (f) htmx SSE for live event stream OR polling-trigger with backoff. (g) E2E test (chromedp or playwright) covers approve + deny + reset-cap. (h) Span coverage via W6 OTel for every HTTP handler. (i) Per-DAG cost panel renders under 200ms p99 against the dev fixture. |
| A+ (stretch) | A + (j) Accessibility audit (axe-core) reports zero violations on each surface. (k) Per-criterion citation lands on substrate when the HITL decide path runs. (l) Adversarial reviewer subagent re-scores against this rubric + posts on the PR per `feedback_agent_pr_review`. (m) Effort lands inside the L8-widened 3-5 wk band; if drift hits 5 wks, surface a slim-down followup before the 6-wk abandon-criterion fires per #408 §6 Diff B. |

## Cites

- `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §3 + §4 MVR-1-T1 + §11
- `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` (W7 design source-of-truth)
- Prior W7 specs (preserved as background context): #318 / #303 / #307
- `feedback_research_design_principles` - htmx + html/template adopted vs React/shadcn/Streamlit
- `feedback_decision_priority` - mobile approval UX is the load-bearing customer-0 unblock
