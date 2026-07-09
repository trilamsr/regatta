# Trap Catalog

`docs/design.md` §Trap Catalog maps each documented AI-agent incident to a
platform-enforcement pattern wired into the architecture. This file
holds the summary table extracted from the README so first-contact
operators don't have to scroll past 20 rows of security prose to reach
the quickstart.

The 13 patterns:

| ID | Pattern |
|---|---|
| P1 | Deterministic gate before AI gate on destructive ops |
| P2 | Two-key approval on irreversible actions |
| P3 | Trusted instructions from `main` only; all other text is data |
| P4 | Least-privilege, ephemeral, environment-scoped credentials |
| P5 | Out-of-band supervisor for limits and kill-switches |
| P6 | Verified grounding for any outward-facing claim |
| P7 | Schema-level scope constraints, not prompt-level |
| P8 | Spend / iteration brakes with mandatory re-approval |
| P9 | Sensitive context segregation |
| P10 | Invisible-glyph normalization + signed prompt artifacts |
| P11 | Agent-artifact release pipelines are themselves attack surface |
| P12 | Inbound vulnerability signals default-escalate |
| P13 | Judge-LLM lineage isolation + read-only metric channel |

P1, P3, P5, P6, P8, P10 each prevent 3+ documented incidents and are
the most load-bearing.

See also:
- [`docs/design.md`](design.md) §Trap Catalog — full catalog with
  incident cross-references
- [`docs/incidents.md`](incidents.md) — primary sources per pattern
