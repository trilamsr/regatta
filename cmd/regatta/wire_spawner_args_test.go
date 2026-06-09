package main

import (
	"log/slog"
	"os"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// TestDefaultClaudeArgs asserts the headless flag set is non-empty so #1085's silent-stdout regression cannot return.
func TestDefaultClaudeArgs(t *testing.T) {
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
