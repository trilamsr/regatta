package pricing

import "time"

// Anthropic maps model SKU → per-million-token USD rates.
//
// Source of truth: https://docs.anthropic.com/en/docs/about-claude/pricing
// Refresh runbook lands in docs/operator/cost-governor.md §"Pricing refresh"
// (Wave 3 T5 deliverable). Each row carries (input, cache_read,
// cache_creation, output) USD-per-Mtok plus a RetiredAfter sunset marker
// (zero value = still active).
//
// EVERY change to this table is a code-review event with a PR body citing
// the pricing-page screenshot or commit-pinned URL.
var Anthropic = map[string]Row{
	"claude-opus-4-7":   {InputUSDPerMTok: 15.00, CacheReadUSDPerMTok: 1.50, CacheCreationUSDPerMTok: 18.75, OutputUSDPerMTok: 75.00, RetiredAfter: time.Time{}},
	"claude-sonnet-4-7": {InputUSDPerMTok: 3.00, CacheReadUSDPerMTok: 0.30, CacheCreationUSDPerMTok: 3.75, OutputUSDPerMTok: 15.00, RetiredAfter: time.Time{}},
	"claude-haiku-4-5":  {InputUSDPerMTok: 0.80, CacheReadUSDPerMTok: 0.08, CacheCreationUSDPerMTok: 1.00, OutputUSDPerMTok: 4.00, RetiredAfter: time.Time{}},
}
