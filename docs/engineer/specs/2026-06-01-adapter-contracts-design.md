---
title: "Adapter contracts for swap-out (P3.8)"
status: active
summary: "P3.8 swap-out adapter contracts: 5 adapters (OTel exporter, OPA RBAC, Sigstore signer, Stripe metered billing, LLM gateway) behind `internal/adapters/<name>/` `sql.Register`-style pattern. Trigger = first customer ask for hosted backend."
---

# Adapter Contracts for Swap-Out — Design Spec

**Date:** 2026-06-01
**Brief:** [2026-05-31-mvp-3-next-level.md](../briefs/2026-05-31-mvp-3-next-level.md) §5 thread #3 — "Adapter contracts for swap-out"
**Status:** Design (pre-implementation)
**Companion specs:** W6 OTel (2026-06-01-mvp3-otel-observability), W8 OPA-RBAC, W10 Sigstore, W12 Metered Billing, Cost-Governor (LLM gateway).

## Prior art adopted

| Adapter | Proven OSS / pattern reused | Why |
|---|---|---|
| OTel exporter | `go.opentelemetry.io/otel/sdk/trace.SpanExporter`, `otlptracehttp`, `stdouttrace` | OTel ships the swap interface verbatim; we just embrace it. |
| OPA RBAC | `github.com/open-policy-agent/opa/v1/sdk` + `rego.New().PrepareForEval()` for in-binary; OPA REST `POST /v1/data/{path}` for hosted | One library, two transports — the OPA project documents both. |
| Sigstore signer | `github.com/sigstore/sigstore-go/pkg/sign` (`NewEphemeralKeypair` ed25519, `NewFulcio`, `NewRekor`) | Sigstore-Go is the official client; ed25519 local-key is the documented dev path. |
| Stripe billing | `github.com/stripe/stripe-go/v85` + `rawrequest` against `/v2/billing/meter_events` | Stripe's meter-event API is the metered-billing primitive; raw-request supports the v2 endpoint until SDK first-class lands. |
| LLM gateway | `Helicone-*` proxy headers (`Helicone-Auth`, `Helicone-User-Id`, `Helicone-RateLimit-Policy`, `Helicone-Property-*`); Portkey virtual-key fallback | Helicone is a transparent base-URL swap — provider SDKs untouched. Headers documented per `wedges/cost-governor.md` prior-art table. |
| Registration | Go stdlib `sql.Register` / `image.RegisterFormat` / `http.Handle` | Widely-deployed global-registry pattern; zero DI framework. |
| Interface evolution | `io.ReadCloser` embedding + capability-detection via type assertion (cf. `http.Pusher`, `fs.ReadDirFS`) | Adds methods without breaking existing impls. |

The "swap option" rule per `feedback_research_design_principles` is satisfied at every seam: the in-binary default is the **escape hatch**, the hosted adapter is the **production path**, and both implement the same Go interface so an operator flips between them by editing a single `regatta.yaml` key.

---

## Common skeleton

Every adapter ships in `internal/adapters/<name>/` and exposes:

```
internal/adapters/<name>/
  iface.go        // Adapter interface (≤8 methods) + Capability constants
  registry.go     // Register(name, factory) + Open(name, cfg) — sql.Register style
  default/*.go    // In-binary default impl (no network, no creds)
  hosted/*.go     // Hosted-service adapter
  contract_test.go// Table-driven test BOTH impls must pass
  config.go       // YAML schema + env-var fallback + Validate()
```

Boot path (`cmd/regatta/serve.go`) calls `adapters.Open(name, cfg)` once per adapter at startup — no DI framework, no init-order surprises.

---

## §1 — OTel Exporter Adapter

### 1.1 Interface (`internal/adapters/otelexport/iface.go`)

```go
// SpanExporter is regatta's swap-seam for OTel trace export. It is a
// thin re-export of go.opentelemetry.io/otel/sdk/trace.SpanExporter so
// any third-party OTel exporter slots in without an adapter shim.
type SpanExporter = sdktrace.SpanExporter

// Factory builds an exporter from a parsed config block. Factories
// register themselves via init() — see registry.go.
type Factory func(ctx context.Context, cfg Config) (SpanExporter, error)

// Lifecycle is implemented by exporters that need pre-shutdown drain
// beyond SpanExporter.Shutdown (e.g. flush a local buffer to disk
// before the OTLP client tears down the HTTP/2 conn).
type Lifecycle interface {
    SpanExporter
    PreShutdown(ctx context.Context) error // capability detection via type assert
}
```

Interface surface = **2 methods** from `SpanExporter` (`ExportSpans`, `Shutdown`) + optional `PreShutdown` via capability detection. Under the cap.

### 1.2 Default in-binary impl

`default/stdout.go` wraps `go.opentelemetry.io/otel/exporters/stdout/stdouttrace`. Zero deps beyond OTel SDK already in `go.mod` (W6). Writes JSON spans to `stderr` when `otel.exporter=stdout` (default) or `otel.exporter=""` (unset). Dev-only — operator sees spans without running a collector.

### 1.3 Hosted adapter contract

`hosted/otlphttp.go` wraps `otlptracehttp.New(ctx, WithEndpointURL, WithHeaders, WithTLSClientConfig)`. Speaks OTLP/HTTP to any compliant collector (Jaeger, Tempo, Honeycomb, Datadog OTLP, vendor SaaS). Retry + backpressure provided by OTel SDK's `WithBatcher`.

### 1.4 Config schema (`regatta.yaml`)

```yaml
otel:
  exporter: stdout | otlp_http   # default: stdout
  endpoint: https://otlp.example.com:4318/v1/traces  # otlp_http only
  headers:
    Authorization: ${OTEL_AUTH_TOKEN}  # env-var interpolation
  tls:
    ca_file: /etc/regatta/otel-ca.pem
    insecure_skip_verify: false
  batch:
    max_queue_size: 2048
    max_export_batch_size: 512
    export_timeout: 30s
```

Env-var fallback: `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS` (OTel-standard envs — operators already know them).

### 1.5 Failure mode

**Fail-open.** If the OTLP endpoint is unreachable, the BatchSpanProcessor drops spans after `max_queue_size` overflow and emits a single `slog.Warn("otel: span queue overflow", count)` per drop window. **Rationale:** observability backbone failure must NEVER block the orchestrator critical path. Drops are themselves observable via the `otel.sdk.exporter.span.exported` self-metric.

### 1.6 Hot-swap

**No.** Exporter is owned by the global `TracerProvider` (`otel.SetTracerProvider`); swapping requires `Shutdown` + re-`Set`, which races with in-flight spans. Operator workflow: restart daemon to change exporters. Documented limitation.

### 1.7 Contract test

`contract_test.go` runs the same table against both impls:

```
TC1 ExportSpans_Success         — round-trips 100 spans; ExportSpans returns nil
TC2 ExportSpans_ContextCanceled — ctx canceled mid-export → ctx.Err()
TC3 Shutdown_Idempotent          — Shutdown twice → second call returns nil
TC4 Shutdown_AfterExport         — Export after Shutdown → ErrShutdown
TC5 ConcurrentExport             — 8 goroutines × 100 spans → no race (go test -race)
TC6 EmptyBatch                   — ExportSpans([]) returns nil
```

### 1.8 Security boundary

Creds via env-var ONLY (`OTEL_AUTH_TOKEN` interpolated into headers). YAML never holds secret values. Rotation hook: re-read env-var on each export via closure over `os.Getenv` is **not** acceptable (perf); instead, expose `SIGHUP` → re-build exporter — but per §1.6 hot-swap is no, so rotation requires restart. Documented.

---

## §2 — OPA RBAC Adapter

### 2.1 Interface (`internal/adapters/rbac/iface.go`)

```go
type Decision struct {
    Allow  bool
    Reason string         // human-readable, surfaced in audit log
    Policy string         // policy path, e.g. "regatta.rbac.allow"
    Trace  []string       // OPA decision-log trace for debug; nil in prod
}

type Authorizer interface {
    // Authorize evaluates input against the configured policy bundle.
    // Input is JSON-encodable; opaque to the interface.
    Authorize(ctx context.Context, input map[string]any) (Decision, error)

    // ReloadBundle hot-reloads the policy bundle. Returns the
    // SHA256 of the loaded bundle; callers stamp into audit log.
    ReloadBundle(ctx context.Context) (sha256 string, err error)

    // Close releases adapter resources.
    Close(ctx context.Context) error
}
```

**3 methods.** Under cap.

### 2.2 Default in-binary impl

`default/embedded.go` uses `github.com/open-policy-agent/opa/v1/rego` with `PrepareForEval` cached per bundle SHA. Bundle source: filesystem path under `.regatta/policies/*.rego`. Ships with a default `allow-all` policy (single rule `default allow := true`) so a fresh `regatta init` works without policy authoring. Operator opts into denial by editing the .rego file. **Why allow-all:** UX-first — friction blocks adoption. Documented loudly in `regatta init` output.

### 2.3 Hosted adapter contract

`hosted/opaapi.go` posts to `POST {url}/v1/data/{policy_path}` with `{"input": ...}` body, expects `{"result": <bool>}` per OPA REST spec. Bundle distribution is **OPA's responsibility** — operator runs OPA server with `--server` flag against their bundle source (Git, S3, OCI). regatta just queries decisions.

### 2.4 Config schema

```yaml
rbac:
  backend: embedded | http   # default: embedded
  embedded:
    bundle_dir: .regatta/policies  # rego files
    decision_log: false
  http:
    url: https://opa.internal.example.com:8181
    decision_path: /v1/data/regatta/rbac/allow
    timeout: 500ms
    token_env: OPA_AUTH_TOKEN   # bearer token via env-var
```

Env-var fallback: `REGATTA_RBAC_URL` (overrides yaml when set).

### 2.5 Failure mode

**Fail-closed.** Authorization is a security-class decision; "OPA unreachable → deny" is the only safe default. Operator can override via `rbac.on_unavailable: deny|allow` (default `deny`; allow only documented for dev with a warning emitted at boot). Maps to W8 spec.

### 2.6 Hot-swap

**Yes.** `ReloadBundle` is in-interface; embedded impl re-prepares Rego query atomically (CAS swap of `*rego.PreparedEvalQuery`), hosted impl re-fetches token. SIGHUP triggers reload. Useful for policy iteration without daemon restart.

### 2.7 Contract test

```
TC1 Authorize_Allow              — input matches → Decision.Allow=true
TC2 Authorize_Deny               — input doesn't match → Allow=false
TC3 Authorize_PolicyError        — malformed input → non-nil error, Allow=false
TC4 ReloadBundle_NewSHA          — bundle change → new SHA returned
TC5 ReloadBundle_SameSHA         — no change → same SHA, no-op
TC6 ConcurrentAuthorize          — 32 goroutines, race-free
TC7 BackendUnavailable_FailClosed — network err → Allow=false, Reason="opa unreachable"
TC8 Close_StopsBackground         — Close cancels reload goroutines
```

### 2.8 Security boundary

Embedded: bundle dir mode `0o700`, rejected if world-readable (boot check). Hosted: bearer token via env-var only; never logged. Rotation: SIGHUP re-reads `token_env`.

---

## §3 — Sigstore Signer Adapter

### 3.1 Interface (`internal/adapters/signer/iface.go`)

```go
type SignedBundle struct {
    Bytes      []byte // protobuf-encoded Sigstore bundle
    MediaType  string // application/vnd.dev.sigstore.bundle.v0.3+json
    DigestAlgo string // "sha256" | "sha512"
    Digest     []byte
}

type Signer interface {
    // Sign produces a Sigstore bundle over artifact bytes.
    Sign(ctx context.Context, artifact io.Reader) (*SignedBundle, error)

    // Verify checks a bundle against an expected digest.
    // Identity policy (cert SAN/issuer) is impl-internal.
    Verify(ctx context.Context, bundle *SignedBundle, expectedDigest []byte) error

    // KeyID returns a stable identifier (PEM fingerprint, OIDC SAN, etc.).
    KeyID() string

    // Close releases ephemeral keys / OIDC tokens.
    Close(ctx context.Context) error
}
```

**4 methods.** Under cap.

### 3.2 Default in-binary impl

`default/ed25519.go` uses `sign.NewEphemeralKeypair(&sign.EphemeralKeypairOptions{Algorithm: PKIX_ED25519})` from sigstore-go. Key persists for daemon lifetime. **No transparency log** — bundles signed with the local-key path are dev-grade; the `KeyID()` returned is the SHA256 of the PEM-encoded public key. Public key is exported on startup to `.regatta/keys/signer.pub` so verifiers downstream can pin it.

### 3.3 Hosted adapter contract

`hosted/fulcio.go` chains `sign.NewFulcio` (OIDC → short-lived cert) + `sign.NewRekor` (transparency log entry) per sigstore-go's documented Bundle flow. OIDC token sourced from the configured provider (GitHub Actions, Google, custom OIDC).

### 3.4 Config schema

```yaml
signer:
  backend: ed25519_local | sigstore   # default: ed25519_local
  ed25519:
    public_key_path: .regatta/keys/signer.pub
  sigstore:
    fulcio_url: https://fulcio.sigstore.dev
    rekor_url: https://rekor.sigstore.dev
    oidc:
      issuer: https://token.actions.githubusercontent.com
      token_env: SIGSTORE_OIDC_TOKEN
      audience: sigstore
    timeout: 30s
```

Env-var fallback: `SIGSTORE_OIDC_TOKEN` only — never key material itself.

### 3.5 Failure mode

**Fail-closed for sign, fail-open for verify-on-replay.**
- Sign failure → workitem transition aborts (we cannot ship an unsigned plan past W10's attestation gate).
- Verify failure on replay → workitem flagged `attestation_invalid`, replay proceeds with the flag (so operators can audit historical artifacts without losing the entire replay run).

### 3.6 Hot-swap

**No.** Key material is identity-shaping; swapping mid-run breaks the audit chain. Restart required. Documented in W10 spec.

### 3.7 Contract test

```
TC1 Sign_Verify_RoundTrip     — Sign then Verify with correct digest → nil
TC2 Verify_WrongDigest        — tampered artifact → ErrDigestMismatch
TC3 Verify_TamperedBundle     — flip one byte in bundle.Bytes → verify fails
TC4 KeyID_Stable               — two KeyID() calls return same string
TC5 Sign_LargeArtifact         — 100 MiB streaming reader → no OOM (bounded mem)
TC6 Sign_ContextCanceled       — ctx canceled → ctx.Err()
TC7 Close_NoLeak               — Close drops keypair (post-Close Sign → ErrClosed)
```

### 3.8 Security boundary

ed25519 default: private key never leaves process memory; never written to disk (per W10 threat model — local-key is for dev, not durable-trust). Sigstore default: OIDC token via env-var only; bundle path embeds the cert chain (public). Rotation: ed25519 → restart; Sigstore → next OIDC token refresh (handled by IdP), no daemon action.

---

## §4 — Stripe Metered Billing Adapter

### 4.1 Interface (`internal/adapters/billing/iface.go`)

```go
type UsageEvent struct {
    EventName  string            // e.g. "regatta.tokens.input"
    CustomerID string            // Stripe customer id OR tenant_id (impl maps)
    Value      int64             // integer units (tokens, USD-cents, count)
    Timestamp  time.Time
    Idempotency string           // dedup key (event_id) — required
    Metadata   map[string]string // copies to Stripe event payload
}

type Exporter interface {
    // Emit enqueues a usage event for the next flush. Non-blocking.
    Emit(ctx context.Context, e UsageEvent) error

    // Flush forces a batch send. Called on shutdown and on size/time triggers.
    Flush(ctx context.Context) error

    // Close drains the queue and stops background flusher.
    Close(ctx context.Context) error
}
```

**3 methods.** Under cap.

### 4.2 Default in-binary impl

`default/noop.go` is a no-op (`Emit` records to an in-memory ring buffer for `regatta usage export --csv` to read; `Flush` writes CSV to `.regatta/usage/YYYY-MM-DD.csv` on rotation). Operators without Stripe still get exportable usage data — UX-first per `feedback_research_design_principles`.

### 4.3 Hosted adapter contract

`hosted/stripe.go` uses `stripe-go/v85` + `rawrequest.Client` to `POST /v2/billing/meter_events` with `{"event_name", "payload": {"value", "stripe_customer_id"}, "identifier": <Idempotency>}`. Batch size 100 events, flush every 30s or on `Close`. Idempotency key prevents double-charge on retry per Stripe's documented semantics.

### 4.4 Config schema

```yaml
billing:
  backend: noop | stripe   # default: noop
  flush_interval: 30s
  batch_size: 100
  noop:
    csv_dir: .regatta/usage
  stripe:
    api_key_env: STRIPE_API_KEY
    customer_map:
      tenant_id_to_stripe_customer:
        acme: cus_abc123
        beta: cus_def456
    meter_events:
      tokens_input: regatta.tokens.input
      tokens_output: regatta.tokens.output
      usd_spend: regatta.usd_spend_cents
```

Env-var fallback: `STRIPE_API_KEY` (required when `backend: stripe`).

### 4.5 Failure mode

**Fail-open with persistent retry queue.** Billing failure must NEVER block agent runs (operators would rather miss billing for an hour than lose an entire DAG to a Stripe outage). Failed events spill to `.regatta/usage/_dead_letter/` for replay via `regatta usage replay`. Reconciled hourly against Anthropic Usage API per cost-governor wedge §"Reconciliation".

### 4.6 Hot-swap

**Yes.** Billing exporter is stateless across daemon restarts (events live in queue files, not the exporter). `regatta config reload` re-instantiates the adapter; in-flight queue files transfer cleanly.

### 4.7 Contract test

```
TC1 Emit_Flush_RoundTrip      — Emit 100 events, Flush → all delivered
TC2 Emit_Idempotency           — same Idempotency twice → single send to backend
TC3 Flush_PartialFailure       — 50/100 fail → 50 in dead-letter, no panic
TC4 Close_DrainsQueue          — Close blocks until queue empty or ctx canceled
TC5 ConcurrentEmit             — 16 goroutines × 100 emits → 1600 unique sent
TC6 BackendUnavailable_Spill   — network err → events written to dead-letter
TC7 LargeBatch                 — 10k events queued, batch_size respected
TC8 InvalidCustomerID_Reject   — empty CustomerID → ErrInvalidEvent (no send)
```

### 4.8 Security boundary

API key via env-var only; never logged (test gate: `grep -i 'sk_test\|sk_live' logs/ → 0 matches`). Customer-ID map: regard as PII (tenant_id leaks operator identity); restrict yaml file mode to `0o600`. Rotation: SIGHUP re-reads env-var.

---

## §5 — LLM Gateway Adapter

### 5.1 Interface (`internal/adapters/llmgateway/iface.go`)

This adapter sits **in front of** `program.ModelClient` (the existing planner interface) — it does not replace it. Instead it decorates outbound LLM HTTP calls with gateway routing (base-URL swap + headers). All four supported gateways (direct, Helicone, Portkey, LiteLLM) share a single header-injection surface.

```go
type Gateway interface {
    // RoundTrip mutates an outbound LLM HTTP request to route through
    // the gateway: sets BaseURL, injects auth + tracking headers,
    // attaches per-DAG virtual key when applicable.
    RoundTrip(req *http.Request, attrs Attrs) error

    // Name returns the gateway slug, e.g. "direct", "helicone".
    Name() string

    // Close releases gateway-side resources (idempotent).
    Close(ctx context.Context) error
}

type Attrs struct {
    TenantID   string            // for Helicone-Property-Tenant
    DAGNodeID  string            // for Helicone-Session-Path
    UserID     string            // for Helicone-User-Id (rate-limit key)
    RateLimit  string            // RFC-7237 policy string, e.g. "1000;w=3600;s=user"
    Properties map[string]string // custom Helicone-Property-* headers
}
```

**3 methods.** Under cap. Implements `http.RoundTripper`-adjacent shape so it composes with stdlib `http.Client.Transport`.

### 5.2 Default in-binary impl

`default/direct.go` is pass-through: leaves `req.URL` and Anthropic auth headers unchanged. **This is the current `provider_anthropic.go` shape** — no behavioral change when operator doesn't configure a gateway. Adopts what already works.

### 5.3 Hosted adapter contract

`hosted/helicone.go`: rewrites base URL to `anthropic.helicone.ai` (Anthropic-pass-through endpoint per Helicone docs), injects `Helicone-Auth: Bearer ${HELICONE_API_KEY}`, `Helicone-User-Id`, `Helicone-Session-Id`, `Helicone-Property-*` from `Attrs`. Supports the rate-limit-policy header for pre-call deny per cost-governor wedge.

`hosted/portkey.go`: rewrites to `api.portkey.ai/v1`, injects `x-portkey-api-key`, `x-portkey-virtual-key` (per-DAG short-lived key minted via Portkey Admin API — separate cron job, out of MVP-3 scope; MVP-3 ships static virtual-key only).

`hosted/litellm.go`: rewrites to operator-configured LiteLLM proxy URL; injects `Authorization: Bearer ${LITELLM_KEY}` + `x-litellm-tag: <DAGNodeID>` for budget-precedence attribution per LiteLLM docs.

### 5.4 Config schema

```yaml
llm_gateway:
  backend: direct | helicone | portkey | litellm   # default: direct
  helicone:
    base_url: https://anthropic.helicone.ai
    api_key_env: HELICONE_API_KEY
    rate_limit_policy: "1000;w=3600;s=user"
    properties:
      environment: production
  portkey:
    base_url: https://api.portkey.ai/v1
    api_key_env: PORTKEY_API_KEY
    virtual_key: pk-vk-acme-prod
  litellm:
    base_url: https://litellm.internal.example.com
    api_key_env: LITELLM_KEY
    tag_attribute: dag_node_id
```

Env-var fallback for each backend; `LLM_GATEWAY_BACKEND` overrides yaml entirely (useful for canary).

### 5.5 Failure mode

**Fail-open to direct.** If the configured gateway is unreachable, fall back to direct Anthropic with an `slog.Warn("llm_gateway: unreachable, falling back to direct", backend=...)`. **Rationale:** LLM gateway is an observability + cost-attribution overlay; agent work must continue. Cost-governor's hard-cap enforcement runs orthogonally inside regatta (per cost-governor wedge §"Pre-call deny on hard cap"); gateway-side deny is bonus, not the only line of defense.

### 5.6 Hot-swap

**Yes.** Gateway is per-request mutation of an HTTP request; the registry just hands out the configured `Gateway`. `regatta config reload` swaps the active impl atomically. Useful for cutover testing (canary 1% traffic to Portkey).

### 5.7 Contract test

```
TC1 RoundTrip_BaseURLRewrite   — req.URL.Host rewritten per backend
TC2 RoundTrip_AttrsToHeaders   — TenantID → expected header (per backend)
TC3 RoundTrip_NoAttrs           — empty Attrs → no spurious headers
TC4 RoundTrip_NoMutation_OnErr  — invalid Attrs → req unchanged + error
TC5 Close_Idempotent            — Close twice → second is nil
TC6 ConcurrentRoundTrip         — 64 goroutines, race-free
TC7 RateLimitHeader_Direct_None — direct backend ignores RateLimit
TC8 PropertyKeys_AllowList      — only `[a-zA-Z0-9_-]{1,64}` keys accepted (Helicone-spec)
```

### 5.8 Security boundary

API keys via env-var only. `Helicone-Auth` value never appears in spans (OTel SpanProcessor scrubs `Authorization|Helicone-Auth|x-portkey-*` headers — implemented as a redact-header allowlist in W6's exporter). Rotation: SIGHUP re-reads env-var; no daemon restart.

---

## §6 — Cross-Cutting Concerns

### 6.1 Registration pattern (`sql.Register` style)

Each adapter package exports `Register(name string, factory Factory) error` and `Open(name string, cfg Config) (Adapter, error)`. Backend packages call `Register` from `init()`. `cmd/regatta/serve.go` calls `Open` once per adapter at boot:

```go
import _ "github.com/trilamsr/regatta/internal/adapters/otelexport/default"
import _ "github.com/trilamsr/regatta/internal/adapters/otelexport/hosted"
// ...

exp, err := otelexport.Open(cfg.OTel.Exporter, cfg.OTel)
```

**Why this and not DI:**
- Zero external dep (matches `feedback_research_design_principles` — adopt proven OSS, here stdlib).
- Test isolation: tests call `Register(name, mockFactory)` directly; no DI graph to mock.
- Operator-debuggable: `regatta adapters list` walks the registry and prints all registered backends.

**Race protection:** the registry is a `sync.RWMutex`-guarded `map[string]Factory`; `Register` after `Open` panics (catches dev-mode double-registration). Tests use `t.Cleanup(func() { Unregister(name) })`.

### 6.2 Versioning (Go interface embedding + capability detection)

New methods land via **optional sub-interface**, NOT by mutating the core interface. Pattern (cf. `http.Pusher`, `fs.ReadDirFS`):

```go
type Authorizer interface { Authorize(...) ... }
type AuthorizerWithBatch interface {
    Authorizer
    AuthorizeBatch(ctx, []map[string]any) ([]Decision, error)
}

// Caller:
if b, ok := authz.(AuthorizerWithBatch); ok { return b.AuthorizeBatch(...) }
// fallback: loop Authorize(...)
```

Existing impls keep compiling; new impls opt in by implementing the sub-interface.

### 6.3 Lifecycle (`Open` / `Close` semantics)

- `Open(name, cfg)` constructs + dial-test (e.g. OPA `/health`, Stripe `GET /v1/balance` head). Failure to dial-test is fatal at boot — operator sees the error in serve startup logs, not 6 hours later when the first auth request hits.
- `Close(ctx)` is **idempotent**. Second call returns nil. Tests pin this.
- Shutdown order (orchestrator.Shutdown):
  1. Stop accepting new work (orchestrator)
  2. Drain in-flight LLM calls (LLM gateway last to close — billing+OTel need its data)
  3. Flush billing exporter
  4. Flush OTel exporter
  5. Close OPA + Signer (read-only, fast)

### 6.4 Observability (every adapter emits its own span)

Each `Open` returns an adapter wrapped by `adapters.instrument(name, impl)` which starts an OTel span around every interface method call. Span name format: `adapter.<name>.<method>`, e.g. `adapter.rbac.authorize`. Attributes: `adapter.backend` (e.g. "embedded"|"http"), `adapter.outcome` (ok|deny|err), and adapter-specific fields. Forward-compat with W6 — uses the W6 TracerProvider directly.

### 6.5 Grade rubric

| Tier | Criteria (all tool-checkable) |
|---|---|
| **B** | All 5 interfaces ship in `internal/adapters/<name>/`; default in-binary impl works (contract test green for default impl on each adapter); registration pattern wired into `serve.go`; `make check` clean. |
| **A** | B + hosted adapter impl green on contract test (same table) for at least 3 of 5 adapters (OTel, OPA, LLM gateway — minimum); SIGHUP reload works for the 3 hot-swap adapters; secret-leak test (`grep -i '<known-secret-prefixes>' logs/ → 0 matches`) green; redact-header allowlist exercised in W6 exporter. |
| **A+** | A + all 5 hosted adapters green on contract test; mutation-tested fail-mode policy (delete the fail-open `recover` and confirm test reds for the 3 fail-open adapters; delete the fail-closed deny path for OPA + Signer and confirm test reds); **swap-out demo working end-to-end**: a single `regatta.yaml` change flips OTel from stdout→OTLP, RBAC from embedded→http, billing from noop→stripe-test-mode, gateway from direct→helicone, signer from ed25519→sigstore — all without code change, e2e test pins this; OTel self-instrumentation (`otel.sdk.exporter.span.exported` ≥1) confirmed. |

### 6.6 Implementation plan (5 file-disjoint subagent tasks)

| Task | Scope | Est. LOC | Spawn order |
|---|---|---|---|
| T1 OTel adapter | `internal/adapters/otelexport/` + serve.go wiring | ~350 | First (W6 depends) |
| T2 OPA RBAC adapter | `internal/adapters/rbac/` + serve.go wiring + bundle dir bootstrap | ~500 | Second (W8 depends) |
| T3 Sigstore signer adapter | `internal/adapters/signer/` + serve.go wiring + key-export | ~400 | Parallel with T4/T5 |
| T4 Stripe billing adapter | `internal/adapters/billing/` + queue file format + serve.go wiring | ~600 (queue files heavy) | Parallel with T3/T5 |
| T5 LLM gateway adapter | `internal/adapters/llmgateway/` + provider_anthropic.go integration (composition, no rewrite) | ~450 | Parallel with T3/T4 |

**Shared primitive owner:** T1 owns `adapters/instrument()` (§6.4). T2-T5 import. Per `feedback_shared_primitive_owner` — name OWNER in dispatch.

**Per-task adversarial reviewer subagent required.** Per `feedback_agent_pr_review` — every implementer spawns reviewer on its own PR before automerge.

### 6.7 Adversarial red-team (8+ edge cases)

1. **Partial impl** — third-party implements `Authorizer` but always returns `Allow: true`. **Mitigation:** OPA bundle SHA stamped into audit log via `ReloadBundle` return; reviewer can spot "default bundle in production." Per W8 spec.
2. **Version mismatch** — old impl lacks `AuthorizerWithBatch`; new caller does `b.AuthorizeBatch`. **Mitigation:** capability-detect via type assertion + fallback loop. Documented §6.2.
3. **Panic recovery** — third-party Authorizer panics inside `Authorize`. **Mitigation:** `adapters.instrument()` wraps with `defer recover()` → returns `ErrPanic` + emits span event. Test: TC `*_PanicRecovered` per adapter.
4. **Deadlock on shutdown** — Billing `Flush` waits for queue, queue waits for HTTP, HTTP waits for ctx, ctx waits for shutdown. **Mitigation:** `Close(ctx)` MUST honor `ctx.Done()`; tests use `context.WithTimeout(2s)` and assert no leak. Shutdown order §6.3 puts billing flush before OTel close so billing's own spans don't race the exporter shutdown.
5. **Secret leak via error msg** — adapter wraps `os.Getenv("STRIPE_API_KEY")` into an error like `"auth failed: bad key sk_live_XYZ"`. **Mitigation:** central `adapters.maskErr(err)` redacts known secret prefixes (`sk_live_`, `sk_test_`, `Bearer `, `eyJ` JWT) before any error escapes the adapter; test gate: `grep -i '<prefix>' logs/ → 0`.
6. **Double-close** — caller calls `Close(ctx)` twice (e.g. boot failure + defer). **Mitigation:** every impl uses `sync.Once` around teardown; contract test TC `Close_Idempotent` per adapter pins.
7. **Registration race** — two `init()` registering the same name (e.g. fork-and-shadow attack on import path). **Mitigation:** `Register` returns an error on duplicate; `Open` panics if name unknown. Boot fails fast. Test: TC `Register_Duplicate_Errors`.
8. **Default impl drift from hosted** — operator tests against `embedded` OPA in dev, ships to `http` OPA in prod; subtle Rego semantic diff (e.g. v1 vs v0 syntax) yields different decisions. **Mitigation:** contract test table is **identical** for both impls; CI runs both; semantic drift surfaces as a contract-test red, not a prod outage. Per §6.5 A+ requires both green.
9. **Hot-swap mid-request** — SIGHUP reload while a `Authorize` call is in-flight; caller holds the old impl. **Mitigation:** registry's `Open` returns a stable pointer per `Open` call; reload publishes a new pointer atomically via `atomic.Pointer[Adapter]` — in-flight callers complete with the old impl; next call sees the new. Test: TC `Reload_InFlightCallSurvives`.
10. **Fail-open silently masks outage** — billing backend has been down for 6 hours; nobody noticed. **Mitigation:** every fail-open path emits a metric (`adapter.fail_open_total{adapter=...}`); operator dashboard (W7) alerts on rate >0 per adapter. Per cost-governor wedge §"Failure modes".
11. **Clock skew on Idempotency** — billing event Timestamp is local-clock, drifts past Stripe's idempotency window. **Mitigation:** Idempotency uses content hash (`sha256(event_name|customer|value|sequence)`), not timestamp. Stripe deduplicates on identifier, not time.
12. **Context-mode leak** — OTel exporter is constructed from `serve.Run`'s `ctx`, but `ctx` is canceled on shutdown before exporter drains. **Mitigation:** shutdown uses a derived `shutdownCtx` with its own 30s deadline, independent of the serve `ctx`. Documented §6.3.

---

## Cite-trail

- Brief §5 thread #3 (this spec's source of truth)
- `feedback_research_design_principles.md` — swap option rule, "adopt proven OSS"
- `feedback_grade_rubric.md` — B/A/A+ template
- `feedback_decision_priority.md` — UX > reference quality > best-practices ordering throughout
- `feedback_shared_primitive_owner.md` — T1 owns `instrument()`
- `feedback_agent_pr_review.md` — per-task reviewer subagent + A+ rubric
- `docs/wedges/cost-governor.md` — Helicone/Portkey/LiteLLM prior-art table
- `docs/superpowers/specs/2026-06-01-mvp3-otel-observability.md` — W6 TracerProvider wiring (peer spec)
- Go stdlib `database/sql.Register`, `image.RegisterFormat`, `net/http.Handle` — registration pattern authority
- sigstore-go `sign.NewEphemeralKeypair` / `sign.NewFulcio` / `sign.NewRekor`
- OPA `github.com/open-policy-agent/opa/v1/sdk` + REST `POST /v1/data/{path}`
- OTel `go.opentelemetry.io/otel/sdk/trace.SpanExporter`, `otlptracehttp.New`, `stdouttrace`
- Stripe `stripe-go/v85` + `rawrequest.Client` against `/v2/billing/meter_events`
- Helicone `anthropic.helicone.ai` + `Helicone-Auth` / `Helicone-User-Id` / `Helicone-RateLimit-Policy` / `Helicone-Property-*`
