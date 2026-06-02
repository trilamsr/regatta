package pricing

import "time"

// Vertex maps GCP-Vertex-tier Claude SKUs → per-million-token USD rates.
//
// SKU key shape: "vertex.<anthropic-model>" — caller derives the prefix
// from operator config (e.g. safety.cost.provider="vertex"). Bare keys
// remain the Anthropic-native default. Issue #240 closes spec §2 OOS.
//
// Source of truth: https://cloud.google.com/vertex-ai/generative-ai/pricing
// — Partner models > Anthropic section, Standard tier (verified
// 2026-06-02). Google price-matches Anthropic list for Claude models on
// the Standard tier; Provisioned Throughput and the Vertex regional
// quota tiers are out of scope for Wave 2 and DO NOT belong in this
// table. Operators on a non-Standard Vertex tier MUST use
// pricing_override_path (#239) — that is the spec-aligned escape hatch.
//
// EVERY change to this table is a code-review event with a PR body
// citing the Vertex pricing-page snapshot URL or commit-pinned reference
// per the "Pricing refresh" runbook in docs/operator/cost-governor.md.
var Vertex = map[string]Row{
	"vertex.claude-opus-4-7":   {InputUSDPerMTok: 15.00, CacheReadUSDPerMTok: 1.50, CacheCreationUSDPerMTok: 18.75, OutputUSDPerMTok: 75.00, RetiredAfter: time.Time{}},
	"vertex.claude-sonnet-4-7": {InputUSDPerMTok: 3.00, CacheReadUSDPerMTok: 0.30, CacheCreationUSDPerMTok: 3.75, OutputUSDPerMTok: 15.00, RetiredAfter: time.Time{}},
	"vertex.claude-haiku-4-5":  {InputUSDPerMTok: 0.80, CacheReadUSDPerMTok: 0.08, CacheCreationUSDPerMTok: 1.00, OutputUSDPerMTok: 4.00, RetiredAfter: time.Time{}},
}
