// Package l4 implements the adversarial-reviewer gate. Mirrors the
// internal/gates/security/ Run(ctx, cfg, in) seam so the scheduler can
// apply it alongside L0 + security at gate-loop time.
package l4

import (
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// scopeName is the OTel instrumentation scope for l4 (keeps the per-scope query slice grep-able).
const scopeName = "github.com/trilamsr/regatta/internal/gates/l4"

// DefaultModel is the spec-binding default; resolution order yaml > env > default (spec §3.6).
const DefaultModel = "claude-sonnet-4-6"

// EnvModel is the unattended-loop escape hatch — yaml still wins, env fills the gap when yaml is empty.
const EnvModel = "REGATTA_GATES_L4_MODEL"

// DefaultMaxDiffChars clips oversize diffs so one PR cannot blow the LLM context;
// above this short-circuits to advisory with an L4-DIFF-OVERSIZE finding.
const DefaultMaxDiffChars = 50_000

// Config is the per-repo gate config — the regatta.yaml gates[] row
// mapped to Go. See internal/gates/l4/README.md for the YAML shape.
type Config struct {
	GateID string

	// Model resolves through ResolveModel; callers pass the yaml value, env + default apply at gate construction.
	Model string

	// SeverityBlock is the mini-DSL list (e.g. ["critical","2*high"]); empty defaults to spec §3.6 R1/R2 baseline.
	SeverityBlock []string

	// MaxDiffChars zero falls back to DefaultMaxDiffChars.
	MaxDiffChars int

	// AdvisoryMode demotes would-be VerdictFail to VerdictAdvisory for
	// the first 100 self-host PRs (spec §4 wave-1 rollout).
	AdvisoryMode bool

	// SecondOpinionModel is the alt model used on [L4-DISPUTE] PRs;
	// empty falls through ResolveSecondOpinionModel to env then default (Opus 4.7).
	SecondOpinionModel string

	// AutoFix opts the gate into surfacing unified-diff Patch bodies on
	// auto_fixable=true findings. Default false strips Patch +
	// AutoFixable before emitting GateResult — operator must opt in
	// (#358). The gate never applies the patch.
	AutoFix bool

	// Invoker is the model call-site (tests inject a stub). Nil panics at Run time so the wiring contract is explicit.
	Invoker Invoker

	// CategoryModels overrides per-category model assignment (keys are
	// spec §3.4 hunt-list category names). Run buckets categories by
	// distinct resolved model and emits one Invoker call per bucket;
	// findings merge before severity routing. Unmapped categories fall
	// back to Model via ResolveCategoryModel.
	CategoryModels map[string]string

	// Meter resolves lazily to otel.Meter(scopeName) when nil so global MeterProvider Setup wins by default.
	Meter metric.Meter

	// Clock nil falls back to time.Now.
	Clock func() time.Time
}

// ResolveMeter returns the configured meter or falls back to the
// global provider's scoped meter. Lazy so a global provider swap takes
// effect on the next call.
func (c Config) ResolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter(scopeName)
}

// Input is what the gate runs against. Constructed by the gate runner
// from PR head SHA + diff + binding-spec + posted scorecard.
type Input struct {
	PRSHA     string
	BaseSHA   string
	RunID     string
	RepoRoot  string
	Diff      string
	Spec      string
	Scorecard string
	PRBody    string
}

// ResolveModel applies spec §3.6 precedence: yaml > env > default.
// Resolution at gate-config load, NOT per-Run — a mid-run model swap
// requires a regatta serve restart (hot-reload deferred §6 followup).
func ResolveModel(yamlVal string) string {
	if yamlVal != "" {
		return yamlVal
	}
	if env := os.Getenv(EnvModel); env != "" {
		return env
	}
	return DefaultModel
}
