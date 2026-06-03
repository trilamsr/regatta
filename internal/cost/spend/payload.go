package spend

// TokenSpendPayload is the substrate `kind='token_spend'` payload shape
// per cost-governor design spec §3.5 lines 264-276. One row per LLM
// call; cumulative spend = SUM(usd_micro) over filtered window.
//
// Money fields: USDMicro is the canonical integer-micro-USD source of
// truth (issue #554 — eliminates SQLite SUM(float) ULP drift); USD is
// the legacy float emission kept for forward-compatible read by
// dashboards + ad-hoc tools that grep $.usd directly. Reader prefers
// $.usd_micro and falls back to $.usd*1e6 for pre-#554 rows.
// Deprecated dual-emit; legacy $.usd / $.delta_usd readers may drop float fields after 2026-Q4 (90 days of clean canonical-int reads).
//
// Writers populate BOTH on every new row so the shadow window is
// read-side-only (no operator config flag, no migration script).
// Downstream the legacy `usd` field is informational; budget cap
// enforcement runs exclusively against `usd_micro`.
type TokenSpendPayload struct {
	USDMicro            USDMicro `json:"usd_micro"`
	USD                 float64  `json:"usd"`
	Model               string   `json:"model"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	OperatorID          string   `json:"operator_id"`
	DAGID               string   `json:"dag_id"`
	WorkItemID          string   `json:"work_item_id"`
	PricingRev          string   `json:"pricing_rev"`
	CallID              string   `json:"call_id"`
}

// BudgetReconciledPayload is the substrate `kind='budget_reconciled'`
// payload shape per spec §3.5 lines 278-289. Emitted by the reconciler
// (T4); reducer is LWW per (tenant_id, period_start) so re-runs do not
// double-count.
//
// Money fields (#554): *_micro int64 hold the canonical signed value;
// *USD float64 are kept for legacy dashboard read. DriftPct stays
// float — it's a ratio, not money, and the cap-enforcement path does
// not aggregate ratios.
// Deprecated dual-emit; legacy $.usd / $.delta_usd readers may drop float fields after 2026-Q4 (90 days of clean canonical-int reads).
type BudgetReconciledPayload struct {
	PeriodStart      int64               `json:"period_start"`
	PeriodEnd        int64               `json:"period_end"`
	ActualUSDMicro   USDMicro            `json:"actual_usd_micro"`
	RecordedUSDMicro USDMicro            `json:"recorded_usd_micro"`
	DeltaUSDMicro    USDMicro            `json:"delta_usd_micro"`
	ActualUSD        float64             `json:"actual_usd"`
	RecordedUSD      float64             `json:"recorded_usd"`
	DeltaUSD         float64             `json:"delta_usd"`
	DriftPct         float64             `json:"drift_pct"`
	ModelBreakdown   []ModelBreakdownRow `json:"model_breakdown"`
	APIResponseSig   string              `json:"api_response_sig"`
}

// ModelBreakdownRow is one Anthropic Cost/Usage API bucket per model
// per period. Sums across rows reconcile to BudgetReconciledPayload's
// totals. Money fields follow the same dual-emit shape as the parent
// payload.
type ModelBreakdownRow struct {
	Model               string   `json:"model"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	USDMicro            USDMicro `json:"usd_micro"`
	USD                 float64  `json:"usd"`
}

// CallRecord is the input shape RecordCall accepts. The writer prices
// the call via pricing.Lookup and builds the TokenSpendPayload from
// these inputs plus the priced row.
type CallRecord struct {
	CallID              string
	RetrySeq            int
	Model               string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	OperatorID          string
	DAGID               string
	WorkItemID          string
	TenantID            string
	WrittenBy           string
	RunID               string
}
