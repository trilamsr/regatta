# MVP-3 W8 — OPA RBAC + multi-tenant + `policies` primitive (design spec, v1)

_Author: design subagent, 2026-06-01. Scope: roadmap wedge W8 (MVP-3 rank #3). Source-of-truth_:
- `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` §3.6.4 (Principal seam + W8 swap point).
- `docs/engineer/specs/2026-06-01-unified-substrate-design.md` §13 row #2 (deferred `policies` primitive — W8 owns it).
- `docs/engineer/plans/2026-06-01-substrate-w1-tasks.md` (T-S1 `AppendEvent` / `Fold` / `RegisterPayloadValidator` / sign+UNIQUE pattern to reuse).
- `docs/engineer/specs/2026-06-01-cost-governor-design.md` §3.6 (config-field convention; W8 mirrors).
- Memory: `feedback_research_design_principles` (OPA > reimpl), `feedback_grade_rubric`, `feedback_spec_pattern_authority`, `feedback_deletion_default`.

---

## 1. Goal + non-goal

### 1.1 Goal

Ship multi-tenant approval routing with policy-as-data. Every authorization decision (who may approve which gate; who may read which run; who may flip which budget) flows through one `Authorizer.Check(ctx, principal, action, resource)` call, evaluated by an embedded OPA Rego engine against a tenant-scoped policy bundle. Defense-in-depth on the existing approval-gates surface (HMAC cookie verifies _who_; OPA verifies _what they may do_).

### 1.2 Non-goal

- Hosted multi-tenant identity provider — deferred to P3.8 adapter wedges (Okta / Auth0 / Keycloak). v1 stays HMAC-token-bound; the cookie payload carries the tenant ID, no SSO/OIDC handshake.
- Per-policy UI editor — operators edit Rego files in their repo. Authoring tooling deferred.
- Policy bundle signing (sigstore / cosign on the `.tar.gz`) — followup. v1 trusts the bundle SHA-256 written via the substrate event.
- Dynamic policy reload via filesystem watcher or `/admin/reload` endpoint — followup. v1 reloads only on `policy_revision` substrate event (the canonical write path).
- OPA Wasm runtime — followup (faster eval; current spec uses the `rego` package interpreter).
- Custom policy DSL — explicitly rejected. OPA Rego is proven OSS (`feedback_research_design_principles`).

---

## 2. In / Out

### IN

1. **Tenant-scoped Principal**: extends the `Principal{ID, Tenant, Roles}` type already declared (but unpopulated) in `internal/web/auth.go` per W7 §3.6.4 / I4. W8 wires `Tenant` from the HMAC cookie payload; `Roles` populated by OPA after first policy eval.
2. **OPA-evaluated authz on every gated handler**: `/approve/{approval_id}` (GET + POST), `/approve/{approval_id}/diff`, `/runs/{run_id}` (DAG list), `/runs/{run_id}/cost`. One middleware layer hooked into `PrincipalFromRequest`'s caller — the W7 handler signature stays stable.
3. **Substrate `policies` primitive** (deferred from substrate v2 W1 per substrate spec §13 row #2). W8 ships:
   - new `EventKind = "policy_revision"` registered via `RegisterPayloadValidator` (T-S1 open-extension hook — substrate W1 task plan §"Cross-task seam contracts").
   - reducer = LWW over `(tenant_id, kind, bundle_sha256)` returning the **active** bundle per tenant.
   - read API: `policies.ActiveBundle(ctx, db, tenant)` returns `(bundleSHA, files map[string]string, err)`.
4. **`Authorizer` interface + concrete OPA impl** swapped into the W7 seam. W7's `PrincipalFromRequest` becomes `PrincipalAndAuthorizer(r)` returning `(Principal, Authorizer, error)`; W7 handlers gain one line: `if err := authz.Check(ctx, principal, "approval.decide", approval.ID); errors.Is(err, ErrDenied) { ... }`. The interface is born with two callers (web handler + CLI re-spawn for `regatta approve --decide`), so it does not violate the "no premature interface" rule from W7 R4.
5. **Default-deny baseline policy bundle** shipped via `embed.FS` at `regatta/v1/default/` (Rego files compiled into the binary). New tenants opt in by writing a `policy_revision` event with a tenant-specific bundle; until they do, every authorization call returns `ErrDenied` (except for the approval-self-vote-by-cookie-issuer path, which keeps existing behaviour).

### OUT

- Hosted IdP / SSO / OIDC (P3.8).
- Per-policy UI.
- Policy bundle signing (followup #1 — pre-enumerated below).
- Dynamic policy reload via filesystem watcher / admin endpoint (followup #2).
- Policy-test-as-code harness (`opa test` integration) — followup #3.
- OPA Wasm runtime (followup #4).
- OPA server / sidecar mode — explicit rejection: in-process `rego.New(...)` is cheaper to operate.

---

## 3. Architecture

### 3.1 `Authorizer` interface — born with two callers (no R4 violation)

```go
// internal/authz/authz.go (NEW)
package authz

type Action string

const (
    ActionApprovalView   Action = "approval.view"
    ActionApprovalDecide Action = "approval.decide"
    ActionRunView        Action = "run.view"
    ActionRunCostView    Action = "run.cost.view"
)

// Resource is the opaque target; e.g. an approval ULID, run ULID, or
// "tenant:<id>" for tenant-scoped ops. OPA inspects via input.resource.
type Resource string

// Decision is what OPA returns. Reason MUST be safe to surface in audit
// log + UI; it is the Rego policy's `reason` binding, not raw policy text.
type Decision struct {
    Allow         bool
    Reason        string
    PolicyRevision string  // bundle_sha256 the decision was rendered against
}

type Authorizer interface {
    Check(ctx context.Context, p web.Principal, a Action, r Resource) (Decision, error)
}

// Sentinels (typed errors — same shape as approval-gates ErrTokenReplay).
var (
    ErrDenied          = errors.New("authz: denied")
    ErrTenantUnknown   = errors.New("authz: tenant unknown")
    ErrPolicyMissing   = errors.New("authz: no policy bundle for tenant")
    ErrPolicyEvalError = errors.New("authz: opa eval error")
)
```

**Two callers at birth** (R4 deletion rationale closed):
- `internal/web/*` handlers (approval + runs + cost).
- `cmd/regatta/approval_decide.go` CLI path — same `Authorizer.Check` call before `approval.DecideTx` runs. Identity for CLI = `Principal{ID: $REGATTA_USER, Tenant: $REGATTA_TENANT, Roles: nil}`; OPA enforces.

ctx-bound principal: `web.WithPrincipal(ctx, p)` / `web.PrincipalFromContext(ctx)`. Authorizer reads via context so deep call paths (e.g. inside `approval.DecideTx`) can re-authorize without re-plumbing.

### 3.2 OPA embedding via `github.com/open-policy-agent/opa/rego`

Per `feedback_research_design_principles` — **adopt proven OSS, do NOT reimplement policy eval**. Rego's compiler, evaluator, and decision-log surface ship with OPA upstream; rebuilding any of that adds maintenance cost without UX gain.

- Import path: `github.com/open-policy-agent/opa/rego` (Apache-2.0).
- One `rego.PreparedEvalQuery` per `(tenant_id, action)`. Compile once at bundle load; reuse across requests.
- Query string: `data.regatta.v1.<action>.decision` returning `{allow: bool, reason: string}`.
- Eval input shape:
  ```json
  {
    "principal": {"id": "...", "tenant": "...", "roles": ["..."]},
    "action":   "approval.decide",
    "resource": "01H8...",
    "now_unix": 1717286400
  }
  ```
- Boot-time default bundle load via `//go:embed policies/regatta/v1/default` (Go `embed.FS`).
- Per-tenant bundles loaded from substrate `policies` primitive on demand + cached in an atomically-swapped store (§3.3).
- No HTTP egress to an OPA server; no sidecar process. In-process only.

Rejected alternatives (recorded for traceability):
- **Cedar** — smaller ecosystem; AWS-flavored; weaker debugging tools.
- **Casbin** — RBAC-only model; no first-class data-driven eval; weaker community than OPA.
- **Custom DSL** — `feedback_research_design_principles` forbids; OPA already solves it.

### 3.3 `policies` substrate primitive

W8 lands the substrate `policies` primitive deferred per substrate spec §13 row #2. **Reuses T-S1 pattern verbatim** (substrate W1 plan §"Cross-task seam contracts"): a new `EventKind`, a `RegisterPayloadValidator` registration in an `init()` block, signed events on the existing `substrate_events` table, fold via a new tenant-scoped helper (§3.3.2). **No new SQL table.**

**Migration #0007** (owned by W8 T2) is required to make T-S1's `substrate_events` schema accept policy bundles:
- Add `policy_revision` to the `substrate_events.kind` CHECK list (T-S1's `0006_substrate.sql:44-45` lists kinds explicitly; new kinds need a CHECK extension).
- Lift `payload_json` size cap to **1 MiB** for rows where `kind='policy_revision'` (T-S1's default cap is 1024 bytes per `0006_substrate.sql:46` — a single typical Rego file exceeds that, and spec §3.3.1's bundle cap is 1 MiB per R3).
- Add index `idx_substrate_events_tenant_kind ON substrate_events(tenant_id, kind, id DESC)` to support the tenant-scoped fold (§3.3.2) without a full table scan.

This is the **only** new migration W8 ships. T1, T3, T4, T5 add zero migrations.

Discovered by W8 T2 implementer (Lane ab284b) during initial dispatch: T-S1's `substrate.AllKinds()` enum in `internal/orchestrator/state/substrate/event.go` AND the SQL CHECK list AND the 1024-byte payload cap together gate policy-event ingest. Two alternatives considered and rejected (recorded in §4): (B) a parallel `policies` SQL table; (C) a CAS-blob payload split. Path A — one additive migration — is the smallest delta and the simplest reasoning path; it preserves the spec's "policies = event log, not a new table" axiom.

#### 3.3.1 Event kind + payload

`policy_revision` joins the existing T-S1 kind enum. Two synchronized edits land in migration #0007 + T-S1's Go-side enum (the **only** files outside `internal/authz/` T2 touches — lockstep edits, no drift):

| Layer | File | Edit |
|---|---|---|
| SQL CHECK | `internal/orchestrator/state/migrations/0007_w8_policy_revision.sql` (NEW) | Drop+recreate `substrate_events.kind` CHECK list with `policy_revision` appended; conditional 1 MiB payload cap for that kind; add `idx_substrate_events_tenant_kind` |
| Go enum | `internal/orchestrator/state/substrate/event.go` (MUTATE — additive only) | `KindPolicyRevision` const + `AllKinds()` slice extension |

The Go-side `policies.KindPolicyRevision` constant in `internal/authz/policies/payload.go` aliases `substrate.KindPolicyRevision` so the open-extension `RegisterPayloadValidator` hook from T-S1 still owns the validator wiring (no edit to `internal/orchestrator/state/substrate/validate.go`).

```go
// internal/authz/policies/payload.go
package policies

import substrate "github.com/trilamsr/regatta/internal/orchestrator/state/substrate"

const KindPolicyRevision substrate.EventKind = "policy_revision"

type PolicyRevisionPayload struct {
    BundleSHA256 string            `json:"bundle_sha256"` // hex; 64 chars
    RegoFiles    map[string]string `json:"rego_files"`    // path -> rego source; keys MUST be prefixed by "regatta/v1/<tenant>/"
    TenantID     string            `json:"tenant_id"`     // duplicates Event.TenantID for fold sanity
    WrittenBy    string            `json:"written_by"`    // principal who issued the revision (audit)
    Notes        string            `json:"notes,omitempty"` // free text; max 4 KiB
}
```

Registered from `init()` via `substrate.RegisterPayloadValidator(KindPolicyRevision, validatePolicyRevision)`. Validator checks:
- BundleSHA256 = `sha256(canonical-json(sort(RegoFiles)))[:64]`. Mismatch ⇒ `ErrPolicyBundleHashMismatch`.
- ≥ 1 Rego file. Empty bundle ⇒ `ErrPolicyBundleEmpty`.
- Per-file size cap 64 KiB; bundle total cap 1 MiB (R3 mitigation in §5).
- Rule count cap (counted via `opa.compile` AST walk) 1 000. (R3 mitigation.)
- Every file path starts with `regatta/v1/` and the second segment is either `default` or matches `^[a-z][a-z0-9_-]{1,62}$` (tenant ID grammar). Cross-tenant file paths ⇒ `ErrPolicyBundlePathInvalid`.
- All Rego files compile via `opa.compile` (boot-time + write-time both call the same path). Compile error ⇒ `ErrPolicyBundleCompileError` (carries OPA diagnostics).

#### 3.3.2 Reducer = LWW over `(tenant_id, kind, bundle_sha256)`

```go
// internal/authz/policies/fold.go
// Returns the most-recent (by written_at DESC, id DESC) bundle per (tenant_id).
// Aligns with substrate spec §4 default LWW reducer; no override needed.
func ActiveBundle(ctx context.Context, db state.DB, tenant string) (sha string, files map[string]string, err error)
```

`ActiveBundle` reads via a new tenant-scoped helper exported by substrate in W8 Wave A T2:

```go
// internal/orchestrator/state/substrate/fold.go (W8 Wave A T2 — NEW helper, additive to T-S1)
// FoldByTenant returns events filtered by (tenant_id, kind) using the
// idx_substrate_events_tenant_kind index added in migration #0007.
// Prepared statement; no direct SELECT escapes substrate.
func FoldByTenant(ctx context.Context, db state.DB, tenantID string, kind EventKind) ([]Event, error)
```

`FoldByTenant` is a **T-S1 followup addition shipped in W8 Wave A** (the existing `substrate.Fold(ctx, db, runID, kind)` from T-S1 #224 keys on `run_id`, which policy events do not carry — they key on `tenant_id`). The helper preserves T-S1's lint-substrate-queries invariant (no direct `SELECT` outside `internal/orchestrator/state/substrate/`).

Fold call sites:
- Boot: `Authorizer.Hydrate(ctx)` walks every tenant ID present in `substrate_events WHERE kind='policy_revision'`, calls `FoldByTenant(ctx, db, tenantID, KindPolicyRevision)`, loads each into the OPA store.
- Runtime: every successful `AppendEvent(KindPolicyRevision, ...)` triggers `Authorizer.Reload(ctx, tenant)` in the same transaction (post-commit callback); `Reload` calls `FoldByTenant` for the one affected tenant.

#### 3.3.3 Atomic OPA store swap on policy change

Race: a `policy_revision` arrives mid-request. Two acceptable behaviours; we pick the simpler:

```go
type opaStore struct {
    queries map[string]*rego.PreparedEvalQuery // key = "tenant/action"
}

type opaAuthorizer struct {
    store atomic.Pointer[opaStore]  // copy-on-write swap
    db    state.DB
}

// Reload(tenant) builds a NEW opaStore (deep-copy current + replace tenant slot),
// then store.Store(newStore). In-flight evals hold the old *opaStore by value
// of atomic.Pointer.Load() and complete against it. Memory is reclaimed via GC.
```

Justification for copy-on-write over RW-lock:
- Eval is the hot path (~1 µs OPA-side per check; we cannot afford `RLock` contention at p99).
- Reload is rare (one per operator-initiated revision).
- Pointer swap is a single uncontended store; GC reclaims old store transparently.
- Closes R4 (stale OPA store on policy change) and R8 (eval-path contention).

### 3.4 Principal extension + multi-tenant cookie binding

W7 v2 §3.6.4 / I4 already declared `Principal{ID, Tenant, Roles}` with `Tenant: "default"` populated for v1. W8 extends:

#### 3.4.1 HMAC cookie payload carries tenant ID

Existing approval-token payload (per `internal/canon`) has `Reviewer`, `ApprovalID`, `Window`, `JTI`. W8 adds:
- **`Tenant` field** to the signed payload (existing canonical-JSON signer in `internal/canon` already supports schema extension via versioned struct — adding a JSON field is wire-back-compat; old tokens without `Tenant` decode with `Tenant=""`).
- On decode, `Tenant=""` ⇒ `Principal.Tenant = "default"` (preserves single-tenant deployments per `feedback_deletion_default` — what got smaller: zero per-tenant bootstrap config required).

#### 3.4.2 Multi-tenant cookie path: `/approve/<tenant>/<approval_id>`

W7 v1 path is `/approve/<approval_id>`. W8 introduces the `<tenant>` segment:
- `GET /approve/<tenant>/<approval_id>` — render approval page; tenant binding enforced cookie-side AND URL-side (both MUST match the signed payload). Mismatch ⇒ `approval_error.tmpl` with new `tenant_mismatch` sentinel.
- Cookie scope tightens: `Path=/approve/<tenant>/<approval_id>` (was `Path=/approve/<approval_id>`). Defends against cross-tenant cookie leakage if a tenant somehow lands on another tenant's URL.
- Legacy URL `/approve/<approval_id>` is preserved with 301 redirect to `/approve/default/<approval_id>` for backwards compat during the rollout window. Removed in the W8.2 followup.

### 3.5 Policy bundle layout

```
internal/authz/policies/embedded/
├── regatta/
│   └── v1/
│       └── default/
│           ├── approval.rego          # default-deny baseline
│           ├── run.rego               # default-deny baseline
│           └── data.json              # optional static facts
└── (tenant bundles loaded from substrate at runtime; never on disk in this dir)
```

Default-deny baseline (`approval.rego`):

```rego
package regatta.v1

# Every action defaults to deny unless a tenant policy explicitly allows.
default approval.decide.decision := {"allow": false, "reason": "default-deny"}
default approval.view.decision   := {"allow": false, "reason": "default-deny"}
default run.view.decision        := {"allow": false, "reason": "default-deny"}
default run.cost.view.decision   := {"allow": false, "reason": "default-deny"}

# The ONE built-in exception: a reviewer holding a valid cookie-bound
# HMAC token for the SAME tenant can view + decide the approval whose
# ULID matches input.resource. This preserves the W7 single-tenant
# UX out-of-the-box; tenants override by writing their own bundle.
approval.decide.decision := {"allow": true, "reason": "hmac-reviewer"} if {
    input.principal.tenant == "default"
    input.principal.id != ""
}
```

Tenants opt in by writing a `policy_revision` event whose Rego files override `default approval.decide.decision := ...` etc. The `embed.FS` baseline ships with the binary and cannot be edited at runtime (defense against on-disk tamper).

### 3.6 Request flow

```
HTTP request
   │
   ▼
PrincipalFromRequest(r) ──► Principal{ID, Tenant, Roles=[]}  (W7 seam, unchanged signature)
   │
   ▼
authz.Check(ctx, principal, action, resource)
   │       │
   │       ├─► opaStore.queries["<tenant>/<action>"] (lookup, ~1 µs)
   │       │
   │       ├─► rego.Eval(input={principal, action, resource, now_unix})
   │       │     • cached PreparedEvalQuery; no compile on hot path
   │       │
   │       └─► Decision{Allow, Reason, PolicyRevision}
   │
   ▼
if !Decision.Allow: render approval_error.tmpl(sentinel="denied", reason=Decision.Reason)
                    + append substrate event KindAuthzDenied for audit
otherwise: proceed to handler body (approval.DecideTx, runs render, etc.)
```

**Audit trail**: every denial appends a `substrate_events` row with `kind="authz_denied"` carrying `{principal_id, tenant, action, resource, policy_revision, reason}`. Reuses substrate sign+UNIQUE+reducer. Operator can query the decision log post-hoc with the existing fold API.

### 3.7 OTel attributes

W8 reuses the W6 OTel attribute set (`docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md`). New attributes on every gated handler span:

| Attribute | Type | Cardinality | Notes |
|---|---|---|---|
| `regatta.authz.tenant` | string | bounded by deploy (≤ ~10³ tenants) | safe |
| `regatta.authz.action` | string | fixed enum (4 values v1) | safe |
| `regatta.authz.decision` | string | `"allow"` / `"deny"` | safe |
| `regatta.authz.reason` | string | bounded by Rego policy authors | clamp at 256 chars; lint Rego to keep reasons short |
| `regatta.authz.policy_revision` | string | bounded-by-deploy (one per tenant write) | bundle sha-256 prefix (8 chars) — closes cardinality risk R7 |
| `regatta.authz.eval_micros` | int | n/a | OPA eval latency for budget regression detection |

**Card-cap on `policy_revision`**: full SHA-256 hex would be high-cardinality if a tenant wrote many revisions — instead emit the 8-char prefix as the OTel attr and the full hex only in the substrate `kind="authz_denied"` audit row. (R7 mitigation.)

---

## 4. Existing patterns reused (deletion default)

Per `feedback_deletion_default` — every adoption MUST cite the existing pattern it reuses; no new primitive justified without one.

| Reused pattern | Source | What W8 adds |
|---|---|---|
| Substrate `AppendEvent` + `Fold` + `RegisterPayloadValidator` | T-S1 (substrate W1 plan, #224) | Registers `KindPolicyRevision` in an `init()`; reducer is default LWW (no override) |
| Substrate sign + UNIQUE (run_id, written_by, nonce) replay protection | T-S1 + substrate spec §2.1 | Policy events sign like any other event; replay-safe by construction |
| OPA Rego embedded as Go library | `github.com/open-policy-agent/opa/rego` (proven OSS) | One PreparedEvalQuery cache per (tenant, action) |
| W7 `Principal` type with `Tenant` field | W7 spec §3.6.4 / I4 | Populates `Tenant` from HMAC payload (W7 deliberately left empty) |
| W7 cookie HMAC + `internal/canon` token verify | W7 §3.6.1 + `internal/canon/approval_token*.go` | Adds optional `Tenant` field to signed payload; wire-back-compat |
| W6 OTel attribute set + span enrichment | W6 spec §3 | Adds 6 `regatta.authz.*` attributes; reuses existing tracer factory |
| Cost-gov `safety.cost` CUE config field convention | cost-gov spec §3.6 | Mirrors as `safety.authz.policy_dir` (optional, defaults to embed.FS default bundle) |
| Approval-gates typed-sentinel error pages | W7 §3.4 `approval_error.tmpl` | Adds `denied`, `tenant_mismatch`, `policy_missing`, `policy_eval_error` sentinels |

**What got smaller**:
- Default-deny baseline eliminates per-tenant bootstrap config for single-tenant deployments (the embed.FS `default` bundle covers them with zero operator action).
- OPA off-the-shelf eliminates the entire custom-DSL design + maintenance burden (no parser, no evaluator, no debugger to ship).
- `policies` lives on `substrate_events` rows — **no new table**. One additive migration (#0007) lifts the kind enum + payload cap + adds the tenant_id index; alternative shapes (B + C below) would have added a new table or a parallel CAS-blob primitive. Path A subtracts two alternative implementations at the cost of one migration row.
- W7 R4 deferred the `Authorizer` interface until a second caller existed. W8 lands with exactly two callers at birth (web handler + CLI), so the interface justifies itself.

**Rejected** (recorded):
- **Postgres Row-Level-Security pattern** (mentioned in brief): SQLite has no RLS; emulating at app layer duplicates OPA's job. Rejected.
- **Building a new `policies` SQL table** (substrate spec §13 prior shape — referred to as **path B** in the W8 T2 dispatch discovery): violates `feedback_deletion_default` when `substrate_events` already serves this need verbatim. Reducer over event log = policy table. Path A's migration #0007 makes the event-log path viable for ≤ 1 MiB bundles without splitting storage across two primitives.
- **CAS-blob payload split** (**path C** in the W8 T2 dispatch discovery): policy bundle bytes would live in `substrate_blobs` (also requires a migration), and the `substrate_events.payload_json` row would carry only the sha256 reference. Smallest payload-cap impact, biggest design surface — adds a second store + a join + a GC question (when does the blob become collectable). Deferred to followup if a future kind ships bundles > 1 MiB; not justified for v1 where the 1 MiB cap is comfortable headroom.

---

## 5. Risk register + mitigations

Severity tags: **S** = ship-blocker, **M** = mitigate-before-merge, **L** = monitor / followup OK.

| ID | Risk | Severity | Mitigation |
|---|---|---|---|
| **R1** | OPA eval latency spike on hot path | M | One `PreparedEvalQuery` per `(tenant, action)` cached in `opaStore`; compile happens at policy-revision write time, never on request. Budget: ≤ 200 µs p99 for `Check`. Bench in §6 A-tier. |
| **R2** | Policy bundle compile error blocks tenant entirely | M | Validator runs `opa.compile` at WRITE time (substrate AppendEvent rejects); bad bundle never reaches the store. Prior bundle remains active until valid revision replaces it (atomic swap). Followup #3 adds `opa test` harness. |
| **R3** | Policy bundle DoS via huge / pathological Rego | M | Hard caps in validator (§3.3.1): per-file 64 KiB; bundle 1 MiB; rule count 1 000. Reject at AppendEvent time. Tested in `policies/payload_test.go`. |
| **R4** | Stale OPA store after policy change | S | Atomic copy-on-write swap of `*opaStore` via `atomic.Pointer` (§3.3.3); reload is post-commit callback of the `policy_revision` AppendEvent transaction. No request sees a half-applied bundle. |
| **R5** | Cross-tenant cookie leakage (cookie set by tenant A surfaces on tenant B's path) | S | Cookie `Path=/approve/<tenant>/<approval_id>` (§3.4.2); HMAC payload carries `Tenant`; handler asserts `payload.Tenant == url.Tenant`. Mismatch ⇒ typed `tenant_mismatch` error page; no audit row leak. Property-tested in A+ tier (§6). |
| **R6** | Default-deny ambiguity ("does deny apply to existing single-tenant deployments?") | S | Default bundle explicitly allows `Principal{Tenant: "default", ID: nonempty}` for `approval.{view,decide}` (§3.5 Rego sample). Existing deployments keep working with zero operator action. Documented in §3.5 + B-tier rubric. |
| **R7** | OTel `policy_revision` cardinality blowup | M | Emit 8-char SHA prefix as OTel attr; full SHA only in audit-row payload (§3.7). Lint test asserts attribute value length ≤ 8. |
| **R8** | RW-lock contention on `opaStore` at high QPS | M | Copy-on-write `atomic.Pointer` (no lock on read path); reload allocates one new store, swaps the pointer. Closed by §3.3.3 design. |
| **R9** | CLI principal injection (operator forges `$REGATTA_USER`) | L | CLI principal trusted only for local dev; production CLI requires HMAC service token (followup — same shape as approval cookie). Documented + tracking issue. v1 surface is web; CLI multi-tenant defers safely. |
| **R10** | Policy author surfaces sensitive data in `reason` (e.g., "user not in group `acme-secret-clearance`") | L | Reason length clamp 256 chars; lint Rego files for `regex(/^(?:[a-z0-9_-]+\.){0,3}[a-z0-9_-]+$/, reason)` shape (allow-only-slugs heuristic). Soft warn at compile; hard fail in A+ tier. Followup-tracked. |
| **R11** | Default bundle drift between binary versions silently changes authz outcome | M | Default bundle's SHA-256 emitted at boot + logged + asserted in startup test (`TestDefaultBundleSHA256_Stable`). Any change to `embedded/regatta/v1/default/*.rego` must update the asserted constant — explicit change, no surprise drift. |
| **R12** | `policy_revision` event from a compromised principal locks legitimate tenant out | M | Writer principal recorded in `WrittenBy`; default bundle ships with `policy_revision` write requiring `roles: ["policy_admin"]` (encoded in Rego itself: bootstrap admin via `--bootstrap-policy-admin <principal>` flag, one-shot at first deploy). Subsequent revisions enforce via OPA. Followup tracking issue: emergency-roll-back CLI command (`regatta authz rollback --tenant <id> --revision <sha>`). |

**R1-R12 count: 12 risks**. All mitigated in-spec or followup-tracked.

---

## 6. Named test plan per task (B / A / A+ tiers)

Per `feedback_grade_rubric` — tool-checkable, distinct per tier. Implementer task slugs T1-T5 from §8.

### B — floor (ships)

Per task:

- **T1 (Authorizer impl + OPA embed)**:
  - `TestAuthorizerCheck_DefaultBundle_AllowsHMACReviewer` — single-tenant default-deploy smoke test.
  - `TestAuthorizerCheck_UnknownTenant_ReturnsErrTenantUnknown` — sentinel surfaces typed error.
  - `TestAuthorizerCheck_EmptyPrincipal_DefaultDenies` — fail-closed property.
  - `TestOpaStore_SwapIsAtomic` — two goroutines: one calls `Check` in a loop, one calls `Reload`; assert no panic + assert each `Check` returns a Decision rendered against exactly ONE bundle SHA (never a torn read).
- **T2 (`policies` substrate primitive)**:
  - `TestPolicyRevision_Append_ValidBundle_Succeeds` — happy path.
  - `TestPolicyRevision_Append_BundleHashMismatch_ReturnsErrPolicyBundleHashMismatch` — validator catches.
  - `TestPolicyRevision_Append_RuleCountCap_Rejects` — 1 001 rules rejected.
  - `TestPolicyRevision_Fold_ReturnsMostRecentBundle` — LWW reducer correctness.
- **T3 (Principal.Tenant wiring + cookie binding)**:
  - `TestPrincipalFromRequest_TenantFromCookiePayload` — happy path.
  - `TestApprovalHandler_TenantMismatch_RendersTypedError` — URL tenant ≠ payload tenant ⇒ `tenant_mismatch` page; zero audit row written.
  - `TestApprovalHandler_LegacyURLRedirectsToDefaultTenant` — backwards compat 301.
- **T4 (default-deny bundle + tenant onboarding)**:
  - `TestDefaultBundleSHA256_Stable` — constant asserts; bundle drift is intentional.
  - `TestTenantOnboardingFlow_FromEmptyToActive` — write `policy_revision` ⇒ `ActiveBundle` returns the written SHA ⇒ `Check` allows what the new bundle allows.
- **T5 (OTel + tests + docs)**:
  - `TestAuthzCheck_EmitsAllAttributes` — span has all 6 `regatta.authz.*` attrs present.
  - `TestAuthzCheck_PolicyRevisionAttr_Is8CharPrefix` — R7 cardinality guard.
  - `TestAuthzCheck_DeniedAuditEventAppended` — denial writes `substrate_events kind=authz_denied`.

### A — target (expected)

All B, plus:

- `BenchmarkAuthorizerCheck_p99Under200Micros` — N=10 000; histogram p99 ≤ 200 µs (R1 mitigation).
- `TestOpaStore_ReloadDuringEval_NoTorn` — fuzzy: 8 goroutines × 1 000 evals each, concurrent Reload; assert every Decision references a known bundle SHA.
- `TestPolicyRevision_OPACompileError_Rejects` — Rego with syntax error returns `ErrPolicyBundleCompileError` carrying OPA diagnostics.
- `TestApprovalDecide_PolicyDenies_CLIAndWebBothReturnDenied` — CLI + web behaviour parity (the two callers of Authorizer).
- `TestTenantOnboarding_CIDocFixture` — runs the onboarding tutorial in `docs/operator/authz-onboarding.md` as a script; asserts every step succeeds.

### A+ — stretch (aspirational)

All A, plus:

- **Property test** (`rapid`-based) — principal-tenant binding: random `Principal{Tenant: A}`, sign cookie, attempt to use against URL `/approve/<B>/...` for any B ≠ A; assert every attempt → `tenant_mismatch`. ≥ 5 000 cases.
- **Decision-log replay** — record all OPA decisions for one CI test run; replay against a future binary; assert every decision is byte-equal (default bundle stability across upgrades).
- **Policy bundle signing** — followup #1 implemented in A+ scope: `cosign sign` the policy bundle; AppendEvent validator verifies signature; rejected bundle path returns `ErrPolicyBundleSignatureInvalid`.

---

## 7. Grade rubric (verbatim)

### B — floor (ships)

- [ ] `Authorizer` interface + concrete OPA impl in `internal/authz/`; two callers wire it (web + CLI).
- [ ] `policies` substrate primitive — `KindPolicyRevision` registered via `RegisterPayloadValidator` from `init()`; no new migration; reducer = default LWW.
- [ ] `Principal.Tenant` populated from HMAC cookie payload; legacy `Tenant=""` payloads decode as `"default"`.
- [ ] Default-deny baseline bundle ships in `embed.FS` at `regatta/v1/default/`; SHA-256 asserted in test.
- [ ] Multi-tenant cookie path `/approve/<tenant>/<approval_id>` enforced; legacy URL 301-redirects to `default` tenant.
- [ ] OTel attrs `regatta.authz.{tenant,action,decision,reason,policy_revision,eval_micros}` present on every gated handler span; `policy_revision` clamped to 8-char SHA prefix.
- [ ] `make check` clean; every B-tier test in §6 passes.
- [ ] Single-tenant deployment requires zero operator config change (default bundle path).

### A — target (expected)

All B, plus:

- [ ] `BenchmarkAuthorizerCheck` p99 ≤ 200 µs at N=10 000 with default bundle.
- [ ] Atomic store swap test: 8 goroutines × 1 000 evals concurrent with reload; zero torn reads, zero panic.
- [ ] CLI principal path (`regatta approve --decide`) invokes the same Authorizer; CLI + web denial paths byte-equal.
- [ ] Tenant onboarding tutorial executable as a CI script (`make e2e-authz-onboarding`); zero manual steps.
- [ ] Adversarial reviewer subagent cleared the PR with zero unaddressed Risk-tier findings (per `feedback_agent_pr_review`).
- [ ] Tracking issues filed for every followup in §10; cited by number in PR body (per `feedback_unaddressed_load_bearing`).

### A+ — stretch (aspirational)

All A, plus:

- [ ] Property test (`rapid`) on principal-tenant binding ≥ 5 000 cases; zero `tenant_mismatch` false-negatives.
- [ ] OPA decision-log replay across binary versions — byte-equal for the default bundle.
- [ ] Policy bundle signing via `cosign` (followup #1 promoted to A+ scope); validator rejects unsigned bundle.
- [ ] Mutation-coverage ≥ 95% on `internal/authz/` (via `go-mutesting`).
- [ ] First-call OPA eval (cold path: PreparedEvalQuery compile) ≤ 5 ms p99 — guards against accidental hot-path compile.

---

## 8. File-disjoint impl decomposition (preview only)

Full plan PR comes after this spec lands. Preview only; **NOT a task breakdown for execution**. Five tasks, file-disjoint where possible.

| # | Task | Files touched | OWNER notes |
|---|---|---|---|
| **T1** | `Authorizer` interface + concrete OPA impl + atomic store swap + ctx-bound principal helpers | `internal/authz/authz.go`, `internal/authz/opa.go`, `internal/authz/store.go`, `internal/authz/ctx.go`, `internal/authz/*_test.go` | OWNS the interface; T3 + T5 import it |
| **T2** | `policies` substrate primitive — `KindPolicyRevision` registration + payload validator + fold API + bundle compile validator + migration #0007 (kind CHECK + payload cap + tenant_id index) + `substrate.FoldByTenant` helper | `internal/authz/policies/payload.go`, `internal/authz/policies/fold.go`, `internal/authz/policies/payload_test.go`, `internal/authz/policies/fold_test.go`, `internal/orchestrator/state/migrations/0007_w8_policy_revision.sql`, `internal/orchestrator/state/substrate/fold.go` (NEW — `FoldByTenant`), `internal/orchestrator/state/substrate/event.go` (MUTATE — `KindPolicyRevision` + `AllKinds()` extension) | Independent of T1; can dispatch in parallel. **OWNS migration #0007 + the `FoldByTenant` T-S1 followup** |
| **T3** | `Principal.Tenant` wiring + HMAC payload extension + cookie path tightening + `tenant_mismatch` sentinel + legacy redirect | `internal/web/auth.go`, `internal/web/approval.go` (handler signature additions only), `internal/canon/approval_token.go`, `internal/canon/approval_token_test.go`, `internal/web/auth_test.go`, `internal/web/templates/approval_error.tmpl` | Depends on T1 (imports Authorizer); file-disjoint from T2 |
| **T4** | Default-deny bundle (`embedded/regatta/v1/default/*.rego`) + tenant onboarding doc + CI fixture | `internal/authz/policies/embedded/regatta/v1/default/approval.rego`, `internal/authz/policies/embedded/regatta/v1/default/run.rego`, `internal/authz/policies/embedded/regatta/v1/default/data.json`, `docs/operator/authz-onboarding.md`, `tests/e2e/authz/onboarding_test.go` | Depends on T1 (boot loader); file-disjoint from T2 + T3 |
| **T5** | OTel attribute wiring + audit event (`kind=authz_denied`) + property tests + `make e2e-authz-onboarding` Makefile target + spec/A-tier benchmarks | `internal/authz/otel.go`, `internal/authz/audit.go`, `internal/authz/property_test.go`, `internal/authz/bench_test.go`, `Makefile`, `cmd/regatta/serve.go` (one-line authorizer-hydrate call) | Depends on T1 + T2; file-disjoint from T3 + T4 |

**Total: 5 file-disjoint tasks**. T1 + T2 dispatch in parallel; T3 + T4 + T5 dispatch in a second wave after T1 lands.

---

## 9. Sequencing

W8 lands **after** W7 Wave 1 (W7 tasks T4-T7 — the HTTP scaffold + `internal/web/auth.go` Principal type). W8 mutates the body of `PrincipalFromRequest` (W7 §3.6.4 explicit forward-compat seam) and adds the Authorizer wire-in; it does NOT re-architect W7.

**Substrate `policies` primitive can dispatch in parallel WITH W7 Wave 1** — T2 in §8 only touches `internal/authz/policies/*` and registers via the existing T-S1 `RegisterPayloadValidator` hook. File-disjoint from W7's web UI; can ship as soon as substrate W1 (T-S1 #224) merges.

Dependency graph:
```
substrate W1 T-S1 (#224, MERGED)
        │
        ├─► W8 T2 (policies primitive) ──┐
        │                                 │
W7 Wave 1 (T4-T7)                         ├─► W8 T1 (Authorizer impl)
        │                                 │
        └─► W7 Wave 2-3 (T8-T14)          ├─► W8 T3 (Principal.Tenant wiring)
                                          ├─► W8 T4 (default-deny bundle)
                                          └─► W8 T5 (OTel + audit + tests)
```

T2 may merge before T1; T1 boots `Authorizer.Hydrate` against whatever policy_revisions already exist (zero is the legal case → only embed.FS default bundle in store).

**Wave A T2 ships (additive to the substrate W1 surface):**
- Migration `0007_w8_policy_revision.sql` — adds `policy_revision` to `substrate_events.kind` CHECK list, lifts `payload_json` cap to 1 MiB for that kind, adds `idx_substrate_events_tenant_kind` to support the tenant-scoped fold.
- `substrate.FoldByTenant(ctx, db, tenantID, kind)` — T-S1 followup helper exported from `internal/orchestrator/state/substrate/fold.go`. Preserves the lint-substrate-queries invariant (no direct `SELECT` outside the substrate package); uses a prepared statement against the new tenant_id index.
- `substrate.KindPolicyRevision` const + `AllKinds()` slice extension in `internal/orchestrator/state/substrate/event.go`. The Go enum and the SQL CHECK move in lockstep — anywhere else, NO. T2 is the only task that touches files outside `internal/authz/`.

---

## 10. Deferred + followups (pre-enumerated)

Per `feedback_unaddressed_load_bearing` — file as gh issues, cite by number in PR body before merge.

1. **Policy bundle signing** (cosign / sigstore on the `RegoFiles` payload).
2. **Dynamic policy reload** via filesystem watcher OR admin endpoint (independent of substrate event path; for emergency operator override).
3. **Per-policy UI editor + Rego-aware playground** (depends on W7.4 UI scope expansion).
4. **OPA Wasm runtime** — replace interpreter with Wasm-compiled bundles for ~10× eval speedup.
5. **Policy-test-as-code harness** — `opa test` integration in `make check`; lints tenant bundles in CI.
6. **CLI service-token + multi-tenant CLI** — closes R9; mirrors the approval cookie HMAC shape for CLI principals.
7. **Emergency rollback CLI** — `regatta authz rollback --tenant <id> --revision <sha>` (R12 mitigation).
8. **Reason-string lint** — enforce slug-shape reasons (R10).
9. **W8.2 legacy-URL redirect removal** — drop `/approve/<approval_id>` 301-redirect after rollout window (clean-up).
10. **Postgres backend** — when SQLite gives way; OPA store stays in-process, but policies fold reads against Postgres `substrate_events`.

---

## 11. References

- W7 v2 spec: `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` §3.6.4 (Principal seam) + R4 (Authorizer deferral closed by W8 born-with-two-callers).
- Substrate v2 spec: `docs/engineer/specs/2026-06-01-unified-substrate-design.md` §13 row #2 (policies primitive deferred to W8) + §4 (LWW reducer default) + §5 (sign-with-nonce invariant) + §6 (lint-substrate-queries).
- Substrate W1 task plan: `docs/engineer/plans/2026-06-01-substrate-w1-tasks.md` §"Cross-task seam contracts" (T-S1 exports `AppendEvent`, `Fold`, `RegisterPayloadValidator`, `DefaultTenantID`).
- Cost-governor spec: `docs/engineer/specs/2026-06-01-cost-governor-design.md` §3.6 (CUE config field convention; W8 mirrors as `safety.authz.policy_dir`).
- W6 OTel backbone: `docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md` §3 (attribute set + tracer factory).
- OPA Rego Go library: https://pkg.go.dev/github.com/open-policy-agent/opa/rego (Apache-2.0).
- OPA bundles: https://www.openpolicyagent.org/docs/latest/management-bundles/.
- Memory: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_spec_pattern_authority`, `feedback_deletion_default`, `feedback_unaddressed_load_bearing`, `feedback_agent_pr_review`.
