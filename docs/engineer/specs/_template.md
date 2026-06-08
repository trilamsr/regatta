---
title: "<short title>"
status: draft
phase: self-host
summary: "<one-sentence summary: what this spec proposes + what mechanical artifact ships>"
date: YYYY-MM-DD
---

<!--
Canonical spec scaffold enforced by `scripts/check-spec-sections.sh` (wired
into `make check`). New / modified specs MUST carry the 7 H2 sections
below (`Problem`, `Design`, `Acceptance`, `Out of scope`, `Adversarial`,
`Implementer brief`, `Reopen trigger`). The gate skips pre-existing
specs (warn-only) and skips files with `spec_type: skeleton-prefetch`
frontmatter. See spec at `docs/engineer/specs/2026-06-08-spec-template-scaffold.md`.

The §N prefix is conventional, not enforced — `## Problem`,
`## §1 Problem`, `## 1. Problem` all match.

ESCAPE VALVE — minimal specs.
If a section isn't load-bearing for this spec, write a 1-line `N/A —
<reason>` body under it. DO NOT omit the H2 — the gate fails closed
on missing headings, not on terse bodies. Per `feedback_default_simpler`,
brevity is preferred over decorative prose.

Example minimal body:
    ## §4 Out of scope
    N/A — single-artifact spec; no deferred scope.
-->


# <Title> — Spec

Memory rules in force: `feedback_<slug>`, `feedback_<slug>`.

```release-notes
[DOCS] <one-paragraph release-notes summary describing the spec scope + the
mechanical artifact (script / gate / docs-only deliverable). No prod-code
change unless this spec ships code in the same PR.>
```

## §1 Problem

State the failure mode in concrete operator-visible terms. Cite the trigger
(session retro, recurring trap, audit). Quote the symptom (CI line, panic
message, manual step) where useful. Name the primary failure mode per
`feedback_root_cause`, not the downstream symptom.

## §2 Design

Describe the proposed mechanical artifact + how it closes the failure mode.
Include pseudo-code, file layout, regex sketches, or schema where load-bearing.
Cite the prior pattern in repo (`scripts/check-<sibling>.sh`, an existing
spec) when this artifact mirrors one. Per `feedback_default_simpler`, prefer
the simplest viable shape and reject hypothetical-drift abstractions.

## §3 Acceptance

Enumerate the falsifiable conditions for "done":

1. <Artifact lands at `<path>`>.
2. <Companion `_test.sh` (or unit test) covers <named fixtures>>.
3. <Wire-up: `make check` chain in `Makefile.d/lint.mk` + `Makefile.d/ci.mk`>.
4. <Red-then-green commit order per `feedback_tdd_discipline`>.

## §4 Out of scope

List what the spec deliberately defers. Cite a follow-up issue (`#NNN`) OR
a future spec when a deferred item is load-bearing. Per `feedback_default_simpler`,
prefer deferring to a tracker over inlining a half-feature.

- <Deferred item> — <why deferred or `#NNN`>.

## §5 Adversarial pass

Independent-reviewer notes per `feedback_adversarial_review_every_step`.
DO NOT self-write this section then mark the PR ready — dispatch
`cavecrew-reviewer` (or equivalent) in a fresh slot, paste verdict here,
cite `Reviewer-agent-id:` in the PR body footer.

- <Edge case / failure mode reviewer hunted>.
- <Verdict: APPROVE / REVISE / BLOCK + tracker `#NNN` if REVISE>.

## §6 Implementer brief

Per `feedback_dispatch_brief_only` — paste-ready scope for the implementer
subagent. Keep narrow; reference the spec for cross-cutting Qs but DO NOT
re-dump full spec text into the dispatch prompt.

```
Scope: <one-paragraph>
Files: <path>, <path>, <path>
TDD order: 1) <red test path>, 2) <impl path>, 3) <green commit>
make ci-check exit: <expected>
Reviewer dispatch: <yes/no — yes if load-bearing per check-reviewer-verdict.sh>
```

## §7 Reopen trigger

When does this spec come back from `shipped` / `archived`? Pin the trigger
to an external signal (external customer ask, 30-day-green-then-flake,
regression count > N, OR new failure mode landed in retro). Per
`feedback_recognize_session_end`, do NOT pre-build for hypothetical drift.

- Reopen when: <signal>.
