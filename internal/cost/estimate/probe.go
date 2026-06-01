package estimate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
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
	Command      string        // claude binary path; default "claude"
	ProbeTimeout time.Duration // capability-detection deadline; default 2s
}

// Probe is the resolved counter selected at process start. Mode picks the
// CountTokens path; the safety fraction is non-zero only in heuristic mode
// and encodes the spec §9 R11 mitigation as integer math to avoid float drift.
type Probe struct {
	Mode    ProbeMode
	Command string

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

// CountTokens returns the active counter's token count for `b`. CLI mode shells
// out per call; on shell-out error the call falls through to the heuristic
// counter so a transient claude failure never crashes the gate.
func (p Probe) CountTokens(b []byte) int64 {
	if p.Mode == ProbeModeClaudeCLI {
		if n, err := countTokensViaCLI(p.Command, b); err == nil {
			return n
		}
	}
	raw := int64(len(b) / 4)
	if p.safetyDenominator == 0 {
		return raw
	}
	return raw * p.safetyNumerator / p.safetyDenominator
}

// hasCountTokensFlag returns true iff the binary exits 0 for `--count-tokens`.
// Every other outcome (missing binary, exec error, non-zero exit, timeout) is
// false, which routes the probe to ProbeModeHeuristic.
func hasCountTokensFlag(command string, timeout time.Duration) bool {
	if command == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// #nosec G204 -- command comes from operator-controlled ProbeConfig
	// (same trust model as internal/orchestrator/spawner/claude.go).
	cmd := exec.CommandContext(ctx, command, "--count-tokens")
	return cmd.Run() == nil
}

// countTokensViaCLI invokes `<claude> --count-tokens` with the prompt on stdin.
// Accepts two output shapes: plain integer ("42\n") and JSON
// ({"input_tokens": 42}). Anything else surfaces an error and the caller falls
// through to the heuristic counter.
func countTokensViaCLI(command string, prompt []byte) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// #nosec G204 -- command captured from operator-controlled ProbeConfig.
	cmd := exec.CommandContext(ctx, command, "--count-tokens")
	cmd.Stdin = bytes.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return 0, errors.New("probe: empty --count-tokens stdout")
	}
	if trimmed[0] == '{' {
		var c struct {
			InputTokens int64 `json:"input_tokens"`
		}
		if err := json.Unmarshal(trimmed, &c); err != nil {
			return 0, err
		}
		return c.InputTokens, nil
	}
	return strconv.ParseInt(string(trimmed), 10, 64)
}
