package reconcile

// expectedCostBucketFields pins the Anthropic Cost API field set the
// CostBucket decoder consumes. A rename in a future anthropic-version
// bump (e.g. cost_usd → cost_amount_usd) drops actualUSD to 0 silently
// — the schema_pin_test catches it BEFORE the reconciler ships. The
// quarterly runbook in docs/operator/cost-governor.md §Anthropic
// response-shape pin walks the live-diff refresh procedure (#277).
var expectedCostBucketFields = []string{
	"bucket_start",
	"bucket_end",
	"model",
	"cost_usd",
}

// expectedUsageBucketFields pins the Anthropic Usage API field set the
// UsageBucket decoder consumes; same drift-defense rationale as
// expectedCostBucketFields (#277).
var expectedUsageBucketFields = []string{
	"bucket_start",
	"bucket_end",
	"model",
	"uncached_input_tokens",
	"cache_read_input_tokens",
	"cache_creation_input_tokens",
	"output_tokens",
}
