package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

const (
	spawnerNameStub   = "stub"
	spawnerNameClaude = "claude"

	// envSpawnerMCPConfig overrides the default platform-null MCP config passed to spawned claude children. Empty = use os.DevNull (Unix /dev/null, Windows NUL).
	envSpawnerMCPConfig = "REGATTA_SPAWNER_MCP_CONFIG"
)

// claudeFlagStreamJSON pins the stream-json output flag (shared by impl + tests).
const claudeFlagStreamJSON = "--output-format=stream-json"

// defaultClaudeArgs are the headless flags the orchestrator stamps onto every claude CLI spawn. #1085 closes the TUI-mode silence (--print + stream-json + verbose); #1086 closes the MCP-inheritance blast (--mcp-config defaults to os.DevNull = /dev/null on Unix, NUL on Windows; override via REGATTA_SPAWNER_MCP_CONFIG).
func defaultClaudeArgs() []string {
	mcp := os.Getenv(envSpawnerMCPConfig)
	if mcp == "" {
		mcp = os.DevNull
	}
	return []string{"--print", claudeFlagStreamJSON, "--verbose", "--mcp-config=" + mcp}
}

// spawnerSet bundles the three handles a serve invocation needs to
// wire the Spawner + Reaper. Only the claude backend populates
// Killer + Worktrees; the stub leaves them nil so runServe knows to
// skip the Reaper.
type spawnerSet struct {
	Spawner   spawner.Spawner
	Killer    reaper.ChildKiller
	Worktrees *spawner.WorktreeManager
}

// buildSpawner returns the spawnerSet selected by the -spawner flag.
//
// The logger parameter is consumed only by the stub branch: the stub
// emits spawn.* structured events through it (spec §5.3). ClaudeSpawner
// currently has no slog callsites — its observability lands when real
// stdout/stderr-stream capture ships (#27, #45), at which point the
// logger will thread through ClaudeSpawnerConfig the same way.
func buildSpawner(name, repoRoot, claudeBin, baseRef string, logger *slog.Logger, db *state.DB, costKey []byte, costKeyID string) (spawnerSet, error) {
	switch name {
	case "", spawnerNameStub:
		return spawnerSet{Spawner: spawner.New(spawner.Config{Logger: logger})}, nil
	case spawnerNameClaude:
		wm, err := spawner.NewWorktreeManager(spawner.WorktreeManagerConfig{RepoRoot: repoRoot})
		if err != nil {
			return spawnerSet{}, fmt.Errorf("worktree manager: %w", err)
		}
		cfg := spawner.ClaudeSpawnerConfig{
			Command: claudeBin,
			BaseRef: baseRef,
			Args:    defaultClaudeArgs(),
		}
		if db != nil && len(costKey) > 0 {
			cfg.OnResultEventFor = spend.SpawnerCallback(db.SQL(),
				spend.WriteOptions{Key: costKey, KeyID: costKeyID},
				spend.CallScope{WrittenBy: "claude-spawner"})
		}
		cs, err := spawner.NewClaudeSpawner(wm, cfg)
		if err != nil {
			return spawnerSet{}, fmt.Errorf("claude spawner: %w", err)
		}
		return spawnerSet{Spawner: cs, Killer: cs, Worktrees: wm}, nil
	default:
		return spawnerSet{}, fmt.Errorf("unknown spawner %q (want stub|claude)", name)
	}
}
