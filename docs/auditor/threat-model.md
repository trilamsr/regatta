# Threat model

Reader: customer security team under NDA; internal auditor.
Read time: 10 minutes.
Expires when: §Threat Model in `docs/design.md` materially changes.

## Status

The authoritative threat model lives in
[`docs/design.md` §Threat Model](../design.md). This file is the
auditor read-order entry point + cross-reference into the design
doc; it does not restate. Read this file first, then jump to the
named section for depth.

## Read order

1. [Adversary stance](../design.md#adversary-stance-normative) -
   what attacker we assume. Includes the explicit list of
   capabilities we model.
2. [Assets](../design.md#assets) - what we are protecting.
3. [Principals](../design.md#principals) - the trust roots and
   their privileges.
4. [Trust boundaries](../design.md#trust-boundaries) - where data
   crosses a privilege gradient + what signing happens at each
   boundary.
5. [Threats explicitly defended](../design.md#threats-explicitly-defended)
   - the mitigation-with-mechanism list. Each threat names the
   gate or pattern that closes it.
6. [Threats explicitly not defended](../design.md#threats-explicitly-not-defended)
   - the deliberate gaps; read this before procurement.
7. [Tamper-evident audit](../design.md#tamper-evident-audit) -
   the out-of-band log mechanism; see also
   [audit-log.md](audit-log.md) when that doc lands.

## Procurement quick reference

- **Adversary class:** active, in-band, model-API-cooperative.
  Side-channel adversary out of scope; egress-content scanning is
  on the roadmap but not load-bearing today.
- **Single-tenant by default:** multi-tenant boundary lands at
  Phase 3 P3.5; pre-Phase-3 deployments are self-hosted per
  customer.
- **Tamper-evident audit:** S3 Object-Lock-COMPLIANCE or syslog;
  the orchestrator host cannot mutate sink contents.
- **Reproducibility:** see [reproducibility.md](reproducibility.md)
  for `-trimpath` + `SOURCE_DATE_EPOCH` + signed-prompt discipline.

## What this file is NOT

- A SOC 2 attestation. Regatta is pre-v1.0; an attestation lands
  after the first paying-customer deployment.
- A penetration-test report. Adversarial-corpus + canary
  injection are internal-only mechanisms; the test artifacts are
  available to customer auditors on request via the channel in
  [`SECURITY.md`](../../SECURITY.md).
- A public bug-bounty channel. Closed-source.
