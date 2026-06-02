# Adversarial third-tier review of PR #422 — wave-2 amend-to-amend

Status: review (hostile-read of `spec/wave-2-amend-to-amend`)
Date: 2026-06-02
Audience: design-phase agents picking up the wave-2 spec after the
second amendment pass.
Scope: PR #422 (`docs/engineer/specs/2026-06-02-wave-2-amend-to-amend.md`)
which folds the seven remaining findings from PR #416 review (G2-G8)
of PR #411 wave-2 amendments.

Memory cites: `feedback_adversarial_review` (hostile-read; never
auto-approve; edge cases + refactor + risk + simplification),
`feedback_pr_body_file_only` (no inline prose in PR body),
`feedback_pr_body_release_notes_mandatory` (release-notes fence),
`feedback_decision_priority` (UX → ease → performance →
best-practices → speed → velocity), `feedback_research_design_principles`
(proven OSS over build-from-scratch; UX > leading-existing-impl >
best-practices > long-term).

Chain: #402 brief → #407 review → #411 amend → #416 review → #422
amend-to-amend.

---

## Headline verdict

**REQUEST CHANGES.** One HIGH (toolchain-cost defense is weak), two
MEDIUM (operator-routing gap + G4 framing-pivot ack), two LOW
(vestigial-Promptfoo-co-primary + Pkl-threshold-doesn't-match-pattern).

The #416 G2-G8 closure is **substantively strong**: the primary swap
to DeepEval on judge-eval-fit grounds matches `feedback_decision_priority`,
the §1 / §6 reconciliation is concrete and dashboardable, and the §0
footnote + §3 reconciling note are surgical edits that close LOW
findings cleanly. The §8 adversarial-review-of-itself section
(AR1-AR4) is the right shape — the implementer thought through their
own contradiction risks before shipping.

The spec ships with one load-bearing gap: **DeepEval introduces a
Python toolchain dependency** for a Go-native repo, defended in §8 AR1
with a one-liner ("most Go-on-Mac setups ship Python") that does not
seriously engage the cross-toolchain-tax question. Per
`feedback_research_design_principles` the proven-OSS preference is
real, but the precision/recall scaffolding DeepEval ships is a small
amount of arithmetic — TP/(TP+FP), TP/(TP+FN), and an LLM-judge call
that any Go SDK can issue. The Go-native alternative deserves a real
candidate row in the §1 table, not a dismissal in the AR-review
footnote.

### Counts

- 0 BLOCKING
- 1 HIGH (H1 — Python-toolchain-tax not seriously defended;
  Go-native alternative not in candidate table)
- 2 MEDIUM (M1 — two-tool operator-routing decision tree missing;
  M2 — G4 closure pivots from "falsifiable fallback predicate" to
  "non-overlapping jobs" without naming that the contract changed)
- 2 LOW (L1 — §2 reopen predicate self-acknowledged-rarely-fires
  makes Promptfoo §1-co-primary path vestigial; L2 — G6 Pkl
  threshold raised to `≥2` but §6 §2 skill-marketplace pattern is
  `≥3`, so the "match the pattern" claim is one-off)

---

## Lens 1 — each #416 G2-G8 finding closed?

| #416 | Severity | PR #422 § | Closed? | Notes |
|---|---|---|---|---|
| G1 | BLOCKING | (was inline-fixed in #411 via `a780774`) | yes | Banned-phrase reworded `best-in-class` → `leading-existing-impl`. Acknowledged in §0 + §1. |
| G2 | BLOCKING | §1 | yes | Root cause named (same `doc-check` banned-phrase that tripped `pr-lint`); confirmed cleared by G1 commit; CI rerun cited (`26841530739`, 6m20s pass). The "verify red was the same root cause as pr-lint red" diagnosis is correct — `make ci` → `ci-check` → `check` → `doc-check.sh` is the call chain, and `doc-check` is the first step. |
| G3 | HIGH | §2 | yes | Decision row swapped to DeepEval primary + Promptfoo fallback. Candidate table re-scored with explicit CLI/CI ergonomics column. `feedback_decision_priority` (UX over ease) cited as the load-bearing argument. **Strongest part of the spec.** |
| G4 | MEDIUM | §3 | partial | R5 rewritten to "non-overlapping jobs, no primary/fallback in normal operation." This pivots the framing rather than satisfying #416 G4's literal ask (a falsifiable "fallback fires rarely" predicate). Defensible but worth naming as a contract change — see M2 below. |
| G5 | MEDIUM | §4 | yes | `inbound_l4_richer_eval_asks` demoted to tracking-only. §1 reopen now AND-gated on Python-subprocess overhead predicate. Asymmetry vs OR-gated rows justified in §8 AR4 (performance trigger vs signal trigger). Concrete. |
| G6 | MEDIUM | §5 | mostly | Pkl threshold raised from `≥1` to `≥2 distinct named operators`. Closes the load-bearing concern (single ask is below noise floor) but the "match the §6 §2 skill-marketplace noise-floor pattern" claim is inexact — §6 §2 uses `≥3`, not `≥2`. See L2. |
| G7 | LOW | §6 | yes | §0 footnote added, top-3 pick #3 standing on tooling-consolidation grounds named. |
| G8 | LOW | §6 | yes | §0 grade vs §3 decision row reconciled with "stdout is floor, LGTM is adopted shape" note. |

All seven findings have a named amendment. Two have partial-closure
notes (G4 pivot, G6 threshold mismatch) flagged below.

## Lens 2 — split DeepEval primary (§1) + Promptfoo fallback / §7 — operator confusion?

The spec's defense is that the two tools own **non-overlapping
surfaces**: DeepEval is the §1 L4-reviewer precision/recall harness;
Promptfoo is the §7 prompt-revision drift suite. The two surfaces use
different invocation paths: `regatta eval reviewer` (DeepEval-backed)
for §1; `promptfoo eval` against a CAS-stored prompt revision for §7.

**M1 (MEDIUM) — the routing decision tree is not specified.** An
operator who wants to evaluate "did my prompt change break the L4
reviewer's discrimination?" hits both surfaces — the prompt change is
§7's job (drift detection) but the discrimination metric is §1's
output. The spec does not name where the boundary lives. A
one-paragraph routing table ("if you are asking X, use DeepEval; if
you are asking Y, use Promptfoo; if Z, run both and the answer is
the AND of their outputs") would close this. Without it the
two-tool split is a cognitive tax the operator pays at every eval
invocation.

The §8 AR1 finding ("higher operator setup cost") partially
acknowledges this but resolves it on install convenience grounds
(`pip install` is a one-liner), not on the harder routing question.

## Lens 3 — DeepEval Python-only dep — does regatta accept Python toolchain just for eval?

**H1 (HIGH) — the cross-toolchain tax is not seriously defended.**

Regatta is Go-native. The L4 gate harness, the cost-governor, the
substrate, the OTel emitter — all Go. Introducing DeepEval as the
primary L4-eval harness means:

1. CI runners must `pip install deepeval` + a transitive dep graph
   (DeepEval pulls `openai`, `instructor`, `pytest`, `tqdm`, etc. —
   ~30-50 transitive Python packages depending on extras).
2. Every contributor who runs `regatta eval reviewer` locally must
   have Python + pip available in PATH at a version DeepEval supports.
3. The Go gate harness shells out to a Python subprocess —
   stdout/stderr/exit-code contract that has to be specified, tested,
   and maintained.
4. Supply-chain attack surface widens: now both Go modules and Python
   wheels must be audited (govulncheck doesn't cover pypi).
5. Self-host operators must ship Python in their runner image — a
   new docker-layer cost.

The spec's defense in §8 AR1 — "most Go-on-Mac setups ship Python";
"`pip install deepeval` is a one-liner"; "the L4 gate harness shells
out via subprocess, no per-operator config" — addresses (1) and (2)
loosely but skips (3), (4), and (5) entirely.

### The Go-native counter-candidate the table omits

Per `feedback_research_design_principles` (proven OSS > build-from-scratch),
the preference is real — but the bar for "build" here is **low**:

- Precision = TP / (TP + FP). Two-line Go function.
- Recall = TP / (TP + FN). Two-line Go function.
- False-positive-rate = FP / (FP + TN). Two-line Go function.
- False-negative-rate = FN / (FN + TP). Two-line Go function.
- LLM-judge call: any Go SDK (`anthropic-sdk-go`, `openai-go`) issues
  the rubric-graded request and parses the JSON output.

DeepEval's `GEval` metric class wraps these in a Python class hierarchy
with a `pytest`-style runner. The wrapping is convenience, not
load-bearing — the math is grade-school arithmetic and the LLM-judge
call is one API request. A Go-native L4-eval harness lives in
`internal/eval/reviewer.go` (~150 lines including tests) and uses the
same OTel emitter / substrate event kind / dashboard panel the spec
already names.

The candidate table at §2 should have a sixth row:

| Candidate | Class | License | Self-host | Judge-eval fit | CLI / CI ergonomics | Cost-of-adoption | Score |
|---|---|---|---|---|---|---|---|
| **Go-native L4 eval (handroll + `anthropic-sdk-go`)** | in-tree Go pkg | MIT (regatta) | yes (no external dep) | **high** — direct control of precision/recall math + LLM-judge prompt | **high** — `regatta eval reviewer` is a Go subcommand, no subprocess shell-out | **lowest** — zero Python toolchain tax, zero new supply-chain audit surface | ? |

Whether this row scores A+ or A depends on the maintenance-vs-deps
trade-off the spec must surface explicitly. Per
`feedback_research_design_principles` the OSS-over-handroll preference
should win **when the OSS impl carries load-bearing complexity** —
DeepEval's load is small-arithmetic + LLM-call-wrapping, neither of
which is hard. The Python toolchain tax may exceed the value the
proven-OSS preference is meant to capture.

### Remediation shape for H1

The spec must either:

(a) **Add the Go-native row to the §2 candidate table** + re-score
all candidates with an explicit "cross-toolchain tax" column +
defend DeepEval against the Go-native row on a load-bearing
dimension, or

(b) **Concede that the cross-toolchain tax is real** and add a §10
R-prefixed self-flagged-risk row that names the Python dep as a
known cost the spec accepts with a measurable revisit predicate
(e.g., "revisit if `python_setup_failures_per_ci_run / total_ci_runs
> 5% for 14 days`").

(a) is the stronger close because it tests the assumption that
DeepEval beats handroll on a load-bearing dimension. The spec does
not currently test that assumption.

## Lens 4 — concrete eval-as-code recipe or hand-wave?

Specified:

- Corpus path: `docs/engineer/eval-corpus/` (~50 PRs, 30 known-good +
  20 known-bad).
- Output metrics: `precision`, `recall`, `false_positive_rate`,
  `false_negative_rate`.
- Emission: OTel span attributes + `kind=l4_eval` substrate event.
- Dashboard surface: cost-governor `l4_reviewer_quality` panel.
- Reopen predicate: `eval_subprocess_overhead_p95 / llm_call_p95 > 0.1`
  for 7 consecutive days promotes Promptfoo to co-primary.

Not specified:

- The `GEval` rubric text itself (the prompt the LLM-judge sees).
- The grading threshold (what `GEval` score counts as "L4 says merge"
  vs "L4 says reject").
- The Python-subprocess invocation contract (argv shape, stdout
  schema, exit-code semantics, timeout, retry).
- The known-good / known-bad PR selection criteria for the eval
  corpus (how is the implementer supposed to pick the 30+20?).

Verdict: **concrete on the metric surface, hand-wave on the inner
loop.** Acceptable for a wedge brief that names primitives, but the
implementer spec at `docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`
must close these gaps. Not a finding against this PR — flagging as
forward-work for the L4 implementer.

## Lens 5 — new findings introduced?

### M2 (MEDIUM) — G4 closure pivots without naming the contract change

#416 G4 asked: "add a falsifiable predicate to §10 R5 that the
DeepEval fallback rarely fires." The implementer's literal ask was
a frequency-bounded predicate (e.g., "fallback fires <5% of evals
per 30-day window"). The §3 rewrite does not deliver that; instead
it **reframes** R5 from a primary/fallback structure to a
non-overlapping-jobs structure. Under the new framing the "fallback
fires rarely" question is moot — there is no fallback in normal
operation.

The reframe is arguably **stronger** (eliminates the contradiction
rather than measuring around it). But the spec does not name that
the contract changed. A one-sentence acknowledgment ("G4's literal
ask was a frequency predicate; the present spec reframes R5 so the
predicate is unnecessary — the F6 anti-pattern is dissolved, not
mitigated") would close this.

Without that acknowledgment, a reader cross-referencing #416 G4 to
§3 sees a missing closure. The actual closure is in the §2 prose
("the two tools own non-overlapping jobs"), not in §3.

### L1 (LOW) — §2 reopen predicate self-acknowledged-rarely-fires makes Promptfoo §1-co-primary vestigial

§8 AR2 itself names this: "the new §1 reopen condition
(`eval_subprocess_overhead_p95 / llm_call_p95 > 0.1`) is unlikely to
fire in practice. Python subprocess overhead is dominated by the
LLM call itself. If the predicate never fires, Promptfoo is
permanently the fallback — effectively a vestigial entry."

The AR2 resolution ("that is the correct outcome … the reopen
predicate is the escape hatch against being wrong, not a planned
event") is reasonable, but it leaves Promptfoo's role in §1 as a
named-but-never-active entry. Per `feedback_deletion_default` (every
PR answers "what got smaller?"; addition needs A+ defense), an
inert entry deserves either deletion or a stronger reopen predicate
that has some chance of firing.

Two cleanup shapes:

1. Drop Promptfoo from §1 entirely. It owns §7. The reopen-against-
   being-wrong escape hatch can live in §10 as a self-flagged risk
   row, not in the §1 decision row.
2. Replace the subprocess-overhead predicate with a measurable
   developer-experience predicate (e.g., "if `regatta eval reviewer`
   wall-clock p95 exceeds 30s for 7 days, evaluate switching to
   Promptfoo's YAML-first runner") which is more likely to actually
   fire.

Either close is small. Flagging as LOW because the spec self-flags
the risk.

### L2 (LOW) — G6 Pkl threshold raised to `≥2`, but `§6 §2` pattern is `≥3`

§5 says: "raised from `≥1` to `≥2` to match the §6 §2 skill-
marketplace noise-floor pattern." The skill-marketplace row uses
`≥3 named asks` (per the original #411 brief, which the amend-to-amend
inherits). So the "match the pattern" claim is one-off: `≥2 ≠ ≥3`.

Two close shapes:

1. Raise to `≥3` to actually match the pattern.
2. Acknowledge that `≥2` is a deliberate compromise (Pkl
   plan-authoring is closer-to-core than skill-marketplace, so a
   lower threshold is justified) and reword the "match the pattern"
   claim to "approach the pattern."

Flagging as LOW because the load-bearing fix (raise from `≥1` to
something > 1) is done.

---

## What the amend-to-amend did well

1. **G3 swap to DeepEval primary on judge-eval-fit grounds** is the
   right call per `feedback_decision_priority`. The new CLI/CI
   ergonomics column makes Promptfoo's implicit edge visible
   without hiding it. The choice tests cleanly.
2. **G2 root-cause diagnosis** is exact: the `verify` CI red was
   the same `doc-check` banned-phrase that tripped `pr-lint`. The
   call-chain (`make ci` → `ci-check` → `check` → `doc-check.sh`)
   is the actual one. The G1 commit clears both. No flake hunt
   needed.
3. **§8 adversarial-review-of-self** (AR1-AR4) surfaces four
   contradiction-risk findings the implementer thought through
   before shipping. AR2 in particular self-flags the same vestigial-
   Promptfoo concern (L1) that an external reviewer would name.
4. **§9 rubric is tool-checkable**, including (k) `scripts/doc-check.sh`
   passes locally. The criterion-as-runnable-attestation discipline
   has been internalized.

## Remediation list (rank-ordered)

1. **H1 (HIGH):** Add the Go-native L4-eval row to the §2 candidate
   table OR add a §10 self-flagged-risk row naming the Python-toolchain
   tax with a measurable revisit predicate.
2. **M1 (MEDIUM):** Add a one-paragraph routing table for the §1
   DeepEval vs §7 Promptfoo split — "if asking X use DeepEval, if Y
   use Promptfoo." Close the operator-confusion vector.
3. **M2 (MEDIUM):** One-sentence acknowledgment in §3 that G4's
   literal ask (falsifiable frequency predicate) was satisfied by
   reframing rather than measuring — the F6 anti-pattern is
   dissolved, not mitigated.
4. **L1 (LOW):** Either drop Promptfoo from §1 (own §7 only) or
   replace the subprocess-overhead reopen predicate with one that
   has a real chance of firing.
5. **L2 (LOW):** Raise Pkl threshold to `≥3` to actually match the
   skill-marketplace pattern, OR reword the "match the pattern"
   claim to "approach the pattern."

## Counter-pick analysis

A Go-native-first reviewer would push H1 to the front and demand
the §2 candidate table re-score with a "cross-toolchain tax" column.
The handroll candidate is ~150 lines of Go + tests; the proven-OSS
preference is meant to capture cases where the OSS impl carries
load-bearing complexity, which DeepEval here does not. The
implementer's defense (DeepEval ships `GEval` precision/recall
scaffolding) is real but the scaffolding is small arithmetic; the
Python dep cost is larger than the saving.

A persona-A-load-bearing reviewer (operator who reads PRs, runs
local L4 evals) would push M1 to the front — the two-tool routing
question is the daily-driver cost the operator pays at every eval
invocation. The HIGH-finding routing gap is more visible to the
operator than the toolchain-tax question.

Both counter-pick weights are defensible; this review weights the
toolchain-tax higher because regatta's Go-native posture is
load-bearing for the self-host story (one binary, no runtime deps),
and DeepEval is the first primitive in this brief that breaks that
posture.

## Adversarial-review compliance

Per `feedback_adversarial_review`, this review:

- Hostile-read (not auto-approved) — REQUEST CHANGES verdict landed.
- Edge cases + refactor + risk + simplification — H1 (toolchain
  refactor), M1 (operator-routing risk), M2 (contract change), L1
  (simplification: drop vestigial entry), L2 (consistency-with-pattern)
  cover the four lenses.
- Counter-pick analysis included (Go-native-first + persona-A-first
  weights named).
- Concrete remediation list with severity rank.
- No banned tokens (this file): grep against the 11-token list
  clean; `scripts/doc-check.sh` passes locally.

---

## Sources cited

- PR #402 brief: `docs/engineer/research/2026-06-02-wedge-wave-2.md`
  (branch `research/wedge-wave-2`)
- PR #407 review: `docs/engineer/reviews/2026-06-02-wave-2-review-of-402.md`
  (branch `review/402-wave-2`)
- PR #411 first amendments: `docs/engineer/specs/2026-06-02-wave-2-amendments.md`
  (branch `spec/wave-2-amendments`; G1 fixed via commit `a780774`)
- PR #416 second review: `docs/engineer/reviews/2026-06-02-wave-2-amendments-review-of-411.md`
  (branch `review/411-wave-2-amendments`)
- PR #422 amend-to-amend: `docs/engineer/specs/2026-06-02-wave-2-amend-to-amend.md`
  (branch `spec/wave-2-amend-to-amend`)
- Customer-0 roadmap: `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md`
- L4 gate implementer spec: `docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`
- DeepEval (Apache-2.0) — github.com/confident-ai/deepeval
- Promptfoo (MIT) — github.com/promptfoo/promptfoo
- CI run for #422: `pr-lint` SUCCESS at queue time; `verify` IN_PROGRESS
  at queue time (was the load-bearing red on #411; should land green
  on #422 since the G1 fix is on the parent branch and #422 is a
  pure-add of a new file).
