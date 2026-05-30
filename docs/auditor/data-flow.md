# Data flow

Reader: customer security team under NDA reviewing what Regatta
sends where.
Read time: 3 minutes.
Expires when: telemetry surface changes OR a new vendor sink is
added.

## Inputs

| Source | What | Authority |
|---|---|---|
| Target repository | Source code; work-item bodies; PR diffs | Read + write via `SpecAdapter` and the PR adapter |
| `regatta.yaml` | Adapter config; gate roster; safety budgets | Operator on disk |
| Audit-sink credentials | Object-Lock S3 / syslog endpoint | Operator-provisioned |
| Model-API credentials | Anthropic API key (single-vendor pre-Phase-3) | Operator-provisioned |

## Outputs

| Destination | What | Retention |
|---|---|---|
| Spec source (issues, markdown) | State-flip annotations + clarification items | Same as the spec source itself |
| Target repository | PR branch commits + PR body | Same as the repo |
| Model API (e.g. Anthropic) | Prompts + structured outputs | Vendor's policy |
| Audit sink | Tamper-evident NDJSON records | Operator's sink policy (Object-Lock-COMPLIANCE recommended) |
| Operational stdout/stderr | Leveled structured logs (info+ by default) | Operator's logging stack |

## What Regatta does NOT do

- Send customer source-code lines at log levels below DEBUG.
- Persist full agent prompt bodies in operational logs (prompts
  are signed artifacts in `contracts/prompts/`; only the SHA is
  logged).
- Cache model-API responses outside the per-PR scratch directory.
- Phone home. No telemetry to maintainer-controlled endpoints
  pre-v1.0; opt-in only post-v1.0.

## Cross-references

- Trust boundaries: [`docs/design.md` §Trust boundaries](../design.md#trust-boundaries).
- Audit-log wire format: [audit-log.md](audit-log.md) (stub today;
  full shape lands with the audit-sink writer).
- Procurement-level summary: [`SECURITY.md`](../../SECURITY.md).
