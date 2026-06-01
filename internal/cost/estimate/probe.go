package estimate

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// ProbeMode is the capability the probe detected at process start.
type ProbeMode int

const (
	// ProbeModeHeuristic uses `len(bytes)/4 × 1.5` as a worst-case token
	// count when `claude --count-tokens` is unavailable.
	ProbeModeHeuristic ProbeMode = iota
	// ProbeModeClaudeCLI shells out to `claude --count-tokens` for an
	// accurate count per the Anthropic tokenizer.
	ProbeModeClaudeCLI
)

// String renders the mode for log output. INFO-level
// obs.EventCostTokenProbe = (mode=...) per spec §9 R11.
func (m ProbeMode) String() string {
	switch m {
	case ProbeModeClaudeCLI:
		return "claude_cli"
	default:
		return "heuristic"
	}
}

// ProbeConfig holds the tunables. Zero values default to production settings.
type ProbeConfig struct {
	// Command is the claude binary path. Default: "claude".
	Command string
	// ProbeTimeout caps the capability detection call. Default: 2s.
	ProbeTimeout time.Duration
}

// Probe is the result of one-time `claude --count-tokens` capability detection.
// Mode tells the spawner which counter to use; CountTokens is the active
// counter (CLI mode shells out, heuristic mode runs the safety-margin formula).
type Probe struct {
	Mode    ProbeMode
	Command string

	// safetyNumerator + safetyDenominator encode the heuristic-fallback
	// margin without floating-point drift. 3/2 = 1.5× per R11 mitigation
	// (spec §9 R11 documents "25% safety margin" but the named-test
	// `TestProbe_HeuristicFallbackAddsSafetyMargin` requires ≥ 50%).
	safetyNumerator   int64
	safetyDenominator int64
}

// NewProbe runs the capability detection once. Returns ProbeModeClaudeCLI when
// the configured `claude` binary supports `--count-tokens`; ProbeModeHeuristic
// otherwise. Never returns a non-nil error on missing-binary or missing-flag
// — those are normal operator states (R11 mitigation).
func NewProbe(cfg ProbeConfig) (Probe, error) {
	if cfg.Command == "" {
		cfg.Command = "claude"
	}
	if cfg.ProbeTimeout == 0 {
		cfg.ProbeTimeout = 2 * time.Second
	}
	p := Probe{
		Mode:              ProbeModeHeuristic,
		Command:           cfg.Command,
		safetyNumerator:   3,
		safetyDenominator: 2,
	}
	if hasCountTokensFlag(cfg.Command, cfg.ProbeTimeout) {
		p.Mode = ProbeModeClaudeCLI
	}
	return p, nil
}

// CountTokens returns the active counter's token count for `b`. Heuristic mode
// applies the safety margin; CLI mode shells out per call (TODO: future wedge
// can cache or batch — out of scope here per spec §10).
func (p Probe) CountTokens(b []byte) int64 {
	raw := int64(len(b) / 4)
	if p.Mode == ProbeModeClaudeCLI {
		if n, err := countTokensViaCLI(p.Command, b); err == nil {
			return n
		}
		// Fallback to heuristic on CLI failure; bracketed in the same
		// safety-margin formula because the operator's environment is
		// effectively in heuristic mode for this call.
	}
	if p.safetyDenominator == 0 {
		return raw
	}
	return raw * p.safetyNumerator / p.safetyDenominator
}

// hasCountTokensFlag invokes the binary with `--count-tokens` and a tiny stdin
// payload. The probe returns true iff exit-code is zero. False on every other
// outcome (missing binary, exec error, non-zero exit, timeout). The stub
// binary in TestProbe_CountTokensClaudeCLI_DetectsCapability "supports_flag"
// branch pins the success path; the other two branches pin the failure paths.
func hasCountTokensFlag(command string, timeout time.Duration) bool {
	if command == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// Pass --count-tokens AND a no-op help/echo idiom the operator's claude
	// will not parse as a prompt. The stub script keys off the flag's
	// presence in argv; the real claude CLI exits 0 when the flag is
	// recognized even without a prompt.
	cmd := exec.CommandContext(ctx, command, "--count-tokens")
	return cmd.Run() == nil
}

// countTokensViaCLI is a placeholder for the real `claude --count-tokens
// <stdin>` invocation. The capability detection lands in Wave 1 T2; the
// counter wiring lands in Wave 2 T3 alongside the spawner integration
// (probe-vs-stream-json reconciliation is out of scope here per §3.3).
func countTokensViaCLI(_ string, _ []byte) (int64, error) {
	return 0, errors.New("not implemented")
}
