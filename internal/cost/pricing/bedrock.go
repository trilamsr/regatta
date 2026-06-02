package pricing

import "time"

// Bedrock maps AWS-Bedrock-tier Claude SKUs → per-million-token USD rates.
//
// SKU key shape: "bedrock.<anthropic-model>" — caller derives the prefix
// from operator config (e.g. safety.cost.provider="bedrock"). The bare
// anthropic key stays the default surface so existing callers are
// unaffected. Issue #240 closes the spec §2 OOS entry.
//
// Source of truth: https://aws.amazon.com/bedrock/pricing/ — Anthropic
// provider tab, Standard / On-Demand tier (verified 2026-06-02). AWS
// price-matches Anthropic list for Claude models on the Standard tier;
// Batch (-50%) and Provisioned Throughput tiers are out of scope for
// Wave 2 and DO NOT belong in this table. Operators on a non-Standard
// Bedrock tier MUST use pricing_override_path (#239) — that is the
// spec-aligned escape hatch.
//
// EVERY change to this table is a code-review event with a PR body
// citing the Bedrock pricing-page snapshot URL or commit-pinned reference
// per the "Pricing refresh" runbook in docs/operator/cost-governor.md.
var Bedrock = map[string]Row{
	"bedrock.claude-opus-4-7":   {InputUSDPerMTok: 15.00, CacheReadUSDPerMTok: 1.50, CacheCreationUSDPerMTok: 18.75, OutputUSDPerMTok: 75.00, RetiredAfter: time.Time{}},
	"bedrock.claude-sonnet-4-7": {InputUSDPerMTok: 3.00, CacheReadUSDPerMTok: 0.30, CacheCreationUSDPerMTok: 3.75, OutputUSDPerMTok: 15.00, RetiredAfter: time.Time{}},
	"bedrock.claude-haiku-4-5":  {InputUSDPerMTok: 0.80, CacheReadUSDPerMTok: 0.08, CacheCreationUSDPerMTok: 1.00, OutputUSDPerMTok: 4.00, RetiredAfter: time.Time{}},
}
