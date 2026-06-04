package reconcile

// Field-set pins fail-close on silent Anthropic rename (#277).
var expectedCostBucketFields = []string{
	"bucket_start",
	"bucket_end",
	"model",
	"cost_usd",
}

var expectedUsageBucketFields = []string{
	"bucket_start",
	"bucket_end",
	"model",
	"uncached_input_tokens",
	"cache_read_input_tokens",
	"cache_creation_input_tokens",
	"output_tokens",
}
