# Followup Items

Followups live as GitHub issues, not in this file. This page is a
thin index pointing at the right query.

## Where to look

| Looking for | Query |
|---|---|
| Everything deferred during review | `gh issue list --label followup --state open` |
| MVP-2 scheduled work | `gh issue list --label mvp-2 --state open` |
| MVP-3+ scheduled work | `gh issue list --label mvp-3 --state open` |
| Tech-debt sweeps | `gh issue list --label tech-debt --state open` |
| Orchestrator backlog (existing) | `gh issue list --search "in:title orchestrator" --state open` |

Web UI equivalent: `https://github.com/trilamsr/regatta/issues?q=is%3Aopen+label%3Afollowup`.

## How to add a followup

When you find a non-blocking issue during review or implementation:

1. **Actionable in this PR?** Fix and move on.
2. **Not actionable?** File a GitHub issue. Body must include:
   - **Source**: which PR / commit / spec section surfaced it
   - **Trigger to schedule**: the concrete condition that should move
     it from "deferred" to "scheduled" (an operator report, a row-
     count threshold, a compliance ask, etc.) — a followup without a
     trigger is shelfware
   - **Related**: file:line pointers to the relevant code
3. Tag with `followup` plus one of `mvp-2` / `mvp-3` / `tech-debt`.

## Why issues, not a doc

- Triage, assignment, milestones, labels all work out of the box
- Searchable alongside the rest of the repo's issue backlog
- `gh issue close` on merge keeps state in lockstep with code
- A markdown list of TODOs in-repo drifts silently and turns into
  shelfware; an issue with no activity is visibly stale
