// Package l4 implements the adversarial-reviewer gate. It mirrors
// the internal/gates/security/ Run(ctx, cfg, in) seam so the
// scheduler can apply it alongside L0 + security at gate-loop time.
//
// Phase: SKELETON. This drop ships the gate-config + severity-block
// routing + Run signature with a pluggable Invoker. The model
// call-site (stream-json adapter, prompt-template loader, OTel span
// emit, parse-fail tolerant fallback) lands in follow-ups tracked
// via [l4-followup] GH issues filed at PR-open.
package l4

import (
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// scopeName is the OTel instrumentation scope the l4 package emits
// metrics under. A-T2 wires gate-decide instruments against this name
// so the per-scope query slice ("gates/l4") stays grep-able.
const scopeName = "github.com/trilamsr/regatta/internal/gates/l4"

// DefaultModel is the spec-binding default. Override via
// regatta.yaml gates[].model or the EnvModel env var. The
// resolution order is yaml > env > default per spec §3.6.
const DefaultModel = "claude-sonnet-4-6"

// EnvModel is the unattended-loop escape hatch for the gate model.
// Yaml-config still wins when set; env only fills the gap when
// yaml leaves the model empty.
const EnvModel = "REGATTA_GATES_L4_MODEL"

// DefaultMaxDiffChars clips oversize diffs to keep one PR from
// blowing the LLM context window. Diffs above this short-circuit
// to advisory-only with an L4-DIFF-OVERSIZE finding.
const DefaultMaxDiffChars = 50_000

// Config is the per-repo gate config, the regatta.yaml gates[] row
// mapped to Go. See internal/gates/l4/README.md for the YAML shape.
type Config struct {
	// GateID is the gates[].id value, e.g. "l4_adversarial".
	GateID string

	// Model resolves through ResolveModel — yaml > env > default.
	// Callers should pass the yaml value directly; env + default
	// fallbacks apply on gate construction, not per Run.
	Model string

	// SeverityBlock is the mini-DSL list, e.g. ["critical","2*high"].
	// Empty defaults to the spec §3.6 R1/R2 baseline.
	SeverityBlock []string

	// MaxDiffChars clips oversize diffs. Zero falls back to
	// DefaultMaxDiffChars.
	MaxDiffChars int

	// AdvisoryMode runs the gate in non-blocking mode for the
	// first 100 self-host PRs per spec §4 wave-1 rollout. When
	// true, a would-be VerdictFail demotes to VerdictAdvisory
	// and Blocking stays false regardless of finding severity.
	AdvisoryMode bool

	// SecondOpinionModel is the alt model the gate escalates to
	// when the PR body disputes a finding via [L4-DISPUTE]. Empty
	// falls through ResolveSecondOpinionModel to env then default
	// (Opus 4.7). Per-Run only; set at config-load.
	SecondOpinionModel string

	// AutoFix opts the gate into surfacing unified-diff Patch
	// bodies on findings the model flags auto_fixable=true.
	// Default false strips Patch + AutoFixable off every finding
	// before emitting the GateResult — the operator must opt in
	// per issue #358. Downstream PR-comment posters render the
	// patch inside a fenced ```diff block; the gate itself never
	// applies the patch.
	AutoFix bool

	// Invoker is the model call-site. Tests inject a stub here.
	// Nil panics at Run time so the wiring contract is explicit.
	Invoker Invoker

	// CategoryModels overrides the per-category model assignment.
	// Keys are the spec §3.4 hunt-list category names (e.g.
	// "security", "refactor"). When set, Run buckets categories by
	// distinct resolved model and emits one Invoker call per bucket;
	// findings merge into the gate result before severity routing.
	// Unmapped categories fall back to the primary Model via
	// ResolveCategoryModel (yaml > env > primary).
	CategoryModels map[string]string

	// Meter is the OTel instrument factory for gate-decide telemetry.
	// Nil resolves to otel.Meter(scopeName) at the first ResolveMeter()
	// call so the global MeterProvider Setup wires (or a noop when
	// Setup was skipped) wins by default. Matches the W6 Config.Tracer
	// pattern so callers stay on one DI seam across trace + metric.
	Meter metric.Meter
}

// ResolveMeter returns the configured meter or falls back to the
// global provider's scoped meter. The fallback is lazy so a global
// provider swap (e.g. test injection of a noop provider) takes effect
// on the next call.
func (c Config) ResolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter(scopeName)
}

// Input is what the gate runs against. Constructed by the gate
// runner from PR head SHA + diff + binding-spec + posted scorecard.
type Input struct {
	PRSHA     string // PR head SHA
	BaseSHA   string // git-merge-base used for diff
	RunID     string // UUID; idempotency key shared across re-runs at same PR SHA
	RepoRoot  string // absolute path to checked-out repo
	Diff      string // unified-diff text; gate clips to MaxDiffChars
	Spec      string // binding-spec markdown inlined into the prompt
	Scorecard string // implementer's PR-body A+ rubric scorecard, verbatim
	PRBody    string // raw PR body; the gate greps it for [L4-DISPUTE] markers
}

// ResolveModel applies the spec §3.6 precedence: yaml > env > default.
// Empty yaml falls through to EnvModel; empty env falls through to
// DefaultModel. Resolution is at gate-config load, NOT per-Run, so a
// mid-run model swap requires a regatta serve restart (hot-reload is
// deferred per §6 followup).
func ResolveModel(yamlVal string) string {
	if yamlVal != "" {
		return yamlVal
	}
	if env := os.Getenv(EnvModel); env != "" {
		return env
	}
	return DefaultModel
}
