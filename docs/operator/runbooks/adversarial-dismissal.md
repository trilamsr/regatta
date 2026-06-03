# Runbook — Adversarial-review dismissal-rate alarm

Alarm: `AdversarialDismissalRateHigh`. Source SLO:
`slo/adversarial-dismissal.yaml`. The alarm warns when the reviewer
subagent's findings are being dismissed at a rate > 50% over a rolling
7-day window AND total finding count > 20 in that window.

Counter source: `regatta_adversarial_findings_total{outcome="dismissed"}`
divided by `regatta_adversarial_findings_total` (all outcomes). See
spec `docs/engineer/specs/2026-06-02-obs-wave-d-operator-surface.md`
§3 for the counter shape + §3.4 for the alarm definition.

## What it means

Half-or-more of the reviewer's findings are being ignored. Either the
reviewer is generating noise the operator does not act on, or the
operator is auto-dismissing without triage. Both states erode the
review loop's signal value.

## First 60 seconds

1. Open `docs/operator/dashboards/adversarial.json` and look at panel
   1 (top-10 recurring patterns, 7-day window). The patterns most
   represented in the dismissal queue are the candidates for one of
   the three causes below.
2. Cross-reference recent dismissals in the substrate event log:
   ```sql
   SELECT pattern, severity, scope, dismissed_at, dismiss_reason
   FROM adversarial_findings
   WHERE outcome = 'dismissed' AND dismissed_at >= datetime('now', '-7 days')
   ORDER BY dismissed_at DESC
   LIMIT 50
   ```
3. Read 5–10 dismiss reasons. The reason text usually narrates the
   actual cause (noise, policy drift, gate-misfire) in plain English.

## Likely causes

### Reviewer noise — same pattern, same dismissal reason, repeated

The reviewer-subagent is flagging a pattern that the repo intentionally
allows (or that another gate already catches). Top-10 panel will show
one pattern dominating the dismissal share.

**Fix.** Recalibrate the reviewer prompt
(`docs/engineer/dispatch-templates/reviewer.md`) to suppress that
pattern OR re-spec the linter rule so the reviewer trusts the upstream
gate. Record the change as an entry in `~/.claude/projects/.../memory/`
via `/learn this` so the recalibration sticks across sessions.

### Repo-policy churn — what was a finding last week is now expected

A merged PR changed a convention (e.g. comment-style rule, godoc shape,
release-notes prefix list) and the reviewer prompt still tests the old
rule. Top-10 panel will show a pattern that *first appeared* in the
7-day window — not a steady-state offender.

**Fix.** Diff the spec / convention file against `main` from 7 days
ago; identify which convention moved; update the reviewer prompt to
match. The `feedback_<slug>` memory file for that convention should
also reflect the new shape.

### Branch-protection misconfig — finding is correct, but operator
cannot act on it

Reviewer is flagging a real issue, but the protection rules let the PR
merge anyway, so the operator dismisses the finding as a fait
accompli. Top-10 panel will show patterns tagged `severity=critical`
or `severity=major` being dismissed (rather than `minor`/`info`/`nit`).

**Fix.** Open `gh api repos/{owner}/{repo}/branches/main/protection`
and reconcile the required-check list against the gate the reviewer
fires on. The branch protection contract should *require* the gate
that catches the dismissed-but-correct finding, so the next PR cannot
merge without addressing it.

## Mitigation while triaging

If the alarm keeps firing during diagnosis, reduce the reviewer's
dispatch concurrency at the wave-size knob (see
`docs/engineer/autonomous-session-prompt.md` §"Dispatch") rather than
silencing the alarm. The signal value of the reviewer is
load-bearing — muting the alarm to land throughput is a self-defeat.

## Rollback

If a recalibration pushed false-negative rate too high (the reviewer
now misses real findings the operator catches in PR review), revert
the dispatch-template change via `git revert` and re-spawn a design
subagent on the convention before tweaking again — see
`feedback_spec_pattern_authority`.

## Related SLOs

- SLO `regatta-adversarial-dismissal-rate` — same source counter,
  threshold-locked at 50% over 7 days.
- The reviewer-loop sits upstream of SLO-6 (PR lifecycle stage p95) —
  if dismissals correlate with `awaiting_review` stage growth, treat
  the two as one incident.
