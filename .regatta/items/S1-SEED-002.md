---
id: S1-SEED-002
title: minted token JTIs land in approval_events + reaper revocation reachable
kind: feature
lane: server
status: planned
linked_artifact: https://github.com/trilamsr/regatta/issues/195
---

Seed work item; references open GH issue #195.

Trigger to flip from `planned` → live dispatch: same as S1-SEED-001 —
operator picks this when the Phase-S2 queue drains, or when an audit
exposes the dead-code revocation path as a latent gap.

Issue body authority: github.com/trilamsr/regatta/issues/195.

## Acceptance criteria

- [planned] c1: minted token JTI rows persist in `approval_events` and the reaper revocation path executes against a real expired token in a unit test
