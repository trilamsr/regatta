---
id: S2-T2
title: adversarial reviewer as first-class L4 gate
lane: self-host
status: planned
linked_artifact: docs/engineer/autonomous-session-prompt.md#L30
---

Source: docs/engineer/autonomous-session-prompt.md#L30

bake the Claude-Code-side reviewer prompt into `internal/gates/`. Today it lives only in dispatch prompts. NEW. Default model: Sonnet 4.6, escape hatch via `regatta.yaml: gates.l4.model`.

<!-- source-sha256: 9eb857e5e75d4198008174c343f5c8d774ac9ad925e6899d2a7caf3419e09324 -->

## Acceptance criteria

- [planned] c1: Land the PRIORITY entry "S2-T2 adversarial reviewer as first-class L4 gate" per the boot prompt; see source line 30.
