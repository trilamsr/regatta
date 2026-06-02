---
id: WAVE-4-06
title: semantic-merge layer above CRDT for P6+P9 blackboard
lane: self-host
status: planned
dependencies: WAVE-4-03
linked_artifact: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md §7.1 24-mo prediction (CRDT-mediated shared state) + §7.5 G4 (blackboard reducer layer reframe) + §11 MVR-3 dispatch list
---

Source: unified next-horizon roadmap §7.1 24-mo prediction + §7.5 G4 reframe ("blackboard reducer layer" not "semantic merge layer"). Background context: `docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md` §6.

Brief: per §7.1 24-mo prediction, multi-agent collab moves to CRDT-mediated shared state (Yjs/Automerge). CRDTs merge syntactically; agent disagreement is semantic — two refactors that touch disjoint lines but contradict in intent. regatta's contribution above the CRDT substrate is the **blackboard reducer layer** (per §7.5 G4 reframe — not "semantic merge layer") that fires the L4 cross-family judge on merge-conflict-detected. Scope: design spike (no implementation) — pick one of (a) Yjs server-side peer + judge-arbitration callback or (b) Automerge with branch-per-agent + judge-mediated rebase. Output: design doc covering substrate choice rationale, judge-invocation surface, integration point with WAVE-4-03's rubric schema, failure-mode catalog. Depends on WAVE-4-03 because the judge surface this layer calls into is defined there.

## Acceptance criteria

- [planned] c1: Design doc at `docs/engineer/specs/p6-p9-semantic-merge-layer.md` covering substrate pick (Yjs vs Automerge), peer-vs-branching tradeoff, judge-call surface, failure modes.
- [planned] c2: Reference to WAVE-4-03's CUE rubric schema for the judge-invocation contract (no schema drift; rubric defined once).
- [planned] c3: 24-mo bet-against signal pinned: design doc names what would falsify the "substrate exists / regatta ships the layer" claim by 2027-12-01 (e.g. no Yjs/Automerge production-agent examples).
