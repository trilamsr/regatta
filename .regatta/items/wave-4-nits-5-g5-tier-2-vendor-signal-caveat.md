---
id: WAVE-4-NIT-5
title: brief §0 6-mo — tier-2-evidence caveat on vendor announcement signal
lane: self-host
status: planned
linked_artifact: https://github.com/trilamsr/regatta/pull/417
---

Source: docs/engineer/reviews/2026-06-02-wave-4-amendments-review-of-414.md G5 (vendor announcements observable but softer than metrics)

Brief: §0 6-mo bet-against row pins two failure signals: <50 combined installs/mo by 2026-12-01 (tier-1 metric) AND "Anthropic announces Skills first-party-only policy" (tier-2 vendor-announcement). G5 nit notes vendor announcements are observable but lower-confidence than metrics. Comment-noise dodge — optional one-line caveat in the §0 6-mo failure-signal cell explicitly flagging the vendor-announcement as tier-2 evidence (lower confidence than install-count). Folds into the #401 amendment commit alongside G2/G3/G4.

## Acceptance criteria

- [planned] c1: Brief §0 6-mo `Failure signal by deadline` cell adds a tier-2 caveat to the Anthropic-announcement clause on the #401 amendment commit; install-count signal stays unannotated as tier-1.
- [planned] c2: Caveat wording is one short clause, not a paragraph; respects §0 table density.
- [planned] c3: `bash scripts/doc-check.sh` exits 0 with the caveat in place.
