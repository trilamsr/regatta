// Failure taxonomy: closed enum + counter for dispatch/PR failures
// (spec OBS-WAVE-C-T4 §5). Regex-table only, P95 < 5ms on the last
// 8KB of CI output.
package obs

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

const failtaxonomyScopeName = "github.com/trilamsr/regatta/internal/obs/failtaxonomy"

// failtaxTailBytes bounds the regex sweep to the last 8KB of CI
// output — P95 classify latency stays <5ms on multi-MB logs (R3).
const failtaxTailBytes = 8 * 1024

// failtaxRule pairs a compiled pattern with its taxonomy bucket;
// first match wins so order in failtaxRules is load-bearing.
type failtaxRule struct {
	pattern *regexp.Regexp
	bucket  Taxonomy
}

var failtaxRules = []failtaxRule{
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
	if len(logTail) > failtaxTailBytes {
		logTail = logTail[len(logTail)-failtaxTailBytes:]
	}
	for _, r := range failtaxRules {
		if r.pattern.MatchString(logTail) {
			return r.bucket
		}
	}
	return TaxUnknown
}

// FailtaxonomyConfig holds the meter DI handle for failure recording.
// Nil falls back to otel.Meter on the package scope.
type FailtaxonomyConfig struct {
	Meter metric.Meter
}

// ResolveMeter returns Meter or a lazily-resolved global fallback.
func (c FailtaxonomyConfig) ResolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter(failtaxonomyScopeName)
}

// FailtaxonomyRecord classifies logTail and increments the failure
// counter under the resolved bucket. Counter-create error is dropped
// — telemetry MUST NOT mask the failure that triggered the call.
func FailtaxonomyRecord(ctx context.Context, cfg FailtaxonomyConfig, logTail string) Taxonomy {
	bucket := Classify(logTail)
	ctr, err := cfg.ResolveMeter().Int64Counter("regatta.pr.failure")
	if err == nil {
		ctr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("taxonomy", string(bucket)),
		))
	}
	return bucket
}
