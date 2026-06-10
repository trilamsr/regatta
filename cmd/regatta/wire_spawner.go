package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

const (
	spawnerNameStub   = "stub"
	spawnerNameClaude = "claude"

	// envSpawnerMCPConfig overrides the default empty-MCP tempfile passed to spawned claude children. Empty = spawner mints a tempfile containing `{"mcpServers":{}}` (claude CLI rejects /dev/null at boot — #1116).
	envSpawnerMCPConfig = "REGATTA_SPAWNER_MCP_CONFIG"

	// mcpEmptyConfigJSON is the minimal claude-CLI-accepted MCP config: a JSON document with an empty mcpServers map. Pinned literal so a future edit cannot silently drop the parseability that #1116 traces to.
	mcpEmptyConfigJSON = `{"mcpServers":{}}`
)

// claudeFlagStreamJSON pins the stream-json output flag (shared by impl + tests).
const claudeFlagStreamJSON = "--output-format=stream-json"

// defaultClaudeArgs are the headless flags the orchestrator stamps onto every claude CLI spawn. #1085 closes the TUI-mode silence (--print + stream-json + verbose); #1116 closes the boot-time MCP rejection — when REGATTA_SPAWNER_MCP_CONFIG is unset, the spawner mints a tempfile containing `{"mcpServers":{}}` and returns a cleanup func that removes it on serve shutdown. When the env var is set, the operator owns the path; cleanup is a no-op.
func defaultClaudeArgs() ([]string, func(), error) {
	mcp := os.Getenv(envSpawnerMCPConfig)
	cleanup := func() {}
	if mcp == "" {
		f, err := os.CreateTemp("", "regatta-mcp-*.json")
		if err != nil {
			return nil, cleanup, fmt.Errorf("mcp config tempfile: %w", err)
		}
		if _, err := f.WriteString(mcpEmptyConfigJSON); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return nil, cleanup, fmt.Errorf("write mcp config tempfile: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(f.Name())
			return nil, cleanup, fmt.Errorf("close mcp config tempfile: %w", err)
		}
		mcp = f.Name()
		owned := mcp
		cleanup = func() { _ = os.Remove(owned) } //nolint:gosec // owned is os.CreateTemp() name, not user input.
	}
	return []string{"--print", claudeFlagStreamJSON, "--verbose", "--mcp-config=" + mcp}, cleanup, nil
}

// spawnerSet bundles the three handles a serve invocation needs to
// wire the Spawner + Reaper. Only the claude backend populates
// Killer + Worktrees; the stub leaves them nil so runServe knows to
// skip the Reaper. Cleanup releases any spawner-owned resources
// (currently the #1116 MCP-config tempfile) and is always safe to
// call — nil-safe and no-op when there is nothing to release.
type spawnerSet struct {
	Spawner   spawner.Spawner
	Killer    reaper.ChildKiller
	Worktrees *spawner.WorktreeManager
	Cleanup   func()
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
		return spawnerSet{Spawner: spawner.New(spawner.Config{Logger: logger}), Cleanup: func() {}}, nil
	case spawnerNameClaude:
		wm, err := spawner.NewWorktreeManager(spawner.WorktreeManagerConfig{RepoRoot: repoRoot})
		if err != nil {
			return spawnerSet{}, fmt.Errorf("worktree manager: %w", err)
		}
		args, cleanup, err := defaultClaudeArgs()
		if err != nil {
			return spawnerSet{}, fmt.Errorf("claude args: %w", err)
		}
		cfg := spawner.ClaudeSpawnerConfig{
			Command: claudeBin,
			BaseRef: baseRef,
			Args:    args,
			Logger:  logger,
		}
		if db != nil {
			cfg.OnAgentExited = newAgentExitedCascade(db, logger)
		}
		if db != nil && len(costKey) > 0 {
			cfg.OnResultEventFor = spend.SpawnerCallback(db.SQL(),
				spend.WriteOptions{Key: costKey, KeyID: costKeyID},
				spend.CallScope{WrittenBy: "claude-spawner"})
		}
		cs, err := spawner.NewClaudeSpawner(wm, cfg)
		if err != nil {
			cleanup()
			return spawnerSet{}, fmt.Errorf("claude spawner: %w", err)
		}
		return spawnerSet{Spawner: cs, Killer: cs, Worktrees: wm, Cleanup: cleanup}, nil
	default:
		return spawnerSet{}, fmt.Errorf("unknown spawner %q (want stub|claude)", name)
	}
}

// newAgentExitedCascade returns the OnAgentExited callback the claude
// spawner fires after cmd.Wait observes child termination. The callback
// drives the running→crashed FSM transition + records the agent.exited
// audit row so the agents table never leaks phantoms with dead PIDs
// (#1218 stall root cause). Best-effort: state-load failures and
// FSM-rejected transitions (already terminal) are logged at WARN; the
// reaper sweeps the surviving cases via PID-liveness checks.
func newAgentExitedCascade(db *state.DB, logger *slog.Logger) spawner.AgentExitedCallback {
	return func(agentID int64, workItemID string, exitCode int, reason spawner.ExitReason, durationMs int64) {
		ctx := context.Background()
		a, err := db.GetAgent(ctx, agentID)
		if err != nil {
			logger.Warn("orchestrator.agent_exited_load_failed",
				string(obs.KeyAgentID), agentID,
				string(obs.KeyWorkItemID), workItemID,
				string(obs.KeyErr), err.Error())
			return
		}
		// Skip the FSM call when the agent already left an active
		// state (race with reaper or rejection-router). The slog line
		// already fired upstream so observability stays intact.
		if isTerminalAgentState(a.State) {
			return
		}
		if _, err := db.TransitionAgent(ctx, agentID, state.AgentCrashed, state.AgentMutation{}); err != nil {
			if errors.Is(err, state.ErrInvalidTransition) {
				return
			}
			logger.Warn("orchestrator.agent_exited_transition_failed",
				string(obs.KeyAgentID), agentID,
				string(obs.KeyWorkItemID), workItemID,
				string(obs.KeyErr), err.Error())
			return
		}
		payload := fmt.Sprintf(`{"exit_code":%d,"exit_reason":%q,"duration_ms":%d}`,
			exitCode, string(reason), durationMs)
		if err := db.RecordEvent(ctx, agentID, string(obs.EventAgentExited), payload); err != nil {
			logger.Warn("orchestrator.agent_exited_record_failed",
				string(obs.KeyAgentID), agentID,
				string(obs.KeyErr), err.Error())
		}
	}
}

func isTerminalAgentState(s state.AgentState) bool {
	switch s {
	case state.AgentDone, state.AgentWithdrawn, state.AgentCrashed, state.AgentEscalated:
		return true
	}
	return false
}
