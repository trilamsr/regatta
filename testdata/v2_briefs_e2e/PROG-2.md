---
id: PROG-2
kind: program
title: MVP-2 conditional-DAG acceptance fixture
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: scan incoming alerts and tag severity
- [planned] c2: deep-remediate high-severity findings
- [planned] c3: fast-path low-severity findings

## DAG shape

F-SCAN runs unconditionally, declares `outputs_schema.severity: enum[high,
low]`. Two predicated outgoing edges fan out: F-SCAN → F-DEEP when
`out.severity == "high"`, F-SCAN → F-QUICK when `out.severity == "low"`.
`default_next` points at F-QUICK so the brief satisfies
`ErrEdgeMissingDefault`. The accompanying v2 brief lives in the e2e test
itself; this markdown is the parent work-item the brief decomposes.
