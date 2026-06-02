# MVP-3 W8 — OPA RBAC + multi-tenant + `policies` primitive — Implementer task breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md` (#266 — merged).
Authority: `feedback_spec_pattern_authority` — implementer deviation from any spec-mandated pattern (Authorizer interface shape per §3.1; OPA Rego embed via `github.com/open-policy-agent/opa/rego` per §3.2; substrate `policies` primitive via existing `RegisterPayloadValidator` open-extension hook with ZERO new SQL migration per §3.3 + §3.3.1; copy-on-write `atomic.Pointer` store swap per §3.3.3 — NO RW-lock alternative; HMAC cookie payload extension via canonical-JSON forward-compat per §3.4.1; cookie path tightening `/approve/<tenant>/<approval_id>` per §3.4.2; default-deny baseline via `embed.FS` at `regatta/v1/default/` per §3.5; OTel attr set per §3.7 with 8-char SHA prefix for `policy_revision`; named tests B/A/A+ per §6) MUST re-spawn the design subagent. NO implementer-chosen alternatives.

Design priority for every decision below (`feedback_decision_priority`): **UX → ease of use → performance → best practices → execution speed → velocity**. Grade rubric (`feedback_grade_rubric`) inherited verbatim from spec §7 — each W8 task carries the spec's B / A / A+ tool-checkable criteria.

---

## Wave overview

- **5 file-disjoint implementer tasks** (T1, T2, T3, T4, T5) per spec §8.
- **Hard prereqs (must be merged to main before W8 dispatches):**
  - Substrate v2 Wave 1 — T-S1 (#224 `feat: substrate event log primitive` — MERGED). Exports `AppendEvent`, `Fold`, `RegisterPayloadValidator`, `DefaultTenantID`, `EventKind` from `internal/orchestrator/state/substrate/`. T2 registers `KindPolicyRevision` via the open-extension hook with no edit to T-S1's `validate.go`.
  - W7 Wave 1 T7 — `Principal{ID, Tenant, Roles}` type added to `internal/web/auth.go` with `Tenant: "default"` populated (plan dispatched separately under #268). **T3 is HARD-BLOCKED on T7 land** — T3 mutates the body of `PrincipalFromRequest` to read `Tenant` from the HMAC payload.
  - W6 OTel backbone — T1 (#172), T2 (#169), T3 (#209), T5 (#210) — all MERGED. T5 (OTel + audit) imports `cfg.Tracer` per W6 normalization.
- **Sequence vs parallel:**
  - **Wave A (parallel; dispatch simultaneously):** T1 (Authorizer impl), T2 (`policies` substrate primitive), T4 (default-deny bundle + onboarding doc + CI fixture). All three are file-disjoint per §1 below. T2 has zero W7 dependency and can dispatch the moment T-S1 (#224) is on main (already met). T4 depends on T1 only for the boot-loader API — T4 ships `embed.FS` + Rego files + the onboarding doc; the boot-loader wiring is a T1 export consumed at T1-merge time.
  - **Wave B (sequenced after W7 Wave 1 T7 lands):** T3 (Principal.Tenant wiring + cookie binding). T3 depends on the existence of `Principal.Tenant` field in `internal/web/auth.go` (W7 Wave 1 T7) + Authorizer interface (T1) for the new handler-side `authz.Check` calls.
  - **Wave C (sequenced last):** T5 (OTel attrs + audit event + property tests + operator doc + Makefile target). T5 depends on T1 (interface + Decision shape) + T2 (`KindPolicyRevision` constant) + T3 (handler middleware seam) + T4 (default bundle, for default-bundle stability test). T5 lands when T1-T4 are all on main.
  - Per `feedback_dispatch_strategy`: Wave A peaks at **3 parallel implementers** (T1, T2, T4) — well within the 10-lane cap and the 3-4 concurrency-cap heuristic. T3 + T5 each dispatch solo behind their dependency fences.
- **Migration phasing (`feedback_migration_number_lock`):** **Migration #0007 owned by T2.** Lifts `substrate_events.kind` CHECK list to include `policy_revision`, lifts `payload_json` size cap to 1 MiB for that kind, adds `idx_substrate_events_tenant_kind(tenant_id, kind, id DESC)`. T1 + T3 + T4 + T5 add **ZERO** new migrations. Discovered by W8 T2 implementer (Lane ab284b) during initial dispatch — T-S1's CHECK list at `migrations/0006_substrate.sql:44-45` excludes `policy_revision` and the 1024-byte `payload_json` cap at `0006_substrate.sql:46` is too small for a single Rego file. Spec §3.3 records alternatives B (parallel `policies` SQL table) and C (CAS-blob payload split) as explicitly rejected; path A — one additive migration — is the smallest delta. T2 also adds an `init()`-block validator dispatch via T-S1's `substrate.RegisterPayloadValidator` open-extension hook (additive; T-S1's `validate.go` is **not** modified — but `substrate/event.go`'s `AllKinds()` and the new `substrate/fold.go` are touched by T2 in lockstep with migration #0007).
- **Concurrency cap (`feedback_session_limit_dispatch`):** Wave A = 3 parallel implementers (T1, T2, T4). Below the 10-lane cap; well within the 3-4 heuristic ceiling. T3 + T5 sequenced solo. Zero risk of session-limit cascade.
- **Deletion default (`feedback_deletion_default`):** every PR body MUST cite a concrete shrinkage. Pre-enumerated below per task; carried verbatim in PR body skeletons §2-§6:
  - **T1:** Authorizer interface born with TWO callers (web handler + CLI re-spawn for `regatta approve --decide` per spec §3.1). Closes the W7 R4 deferral — the interface justifies itself on day one, no premature-interface debt.
  - **T2:** `policies` primitive ships via existing `substrate_events` rows via `RegisterPayloadValidator` open-extension — ZERO new SQL table, ZERO new reducer (default LWW over `(tenant_id, kind, bundle_sha256)` per spec §3.3.2). **ONE additive migration (#0007)** lifts the kind CHECK + payload cap + adds the tenant_id index — subtracts two alternative implementations (path B = parallel `policies` SQL table; path C = CAS-blob payload split — both rejected in spec §4). Migration #0007's three-line CHECK extension + one-line index + one-line cap lift is materially smaller than a new table's CREATE + indexes + reducer override OR a CAS-blob primitive's blob store + GC question. Net delta: +1 migration, -2 alternative storage primitives.
  - **T3:** wire-back-compat HMAC payload extension — legacy tokens with `Tenant=""` decode as `Tenant="default"`. Existing single-tenant deployments require **zero operator config change** for the rollout window (spec §3.4.1 + §3.5 default-deny baseline's HMAC-reviewer exception).
  - **T4:** `embed.FS` default-deny baseline ships in the binary. New single-tenant deployments need **zero per-tenant bootstrap config**. OPA off-the-shelf eliminates the entire custom-DSL design + maintenance burden (no parser, no evaluator, no debugger to ship — per spec §3.2 rejected-alternative recording).
  - **T5:** OTel `policy_revision` attribute clamped to 8-char SHA prefix — closes R7 cardinality blow-up without a separate "cardinality regression alarm" event kind. Reuses the W6 attribute infra; no new tracer factory.
- **Followup filing (`feedback_unaddressed_load_bearing` + `feedback_followup_filing_universal`):** every load-bearing deferred item in spec §10 is filed as a `[w8-opa-rbac-followup]` issue PRE-MERGE; PR bodies cite the numbers. The full §7 followup template list (§7 of this plan) pre-enumerates all 10 issues — see §7 below.

---

## §1 File-disjoint table

| Task | Path (exclusive write scope) | Depends-on (W8 + main) | Effort | TDD tests (count: named) |
| ---- | ---------------------------- | ---------------------- | ------ | ------------------------ |
| **T1** | `internal/authz/authz.go` (NEW; interface + Action/Resource/Decision/sentinels per spec §3.1); `internal/authz/opa.go` (NEW; concrete `opaAuthorizer` + `Check` + `Hydrate` + `Reload` + `PreparedEvalQuery` cache per spec §3.2); `internal/authz/store.go` (NEW; `opaStore` + `atomic.Pointer` copy-on-write swap per spec §3.3.3); `internal/authz/ctx.go` (NEW; `WithPrincipal` / `PrincipalFromContext` ctx-binding helpers per spec §3.1); `internal/authz/*_test.go` (B-tier 4 + A-tier 2 named tests below) | substrate W1 T-S1 (#224, MERGED); `github.com/open-policy-agent/opa/rego` (new go.mod dep — Apache-2.0) | M | 7 named (B 4, A 2, perf-gate 1). Spec §6 T1 + §7 B/A. |
| **T2** | `internal/authz/policies/payload.go` (NEW; `KindPolicyRevision` alias + `PolicyRevisionPayload` struct + `validatePolicyRevision` + `init()` registration via `substrate.RegisterPayloadValidator` per spec §3.3.1); `internal/authz/policies/fold.go` (NEW; `ActiveBundle(ctx, db, tenant) (sha, files, err)` wrapping `substrate.FoldByTenant` per spec §3.3.2); `internal/authz/policies/compile.go` (NEW; `opa.compile` wrapper for write-time validation per spec §3.3.1); `internal/authz/policies/payload_test.go` + `fold_test.go` + `compile_test.go`; **`internal/orchestrator/state/migrations/0007_w8_policy_revision.sql` (NEW; kind CHECK extension + 1 MiB payload cap for `policy_revision` + `idx_substrate_events_tenant_kind`)**; **`internal/orchestrator/state/substrate/fold.go` (NEW; `FoldByTenant(ctx, db, tenantID, kind)` helper — T-S1 followup; prepared statement against the new tenant_id index)**; **`internal/orchestrator/state/substrate/event.go` (MUTATE; ADD `KindPolicyRevision` const + extend `AllKinds()` slice — additive only, in lockstep with migration #0007's CHECK list)** | substrate W1 T-S1 (#224, MERGED) | M | 6 named (B 4, A 2). Spec §6 T2 + §7 B/A. |
| **T3** | `internal/web/auth.go` (MUTATE — populate `Principal.Tenant` from HMAC payload + add `authz.Check` middleware call inside `PrincipalFromRequest` caller); `internal/web/approval.go` (MUTATE — add `tenant` URL segment routing + `tenant_mismatch` sentinel page + legacy `/approve/<approval_id>` 301 redirect); `internal/canon/approval_token.go` (MUTATE — extend `ApprovalTokenPayload` with optional `Tenant` field; legacy `Tenant=""` decodes as `"default"`); `internal/canon/approval_token_test.go` (MUTATE — add wire-back-compat decode test); `internal/web/auth_test.go` (NEW); `internal/web/templates/approval_error.tmpl` (MUTATE — add `tenant_mismatch` sentinel branch) | W7 Wave 1 T7 (`Principal{ID, Tenant, Roles}` type landed; plan #268); T1 (Authorizer interface) | M | 5 named (B 3, A 2). Spec §6 T3 + §7 B/A. |
| **T4** | `internal/authz/policies/embedded/regatta/v1/default/approval.rego` (NEW; default-deny baseline + HMAC-reviewer exception per spec §3.5); `internal/authz/policies/embedded/regatta/v1/default/run.rego` (NEW; default-deny for run.view + run.cost.view per spec §3.5); `internal/authz/policies/embedded/regatta/v1/default/data.json` (NEW; optional static facts placeholder); `internal/authz/policies/embedded/embed.go` (NEW; `//go:embed regatta/v1/default` `embed.FS` export); `internal/authz/policies/embedded/embed_test.go` (NEW; bundle SHA stability + onboarding flow tests); `docs/operator/rbac-onboarding.md` (NEW; tenant onboarding tutorial); `tests/e2e/authz/onboarding_test.go` (NEW; CI-executable onboarding script) | T1 (boot loader API); T2 (`KindPolicyRevision` const for the onboarding flow test) | S | 4 named (B 2, A 2). Spec §6 T4 + §7 B/A. |
| **T5** | `internal/authz/otel.go` (NEW; `regatta.authz.*` attribute setter on the Check span per spec §3.7); `internal/authz/audit.go` (NEW; `KindAuthzDenied` substrate event emission on every deny per spec §3.6 + new constant registered via T-S1's `RegisterPayloadValidator`); `internal/authz/property_test.go` (NEW; `pgregory.net/rapid` ≥ 5 000-case principal-tenant binding property test per spec §6 A+); `internal/authz/bench_test.go` (NEW; `BenchmarkAuthorizerCheck` p99 ≤ 200 µs per spec §6 A + §7 A); `Makefile` (MUTATE — add `e2e-authz-onboarding` target running T4's tutorial); `cmd/regatta/serve.go` (MUTATE — one-line `authz.NewOPAAuthorizer(...).Hydrate(ctx)` call at serve startup per spec §3.6); `docs/operator/rbac.md` (NEW; operator-facing RBAC doc per spec §3.7 + §10 references) | T1 + T2 + T3 + T4 all MERGED | M | 7 named (B 3, A 2, A+ 2). Spec §6 T5 + §7 B/A/A+. |

**Disjointness verification (`grep` at plan time):**

- T1 writes only to `internal/authz/{authz,opa,store,ctx}*.go`.
- T2 writes only to `internal/authz/policies/{payload,fold,compile}*.go` + `internal/orchestrator/state/migrations/0007_w8_policy_revision.sql` + `internal/orchestrator/state/substrate/fold.go` (NEW) + `internal/orchestrator/state/substrate/event.go` (MUTATE — additive only).
- T3 writes only to `internal/web/{auth,approval}*.go`, `internal/canon/approval_token*.go`, `internal/web/templates/approval_error.tmpl`.
- T4 writes only to `internal/authz/policies/embedded/**` + `docs/operator/rbac-onboarding.md` + `tests/e2e/authz/onboarding_test.go`.
- T5 writes only to `internal/authz/{otel,audit,property_test,bench_test}.go` + `Makefile` + `cmd/regatta/serve.go` (one-line addition) + `docs/operator/rbac.md`.
- T1 + T2: `internal/authz/` (T1) vs `internal/authz/policies/` (T2) — distinct subdirectories. Zero file overlap. T2 registers `KindPolicyRevision` via T-S1's `RegisterPayloadValidator` open-extension hook from its own `init()`; substrate's `validate.go` is NOT modified.
- T1 + T4: `internal/authz/` (T1) vs `internal/authz/policies/embedded/` (T4) — distinct subdirectories. Zero file overlap. T4 calls T1's exported `Authorizer.Hydrate` from its boot-time test only.
- T2 + T4: both under `internal/authz/policies/`; T2 owns `payload.go` / `fold.go` / `compile.go`; T4 owns `embedded/**` (a subdirectory). Zero file overlap.
- T3 + everyone: T3 lives in `internal/web/` + `internal/canon/`; zero overlap with `internal/authz/**`.
- T5 + T1: both under `internal/authz/`; T5 owns `otel.go` / `audit.go` / `property_test.go` / `bench_test.go`; T1 owns `authz.go` / `opa.go` / `store.go` / `ctx.go`. Zero file overlap. T5 imports the T1 interface; T5 does NOT modify any T1 file.
- T5 + T2: T5 owns `audit.go` which registers a NEW substrate event kind (`KindAuthzDenied`) via T-S1's `RegisterPayloadValidator` from its own `init()`. T5 does NOT modify T2's `payload.go`.

## Cross-task seam contracts (load-bearing — implementer MUST honour exactly)

- **T1 exports:**
  - Types: `authz.Action` (string), `authz.Resource` (string), `authz.Decision{Allow, Reason, PolicyRevision}`, `authz.Authorizer` (interface with `Check(ctx, p web.Principal, a Action, r Resource) (Decision, error)`).
  - Action constants: `authz.ActionApprovalView`, `authz.ActionApprovalDecide`, `authz.ActionRunView`, `authz.ActionRunCostView` (spec §3.1 verbatim — 4 values).
  - Sentinels: `authz.ErrDenied`, `authz.ErrTenantUnknown`, `authz.ErrPolicyMissing`, `authz.ErrPolicyEvalError`.
  - Constructor: `authz.NewOPAAuthorizer(cfg Config) *opaAuthorizer` + `(*opaAuthorizer).Hydrate(ctx) error` + `(*opaAuthorizer).Reload(ctx, tenant) error`.
  - Ctx helpers: `authz.WithPrincipal(ctx, p) context.Context` + `authz.PrincipalFromContext(ctx) (web.Principal, bool)`.
  - T3 imports `authz.Action*` + `authz.Authorizer` + `authz.WithPrincipal`. T4 imports `authz.NewOPAAuthorizer` for its boot test. T5 imports `authz.Authorizer` + `authz.Decision` + the sentinels.
- **T2 exports:**
  - Constant: `policies.KindPolicyRevision substrate.EventKind = "policy_revision"`.
  - Types: `policies.PolicyRevisionPayload` (6 fields per spec §3.3.1 verbatim — `BundleSHA256`, `RegoFiles`, `TenantID`, `WrittenBy`, `Notes` + JSON tags).
  - Errors: `policies.ErrPolicyBundleHashMismatch`, `policies.ErrPolicyBundleEmpty`, `policies.ErrPolicyBundlePathInvalid`, `policies.ErrPolicyBundleCompileError`.
  - API: `policies.ActiveBundle(ctx, db state.DB, tenant string) (sha string, files map[string]string, err error)`.
  - T1 imports `policies.ActiveBundle` for `Hydrate` + `Reload`. T4 imports `policies.KindPolicyRevision` for its onboarding-flow test. T5 imports `policies.KindPolicyRevision` for the policy-revision OTel attribute extraction.
- **T2 substrate registration:** T2 ships an `init()` block in `payload.go` that calls `substrate.RegisterPayloadValidator(KindPolicyRevision, validatePolicyRevision)` per T-S1 #224's open-extension contract. T2 does NOT modify `internal/orchestrator/state/substrate/validate.go`. T2 DOES modify `internal/orchestrator/state/substrate/event.go` (additively — `KindPolicyRevision` const + `AllKinds()` slice extension; lockstep with migration #0007's CHECK list) AND adds `internal/orchestrator/state/substrate/fold.go` (NEW — `FoldByTenant` T-S1 followup helper). The validator is hardcoded LWW reducer per substrate spec §4 default — no override needed.
- **T2 substrate exports (additive to T-S1):** `substrate.KindPolicyRevision` const, `substrate.FoldByTenant(ctx, db, tenantID, kind) ([]Event, error)`. The Go-side `policies.KindPolicyRevision` aliases `substrate.KindPolicyRevision` so the open-extension validator wiring stays in `internal/authz/policies/`. `FoldByTenant` uses a prepared statement against the new `idx_substrate_events_tenant_kind` index — direct `SELECT` outside the substrate package remains forbidden by the lint-substrate-queries gate.
- **T3 ↔ T1 wire-in:** T3 mutates the body of `internal/web/auth.go::PrincipalFromRequest` to (a) read `Tenant` from the HMAC payload via T3's extended `internal/canon/approval_token.go` and (b) attach the `Principal` to the request ctx via `authz.WithPrincipal(ctx, p)`. The signature of `PrincipalFromRequest` stays stable (W7 §3.6.4 forward-compat seam). T3 calls `authz.Authorizer.Check` from the gated handlers (`internal/web/approval.go`).
- **T3 ↔ canon:** `internal/canon/approval_token.go` extends `ApprovalTokenPayload` with `Tenant string \`json:"tenant,omitempty"\`` (zero-value omitempty preserves canonical-JSON forward-compat per spec §3.4.1 — legacy tokens without the field round-trip identically). Decode-side fills `Principal.Tenant = "default"` when `Tenant == ""`.
- **T4 default bundle SHA constant:** T4 exports `policies.embedded.DefaultBundleSHA256 string` (computed at package-init time over the canonical-JSON of the embedded Rego files). T5 imports this constant for `TestDefaultBundleSHA256_Stable` (spec §6 B-tier).
- **T5 substrate registration (audit event kind):** T5 ships an `init()` block in `audit.go` that registers `KindAuthzDenied substrate.EventKind = "authz_denied"` via `substrate.RegisterPayloadValidator(KindAuthzDenied, validateAuthzDenied)`. T5 does NOT modify T-S1's `validate.go`. Audit-event payload shape per spec §3.6 (`{principal_id, tenant, action, resource, policy_revision, reason}`).
- **T5 OTel attribute set (verbatim spec §3.7):** `regatta.authz.tenant`, `regatta.authz.action`, `regatta.authz.decision`, `regatta.authz.reason`, `regatta.authz.policy_revision`, `regatta.authz.eval_micros`. `policy_revision` attribute MUST be the 8-char SHA prefix; T5 imports `policies.PolicyRevisionPayload.BundleSHA256` and slices `[:8]`.
- **Shared-primitive owner (`feedback_shared_primitive_owner`):** T2 OWNS the `policies` substrate primitive (KindPolicyRevision + payload validator + fold). T1 + T4 + T5 import T2's exports — no parallel registration, no parallel payload struct. T5 OWNS the `KindAuthzDenied` audit event kind (a separate primitive, dedicated to T5's audit-write path). No task other than T5 registers `KindAuthzDenied`.

---

## §2 Task T1 — Authorizer interface + OPA Rego embed + atomic store swap

### Scope
- **`internal/authz/authz.go`** — NEW. Per spec §3.1 verbatim:
  - `Action` string type + 4 constants (`ActionApprovalView`, `ActionApprovalDecide`, `ActionRunView`, `ActionRunCostView`).
  - `Resource` string type.
  - `Decision` struct (`Allow bool`, `Reason string`, `PolicyRevision string` — 8-char SHA prefix; full SHA in audit row per §3.7 R7).
  - `Authorizer` interface — `Check(ctx context.Context, p web.Principal, a Action, r Resource) (Decision, error)`.
  - Sentinels: `ErrDenied`, `ErrTenantUnknown`, `ErrPolicyMissing`, `ErrPolicyEvalError`.
- **`internal/authz/opa.go`** — NEW. Concrete `opaAuthorizer`:
  - Struct fields: `store atomic.Pointer[opaStore]`, `db state.DB`, `defaultBundle embed.FS` (from T4's exported `embedded.FS`), `tracer trace.Tracer`.
  - `NewOPAAuthorizer(cfg Config) *opaAuthorizer` constructor.
  - `Hydrate(ctx) error` — walks `SELECT DISTINCT tenant_id FROM substrate_events WHERE kind='policy_revision'`, calls `policies.ActiveBundle` per tenant, compiles `rego.PreparedEvalQuery` per `(tenant, action)`, builds initial `opaStore`, `store.Store(newStore)`. On empty result: store the default bundle compiled per the 4 actions.
  - `Reload(ctx, tenant) error` — post-commit callback registered with the `AppendEvent(KindPolicyRevision, ...)` transaction. Deep-copies current `*opaStore`, replaces the tenant's 4 `PreparedEvalQuery` slots, swaps via `store.Store(newStore)`.
  - `Check(ctx, p, a, r) (Decision, error)` — load `*opaStore` via `store.Load()`; look up `queries[tenant+"/"+a]` (~1 µs); run `rego.Eval(input={principal, action, resource, now_unix})`; map result → `Decision`. Sentinel mapping: missing tenant slot → `ErrTenantUnknown`; missing bundle (no default fallback) → `ErrPolicyMissing`; eval error (panic-safe wrapper) → `ErrPolicyEvalError`.
- **`internal/authz/store.go`** — NEW. `opaStore` struct (`queries map[string]*rego.PreparedEvalQuery`) + helpers. The atomic copy-on-write swap is the load-bearing invariant — implementer MUST use `atomic.Pointer[opaStore]`, NOT `sync.RWMutex`. Spec §3.3.3 + R8 explicit rejection of the RW-lock alternative.
- **`internal/authz/ctx.go`** — NEW. `WithPrincipal(ctx, p)` / `PrincipalFromContext(ctx)` ctx-bound helpers per spec §3.1 line 100. Type-asserts via an unexported `ctxKey` struct.
- **`internal/authz/*_test.go`** — tests below.

### Prereqs (cite spec sections)
- Spec §3.1 — Authorizer interface + sentinels + ctx-bound principal.
- Spec §3.2 — OPA embedding via `github.com/open-policy-agent/opa/rego`; rejected alternatives (Cedar, Casbin, custom DSL).
- Spec §3.3.3 — atomic `*opaStore` copy-on-write swap; R4 + R8 mitigation.
- Spec §5 R1 — OPA eval p99 ≤ 200 µs (Benchmark target, A-tier).
- Spec §5 R4 — atomic store swap (S-severity).
- Spec §5 R8 — RW-lock alternative rejected; copy-on-write only.
- Spec §11 — OPA Rego library reference + Apache-2.0 license OK for vendoring.

### Existing patterns to reuse (do NOT reinvent)
- **substrate.FoldByTenant / substrate.AppendEvent:** T-S1 #224 + T2's followup. `Hydrate` enumerates tenants via the new `substrate.FoldByTenant(ctx, db, tenantID, KindPolicyRevision)` helper (T2-owned, T-S1 followup) — NOT via the run_id-keyed `substrate.Fold` (policy events do not carry a run_id). Per-tenant bundle load goes through `policies.ActiveBundle` (T2-owned).
- **W6 tracer pattern:** `cfg.Tracer trace.Tracer` field on `Config` per W6 T5 #210 normalization; fallback to `otel.Tracer("internal/authz")`. Existing convention; do NOT redefine.
- **OPA Rego API:** `rego.New(...)` → `PrepareForEval(ctx)` → `PreparedEvalQuery.Eval(ctx, rego.EvalInput(input))`. Standard upstream pattern; reference https://pkg.go.dev/github.com/open-policy-agent/opa/rego.
- **`web.Principal` type:** added by W7 Wave 1 T7 (#268) — `Principal{ID, Tenant, Roles}` in `internal/web/auth.go`. T1 imports the type; does NOT redefine.
- **Context-key idiom:** `type ctxKey struct{}; var principalKey ctxKey` — standard Go pattern; mirrors `internal/orchestrator/state/substrate/ctx.go` if present.

### TDD test list (failing-output capture step required)

Per `feedback_tdd_discipline`: implementer writes each test first, runs `go test ./internal/authz/ -run <name> -v`, **captures failing output (paste into PR body)**, then implements. "Tests would have failed" is NOT acceptable.

**B-tier (4 named — spec §6 T1 verbatim):**

1. `TestAuthorizerCheck_DefaultBundle_AllowsHMACReviewer` — single-tenant default-deploy smoke: `Principal{ID: "alice", Tenant: "default"}` + `ActionApprovalDecide` + valid Resource → `Decision.Allow == true`, `Decision.Reason == "hmac-reviewer"`.
2. `TestAuthorizerCheck_UnknownTenant_ReturnsErrTenantUnknown` — `Principal{Tenant: "acme"}` against a store with no `acme` bundle → `errors.Is(err, authz.ErrTenantUnknown)`.
3. `TestAuthorizerCheck_EmptyPrincipal_DefaultDenies` — `Principal{ID: "", Tenant: "default"}` → `Decision.Allow == false`, `Decision.Reason == "default-deny"`. Pins fail-closed property.
4. `TestOpaStore_SwapIsAtomic` — two goroutines: one calls `Check` in a 1 000-iteration loop; the other calls `Reload(ctx, "default")` 100 times. Assert no panic + assert every `Check` returns a `Decision` rendered against exactly ONE bundle SHA per call (no torn read). Implementation: each `Decision.PolicyRevision` is one of {SHA-before-reload, SHA-after-reload} — never a mix.

**A-tier (2 named — spec §6 T1 verbatim + spec §7 A):**

5. `TestOpaStore_ReloadDuringEval_NoTorn` — fuzzy concurrent: 8 goroutines × 1 000 evals each, concurrent `Reload`; assert every `Decision.PolicyRevision` references a known bundle SHA in `{SHA_v1, SHA_v2, ..., SHA_vN}` (the set of revisions appended during the test).
6. `TestAuthorizerCheck_CtxBoundPrincipal_Roundtrip` — `WithPrincipal(ctx, p)` + `PrincipalFromContext(ctx)` returns the same `Principal` value; missing ctx returns `(zero, false)`.

**Perf gate (1 named — spec §6 A + §7 A):**

7. `BenchmarkAuthorizerCheck_p99Under200Micros` — N=10 000 with the default bundle; collect histogram; assert p99 ≤ 200 µs. Pins R1 mitigation. PR body MUST paste `benchstat` output; if p99 > 200 µs, STOP and re-spawn the design subagent (do NOT degrade the budget yourself).

### PR body skeleton

````
## Summary

W8 T1 ships the Authorizer interface + concrete OPA Rego embed +
atomic store swap per
docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md §3.1 §3.2 §3.3.3.

- internal/authz/authz.go — Action / Resource / Decision / Authorizer
  interface + 4 action constants + 4 typed sentinels per §3.1.
- internal/authz/opa.go — opaAuthorizer with PreparedEvalQuery cache
  per (tenant, action), Hydrate + Reload + Check per §3.2. Eval input
  shape {principal, action, resource, now_unix} per §3.2 lines 110-117.
- internal/authz/store.go — opaStore struct + atomic.Pointer[opaStore]
  copy-on-write swap per §3.3.3. NO RW-lock (§5 R8 rejection).
- internal/authz/ctx.go — WithPrincipal / PrincipalFromContext
  ctx-bound helpers per §3.1 line 100.

Adopts `github.com/open-policy-agent/opa/rego` (Apache-2.0; proven OSS
per feedback_research_design_principles + spec §3.2 line 104).

Born with two callers (web handler + CLI re-spawn for
`regatta approve --decide`) per spec §3.1 line 96 — closes the W7 R4
deferral; the interface justifies itself on day one.

## Why

MVP-3 W8 Task T1. The Authorizer interface is the seam W7 §3.6.4
explicitly deferred (R4: "no premature interface until a second
caller"). W8 lands with two callers at birth — interface born on
the day it is consumed by 2+ sites.

## Test plan

- [x] B-tier: TestAuthorizerCheck_DefaultBundle_AllowsHMACReviewer,
       TestAuthorizerCheck_UnknownTenant_ReturnsErrTenantUnknown,
       TestAuthorizerCheck_EmptyPrincipal_DefaultDenies,
       TestOpaStore_SwapIsAtomic.
- [x] A-tier: TestOpaStore_ReloadDuringEval_NoTorn,
       TestAuthorizerCheck_CtxBoundPrincipal_Roundtrip.
- [x] Perf gate: BenchmarkAuthorizerCheck_p99Under200Micros
       p99 ≤ 200 µs (benchstat output below).
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Benchstat output (perf-gate R1)

<paste benchstat output — required before PR opens per spec §5 R1>

## A+ scorecard

<paste verbatim per feedback_a_plus_scorecard_required>

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w8-opa-rbac-followup] F1 policy bundle signing via cosign (#NNN; spec §10 #1)
- [w8-opa-rbac-followup] F4 OPA Wasm runtime (#NNN; spec §10 #4)

## Deletion default

Authorizer interface born with TWO callers (web + CLI) per spec §3.1
line 96 — zero "premature interface" debt. Closes W7 R4 deferral.

```release-notes
[FEATURE] Authorizer interface + concrete OPA Rego embed (W8 T1 — interface only; handler wiring lands in T3, default bundle in T4, audit + OTel in T5)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w8-t1. Branch off main:
`git checkout -b feat/w8-t1-authorizer-opa main`.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md.
Read ALL of: §3.1 (Authorizer interface), §3.2 (OPA embedding), §3.3.3
(atomic store swap), §5 R1 + R4 + R8 (perf budget + store-swap +
RW-lock-rejection), §6 T1 (named test list), §7 B/A rubric, §8 file
disjoint row T1, §11 (OPA library reference).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (interface shape; OPA rego embed via
github.com/open-policy-agent/opa/rego; atomic.Pointer copy-on-write
swap — NO RW-lock alternative; Decision.PolicyRevision = 8-char SHA
prefix; PreparedEvalQuery cache per (tenant, action); sentinel naming
verbatim), STOP and report — do NOT pick an alternative yourself.
Re-spawn the design subagent.

Per feedback_grade_rubric + feedback_a_plus_scorecard_required: PR body
MUST end with the A+ scorecard pasted verbatim. Score B/A/A+ per
spec §7.

Per feedback_no_signatures: NO Co-Authored-By, NO AI footers anywhere
(commits, PR body, code comments).

Per feedback_doc_check_banned_phrases: scripts/doc-check.sh greps every
tracked markdown file (outside research/ + docs/rfcs/) for a hardcoded
banned-token regex and fails closed on the first hit. The token list is
defined in scripts/doc-check.sh `banned_tokens=(...)`; read it there
before composing any new prose. Reword to falsifiable claims; do NOT
quote the tokens inline (the lint cannot distinguish quoted from
asserted use).

# Scope (exclusive write paths — file-disjoint with T2, T3, T4, T5)

- internal/authz/authz.go        (NEW; types + interface + sentinels)
- internal/authz/opa.go          (NEW; opaAuthorizer + Hydrate + Reload + Check)
- internal/authz/store.go        (NEW; opaStore + atomic.Pointer swap)
- internal/authz/ctx.go          (NEW; WithPrincipal / PrincipalFromContext)
- internal/authz/authz_test.go   (NEW; B-tier 4 tests)
- internal/authz/opa_test.go     (NEW; A-tier 2 tests)
- internal/authz/bench_test.go   (NEW for T1 perf gate ONLY — T5 expands to property tests + audit benches; T1 commits ONLY the BenchmarkAuthorizerCheck_p99Under200Micros perf gate)
- go.mod / go.sum                (add github.com/open-policy-agent/opa)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/authz/policies/ — that is T2's scope.
- Do NOT touch internal/authz/policies/embedded/ — that is T4's scope.
- Do NOT touch internal/web/ — that is T3's scope.
- Do NOT touch internal/authz/otel.go or audit.go — T5's scope.
- Do NOT touch cmd/regatta/ — T5 owns the one-line serve.go wiring.

If you discover a missing seam in an out-of-scope file, STOP and
report. File a tracking issue per finding; do NOT edit out of scope.

# Output path slug (per feedback_plan_subagent_dup_files)

Branch: feat/w8-t1-authorizer-opa.
PR title: feat(w8): T1 Authorizer interface + concrete OPA Rego embed + atomic store swap.

# Patterns to reuse (do NOT reinvent)

- substrate.Fold + substrate.AppendEvent: T-S1 #224 exported API.
- W6 tracer factory: cfg.Tracer field on Config; fallback to
  otel.Tracer("internal/authz"). See W6 T5 #210 normalization.
- OPA Rego pattern: rego.New(...).PrepareForEval(ctx) →
  PreparedEvalQuery.Eval(ctx, rego.EvalInput(input)). Reference
  https://pkg.go.dev/github.com/open-policy-agent/opa/rego.
- web.Principal type: added by W7 Wave 1 T7 (#268) in
  internal/web/auth.go. Import; do NOT redefine.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test below:
  1. Write the test file first.
  2. Run `go test ./internal/authz/ -run <TestName> -v`.
  3. CAPTURE the failing output (paste at least 4 representative
     samples into PR body's "Failing-test output" section). "Tests
     would have failed" is NOT acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (7 named; spec §6 T1 + §7 B/A)

B-tier:
1. TestAuthorizerCheck_DefaultBundle_AllowsHMACReviewer
2. TestAuthorizerCheck_UnknownTenant_ReturnsErrTenantUnknown
3. TestAuthorizerCheck_EmptyPrincipal_DefaultDenies
4. TestOpaStore_SwapIsAtomic

A-tier:
5. TestOpaStore_ReloadDuringEval_NoTorn
6. TestAuthorizerCheck_CtxBoundPrincipal_Roundtrip

Perf gate (REQUIRED before opening PR):
7. BenchmarkAuthorizerCheck_p99Under200Micros — N=10 000; p99 ≤
   200 µs. Run `go test -bench BenchmarkAuthorizerCheck -benchmem
   -count=10 ./internal/authz/` + paste benchstat output. If p99 >
   200 µs, STOP and re-spawn design subagent (do NOT degrade the
   budget yourself).

# Workflow after green

  1. Run `make pre-push-check` clean. Do NOT skip hooks (--no-verify
     banned per feedback_pr_lint_gates).
  2. Run `bash scripts/doc-check.sh` + `bash scripts/stale-todo.sh`
     — both MUST exit 0 (per feedback_pr_lint_gates).
  3. Sweep comments per feedback_comments_discipline: WHY not WHAT;
     test-function godocs ≤ 1 line; drop superfluous.
  4. Push branch.
  5. File followup issues F1 (cosign bundle signing) + F4 (OPA Wasm
     runtime) as `[w8-opa-rbac-followup]`-prefixed issues; gather
     numbers.
  6. Open PR via `gh pr create --base main --body-file <path>`
     (heredoc banned per feedback_pr_lint_gates). PR body MUST end
     with the literal ` ```release-notes\n[FEATURE] ...\n``` ` fence
     (per feedback_pr_body_release_notes_fence). Grep-verify the
     fence is present before push.
  7. Spawn ONE adversarial reviewer subagent (per
     feedback_adversarial_review + feedback_agent_pr_review) with
     hunt list:
       - Interface shape EXACT match to spec §3.1 (4 actions, 4
         sentinels, Decision struct fields).
       - atomic.Pointer (NOT sync.RWMutex). Verify via grep.
       - PreparedEvalQuery cached per (tenant, action) — NOT
         recompiled on hot path. Verify via Hydrate + Reload calling
         rego.PrepareForEval, never on Check.
       - PolicyRevision = 8-char SHA prefix. Full SHA stays in audit
         row (T5's scope; T1 just exports the shape).
       - Sentinel error chains: errors.Is(err, ErrDenied) etc.
       - go.mod / go.sum: github.com/open-policy-agent/opa is
         Apache-2.0; no GPL contamination.
       - Eval-path latency: BenchmarkAuthorizerCheck p99 ≤ 200 µs at
         N=10 000. Benchstat output present in PR body.
       - No new SQL migration. (T1 touches zero SQL.)
       - No AI signatures (feedback_no_signatures).
       - Comments discipline: WHY not WHAT; test godocs ≤ 1 line.
  8. Apply reviewer findings inline (or file tracking issue + cite
     in PR body per feedback_unaddressed_load_bearing).
  9. Re-run pre-push-check + doc-check.sh + stale-todo.sh; force-push.
 10. Verify CI green (pr-lint, check-release-notes, check-tdd, build,
     test) BEFORE flipping automerge per
     feedback_review_before_automerge.
 11. Flip automerge ONLY after reviewer cleared the PR.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 4 of the 7 tests.
- Pasted benchstat output for the perf gate.
- 2 followup issue numbers filed.
- Adversarial reviewer verdict (APPROVE or full findings list).
- A+ scorecard (verbatim per feedback_a_plus_scorecard_required).
- One-line diff stat: files changed + LoC added.

Begin now. NEVER pause for user input.
```

---

## §3 Task T2 — `policies` substrate primitive (`KindPolicyRevision` + validator + fold)

### Scope

- **`internal/authz/policies/payload.go`** — NEW. Per spec §3.3.1 verbatim:
  - `KindPolicyRevision substrate.EventKind = "policy_revision"` constant.
  - `PolicyRevisionPayload` struct — 5 fields with JSON tags verbatim from spec lines 141-148.
  - `validatePolicyRevision(payload json.RawMessage) error` — per spec §3.3.1 verbatim:
    - `BundleSHA256 == sha256(canonical-json(sort(RegoFiles)))[:64]` (mismatch ⇒ `ErrPolicyBundleHashMismatch`).
    - `len(RegoFiles) >= 1` (empty ⇒ `ErrPolicyBundleEmpty`).
    - Per-file ≤ 64 KiB; bundle total ≤ 1 MiB (R3 mitigation).
    - Rule count ≤ 1 000 via `opa.compile` AST walk (R3 mitigation).
    - Path grammar: every key starts with `regatta/v1/`; second segment is `default` or matches `^[a-z][a-z0-9_-]{1,62}$`.
    - All Rego files compile via `opa.compile` (calls T2's `compile.go` wrapper).
  - `init()` block: `substrate.RegisterPayloadValidator(KindPolicyRevision, validatePolicyRevision)`.
  - Errors: `ErrPolicyBundleHashMismatch`, `ErrPolicyBundleEmpty`, `ErrPolicyBundlePathInvalid`, `ErrPolicyBundleCompileError`.
- **`internal/authz/policies/fold.go`** — NEW. `ActiveBundle(ctx, db state.DB, tenant string) (sha string, files map[string]string, err error)` per spec §3.3.2 verbatim. Reads via `substrate.Fold(ctx, db, runID="", kind=KindPolicyRevision)`, filters by `payload.TenantID == tenant`, returns the most recent (by `written_at DESC, id DESC` — substrate default LWW reducer).
- **`internal/authz/policies/compile.go`** — NEW. `Compile(files map[string]string) (*ast.Compiler, error)` thin wrapper over `opa.compile` for write-time validation. Pure delegation; ~30 LoC. Centralizes the OPA import so payload.go doesn't pull the AST module.
- **`internal/authz/policies/payload_test.go`** + **`fold_test.go`** + **`compile_test.go`** — tests below.

### Prereqs (cite spec sections)
- Spec §3.3 — `policies` substrate primitive overview (no new table, no new migration).
- Spec §3.3.1 — `KindPolicyRevision` + payload struct + validator rules **verbatim**.
- Spec §3.3.2 — LWW reducer + `ActiveBundle` API signature.
- Spec §5 R3 — bundle size caps (64 KiB / 1 MiB / 1 000 rules).
- Spec §5 R12 — policy_revision write requires `roles: ["policy_admin"]` (encoded in Rego itself — T4's default bundle owns this rule; T2's validator does NOT enforce role; T2 only validates payload shape).
- Spec §6 T2 — named test list.
- Spec §11 — OPA library reference.

### Existing patterns to reuse (do NOT reinvent)
- **substrate.RegisterPayloadValidator:** T-S1 #224's open-extension hook. Pattern: `func init() { substrate.RegisterPayloadValidator(KindPolicyRevision, validatePolicyRevision) }`.
- **substrate.Fold:** T-S1 #224 export. `ActiveBundle` calls Fold; does NOT re-implement substrate read.
- **Canonical JSON helper:** `internal/canon/canon.go` (existing). Use for the `sort(RegoFiles)` canonical form before SHA-256. Do NOT introduce a new canonicalizer.
- **OPA compile API:** `github.com/open-policy-agent/opa/ast.CompileModules(map[string]string)` — standard upstream pattern. Reference https://pkg.go.dev/github.com/open-policy-agent/opa/ast.

### TDD test list

Per `feedback_tdd_discipline`: failing-output capture required.

**B-tier (4 named — spec §6 T2 verbatim):**

1. `TestPolicyRevision_Append_ValidBundle_Succeeds` — happy path: build canonical bundle, compute SHA-256, call `substrate.AppendEvent(KindPolicyRevision, ...)`, assert one row written + `ActiveBundle` returns the bundle SHA.
2. `TestPolicyRevision_Append_BundleHashMismatch_ReturnsErrPolicyBundleHashMismatch` — payload `BundleSHA256` ≠ computed SHA → validator catches, `errors.Is(err, ErrPolicyBundleHashMismatch)`, no substrate row.
3. `TestPolicyRevision_Append_RuleCountCap_Rejects` — synthetic bundle with 1 001 Rego rules → validator rejects with `ErrPolicyBundleCompileError` wrapping rule-count-exceeded. Pins R3 mitigation.
4. `TestPolicyRevision_Fold_ReturnsMostRecentBundle` — append 3 bundles for same tenant; `ActiveBundle` returns the third (by written_at DESC). Pins LWW reducer correctness.

**A-tier (2 named — spec §6 T2 verbatim + §7 A):**

5. `TestPolicyRevision_Append_RegoFilesEmpty_Rejects` — `RegoFiles == nil` or `len == 0` → `errors.Is(err, ErrPolicyBundleEmpty)`. Pins R3 + spec §3.3.1.
6. `TestPolicyRevision_OPACompileError_Rejects` — Rego with syntax error (e.g. unclosed brace) → `errors.Is(err, ErrPolicyBundleCompileError)` carrying OPA diagnostics. Pins R2 mitigation.

### PR body skeleton

````
## Summary

W8 T2 ships the `policies` substrate primitive (`KindPolicyRevision`
+ payload validator + fold API) + migration #0007 (kind CHECK +
1 MiB payload cap + tenant_id index) + `substrate.FoldByTenant`
T-S1 followup helper, per
docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md §3.3.

- internal/authz/policies/payload.go — KindPolicyRevision alias +
  PolicyRevisionPayload struct (5 fields verbatim spec §3.3.1) +
  validatePolicyRevision (SHA + size + path grammar + opa.compile
  checks per §3.3.1) + init() registering via T-S1 #224's
  substrate.RegisterPayloadValidator open-extension hook.
- internal/authz/policies/fold.go — ActiveBundle(ctx, db, tenant)
  returning (sha, files, err) per §3.3.2. Wraps the NEW
  substrate.FoldByTenant helper; LWW reducer via the prepared
  statement against the new tenant_id index.
- internal/authz/policies/compile.go — Compile(files) thin wrapper
  over opa.ast.CompileModules; centralizes the OPA AST import.
- internal/orchestrator/state/migrations/0007_w8_policy_revision.sql
  — additive: appends `policy_revision` to substrate_events.kind
  CHECK list; lifts payload_json cap to 1 MiB for that kind only;
  adds idx_substrate_events_tenant_kind(tenant_id, kind, id DESC).
- internal/orchestrator/state/substrate/fold.go — FoldByTenant
  helper (T-S1 followup); prepared statement; uses the new index.
- internal/orchestrator/state/substrate/event.go — adds
  KindPolicyRevision const + extends AllKinds() slice (additive
  only; in lockstep with migration #0007's CHECK list).

ONE additive migration (#0007). T-S1 #224's open-extension contract
honored: substrate's validate.go is NOT modified.

## Why

MVP-3 W8 Task T2. The policies primitive is the data plane for OPA
bundles. T-S1 #224's RegisterPayloadValidator hook means T2 ships
without a new table, a new reducer override, or a new migration —
substrate spec §13 row #2's deferral closes here.

## Test plan

- [x] B-tier: TestPolicyRevision_Append_ValidBundle_Succeeds,
       TestPolicyRevision_Append_BundleHashMismatch_ReturnsErrPolicyBundleHashMismatch,
       TestPolicyRevision_Append_RuleCountCap_Rejects,
       TestPolicyRevision_Fold_ReturnsMostRecentBundle.
- [x] A-tier: TestPolicyRevision_Append_RegoFilesEmpty_Rejects,
       TestPolicyRevision_OPACompileError_Rejects.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## A+ scorecard

<paste verbatim per feedback_a_plus_scorecard_required>

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w8-opa-rbac-followup] F2 dynamic policy reload via fs watcher / admin endpoint (#NNN; spec §10 #2)
- [w8-opa-rbac-followup] F5 opa test harness in make check (#NNN; spec §10 #5)

## Deletion default

`policies` ships via existing substrate_events rows (T-S1 #224's
RegisterPayloadValidator open-extension hook). ZERO new SQL table,
ZERO new reducer. ONE additive migration (#0007) lifts the kind
CHECK + 1 MiB payload cap for `policy_revision` + adds
`idx_substrate_events_tenant_kind` — subtracts two alternative
implementations (path B: parallel `policies` SQL table; path C:
CAS-blob payload split — both rejected in spec §4). Net delta:
+1 migration, -2 alternative storage primitives.

```release-notes
[FEATURE] policies substrate primitive (KindPolicyRevision + payload validator + fold API + migration #0007 + substrate.FoldByTenant; W8 T2)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w8-t2. Branch off main:
`git checkout -b feat/w8-t2-policies-substrate main`.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md.
Read ALL of: §3.3 (policies substrate overview), §3.3.1 (payload +
validator rules — verbatim), §3.3.2 (LWW reducer + ActiveBundle), §5
R2 + R3 + R12 (bundle compile-error + DoS caps + policy_admin role),
§6 T2 (named test list), §7 B/A rubric, §8 file disjoint row T2.

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (KindPolicyRevision via RegisterPayloadValidator
— do NOT modify substrate's validate.go; PolicyRevisionPayload field
shape verbatim; validator rules verbatim — SHA computed over
canonical-json(sort(RegoFiles))[:64], per-file ≤ 64 KiB, bundle ≤ 1
MiB, ≤ 1 000 rules, path grammar; LWW reducer via substrate.Fold —
no override; ZERO new SQL migration), STOP and report — do NOT pick
an alternative yourself. Re-spawn the design subagent.

Per feedback_grade_rubric + feedback_a_plus_scorecard_required: PR
body MUST end with the A+ scorecard pasted verbatim.

Per feedback_no_signatures: NO Co-Authored-By / AI footers anywhere.

Per feedback_doc_check_banned_phrases: see T1 prompt for token list.

# Scope (exclusive write paths — file-disjoint with T1, T3, T4, T5)

- internal/authz/policies/payload.go                                       (NEW; KindPolicyRevision alias + struct + validator + init())
- internal/authz/policies/fold.go                                          (NEW; ActiveBundle wraps substrate.FoldByTenant)
- internal/authz/policies/compile.go                                       (NEW; opa.ast.CompileModules wrapper)
- internal/authz/policies/payload_test.go                                  (NEW)
- internal/authz/policies/fold_test.go                                     (NEW)
- internal/authz/policies/compile_test.go                                  (NEW)
- internal/orchestrator/state/migrations/0007_w8_policy_revision.sql       (NEW; CHECK extension + 1 MiB cap for policy_revision + idx_substrate_events_tenant_kind)
- internal/orchestrator/state/substrate/fold.go                            (NEW; FoldByTenant T-S1 followup helper)
- internal/orchestrator/state/substrate/event.go                           (MUTATE; ADD KindPolicyRevision const + AllKinds() slice extension — additive only, in lockstep with #0007)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/authz/ root (T1's scope) — your imports cross
  the boundary one-way: T1 imports policies; policies does NOT import
  authz.
- Do NOT touch internal/authz/policies/embedded/ — T4's scope.
- Do NOT touch internal/orchestrator/state/substrate/validate.go — use
  the RegisterPayloadValidator open-extension hook from your own
  init().
- ADD `KindPolicyRevision` to substrate/event.go's enum const block AND
  to AllKinds() AND to migration #0007's CHECK list in lockstep —
  anywhere else NO. The Go enum, the SQL CHECK, and the validator
  registration are one atomic edit set; if you find yourself wanting
  to touch any other substrate file, STOP and re-spawn the design
  subagent.
- Do NOT register KindAuthzDenied — that is T5's scope.

If you discover a missing seam in an out-of-scope file, STOP and
report.

# Output path slug (per feedback_plan_subagent_dup_files)

Branch: feat/w8-t2-policies-substrate.
PR title: feat(w8): T2 policies substrate primitive — KindPolicyRevision + payload validator + fold API.

# Patterns to reuse (do NOT reinvent)

- substrate.RegisterPayloadValidator: T-S1 #224 open-extension hook.
  Pattern: func init() { substrate.RegisterPayloadValidator(K, fn) }.
- substrate.Fold: T-S1 #224 export. ActiveBundle wraps Fold; no
  direct substrate_events query.
- Canonical JSON: internal/canon/canon.go existing helper. Use for
  sort(RegoFiles) before SHA-256.
- opa.ast.CompileModules: standard upstream. Reference
  https://pkg.go.dev/github.com/open-policy-agent/opa/ast.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test:
  1. Write test first.
  2. Run `go test ./internal/authz/policies/ -run <TestName> -v`.
  3. CAPTURE failing output.
  4. Implement minimum.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (6 named; spec §6 T2 + §7 B/A)

B-tier:
1. TestPolicyRevision_Append_ValidBundle_Succeeds
2. TestPolicyRevision_Append_BundleHashMismatch_ReturnsErrPolicyBundleHashMismatch
3. TestPolicyRevision_Append_RuleCountCap_Rejects
4. TestPolicyRevision_Fold_ReturnsMostRecentBundle

A-tier:
5. TestPolicyRevision_Append_RegoFilesEmpty_Rejects
6. TestPolicyRevision_OPACompileError_Rejects

# Workflow after green

  1. Run `make pre-push-check` clean.
  2. Run `bash scripts/doc-check.sh` + `bash scripts/stale-todo.sh`
     — both MUST exit 0.
  3. Sweep comments per feedback_comments_discipline.
  4. Push branch.
  5. File followup issues F2 (dynamic reload) + F5 (opa test harness)
     as `[w8-opa-rbac-followup]`-prefixed; gather numbers.
  6. Open PR via `gh pr create --base main --body-file <path>`. PR
     body MUST end with the ` ```release-notes\n[FEATURE] ...\n``` `
     fence; grep-verify before push.
  7. Spawn ONE adversarial reviewer subagent with hunt list:
       - PolicyRevisionPayload field shape EXACT match to spec
         §3.3.1 (5 fields, JSON tags verbatim).
       - Validator rules EXACT match: SHA over canonical-json + size
         caps + path grammar + opa.compile.
       - init() registers via substrate.RegisterPayloadValidator —
         NOT a direct edit to substrate/validate.go. Verify via grep.
       - ActiveBundle reads via the NEW substrate.FoldByTenant helper
         — NOT a direct `SELECT * FROM substrate_events` anywhere
         outside `internal/orchestrator/state/substrate/`. Verify via
         grep.
       - substrate.FoldByTenant uses a **prepared statement** (not
         string concatenation) and the new
         `idx_substrate_events_tenant_kind` index. Verify via
         `EXPLAIN QUERY PLAN` in a test + grep for `db.PrepareContext`
         or equivalent.
       - Canonical JSON: uses internal/canon helper. Verify via
         grep + import list.
       - LWW reducer: ActiveBundle returns most recent by
         (written_at, id) DESC — no override registration.
       - Migration #0007 is the ONLY new migration. Verify via
         `ls internal/orchestrator/state/migrations/` count = T-S1 + 1.
         No #0008+ in the diff.
       - Migration #0007 lifts the kind CHECK to include
         `policy_revision`; lifts the `payload_json` cap to 1 MiB
         for that kind ONLY (other kinds still capped at 1024 bytes);
         adds `idx_substrate_events_tenant_kind(tenant_id, kind, id DESC)`.
       - substrate/event.go: KindPolicyRevision const ADDED;
         AllKinds() slice EXTENDED — no other edits to that file.
       - No AI signatures (feedback_no_signatures).
       - Comments discipline.
  8. Apply findings; re-run pre-push-check + doc-check + stale-todo;
     force-push.
  9. Verify CI green; flip automerge.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 4 of the 6 tests.
- 2 followup issue numbers filed.
- Adversarial reviewer verdict.
- A+ scorecard.
- One-line diff stat.

Begin now. NEVER pause for user input.
```

---

## §4 Task T3 — Principal.Tenant wiring + cookie binding + legacy redirect

### Scope

- **`internal/canon/approval_token.go`** — MUTATE. Extend `ApprovalTokenPayload` with optional `Tenant string \`json:"tenant,omitempty"\``. Per spec §3.4.1, the canonical-JSON signer is wire-back-compat: legacy tokens without the field decode with `Tenant == ""`. On decode-side, `Tenant == ""` ⇒ caller fills `Principal.Tenant = "default"`. Net diff ≤ 6 LoC (one field + one JSON tag).
- **`internal/canon/approval_token_test.go`** — MUTATE. Add `TestApprovalToken_LegacyPayload_DecodesAsDefaultTenant` (wire-back-compat verification — a payload signed before T3 lands decodes verbatim post-T3, with `Tenant == ""`).
- **`internal/web/auth.go`** — MUTATE. Populate `Principal.Tenant` inside `PrincipalFromRequest` from the decoded HMAC payload's `Tenant` field. Empty ⇒ default to `"default"` (spec §3.4.1). Attach the populated `Principal` to ctx via `authz.WithPrincipal(ctx, p)` so downstream handlers + `approval.DecideTx` reads via `authz.PrincipalFromContext`. Net diff ≤ 12 LoC.
- **`internal/web/approval.go`** — MUTATE. Per spec §3.4.2:
  - Route `/approve/<tenant>/<approval_id>` (GET + POST) — tenant binding enforced cookie-side AND URL-side (both MUST match the signed payload). Mismatch ⇒ render `approval_error.tmpl` with `tenant_mismatch` sentinel; zero audit row written (R5 mitigation).
  - Cookie scope: `Path=/approve/<tenant>/<approval_id>` (was `Path=/approve/<approval_id>`).
  - Legacy `/approve/<approval_id>` ⇒ 301 redirect to `/approve/default/<approval_id>` (rollout backwards-compat; removed in W8.2 followup F9).
  - Call `authz.Check(ctx, principal, authz.ActionApprovalDecide, approval.ID)` BEFORE `approval.DecideTx`; `errors.Is(err, authz.ErrDenied)` ⇒ render `approval_error.tmpl` with `denied` sentinel + audit row appended via T5's `audit.go` (the audit-append seam is a T5 export; T3 calls T5's exported `audit.AppendDenied(ctx, decision)` function — landing-order note at T3-implementation time: if T5 hasn't landed yet, T3 wires a stub function whose body is empty; T5 fills it in. Document this in the dispatch prompt).
- **`internal/web/auth_test.go`** — NEW. Tests below.
- **`internal/web/templates/approval_error.tmpl`** — MUTATE. Add `tenant_mismatch` sentinel branch + `denied` sentinel branch (the latter is wired-up by T3's handler; the template renders it).

### Prereqs (cite spec sections)
- Spec §3.4.1 — HMAC cookie payload carries `Tenant`; wire-back-compat with `Tenant == ""`.
- Spec §3.4.2 — multi-tenant cookie path; cookie scope; legacy 301 redirect.
- Spec §3.6 — request flow (Authorizer.Check inserted between PrincipalFromRequest and the handler body).
- Spec §5 R5 — cross-tenant cookie leakage (S-severity; cookie Path tightening + URL-side tenant assertion).
- Spec §6 T3 — named test list.
- Spec §7 B/A rubric.

### Existing patterns to reuse (do NOT reinvent)
- **`internal/canon/approval_token.go` decode pattern:** existing canonical-JSON decode via `canon.Verify` + `json.Unmarshal`. Add `Tenant` field to the payload struct; existing decode pipeline handles omitempty via `json:"tenant,omitempty"`.
- **`internal/web/auth.go::PrincipalFromRequest` shape:** W7 §3.6.4 / I4 declared the function returning `(Principal, error)`. T3 mutates the body; signature stays stable per spec line 34: "the W7 handler signature stays stable".
- **HTTP routing pattern:** existing `internal/web/approval.go` uses `net/http.ServeMux` with `gorilla/mux`-style paths (whichever the W7 Wave 1 T7 PR landed). T3 reuses; does NOT introduce a new router.
- **Template rendering:** existing `approval_error.tmpl` per W7 §3.4. Add two new sentinel branches; do NOT introduce a new template file.

### TDD test list

Per `feedback_tdd_discipline`: failing-output capture required.

**B-tier (3 named — spec §6 T3 verbatim):**

1. `TestPrincipalFromRequest_TenantFromCookiePayload` — sign an approval token with `Tenant: "acme"`, attach as cookie, call `PrincipalFromRequest(r)` → `Principal.Tenant == "acme"`. Also: empty-tenant payload → `Principal.Tenant == "default"`.
2. `TestApprovalHandler_TenantMismatch_RendersTypedError` — payload `Tenant: "acme"`, URL `/approve/zeta/01HZ...` (different tenant) → response renders `approval_error.tmpl` with `tenant_mismatch` sentinel; HTTP status 403; **zero substrate audit row written** (R5 mitigation — no audit-row leak across tenant boundary).
3. `TestApprovalHandler_LegacyURLRedirectsToDefaultTenant` — `GET /approve/01HZ...` → 301 redirect to `/approve/default/01HZ...`. Backwards compat for rollout window.

**A-tier (2 named — spec §6 T3 verbatim + §7 A):**

4. `TestApprovalToken_LegacyPayload_DecodesAsDefaultTenant` — canonical-JSON payload without `Tenant` field (pre-T3 shape) decodes verbatim post-T3; consumer fills `Tenant = "default"`. Pins wire-back-compat invariant.
5. `TestApprovalDecide_PolicyDenies_CLIAndWebBothReturnDenied` — Authorizer.Check returns `Decision{Allow: false, Reason: "..."}` for both the web handler path AND the CLI path (`regatta approve --decide` shells through the same Authorizer.Check). Both return the same `denied` sentinel error page / CLI exit code. Pins CLI + web parity (spec §3.1 line 96).

### PR body skeleton

````
## Summary

W8 T3 wires `Principal.Tenant` from the HMAC cookie payload, tightens
the cookie path to `/approve/<tenant>/<approval_id>`, and adds the
`tenant_mismatch` + `denied` sentinel error pages, per
docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md §3.4.1 + §3.4.2 +
§3.6.

- internal/canon/approval_token.go — ApprovalTokenPayload gains
  optional `Tenant string \`json:"tenant,omitempty"\`` field.
  Wire-back-compat: legacy payloads without the field decode as
  Tenant="" ⇒ Principal.Tenant="default".
- internal/web/auth.go — PrincipalFromRequest populates
  Principal.Tenant; attaches Principal to ctx via authz.WithPrincipal.
- internal/web/approval.go — routes /approve/<tenant>/<approval_id>;
  enforces tenant binding cookie + URL match; renders typed
  tenant_mismatch error page on mismatch (zero audit row written;
  R5 mitigation). Legacy /approve/<approval_id> 301-redirects to
  /approve/default/<approval_id>.
- internal/web/templates/approval_error.tmpl — new tenant_mismatch
  + denied sentinel branches.

## Why

MVP-3 W8 Task T3. The Principal type was declared (but unpopulated)
by W7 Wave 1 T7 with `Tenant: "default"`; T3 wires the Tenant from
the HMAC payload, gating every approval decision through OPA via T1's
Authorizer.Check.

## Test plan

- [x] B-tier: TestPrincipalFromRequest_TenantFromCookiePayload,
       TestApprovalHandler_TenantMismatch_RendersTypedError,
       TestApprovalHandler_LegacyURLRedirectsToDefaultTenant.
- [x] A-tier: TestApprovalToken_LegacyPayload_DecodesAsDefaultTenant,
       TestApprovalDecide_PolicyDenies_CLIAndWebBothReturnDenied.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## A+ scorecard

<paste verbatim per feedback_a_plus_scorecard_required>

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w8-opa-rbac-followup] F9 W8.2 legacy /approve/<approval_id> 301-redirect removal (#NNN; spec §10 #9)

## Deletion default

Wire-back-compat HMAC payload extension — legacy tokens with
`Tenant==""` decode as `Tenant="default"`. Single-tenant deployments
require zero operator config change for the rollout window
(spec §3.4.1 + §3.5 default-deny baseline's HMAC-reviewer exception).

```release-notes
[FEATURE] Principal.Tenant wiring + multi-tenant cookie path + tenant_mismatch sentinel error page (W8 T3 — rollout-window backwards-compat 301 redirect from legacy /approve/<approval_id>)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w8-t3. Branch off main AFTER W7 Wave 1 T7
(#268 plan) lands:
`git fetch origin && git checkout -b feat/w8-t3-principal-tenant main`.

If W7 Wave 1 T7's `Principal{ID, Tenant, Roles}` type is not yet on
main, STOP — do NOT proceed. T3 is hard-blocked on the type's
existence in internal/web/auth.go.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md.
Read ALL of: §3.4 (Principal extension + multi-tenant cookie binding),
§3.4.1 (HMAC payload + wire-back-compat), §3.4.2 (cookie path
tightening + legacy redirect), §3.6 (request flow), §5 R5 + R6
(cross-tenant cookie leakage + default-deny ambiguity), §6 T3 (named
test list), §7 B/A rubric, §8 file disjoint row T3.

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (Tenant field optional in HMAC payload with
omitempty; legacy decode ⇒ "default"; cookie path
`/approve/<tenant>/<approval_id>`; mismatch ⇒ typed tenant_mismatch
page with ZERO audit row; legacy 301 redirect to
`/approve/default/<approval_id>`; PrincipalFromRequest signature
stable), STOP and report — do NOT pick an alternative yourself.
Re-spawn the design subagent.

Per feedback_grade_rubric + feedback_a_plus_scorecard_required: PR
body MUST end with the A+ scorecard pasted verbatim.

Per feedback_no_signatures + feedback_doc_check_banned_phrases: see
T1 prompt for token list + no AI footers anywhere.

Per feedback_comments_discipline: WHY not WHAT; test godocs ≤ 1
line; sweep before push.

# Scope (exclusive write paths — file-disjoint with T1, T2, T4, T5)

- internal/canon/approval_token.go             (MUTATE — add Tenant field, ≤ 6 LoC)
- internal/canon/approval_token_test.go        (MUTATE — add legacy-decode test)
- internal/web/auth.go                         (MUTATE — populate Principal.Tenant, attach to ctx)
- internal/web/approval.go                     (MUTATE — multi-tenant route + tenant_mismatch + 301 redirect)
- internal/web/auth_test.go                    (NEW)
- internal/web/templates/approval_error.tmpl   (MUTATE — tenant_mismatch + denied sentinels)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/authz/ root (T1's scope) — import only the
  Authorizer interface + WithPrincipal + ActionApprovalDecide.
- Do NOT touch internal/authz/policies/ (T2's scope).
- Do NOT touch internal/authz/policies/embedded/ (T4's scope).
- Do NOT touch internal/authz/otel.go or audit.go (T5's scope).
- audit.AppendDenied(ctx, decision) is a T5 export; if T5 has not
  yet landed, wire a no-op stub in your branch (call the function as
  `_ = audit.AppendDenied(ctx, dec)` but the function body is `return
  nil`). T5's PR replaces the body. Document this stub in the PR body.
  If you discover T5 has landed first, import the real function.

If you discover a missing seam in an out-of-scope file, STOP and
report.

# Output path slug (per feedback_plan_subagent_dup_files)

Branch: feat/w8-t3-principal-tenant.
PR title: feat(w8): T3 Principal.Tenant wiring + multi-tenant cookie path + tenant_mismatch sentinel.

# Patterns to reuse (do NOT reinvent)

- canonical-JSON decode: existing canon.Verify + json.Unmarshal
  pattern. Add the Tenant field with json:"tenant,omitempty"; the
  existing decode pipeline handles wire-back-compat.
- PrincipalFromRequest signature: stable per spec line 34. Mutate
  body only.
- HTTP router: existing internal/web/approval.go pattern from W7
  Wave 1 T7. Reuse; do NOT introduce a new router.
- approval_error.tmpl: existing W7 §3.4 template. Add two new
  sentinel branches; do NOT introduce a new template file.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test:
  1. Write test first.
  2. Run `go test ./internal/web/ ./internal/canon/ -run <TestName> -v`.
  3. CAPTURE failing output.
  4. Implement minimum.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (5 named; spec §6 T3 + §7 B/A)

B-tier:
1. TestPrincipalFromRequest_TenantFromCookiePayload
2. TestApprovalHandler_TenantMismatch_RendersTypedError
3. TestApprovalHandler_LegacyURLRedirectsToDefaultTenant

A-tier:
4. TestApprovalToken_LegacyPayload_DecodesAsDefaultTenant
5. TestApprovalDecide_PolicyDenies_CLIAndWebBothReturnDenied

# Workflow after green

  1. Run `make pre-push-check` clean.
  2. Run `bash scripts/doc-check.sh` + `bash scripts/stale-todo.sh`
     — both MUST exit 0.
  3. Comments sweep.
  4. Push branch.
  5. File followup issue F9 (W8.2 legacy redirect removal) as
     `[w8-opa-rbac-followup]`-prefixed; gather number.
  6. Open PR via `gh pr create --base main --body-file <path>`. PR
     body MUST end with the ` ```release-notes\n[FEATURE] ...\n``` `
     fence; grep-verify before push.
  7. Spawn ONE adversarial reviewer subagent with hunt list:
       - ApprovalTokenPayload Tenant field json tag has omitempty.
         Verify via grep.
       - Wire-back-compat: legacy payload decodes; tested.
       - Cookie Path tightened to /approve/<tenant>/<approval_id>.
         Verify via grep on the Set-Cookie path attribute.
       - tenant_mismatch ⇒ ZERO audit row written. Verify via the
         negative-assertion in the test.
       - Legacy /approve/<approval_id> 301 redirect — not 302 (302
         is non-cacheable; spec says 301).
       - authz.Check called BEFORE approval.DecideTx (defense in
         depth — payload verified ≠ action authorized).
       - PrincipalFromRequest signature unchanged (W7 §3.6.4 forward
         compat). Verify via diff.
       - audit.AppendDenied stub OR real call — document in PR body.
       - No AI signatures.
       - Comments discipline.
  8. Apply findings; re-run pre-push-check + doc-check + stale-todo;
     force-push.
  9. Verify CI green; flip automerge.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 3 of the 5 tests.
- 1 followup issue number filed.
- Adversarial reviewer verdict.
- A+ scorecard.
- One-line diff stat.

Begin now. NEVER pause for user input.
```

---

## §5 Task T4 — Default-deny policy bundle + tenant onboarding doc + CI fixture

### Scope

- **`internal/authz/policies/embedded/regatta/v1/default/approval.rego`** — NEW. Default-deny baseline per spec §3.5 verbatim. Defines `default approval.{decide,view}.decision := {"allow": false, "reason": "default-deny"}` + the one built-in exception (`hmac-reviewer` for `Principal{Tenant: "default", ID: nonempty}`).
- **`internal/authz/policies/embedded/regatta/v1/default/run.rego`** — NEW. Default-deny baseline for `run.{view,cost.view}.decision`. Same shape as approval.rego.
- **`internal/authz/policies/embedded/regatta/v1/default/data.json`** — NEW. Optional static-facts placeholder (`{}` to start; tenants override at write time).
- **`internal/authz/policies/embedded/embed.go`** — NEW. `//go:embed regatta/v1/default` `embed.FS` export named `embedded.FS`. Plus `DefaultBundleSHA256 string` package-init constant computed at package load over the canonical-JSON of the embedded Rego files (T5's stability test asserts this).
- **`internal/authz/policies/embedded/embed_test.go`** — NEW. Tests below.
- **`docs/operator/rbac-onboarding.md`** — NEW. Tenant onboarding tutorial per spec §10 + §11 reference. Step-by-step: write a tenant bundle, sign it with the HMAC keyring, POST to the admin endpoint OR shell `regatta authz policy-revision --tenant acme --files acme.rego`, verify `ActiveBundle(ctx, db, "acme")` returns the new SHA, verify a denied call now renders the tenant's `reason`.
- **`tests/e2e/authz/onboarding_test.go`** — NEW. CI-executable script that walks the operator doc's steps. Asserts every step succeeds. Pins A-tier rubric `TestTenantOnboarding_CIDocFixture` (spec §6 A).

### Prereqs (cite spec sections)
- Spec §3.5 — bundle layout + default-deny Rego sample **verbatim**.
- Spec §3.4.1 — default-deny baseline's HMAC-reviewer exception (only when `Principal.Tenant == "default"` AND `Principal.ID != ""`).
- Spec §6 T4 — named test list.
- Spec §7 B (default-deny baseline shipped) + A (onboarding flow CI-executable).
- Spec §10 #2 + #5 — followup tracking (dynamic reload + opa test harness — file numbers not owned by T4 but referenced).
- Spec §11 — OPA bundles reference https://www.openpolicyagent.org/docs/latest/management-bundles/.

### Existing patterns to reuse (do NOT reinvent)
- **`//go:embed`:** standard Go pattern. Reference `embed.FS` upstream docs; mirror any existing `//go:embed` usage in the regatta codebase (e.g. `cmd/regatta/init_assets/` if present).
- **Rego file structure:** OPA upstream conventions — package + default rules + override rules. Reference https://www.openpolicyagent.org/docs/latest/policy-language/.
- **T1's `policies.ActiveBundle` API:** T4's onboarding test calls T2's `policies.ActiveBundle` + T1's `authz.NewOPAAuthorizer(...).Hydrate(ctx)` — neither is redefined.

### TDD test list

**B-tier (2 named — spec §6 T4 verbatim):**

1. `TestDefaultBundleSHA256_Stable` — pre-asserted constant: `DefaultBundleSHA256 == "<computed hex>"`. Pins R11 mitigation (bundle drift detected via explicit assertion; no surprise silent change between binary versions).
2. `TestDefaultBundle_HMACReviewer_AllowsApprovalDecide` — load the default bundle into a fresh `opaAuthorizer`; call `Check(ctx, Principal{ID: "alice", Tenant: "default"}, ActionApprovalDecide, "01HZ...")` → `Decision.Allow == true`. Pins spec §3.5 HMAC-reviewer exception.

**A-tier (2 named — spec §6 T4 verbatim + §7 A):**

3. `TestTenantOnboardingFlow_FromEmptyToActive` — start with a fresh DB (only the default bundle hydrated); write a `policy_revision` event for tenant `acme` whose Rego allows `approval.decide` for `Principal{Tenant: "acme", Roles: ["reviewer"]}`; assert `ActiveBundle(ctx, db, "acme")` returns the written SHA; assert `Check(ctx, Principal{Tenant: "acme", Roles: ["reviewer"]}, ActionApprovalDecide, ...)` returns `Allow: true` with the tenant's reason.
4. `TestTenantOnboarding_CIDocFixture` — exec the onboarding tutorial in `docs/operator/rbac-onboarding.md` as a script (parses fenced shell-blocks, runs them sequentially); asserts every step exits 0 + the final assertion's expected output.

### PR body skeleton

````
## Summary

W8 T4 ships the default-deny baseline policy bundle (embed.FS) +
tenant onboarding tutorial + CI fixture per
docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md §3.5 + §6 T4.

- internal/authz/policies/embedded/regatta/v1/default/{approval,run}.rego
  — default-deny per action + one built-in HMAC-reviewer exception
  for Principal{Tenant: "default", ID: nonempty} per §3.5.
- internal/authz/policies/embedded/regatta/v1/default/data.json
  — static-facts placeholder (tenants override at write time).
- internal/authz/policies/embedded/embed.go — //go:embed
  regatta/v1/default + DefaultBundleSHA256 string computed at
  package init.
- docs/operator/rbac-onboarding.md — tenant onboarding tutorial.
  Shell-block-fenced steps consumed by tests/e2e/authz/onboarding_test.go.
- tests/e2e/authz/onboarding_test.go — CI-executable onboarding
  fixture (A-tier rubric pin per spec §7 A).

Default-deny baseline ships in the binary — new single-tenant
deployments require ZERO per-tenant bootstrap config (default bundle
covers them with the HMAC-reviewer exception per §3.5).

## Why

MVP-3 W8 Task T4. The default-deny baseline closes R6 ambiguity:
existing single-tenant deployments keep working with zero operator
action; multi-tenant deployments opt in by writing a policy_revision
event with a tenant-specific bundle.

## Test plan

- [x] B-tier: TestDefaultBundleSHA256_Stable,
       TestDefaultBundle_HMACReviewer_AllowsApprovalDecide.
- [x] A-tier: TestTenantOnboardingFlow_FromEmptyToActive,
       TestTenantOnboarding_CIDocFixture.
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## A+ scorecard

<paste verbatim per feedback_a_plus_scorecard_required>

## Followup issues filed (per feedback_unaddressed_load_bearing)

- [w8-opa-rbac-followup] F3 per-policy UI editor + Rego playground (#NNN; spec §10 #3)

## Deletion default

embed.FS default-deny baseline ships in the binary — new
single-tenant deployments need ZERO per-tenant bootstrap config.
OPA off-the-shelf eliminates the entire custom-DSL design +
maintenance burden (per §3.2 rejected-alternative recording: no
parser, no evaluator, no debugger to ship).

```release-notes
[FEATURE] default-deny policy bundle + tenant onboarding tutorial + CI fixture (W8 T4 — single-tenant zero-config; multi-tenant via policy_revision substrate event)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w8-t4. Branch off main:
`git checkout -b feat/w8-t4-default-bundle main`.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md.
Read ALL of: §3.5 (bundle layout + Rego sample VERBATIM), §3.4.1
(default-deny + HMAC-reviewer exception), §6 T4 (named tests), §7
B/A rubric, §8 file disjoint row T4, §11 (OPA bundles upstream).

Per feedback_spec_pattern_authority: the default-deny Rego in spec
§3.5 is VERBATIM. If you want to add new default rules (e.g.
"allow run.view if Principal.Roles contains 'auditor'"), STOP and
report. The default bundle is intentionally minimal — tenants
override; defaults stay simple.

Per feedback_grade_rubric + feedback_a_plus_scorecard_required: PR
body MUST end with the A+ scorecard verbatim.

Per feedback_no_signatures + feedback_doc_check_banned_phrases: see
T1 prompt.

# Scope (exclusive write paths — file-disjoint with T1, T2, T3, T5)

- internal/authz/policies/embedded/regatta/v1/default/approval.rego  (NEW)
- internal/authz/policies/embedded/regatta/v1/default/run.rego       (NEW)
- internal/authz/policies/embedded/regatta/v1/default/data.json      (NEW; "{}" to start)
- internal/authz/policies/embedded/embed.go                          (NEW; //go:embed + DefaultBundleSHA256)
- internal/authz/policies/embedded/embed_test.go                     (NEW)
- docs/operator/rbac-onboarding.md                                   (NEW; tutorial)
- tests/e2e/authz/onboarding_test.go                                 (NEW; CI-executable script)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/authz/ root (T1's scope).
- Do NOT touch internal/authz/policies/ root (T2's scope) — your
  embedded/ directory is a sibling.
- Do NOT touch internal/web/ (T3's scope).
- Do NOT touch internal/authz/otel.go / audit.go / property_test.go
  / bench_test.go (T5's scope).
- Do NOT touch docs/operator/rbac.md (T5's scope — operator-facing
  RBAC reference doc; T4 owns the onboarding tutorial only).

# Output path slug (per feedback_plan_subagent_dup_files)

Branch: feat/w8-t4-default-bundle.
PR title: feat(w8): T4 default-deny policy bundle + tenant onboarding doc + CI fixture.

# Patterns to reuse (do NOT reinvent)

- //go:embed: standard Go pattern. Mirror the existing usage in
  cmd/regatta/init_assets/ (if present).
- Rego file structure: OPA upstream conventions. Reference
  https://www.openpolicyagent.org/docs/latest/policy-language/.
- T1 Authorizer + T2 policies.ActiveBundle: import; do NOT redefine.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test:
  1. Write test first.
  2. Run `go test ./internal/authz/policies/embedded/ ./tests/e2e/authz/ -run <TestName> -v`.
  3. CAPTURE failing output.
  4. Implement minimum.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (4 named; spec §6 T4 + §7 B/A)

B-tier:
1. TestDefaultBundleSHA256_Stable
2. TestDefaultBundle_HMACReviewer_AllowsApprovalDecide

A-tier:
3. TestTenantOnboardingFlow_FromEmptyToActive
4. TestTenantOnboarding_CIDocFixture

# Workflow after green

  1. Run `make pre-push-check` clean.
  2. Run `bash scripts/doc-check.sh` + `bash scripts/stale-todo.sh`
     — both MUST exit 0. The operator doc MUST NOT contain banned
     phrases (see token list in T1 prompt).
  3. Comments sweep.
  4. Push branch.
  5. File followup issue F3 (per-policy UI editor + playground)
     as `[w8-opa-rbac-followup]`-prefixed; gather number.
  6. Open PR via `gh pr create --base main --body-file <path>`. PR
     body MUST end with the ` ```release-notes\n[FEATURE] ...\n``` `
     fence; grep-verify before push.
  7. Spawn ONE adversarial reviewer subagent with hunt list:
       - default-deny Rego matches spec §3.5 VERBATIM. Diff the
         file vs the spec excerpt.
       - HMAC-reviewer exception ONLY when Tenant=="default" AND
         ID!="". Verify via the Rego rule.
       - //go:embed scope is regatta/v1/default — no tenant
         bundles ever in the embed.FS.
       - DefaultBundleSHA256 is computed at package init over
         canonical-JSON of the Rego files (sorted keys). Verify.
       - Onboarding doc has executable shell blocks (CI runs them).
         No prose-only steps.
       - Onboarding doc has NO banned phrases.
       - No AI signatures.
       - Comments discipline.
  8. Apply findings; re-run pre-push-check + doc-check + stale-todo;
     force-push.
  9. Verify CI green; flip automerge.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 3 of the 4 tests.
- 1 followup issue number filed.
- Adversarial reviewer verdict.
- A+ scorecard.
- One-line diff stat.

Begin now. NEVER pause for user input.
```

---

## §6 Task T5 — OTel attrs + audit event + property tests + operator doc + Makefile target

### Scope

- **`internal/authz/otel.go`** — NEW. `setAuthzAttrs(span trace.Span, decision Decision, tenant string, action Action, evalMicros int64)` setter that attaches the 6 `regatta.authz.*` attrs per spec §3.7 verbatim:
  - `regatta.authz.tenant` (string)
  - `regatta.authz.action` (string; enum of 4 values)
  - `regatta.authz.decision` (`"allow"` / `"deny"`)
  - `regatta.authz.reason` (string; clamped at 256 chars per R10)
  - `regatta.authz.policy_revision` (8-char SHA prefix per R7)
  - `regatta.authz.eval_micros` (int64; OPA eval latency)
- **`internal/authz/audit.go`** — NEW. `KindAuthzDenied substrate.EventKind = "authz_denied"` constant + `AuthzDeniedPayload` struct (`PrincipalID`, `Tenant`, `Action`, `Resource`, `PolicyRevision` (full SHA — not 8-char prefix; cardinality OK in substrate row per R7), `Reason`) + `init()` registers via `substrate.RegisterPayloadValidator(KindAuthzDenied, validateAuthzDenied)`. Exports `AppendDenied(ctx context.Context, dec Decision, p web.Principal, a Action, r Resource) error` for T3 handler call.
- **`internal/authz/property_test.go`** — NEW. `pgregory.net/rapid`-based property test. ≥ 5 000 cases of random `Principal{Tenant: A}` + URL `/approve/<B>/...` for `B ≠ A` → assert every attempt renders `tenant_mismatch` page. Pins A+ rubric.
- **`internal/authz/bench_test.go`** — MUTATE (T1 already shipped `BenchmarkAuthorizerCheck_p99Under200Micros`; T5 adds `BenchmarkAuthzCheck_WithAuditAndOTel_p99Under250Micros`). Full-stack budget: Check + setAuthzAttrs + (on deny) AppendDenied ≤ 250 µs p99.
- **`Makefile`** — MUTATE. Add `e2e-authz-onboarding` target that runs `go test ./tests/e2e/authz/... -tags=e2e -v` (T4 owns the fixture; T5 owns the Makefile entry point). Per `feedback_dispatch_strategy` decision priority: target exists so operators can verify onboarding locally without diffing the source tree.
- **`cmd/regatta/serve.go`** — MUTATE. One-line addition: `if err := authz.NewOPAAuthorizer(...).Hydrate(ctx); err != nil { return fmt.Errorf("authz hydrate: %w", err) }` at serve startup, BEFORE the HTTP listener binds. Net diff ≤ 6 LoC (declaration + error wrap + handler-wiring closure capturing the authorizer).
- **`docs/operator/rbac.md`** — NEW. Operator-facing RBAC reference doc per spec §10 + §11. Covers: the 4 actions; tenant onboarding (with link to T4's `rbac-onboarding.md`); the 4 sentinel error pages (`denied`, `tenant_mismatch`, `policy_missing`, `policy_eval_error`); the 6 OTel attributes (what they mean + when to alert); the audit event kind (`authz_denied`); the default-deny semantics; the legacy-redirect rollout note.

### Prereqs (cite spec sections)
- Spec §3.6 — request flow + audit row emission on deny.
- Spec §3.7 — OTel attribute set verbatim + cardinality discipline (R7 8-char SHA prefix).
- Spec §5 R7 — `policy_revision` cardinality mitigation (8-char prefix as OTel attr).
- Spec §5 R10 — reason-string length clamp + slug-shape lint (lint deferred to followup F8; T5 owns the 256-char clamp).
- Spec §6 T5 — named test list.
- Spec §7 A+ — property test ≥ 5 000 cases; OPA decision-log replay; cosign bundle signing (A+ #3 deferred to followup F1; T5 owns the property test + the eval-latency budget).
- Spec §10 — followup-issue list (T5 cites all 10 issue numbers in PR body; reuses numbers filed by T1-T4 + files the remaining ones).

### Existing patterns to reuse (do NOT reinvent)
- **W6 attribute setter pattern:** `internal/obs/otel/` existing helpers. Mirror the shape (one function per attribute group; defensive nil-span guard).
- **substrate.RegisterPayloadValidator + AppendEvent:** for the `KindAuthzDenied` audit row write.
- **`rapid.Check`:** existing pattern from substrate W1 T-S3's `cycle_property_test.go`. Reference for the seed-deterministic-across-CI invariant.
- **W6 tracer factory:** `cfg.Tracer` fallback to `otel.Tracer(...)` per W6 T5 normalization.

### TDD test list

**B-tier (3 named — spec §6 T5 verbatim):**

1. `TestAuthzCheck_EmitsAllAttributes` — wrap `Authorizer.Check` in a test span; call Check; recorded span has all 6 `regatta.authz.*` attrs present.
2. `TestAuthzCheck_PolicyRevisionAttr_Is8CharPrefix` — `len(attrs["regatta.authz.policy_revision"]) == 8`. Pins R7 cardinality guard.
3. `TestAuthzCheck_DeniedAuditEventAppended` — Check returns `Decision{Allow: false}`; substrate has one `kind='authz_denied'` row with payload matching the deny shape.

**A-tier (2 named — spec §6 T5 verbatim + §7 A):**

4. `TestAuthzCheck_AllowDecision_NoAuditRow` — Check returns `Decision{Allow: true}`; substrate has ZERO `kind='authz_denied'` rows. Pins the "audit only on deny" invariant (no audit-row write amplification on the happy path).
5. `BenchmarkAuthzCheck_WithAuditAndOTel_p99Under250Micros` — full-stack budget: Check + setAuthzAttrs + (on deny) AppendDenied ≤ 250 µs p99. PR body MUST paste benchstat output.

**A+ tier (2 named — spec §6 A+ + §7 A+):**

6. `TestPropertyTenantBinding_RandomMismatchAlwaysRendersTenantMismatch` — rapid.Check; ≥ 5 000 cases of random `Principal{Tenant: A}` + URL `/approve/<B>/...` where `B ≠ A` → every attempt renders `tenant_mismatch` page; zero `false-negative`. Pins R5 + spec §7 A+ #1.
7. `TestAuthzReason_LengthClampedAt256Chars` — synthetic Rego whose `reason` binding returns a 500-char string → OTel attr `regatta.authz.reason` value is exactly 256 chars (clamped). Pins R10 mitigation.

### PR body skeleton

````
## Summary

W8 T5 wires OTel attrs + audit event + property tests + operator doc
+ Makefile target per docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md
§3.6 + §3.7 + §6 T5.

- internal/authz/otel.go — setAuthzAttrs attaches the 6
  regatta.authz.* attrs verbatim per spec §3.7. policy_revision is
  8-char SHA prefix per R7. reason clamped at 256 chars per R10.
- internal/authz/audit.go — KindAuthzDenied substrate event kind +
  AuthzDeniedPayload + init() registering via T-S1 #224's
  RegisterPayloadValidator. AppendDenied called from T3's handler
  on every deny.
- internal/authz/property_test.go — rapid.Check ≥ 5 000 cases of
  random principal-tenant binding → every B ≠ A renders
  tenant_mismatch.
- internal/authz/bench_test.go — full-stack p99 ≤ 250 µs.
- Makefile — e2e-authz-onboarding target runs T4's CI fixture.
- cmd/regatta/serve.go — one-line authz.NewOPAAuthorizer(...).Hydrate
  at serve startup; ≤ 6 LoC delta.
- docs/operator/rbac.md — operator-facing RBAC reference doc.

## Why

MVP-3 W8 Task T5. The OTel + audit + property test layer makes W8
observable + falsifiable. The 8-char SHA prefix attr closes the R7
cardinality risk; the property test closes the R5 cross-tenant
cookie leakage risk at ≥ 5 000-case confidence.

## Test plan

- [x] B-tier: TestAuthzCheck_EmitsAllAttributes,
       TestAuthzCheck_PolicyRevisionAttr_Is8CharPrefix,
       TestAuthzCheck_DeniedAuditEventAppended.
- [x] A-tier: TestAuthzCheck_AllowDecision_NoAuditRow,
       BenchmarkAuthzCheck_WithAuditAndOTel_p99Under250Micros.
- [x] A+ tier: TestPropertyTenantBinding_RandomMismatchAlwaysRendersTenantMismatch,
       TestAuthzReason_LengthClampedAt256Chars.
- [x] make pre-push-check clean.
- [x] make e2e-authz-onboarding clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Benchstat output

<paste from `go test -bench BenchmarkAuthzCheck -count=10`>

## A+ scorecard

<paste verbatim per feedback_a_plus_scorecard_required>

## Followup issues filed / referenced (per feedback_unaddressed_load_bearing)

PR body lists all 10 W8 followup issues (numbers carried forward
from T1-T4 PRs + new from T5):
- [w8-opa-rbac-followup] F1 cosign bundle signing (#NNN; filed by T1)
- [w8-opa-rbac-followup] F2 dynamic policy reload (#NNN; filed by T2)
- [w8-opa-rbac-followup] F3 per-policy UI editor + playground (#NNN; filed by T4)
- [w8-opa-rbac-followup] F4 OPA Wasm runtime (#NNN; filed by T1)
- [w8-opa-rbac-followup] F5 opa test harness in make check (#NNN; filed by T2)
- [w8-opa-rbac-followup] F6 CLI service-token + multi-tenant CLI (#NNN; T5 file)
- [w8-opa-rbac-followup] F7 emergency rollback CLI (#NNN; T5 file)
- [w8-opa-rbac-followup] F8 reason-string slug-shape lint (#NNN; T5 file)
- [w8-opa-rbac-followup] F9 W8.2 legacy 301 redirect removal (#NNN; filed by T3)
- [w8-opa-rbac-followup] F10 Postgres backend (#NNN; T5 file)

## Deletion default

OTel `policy_revision` attr clamped to 8-char SHA prefix — closes
R7 cardinality blow-up without a separate "cardinality regression
alarm" event kind. Reuses W6 attribute infra; no new tracer factory.

```release-notes
[FEATURE] OPA RBAC OTel attrs + authz_denied substrate audit + property test (≥ 5 000 cases) + operator doc + e2e-authz-onboarding Makefile target (W8 T5 — closes MVP-3 W8)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w8-t5. Branch off main AFTER T1 + T2 + T3 +
T4 are ALL merged:
`git fetch origin && git checkout -b feat/w8-t5-otel-audit-property main`.

If any of T1-T4 is not yet on main, STOP — T5 is hard-blocked.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md.
Read ALL of: §3.6 (request flow + audit on deny), §3.7 (OTel attrs
VERBATIM), §5 R7 + R10 + R11 (cardinality cap; reason length; bundle
SHA stability), §6 T5 (named test list), §7 B/A/A+ rubric, §8 file
disjoint row T5, §10 (followup list).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (6 OTel attrs verbatim names; 8-char SHA
prefix for policy_revision; 256-char clamp for reason; KindAuthzDenied
via RegisterPayloadValidator — do NOT modify substrate's validate.go;
audit-row write ONLY on deny — not on allow; property test ≥ 5 000
cases via rapid.Check), STOP and report — do NOT pick an alternative
yourself. Re-spawn the design subagent.

Per feedback_grade_rubric + feedback_a_plus_scorecard_required: PR
body MUST end with the A+ scorecard verbatim.

Per feedback_no_signatures + feedback_doc_check_banned_phrases +
feedback_comments_discipline: see T1 prompt.

# Scope (exclusive write paths — file-disjoint with T1, T2, T3, T4)

- internal/authz/otel.go                       (NEW; setAuthzAttrs)
- internal/authz/audit.go                      (NEW; KindAuthzDenied + AuthzDeniedPayload + AppendDenied + init())
- internal/authz/property_test.go              (NEW; rapid ≥ 5 000 cases)
- internal/authz/bench_test.go                 (MUTATE — T1 shipped the perf gate; T5 adds the with-audit-and-otel benchmark)
- internal/authz/otel_test.go                  (NEW; B-tier 3 + A-tier 1)
- internal/authz/audit_test.go                 (NEW; A-tier 1 + A+ 1)
- Makefile                                     (MUTATE — add e2e-authz-onboarding target; ≤ 4 LoC)
- cmd/regatta/serve.go                         (MUTATE — one-line Hydrate call; ≤ 6 LoC)
- docs/operator/rbac.md                        (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/authz/{authz,opa,store,ctx}.go (T1's scope).
- Do NOT touch internal/authz/policies/ (T2's scope).
- Do NOT touch internal/authz/policies/embedded/ (T4's scope) —
  import the FS + the constant.
- Do NOT touch internal/web/ (T3's scope) — T3 calls audit.AppendDenied
  (stub or real depending on landing order). T5's PR provides the
  real implementation; if T3's PR already merged with the stub, the
  stub is replaced by T5's real function (NO signature change).
- Do NOT touch docs/operator/rbac-onboarding.md (T4's scope) — T5
  owns the reference doc; T4 owns the tutorial.

# Output path slug (per feedback_plan_subagent_dup_files)

Branch: feat/w8-t5-otel-audit-property.
PR title: feat(w8): T5 OPA RBAC OTel attrs + authz_denied audit + property test + operator doc + Makefile target.

# Patterns to reuse (do NOT reinvent)

- W6 attribute setter: internal/obs/otel/ helpers.
- substrate.RegisterPayloadValidator + AppendEvent: T-S1 #224.
- rapid.Check seed-deterministic pattern: substrate W1 T-S3's
  cycle_property_test.go.
- W6 tracer factory: cfg.Tracer fallback.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test:
  1. Write test first.
  2. Run `go test ./internal/authz/ -run <TestName> -v`.
  3. CAPTURE failing output.
  4. Implement minimum.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (7 named; spec §6 T5 + §7 B/A/A+)

B-tier:
1. TestAuthzCheck_EmitsAllAttributes
2. TestAuthzCheck_PolicyRevisionAttr_Is8CharPrefix
3. TestAuthzCheck_DeniedAuditEventAppended

A-tier:
4. TestAuthzCheck_AllowDecision_NoAuditRow
5. BenchmarkAuthzCheck_WithAuditAndOTel_p99Under250Micros

A+ tier:
6. TestPropertyTenantBinding_RandomMismatchAlwaysRendersTenantMismatch (≥ 5 000 cases)
7. TestAuthzReason_LengthClampedAt256Chars

# Workflow after green

  1. Run `make pre-push-check` clean.
  2. Run `make e2e-authz-onboarding` clean (validates T4's fixture
     via the new target).
  3. Run `bash scripts/doc-check.sh` + `bash scripts/stale-todo.sh`
     — both MUST exit 0. docs/operator/rbac.md MUST NOT contain
     banned phrases.
  4. Comments sweep.
  5. Push branch.
  6. File followup issues F6 (CLI service-token), F7 (emergency
     rollback CLI), F8 (reason-string slug-shape lint), F10
     (Postgres backend) as `[w8-opa-rbac-followup]`-prefixed; gather
     numbers. Carry forward F1, F2, F3, F4, F5, F9 from T1-T4 PRs.
  7. Open PR via `gh pr create --base main --body-file <path>`. PR
     body MUST end with the ` ```release-notes\n[FEATURE] ...\n``` `
     fence; grep-verify before push. PR body MUST list ALL 10
     followup issue numbers.
  8. Spawn ONE adversarial reviewer subagent with hunt list:
       - OTel attr names EXACT match to spec §3.7. Diff names.
       - policy_revision is 8-char SHA prefix in OTel; full SHA in
         the audit row (per R7).
       - reason clamped at 256 chars; verify the clamp is on the
         OTel attr value, not on the Decision.Reason field.
       - KindAuthzDenied registered via RegisterPayloadValidator
         from init() in audit.go — NOT a substrate validate.go edit.
       - AppendDenied called only on Decision.Allow == false;
         no audit row on allow path.
       - property test ≥ 5 000 cases via rapid.Check seed-deterministic
         across CI runs.
       - Bench p99 ≤ 250 µs at N=10 000.
       - cmd/regatta/serve.go diff ≤ 6 LoC; Hydrate failure surfaces
         as fatal serve error.
       - docs/operator/rbac.md has NO banned phrases; doc-check.sh
         clean.
       - All 10 followup issue numbers cited in PR body.
       - No AI signatures.
       - Comments discipline.
  9. Apply findings; re-run pre-push-check + e2e-authz-onboarding +
     doc-check + stale-todo; force-push.
 10. Verify CI green; flip automerge.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 4 of the 7 tests.
- Pasted benchstat output for the with-audit-and-otel benchmark.
- 4 newly-filed followup issue numbers (F6, F7, F8, F10) + 6
  carried forward (F1, F2, F3, F4, F5, F9).
- Adversarial reviewer verdict.
- A+ scorecard.
- One-line diff stat.

Begin now. NEVER pause for user input.
```

---

## §7 Followup issue templates (pre-enumerated; spec §10)

Per `feedback_unaddressed_load_bearing` + `feedback_followup_filing_universal`: every load-bearing deferred item in spec §10 is filed as a `[w8-opa-rbac-followup]`-prefixed gh issue PRE-MERGE; PR bodies cite by number. T1 files F1 + F4; T2 files F2 + F5; T3 files F9; T4 files F3; T5 files F6 + F7 + F8 + F10. **All 10 issues MUST be open before T5's PR opens.**

| # | Title | Owner-PR | Spec ref |
|---|---|---|---|
| F1 | `[w8-opa-rbac-followup] Policy bundle signing via cosign / sigstore` | T1 | §10 #1 |
| F2 | `[w8-opa-rbac-followup] Dynamic policy reload via fs watcher or admin endpoint` (note: T2's PR ships migration #0007 — kind CHECK + 1 MiB cap + tenant_id index — and `substrate.FoldByTenant` T-S1 followup helper; F2 builds atop both) | T2 | §10 #2 |
| F3 | `[w8-opa-rbac-followup] Per-policy UI editor + Rego-aware playground (depends on W7.4 UI scope expansion)` | T4 | §10 #3 |
| F4 | `[w8-opa-rbac-followup] OPA Wasm runtime for ~10x eval speedup` | T1 | §10 #4 |
| F5 | `[w8-opa-rbac-followup] opa test harness in make check (lints tenant bundles in CI)` | T2 | §10 #5 |
| F6 | `[w8-opa-rbac-followup] CLI service-token + multi-tenant CLI (closes R9; mirrors approval cookie HMAC shape)` | T5 | §10 #6 |
| F7 | `[w8-opa-rbac-followup] Emergency rollback CLI: regatta authz rollback --tenant <id> --revision <sha> (R12 mitigation)` | T5 | §10 #7 |
| F8 | `[w8-opa-rbac-followup] Reason-string slug-shape lint (R10 mitigation: regex(/^(?:[a-z0-9_-]+\.){0,3}[a-z0-9_-]+$/, reason))` | T5 | §10 #8 |
| F9 | `[w8-opa-rbac-followup] W8.2 legacy /approve/<approval_id> 301-redirect removal (clean-up; post-rollout window)` | T3 | §10 #9 |
| F10 | `[w8-opa-rbac-followup] Postgres backend (OPA store stays in-process; policies fold reads against Postgres substrate_events)` | T5 | §10 #10 |

Each issue body template:

```
## Source
Spec: docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md §10 #<N>.
Filed as a pre-merge followup per feedback_unaddressed_load_bearing.

## Scope
<one-paragraph extract from spec §10>

## Acceptance
<bullets per the spec-referenced mitigation; one A-tier-grade test named>

## Out of W8 scope
<one sentence: why deferred; what risk it closes when landed>
```

---

_Plan authority: this plan is a dispatch artifact only. The main session copy-pastes the §2/§3/§4/§5/§6 dispatch prompts into Agent tool calls AFTER each task's listed prereqs are merged. NO implementation, NO commit from this file._
