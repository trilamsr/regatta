// Package failtaxonomy classifies dispatch / PR failure logs into a
// closed enum and emits a counter labeled with the bucket. Spec
// OBS-WAVE-C-T4 §5.
//
// Hot-path constraints: regex-table only, no LLM, P95 < 5ms target on
// realistic log payloads (last 8KB of CI output). LLM verify-only fallback
// is deferred to W4 self-improve per `feedback_research_design_principles`
// (deterministic operational signal beats slow non-deterministic
// classification).
package failtaxonomy

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Taxonomy is the closed enum of failure buckets the operator sees on
// the dashboard pie + heatmap.
type Taxonomy string

// Closed enum — adding a 9th bucket MUST update TestFailureTaxonomyEnum_Closed
// AND the counter's cardinality budget docstring in spec §5.1.
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

// scopeName pins the OTel instrumentation scope.
const scopeName = "github.com/trilamsr/regatta/internal/obs/failtaxonomy"

// tailBytes is the trailing log window the regex sweep scans. 99% of
// failure signatures live near the end of the CI log; bounding the
// scan keeps P95 classify latency under the 5ms target on multi-MB
// logs (R3).
const tailBytes = 8 * 1024

// rule pairs a compiled pattern with its taxonomy bucket. First match
// wins so order is load-bearing — the most-specific patterns sit at
// the top of the table.
type rule struct {
	pattern *regexp.Regexp
	bucket  Taxonomy
}

// rules is the canonical regex table. Operator-readable patterns — each
// maps to one bucket. Compile-once at package init keeps hot-path
// Classify allocation-free.
var rules = []rule{
	// Cost-cap fires before any compute starts; check first.
	{regexp.MustCompile(`(?i)(cost.cap|budget.exceed|cost.exhausted|tenant.cost.cap)`), TaxCostCap},
	// Timeout — context deadline or explicit timeout.
	{regexp.MustCompile(`(?i)(context\s+deadline\s+exceeded|timed?\s*out|deadline\s+exceeded)`), TaxTimeout},
	// Merge conflict — git-surface signature.
	{regexp.MustCompile(`(?i)(merge\s+conflict|conflict\s+in\s+\S+|both\s+modified:)`), TaxConflict},
	// Gate-reject — CELDecider verdict in substrate.
	{regexp.MustCompile(`(?i)(gate.reject|cel.decider.*reject|policy.block|forbidden\s+pattern)`), TaxGateReject},
	// Reviewer-block — review-loop or PR-comment signal.
	{regexp.MustCompile(`(?i)(reviewer.block|review.unresolved|changes\s+requested|review.required)`), TaxReviewerBlock},
	// Crash — panic / segfault / OOM / killed.
	{regexp.MustCompile(`(?i)(panic:|runtime\s+error|segmentation\s+fault|oomkill|killed\s+by\s+signal)`), TaxCrash},
	// CI-fail — generic CI surface; placed last so more-specific buckets
	// win first.
	{regexp.MustCompile(`(?i)(ci\s+fail|test\s+fail|build\s+fail|exit\s+(status\s+)?[1-9]|FAIL\s+\S+|lint\s+fail|compile\s+fail|\.go:\d+:\s+undefined)`), TaxCIFail},
}

// Classify scans the trailing 8KB of log for a rule match and returns
// the first-match bucket. Empty / no-match input routes to TaxUnknown
// per spec §5.1.
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

// ResolveMeter returns the configured meter or falls back lazily.
func (c Config) ResolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter(scopeName)
}

// Record classifies logTail and increments the failure counter under
// the resolved bucket. The taxonomy label is closed-enum-bounded so
// steady-state cardinality is len(AllTaxonomies()) cells. Counter-create
// error is dropped — telemetry MUST NOT mask the underlying failure
// that triggered the classify call.
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
