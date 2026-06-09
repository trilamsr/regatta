# Reviewer-of-reviewer pattern — research survey (#1087)

Research-only survey for #1087. The single-pass reviewer-subagent rule
(`feedback_adversarial_review` in `CLAUDE.md`, mechanically backed by
`scripts/check-reviewer-verdict.sh`) treats *one* independent adversarial
pass as sufficient. The 2026-06-08 retro and three 2026-06-09 observation
windows contradict that assumption. This doc surveys prior art on layered
review, frames the statistics, costs the second pass against measured
token budgets, and names failure modes. It does *not* propose schema or
config — `docs/engineer/research/2026-06-08-reviewer-of-reviewer-feasibility.md`
already sketches the gate shape (`gates.reviewer_of_reviewer.enabled`);
this doc is the empirical justification feasibility cited but did not
derive.

## Problem

The 2026-06-08 dogfood session spawned a fresh adversarial reviewer at
session-end to audit the prior twelve in-session reviewer passes. Of six
findings the operator selected for re-audit, **five were factually wrong**.
Three 2026-06-09 observation windows sampled the inverse: when reviewer
findings were re-audited by an *independent* second-pass reviewer, how
often did the second pass reject the first?

- #1118 (5 findings) — second pass kept 5/5.
- #1120 (5 findings) — second pass kept 5/5.
- #1124 (3 findings) — second pass kept 3/3.

Pooled: 13/13 valid in-flight, 1/6 valid in retro. The variance is the
load-bearing observation — a single-pass reviewer on the same template
can hit precision near 1.0 on a well-scoped PR and near 0.17 on an
end-of-session batch, and the operator cannot tell which regime without
a second pass.

The current gate enforces *presence* of one independent reviewer, not
precision. `feedback_no_self_tagged_approve` closes the zero-reviewer
hole; `feedback_adversarial_review_every_step` extends presence to
specs. Neither addresses the case where the one reviewer is genuinely
independent but mis-fires.

## Prior art

Three layered-review systems published under permissive licenses anchor
points on the spectrum of how seriously the second pass is taken.

1. **`google/eng-practices` Code Reviewer's Guide** — CC-BY-3.0
   (https://github.com/google/eng-practices/blob/master/LICENSE), HEAD
   https://github.com/google/eng-practices/blob/master/review/reviewer/standard.md .
   Names readability-review as a second axis layered over functional
   review: one reviewer covers correctness, a second (language-certified)
   covers style and idiom. Two passes by construction; no single reviewer
   ships a CL alone in non-trivial paths. Closest prior art for
   "mechanically required second adversarial axis."
2. **OpenSSF Scorecard `Code-Review` check v5.x** — Apache-2.0
   (https://github.com/ossf/scorecard/blob/main/LICENSE), HEAD
   https://github.com/ossf/scorecard/blob/main/checks/code_review.go .
   Scores a repo on whether merged PRs carry ≥1 reviewer approval distinct
   from the author. A repo gating on two approvals scores higher than one;
   the check encodes the assumption that N≥2 catches what N=1 misses, but
   does not grade the second reviewer's calls.
3. **GitHub `CODEOWNERS` + branch-protection "Require approvals: 2"** —
   GitHub Docs
   (https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-security/managing-protected-branches),
   no SPDX. Allows N-of-M approver chains routed by path. Chromium's
   `OWNERS` files (BSD-3-Clause,
   https://chromium.googlesource.com/chromium/src/+/refs/heads/main/LICENSE)
   extend this to per-directory two-approver requirements on
   `//security/`, `//net/`, `//chrome/browser/safe_browsing/`. Two-pass is
   load-bearing for security-critical surfaces; one pass is acceptable
   elsewhere.

Two academic prior arts inform framing. Empirical-software-engineering
literature on peer-review reliability (Kitchenham & Pfleeger, *Personal
Opinion Surveys*, 2008) reports inter-rater agreement on bug-finding
clustered at κ = 0.4–0.6 — two raters diverge on roughly a third of
findings. ESLint's `rules/` (MIT,
https://github.com/eslint/eslint/blob/main/LICENSE) requires two
maintainer +1s before rule merge; the issue tracker documents cases
where the second +1 caught a false-positive regex the first missed.

Recurring pattern: when the cost of a wrong "ship" exceeds the cost of
one extra review pass, every surveyed system layers a second pass on a
*routed* subset of the change set. None applies it uniformly — routing
is by path (`CODEOWNERS`/Chromium), axis (eng-practices readability),
or maintainer convention (ESLint).

## Statistical framing

Frame a single reviewer as a binary classifier on findings: precision
*p* (raised findings that are real), recall *r* (real bugs raised). The
2026-06-08 retro suggests *p* ≈ 0.17 on end-of-session audit batches;
2026-06-09 in-flight samples suggest *p* ≈ 1.0. Distribution is
bimodal, not a single *p*.

Assume two conditionally-independent reviewers — distinct agent-ids,
transcripts, no shared first-pass memo. Let *p₁*, *p₂* be precisions,
*r₁*, *r₂* recalls.

- **Single pass** keeps every raised finding: *p₁* true + *(1 − p₁)*
  false per raised.
- **Intersection** (raise only when both agree): true yield scales by
  *r₂*; false rate scales by *(1 − p₁)·(1 − p₂)*. Worked example: *p* =
  *r* = 0.8 → keep 64% true, 4% false. Precision rises 0.8 → ~0.94.
- **Union** (raise when either agrees): true yield rises to
  *1 − (1 − r₁)(1 − r₂)* = 0.96. False rate rises to 0.36. Precision
  falls 0.8 → ~0.73.

The reviewer-of-reviewer pattern is neither intersection nor union — it
is the **audit rule**: the second pass votes on the first pass's
findings, not on the artifact directly. The second reviewer rejects
findings the first raised but does not raise findings the first missed.
Worked example: *p₁* = 0.17 (retro batch), audit precision *p₂* = 0.8 →
audit keeps 0.17 + 0.83·0.2 = 33% of first-pass findings, of which
0.17 / 0.33 ≈ 51% are real. Precision rises 0.17 → ~0.51 — a 3× lift on
the regime where it matters.

The audit rule does *not* improve recall — missed bugs stay missed
unless the second pass sees the artifact, not the memo. Mixed pattern
recovers union-rule recall at union-rule false-positive cost.

Caveat: independence is the load-bearing assumption. Reviewers from the
same template/model class with overlapping context are correlated, not
independent. The numbers above are upper bounds; empirical κ on layered
LLM review is not yet measured here. The 13/13 + 1/6 split is
suggestive, not calibrated.

## Cost vs value

Session-1106 spawned ~8 reviewer subagents on load-bearing PRs; the
audit pass doubles dispatches to ~16. Token cost per audit pass measured
in the retro was ~12k input / ~3k output — roughly one-third of a
first-pass reviewer because the audit reads the finding list, not the
full diff. Latency adds one serialized ~5-min round per audited PR.

The pattern earns its keep when the cost of a wrong "ship" exceeds 2× the
cost of a clean review pass. Three PR classes meet that bar today:

- **Schema migrations** (`internal/store/migrations/`,
  `contracts/schemas/`). Wrong-shipped migration is hard to roll back;
  replaying corrupted history costs more than an extra reviewer.
- **CI gate changes** (`scripts/check-*.sh`, `Makefile.d/ci.mk`,
  `.github/workflows/pr-lint*.yml`). A gate that silently stops firing
  is invisible until the next regression slips. `feedback_trap_projection`
  notes the recurring cost.
- **Agent-rule surfaces** (`CLAUDE.md`,
  `docs/engineer/dispatch-templates/*`,
  `internal/orchestrator/spawner/claude.go::defaultPromptBuilder`). Drift
  here propagates to every future dispatch; one missed bypass token
  compounds.

Over-spend classes — dep bumps <20 LoC with CI green, single-file doc
strips, pure-rename refactors verified by `check-byte-equal-pin` — all
already auto-skip independent reviewers under
`feedback_review_proportional`. The same predicate covers the second
pass.

## Failure modes

1. **Collusion on shared blind spots.** Same-template/same-model
   reviewers share priors; if the template under-weights, say,
   integer-overflow, neither raises it. Mitigation per
   `google/eng-practices`: route the second pass to a different
   reviewer-template — a `reviewer-of-reviewer.md` that prompts only
   "audit the first reviewer — which findings would *you* not have
   raised?"
2. **Recursive review.** If the second pass mis-fires, does a third pass
   audit it? Halting rule: depth caps at 2 unless the second pass
   returns `INSUFFICIENT_EVIDENCE` (cf. the rubric doc's proposed
   verdict slot). Recursion past depth 2 is a session bug, not a feature.
3. **Audit-pass apathy.** If the audit reviewer defaults to "first
   reviewer was right" under uncertainty, precision lift collapses. The
   audit dispatch prompt MUST require enumerating *which finding the
   auditor would not have raised and why*, not a blanket re-attestation.
4. **Operator-as-second-pass.** Operator inspection before APPROVE makes
   an LLM second pass duplicative. The rule is "automate the thing the
   operator did manually," not "add a layer on top of operator review."
   Skip when an operator agent-id is on the dispatch.

## Recommendation

Three triggers for second-pass dispatch in regatta, ranked by signal
strength against measured retro data:

- **Schema-migration paths**. Trigger unconditional. Single migrations
  have shipped with reviewer-quiet-accept (#1056 case in the rubric
  doc); the audit pass is the cheapest fix for the class.
- **CI gate changes**. Trigger when the diff adds or removes regex
  tokens, modifies fail-closed predicates, or touches a gate-test under
  `scripts/check-*_test.sh`.
- **Agent-rule surfaces**. Trigger unconditional. Worker-prompt parity
  drift surfaced twice in the last week (#1047, #1056); both would have
  been caught by an audit pass scoped to anchored-rule diffs.

Spec-only PRs should *not* trigger second-pass by default — the
brief/spec adversarial rule already mandates one independent pass and
doubling fails the `feedback_default_simpler` test. Re-evaluate after a
30-day green window on the three trigger classes.

## Open questions

1. Separate `reviewer-of-reviewer.md` template or `--mode=audit` flag
   on `reviewer.md`? Separate cuts blind-spot correlation (failure mode
   1); a flag halves maintenance.
2. Routing when the first reviewer was the operator? Skip-on-operator-id
   is obvious; corner case is operator-opened mid-session PRs with
   `<!-- operator-opened: -->` markers.
3. What promotes opt-in to default-on for the three trigger classes?
   Candidate: a measured precision lift ≥2× over a rolling 20-PR sample.
4. Memo-only or memo+artifact? Memo-only is cheaper and matches the
   audit rule's shape; memo+artifact recovers recall at union-rule cost.

## References

- `docs/engineer/research/2026-06-08-reviewer-of-reviewer-feasibility.md`
- `docs/engineer/research/2026-06-09-reviewer-quality-rubric.md`
- `CLAUDE.md` `feedback_adversarial_review`,
  `feedback_adversarial_review_every_step`,
  `feedback_no_self_tagged_approve`,
  `feedback_review_proportional`,
  `feedback_default_simpler`,
  `feedback_trap_projection`.
- Issue #1087 — symptom statement (5-of-6 wrong first-pass findings).
- `scripts/check-reviewer-verdict.sh` — current single-pass enforcement.
- `scripts/lib/reviewer-verdict/verdict.sh` — gate library (`#1045`).

```release-notes
[DOCS] research: reviewer-of-reviewer adversarial pass survey (#1087)
```
