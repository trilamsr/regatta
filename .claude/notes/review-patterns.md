# Review patterns

Lessons about multi-lens review, adversarial passes, and
iterative review prompt construction when running review tasks
via the Agent tool. Directly relevant to how regatta's L3 / L4 /
L5 review gates and the human L6 reviewer should operate.
Newest-first.

### Pin the review target sha before dispatching the reviewer

A reviewer background job dispatched by SHA X may return a
verdict citing code that no longer exists on the PR head. The
failure mode: the implementer force-pushes an amendment after
`gh pr ready` (violating stop-at-pr-ready), the review reads
the diff at the newer HEAD but reasons over the pre-amend state
it saw first, or the reviewer runs long enough for a fresh
push to land. The verdict cites 4 findings; 3 are already
resolved in the amended code, 1 is a false positive. Every
finding needs re-triage against the current tree, costing more
than a re-dispatch would have.

Prevention: capture `gh pr view <N> --json headRefOid` before
launching the review, paste the sha into the dispatch prompt,
and instruct the reviewer to abort with INSUFFICIENT_EVIDENCE
if `git rev-parse HEAD` inside the worktree does not match.
A one-line check is cheaper than reconciling stale findings.

Anchor: `gh pr view <N> --json headRefOid,commits[-1].oid`
matches the reviewed sha; mismatch means the tree moved
mid-review and the verdict is void.

### Name the exact PR number in the first sentence of a review dispatch

Review dispatch prompts that lead with lens language ("review
the diff", "check the finding count") but bury the target PR
number lower down can misfire when the background job receives
cascading task-notifications from other in-flight jobs. The
reviewer's context can drift and end up reasoning about a
neighboring PR that shares surface area. Verdict never emits
for the intended PR; re-dispatch is the only recovery.

Prevention: put the PR number in the first sentence, repeat it
adjacent to the diff-path reference, and instruct the reviewer
to refuse work on any other PR number that surfaces in its
context. Belt-and-suspenders framing costs one clause; a
wasted review costs a full run.

Anchor: dispatch prompt line 1 contains `PR #<N>`; grep the
prompt for other `#\d+` references and remove or annotate them.

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
