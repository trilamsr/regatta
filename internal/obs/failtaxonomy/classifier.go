// Package failtaxonomy classifies dispatch / PR failure logs into a
// closed enum and emits a counter labeled with the bucket (spec
// OBS-WAVE-C-T4 §5). Hot-path constraint: regex-table only, P95 < 5ms
// on the last 8KB of CI output. LLM verify-only fallback deferred to
// W4 — deterministic operational signal beats slow non-deterministic
// classification.
package failtaxonomy

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Taxonomy is the closed enum of failure buckets surfaced on the
// dashboard pie + heatmap. Adding a 9th bucket MUST update
// TestFailureTaxonomyEnum_Closed AND the cardinality budget in §5.1.
type Taxonomy string

// Closed-enum failure buckets surfaced on the operator dashboard.
const (
	TaxGateReject    Taxonomy = "gate_reject"
	TaxCIFail        Taxonomy = "ci_fail"
	TaxReviewerBlock Taxonomy = "reviewer_block"
	TaxConflict      Taxonomy = "conflict"
	TaxTimeout       Taxonomy = "timeout"
	TaxCostCap       Taxonomy = "cost_cap"
	TaxCrash         Taxonomy = "crash"
	TaxUnknown       Taxonomy = "unknown"
)

// AllTaxonomies returns the canonical bucket list in declaration order.
func AllTaxonomies() []Taxonomy {
	return []Taxonomy{
		TaxGateReject, TaxCIFail, TaxReviewerBlock, TaxConflict,
		TaxTimeout, TaxCostCap, TaxCrash, TaxUnknown,
	}
}

const scopeName = "github.com/trilamsr/regatta/internal/obs/failtaxonomy"

// tailBytes is the trailing log window the regex sweep scans. 99% of
// failure signatures live near the end of the CI log; bounding here
// keeps P95 classify latency under 5ms on multi-MB logs (R3).
const tailBytes = 8 * 1024

// rule pairs a compiled pattern with its taxonomy bucket. First match
// wins, so order is load-bearing — most-specific patterns at the top.
type rule struct {
	pattern *regexp.Regexp
	bucket  Taxonomy
}

// Operator-readable canonical pattern table. Compile-once at package
// init keeps Classify allocation-free.
var rules = []rule{
	{regexp.MustCompile(`(?i)(cost.cap|budget.exceed|cost.exhausted|tenant.cost.cap)`), TaxCostCap},
	{regexp.MustCompile(`(?i)(context\s+deadline\s+exceeded|timed?\s*out|deadline\s+exceeded)`), TaxTimeout},
	{regexp.MustCompile(`(?i)(merge\s+conflict|conflict\s+in\s+\S+|both\s+modified:)`), TaxConflict},
	{regexp.MustCompile(`(?i)(gate.reject|cel.decider.*reject|policy.block|forbidden\s+pattern)`), TaxGateReject},
	{regexp.MustCompile(`(?i)(reviewer.block|review.unresolved|changes\s+requested|review.required)`), TaxReviewerBlock},
	{regexp.MustCompile(`(?i)(panic:|runtime\s+error|segmentation\s+fault|oomkill|killed\s+by\s+signal)`), TaxCrash},
	// CI-fail last: more-specific buckets win first.
	{regexp.MustCompile(`(?i)(ci\s+fail|test\s+fail|build\s+fail|exit\s+(status\s+)?[1-9]|FAIL\s+\S+|lint\s+fail|compile\s+fail|\.go:\d+:\s+undefined)`), TaxCIFail},
}

// Classify scans the trailing tailBytes for a rule match. Empty /
// no-match routes to TaxUnknown (spec §5.1).
func Classify(logTail string) Taxonomy {
	if len(logTail) > tailBytes {
		logTail = logTail[len(logTail)-tailBytes:]
	}
	for _, r := range rules {
		if r.pattern.MatchString(logTail) {
			return r.bucket
		}
	}
	return TaxUnknown
}

// Config holds the meter DI handle. Nil falls back to otel.Meter on
// the package scope.
type Config struct {
	Meter metric.Meter
}

// ResolveMeter returns Meter or a lazily-resolved global fallback.
func (c Config) ResolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter(scopeName)
}

// Record classifies logTail and increments the failure counter under
// the resolved bucket. Counter-create error is dropped — telemetry
// MUST NOT mask the underlying failure that triggered the call.
func Record(ctx context.Context, cfg Config, logTail string) Taxonomy {
	bucket := Classify(logTail)
	ctr, err := cfg.ResolveMeter().Int64Counter("regatta.pr.failure")
	if err == nil {
		ctr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("taxonomy", string(bucket)),
		))
	}
	return bucket
}
