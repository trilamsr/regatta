---
id: SMOKE-001
title: cross-restart-persistent brief replay protection
kind: feature
lane: server
status: planned
linked_artifact: https://github.com/trilamsr/regatta/issues/92
---

Fixture for the Phase S1 smoke test. Mirrors GH issue #92 (a real open
`[followup]`-labelled issue at fixture-creation time) so the smoke
loop has a non-trivial, repo-grounded work item to traverse from
intake through PR-merge journal write. The body is intentionally
minimal — the smoke test asserts loop wiring, not body parsing.

## Acceptance criteria

- [planned] c1: processed_briefs table + migration land per issue #92 body
