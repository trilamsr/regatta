---
title: "MVR-3-T2 Stripe Metering — billing adapter (skeleton-tier pre-fetch)"
status: skeleton-prefetch
summary: Pre-fetch skeleton for MVR-3-T2 Stripe-metered-usage wedge gated behind the P3.8 billing adapter; full spec re-spawns when MVR-3 trigger fires (5 paying customers OR billing-specific customer ask). Locks scope, prior-art, risks, test plan, dep-order so trigger-time dispatch is fill-in rather than green-field.
---

# MVR-3-T2 Stripe Metering — billing adapter (skeleton-tier pre-fetch)

_Author: design subagent, 2026-06-03. Skeleton-tier per `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 Phase MVR-3 row T2 (M, 2-3 wks, dep=Stripe SDK). This spec is the pre-fetch contract; it does NOT dispatch implementer subagents._

Cites: `feedback_research_design_principles` (adopt stripe-go > bespoke billing), `feedback_decision_priority` (UX > ease > performance > best-practices > velocity), `feedback_grade_rubric`, `feedback_deletion_default`, `feedback_migration_number_lock` (zero new migrations — substrate-only), `feedback_spec_pattern_authority`.

Prior-art baseline: `docs/engineer/specs/2026-06-01-w12-billing-design.md` (50 KB Wave 1 design) is the source-of-truth for the full surface. This skeleton inherits its decisions and re-litigates only the MVR-3 slice.

---

## 0. Scope (in / out)

### In scope (MVR-3-T2)

- `internal/billing/billing.go` interface `{PushUsage(ctx, tenant, period, usd) error; CloseRecord(ctx, ...) error}` behind the P3.8 billing adapter seam. Two impls: `noop` (default; logs only) and `stripe` (real metered-usage push).
- Per-tenant monthly USD rollup over substrate `events.kind='budget_reconciled'` (no new table — cost-governor already writes this).
- One new substrate event `kind='billing_period_closed'` (reducer = `lww` by `tenant_id, period_start`). Substrate primitive, ZERO migrations.
- Stripe metered-usage push via `subscription_item.create_usage_record`. Idempotency key = `sha256(tenant_id || period_start)`.
- Operator CLI `regatta billing close --period YYYY-MM`. Read-only `/billing` route on W7 UI (lists closed periods; no UI-triggered Stripe push).
- Invoice markdown render at `.regatta/invoices/<tenant>/<YYYY-MM>.md` (deterministic, byte-stable).

### Out of scope (MVR-3-T2)

- PDF invoice rendering (follow-up wedge per W12 §10 D1; markdown-only v1).
- Anthropic Usage API reconciliation (deferred to MVR-4 once a customer audits the rollup).
- Real-time burn meter on operator UI (cost-governor already covers this; W12 adds historical view only).
- Multi-currency (USD-only v1; EUR/GBP behind a followup once a non-US customer asks).
- Streaming Stripe webhook receiver (push-only v1; webhook reconciliation lands when Stripe disputes happen).
- Tax calculation (Stripe Tax integration — followup).

## 1. Prior art (cite version + license)

| Primitive | Adopted from | Version | License | What we take |
|---|---|---|---|---|
| Stripe SDK | [stripe-go](https://github.com/stripe/stripe-go) | v76 (latest stable as of 2026-06) | MIT | `subscription_item.create_usage_record` shape; idempotency-key header semantics |
| Metered-usage model | [Stripe Metered Billing](https://stripe.com/docs/billing/subscriptions/metered) | n/a | Stripe docs | Per-subscription-item usage records; monthly aggregation by Stripe |
| Invoice markdown shape | `docs/operator/billing.md` (this repo, to be authored at impl time) | n/a | repo-internal | Header + table + footer template; reused from S3-T3 operator-doc style |
| Substrate event reducer | `internal/substrate/reducer.go::lww` (regatta, shipped Wave 1) | n/a | repo-internal | `lww` reducer for `billing_period_closed` keyed by `(tenant_id, period_start)` |
| Cost-governor rollup source | `docs/engineer/specs/phase-x/2026-06-01-cost-governor-design.md` §3.4 (Wave 2 reconciler) | n/a | repo-internal | `budget_reconciled` event is the canonical USD ground-truth — billing is pure consumer |

Rejected alternatives: bespoke Stripe-API HTTP client (re-implementing stripe-go's idempotency + retry); Lago / OpenMeter (adds infra surface a single-binary regatta should not depend on); per-tenant cron writing usage rows in-app (cost-governor reconciler is already this loop).

## 2. Architecture (high-level)

```
internal/billing/
  billing.go         // interface + Register()
  noop.go            // default impl; logs USD totals
  stripe.go          // stripe-go-v76 impl; behind build tag if dep weight concerns
  rollup.go          // sum events.kind='budget_reconciled' over (tenant, period)
  invoice.go         // deterministic markdown render
cmd/regatta/billing.go     // CLI: regatta billing close --period YYYY-MM
internal/web/routes/billing.go  // read-only /billing route on W7 UI
```

Close ritual (single transaction per tenant):

1. Sum `budget_reconciled` for `(tenant_id, period)`.
2. Emit `events.kind='billing_period_closed'` (lww).
3. Render markdown invoice to disk.
4. Push Stripe `create_usage_record` AFTER substrate commit (Stripe idempotency + substrate UNIQUE-on-(tenant_id, period_start) make re-run byte-equal).

## 3. Key risks (≥6 named)

| # | Risk | Mitigation |
|---|---|---|
| R1 | Stripe API outage at close time | Substrate commit happens FIRST; Stripe push is retryable; idempotency key dedupes |
| R2 | Pricing-applied-twice (cost-governor §9 R-A4) | Billing reads `budget_reconciled` directly; never re-applies pricing; rollup is pure SUM |
| R3 | stripe-go dep brings >40 transitive deps | Build-tag gated (`-tags stripe`); default build uses noop impl; bloat measured at PR time |
| R4 | Operator triggers close twice for same period | Substrate UNIQUE constraint on `(tenant_id, period_start)` for `billing_period_closed` → second close errors loudly |
| R5 | Currency confusion (USD vs cents) | Stripe SDK takes integer-cents; rollup converts at the SDK boundary, never internally |
| R6 | Tenant deleted between rollup and Stripe push | Soft-delete only at substrate layer; push uses `subscription_item_id` snapshot at close time |
| R7 | Time-zone drift on period boundary | All periods are calendar-month UTC; operator config can override, defaults UTC |
| R8 | Stripe rate-limit at scale (>100 tenants close simultaneously) | Push loop is serial per binary; concurrency cap config-driven (default 1, max 10) |
| R9 | Test pollution — real Stripe API hit from CI | Default `--dry-run` in test mode; Stripe key sourced only from `STRIPE_SECRET_KEY` env at runtime |

## 4. Test plan (≥8)

1. `TestBillingClose_NoopAdapter_RollupOnly` — close period; assert markdown invoice + substrate event; no external call.
2. `TestBillingClose_StripeAdapter_StubbedAPI` — mock Stripe HTTP endpoint; assert `create_usage_record` called with correct idempotency key.
3. `TestBillingClose_IdempotentOnRetry` — close same period twice; second call no-ops at Stripe (idempotency key dedupe) and errors at substrate (UNIQUE constraint).
4. `TestBillingRollup_SumsBudgetReconciled` — seed 5 `budget_reconciled` events; assert rollup = SUM.
5. `TestBillingRollup_TenantScoped` — events across 3 tenants; rollup returns one row per tenant.
6. `TestBillingInvoice_Deterministic` — render twice; bytes identical (no clock-stamped fields beyond `period`).
7. `TestBillingClose_FailsClosed_OnMissingStripeKey` — stripe adapter + no env var → loud error, no partial write.
8. `TestBillingClose_StripePushAfterSubstrateCommit` — fault-inject Stripe failure; assert substrate row exists, retry on next close call dedupes.
9. `TestBillingWebRoute_ReadOnly` — GET /billing returns table; POST /billing returns 405.
10. `BenchmarkBillingRollup_1kTenants` — rollup p99 ≤ 100ms at 1k tenants × 30 days.
11. `FuzzInvoiceTemplate` — random tenant/period inputs render to valid markdown (no template injection).

## 5. Dep order

1. **MUST be merged first:** P3.8 billing-adapter seam (`docs/engineer/specs/2026-06-01-adapter-contracts-design.md`) — same seam as MVR-3-T1 signer.
2. **MUST be merged first:** Cost-governor Wave 2 reconciler (`docs/engineer/specs/phase-x/2026-06-01-cost-governor-design.md` §3.4) — billing is a pure consumer of `budget_reconciled` events.
3. **MUST be merged first:** Substrate Wave 1 (`substrate_events`) — billing rides the substrate; ZERO new migrations.
4. **SHOULD be merged first:** W7 Wave 2 admin pages (`docs/engineer/specs/2026-06-01-w7-wave2-admin-pages-design.md`) — reuses the embed.FS template loader + cookie-HMAC middleware for the `/billing` route.
5. **No dep on MVR-3-T1 / T3 / T4** — Stripe metering is orthogonal to Sigstore, blackboard, and research-mode.
6. **Trigger:** MVR-3 entry per roadmap §4 (5 paying customers OR billing-specific ask).

## 6. Grade rubric (filled at dispatch time)

| Criterion | B (must) | A (should) | A+ (aspires) |
|---|---|---|---|
| `make check` clean | _filled at dispatch_ | _filled_ | _filled_ |
| ZERO new migrations | _filled_ | _filled_ | _filled_ |
| Close ritual byte-equal on re-run | _filled_ | _filled_ | _filled_ |
| Adapter swap is one-line config | _filled_ | _filled_ | _filled_ |
| Deletion ledger | _filled_ | _filled_ | _filled_ |
| Operator runbook covers Stripe-down failure mode | _filled_ | _filled_ | _filled_ |

## 7. What got smaller

Skeleton-tier defers PDF rendering + Anthropic Usage API reconciliation + multi-currency + tax to followups. MVR-3-T2 ships ONLY the per-tenant monthly USD rollup + Stripe push + close ritual — minimum surface that closes the "metered billing" claim blocking MVR-3 persona-B/C/D revenue.
