# Review patterns

Lessons about multi-lens review, adversarial passes, and
iterative review prompt construction when running review tasks
via the Agent tool. Directly relevant to how regatta's L3 / L4 /
L5 review gates and the human L6 reviewer should operate.
Newest-first.

### Pin review to the pushed commit, not the dirty worktree

When dispatching a review of a PR, the prompt must name the exact
pushed sha and instruct reading via `gh pr diff <N>` /
`git show <sha>:<path>` — never the working tree. A worktree raced
by a second writer can hold another change's uncommitted scratch, or
a fix not yet in the reviewed commit, so a review that reads files
instead of the commit reviews a phantom and reports findings that
don't match what actually merges. A returned reviewer id that does
not match the real dispatched id is a fabricated attestation: void
the verdict and re-review — same integrity class as a self-tagged
approval.

Anchor: `scripts/check-reviewer-verdict.sh` checks id *shape*, not
authenticity. Regression proof — PR #1301 commit `0a80f2f`: a review
approved the stale `6aa57e0` against a dirty worktree and returned a
fabricated id `a4f8e2c7d9a1b3f6`; the real fix (an empty-paths
fail-open in the auto-merge classifier) only landed because the
implementer ran its own independent pass.

### Self-rate work, then write criteria for the next grade up

Before declaring your own work done, rate it (B+, A-, A) and
write the measurable criteria that would elevate it one letter
grade. Implement the criteria that fit within the current PR's
bounded scope; defer the rest to FOLLOWUPS. Re-rate. Repeat
until the marginal cost of the next jump exceeds the marginal
value.

The forcing function - "what specifically would elevate this?" -
is sharper than "anything else?" because it requires
articulating measurable criteria.

Diminishing-returns inflection: A to A+ produces smaller deltas
than B+ to A. Pay the A+ cost when explicitly asked or when the
work is load-bearing enough to justify it; otherwise stop at A.

This pattern applies to gate-stack design too: an L4 reviewer
that returns only "approve / reject" leaves the agent no signal
to climb the grade ladder. Rejections carry the criteria that
would convert them to approvals.

### Spawn an explicit adversarial reviewer after graded reviewers converge

"Find what others missed" produces a qualitatively different
signal than graded review. Graded reviewers converge on "looks
good" via the same lenses; an adversarial pass changes the
prompt to "assume there is a bug; find it" and surfaces
different findings.

For regatta: L5's adversarial gate runs *after* L3 and L4 have
converged, with the explicit job of falsifying their consensus.
A six-judge unanimous green that an adversarial seventh judge
overturns is the system working as designed, not a regression
in the first six.

### Pre-load prior-round findings when running an iterative review

Review prompts that do not enumerate what already shipped in
earlier rounds re-surface those exact findings as "new." The
delta-quality output of an iterative review depends on each
round explicitly listing the artifacts (rules, fixtures, knobs,
doc sections) that landed previously, so reviewers focus on the
residual gap.

For an Agent tool call dispatching an iterative review, the
prompt body must include an explicit "what already shipped"
block; without it, the review repeats itself. Regatta's
LessonCapture and agent-state contracts should carry the same
shape: each iteration knows what previous iterations resolved.

### Mutation-verify falsifier tests before claiming defended

A test that asserts a production-code behavior is only a real
falsifier if removing the behavior makes it fail. After
landing a "guard" line, mutation-verify it: comment out the
production line and confirm the test goes red. If the test
still passes, the test wasn't exercising the wired-up path.

Gate-stack canary archetypes follow the same rule: the canary
PR is only a real falsifier of the gate if disabling the gate
makes the canary slip through. Author canaries against the
mutation-verify standard.
