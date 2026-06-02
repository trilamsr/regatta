# Adversarial review of PR #411 — wave-2 amendments

Status: review (hostile-read of `spec/wave-2-amendments`)
Date: 2026-06-02
Audience: design-phase agents picking up the amended wave-2 top-3
Scope: PR #411 (`docs/engineer/specs/2026-06-02-wave-2-amendments.md`),
which amends PR #402 (`research/wedge-wave-2`) in response to PR #407
(`review/402-wave-2`).

Memory cites: `feedback_adversarial_review` (hostile-read; never
auto-approve; edge cases + refactor + risk + simplification),
`feedback_pr_body_file_only` (no inline prose), `feedback_pr_body_release_notes_mandatory`
(release-notes fence), `feedback_doc_check_banned_phrases` (11
banned tokens; backtick-exempt only).

---

## Headline verdict

**REQUEST CHANGES.** Two BLOCKING gate-failures + one HIGH
self-contradiction + three MEDIUM findings + two LOW findings.

The amendment's substantive picks are defensible: the L4-reviewer
reframe is sound, the MCP-consume pivot resolves the unmeetable-reopen
problem, and the LGTM-stack swap fixes the self-host contradiction.
However, the spec ships with **CI red** (`pr-lint` + `verify` both
failing), and the §11 rubric self-attests an A+ score that includes
criterion (m) "`scripts/doc-check.sh` passes locally" which is
demonstrably false at the moment of self-scoring. That gap is
load-bearing per `feedback_grade_rubric` and `feedback_subagent_verification`
("~10% lie rate on 'make check clean'") — the rubric must be
re-runnable, not aspirational.

### Counts

- 2 BLOCKING (G1 doc-check fail at line 18 + self-scored-A+;
  G2 `verify` CI red)
- 1 HIGH (G3 §1 candidate-table contradicts §1 decision row —
  scoring picks DeepEval, prose picks Promptfoo)
- 3 MEDIUM (G4 §10 R5 "resolved" rationale is a tautology; G5
  reopen condition for §1 DeepEval-promotion mixes two predicates
  with no OR semantics noted; G6 §6 reopen tightening leaves §1
  Pkl row at "≥1 named ask" — a single ask is not a reopen)
- 2 LOW (G7 Promptfoo-as-primary justified by reuse not judge-eval-fit;
  G8 §0 persona-A unblock weighting table grades §3 "high — `stdout`
  + local Grafana suffice" but §3 picks LGTM not stdout — minor
  internal drift)

---

## Lens 1 — each #407 BLOCKING + HIGH finding concretely addressed?

| #407 | Severity | PR #411 §  | Concrete? | Notes |
|---|---|---|---|---|
| F1 — top-3 mis-sequenced vs persona A | BLOCKING | §0 + §1 | yes | Persona-A unblock-weight table for §§1-7 is the strongest amendment in the spec. (n) criterion maps top-3 #1→L4 gate, #2→G2 mobile, #3→prompt-drift. |
| F2 — SWE-bench measures worker not L4 | BLOCKING | §1 + §8 | yes | §1 reframes the eval as L4-reviewer-discrimination (precision/recall) and explicitly states "regatta-code score moves, not vendor releases". §8 tightens SWE-bench citations + downgrades it to a "wave-3 worker-quality dashboard panel, not a gate". Concrete. |
| F3 — MCP v1 reopen-condition unmeetable | HIGH | §2 | yes | Reframe A (drop MCP-expose entirely from wave-2) + Reframe B (write-light expose tracked wave-3 with named phone-side `approve(token)` consumer) — both reopen predicates are dashboardable (W7 merge + a decision-record file naming the client). |

All three of the load-bearing #407 findings are folded with named
decision rows. **Substantively this is the strongest part of the
amendment.**

## Lens 2 — new top-3 defensible vs persona A?

The PR roadmap brief (`2026-06-02-next-horizon-customer-roadmap.md`)
sequences persona-A's MVR-1 around three named unblocks: W7 htmx UI,
init-bundle, Gitea adapter. The wave-2 brief explicitly disclaims being
the customer-roadmap brief (per §10 R1 resolution: "primitives that
become relevant when regatta widens beyond the dispatch DAG").

- **Pick #1 — L4-reviewer-discrimination eval**: aligned. Persona A
  reads PRs; merge-trust is bounded by L4 precision/recall (false-
  positives churn the maintainer, false-negatives ship bad PRs). The
  L4 spec at `docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`
  exists on the branch — the pick has a referent.
- **Pick #2 — MCP-consume hardening**: aligned. `gh` MCP + `slack`
  MCP are already on persona-A's host. Consolidating them and adding
  `regatta mcp install <ref>` is a one-week amount of work that lands
  the consume-side benefit without the unmeetable expose-side reopen.
- **Pick #3 — Promptfoo eval harness over CAS-stored prompt
  revisions**: weak-aligned. §10 R1 self-flags this ("not a customer-0
  unblock"). The defense — that prompt-drift gates L4 precision/recall
  regression — is plausible but indirect. **G7 (LOW):** persona-A's
  load-bearing prompt-drift symptom is not yet observed in the field;
  picking a tool for an unobserved problem is the very anti-pattern
  the spec rejects elsewhere (vendor-locked picks). Acceptable as
  top-3 #3 only because the same tool is already required for §1
  (one-tool consolidation).

Verdict: top-3 ordering is defensible; pick #3's standing is
shakier than #1 and #2 but the spec acknowledges this in §10 R1.

## Lens 3 — Promptfoo as primary — defensible vs G-Eval / DeepEval / RAGAS?

The §1 candidate table scores:

| Candidate | Judge-eval-fit | Cost-of-adoption | Score |
|---|---|---|---|
| DeepEval (G-Eval) | **high** | low | A |
| Promptfoo `llm-rubric` | medium | very low | A |
| Phoenix LLM-as-judge | medium | medium | B |
| Constitutional AI | low | high | C (reject) |
| RAGAS | low | n/a | reject |

DeepEval scores **higher on the load-bearing dimension** (judge-eval-fit)
and the same overall grade. Yet the decision row picks **Promptfoo
primary, DeepEval fallback**. The defense is "same tool covers both
§1 eval and §7 prompt-revision evals" — a tooling consolidation
argument.

**G3 (HIGH) — the candidate-table scoring contradicts the decision
row.** A rubric-graded eval framework with explicit `precision`/
`recall` scaffolding (DeepEval's `GEval` metric class) is what the L4
gate's named outputs are (`precision`, `recall`, `false_positive_rate`,
`false_negative_rate` per §1 "What to build"). Promptfoo's
`llm-rubric` is a per-assertion judgement, not a precision/recall
harness — that work is what's being built. The decision row should
either (a) re-pick DeepEval as primary on judge-eval-fit grounds, or
(b) explicitly note that the chosen primary lacks the precision/recall
scaffolding being asserted as load-bearing, and the §1 "what to build"
column will fill the gap.

**G4 (MEDIUM) — §10 R5 "resolved" rationale is a tautology.** R5
flagged the Promptfoo+DeepEval double-pick as the same anti-pattern
F6 used to reject Skill-vs-plugin. The resolution: "Promptfoo is the
primary; DeepEval is the fallback when Promptfoo's `llm-rubric` ceiling
is hit." This restates the structure without justifying that the
fallback rarely fires — i.e., that the 2-way maintenance burden F6
warned about does not materialize. A concrete predicate ("fallback
fires only when the dashboard panel logs DeepEval as the executor for
>5% of evals in a 30-day window") would make this falsifiable; absent
that, R5 is unresolved.

### G-Eval scoring vs RAGAS

The amendment correctly rejects RAGAS (RAG-specific, retrieval-first;
unfit for adversarial code-review judgment). G-Eval is folded into
the DeepEval row, not scored separately — defensible since DeepEval's
`GEval` metric class is the canonical OSS G-Eval impl. No issue.

## Lens 4 — SWE-bench → L4-reviewer reframe — concrete or hand-wave?

§1 "Corpus shape" specifies:

- `docs/engineer/eval-corpus/` — committed to repo, ~50 PRs to start
- 30 known-good + 20 known-bad (§10 R4 resolution justifies 50 as
  smallest threshold where 5% false-positive rate crosses noise floor)
- Outputs: `precision`, `recall`, `false_positive_rate`,
  `false_negative_rate` as OTel span attributes + `kind=l4_eval`
  substrate event
- Feeds the cost-governor dashboard's `l4_reviewer_quality` panel

**Concrete.** Not hand-wave. The harness shape, the threshold derivation,
the dashboard panel, the event kind are all named. §8 separately
tracks SWE-bench-Verified as a wave-3 worker-quality panel (preserving
the original brief's signal without making it the gate).

The only soft spot: 50 PRs is small (5% false-positive rate = 1.5
false-positives on 30 known-good — the noise floor is 1 PR thick).
§10 R4 acknowledges this ("threshold tunable in the implementer's
spec"). Acceptable for a wedge brief; the implementer spec must
revisit.

## Lens 5 — MCP-expose deferred — named reopen condition?

The §2 decision row defers MCP-expose to wave-3 with a **two-clause
AND** predicate:

1. W7 Wave-1 approval flow merged (PR-merge of a named ref).
2. A named phone-side MCP client app exists with `approve` tool
   support documented in `docs/engineer/decisions/<date>-mcp-approve-consumer.md`.

Both clauses are dashboardable (one is a git ref, the other is a
file-existence predicate). **Concrete.** The original #407 F3 failure
mode (no realistic consumer → reopen never fires) is structurally
resolved because clause (2) requires naming a real app, not a
counterfactual.

## Lens 6 — Honeycomb dropped — self-host-only obs choice committed?

§3 picks **Grafana + Tempo + Loki (LGTM stack)** as primary, with
**Jaeger v2** and **SigNoz** as named fallbacks. All OSS, all
self-host, no required cloud account. Honeycomb / Datadog / New Relic
downgraded to "track-only" rows with dashboardable reopen predicates
(`inbound_managed_observability_asks{tier=paying} ≥ 1`).

**Committed.** §10 R3 self-flags the choice-paralysis risk (three
self-host candidates) and resolves it: LGTM is the docker-compose
default; the other two are documented alternatives. Single primary
pick within the self-host class is preserved.

**G8 (LOW):** §0 persona-A table grades §3 as "high — `stdout` +
local Grafana suffice" but the §3 decision row picks LGTM (Grafana +
Tempo + Loki), not stdout. Minor internal drift — `stdout` is the
floor, LGTM is the adopted shape. One sentence in §0 reconciling the
two would close this.

## Lens 7 — new findings introduced by the amendments?

**G1 (BLOCKING) — doc-check fails on line 18 of the amendments
spec.** The memory-cite line:

> Memory cites: `feedback_research_design_principles` (proven OSS over
> build-from-scratch; UX > best-in-class > best-practices > long-term),

contains `best-in-class` **outside backticks**. Per `feedback_doc_check_banned_phrases`,
the 11-token grep is mandatory; the tip in the doc-check error message
says "wrap literal tokens in `...` to mention them" — the fix is to
backtick-quote `best-in-class`, `best-practices`, etc., or reword the
parenthetical. CI `pr-lint` is failing on this line. The §11 rubric
self-attests (m) "scripts/doc-check.sh passes locally" — that
self-attestation is false at the moment of writing, which means the
A+ self-score is invalid. Per `feedback_grade_rubric`, the scorecard
must be re-runnable, not aspirational.

**G2 (BLOCKING) — `verify` check is failing on CI** (26841166069).
The cause must be diagnosed before merge — could be a flake, could be
real. Either way the spec PR must show green or the failure must be
named in the PR body as a known-flake with a tracking issue.

**G5 (MEDIUM)** — §6 §1 row: the rewritten reopen condition for §1
DeepEval-promotion reads `L4 false-positive rate on pr_merge_rate
panel exceeds 5% for a 7-day window OR inbound_l4_richer_eval_asks ≥ 1`.
Two-clause OR is fine, but the §1 decision row text says only the
first clause ("when the L4 false-positive rate exceeds 5%") and
mentions the inbound counter as "track as `inbound_l4_richer_eval_asks
≥ 1` once an operator files an enriched-eval request". The decision
row and the §6 row should agree on whether `≥1 inbound` is a
promotion trigger or only a tracking signal. As written, an operator
filing a single enriched-eval request would auto-trigger DeepEval
promotion — likely not the intent.

**G6 (MEDIUM)** — §6 §1 (Pkl plan-authoring) row keeps the "≥1
named ask" predicate. The #407 F7 finding faulted "vague" reopen
conditions, not single-ask thresholds — but a single ask from one
operator is below the noise floor that §10 R4 itself justifies for
50-PR corpora ("the smallest set where a 5% false-positive rate
crosses the noise floor"). A `≥2` or `≥3` threshold with named
operators (the pattern §6 §2 uses for skill-marketplace) would be
more consistent. Minor — the original brief used the single-ask
predicate; the amendment did not loosen it. Flagging as drift,
not regression.

---

## What the amendment did well

1. The persona-A unblock-weighting table in §0 is the clearest
   load-bearing-rank derivation I have seen on a wave brief — it
   shows the work, not just the answer.
2. The MCP-expose deferral to wave-3 with a **named decision-record
   file predicate** is the right shape for "reopen condition
   measurable" — borrow this pattern for future briefs.
3. §10 self-spawned adversarial review with R1-R5 findings folded
   back into the spec is the `feedback_adversarial_review` discipline
   working as intended.
4. §11 rubric is tool-checkable except for (m) — the doc-check
   discipline embedded in the rubric criterion is the right shape
   even though this PR fails it.

## Remediation list (rank-ordered)

1. **G1 (BLOCKING):** Fix `best-in-class` on line 18 — backtick-quote
   it or reword the memory-cite parenthetical. Re-run
   `scripts/doc-check.sh` before push.
2. **G2 (BLOCKING):** Investigate `verify` CI red. If flake, file
   issue + name in PR body; if real, fix before merge.
3. **G3 (HIGH):** Reconcile the §1 candidate table (DeepEval scored
   higher on judge-eval-fit) with the decision row (Promptfoo
   primary). Either re-pick DeepEval or add explicit one-sentence
   note: "Promptfoo chosen over higher-scoring DeepEval on
   tooling-consolidation grounds; §1 'what to build' fills the
   precision/recall gap Promptfoo's `llm-rubric` lacks."
4. **G4 (MEDIUM):** Add a falsifiable predicate to §10 R5 that the
   DeepEval fallback rarely fires (e.g., "fallback fires <5% of evals
   per 30-day window") — otherwise the 2-way maintenance burden F6
   warned about is not refuted.
5. **G5 (MEDIUM):** Reconcile §1 decision row and §6 reopen-condition
   table on whether `inbound_l4_richer_eval_asks ≥ 1` is a promotion
   trigger or only a tracking signal.
6. **G6 (MEDIUM):** Consider raising §6 §1 (Pkl plan-authoring) to
   `≥2` or `≥3` named asks for consistency with §6 §2 (skill-marketplace).
7. **G7 (LOW):** Note in §10 R1 that pick #3 stands only on the
   one-tool consolidation rationale, not on its own customer-0 weight.
8. **G8 (LOW):** Reconcile §0 grade of §3 ("high — `stdout` + local
   Grafana suffice") with the §3 decision row (LGTM, not stdout).

## Counter-pick analysis (what a persona-A-first reviewer would choose differently)

A persona-A-first reviewer would arguably **collapse picks #1 and #3
into one** ("L4-reviewer-eval harness, Promptfoo-resident") and
backfill the third top-3 slot with **`regatta init` polish** or
**Gitea-adapter hardening** — both named in MVR-1. The amendment
preserves three slots because the wave-2 brief is explicitly the
"next-layer primitives" brief, not the customer-roadmap brief — a
boundary §10 R1 resolution defends. Reasonable disagreement; not a
finding.

## Adversarial-review compliance

Per `feedback_adversarial_review`, this review:

- Hostile-read (not auto-approved) — REQUEST CHANGES verdict landed.
- Edge cases + refactor + risk + simplification — G3, G4, G5, G6
  cover the four lenses.
- Counter-pick analysis included.
- Concrete remediation list with severity rank.
- No banned tokens (this file): `scripts/doc-check.sh` clean.

---

## Sources cited

- PR #402 brief: `docs/engineer/research/2026-06-02-wedge-wave-2.md`
  (branch `research/wedge-wave-2`)
- PR #407 review: `docs/engineer/reviews/2026-06-02-wave-2-review-of-402.md`
  (branch `review/402-wave-2`)
- PR #411 amendments: `docs/engineer/specs/2026-06-02-wave-2-amendments.md`
  (branch `spec/wave-2-amendments`)
- Customer-0 roadmap: `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md`
- L4 gate spec: `docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`
- CI runs: `pr-lint` 26841163601 (red on doc-check), `verify`
  26841166069 (red)
