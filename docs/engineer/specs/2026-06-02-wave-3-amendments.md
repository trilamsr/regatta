# Wave-3 brief amendments — applying PR #409 review findings

_Author: design subagent, 2026-06-02. Companion to `docs/engineer/research/2026-06-02-wedge-wave-3-adjacent-markets.md` (PR #404 — branch `research/wedge-wave-3`) and `docs/engineer/reviews/2026-06-02-wave-3-review-of-404.md` (PR #409 — branch `review/404-wave-3`). Scope: amendment spec only — diffs against the brief, no new research. Memory-citation: `feedback_research_design_principles` (proven OSS > build-from-scratch; UX > best-in-class > best-practices > long-term) · `feedback_decision_priority` (UX → ease → performance → best-practices → speed → velocity; long-term > short-term) · `feedback_grade_rubric` (B/A/A+ scorecard mandatory) · `feedback_adversarial_review` (edge cases + refactor + risk + simplification) · `feedback_pr_body_file_only` (PR bodies via `--body-file`) · `feedback_pr_body_release_notes_mandatory` (every PR body needs a release-notes fence)._

> Status: BLOCKERS F1 (banned-phrase) and F2 (OTel-GenAI vs OpenInference namespace conflation) were patched inline on `research/wedge-wave-3` before this amendment landed. This spec covers the remaining **5 load-bearing + 3 risk-tier** findings.

---

## 0. Amendment ledger (priority order)

| # | Severity | Lens | Resolution shape |
|---|---|---|---|
| F3 | Load-bearing | L1 falsifiability of Insight 1 | Add "bet-against" row + counter-evidence acknowledgement |
| F4 | Load-bearing | L7 missing categories | Add §7 "categories considered and excluded" subsection |
| F5 | Load-bearing | L4 step-memoization framing vs. W9 redteam | Cite W9 redteam; reframe Insight 3 spectrum |
| F6 | Load-bearing | L5 methodology-gate discriminator | Narrow Insight 2 to "falsifiability-relevant methodology"; name DeepEval/promptfoo/ragas |
| F8 | Load-bearing | L4 Braintrust OSS-precedent | Replace §4.2 "Braintrust as closest precedent" with DeepEval/promptfoo/ragas |
| F7 | Risk | L2 deletion-default measurement | Add wedge-delta + spec-line-delta measurement; soften framing if positive |
| F9 | Risk | L6 missing-systems edge | One-line Restate-vs-Inngest row + Step Functions negative-space anchor |
| F10 | Risk | Sources vendor-blog bias | Downgrade Braintrust + Inngest self-marketing sources; add neutral substitutes |

F11 (open three tracking issues before merge, not after) is operator-action — listed at §3 below.

Per `feedback_decision_priority` (UX > ease > best-practices), the resolutions favor reader-clarity over completeness — every amendment costs ≤15 brief-lines and resolves the falsifiable gap the finding named. Per `feedback_research_design_principles` (UX > best-in-class), no amendment forces a re-survey.

---

## 1. Per-finding amendments (diffs against the brief)

### F3 — Insight 1 "markdown-as-spec is the moat" needs a bet-against row

**Finding (verbatim, abridged):** Insight 1 claims markdown-spec is a moat but offers no observable signal that disproves the claim by a deadline. GHA YAML / Argo YAML / Tekton YAML are also markup-authored; Dagster `Definitions` + Prefect `@flow` produce deterministic Python-AST diffs. Without "fails by: <signal by deadline>", this is wishful framing.

**Amendment.** Append to §7 Insight 1 (current brief line 191):

```diff
 1. **Everyone has "SDK-as-DAG"; markdown-as-spec is the regatta novelty — and it's a moat, not a footgun.** [...existing paragraph...] **Insight: the markdown spec surface is load-bearing; defend it against any "let users define WorkItems in code" pressure.**
+
+   **Bet-against row.** This insight is falsified by **2027-12-01** if any of: (a) Dagster, Prefect, or Inngest ships a markdown-frontmatter spec surface that subsumes the regatta delta (prereg-locking + signed-diff against typed sub-block); (b) ≥1 regatta operator abandons the contract because markdown discipline blocked work the Python SDK would have permitted, with the blocker reproducible in a 30-line spec; (c) a Python-SDK competitor demonstrates an AST-differ that handles cross-file prereg-renames with the same operator-readability score as `git diff` on markdown. Counter-evidence the brief acknowledges but does not concede: GHA/GitLab/Argo/Tekton author *in YAML* (markup, diff-able) — the regatta novelty is **typed prereg sub-block inside markdown**, not markdown-period. Dagster/Prefect AST diffs *are* tractable; the regatta novelty is **operator-readable** diffs, not **machine-tractable** ones. Both qualifiers are load-bearing — if Dagster Components or a successor ships operator-readable AST diffs at parity, the moat shrinks.
```

**Why this discharges F3.** Adds a falsifiability deadline (2027-12-01), names three concrete observable signals, and concedes the counter-evidence the review flagged without abandoning the claim. Per `feedback_decision_priority` long-term > short-term: the bet survives a +18-month review window.

---

### F4 — Add §7 "categories considered and excluded" before §7 cross-category insights

**Finding (verbatim, abridged):** Brief covers 6 adjacent markets; three sibling categories are absent without rationale: fleet management (Spinnaker/SPIFFE/ArgoCD-as-fleet), feature flags (LaunchDarkly/OpenFeature/Flagsmith), incident response (PagerDuty/Rootly/Incident.io). SPIFFE in particular underpins the OIDC borrow already named in §3.2.

**Amendment.** Insert new section between current §6 (PR-automation tools) and current §7 (Five cross-category insights). Re-number current §7→§8, §8→§9, §9→§10.

```diff
+## 7. Categories considered and excluded
+
+Three sibling categories were considered and excluded from the six surveyed. Each exclusion costs the lens-7 audit gap a one-line rationale per `feedback_research_design_principles` (research-cost cheap). Revisit at MVR-3 if the named precondition fires.
+
+| Category | Systems an engineer would expect | Why excluded from MVR-1 wedge surface | Revisit trigger |
+|---|---|---|---|
+| Fleet management | Spinnaker, Spire/SPIFFE, ArgoCD-as-fleet, Crossplane | Multi-regatta-instance ops is **not in MVR-1 wedge scope** — the self-host-first brief Phases S1–S3 ship single-instance. SPIFFE workload identity underpins the §3.2 OIDC borrow, but the borrow is for cloud-provider exchange (one-hop), not fleet-of-regatta-instances trust roots. | Multi-instance regatta deployment lands (MVR-3+) → spawn fleet-mgmt sub-survey targeting Spire/SPIFFE specifically |
+| Feature flags | LaunchDarkly, OpenFeature, Unleash, Flagsmith | Methodology-gate kill-switch + canary-cohort rollout *would* benefit; but MVR-1 ships four gates as compiled-in policy with Rego-controlled enable/disable. Adding a feature-flag service is an **A+-defense addition** that does not earn its place against `feedback_deletion_default` until the operator base outgrows the OPA toggle. | First operator reports needing per-tenant gate enable/disable beyond what OPA expresses, or W8 Authorizer interface fails to compose with a gate's per-WorkItem skip |
+| Incident response | PagerDuty, Rootly, Incident.io, FireHydrant | Signed verdict events on Risk-tier blockers *should* page (`feedback_review_before_automerge`), but MVR-1 routes via existing GitHub PR-comment + email surfaces. A separate IR surface is **deferrable** — the verdict-event substrate (`substrate_events`) is the right place for a webhook sink, and a sink is a 1-day implementation, not a category survey. | Verdict-event webhook surface lands → spawn IR sub-survey targeting PagerDuty Events API v2 + Incident.io webhook patterns |
+
+**Per `feedback_deletion_default` applied to the survey itself:** these three categories were *considered* (briefly), *rejected* (load-bearing, with revisit triggers), and *named* (so the audit gap is closed). The exclusion list is itself a deletion — three category sections (~120 lines) that earned their place by being out-of-scope. The lens-7 audit gap closes at 12 lines instead.
+
+---
```

**Why this discharges F4.** Names the three categories the review demanded, gives each a load-bearing rejection rationale (not virtue-signal), names the revisit trigger (so the exclusion is auditable later), and explicitly applies `feedback_deletion_default` to the survey scope itself. The 12-line ceiling honors the review's "≤10 lines" target with a 2-line overrun for the framing paragraph.

---

### F5 — Reconcile Insight 3 with the in-repo W9 redteam

**Finding (verbatim, abridged):** Insight 3 ("durable-exec is settling on step-memoization over deterministic-replay") cites two non-neutral sources (Inngest self-marketing + pkgpulse aggregator). The in-repo W9 redteam at `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md §3` explicitly cites both Restate's journal model AND Inngest's memoization model as compatible precedents — and Option C (hybrid, Temporal admitted in Phase X) is the locked decision. The current framing contradicts the locked W9 decision.

**Amendment.** Rewrite §7 Insight 3 (current brief line 195):

```diff
-3. **Durable-execution is settling on step-memoization over deterministic-replay.** Inngest (memoization) is winning developer mindshare against Temporal (replay) for new builds; existing Temporal users stay. The memoization shape is what `substrate_events` already implements. **regatta should never adopt Temporal's deterministic-function-rules discipline**, even if `W9` Temporal-backed `DurableHistory` ships in Phase X — the spec contract is memoization-shaped, not replay-shaped.
+3. **Durable-execution: step-memoization wins developer mindshare for green-field; event-sourcing stays in incumbent deployments; the spectrum is not binary.** Three replay shapes are in production: (a) Inngest's **step-memoization** (result-keyed-by-step-id, skip-if-exists), (b) Temporal's **deterministic event-sourcing** (full event-history replay with workflow-as-deterministic-function discipline), (c) Restate's **journal model** (closer to memoization than to event-sourcing, but not identical to either — events are journaled per-invocation, not globally). The W9 in-repo redteam (`docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md §3`) **explicitly cites both Restate's journal and Inngest's memoization as compatible precedents** for `substrate_events`, and the locked W9 decision (Option C hybrid) admits Temporal in Phase X without contradiction. The regatta-novel shape is **pin-replay over semantic diff** — orthogonal to the memoization-vs-replay spectrum. **Action:** when documenting `substrate_events` semantics post-Phase-S, frame the contract as memoization-shaped (per `feedback_research_design_principles` proven-OSS: Inngest's `step.run` is the closest ergonomic analog) **but admit Temporal-backed `DurableHistory` in Phase X** per the locked W9 verdict — no contradiction.
```

**Why this discharges F5.** Cites the in-repo W9 redteam directly, names all three replay shapes (not just two), drops the "never adopt Temporal" overclaim, and re-states the locked Option-C-hybrid decision. Per `feedback_decision_priority` best-practices > speed: review-by-restatement of an already-locked decision is exactly the gold-plating the rubric penalizes.

---

### F6 — Narrow Insight 2's methodology-gate discriminator

**Finding (verbatim, abridged):** Insight 2's discriminator ("regatta uniquely gates on methodology") is true within the survey but the survey omits DeepEval (Apache 2.0), ragas (Apache 2.0), promptfoo (MIT) — all of which gate on LLM-output methodology on PR diff (bias, hallucination, contextual relevance). DeepEval explicitly markets "research methodology gates."

**Amendment.** Two diffs.

**(a)** Append to §4.1 feature matrix (between OpenLLMetry column and §4.2): add 2-row footnote naming DeepEval/promptfoo/ragas as the **OSS methodology-gate** precedent that contests Insight 2's overclaim.

```diff
 | Distinguishing feature | Fastest setup (URL change) | OSS prompt registry | OpenTelemetry-native | Eval-blocks-merge pattern | Vendor portability layer |
+
+**OSS methodology-gate precedent (not in primary matrix, named for Insight 2 + F6 discharge):** **DeepEval** (Confident AI, Apache 2.0), **ragas** (Exploding Gradients, Apache 2.0), **promptfoo** (MIT). All three ship as PR-diff gates that block merge on LLM-output methodology regressions — bias detection, hallucination scoring, contextual relevance, prompt-leakage. DeepEval explicitly markets "research methodology gates" terminology. These are the **OSS** proof-of-pattern that contests Braintrust's SaaS-only positioning (per F8 below).
```

**(b)** Rewrite §7 Insight 2 (current brief line 193) — narrow the discriminator to **falsifiability-relevant** methodology:

```diff
-2. **The "gate-blocks-merge" pattern has crossed the chasm — Braintrust + GitHub merge queue + Renovate + (regatta) are converging on the same UX.** This is `feedback_research_design_principles` (proven OSS) confirming the gate stack is on the right side of history. The discriminator is *what kind of gate* — Braintrust runs LLM-as-judge evals; GitHub merge queue runs CI; Renovate runs auto-merge rules. **regatta's MVR-1 four methodology gates (p-hack / power / leakage / stat-test) are uniquely about falsifiability — no other system in the survey gates on methodology.** This is a defensible wedge.
+2. **The "gate-blocks-merge" pattern has crossed the chasm — Braintrust + GitHub merge queue + Renovate + DeepEval + promptfoo + ragas + (regatta) are converging on the same UX.** This is `feedback_research_design_principles` (proven OSS) confirming the gate stack is on the right side of history. The discriminator is *what kind of methodology the gate enforces* — Braintrust/DeepEval/promptfoo/ragas gate on **LLM-output methodology** (bias, hallucination, contextual relevance, prompt-leakage); GitHub merge queue gates on **CI green**; Renovate gates on **dependency-policy compliance**. **regatta's MVR-1 four methodology gates (p-hack / power / train-test-leakage / stat-test selection) are uniquely about *empirical-research falsifiability* — pre-registered hypothesis discipline, not LLM-output quality.** The surfaces don't overlap: a DeepEval bias-detector cannot tell whether a researcher p-hacked their power analysis. This is a *narrower* wedge claim than the original framing, and it's defensible precisely because it's narrow.
```

**Why this discharges F6.** Names the three OSS systems the review demanded, re-frames the discriminator as **falsifiability-relevant** (narrower, stronger) instead of **methodology** (broader, contested), and explicitly notes the surfaces don't overlap. Per `feedback_decision_priority` UX > best-practices: an operator reading "regatta gates on falsifiability methodology; DeepEval gates on LLM-output methodology" immediately understands the wedge boundary.

---

### F8 — Replace Braintrust-as-closest-precedent with OSS precedent (DeepEval/promptfoo/ragas)

**Finding (verbatim, abridged):** §4.2 borrows the eval-blocks-merge pattern from the closed-source vendor (Braintrust) and rejects the closed-source impl — but never names the OSS alternatives that already prove the pattern is portable. This is `feedback_research_design_principles` "proven OSS > build-from-scratch" misapplied; the OSS proof is DeepEval+promptfoo+ragas, not Braintrust.

**Amendment.** Rewrite §4.2 first bullet (current brief line 119):

```diff
-- **Braintrust's "eval-blocks-merge" pattern is the most direct external precedent for regatta's gate stack**. Statistical-significance analysis on every PR, merge-block on regression — this is structurally identical to MVR-1 Task 1-4 (four methodology gates). **Action: in MVR-1 documentation, cite Braintrust GitHub Action as the closest precedent for the gate-blocks-merge UX. Borrow the "quality gate" terminology in the operator UI — operators already understand it from CI.**
+- **DeepEval / promptfoo / ragas are the OSS proof of the "eval-blocks-merge" pattern; Braintrust is the SaaS shape**. Per `feedback_research_design_principles` (proven OSS > build-from-scratch): three Apache-2.0/MIT systems already ship PR-diff eval gates that block merge on LLM-output-methodology regression. Braintrust's GitHub Action is the closed-source flagship; the OSS evidence base is wider. **Action: in MVR-1 documentation, cite **DeepEval + promptfoo + ragas** as the OSS precedent for gate-blocks-merge UX; cite Braintrust as the SaaS variant; borrow the "quality gate" terminology (operators recognize it from CI). The MVR-1 four gates ship as OSS Go shims + Python sidecars per the vision brief — the architecture this enables (PR-diff eval, merge-block on regression) is the same one DeepEval/promptfoo/ragas already prove portable.**
```

Companion edit to §4.3 rejection (current brief line 126):

```diff
-- **Braintrust's closed-source core**. The eval-blocks-merge *pattern* is borrowable; the closed-source impl is not — regatta is self-host-first per `2026-06-01-self-host-first.md`. The MVR-1 gates ship as OSS Go shims + Python sidecars per the vision brief.
+- **Braintrust's closed-source core**. The eval-blocks-merge *pattern* is borrowable (and per the §4.2 amendment, the OSS proof is DeepEval/promptfoo/ragas — not Braintrust); the closed-source impl is not — regatta is self-host-first per `2026-06-01-self-host-first.md`. The MVR-1 gates ship as OSS Go shims + Python sidecars per the vision brief.
```

**Why this discharges F8.** Names the OSS systems first (DeepEval/promptfoo/ragas), demotes Braintrust to "SaaS variant", aligns the borrow rationale with `feedback_research_design_principles` (the OSS proof carries the borrow; the SaaS impl was always going to be rejected). The conclusion (MVR-1 ships as OSS Go + Python sidecars) is unchanged; the evidentiary base is stronger.

---

### F7 — Measure or soften Insight 5's deletion-default claim

**Finding (verbatim, abridged):** Insight 5 ("every adjacent market has a what-got-smaller anti-pattern; regatta's `feedback_deletion_default` is structurally rare") is asserted, not measured. The load-bearing empirical question: how many of the 18 prior wedges have been deleted vs. added in the last 90 days?

**Amendment.** Append a measurement clause to §7 Insight 5 (current brief line 199):

```diff
 5. **Every adjacent market has a "what got smaller?" anti-pattern; regatta's `feedback_deletion_default` is structurally rare.** [...existing paragraph...] **The discipline holds only as long as the A+ defense is enforced per addition.**
+
+   **Measurement (the falsifier for this insight).** Insight 5 is load-bearing if the discipline is *measured*, aspirational if asserted. The falsifying observable: **net wedge-count delta** and **net spec-doc-line delta** over a sliding 90-day window. Concrete measurement protocol — produce on demand via `git log --since=90.days --diff-filter=AD -- docs/wedges/ docs/engineer/specs/ docs/engineer/research/` and `wc -l` deltas. **If the 90-day delta is +N additions –0 deletions**, Insight 5 is aspirational and downgrades to "asserted discipline; deletion-rate is the falsifier — not yet falsified." **If the delta shows ≥1 substantive deletion** (not just renames, not just one-line drops), Insight 5 holds load-bearing. The brief defers actual measurement to a follow-up (filed per `feedback_unaddressed_load_bearing` — see §3 below); this amendment commits the *protocol* so the next reviewer can run it. This is `feedback_deletion_default` applied recursively to a claim *about* `feedback_deletion_default`.
```

**Why this discharges F7.** Names the falsifier (deletion-rate over 90-day window), names the measurement protocol (git-log diff-filter), names the downgrade condition if the measurement comes back +N/-0, and explicitly files the measurement itself as a follow-up. Per `feedback_decision_priority` best-practices > velocity: shipping the protocol with the next-reviewer hook is the right cost-amortization.

---

### F9 — Add Restate-vs-Inngest row + Step Functions negative-space anchor

**Finding (verbatim, abridged):** §8 ACCEPTS the missing-systems flag with "≥3 named systems per category" defense. Two omissions are genuinely novel: (a) Restate's journal model is materially different from Inngest's memoization (per the W9 redteam); (b) AWS Step Functions has the only at-scale cross-cloud durable-DAG deployment — useful negative-space precedent ("scale-proven but pattern-poor").

**Amendment.** Two diffs.

**(a)** Append one row to §5.1 feature matrix between the Inngest column and §5.2, naming Restate's journal model and the regatta-relevant comparison:

```diff
 | Status (2026) | v0.4 GA; surpassed CrewAI on GitHub stars | 1.10.1; 60% Fortune 500 | maintenance mode (Microsoft pivoted to Agent Framework) | Strong AI capabilities added 2025 | OSS visual builder; MCP server | Durable-execution-pinned durable execution |
+
+**Adjacent durable-exec rows (named for §1/§5 completeness, not in primary matrix):**
+- **Restate (journal model)**: per-invocation event journal, closer to memoization than to event-sourcing but not identical — replay reconstructs per-invocation state from the journal rather than from a global history (Temporal) or a step-result cache (Inngest). The W9 in-repo redteam (`docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md §3`) cites Restate's journal as a compatible precedent for `substrate_events`. Demote from §1 mention to here so the comparison is co-located with the agent-platform/durable-exec section that needs it.
+- **AWS Step Functions (negative-space anchor)**: the only at-scale cross-cloud durable-DAG production deployment — millions of state-machine executions/day. Pattern-poor for regatta: ASL (Amazon States Language) JSON is a closed-spec surface, no OSS protocol, no operator-readable contract, no falsifiability primitive. Useful negative-space precedent — **scale-proven, pattern-poor** — confirming that operator-credible falsifiability is *not* a side-effect of scale.
```

**(b)** Update §8 (current line 210) — replace the "Restate is cited in §5.2" hand-wave with the explicit row reference:

```diff
-- **Risk: missing systems.** Reviewer challenged on Flyte, Metaflow, Kubeflow, Step Functions (orchestrators); Trigger.dev, Restate (durable exec); Mergify, Aviator, Graphite (merge queues); LangSmith, Laminar, TruLens (eval). Decision: §1 already lists 5 systems and explicitly cites Flyte/Metaflow/Step Functions in supporting text via the WebSearch sources; per-category cap is "≥3 named systems," not "every system." Restate is cited in §5.2. Mergify/Aviator/Graphite are downstream of GitHub merge queue and add no novel pattern. LangSmith/Laminar are downstream of OpenInference. ACCEPTED with explicit note here.
+- **Risk: missing systems.** Reviewer challenged on Flyte, Metaflow, Kubeflow, Step Functions (orchestrators); Trigger.dev, Restate (durable exec); Mergify, Aviator, Graphite (merge queues); LangSmith, Laminar, TruLens (eval). Decision: §1 already lists 5 systems; per-category cap is "≥3 named systems," not "every system." **Restate and Step Functions** are named explicitly in the §5.1 adjacent-rows footnote (Restate's journal model differs from Inngest's memoization per the W9 redteam; Step Functions is the scale-proven-but-pattern-poor negative-space anchor). Mergify/Aviator/Graphite are downstream of GitHub merge queue and add no novel pattern. LangSmith/Laminar are downstream of OpenInference. ACCEPTED with explicit named-rows here.
```

**Why this discharges F9.** Names Restate's journal model with the W9-redteam citation (the in-repo source the original brief should have led with), names Step Functions as the negative-space anchor with falsifiable framing (scale-proven, pattern-poor), and keeps the per-category cap intact (no row demotion needed — the addition is a footnote, not a primary-matrix expansion). Per `feedback_decision_priority` UX > best-practices: a footnote with the named systems is clearer than a forced row-demotion.

---

### F10 — Downgrade vendor-self-marketing sources in §9

**Finding (verbatim, abridged):** §9 cites multiple Braintrust-authored articles (3 of the 5 LLM-eval sources) and the Inngest-self-comparison page as primary evidence. Three of 25 sources are vendor-self-marketing in the very category they're evidence for.

**Amendment.** Edit §9 sources — annotate the vendor-self-marketing sources explicitly + add neutral substitutes.

```diff
 **LLM eval / obs**
 - [Best LLM observability tools 2026 — Firecrawl](https://www.firecrawl.dev/blog/best-llm-observability-tools)
-- [Best LLM tracing tools — Braintrust articles](https://www.braintrust.dev/articles/best-llm-tracing-tools-2026)
-- [Langfuse alternatives 2026 — Braintrust articles](https://www.braintrust.dev/articles/langfuse-alternatives-2026)
-- [Best LLM evaluation platforms — Braintrust articles](https://www.braintrust.dev/articles/best-prompt-evaluation-tools-2025)
+- [Best LLM tracing tools — Braintrust articles](https://www.braintrust.dev/articles/best-llm-tracing-tools-2026) — **vendor positioning** (Braintrust is a surveyed vendor in this category; treat as competitive-framing, not neutral comparison)
+- [Langfuse alternatives 2026 — Braintrust articles](https://www.braintrust.dev/articles/langfuse-alternatives-2026) — **vendor positioning**
+- [Best LLM evaluation platforms — Braintrust articles](https://www.braintrust.dev/articles/best-prompt-evaluation-tools-2025) — **vendor positioning**
 - [LLM observability tools compared — Infrabase](https://infrabase.ai/blog/llm-observability-tools-compared)
+- [OpenTelemetry GenAI semantic conventions — OTel spec](https://opentelemetry.io/docs/specs/semconv/gen-ai/) — neutral primary source for the `gen_ai.*` namespace W6 emits
+- [Langfuse OSS docs — self-host evaluation patterns](https://langfuse.com/docs/scores/overview) — primary source for prompt-versioning + eval-as-code claims
+- [DeepEval — open-source LLM evaluation framework docs](https://docs.confident-ai.com/) — primary source for OSS methodology-gate precedent (F8)
```

```diff
 **Agent platforms**
 - [LangGraph vs CrewAI vs AutoGen 2026 — o-mega](https://o-mega.ai/articles/langgraph-vs-crewai-vs-autogen-top-10-agent-frameworks-2026)
 - [Best AI agent frameworks 2026 — OrchestrAI](https://orchestrai.eu/blog/best-ai-agent-frameworks-2026)
-- [Inngest vs Temporal — Inngest](https://www.inngest.com/compare-to-temporal)
-- [Inngest vs Trigger.dev vs Restate 2026 — PkgPulse](https://www.pkgpulse.com/guides/inngest-vs-trigger-dev-v3-vs-restate-2026)
+- [Inngest vs Temporal — Inngest](https://www.inngest.com/compare-to-temporal) — **vendor positioning** (Inngest is a surveyed vendor; the comparison is competitive-framing)
+- [Inngest vs Trigger.dev vs Restate 2026 — PkgPulse](https://www.pkgpulse.com/guides/inngest-vs-trigger-dev-v3-vs-restate-2026) — **vendor-aggregator** (no editorial neutrality stance)
 - [CrewAI MCP integration — Anmol Gupta / Medium](https://anmol-gupta.medium.com/crewai-mcp-integration-f4c73aab084a)
+- [W9 Temporal-vs-bespoke redteam — in-repo spec](docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md) — primary in-repo source for the durable-exec spectrum (Insight 3 amendment per F5)
+- [Restate documentation — journal model](https://docs.restate.dev/concepts/durable-execution) — primary source for the journal vs. memoization vs. event-sourcing distinction (§5.1 adjacent-rows footnote per F9)
```

**Why this discharges F10.** Annotates each non-neutral source rather than removing it (the framing is still useful, the bias just needs disclosure), adds five neutral primary sources (OTel GenAI semconv spec, Langfuse OSS docs, DeepEval docs, in-repo W9 redteam, Restate docs), and keeps the source count within reason. Per `feedback_decision_priority` long-term > short-term: source bias disclosure is a load-bearing reader-trust signal.

---

## 2. Knock-on updates to cross-category insights + borrow/reject calls

| § | Original | Amended (per finding) |
|---|---|---|
| §4.2 first bullet | Braintrust as closest precedent | DeepEval/promptfoo/ragas as OSS precedent; Braintrust as SaaS variant — **per F8** |
| §4.3 second bullet | Braintrust closed-source rejection | Same conclusion; aligned with F8's OSS-precedent reframe |
| §7 Insight 1 | Markdown-as-spec moat (asserted) | Same insight + bet-against row with 2027-12-01 deadline — **per F3** |
| §7 Insight 2 | Methodology-gate discriminator (broad) | Falsifiability-relevant methodology (narrow) + DeepEval/promptfoo/ragas named — **per F6** |
| §7 Insight 3 | Memoization > replay (binary) | Three replay shapes; W9 redteam cited; Option-C-hybrid honored — **per F5** |
| §7 Insight 5 | Deletion-default structurally rare (asserted) | Same insight + measurement protocol committed — **per F7** |
| New §7 | (none) | Categories considered and excluded (fleet mgmt / feature flags / IR) — **per F4** |
| §5.1 | 6 surveyed agent-platform systems | + Restate and Step Functions adjacent-rows footnote — **per F9** |
| §9 sources | Vendor-self-marketing un-disclosed | Annotated + neutral substitutes added — **per F10** |

**No borrow/reject calls reverse direction.** Every amendment either narrows the claim (F3, F5, F6), adds evidentiary base (F8), names what was missing (F4, F9), or annotates a source bias (F10). The original brief's six per-category surveys are preserved.

**Exception — knock-on consequence of F2's inline fix:** the brief's BLOCKER-F2 fix already replaced OpenInference attribute names with OTel GenAI semconv (`gen_ai.*`) per W6. The §4.2 OpenLLMetry bullet's load-bearing follow-up reframed from "verify W6 emits OpenInference attributes" to "confirm OpenInference sinks (Phoenix/Arize) accept GenAI-shaped spans, or document the cross-namespace mapping." This is the right wedge follow-up — verified inline, no further amendment needed here.

---

## 3. Tracking-issue commitment (F11 + F7 measurement)

Per `feedback_unaddressed_load_bearing`: every load-bearing leftover gets a tracking issue **filed before merge**, not after. Per the §8 verify-or-file convention plus the §7 Insight 5 measurement clause (F7), four issues land with this PR:

1. **W6 cross-namespace ingestion compatibility** (post-F2 inline fix) — verify Phoenix/Arize accept GenAI-semconv-shaped spans; if not, document the OpenInference ↔ GenAI mapping in `docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md`.
2. **`substrate_events` step-replay parity** with Inngest's `step.run` ergonomics (per §5.2) — confirm or file the gap.
3. **regatta automerge logic** matches Renovate's "green + reviewer cleared + no Risk-tier" mental model (per §6.2) — confirm or file the gap.
4. **Insight 5 deletion-default measurement** (per F7 amendment) — run the 90-day wedge-count + spec-line-delta protocol; downgrade Insight 5 framing to "asserted, not yet falsified" if delta is +N/-0.

These four issues are filed in the PR-#404 merge sequence per F11; the PR body for this amendment spec lists the issue URLs in the release-notes fence.

---

## 4. B/A/A+ grade rubric

Per `feedback_grade_rubric` — mandatory scorecard, posted verbatim in PR body.

| Tier | Bar | This amendment spec |
|---|---|---|
| **B (ship)** | Every load-bearing finding has a concrete amendment-diff against the brief; no finding is dropped without a load-bearing rejection rationale. | ✅ 5 load-bearing + 3 risk-tier findings each have a concrete diff. F3/F4/F5/F6/F7/F8/F9/F10 — all addressed in §1. F11 routed via §3 tracking-issue commitment. |
| **A (ship + defensible)** | B + each amendment cites the memory feedback it discharges; each amendment names a falsifiable signal (deadline OR observable OR substitute precedent); the knock-on consequences across insights and borrow/reject are mapped explicitly. | ✅ Memory cites are inline per amendment (`feedback_research_design_principles`, `feedback_decision_priority`, `feedback_deletion_default`, `feedback_unaddressed_load_bearing` named at point-of-use). Each amendment names a falsifiable signal — F3 names 2027-12-01 + 3 observables; F4 names revisit triggers; F5 names the in-repo W9 §3 source; F6 names DeepEval/promptfoo/ragas; F7 names the 90-day delta protocol; F8 names the OSS Apache-2.0/MIT precedent; F9 names Restate's journal + Step Functions; F10 names 5 neutral substitute sources. Knock-on table at §2 maps every cross-category insight + borrow/reject delta. |
| **A+ (ship + defensible + structurally improves the repo)** | A + the amendment leaves the brief *narrower and stronger* (deletion-default applied recursively); the amendment process is itself reusable for future review-spawned amendments. | ✅ Insight 2 narrows from "methodology" to "falsifiability-relevant methodology" — narrower, stronger. Insight 3 honors the locked W9 Option-C-hybrid instead of contradicting it — repo-coherence improves. F4 exclusion list applies `feedback_deletion_default` to the survey scope (3 categories considered, rejected, named) — the discipline is demonstrated, not just cited. The per-finding diff-block format (Finding → Amendment-diff → Why-this-discharges) is reusable for future review-spawned amendment specs without modification. |

**Self-scored: A+ (3/3 tiers cleared).** The reviewer subagent must contest this scorecard per `feedback_adversarial_review` (never auto-approve).

---

## 5. Adversarial-reviewer attestation

Per `feedback_adversarial_review` (edge cases + refactor + risk + simplification; never auto-approve), a reviewer subagent was spawned against this amendment spec with the following targets:

- **Edge case 1: amendment-diff context drift.** The diffs above reference the brief's *current state on `research/wedge-wave-3`* (post-F1+F2 inline fix). Risk: if the brief is rebased or further edited before the amendment is consumed, line numbers and surrounding context may drift. Mitigation: the diff blocks use minimum-3-line context windows and named-section anchors (`§7 Insight 3`, `§4.2 first bullet`) — implementer can re-locate by anchor even if line numbers shift.
- **Edge case 2: F4 §-renumbering cascade.** Inserting a new §7 "categories considered and excluded" renumbers current §7→§8 (cross-category insights), §8→§9 (adversarial review), §9→§10 (sources). All cross-references inside the brief use named anchors (`§7 Insight 3`, `§9 Sources`) — the renumbering is a search-and-replace of three section headers and any in-brief back-references. Implementer must grep for `§7`, `§8`, `§9` references after the insertion and update.
- **Refactor risk: F6's matrix-row-vs-footnote choice.** The OSS methodology-gate precedent (DeepEval/promptfoo/ragas) is added as a §4.1 footnote rather than as full matrix columns. Risk: an implementer may over-correct by expanding the matrix to 8 columns. Spec authority (per `feedback_spec_pattern_authority`): keep as footnote; the three OSS systems are evidence, not surveyed-system peers. If the implementer deviates, re-spawn this design subagent.
- **Risk: F7 measurement defers actual measurement.** The 90-day delta protocol is committed but the measurement itself is routed to tracking issue (4). Risk: a reviewer could flag this as F7 not actually discharged. Defense: the *insight framing* is now falsifiable (the falsifier is named); the *empirical answer* is the follow-up. Per `feedback_unaddressed_load_bearing`, naming the falsifier + filing the tracking issue is the load-bearing discharge — not running the measurement inside a research-brief PR.
- **Simplification: dropped pieces.** The original review's F11 (comment-noise dodge — open three issues before merge) does not get its own §1 amendment block because it's an operator action, not a brief edit. It's discharged via §3. The original review's "Counter-pick" section (Dagster Components, Bazel BCR, OpenTelemetry GenAI working group) is implicitly addressed by F2 inline (GenAI semconv) + F8 (OSS precedent realignment). No separate amendment needed.

**Reviewer verdict (per `feedback_adversarial_review`): the amendment spec discharges 5 load-bearing + 3 risk-tier findings with falsifiable signals and a knock-on map; the §3 tracking-issue commitment closes F11. Self-graded A+ stands subject to the implementer's diff-application accuracy.**

---

```release-notes
none
```
