---
id: WAVE-4-NIT-4
title: brief §0 24-mo row — parallel "floor/ceiling" wording with §6
lane: self-host
status: planned
linked_artifact: https://github.com/trilamsr/regatta/pull/417
---

Source: docs/engineer/reviews/2026-06-02-wave-4-amendments-review-of-414.md G4 (§0/§6 wording parallel; semantic-merge gap framing)

Brief: The Lens-8 amendment correctly adds the semantic-merge open-problem row to §6 ("Yjs is the floor, the L4 adversarial judge is the ceiling"), but the §0 24-mo prediction's `Predicted impact` cell uses a different phrasing ("regatta's contribution is the semantic-merge layer above the CRDT, judge-arbitration on intent conflict"). Tightening: §0 24-mo `Predicted impact` cell reuses §6's "Yjs is the floor, L4 adversarial judge is the ceiling" wording so the two sections parallel. Folds into the #401 amendment commit alongside G2/G3/G5.

## Acceptance criteria

- [planned] c1: Brief §0 24-mo `Predicted impact` cell adopts the §6 "floor/ceiling" wording verbatim on the #401 amendment commit.
- [planned] c2: §6 open-problem row wording stays unchanged; §0 and §6 are textually parallel where they describe the same gap.
- [planned] c3: `bash scripts/doc-check.sh` exits 0 with the parallel wording in place.
