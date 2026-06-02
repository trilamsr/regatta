---
id: WAVE-4-NIT-1
title: brief-PR doc-check exit-code posted in PR #401 thread
lane: self-host
status: planned
linked_artifact: https://github.com/trilamsr/regatta/pull/417
---

Source: docs/engineer/reviews/2026-06-02-wave-4-amendments-review-of-414.md G1 (spec-vs-implementation gap; verification deferred to brief-PR)

Brief: The amendments spec (PR #414) is a contract — actual brief edits land on PR #401 in a follow-up commit. G1 nit records the expected risk: the spec's `doc-check` only covers the spec file; the amended brief's `doc-check` runs after #414 merges. Mitigation: brief-PR #401 author runs `bash scripts/doc-check.sh` post-amendment-commit and posts the exit code in the #401 PR thread before requesting merge. One-line action; no code change.

## Acceptance criteria

- [planned] c1: Comment posted in PR #401 thread quoting `bash scripts/doc-check.sh` exit code (must be 0) post-amendment-commit.
- [planned] c2: If exit code is non-zero, the amendment commit is fixed before merge — not the doc-check gate suppressed.
