# Audit log

Reader: customer security team under NDA reviewing the
tamper-evident audit trail.
Read time: 3 minutes.
Expires when: audit-sink writer impl lands AND wire format
stabilizes (activation trigger: `internal/audit/` populated).

## Status

Stub. The audit-sink writer is queued for MVP-3+ per `internal/audit/README.md`.
This file is the auditor's bookmark; concrete wire format,
retention semantics, and operator response runbook land when the
impl ships.

## What is already documented

- The audit sink is operator-configured via `regatta.yaml`
  `telemetry.audit_sink` (`s3://` with Object-Lock-COMPLIANCE
  recommended; `syslog://` supported).
- Trust-boundary discussion lives in
  [`docs/design.md` §Trust boundaries](../design.md#trust-boundaries)
  + [§Tamper-evident audit](../design.md#tamper-evident-audit).
- Out-of-band: the orchestrator host cannot mutate sink contents
  under normal operation.

## What lands when impl ships

- Wire format: NDJSON, one record per agent action / gate verdict
  / canary injection.
- Record fields: actor, action, work-item-id, prompt-SHA,
  decision tree, signature.
- Retention: bounded by sink policy; Object-Lock-COMPLIANCE
  default for S3.
- Replay: `regatta audit-replay --sink <uri> --window 1d` lets
  the operator reconstruct a session offline.

This doc gains those sections when `internal/audit/audit.go`
lands.
