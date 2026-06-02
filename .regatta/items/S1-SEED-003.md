---
id: S1-SEED-003
title: tighten Notifier interface to enforce contract invariants
kind: feature
lane: server
status: planned
linked_artifact: https://github.com/trilamsr/regatta/issues/198
---

Seed work item; references open GH issue #198.

Trigger to flip from `planned` → live dispatch: when the approval-gate
production notifier wiring lands (Phase S2 follow-up). The looser
interface is a known A+-aspiration leftover from #134 review.

Issue body authority: github.com/trilamsr/regatta/issues/198.

## Acceptance criteria

- [planned] c1: Notifier interface rejects the seven invariant-violating shapes called out in #134 review notes via compile-time + runtime checks
