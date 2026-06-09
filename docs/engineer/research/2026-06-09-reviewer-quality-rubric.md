# Reviewer A+ quality rubric — research survey (#1062)

Research-only survey for #1062. The reviewer-verdict gate (`scripts/check-reviewer-verdict.sh`) enforces review *presence* — a `Reviewer-recommendation: APPROVE` token paired with a non-author `Reviewer-agent-id:` — not review *quality*. Across the 2026-06-08 session every reviewer subagent returned APPROVE or REVISE-then-APPROVE; #1056 still shipped with a bypass (#1057) the reviewer only would have caught if the operator had prompted "look for the bypass." This doc surveys prior art on review-quality grading, defines two terms #1062 introduces (negative-space audit, A+ delta), enumerates measurement options, and lists failure modes a stricter rubric must defend against.

## Problem

Three discipline gaps surfaced in the 2026-06-08 retro, re-validated against five recent reviewer dispatches — #1118 (5 valid findings), #1120 (5), #1124 (3, BLOCK), #1126 (2, REVISE), #1127 (APPROVE, zero):

1. **No A+ delta.** Per `feedback_grade_rubric` the B/A/A+ scorecard is unenforced; every load-bearing PR self-grades B-implicit. None of the five reviewers named the specific evidence that would close the B→A gap on the artifact under review.
2. **No negative-space audit.** No reviewer was instructed to propose three concrete bypass attempts. #1056 shipped a string-literal symbol-match bypass because the template's "edge cases" lens (`docs/engineer/dispatch-templates/reviewer.md:36`) reads as "test boundary inputs," not "enumerate adversarial bypasses."
3. **Reviewer defaults APPROVE under uncertainty.** The closed-enum `APPROVE / REVISE / BLOCK` (`scripts/lib/reviewer-verdict/verdict.sh`) has no `INSUFFICIENT_EVIDENCE` slot. Quiet acceptance is the path of least token-cost.

Mechanical contract today: presence. Honor-system contract: quality. The retro asked how to close the gap without re-introducing the rubric-token enforcement that #906 deliberately killed.

## Prior art

Three open-source reviewer-quality projects anchor different points on the spectrum.

1. **`thoughtbot/guides` "Code Review"** — Apache-2.0 (https://github.com/thoughtbot/guides/blob/main/LICENSE.md), HEAD https://github.com/thoughtbot/guides/blob/main/code-review/README.md . Prose checklist naming what reviewers *must produce* rather than what authors must change: ask for clarification, identify simplifications, move philosophical disputes offline. Closest prior art for "review-quality is reviewer-output discipline." No grading; relies on team culture.
2. **`google/eng-practices` "Code Reviewer's Guide"** — CC-BY-3.0 (https://github.com/google/eng-practices/blob/master/LICENSE), HEAD https://github.com/google/eng-practices/blob/master/review/reviewer/looking-for.md . Names eight axes a reviewer must explicitly evaluate before LGTM: design, functionality, complexity, tests, naming, comments, style, docs. Companion `every-line.md` is the closest published "make the reviewer prove they actually read it" discipline — diff-coverage discipline, not adversarial enumeration.
3. **`danger/danger-js`** — MIT (https://github.com/danger/danger-js/blob/main/LICENSE.md), v12.x. CI plugin that fails a PR when `Dangerfile` rules are violated (description-length floors, test-files-accompany-`lib/`). Closest mechanical reviewer-quality gate. Limitation: grades *author* output, not reviewer output, because GitHub does not expose reviewer comment volume as first-class CI signal. Demonstrates the seam, not the metric.

Two near-misses: **`reviewdog/reviewdog`** (MIT, v0.18.x) posts lint findings as review comments — a transport, no rubric; **GitHub `CODEOWNERS` + N-approvals branch protection** — enforces review presence, zero quality signal. Regatta's current gate sits at this tier.

Recurring pattern: review-quality rubric is *prose* (humans grade humans) when adversarial coverage is the goal, *transport structure* when format consistency is the goal. None of the surveyed systems mechanically grades whether the reviewer *found anything that mattered*. The 30-day-bug-reopen approach is documented in academic literature but not shipped as a CI gate by any OSS project surveyed.

## Definition: negative-space audit

The bug space in a PR is the union of (a) bugs visible in the additions and (b) bugs *not* present that should be. The current "edge cases" lens covers (a). Negative-space audit names (b) and forces enumeration:

- Missing tests — diff adds a code path, diff does not exercise it.
- Missing docs — exported symbol, no godoc capturing WHY.
- Missing migration — schema change, no `internal/store/migrations/<N>_*.sql` increment.
- Missing error handling — function returns `error`, caller drops it silently.
- Missing CI gate — new banned-phrase token, no `scripts/doc-check.sh` regex update.
- Missing closure — acceptance criterion AC-N, no commit traces it.
- Missing bypass — gate matches symbol foo, reviewer proposes three strings that *look like* foo but evade the regex.

#1062 c1 proposes "≥3 bypass attempts + outcome each (mitigated / accepted / filed)." Three is small enough to fail-fast when none exist, large enough that "I checked one obvious bypass" cannot pass.

## Definition: A+ delta

Three tiers, distinguished by *what the reviewer caught*:

- **B-tier** — code works under happy path; reviewer confirms.
- **A-tier** — code works; reviewer caught ≥1 HIGH/MED finding worth fixing inline (not LOW comment-noise).
- **A+ tier** — reviewer found a finding that *surprised the implementer*. Implementer says "good catch — I would have shipped this bug." #1057's symbol-match bypass would have been A+ if a reviewer raised it pre-merge.

The A+ delta is the one-paragraph articulation: what specific additional evidence (test, run, audit, prior-art lookup) would have promoted this review to A+? Empty answer is allowed only with `<!-- a-plus-not-applicable: <reason> -->`; the gate refuses to assume "no delta named" means "no delta exists."

The asymmetry: A-tier is reviewer-internal; A+ tier is implementer-witnessed. The mechanical contract cannot enforce implementer concession — honor-system signal. It *can* enforce that the delta names *what would have made the find non-obvious* (hidden invariant, cross-package interaction, soak-only timing window). Vague deltas ("more thorough review") fail.

## Measurement options

Three mechanical approaches for grading reviewer quality post-hoc, in increasing order of cost and signal strength:

**(a) Human-rated 1-week sample.** Operator tags a fixed sample (e.g. 10 PRs/week) `B / A / A+ / INSUFFICIENT`. High signal per-PR. Cons: O(N) operator load defeats autonomy. Best for calibration windows (first month after a new rubric ships).

**(b) 30-day-bug-reopen rate.** Each merged PR carries `Reviewer-agent-id:`. Tracking issue filed within 30 days citing the PR # debits the reviewer. Mechanical, falsifiable, no per-PR operator input. Cons: 30-day lag; "should the reviewer have caught this" is itself a judgment call. Best as trailing audit, not per-PR gate.

**(c) Reviewer self-grade vs follow-up A+ score.** First reviewer self-grades B/A/A+; a second reviewer (load-bearing-doc surfaces or randomized 10% sample) re-scores. Disagreement is logged. Catches systematic over-grading. Cons: doubles subagent cost on sampled PRs; second reviewer can also default APPROVE.

Pragmatic recommendation: (b) as the trailing audit + (c) at 10% sample for fast spot signal + (a) reserved for calibration weeks. The combination yields slow-trailing + fast-spot + authoritative-anchor.

## Failure modes

Five anti-patterns ranked by likelihood:

1. **"Find something to say."** Reviewer files three LOW-tier comment-noise findings to clear the audit bar; operator stops reading output. Defense: reject LOW-only audits — contract is "three bypass attempts," not "three findings of any tier."
2. **Boilerplate negative-space.** Reviewer copy-pastes the same three attempts across every PR ("integer overflow / SQL injection / TOCTOU"). Defense: require *PR-specific* attempts citing `path:line` of the surface under test. Bypass attempt without a diff citation cannot pass.
3. **A+ delta inflation.** Reviewer names an out-of-scope delta ("full TLA+ verification") — technically true, unactionable. Defense: "proportional evidence" constraint — delta unobtainable in <2× current review effort is rejected.
4. **INSUFFICIENT_EVIDENCE inflation.** New verdict slot becomes path of least resistance; operator becomes de-facto grader. Defense: pair INSUFFICIENT_EVIDENCE with `Confidence-evidence-needed:` naming a tracker issue # — punt cost equals file-and-link cost.
5. **Self-tag re-emergence.** Author drafts the audit paragraph, real reviewer rubber-stamps. Existing allowlist-shape check catches the gross case (`feedback_no_self_tagged_approve`). Rubric-specific case needs a parity check between audit-author and verdict-author.

## Recommendation

Three asks for the spec phase:

1. **Negative-space audit** as ≥3 PR-specific bypass attempts with disposition. Pair with the `INSUFFICIENT_EVIDENCE` verdict slot.
2. **A+ delta as a one paragraph** with "proportional evidence" constraint + `<!-- a-plus-not-applicable: <reason> -->` escape. Avoid token-grammar enforcement (#906 killed that pattern); enforce *presence and shape*, not semantic content.
3. **Measurement (b) + (c)** as the trailing signal. Defer (a) to calibration windows. The 30-day-reopen ledger is one file under `docs/engineer/audits/`, updated by whoever files the tracking issue; no new infrastructure.

Implementation of the gate extensions, regression-test fixtures, and dispatch-template edits are spec-phase work (#1062 c2-c5).

## Open questions

- Does the 30-day window catch the bugs that matter, or do bypasses (#1057-class) surface in 7 days vs 90+ days? Measure the tracking-issue ledger's actual lag distribution before encoding `30` as a magic number.
- Is "implementer surprise" capturable mechanically? Implementer-side `<!-- a-plus-confirmed: <agent-id> -->` after seeing reviewer output is one option; risks gaming.
- Does the rubric apply to LOAD-BEARING-DOC PRs identically, or do prose-only PRs need a different audit shape ("≥3 ways a future agent might misread this rule")?
- Right cap on reviewer-subagent cost when (c) double-spawns? 10% sample is a guess.

## References

- #1062 — this research's parent (reviewer subagents default APPROVE without negative-space audit or A+ delta)
- #1057 — symbol-match bypass that escaped review APPROVE (post-merge tracker)
- #1056 — original PR whose reviewer missed #1057
- #906 — prior token-grammar enforcement attempt; killed deliberately
- `docs/engineer/dispatch-templates/reviewer.md` — current template under audit
- `scripts/check-reviewer-verdict.sh` — current gate enforces presence not quality
- CLAUDE.md `feedback_adversarial_review_every_step` — design briefs, specs, prompts get adversarial pass not just PRs
- CLAUDE.md `feedback_grade_rubric` — operator A+ scorecard stays unenforced
- CLAUDE.md `feedback_no_self_tagged_approve` — author writing own APPROVE = zero adversarial pass
- `thoughtbot/guides` code-review prose checklist — Apache-2.0
- `google/eng-practices` reviewer's guide eight axes — CC-BY-3.0
- `danger/danger-js` Dangerfile CI mechanical gate — MIT
