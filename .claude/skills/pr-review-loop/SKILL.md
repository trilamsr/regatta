---
name: pr-review-loop
description: Run a 5-phase rigorous review of the open PR on this worktree's branch. Phases — author self-review → 8-lens stakeholder parallel review → adversarial deep read → A+ aspiration → simplification + final comment sweep. Spine — operator-first decision priority, validation cycle (research → validate → contradict → re-validate → synthesize), TDD discipline, reproducibility-required tests, explicit edge-case hunt, evolving rubric from independent reviewer-subagent proposals. Orchestrates phases directly in the current session; no external plugin dependency.
---

# PR review loop

When this skill is invoked, satisfy preconditions and orchestrate
the 5 review phases directly in the current session. Each phase
ends with a commit on the branch carrying its findings table; the
final turn emits the readiness audit and completion promise.

## When to run this skill

Structured rigor — five phases, ~14 subagent dispatches, several
commits, full `<final-report>`. Use it when that rigor pays for
itself:

- **Use:** security-sensitive PR (parser, RBAC, auth, signing);
  large diff (>500 changed lines); milestone-critical-path PR; PR
  flagged by a maintainer for extra scrutiny; PR you authored and
  want an adversarial audit on before requesting human review.
- **Skip:** doc-only PRs ≤50 changed lines; revert PRs; mechanical
  rename PRs; CI-only tweaks. The diff-size + path precondition
  below auto-offers a single-subagent review for these cases.

## Precondition checks

Before orchestrating the loop, verify:

1. `git rev-parse --is-inside-work-tree` succeeds. If not, surface
   the gap and stop — nothing to review.
2. `git branch --show-current` returns a branch. If it is `main` or
   `master`, surface the gap and stop — no PR open against main to
   review. If the branch matches `feat/m<X>-*`, AUTO-RESOLVE the
   milestone tag `M<X>` and use `MILESTONES.md "### M<X>."` as the
   rubric anchor. Otherwise (`worktree-*`, `docs/*`, `chore/*`,
   `ci/*`, etc.), AUTO-RESOLVE to `(no-milestone)` and rely on
   PRINCIPLES.md / NORTHSTARS.md / STYLE.md / MEMORY.md only. Never
   ask the operator which milestone applies; the branch name is
   authoritative.
3. `gh pr view --json number` returns a number (an open PR exists
   for the branch). If not, surface the gap and stop. Do NOT
   auto-open a PR; PR authorship is the operator's decision.
4. **Diff-size + path gate.** Run `git diff origin/main...HEAD
   --stat` and `git diff origin/main...HEAD --name-only`. If the
   diff is under 200 changed lines AND every changed path matches
   the low-risk set (`*.md`, `.github/**`, `docs/**`, `.claude/**`),
   the 5-phase machinery is over-budget. Ask the operator once:
   "PR is small/low-risk (N lines across paths X/Y/Z). The 5-phase
   loop costs ~14 subagent dispatches and 5 commits. Run a
   single-subagent review instead?" Default: yes. On yes, dispatch
   ONE read-only `Agent` subagent with combined SRE+Maintainer+
   Security lens using the `<reviewer-brief>` below, surface its
   findings, and exit. Only continue to the 5 phases if the
   operator explicitly chooses the heavy path.

If a check fails after auto-resolution (no work tree, on main, no
PR), surface the precondition gap and stop.

## How the loop runs

After preconditions pass, this skill orchestrates the 5 phases
directly. No plugin invocation; the agent executes the phase
definitions below in order, dispatching subagents via the `Agent`
tool in parallel where a phase specifies parallel dispatch.

Before Phase 1, initialize `.claude/pr-review-loop.local.md` if it
does not exist, with the two append-only sections set to `(none
yet)`:

```
# pr-review-loop session state

## Discovered constraints (append-only)

(none yet)

## Rubric evolution (append-only)

(none yet)
```

The file is session-ephemeral and gitignored. Promote anything
load-bearing to `docs/FOLLOWUPS.md` or the PR description if it
should outlast the loop.

## Guiding principle

PR reviews must:

1. Align with principles in the repo — PRINCIPLES.md, NORTHSTARS.md,
   STYLE.md, `docs/STYLE-docs.md`, MEMORY.md.
2. Satisfy the initial rubric (`MILESTONES.md "### M<X>."` when the
   branch maps to a milestone; otherwise repo-standards only) and
   any additions accepted during this loop.
3. Decide in priority order:
   - The on-call operator running regatta in production
     (PRINCIPLES §13).
   - The next-tier customer of the change (researcher, contributor,
     adopter, SRE — whichever the diff most directly affects).
   - Long-term health and simplicity of the repo.
4. Apply TDD for every code change (failing test → watch fail →
   implement → watch pass → mutation-verify). Per
   `feedback_tdd_falsifiable`.
5. Reproducibility is non-negotiable. Tests are deterministic,
   seed-pinned, hermetic, re-runnable from a fresh checkout.
   Hard-proof commands are recorded with exact re-runnable
   invocations. Per PRINCIPLES §12.
6. Hunt edge cases explicitly. Each phase surfaces ≥1 edge case OR
   records why none exist. Edge cases become failing tests BEFORE
   any fix lands.

Every accept / defer / skip decision cites which of these it serves.

## Validation cycle

No finding is accepted on first dispatch. Each goes through:

1. Research. What evidence would prove this finding true?
2. Validate. Run that evidence. Record the output.
3. Contradict. Independently look for evidence that would refute
   the finding.
4. Re-validate. Run the contradiction evidence. Record the output.
5. Synthesize. Apply only findings that survive contradiction with
   hard proof. Reject claims that don't reproduce; record rejection
   rationale.

Findings without proposable hard-proof default to NIT severity and
defer unless multiple lenses raise them.

## TDD discipline

Every code change made during this loop follows TDD:

1. Write the failing test first.
2. Watch it fail. Record the failure mode.
3. Implement the minimal change.
4. Watch it pass.
5. Mutation-verify: invert the invariant; confirm the test fails;
   restore.

For removed code: the regression test is the absence test or lint
rule that would fail if the code were silently re-added. If none
exists, record `(unverified — defended by code review only)`.

Test reproducibility — every test landed satisfies:
- Deterministic: same inputs produce same outputs.
- Seed-pinned: random-driven tests use an explicit seed recorded
  in the test name or body.
- Hermetic: no network, no real-clock, no filesystem-ordering
  reliance, no shared mutable state.
- Re-runnable: `git clean -fdx && make ci` passes on the same SHA.

Tests violating these are flagged in Phase 5.

## Initial and evolving rubric

Initial rubric: `MILESTONES.md "### M<X>."` when the branch maps to
a milestone; otherwise repo-standards in PRINCIPLES.md /
NORTHSTARS.md / STYLE.md / MEMORY.md only.

Independent review agents (phases 2–5 subagents) may propose
ADDITIONS — new criteria framed as falsifiable shapes (test,
threshold, doc section, vendor citation). Validate each through
the validation cycle:

- Load-bearing (reproduces, serves operator/customer/repo,
  measurable) → add to binding rubric; apply via TDD.
- Taste-call (can't be reproduced, doesn't survive contradict) →
  reject; record as `explicitly-skipped: taste-call`.
- Future work (load-bearing but out of scope) → defer to
  `docs/FOLLOWUPS.md` with `Revisit when:` clause.

Evolved rubric is recorded in `.claude/pr-review-loop.local.md` under
`## Rubric evolution (append-only)`. One line per entry, prefixed
by the phase that proposed it. Later phases (within the same
session) read the current evolved rubric and check the diff
against it.

## Acceptance and stopping rule

Both share one canonical checklist. Emit
`<promise>RIGOROUS-REVIEW-COMPLETE</promise>` only when ALL of:

- Five `[review-pass-N-*]` commits exist on the branch in phase
  order. Verifier:
  `git log origin/main..HEAD --oneline | grep -c '\[review-pass-'`
  returns 5.
- Each phase's commit body contains its pushback table, hard-proof
  citations, validation-cycle and TDD records, and any rubric
  additions proposed and accepted in that phase.
- Each finding has an action: `applied <SHA>`,
  `deferred <FOLLOWUPS.md ref>`, or `explicitly-skipped` (with a
  PRINCIPLES.md / NORTHSTARS.md / STYLE.md / MEMORY.md slug citation).
- `.claude/pr-review-loop.local.md` has a `## Rubric evolution` section
  with one bullet per accepted addition, prefixed by the phase that
  proposed it.
- The final turn emits, in order: `<final-report>` block,
  `<readiness-audit>` block (one bullet per acceptance criterion +
  concrete evidence), `<promise>RIGOROUS-REVIEW-COMPLETE</promise>`.
- `make ci` clean on the latest commit.
- `gh pr checks <PR#>` reports all `pass`.
- AI-vocab grep gate clean across the diff (excludes
  `.claude/skills/`).
- DCO `Signed-off-by:` on every commit. NO `Assisted-by:` trailer,
  NO `Co-Authored-By:` for AI, NO AI mention in commit body or PR.
  Per `feedback_no_ai_mentions_in_repo`.
- No commits to `main`; no force-push; no rebase past pushed commits.
- No scope creep: findings outside the milestone rubric defer to
  `docs/FOLLOWUPS.md`.
- Every new code path lands via TDD.
- Every comment added or kept passes the six-months-cold-reader
  test. Phases 1 and 5 both remove existing comments that fail.
- PR title and body reflect the final branch state. Phase 5 either
  edits them to be concise, humanlike, and intuitive (no AI vocab,
  no review-loop mention, no churn-for-churn) or records
  `(unchanged — already accurate)` with a one-line justification.
  Readiness audit cites the `gh pr edit` command or the unchanged
  justification.
- Every finding tagged `Beneficiary: operator` cites a specific
  operator-facing surface — alert rule path, runbook section, log
  line, RSS/cardinality budget, dashboard panel, or `make ci` /
  `make check` failure mode. Findings without such a citation
  downgrade to `repo-long-term`. Per `feedback_anti_bureaucracy`.

If Phase 5 lands but any condition fails: do NOT emit the promise.
Fix forward in NON-`[review-pass-*]` commits.

## Pass order

Five phases, in this order. Each operates on the current state of
the branch (after prior phases' commits) and the current evolved
rubric (initial + additions). Apply the validation cycle to every
finding and TDD to every code change.

### Phase 1 — Author rigorous self-review

Read the diff LINE BY LINE against the initial rubric. For every
assumption or claim in the original commits:

- Tests: does the test ACTUALLY test the claimed invariant? Read
  the test body. Mutation-verify. Check reproducibility.
- Numbers: every quoted number has a citation or `(unverified)`
  marker. Citations include exact re-runnable command.
- Comments: remove any that doesn't carry six-months-cold reader
  value.

Edge-case hunt — required deliverable. Enumerate ≥5 candidates
(boundary inputs, error paths, concurrency, empty/malformed/
oversized inputs, partial state, network partition, restart-mid-op).
For each: cite an existing test, OR write the failing test first
then fix, OR defer to `docs/FOLLOWUPS.md`.

End with commit: `[review-pass-1-self]`.

### Phase 2 — Stakeholder-lens parallel review

Dispatch reviewer subagents IN PARALLEL (single assistant turn,
multiple `Agent` tool calls), one per lens:

| Lens | Specific concerns |
|------|-------------------|
| Performance engineer | Allocation patterns, hot-path discipline, lock contention, GC pressure, payload size, throughput per host |
| SRE / Infra | k8s manifests, RBAC, self-telemetry, upgrade/rollback, blast radius, on-call ergonomics |
| Maintainer | Code clarity, test coverage, architectural cohesion, onboarding burden, naming |
| Contributor | CI signal quality, review velocity, contribution-path docs, style guide adherence |
| Operator / User | Stability, resource usage, log noise, debuggability, alert design, runbook clarity |
| Adopter | Install ergonomics, default-config safety, perf footprint, security defaults, docs first-touch |
| Security | Privilege surface, network egress, data retention, log redaction, dependency pinning, RCE surface |
| Researcher | Data fidelity, reproducibility, OTel-semconv alignment |

Use the `<reviewer-brief>` below. Each subagent is read-only and
may propose rubric additions. Apply the validation cycle to every
finding; apply TDD to every accepted code change. Synthesize
proposed rubric additions per the criteria under "Initial and
evolving rubric" above; record load-bearing additions to
`.claude/pr-review-loop.local.md § Rubric evolution`.

End with commit: `[review-pass-2-stakeholders]`.

### Phase 3 — Adversarial deep review

Dispatch 2 reviewer subagents (fresh, NOT the lens reviewers).
Each does an independent line-by-line adversarial read against
the current evolved rubric. Brief: "the author's claim of
completion is a hypothesis, not a fact. Test it."

Run the contradict step explicitly. Drop findings that don't
survive. Apply TDD to surviving findings.

End with commit: `[review-pass-3-adversarial]`.

### Phase 4 — A+ aspiration

Dispatch 2 reviewer subagents to:

1. Rate the PR + milestone delivery on a letter grade against the
   current evolved rubric.
2. List concrete criteria that would elevate the PR to A+. Each
   criterion MUST be measurable. "Be more elegant" is not valid.

Apply the validation cycle to each criterion. Load-bearing ones
become rubric additions AND get implemented via TDD. Taste-calls
and future-work defer to `docs/FOLLOWUPS.md`.

End with commit: `[review-pass-4-aplus]`.

### Phase 5 — Simplification + final comment sweep

Dispatch 2 reviewer subagents to find OVER-ENGINEERING:
unnecessary abstractions, premature generalization, gold-plating,
performance optimization without measured baseline. Each finding
requires hard proof that the abstracted thing is IMPORTANT. If no
proof: REMOVE.

Then a final superfluous-comment sweep ACROSS THE WHOLE DIFF.

**Then sync PR title and summary to the final branch state.**
After five phases of review, the branch's title/body from PR
creation is likely stale — findings landed, scope tightened,
sometimes the headline claim shifted. Re-derive both from the
current diff:

- Read `gh pr view <PR#> --json title,body`.
- Read the current diff: `git diff origin/main...HEAD --stat` and
  the full diff for context.
- Rewrite to be **concise, humanlike, and intuitive**. A teammate
  skimming the PR list should grasp what changed and why in one
  glance. Drop ceremony, AI-style hedging, em-dashes used as
  sentence-connectors, bullet lists where prose works, and any
  section header that doesn't earn its weight.
- Title: imperative, ≤72 chars, one prefix tag if the repo uses
  them (e.g., `[ci]`, `[docs]`, `[feat]`) — match the convention
  visible in `git log origin/main --oneline -20`.
- Body: lead with what changed and why in 1–3 sentences; only add
  sections (test plan, follow-ups, rollback) if they carry signal
  beyond the diff itself. No AI vocabulary; passes the AI-vocab
  grep gate. No mention of the review loop, phases, subagents, or
  this skill.
- Apply via `gh pr edit <PR#> --title '<new>' --body '<new>'`. If
  title and body are already accurate after the five phases,
  record `(unchanged — already accurate)` and skip the edit. Do
  NOT churn for churn's sake.

End with commit: `[review-pass-5-simplify]`.

After `[review-pass-5-simplify]`:
- Run `make ci`; verify clean.
- Run `gh pr checks <PR#>`; verify all green.
- Emit `<final-report>` → `<readiness-audit>` → completion promise
  IN THE SAME TURN.

## Reviewer brief

Dispatch reviewer subagents with this brief verbatim. Substitute
`<LENS NAME>` and `<LENS CONCERNS>` per phase, and substitute
`<PR#>` and `<RUBRIC ANCHOR>` (either `MILESTONES.md "### M<X>."`
with the resolved milestone, or `(no rubric anchor — milestone
absent; rely on PRINCIPLES.md / NORTHSTARS.md / STYLE.md /
MEMORY.md only)`) before dispatch.

  > You are an INDEPENDENT, READ-ONLY reviewer of a code change.
  > Role: **<LENS NAME>**. Concerns: <LENS CONCERNS>.
  >
  > Inputs (cwd is the worktree root):
  > - Diff:           `git diff origin/main...HEAD`
  > - Branch:         `git branch --show-current`
  > - Initial rubric: <RUBRIC ANCHOR>
  > - Evolved rubric: `.claude/pr-review-loop.local.md` §"Rubric evolution"
  > - Standards:      PRINCIPLES.md, NORTHSTARS.md, STYLE.md,
  >                   docs/STYLE-docs.md, MEMORY.md
  >
  > For every finding, propose HARD PROOF (a test that should fail
  > before fix and pass after; a grep that should return zero hits
  > after fix; a benchmark threshold; a vendor doc URL) AND a
  > CONTRADICTION (what evidence would refute the finding).
  > Findings without proposable proof default to NIT.
  >
  > You MAY propose rubric additions if the PR exposes a class of
  > correctness or quality concern. Each must be measurable.
  >
  > Output strictly:
  >
  > Findings:
  > 1. Severity:        BLOCKER | CONCERN | NIT
  >    Beneficiary:     operator | customer-<role> | repo-long-term
  >    Location:        file:line (or "PR-level")
  >    Description:     <one short paragraph>
  >    Proposed proof:  <how the author should verify>
  >    Proposed contradiction: <what evidence would refute>
  >    Fix:             <one line>
  > 2. (...)
  >
  > Proposed rubric additions (optional):
  > - <addition: one line, measurable>
  >
  > Verdict: APPROVED | CONCERNS-REQUIRE-FIX | BLOCKER-REQUIRES-RESOLUTION
  > Reason:  <one paragraph naming the beneficiary served>
  >
  > If no findings: `Findings: (none)` + verdict `APPROVED`.
  > Do NOT modify code, push commits, or alter branch state.

## Final report format

After Phase 5 + checks-green, emit in the same turn as the
readiness audit and the promise:

<final-report>
PR:        #<N> — <title>
Branch:    <branch>
Milestone: M<X>

Phase-by-phase findings:
- Phase 1 (self):          <B>/<C>/<N> raised, <applied>/<deferred>/<skipped>
- Phase 2 (stakeholders):  ...
- Phase 3 (adversarial):   ...
- Phase 4 (A+ aspiration): ...
- Phase 5 (simplify):      ...

Validation-cycle stats:
- Findings rejected during contradict:        <count>
- Findings whose hard-proof did not reproduce: <count>

TDD discipline stats:
- New code changes landed via failing-test-first: <count>
- Tests with mutation-verify outcome recorded:    <count>
- Tests reproducibility-verified:                 <count>

Hard-proof artifacts:
- New tests added:                          <count>
- Edge-case tests added (TDD):              <count>
- Comments removed:                         <count>
- Abstractions / over-engineering removed:  <count>
- Lines simplified (approx delta):          <number>

Rubric evolution:
- Initial rubric items: <count>
- Rubric additions accepted: <count>
  - [P2] <addition>: <evidence>
  - (...)
- Rubric additions deferred to FOLLOWUPS:
  - <addition>: <reason>

Cross-phase patterns (≤3 bullets):
- <pattern>

Per-lens final verdicts (phase 2):
- Performance:    APPROVED | CONCERNS | BLOCKER
- SRE/Infra:      ...
- Maintainer:     ...
- Contributor:    ...
- Operator/User:  ...
- Adopter:        ...
- Security:       ...
- Researcher:     ...

A+ criteria met this phase: <count>
A+ criteria deferred (taste / future work):
- <criterion> → FOLLOWUPS.md row

Beneficiary tally (apply / defer / skip by primary beneficiary):
- Operator:         <applied>/<deferred>/<skipped>
- Customer-<role>:  <applied>/<deferred>/<skipped>
- Repo long-term:   <applied>/<deferred>/<skipped>

Merge recommendation: READY-AFTER-RIGOROUS-REVIEW | HOLD-WITH-FOLLOWUPS
Reason: <one paragraph framed through guiding-principle priority>
</final-report>

## Readiness audit (every promise emission)

Before the completion-promise tag, emit `<readiness-audit>` in the
same turn with one bullet per acceptance criterion + concrete
evidence (test name, commit SHA, command output, file:line
citation, CI run URL, FOLLOWUPS.md bullet reference, rubric-
evolution line). Missing evidence = no promise.

## Per-finding pushback structure

| ID | Phase | Lens(es) | Beneficiary | Severity | Finding | Proof | Contradict | TDD record | Rubric+? | Action | Rationale |

- ID: monotonic ACROSS phases — `P1.1`, `P1.2`, ... `P5.N`.
- Lens(es): `self`, `performance`, `sre`, `maintainer`,
  `contributor`, `operator`, `adopter`, `security`, `researcher`,
  `adversarial-1`, `adversarial-2`, `aplus-1`, `aplus-2`,
  `simplify-1`, `simplify-2`.
- Beneficiary: `operator`, `customer-<role>`, `repo-long-term`.
- Severity: BLOCKER / CONCERN / NIT. Escalate one notch if raised
  by 2+ lenses or phases.
- Proof: REQUIRED for `applied`.
- Contradict: REQUIRED for `applied`.
- TDD record: REQUIRED for code-change `applied`.
- Rubric+?: `yes` if this finding led to a binding rubric addition.
- Action: `applied <SHA>` / `deferred <FOLLOWUPS.md ref>` /
  `explicitly-skipped`.
- Rationale: REQUIRED for `explicitly-skipped` and disagree cases.

## Regatta constraints

Apply MEMORY.md regatta rules in full. Per
`feedback_anti_bureaucracy`, priorities without falsifiable
enforcement are ceremony — every `Beneficiary: operator` finding
cites a specific operator-facing surface (alert rule, runbook,
log line, RSS budget, dashboard panel, or `make ci` / `make check`
failure mode).

AI-vocab grep gate — CONVENTION-ONLY (not enforced by CI as of
2026-05; agents run this manually pre-push; excludes
`.claude/skills/`):

  `grep -rn -i 'ralph\|loop[ \-_]?[1-5]\|pass[ \-_]?[1-5]\|four[ \-]?loops\?\|reviewer agents\?\|subagents\?\|loop design\|loop prompt' \
    --include='*.md' --include='*.go' --include='*.yaml' \
    --exclude-dir='.git' --exclude-dir='.claude/skills'`

Commits: DCO `Signed-off-by:` via `git commit -s`. NO
`Assisted-by:` trailer. NO `Co-Authored-By:` for AI. NO AI
mention anywhere.

## Output register

Phase 2+: emit a delta vs prior phase. Apply MEMORY.md
`feedback_trim_by_default` and `feedback_decide_dont_hedge`.

## Discovered constraints + Rubric evolution

Both sections live in `.claude/pr-review-loop.local.md` (session-
ephemeral, gitignored, initialized at Phase 1 start). At the end
of every phase:

- If this phase surfaced a recurring pattern, footgun, or
  cross-phase observation, APPEND a bullet to `## Discovered
  constraints (append-only)` in the local file.
- If this phase produced a BINDING rubric addition, APPEND a
  bullet to `## Rubric evolution (append-only)` in the local
  file. Prefix with `[P<N>-<phase-tag>]`.

Do not delete or rewrite prior bullets.
