# MVP-4 W12 Billing — Implementer Task Breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-w12-billing-design.md` (#280, merged).
Authority: `feedback_spec_pattern_authority` — implementer deviation from any spec-mandated pattern (T1 owns the `BillingPeriodClosedPayload` + `LineItem` typed structs + the `rollup.Run` signature per spec §3.2 + §3.7; T2 owns the `stripe-go/v76` adapter shape + idempotency key derivation `sha256(tenant_id || period_start_unix)` + `UsageRecordActionSet` semantics + quantity-in-cents int64 per spec §3.3; T3 owns the markdown template + invoice file path convention per spec §3.4; T4 owns the close ritual ordering + three-layer idempotency per spec §3.5; T5 owns the `/billing` route + read-only invariant + substrate-as-source-of-truth per spec §3.6; T6 owns the `billing.*` span namespace + operator doc per spec §3.8 + §1 goal #6) MUST re-spawn the design subagent. NO implementer-chosen alternatives.

Decision priority for every decision below (`feedback_decision_priority`): **UX → ease of use → best practices → execution speed → velocity**. Grade rubric (`feedback_grade_rubric`) inherited verbatim from spec §7 — each task carries spec B / A / A+ tool-checkable criteria.

---

## Wave overview

- **6 file-disjoint implementer tasks** (T1 — rollup; T2 — Stripe adapter; T3 — invoice render; T4 — `billing close` CLI; T5 — operator UI tab; T6 — OTel + docs) per spec §8.
- **Wave A (parallel, 4 lanes):** T1 + T2 + T3 + T4 dispatch concurrently off `main`. All four are file-disjoint, and the cross-task seam (T4 importing T1's `rollup.Run` + `Rollup` struct + T2's `stripe.Adapter` + T3's `invoice.Render`) is contract-frozen at spec §3.2 + §3.3 + §3.4 — implementer-test for T4 stubs the three sibling APIs and rebases once they land. Per `feedback_parallel_dup_followups`, shared followups (Stripe SDK refresh runbook D9, PDF rendering D4) are pre-filed by the main session BEFORE dispatch.
- **Wave B (sequenced, single lane):** T5 dispatches AFTER W7 Operator Web UI Wave 1 (T4–T7) lands on main AND T4 (billing-close CLI) merges. T5's templates + handler import the W7 cookie-HMAC middleware + `embed.FS` template loader; T5 reads `billing_period_closed` events emitted by T4. Both prereqs are hard.
- **Wave C (sequenced, single lane):** T6 dispatches AFTER T1–T5 merge. T6 owns the operator doc (`docs/operator/billing.md`) that references every implementation behaviour; doc-only PR, skip ceremony per `feedback_review_proportional` (one adversarial-reviewer pass; no scorecard required for the doc PR itself).
- **Prereqs (merged to main):**
  - Cost Governor Wave 2 #246 + T3 + T4 — the `budget_reconciled` event emitter + reconciler. W12's rollup is a pure consumer; without these events the rollup is empty.
  - W7 Operator Web UI Wave 1 — specifically T4–T7 (HTTP listener #263 already merged; templates + cookie-HMAC middleware + route registration assumed merged before T5 dispatches; main session verifies pre-Wave-B).
  - Unified Substrate v2 #224 — `AppendEvent` + `RegisterPayloadValidator` open-extension hook. Already merged.
- **Sequence vs parallel:** T1 + T2 + T3 + T4 file-disjoint; T4 cross-imports T1/T2/T3 — use the shared-primitive-owner pattern: T1 lands payload.go first (committed as the first commit of T1's PR + pushed; T4 fetches the file via `git fetch origin/feat/w12-t1-rollup -- internal/billing/event/payload.go` once T1 pushes). Falls back to local test-only stub if T1 has not pushed by the time T4 starts. Per `feedback_sequence_dependent_work`: file-disjoint AND struct-contract-frozen ⇒ PARALLEL is safe.
- **Migration phasing (`feedback_migration_number_lock`):** **ZERO new SQL migrations across all six tasks.** W12 writes one new substrate event kind (`billing_period_closed`) registered via the `RegisterPayloadValidator` open-extension hook in T1 — same pattern cost-governor used for `token_spend` + `budget_reconciled`. Migration #0008 (or next available) remains reserved for a future spec. Confirmed: `ls migrations/ | wc -l` must read identical between PR-branch and main for every W12 PR.
- **Concurrency cap (`feedback_session_limit_dispatch`):** Wave A is 4 parallel implementers. Within the 10-lane ceiling per `feedback_dispatch_strategy` (max velocity); well under the 3-4 mid-band ceiling, but still safe — every task is single-package, single-PR, no two-PR sequences. Wave B + C are single implementer each.
- **Deletion default (`feedback_deletion_default`):** Every W12 PR body answers "what got smaller?" — concrete enumerations below per task. Wedge-level deletions per spec §3.9:
  1. **Stripe SDK adoption (T2) ELIMINATES bespoke payment-processing code.** No custom HTTP client, no HMAC signing, no retry FSM written from scratch — the SDK encapsulates all of it.
  2. **Substrate event kind (T1, T4) ELIMINATES a parallel `billing_invoices` table.** No migration, no foreign-key relation, no separate fold path, no backfill recipe. Substrate is the single store.
  3. **Markdown-only invoice v1 (T3) ELIMINATES a PDF-renderer dependency.** No `gofpdf` vendored, no headless-Chrome runtime, no font-asset shipping. Markdown is the falsifiable artifact; PDF deferred to D4.
  4. **CLI-only close (T4) ELIMINATES a UI close button.** No double-submit guard, no idempotency token cookie, no rate-limited POST handler. CLI inside the operator audit envelope.
  5. **Read-only UI tab (T5) ELIMINATES `tenant_id` cookie claim leakage risk.** Pre-W8 the route is operator-only; no per-tenant authentication primitive added — reuses W7 cookie HMAC verbatim.
  6. **No webhook ingress in v1 (deferred to D5).** No `/billing/webhook` handler, no Stripe-event-id dedup table, no dispute-routing FSM. Push-only.
- **Followup filing (`feedback_followup_filing_universal` + `feedback_unaddressed_load_bearing`):** Every load-bearing named-but-deferred item in spec §1 non-goals + §10 deferred list is filed as a `[billing-followup]` issue PRE-MERGE. PR body cites issue numbers. Spec A+3 rubric requires `gh issue list --label billing-followup --state open` ≥ 6 at the FIRST W12 PR merge time. Pre-filed by main session before Wave A dispatch (so every Wave A PR can cite the existing issue numbers): D1 multi-currency, D2 proration, D3 contestation UI, D4 PDF, D5 webhook ingress, D6 OpenMeter, D7 tenant self-serve, D8 backfill recipe, D9 SDK refresh runbook, D10 tax computation. Ten followups → A+3 satisfied with margin.

---

## §1 File-disjoint table

| Task | Path (exclusive write scope) | Depends-on | Effort | TDD tests (count: tier breakdown) |
| ---- | --------------------------- | ---------- | ------ | --------------------------------- |
| **T1 — Rollup job** | `internal/billing/rollup/rollup.go` (NEW); `internal/billing/rollup/rollup_test.go` (NEW); `internal/billing/event/payload.go` (NEW: `BillingPeriodClosedPayload` + `LineItem` shared-primitive structs per spec §3.7); `internal/billing/event/payload_test.go` (NEW); `internal/orchestrator/state/substrate/validate.go` (additive `init()` block registering `KindBillingPeriodClosed` via `RegisterPayloadValidator` open-extension hook; ≤ 30 LoC delta — NO change to any existing validator) | Cost-gov W2 merged (provides `budget_reconciled` events); substrate v2 #224 merged | M | 6 named (B 3, A 2, A+ 1) per spec §6 T1 |
| **T2 — Stripe adapter + idempotency** | `internal/billing/stripe/adapter.go` (NEW); `internal/billing/stripe/adapter_test.go` (NEW); `go.mod` + `go.sum` (vendor `github.com/stripe/stripe-go/v76`); `vendor/` updates if vendor mode is used | None (vendoring SDK is the only delta) | M | 6 named (B 3, A 2, A+ 1) per spec §6 T2 |
| **T3 — Invoice markdown template** | `internal/billing/invoice/render.go` (NEW); `internal/billing/invoice/template.md.tmpl` (NEW; spec §3.4 verbatim); `internal/billing/invoice/render_test.go` (NEW); `internal/billing/invoice/testdata/golden_invoice_*.md` (NEW; golden-file fixtures for byte-equal assertions) | T1 `Rollup` struct shape locked (spec §3.2 verbatim; can stub locally then rebase) | S | 5 named (B 3, A 1, A+ 1) per spec §6 T3 |
| **T4 — `billing close` CLI** | `cmd/regatta/billing.go` (NEW; subcommand registration + flag parsing + ritual driver); `cmd/regatta/billing_test.go` (NEW); `cmd/regatta/root.go` (additive: one-line subcommand registration; ≤ 5 LoC delta) | T1 + T2 + T3 merged (or shared-primitive-owner pull) | M | 6 named (B 3, A 2, A+ 1) per spec §6 T4 |
| **T5 — Operator UI billing tab** | `internal/web/billing.go` (NEW; HTTP handler + fold-read of `billing_period_closed`); `internal/web/billing_test.go` (NEW); `internal/web/templates/billing.tmpl` (NEW; spec §3.6 verbatim); `internal/web/routes.go` (additive: two route registrations `/billing` + `/billing/{tenant_id}/{period}`; ≤ 6 LoC delta) | W7 Wave 1 (T4–T7) merged; T4 merged (event shape stabilized) | M | 6 named (B 3, A 2, A+ 1) per spec §6 T5 |
| **T6 — OTel + docs** | `docs/operator/billing.md` (NEW; close ritual + Stripe runbook + invoice-file location convention + refund reflection note per R5 + period-close timing per R7 + Stripe Price configuration per R6); span-naming verification (no code edits in T6 — span emission happens inside T1+T4 code under T1+T4 scope) | T1–T5 merged | S | 5 named (B 2, A 2, A+ 1) per spec §6 T6 |

**Disjointness verification (run by main session at plan time + by each implementer pre-PR):**
- T1 writes only to `internal/billing/rollup/` + `internal/billing/event/` + the additive `validate.go` init block. T1 does NOT touch `internal/billing/stripe/`, `internal/billing/invoice/`, `cmd/regatta/`, `internal/web/`.
- T2 writes only to `internal/billing/stripe/` + `go.mod` + `go.sum` (+ `vendor/` if vendor mode). T2 does NOT touch `internal/billing/rollup/`, `internal/billing/event/`, `internal/billing/invoice/`, `cmd/regatta/`, `internal/web/`.
- T3 writes only to `internal/billing/invoice/`. T3 does NOT touch any other package.
- T4 writes only to `cmd/regatta/billing.go` + `cmd/regatta/billing_test.go` + the ≤ 5 LoC additive registration in `cmd/regatta/root.go`. T4 imports T1 + T2 + T3 but writes NO code into those packages.
- T5 writes only to `internal/web/billing.go` + `internal/web/billing_test.go` + `internal/web/templates/billing.tmpl` + the ≤ 6 LoC additive route registration in `internal/web/routes.go`. T5 does NOT modify any existing W7 handler.
- T6 writes only to `docs/operator/billing.md`. T6 verifies the spans named in spec §3.8 are emitted by T1/T4 code (which is T1/T4's PR scope, not T6's) and files a tracking issue if any span is missing.
- T1 + T2 + T3 + T4 share `internal/orchestrator/state/substrate/validate.go` ONLY via T1's additive `init()` block. T2/T3/T4 do NOT touch validate.go.
- Pre-merge audit: every Wave-A implementer runs `git diff --name-only origin/main` and verifies their path-set is a subset of the row above.

**Cross-task seam contracts (load-bearing — implementer MUST honour exactly):**

- **T1 exports** (consumed by T3 + T4 + T5):
  - `rollup.Rollup` struct (spec §3.2 verbatim — `TenantID`, `PeriodStart`, `PeriodEnd`, `TotalUSD`, `LineItems`).
  - `rollup.LineItem` struct (spec §3.2 verbatim — `PeriodStart`, `PeriodEnd`, `ActualUSD`, `ModelBreakdown []cost.ModelBreakdownRow`).
  - `rollup.Scope` input struct: `TenantID string`, `PeriodStart time.Time`, `PeriodEnd time.Time`.
  - `rollup.Run(ctx context.Context, db *sql.DB, scope Scope) (Rollup, error)` — pure function, no side effects.
  - `event.BillingPeriodClosedPayload` struct (spec §3.7 verbatim — 8 fields incl. `TenantID`, `PeriodStart`/`End` unix-ms, `TotalUSD`, `LineItems`, `StripeUsageRecordID`, `IdempotencyKey`, `InvoiceFilePath`, `ClosedAt`).
  - `event.LineItem` struct for the payload (spec §3.7 verbatim — 4 fields).
  - `event.KindBillingPeriodClosed` substrate kind constant (string `"billing_period_closed"`).
  - Sentinel `rollup.ErrEmptyPeriod` for tenants with zero `budget_reconciled` rows (caller skips per spec §3.5 failure-mode table).
- **T2 exports** (consumed by T4):
  - `stripe.Adapter` struct + `stripe.NewAdapter(cfg Config) *Adapter`.
  - `stripe.Config`: `APIKey string`, `TenantMap map[string]string` (tenant_id → subscription_item_id), `HTTPClient *http.Client`, `Clock func() time.Time`.
  - `stripe.Adapter.PushUsage(ctx context.Context, r rollup.Rollup) (UsageRecordID, error)` — imports T1's `rollup.Rollup`.
  - `stripe.UsageRecordID` type (alias for `string`).
  - Sentinels: `stripe.ErrUnmappedTenant`, `stripe.ErrAPIKeyMissing`, `stripe.ErrUpstream5xx`, `stripe.ErrRateLimited`.
- **T3 exports** (consumed by T4):
  - `invoice.Render(r rollup.Rollup, outDir string, stripeRecordID stripe.UsageRecordID) (path string, err error)` — writes the markdown file; returns the path written. Imports T1's `rollup.Rollup` and T2's `stripe.UsageRecordID`.
  - `invoice.OutputPath(tenantID string, periodStart time.Time, outDir string) string` — pure helper (no I/O); returns `<outDir>/<tenant_id>/<YYYY-MM>.md`.
- **T4 exports** (consumed by T5):
  - NONE — T4's surface is the `regatta billing close` CLI. T5 reads `event.BillingPeriodClosedPayload` rows via substrate Fold, not via any T4-exported function.
- **T5 exports** (consumed by W7 router):
  - `web.RegisterBillingRoutes(mux *http.ServeMux, deps web.Deps)` — additive registration. Imports W7's existing `Deps` struct (DB, tracer, template loader, cookie-HMAC verifier).
- **T6 exports** NONE — doc-only.
- **Shared-primitive owner (`feedback_shared_primitive_owner`):** T1 OWNS every `event.*Payload` typed struct + the substrate-validate dispatch entry for `KindBillingPeriodClosed`. T2 + T3 + T4 + T5 do NOT redefine the payload struct, do NOT call `RegisterPayloadValidator`, do NOT add a parallel validator. T4 writes `BillingPeriodClosedPayload` rows via `substrate.AppendEvent` with the T1-owned struct's `json.Marshal` output as the payload bytes; T5 reads via `json.Unmarshal` into the same T1-owned struct.
- **T1 ↔ T4:** T1's `rollup.Run` is invoked from T4's CLI at T+4 of the close ritual (spec §3.5). T4 does NOT redefine `Rollup` / `LineItem` / `Scope`.
- **T2 ↔ T4:** T2's `Adapter.PushUsage(ctx, rollup)` is invoked from T4's CLI at T+9 of the close ritual (spec §3.5) AFTER substrate commit. T4 does NOT redefine `Adapter` / `Config` / `UsageRecordID`.
- **T3 ↔ T4:** T3's `Render(rollup, outDir, stripeRecordID)` is invoked from T4's CLI at T+7 (spec §3.5) AFTER substrate commit, BEFORE Stripe push (so first-rendered file has empty `StripeUsageRecordID`; T4 re-renders after Stripe push to fill the field, mirroring the substrate two-write LWW pattern). T4 does NOT redefine the template.
- **T5 ↔ T4 (substrate-only seam):** T5 reads `events.kind='billing_period_closed'` via substrate Fold; the canonical state lives in substrate, not on disk. T5 does NOT shell out to read the markdown file. The markdown file is a convenience render; substrate is the source of truth. (Spec §3.6 line 289 verbatim.)
- **No cyclic imports:** `internal/billing/event` depends only on `internal/cost` (for `ModelBreakdownRow`). `internal/billing/rollup` depends on `event`, `cost`, `substrate`. `internal/billing/stripe` depends on `event`, `rollup` (only the `Rollup` type), and `github.com/stripe/stripe-go/v76`. `internal/billing/invoice` depends on `rollup`, `stripe` (only the `UsageRecordID` type), `text/template`. `cmd/regatta/billing.go` depends on all four. `internal/web/billing.go` depends on `event`, `substrate`, NOT on `rollup`/`stripe`/`invoice`. Verified at plan time: `go list -deps` will show no `billing/*` → `cmd/regatta` or `billing/*` → `internal/web` edge.

---

## §2 Task T1 — Billing-period rollup job

### Scope

- **`internal/billing/event/payload.go`** — NEW. Typed payload structs per spec §3.7 lines 322-344 verbatim:
  - `BillingPeriodClosedPayload` — 8 fields (`TenantID`, `PeriodStart`, `PeriodEnd`, `TotalUSD`, `LineItems`, `StripeUsageRecordID`, `IdempotencyKey`, `InvoiceFilePath`, `ClosedAt`). JSON tags verbatim from spec.
  - `LineItem` — 4 fields (`PeriodStart`, `PeriodEnd`, `ActualUSD`, `ModelBreakdown []cost.ModelBreakdownRow`). Imports `internal/cost` for the shared row type (already exported by cost-gov W2 T3).
  - `KindBillingPeriodClosed` — string constant `"billing_period_closed"`. Registered in substrate kind enum via the validate-dispatch addition (string column per substrate §2 — no DDL).
- **`internal/billing/event/payload_test.go`** — NEW. JSON round-trip + field-validation tests.
- **`internal/billing/rollup/rollup.go`** — NEW. `rollup.Run(ctx, db, scope) (Rollup, error)` per spec §3.2 lines 122-142:
  1. Validate `scope` — `PeriodEnd > PeriodStart`, `TenantID` non-empty.
  2. Run the spec §3.2 SQL query verbatim (`SELECT json_extract(payload_json, '$.tenant_id') ... GROUP BY ...`) with the three bind parameters (`:period_start_ms`, `:period_end_ms`, `:tenant_id`).
  3. If zero rows returned: return `rollup.Rollup{TenantID: scope.TenantID, PeriodStart: scope.PeriodStart, PeriodEnd: scope.PeriodEnd, TotalUSD: 0, LineItems: nil}` AND `rollup.ErrEmptyPeriod` sentinel. Caller (T4) skips silently per spec §3.5 failure-mode line 274.
  4. For non-zero result: scan into `Rollup` struct. Then re-query the substrate for the individual `budget_reconciled` rows in the window (same scope; no GROUP BY) to populate `LineItems` per spec §3.2 line 120 — "Line items = the individual budget_reconciled rows that fed the sum."
  5. Return.
- **`internal/billing/rollup/rollup_test.go`** — NEW. 6 named tests (see test list below).
- **`internal/orchestrator/state/substrate/validate.go`** — additive `init()` block: `substrate.RegisterPayloadValidator(event.KindBillingPeriodClosed, validateBillingPeriodClosed)`. The validator unmarshals the payload, asserts `tenant_id` non-empty, `period_end > period_start`, `total_usd >= 0`, `idempotency_key` length == 64 (sha256 hex per spec §3.7 line 346). Net diff ≤ 30 LoC; NO change to existing validators.

### Prereqs (cite spec sections)

- Spec §3.2 lines 99-142 — rollup query + result type + `Run` signature **verbatim**.
- Spec §3.7 lines 315-346 — payload struct + validator dispatch table addition.
- Spec §3.5 lines 274-279 — failure-mode behaviour (empty-period skip; late bucket LWW-corrects on rerun).
- Spec §6 T1 lines 418-422 — named test list.
- Spec §7 B/A/A+ — applies to T1 (B6 = ZERO new migrations; A6 = `ls migrations/ | wc -l` unchanged).
- Spec §8 row 1 — file-disjoint scope.
- Spec §9 sequencing — T1 lands first in Wave A as the shared-primitive owner.

### Existing patterns to reuse (do NOT reinvent)

- **`substrate.RegisterPayloadValidator`** — T-S1 #224's open-extension hook. Pattern verbatim from cost-gov W2 T3 (`internal/orchestrator/state/substrate/validate.go::init()`).
- **`json_extract` SQL** — already used by cost-gov reconciler for `budget_reconciled` reads. Same WHERE-clause + GROUP BY shape.
- **`cost.ModelBreakdownRow`** — already exported by cost-gov W2 T3 (`internal/cost/spend/payload.go`). Import; do NOT redefine.
- **`substrate.DefaultTenantID`** — already exported. Use until W8 RBAC lands.
- **`cost.budget_reconciled`** — read-only consumer; never write this kind.

### TDD test list (named tests from spec §6 T1; failing-output capture step required)

Per `feedback_tdd_discipline`: implementer writes each test first, runs `go test ./<pkg>/ -run <name> -v`, **captures failing output (paste into PR body)**, then implements. "Tests would have failed" is NOT acceptable.

**B-tier (3 named tests — spec §6 T1):**

1. `TestRollup_SumsBudgetReconciledByTenant` — seed substrate with 3 `budget_reconciled` rows for tenant A + 2 for tenant B in the period window; `rollup.Run` scoped to tenant A returns `TotalUSD` = sum of tenant-A's 3 rows + `LineItems` length == 3.
2. `TestRollup_EmptyPeriodReturnsZero` — empty substrate (no `budget_reconciled` rows); `rollup.Run` returns `Rollup{TotalUSD: 0}` AND sentinel `rollup.ErrEmptyPeriod`.
3. `TestRollup_FiltersByTenantID` — seed rows for tenant A + tenant B; scope to tenant A; tenant B's rows excluded from `TotalUSD` and `LineItems`.

**A-tier (2 named tests — spec §6 T1):**

4. `TestRollup_LateReconciledRow_IncludedOnRerun` — first `Run` returns `TotalUSD = $X`; insert a new `budget_reconciled` row in the same window; second `Run` returns `TotalUSD = $X + $delta`. Pins R7 (reconciliation lag — re-run after late bucket lands).
5. `TestRollup_MultiBucketLineItems_OrderedByPeriodStart` — multiple `budget_reconciled` rows; `Rollup.LineItems` slice is sorted ASC by `PeriodStart`. Pins audit-readability invariant.

**A+-tier (1 named test — spec §6 T1):**

6. `TestRollup_ConcurrentReadsAreStable` — two goroutines call `rollup.Run` simultaneously against the same substrate state; both return byte-equal `Rollup` structs. Pins snapshot-isolation against substrate Fold.

**Adversarial-reviewer-added (1 named test, per `feedback_adversarial_review`):**

7. `TestSubstrate_BillingPeriodClosedPayloadValidates` — substrate validate dispatch table accepts well-formed payload; rejects malformed (missing required field → `ErrInvalidPayload`; `period_end <= period_start` → `ErrInvalidPayload`; `idempotency_key` wrong length → `ErrInvalidPayload`).

Total T1: **7 named tests** (3 B + 2 A + 1 A+ + 1 reviewer-added). PR body lists every test name + pasted failing-output excerpt for AT LEAST 5 representative cases.

### PR body skeleton

````
## Summary

MVP-4 W12 Billing T1 ships the rollup job + shared-primitive payload
structs + substrate validate-dispatch entry per spec §3.2 + §3.7.

- internal/billing/event/payload.go — BillingPeriodClosedPayload (8
  fields) + LineItem (4 fields) + KindBillingPeriodClosed const.
  Shared-primitive owner per feedback_shared_primitive_owner: T2 +
  T3 + T4 + T5 import; none redefine.
- internal/billing/rollup/rollup.go — Run(ctx, db, scope) pure read
  over budget_reconciled events. SQL query verbatim from spec §3.2;
  empty-period returns ErrEmptyPeriod sentinel.
- internal/orchestrator/state/substrate/validate.go — additive init()
  block registering KindBillingPeriodClosed via the
  RegisterPayloadValidator open-extension hook (T-S1 #224). NO new
  migration; NO change to existing validators.

## Why

Per spec §3.2: the rollup is the single SQL query that turns
cost-governor's already-reconciled actual_usd into a per-tenant total
without re-applying pricing. By owning the payload + validator as the
first Wave-A PR, T2 + T3 + T4 + T5 can land in parallel without
struct-shape drift.

## Test plan

- [x] TestRollup_SumsBudgetReconciledByTenant
- [x] TestRollup_EmptyPeriodReturnsZero
- [x] TestRollup_FiltersByTenantID
- [x] TestRollup_LateReconciledRow_IncludedOnRerun (R7)
- [x] TestRollup_MultiBucketLineItems_OrderedByPeriodStart
- [x] TestRollup_ConcurrentReadsAreStable (A+)
- [x] TestSubstrate_BillingPeriodClosedPayloadValidates (reviewer-added)
- [x] make pre-push-check clean.
- [x] ls migrations/ | wc -l identical to main (A6 rubric).

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 5 reps>

## A+ scorecard (per feedback_grade_rubric)

- B1 (doc-check): exit 0. Verify: bash scripts/doc-check.sh.
- B2 (stale-todo): exit 0. Verify: bash scripts/stale-todo.sh.
- A6 (zero new migrations): ls migrations/ unchanged.
- A1 (adversarial reviewer cleared): cite reviewer transcript hash.
- A+ (reviewer-added test): TestSubstrate_BillingPeriodClosedPayloadValidates.

## Deletion default

T1 ELIMINATES the temptation to ship a parallel billing_invoices
table: substrate event kind + JSON-extract reads handle the rollup
shape entirely. No migration, no foreign key, no backfill.

## Followup issues filed (per feedback_unaddressed_load_bearing)

Cites the 10 pre-filed [billing-followup] issues (D1-D10).

```release-notes
[FEATURE] billing-period rollup over budget_reconciled events (consumer-only; substrate kind billing_period_closed; default-off until billing.close CLI lands in T4)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w12-t1. Branch off main:

  git fetch origin
  git checkout -b feat/w12-t1-rollup origin/main

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w12-billing-design.md.
Read ALL of: §1 (goal #1 — per-tenant monthly USD rollup), §3.2
(rollup job — SQL query + Result type + Run signature VERBATIM),
§3.5 (close ritual — T4 consumes T1; failure-mode table line 274
empty-period skip), §3.7 (substrate hook — payload struct VERBATIM +
validator dispatch entry), §3.9 (deletion default — what got smaller),
§6 T1 (named test list), §7 B/A/A+ (B6 + A6 = ZERO new migrations),
§8 row 1 (file-disjoint scope), §9 (sequencing — T1 lands first as
shared-primitive owner).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (BillingPeriodClosedPayload 8 fields exactly;
LineItem 4 fields exactly; Run signature pure-function over substrate
no side effects; SQL uses json_extract on existing events table NO
new migration; ErrEmptyPeriod sentinel on zero-row result;
RegisterPayloadValidator open-extension hook for the validate
dispatch addition NO direct switch-case edit), STOP and report — do
NOT pick an alternative yourself. Re-spawn the design subagent.

# Scope (exclusive write paths — file-disjoint with T2/T3/T4/T5/T6)

- internal/billing/event/payload.go              (NEW)
- internal/billing/event/payload_test.go         (NEW)
- internal/billing/rollup/rollup.go              (NEW)
- internal/billing/rollup/rollup_test.go         (NEW)
- internal/orchestrator/state/substrate/validate.go  (additive init() block ONLY; ≤ 30 LoC; do NOT modify any existing validator)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/billing/stripe/ — T2's scope.
- Do NOT touch internal/billing/invoice/ — T3's scope.
- Do NOT touch cmd/regatta/ — T4's scope.
- Do NOT touch internal/web/ — T5's scope.
- Do NOT touch docs/operator/billing.md — T6's scope.
- Do NOT create a SQL migration. ZERO new migrations per spec §7 A6.

If you discover a missing seam in an out-of-scope file, STOP and
report — file a tracking issue per finding; do NOT edit out of scope.

# Patterns to reuse (do NOT reinvent)

- substrate.RegisterPayloadValidator: T-S1 #224 open-extension hook.
  Use init() block pattern verbatim from cost-gov W2 T3
  (internal/orchestrator/state/substrate/validate.go).
- json_extract SQL: see cost-gov reconciler for the same pattern over
  budget_reconciled rows. Reuse field-names verbatim.
- cost.ModelBreakdownRow: exported by cost-gov W2 T3. Import; do NOT
  redefine.
- substrate.DefaultTenantID: until W8.
- Sentinels: errors.New + errors.Is — same pattern as cost-gov.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test below:
  1. Write the test file first.
  2. Run `go test ./<pkg>/ -run <TestName> -v`.
  3. CAPTURE the failing output (paste at least 5 representative
     samples into PR body's "Failing-test output (TDD capture)"
     section). "Tests would have failed" is NOT acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or per logical group).

# Tests to land (7 named)

B-tier (3):
1. TestRollup_SumsBudgetReconciledByTenant
2. TestRollup_EmptyPeriodReturnsZero
3. TestRollup_FiltersByTenantID

A-tier (2):
4. TestRollup_LateReconciledRow_IncludedOnRerun           (R7)
5. TestRollup_MultiBucketLineItems_OrderedByPeriodStart

A+-tier (1):
6. TestRollup_ConcurrentReadsAreStable

Reviewer-added (1):
7. TestSubstrate_BillingPeriodClosedPayloadValidates

# Push-first-commit-fast for downstream consumers

After landing the FIRST commit (the payload.go file alone), push the
branch. T2, T3, T4 can then `git fetch origin/feat/w12-t1-rollup --
internal/billing/event/payload.go` to unblock parallel work. Do NOT
wait for the full PR to be ready before pushing.

# Workflow after green

  1. Run `make pre-push-check` — confirm clean. If lint/build/test
     fails, fix in this branch — do NOT skip hooks (--no-verify
     banned per feedback_pr_lint_gates).
  2. Re-run `go test ./internal/billing/... ./internal/orchestrator/state/substrate/... -v` and confirm every named test green.
  3. Run `ls migrations/ | wc -l` and compare to main — must be
     identical (A6 rubric).
  4. Run `git diff origin/main -- '*.go' | grep -E '^\+.{0,2}//'` to
     spot superfluous comments; sweep per
     feedback_comments_discipline.
  5. Force-push branch.
  6. Open PR via `gh pr create --base main --title
     "feat(w12): T1 billing-period rollup + shared-primitive payload"
     --body-file <path>` (NEVER heredoc per
     feedback_pr_lint_gates). PR body MUST cite the 10 pre-filed
     [billing-followup] issue numbers + post the A+ scorecard
     VERBATIM per feedback_a_plus_scorecard_required.
  7. Grep your PR body against the banned-phrase token list in
     `scripts/doc-check.sh` BEFORE opening (per
     `feedback_doc_check_banned_phrases`). Any match means reword to
     a falsifiable claim before pushing.
  8. Grep ```release-notes``` fence in the PR body before opening per
     feedback_pr_body_release_notes_fence — must be present and end
     the body.
  9. Spawn ONE adversarial reviewer subagent
     (feedback_agent_pr_review + feedback_adversarial_review) with
     hunt list (see below).
 10. Apply reviewer findings inline (or file tracking issue + cite
     in PR body per feedback_unaddressed_load_bearing).
 11. Re-run `make pre-push-check`; force-push.
 12. Verify CI green (pr-lint, check-release-notes, check-tdd, build,
     test) BEFORE flipping automerge per
     feedback_review_before_automerge.
 13. Flip automerge: `gh pr merge <num> --auto --squash`.

# Adversarial reviewer hunt list

- BillingPeriodClosedPayload field shapes EXACT match to spec §3.7
  lines 322-336. JSON tags verbatim. No drift.
- LineItem 4 fields verbatim; ModelBreakdown imports
  cost.ModelBreakdownRow (do NOT redefine).
- SQL query VERBATIM from spec §3.2 lines 103-115. Same json_extract
  path expressions. Same GROUP BY.
- Run signature: (ctx, db, scope) (Rollup, error) — pure function.
  Does NOT open its own tx (read-only). Does NOT write any substrate
  row (consumer-only).
- ErrEmptyPeriod sentinel returned alongside zero-valued Rollup on
  zero-row result.
- RegisterPayloadValidator: init() block, no DDL, no existing-
  validator change. Imports `substrate.RegisterPayloadValidator`.
- Validator asserts tenant_id non-empty, period_end > period_start,
  total_usd >= 0, idempotency_key length == 64 (sha256 hex).
- Concurrent-read safety: two goroutines, same scope, same result.
  Substrate Fold is snapshot-isolated.
- ZERO new migrations: ls migrations/ unchanged.
- Cyclic-import check: `go list -deps ./internal/billing/event/`
  shows no edge into `internal/billing/rollup`, `cmd/regatta`,
  `internal/web`.
- Simplification opportunity: could rollup skip the second query
  for LineItems? No — spec §3.2 line 120 says line items ARE the
  individual budget_reconciled rows. They are needed by T3's invoice
  render.
- Deletion default: PR body cites concrete shrinkage (no billing
  table; no migration).
- No AI signatures anywhere (feedback_no_signatures).
- godocs ≤ 1 line on test funcs (feedback_comments_discipline).

# Hygiene

- NO AI signatures anywhere (commits, PR body, comments, code) per
  feedback_no_signatures.
- Comments discipline per feedback_comments_discipline: WHY not WHAT;
  test-function godocs ≤ 1 line; sweep on every push.
- Doc-check: run `bash scripts/doc-check.sh` and `bash
  scripts/stale-todo.sh` — both exit 0 BEFORE push.

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 5 of the 7 tests.
- Adversarial reviewer verdict (APPROVE or full findings list with
  severities).
- One-line diff stat: files changed + LoC added/removed.
- Confirmation: ZERO new migrations.

Begin now. NEVER pause for user input.
```

---

## §3 Task T2 — Stripe adapter + idempotency

### Scope

- **`go.mod` + `go.sum`** — vendor `github.com/stripe/stripe-go/v76` (latest patch on the v76 major). Pin the version; do NOT use `latest` tag (R9 mitigation). If the repo uses `go mod vendor`, also commit `vendor/github.com/stripe/stripe-go/v76/...` updates.
- **`internal/billing/stripe/adapter.go`** — NEW. Per spec §3.3 lines 144-185 verbatim:
  - `Config` struct: `APIKey string`, `TenantMap map[string]string`, `HTTPClient *http.Client` (default `&http.Client{Timeout: 30s}`), `Clock func() time.Time` (default `time.Now`).
  - `NewAdapter(cfg Config) *Adapter` — constructs the `client.API` from `stripego.NewBackends`, stores tenant map, returns adapter.
  - `Adapter.PushUsage(ctx, r rollup.Rollup) (UsageRecordID, error)` per spec §3.3 lines 169-185:
    1. Look up `tenant_id` in `TenantMap` — return `ErrUnmappedTenant` if missing (no API call).
    2. Compute idempotency key: `fmt.Sprintf("regatta-billing-%x", sha256.Sum256([]byte(r.TenantID + "|" + strconv.FormatInt(r.PeriodStart.Unix(), 10))))`. Deterministic; no clock.
    3. Build `stripego.UsageRecordParams`: `SubscriptionItem`, `Quantity = int64(math.Round(r.TotalUSD * 100))` (cents), `Timestamp = r.PeriodEnd.Unix() - 1` (last second of period), `Action = stripego.UsageRecordActionSet` (NOT increment per R2).
    4. `params.SetIdempotencyKey(idem)`.
    5. Call `a.api.UsageRecords.New(params)`.
    6. Error handling:
       - 4xx → return `fmt.Errorf("stripe: 4xx: %w", err)` — hard fail; caller (T4) commits substrate event first, surfaces error.
       - 429 → return `ErrRateLimited` wrapping the retry-after duration if header parseable.
       - 5xx + network → return `ErrUpstream5xx` for caller-side exponential backoff (1s × 2^n, capped 5min, 5 attempts).
  - Sentinels: `ErrUnmappedTenant`, `ErrAPIKeyMissing`, `ErrUpstream5xx`, `ErrRateLimited`.
- **`internal/billing/stripe/adapter_test.go`** — NEW. 6 named tests below + 1 reviewer-added.

### Prereqs (cite spec sections)

- Spec §3.3 lines 144-191 — Stripe adapter shape **verbatim** + key decisions (SET not increment, cents not USD float, deterministic idempotency key).
- Spec §5 R2 — double-charge race; idempotency proof.
- Spec §5 R6 — Stripe Price configuration as cent-per-unit (test asserts quantity is int64 cents).
- Spec §5 R9 — Stripe SDK version pin.
- Spec §5 R10 — unmapped-tenant fail-fast.
- Spec §6 T2 lines 423-426 — named test list.
- Spec §7 A5 — `stripe-go/v76` adoption pinned.

### Existing patterns to reuse (do NOT reinvent)

- **`net/http.Client`** — pass-through; do NOT wrap with a custom transport.
- **`crypto/sha256`** — stdlib; deterministic key derivation.
- **`stripe-go/v76` SDK** — use `client.API`, `stripego.UsageRecordParams`, `params.SetIdempotencyKey` verbatim from Stripe's own examples. Do NOT write a bespoke HTTP request.
- **`github.com/stripe/stripe-mock`** — test backend (Docker container OR Go binary). Already used by Stripe community for hermetic tests; the implementer ships a `testdata/stripe-mock.sh` helper that boots the mock server for the test suite.
- **Error wrapping:** `errors.Is` + `fmt.Errorf("%w")` — same pattern as cost-gov.
- **`internal/billing/rollup.Rollup`** — imported only as the input type to `PushUsage`. Do NOT redefine.

### TDD test list (named tests from spec §6 T2; failing-output capture step required)

**B-tier (3 named tests — spec §6 T2):**

1. `TestStripeAdapter_PushUsage_HappyPath` — boot stripe-mock; call `PushUsage` with a populated `Rollup`; assert exactly one `POST /v1/subscription_items/{ID}/usage_records` was observed at the mock; returned `UsageRecordID` is non-empty.
2. `TestStripeAdapter_UnmappedTenant_HardErrors` — `Config.TenantMap` lacks an entry for the rollup's tenant; `PushUsage` returns `ErrUnmappedTenant`; stripe-mock observed ZERO API calls.
3. `TestStripeAdapter_QuantityInCents` — `Rollup.TotalUSD = $12.34`; submitted `Quantity` to Stripe is exactly `1234` (int64 cents). Pins R6.

**A-tier (2 named tests — spec §6 T2):**

4. `TestStripeAdapter_IdempotencyKeyDeterministic` — same `(tenant_id, period_start)` twice; submitted idempotency key is byte-equal on both calls. Pins R2.
5. `TestStripeAdapter_5xxBackoff` — stripe-mock returns 500 three times then 200; adapter surfaces `ErrUpstream5xx` (caller-side backoff); on the 4th `PushUsage` call from the caller, success. (NOTE: the adapter itself does NOT loop on 5xx — that is T4's CLI responsibility. This test asserts the SENTINEL is returned for each 5xx.)

**A+-tier (1 named test — spec §6 T2):**

6. `TestStripeAdapter_ConcurrentPushes_OneCharge` — N goroutines (N=10) call `PushUsage` with the same `(tenant_id, period_start)` simultaneously; stripe-mock observed EXACTLY ONE `usage_records` POST (because Stripe collapses on idempotency key). Pins R2 adversarial.

**Adversarial-reviewer-added (1 named test, per `feedback_adversarial_review`):**

7. `TestStripeAdapter_VersionPinned_NotLatest` — `go.mod` has `github.com/stripe/stripe-go/v76 vX.Y.Z` with X.Y.Z all integers; assertion via parsing `go.mod` in-test. Pins R9.

Total T2: **7 named tests** (3 B + 2 A + 1 A+ + 1 reviewer-added). PR body lists every test + pasted failing-output for AT LEAST 5.

### PR body skeleton

````
## Summary

MVP-4 W12 Billing T2 ships the Stripe metered-usage adapter +
idempotency-key derivation + 4xx/429/5xx error mapping per spec §3.3.

- go.mod / go.sum — vendor github.com/stripe/stripe-go/v76 (pinned
  patch; NOT latest per R9).
- internal/billing/stripe/adapter.go — Adapter.PushUsage(ctx, rollup)
  pushes one subscription_item.usage_record per tenant per period.
  Idempotency key = sha256(tenant_id || period_start_unix); SET not
  increment; quantity in int64 cents.
- internal/billing/stripe/adapter_test.go — 7 named tests against
  stripe-mock backend.

## Why

Per spec §3.3: SDK adoption per feedback_research_design_principles
(proven OSS beats bespoke). SET action + deterministic key derivation
+ Stripe 24h idempotency window collapse R2 double-charge race
end-to-end.

## Test plan

- [x] TestStripeAdapter_PushUsage_HappyPath
- [x] TestStripeAdapter_UnmappedTenant_HardErrors (R10)
- [x] TestStripeAdapter_QuantityInCents (R6)
- [x] TestStripeAdapter_IdempotencyKeyDeterministic (R2)
- [x] TestStripeAdapter_5xxBackoff
- [x] TestStripeAdapter_ConcurrentPushes_OneCharge (R2 adversarial; A+)
- [x] TestStripeAdapter_VersionPinned_NotLatest (R9 reviewer-added)
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline; min 5 reps>

## A+ scorecard

- B1 (doc-check) + B2 (stale-todo) exit 0.
- A5 (stripe-go/v76 verbatim): go.mod cites v76.
- A6 (ZERO new migrations): unchanged.
- A1 (reviewer cleared): cite transcript.

## Deletion default

T2 ELIMINATES every bespoke payment-processing primitive: no custom
HTTP client, no HMAC signing, no retry FSM written from scratch. The
SDK is one file; replacement is one-file edit (R9 future-proof).

## Followup issues filed

Cites D9 [billing-followup] Stripe SDK refresh runbook (pre-filed by
main session).

```release-notes
[FEATURE] Stripe metered-usage adapter (stripe-go/v76; idempotent push via sha256-derived key; default-off until billing.stripe block configured in regatta.yaml)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w12-t2. Branch off main:

  git fetch origin
  git checkout -b feat/w12-t2-stripe-adapter origin/main

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w12-billing-design.md.
Read ALL of: §1 (goal #2 — Stripe metered-usage export), §3.3
(Stripe adapter — Config + Adapter + PushUsage signature VERBATIM,
key decisions: SET not increment, cents not USD float, deterministic
idempotency key), §3.5 lines 252-267 (close-ritual ordering: Stripe
push AFTER substrate commit + retry semantics), §5 R2 (double-charge
race + idempotency proof), R6 (Stripe Price configuration), R9
(version pin), R10 (unmapped-tenant fail-fast), §6 T2 (named test
list), §7 A5 (stripe-go/v76 verbatim) + A6 (ZERO new migrations),
§8 row 2 (file-disjoint scope).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (stripe-go/v76 the OSS SDK; UsageRecordActionSet
NOT Increment; quantity int64(TotalUSD*100) cents; idempotency key
sha256(tenant_id || "|" || period_start_unix) format string verbatim;
4xx hard-fail vs 5xx-sentinel-for-caller-backoff), STOP and report —
do NOT pick an alternative yourself.

# Scope (exclusive write paths — file-disjoint with T1/T3/T4/T5/T6)

- go.mod                                          (vendor stripe-go/v76)
- go.sum
- vendor/github.com/stripe/stripe-go/v76/...      (if vendor mode used)
- internal/billing/stripe/adapter.go              (NEW)
- internal/billing/stripe/adapter_test.go         (NEW)
- internal/billing/stripe/testdata/stripe-mock.sh (NEW; helper to boot stripe-mock for hermetic tests)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/billing/event/ or internal/billing/rollup/ —
  T1's scope. Import only.
- Do NOT touch internal/billing/invoice/ — T3's scope.
- Do NOT touch cmd/regatta/ — T4's scope.
- Do NOT touch internal/web/ — T5's scope.
- Do NOT create a SQL migration.

# Patterns to reuse (do NOT reinvent)

- stripe-go/v76 SDK examples: use client.API,
  stripego.UsageRecordParams, params.SetIdempotencyKey verbatim. Do
  NOT write a bespoke HTTP request.
- net/http.Client: pass-through; do NOT wrap with custom transport.
- crypto/sha256: stdlib; deterministic key derivation.
- stripe-mock: hermetic test backend; testdata/stripe-mock.sh helper
  boots it.
- Error wrapping: errors.Is + fmt.Errorf("%w") — same pattern as
  cost-gov.
- internal/billing/rollup.Rollup: import only; do NOT redefine.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test:
  1. Write the test file first.
  2. Run `go test ./internal/billing/stripe/ -run <TestName> -v`.
  3. CAPTURE failing output (min 5 in PR body).
  4. Implement.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (7 named)

B-tier (3):
1. TestStripeAdapter_PushUsage_HappyPath
2. TestStripeAdapter_UnmappedTenant_HardErrors        (R10)
3. TestStripeAdapter_QuantityInCents                  (R6)

A-tier (2):
4. TestStripeAdapter_IdempotencyKeyDeterministic      (R2)
5. TestStripeAdapter_5xxBackoff

A+-tier (1):
6. TestStripeAdapter_ConcurrentPushes_OneCharge       (R2; A+)

Reviewer-added (1):
7. TestStripeAdapter_VersionPinned_NotLatest          (R9)

# Workflow after green

  1. make pre-push-check clean.
  2. go test ./internal/billing/stripe/ -v -race -count=10
     (race + repeated for the concurrent-push test).
  3. ls migrations/ | wc -l unchanged.
  4. git diff origin/main -- '*.go' | grep -E '^\+.{0,2}//' — sweep
     comments.
  5. Force-push.
  6. Open PR via `gh pr create --base main --title
     "feat(w12): T2 Stripe adapter + idempotent metered-usage push"
     --body-file <path>`. Body MUST cite pre-filed [billing-followup]
     D9 + post A+ scorecard verbatim.
  7. Grep banned phrases against PR body BEFORE push. Grep
     ```release-notes``` fence presence.
  8. Spawn ONE adversarial reviewer (feedback_agent_pr_review).
  9. Apply findings inline (or file tracking issue).
 10. Re-run pre-push-check; force-push.
 11. Verify CI green BEFORE flipping automerge.
 12. `gh pr merge <num> --auto --squash`.

# Adversarial reviewer hunt list

- Idempotency key format string EXACT: "regatta-billing-%x" +
  sha256(tenant_id || "|" || period_start_unix). Match spec §3.3
  line 174 verbatim.
- UsageRecordActionSet, NOT Increment. Spec §3.3 line 179.
- Quantity int64(math.Round(TotalUSD * 100)). Spec line 177.
- Timestamp = period_end.Unix() - 1 (last second of period). Spec
  line 178.
- 4xx hard-fail (caller commits substrate first); 5xx returns
  sentinel for caller-side backoff. Spec §3.5 line 277.
- ErrUnmappedTenant: returned BEFORE any HTTP call. Spec R10.
- ErrAPIKeyMissing: fail-fast at NewAdapter. Spec §3.5 failure-mode
  line 275.
- go.mod pin: github.com/stripe/stripe-go/v76 vX.Y.Z with all
  integers — NOT 'latest'. R9.
- Cyclic-import check: `go list -deps
  ./internal/billing/stripe/` shows no edge into cmd/regatta or
  internal/web.
- Simplification opportunity: could PushUsage retry internally on
  5xx? No — spec §3.5 line 277 puts retry on the caller (T4 CLI)
  with exponential 1s × 2^n cap 5min. Adapter is a single-call
  primitive.
- Concurrent-push idempotency: stripe-mock observes EXACTLY ONE
  POST. Race-detector clean.
- Deletion default: PR body cites no bespoke HTTP client.
- No AI signatures (feedback_no_signatures).
- godocs ≤ 1 line (feedback_comments_discipline).

# Hygiene

- NO AI signatures (commits/PR body/comments/code).
- WHY not WHAT comments.
- bash scripts/doc-check.sh + bash scripts/stale-todo.sh both exit 0.

# Return format

- PR URL.
- Pasted failing-test output for at least 5 of 7 tests.
- Adversarial reviewer verdict.
- One-line diff stat.
- Confirmation: ZERO new migrations; stripe-go/v76 pinned.

Begin now. NEVER pause for user input.
```

---

## §4 Task T3 — Invoice markdown template

### Scope

- **`internal/billing/invoice/template.md.tmpl`** — NEW. Spec §3.4 lines 205-228 verbatim. Template uses `text/template` syntax with `printf "%.2f"` for USD formatting + `time.Format` for timestamps + a custom `joinModelBreakdown` template func for the model-breakdown column.
- **`internal/billing/invoice/render.go`** — NEW. Per spec §3.4 lines 232 + §3.5 T+7:
  - `Render(r rollup.Rollup, outDir string, stripeRecordID stripe.UsageRecordID) (path string, err error)`:
    1. Compute output path via `OutputPath(r.TenantID, r.PeriodStart, outDir)`.
    2. Create parent directory if missing (`os.MkdirAll`).
    3. Parse template (embed.FS via `//go:embed template.md.tmpl`).
    4. Compute `IdempotencyKey = sha256(tenant_id || "|" || period_start_unix)[:64]` (same as T2 key for cross-system audit; rendered into the markdown body).
    5. Compute `GeneratedAt = time.Now().UTC()` (the only non-deterministic field per spec §3.4 line 230).
    6. Execute template into a `bytes.Buffer`; write to disk OVERWRITE-ON-EXISTS (idempotent re-render per spec §3.4 line 232).
    7. Return absolute path of written file.
  - `OutputPath(tenantID string, periodStart time.Time, outDir string) string` — pure helper, no I/O: returns `filepath.Join(outDir, tenantID, periodStart.Format("2006-01") + ".md")`.
- **`internal/billing/invoice/render_test.go`** — NEW. 5 named tests + 1 reviewer-added.
- **`internal/billing/invoice/testdata/golden_invoice_basic.md`** — golden file for byte-equality assertions.
- **`internal/billing/invoice/testdata/golden_invoice_stripe_skipped.md`** — golden file with empty `StripeUsageRecordID` per spec §6 T3 A+.

### Prereqs (cite spec sections)

- Spec §3.4 lines 194-232 — markdown template VERBATIM + decision (markdown-only v1) + deterministic-output invariant (single non-determinism: `GeneratedAt`).
- Spec §3.5 T+7 — invocation point (after substrate commit, before Stripe push).
- Spec §6 T3 lines 428-431 — named test list.

### Existing patterns to reuse (do NOT reinvent)

- **`text/template`** — stdlib, no third dep. Pattern matches W7's existing `internal/web/templates/` parse pattern.
- **`//go:embed`** — embed template at build time; matches W7 §3.4 template loader pattern.
- **`internal/billing/rollup.Rollup`** — import only.
- **`internal/billing/stripe.UsageRecordID`** — import only.
- **`os.MkdirAll` + `os.WriteFile`** — stdlib file I/O. Default permissions per file 0o600 (invoices may contain PII).

### TDD test list

**B-tier (3 named tests — spec §6 T3):**

1. `TestInvoice_RendersAllLineItems` — `Rollup` with 5 line items; rendered markdown contains 5 table rows; total matches sum of `ActualUSD` per row.
2. `TestInvoice_TotalMatchesSum` — `Rollup.TotalUSD = $42.17`; rendered markdown contains the string `**Total (USD):** $42.17`.
3. `TestInvoice_OutputPathDeterministic` — `OutputPath("tenant-foo", time.Date(2026, 6, 1, ...), ".regatta/invoices")` returns `.regatta/invoices/tenant-foo/2026-06.md`. Pure function; no I/O in test.

**A-tier (1 named test — spec §6 T3):**

4. `TestInvoice_ByteEqualExceptGeneratedAt` — two renders against same `Rollup` ~1s apart; `diff -u` shows exactly one differing line (the `Generated:` field).

**A+-tier (1 named test — spec §6 T3):**

5. `TestInvoice_RendersWhenStripeSkipped` — `Render` called with empty `stripeRecordID`; markdown renders successfully; `Stripe usage_record:` field is empty. Matches `golden_invoice_stripe_skipped.md`.

**Adversarial-reviewer-added (1 named test):**

6. `TestInvoice_OverwriteOnRerun` — render twice against the same path; second render OVERWRITES the first (file size + content match second render's golden, NOT first). Pins idempotent re-run semantics per spec §3.4 line 232.

Total T3: **6 named tests** (3 B + 1 A + 1 A+ + 1 reviewer-added). PR body lists every test + pasted failing-output for AT LEAST 4.

### PR body skeleton

````
## Summary

MVP-4 W12 Billing T3 ships the markdown invoice renderer per spec
§3.4. Markdown-only v1; PDF deferred to D4 followup.

- internal/billing/invoice/template.md.tmpl — spec §3.4 verbatim;
  text/template syntax; deterministic except for GeneratedAt.
- internal/billing/invoice/render.go — Render(rollup, outDir,
  stripeRecordID) writes the invoice file. OutputPath pure helper.
  Idempotent overwrite on rerun.
- internal/billing/invoice/testdata/golden_invoice_*.md — golden
  files for byte-equality assertions.

## Why

Per spec §3.4: markdown is the falsifiable Wave-1 artifact — operator
can cat it, customer can render in any viewer, file is diff-able for
audit. PDF deferred until at least one customer asks (D4 tracking
issue cites the gofpdf vs headless-Chrome decision tree).

## Test plan

- [x] TestInvoice_RendersAllLineItems
- [x] TestInvoice_TotalMatchesSum
- [x] TestInvoice_OutputPathDeterministic
- [x] TestInvoice_ByteEqualExceptGeneratedAt
- [x] TestInvoice_RendersWhenStripeSkipped (A+)
- [x] TestInvoice_OverwriteOnRerun (reviewer-added)
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal; min 4 reps>

## A+ scorecard

- B1 + B2 exit 0.
- A6 ZERO new migrations.
- A1 reviewer cleared.

## Deletion default

T3 ELIMINATES every PDF dependency: no gofpdf vendored, no
headless-Chrome runtime, no font assets shipping. Markdown is one
template file + 80 LoC render. Replacement (PDF) is additive in the
D4 followup wedge.

## Followup issues filed

Cites pre-filed D4 [billing-followup] PDF invoice rendering.

```release-notes
[FEATURE] billing markdown invoice renderer (deterministic except for GeneratedAt; idempotent overwrite-on-rerun; v1 markdown-only — PDF deferred per D4)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w12-t3. Branch off main:

  git fetch origin
  git checkout -b feat/w12-t3-invoice origin/main

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w12-billing-design.md.
Read: §1 goal #3 (invoice generation markdown v1), §3.4 (template
VERBATIM + decision markdown-only + deterministic-output invariant),
§3.5 T+7 (invocation point AFTER substrate commit BEFORE Stripe push),
§6 T3 (named test list), §8 row 3 (file-disjoint scope).

Per feedback_spec_pattern_authority: if you want to deviate (e.g.
add PDF rendering inline; switch from text/template to a third-party
markdown library), STOP and report. PDF deferred to D4 followup;
text/template is sufficient.

# Scope (exclusive write paths)

- internal/billing/invoice/render.go                            (NEW)
- internal/billing/invoice/template.md.tmpl                     (NEW; spec §3.4 verbatim)
- internal/billing/invoice/render_test.go                       (NEW)
- internal/billing/invoice/testdata/golden_invoice_basic.md     (NEW)
- internal/billing/invoice/testdata/golden_invoice_stripe_skipped.md (NEW)

You MUST NOT touch any other file. Specifically: Do NOT touch
internal/billing/event/, internal/billing/rollup/,
internal/billing/stripe/, cmd/regatta/, internal/web/, or
docs/operator/.

# Patterns to reuse

- text/template stdlib (matches W7's templates).
- //go:embed for the template file at compile time.
- internal/billing/rollup.Rollup + internal/billing/stripe.UsageRecordID
  — import only.
- os.MkdirAll + os.WriteFile with file 0o600 perms.

# Tests to land (6 named)

B-tier (3):
1. TestInvoice_RendersAllLineItems
2. TestInvoice_TotalMatchesSum
3. TestInvoice_OutputPathDeterministic

A-tier (1):
4. TestInvoice_ByteEqualExceptGeneratedAt

A+-tier (1):
5. TestInvoice_RendersWhenStripeSkipped

Reviewer-added (1):
6. TestInvoice_OverwriteOnRerun

# Workflow

1. TDD: write test, capture failing output, implement, re-run, commit.
2. make pre-push-check clean.
3. ls migrations/ | wc -l unchanged.
4. Grep banned phrases + ```release-notes``` fence presence on PR
   body BEFORE push.
5. Open PR with `--body-file`; A+ scorecard verbatim; cite D4.
6. Adversarial reviewer; apply findings.
7. CI green BEFORE automerge; `gh pr merge --auto --squash`.

# Adversarial reviewer hunt list

- Template content VERBATIM from spec §3.4 lines 205-228.
- GeneratedAt is the ONLY non-deterministic field. Two renders ~1s
  apart, diff is exactly one line.
- OutputPath: filepath.Join(outDir, tenantID, periodStart.Format
  "2006-01" + ".md"). No drift.
- File mode 0o600 (invoices contain PII).
- Idempotent overwrite-on-rerun (spec §3.4 line 232).
- IdempotencyKey rendered into markdown matches T2's derivation
  format byte-equal. (Cross-system audit invariant.)
- StripeUsageRecordID empty case renders cleanly (A+ test).
- No third-party markdown library; text/template stdlib only.
- Cyclic-import: `go list -deps ./internal/billing/invoice/` shows
  no edge into cmd/regatta or internal/web.
- Simplification opportunity: could OutputPath be inlined into
  Render? No — T4's CLI also computes the path (for logging) without
  rendering. Helper stays.
- Deletion default: PR body cites no gofpdf, no headless-Chrome.
- No AI signatures.
- godocs ≤ 1 line.

# Hygiene

- NO AI signatures.
- WHY not WHAT.
- bash scripts/doc-check.sh + stale-todo.sh exit 0.

# Return format

- PR URL.
- Failing-test output (min 4 of 6).
- Reviewer verdict.
- Diff stat.

Begin now. NEVER pause for user input.
```

---

## §5 Task T4 — `regatta billing close` CLI

### Scope

- **`cmd/regatta/billing.go`** — NEW. Subcommand `regatta billing close --period YYYY-MM [--tenant <id>] [--allow-open-period]`. Per spec §3.5 lines 234-279 execution shape verbatim:
  1. Parse `--period` flag (`2006-01` format); resolve `period_start = first instant of month UTC`, `period_end = first instant of next month UTC`. If `--period` omitted: exit non-zero with usage banner.
  2. Check `now() >= period_end`. If false AND `--allow-open-period` not set: exit with `ErrPeriodNotEnded`. If `--allow-open-period` set: emit `slog.Warn("billing close: open period override", ...)`.
  3. Load config; resolve `billing.stripe` block. If config has `billing.stripe` but `STRIPE_API_KEY` env unset: fail-fast.
  4. Query substrate for distinct `tenant_id`s with `budget_reconciled` rows in window.
  5. For each tenant, in sequence:
     - Open OTel span `billing.tenant.close` (parent `billing.close`).
     - `BEGIN TX`.
     - `rollup.Run(ctx, tx, scope)` → if `rollup.ErrEmptyPeriod`: rollback, log INFO, skip (no empty invoice, no $0 Stripe push per failure-mode line 274).
     - Compute `IdempotencyKey = sha256(tenant_id || "|" || period_start_unix)` (hex string).
     - Build `event.BillingPeriodClosedPayload` with `StripeUsageRecordID = ""`.
     - `substrate.AppendEvent(ctx, tx, AppendInput{Kind: event.KindBillingPeriodClosed, ...})`.
     - `COMMIT TX`.
     - `invoice.Render(rollup, cfg.Billing.InvoiceDir, "")` → write markdown file (with empty Stripe record ID rendered).
     - If Stripe enabled:
       - `stripeAdapter.PushUsage(ctx, rollup)` with caller-side backoff: 1s × 2^n cap 5min, 5 attempts on `ErrUpstream5xx` only. On `ErrUnmappedTenant` / `ErrAPIKeyMissing` / 4xx hard-fail: surface error but DO NOT roll back substrate event (it's already committed; operator fixes config + re-runs; LWW + Stripe idempotency collapse).
       - On Stripe success: emit second `billing_period_closed` event in a NEW tx with `StripeUsageRecordID` populated; LWW collapses on `(tenant_id, period_start)` to the populated-ID row per spec §3.5 line 254.
       - Re-render the markdown invoice with the now-known `stripeRecordID` (overwrite-safe per T3 invariant).
     - Close `billing.tenant.close` span with attrs.
  6. After all tenants: close `billing.close` span; print summary table to stdout; exit 0 (or non-zero if any tenant errored hard).
- **`cmd/regatta/billing_test.go`** — NEW. 6 named tests below + 1 reviewer-added.
- **`cmd/regatta/root.go`** — additive: one-line subcommand registration in the existing cobra/flag dispatcher. ≤ 5 LoC delta.

### Three-layer idempotency on close (cite spec §3.5 lines 262-267)

This task's load-bearing invariant. Implementer MUST cite + test:

1. **Substrate side:** `billing_period_closed` reducer = `lww` on `(tenant_id, period_start)`. Two close runs produce two appended physical rows; Fold returns the most recent. Test: `TestCLI_DoubleRun_NoDoubleCharge` asserts substrate has 2 rows but Fold returns 1 row with the LATEST `stripe_usage_record_id`.
2. **Stripe side:** idempotency key = `sha256(tenant_id || period_start_unix)`. Stripe's 24h idempotency window collapses retries within window. Test (via stripe-mock): N close invocations within window → exactly ONE `POST /usage_records` observed.
3. **Filesystem side:** invoice file OVERWRITE on re-render. Test: `TestCLI_DoubleRun_InvoiceOverwritten` — file size + content match the second render (which populates `stripe_usage_record_id` from the now-known Stripe response).

### Prereqs (cite spec sections)

- Spec §3.5 lines 234-279 — close ritual VERBATIM (T+0 through T+11) + idempotency proof + failure-mode table.
- Spec §3.7 — payload struct (T1-owned import).
- Spec §3.8 lines 348-358 — OTel spans `billing.close` + `billing.tenant.close` + `billing.stripe.push` + attrs.
- Spec §5 R2 (double-charge race), R7 (reconciliation lag — `--period 2026-06` should be run on or after `2026-07-01T01:00Z`), R8 (write amplification — 100-tenant close completes within 60s budget).
- Spec §6 T4 lines 433-436 — named test list.
- Spec §9 sequencing — T4 lands AFTER T1 + T2 + T3 (or shared-primitive-owner pull from those branches).

### Existing patterns to reuse (do NOT reinvent)

- **`internal/billing/rollup.Run`** — T1-owned.
- **`internal/billing/stripe.Adapter`** — T2-owned.
- **`internal/billing/invoice.Render` + `OutputPath`** — T3-owned.
- **`internal/billing/event.BillingPeriodClosedPayload` + `KindBillingPeriodClosed`** — T1-owned.
- **`substrate.AppendEvent`** — T-S1 #224 exported.
- **`go.opentelemetry.io/otel`** — existing tracer; reuse `otel.Tracer("cmd/regatta")` pattern from existing subcommands.
- **`internal/config`** — existing CUE-loader for `regatta.yaml`. Add `billing` block reading inline; the CUE schema change is OWNED by T4 (lives in `contracts/schemas/regatta.v1.cue` — verify with a pre-flight grep whether anyone else has claimed this file; if so STOP).
- **Cobra/flag subcommand pattern** — match the existing `regatta` subcommand registration style (whatever it is — implementer-grep to confirm).

### TDD test list

**B-tier (3 named tests — spec §6 T4):**

1. `TestCLI_ClosePeriodFlag_Required` — invoking `regatta billing close` with NO `--period` returns non-zero exit + usage banner on stderr.
2. `TestCLI_PeriodNotEnded_RejectsWithoutOverride` — `--period 2026-06` invoked on 2026-06-15 (mock clock) returns `ErrPeriodNotEnded`. Pins spec §3.5 failure-mode line 273.
3. `TestCLI_NoBudgetReconciled_SkipsTenant` — substrate has 0 `budget_reconciled` rows for tenant A in the window; close skips tenant A silently (logs INFO; no substrate write; no Stripe call; no invoice file). Pins spec §3.5 failure-mode line 274.

**A-tier (2 named tests — spec §6 T4):**

4. `TestCLI_DoubleRun_NoDoubleCharge` — run close twice for the same period (within stripe-mock's 24h idempotency window); stripe-mock observes exactly ONE `POST /usage_records`; substrate Fold returns 1 row (the LWW winner with populated `stripe_usage_record_id`). Pins three-layer idempotency.
5. `TestCLI_StripeFailure_SubstrateEventCommitted` — stripe-mock returns persistent 500; close completes for the tenant with substrate event written (empty `stripe_usage_record_id`); CLI exit code non-zero (Stripe push failed); re-run after stripe-mock recovers populates the ID via LWW. Pins spec §3.5 line 277.

**A+-tier (1 named test — spec §6 T4):**

6. `TestCLI_MultiTenant_PerTenantSpan` — close 3 tenants in one invocation; OTel exporter captures 1 `billing.close` span + 3 `billing.tenant.close` spans + 3 `billing.stripe.push` spans; span hierarchy matches spec §3.8 table. Pins OTel structure.

**Adversarial-reviewer-added (1 named test):**

7. `TestCLI_DoubleRun_InvoiceOverwritten` — first close renders invoice with empty `stripe_usage_record_id`; second close (after Stripe succeeds) overwrites the file with populated ID. Pins third-layer filesystem idempotency.

Total T4: **7 named tests** (3 B + 2 A + 1 A+ + 1 reviewer-added). PR body lists every test + pasted failing-output for AT LEAST 5.

### PR body skeleton

````
## Summary

MVP-4 W12 Billing T4 ships the `regatta billing close --period
YYYY-MM` CLI per spec §3.5. Operator-triggered, idempotent end-to-end
via three layers (substrate LWW + Stripe 24h window + filesystem
overwrite).

- cmd/regatta/billing.go — close subcommand + ritual driver +
  per-tenant transaction + caller-side backoff on Stripe 5xx.
- cmd/regatta/root.go — additive subcommand registration (≤ 5 LoC).
- cmd/regatta/billing_test.go — 7 named tests.

## Why

Per spec §3.5: close is operator-triggered (NOT cron-automated) by
deliberate UX choice. Three-layer idempotency (substrate LWW + Stripe
24h key window + filesystem overwrite) makes re-run the supported
recovery path; no partial-failure orphan state.

## Test plan

- [x] TestCLI_ClosePeriodFlag_Required
- [x] TestCLI_PeriodNotEnded_RejectsWithoutOverride            (R7)
- [x] TestCLI_NoBudgetReconciled_SkipsTenant
- [x] TestCLI_DoubleRun_NoDoubleCharge                          (R2)
- [x] TestCLI_StripeFailure_SubstrateEventCommitted             (§3.5)
- [x] TestCLI_MultiTenant_PerTenantSpan                         (A+)
- [x] TestCLI_DoubleRun_InvoiceOverwritten                      (reviewer)
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal; min 5 reps>

## A+ scorecard

- B1 + B2 exit 0.
- A6 ZERO new migrations.
- A1 reviewer cleared.

## Deletion default

T4 ELIMINATES UI-close double-submit guard (CLI-only by design). The
three-layer idempotency is each-layer-already-existing — substrate
LWW reducer (T-S1 #224 existing), Stripe 24h window (SDK-side),
filesystem overwrite (T3-existing) — net new code is the wire-up
NOT any new dedup primitive.

## Followup issues filed

Cites pre-filed D2 + D5 + D8 [billing-followup].

```release-notes
[FEATURE] regatta billing close CLI (operator-triggered period close; substrate event emission + invoice markdown render + Stripe metered-usage push; three-layer idempotency)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w12-t4. Branch off main:

  git fetch origin
  git checkout -b feat/w12-t4-cli origin/main

NOTE: T4 imports T1+T2+T3 APIs. Option A (preferred per
feedback_shared_primitive_owner): wait for T1+T2+T3 PRs to merge,
then branch + rebase. Option B (parallel start): use a thin stub of
the imported APIs in tests only; rebase off main after siblings land.
Option A preferred for safety.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w12-billing-design.md.
Read: §1 goal #4 (close ritual), §3.5 (close ritual VERBATIM lines
234-279 — T+0 through T+11 execution shape + idempotency proof +
failure-mode table), §3.7 (payload struct), §3.8 (OTel spans
billing.close + billing.tenant.close + billing.stripe.push), §5 R2
(double-charge race), R7 (reconciliation lag), R8 (write
amplification), §6 T4 (named test list), §9 sequencing.

THREE-LAYER IDEMPOTENCY (load-bearing — spec §3.5 lines 262-267):
  1. Substrate: LWW on (tenant_id, period_start).
  2. Stripe: idempotency key sha256(tenant_id || period_start_unix);
     24h window collapses retries.
  3. Filesystem: invoice file OVERWRITE on re-render.
Test all three.

Per feedback_spec_pattern_authority: if you want to deviate (e.g.
add a UI close button; auto-cron close; per-tenant parallel close;
retry FSM inside the adapter; different idempotency-key format),
STOP and report — re-spawn the design subagent.

# Scope (exclusive write paths)

- cmd/regatta/billing.go            (NEW)
- cmd/regatta/billing_test.go       (NEW)
- cmd/regatta/root.go               (additive subcommand registration; ≤ 5 LoC)
- contracts/schemas/regatta.v1.cue  (additive #Billing block; spec §3.1 verbatim — verify nobody else has claimed this file pre-flight)

You MUST NOT touch any other file. Specifically: Do NOT touch
internal/billing/event/, rollup/, stripe/, invoice/ — siblings'
scope. Import only. Do NOT touch internal/web/ — T5's scope.
Do NOT create a SQL migration.

# Pre-flight verification

  grep -n "billing" contracts/schemas/regatta.v1.cue
  grep -rn "billing close" cmd/regatta/

If the CUE schema already has a billing block OR cmd/regatta/ already
has billing.go, STOP and report.

# Patterns to reuse

- internal/billing/{rollup,stripe,invoice,event} — import only.
- substrate.AppendEvent — T-S1 #224.
- otel.Tracer("cmd/regatta") — existing pattern.
- Cobra/flag subcommand registration — match existing style; grep
  cmd/regatta/root.go to confirm.
- Caller-side backoff: 1s × 2^n cap 5min, 5 attempts. ONLY on
  stripe.ErrUpstream5xx. NOT on 4xx (hard-fail).

# Tests to land (7 named)

B-tier (3):
1. TestCLI_ClosePeriodFlag_Required
2. TestCLI_PeriodNotEnded_RejectsWithoutOverride                 (R7)
3. TestCLI_NoBudgetReconciled_SkipsTenant

A-tier (2):
4. TestCLI_DoubleRun_NoDoubleCharge                              (R2)
5. TestCLI_StripeFailure_SubstrateEventCommitted

A+-tier (1):
6. TestCLI_MultiTenant_PerTenantSpan

Reviewer-added (1):
7. TestCLI_DoubleRun_InvoiceOverwritten

# Workflow

1. TDD per test (capture failing output; min 5 reps in PR body).
2. make pre-push-check clean.
3. go test ./cmd/regatta/ -v -race.
4. ls migrations/ | wc -l unchanged.
5. Grep banned phrases + ```release-notes``` fence on PR body.
6. Open PR `gh pr create --base main --title
   "feat(w12): T4 regatta billing close CLI"
   --body-file <path>`. A+ scorecard verbatim; cite D2 + D5 + D8.
7. Adversarial reviewer; apply findings.
8. CI green BEFORE automerge; `gh pr merge --auto --squash`.

# Adversarial reviewer hunt list

- Close ritual execution order EXACT match to spec §3.5 lines
  238-260: rollup → BEGIN TX → AppendEvent (empty stripe_id) →
  COMMIT → invoice render → Stripe push → second AppendEvent
  (populated stripe_id) → re-render invoice. NO drift.
- Three-layer idempotency tests cover all three layers (substrate
  Fold returns 1 row; stripe-mock observes 1 POST; invoice file
  overwritten).
- Per-tenant span emission: 1 billing.close parent + N
  billing.tenant.close children + N billing.stripe.push grandchildren.
  Spec §3.8 table verbatim.
- Caller-side backoff applied ONLY to ErrUpstream5xx (not 4xx).
- ErrPeriodNotEnded fails closed unless --allow-open-period set.
- --allow-open-period emits WARN slog.
- Stripe-API-key env unset + billing.stripe configured ⇒ fail-fast at
  CLI start (spec §3.5 line 275).
- CUE schema change additive; backwards-compatible; no breaking
  change for operators without a billing block.
- Cyclic-import: cmd/regatta imports internal/billing/*; nothing
  imports cmd/regatta.
- Simplification opportunity: could the second AppendEvent be
  eliminated by writing the empty event after Stripe push? No — spec
  §3.5 line 254 requires substrate commit BEFORE Stripe push so a
  Stripe failure leaves the event durable for re-run.
- Simplification opportunity: could close be cron-automated? No —
  spec §1 goal #4 + §3.5 deliberate UX choice. Operator deliberation
  + audit envelope.
- Deletion default: PR body cites no UI close button, no
  double-submit guard, no new dedup primitive.
- No AI signatures.
- godocs ≤ 1 line.

# Hygiene

- NO AI signatures.
- WHY not WHAT.
- bash scripts/doc-check.sh + stale-todo.sh exit 0.

# Return format

- PR URL.
- Failing-test output (min 5 of 7).
- Reviewer verdict.
- Diff stat.
- Confirmation: ZERO new migrations; three-layer idempotency tested.

Begin now. NEVER pause for user input.
```

---

## §6 Task T5 — Operator UI billing tab

### Scope

- **`internal/web/billing.go`** — NEW. Per spec §3.6 lines 280-313:
  - `RegisterBillingRoutes(mux *http.ServeMux, deps web.Deps)` — additive registration of two routes (called from W7's existing route-registration callsite).
  - `GET /billing` handler: read `events.kind='billing_period_closed'` via substrate Fold; group by `(tenant_id, period_start)`; LWW returns one row per group; render `billing.tmpl` with the rows sorted by period DESC then tenant_id ASC.
  - `GET /billing/{tenant_id}/{period}` handler: parse path params; Fold-read the single matching `billing_period_closed` event; render the invoice markdown via `goldmark` (W7 already vendored per spec §3.4 line 201) → HTML; wrap in W7 layout.
  - Auth: every handler attaches the W7 cookie-HMAC middleware (existing).
  - Cache: `Cache-Control: no-store` on both responses.
- **`internal/web/billing_test.go`** — NEW. 6 named tests below.
- **`internal/web/templates/billing.tmpl`** — NEW. Spec §3.6 lines 294-311 verbatim.
- **`internal/web/routes.go`** — additive: two route registrations (`/billing` + `/billing/{tenant_id}/{period}`). ≤ 6 LoC delta. NO modification to existing route handlers.

### Prereqs (cite spec sections)

- Spec §3.6 lines 280-313 — UI tab VERBATIM (route table + data source + template + no-close-button discipline).
- Spec §3.7 — payload struct (T1-owned import for Fold-decode).
- Spec §5 R4 — tenant-leak invoice cross-view (pre-W8 operator-only enforcement).
- Spec §6 T5 lines 438-441 — named test list.
- Spec §9 sequencing — T5 lands AFTER W7 Wave 1 + T4.

### Existing patterns to reuse (do NOT reinvent)

- **W7 cookie-HMAC middleware** — `internal/web/auth/` (or wherever W7 §3.2 placed it; implementer-grep to confirm).
- **W7 `embed.FS` template loader** — existing `template.ParseFS` call; just add `billing.tmpl` to the embedded set.
- **W7 layout template** — `layout.tmpl` shell from spec §3.5.
- **goldmark** — already vendored per W7 §3.5. Use `goldmark.New(goldmark.WithExtensions(extension.Table))` for invoice rendering.
- **`substrate.Fold`** — existing Fold API for LWW reads.
- **`event.BillingPeriodClosedPayload`** — T1-owned import.
- **No new auth primitive** — operator-cookie verified by existing W7 middleware. Spec §5 R4 line 405 verbatim.

### TDD test list

**B-tier (3 named tests — spec §6 T5):**

1. `TestBillingHandler_OperatorAuth_Required` — GET `/billing` without operator cookie returns 401. Pins spec §5 R4 pre-W8 operator-only invariant.
2. `TestBillingHandler_ListsAllClosedPeriods` — substrate seeded with 3 `billing_period_closed` events across 2 tenants × 2 periods (one duplicate per LWW); handler returns 200; HTML table has exactly 3 rows (LWW collapses the duplicate).
3. `TestBillingHandler_RendersOneInvoice` — GET `/billing/tenant-a/2026-06` returns 200; HTML body contains the rendered markdown of the closed invoice (total, line items, Stripe usage record ID).

**A-tier (2 named tests — spec §6 T5):**

4. `TestBillingHandler_NoStoreCache` — both responses have `Cache-Control: no-store` header. Pins spec §3.6 route table verbatim.
5. `TestBillingHandler_OrdersByPeriodDescending` — multi-period seed; rendered table rows sorted by `period_start` DESC then `tenant_id` ASC. Audit-readability invariant.

**A+-tier (1 named test — spec §6 T5):**

6. `TestBillingHandler_PreW8_OperatorOnly_PostW8_TenantScoped` — config-flag-driven test: with `rbac.w8_enabled=false` (default), operator cookie passes for any tenant_id path; with `rbac.w8_enabled=true`, handler asserts `cookie.TenantID == path.TenantID` else 403. Pins spec §5 R4 mitigation.

Total T5: **6 named tests** (3 B + 2 A + 1 A+). PR body lists every test + pasted failing-output for AT LEAST 4.

### PR body skeleton

````
## Summary

MVP-4 W12 Billing T5 ships the operator UI billing tab per spec §3.6.
Read-only `/billing` + `/billing/{tenant_id}/{period}` routes;
substrate-as-source-of-truth (NOT filesystem); no close button.

- internal/web/billing.go — RegisterBillingRoutes + GET handlers;
  Fold-read billing_period_closed events; LWW collapses duplicates.
- internal/web/templates/billing.tmpl — spec §3.6 verbatim.
- internal/web/routes.go — additive two-route registration (≤ 6 LoC).

## Why

Per spec §3.6: read-only by deliberate discipline — close ritual is
CLI-only (T4) inside the operator audit envelope. UI close button
invites the "click twice → double-bill" trap. Substrate is the
source of truth so the markdown file can be regenerated at any time.

## Test plan

- [x] TestBillingHandler_OperatorAuth_Required                  (R4)
- [x] TestBillingHandler_ListsAllClosedPeriods
- [x] TestBillingHandler_RendersOneInvoice
- [x] TestBillingHandler_NoStoreCache
- [x] TestBillingHandler_OrdersByPeriodDescending
- [x] TestBillingHandler_PreW8_OperatorOnly_PostW8_TenantScoped  (A+)
- [x] make pre-push-check clean.

## Failing-test output (TDD capture, before impl)

<paste from terminal; min 4 reps>

## A+ scorecard

- B1 + B2 exit 0.
- A6 ZERO new migrations.
- A1 reviewer cleared.

## Deletion default

T5 ELIMINATES every per-tenant authentication primitive that a
self-serve billing page would have required: pre-W8 the route is
operator-only via the existing W7 cookie HMAC; no new auth code. The
no-close-button discipline ELIMINATES a double-submit guard + an
idempotency token cookie + a rate-limited POST handler.

## Followup issues filed

Cites pre-filed D3 + D7 [billing-followup].

```release-notes
[FEATURE] operator UI billing tab (read-only /billing route; lists tenants × periods; renders invoice markdown via goldmark; substrate-as-source-of-truth)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w12-t5. Branch off main AFTER W7 Wave 1 +
T4 merged:

  git fetch origin
  git checkout -b feat/w12-t5-ui origin/main

# Pre-flight verification

Verify W7 Wave 1 (T4-T7) lands on main:
  grep -rn "RegisterRoute\|ParseFS\|cookie-HMAC\|cookieHMAC" internal/web/

Verify T4 merged:
  grep -rn "billing close" cmd/regatta/

If either missing, STOP and report — main session must complete
prereqs before T5 dispatches.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w12-billing-design.md.
Read: §1 goal #5 (operator-facing billing dashboard), §3.6 (UI tab
VERBATIM — route table + data source SUBSTRATE-ONLY + template +
no-close-button discipline), §3.7 (payload struct for Fold decode),
§5 R4 (tenant-leak pre-W8 operator-only enforcement), §6 T5 (named
test list), §9 sequencing.

Per feedback_spec_pattern_authority: if you want to deviate (e.g.
add a close button; read from filesystem instead of substrate; add
a tenant-scoped public route pre-W8), STOP and report.

# Scope (exclusive write paths)

- internal/web/billing.go              (NEW)
- internal/web/billing_test.go         (NEW)
- internal/web/templates/billing.tmpl  (NEW; spec §3.6 verbatim)
- internal/web/routes.go               (additive ≤ 6 LoC; do NOT modify existing handlers)

You MUST NOT touch any other file. Specifically: Do NOT touch
internal/billing/* — Wave A scope. Import only. Do NOT touch
cmd/regatta/ — T4's scope.

# Patterns to reuse

- W7 cookie-HMAC middleware — grep internal/web/ to find.
- W7 embed.FS template loader — add billing.tmpl to the parse-fs call.
- W7 layout.tmpl shell.
- goldmark for markdown → HTML.
- substrate.Fold for LWW reads.
- event.BillingPeriodClosedPayload — T1 import.

# Tests to land (6 named)

B-tier (3):
1. TestBillingHandler_OperatorAuth_Required                    (R4)
2. TestBillingHandler_ListsAllClosedPeriods
3. TestBillingHandler_RendersOneInvoice

A-tier (2):
4. TestBillingHandler_NoStoreCache
5. TestBillingHandler_OrdersByPeriodDescending

A+-tier (1):
6. TestBillingHandler_PreW8_OperatorOnly_PostW8_TenantScoped

# Workflow

1. TDD per test (min 4 failing-output reps in PR body).
2. make pre-push-check clean.
3. ls migrations/ unchanged.
4. Grep banned phrases + ```release-notes``` fence.
5. Open PR `gh pr create --base main --title
   "feat(w12): T5 operator UI billing tab"
   --body-file <path>`. A+ scorecard verbatim; cite D3 + D7.
6. Adversarial reviewer; apply findings.
7. CI green BEFORE automerge; `gh pr merge --auto --squash`.

# Adversarial reviewer hunt list

- Data source SUBSTRATE-ONLY. Handler does NOT shell out to read
  markdown files. Spec §3.6 line 289 verbatim.
- LWW collapses duplicate billing_period_closed events; Fold returns
  one row per (tenant_id, period_start).
- No close button anywhere in the template; UI is passive. Spec §3.6
  line 313.
- Cookie HMAC reuse — no new auth code. Spec R4 line 404.
- Cache-Control: no-store on both routes.
- Order: period_start DESC then tenant_id ASC.
- Pre-W8 vs post-W8 tenant-scoping config-flag-gated.
- Template VERBATIM to spec §3.6 lines 294-311.
- Cyclic-import: `go list -deps ./internal/web/` shows no edge into
  cmd/regatta or internal/billing/stripe.
- Simplification opportunity: could the handler render markdown on
  the fly without goldmark? No — W7 already vendors goldmark; reuse.
- Deletion default: PR body cites no per-tenant auth, no close
  button, no double-submit guard.
- No AI signatures.
- godocs ≤ 1 line.

# Hygiene

- NO AI signatures.
- WHY not WHAT.
- doc-check + stale-todo exit 0.

# Return format

- PR URL.
- Failing-test output (min 4 of 6).
- Reviewer verdict.
- Diff stat.

Begin now. NEVER pause for user input.
```

---

## §7 Task T6 — OTel + operator doc

### Scope

- **`docs/operator/billing.md`** — NEW. ~300 lines. Cover:
  - Close ritual end-to-end walkthrough (CLI invocation; expected stdout; OTel span tree).
  - Stripe webhook reconciliation runbook (manual: "operator emails customer with `usage_record` ID; customer matches against Stripe Dashboard invoice"). Per R1 deferral.
  - Invoice file location convention (`<invoice_dir>/<tenant_id>/<YYYY-MM>.md`); .regatta is gitignored by default; redirecting to a mounted volume for retention.
  - Refund handling: Stripe Dashboard is source of truth; regatta substrate is append-only history. Per R5.
  - Reconciliation lag note (R7): operator runs close ON OR AFTER first instant of next month UTC + 1 reconciler-tick interval.
  - Stripe Price object configuration: `unit_amount: 1, billing_scheme: per_unit` (so 1 cent regatta = 1 cent Stripe). Per R6.
  - Dispute workflow v1: file-based (tenant emails operator; operator amends substrate manually; re-runs close). Per spec §1 non-goal "Invoice contestation workflow".
  - Multi-currency limitation: USD only v1. Per R6 spec lines.
  - Period-shape lock: do not change `billing.period` after first close. Per R3.
  - OTel cardinality recommendation: 1 `billing.close` + N `billing.tenant.close` per invocation; no per-line-item span.
- **Span emission verification:** T6 does NOT write any Go code. Instead, T6 grep-asserts that T1 + T4 emit the spec §3.8 spans. If a span is missing OR has incorrect attrs, T6 files a tracking issue + cites in PR body (does NOT fix in-place; that is T1 or T4's scope).

### Prereqs (cite spec sections)

- Spec §3.8 lines 348-358 — OTel spans + attrs.
- Spec §1 goal #6 — OTel + operator doc.
- Spec §5 R1, R5, R6, R7 — operator-doc-as-mitigation.
- Spec §6 T6 lines 443-446 — named test list.
- Spec §9 sequencing — T6 lands AFTER T1-T5.

### Existing patterns to reuse (do NOT reinvent)

- **`docs/operator/cost-governor.md`** (cost-gov W3 T5) — same format conventions: env-var contract; precedence rule; runbook examples; OTel attr recommendations.
- **`bash scripts/doc-check.sh`** — banned-phrase + markdown-link gates.
- **`bash scripts/stale-todo.sh`** — TODO sweep.

### TDD test list

**B-tier (2 named tests — spec §6 T6):**

1. `TestOTel_BillingCloseSpan_Emitted` — fake OTel exporter; run a close cycle; assert one span named `billing.close` with attrs `regatta.billing.period_start`, `regatta.billing.period_end`, `regatta.billing.tenant_count`, `regatta.billing.total_usd_aggregate`, `regatta.billing.stripe_enabled`. (NOTE: this test SHOULD already exist in T4's suite as `TestCLI_MultiTenant_PerTenantSpan`; T6 verifies it does + files a tracking issue if not.)
2. `TestOTel_StripePushSpan_Attached` — assert `billing.stripe.push` span has parent `billing.tenant.close` with attrs per spec §3.8.

**A-tier (2 named tests — spec §6 T6):**

3. `TestOperatorDoc_LintsAgainstBannedPhrases` — `bash scripts/doc-check.sh` exit 0 against `docs/operator/billing.md`.
4. `TestOperatorDoc_CoversRefundRunbook` — grep-assert `docs/operator/billing.md` contains a section heading matching `## Refund` (per R5 mitigation).

**A+-tier (1 named test — spec §6 T6):**

5. `TestOperatorDoc_LinksAllResolveOnDisk` — already covered by `scripts/doc-check.sh` markdown-link gate. T6 confirms.

Total T6: **5 named tests** (2 B + 2 A + 1 A+). Doc-only PR — skip ceremony per `feedback_review_proportional`. ONE reviewer pass.

### PR body skeleton

````
## Summary

MVP-4 W12 Billing T6 ships the operator doc per spec §1 goal #6 +
verifies T1/T4 emit the spec §3.8 OTel spans.

- docs/operator/billing.md — close ritual walkthrough + refund
  runbook (R5) + period-close timing (R7) + Stripe Price
  configuration (R6) + period-shape lock (R3) + multi-currency
  limitation note + OTel cardinality recommendation.
- Span emission verification: grep-asserts T1 + T4 emit
  billing.close + billing.tenant.close + billing.stripe.push per
  spec §3.8 table. (No code edits in T6; tracking issue filed if
  any span is missing.)

## Why

Per spec §1 goal #6 + §3.9 deletion default: operator doc is the
final wedge artifact. It closes every R-tier mitigation gap that
requires operator action (R1 webhook deferral; R5 refunds; R6 Stripe
Price; R7 timing; R3 period-shape lock).

## Test plan

- [x] TestOTel_BillingCloseSpan_Emitted        (verifies T4)
- [x] TestOTel_StripePushSpan_Attached         (verifies T4)
- [x] TestOperatorDoc_LintsAgainstBannedPhrases
- [x] TestOperatorDoc_CoversRefundRunbook
- [x] TestOperatorDoc_LinksAllResolveOnDisk

## Doc-only PR — ceremony skipped

Per feedback_review_proportional: doc-only PR skips A+ scorecard +
multi-reviewer. ONE adversarial reviewer pass at PR open; done.

```release-notes
[DOC] operator billing runbook (close ritual + Stripe Price config + refund handling + period-close timing)
```
````

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w12-t6. Branch off main AFTER T1-T5 merged:

  git fetch origin
  git checkout -b docs/w12-t6-operator-doc origin/main

# Pre-flight verification

Verify T1-T5 merged:
  ls internal/billing/{rollup,stripe,invoice,event}/
  ls cmd/regatta/billing.go
  ls internal/web/billing.go

If any missing, STOP and report.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w12-billing-design.md.
Read: §1 goal #6, §3.8 (OTel spans), §3.9 (deletion default audit),
§5 R1 (webhook deferral), R3 (period-shape lock), R5 (refund
runbook), R6 (Stripe Price config), R7 (period-close timing), §6
T6 (named test list).

Per feedback_spec_pattern_authority: if you want to deviate (e.g.
add a billing CLI to docs that doesn't exist; rewrite span names),
STOP and report.

# Scope (exclusive write paths)

- docs/operator/billing.md  (NEW)

You MUST NOT touch any other file.

# Patterns to reuse

- docs/operator/cost-governor.md format conventions.
- Markdown link integrity via scripts/doc-check.sh.

# Tests to land (5 named)

B-tier (2):
1. TestOTel_BillingCloseSpan_Emitted          (verifies T4-emitted span)
2. TestOTel_StripePushSpan_Attached           (verifies T4-emitted span)

A-tier (2):
3. TestOperatorDoc_LintsAgainstBannedPhrases
4. TestOperatorDoc_CoversRefundRunbook

A+-tier (1):
5. TestOperatorDoc_LinksAllResolveOnDisk

# Workflow

1. Draft docs/operator/billing.md.
2. bash scripts/doc-check.sh exit 0.
3. bash scripts/stale-todo.sh exit 0.
4. Span verification: grep -rn 'billing.close\|billing.tenant.close\|billing.stripe.push' internal/billing/ cmd/regatta/ — every spec §3.8 span name appears. If missing, file tracking issue + cite in PR body.
5. Grep banned phrases on PR body BEFORE push.
6. Grep ```release-notes``` fence presence.
7. Open PR `gh pr create --base main --title
   "docs(w12): T6 operator billing runbook"
   --body-file <path>`.
8. ONE adversarial reviewer pass (doc-only — ceremony skipped per
   feedback_review_proportional).
9. CI green BEFORE automerge; `gh pr merge --auto --squash`.

# Adversarial reviewer hunt list

- Every R-tier mitigation that names operator-doc as its fix has a
  section in billing.md (R1 webhook deferral note; R3 period-shape
  lock; R5 refund runbook; R6 Stripe Price config; R7 timing).
- Banned-phrase lint clean (scripts/doc-check.sh).
- Every markdown link resolves on disk.
- Span verification grep finds every spec §3.8 span name in T1/T4
  code.
- No AI signatures.

# Hygiene

- NO AI signatures.
- doc-check + stale-todo exit 0.

# Return format

- PR URL.
- Reviewer verdict.
- Diff stat.
- Confirmation: every spec §3.8 span name found in code (or
  tracking issue cited).

Begin now. NEVER pause for user input.
```

---

## §8 Followup issue templates (pre-enumerated; main session files PRE-WAVE-A)

Per `feedback_parallel_dup_followups`: main session pre-files these 10 followups BEFORE Wave A dispatches, so every Wave A PR can cite the existing issue numbers. Per `feedback_unaddressed_load_bearing`: every load-bearing item gets a tracking issue + acceptance criteria.

### D1 — Multi-currency support

```
Title: [billing-followup] multi-currency support (EUR/GBP/JPY price IDs)
Label: billing-followup
Body:

Spec source: docs/engineer/specs/2026-06-01-w12-billing-design.md §1 non-goal + §10 D1.

Load-bearing: blocks non-US customer onboarding.

Acceptance criteria:
- regatta.yaml billing.stripe block accepts per-tenant currency override (default USD).
- TokenSpendPayload + BillingPeriodClosedPayload carry currency code (ISO 4217); pre-W12 rows default to USD for backward compat.
- Stripe Price object lookup resolves per (tenant, currency) tuple.
- Operator runbook covers FX-rate sourcing (deferred: Stripe handles via Tax/FX, OR per-jurisdiction operator config).

Out of scope: real-time FX conversion; per-line-item currency mixing.

Triggers fix when: at least one EUR/GBP/JPY customer signs.
```

### D2 — Mid-period proration

```
Title: [billing-followup] mid-period proration support
Label: billing-followup
Body:

Spec source: §1 non-goal + §10 D2.

Load-bearing: any pilot conversion mid-month gets a $0 invoice for the partial month in v1 (explicit operator workaround).

Acceptance criteria:
- billing.proration: true config flag.
- Tenant onboard timestamp tracked (substrate kind tenant_onboarded or read from existing event).
- Rollup pro-rates the first partial period by day-of-month / days-in-month.
- Stripe Price configured as per-cent unit (already true per R6); proration is a quantity adjustment.

Out of scope: weekly/quarterly period proration.
```

### D3 — Invoice contestation UI

```
Title: [billing-followup] in-UI invoice contestation
Label: billing-followup
Body:

Spec source: §1 non-goal + §10 D3.

Load-bearing for tenant self-serve story (depends on D7 + W8 RBAC).

Acceptance criteria:
- /billing/{tenant_id}/{period}/contest POST endpoint (tenant-cookie auth).
- Substrate kind invoice_contested with payload {invoice_ref, contested_line_item_id, reason}.
- Operator UI surfaces contested invoices on a separate tab.
- Resolution = operator amends substrate event + re-runs regatta billing close.

Out of scope: dispute SLA tracking; automated refund routing.
```

### D4 — PDF invoice rendering

```
Title: [billing-followup] PDF invoice rendering
Label: billing-followup
Body:

Spec source: §1 non-goal + §10 D4 + §3.4 decision tree.

Acceptance criteria:
- regatta billing close generates both .md and .pdf outputs.
- PDF renderer choice documented (gofpdf vs chromedp vs goldmark→HTML→print).
- File 0o600 perms preserved.

Decision tree captured in spec §3.4 lines 198-201:
- gofpdf — pure Go, MIT, hermetic; layout verbose.
- chromedp headless Chrome — high-fidelity HTML→PDF; ~200MB runtime.
- markdown→HTML via goldmark + browser-side print — defer printing to operator.

Triggers fix when: at least one customer asks for a PDF.
```

### D5 — Stripe webhook ingress

```
Title: [billing-followup] Stripe webhook ingress
Label: billing-followup
Body:

Spec source: §1 non-goal + §10 D5 + §5 R1.

Load-bearing for dispute/refund reflection per R5.

Acceptance criteria:
- POST /billing/webhook handler with Stripe signature verification.
- Substrate kind stripe_webhook_received; reducer lww on event.id (R1 replay dedup).
- Operator-doc runbook: refund reflection via webhook → substrate.

Out of scope: non-Stripe webhook providers (Bedrock, etc).
```

### D6 — OpenMeter adapter

```
Title: [billing-followup] OpenMeter adapter for granular usage events
Label: billing-followup
Body:

Spec source: §1 non-goal + §10 D6 + brief §"W12 — Metered billing".

Load-bearing only if a customer demands hourly/daily granular usage events.

Acceptance criteria:
- internal/billing/openmeter/ adapter mirrors internal/billing/stripe/ shape.
- regatta.yaml billing.openmeter block (HTTP endpoint + auth token).
- Rollup job emits granular events (per budget_reconciled row) to OpenMeter in addition to monthly Stripe push.

Out of scope: replacing Stripe push with OpenMeter push.
```

### D7 — Tenant self-serve UI

```
Title: [billing-followup] tenant self-serve billing page
Label: billing-followup
Body:

Spec source: §1 non-goal + §10 D7 + §5 R4.

Load-bearing: depends on W8 RBAC tenant_id cookie claim.

Acceptance criteria:
- /tenant/{tenant_id}/billing route with tenant-cookie auth.
- Handler asserts cookie.TenantID == path.TenantID else 403.
- Read-only listing of the tenant's own invoices.

Out of scope: cross-tenant visibility.
```

### D8 — Backfill recipe

```
Title: [billing-followup] historical period backfill recipe
Label: billing-followup
Body:

Spec source: §10 D8 + §5 R8.

Acceptance criteria:
- docs/operator/billing.md gains a "Backfill" section.
- regatta billing close --period <past> works for any historical period (LWW on substrate; idempotent re-run).
- Worked example: closing 1000 tenants × 12 periods in a single bulk run; expected runtime ≤ make-check 60s budget per spec §5 R8.

Out of scope: parallel-per-tenant close.
```

### D9 — Stripe SDK refresh runbook

```
Title: [billing-followup] Stripe SDK refresh runbook
Label: billing-followup
Body:

Spec source: §10 D9 + §5 R9.

Acceptance criteria:
- docs/operator/billing.md gains a "Quarterly Stripe SDK refresh" section.
- Pin upgrade cadence (quarterly).
- Compatibility-test script: run go test ./internal/billing/stripe/ against the latest v76.X.Y; if green, bump go.mod.

Out of scope: auto-upgrading to v77+ (major bump = adapter rewrite).
```

### D10 — Tax computation

```
Title: [billing-followup] per-jurisdiction tax integration
Label: billing-followup
Body:

Spec source: §1 non-goal + §10 D10.

Load-bearing only if customer is in a non-Stripe-Tax-covered jurisdiction.

Acceptance criteria:
- regatta.yaml billing.tax block (provider: stripe-tax | manual | none).
- For manual: operator config provides per-jurisdiction rate; rollup applies before Stripe push.
- For stripe-tax: rely on Stripe's tax engine (current v1 default).

Out of scope: tax-authority reporting integration.
```

---

## §9 Adversarial-review pass (applied inline)

Per `feedback_adversarial_review` + `feedback_simplify_reviewer` + `feedback_agent_pr_review`: one reviewer subagent ran against this plan draft. Findings + applied fixes below.

1. **Shared-primitive-owner hunt.** Initial draft had T2 and T3 each defining `LineItem` locally. Fix: T1 owns every `event.*` payload struct + the validator dispatch entry; T2/T3/T4/T5 import. Documented in §1 Cross-task seam contracts.

2. **Migration audit.** Re-checked spec §3.7 + §7 A6: ZERO new migrations. Every task scope row now explicitly states "Do NOT create a SQL migration"; Wave overview line "ZERO new SQL migrations across all six tasks"; T1 dispatch hunt list checks `ls migrations/ | wc -l` identical to main.

3. **Three-layer idempotency test coverage.** Spec §3.5 lines 262-267 names three layers; initial draft had T4 testing only substrate + Stripe. Fix: T4 test list now includes `TestCLI_DoubleRun_InvoiceOverwritten` (reviewer-added) for filesystem layer. T4 dispatch prompt cites all three layers as load-bearing.

4. **CUE schema ownership.** Spec §3.1 adds a `#Billing` block to `contracts/schemas/regatta.v1.cue`. Initial draft did not specify which task owns this file. Fix: T4 owns it (since T4 is the first task to actually READ the config block). T4 dispatch prompt has a pre-flight grep check.

5. **Followup pre-filing.** Spec A+3 requires ≥ 6 `[billing-followup]` issues at first-PR-merge time. Initial draft had implementers each file their own. Fix: per `feedback_parallel_dup_followups`, main session pre-files all 10 (D1-D10) BEFORE Wave A dispatch; Wave A PR bodies cite the issue numbers. Eliminates duplicate-filing race.

6. **W7 prereq verification.** T5 depends on W7 Wave 1. Initial draft assumed W7 had landed. Fix: T5 dispatch prompt has a pre-flight grep check for cookie-HMAC + `ParseFS` + route-registration patterns; STOPs if W7 not landed.

7. **Stripe SDK version pinning.** Spec R9 says don't use `latest`. Initial draft did not test this. Fix: T2 reviewer-added test `TestStripeAdapter_VersionPinned_NotLatest` parses `go.mod` to assert all-integer version pin.

8. **Cyclic-import audit.** `internal/billing/event` could grow a cycle if anyone imports it back from `internal/orchestrator/state/substrate`. Fix: §1 Cross-task seam contracts has an explicit "No cyclic imports" subsection listing every allowed edge; T1 dispatch hunt list runs `go list -deps`.

9. **Stripe-mock test backend.** T2 needs a hermetic Stripe backend. Fix: T2 scope includes `testdata/stripe-mock.sh` helper that boots the stripe-mock binary; dispatch prompt names the test backend.

10. **Operator-only enforcement pre-W8.** Spec R4 says `/billing/{tenant_id}/...` is operator-only pre-W8 then tenant-scoped post-W8. T5 A+ test covers this with a config-flag-driven assertion. Confirmed in test 6 of T5.

11. **Doc-check banned phrases.** Plan body grep-checked at write time against the banned-phrase token list in `scripts/doc-check.sh` (per `feedback_doc_check_banned_phrases` — 11-token marketing-prose lint). Hits reworded throughout to falsifiable claims. Every dispatch prompt + PR body skeleton cites the grep step pre-push.

12. **Release-notes fence universal.** Every PR body skeleton ends with ` ```release-notes ... ``` ` fence per `feedback_pr_body_release_notes_fence`. T6 doc-only PR fence reads `[DOC] operator billing runbook ...`; Wave A PRs read `[FEATURE]`.

13. **No AI signatures.** Every dispatch prompt + PR body skeleton ends with the explicit "NO AI signatures" reminder.

14. **T6 doc-only ceremony skip.** Per `feedback_review_proportional`: T6 is a doc-only PR; skip A+ scorecard + multi-reviewer. ONE adversarial reviewer pass at open. Documented in §7.

15. **Wave A concurrency cap.** 4 parallel implementers within the 10-lane ceiling (`feedback_dispatch_strategy` max velocity). Safe — every task is single-package, single-PR.

16. **T6 doc covers every R-tier operator-fixable risk.** R1 (webhook deferral), R3 (period-shape lock), R5 (refund), R6 (Stripe Price), R7 (timing) all named-and-mitigated by operator-doc per spec. T6 test 4 grep-asserts the refund section. Other sections asserted by the doc-check link-integrity gate.

---

_Plan authority: this plan is a dispatch artifact only. The main session copy-pastes the §2-§7 dispatch prompts into Agent tool invocations once prereqs are confirmed. Implementer subagents are accountable for their PR; main session is accountable for sequencing + Wave A concurrency + automerge gating per `feedback_review_before_automerge`._
