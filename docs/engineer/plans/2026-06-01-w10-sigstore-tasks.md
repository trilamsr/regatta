# MVP-4 W10 Sigstore cosign + Rekor — Implementer Task Breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-w10-sigstore-design.md` (#284 merged).
Authority: `feedback_spec_pattern_authority` — implementer deviation from any spec-mandated
pattern (T1 owns the `Sign` + `Verify` API + six typed sentinels + sigstore-go SDK adoption +
tree-head cache; cross-prefix `local:` vs `https:` refusal; `-tags=production` gate refuses
`local:` identities; ZERO new SQL migrations; sigstore public-good Rekor + sigstore-public TUF
root default; bundle JSON shape per cosign sign-blob; OTel attr set verbatim from spec §3.8;
cosign sign-blob `--bundle` upload step per release artifact in CI) MUST re-spawn the design
subagent. NO implementer-chosen alternatives.

Design priority for every decision below (`feedback_decision_priority`): **UX → ease of use
→ best practices → execution speed → velocity**. Grade rubric (`feedback_grade_rubric`)
inherited verbatim from spec §7 — each task carries the spec's B / A / A+ tool-checkable
criteria + the mandatory PR-body scorecard.

---

## Wave overview

- **6 file-disjoint implementer tasks** (T1–T6) per spec §8. Three dispatch waves per spec §9
  sequencing: **Wave A** (T1 + T2 + T5 parallel from `main`), **Wave B** (T3 + T4 parallel
  after T1 merges to `main`), **Wave C** (T6 after Wave B). T2 + T5 land independent of T1
  internals — T2 is the release-workflow YAML + signing shell script (calls `cosign sign-blob`
  binary directly, NOT the Go `Sign` wrapper); T5 is the CLI subcommand layer that wraps
  `Sign` + `Verify` but can compile against a thin local-stub interface until T1 lands then
  rebase. T3 + T4 cannot start until T1's `Verify` API is on `main` (cross-import on six typed
  sentinels). T6 sequences last because its OTel test + audit event scope reaches into every
  preceding task's call-sites.
- **Prereqs (merged to main):**
  - **W8 OPA RBAC** — MERGED. T3 reads `internal/gates/authz/loader.go` (W8-created;
    `Authorizer.Hydrate` boot path) + `policy_revision` event payload (W8 spec §3.3.1 declared
    `BundleSHA256` field; T3 ADDS `Signature []byte` to the same payload).
  - **Cost-governor W2 (`internal/cost/pricing`)** — MERGED. T4 reads the var-map shape and
    adds the parallel signed-canonical sidecar files (`pricing.go.canonical` + `pricing.go.sig`
    + boot-time `Load`).
  - **W6 OTel backbone** — MERGED. T6 reuses the tracer factory + attribute conventions
    verbatim (per spec §3.8); new span name `sign.verify` (kind=internal).
  - **Substrate v2 Wave 1** (#224 event log + sign+UNIQUE + `RegisterPayloadValidator`) —
    MERGED. T6 audit events (`kind=sign_verified` + `kind=sign_failed`) ride the same
    substrate write API; ZERO new migration.
- **Sequence vs parallel:**
  - **Wave A (T1 + T2 + T5 parallel from `main`).** T1 owns the Go API + sentinels + sigstore-go
    SDK pin; T2 owns the CI workflow YAML + signing shell script (uses the `cosign` CLI binary
    directly — no Go-import dependency on T1); T5 owns the operator-facing CLI subcommands
    (`regatta sign init-dev-key`, `regatta sign --identity ...`, `regatta sign
    refresh-rekor-root`) + the `-tags=production` / `-tags=dev` build-tag gate. T5 can compile
    against a local interface stub matching T1's signature until T1 lands, then rebase off
    `main` once T1 merges. Per `feedback_dispatch_strategy`: 3 parallel implementers in
    Wave A, well under the 10-lane cap.
  - **Wave B (T3 + T4 parallel from `main` AFTER T1 merges).** T3 wires the policy-bundle
    `Verify` into W8's `Authorizer.Hydrate`; T4 wires the pricing-table `Verify` into boot.
    Both import `sigstore.Verify` + the typed sentinels — file-disjoint and import-only on T1.
    Dispatch both simultaneously; merge order independent.
  - **Wave C (T6 after Wave B merges).** T6 lands the OTel attrs + `sign.verify` span +
    audit-event emission + plan-as-code loader stub (`internal/plan/loader.go::Verifier`
    interface) + operator docs. T6 reads T3 + T4 call-sites to wire the OTel spans through
    every Verify path.
- **Migration phasing (`feedback_migration_number_lock`):** **ZERO new SQL migrations across
  all six tasks.** Per spec §4 "what got smaller": signing rides the existing W8
  `policy_revision` event payload (T3 adds a `Signature []byte` field — payload schema
  change, not DDL); pricing-table signature is embedded at `internal/cost/pricing/pricing.go.sig`
  (filesystem, not DB); release-artifact signatures live alongside artifacts on the GitHub
  release; T6 audit events ride substrate's existing event-log table via
  `RegisterPayloadValidator` open-extension hook. **Migration count delta: 0.** Implementers
  who feel a migration is needed STOP and re-spawn the design subagent.
- **Deletion default (`feedback_deletion_default` — every PR answers "what got smaller?"):**
  - **T1:** sigstore-go SDK adoption ELIMINATES ~2 000 LoC of custom signing primitives
    (Ed25519 signer + flat-file tlog + cert validation + trust-root pinning) per spec §4 line
    303. Adopts CNCF-graduated proven OSS per `feedback_research_design_principles`. The six
    typed sentinels REPLACE string-matching by callers (mirrors W7 + W8 pattern).
  - **T2:** GitHub Actions OIDC keyless ELIMINATES long-lived signing keys (no rotation
    burden, no key custody, no leak surface). `cosign sign-blob` binary call eliminates a Go
    re-implementation in the release workflow. Zero secrets added to repo.
  - **T3:** Closes W8 spec §10 followup #1 (policy bundle signing was Wave-1 followup; W10 T3
    closes it). The `Signature []byte` field on the existing payload ELIMINATES the parallel
    "signed-policy-revision" event kind that would otherwise be needed.
  - **T4:** Closes cost-gov §9 R-A4 caveat (pricing-applied-twice defect's exposure window
    narrows from "always present" to "only present if signing opted out"). The reconciler's
    drift signal becomes redundancy, not the sole defense. Per spec §3.7 line 263.
  - **T5:** `regatta sign init-dev-key` + `regatta sign --identity local:dev` REPLACE
    ~150 LoC of bespoke key-management UX that custom signing would otherwise need (per spec
    §4 line 302). Three subcommands under existing `regatta sign` namespace — zero new
    top-level CLI subcommand.
  - **T6:** Reuses W6 tracer factory + slog→OTel bridge verbatim. Plan-as-code loader stub is
    interface-only (P4 wires the consumer with zero refactor). The `sign.verify` span is the
    ONE new span name across W10; no parallel naming convention.
- **Concurrency cap (`feedback_dispatch_strategy`):** Wave A = 3 implementers; Wave B = 2;
  Wave C = 1. Peak 3 — well under the 10-lane ceiling. Total 6 PRs across three waves; cap
  budget ~3 sessions assuming auto-merge gates clean.
- **Followup filing (`feedback_unaddressed_load_bearing`):** every load-bearing named-but-deferred
  item in spec §1.2 (non-goals) + §10 (followups, pre-enumerated) is filed as a
  `[w10-followup]` tracking issue PRE-MERGE; PR body cites the issue numbers. §8 of this plan
  pre-enumerates the 13 issue templates so implementers file the deltas not already filed by
  prior tasks.
- **Hygiene gates** (per `feedback_doc_check_banned_phrases` + `feedback_pr_lint_gates`):
  every PR runs `bash scripts/doc-check.sh` (markdown link integrity + banned-phrase lint) +
  `bash scripts/stale-todo.sh` (issue-ref required on TODO/FIXME/XXX past
  WINDOW_DAYS) before push. This plan deliberately references the script + the memory file
  `feedback_doc_check_banned_phrases.md` rather than inlining the literal banned-token list —
  per #297 + #296 fix cycles, plans that inline the token list self-trip the very gate they
  describe. **Implementers READ the memory file for the current list; do NOT copy it into PR
  body or commit messages.** Every PR body MUST end with ` ```release-notes\nnone\n``` ` (or a
  one-line `[FEATURE]`/`[FIX]` line; doc-only PRs use `none`) per
  `feedback_pr_body_release_notes_fence`; grep-verify before push.

---

## §1 File-disjoint table

| Task | Path (exclusive write scope) | Depends-on (Wave + main) | Effort | TDD tests (count: B / A / A+) |
| ---- | --------------------------- | ------------------------ | ------ | ------------------------------ |
| **T1** | `internal/sign/sigstore/sign.go` (NEW); `internal/sign/sigstore/verify.go` (NEW); `internal/sign/sigstore/errors.go` (NEW — six typed sentinels); `internal/sign/sigstore/cache.go` (NEW — Rekor tree-head cache at `$XDG_CACHE_HOME/regatta/rekor/root.json`); `internal/sign/sigstore/{sign,verify,errors,cache}_test.go`; `go.mod` + `go.sum` (sigstore-go SDK dep) | Wave A; W8 + cost-gov W2 merged | M | **5 / 2 / 3** = 10 named |
| **T2** | `.github/workflows/release.yml` (extend with `cosign sign-blob` step + `id-token: write` permission); `scripts/release/sign-artifacts.sh` (NEW — wraps `cosign sign-blob` invocation per artifact); `scripts/release/sign-artifacts_test.sh` (NEW — bats / bash assertion harness); `.github/workflows/release.yml` golden assertions in `scripts/release/sign-artifacts_test.sh` | Wave A; independent of T1 internals | S | **2 / 1 / 0** = 3 named |
| **T3** | `internal/gates/authz/loader.go` (W8-created; ADD `sigstore.Verify` call inside `loadBundle` per spec §3.5 lines 197-204); `internal/authz/policies/payload.go` (W8-created; ADD `Signature []byte` field to the existing `PolicyRevisionPayload` struct per spec §3.5 line 207); `internal/cuevalidate/sign.cue` (NEW — `safety.sign.{policy_required bool, expected_identities []string}` schema); `internal/gates/authz/loader_test.go` (extend with three named tests below) | Wave B; T1 merged | S | **3 / 2 / 0** = 5 named |
| **T4** | `internal/cost/pricing/pricing_verify.go` (NEW — `Load(ctx, expectedIdentity) error` per spec §3.7 lines 249-260; `//go:embed pricing.go.canonical` + `pricing.go.sig`); `internal/cost/pricing/pricing.go.canonical` (NEW — canonical-JSON of the var map, written by `go generate`); `internal/cost/pricing/pricing.go.sig` (NEW — cosign bundle JSON over `.canonical`; CI re-signs on merge; local-dev re-signs with `local:dev`); `internal/cost/pricing/canonical.go` (NEW — `go:generate` directive + canonicalizer function); `internal/cost/pricing/pricing_verify_test.go`; `cmd/regatta/serve.go` (ONE-LINE addition: `pricing.Load(ctx, cfg.Safety.Sign.PricingExpectedIdentity)` at boot; ≤ 6 LoC delta) | Wave B; T1 merged; cost-gov W2 merged | M | **3 / 2 / 0** = 5 named |
| **T5** | `cmd/regatta/sign.go` (NEW — `regatta sign` cobra subcommand root); `cmd/regatta/sign_init_dev_key.go` (NEW — ECDSA-P256 keypair gen at `$XDG_DATA_HOME/regatta/keys/dev.{pub,priv}` per spec §3.3 lines 161-175); `cmd/regatta/sign_refresh_rekor_root.go` (NEW — forced re-fetch of Rekor tree head per spec §3.4 line 188); `cmd/regatta/sign_sign_blob.go` (NEW — `regatta sign --identity ... --in ... --out ...` thin wrapper around `sigstore.Sign`); `internal/sign/sigstore/build_production.go` (NEW — `//go:build production`; refuses `local:` identities in `Verify` per spec §3.3 R5 line 177); `internal/sign/sigstore/build_dev.go` (NEW — `//go:build !production`; accepts `local:`); `cmd/regatta/sign_test.go`; `internal/sign/sigstore/build_production_test.go` | Wave A; can stub `sigstore.Sign` interface locally until T1 lands then rebase | M | **3 / 1 / 1** = 5 named |
| **T6** | `internal/sign/sigstore/otel.go` (NEW — span emission helpers per spec §3.8 attr table); `internal/sign/sigstore/audit.go` (NEW — substrate `kind=sign_verified` + `kind=sign_failed` event emission via `RegisterPayloadValidator` open-extension); `internal/plan/loader.go` (NEW STUB — declares `Verifier` interface + `DefaultVerifier sigstoreVerifier{}` per spec §3.6); `docs/operator/sign.md` (NEW — security-model doc); `docs/operator/sign-rekor-outage.md` (NEW — R1 outage runbook); `docs/operator/sign-identity-rotation.md` (NEW — R3 rotation runbook); `docs/operator/sign-airgap.md` (NEW — R10 air-gap deploy guide stub); `internal/sign/sigstore/otel_test.go`; `internal/sign/sigstore/audit_test.go` | Wave C; T1 + T3 + T4 merged | M | **2 / 1 / 0** = 3 named |

**Total tests across W10: 31 named** (B 18 / A 9 / A+ 4).

### Disjointness verification (`grep` at plan time)

- **T1 ↔ T2:** zero file overlap. T1 writes `internal/sign/sigstore/`; T2 writes
  `.github/workflows/release.yml` + `scripts/release/`. Verified at plan time by listing the
  exclusive-write-paths column above.
- **T1 ↔ T5:** **partial overlap on `internal/sign/sigstore/build_{production,dev}.go`** —
  these two files are T5's exclusive scope (build-tag gate is a T5 concern, NOT T1's). T1
  writes `sign.go`, `verify.go`, `errors.go`, `cache.go` + their `_test.go` siblings. T5
  writes the two `build_*.go` tag-gated files. Per `feedback_plan_subagent_dup_files`, T5's
  dispatch prompt EXPLICITLY claims `internal/sign/sigstore/build_production.go` +
  `internal/sign/sigstore/build_dev.go` as T5's exclusive scope; T1's dispatch prompt
  EXPLICITLY excludes those two filenames.
- **T2 ↔ T5:** zero file overlap. T2 writes CI workflow + shell scripts; T5 writes the
  `cmd/regatta/sign_*.go` family + the build-tag gate.
- **T3 ↔ T4:** zero file overlap. T3 writes `internal/gates/authz/` +
  `internal/authz/policies/` + `internal/cuevalidate/sign.cue`. T4 writes
  `internal/cost/pricing/` + ONE-LINE addition to `cmd/regatta/serve.go`.
- **T3 ↔ T6:** zero file overlap. T6 stays in `internal/sign/sigstore/{otel,audit}.go` +
  `internal/plan/loader.go` + `docs/operator/sign*.md`. T6 does NOT modify T3's loader files.
- **T4 ↔ T6 + `cmd/regatta/serve.go`:** T4 owns the ONE-LINE `pricing.Load(...)` call at boot.
  T6 does NOT touch `cmd/regatta/serve.go`. If T6 needs to wire OTel context through the boot
  call site, T6 files a tracking issue + reads (not writes) `serve.go`.
- **T5 ↔ T6:** zero file overlap. T5 owns the CLI subcommand layer; T6 owns OTel + audit +
  docs + plan-loader stub.

### Cross-task seam contracts (load-bearing — implementers MUST honour exactly)

- **T1 exports:** `sigstore.Sign(ctx, artifact []byte, identity string) (signature []byte,
  err error)`, `sigstore.Verify(ctx, artifact []byte, signature []byte, expectedIdentity
  string) error`, and six typed sentinels per spec §3.2:
  `sigstore.ErrSignatureInvalid`, `sigstore.ErrIdentityMismatch`,
  `sigstore.ErrRekorInclusionInvalid`, `sigstore.ErrRekorUnreachable`,
  `sigstore.ErrBundleMalformed`, `sigstore.ErrIdentityNotAllowed`. Sentinels MUST be
  `errors.Is`-compatible (no string-matching by callers). T3, T4, T5, T6 import these.
- **T1 internal interface for T5 build-tag gate:** T1 exports a package-private hook
  `var verifyLocalIdentity = func(...) error { ... }` (or equivalent build-tag-swappable
  symbol) that T5's `build_production.go` overrides to return `ErrIdentityNotAllowed` when
  expectedIdentity has prefix `local:`. T1's dispatch prompt declares the hook's name + the
  contract; T5's dispatch prompt is FROZEN against this name.
- **T1 → T3 cross-import:** T3 imports `sigstore.Verify` + `sigstore.ErrIdentityMismatch` +
  `sigstore.ErrRekorInclusionInvalid` only. T3 wraps the error via
  `errors.Join(ErrPolicyBundleSignatureInvalid, err)` per spec §3.5 line 201. NEW T3-owned
  sentinel `authz.ErrPolicyBundleSignatureInvalid` declared in
  `internal/gates/authz/loader.go` (or a sibling `errors.go` in the same package; implementer
  picks one and stays consistent).
- **T1 → T4 cross-import:** T4 imports `sigstore.Verify` + the six sentinels. T4 declares two
  new sentinels in `internal/cost/pricing/pricing_verify.go`:
  `pricing.ErrPricingTableCanonicalDrift`, `pricing.ErrPricingTableSignatureInvalid`. T4 wraps
  via `errors.Join(ErrPricingTableSignatureInvalid, err)` per spec §3.7 line 257.
- **T1 → T5 cross-import:** T5 imports `sigstore.Sign` + `sigstore.Verify` + sentinels. The
  `regatta sign --identity local:dev` path calls `Sign` directly with the dev keypair loaded
  from `$XDG_DATA_HOME/regatta/keys/`. T5's CLI does NOT bypass `sigstore.Sign` (no parallel
  signing path).
- **T1 → T6 cross-import:** T6 imports T1's `sigstore.Verify` for the OTel-instrumented
  wrapper. T6 does NOT modify T1's `verify.go`; instead T6 adds a sibling helper
  `internal/sign/sigstore/otel.go::VerifyWithSpan(ctx, ...)` that opens the `sign.verify`
  span, calls T1's bare `Verify`, sets the six attrs per spec §3.8, then ends the span.
  Callers (T3, T4) migrate to `VerifyWithSpan` in T6's PR (one-line replacements at the two
  call-sites in `loader.go` + `pricing_verify.go`).
- **T2 ↔ T1 seam:** **NONE at compile time.** T2 calls the `cosign` CLI binary directly via
  shell. The Go API (T1) is invoked only at verification time (runtime, in regatta itself);
  T2's CI signing step has no Go-import dependency on T1. This makes T2 parallel-safe.
- **T3 ↔ T1 payload seam:** T3 adds `Signature []byte` to `PolicyRevisionPayload`. The field
  carries the cosign bundle JSON returned by T1's `Sign`. T3 calls T1's `Verify` with
  `rev.canonicalBytes()` as the artifact + `rev.Signature` as the signature bytes per spec
  §3.5 line 199. **The substrate event payload schema change is additive** (new optional
  field; old events have empty bytes); see back-compat note under T3 Scope.
- **T4 ↔ T1 seam:** T4 calls T1's `Verify` against the embedded `pricingCanonical` bytes +
  embedded `pricingSignature` bytes. T4 declares the expected identity from the CUE config
  field `safety.sign.pricing_expected_identity` (T3 declares the parallel `safety.sign.*`
  block; T4 adds the `pricing_expected_identity` sub-field — file-disjoint because T3 owns
  `internal/cuevalidate/sign.cue` and T4 grep-verifies the field is present before merging).
  **Coordination note:** T3 and T4 land in parallel; the CUE field for pricing MUST appear in
  T3's `sign.cue` PR (T3 declares the full schema upfront). T3's dispatch prompt is
  parameterized to declare BOTH `policy_required` + `pricing_required` + `expected_identities`
  + `pricing_expected_identity` + `policy_expected_identity` so T4 has a stable field name.
- **T5 → T1 seam:** beyond direct API import, T5's `build_production.go` and `build_dev.go`
  live UNDER `internal/sign/sigstore/` (T1's package) because the build-tag gate must compile
  against the same package as `Verify`. T5's dispatch prompt clamps to these two filenames
  ONLY in T1's directory.
- **T6 → T3 + T4 call-site rewrite:** T6's PR contains EXACTLY TWO one-line replacements
  in `internal/gates/authz/loader.go` + `internal/cost/pricing/pricing_verify.go` that swap
  `sigstore.Verify(...)` → `sigstore.VerifyWithSpan(...)`. T6's net diff in those two files is
  ≤ 4 LoC combined.
- **T6 substrate audit-event seam:** T6 adds two new substrate event kinds via
  `RegisterPayloadValidator(KindSignVerified, ...)` + `RegisterPayloadValidator(KindSignFailed,
  ...)`. The validators live in `internal/sign/sigstore/audit.go::init()`. No DDL change. No
  parallel write path — T6 writes events via `substrate.AppendEvent` with the T6-owned
  payload struct's `json.Marshal` output.

---

## §2 Task T1 — `Sign` + `Verify` wrapper + sigstore-go SDK adoption

### Scope

- **`internal/sign/sigstore/sign.go`** — NEW. `Sign(ctx, artifact []byte, identity string)
  (signature []byte, err error)`:
  - **OIDC keyless path** (identity prefix `https://`):
    1. Read OIDC token from `ACTIONS_ID_TOKEN_REQUEST_TOKEN` + `ACTIONS_ID_TOKEN_REQUEST_URL`
       environment variables.
    2. POST token to Fulcio (`https://fulcio.sigstore.dev`); receive ephemeral signing cert.
    3. Sign `sha256(artifact)` with cert's private key (in-memory only).
    4. Submit `{sig, cert, artifact-hash}` to Rekor (`https://rekor.sigstore.dev`); receive
       inclusion proof (UUID + log index + signed tree head).
    5. Return cosign-bundle-JSON-encoded `{signature, cert chain, Rekor entry, inclusion
       proof}`.
  - **Local-dev path** (identity prefix `local:`):
    1. Load keypair from `$XDG_DATA_HOME/regatta/keys/<keyname>.{pub,priv}` (default
       `~/.local/share/regatta/keys/`).
    2. Sign `sha256(artifact)` with the private ECDSA-P256 key.
    3. Append local tlog entry at `$XDG_DATA_HOME/regatta/tlog/<uuid>.json` (append-only
       file; inclusion-proof stub asserting "uuid present at index N").
    4. Return bundle JSON whose Rekor URL field is the local tlog URI
       (e.g. `file:///.../tlog/<uuid>.json`).
  - Both paths use the sigstore-go SDK (`github.com/sigstore/sigstore-go`) — no crypto code
    written here. Sign is a thin orchestrator over SDK primitives.
- **`internal/sign/sigstore/verify.go`** — NEW. `Verify(ctx, artifact, signature,
  expectedIdentity) error`:
  1. Parse `signature` as cosign bundle JSON. Malformed ⇒ `ErrBundleMalformed`.
  2. Detect identity prefix on `expectedIdentity`: `https://` ⇒ OIDC path; `local:` ⇒ dev
     path. Cross-prefix verification REFUSED — a `local:` signature presented against an
     `https:` expected identity returns `ErrIdentityMismatch` even if bytes happen to verify
     (defense against dev-key promotion to prod; spec §3.2 line 105-108).
  3. **OIDC path:**
     1. Verify cosign signature over `artifact` bytes (offline; pub key in cert).
     2. Verify cert chain against sigstore-public root (TUF-pinned via sigstore-go's
        embedded trust root).
     3. Verify OIDC subject in cert SAN equals `expectedIdentity`. Mismatch ⇒
        `ErrIdentityMismatch`.
     4. Verify Rekor inclusion proof against Rekor's signed tree head. Tree head fetched
        from cache (`cache.go`) — first boot populates cache; subsequent boots offline.
        Rekor unreachable on first boot ⇒ `ErrRekorUnreachable`. Inclusion proof invalid
        ⇒ `ErrRekorInclusionInvalid`.
  4. **Local-dev path:**
     1. Build-tag gate (T5-owned files `build_production.go` + `build_dev.go`): under
        `-tags=production`, return `ErrIdentityNotAllowed` immediately. Under default tag,
        proceed.
     2. Verify ECDSA signature over `artifact` bytes with the pubkey embedded in the bundle.
     3. Verify local tlog entry exists at the URI in the bundle (file-existence check).
- **`internal/sign/sigstore/errors.go`** — NEW. Six typed sentinels per spec §3.2:
  - `ErrSignatureInvalid`
  - `ErrIdentityMismatch`
  - `ErrRekorInclusionInvalid`
  - `ErrRekorUnreachable`
  - `ErrBundleMalformed`
  - `ErrIdentityNotAllowed`
  Each is a package-level `var Err... = errors.New("sigstore: ...")` sentinel.
  `errors.Is`-compatible. NO string-matching by callers.
- **`internal/sign/sigstore/cache.go`** — NEW. Rekor tree-head cache at
  `$XDG_CACHE_HOME/regatta/rekor/root.json`:
  - `func LoadTreeHead(ctx) (treeHead, error)` — read cache; if missing OR stale beyond
    `--allow-stale-rekor-root` window (default 30 days, clamp 90), fetch from
    `rekor.sigstore.dev/api/v1/log` + write atomically.
  - `func ForceRefresh(ctx) error` — called by T5's `regatta sign refresh-rekor-root` CLI.
  - Atomic write pattern: write to `<path>.tmp` + `os.Rename` (POSIX-atomic).
- **`go.mod` + `go.sum`** — add `github.com/sigstore/sigstore-go` pinned at latest stable
  v1.x. NO breaking-change drift policy: dependabot watches; PRs gated through `make check`.
  Per spec §5 R8.

### Prereqs (cite spec sections)

- Spec §2 IN items #1 (Sign/Verify pair), #3 (Rekor inclusion proof verification).
- Spec §3.1 — `Sign` signature **verbatim**.
- Spec §3.2 — `Verify` signature + six sentinels **verbatim** + cross-prefix refusal
  invariant.
- Spec §3.3 — keyless flow (Fulcio + Rekor) + local-dev fallback.
- Spec §3.4 — Rekor logging + tree-head cache + downtime resilience.
- Spec §4 — sigstore-go SDK adoption; no custom signing scheme.
- Spec §5 R1 (Rekor downtime), R5 (dev key promotion), R6 (key compromise), R8 (SDK drift).
- Spec §6 T1 — exhaustive named-test list (5 B-tier tests transcribed below).
- Spec §7 B/A/A+ — applies to T1 (B floor = wrapper ships; A target = benchmark p99 + Rekor
  unreachable path + e2e fixture; A+ = property test + mutation coverage + fuzz +
  cross-binary + cold-path).
- Spec §8 — file-disjoint table row 1 (T1 scope + OWNS-the-API note).
- Spec §9 — T1 dispatches first; T3 + T4 + T5 + T6 cross-import.

### Existing patterns to reuse (do NOT reinvent)

- **sigstore-go SDK** (`github.com/sigstore/sigstore-go`, Apache-2.0, CNCF graduated) —
  Sign/Verify/Fulcio/Rekor/TUF clients. Pin in `go.mod`; consume via the SDK's `verify`
  package for the Verify path; `sign` package for the Sign path. **Do NOT** write any crypto
  code — `crypto/ecdsa`, `crypto/x509`, `crypto/rsa` imports in `internal/sign/sigstore/` are
  CI-rejected per `feedback_research_design_principles`.
- **Typed sentinel pattern** — mirrors W7 approval-gates + W8 `ErrDenied` family.
  `var Err... = errors.New("sigstore: ...")`. `errors.Is` for callers; `errors.Join` for
  wrapping.
- **XDG Base Directory** — local-dev keys at `$XDG_DATA_HOME/regatta/keys/` (default
  `~/.local/share/regatta/keys/`); cache at `$XDG_CACHE_HOME/regatta/rekor/` (default
  `~/.cache/regatta/rekor/`); local tlog at `$XDG_DATA_HOME/regatta/tlog/`. Use
  `os.UserConfigDir` / `os.UserCacheDir` + an XDG helper if one exists in `internal/`
  already; otherwise inline `os.Getenv("XDG_DATA_HOME")` with the POSIX fallback. **Do NOT**
  introduce a new XDG library dep.
- **`os.Rename` atomic-write pattern** — use the existing helper if one exists in
  `internal/`; otherwise `os.WriteFile(tmp, ...) + os.Rename(tmp, final)`. Mirrors
  substrate v2's atomic-event-write pattern.

### TDD test list (named tests per spec §6 T1; failing-output capture step required)

Per `feedback_tdd_discipline`: implementer writes each test first, runs
`go test ./internal/sign/sigstore/ -run <TestName> -v`, **captures failing output (paste at
least 6 representative samples into PR body)**, then implements. "Tests would have failed" is
NOT acceptable.

**B-tier (5 named tests — spec §6 T1 + §7 B):**

1. `TestSign_OIDCKeyless_RoundTrips` — sign with sigstore-go test harness's mocked OIDC token
   + mocked Fulcio + mocked Rekor → Verify returns nil. Pins happy-path round-trip.
2. `TestVerify_IdentityMismatch_ReturnsErrIdentityMismatch` — wrong `expectedIdentity` ⇒
   typed sentinel via `errors.Is`. Pins R5 cross-identity refusal.
3. `TestVerify_RekorInclusionInvalid_ReturnsErrRekorInclusionInvalid` — tampered
   inclusion-proof bytes in the bundle ⇒ typed sentinel. Pins R6 defense-in-depth (a
   compromised signing cert without Rekor inclusion still rejected).
4. `TestVerify_BundleMalformed_ReturnsErrBundleMalformed` — malformed signature bytes (not
   valid JSON; not cosign bundle shape) ⇒ typed sentinel.
5. `TestSign_LocalIdentity_WritesLocalTlog` — `Sign(ctx, artifact, "local:dev")` under
   default build tag writes to `$XDG_DATA_HOME/regatta/tlog/<uuid>.json`; bundle JSON
   references the local tlog URI. Pins local-dev seam.

**A-tier (2 named tests — spec §6 T1 A + §7 A):**

6. `BenchmarkVerify_PolicyBundle_p99Under5Millis` — N=1 000 verifications against a fixed
   cosign bundle (offline; tree head cached); assert p99 ≤ 5 ms via
   `b.ReportMetric(p99Micros, "p99-micros")` + a `Run` assertion that fails the test if p99
   > 5000. Pins offline-fast invariant.
7. `TestVerify_RekorUnreachable_ReturnsErrRekorUnreachable` — Rekor mock returns 5xx on
   tree-head fetch + cache is empty ⇒ typed sentinel. Then with cache populated, same scenario
   succeeds (cached-tree-head fallback). Pins R1 mitigation.

**A+-tier (3 named tests — spec §6 T1 A+ + §7 A+):**

8. `TestPropertySignVerifyRoundTrip` — `rapid`-based property test; for any byte sequence
   `x` of length 0..16 KiB and any `identity` from a generated set of 4 (`local:test1`,
   `local:test2`, `https://example.org/wf1`, `https://example.org/wf2`), `Sign(x, id);
   Verify(x, sig, id)` returns nil. Cross-identity `Verify(x, sig, id')` for `id' != id`
   returns `ErrIdentityMismatch`. ≥ 5 000 cases (`rapid.Check(t, prop)` default reaches
   5 000 with the `numChecks` flag set).
9. `TestVerify_FuzzInclusionProofRejected` — `go-fuzz` corpus over the inclusion-proof
   verification path; ≥ 10 minutes; zero crashes; every mutated proof rejected via
   `ErrRekorInclusionInvalid`. Run as `go test -run=TestVerify_FuzzInclusionProofRejected
   -fuzz=. -fuzztime=10m` in CI nightly; standard short run for `make check` is 30s.
10. `TestVerify_CrossBinaryStability` — sign with binary built from `git rev-parse HEAD`;
    verify with a second binary built from the same commit but a different `GOFLAGS` (e.g.
    `-trimpath` vs not). Byte-equal pass. Pins reproducibility invariant.

### PR body skeleton — T1

````
## Summary

W10 T1: ship the sigstore-go-backed `Sign` + `Verify` wrapper + six typed
sentinels + Rekor tree-head cache. Foundation for T3 + T4 + T5 + T6.

- internal/sign/sigstore/sign.go — Sign(ctx, artifact, identity) (sig, err)
  per spec §3.1. OIDC keyless via Fulcio + Rekor; local-dev via XDG keypair
  + local tlog stub.
- internal/sign/sigstore/verify.go — Verify(ctx, artifact, sig, expectedIdentity)
  error per spec §3.2. Cross-prefix refusal (local: vs https:) hard-coded.
- internal/sign/sigstore/errors.go — six typed sentinels (ErrSignatureInvalid,
  ErrIdentityMismatch, ErrRekorInclusionInvalid, ErrRekorUnreachable,
  ErrBundleMalformed, ErrIdentityNotAllowed). All errors.Is-compatible.
- internal/sign/sigstore/cache.go — Rekor tree-head cache at
  $XDG_CACHE_HOME/regatta/rekor/root.json. Atomic write; 30-day staleness
  default; ForceRefresh hook for T5's refresh-rekor-root CLI.
- go.mod + go.sum — pin sigstore-go SDK at latest v1.x stable.

## Why

Per `feedback_research_design_principles` — adopt sigstore-go (CNCF graduated;
≥ 10k stars; ≥ 3y public history) over custom signing. Eliminates ~2 000 LoC
of crypto + tlog + cert-validation code. The six typed sentinels mirror W7 +
W8 patterns: callers use errors.Is, never string-match. The cross-prefix
refusal defends against dev-key promotion to prod (R5).

## Test plan

- [x] B-tier (5): TestSign_OIDCKeyless_RoundTrips, TestVerify_IdentityMismatch,
       TestVerify_RekorInclusionInvalid, TestVerify_BundleMalformed,
       TestSign_LocalIdentity_WritesLocalTlog.
- [x] A-tier (2): BenchmarkVerify_PolicyBundle_p99Under5Millis,
       TestVerify_RekorUnreachable.
- [x] A+-tier (3): TestPropertySignVerifyRoundTrip,
       TestVerify_FuzzInclusionProofRejected,
       TestVerify_CrossBinaryStability.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 6 reps>

## Grade rubric scorecard (per feedback_grade_rubric — MANDATORY)

### B — floor (ships)

- [x] `internal/sign/sigstore` package ships with Sign + Verify + 6 typed sentinels.
- [x] Sign supports OIDC keyless + local: identity prefixes.
- [x] Verify cross-prefix refusal (local: signature vs https: expected ⇒ ErrIdentityMismatch).
- [x] Rekor tree-head cache at $XDG_CACHE_HOME/regatta/rekor/root.json; atomic write.
- [x] go.mod pins sigstore-go at v1.x stable.
- [x] ZERO new SQL migrations.
- [x] make check clean.

### A — target (expected)

All B, plus:
- [x] BenchmarkVerify_PolicyBundle p99 ≤ 5 ms offline.
- [x] TestVerify_RekorUnreachable + cached-tree-head fallback tested.
- [x] Adversarial reviewer cleared with zero unaddressed Risk-tier findings.

### A+ — stretch (aspirational)

- [x] Property test ≥ 5 000 cases (`rapid`).
- [x] Fuzz inclusion-proof ≥ 10 min nightly; 30s short-run in `make check`.
- [x] Cross-binary signature stability.

## Deletion default (per feedback_deletion_default)

sigstore-go SDK adoption ELIMINATES ~2 000 LoC of custom crypto + tlog + cert
validation per spec §4 line 303. The six typed sentinels REPLACE string-matching
by callers (callers across T3 + T4 + T5 + T6 use errors.Is exclusively — no
parallel error type, no error-message-parse code).

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w10-followup] sigstore-go v2 migration (#NNN; spec §5 R8 + §10 #8)
- [w10-followup] Hardware-key support (YubiKey/TPM/KMS) (#NNN; spec §1.2 + §10 #2)
- [w10-followup] Private TUF root (#NNN; spec §1.2 + §10 #3)
- [w10-followup] Air-gap deploy guide (#NNN; spec §5 R10 + §10 #12)

```release-notes
[FEATURE] sigstore-go-backed Sign + Verify wrapper for regatta artifacts (foundation for W10 policy-bundle + pricing-table signing; default-off until T3 + T4 wire the call sites)
```
````

### Dispatch prompt — T1 (paste-ready)

```
You are an implementer subagent working on a fresh git worktree at
.claude/worktrees/agent-w10-t1 on branch feat/w10-t1-sigstore-wrapper off main.

# Spec authority (per feedback_spec_pattern_authority)

Source-of-truth: docs/engineer/specs/2026-06-01-w10-sigstore-design.md.
Read in full: §1 goal, §2 IN/OUT, §3.1 (Sign), §3.2 (Verify + six sentinels),
§3.3 (keyless + local-dev flows), §3.4 (Rekor logging + tree-head cache),
§4 (deletion-default; sigstore-go SDK), §5 R1+R5+R6+R8 (risk mitigations),
§6 T1 (B/A/A+ named test list), §7 (grade rubric verbatim), §8 row 1
(file-disjoint scope; OWNS-the-API note).

If you want to deviate from any spec-mandated pattern — the Sign/Verify
signatures verbatim, the six typed sentinel names verbatim, the cross-prefix
refusal invariant, sigstore-go as the only crypto path, the XDG layout for
keys + cache + tlog, the bundle JSON shape (cosign-compatible) — STOP and
report. Re-spawn the design subagent. Do NOT pick an alternative yourself.

# Scope (exclusive write paths)

- internal/sign/sigstore/sign.go               (NEW)
- internal/sign/sigstore/verify.go             (NEW)
- internal/sign/sigstore/errors.go             (NEW; six sentinels)
- internal/sign/sigstore/cache.go              (NEW; Rekor tree-head cache)
- internal/sign/sigstore/{sign,verify,errors,cache}_test.go  (NEW)
- go.mod, go.sum                                (pin sigstore-go v1.x stable)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/sign/sigstore/build_production.go OR build_dev.go
  — those are T5's exclusive scope (build-tag gate). Your verify.go MUST
  call out to a package-private hook (e.g. `var verifyLocalIdentity = ...`)
  that T5's build_*.go files swap.
- Do NOT touch internal/sign/sigstore/otel.go OR audit.go OR any
  internal/plan/loader.go — those are T6's exclusive scope.
- Do NOT touch internal/gates/authz/ OR internal/cost/pricing/ OR
  cmd/regatta/ — those are T3, T4, T5 scope.
- Do NOT add any crypto/* import in your package (crypto/ecdsa, crypto/x509,
  crypto/rsa). All crypto goes through sigstore-go SDK.

If you discover a missing seam in an out-of-scope file, STOP and report —
file a tracking issue per finding; do NOT edit out of scope.

# Patterns to reuse (do NOT reinvent)

- sigstore-go SDK: github.com/sigstore/sigstore-go (Apache-2.0, CNCF
  graduated). Pin at latest v1.x stable. Use the SDK's `verify` package for
  Verify; `sign` package for Sign. Zero custom crypto.
- Typed sentinels: mirror W7 approval-gates + W8 ErrDenied family.
  `var Err... = errors.New("sigstore: ...")`. errors.Is for callers;
  errors.Join for wrapping.
- XDG Base Directory: $XDG_DATA_HOME/regatta/keys/, $XDG_CACHE_HOME/regatta/rekor/,
  $XDG_DATA_HOME/regatta/tlog/. Use os.Getenv + POSIX defaults inline; do
  NOT add a new XDG library dep.
- Atomic write: os.WriteFile(tmp, ...) + os.Rename(tmp, final). Mirrors
  substrate v2's pattern.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test below:
  1. Write the test file first.
  2. Run `go test ./internal/sign/sigstore/ -run <TestName> -v`.
  3. CAPTURE the failing output (paste ≥ 6 representative samples into PR
     body's "Failing-test output (TDD capture)" section). "Tests would have
     failed" is NOT acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or per logical group; squash later).

# Tests to land (10 named: 5 B + 2 A + 3 A+)

  B1. TestSign_OIDCKeyless_RoundTrips
  B2. TestVerify_IdentityMismatch_ReturnsErrIdentityMismatch
  B3. TestVerify_RekorInclusionInvalid_ReturnsErrRekorInclusionInvalid
  B4. TestVerify_BundleMalformed_ReturnsErrBundleMalformed
  B5. TestSign_LocalIdentity_WritesLocalTlog
  A1. BenchmarkVerify_PolicyBundle_p99Under5Millis
  A2. TestVerify_RekorUnreachable_ReturnsErrRekorUnreachable
  P1. TestPropertySignVerifyRoundTrip            (rapid; ≥ 5 000 cases)
  P2. TestVerify_FuzzInclusionProofRejected      (go-fuzz; nightly 10 min;
                                                 30s short-run in make check)
  P3. TestVerify_CrossBinaryStability

# Workflow after green

  1. Run `make pre-push-check` — confirm clean. NO --no-verify per
     feedback_pr_lint_gates.
  2. Run `bash scripts/doc-check.sh` and `bash scripts/stale-todo.sh`;
     both exit 0.
  3. Re-run `go test ./internal/sign/sigstore/ -v` (full suite); confirm
     all 10 named tests pass.
  4. Sweep superfluous comments per feedback_comments_discipline: WHY not
     WHAT; test-function godocs ≤ 1 line. Run
     `git diff origin/main -- '*.go' | grep -E '^\+.{0,2}//'` and prune.
  5. File the 4 followup tracking issues (see PR body Followups section).
  6. Push branch: `git push -u origin feat/w10-t1-sigstore-wrapper`.
  7. Open PR via `gh pr create --base main --title
     "feat(w10): T1 sigstore-go Sign + Verify wrapper + six typed sentinels"
     --body-file <path>` (NEVER heredoc per feedback_pr_lint_gates). PR body
     MUST end with the ```release-notes fence — grep-verify before push.
  8. Spawn ONE adversarial reviewer subagent (per feedback_adversarial_review
     + feedback_agent_pr_review) with hunt list (see below).
  9. Apply reviewer findings inline OR file tracking issue + cite per
     feedback_unaddressed_load_bearing.
 10. Re-run pre-push-check; force-push.
 11. Verify CI green (pr-lint, check-release-notes, check-tdd, build, test)
     BEFORE flipping automerge per feedback_review_before_automerge.
 12. Flip automerge ONLY after reviewer cleared the PR.
 13. Post the A+ scorecard verbatim in the PR body (per feedback_grade_rubric).

# Adversarial reviewer hunt list

- Sign/Verify signatures EXACT verbatim from spec §3.1 + §3.2. NO param
  reorder, NO additional optional param, NO context return.
- Six sentinel names + spellings VERBATIM. ErrIdentityNotAllowed (NOT
  ErrIdentityRefused, NOT ErrLocalNotAllowed).
- Cross-prefix refusal: local: signature presented against https: expected ⇒
  ErrIdentityMismatch even when bytes happen to verify. Test B2 must cover
  this exact case, not just "wrong OIDC subject".
- Build-tag gate hook: verify.go MUST export the swappable symbol (var or
  internal func) that T5's build_production.go can override. Document the
  hook name in a package comment so T5's implementer can grep it.
- Zero crypto/* imports: verify with `go list -deps ./internal/sign/sigstore/
  | grep '^crypto/'` — only the sigstore-go SDK transitively pulls crypto.
  The package's own *.go must NOT import crypto/ecdsa, crypto/x509, crypto/rsa.
- XDG fallback: $XDG_DATA_HOME unset ⇒ ~/.local/share. $XDG_CACHE_HOME unset
  ⇒ ~/.cache. Tested on Linux + macOS.
- Atomic write: os.Rename only (not os.WriteFile with overwrite). Verified
  by reading the test that crashes the process mid-write and asserts the
  prior cache state is intact.
- Sentinel discipline: `errors.Is(err, sigstore.ErrXxx)` — NO string-matching
  in tests (`strings.Contains(err.Error(), ...)` is a smell).
- Bundle JSON shape: cosign-compatible — i.e. `cosign verify-blob --bundle`
  must verify a bundle produced by T1's Sign. Integration test in B1 confirms
  this by round-tripping through the cosign CLI (or sigstore-go's verify
  primitive, which is what the CLI uses).
- Simplification opportunity: could the cache.go file be folded into
  verify.go? Probably yes for the < 50-LoC version; let the implementer pick
  one and stay consistent. No mandate.
- ZERO new SQL migrations. Confirm `git status --porcelain | grep migration`
  returns empty.
- No AI signatures anywhere (feedback_no_signatures).
- godocs ≤ 1 line on test funcs (feedback_comments_discipline).

# Hygiene

- NO AI signatures anywhere (commits, PR body, comments, code) per
  feedback_no_signatures.
- Comments discipline per feedback_comments_discipline: WHY not WHAT;
  test-function godocs ≤ 1 line; sweep on every push.
- Doc-check: run `bash scripts/doc-check.sh` — exit 0. The script's
  banned-token list lives in feedback_doc_check_banned_phrases.md; READ
  the memory file (do NOT copy the list into PR body or commit messages,
  per #297 + #296 fix cycles which showed plans/PRs that inlined the list
  self-tripped the gate).
- Stale-TODO check: `bash scripts/stale-todo.sh` — exit 0. Any
  TODO/FIXME/XXX in your diff MUST cite an issue (#NNN or URL) OR be added
  within the WINDOW_DAYS window.
- PR body release-notes fence: every PR ends with ```release-notes\n<note
  or "none">\n```. Grep-verify before push.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for ≥ 6 of the 10 tests.
- The 4 followup issue numbers filed.
- Adversarial reviewer verdict (APPROVE or full findings list with severities).
- One-line diff stat: files changed + LoC added/removed.
- A+ scorecard checklist (every checkbox under B + A + A+) marked verbatim.

Begin now. NEVER pause for user input.
```

---

## §3 Task T2 — CI workflow: sign every release artifact via GitHub OIDC keyless

### Scope

- **`.github/workflows/release.yml`** — extend the existing release job:
  - Add top-level `permissions: { id-token: write, contents: write }` per spec §2 IN #8.
  - Install `cosign` via `sigstore/cosign-installer@v3` (pinned by major).
  - For every release artifact pattern (release tarball, embedded default policy bundle,
    pricing-table canonical file), invoke `scripts/release/sign-artifacts.sh <artifact>`
    which calls `cosign sign-blob --bundle <artifact>.bundle.json --yes <artifact>`. Upload
    the `.bundle.json` next to the artifact via `softprops/action-gh-release` (or the
    existing release-upload action; preserve current action choice).
  - The OIDC subject for these artifacts is
    `https://github.com/<org>/<repo>/.github/workflows/release.yml@refs/tags/<tag>` —
    emit it into the release notes body so operators know which identity to whitelist.
- **`scripts/release/sign-artifacts.sh`** — NEW. One-shot script invoked per artifact:
  ```bash
  #!/usr/bin/env bash
  set -euo pipefail
  artifact="$1"
  cosign sign-blob --bundle "${artifact}.bundle.json" --yes "${artifact}"
  ```
  Plus a small wrapper that iterates a glob if the workflow needs multi-artifact in one step.
- **`scripts/release/sign-artifacts_test.sh`** — NEW. Bash test asserting:
  - Workflow YAML has `id-token: write` permission.
  - Workflow has a `cosign sign-blob` step for every artifact pattern.
  - `sign-artifacts.sh` exists, is executable (`test -x`), and runs `cosign sign-blob --bundle`
    with `--yes`.
  - The workflow installs cosign via `sigstore/cosign-installer` pinned by major version.
  - The OIDC subject string appears in the release-body template (so operators see it).

### Prereqs (cite spec sections)

- Spec §2 IN #2 (cosign keyless via GitHub OIDC for CI-produced artifacts) + #8 (CI workflow
  change).
- Spec §3.3 — keyless flow diagram (CI release job branch).
- Spec §5 R2 (OIDC token expiry mid-run) — retry policy 3 attempts with 5s/15s/30s backoff.
- Spec §5 R3 (identity rotation drift) — OIDC subject string emitted into release notes.
- Spec §6 T2 — exhaustive named-test list (2 B-tier transcribed below).
- Spec §7 B/A — B floor = release workflow signs every artifact; A target = e2e fixture
  exercises sign+verify (lands in T1's PR, not T2's, since T1 has the verify path; T2's
  e2e contribution is the workflow assertion).

### Existing patterns to reuse (do NOT reinvent)

- **`sigstore/cosign-installer@v3`** — proven OSS GitHub Action that installs the `cosign`
  CLI binary. Pin by major. Do NOT vendor cosign.
- **GitHub Actions OIDC** — `permissions: id-token: write` is the built-in mechanism. Do NOT
  add a third-party OIDC bridge.
- **Existing release-upload action** — preserve whatever the current `release.yml` uses
  (likely `softprops/action-gh-release` or `actions/upload-release-asset`). Add the
  `.bundle.json` to the same upload step's artifact list.
- **Bash test harness** — repo already uses bats / plain-bash for shell tests (see
  `scripts/doc-check.sh` for the existing pattern). Mirror that style.

### TDD test list (named tests per spec §6 T2)

**B-tier (2 named tests):**

1. `TestReleaseWorkflow_SignsEveryArtifact` — bash test that greps `.github/workflows/release.yml`
   for a `cosign sign-blob` step for every artifact pattern (release tarball, policy bundle,
   pricing canonical). Failing baseline: pre-T2 workflow has zero `cosign` references.
2. `TestReleaseWorkflow_HasIdTokenWritePermission` — YAML assertion: top-level OR job-level
   `permissions:` block contains `id-token: write`.

**A-tier (1 named test):**

3. `TestSignArtifactsScript_InvokesCosignWithBundleFlag` — shell test running
   `scripts/release/sign-artifacts.sh` against a temp file with `cosign` stubbed to a shell
   function that records its args; asserts `--bundle <artifact>.bundle.json --yes <artifact>`
   args appear in order.

### PR body skeleton — T2

````
## Summary

W10 T2: extend the release workflow to sign every release artifact via
`cosign sign-blob` with GitHub OIDC keyless flow. Per spec §2 IN #8 + §3.3.

- .github/workflows/release.yml — add `permissions: id-token: write +
  contents: write`. Install cosign via `sigstore/cosign-installer@v3`.
  Sign each artifact + upload `.bundle.json` alongside.
- scripts/release/sign-artifacts.sh — NEW. Thin shell wrapper around
  `cosign sign-blob --bundle <artifact>.bundle.json --yes <artifact>`.
- scripts/release/sign-artifacts_test.sh — NEW. Asserts workflow + script
  invariants.

The OIDC subject string is emitted into the release-body template so
operators can populate `safety.sign.expected_identities` (per spec §5 R3).

## Why

Per `feedback_research_design_principles` — GitHub OIDC keyless eliminates
long-lived signing keys (no rotation, no custody, no leak). Per spec §5 R2,
each artifact signed in a separate step → failure isolation; a single
artifact failure does not roll back prior signatures.

## Test plan

- [x] B-tier (2): TestReleaseWorkflow_SignsEveryArtifact,
       TestReleaseWorkflow_HasIdTokenWritePermission.
- [x] A-tier (1): TestSignArtifactsScript_InvokesCosignWithBundleFlag.
- [x] make pre-push-check clean.
- [x] Dry-run release workflow on a feature branch with `act` OR a draft
       release tag (per operator-doc R2 runbook).

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Grade rubric scorecard (per feedback_grade_rubric — MANDATORY)

### B — floor (ships)

- [x] Release workflow signs every artifact via cosign sign-blob.
- [x] Workflow has id-token: write permission.
- [x] `.bundle.json` uploaded alongside each artifact on the GitHub release.
- [x] OIDC subject string emitted into release-body template.

### A — target (expected)

All B, plus:
- [x] Per-artifact signing in separate steps (failure isolation per R2).
- [x] Adversarial reviewer cleared with zero unaddressed Risk-tier findings.

## Deletion default (per feedback_deletion_default)

GitHub OIDC keyless ELIMINATES the long-lived-key custody surface entirely.
Zero new secrets in repo / no rotation runbook / no key-handler service.
The `cosign sign-blob` CLI invocation REPLACES a Go re-implementation in
the release workflow.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w10-followup] OCI registry as artifact store (#NNN; spec §1.2 + §10 #7)
- [w10-followup] SLSA L4 build provenance (#NNN; spec §1.2 + §10 #1)

```release-notes
[FEATURE] release-workflow cosign sign-blob signing for every release artifact + .bundle.json upload to GitHub release (OIDC keyless via GitHub Actions id-token)
```
````

### Dispatch prompt — T2 (paste-ready)

```
You are an implementer subagent on a fresh git worktree at
.claude/worktrees/agent-w10-t2 on branch feat/w10-t2-ci-sign-release off main.

# Spec authority (per feedback_spec_pattern_authority)

Source-of-truth: docs/engineer/specs/2026-06-01-w10-sigstore-design.md.
Read in full: §2 IN #2 + #8, §3.3 (keyless flow CI branch), §5 R2 + R3
(OIDC expiry, identity rotation), §6 T2 (named-test list), §7 B/A rubric,
§8 row 2 (file-disjoint scope; "independent of T1 once interface signature
is frozen" — but T2's signing path is the CLI binary, NOT T1's Go API, so
T2 has ZERO compile-time dependency on T1 and dispatches in Wave A
parallel).

If you want to deviate from any spec-mandated pattern — id-token: write
permission scope, cosign-installer pinned by major, --bundle flag on every
sign-blob call, .bundle.json upload alongside each artifact, OIDC subject
emitted into release notes — STOP and report. Re-spawn the design subagent.

# Scope (exclusive write paths)

- .github/workflows/release.yml                  (extend)
- scripts/release/sign-artifacts.sh              (NEW)
- scripts/release/sign-artifacts_test.sh         (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/sign/sigstore/ — T1's scope.
- Do NOT touch cmd/regatta/ — T5's scope.
- Do NOT touch internal/gates/authz/ OR internal/cost/pricing/ — T3, T4.

# Patterns to reuse (do NOT reinvent)

- sigstore/cosign-installer@v3 — pin by major; do NOT vendor cosign.
- GitHub Actions id-token: write — built-in OIDC; no third-party bridge.
- Existing release-upload action — preserve current action choice; add
  .bundle.json to its asset list, don't introduce a parallel upload step.
- Bash test harness — mirror scripts/doc-check.sh style.

# Workflow steps (TDD — feedback_tdd_discipline)

For each named test:
  1. Write test first.
  2. Run `bash scripts/release/sign-artifacts_test.sh` (the test script
     itself) and capture failing output.
  3. Implement minimum to pass; re-run; confirm green.

# Tests to land (3 named: 2 B + 1 A)

  B1. TestReleaseWorkflow_SignsEveryArtifact
  B2. TestReleaseWorkflow_HasIdTokenWritePermission
  A1. TestSignArtifactsScript_InvokesCosignWithBundleFlag

# Workflow after green

  1. make pre-push-check clean.
  2. bash scripts/doc-check.sh && bash scripts/stale-todo.sh — both exit 0.
  3. File the 2 followup issues (OCI registry; SLSA L4).
  4. Push branch.
  5. Open PR via `gh pr create --base main --title
     "feat(w10): T2 release-workflow cosign sign-blob signing" --body-file <path>`
     (NEVER heredoc). PR body ends with the ```release-notes fence.
  6. Spawn adversarial reviewer subagent with hunt list (see below).
  7. Apply findings inline OR file tracking issue.
  8. Verify CI green BEFORE flipping automerge.
  9. Post A+ scorecard verbatim in PR body.

# Adversarial reviewer hunt list

- permissions: id-token: write at the SMALLEST scope that compiles (job-level
  preferred over workflow-level).
- cosign-installer pinned by major (NOT by floating tag).
- Each artifact signed in its OWN step (failure isolation per R2).
- --bundle flag present (writes the cosign bundle JSON; NOT just `--output-signature`).
- --yes flag present (else cosign prompts interactively in CI).
- Upload step adds .bundle.json to the same release asset list.
- OIDC subject string appears in the release-body template.
- Retry policy: 3 attempts with 5s/15s/30s backoff per R2. Documented in
  the workflow comments OR a sibling runbook entry — adversarial reviewer
  picks one and confirms it's discoverable.
- No AI signatures anywhere.

# Hygiene

- NO AI signatures.
- Doc-check + stale-todo gates exit 0.
- ```release-notes fence — grep-verify before push.

# Return format

- PR URL.
- Failing-test output ≥ 3 of 3.
- Followup issue numbers.
- Reviewer verdict.
- Diff stat.
- A+ scorecard verbatim.

Begin now. NEVER pause for user input.
```

---

## §4 Task T3 — Policy-bundle Verify integration (W8 loader hookup)

### Scope

- **`internal/gates/authz/loader.go`** — W8-created. ADD `sigstore.Verify` call inside
  `loadBundle` per spec §3.5 lines 197-204. The expected identity is read from
  `a.cfg.PolicyBundleSignerIdentity` (NEW field on the existing W8 `opaAuthorizer.cfg`
  struct). Net diff ≤ 30 LoC.
  - On verify failure: return `errors.Join(ErrPolicyBundleSignatureInvalid, err)`. Atomic
    swap NO-OPs; tenant stays on prior bundle (fail-closed).
- **`internal/authz/policies/payload.go`** — W8-created `PolicyRevisionPayload` struct. ADD
  `Signature []byte` field (cosign bundle JSON bytes). Update the JSON tag to
  `json:"signature,omitempty"` so back-compat works for pre-W10 events with empty bytes.
  Update the existing payload validator (registered by T-S1 #224's
  `RegisterPayloadValidator(KindPolicyRevision, ...)` hook in
  `internal/orchestrator/state/substrate/validate.go` — if W8 owns it, T3 EXTENDS its
  closure to require `Signature != nil` WHEN `safety.sign.policy_required: true`).
- **`internal/cuevalidate/sign.cue`** — NEW. CUE schema for the `safety.sign` config block:
  ```cue
  package cuevalidate

  #SafetySign: {
      // policy_required: when true, policy_revision events without a Signature are rejected.
      policy_required: bool | *true

      // pricing_required: when true, pricing.go.canonical signature verification is mandatory
      //   at boot. T4 owns the consumer; T3 declares the schema upfront.
      pricing_required: bool | *true

      // expected_identities: OIDC subject strings (https://) or local: prefixes accepted as
      //   trust anchors for policy bundles. Set semantics; identity rotation per R3.
      expected_identities: [...string] & list.MinItems(1) | *[]

      // policy_expected_identity: convenience singleton for policy bundles. If unset,
      //   expected_identities is used.
      policy_expected_identity?: string

      // pricing_expected_identity: convenience singleton for pricing-table. T4 reads this
      //   field at boot.
      pricing_expected_identity?: string

      // rekor_url: override the default Rekor instance. v1 default = sigstore public-good.
      rekor_url: string | *"https://rekor.sigstore.dev"
  }
  ```
  **Cross-task coordination:** T3 declares the full `safety.sign` schema; T4 reads
  `pricing_expected_identity` + `pricing_required` from this same file. T3's PR is the
  schema owner.
- **`internal/gates/authz/loader_test.go`** — extend with the three B-tier tests below.

### Prereqs (cite spec sections)

- Spec §2 IN #4 (policy bundle load-path Verify).
- Spec §3.5 — load-path Verify integration; back-compat for pre-W10 events.
- Spec §4 — W8 policy bundle loader pattern reused; one Verify call added.
- Spec §5 R3 (identity rotation drift) — `expected_identities` set semantics + CUE rejects
  empty list when `policy_required: true`.
- Spec §5 R4 (ToCToU) — `Verify` returns nil-or-err only; verified bytes consumed directly.
- Spec §6 T3 — 3 B-tier + 2 A-tier named tests.
- Spec §8 row 3.
- W8 spec §3.3 (Authorizer.Hydrate) + §10 followup #1 (policy bundle signing — closed by W10
  T3; PR body cross-references both).

### Existing patterns to reuse (do NOT reinvent)

- **W8 `loadBundle` path** — `internal/gates/authz/loader.go::loadBundle(ctx, rev)`. T3
  inserts the `sigstore.Verify` call BEFORE the existing OPA compile + store-swap. NO change
  to the swap path.
- **`sigstore.Verify`** — T1-owned. Imports: `sigstore.Verify`,
  `sigstore.ErrIdentityMismatch`, `sigstore.ErrRekorInclusionInvalid`. Wrap via
  `errors.Join(ErrPolicyBundleSignatureInvalid, err)`.
- **CUE validator** — `internal/cuevalidate/` already exists (cost-gov §3.6 pattern owns
  `safety.cost`). T3 adds `safety.sign` block alongside. Mirror the existing field-naming +
  default conventions.
- **Substrate payload validator** — T-S1 #224's `RegisterPayloadValidator(kind, fn)` hook.
  T3 extends the existing `KindPolicyRevision` validator (if W8 owns it) or registers a new
  one (if W8 left it for W10). Implementer greps + picks; either path works.

### TDD test list (named tests per spec §6 T3)

**B-tier (3 named tests):**

1. `TestAuthorizerLoadBundle_ValidSignature_LoadsBundle` — happy path: cosign-signed bundle
   bytes + matching `expectedIdentity` → bundle hydrates into OPA store; tenant's
   `compiledPolicy` is the new bundle.
2. `TestAuthorizerLoadBundle_InvalidSignature_PreservesPriorBundle` — fail-closed: invalid
   sig ⇒ `errors.Join` returns `ErrPolicyBundleSignatureInvalid`; tenant's `compiledPolicy`
   is UNCHANGED (atomic swap NO-OPs).
3. `TestPolicyRevisionPayload_BackCompat_EmptySignatureRespectsConfig` — empty `Signature`
   + `policy_required: false` ⇒ accept (back-compat); empty `Signature` + `policy_required:
   true` ⇒ reject.

**A-tier (2 named tests):**

4. `TestAuthorizerLoadBundle_IdentityRotation_BothIdentitiesAccepted` —
   `expected_identities` is a set with two entries; bundle signed under either identity
   verifies. Pins R3.
5. `TestCueValidator_PolicyRequiredTrueRejectsEmptyExpectedIdentities` — CUE config with
   `policy_required: true` AND `expected_identities: []` ⇒ validator rejects at config load.

### PR body skeleton — T3

````
## Summary

W10 T3: wire the W8 policy-bundle loader through sigstore.Verify per spec
§3.5. Closes W8 §10 followup #1 (policy bundle signing).

- internal/gates/authz/loader.go — loadBundle calls sigstore.Verify before
  the existing OPA compile + store-swap. Invalid sig ⇒
  errors.Join(ErrPolicyBundleSignatureInvalid, err); atomic swap NO-OPs;
  tenant stays on prior bundle (fail-closed per R4).
- internal/authz/policies/payload.go — PolicyRevisionPayload gains
  Signature []byte. JSON tag `signature,omitempty` for back-compat with
  pre-W10 events.
- internal/cuevalidate/sign.cue — NEW. safety.sign schema:
  policy_required + pricing_required + expected_identities[] +
  policy_expected_identity + pricing_expected_identity + rekor_url.
  T3 owns the schema; T4 reads pricing_* fields.
- internal/gates/authz/loader_test.go — 5 named tests.

## Why

Per spec §3.5: every policy bundle MUST verify before OPA compile. Per R4,
Verify takes the same bytes Use takes (no rebinding seam) — closes ToCToU.
Per R3, expected_identities is a SET (rotation-friendly), not a singleton.
The CUE validator rejects empty list when policy_required is true so an
operator can't accidentally trust everyone.

## Test plan

- [x] B-tier (3): TestAuthorizerLoadBundle_ValidSignature_LoadsBundle,
       TestAuthorizerLoadBundle_InvalidSignature_PreservesPriorBundle,
       TestPolicyRevisionPayload_BackCompat_EmptySignatureRespectsConfig.
- [x] A-tier (2): TestAuthorizerLoadBundle_IdentityRotation_BothIdentitiesAccepted,
       TestCueValidator_PolicyRequiredTrueRejectsEmptyExpectedIdentities.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Grade rubric scorecard (per feedback_grade_rubric — MANDATORY)

### B — floor (ships)

- [x] loadBundle calls sigstore.Verify; invalid sig ⇒ fail-closed.
- [x] PolicyRevisionPayload Signature field added (back-compat preserved).
- [x] safety.sign CUE schema declared.

### A — target (expected)

All B, plus:
- [x] Identity rotation supported via expected_identities []string set.
- [x] CUE validator rejects empty list when policy_required: true.
- [x] Adversarial reviewer cleared.

## Deletion default (per feedback_deletion_default)

CLOSES W8 §10 followup #1 (policy bundle signing was a Wave-1 followup
deferred from W8 to W10; T3 closes it). The Signature []byte field on
the existing payload ELIMINATES the parallel "signed-policy-revision"
event kind that would otherwise be needed. ZERO new SQL migrations.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w10-followup] Multi-signature threshold m-of-n (#NNN; spec §10 #9)
- [w10-followup] policy_required: true becomes per-tenant default rollout
   (#NNN; spec §10 #13 + W8.2)

```release-notes
[FEATURE] policy-bundle signature verification at W8 loader boot (Sign field on policy_revision payload; safety.sign.expected_identities CUE schema; fail-closed on invalid sig)
```
````

### Dispatch prompt — T3 (paste-ready)

```
You are an implementer subagent on a fresh git worktree at
.claude/worktrees/agent-w10-t3 on branch feat/w10-t3-policy-verify off main.
WAIT until T1 (#NNN — sigstore wrapper) has merged to main; rebase off
fresh main before starting.

# Spec authority (per feedback_spec_pattern_authority)

Source-of-truth: docs/engineer/specs/2026-06-01-w10-sigstore-design.md.
Read in full: §2 IN #4, §3.5 (policy bundle Verify integration verbatim),
§4 (W8 pattern reuse + Signature field on existing payload), §5 R3 + R4,
§6 T3 (named tests), §7 B/A rubric, §8 row 3.

Cross-spec: W8 spec §3.3 (Authorizer.Hydrate) + §10 followup #1 (policy
bundle signing — your PR CLOSES this).

If you want to deviate from any spec-mandated pattern — Signature []byte
field name (NOT Sig, NOT BundleJSON); ErrPolicyBundleSignatureInvalid
sentinel name; fail-closed atomic-swap NO-OP semantics; expected_identities
[]string set semantics; CUE field names verbatim — STOP and report.

# Scope (exclusive write paths)

- internal/gates/authz/loader.go                 (extend; ≤ 30 LoC delta)
- internal/authz/policies/payload.go             (extend; add Signature field)
- internal/cuevalidate/sign.cue                  (NEW; safety.sign schema)
- internal/gates/authz/loader_test.go            (extend; 5 named tests)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/cost/pricing/ — T4's scope.
- Do NOT touch cmd/regatta/ — T5's scope.
- Do NOT touch internal/sign/sigstore/ — T1's scope; you only IMPORT.

# Patterns to reuse

- sigstore.Verify + sigstore.ErrIdentityMismatch + sigstore.ErrRekorInclusionInvalid
  — from T1.
- W8 loadBundle path — insert Verify call BEFORE existing OPA compile.
- CUE validator pattern — mirror existing safety.cost block in
  internal/cuevalidate/.
- Substrate RegisterPayloadValidator — extend existing KindPolicyRevision
  validator (if W8 owns it) OR register new one (if W8 left it). Grep + pick.

# Workflow steps (TDD)

For each of 5 named tests: write test → capture failing output → implement →
re-run green → commit.

# Tests to land (5 named: 3 B + 2 A)

  B1. TestAuthorizerLoadBundle_ValidSignature_LoadsBundle
  B2. TestAuthorizerLoadBundle_InvalidSignature_PreservesPriorBundle
  B3. TestPolicyRevisionPayload_BackCompat_EmptySignatureRespectsConfig
  A1. TestAuthorizerLoadBundle_IdentityRotation_BothIdentitiesAccepted
  A2. TestCueValidator_PolicyRequiredTrueRejectsEmptyExpectedIdentities

# Workflow after green

  1. make pre-push-check clean.
  2. bash scripts/doc-check.sh && bash scripts/stale-todo.sh exit 0.
  3. File 2 followup issues (multi-sig m-of-n; W8.2 policy_required default).
  4. Push branch.
  5. gh pr create --base main --title
     "feat(w10): T3 policy-bundle sigstore.Verify integration (closes W8 §10 followup #1)"
     --body-file <path>. ```release-notes fence — grep-verify.
  6. Adversarial reviewer subagent + hunt list.
  7. Apply findings inline OR tracking issue.
  8. CI green BEFORE automerge.
  9. A+ scorecard verbatim in PR body.

# Adversarial reviewer hunt list

- Signature []byte field name verbatim (NOT Sig, NOT BundleJSON).
- json:"signature,omitempty" tag — back-compat with pre-W10 events
  REQUIRES `omitempty`.
- ErrPolicyBundleSignatureInvalid sentinel name verbatim.
- errors.Join wrap shape: errors.Join(ErrPolicyBundleSignatureInvalid, err)
  — NOT fmt.Errorf("%w: %w", ...). Both work; reviewer picks one and
  confirms callers use errors.Is.
- Atomic-swap NO-OP semantics: on Verify error, RETURN before the
  store-swap. Prior bundle remains active. Test B2 must assert this by
  reading the post-error compiledPolicy and comparing to pre-attempt state.
- expected_identities is a SET, not a singleton. Set semantics via
  []string + iterate-and-match-any. CUE list.MinItems(1) when
  policy_required: true.
- CUE schema field names verbatim (policy_required, pricing_required,
  expected_identities, policy_expected_identity, pricing_expected_identity,
  rekor_url).
- T4 cross-coordination: pricing_required + pricing_expected_identity
  declared in T3's sign.cue (NOT in a separate file owned by T4). Document
  this in the schema's package comment so T4's implementer grep-confirms.
- Closes W8 §10 followup #1 — PR body cites the closure explicitly.
- No AI signatures.
- godocs ≤ 1 line on test funcs.

# Hygiene

- NO AI signatures, doc-check + stale-todo exit 0, ```release-notes fence.

# Return format

PR URL, failing-test output ≥ 5 of 5, 2 followup numbers, reviewer verdict,
diff stat, A+ scorecard verbatim. Begin now.
```

---

## §5 Task T4 — Pricing-table Verify integration

### Scope

- **`internal/cost/pricing/pricing_verify.go`** — NEW. Per spec §3.7 lines 249-260:
  ```go
  //go:embed pricing.go.canonical
  var pricingCanonical []byte

  //go:embed pricing.go.sig
  var pricingSignature []byte

  func Load(ctx context.Context, expectedIdentity string) error {
      if !bytes.Equal(canonicalize(table), pricingCanonical) {
          return ErrPricingTableCanonicalDrift
      }
      if err := sigstore.Verify(ctx, pricingCanonical, pricingSignature, expectedIdentity); err != nil {
          return errors.Join(ErrPricingTableSignatureInvalid, err)
      }
      return nil
  }
  ```
  Two new sentinels: `ErrPricingTableCanonicalDrift`, `ErrPricingTableSignatureInvalid`.
  Mismatch ⇒ slog `obs.EventPricingTableTamper` ERROR + caller (boot) exits non-zero.
- **`internal/cost/pricing/canonical.go`** — NEW. `go:generate` directive + the
  `canonicalize(table)` function. Re-emits `pricing.go.canonical` from the in-memory var map
  with sorted keys (JSON canonicalization; encoding/json decode-then-marshal with sorted
  keys per the same pattern T4-W2 used for `APIResponseSig`).
- **`internal/cost/pricing/pricing.go.canonical`** — NEW. Generated artifact (canonical-JSON
  of the var map). Committed to repo; CI re-runs `go generate` + verifies no drift.
- **`internal/cost/pricing/pricing.go.sig`** — NEW. cosign bundle JSON over `.canonical`.
  CI signs on tag releases; local-dev re-signs with `local:dev`. Operators editing
  `pricing.go` locally MUST re-run `go generate` AND re-sign (`regatta sign --identity
  local:dev --in pricing.go.canonical --out pricing.go.sig`) — but the binary must be built
  with `-tags=dev` (or `!production`) for `local:` identities to verify.
- **`internal/cost/pricing/pricing_verify_test.go`** — 5 named tests below.
- **`cmd/regatta/serve.go`** — ONE-LINE addition: `pricing.Load(ctx,
  cfg.Safety.Sign.PricingExpectedIdentity)` at boot, BEFORE any pricing math. Mismatch ⇒
  `log.Fatalf("pricing: %v", err)` + non-zero exit. Net diff ≤ 6 LoC.

### Prereqs (cite spec sections)

- Spec §2 IN #5 (pricing-table load-path Verify).
- Spec §3.7 — pricing-table verification at boot **verbatim**.
- Spec §4 — cost-gov §9 R-A4 caveat narrowed; reconciler drift signal becomes redundancy.
- Spec §5 R4 (ToCToU) — re-canonicalize in-memory map + compare to verified canonical bytes.
- Spec §6 T4 — 3 B-tier + 2 A-tier tests.
- Spec §8 row 4.
- Cost-gov spec §9 R-A4 + §3.5 (`pricing.Lookup` already in use by W2 T2-merged).

### Existing patterns to reuse (do NOT reinvent)

- **`sigstore.Verify`** — from T1.
- **Canonical-JSON decode-then-marshal** — pattern from cost-gov W2 T4 reconciler's
  `APIResponseSig` computation. Mirror that helper exactly.
- **`go:embed`** — standard Go stdlib; embed `.canonical` + `.sig` files at package level.
- **`obs.EventPricingTableTamper`** — slog event constant declared in T4 (or under
  `internal/obs/` if W2 already declared a parallel event for tamper detection; grep first).
- **`cmd/regatta/serve.go` boot path** — existing pattern from cost-gov W2 + substrate-W1
  boot. Add ONE call to `pricing.Load`; do NOT introduce a new init phase.

### TDD test list (named tests per spec §6 T4)

**B-tier (3 named tests):**

1. `TestPricingLoad_ValidSignature_Succeeds` — happy path: canonical matches in-memory map;
   sig verifies under the expected identity; `Load` returns nil.
2. `TestPricingLoad_CanonicalDrift_ReturnsErrPricingTableCanonicalDrift` — edit the
   in-memory var map (test mutates a copy) WITHOUT regenerating canonical ⇒ typed sentinel.
   Pins the "edit pricing.go but forget to regenerate" defect (spec §3.7 line 253).
3. `TestPricingLoad_SignatureMismatch_ReturnsErrPricingTableSignatureInvalid` — sig over
   wrong bytes (test fixture) ⇒ typed sentinel + slog `obs.EventPricingTableTamper` at ERROR.

**A-tier (2 named tests):**

4. `TestPricingLoad_ProductionBuild_RejectsLocalSig` — `-tags=production` build refuses a
   pricing table signed under `local:dev` identity (returns `ErrPricingTableSignatureInvalid`
   wrapping `sigstore.ErrIdentityNotAllowed`). Pins R5 dev-key-promotion defense for the
   pricing-table path.
5. `TestServeMain_PricingLoadFailure_NonZeroExit` — integration: stub a tampered canonical
   file; `cmd/regatta/serve.go` boot exits non-zero. Asserted via `cmd/regatta/serve_test.go`
   or a binary-invocation test if one exists; otherwise wrap the call in a testable helper
   `runServe(ctx, cfg) int` so the test can assert the exit code without `os.Exit`.

### PR body skeleton — T4

````
## Summary

W10 T4: ship boot-time pricing-table verification per spec §3.7. Closes
the cost-gov §9 R-A4 caveat (pricing-applied-twice defect becomes
detectable at boot).

- internal/cost/pricing/pricing_verify.go — NEW. Load(ctx, expectedIdentity).
  Re-canonicalizes the in-memory var map, compares to embedded canonical
  bytes (catches "edit pricing.go but forget to regenerate" drift), then
  verifies cosign signature.
- internal/cost/pricing/canonical.go — NEW. go:generate directive + the
  canonicalize() helper (decode-then-marshal with sorted keys; mirrors
  W2 T4 APIResponseSig pattern).
- internal/cost/pricing/pricing.go.canonical — NEW (generated).
- internal/cost/pricing/pricing.go.sig — NEW (cosign bundle JSON; CI signs
  on release; local-dev re-signs with `regatta sign --identity local:dev`).
- internal/cost/pricing/pricing_verify_test.go — 5 named tests.
- cmd/regatta/serve.go — ONE-LINE addition: pricing.Load(ctx, cfg.Safety.Sign.PricingExpectedIdentity)
  at boot. Mismatch ⇒ log.Fatalf + non-zero exit.

## Why

Per spec §3.7 + cost-gov §9 R-A4: the "pricing-applied-twice" defect was
invisible to the reconciler when both sides applied the same (potentially
tampered) table. T4 makes the table itself non-tamperable at boot — any
local edit to pricing.go without re-running CI (which re-signs against the
release OIDC identity) fails Load with ErrPricingTableSignatureInvalid.
Per R4, Load re-canonicalizes the in-memory map and compares to the
verified canonical bytes — closes ToCToU (verified bytes are the bytes
consumed by Lookup).

## Test plan

- [x] B-tier (3): TestPricingLoad_ValidSignature_Succeeds,
       TestPricingLoad_CanonicalDrift_ReturnsErrPricingTableCanonicalDrift,
       TestPricingLoad_SignatureMismatch_ReturnsErrPricingTableSignatureInvalid.
- [x] A-tier (2): TestPricingLoad_ProductionBuild_RejectsLocalSig,
       TestServeMain_PricingLoadFailure_NonZeroExit.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Grade rubric scorecard (per feedback_grade_rubric — MANDATORY)

### B — floor (ships)

- [x] internal/cost/pricing.Load(ctx, expectedIdentity) calls sigstore.Verify.
- [x] Two typed sentinels: ErrPricingTableCanonicalDrift +
       ErrPricingTableSignatureInvalid.
- [x] cmd/regatta/serve.go boot calls pricing.Load before any pricing math;
       mismatch ⇒ non-zero exit.
- [x] obs.EventPricingTableTamper slog at ERROR on mismatch.
- [x] go generate ./internal/cost/pricing/... re-emits .canonical; CI verifies
       no drift.

### A — target (expected)

All B, plus:
- [x] -tags=production build refuses local:-signed pricing tables.
- [x] Adversarial reviewer cleared.

## Deletion default (per feedback_deletion_default)

NARROWS cost-gov §9 R-A4 caveat: the pricing-applied-twice defect's
exposure window shrinks from "always present, mitigated only by drift
signal" to "only present if signing is opted out via pricing_required:
false". The reconciler's drift signal becomes a redundancy, not the
sole defense. ZERO new SQL migrations.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w10-followup] OCI registry as artifact store for pricing.sig
   (#NNN; spec §10 #7 — file only if T2 didn't already)
- [w10-followup] Hardware-key support for local pricing re-sign
   (#NNN; spec §10 #2 — file only if T1 didn't already)

```release-notes
[FEATURE] pricing-table boot-time sigstore.Verify (closes cost-gov §9 R-A4 caveat; fail-closed on signature mismatch or canonical drift)
```
````

### Dispatch prompt — T4 (paste-ready)

```
You are an implementer subagent on a fresh git worktree at
.claude/worktrees/agent-w10-t4 on branch feat/w10-t4-pricing-verify off main.
WAIT until T1 (sigstore wrapper) has merged to main; rebase off fresh main.
T3 (sign.cue) may merge in parallel — coordinate field names with T3's PR
(pricing_required + pricing_expected_identity declared in T3's sign.cue).

# Spec authority (per feedback_spec_pattern_authority)

Source-of-truth: docs/engineer/specs/2026-06-01-w10-sigstore-design.md.
Read in full: §2 IN #5, §3.7 (pricing-table verify verbatim), §4 (cost-gov
R-A4 narrowed), §5 R4 + R5, §6 T4 (named tests), §7 B/A rubric, §8 row 4.

Cross-spec: cost-gov spec §9 R-A4 caveat — your PR NARROWS this; PR body
cites the closure of the exposure window.

If you want to deviate from any spec-mandated pattern — Load signature
verbatim; ErrPricingTableCanonicalDrift + ErrPricingTableSignatureInvalid
sentinel names; canonical-JSON sorted-keys algorithm; obs.EventPricingTableTamper
slog event name; ONE-LINE serve.go addition — STOP and report.

# Scope (exclusive write paths)

- internal/cost/pricing/pricing_verify.go        (NEW)
- internal/cost/pricing/canonical.go             (NEW; go:generate + helper)
- internal/cost/pricing/pricing.go.canonical     (NEW; generated)
- internal/cost/pricing/pricing.go.sig           (NEW; cosign bundle)
- internal/cost/pricing/pricing_verify_test.go   (NEW)
- cmd/regatta/serve.go                           (ONE-LINE addition; ≤ 6 LoC delta)

You MUST NOT touch any other file. Specifically:
- Do NOT modify internal/cost/pricing/pricing.go (the existing var map)
  except via go:generate output if your canonicalize() helper writes it.
  In practice: the var map is read-only here; only .canonical + .sig are
  new files.
- Do NOT touch internal/gates/authz/ — T3's scope.
- Do NOT touch internal/sign/sigstore/ — T1's; you only IMPORT.
- Do NOT touch internal/cuevalidate/sign.cue — T3 declares the full schema
  (pricing_required + pricing_expected_identity included). Grep-verify the
  field names exist; report if missing.

# Patterns to reuse

- sigstore.Verify + six sentinels — T1.
- canonical-JSON (decode-then-marshal sorted keys) — cost-gov W2 T4
  APIResponseSig helper. Mirror.
- go:embed — stdlib; embed .canonical + .sig.
- obs slog event — declare obs.EventPricingTableTamper in obs/event.go OR
  reuse if W2 already has a parallel event (grep first).
- cmd/regatta/serve.go boot pattern — existing W2 + substrate boot; add ONE
  call; do NOT introduce a new init phase.

# Workflow steps (TDD)

For each of 5 named tests: write test → capture failing output → implement →
green → commit.

# Tests to land (5 named: 3 B + 2 A)

  B1. TestPricingLoad_ValidSignature_Succeeds
  B2. TestPricingLoad_CanonicalDrift_ReturnsErrPricingTableCanonicalDrift
  B3. TestPricingLoad_SignatureMismatch_ReturnsErrPricingTableSignatureInvalid
  A1. TestPricingLoad_ProductionBuild_RejectsLocalSig
  A2. TestServeMain_PricingLoadFailure_NonZeroExit

# Workflow after green

  1. make pre-push-check clean.
  2. doc-check + stale-todo exit 0.
  3. File 2 followups (OCI registry; hardware-key — only if not already filed).
  4. Push branch.
  5. gh pr create --base main --title
     "feat(w10): T4 pricing-table boot-time sigstore.Verify (closes cost-gov R-A4)"
     --body-file <path>. ```release-notes fence — grep-verify.
  6. Adversarial reviewer + hunt list.
  7. Apply findings.
  8. CI green BEFORE automerge.
  9. A+ scorecard verbatim.

# Adversarial reviewer hunt list

- Load signature verbatim: `func Load(ctx context.Context, expectedIdentity
  string) error`. No additional params; no return of canonical bytes
  (ToCToU defense — Load is nil-or-err only).
- Sentinel names: ErrPricingTableCanonicalDrift,
  ErrPricingTableSignatureInvalid (verbatim).
- Canonical-JSON sorted keys — decode-then-marshal with json.Marshal +
  map[string]interface{} key sort; NOT lexical-string of the original
  source file.
- go:embed of .canonical and .sig — at package level, NOT inside a function.
- cmd/regatta/serve.go: ONE-LINE addition; ≤ 6 LoC delta. log.Fatalf on
  mismatch (or equivalent non-zero exit pattern from the existing boot path).
- obs.EventPricingTableTamper slog at ERROR (NOT WARN).
- T4 wraps via errors.Join(ErrPricingTableSignatureInvalid, err) — callers
  use errors.Is to distinguish from canonical-drift vs sigstore sentinels.
- ToCToU defense (R4): Load re-canonicalizes the in-memory var map and
  compares to embedded canonical bytes BEFORE sigstore.Verify. If they
  differ, return ErrPricingTableCanonicalDrift — do NOT proceed to Verify
  (which would pass against tampered canonical bytes and miss the drift).
- Build-tag gate: prod build with local:-signed pricing ⇒
  ErrIdentityNotAllowed propagates through Load as
  ErrPricingTableSignatureInvalid (wrapped).
- ZERO new SQL migrations.
- Closes cost-gov §9 R-A4 caveat — PR body cites the closure.
- No AI signatures; godocs ≤ 1 line.

# Hygiene

- NO AI signatures, doc-check + stale-todo exit 0, ```release-notes fence.

# Return format

PR URL, failing-test output ≥ 5 of 5, 2 followup numbers, reviewer verdict,
diff stat, A+ scorecard verbatim. Begin now.
```

---

## §6 Task T5 — Local-dev fallback keys + `regatta sign` CLI + build-tag gate

### Scope

- **`cmd/regatta/sign.go`** — NEW. cobra subcommand root: `regatta sign`. Three
  subcommands: `init-dev-key`, `--identity ... --in ... --out ...` (the default sign
  action), `refresh-rekor-root`.
- **`cmd/regatta/sign_init_dev_key.go`** — NEW. `regatta sign init-dev-key`:
  - Generates an ECDSA-P256 keypair via `sigstore-go`'s key-gen helper (NOT raw
    `crypto/ecdsa`).
  - Writes to `$XDG_DATA_HOME/regatta/keys/dev.{pub,priv}` (default
    `~/.local/share/regatta/keys/`).
  - Permissions: priv = `0600`; pub = `0644`. Verified post-write via `os.Stat`.
  - Idempotent: if keys exist, prints "already initialized" + exits 0 (no overwrite).
- **`cmd/regatta/sign_sign_blob.go`** — NEW. `regatta sign --identity ... --in ... --out ...`:
  - Reads `<in>` into memory.
  - Calls `sigstore.Sign(ctx, bytes, identity)`.
  - Writes signature to `<out>`.
  - For `local:` identities under default build tag, this exercises T1's local-dev path.
  - Under `-tags=production`, refuses `local:` identities (relies on T1's `Verify`
    propagation — but `Sign` itself must also refuse `local:` in prod builds per spec §3.3
    line 177; document the symmetry).
- **`cmd/regatta/sign_refresh_rekor_root.go`** — NEW. `regatta sign refresh-rekor-root`:
  forced re-fetch of Rekor tree head via `sigstore.cache.ForceRefresh(ctx)` (T1-exported).
- **`internal/sign/sigstore/build_production.go`** — NEW. `//go:build production` build
  constraint. Overrides T1's swappable hook (e.g. `verifyLocalIdentity`) to return
  `ErrIdentityNotAllowed` whenever `expectedIdentity` has prefix `local:`.
- **`internal/sign/sigstore/build_dev.go`** — NEW. `//go:build !production` (default tag).
  Overrides T1's swappable hook to ALLOW `local:` identities through the local-dev verify
  path.
- **`cmd/regatta/sign_test.go`** — 4 named tests below.
- **`internal/sign/sigstore/build_production_test.go`** — 1 named test (the prod-refusal
  invariant).

### Prereqs (cite spec sections)

- Spec §2 IN #7 (local-dev fallback keys + CLI).
- Spec §3.3 — local-dev flow + key permissions + prod-tag refusal.
- Spec §5 R5 (dev key promoted to prod) — `-tags=production` refusal is THE primary
  mitigation; T5 owns the implementation.
- Spec §6 T5 — 3 B + 1 A + 1 A+.
- Spec §7 B/A/A+.
- Spec §8 row 5.

### Existing patterns to reuse (do NOT reinvent)

- **cobra** — existing CLI framework (every other `regatta <cmd>` uses it). Mirror the
  existing subcommand registration pattern.
- **sigstore-go key-gen helper** — generates ECDSA-P256 via SDK; do NOT use raw
  `crypto/ecdsa`.
- **XDG Base Directory** — same pattern as T1.
- **`//go:build` tag pattern** — Go stdlib idiom; one file per tag. The swappable hook in
  T1 (e.g. `var verifyLocalIdentity = ...`) is what these two build files override.

### TDD test list (named tests per spec §6 T5)

**B-tier (3 named tests):**

1. `TestInitDevKey_GeneratesECDSAP256Pair` — `regatta sign init-dev-key` writes files at
   the XDG path with perms 0600/0644; the key parses as ECDSA-P256.
2. `TestSignVerify_LocalIdentity_RoundTrips` — sign with `local:dev`, verify with
   `local:dev`; no network call. Pins local-dev seam end-to-end.
3. `TestInitDevKey_Idempotent_DoesNotOverwrite` — re-run does not overwrite existing keys;
   prints "already initialized"; exits 0.

**A-tier (1 named test):**

4. `TestVerify_LocalIdentity_RefusedInProdBuild` — under `//go:build production` test
   harness, `sigstore.Verify(ctx, ..., "local:dev")` returns `ErrIdentityNotAllowed`. Pins
   R5 mitigation.

**A+-tier (1 named test):**

5. `TestRefreshRekorRoot_ForcesCacheRefetch` — `regatta sign refresh-rekor-root` invokes
   `cache.ForceRefresh`; cache file's mtime is updated; subsequent boot reads the fresh
   head. Pins R1 operator escape hatch.

### PR body skeleton — T5

````
## Summary

W10 T5: `regatta sign` CLI subcommands (init-dev-key + sign-blob +
refresh-rekor-root) + the `-tags=production` build-tag gate that refuses
local: identities in prod builds.

- cmd/regatta/sign.go — cobra subcommand root.
- cmd/regatta/sign_init_dev_key.go — ECDSA-P256 keypair gen at
  $XDG_DATA_HOME/regatta/keys/. Perms 0600/0644. Idempotent.
- cmd/regatta/sign_sign_blob.go — thin wrapper around sigstore.Sign.
- cmd/regatta/sign_refresh_rekor_root.go — forced Rekor tree-head re-fetch.
- internal/sign/sigstore/build_production.go — //go:build production;
  refuses local: identities via T1's swappable hook.
- internal/sign/sigstore/build_dev.go — //go:build !production; allows
  local: through the dev path.
- cmd/regatta/sign_test.go + internal/sign/sigstore/build_production_test.go
  — 5 named tests.

## Why

Per spec §3.3: local-dev fallback gives operators a network-free signing
path on workstations while keeping the prod surface tight. R5 (dev key
promoted to prod) is closed by the build-tag gate — a prod binary refuses
local: identities at the package boundary, so a copied key file is inert.
The XDG layout keeps key files inside the user's per-user data directory
with restrictive perms (0600 priv).

## Test plan

- [x] B-tier (3): TestInitDevKey_GeneratesECDSAP256Pair,
       TestSignVerify_LocalIdentity_RoundTrips,
       TestInitDevKey_Idempotent_DoesNotOverwrite.
- [x] A-tier (1): TestVerify_LocalIdentity_RefusedInProdBuild.
- [x] A+-tier (1): TestRefreshRekorRoot_ForcesCacheRefetch.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal>

## Grade rubric scorecard (per feedback_grade_rubric — MANDATORY)

### B — floor (ships)

- [x] regatta sign init-dev-key + sign-blob + refresh-rekor-root subcommands.
- [x] Keypair perms 0600/0644 verified post-write.
- [x] -tags=production refuses local: identities (R5 mitigation).
- [x] Idempotent init-dev-key (no overwrite).

### A — target (expected)

All B, plus:
- [x] Build-tag gate property-tested in prod-build harness.
- [x] Adversarial reviewer cleared.

### A+ — stretch

- [x] refresh-rekor-root forces re-fetch and updates cache mtime.

## Deletion default (per feedback_deletion_default)

The three subcommands under existing `regatta sign` namespace REPLACE
~150 LoC of bespoke key-management UX that custom signing would
otherwise need (per spec §4 line 302). ZERO new top-level CLI
subcommand. ZERO new SQL migrations.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w10-followup] Operator-facing key-rotation UI (#NNN; spec §1.2 + §10 #5)
- [w10-followup] Signature pinning by fingerprint TOFU (#NNN; spec §10 #10)

```release-notes
[FEATURE] regatta sign CLI subcommands (init-dev-key + sign-blob + refresh-rekor-root) + -tags=production build-tag gate refusing local: identities in prod builds
```
````

### Dispatch prompt — T5 (paste-ready)

```
You are an implementer subagent on a fresh git worktree at
.claude/worktrees/agent-w10-t5 on branch feat/w10-t5-cli-sign off main.
You can dispatch in Wave A parallel with T1 + T2 BUT must rebase off main
once T1 merges; until then, define a thin local interface stub matching
T1's Sign + Verify signatures so your tests compile.

# Spec authority (per feedback_spec_pattern_authority)

Source-of-truth: docs/engineer/specs/2026-06-01-w10-sigstore-design.md.
Read in full: §2 IN #7, §3.3 (local-dev flow + key perms + prod refusal),
§5 R5 (build-tag gate is THE mitigation; you own it), §6 T5 (named tests),
§7 B/A/A+ rubric, §8 row 5.

If you want to deviate from any spec-mandated pattern — three subcommand
names (init-dev-key, refresh-rekor-root, the default sign-blob action with
--identity/--in/--out); XDG_DATA_HOME path; 0600/0644 perms; ECDSA-P256
keypair; -tags=production gate file names (build_production.go + build_dev.go);
idempotent init-dev-key — STOP and report.

# Scope (exclusive write paths)

- cmd/regatta/sign.go                            (NEW; cobra root)
- cmd/regatta/sign_init_dev_key.go               (NEW)
- cmd/regatta/sign_sign_blob.go                  (NEW)
- cmd/regatta/sign_refresh_rekor_root.go         (NEW)
- internal/sign/sigstore/build_production.go     (NEW; //go:build production)
- internal/sign/sigstore/build_dev.go            (NEW; //go:build !production)
- cmd/regatta/sign_test.go                       (NEW)
- internal/sign/sigstore/build_production_test.go (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT modify internal/sign/sigstore/{sign,verify,errors,cache}.go — T1's.
  Your build_*.go files override the swappable hook T1 exports.
- Do NOT touch internal/gates/authz/ OR internal/cost/pricing/ — T3, T4.

PER feedback_plan_subagent_dup_files: build_production.go +
build_dev.go are YOUR EXCLUSIVE SCOPE. T1's dispatch prompt EXPLICITLY
excludes these two filenames. If T1's PR includes either of those files,
STOP and report — this is a duplicate file collision and the plan needs
re-spawning.

# Patterns to reuse

- cobra subcommand registration — mirror existing regatta CLI subcommands.
- sigstore-go key-gen helper — NOT raw crypto/ecdsa.
- XDG Base Directory — same as T1.
- //go:build tag — Go stdlib idiom.

# Workflow steps (TDD)

For each of 5 named tests: write test → capture failing output → implement
→ green → commit.

# Tests to land (5 named: 3 B + 1 A + 1 A+)

  B1. TestInitDevKey_GeneratesECDSAP256Pair
  B2. TestSignVerify_LocalIdentity_RoundTrips
  B3. TestInitDevKey_Idempotent_DoesNotOverwrite
  A1. TestVerify_LocalIdentity_RefusedInProdBuild
  P1. TestRefreshRekorRoot_ForcesCacheRefetch

# Workflow after green

  1. make pre-push-check clean (both default tag AND -tags=production).
     Run: `go test -tags=production ./internal/sign/sigstore/...` — confirm
     A1 passes.
  2. doc-check + stale-todo exit 0.
  3. File 2 followups (key-rotation UI; TOFU pinning).
  4. Push branch.
  5. gh pr create --base main --title
     "feat(w10): T5 regatta sign CLI + local-dev keys + -tags=production gate"
     --body-file <path>. ```release-notes fence — grep-verify.
  6. Adversarial reviewer + hunt list.
  7. Apply findings.
  8. CI green BEFORE automerge.
  9. A+ scorecard verbatim.

# Adversarial reviewer hunt list

- Subcommand names verbatim: init-dev-key, refresh-rekor-root. The default
  sign action takes --identity, --in, --out (NOT --key, --input, --output).
- XDG path verbatim: $XDG_DATA_HOME/regatta/keys/ (fallback ~/.local/share/
  regatta/keys/). NOT ~/.regatta/.
- Perms 0600 (priv) + 0644 (pub) — verified via os.Stat post-write.
- ECDSA-P256 via sigstore-go SDK helper; NO crypto/ecdsa import in
  cmd/regatta/.
- Idempotent init-dev-key: re-run prints "already initialized" + exits 0.
  Test B3 must cover this exact case.
- //go:build production OR //go:build !production (the negation form for
  the default-on dev gate). The two files MUST have mutually exclusive
  build constraints; verified via `go build -tags=production .` AND
  `go build .` both compile.
- T5's build_*.go files MUST override T1's swappable hook by NAME — grep
  T1's verify.go for `var verifyLocalIdentity` (or whatever T1 named it);
  the two build_*.go files reassign it in init(). Document the name in
  the package comment so the linkage is discoverable.
- Build-tag refusal is at the PACKAGE BOUNDARY in sigstore.Verify — NOT
  at the CLI layer. A test that calls sigstore.Verify directly under
  prod build with local: identity must return ErrIdentityNotAllowed. The
  CLI layer is the convenient surface, but the gate lives one layer down.
- refresh-rekor-root: invokes cache.ForceRefresh (T1-exported). NOT a
  parallel re-fetch implementation.
- ZERO new SQL migrations.
- No AI signatures; godocs ≤ 1 line.

# Hygiene

- NO AI signatures, doc-check + stale-todo exit 0, ```release-notes fence.

# Return format

PR URL, failing-test output ≥ 5 of 5, 2 followup numbers, reviewer verdict,
diff stat, A+ scorecard verbatim. Begin now.
```

---

## §7 Task T6 — OTel + audit events + plan-as-code loader stub + operator docs

### Scope

- **`internal/sign/sigstore/otel.go`** — NEW. `VerifyWithSpan(ctx, artifact, sig,
  expectedIdentity, kind) error` opens the `sign.verify` span (kind=internal), sets the six
  attrs from spec §3.8, calls T1's bare `Verify`, sets `regatta.sign.verified` based on
  outcome + sets `regatta.sign.failure_reason` (one of six sentinel-derived strings) on
  failure, ends span. The new `kind` param is the `regatta.sign.artifact_kind` attr value
  (enum: `policy|pricing|plan|release`).
- **`internal/sign/sigstore/audit.go`** — NEW. Two new substrate event kinds:
  `KindSignVerified` + `KindSignFailed` (declared in substrate's kind enum via an
  open-extension addition if T-S1 #224's pattern allows — otherwise re-export the constants
  under `sigstore.` namespace and register validators). `init()` block calls
  `substrate.RegisterPayloadValidator` for both kinds. Each Verify outcome (from
  `VerifyWithSpan`) emits one event with payload `{Identity, RekorUUID, ArtifactKind,
  VerifyMicros, FailureReason}`. NO new DDL.
- **`internal/plan/loader.go`** — NEW STUB. Per spec §3.6:
  ```go
  package plan

  type Verifier interface {
      Verify(ctx context.Context, artifact []byte, signature []byte, identity string) error
  }

  type sigstoreVerifier struct{}

  func (sigstoreVerifier) Verify(ctx context.Context, artifact, signature []byte, identity string) error {
      return sigstore.VerifyWithSpan(ctx, artifact, signature, identity, "plan")
  }

  var DefaultVerifier Verifier = sigstoreVerifier{}
  ```
  P4 (plan-as-code wedge) wires the actual `.regatta/plans/*.yaml` consumer. W10's
  contribution: the seam exists; P4 implements zero plumbing.
- **`docs/operator/sign.md`** — NEW. Security-model doc covering: artifact-signing surface
  + trust roots + OIDC keyless model + local-dev fallback + CUE config reference + every
  failure mode.
- **`docs/operator/sign-rekor-outage.md`** — NEW. R1 outage runbook.
- **`docs/operator/sign-identity-rotation.md`** — NEW. R3 rotation runbook.
- **`docs/operator/sign-airgap.md`** — NEW. R10 air-gap deploy guide (stub; full guide is
  followup #12 per spec §10).
- **`internal/sign/sigstore/otel_test.go`** + **`internal/sign/sigstore/audit_test.go`** —
  3 named tests below.
- **Call-site rewrite**: T6 ALSO touches the two call-sites in T3
  (`internal/gates/authz/loader.go`) + T4 (`internal/cost/pricing/pricing_verify.go`) to
  swap `sigstore.Verify(...)` → `sigstore.VerifyWithSpan(..., "policy")` and
  `..., "pricing"`. **Net diff in those two files: ≤ 4 LoC combined.** This is the only
  cross-file edit T6 makes outside its primary scope.

### Prereqs (cite spec sections)

- Spec §2 IN #9 (OTel attrs).
- Spec §3.6 (plan-as-code seam — T6 ships the stub).
- Spec §3.8 — OTel attr table verbatim.
- Spec §5 R7 (rekor_uuid cardinality blowup — 8-char prefix in OTel attr).
- Spec §6 T6 — 2 B + 1 A.
- Spec §8 row 6.
- W6 spec §3 (OTel backbone tracer factory + attribute conventions).

### Existing patterns to reuse (do NOT reinvent)

- **W6 tracer factory** — `otel.Tracer("internal/sign/sigstore")` + the existing W6
  attribute conventions. NO new tracer factory.
- **substrate `RegisterPayloadValidator`** — T-S1 #224's open-extension hook. Mirror
  cost-gov T3's `init()` block pattern.
- **slog→OTel bridge** — W6 T2 #169. Emit `obs.EventSignVerifyBootstrap` (T6-owned slog
  event constant) on the cached-tree-head-fallback path per R10.
- **`go-cmp` or table-driven test** — mirror existing test conventions in
  `internal/gates/` and `internal/cost/`.

### TDD test list (named tests per spec §6 T6)

**B-tier (2 named tests):**

1. `TestVerify_EmitsAllAttributes` — `VerifyWithSpan` ends a span with all 6
   `regatta.sign.*` attrs set (identity, rekor_uuid, verified, artifact_kind, verify_micros,
   failure_reason-when-failed). Asserted via a `tracetest` exporter mock.
2. `TestVerify_RekorUUIDAttr_Is8CharPrefix` — `regatta.sign.rekor_uuid` attr value length
   is exactly 8 chars (R7 cardinality guard).

**A-tier (1 named test):**

3. `TestSignVerifyAuditEvents_PayloadIncludesFullRekorUUID` — substrate audit event
   payload for `kind=sign_verified` includes the FULL rekor_uuid (the truncation is OTel-only
   per R7); event row exists in substrate after a successful Verify call via `VerifyWithSpan`.

### PR body skeleton — T6

````
## Summary

W10 T6 (final wave): wire OTel attrs + substrate audit events + plan-as-code
loader stub + four operator docs. Closes W10 from a user-visible perspective.

- internal/sign/sigstore/otel.go — VerifyWithSpan(ctx, ..., kind) opens
  `sign.verify` span, sets 6 attrs per spec §3.8, calls T1's Verify, emits
  audit event.
- internal/sign/sigstore/audit.go — substrate KindSignVerified +
  KindSignFailed event kinds + RegisterPayloadValidator init() block.
  ZERO new DDL.
- internal/plan/loader.go — NEW STUB. Verifier interface +
  sigstoreVerifier{} default impl. P4 plan-as-code wedge wires the
  consumer with zero refactor.
- docs/operator/sign.md — security model + CUE config reference.
- docs/operator/sign-rekor-outage.md — R1 runbook.
- docs/operator/sign-identity-rotation.md — R3 runbook.
- docs/operator/sign-airgap.md — R10 stub (full guide is followup #12).
- internal/gates/authz/loader.go + internal/cost/pricing/pricing_verify.go
  — ≤ 4 LoC combined: swap sigstore.Verify(...) → VerifyWithSpan(..., "policy")
  / VerifyWithSpan(..., "pricing").

## Why

Per spec §3.8 + W6 OTel backbone: every Verify call gets observability +
audit-event lineage so an operator can debug a signature failure post-hoc.
Per R7: rekor_uuid is 8-char in the OTel attr (cardinality bound), full
UUID in the substrate event payload (durable audit trail). The plan-as-code
stub lets P4 land with zero plumbing — drop in DefaultVerifier + call Verify
before YAML parse.

## Test plan

- [x] B-tier (2): TestVerify_EmitsAllAttributes,
       TestVerify_RekorUUIDAttr_Is8CharPrefix.
- [x] A-tier (1): TestSignVerifyAuditEvents_PayloadIncludesFullRekorUUID.
- [x] make pre-push-check clean.
- [x] doc-check + stale-todo exit 0.

## Failing-test output (TDD capture, before impl)

<paste from terminal>

## Grade rubric scorecard (per feedback_grade_rubric — MANDATORY)

### B — floor (ships)

- [x] 6 OTel attrs emitted on every Verify span.
- [x] rekor_uuid attr is 8-char prefix (R7).
- [x] sign.verify span name; parent is caller's span.
- [x] KindSignVerified + KindSignFailed substrate event kinds; ZERO new DDL.
- [x] Plan-as-code Verifier interface stub.
- [x] 4 operator docs (sign.md, sign-rekor-outage.md, sign-identity-rotation.md,
       sign-airgap.md).

### A — target (expected)

All B, plus:
- [x] Substrate audit event payload includes FULL rekor_uuid.
- [x] Adversarial reviewer cleared.

## Deletion default (per feedback_deletion_default)

REUSES W6 tracer factory + slog→OTel bridge verbatim. The `sign.verify`
span is the ONE new span name across W10; no parallel naming convention.
Plan-as-code stub is interface-only (P4 wires the consumer with ZERO
refactor). ZERO new SQL migrations.

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w10-followup] Rekor monitor + alert pipeline (#NNN; spec §5 R6 + §10 #6)
- [w10-followup] W10.2 in-toto agent-decision attestation (#NNN; spec §10 #11)
- [w10-followup] Runtime attestation via substrate event stream (#NNN; spec §10 #4)

```release-notes
[FEATURE] sigstore OTel attrs (sign.verify span + 6 attrs per spec §3.8) + substrate audit events (kind=sign_verified / kind=sign_failed; ZERO new DDL) + plan-as-code Verifier interface stub + 4 operator docs (security model, Rekor outage, identity rotation, air-gap stub)
```
````

### Dispatch prompt — T6 (paste-ready)

```
You are an implementer subagent on a fresh git worktree at
.claude/worktrees/agent-w10-t6 on branch feat/w10-t6-otel-docs off main.
WAIT until T1 + T3 + T4 + T5 have ALL merged to main; rebase off fresh main.

# Spec authority (per feedback_spec_pattern_authority)

Source-of-truth: docs/engineer/specs/2026-06-01-w10-sigstore-design.md.
Read in full: §2 IN #9, §3.6 (plan-as-code stub), §3.8 (OTel attr table
verbatim), §5 R7 + R10, §6 T6 (named tests), §7 B/A rubric, §8 row 6.
Cross-spec: W6 spec §3 (OTel backbone — tracer factory + attribute
conventions; REUSE verbatim).

If you want to deviate from any spec-mandated pattern — sign.verify span
name; 6 attr names + types from §3.8 table; rekor_uuid 8-char-prefix
truncation (NOT 16, NOT full); KindSignVerified + KindSignFailed event
kind names; Verifier interface signature in plan/loader.go — STOP and report.

# Scope (exclusive write paths)

- internal/sign/sigstore/otel.go                 (NEW)
- internal/sign/sigstore/audit.go                (NEW)
- internal/plan/loader.go                        (NEW STUB)
- docs/operator/sign.md                          (NEW)
- docs/operator/sign-rekor-outage.md             (NEW)
- docs/operator/sign-identity-rotation.md        (NEW)
- docs/operator/sign-airgap.md                   (NEW)
- internal/sign/sigstore/otel_test.go            (NEW)
- internal/sign/sigstore/audit_test.go           (NEW)

Cross-file ONE-LINE edits (≤ 4 LoC combined):
- internal/gates/authz/loader.go     (sigstore.Verify → VerifyWithSpan, "policy")
- internal/cost/pricing/pricing_verify.go (sigstore.Verify → VerifyWithSpan, "pricing")

You MUST NOT touch any other file. Specifically:
- Do NOT modify internal/sign/sigstore/{sign,verify,errors,cache}.go — T1's
  scope. You only ADD sibling files + IMPORT T1's API.
- Do NOT modify cmd/regatta/ — T5's scope.
- Do NOT modify internal/authz/policies/payload.go OR internal/cuevalidate/
  — T3's scope.

# Patterns to reuse

- W6 tracer factory: otel.Tracer("internal/sign/sigstore"). REUSE; no new
  factory.
- substrate.RegisterPayloadValidator — T-S1 #224's open-extension hook.
  init() block. Mirror cost-gov T3 pattern.
- slog→OTel bridge — W6 T2 #169. Emit obs.EventSignVerifyBootstrap on the
  cached-tree-head-fallback path per R10.
- Doc-check: bash scripts/doc-check.sh — every relative .md link MUST
  resolve; banned-phrase lint exits 0. READ feedback_doc_check_banned_phrases.md
  for the token list; do NOT inline the list in any of the four docs.

# Workflow steps (TDD)

For each of 3 named tests: write test → capture failing output → implement
→ green → commit.

# Tests to land (3 named: 2 B + 1 A)

  B1. TestVerify_EmitsAllAttributes
  B2. TestVerify_RekorUUIDAttr_Is8CharPrefix
  A1. TestSignVerifyAuditEvents_PayloadIncludesFullRekorUUID

# Workflow after green

  1. make pre-push-check clean.
  2. bash scripts/doc-check.sh && bash scripts/stale-todo.sh — exit 0.
     The four new .md files must have every relative .md link resolve.
  3. File 3 followups (Rekor monitor; W10.2 in-toto; runtime attestation).
  4. Push branch.
  5. gh pr create --base main --title
     "feat(w10): T6 OTel attrs + substrate audit events + plan loader stub + operator docs"
     --body-file <path>. ```release-notes fence — grep-verify.
  6. Adversarial reviewer + hunt list.
  7. Apply findings.
  8. CI green BEFORE automerge.
  9. A+ scorecard verbatim.

# Adversarial reviewer hunt list

- Span name: sign.verify (NOT sigstore.verify, NOT sign_verify).
- Attr names verbatim from spec §3.8: regatta.sign.identity,
  regatta.sign.rekor_uuid, regatta.sign.verified, regatta.sign.artifact_kind,
  regatta.sign.verify_micros, regatta.sign.failure_reason.
- rekor_uuid is EXACTLY 8 chars in the OTel attr (R7). FULL uuid in
  substrate audit event payload.
- artifact_kind enum: policy|pricing|plan|release. NOT bundle, NOT cert,
  NOT artifact. Lowercase.
- failure_reason enum: derived from the six sigstore sentinels. Map e.g.
  ErrIdentityMismatch → "identity_mismatch". Document the mapping in the
  package comment.
- KindSignVerified + KindSignFailed event kinds — declared via substrate's
  open-extension hook; ZERO new DDL. Verified via `git diff origin/main --
  '*.sql'` returning empty.
- Plan-as-code stub: Verifier interface + DefaultVerifier. P4 wires the
  consumer. T6 ships zero consumer code.
- Cross-file ONE-LINE edits: net diff in loader.go + pricing_verify.go
  combined ≤ 4 LoC. Confirm via `git diff origin/main -- internal/gates/authz/loader.go internal/cost/pricing/pricing_verify.go | grep '^+' | wc -l`.
- Doc-check passes: NO banned tokens in the four new .md files. The plan
  itself references feedback_doc_check_banned_phrases.md as authority for
  the list; if a token appears, sweep before push (per #297 + #296 fix
  cycles, plans that inline the list self-tripped — do NOT inline).
- Every relative .md link in the four new docs resolves to a file on disk
  (doc-check's link-integrity gate).
- ZERO new SQL migrations.
- No AI signatures; godocs ≤ 1 line.

# Hygiene

- NO AI signatures, doc-check + stale-todo exit 0, ```release-notes fence.

# Return format

PR URL, failing-test output ≥ 3 of 3, 3 followup numbers, reviewer verdict,
diff stat, A+ scorecard verbatim. Begin now.
```

---

## §8 Followup issue templates (pre-enumerated)

Per `feedback_unaddressed_load_bearing`: every load-bearing named-but-deferred item from
spec §1.2 (non-goals) + §10 (followups list) is filed as a `[w10-followup]` tracking issue
PRE-MERGE; the relevant PR body cites the issue number. The 13 templates below align with
spec §10's enumeration. Implementers file the deltas not already filed by prior tasks; track
issue numbers cross-PR.

| # | Title | Owner-task (files it) | Spec link |
| - | ----- | --------------------- | --------- |
| 1 | `[w10-followup] SLSA L4 build provenance` | T2 | §1.2 + §10 #1 |
| 2 | `[w10-followup] Hardware-key support (YubiKey / TPM / cloud-KMS)` | T1 | §1.2 + §10 #2 |
| 3 | `[w10-followup] Private TUF root (operator-managed)` | T1 | §1.2 + §10 #3 |
| 4 | `[w10-followup] Runtime attestation (in-toto over substrate event stream)` | T6 | §1.2 + §10 #4 |
| 5 | `[w10-followup] Operator-facing key-rotation UI` | T5 | §1.2 + §10 #5 |
| 6 | `[w10-followup] Rekor monitor + alert pipeline (R6 defense-in-depth)` | T6 | §5 R6 + §10 #6 |
| 7 | `[w10-followup] OCI registry as artifact store` | T2 | §1.2 + §10 #7 |
| 8 | `[w10-followup] sigstore-go v2 migration` | T1 | §5 R8 + §10 #8 |
| 9 | `[w10-followup] Multi-signature threshold (m-of-n trust roots)` | T3 | §10 #9 |
| 10 | `[w10-followup] Signature pinning by fingerprint (TOFU)` | T5 | §10 #10 |
| 11 | `[w10-followup] W10.2 — in-toto SLSA-3 agent-decision attestation` | T6 | §10 #11 |
| 12 | `[w10-followup] Air-gap deploy guide (Rekor tree head + TUF root sidecar)` | T6 | §5 R10 + §10 #12 |
| 13 | `[w10-followup] W8.2 — policy_required: true per-tenant default rollout` | T3 | §10 #13 |

**Cross-PR coordination:** each implementer greps existing issues (`gh issue list --label
w10-followup`) before filing to dedupe. PR body Followups section cites issue numbers; if
another implementer already filed the same template, that implementer's PR is the source of
truth + the second PR cites the existing number.

---

## §9 After Wave C — handoff to follow-up wedges (W10.2, P4)

Wave C exit gate (per spec §9 sequencing + spec §10 followups):
- All six PRs (T1–T6) merged to main.
- 13 followup tracking issues filed (one per spec §10 line item).
- Adversarial reviewer cleared every PR with zero unaddressed Risk-tier findings
  (per `feedback_review_before_automerge`).
- `bash scripts/doc-check.sh` + `bash scripts/stale-todo.sh` exit 0 on main.

**Roadmap pre-fetch** (per `feedback_roadmap_pre_fetch`):

- **W10.2 — in-toto SLSA-3 agent-decision attestation** (spec §1.2 + §10 #11): the
  regatta-specific differentiator called out in the MVP-3 brief. Signs the substrate event
  stream as the in-toto subject. Depends on W11 blackboard substrate-event subscription
  model. Pre-fetched into the autonomous-session-prompt next-horizon block.
- **P4 — plan-as-code consumer wire-up** (spec §3.6): consumes T6's `plan.DefaultVerifier`.
  P4 plan-author drops `.regatta/plans/*.yaml` parser + calls `DefaultVerifier.Verify`
  before YAML parse. ZERO refactor in W10's stub.

**autonomous-session-prompt refresh** (per `feedback_boot_prompt_per_wave_refresh`): after
Wave C merges, refresh `docs/engineer/autonomous-session-prompt.md` to drop W10 entries +
add W10.2 + P4 entries. Drop W10 from the active-wave block in the same commit that lands T6.
