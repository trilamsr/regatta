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

import "os"

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

	// Invoker is the model call-site. Tests inject a stub here.
	// Nil panics at Run time so the wiring contract is explicit.
	Invoker Invoker
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
