---
id: WAVE-4-NIT-2
title: brief §7 — Operator row dating fix (preview not GA)
lane: self-host
status: planned
linked_artifact: https://github.com/trilamsr/regatta/pull/417
---

Source: docs/engineer/reviews/2026-06-02-wave-4-amendments-review-of-414.md G2 (Operator Jan 2025 was preview, not GA)

Brief: F4 amendment pins OpenAI Operator to `Jan 2025 OpenAI Operator GA`. Operator launched as research-preview Jan 2025 (Pro-tier only); broader availability shifted through 2025. The "GA" label is loose. One-word fix on the brief PR amendment commit: reword to `Jan 2025 launch (Pro preview) → broader availability through 2025`. Folds into the brief-PR (#401) amendment commit alongside G3/G4/G5.

## Acceptance criteria

- [planned] c1: Brief §7 Operator row text reworded to "Jan 2025 launch (Pro preview) → broader availability through 2025" (or equivalent honest framing) on the #401 amendment commit.
- [planned] c2: `bash scripts/doc-check.sh` exits 0 with the reworded text in place.
