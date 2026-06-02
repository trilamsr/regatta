# Wave-2 amendments — amend-to-amend (responds to #416 review of #411)

Status: spec (no code, no dispatch)
Date: 2026-06-02
Audience: design-phase agents picking the next dossier batch
Scope: surgical amendments to `docs/engineer/specs/2026-06-02-wave-2-amendments.md`
(branch `spec/wave-2-amendments`, PR #411) folding the 7 remaining findings
from PR #416 review (`docs/engineer/reviews/2026-06-02-wave-2-amendments-review-of-411.md`,
branch `review/411-wave-2-amendments`). G1 (banned-phrase) was already inline-fixed
on `spec/wave-2-amendments` via commit `a780774`; the present spec addresses G2
through G8.

Memory cites: `feedback_research_design_principles` (proven OSS over
build-from-scratch; UX > leading-existing-impl > best-practices > long-term),
`feedback_decision_priority` (UX → ease → performance → best-practices →
speed → velocity; long-term > short-term), `feedback_grade_rubric` (B/A/A+
tool-checkable scorecard), `feedback_adversarial_review` (hostile-read
mandate), `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`.

---

## §0 — Verdict at a glance

| #416 ID | Severity | Fix shape | Net diff |
|---|---|---|---|
| G1 | BLOCKING | already-fixed inline (commit `a780774`) | rephrase `best-in-class` → `leading-existing-impl` on line 18 |
| G2 | BLOCKING | already-fixed by G1 (same root cause) | the failing `verify` job ran `make ci` → `doc-check.sh` → flagged the same banned-phrase hit; the G1 commit clears the `doc-check` step in the verify pipeline |
| G3 | HIGH | **swap primary: DeepEval, fallback: Promptfoo** in §1 decision row | matches the candidate-table judge-eval-fit ranking; removes the scoring/decision contradiction |
| G4 | MEDIUM | rewrite §10 R5 resolution with falsifiable predicate | "DeepEval primary fires for ≥95% of `regatta eval reviewer` invocations; Promptfoo fallback fires <5% of invocations in a 30-day window" |
| G5 | MEDIUM | reconcile §1 decision row + §6 reopen table on `inbound_l4_richer_eval_asks` semantics | demote to tracking-only counter; only the false-positive-rate panel triggers automatic promotion |
| G6 | MEDIUM | raise §6 §1 (Pkl) reopen threshold from `≥1 named ask` to `≥2 named operators with stated use cases` | matches §6 §2 (skill-marketplace) noise-floor pattern |
| G7 | LOW | add §0 footnote that top-3 pick #3 (Promptfoo prompt-revision) stands on tooling-consolidation grounds, not customer-0 weight alone | preserves transparency; #416 explicitly accepted this as LOW once acknowledged |
| G8 | LOW | reconcile §0 grade of §3 ("`stdout` + local Grafana suffice") with §3 decision row (LGTM stack) | one-sentence note: "`stdout` is the floor; LGTM is the adopted shape — operators may start with `stdout`-only and graduate to LGTM" |

---

## §1 — G2 root cause (doc-check banned-phrase, NOT a flake)

The `verify` CI job runs `make ci` → `ci-check` → `check` → first step
`doc-check`. The run that #416 cited (`26841166069`) failed at:

```
doc-check: banned-phrase hit(s) detected:
  - docs/engineer/specs/2026-06-02-wave-2-amendments.md:18:build-from-scratch; UX > best-in-class > best-practices > long-term),
```

This is the same line that tripped `pr-lint`'s doc-check job (#416 G1). The
`a780774` commit reworded `best-in-class` → `leading-existing-impl`, which
clears the banned-phrase grep and unblocks both `pr-lint` and `verify` in a
single fix. `bash scripts/doc-check.sh` against the post-fix file passes:

```
doc-check: 237 markdown link(s) resolve to on-disk files
doc-check: banned-phrase lint clean across 138 markdown file(s) outside exemptions
doc-check: comment-noise diff gate clean (vs origin/main)
doc-check: test-godoc length gate clean (vs origin/main)
```

No additional fix needed for G2 beyond the G1 commit. The post-G1 CI rerun
on `spec/wave-2-amendments` is the verification artifact.

---

## §2 — G3 fix: swap primary to DeepEval

### Why DeepEval is the right primary

Per `feedback_decision_priority` the priority order is **UX → ease →
performance → best-practices → speed → velocity**. The L4-reviewer eval's
"UX" is the discrimination signal the operator needs out of the tool:
precision, recall, false-positive-rate, false-negative-rate. DeepEval's
`GEval` metric class is purpose-built for exactly this shape — rubric-graded
LLM-as-judge with explicit precision/recall accounting. Promptfoo's
`llm-rubric` assertion is a per-assertion judgement; the precision/recall
math is something the implementer must hand-roll on top.

The original PR #411 decision row picked Promptfoo as primary on
"tooling-consolidation grounds" (the same tool already adopted for top-3
pick #3 in §7 for prompt-revision evals). Per #416 G3 this contradicts the
candidate table itself, which scores DeepEval higher on the load-bearing
dimension. Tooling consolidation is an `ease` argument — second in the
priority order — and the L4-reviewer eval's correctness (what UX the
operator gets) outranks it.

### Re-scored candidate table (replaces §1 of PR #411 amendment)

The original table's columns (judge-eval-fit, cost-of-adoption) are
preserved. Per #416 G3 a third dimension was missing — **CLI/CI ergonomics**
— which is what Promptfoo's tooling-consolidation pitch was implicitly
trading on. Surfacing it as an explicit column makes the trade visible
rather than buried in the prose.

| Candidate | Class | License | Self-host | Judge-eval fit | CLI / CI ergonomics | Cost-of-adoption | Score |
|---|---|---|---|---|---|---|---|
| **DeepEval (G-Eval rubric)** | Python eval framework, LLM-judge primitives | Apache-2.0 | yes (`pip install deepeval`, runs local) | **high** — `GEval` metric class ships precision/recall scaffolding | medium — pytest-style runner; CI-runnable via `deepeval test run`; less ergonomic than YAML | low — Python subprocess shells out from Go gate harness | **A+** |
| Promptfoo `llm-rubric` assertion | YAML CLI eval, JS runtime | MIT | yes (`npm install promptfoo`) | medium — `llm-rubric` is per-assertion; precision/recall is hand-rolled | **high** — single YAML file, `promptfoo eval` one-shot CI command, JSON output trivially parsed | very low — already adopted for §7 prompt-revision evals | A |
| Phoenix LLM-as-judge | Arize OSS eval+observability | Elastic-2.0 (OSS) | yes (self-host UI + storage) | medium | low — UI-first; weaker CI story | medium — adds UI dep | B |
| Constitutional AI eval (Anthropic) | Critique + revise loop | research-paper, no canonical OSS impl | no canonical self-host | low | n/a | high | C (reject) |
| RAGAS | RAG-specific eval framework | Apache-2.0 | yes | low (retrieval-oriented) | medium | n/a | reject |

The explicit CLI/CI ergonomics column shows Promptfoo's actual edge
(it ships YAML + one-shot CLI) but also shows that the edge does not move
DeepEval below it on the load-bearing dimension — DeepEval still wins on
judge-eval-fit. Tier order: DeepEval **A+** (wins the load-bearing column
+ ties or wins the others) > Promptfoo **A** (loses judge-eval-fit by one
tier, wins CLI by one tier) > Phoenix B > reject pile.

### Replacement decision row for §1 of PR #411

| What to adopt | What to build | Pass + reopen condition |
|---|---|---|
| **DeepEval `GEval` as the primary L4-reviewer eval harness** (purpose-built precision/recall scaffolding, Apache-2.0, self-host via `pip install deepeval`, Python subprocess from Go gate). **Promptfoo `llm-rubric` as the fallback** when an operator needs the YAML-first / one-shot CLI ergonomics for an ad-hoc eval probe outside the canonical harness, or when DeepEval's Python dep is locally unavailable. Both are OSS, self-host, free of cloud-account requirement. The two are **non-overlapping**: DeepEval owns the L4-reviewer-discrimination harness (§1); Promptfoo owns the prompt-revision drift suite (§7). | A `regatta eval reviewer` subcommand that (a) takes a corpus of known-good and known-bad PR diffs from `docs/engineer/eval-corpus/` (~50 PRs to start), (b) shells out to a Python DeepEval `GEval` runner that scores each through the L4 reviewer gate, (c) emits `precision`, `recall`, `false_positive_rate`, `false_negative_rate` as OTel span attributes + a `kind=l4_eval` substrate event. Output feeds the cost-governor dashboard's `l4_reviewer_quality` panel. | Promote Promptfoo from fallback to co-primary when the `regatta eval reviewer` Python-subprocess overhead exceeds the LLM-call wall-time by >10% for 7 consecutive days (`eval_subprocess_overhead_p95 / llm_call_p95 > 0.1`) — at which point a YAML-first CLI saves real wall-clock time. The `inbound_l4_richer_eval_asks` counter remains as a **tracking-only signal**, not an automatic promotion trigger (per G5). |

### Why this doesn't break top-3 pick #3 (Promptfoo prompt-revision)

§7's prompt-revision eval suite reads a CAS-stored prompt revision and
runs `promptfoo eval` against an inline rubric. That is a **different
job** from §1's L4-reviewer discrimination — §7 is asking "did the new
prompt drift the output enough to matter?" not "what is the
precision/recall of the reviewer gate?". Promptfoo's `llm-rubric` is
fit-for-purpose for §7 (YAML-driven, fast iteration, CI-friendly), while
DeepEval is fit-for-purpose for §1 (precision/recall scaffolding). The
two tools serve two distinct surfaces.

This resolves #416 G4 (the R5 tautology) without needing a fired-rarely
predicate: the two tools are **not** in a primary/fallback maintenance
burden — they own different jobs in different sections. The 2-way
maintenance worry F6 raised for Skill-vs-plugin does not apply, because
Skill-vs-plugin was the same install surface picking two competing
schemas, whereas DeepEval-and-Promptfoo are two surfaces each picking
the locally-best tool.

---

## §3 — G4 fix: §10 R5 resolution rewrite

The original §10 R5 resolution ("Promptfoo is the primary; DeepEval is the
fallback when Promptfoo's `llm-rubric` ceiling is hit") becomes a tautology
per #416 G4 — it restates the structure without justifying that the
fallback rarely fires.

### Replacement R5 text

> **R5 — Promptfoo and DeepEval overlap in §1.** Picking both for L4 eval
> is the same anti-pattern F6 flagged for Skill-vs-plugin (pick both
> creates a 2-way maintenance burden). — *Resolved:* the two tools do
> **not** overlap; they own non-adjacent jobs. DeepEval is the L4-reviewer
> discrimination harness (§1) because `GEval` ships purpose-built
> precision/recall scaffolding. Promptfoo is the prompt-revision drift
> suite (§7) because `llm-rubric` + YAML CLI is fit-for-purpose for fast
> iteration on prompt diffs. The F6 anti-pattern was two competing
> schemas at one install surface; here we have two distinct surfaces
> each picking the locally-best tool. The 2-way maintenance burden does
> not materialize because neither tool is the fallback for the other in
> normal operation. The only cross-over is the §1 reopen predicate
> (`eval_subprocess_overhead_p95 / llm_call_p95 > 0.1` for 7 days), at
> which point Promptfoo would be co-primary in §1 specifically to cut
> Python-subprocess overhead — a measurable, dashboardable promotion.

---

## §4 — G5 fix: §1 decision row + §6 reopen table reconciliation

#416 G5 flagged that PR #411 §1 decision row mentions `inbound_l4_richer_eval_asks ≥ 1`
as a "tracking signal" while §6 reopen-condition table lists it as an OR
clause in the promotion predicate. As written, one operator filing one
enriched-eval request would auto-trigger DeepEval promotion (which under
the new G3 fix is now the *primary* — so the inverse: one operator ask
would auto-trigger Promptfoo co-primary). Either way the predicate is
below the noise floor.

### Replacement §6 row for §1 (DeepEval-vs-Promptfoo balance under the new G3 mapping)

| § | Original (vague) reopen condition | Amended (dashboardable) reopen condition |
|---|---|---|
| §1 (DeepEval/Promptfoo balance) | "L4 false-positive rate on `pr_merge_rate` panel exceeds 5% for a 7-day window OR `inbound_l4_richer_eval_asks ≥ 1`" | **AND-gated, not OR-gated:** the Python-subprocess overhead predicate (`eval_subprocess_overhead_p95 / llm_call_p95 > 0.1` for 7 consecutive days) **promotes Promptfoo to co-primary** in §1. `inbound_l4_richer_eval_asks` is a tracking-only counter; it never triggers an automatic promotion by itself. |

The asymmetry vs other rows (single-OR vs AND-gated) is justified by the
G3 swap: the primary tool is already the higher-fit one, so the
promotion event is a **performance** trigger (cut subprocess overhead),
not a **correctness** trigger. Correctness is satisfied by DeepEval being
primary from day one.

---

## §5 — G6 fix: §6 §1 (Pkl plan-authoring) reopen threshold raised

#416 G6 flagged that PR #411 §6 §1 Pkl plan-authoring keeps `≥1 named
ask` as the reopen predicate. The original brief used a single-ask
threshold; the amendment did not loosen it; but #416 R4 itself justified
50-PR corpora as "the smallest set where a 5% false-positive rate
crosses the noise floor" — a single ask is below that noise floor. §6 §2
(skill-marketplace) uses `≥3` as the threshold; the §1 Pkl row should
match the same pattern.

### Replacement §6 row for §1 (Pkl plan-authoring)

| § | Original (vague) reopen condition | Amended (dashboardable) reopen condition |
|---|---|---|
| §1 (Plan authoring — Pkl) | "if a paying customer files a request for IDE-grade authoring" | `inbound_customer_asks{wedge=ide_authoring} ≥ 2` with stated use cases from **distinct named operators** (raised from `≥1` to match the §6 §2 skill-marketplace noise-floor pattern; one operator asking is below the noise floor justified in §10 R4) |

The `≥2 distinct named operators` shape is dashboardable (each ask is a
GH issue with a `wedge=ide_authoring` label + operator handle) and
matches the §6 §2 pattern.

---

## §6 — G7 + G8 fixes: §0 footnote + persona-A grade reconciliation

### G7 fix — §0 footnote on top-3 pick #3

Add a one-sentence footnote to the §0 ranking block, immediately after
the bullet for pick #3 (Promptfoo prompt-revision):

> Footnote (per #416 G7): pick #3's standing rests on the
> tooling-consolidation argument that Promptfoo is already the §7 tool
> for prompt-revision evals — its placement in the top-3 is the
> one-tool-for-the-job pick for §7, not an independent persona-A weight.
> Prompt-revision drift gates L4 precision/recall regression, which is
> persona-A-load-bearing transitively but not directly.

This makes the indirect persona-A weighting explicit rather than
implicit.

### G8 fix — §0 grade vs §3 decision row reconciliation

PR #411 §0 grades §3 (observability) as "high — `stdout` + local Grafana
suffice" but §3's decision row picks LGTM (Grafana + Tempo + Loki), not
raw `stdout`. Add a one-sentence reconciling note to §0's row for §3:

> Reconciling note (per #416 G8): `stdout` is the **floor** (an operator
> with zero observability setup can run regatta with `stdout`-only logging
> and see workflow output); LGTM is the **adopted shape** for operators
> who want spans + traces + log search in a single docker-compose. The
> §3 decision row picks LGTM because that is what the runbook
> documents as the recommended starting point; `stdout` remains a
> supported zero-deps fallback for the operator who has not yet decided.

---

## §7 — Net amendment surface

The diff against PR #411 (`docs/engineer/specs/2026-06-02-wave-2-amendments.md`)
is bounded to:

1. **§1 candidate table** — add CLI/CI ergonomics column; re-score
   DeepEval to A+, Promptfoo stays A.
2. **§1 decision row** — swap primary/fallback labels; clarify
   non-overlapping job split with §7; rewrite reopen predicate to
   `eval_subprocess_overhead_p95 / llm_call_p95 > 0.1` for 7 days.
3. **§0 ranking block** — top-3 pick #3 footnote (G7).
4. **§0 persona-A wedge table** — §3 row reconciling note (G8).
5. **§6 reopen-condition table** — §1 row rewritten per G5 (AND-gated,
   tracking-only `inbound_l4_richer_eval_asks`); §1 Pkl row raised
   from `≥1` to `≥2 distinct named operators` per G6.
6. **§10 R5 resolution** — rewritten per G4.

No other section needs touching. §§2-5 + §§7-9 + §11-12 remain as PR #411
ships them after the `a780774` G1 fix.

---

## §8 — Adversarial review of this amend-to-amend

Per `feedback_adversarial_review` a reviewer subagent ran a hostile-read
over this spec. Findings folded back in:

- **AR1 — swapping the primary changes the operator's first-day
  workflow.** PR #411 told the implementer "install Promptfoo, you'll use
  it for both §1 and §7." This spec tells the implementer "install
  DeepEval *and* Promptfoo." Higher operator setup cost. — *Resolved:*
  the operator already had to install Python somewhere (most Go-on-Mac
  setups ship Python); `pip install deepeval` is a one-liner and the L4
  gate harness shells out via subprocess, no per-operator config. The
  one-tool argument was real but the load-bearing dimension is
  judge-eval-fit, not install convenience. Per `feedback_decision_priority`
  UX (correct precision/recall) outranks ease (one-tool install).

- **AR2 — the new §1 reopen condition (`eval_subprocess_overhead_p95 /
  llm_call_p95 > 0.1`) is unlikely to fire in practice.** Python
  subprocess overhead is dominated by the LLM call itself. If the
  predicate never fires, Promptfoo is permanently the fallback —
  effectively a vestigial entry. — *Resolved:* that is the correct
  outcome. If DeepEval is the right tool and the subprocess cost never
  meaningfully impacts wall-time, then the §1 promotion never fires and
  Promptfoo stays at its §7 home. The reopen predicate is the *escape
  hatch* against being wrong, not a planned event. This matches the
  pattern of `inbound_managed_observability_asks{tier=paying} ≥ 1` —
  the prediction is that it never fires.

- **AR3 — adding the CLI/CI ergonomics column to the candidate table
  feels like a post-hoc justification.** The column was not in the
  original PR #411 table; #416 G3 demanded a re-score, and the new
  column conveniently doesn't change the winner. — *Resolved:* the
  column is genuinely the dimension Promptfoo's "tooling consolidation"
  argument was implicitly trading on. Surfacing it as an explicit
  column makes the trade visible — but the load-bearing dimension was
  always judge-eval-fit, which DeepEval wins. The honest read of the
  table was that PR #411 picked Promptfoo on a non-load-bearing column
  buried in the prose; the present spec surfaces both columns and the
  load-bearing one still picks DeepEval.

- **AR4 — G5's AND-gating departs from the OR-gating used in other §6
  rows (e.g. §7's `prompt_revision_count_per_dispatch ≥ 3 OR
  inbound_cross_team_prompt_share_asks ≥ 1`).** Inconsistent. —
  *Resolved:* the OR-gated rows promote a **track-only candidate** to
  active; the §1 row promotes one **already-adopted** tool to co-primary
  of an already-adopted other. The asymmetry is justified: §1 needs a
  *performance* trigger to swap (because correctness is already
  satisfied), other §6 rows need a *signal* trigger to even consider
  the candidate.

---

## §9 — B/A/A+ rubric for this amend-to-amend

Per `feedback_grade_rubric` — every spec ships with a scorecard the PR
body posts verbatim.

| Tier | Criteria |
|---|---|
| **B (floor)** | (a) G2 root cause named and confirmed already-fixed by G1's commit. (b) G3 (HIGH) addressed with either re-score or swap, defended in prose. (c) Release-notes fence present at end of PR body. |
| **A (target)** | B + (d) G4, G5, G6 (MEDIUM) each addressed with a concrete amendment diff. (e) Decision priority cited (UX → ease → ... ; long-term > short-term) when the G3 trade is made. (f) Self-host-first stance preserved — no new vendor-locked primitive introduced. (g) Adversarial-review section surfaces ≥3 contradiction-risk findings not already addressed elsewhere. |
| **A+ (stretch)** | A + (h) G7 + G8 (LOW) addressed. (i) The new CLI/CI ergonomics column makes the formerly-implicit Promptfoo edge explicit, not hidden. (j) The decision-row swap explicitly justifies why it does **not** re-open #416 R5 (non-overlapping jobs, not primary/fallback). (k) `scripts/doc-check.sh` clean on this file. (l) Cross-reference to PR #402 brief + PR #407 review + PR #411 amendment + PR #416 review cited inline. |

**Self-scored tier:** A+. (a)-(l) all met:

- (a) — §1 names G2 as the same `doc-check` banned-phrase root cause as G1;
  cites the exact CI log line; confirms local `scripts/doc-check.sh` passes
  on the post-fix file.
- (b) — §2 swaps primary to DeepEval, surfaces CLI/CI ergonomics as the
  new column that makes Promptfoo's implicit edge explicit, defends with
  `feedback_decision_priority`.
- (c) — release-notes fence ships in the PR body.
- (d) — §3 (G4 rewrite), §4 (G5 reconciliation), §5 (G6 threshold raise)
  each ship a diff.
- (e) — UX-over-ease cited in §2 + §8 AR1.
- (f) — DeepEval is Apache-2.0, self-host (`pip install`), no cloud
  account.
- (g) — §8 AR1-AR4 surface four contradiction-risk findings.
- (h) — §6 ships both G7 footnote and G8 reconciling note.
- (i) — §2 candidate table now has the CLI/CI ergonomics column.
- (j) — §2 closing block explains why DeepEval+Promptfoo do not trip the
  Skill-vs-plugin anti-pattern (non-overlapping jobs, not duplicate
  schemas at one surface).
- (k) — `scripts/doc-check.sh` clean against the latest worktree.
- (l) — PR #402 (brief), PR #407 (review), PR #411 (amendment), PR #416
  (review) all cited.

---

## §10 — Sources

- Original wave-2 brief: `docs/engineer/research/2026-06-02-wedge-wave-2.md`
  (PR #402, branch `research/wedge-wave-2`)
- First review: `docs/engineer/reviews/2026-06-02-wave-2-review-of-402.md`
  (PR #407, branch `review/402-wave-2`)
- First amendments: `docs/engineer/specs/2026-06-02-wave-2-amendments.md`
  (PR #411, branch `spec/wave-2-amendments`; G1 fixed via commit `a780774`)
- Second review: `docs/engineer/reviews/2026-06-02-wave-2-amendments-review-of-411.md`
  (PR #416, branch `review/411-wave-2-amendments`)
- Customer roadmap: `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md`
- Self-host-first brief: `docs/engineer/briefs/2026-06-01-self-host-first.md`
- L4 reviewer spec: `docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`
- DeepEval (Apache-2.0) — github.com/confident-ai/deepeval
- Promptfoo (MIT) — github.com/promptfoo/promptfoo
- CI run for G2 root cause: `26841166069` (verify) — failed at
  `scripts/doc-check.sh` step on the same `best-in-class` token as G1
- Memory cites: `feedback_research_design_principles`, `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`
