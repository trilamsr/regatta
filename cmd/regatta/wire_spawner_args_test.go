package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// TestDefaultClaudeArgs asserts the headless flag set is non-empty so #1085's silent-stdout regression cannot return.
func TestDefaultClaudeArgs(t *testing.T) {
	t.Setenv(envSpawnerMCPConfig, "")
	args, cleanup, err := defaultClaudeArgs()
	if err != nil {
		t.Fatalf("defaultClaudeArgs: %v", err)
	}
	t.Cleanup(cleanup)
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
	if set.Cleanup != nil {
		t.Cleanup(set.Cleanup)
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

// TestDefaultClaudeArgs_MCPConfigDefaultsToEmptyJSONTempfile pins #1116: claude CLI rejects `--mcp-config=/dev/null` at boot ("MCP config is not a valid JSON") so the default tempfile MUST contain a parseable `{"mcpServers":{}}` payload, not be os.DevNull.
func TestDefaultClaudeArgs_MCPConfigDefaultsToEmptyJSONTempfile(t *testing.T) {
	t.Setenv(envSpawnerMCPConfig, "")
	args, cleanup, err := defaultClaudeArgs()
	if err != nil {
		t.Fatalf("defaultClaudeArgs: %v", err)
	}
	t.Cleanup(cleanup)

	var mcpPath string
	for _, a := range args {
		if strings.HasPrefix(a, "--mcp-config=") {
			mcpPath = strings.TrimPrefix(a, "--mcp-config=")
			break
		}
	}
	if mcpPath == "" {
		t.Fatalf("--mcp-config=<path> missing from args=%v", args)
	}
	if mcpPath == os.DevNull {
		t.Fatalf("--mcp-config=%s is rejected by claude CLI as invalid JSON (#1116); want path to tempfile containing {\"mcpServers\":{}}", os.DevNull)
	}
	body, err := os.ReadFile(mcpPath) //nolint:gosec // mcpPath is os.CreateTemp() name, not user input.
	if err != nil {
		t.Fatalf("read mcp tempfile %q: %v", mcpPath, err)
	}
	var parsed struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("mcp tempfile %q does not parse as JSON: %v (body=%q)", mcpPath, err, body)
	}
	if parsed.MCPServers == nil {
		t.Fatalf("mcp tempfile %q missing mcpServers key; got body=%q", mcpPath, body)
	}
	if len(parsed.MCPServers) != 0 {
		t.Fatalf("mcp tempfile %q has non-empty mcpServers=%v; default must be zero-MCP", mcpPath, parsed.MCPServers)
	}
}

// TestDefaultClaudeArgs_MCPConfigCleanupRemovesTempfile asserts the returned cleanup func removes the tempfile so /tmp doesn't leak across serve restarts.
func TestDefaultClaudeArgs_MCPConfigCleanupRemovesTempfile(t *testing.T) {
	t.Setenv(envSpawnerMCPConfig, "")
	args, cleanup, err := defaultClaudeArgs()
	if err != nil {
		t.Fatalf("defaultClaudeArgs: %v", err)
	}
	var mcpPath string
	for _, a := range args {
		if strings.HasPrefix(a, "--mcp-config=") {
			mcpPath = strings.TrimPrefix(a, "--mcp-config=")
			break
		}
	}
	if _, err := os.Stat(mcpPath); err != nil {
		t.Fatalf("tempfile %q must exist before cleanup; stat err=%v", mcpPath, err)
	}
	cleanup()
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Fatalf("tempfile %q must be removed after cleanup; stat err=%v", mcpPath, err)
	}
}

// TestDefaultClaudeArgs_MCPConfigEnvOverride confirms operator can override the default via env (#1086 escape hatch). Uses t.TempDir() so the path is portable across CI hosts.
func TestDefaultClaudeArgs_MCPConfigEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv(envSpawnerMCPConfig, override)
	args, cleanup, err := defaultClaudeArgs()
	if err != nil {
		t.Fatalf("defaultClaudeArgs: %v", err)
	}
	t.Cleanup(cleanup)
	want := "--mcp-config=" + override
	if !slices.Contains(args, want) {
		t.Fatalf("want %s in args, got %v", want, args)
	}
}

// TestDefaultClaudeArgs_MCPConfigEnvOverrideSkipsTempfile: when operator pins REGATTA_SPAWNER_MCP_CONFIG, the spawner MUST NOT create or clean up a tempfile — the operator owns that path.
func TestDefaultClaudeArgs_MCPConfigEnvOverrideSkipsTempfile(t *testing.T) {
	override := filepath.Join(t.TempDir(), "operator-owned.json")
	t.Setenv(envSpawnerMCPConfig, override)
	_, cleanup, err := defaultClaudeArgs()
	if err != nil {
		t.Fatalf("defaultClaudeArgs: %v", err)
	}
	// Cleanup MUST be a no-op (override path is operator-owned and was never written by us).
	cleanup()
	if _, err := os.Stat(override); !os.IsNotExist(err) {
		t.Fatalf("override path %q must not be created by the spawner; stat err=%v", override, err)
	}
}
