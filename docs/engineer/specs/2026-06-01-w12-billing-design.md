# MVP-4 W12 Billing (per-tenant USD rollup + Stripe metered-usage) — Design Spec

Status: ready for review
Date: 2026-06-01
Author: design subagent <tri@maydow.com>
Issue umbrella: TBD (this spec stands up the umbrella)
Depends on:
- **Hard prereq (must be merged):** Cost Governor Wave 2 — `docs/engineer/specs/2026-06-01-cost-governor-design.md` §3.4 + §3.5. Specifically the T3 reconciler tick that emits `events.kind='budget_reconciled'` (lww by `tenant_id, period_start`) and the T4 substrate writer wiring. W12 is a pure CONSUMER of those events — this wedge writes no `budget_reconciled` rows, only reads them.
- **Hard prereq (must be merged):** W7 Operator Web UI Wave 1 — `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` §3.2 (`internal/web/` package) + §3.3 (route table) + §3.5 (templates) + W7.0 HTTP listener landed in #263. Billing UI tab adds one route alongside the existing `/runs/...` and `/approve/...` routes; the embed.FS template loader + cookie-HMAC auth middleware + Tailwind/htmx asset pipeline are all reused verbatim.
- **Hard prereq (merged):** Unified Substrate v2 — `docs/engineer/specs/2026-06-01-unified-substrate-design.md`. W12 adds one new event kind `billing_period_closed` (reducer = `lww` by `tenant_id, period_start`). No DDL change — kind is a string column per substrate §2.
- **Soft prereq:** W8 RBAC `tenant_id` propagation. Until W8, every billing read filters on `substrate.DefaultTenantID`; the swap is one-line per the substrate forward-fit pattern.

Binding brief: `docs/engineer/briefs/2026-05-31-mvp-3-next-level.md` §1 ("No billing/usage export: cost governor records spend internally; no per-tenant invoice CSV, no Stripe metered-billing webhook, no chargeback report") + §"W12 — Metered billing + tenant usage export" (rank #7): per-tenant USD + token rollups exported as Stripe metered-billing events; reconciles against Anthropic Usage API; operator config `regatta.yaml: billing.export: stripe://...` → monthly invoices generated automatically; tenants see real-time burn in W7 UI; effort estimate "medium — ~4 file-disjoint tasks".

Roadmap fit: brief §5 thread #2 ("W12 = usage rollups aggregate spans") + §"Adapter contracts for swap-out" (Stripe billing ships as an interface with default in-binary impl + adapter for hosted service). Cost-governor wedge §2 OOS list explicitly names "Streaming Stripe metered-billing webhook: brief §4 W12 explicitly owns this" — this spec picks up that hand-off.

Memory rules in force: `wedge_cost_governor` (prior-art patterns + reconciler ground truth), `wedge_roadmap_assessment` (MVP-4 W12 placement), `feedback_research_design_principles` (adopt proven OSS Stripe SDK over bespoke), `feedback_decision_priority` (UX > best-practices > velocity), `feedback_grade_rubric` (B/A/A+ tool-checkable), `feedback_migration_number_lock` (ZERO new migrations — substrate-only), `feedback_doc_check_banned_phrases` (no marketing language), `feedback_deletion_default` (every PR answers "what got smaller?"), `feedback_unaddressed_load_bearing` (file tracking issue for every named-but-deferred item), `feedback_spec_pattern_authority` (one pattern mandated per surface — deviation requires re-spawn).

---

## §1 Goal + non-goal

### Goal (in scope this wedge)

1. **Per-tenant monthly USD rollup** — sum `events.kind='budget_reconciled'` rows scoped to `(tenant_id, period_start, period_end)` into a single canonical monthly total. Default period = calendar month UTC; period boundary config-driven for future tuning. Rollup is a pure read over substrate; no parallel table, no cache.
2. **Stripe metered-usage export** — push monthly tenant totals to Stripe via `subscription_item.create_usage_record` (proven OSS adapter `github.com/stripe/stripe-go/v76`). Idempotent: idempotency key = `sha256(tenant_id || period_start)` ensures retries collapse to one charge.
3. **Invoice generation (markdown v1)** — render per-tenant monthly invoice as a deterministic markdown document at `.regatta/invoices/<tenant_id>/<YYYY-MM>.md`. PDF generation is deferred to a follow-up wedge per §10 D1 — markdown-only is the falsifiable Wave-1 deliverable.
4. **Billing-period close ritual** — operator-triggered CLI: `regatta billing close --period 2026-06`. The command (a) sums `budget_reconciled` over the closed period per tenant, (b) emits one `events.kind='billing_period_closed'` row per tenant (lww), (c) renders the markdown invoice, (d) pushes the Stripe metered-usage record. Single transaction per tenant; partial failure leaves NO orphan state (Stripe push happens AFTER substrate commit; Stripe idempotency key + substrate UNIQUE-on-(tenant_id, period_start) make the close re-runnable byte-equal).
5. **Operator-facing billing dashboard tab** — new read-only `/billing` route on the W7 operator UI. Lists `tenants × periods` table; each row links to the rendered invoice file. Read-only (close is CLI-only — UI cannot trigger Stripe push).
6. **OTel + operator doc** — `billing.close` span, `billing.stripe.push` span, `regatta.billing.tenant_id` attribute on both. Operator doc `docs/operator/billing.md` covers the close ritual, Stripe webhook reconciliation runbook, and the invoice-file location convention.

### Non-goal (deferred — tracking issues filed at impl time per `feedback_unaddressed_load_bearing`)

- **Payment processing** — Stripe owns charge collection. W12 only pushes usage records; charge collection, dunning, refund processing all live in Stripe Dashboard. Out of scope.
- **Multi-currency v1** — USD only. The Stripe `Price` object must be USD-denominated; the operator runbook documents this constraint. Tracking issue: `[billing-followup] multi-currency support (EUR/GBP/JPY price IDs)` — lands after W12 stabilises and at least one customer requests it.
- **Proration v1** — full calendar months only. Tenants that onboard mid-month receive a $0 invoice for that month (no partial rollup). Tracking issue: `[billing-followup] mid-period proration support`.
- **Invoice contestation workflow** — disputes are file-based v1: tenant emails operator with the invoice file ref + a contested line item; operator amends the substrate event manually + re-runs close. UI for contestation is deferred. Tracking issue: `[billing-followup] in-UI invoice contestation`.
- **PDF invoice generation** — markdown only v1. Decision tree captured in §3.4. Tracking issue: `[billing-followup] PDF invoice rendering`.
- **Real-time billing webhooks** — Stripe webhook ingress (e.g. `invoice.payment_succeeded` → mark tenant paid) is deferred. W12 is push-only; pull side is a follow-up. Tracking issue: `[billing-followup] Stripe webhook ingress`.

---

## §2 In/Out — scope matrix

| Concern | In scope (this wedge) | Out of scope (deferred / other wedge) |
|---|---|---|
| Storage | Substrate events `billing_period_closed` (lww), reads of `budget_reconciled` (cost-gov-owned) | Any new SQL table; any DDL migration |
| Period semantics | Calendar month UTC, full months only | Mid-month proration; week/quarter periods |
| Currency | USD | EUR/GBP/JPY |
| Charge mechanism | Stripe metered-usage record push (`subscription_item.create_usage_record`) | Direct card charge; ACH; invoice mailing; dunning |
| Invoice format | Deterministic markdown to `.regatta/invoices/<tenant>/<period>.md` | PDF; HTML email; CSV-only export |
| Close mechanism | `regatta billing close --period YYYY-MM` CLI; idempotent | UI-triggered close; cron-automated close |
| Operator UI | `/billing` route — read-only table; per-tenant per-period invoice links | Tenant-self-serve UI; per-line-item editing |
| Reconciliation source | `budget_reconciled` events (cost-gov already reconciles against Anthropic Cost API) | Direct Anthropic Usage API re-fetch in W12 |
| Webhooks | None (push-only) | Stripe webhook ingress; ZenDesk webhook on dispute |
| Refunds | Manual via Stripe Dashboard | In-regatta refund workflow |
| Tax | Stripe Tax handles (operator-configured at Stripe layer) | Per-jurisdiction tax computation in regatta |

The wedge stops at the substrate `billing_period_closed` event + the Stripe API call + the markdown file + the read-only UI tab. Everything else routes to a `[billing-followup]` tracking issue.

---

## §3 Architecture

### §3.1 Billing periods — config-driven, default monthly calendar UTC

Billing period boundaries live in `regatta.yaml` under a new optional `billing` block. Default = calendar month UTC (period_start = first instant of month, period_end = first instant of next month exclusive). Operator override exists but is documented as "do not change after first close" — switching period shape mid-stream creates an LWW collision on the substrate uniqueness key (R3 in §5 covers this).

CUE schema change at `contracts/schemas/regatta.v1.cue` (BACKWARDS-COMPATIBLE):

```cue
#Regatta: {
    // … existing fields unchanged …
    billing?: #Billing
}

#Billing: {
    // Cadence of the closed billing period. Wave 1 = monthly only; weekly/quarterly
    // are deferred until at least one operator asks.
    period:                *"monthly" | "monthly"
    // Stripe configuration. When omitted, billing close still emits the substrate
    // event + renders the markdown invoice but the Stripe push is skipped with
    // a WARN log (read-only mode — operator can spot-check the rollup without
    // touching a paid Stripe account).
    stripe?: {
        api_key_env:            *"STRIPE_API_KEY" | string
        subscription_item_id:   string             // per-tenant lookup keyed off tenant_id; map in stripe.tenant_map
        tenant_map?: [string]: string              // tenant_id → stripe subscription_item_id
    }
    // Invoice output directory (relative to repo root). Default writes inside
    // .regatta/ which is .gitignored by convention; operator may redirect to
    // a mounted volume for retention.
    invoice_dir:                *".regatta/invoices" | string
}
```

A `billing: {}` empty block is rejected at config-load (no fields set ⇒ block is meaningless). A `billing: { stripe: {} }` empty Stripe sub-block IS allowed and means "skip Stripe push, render markdown only" — documented in operator doc as the dry-run mode.

### §3.2 Rollup job — pure read over `budget_reconciled` events

The rollup is a SINGLE SQL query, executed once per close, per tenant. No background goroutine, no cache, no parallel table. The query:

```sql
SELECT
    json_extract(payload_json, '$.tenant_id')      AS tenant_id,
    SUM(json_extract(payload_json, '$.actual_usd')) AS total_usd,
    COUNT(*)                                       AS bucket_count,
    MIN(json_extract(payload_json, '$.period_start')) AS first_bucket_start,
    MAX(json_extract(payload_json, '$.period_end'))   AS last_bucket_end
FROM events
WHERE kind = 'budget_reconciled'
  AND json_extract(payload_json, '$.period_start') >= :period_start_ms
  AND json_extract(payload_json, '$.period_end')   <= :period_end_ms
  AND json_extract(payload_json, '$.tenant_id')    = :tenant_id
GROUP BY json_extract(payload_json, '$.tenant_id');
```

`actual_usd` comes from the cost-governor's Anthropic Cost API reconciliation (per cost-gov §3.4) — it is the canonical Anthropic-billed amount, not a regatta-computed estimate. This eliminates the "pricing applied twice" trap that cost-gov §3.1 calls out for the Portkey adapter pattern: regatta only sums; the source of price truth is Anthropic itself.

**Line items** = the individual `budget_reconciled` rows that fed the sum. Each line item carries `{period_start, period_end, actual_usd, model_breakdown[]}` per cost-gov §3.5. The invoice markdown lists them; the Stripe push aggregates them.

**Result type** (`internal/billing/rollup/rollup.go`):

```go
type Rollup struct {
    TenantID    string
    PeriodStart time.Time   // first instant of the billing period (UTC)
    PeriodEnd   time.Time   // first instant of the next period (UTC), exclusive
    TotalUSD    float64     // SUM(budget_reconciled.actual_usd)
    LineItems   []LineItem  // one per source budget_reconciled row
}

type LineItem struct {
    PeriodStart    time.Time
    PeriodEnd      time.Time
    ActualUSD      float64
    ModelBreakdown []cost.ModelBreakdownRow  // reused from cost-gov §3.5
}

// Run is the rollup entry point. Pure function over substrate. Idempotent.
func Run(ctx context.Context, db *sql.DB, scope Scope) (Rollup, error)
```

### §3.3 Stripe adapter — proven OSS `stripe-go/v76`

**Adopted verbatim:** `github.com/stripe/stripe-go/v76` — Stripe's official Go SDK. Per `feedback_research_design_principles`: proven OSS beats build-from-scratch when the adapter exists. Stripe SDK is the canonical reference for metered-usage push; no bespoke HTTP client.

**Endpoint used:** `subscription_item.create_usage_record` — a single API call per tenant per period. The SDK encapsulates HMAC signing, retry, error mapping, OTel-friendly request IDs.

```go
// internal/billing/stripe/adapter.go
package stripe

import (
    stripego "github.com/stripe/stripe-go/v76"
    "github.com/stripe/stripe-go/v76/client"
)

type Adapter struct {
    api    *client.API
    tenant map[string]string  // tenant_id → subscription_item_id
    clock  func() time.Time
}

// PushUsage records one metered-usage entry on the tenant's Stripe subscription_item.
// Idempotent: idempotency_key = sha256(tenant_id || period_start_unix). A retry
// with identical (tenant, period) collapses to a single charge — Stripe enforces
// this server-side (24h idempotency window per Stripe docs).
func (a *Adapter) PushUsage(ctx context.Context, r rollup.Rollup) (StripeUsageRecordID, error) {
    subItem, ok := a.tenant[r.TenantID]
    if !ok {
        return "", fmt.Errorf("billing/stripe: no subscription_item_id mapped for tenant %q", r.TenantID)
    }
    idem := fmt.Sprintf("regatta-billing-%x", sha256.Sum256([]byte(r.TenantID + "|" + strconv.FormatInt(r.PeriodStart.Unix(), 10))))
    params := &stripego.UsageRecordParams{
        SubscriptionItem: stripego.String(subItem),
        Quantity:         stripego.Int64(int64(math.Round(r.TotalUSD * 100))),  // cents
        Timestamp:        stripego.Int64(r.PeriodEnd.Unix() - 1),                // last second of period
        Action:           stripego.String(stripego.UsageRecordActionSet),       // SET, not INCREMENT — idempotent semantics
    }
    params.SetIdempotencyKey(idem)
    rec, err := a.api.UsageRecords.New(params)
    // … handle stripe.Error: 4xx hard-fail; 5xx + network = exponential backoff caller-side …
}
```

**Key adapter decisions:**
- **`UsageRecordActionSet`, not `Increment`.** SET is idempotent against the (subscription_item, timestamp) tuple; INCREMENT would double-bill on retry. The cost is that two close runs in the same period must agree on the final number — which they do, because both read the same substrate state.
- **Quantity in cents (int64), not USD float.** Stripe's metered-usage quantity is integer; representing $12.34 as 1234 cents avoids float-rounding drift across retries.
- **Idempotency key derivation is deterministic, no clock.** `sha256(tenant_id || period_start_unix)` — same input → same key, regardless of when the operator re-runs.

**OSS scan rejected:** OpenMeter (CNCF metered-billing OSS, mentioned in the brief §"W12 — Metered billing") — Wave-1 rejected because it adds a separate event-bus dep + a parallel ingest pipeline + its own deployment surface, all to solve a problem (per-tenant USD rollup over already-reconciled events) that is a SINGLE SQL query in regatta's substrate. The brief calls out OpenMeter as "adapter pattern" — that adapter slot remains open for a `[billing-followup]` issue when/if a customer demands hourly/daily granular usage events. v1 stays at month-bucket aggregation pushed directly to Stripe.

### §3.4 Invoice generation — markdown template (PDF deferred)

**Decision: markdown-only v1.** PDF generation requires either headless Chrome (heavyweight runtime dep, security surface) or a pure-Go renderer like `github.com/jung-kurt/gofpdf` (active OSS, but a third dependency on top of Stripe + substrate that is not load-bearing for Wave-1). Markdown is the falsifiable Wave-1 deliverable: an operator can `cat` the file, a customer can render via any markdown viewer, and the file is `diff`-able for audit.

**PDF research artifact** (saved as a `[billing-followup]` issue at PR time):
- `gofpdf` — pure Go, MIT, active commits in 2025-2026, no CGO. Pros: hermetic, ships in the regatta binary. Cons: layout control is verbose; tables require manual cell positioning.
- Headless Chrome (e.g. via `github.com/chromedp/chromedp`) — high-fidelity HTML→PDF. Pros: reuses the markdown→HTML pipeline. Cons: ships a Chrome binary or requires it on the operator host; ~200MB runtime surface.
- Decision deferred: customer feedback determines whether PDF is required. Until then, markdown via `github.com/yuin/goldmark` (already in regatta's vendored set per W7 §3.5 for the operator UI) suffices for HTML preview on the `/billing` tab.

**Markdown template** (`internal/billing/invoice/template.md.tmpl`):

```
# Regatta Invoice

**Tenant:** {{ .TenantID }}
**Period:** {{ .PeriodStart.Format "2006-01-02" }} → {{ .PeriodEnd.Format "2006-01-02" }} (exclusive)
**Generated:** {{ .GeneratedAt.Format time.RFC3339 }}
**Total (USD):** ${{ printf "%.2f" .TotalUSD }}
**Stripe usage_record:** {{ .StripeUsageRecordID }}

---

## Line items

| Bucket start (UTC) | Bucket end (UTC) | Amount (USD) | Model breakdown |
|---|---|---|---|
{{ range .LineItems -}}
| {{ .PeriodStart.Format "2006-01-02 15:04" }} | {{ .PeriodEnd.Format "2006-01-02 15:04" }} | ${{ printf "%.2f" .ActualUSD }} | {{ joinModelBreakdown .ModelBreakdown }} |
{{ end }}

---

Reconciliation source: Anthropic Cost API (per cost-governor reconciler §3.4).
Idempotency key: `{{ .IdempotencyKey }}` (sha256(tenant_id || period_start_unix)).
```

**Deterministic output** — same `(tenant_id, period_start)` always renders the same file byte-equal except for `GeneratedAt`. The `GeneratedAt` field is deliberately the only non-deterministic line; the operator runbook documents diffing invoices for byte-equality EXCEPT that line.

**Output path:** `<invoice_dir>/<tenant_id>/<YYYY-MM>.md`. Directory created if missing. Existing file is OVERWRITTEN on re-close (idempotent re-run is the supported retry path; the substrate `billing_period_closed` event remains the canonical record).

### §3.5 Billing-period-close ritual — operator-triggered CLI, idempotent

**Command:** `regatta billing close --period 2026-06`. Optional `--tenant <id>` to close one tenant only (default: all tenants present in `budget_reconciled` events for the period).

**Execution shape (per tenant, in a single transaction):**

```
T+0   parse --period flag; resolve period_start / period_end (UTC month bounds)
T+1   query substrate: list tenants with budget_reconciled rows in window
T+2   for each tenant:
T+3     BEGIN TX
T+4       rollup.Run(ctx, tx, scope) → Rollup{tenant, total_usd, line_items}
T+5       substrate.AppendEvent(tx, kind='billing_period_closed', payload={
              tenant_id, period_start, period_end, total_usd, line_items,
              stripe_usage_record_id: ""   // filled in T+8 on success; blank on Stripe-skip
          })
T+6     COMMIT
T+7     invoice.Render(rollup, invoiceDir) → write .regatta/invoices/<tenant>/<period>.md
T+8     if stripe enabled:
T+9       stripeAdapter.PushUsage(ctx, rollup) → usage_record_id
T+10      substrate.AppendEvent(NEW TX, kind='billing_period_closed', payload={
              ... same fields ..., stripe_usage_record_id: usage_record_id
          })
            // LWW collapses on (tenant_id, period_start) — the row with the
            // populated stripe_usage_record_id wins over the empty one.
T+11  emit OTel span `billing.close` with attrs {tenant_count, total_usd_aggregate}
```

**Idempotency proof** (addresses R2 in §5):
- **Substrate side:** `billing_period_closed` reducer = `lww` on `(tenant_id, period_start)`. Two close runs produce two appended rows; the fold returns the most recent. The fold result is what the UI reads + what the next reconciliation references. Replay-safe per substrate v2 §4.
- **Stripe side:** idempotency key = `sha256(tenant_id || period_start_unix)`. Stripe's 24h idempotency window matches or exceeds any sane retry cadence. Two pushes within 24h with the same key collapse to one charge; Stripe returns the original `usage_record` ID.
- **Filesystem side:** invoice file is OVERWRITTEN deterministically. Operator-visible diff = just the `GeneratedAt` timestamp.
- **Cross-system side:** Stripe push happens AFTER substrate commit. If the Stripe push fails (5xx, network down), the substrate `billing_period_closed` event is already durable with empty `stripe_usage_record_id`; operator re-runs `regatta billing close` and the Stripe push retries with the same idempotency key. If the substrate commit fails (rare — same DB the rest of regatta uses), Stripe was never called; operator re-runs.

**Failure modes:**

| Scenario | Behaviour |
|---|---|
| `--period` flag omitted | CLI exits with usage banner; non-zero exit. |
| Period not yet ended (operator runs `--period 2026-06` on 2026-06-15) | CLI exits with `ErrPeriodNotEnded`. Operator must wait until first instant of 2026-07 UTC. Override flag `--allow-open-period` for test/dry-run use only; emits a WARN slog. |
| No `budget_reconciled` events for a tenant in the period | Tenant skipped; logged at INFO. No empty invoice, no $0 Stripe push. |
| Stripe API key env unset + `billing.stripe` configured | Fail-fast at CLI start with clear error. Operator either unsets the Stripe config or sets the key. |
| Stripe API returns 4xx (e.g. invalid `subscription_item_id`) | Hard fail for that tenant; substrate event ALREADY committed; operator fixes the tenant map and re-runs (Stripe idempotency collapses the retry). |
| Stripe API returns 5xx | Exponential backoff (1s × 2^n, capped 5min, 5 attempts); persistent failure leaves the substrate event with empty `stripe_usage_record_id`; operator re-runs after upstream recovers. |
| Two operators run `billing close` simultaneously | LWW substrate semantics + Stripe idempotency collapse both runs to one charge. No double-bill. |

### §3.6 Operator UI tab — read-only `/billing` route on W7

**New route on the W7 listener** (per W7 §3.3):

| Method | Path | Purpose | Auth | Cache |
|---|---|---|---|---|
| `GET` | `/billing` | List all tenants × periods with closed invoices. HTML table. | Cookie HMAC (per W7 §3.2) | `Cache-Control: no-store` |
| `GET` | `/billing/{tenant_id}/{period}` | Render one invoice (markdown → HTML via `goldmark`). | Cookie HMAC | `Cache-Control: no-store` |

**Data source:** SUBSTRATE ONLY. The UI reads `events.kind='billing_period_closed'` via Fold. Markdown files on disk are the canonical render artifact but the UI does NOT read them directly — the UI re-renders from substrate state. This means the markdown file can be regenerated from substrate at any time; the file is a convenience for `cat`/email, not a source of truth.

**Template** (new file `internal/web/templates/billing.tmpl`, reuses the W7 layout):

```html
{{ define "billing" }}
<table class="billing-grid">
  <thead><tr><th>Tenant</th><th>Period</th><th>Total (USD)</th><th>Closed at</th><th>Stripe ref</th><th>Invoice</th></tr></thead>
  <tbody>
  {{ range .Rows }}
    <tr>
      <td>{{ .TenantID }}</td>
      <td>{{ .PeriodStart.Format "2006-01" }}</td>
      <td>${{ printf "%.2f" .TotalUSD }}</td>
      <td>{{ .ClosedAt.Format time.RFC3339 }}</td>
      <td>{{ if .StripeUsageRecordID }}<code>{{ .StripeUsageRecordID }}</code>{{ else }}<em>not pushed</em>{{ end }}</td>
      <td><a href="/billing/{{ .TenantID }}/{{ .PeriodStart.Format "2006-01" }}">view</a></td>
    </tr>
  {{ end }}
  </tbody>
</table>
{{ end }}
```

**No close button in the UI.** Close is CLI-only per §3.5 — the UI is a passive viewer. This is an intentional discipline: a UI close button invites the "click twice → double-bill" trap; CLI-only forces operator deliberation and lives inside the same auth/audit envelope as every other operator command.

### §3.7 Substrate hook — one new event kind, zero migrations

**New event kind:** `billing_period_closed`. Registered alongside `token_spend` + `budget_reconciled` in the substrate kind enum (string column per substrate §2 — no DDL).

**Reducer:** `lww` on `(tenant_id, period_start)`. Most recent commit wins. Matches the cost-governor's `budget_reconciled` reducer choice for the same reasoning: close is operator-triggered, retries are expected, last write reflects the true terminal state (especially for the empty-then-populated `stripe_usage_record_id` flow in §3.5).

**Payload struct** (`internal/billing/event/payload.go`):

```go
// BillingPeriodClosedPayload — substrate events.kind='billing_period_closed'
// Reducer: lww on (tenant_id, period_start). One row per tenant per closed period.
type BillingPeriodClosedPayload struct {
    TenantID            string     `json:"tenant_id"`            // primary scope key
    PeriodStart         int64      `json:"period_start"`         // unix ms; UTC month start
    PeriodEnd           int64      `json:"period_end"`           // unix ms; UTC next-month start, exclusive
    TotalUSD            float64    `json:"total_usd"`            // SUM(budget_reconciled.actual_usd) over period
    LineItems           []LineItem `json:"line_items"`            // one per source budget_reconciled
    StripeUsageRecordID string     `json:"stripe_usage_record_id"` // populated on Stripe push success; "" otherwise
    IdempotencyKey      string     `json:"idempotency_key"`       // sha256(tenant_id || period_start_unix)
    InvoiceFilePath     string     `json:"invoice_file_path"`     // relative path to rendered markdown
    ClosedAt            int64      `json:"closed_at"`             // unix ms; T+11 wall-clock at close
}

type LineItem struct {
    PeriodStart    int64                       `json:"period_start"`
    PeriodEnd      int64                       `json:"period_end"`
    ActualUSD      float64                     `json:"actual_usd"`
    ModelBreakdown []cost.ModelBreakdownRow    `json:"model_breakdown"`  // imported from internal/cost
}
```

**Dispatch-table validation** in `internal/orchestrator/state/substrate/validate.go` (per substrate v2 §2.1 "Per-kind payload validation"): assert non-empty `tenant_id`, `period_end > period_start`, `total_usd >= 0`, `idempotency_key` length == 64 (sha256 hex).

### §3.8 OTel hook — two new spans, one new attribute namespace

**New spans** (under the existing `regatta.billing.*` namespace; no collision with W6 `gen_ai.*` or cost-governor `regatta.cost.*`):

| Span name | Parent | Attributes | Lifetime |
|---|---|---|---|
| `billing.close` | CLI root span | `regatta.billing.period_start`, `regatta.billing.period_end`, `regatta.billing.tenant_count`, `regatta.billing.total_usd_aggregate`, `regatta.billing.stripe_enabled` (bool) | One per `billing close` invocation |
| `billing.tenant.close` | `billing.close` | `regatta.billing.tenant_id`, `regatta.billing.total_usd`, `regatta.billing.line_item_count`, `regatta.billing.stripe_usage_record_id` (when set) | One per tenant per close |
| `billing.stripe.push` | `billing.tenant.close` | `regatta.billing.tenant_id`, `regatta.billing.idempotency_key`, `stripe.usage_record_id` (Stripe SDK attribute) | One per Stripe API call |

Cardinality bound: `lane_cap × num_tenants` per close invocation — operator-known.

### §3.9 What got smaller (deletion-default audit per `feedback_deletion_default`)

The spec deliberately cuts surface area where the temptation to over-build is high:

1. **No new SQL table.** Substrate is the single store. Compared to a hypothetical `billing_invoices` table, we save: a migration, a foreign-key relationship to `events`, a separate fold path, a backfill recipe. The trade: every read filters on `kind='billing_period_closed'` + JSON extraction — already the norm for cost-gov reads.
2. **No PDF in v1.** Cut the `gofpdf` dep + the headless-Chrome runtime decision. Markdown is the v1 falsifiable artifact.
3. **No OpenMeter adapter slot.** The brief mentions it as a candidate; we explicitly DEFER until a customer demands per-event granularity. v1 = month-bucket only.
4. **No UI close button.** One less route, one less form, one less double-submit guard. CLI-only.
5. **No webhook ingress in v1.** Push-only. The Stripe→regatta direction (e.g. dispute, refund, dunning) is entirely deferred.
6. **No estimator in W12.** The rollup reads the cost-governor's already-reconciled actual_usd — no parallel estimation logic.
7. **No `usage_record` create_action=increment.** Set-mode is idempotent; increment-mode would force a separate dedup table.
8. **No new auth/RBAC primitive.** Reuses W7 cookie HMAC.

Net new code estimate: ~700 LoC Go (rollup ~120, stripe adapter ~150, invoice render ~80, CLI ~120, UI route+template ~150, payload+validation ~80) plus ~100 lines test scaffolding. Compares favourably to cost-gov's ~1100 LoC budget for a wedge of similar customer-facing impact.

---

## §4 Existing patterns reused

| Pattern | Source spec | How W12 reuses it |
|---|---|---|
| `substrate.AppendEvent(tx, kind, payload, ...)` writer | Unified Substrate v2 §3 | `billing_period_closed` writer at close T+5 and T+10. One-line call. |
| Substrate fold + LWW reducer | Substrate v2 §4 | `billing_period_closed` reducer = lww on `(tenant_id, period_start)`. Identical shape to `budget_reconciled` (cost-gov §3.5). |
| Dispatch-table payload validation | Substrate v2 §2.1 | `BillingPeriodClosedPayload` validator registered in `internal/orchestrator/state/substrate/validate.go`. Same shape as `TokenSpendPayloadValidator`. |
| `budget_reconciled` event emission | Cost-governor §3.4 | W12 is a pure CONSUMER — reads `actual_usd` from cost-gov's already-emitted rows. No re-fetch from Anthropic. |
| W7 cookie HMAC auth middleware | W7 §3.2 | New `/billing` + `/billing/{tenant}/{period}` routes attach the existing middleware. Zero new auth code. |
| W7 embed.FS template loader | W7 §3.4 | New `billing.tmpl` added to `internal/web/templates/`; loaded by the existing `template.ParseFS` call at boot. |
| W7 htmx + Tailwind asset pipeline | W7 §3.5 | `/billing` table uses the same `layout.tmpl` shell; no new JS, no new CSS. |
| Slog → OTel bridge (W6 T2) | W6 OTel backbone §3.2 | `billing.close` span events auto-bridge to slog via the existing emitter. No new wiring. |
| Hardcoded model-pricing table | Cost-governor §3.8 | Not used directly — W12 sums Anthropic-priced amounts, never re-applies prices. Pricing-drift trap (cost-gov R-A4) cannot happen here. |
| Anthropic Cost API as canonical source | Cost-governor §3.4 | W12 inherits cost-gov's reconciler choice. If cost-gov reconciles against the Usage API fallback (Cost API down), the W12 rollup transparently reads the fallback-priced rows. |
| Stripe SDK `subscription_item.create_usage_record` | New (this wedge) | First adoption of `stripe-go/v76` in the codebase. Vendored via `go.mod`; no transitive bloat (Stripe SDK is std-lib only + their internal client). |

---

## §5 Risk register (R1-R10)

Each risk: stated threat, falsifiable test, mitigation. Per `feedback_adversarial_review`: every R-tier risk either has a fix in §3 or a tracking issue cited at PR time.

| ID | Risk | Threat scenario | Mitigation (in §3 or deferred) | Test (B/A/A+) |
|---|---|---|---|---|
| **R1** | **Stripe webhook replay** | Future Stripe-ingress wedge gets re-delivered an old `usage_record.created` event; if regatta state-machines on it, it could double-record. | Deferred entirely from v1 (no webhook ingress per §1 non-goal). Tracking issue filed; future wedge MUST implement Stripe-event-id deduplication via substrate (`events.kind='stripe_webhook_received'`, lww on `event.id`). | A-tier: `TestStripeReplay_DocumentedInFollowupIssue` — asserts the followup issue exists with replay-dedup-as-acceptance criterion. |
| **R2** | **Double-charge race on close retry** | Two operators run `billing close --period 2026-06` simultaneously, or one operator runs it twice within 24h. Naive impl pushes two Stripe records → two invoice lines → customer over-billed. | (1) Stripe idempotency key = `sha256(tenant_id || period_start_unix)` (§3.3). (2) Stripe 24h idempotency window collapses. (3) `UsageRecordActionSet` (not Increment) — SET is idempotent against (sub_item, timestamp) by design. (4) Substrate LWW makes the substrate side commutative. | A+: `TestClose_DoubleRun_OnlyOneStripeCharge` — uses stripe-mock to assert exactly one `POST /v1/subscription_items/{ID}/usage_records` lands across N concurrent invocations. |
| **R3** | **Period-close idempotency break on schema drift** | Operator changes `billing.period` from `monthly` to (hypothetical) `weekly` mid-stream. LWW key `(tenant_id, period_start)` is preserved but `period_end` shifts — same `period_start` could now span a different window. | (1) Wave-1 schema locks `period: *"monthly" | "monthly"` — CUE rejects anything else. (2) Operator doc names this as "do not change after first close." (3) Tracking issue for weekly/quarterly support cites this as the migration concern. | B: `TestSchemaLock_RejectsNonMonthly_v1` — config-load test asserts non-monthly period values fail validation. |
| **R4** | **Tenant-leak invoice cross-view** | Tenant A's invoice URL `/billing/tenant-a/2026-06` is guessable by tenant B; default cookie HMAC is operator-level not tenant-level until W8 RBAC. | (1) Pre-W8: the `/billing` UI is OPERATOR-ONLY per W7 §3.2 cookie HMAC. There is no tenant-self-serve route in W12. (2) Doc explicitly: "no public tenant access until W8 RBAC lands `tenant_id` in the cookie claim." (3) After W8: handler asserts `cookie.TenantID == path.TenantID` for the `/billing/{tenant_id}/...` route. | A: `TestBillingRoute_PreW8_OperatorOnly` — request without operator cookie returns 401; A+ post-W8: `TestBillingRoute_PostW8_TenantScoped`. |
| **R5** | **Refund handling drift** | Operator issues a refund in Stripe Dashboard; regatta substrate still shows the original `total_usd`. Next month's rollup is unaffected (good), but the in-UI total for that period is now wrong. | (1) Stripe Dashboard is the source of truth for refunds — operator doc names this. (2) `[billing-followup] Stripe webhook ingress` tracking issue covers in-regatta refund reflection. (3) Substrate event is immutable history; refund is a Stripe-side correction, not a substrate rewrite. | A: `TestRefund_DocumentedInOperatorDoc` — operator doc lint asserts refund section exists. |
| **R6** | **Pricing-table drift between cost-gov and Stripe Price** | Operator configures Stripe Price as $0.001/usage_unit; cost-gov pricing table evolves; regatta-billed USD diverges from Stripe-derived USD. | (1) W12 pushes raw cents (TotalUSD × 100) — there is NO unit-price step in Stripe. The Stripe Price object should be configured as `unit_amount: 1, billing_scheme: per_unit` so 1 cent in regatta = 1 cent in Stripe. (2) Operator doc covers the Stripe Price configuration explicitly. (3) Reconciliation tracking issue covers cross-checking Stripe invoice total against substrate total. | A: `TestStripe_Unit_AmountIsCent` — Stripe-mock assertion that submitted quantity is `int64(TotalUSD * 100)`. |
| **R7** | **Reconciliation lag** | Cost-governor reconciler runs hourly; close runs at month boundary. Last hour of the month may not yet have a `budget_reconciled` row. | (1) `--period 2026-06` closes the period [2026-06-01T00:00Z, 2026-07-01T00:00Z). Operator runs the close on or after 2026-07-01T01:00Z (one cost-gov reconciler tick later). (2) Operator doc names "wait at least 1 reconciler interval after period end before closing." (3) Re-running the close after a missed bucket reconciles is supported via LWW. | A: `TestClose_LateBucket_LWWCorrects` — simulates a `budget_reconciled` row landing after a first close; re-running close overwrites the rollup. |
| **R8** | **Substrate write amplification on close** | Closing 1000 tenants × 12 monthly periods = 12k events per backfill run. | (1) Closes are operator-triggered, not automatic — natural rate-limiting. (2) Backfill recipe lives in the operator runbook (`[billing-followup] backfill recipe`). (3) Substrate `events` insertion is the existing critical path; no novel write pattern. | A: `TestClose_BulkRun_NoTimeout` — 100-tenant synthetic close completes within `make check`'s 60s budget. |
| **R9** | **`subscription_item.create_usage_record` retired** | Stripe deprecates the metered-usage API (it has been re-architected before via Pricing v2). | (1) Adapter is one file (`internal/billing/stripe/adapter.go`). API surface change is a one-file edit. (2) `stripe-go/v76` version pin in `go.mod` insulates against silent breakage. (3) Tracking issue: `[billing-followup] adapter version refresh runbook` — quarterly Stripe SDK bump cadence. | B: `TestStripeSDK_PinnedVersion` — `go.mod` lint asserts pinned major version, not a `latest` tag. |
| **R10** | **Empty `tenant_map` silently no-ops Stripe push** | Operator forgets to add tenant-id → subscription_item_id mapping; close runs, substrate event lands, but Stripe push is skipped. Invoice generated but never charged. | (1) Fail-fast: `PushUsage` returns explicit error when tenant is missing from map (§3.3). (2) Close ritual surfaces the error at the `billing.tenant.close` span + non-zero CLI exit. (3) Operator doc covers the tenant-map config + bootstrap recipe. | A: `TestPushUsage_UnmappedTenant_HardErrors` — Stripe adapter returns error, never makes the API call. |

---

## §6 Test plan per task (B/A/A+ tier)

Per `feedback_grade_rubric`: every task ships a B-tier (basic correctness), A-tier (edge cases + adversarial), A+ (independent reviewer-found gap closure) test set. Implementer dispatches MUST cite their tier-mapping in the PR body scorecard.

### T1 — Billing-period rollup job (`internal/billing/rollup/`)
- **B-tier:** `TestRollup_SumsBudgetReconciledByTenant`, `TestRollup_EmptyPeriodReturnsZero`, `TestRollup_FiltersByTenantID`.
- **A-tier:** `TestRollup_LateReconciledRow_IncludedOnRerun`, `TestRollup_MultiBucketLineItems_OrderedByPeriodStart`.
- **A+:** `TestRollup_ConcurrentReadsAreStable` (snapshot-isolation assertion against substrate Fold).

### T2 — Stripe adapter + idempotency (`internal/billing/stripe/`)
- **B-tier:** `TestStripeAdapter_PushUsage_HappyPath` (stripe-mock backend), `TestStripeAdapter_UnmappedTenant_HardErrors`, `TestStripeAdapter_QuantityInCents`.
- **A-tier:** `TestStripeAdapter_IdempotencyKeyDeterministic`, `TestStripeAdapter_5xxBackoff`.
- **A+:** `TestStripeAdapter_ConcurrentPushes_OneCharge` (N goroutines, assert exactly one POST observed).

### T3 — Invoice markdown template (`internal/billing/invoice/`)
- **B-tier:** `TestInvoice_RendersAllLineItems`, `TestInvoice_TotalMatchesSum`, `TestInvoice_OutputPathDeterministic`.
- **A-tier:** `TestInvoice_ByteEqualExceptGeneratedAt` (two renders with same input, diff is one line).
- **A+:** `TestInvoice_RendersWhenStripeSkipped` (StripeUsageRecordID empty case).

### T4 — `billing close` CLI (`cmd/regatta/billing.go`)
- **B-tier:** `TestCLI_ClosePeriodFlag_Required`, `TestCLI_PeriodNotEnded_RejectsWithoutOverride`, `TestCLI_NoBudgetReconciled_SkipsTenant`.
- **A-tier:** `TestCLI_DoubleRun_NoDoubleCharge` (integration with stripe-mock), `TestCLI_StripeFailure_SubstrateEventCommitted`.
- **A+:** `TestCLI_MultiTenant_PerTenantSpan` (OTel hierarchy assertion).

### T5 — Operator UI billing tab (`internal/web/billing.go`)
- **B-tier:** `TestBillingHandler_OperatorAuth_Required`, `TestBillingHandler_ListsAllClosedPeriods`, `TestBillingHandler_RendersOneInvoice`.
- **A-tier:** `TestBillingHandler_NoStoreCache`, `TestBillingHandler_OrdersByPeriodDescending`.
- **A+:** `TestBillingHandler_PreW8_OperatorOnly_PostW8_TenantScoped` (config-flag-driven test).

### T6 — OTel + operator doc (`docs/operator/billing.md` + tracer wiring)
- **B-tier:** `TestOTel_BillingCloseSpan_Emitted`, `TestOTel_StripePushSpan_Attached`.
- **A-tier:** `TestOperatorDoc_LintsAgainstBannedPhrases`, `TestOperatorDoc_CoversRefundRunbook`.
- **A+:** `TestOperatorDoc_LinksAllResolveOnDisk` (already covered by `scripts/doc-check.sh`).

**Cross-task A+ requirement:** every implementer PR spawns the adversarial reviewer subagent per `feedback_agent_pr_review`. Reviewer must find ≥ 1 gap (or sign off explicitly that none remain) AND propose ≥ 1 deletion per `feedback_deletion_default`.

---

## §7 Grade rubric (B / A / A+)

Per `feedback_grade_rubric`: each grade has tool-checkable criteria. The PR body scorecard MUST cite the verification command for each line.

### B — basic correctness
- **B1.** `bash scripts/doc-check.sh` exits 0 against the spec + any new docs. **Verify:** rerun the command.
- **B2.** `bash scripts/stale-todo.sh` exits 0. **Verify:** rerun the command.
- **B3.** Every section §1-§10 present in the spec. **Verify:** `grep -E '^## §' docs/engineer/specs/2026-06-01-w12-billing-design.md | wc -l` ≥ 10.
- **B4.** Risk register has R1-R10 (10 risks). **Verify:** `grep -cE '^\| \*\*R[0-9]+\*\*' docs/engineer/specs/2026-06-01-w12-billing-design.md` == 10.
- **B5.** Every deferred item names a tracking issue with prefix `[billing-followup]`. **Verify:** `grep -c '\[billing-followup\]' docs/engineer/specs/...md` ≥ 6.

### A — edge cases + deletion + reviewer-cleared
- **A1.** B + adversarial-reviewer subagent runs against the spec and finds zero unaddressed issues. **Verify:** PR body cites the reviewer subagent transcript hash.
- **A2.** §3.9 "What got smaller" enumerates ≥ 6 deletions vs the naive build. **Verify:** count the bullets — must be ≥ 6.
- **A3.** §5 every R-tier risk maps to either an in-spec mitigation OR a `[billing-followup]` tracking issue. **Verify:** no risk row reads "TBD" or "??".
- **A4.** §6 every task T1-T6 lists ≥ 3 B-tier tests + ≥ 2 A-tier + ≥ 1 A+. **Verify:** count assertions per row.
- **A5.** Stripe SDK adoption is pinned to v76 in any code preview (none in this spec; impl tasks must hold). **Verify:** spec text says `stripe-go/v76` verbatim.
- **A6.** ZERO new migration files. **Verify:** `ls migrations/ | wc -l` unchanged between main and PR.

### A+ — independent reviewer finds + closes
- **A+1.** A + the reviewer subagent FINDS a gap the author missed AND the spec edits to address it pre-merge. **Verify:** PR body cites the gap + the fix commit SHA.
- **A+2.** Spec body proposes ≥ 1 deletion-of-existing-surface that the wedge enables (per `feedback_deletion_default`). **Verify:** the deletion candidate is named in §3.9 or §10.
- **A+3.** `[billing-followup]` issues filed BEFORE merge with each followup's acceptance criteria + load-bearing-ness annotated. **Verify:** `gh issue list --label billing-followup --state open` ≥ 6 at PR-merge time.

---

## §8 File-disjoint impl decomposition (preview only — full task plan ships separately)

| Task | Owner | Path (exclusive) | Depends-on | Effort |
|---|---|---|---|---|
| T1 — Rollup job | impl-subagent | `internal/billing/rollup/` (rollup.go, rollup_test.go) | cost-gov W2 merged | ~1d |
| T2 — Stripe adapter + idempotency | impl-subagent | `internal/billing/stripe/` (adapter.go, adapter_test.go) | stripe-go/v76 vendored | ~1d |
| T3 — Invoice markdown template | impl-subagent | `internal/billing/invoice/` (render.go, template.md.tmpl, render_test.go) | T1 type definition merged | ~0.5d |
| T4 — `billing close` CLI | impl-subagent | `cmd/regatta/billing.go`, `cmd/regatta/billing_test.go` | T1+T2+T3 merged | ~1d |
| T5 — Operator UI billing tab | impl-subagent | `internal/web/billing.go`, `internal/web/templates/billing.tmpl`, `internal/web/billing_test.go` | W7 W1 + T4 merged | ~1d |
| T6 — OTel + docs | impl-subagent | `docs/operator/billing.md`, span wiring in T1+T4 (touched files claimed by T1/T4) | T1-T5 merged | ~0.5d |

**File-disjointness check:** every task owns disjoint file paths. T6's span wiring lives inside files already owned by T1+T4 — T6 is a doc-only PR that depends on T1+T4 spans being implementer-emitted at their PR. Operator doc is the only T6-owned file.

**Shared primitives owner:** T1 owns `BillingPeriodClosedPayload` + `LineItem` types in `internal/billing/event/payload.go`. T2, T3, T4 import. T1 ships first; downstream tasks unblock on T1 merge.

---

## §9 Sequencing

**W12 lands AFTER:**
- Cost-governor W2 — specifically T3 (reconciler tick emitting `budget_reconciled`) + T4 (Anthropic Cost API adapter). Without these, W12's rollup has no source data.
- W7 Operator Web UI Wave 1 — specifically T4-T7 (HTTP listener, embed.FS templates, cookie HMAC middleware, route registration). Without these, W12's UI tab has no host.

**W12 is independent of:**
- W8 RBAC (soft prereq only — W12 ships with `substrate.DefaultTenantID` and forward-fits when W8 lands).
- W9 replay (W12 emits no replay-critical events beyond substrate-standard).
- W10 attestations.
- W11 cost-governor enhancements (W12 consumes the W11-defined `actual_usd`; further W11 tuning does not block W12).

**Within W12:** Wave 1 = T1 + T2 + T3 in parallel (no shared mutable surface). Wave 2 = T4 (depends on T1-T3). Wave 3 = T5 + T6 in parallel (T5 depends on T4 for the substrate state shape; T6 depends on T1-T5 for span emission).

Estimated calendar duration: ~4 days end-to-end with 3 implementer subagents in parallel for Wave 1 + sequential Wave 2 + parallel Wave 3.

---

## §10 Deferred (named-but-deferred items per `feedback_unaddressed_load_bearing`)

Each deferred item ships a `[billing-followup]` tracking issue filed PRE-MERGE. PR body cites issue numbers.

- **D1 — Multi-currency support.** USD-only v1 per §1 non-goal. Tracking issue: `[billing-followup] multi-currency support (EUR/GBP/JPY price IDs)`. Load-bearing: blocks non-US customer onboarding.
- **D2 — Mid-period proration.** Full-month-only v1. Tracking issue: `[billing-followup] mid-period proration support`. Load-bearing: any pilot conversion mid-month sees a $0 invoice for the partial month, which is an explicit operator workaround in v1.
- **D3 — Invoice contestation UI.** File-based v1. Tracking issue: `[billing-followup] in-UI invoice contestation`. Load-bearing for tenant self-serve story.
- **D4 — PDF invoice rendering.** Markdown-only v1. Tracking issue: `[billing-followup] PDF invoice rendering`. Decision tree (gofpdf vs headless chrome vs markdown→HTML→print) in the issue body.
- **D5 — Real-time Stripe webhook ingress.** Push-only v1. Tracking issue: `[billing-followup] Stripe webhook ingress`. Load-bearing for dispute/refund reflection per R5.
- **D6 — OpenMeter adapter slot.** Direct-Stripe-only v1. Tracking issue: `[billing-followup] OpenMeter adapter for granular usage events`. Load-bearing only if a customer demands hourly/daily granular usage events.
- **D7 — Tenant self-serve UI.** Operator-only v1. Tracking issue: `[billing-followup] tenant self-serve billing page`. Load-bearing: depends on W8 RBAC `tenant_id` cookie claim.
- **D8 — Backfill recipe for historical periods.** Forward-only v1. Tracking issue: `[billing-followup] historical period backfill recipe`.
- **D9 — Quarterly Stripe SDK refresh runbook.** Pinned-version v1. Tracking issue: `[billing-followup] Stripe SDK refresh runbook`.
- **D10 — Tax computation.** Stripe-handled v1. Tracking issue: `[billing-followup] per-jurisdiction tax integration` — load-bearing only if customer is in a non-Stripe-Tax-covered jurisdiction.

**Deletion candidates this wedge enables** (per `feedback_deletion_default`):
- The cost-gov spec's §10 reference to "Streaming Stripe metered-billing webhook" can be removed from the cost-gov spec's deferred list once W12 lands — that bullet is now W12-owned.
- The brief §1 line "No billing/usage export" can be edited at W12 merge time to reflect the new state.

---
