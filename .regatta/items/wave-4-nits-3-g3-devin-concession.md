---
id: WAVE-4-NIT-3
title: brief §1 — Devin row concession (re-plan loop regatta lacks)
lane: self-host
status: planned
linked_artifact: https://github.com/trilamsr/regatta/pull/417
---

Source: docs/engineer/reviews/2026-06-02-wave-4-amendments-review-of-414.md G3 (Devin differentiation one-sided; doesn't name what regatta concedes)

Brief: F2 amendment added the `regatta wedge they miss` column for 6 competitors. Devin row reads honestly on what regatta has that Devin lacks — but the brief's own §1 calls Devin's re-plan loop "the pattern to copy", so the differentiation column should also name what Devin has that regatta concedes. Half-sentence fix on the brief PR amendment commit: append "regatta concedes Devin's autonomous re-plan loop; recovers via P2 HITL + CUE plans" to the Devin row. Folds into the #401 amendment commit alongside G2/G4/G5.

## Acceptance criteria

- [planned] c1: Brief §1 Devin row appends the concession half-sentence on the #401 amendment commit; no other rows altered.
- [planned] c2: `bash scripts/doc-check.sh` exits 0 with the added text in place.
