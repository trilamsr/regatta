package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// TestDefaultClaudeArgs asserts the headless flag set is non-empty so #1085's silent-stdout regression cannot return.
func TestDefaultClaudeArgs(t *testing.T) {
	t.Setenv(envSpawnerMCPConfig, "")
	args := defaultClaudeArgs()
	if len(args) == 0 {
		t.Fatalf("defaultClaudeArgs() returned empty; agents will spawn in TUI mode and emit no stdout (#1085)")
	}
	wants := map[string]bool{
		"--print":            false,
		claudeFlagStreamJSON: false,
		"--verbose":          false,
	}
	for _, a := range args {
		if _, ok := wants[a]; ok {
			wants[a] = true
		}
	}
	for flag, present := range wants {
		if !present {
			t.Fatalf("defaultClaudeArgs() missing %q (needed for ParseStream + operator log visibility)", flag)
		}
	}
}

// TestBuildSpawner_ClaudeWiresArgs asserts buildSpawner threads defaultClaudeArgs() through ClaudeSpawnerConfig.Args. A future edit setting Args: nil silently regresses #1085 unless this wire-path test catches it (per reviewer a99d05fff338bf7ec).
func TestBuildSpawner_ClaudeWiresArgs(t *testing.T) {
	tmp := t.TempDir()
	db := openTempDB(t, tmp)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	set, err := buildSpawner("claude", tmp, "claude", "HEAD", logger, db, nil, "")
	if err != nil {
		t.Fatalf("buildSpawner: %v", err)
	}
	cs, ok := set.Spawner.(*spawner.ClaudeSpawner)
	if !ok {
		t.Fatalf("set.Spawner = %T; want *spawner.ClaudeSpawner", set.Spawner)
	}
	args := cs.Config().Args
	if len(args) == 0 {
		t.Fatalf("ClaudeSpawnerConfig.Args is empty after buildSpawner; #1085 wire regression — agents will spawn in TUI mode")
	}
	wantOne := claudeFlagStreamJSON
	found := false
	for _, a := range args {
		if a == wantOne {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ClaudeSpawnerConfig.Args=%v, missing %q", args, wantOne)
	}
}

// TestDefaultClaudeArgs_MCPConfigDefaultsToPlatformNull pins the #1086 zero-MCP child default — every spawn passes --mcp-config=os.DevNull (Unix /dev/null, Windows NUL) when REGATTA_SPAWNER_MCP_CONFIG is unset. Uses os.DevNull so the test passes on Windows CI per reviewer ab98fe077a696dae6.
func TestDefaultClaudeArgs_MCPConfigDefaultsToPlatformNull(t *testing.T) {
	t.Setenv(envSpawnerMCPConfig, "")
	args := defaultClaudeArgs()
	want := "--mcp-config=" + os.DevNull
	if !slices.Contains(args, want) {
		t.Fatalf("want %s in args, got %v", want, args)
	}
}

// TestDefaultClaudeArgs_MCPConfigEnvOverride confirms operator can override the default via env (#1086 escape hatch). Uses t.TempDir() so the path is portable across CI hosts.
func TestDefaultClaudeArgs_MCPConfigEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv(envSpawnerMCPConfig, override)
	args := defaultClaudeArgs()
	want := "--mcp-config=" + override
	if !slices.Contains(args, want) {
		t.Fatalf("want %s in args, got %v", want, args)
	}
}
