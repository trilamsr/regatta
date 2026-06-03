---
title: "MVR-3-T1 Sigstore — cosign behind signer adapter (skeleton-tier pre-fetch)"
status: active
summary: Pre-fetch skeleton for MVR-3-T1 cosign-keyless wedge gated behind the P3.8 signer adapter; full spec re-spawns when MVR-3 trigger fires (5 paying customers OR cosign-specific customer ask). Locks scope, prior-art, risks, test plan, dep-order so the trigger-time dispatch is a fill-in rather than a green-field design.
---

# MVR-3-T1 Sigstore — cosign behind signer adapter (skeleton-tier pre-fetch)

_Author: design subagent, 2026-06-03. Skeleton-tier per `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 Phase MVR-3 row T1 (S, 1-2 wks, dep=cosign). This spec is the pre-fetch contract; it does NOT dispatch implementer subagents._

Cites: `feedback_research_design_principles` (adopt cosign over bespoke crypto), `feedback_decision_priority` (UX > ease > performance > best-practices > velocity), `feedback_grade_rubric` (B/A/A+ scorecard at dispatch time), `feedback_deletion_default` (every PR answers "what got smaller?"), `feedback_spec_pattern_authority` (deviation re-spawns the design subagent).

Prior-art baseline: `docs/engineer/specs/2026-06-01-w10-sigstore-design.md` (43 KB Wave 1 design) is the source-of-truth for the full surface. This skeleton inherits its decisions and only re-litigates the slice MVR-3 ships — see §0.

---

## 0. Scope (in / out)

### In scope (MVR-3-T1)

- `internal/sign/signer.go` interface `{Sign(ctx, bytes) ([]byte, error); Verify(ctx, bytes, sig) error}` with two impls: `local` (HMAC, default per Phase S substrate) and `cosign` (keyless CI flow + self-signed-key local-dev fallback).
- One config knob `signer.kind: {local,cosign}` (regatta.yaml top-level). Default `local` preserves Phase-S behavior.
- Cosign-keyless flow via GitHub OIDC for CI-signed artifacts; self-signed-key file (`~/.regatta/cosign.key`) for local-dev.
- Rekor public-good transparency-log inclusion proof captured at sign time, verified at load time on policy bundles + pricing tables.
- Adapter sits behind the P3.8 signer-adapter seam (`docs/engineer/specs/2026-06-01-adapter-contracts-design.md`) — no new exported surface beyond the interface.

### Out of scope (MVR-3-T1)

- SLSA L4 hermetic builds (W10's full L3 ambition stays in the W10 spec for later).
- In-toto agent-decision attestations (W10.2, separate wedge).
- Hardware-key support (YubiKey / TPM / KMS — followup once a regulated buyer asks).
- Private TUF root (air-gapped deployments — followup).
- OCI registry as artifact store — sigs stay alongside artifacts on FS / substrate event payload.
- Operator-facing key-rotation UI — `regatta sign rotate` CLI only.

## 1. Prior art (cite version + license)

| Primitive | Adopted from | Version | License | What we take |
|---|---|---|---|---|
| Keyless OIDC signer | [sigstore-go](https://github.com/sigstore/sigstore-go) | v0.6.x (2026 line) | Apache-2.0 | Go API for `Sign` + `Verify` against the public Fulcio CA |
| Rekor transparency log | [Rekor](https://github.com/sigstore/rekor) public-good instance | v1.3.x | Apache-2.0 | Inclusion-proof shape + verification client |
| cosign CLI (fallback) | [cosign](https://github.com/sigstore/cosign) | v2.4.x | Apache-2.0 | Shell-out path if sigstore-go p99 latency exceeds 100ms (abandon-criterion fallback per roadmap §4) |
| Local HMAC default | `contracts/schemas/sign.go` (regatta, shipped) | n/a | repo-internal | Canonicalization + HMAC shape; reused as `local` adapter impl |

Rejected alternatives (defended at dispatch time): bespoke Ed25519 signer (re-inventing crypto), GPG-based supply chain (operator UX regression vs cosign keyless), `gosec`-style code-signing only (does not cover policy bundles + pricing tables).

## 2. Architecture (high-level)

```
internal/sign/
  signer.go          // interface + Register() registry
  local.go           // HMAC impl (existing, lifted from contracts/schemas/sign.go)
  cosign.go          // sigstore-go impl, behind build tag if dep weight concerns
  cosign_cli.go      // shell-out fallback (cosign binary), opt-in via config
internal/policy/
  loader.go          // calls signer.Verify() at load time; fails closed on missing sig
internal/pricing/
  loader.go          // same — pricing table load asserts signature + Rekor proof
cmd/regatta/sign.go  // CLI: regatta sign {policy,pricing,release} + rotate
```

Boot-time registration mirrors `database/sql` driver pattern (per repo `internal/adapters/` convention). One adapter per binary build (resolved at config-load, not per-call).

## 3. Key risks (≥6 named)

| # | Risk | Mitigation |
|---|---|---|
| R1 | Rekor public-good outage breaks sign hot path | Sign uses local-cache + async Rekor submit; verify allows a 24h grace window for inclusion-proof fetch (config-gated) |
| R2 | sigstore-go dep brings >50 transitive deps + binary bloat | Build-tag gated (`-tags sigstore`); default build stays HMAC; bloat measured at PR time vs `feedback_deletion_default` |
| R3 | cosign-keyless OIDC requires GitHub-Actions identity — local-dev cannot reach | Self-signed-key fallback file path; `regatta sign --keyless=false` flag overrides |
| R4 | Pricing-applied-twice integrity vector (cited in W10 spec §9 R-A4) | Pricing-table loader asserts sig + Rekor proof BEFORE the cost-governor reads any row |
| R5 | Key-rotation drill (per `feedback_root_cause`) — rotating cosign root breaks every cached verification | `regatta sign rotate` re-signs all extant bundles in one transaction; Rekor entries are append-only so old proofs stay valid |
| R6 | Adapter swap fragility — operator flips `signer.kind: cosign` mid-run | Boot-time freeze; runtime swap returns error; documented in operator runbook |
| R7 | Signer latency p99 > 100ms on hot path | Abandon-criterion per roadmap §4: swap sigstore-go → cosign-CLI shell-out OR revert to HMAC local |
| R8 | OIDC token theft from CI logs leaks signing identity | OIDC token is short-lived (5 min default); never logged; redaction sweep in `internal/sign/cosign.go::Sign` |

## 4. Test plan (≥8)

1. `TestSignerInterface_LocalAndCosign_RoundTrip` — Sign then Verify byte-equal for both adapters.
2. `TestSignerVerify_RejectsTamperedBytes` — flip one byte in the payload; Verify must fail.
3. `TestSignerVerify_RejectsTamperedSignature` — flip one byte in the sig; Verify must fail.
4. `TestPolicyLoader_FailsClosed_OnMissingSig` — policy bundle without `.sig` sidecar refuses to load.
5. `TestPricingLoader_FailsClosed_OnRekorInclusionMissing` — sig present, Rekor proof absent → load error.
6. `TestSignerAdapter_BootFreeze` — config swap mid-run returns `ErrAdapterFrozen`.
7. `TestRegattaSignRotate_ReSignsAllBundles` — rotate root key; every bundle re-signed in one tx; old sigs invalidated.
8. `TestCosignKeyless_HappyPath_StubbedOIDC` — mock GH OIDC token endpoint; assert Fulcio cert chain validated.
9. `TestCosignKeyless_RekorGraceWindow` — Rekor stub returns 503; verify still passes within 24h grace.
10. `BenchmarkSignerHotPath_LocalVsCosign` — p99 latency budget assertion (cosign ≤100ms; HMAC ≤1ms).
11. `FuzzCanonicalizer` — JSON canonicalization for sig must be deterministic across whitespace permutations.

## 5. Dep order

1. **MUST be merged first:** P3.8 signer-adapter seam (`docs/engineer/specs/2026-06-01-adapter-contracts-design.md`) — Phase MVR-2 or earlier; this wedge has no dep beyond a stable `internal/sign.Signer` interface.
2. **Soft dep:** S3-T3 HMAC key-rotation drill (`docs/engineer/specs/2026-06-02-s3-t3-key-rotation-drill.md`) — provides the operator runbook this wedge extends to cosign key rotation.
3. **No dep on W11 / W12 / W7-Wave2** — cosign-keyless is orthogonal to blackboard, billing, and operator UI.
4. **Trigger:** MVR-3 entry per roadmap §4 (5 paying customers OR cosign-specific ask). Skeleton stays unflighted until trigger fires.

## 6. Grade rubric (filled at dispatch time)

Per `feedback_grade_rubric`: PR body MUST post scorecard verbatim with B/A/A+ targets. This skeleton commits the criterion list; targets get filled when the design re-spawns at trigger time.

| Criterion | B (must) | A (should) | A+ (aspires) |
|---|---|---|---|
| Tests pass `make check` clean | _filled at dispatch_ | _filled_ | _filled_ |
| Signer adapter swap is one-line config | _filled_ | _filled_ | _filled_ |
| Sign hot-path p99 latency | _filled_ | _filled_ | _filled_ |
| Rekor inclusion-proof verified end-to-end | _filled_ | _filled_ | _filled_ |
| Deletion ledger | _filled_ | _filled_ | _filled_ |
| Operator runbook delta | _filled_ | _filled_ | _filled_ |

## 7. What got smaller

Skeleton-tier defers the full 43 KB W10 design's L4 ambition + in-toto + OCI + hardware-key surface to followups. MVR-3-T1 ships ONLY the cosign-keyless + Rekor + adapter-seam slice — minimum signing surface that closes the "publication-credible audit chain" criterion blocking research-mode dispatch.
