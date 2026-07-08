---
name: fpr
description: Fix the GPT-5.5 bot's PR review on the current branch. Reads the latest `## GPT-5.5 independent review` comment posted by `.github/workflows/gpt-pr-review.yml`, addresses every HIGH and MED finding inline (or files a tracking issue when out of scope), commits per-finding with a citing message, replies to the bot comment with the resolution per finding, and pushes back to the same PR. Use when the bot has posted a `REVISE` or `BLOCK` verdict on the open PR for this worktree, or when the user types `/fpr`.
---

# fix-pr-review (fpr)

Address the GPT-5.5 bot's review on the open PR for this worktree's
branch. The bot is the OpenAI-vendor counterpart to the Claude-Opus
author in this repo (see `scripts/gpt-pr-review.sh` +
`.github/workflows/gpt-pr-review.yml`). Its review carries the verdict
and HIGH/MED findings; this skill is the matched fix loop.

## Preconditions

Before doing anything else, verify all of:

1. `git rev-parse --is-inside-work-tree` succeeds. Otherwise surface
   and stop.
2. `git branch --show-current` is not `main` / `master`. Otherwise
   surface and stop — no PR to fix.
3. A PR is open on the current branch:
   `gh pr view --json number,state -L 1`. If `state != OPEN`, surface
   and stop.
4. The bot has commented on this PR. Fetch the latest comment from
   the bot:
   ```
   gh pr view <N> --json comments --jq '
     .comments
     | map(select(.body | startswith("## GPT-5.5 independent review")))
     | last
   '
   ```
   If no such comment exists, surface "bot review not posted yet —
   push a commit and wait for `.github/workflows/gpt-pr-review.yml` to
   run" and stop.
5. `OPENAI_API_KEY` is NOT required locally — the skill only consumes
   the bot's comment; it does not call OpenAI.

## Phase 1 — parse findings

From the bot comment body:

- Line 1 (after the heading) carries the verdict: `APPROVE`, `REVISE`,
  or `BLOCK`. If `APPROVE`, surface "bot already approved — nothing to
  fix" and stop.
- Bullet list of findings follows. Each bullet has the shape
  `<severity>, <file>:<line>, <problem>, <fix>`. Parse into rows.
- Drop any LOW / nit findings the bot may have leaked through; address
  only HIGH and MED.

Write the parsed table to `$CLAUDE_JOB_DIR/fpr-findings.md` so phase 2
can iterate.

## Phase 2 — fix each finding

For every HIGH / MED finding, in the order listed:

1. **Read the cited file + neighbourhood** before editing. The bot's
   line numbers are a LEAD, not GROUND TRUTH — per
   `feedback_subagent_output_verify`. Spot-check that the symbol /
   pattern the bot cited is actually present.
2. **Decide inline-fix vs tracking issue:**
   - Inline-fix when scope is bounded to one or two files AND the fix
     plainly improves the PR.
   - Tracking issue when scope crosses subsystems, breaks an API the
     PR is not chartered to touch, or rewrites a design the bot did
     not see context for. Use `gh issue create` with title
     `[BOT #PR] <severity> <category>: <problem>` and a body that
     quotes the bot's bullet verbatim. Cite the issue number in the
     reply to the bot.
3. **TDD where applicable** — production code paths get a failing
   test first, then the fix. Skip TDD only for pure deletions and
   pure comment trims; the bot's biggest wins are typically
   simplification + comment trims, so this branch is common.
4. **Commit per finding** with a message of the form:
   ```
   [<TYPE>] fix <one-line problem>

   Addresses GPT-5.5 bot finding (PR #<N>): <bot quote>.
   ```
   No AI signatures (per `feedback_no_signatures`). One finding per
   commit so the PR history shows the bot loop legibly.
5. **Reply to the bot's comment** with the fix summary and the
   commit SHA:
   ```
   gh pr comment <N> --body-file <reply>
   ```
   Reply body:
   ```
   Re: <bot quote, one line>
   Fix: <one-line resolution>
   Commit: <sha>
   ```
   If the finding became a tracking issue, replace `Commit:` with
   `Tracking: #<issue>` per `feedback_reviewer_findings_to_issues`.

## Phase 3 — verify

After all findings are addressed:

1. Run the project's pre-push gate: `make pre-push-check`. Any
   failure that the bot's fixes introduced gets a fix commit on top
   of the loop. Per `feedback_subagent_verification`, do not trust
   the bot's claim that "this is the only change needed".
2. Re-run `bash scripts/gpt-pr-review_test.sh` if the bot or fpr
   files themselves were touched.
3. Push the branch: `git push`. The `gpt-pr-review` workflow will
   re-fire automatically and post a fresh verdict. Surface the
   re-review URL to the operator.

## Phase 4 — drive verdict to APPROVE

Iterate Phase 1 → 3 until the bot returns `APPROVE` OR the operator
calls a halt. Hard cap: 3 fpr loops per PR — beyond that, the bot is
either wrong about something load-bearing (file a counter-finding
issue + ping the operator) or the PR is mis-scoped (split it).

## Out of scope

- Don't rewrite the PR title or body unless a finding explicitly
  demands it.
- Don't touch unrelated files. The bot's review is the contract; the
  fix surface matches.
- Don't auto-merge after `APPROVE`. The bot is advisory; merge
  authority stays with the operator / autonomous loop per
  `docs/engineer/briefs/2026-06-07-gpt-pr-review-bot.md`.

## Decision priority (per CLAUDE.md)

UX > ease > performance > best-practices > speed > velocity. Long-term
> short-term. Apply to every fix / tracking-issue / counter-finding
decision; never ask the operator.

## Reopen triggers

- Bot verdict format changes (e.g. JSON instead of bullet list) →
  rewrite phase 1 parser.
- Inline-fix vs tracking-issue split rate diverges from ~80/20 → tune
  the decision rule.
- Loop hits the 3-iteration cap repeatedly → either the bot prompt or
  the fpr decision rule has drifted; revisit
  `docs/engineer/briefs/2026-06-07-gpt-pr-review-bot.md`.
