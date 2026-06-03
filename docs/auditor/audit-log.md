# Audit log

Reader: customer security team under NDA reviewing the
tamper-evident audit trail.
Read time: 4 minutes.
Expires when: the audit-sink writer ships and the wire format
stabilizes.

## Status

The substrate event log (`substrate_events` table) is the
authoritative tamper-evident timeline today. Every event — including
every `kind=gate_verdict` — is HMAC-signed at append time. The
out-of-band audit sink (S3 / syslog) is queued for MVP-3+; this file
will gain its wire format + retention runbook when that lands.

## What the HMAC chain proves (and does not prove)

The substrate event log is **tamper-evident**, not **reproducible**.

| Claim | Backed by chain? |
|---|---|
| The recorded bytes of event E were not mutated after sign. | yes |
| The producer of event E identified itself with key_id K. | yes |
| Re-running E's producer would yield the same payload. | only when `payload.deterministic = true` |
| The verdict in E is "correct". | no — correctness is a gate-design question, not a chain question |

Issue #550 reframe: re-verification of a `gate_verdict` event proves
the bytes are intact and signed by a known key. It does NOT prove the
verdict would re-execute identically — that property is a per-gate
attribute carried in the payload's `deterministic` flag.

## What is already documented

- The audit sink is operator-configured via `regatta.yaml`
  `telemetry.audit_sink` (`s3://` with Object-Lock-COMPLIANCE
  recommended; `syslog://` supported).
- Trust-boundary discussion lives in
  [`docs/design.md` §Trust boundaries](../design.md#trust-boundaries)
  + [§Tamper-evident audit](../design.md#tamper-evident-audit).
- Out-of-band: the orchestrator host cannot mutate sink contents
  under normal operation.

## Querying the chain today

```sh
export REGATTA_AUDIT_HMAC_KEY=<32-byte hex>
export REGATTA_AUDIT_HMAC_KEY_ID=<key id>
regatta audit verify --run-id <run-id> --db regatta.db --format json
```

Output per gate:

- `hmac_status` — `chain-ok` / `chain-broken` / `chain-unverifiable`.
- `audit_posture` — `reproduce` / `verify-only`. Tells the auditor
  whether re-execution is even meaningful.
- `tool`, `tool_version` — the producer identity journaled into the
  payload at write time. An auditor cannot retroactively change
  these; if a producer is upgraded mid-run, the recorded value
  remains pinned to the version that wrote each verdict.
- `recorded_schema_version` vs `running_schema_version` — surfaces
  schema-skew when the binary running `audit verify` is newer than
  the binary that wrote the verdict.

## What lands when out-of-band sink ships

- Wire format: NDJSON, one record per agent action / gate verdict
  / canary injection.
- Record fields: actor, action, work-item-id, prompt-SHA,
  decision tree, signature, tool, tool_version, deterministic.
- Retention: bounded by sink policy; Object-Lock-COMPLIANCE
  default for S3.
- Replay: `regatta audit-replay --sink <uri> --window 1d` lets
  the operator reconstruct a session offline.

This doc gains those sections when `internal/audit/audit.go`
lands.
