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

## STRIDE matrix

Each row is a Regatta component; each column is a STRIDE category.
Cell content names the defense + the file:line where it lives.
Empty cell = explicit residual risk (see §"Threats explicitly not
defended" in design.md).

| Component | Spoofing | Tampering | Repudiation | Information disclosure | Denial of service | Elevation of privilege |
|---|---|---|---|---|---|---|
| `regatta.yaml` config load | CUE schema + signed `main` SHA gate (`internal/config/validate/load.go`) | L0 immutability over `regatta.yaml` itself (`internal/gates/l0/refs.go`) | HMAC over emitted plan + audit log (`contracts/schemas/sign.go`) | Operator-owned; not exposed | `timeout_seconds` per gate; budget caps (`contracts/schemas/regatta.v1.cue`) | `safety.agent_creds_scope` enum; CODEOWNERS-gated edits |
| Spec source (markdown_catalog / GH Issues) | Source SHA pinned + verified per WorkItem (`contracts/schemas/spec_adapter.go:SourceRef`) | L0 byte-equality + NFC + invisible-glyph strip (`internal/gates/l0/normalize.go`) | Read-only by orchestrator; no rep risk | Reads are local | `MinPollInterval` capability (`contracts/schemas/spec_adapter.go`) | Adapter cannot mutate source (no `Create()` method per `docs/rfcs/0001-mvp-1-program-publish.md`) |
| Worker agent (Claude) | `claude --resume` session-id + worktree path (`internal/orchestrator/spawner/claude.go`) | Re-run `commands_run` independently; mismatch fails closed (`internal/program/handoff.go:ReRunMismatch`) | Signed handoff per worker run; prev_record_hash chains (`contracts/schemas/handoff.schema.json`) | Spawn-scoped env; no inherited creds (P4) | `iteration_cap` + `spend_cap_usd` (`contracts/schemas/regatta.v1.cue`) | Worker cannot self-merge; L6 = branch protection (`scripts/apply-branch-protection.sh`) |
| Gate runners (L0, security) | Embedded prompts pinned via `contracts/schemas/regatta.v1.cue` `#Prompts` | Each GateResult signed; `Sign` requires MinKeyLen=32 (`contracts/schemas/sign.go:Sign`) | HMAC + prev_record_hash chain (`contracts/schemas/gate_result.schema.json`) | `Secret` wrapper redacts API keys (`contracts/schemas/secret.go`) | `ctx` with timeout from regatta.yaml | Gates run in same trust domain as orchestrator; no privilege gradient inside |
| Orchestrator state (sqlite) | Single-writer pool (`internal/orchestrator/state/state.go:Open` `SetMaxOpenConns(1)`) | Schema-version migration + forward-only (`internal/orchestrator/state/state.go:migrate`) | Per-event row + heartbeat anchors (`contracts/schemas/gate_result.go:TelemetryHeartbeat`) | File-system permissions (operator-owned) | `busy_timeout(5000)` pragma (`internal/orchestrator/state/state.go:DSN`) | DB path operator-owned; no remote write surface |
| Audit sink (S3 / syslog) | Operator-controlled credentials, out of orchestrator scope | Object-Lock-COMPLIANCE mode (S3); hash-chain per writer (`contracts/schemas/gate_result.schema.json` `prev_record_hash`) | Hash chain reveals dropped records | Sink in customer VPC; no third-party transit | Backpressure handled by writer (Phase P2.6 Vector when needed) | Read access governed by customer IAM |
| Release pipeline | SSH-signed tag verified via GitHub API (`.github/workflows/release.yml`) | `actions/attest-build-provenance@v2.4.0` SLSA attestation pinned by SHA (`.github/workflows/release.yml`) | `gh release create` writes immutable artifact + checksum (`.github/workflows/release.yml`) | Provenance + SBOM bundled with release artifact | Single-arch matrix today | `id-token: write` only in release job (`.github/workflows/release.yml`) |

Empty Information-Disclosure cells on operator-owned components are
deliberate residual risk — those surfaces are governed by customer
IAM, not Regatta code.

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
