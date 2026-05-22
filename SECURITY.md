# Security

Reader: customer procurement / security team under NDA.
Read time: 5 minutes.
Expires when: data-flow, audit-sink, or escalation channel changes.

This file is the procurement document. Vulnerability reporting +
data-handling posture for Regatta deployed against a customer
repository.

## Data handling

Regatta operates on:

- Source code in the target repository (read + write via the
  configured `SpecAdapter`).
- Work-item bodies from the configured spec source (GitHub Issues,
  GitLab Issues, Jira, Linear, markdown_catalog).
- Model-API request + response bodies (sent to the configured
  vendor, e.g. Anthropic).
- Audit-log records emitted to the configured `telemetry.audit_sink`.

Regatta does NOT:

- Send customer source-code lines to log levels below DEBUG.
- Persist full agent prompt bodies in operational logs (prompts are
  signed artifacts in `contracts/prompts/`; only the SHA is logged).
- Cache model-API responses outside the per-PR scratch directory.

## Tamper-evident audit

The audit sink is operator-configured (S3 with Object-Lock recommended;
syslog supported). Sink endpoint is the only out-of-band trust
boundary; the orchestrator host cannot mutate sink contents under
normal operation. See `docs/design.md` §Threat Model §Trust
boundaries.

## Reproducible builds

Release binaries are built with `-trimpath` and honor
`SOURCE_DATE_EPOCH`. Provenance attestation lands with each tagged
release (Wave 3 release workflow). Customer audit teams may request
the attestation file via the escalation channel below.

## Escalation channel

- Security incidents: tri@maydow.com (PGP key on request).
- Procurement / SBOM / attestation requests: tri@maydow.com.

Response SLO during pre-v1.0: best-effort within 5 business days.
Post-v1.0 SLOs are set per-customer in the license agreement.

## What this file is NOT

- A public bug-bounty channel. Regatta is closed-source.
- A code-of-conduct surface (covered by license + employment).
- A SLSA Scorecard target (private repo; mechanisms over badging).
