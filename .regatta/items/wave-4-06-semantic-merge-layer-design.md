---
id: WAVE-4-06
title: semantic-merge layer above CRDT for P6+P9 blackboard
lane: self-host
status: planned
dependencies: WAVE-4-03
linked_artifact: https://github.com/trilamsr/regatta/pull/401
---

Source: docs/engineer/specs/2026-06-02-wave-4-amendments.md Lens-8 (semantic-merge gap; Yjs is the floor, L4 adversarial judge is the ceiling) + docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md §6

Brief: §6 24-mo prediction softened from "collapse two wedges into one library" to "substrate + semantic layer". CRDTs (Yjs/Automerge) merge syntactically — text + JSON; agent disagreement is semantic — two refactors that touch disjoint lines but contradict in intent. regatta's contribution above the CRDT substrate is the **L4 cross-family judge gate fired on merge-conflict-detected**. Scope: design spike (no implementation) — pick one of (a) Yjs server-side peer + judge-arbitration callback or (b) Automerge with branch-per-agent + judge-mediated rebase. Output: design doc covering substrate choice rationale, judge-invocation surface, integration point with WAVE-4-03's rubric schema, failure-mode catalog. Depends on WAVE-4-03 because the judge surface this layer calls into is defined there.

## Acceptance criteria

- [planned] c1: Design doc at `docs/engineer/specs/p6-p9-semantic-merge-layer.md` covering substrate pick (Yjs vs Automerge), peer-vs-branching tradeoff, judge-call surface, failure modes.
- [planned] c2: Reference to WAVE-4-03's CUE rubric schema for the judge-invocation contract (no schema drift; rubric defined once).
- [planned] c3: 24-mo bet-against signal pinned: design doc names what would falsify the "substrate exists / regatta ships the layer" claim by 2027-12-01 (e.g. no Yjs/Automerge production-agent examples).
