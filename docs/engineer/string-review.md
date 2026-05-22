# String review checklist

Reader: internal engineer authoring or modifying customer-visible
strings (error messages, log lines, PR-comment templates, CLI help
text).
Read time: 2 minutes.
Expires when: customer-impact priority rule (spec section 3) is
revised.

## Why this exists

The customer-operator never opens a `.go` file. Their experience
surface is the gaps Regatta lives in: CLI output, error messages,
config feedback, PR comments, audit-log queries, upgrade prompts,
cost summaries. Strings ARE the product for the operator.

Rule: every customer-visible string passes the five-item checklist
below.

## The checklist

- [ ] Names the failed precondition or expected state. "validation
      failed" is not enough; cite the rule, the field, the value
      that violated it.
- [ ] Suggests the corrective action OR cites a runbook URL. The
      operator should know what to do next without re-grepping the
      docs.
- [ ] No internal jargon. Package names, function names, internal
      error types, Go type names: rephrase as concepts the
      operator already understands.
- [ ] If the action is irreversible (delete, force-push,
      audit-sink rotation), the headline says so. Single-word
      irreversibility marker beats a paragraph.
- [ ] Fits in one terminal line (<=80 cols) where possible.
      Multi-line is reserved for paste-into-issue cases (e.g.
      cue-validation enumeration).

## Solo-scale review process

Self-review with a 24-hour cooling-off period before merge on any
PR that adds customer-visible strings. Open the PR, sleep on it,
re-read the strings the next morning with fresh eyes. If they
still pass the checklist, ship.

Spec section 3 priority 1: customer impact. This checklist is the
falsifying consumer for that priority at solo scale.

## Examples

| Anti-example | Fixed |
|---|---|
| `validation failed` | `regatta.yaml: gates[0].severity_block: unknown operator '%'; allowed: '&', '\|', 'N*severity'` |
| `cannot proceed` | `regatta verify-repo-config: branch-protection missing 'enforce_admins=true' on main; see docs/operator/day1.md#what-verify-repo-config-catches` |
| `error: nil pointer` | `regatta serve: adapter github_issues: token absent (set GITHUB_TOKEN or `regatta.yaml repo.host_token_env`)` |
| `done` | `regatta migrate-config: regatta.yaml upgraded v1 -> v2; back up regatta.db before starting orchestrator` |

## When in doubt

Run the string by an engineer who did not write the code. If they
can't paste it into a Slack message to a customer and have the
customer understand the next step, rewrite.
