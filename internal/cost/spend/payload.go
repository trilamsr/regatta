package spend

// TokenSpendPayload is the substrate `kind='token_spend'` payload (spec §3.5):
// one row per LLM call; cumulative spend = SUM(usd_micro) over filtered window.
// USDMicro is canonical (eliminates SQLite SUM(float) ULP drift, #554); USD is
// legacy float dual-emit for dashboard read — readers prefer $.usd_micro,
// fall back to $.usd*1e6 for pre-#554 rows. Cap enforcement uses usd_micro
// exclusively; float fields removable after 2026-Q4.
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

// BudgetReconciledPayload is the substrate `kind='budget_reconciled'` payload
// (spec §3.5); reducer is LWW per (tenant_id, period_start) so re-runs don't
// double-count. *_micro int64 are canonical (#554); *USD float64 are legacy
// dashboard-read dual-emit removable after 2026-Q4. DriftPct stays float —
// it's a ratio, not money, and cap-enforcement doesn't aggregate ratios.
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

// ModelBreakdownRow is one Anthropic Cost/Usage API bucket per (model, period); sums reconcile to BudgetReconciledPayload totals. Money fields follow the parent's dual-emit shape.
type ModelBreakdownRow struct {
	Model               string   `json:"model"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	USDMicro            USDMicro `json:"usd_micro"`
	USD                 float64  `json:"usd"`
}

// CallRecord is the input shape RecordCall accepts; writer prices via pricing.Lookup and builds TokenSpendPayload from these fields plus the priced row.
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
