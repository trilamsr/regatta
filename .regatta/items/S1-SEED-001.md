---
id: S1-SEED-001
title: approval-gates end-to-end integration test + property fold≡status
kind: feature
lane: server
status: planned
linked_artifact: https://github.com/trilamsr/regatta/issues/182
---

Seed work item for the markdown adapter; references open GH issue #182.

Trigger to flip from `planned` → live dispatch: when the operator runs
`regatta serve` against this repo and decides this is the next item.
Until then the catalog row stays parked so Phase-S2 has something to
chew on once S1-T3 (boot-prompt → brief converter) lands.

Issue body authority: github.com/trilamsr/regatta/issues/182.

## Acceptance criteria

- [planned] c1: end-to-end test asserting fold≡status property lands in `internal/orchestrator/state/` per #182 body
