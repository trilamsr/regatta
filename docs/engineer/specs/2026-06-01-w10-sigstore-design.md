---
title: "MVP-4 W10 — Sigstore cosign + Rekor transparency log (design spec, v1)"
status: skeleton-prefetch
summary: "W10 attestation chain — deferred to Phase X per docs/engineer/briefs/2026-06-01-self-host-first.md §4 (no downstream verifier yet)."
---

# MVP-4 W10 — Sigstore cosign + Rekor transparency log (design spec, v1)

_Author: design subagent, 2026-06-01. Scope: roadmap wedge W10 (MVP-4 rank #5). Source-of-truth_:
- `docs/engineer/briefs/2026-05-31-mvp-3-next-level.md` §"W10 — Sigstore / in-toto plan-and-artifact provenance" (MVP-4 rank #5, dependencies W6 + W2).
- `docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md` §3.3 + §10 followup #1 (policy bundle signing — W8 deferred to W10).
- `docs/engineer/specs/2026-06-01-cost-governor-design.md` §9 R1 (pricing drift) + §9 R-A4 caveat (pricing-applied-twice — closes the integrity vector that table tampering can hide behind).
- Memory: `feedback_research_design_principles` (sigstore-go > custom signer), `feedback_grade_rubric`, `feedback_migration_number_lock`, `feedback_deletion_default`, `feedback_doc_check_banned_phrases`.

---

## §1 Goal + non-goal

### 1.1 Goal

Ship a supply-chain signing layer that wraps four classes of regatta artifact (policy bundles, pricing tables, future plan-as-code bundles, release tarballs) behind one `internal/sign/sigstore.{Sign,Verify}` pair. Signing uses cosign keyless flow via GitHub OIDC for CI-produced artifacts and a local-dev self-signed-key fallback for operator workstations. Every signature is logged to the Rekor public-good transparency log; verification asserts both signature validity AND Rekor inclusion proof. Load-time verification on policy bundles + pricing tables + (future) plan-as-code bundles fails closed on missing or mismatched signatures.

### 1.2 Non-goal

- **SLSA L4 build provenance** — deferred. W10 targets SLSA L3 (build provenance for release tarballs via GitHub Artifact Attestations); L4 (hermetic builds, two-party review) is a separate workstream.
- **Hardware-key support v1** — deferred. v1 uses OIDC-keyless (CI) + self-signed-key file (local dev). YubiKey / TPM / KMS integration is a followup once a regulated buyer asks.
- **Private TUF root** — deferred. v1 uses the sigstore-public TUF root (default `tuf-repo-cdn.sigstore.dev`). Private root targets air-gapped deployments; followup.
- **Runtime attestation** (in-toto attestations of agent decisions per run) — deferred to W11 blackboard subscription model where the substrate-event stream becomes the in-toto subject.
- **In-toto SLSA-3 attestation schema for agent decision lineage** (the differentiator called out in the MVP-3 brief) — split into a follow-up wedge W10.2. v1 closes the simpler artifact-signing surface so W8 + cost-gov can swap on top.
- **Operator-facing key-rotation UI** — followup. v1 rotates via re-signing + a documented `regatta sign rotate` CLI.
- **OCI registry as the artifact store** — followup. v1 stores signatures alongside artifacts on the filesystem / substrate event payload; pushing to an OCI registry as cosign's native shape lands when regatta itself ships a registry adapter.

---

## §2 In / Out

### IN

1. **`internal/sign/sigstore` package** — concrete impls of `Sign(ctx, artifact []byte, identity string) (signature []byte, error)` and `Verify(ctx, artifact []byte, signature []byte, expectedIdentity string) error`. Identity = OIDC subject string (e.g. `https://github.com/trilamsr/regatta/.github/workflows/release.yml@refs/tags/v0.x.y`) or a local-dev keypair fingerprint (`local:sha256:<hex>`).
2. **cosign keyless via GitHub OIDC** for CI-produced artifacts. The release workflow signs every published tarball + pricing table + embedded default policy bundle; the signature + Rekor entry UUID are uploaded alongside as `<artifact>.sig` and `<artifact>.bundle.json` (cosign bundle JSON format).
3. **Rekor inclusion proof verification** — every `Verify` call asserts (a) cosign signature validity, (b) Rekor inclusion proof against the public-good Rekor instance (`rekor.sigstore.dev`), AND (c) signing-cert OIDC subject equals `expectedIdentity`. All three are necessary; any single failure ⇒ typed sentinel error.
4. **Policy bundle load-path Verify** — `internal/gates/authz/loader.go` (the W8-created policy-bundle loader; W8 spec §3.3.2 boot path `Authorizer.Hydrate`) calls `sigstore.Verify` before parsing the bundle. Unsigned or mismatched-signature bundle ⇒ `ErrPolicyBundleSignatureInvalid`; tenant stays on prior bundle (atomic swap NO-OPs); fail-closed.
5. **Pricing-table load-path Verify** — `internal/cost/pricing.Load(ctx)` at boot calls `sigstore.Verify` against the embedded `pricing.go` table's signature shipped at `internal/cost/pricing/pricing.go.sig`. Mismatch ⇒ boot fails with `ErrPricingTableSignatureInvalid` + slog `obs.EventPricingTableTamper` ERROR + non-zero process exit. Closes the cost-gov R-A4 caveat ("pricing-applied-twice" defect is invisible to reconciler) by making table-substitution detectable at boot, before any pricing math runs.
6. **Plan-as-code load-path Verify (seam only — full impl when P4 lands)** — `internal/plan/loader.go` declares a `Verifier` interface in v1 with a default impl that calls `sigstore.Verify`. Plan-as-code wedge (P4) wires the actual `.regatta/plans/*.yaml` consumer; W10 ships the seam so P4 lands with zero refactor.
7. **Local-dev fallback keys** — `regatta sign init-dev-key` CLI generates an ECDSA-P256 keypair at `$XDG_DATA_HOME/regatta/keys/dev.{pub,priv}` (default `~/.local/share/regatta/keys/` per XDG Base Directory). `Sign` with identity prefix `local:` uses the dev key + writes to a local Rekor-compatible tlog stub (`$XDG_DATA_HOME/regatta/tlog/`). `Verify` distinguishes `local:` vs OIDC identities by prefix; dev signatures NEVER verify against the public Rekor (no cross-environment leakage).
8. **CI workflow change** — `.github/workflows/release.yml` adds a `cosign sign-blob --identity-token <oidc>` step for every release artifact; uploads `.sig` + `.bundle.json` to the GitHub release. Workflow has `id-token: write` permission scoped to the release job.
9. **OTel attrs** — `regatta.sign.identity`, `regatta.sign.rekor_uuid`, `regatta.sign.verified` (bool), `regatta.sign.artifact_kind` (enum: `policy|pricing|plan|release`), `regatta.sign.verify_micros` on every `Verify` span. Card-cap matches W8 R7 pattern: `rekor_uuid` clamped to 8-char prefix in span attr; full UUID only in audit event payload.

### OUT

- SLSA L4, hardware keys, private TUF root, runtime attestation (per §1.2).
- In-toto attestation of agent decisions (W10.2 followup).
- Operator key-rotation UI (followup).
- OCI registry as artifact store (followup).
- Custom signing scheme — explicitly rejected per `feedback_research_design_principles`. cosign + Rekor are the proven primitives.
- Multi-signature thresholds (m-of-n trust roots) — followup; v1 uses single-identity verification.
- Signature pinning by fingerprint (TOFU-style) — followup; v1 uses OIDC identity matching.

---

## §3 Architecture

### 3.1 Signing API

```go
// internal/sign/sigstore/sign.go (NEW)
package sigstore

// Sign produces a cosign-compatible signature over artifact bytes. identity
// is the OIDC subject for keyless flow (e.g. a GitHub Actions workflow URL)
// OR a local-dev key reference of the form "local:<keyname>".
//
// Signature bytes are the cosign bundle JSON (signature + signing cert chain
// + Rekor inclusion proof). Callers persist the bundle alongside the artifact.
//
// For OIDC identity: requires a valid OIDC token in the ambient environment
// (GitHub Actions sets ACTIONS_ID_TOKEN_REQUEST_TOKEN + ACTIONS_ID_TOKEN_REQUEST_URL);
// fetches an ephemeral signing cert from Fulcio; signs; submits to Rekor;
// returns the bundle JSON.
//
// For local identity: loads the keypair from XDG_DATA_HOME/regatta/keys/<keyname>;
// signs locally; appends to the local tlog stub; returns a bundle JSON whose
// Rekor URL field is the local tlog URI (e.g. file:///.../tlog/<uuid>.json).
func Sign(ctx context.Context, artifact []byte, identity string) (signature []byte, err error)
```

### 3.2 Verification API

```go
// internal/sign/sigstore/verify.go (NEW)

// Verify checks that signature is a cosign bundle over artifact, the signing
// cert's OIDC subject equals expectedIdentity, AND the bundle's Rekor entry
// has a valid inclusion proof.
//
// Returns nil on success; otherwise one of:
//   - ErrSignatureInvalid       — cosign signature does not verify against artifact
//   - ErrIdentityMismatch       — signing cert OIDC subject != expectedIdentity
//   - ErrRekorInclusionInvalid  — Rekor inclusion proof does not verify
//   - ErrRekorUnreachable       — Rekor service did not respond within budget
//   - ErrBundleMalformed        — signature bytes are not a valid cosign bundle
//   - ErrIdentityNotAllowed     — expectedIdentity has prefix "local:" but
//                                  this binary was built with -tags=production
//                                  (defense against local-key bypass in prod)
//
// expectedIdentity prefixes (mutually exclusive):
//   - "https://"   → OIDC keyless flow; verify against sigstore-public Fulcio + Rekor
//   - "local:"     → dev fallback; verify against local keypair + local tlog
//
// Cross-prefix verification is REFUSED — a local: signature presented against
// a https: expected identity returns ErrIdentityMismatch even if bytes happen
// to verify (defense against dev-key promotion to prod).
func Verify(ctx context.Context, artifact []byte, signature []byte, expectedIdentity string) error
```

Sentinels follow the W7 approval-gates pattern (typed errors, no string-matching by callers).

### 3.3 Keyless flow (cosign + Fulcio + Rekor)

W10 adopts the [sigstore-go SDK](https://github.com/sigstore/sigstore-go) (Apache-2.0, CNCF graduated). Per `feedback_research_design_principles` — adopt proven OSS; no custom signing.

```
CI release job (.github/workflows/release.yml)
   │  permissions: { id-token: write, contents: write }
   ▼
1. Build release tarball / embed default policy bundle / freeze pricing table
   │
   ▼
2. Fetch OIDC token via ACTIONS_ID_TOKEN_REQUEST_TOKEN + ..._URL
   │     Subject = "https://github.com/trilamsr/regatta/.github/workflows/release.yml@refs/tags/<tag>"
   ▼
3. cosign sign-blob --bundle <artifact>.bundle.json <artifact>
   │     • POSTs OIDC token to Fulcio (fulcio.sigstore.dev) → ephemeral signing cert
   │     • Signs artifact SHA-256 with cert's private key (held in memory only)
   │     • Submits {sig, cert, artifact-hash} to Rekor (rekor.sigstore.dev)
   │     • Rekor returns inclusion proof (UUID + log index + signed tree head)
   │     • Bundle JSON = {signature, cert chain, Rekor entry, inclusion proof}
   ▼
4. Upload <artifact>.bundle.json to the GitHub release / artifact registry
```

**Operator verification** (post-install or at runtime):

```
regatta startup
   │
   ▼
1. Load embedded artifact bytes (pricing.go.sig, policy bundle .sig)
   │
   ▼
2. sigstore.Verify(ctx, artifact, sig, expectedIdentity)
   │     • Parse bundle JSON
   │     • Verify cosign signature over artifact (offline; pub key in cert)
   │     • Verify cert chain against sigstore-public root (TUF-pinned)
   │     • Verify OIDC subject in cert SAN == expectedIdentity
   │     • Verify Rekor inclusion proof against Rekor's signed tree head
   │       (TUF-pinned Rekor pubkey; offline once tree head is cached)
   ▼
3. Pass → load artifact; Fail → typed sentinel + ERROR slog + non-zero exit
```

**Why keyless**: no long-lived signing key to rotate / lose / leak. The OIDC token is the trust anchor; Fulcio mints a short-lived (10 min) cert. Rekor's transparency log is the durable record. This shape is shared with GitHub Artifact Attestations, npm provenance, PyPI Trusted Publishers — all proven OSS supply-chain adopters.

**Local-dev fallback** (operator workstation, no GitHub OIDC available):

```
regatta sign init-dev-key
   │
   ▼
ECDSA-P256 keypair generated at $XDG_DATA_HOME/regatta/keys/dev.{pub,priv}
   │   Permissions: priv = 0600; pub = 0644.
   ▼
regatta sign --identity local:dev --in artifact --out artifact.sig
   │
   ▼
Bundle JSON written with:
   • signature (ECDSA over SHA-256(artifact))
   • Public key (embedded; no Fulcio cert)
   • Local tlog entry at $XDG_DATA_HOME/regatta/tlog/<uuid>.json
     (append-only file; inclusion-proof stub asserting "uuid present at index N")
```

The local tlog is **not** trust-equivalent to Rekor — it is a developer-experience seam so local-dev workflows can exercise the Verify path without a network call. Production binaries built with `-tags=production` refuse `local:` identities in `Verify` (ErrIdentityNotAllowed); see R5 mitigation.

### 3.4 Rekor logging — every signature pushes; every verify checks inclusion

cosign's default `sign-blob` invocation uploads to Rekor by default (`--no-tlog-upload` is the opt-out and W10 does NOT use it). Each Rekor entry contains:
- artifact SHA-256
- signature bytes
- signing cert (Fulcio-issued, with OIDC subject in SAN)
- log index + UUID + signed tree head

`Verify` rejects signatures whose Rekor inclusion proof does not verify against the current Rekor tree head. The tree head is fetched once per process at startup and cached; subsequent verifies are offline against the cached head. A `regatta sign refresh-rekor-root` CLI re-fetches when the operator wants a fresher head (e.g. after a known Rekor tree update).

**Rekor downtime resilience** (R1 mitigation): verification at boot is OFFLINE-capable once the Rekor tree head is cached. First boot of a fresh install requires Rekor reachability (one-time bootstrap); subsequent boots verify against the cached head with no network call.

### 3.5 Policy bundle load-path Verify integration

W8 spec §3.3.2 declared `Authorizer.Hydrate(ctx)` as the boot path that loads policy bundles from `substrate_events WHERE kind='policy_revision'`. W10 inserts a Verify step:

```go
// internal/gates/authz/loader.go (W8-created; W10 adds the Verify call)
func (a *opaAuthorizer) loadBundle(ctx context.Context, rev PolicyRevisionPayload) error {
    expectedIdentity := a.cfg.PolicyBundleSignerIdentity  // operator-configured per tenant
    if err := sigstore.Verify(ctx, rev.canonicalBytes(), rev.Signature, expectedIdentity); err != nil {
        // Fail-closed: tenant stays on prior bundle (atomic swap NO-OPs).
        return fmt.Errorf("policy bundle %s: %w", rev.BundleSHA256[:8], errors.Join(ErrPolicyBundleSignatureInvalid, err))
    }
    // ... existing W8 OPA compile + store-swap path proceeds
}
```

**W8 spec §3.3.1 already declared `BundleSHA256` in the payload**; W10 adds a `Signature []byte` field (cosign bundle JSON) to the same payload. Wire-back-compat for tenants on pre-W10 deployments: a `policy_revision` event with empty `Signature` is accepted ONLY when `safety.sign.policy_required: false` (CUE config field, default `true` — fail-closed). Operators who opt out of signing (e.g. air-gapped, no CI) flip the flag with documented risk.

### 3.6 Plan-as-code load-path Verify (seam — full impl deferred to P4)

```go
// internal/plan/loader.go (W10 stub; P4 implements consumer)
package plan

type Verifier interface {
    Verify(ctx context.Context, artifact []byte, signature []byte, identity string) error
}

// DefaultVerifier wraps sigstore.Verify.
var DefaultVerifier Verifier = sigstoreVerifier{}
```

P4 (plan-as-code wedge) wires this into its `.regatta/plans/*.yaml` consumer. W10's contribution: the seam exists; P4 implements zero plumbing — drop in `DefaultVerifier` and call `.Verify` before YAML parse.

### 3.7 Pricing-table verification at boot

cost-gov W2's `pricing.go` is currently a Go-source `var` map. W10 ships a parallel signed table format:

```
internal/cost/pricing/
├── pricing.go              # the var map (unchanged)
├── pricing.go.canonical    # NEW: canonical-JSON of the same map, written by go generate
├── pricing.go.sig          # NEW: cosign bundle over .canonical
└── pricing_verify.go       # NEW: boot-time Load() calls sigstore.Verify
```

```go
// internal/cost/pricing/pricing_verify.go
//go:embed pricing.go.canonical
var pricingCanonical []byte

//go:embed pricing.go.sig
var pricingSignature []byte

// Load is called once at boot from cmd/regatta/serve.go. Verifies the embedded
// canonical bytes match the var map (defense against pricing.go drift relative
// to the canonical file) AND that the cosign signature verifies against the
// canonical bytes under the release-workflow OIDC identity.
func Load(ctx context.Context, expectedIdentity string) error {
    // 1. Re-canonicalize the in-memory var map; must equal pricingCanonical
    //    (catches the "edit pricing.go but forget to regenerate canonical" defect).
    if !bytes.Equal(canonicalize(table), pricingCanonical) {
        return ErrPricingTableCanonicalDrift
    }
    // 2. Verify cosign signature over canonical bytes.
    if err := sigstore.Verify(ctx, pricingCanonical, pricingSignature, expectedIdentity); err != nil {
        return errors.Join(ErrPricingTableSignatureInvalid, err)
    }
    return nil
}
```

**Closes cost-gov §9 R-A4 caveat**: the "pricing-applied-twice" defect was invisible to the reconciler when both sides apply the same (potentially-tampered) table. W10 makes the table itself non-tamperable at boot: any local edit to `pricing.go` without re-running CI (which re-signs against the release OIDC identity) fails `Load` with `ErrPricingTableSignatureInvalid`. The reconciler's drift signal becomes a redundancy, not the sole defense.

`go generate ./internal/cost/pricing/...` re-emits `pricing.go.canonical`; CI signs it; PR review includes the .sig diff. Operators running locally can re-sign with their dev key (`regatta sign --identity local:dev --in pricing.go.canonical --out pricing.go.sig`) — but the binary must then be built with `-tags=dev` else `Verify` refuses `local:` identities.

### 3.8 OTel attrs (per W6 OTel backbone)

| Attribute | Type | Cardinality | Notes |
|---|---|---|---|
| `regatta.sign.identity` | string | bounded by deploy (≤ 10² identities across CI workflows + tenants) | safe |
| `regatta.sign.rekor_uuid` | string | 8-char prefix; full UUID in audit event payload only | R7-mirror — bounds cardinality |
| `regatta.sign.verified` | bool | 2 | safe |
| `regatta.sign.artifact_kind` | string | fixed enum (`policy|pricing|plan|release`) | safe |
| `regatta.sign.verify_micros` | int | n/a | budget regression — see A-tier bench |
| `regatta.sign.failure_reason` | string | fixed enum (six sentinels from §3.2) | only emitted on `verified=false` |

Tracer factory + span propagation reuse W6 conventions verbatim. New span name: `sign.verify` (kind=internal); parent is whatever boot/load span called it (e.g. `authz.hydrate`, `cost.pricing.load`).

---

## §4 Existing patterns reused (deletion default)

Per `feedback_deletion_default` — every adoption MUST cite the existing pattern it reuses; no new primitive justified without one.

| Reused pattern | Source | What W10 adds |
|---|---|---|
| sigstore-go SDK (cosign + Fulcio + Rekor clients) | `github.com/sigstore/sigstore-go` (Apache-2.0, CNCF graduated) | One `Sign` + one `Verify` wrapper; zero crypto code |
| GitHub Actions OIDC token | `actions/runner` + `id-token: write` permission (built-in) | One CI workflow step adds `cosign sign-blob` |
| Rekor public-good transparency log | `rekor.sigstore.dev` (Linux Foundation hosted) | Default Rekor URL; no operator config required |
| TUF root via sigstore-public TUF | `tuf-repo-cdn.sigstore.dev` (sigstore-go ships the trust root embedded) | Pinned trust root; no operator key management |
| W8 policy bundle loader (`Authorizer.Hydrate`) | W8 spec §3.3.2 | One `sigstore.Verify` call before OPA compile |
| W8 `policy_revision` event payload | W8 spec §3.3.1 (already-signed-by-substrate event) | Adds `Signature []byte` field for the cosign bundle |
| Cost-gov `pricing.go` table | cost-gov spec §3 (Wave 1 hardcoded table) | Adds `pricing.go.canonical` + `.sig` + boot-time Verify |
| W6 OTel attribute set + tracer factory | W6 spec §3 | Adds 6 `regatta.sign.*` attrs; new `sign.verify` span |
| CUE config validator (cost-gov §3.6 pattern) | cost-gov spec §3.6 | Adds `safety.sign.{policy_required, pricing_required, expected_identities[]}` |
| XDG Base Directory convention | Linux desktop spec (POSIX-portable) | Local dev keys + tlog stub live under `XDG_DATA_HOME` |
| Typed sentinel errors (`errors.Is` pattern) | W7 approval-gates + W8 `ErrDenied` family | Six `Err*` sentinels in `internal/sign/sigstore/` |

**What got smaller**:
- **Zero new SQL migrations**. Pricing-table signature is embedded; policy-bundle signature rides the existing `policy_revision` event payload (W8-created); release-artifact signatures live alongside artifacts on the GitHub release. Migration count delta: **0**.
- **Zero new CLI subcommands at the top level**. Signing is automatic in CI; verifying is automatic at boot. Operator-facing CLI = three subcommands under existing `regatta sign` namespace (`init-dev-key`, `refresh-rekor-root`, `--identity ... --in ... --out ...`) — replaces ~150 LoC of bespoke key-management UX that would otherwise be needed.
- **No custom signing scheme, no custom transparency log, no custom trust root**. sigstore-go absorbs all three. Estimated LoC saved: ~2 000 (crypto code + tlog + cert validation).
- **W8 followup #1 (policy bundle signing) collapses into this spec** — W8 spec §10 listed it as a Wave-1 followup; W10 closes it.
- **cost-gov §9 R-A4 caveat shrinks**: the "pricing-applied-twice" defect's exposure window narrows from "always present, mitigated only by drift signal" to "only present if signing is opted out via `safety.sign.pricing_required: false`."

**Rejected** (recorded):
- **Build a custom Ed25519 signer + flat-file tlog**. Rejected per `feedback_research_design_principles`; sigstore-go is proven OSS (CNCF graduated; ≥10k stars; ≥3yr public history).
- **GPG + keyserver** (legacy supply-chain shape). Rejected — keyserver trust model is weaker than Rekor; no transparency log; long-lived keys.
- **Notary v2** (CNCF; OCI-native). Rejected v1 — regatta doesn't ship as an OCI image (yet); cosign sign-blob is the right shape for filesystem artifacts. Revisit when OCI registry adoption lands.
- **Per-tenant private TUF root**. Rejected v1 — adds operator burden (root key custody) without v1 benefit. Followup when an air-gapped buyer asks.
- **Sign every substrate event** (in-toto-style runtime attestation). Rejected v1 — already covered by substrate's `sign+UNIQUE` HMAC pattern (T-S1). Cosign signing of every event is expensive (Rekor write per event) and the threat model differs (substrate HMAC defends in-process; cosign defends cross-boundary). Deferred to W11.

---

## §5 Risk register + mitigations

Severity tags: **S** = ship-blocker, **M** = mitigate-before-merge, **L** = monitor / followup OK.

| ID | Risk | Severity | Mitigation |
|---|---|---|---|
| **R1** | Rekor downtime makes first boot fail; operator can't start regatta | M | Tree head cached in `$XDG_CACHE_HOME/regatta/rekor/root.json` after first successful fetch; subsequent boots verify offline against cached head. First-boot bootstrap requires Rekor reachability — documented as a one-time install precondition. `regatta sign refresh-rekor-root` for forced re-fetch. CLI `--allow-stale-rekor-root <duration>` accepts a cached head up to `<duration>` old (default 30 days; clamped at 90). Operator-doc §"Rekor outage runbook" covers. |
| **R2** | OIDC token expires mid-CI-run; signing fails partway through release | M | cosign sign-blob fetches a fresh OIDC token per artifact (`ACTIONS_ID_TOKEN_REQUEST_TOKEN` is refreshable for the workflow duration). Release workflow signs each artifact in a separate step (failure isolation); a single artifact failure does not roll back prior signatures. Retry policy: 3 attempts with 5s/15s/30s backoff. Operator-doc covers "if release job fails on signing step, rerun the job — Rekor tolerates duplicate entries." |
| **R3** | Identity rotation drift — release workflow renamed / moved; old `expectedIdentity` in operator config no longer matches | M | `safety.sign.expected_identities` is a `[]string` (set semantics), not a singleton. Operators add the new identity, restart, retire the old identity. CUE validator rejects empty list when `safety.sign.policy_required: true`. CI emits the OIDC subject string into the release notes as the identity to whitelist. Documented "identity rotation runbook" in operator doc. |
| **R4** | ToCToU between Verify and Use — bytes verified at T then mutated before consumption at T+δ | M | `Verify` returns nil-or-err only; it does NOT return artifact bytes (no rebinding seam). Callers verify the SAME `[]byte` they then consume — `pricing.Load` re-canonicalizes the in-memory map and compares to the verified `pricingCanonical` bytes, closing the file-system-tamper ToCToU. Policy-bundle path: bundle bytes are in the same `policy_revision` substrate event payload as the signature — atomic write to substrate (sign+UNIQUE) prevents intra-event tamper. Tested in `TestVerify_AndUse_Atomic`. |
| **R5** | Dev key promoted to prod (operator copies `~/.local/share/regatta/keys/dev.priv` to a server) | S | Production builds use `-tags=production`; `internal/sign/sigstore/build_production.go` gates `Verify` to refuse `local:` identities (`ErrIdentityNotAllowed`). `regatta sign --identity local:...` is rejected at runtime in prod builds. CI build verifies the tag is set for release artifacts (`make verify-prod-build`). Property-tested in A+ tier. |
| **R6** | Key compromise (CI OIDC token leaked) — attacker signs a malicious bundle | S | Defense-in-depth: (a) GitHub OIDC tokens are workflow-scoped + 10min-TTL; an exfiltrated token expires before useful misuse window; (b) Fulcio cert binds to the OIDC subject — attacker can sign only as the leaked workflow, not as arbitrary identities; (c) Rekor inclusion proof is public — a malicious signing event is detectable via Rekor monitoring (followup #6); (d) Operator `expected_identities` whitelist is per-tenant — even a successful malicious sign-as-release-workflow only impacts tenants who trust that identity. Documented in security model doc. Tracking issue: `[w10-followup] Rekor monitor + alert pipeline`. |
| **R7** | OTel `rekor_uuid` cardinality blowup | M | 8-char prefix in OTel attrs; full UUID only in substrate audit event payload (`kind=sign_verified` / `kind=sign_failed`). Mirrors W8 R7 mitigation pattern. Lint test asserts attr value length ≤ 8 across the codebase. |
| **R8** | sigstore-go SDK breaking-change drift | L | Pinned at `go.mod` version (initial: v1.x — latest stable). Dependabot watches; PRs gated through `make check` (which exercises every `Sign` + `Verify` test path). SDK API is small (~6 calls used); migration cost on major bump is bounded. Followup tracking issue: `[w10-followup] sigstore-go v2 migration` opens when v2 lands. |
| **R9** | Rekor public-good instance retired or rate-limits aggressively | L | Public-good Rekor is Linux Foundation-hosted with SLA-class uptime + no fee structure that would justify rate-limiting individual verifiers. Worst case: switch to a private Rekor (followup) or to a different transparency log (CT-style). The Verify API is Rekor-agnostic; the trust root is configurable via `safety.sign.rekor_url` (CUE field; default `https://rekor.sigstore.dev`). Tracking issue: `[w10-followup] private Rekor support`. |
| **R10** | First boot of fresh deploy fails because Rekor is unreachable (network policy / air-gap) | M | First-boot path emits `obs.EventSignVerifyBootstrap reason=rekor_unreachable` WARN + falls back to `--allow-bootstrap-without-rekor` flag (default OFF; explicit operator opt-in). With the flag, boot proceeds but `Verify` records signatures as `verified=false`; load-time gate behaviour stays fail-closed for policy + pricing. Documented runbook covers air-gapped deploy pattern (cache the Rekor tree head + sigstore TUF root in a sidecar artifact; bring online via `regatta sign import-trust-root --from <path>`). Tracking issue: `[w10-followup] air-gap deploy guide`. |

**R1-R10 count: 10 risks**. R5 + R6 ship-blockers covered in-spec; R1-R4, R7, R10 mitigated in-spec; R8, R9 monitored via followup tracking issues.

---

## §6 Named test plan per task (B / A / A+ tiers)

Per `feedback_grade_rubric` — tool-checkable, distinct per tier. Task slugs T1-T6 from §8.

### B — floor (ships)

Per task:

- **T1 (`Sign` + `Verify` wrapper)**:
  - `TestSign_OIDCKeyless_RoundTrips` — happy-path: sign with mocked OIDC token (using sigstore-go test harness), verify returns nil.
  - `TestVerify_IdentityMismatch_ReturnsErrIdentityMismatch` — wrong `expectedIdentity` ⇒ typed sentinel.
  - `TestVerify_RekorInclusionInvalid_ReturnsErrRekorInclusionInvalid` — tampered inclusion proof ⇒ typed sentinel.
  - `TestVerify_BundleMalformed_ReturnsErrBundleMalformed` — malformed signature bytes ⇒ typed sentinel.
  - `TestSign_LocalIdentity_WritesLocalTlog` — `local:dev` identity writes to `$XDG_DATA_HOME/regatta/tlog/`.
- **T2 (CI workflow change — release signing)**:
  - `TestReleaseWorkflow_SignsEveryArtifact` — golden test asserting `.github/workflows/release.yml` has a `cosign sign-blob` step for every artifact pattern.
  - `TestReleaseWorkflow_HasIdTokenWritePermission` — YAML assertion on `permissions: id-token: write`.
- **T3 (policy-bundle Verify integration)**:
  - `TestAuthorizerLoadBundle_ValidSignature_LoadsBundle` — happy path; bundle hydrates into OPA store.
  - `TestAuthorizerLoadBundle_InvalidSignature_PreservesPriorBundle` — fail-closed: invalid sig ⇒ atomic swap NO-OPs; prior bundle still active.
  - `TestPolicyRevisionPayload_BackCompat_EmptySignatureRespectsConfig` — empty `Signature` + `policy_required: false` ⇒ accept; empty `Signature` + `policy_required: true` ⇒ reject.
- **T4 (pricing-table Verify integration)**:
  - `TestPricingLoad_ValidSignature_Succeeds` — happy path; canonical matches in-memory map; sig verifies.
  - `TestPricingLoad_CanonicalDrift_ReturnsErrPricingTableCanonicalDrift` — pricing.go edited but canonical not regenerated ⇒ typed sentinel.
  - `TestPricingLoad_SignatureMismatch_ReturnsErrPricingTableSignatureInvalid` — sig over wrong bytes ⇒ typed sentinel + slog `obs.EventPricingTableTamper` ERROR.
- **T5 (local-dev fallback keys)**:
  - `TestInitDevKey_GeneratesECDSAP256Pair` — keypair file perms 0600/0644; key parses.
  - `TestSignVerify_LocalIdentity_RoundTrips` — sign + verify with `local:dev`; no network call.
  - `TestVerify_LocalIdentity_RefusedInProdBuild` — production-tagged build returns `ErrIdentityNotAllowed`.
- **T6 (OTel + docs)**:
  - `TestVerify_EmitsAllAttributes` — span has all 6 `regatta.sign.*` attrs.
  - `TestVerify_RekorUUIDAttr_Is8CharPrefix` — R7 cardinality guard.

### A — target (expected)

All B, plus:

- `BenchmarkVerify_PolicyBundle_p99Under5Millis` — N=1 000; histogram p99 ≤ 5 ms (offline, tree-head cached).
- `TestVerify_RekorUnreachable_ReturnsErrRekorUnreachable` — Rekor mock returns 5xx ⇒ typed sentinel + cached-tree-head fallback exercised.
- `TestAuthorizerLoadBundle_IdentityRotation_BothIdentitiesAccepted` — `expected_identities` is a set; either matches.
- `TestPricingLoad_ProductionBuild_RejectsLocalSig` — prod build refuses local-signed pricing table.
- `TestReleaseSigningE2E_BuildSignVerify` — end-to-end CI fixture: build a fake release artifact, sign in a mocked-OIDC harness, verify against the expected workflow identity.

### A+ — stretch (aspirational)

All A, plus:

- **Property test** (`rapid`-based) — `Sign(x, id); Verify(x, sig, id)` succeeds for any byte sequence x of length 0..16 KiB and any identity from a generated set; cross-identity `Verify(x, sig, id')` for id' ≠ id always returns `ErrIdentityMismatch`. ≥ 5 000 cases.
- **Mutation-coverage ≥ 95%** on `internal/sign/sigstore/` (via `go-mutesting`). Every sentinel branch survives mutation only when caught by a named test.
- **Rekor inclusion-proof fuzz** — `go-fuzz` over the inclusion-proof verification path; ≥ 10 minutes; zero crashes; every mutated proof rejected via `ErrRekorInclusionInvalid`.
- **Cross-binary signature stability** — sign in CI binary v_N, verify in CI binary v_{N+1}; byte-equal pass for the default policy bundle and pricing table.
- **Cold-path Verify** (first call, tree head fetch) ≤ 500 ms p99 against the public-good Rekor — guards against accidental hot-path Rekor calls.

---

## §7 Grade rubric (verbatim)

### B — floor (ships)

- [ ] `internal/sign/sigstore` package shipped with `Sign(ctx, artifact, identity) (sig, err)` + `Verify(ctx, artifact, sig, expectedIdentity) error`; six typed sentinels defined.
- [ ] Release workflow (`.github/workflows/release.yml`) signs every release artifact via `cosign sign-blob`; uploads `.sig` / `.bundle.json` alongside.
- [ ] Policy-bundle loader (W8 `Authorizer.Hydrate`) calls `sigstore.Verify`; invalid sig ⇒ fail-closed (prior bundle retained).
- [ ] Pricing-table loader (`internal/cost/pricing.Load`) calls `sigstore.Verify` at boot; mismatch ⇒ non-zero exit.
- [ ] Plan-as-code loader stub (`internal/plan/loader.go`) declares `Verifier` interface with default `sigstoreVerifier` impl.
- [ ] Local-dev fallback: `regatta sign init-dev-key` + `regatta sign --identity local:dev` work end-to-end with no network.
- [ ] Production build refuses `local:` identities (`-tags=production` gate).
- [ ] OTel attrs `regatta.sign.{identity,rekor_uuid,verified,artifact_kind,verify_micros,failure_reason}` emitted on every Verify span; `rekor_uuid` clamped to 8-char prefix.
- [ ] **Zero new SQL migrations.** Migration count delta = 0.
- [ ] `make check` clean; every B-tier test in §6 passes.

### A — target (expected)

All B, plus:

- [ ] `BenchmarkVerify_PolicyBundle` p99 ≤ 5 ms offline.
- [ ] Identity rotation supported via `safety.sign.expected_identities []string` set semantics; CUE validator rejects empty list when `policy_required: true`.
- [ ] Rekor unreachable path tested + documented in operator runbook.
- [ ] End-to-end CI fixture (`TestReleaseSigningE2E_BuildSignVerify`) exercises sign + verify with mocked OIDC harness.
- [ ] Adversarial reviewer subagent cleared the PR with zero unaddressed Risk-tier findings (per `feedback_agent_pr_review`).
- [ ] Tracking issues filed for every followup in §10; cited by number in PR body (per `feedback_unaddressed_load_bearing`).

### A+ — stretch (aspirational)

All A, plus:

- [ ] Property test (`rapid`) on Sign/Verify round-trips ≥ 5 000 cases; cross-identity verification rejected in every case.
- [ ] Mutation-coverage ≥ 95% on `internal/sign/sigstore/` via `go-mutesting`.
- [ ] Rekor inclusion-proof fuzz ≥ 10 minutes; zero crashes.
- [ ] Cross-binary signature stability — sig produced by binary v_N verifies in binary v_{N+1}.
- [ ] Cold-path Verify (first call, tree-head fetch) ≤ 500 ms p99.

---

## §8 File-disjoint impl decomposition (preview only)

Full plan PR comes after this spec lands. Preview only; **NOT a task breakdown for execution**. Six tasks, file-disjoint where possible.

| # | Task | Files touched | OWNER notes |
|---|---|---|---|
| **T1** | `Sign` + `Verify` wrapper + six typed sentinels + sigstore-go SDK integration + tree-head cache | `internal/sign/sigstore/sign.go`, `internal/sign/sigstore/verify.go`, `internal/sign/sigstore/errors.go`, `internal/sign/sigstore/cache.go`, `internal/sign/sigstore/*_test.go`, `go.mod` (sigstore-go dep), `go.sum` | OWNS the API; T3 + T4 + T5 + T6 import it |
| **T2** | CI workflow change — sign every release artifact + upload bundle JSON | `.github/workflows/release.yml`, `scripts/release/sign-artifacts.sh`, `scripts/release/sign-artifacts_test.sh` | Independent of T1 once interface signature is frozen; can dispatch in parallel after T1's API lands |
| **T3** | Policy-bundle Verify integration (W8 loader hookup) + `Signature` field on `policy_revision` payload + `safety.sign.policy_required` CUE config | `internal/gates/authz/loader.go`, `internal/authz/policies/payload.go` (add `Signature []byte`), `internal/cuevalidate/sign.cue`, `internal/gates/authz/loader_test.go` | Depends on T1 + W8 already merged; file-disjoint from T4 + T5 |
| **T4** | Pricing-table Verify integration + `pricing.go.canonical` + `pricing.go.sig` + boot-time `Load` + `obs.EventPricingTableTamper` | `internal/cost/pricing/pricing_verify.go`, `internal/cost/pricing/pricing.go.canonical`, `internal/cost/pricing/pricing.go.sig`, `internal/cost/pricing/canonical.go` (go generate), `internal/cost/pricing/pricing_verify_test.go`, `cmd/regatta/serve.go` (one-line `pricing.Load` call) | Depends on T1 + cost-gov W2 already merged; file-disjoint from T3 + T5 |
| **T5** | Local-dev fallback keys + `regatta sign` CLI subcommands + `-tags=production` gate + XDG directory layout | `cmd/regatta/sign.go`, `cmd/regatta/sign_init_dev_key.go`, `cmd/regatta/sign_refresh_rekor_root.go`, `internal/sign/sigstore/build_production.go`, `internal/sign/sigstore/build_dev.go`, `cmd/regatta/sign_test.go` | Depends on T1; file-disjoint from T2 + T3 + T4 |
| **T6** | OTel attrs + `sign.verify` span + audit events (`kind=sign_verified` / `kind=sign_failed`) + operator docs (security model + Rekor outage runbook + identity rotation runbook + air-gap guide) + plan-as-code loader stub | `internal/sign/sigstore/otel.go`, `internal/sign/sigstore/audit.go`, `internal/plan/loader.go` (stub only), `docs/operator/sign.md`, `docs/operator/sign-rekor-outage.md`, `docs/operator/sign-identity-rotation.md`, `internal/sign/sigstore/otel_test.go` | Depends on T1 + T3 + T4; file-disjoint from T2 + T5 |

**Total: 6 file-disjoint tasks**. T1 dispatches first; T2 + T3 + T4 + T5 dispatch in a second wave after T1's API lands; T6 dispatches last (depends on multiple). Migration number lock per `feedback_migration_number_lock`: **N/A — zero migrations added**.

---

## §9 Sequencing

W10 lands **AFTER** W8 (policy-bundle loader exists, `policy_revision` payload schema is stable) AND cost-gov W2 (`pricing.go` exists, ready for canonical + sig sidecar). W10 is **independent** of W7 (operator UI; signing is invisible to the UI v1), W11 (blackboard; W10 is artifact-level, not event-level), W12 (usage rollups).

```
W8 (OPA RBAC, MERGED)                cost-gov W2 (pricing.go, MERGED)
    │                                          │
    └────────────────┬─────────────────────────┘
                     │
                     ▼
                W10 T1 (Sign/Verify wrapper)
                     │
       ┌─────────────┼─────────────┬─────────────┐
       ▼             ▼             ▼             ▼
    W10 T2       W10 T3       W10 T4         W10 T5
   (CI sign)   (policy)     (pricing)     (local-dev)
       │             │             │             │
       └─────────────┴──────┬──────┴─────────────┘
                            ▼
                       W10 T6 (OTel + docs + plan stub)
```

T1 must merge before T2-T5 (API contract dependency). T2-T5 dispatch in parallel after T1. T6 lands last (consumes all preceding interfaces).

**Cross-spec dependency note**: W8 spec §10 followup #1 (policy bundle signing) is closed by W10 T3. cost-gov spec §9 R-A4 caveat is narrowed by W10 T4. Both cross-references explicitly cited in T3 + T4 PR bodies.

---

## §10 Deferred + followups (pre-enumerated)

Per `feedback_unaddressed_load_bearing` — file as gh issues, cite by number in PR body before merge.

1. **SLSA L4 build provenance** — hermetic builds + two-party PR review + reproducible toolchain pinning. Independent of W10's artifact-signing surface.
2. **Hardware-key support** — YubiKey / TPM / cloud-KMS integration for the local-dev fallback path; closes operator-key custody for air-gapped deployments.
3. **Private TUF root** — operator-managed sigstore trust root; required for fully air-gapped deployments.
4. **Runtime attestation** (in-toto SLSA-3 attestation of agent decision lineage) — deferred to W10.2; signs the substrate event stream as the in-toto subject.
5. **Operator-facing key-rotation UI** — wraps `regatta sign refresh-rekor-root` + `expected_identities` edit + bundle re-sign in a UI flow.
6. **Rekor monitor + alert pipeline** — watch Rekor for unexpected sign events under our identities; R6 defense-in-depth.
7. **OCI registry as artifact store** — push signatures to an OCI registry (cosign's native shape); applies when regatta itself adopts OCI for distribution.
8. **sigstore-go v2 migration** — opens when sigstore-go v2 ships; bounded API surface keeps migration cost low.
9. **Multi-signature threshold (m-of-n)** — multiple independent signers required; closes single-identity compromise R6.
10. **Signature pinning by fingerprint (TOFU)** — alternative trust model for deployments that refuse OIDC dependency.
11. **W10.2 — in-toto agent-decision attestation** — full SLSA-3-style attestation of agent decision lineage; the regatta-specific differentiator called out in MVP-3 brief.
12. **Air-gap deploy guide** — documented runbook for caching Rekor tree head + sigstore TUF root in a sidecar artifact (R10 followup).
13. **W8.2 — `policy_required: true` becomes the default for all tenants** — current default is true at the global CUE level but per-tenant override is allowed; rollout wave eliminates the override.

---

## §11 References

- W8 OPA RBAC spec: `docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md` §3.3 (policy bundle loader) + §10 followup #1 (policy bundle signing, closed by W10).
- cost-governor spec: `docs/engineer/specs/2026-06-01-cost-governor-design.md` §9 R1 (pricing drift) + §9 R-A4 caveat (pricing-applied-twice — narrowed by W10).
- MVP-3 brief: `docs/engineer/briefs/2026-05-31-mvp-3-next-level.md` §"W10 — Sigstore / in-toto plan-and-artifact provenance" + §"HMAC stays internal, Sigstore goes external".
- W6 OTel backbone: `docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md` §3 (attribute set + tracer factory; reused for `sign.verify` span).
- sigstore-go SDK: https://github.com/sigstore/sigstore-go (Apache-2.0, CNCF graduated).
- cosign sign-blob: https://docs.sigstore.dev/cosign/signing/signing_with_blobs/.
- Rekor public-good instance: https://docs.sigstore.dev/logging/overview/.
- Fulcio (OIDC → ephemeral cert): https://docs.sigstore.dev/certificate_authority/overview/.
- GitHub Actions OIDC: https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect.
- Memory: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_migration_number_lock`, `feedback_deletion_default`, `feedback_doc_check_banned_phrases`, `feedback_unaddressed_load_bearing`, `feedback_agent_pr_review`.
