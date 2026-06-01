package estimate_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/cost/estimate"
)

// TestProbe_CountTokensClaudeCLI_DetectsCapability pins CLI-flag detection + heuristic fallback + no-panic on missing binary.
func TestProbe_CountTokensClaudeCLI_DetectsCapability(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not portable to windows")
	}

	t.Run("missing_binary_falls_back", func(t *testing.T) {
		p, err := estimate.NewProbe(estimate.ProbeConfig{Command: filepath.Join(t.TempDir(), "no-such-claude-binary")})
		if err != nil {
			t.Fatalf("NewProbe missing binary returned error %v; want nil + heuristic mode", err)
		}
		if p.Mode != estimate.ProbeModeHeuristic {
			t.Fatalf("missing binary: Mode=%v; want ProbeModeHeuristic", p.Mode)
		}
		got := p.CountTokens([]byte("hello world"))
		if got <= 0 {
			t.Fatalf("CountTokens returned %d; want > 0", got)
		}
	})

	t.Run("supports_flag", func(t *testing.T) {
		dir := t.TempDir()
		stub := filepath.Join(dir, "claude")
		// Stub binary: supports --count-tokens (prints integer when invoked
		// with the flag); fails closed otherwise so the probe can tell.
		script := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = \"--count-tokens\" ]; then\n    echo 42\n    exit 0\n  fi\ndone\nexit 1\n"
		if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
			t.Fatalf("write stub: %v", err)
		}
		p, err := estimate.NewProbe(estimate.ProbeConfig{Command: stub, ProbeTimeout: 30 * time.Second})
		if err != nil {
			t.Fatalf("NewProbe: %v", err)
		}
		if p.Mode != estimate.ProbeModeClaudeCLI {
			t.Fatalf("Mode=%v; want ProbeModeClaudeCLI", p.Mode)
		}
	})

	t.Run("missing_flag_falls_back", func(t *testing.T) {
		dir := t.TempDir()
		stub := filepath.Join(dir, "claude")
		// Stub binary: rejects every flag (simulates old claude CLI).
		script := "#!/bin/sh\nexit 1\n"
		if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
			t.Fatalf("write stub: %v", err)
		}
		p, err := estimate.NewProbe(estimate.ProbeConfig{Command: stub, ProbeTimeout: 30 * time.Second})
		if err != nil {
			t.Fatalf("NewProbe: %v", err)
		}
		if p.Mode != estimate.ProbeModeHeuristic {
			t.Fatalf("missing-flag: Mode=%v; want ProbeModeHeuristic", p.Mode)
		}
	})
}

// TestProbe_HeuristicFallbackAddsSafetyMargin pins R11/I1 mitigation (≥ 50% above raw len/4).
func TestProbe_HeuristicFallbackAddsSafetyMargin(t *testing.T) {
	p, err := estimate.NewProbe(estimate.ProbeConfig{Command: "/nonexistent/claude-bin"})
	if err != nil {
		t.Fatalf("NewProbe: %v", err)
	}
	if p.Mode != estimate.ProbeModeHeuristic {
		t.Fatalf("missing binary should yield heuristic mode; got %v", p.Mode)
	}

	// Use a long input so integer-division rounding does not dominate the margin.
	input := []byte(strings.Repeat("x", 4_000))
	raw := int64(len(input) / 4) // 1000
	got := p.CountTokens(input)
	wantFloor := raw + raw/2 // ≥ 50% above raw
	if got < wantFloor {
		t.Fatalf("heuristic safety margin too small: got=%d raw=%d wantFloor=%d", got, raw, wantFloor)
	}
}
