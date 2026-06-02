---
id: WAVE-4-NIT-1
title: brief-PR doc-check exit-code posted in PR thread (procedural, recurring)
lane: self-host
status: planned
linked_artifact: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md §7.5 G1
---

Source: unified next-horizon roadmap §7.5 G1 — generalized to a standing process item ("brief-PR author runs `bash scripts/doc-check.sh` post-amendment-commit and posts the exit code in the PR thread before requesting merge"). The original superseded wave-4 amendment workflow (PRs #401 / #414 / #417) no longer applies; the principle does — every roadmap-brief PR re-verifies doc-check after edits.

## Acceptance criteria

- [planned] c1: Comment posted in the brief PR thread quoting `bash scripts/doc-check.sh` exit code (must be 0) after every brief amendment commit.
- [planned] c2: If exit code is non-zero, the amendment commit is fixed before merge — not the doc-check gate suppressed.
