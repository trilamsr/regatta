package spend

// TokenSpendPayload is the substrate `kind='token_spend'` payload shape
// per cost-governor design spec §3.5 lines 264-276. One row per LLM
// call; cumulative spend = SUM(usd) over filtered window.
type TokenSpendPayload struct {
	USD                 float64 `json:"usd"`
	Model               string  `json:"model"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	OperatorID          string  `json:"operator_id"`
	DAGID               string  `json:"dag_id"`
	WorkItemID          string  `json:"work_item_id"`
	PricingRev          string  `json:"pricing_rev"`
	CallID              string  `json:"call_id"`
}

// BudgetReconciledPayload is the substrate `kind='budget_reconciled'`
// payload shape per spec §3.5 lines 278-289. Emitted by the reconciler
// (T4); reducer is LWW per (tenant_id, period_start) so re-runs do not
// double-count.
type BudgetReconciledPayload struct {
	PeriodStart    int64                `json:"period_start"`
	PeriodEnd      int64                `json:"period_end"`
	ActualUSD      float64              `json:"actual_usd"`
	RecordedUSD    float64              `json:"recorded_usd"`
	DeltaUSD       float64              `json:"delta_usd"`
	DriftPct       float64              `json:"drift_pct"`
	ModelBreakdown []ModelBreakdownRow  `json:"model_breakdown"`
	APIResponseSig string               `json:"api_response_sig"`
}

// ModelBreakdownRow is one Anthropic Cost/Usage API bucket per model
// per period. Sums across rows reconcile to BudgetReconciledPayload's
// totals.
type ModelBreakdownRow struct {
	Model               string  `json:"model"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	USD                 float64 `json:"usd"`
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
