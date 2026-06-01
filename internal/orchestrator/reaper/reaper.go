// Package reaper tears down worktrees and child processes for
// agents that have reached a terminal state.
//
// Per docs/design.md §Reaper, terminal transitions trigger:
//
//   1. Send SIGTERM to the agent's child process (if known)
//   2. Remove the worktree via WorktreeManager
//   3. Release any locks the agent still holds
//   4. Kick the scheduler so its next tick promotes a pending agent
//
// The skeleton implements 1-3. Step 4 lands once the orchestrator
// exposes a tick-now signal; the existing periodic ticker absorbs
// the latency until then.
package reaper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// ChildKiller is implemented by spawners that own live processes.
// The Reaper consults this to send SIGTERM before removing the
// worktree (a forced rm of a worktree currently being written to
// races against the child's filesystem activity).
type ChildKiller interface {
	// KillAgent best-effort signals the agent's child process and
	// returns whether a process was actually signaled. The
	// implementation MUST be safe to call when the agent is unknown
	// (returns false, nil).
	KillAgent(agentID int64) (signaled bool, err error)
}

// Config wires the Reaper's collaborators and structured-event sink.
// All fields except Logger are required; Logger defaults to
// slog.Default() so embedded callers still get output (spec §4.1).
type Config struct {
	DB     *state.DB
	WM     *spawner.WorktreeManager
	Killer ChildKiller
	Logger *slog.Logger

	// Tracer is the OTel tracer this component uses to open spans.
	// Nil falls back to otel.Tracer("reaper") which resolves to the
	// global provider — noop until obs/otel.Setup runs. Per W6 spec
	// §3.3 + feedback_spec_pattern_authority.
	Tracer trace.Tracer
}

// Reaper owns the post-terminal cleanup path.
type Reaper struct {
	db       *state.DB
	wm       *spawner.WorktreeManager
	killer   ChildKiller
	log      *slog.Logger
	tracer   trace.Tracer
	terminal []state.AgentState
}

// New constructs a Reaper from Config. cfg.Killer may be nil if the
// spawner has no live processes to signal (e.g. the stub). cfg.Logger
// nil defaults to slog.Default().
func New(cfg Config) *Reaper {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("reaper")
	}
	return &Reaper{
		db:     cfg.DB,
		wm:     cfg.WM,
		killer: cfg.Killer,
		log:    log,
		tracer: tracer,
		terminal: []state.AgentState{
			state.AgentDone,
			state.AgentWithdrawn,
			state.AgentEscalated,
		},
	}
}

// Reap performs idempotent cleanup for a single agent: signal the
// child, remove the worktree, release any leftover locks. The agent
// row itself is left in place so the audit trail survives.
//
// Reap refuses to operate on an agent that is not currently in a
// terminal state (done | withdrawn | escalated): killing a live
// agent's child and removing its worktree mid-run would corrupt
// the agent's PR and leave the state machine inconsistent.
//
// Returns nil if the agent has no leftover state (the common case
// for already-reaped agents).
func (r *Reaper) Reap(ctx context.Context, agentID int64) error {
	detectedAt := time.Now()
	agent, err := r.db.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("reaper: load agent %d: %w", agentID, err)
	}
	if !r.isTerminal(agent.State) {
		r.log.Info(string(obs.EventReapSkipped),
			string(obs.KeyAgentID), agentID,
			string(obs.KeyReason), "not_terminal",
		)
		return fmt.Errorf("%w: agent %d is in %s", ErrAgentNotTerminal, agentID, agent.State)
	}
	r.log.Info(string(obs.EventReapCandidateDetected),
		string(obs.KeyAgentID), agentID,
		string(obs.KeyWorkItemID), agent.WorkItemID,
	)
	if r.killer != nil {
		signaled, err := r.killer.KillAgent(agentID)
		if err != nil {
			return fmt.Errorf("reaper: kill agent %d: %w", agentID, err)
		}
		if signaled {
			r.log.Info(string(obs.EventReapKilled),
				string(obs.KeyAgentID), agentID,
				string(obs.KeyDurationMs), time.Since(detectedAt).Milliseconds(),
			)
		}
	}
	if r.wm != nil {
		if err := r.wm.Remove(ctx, agentID); err != nil {
			return fmt.Errorf("reaper: remove worktree for agent %d: %w", agentID, err)
		}
	}
	if _, err := r.db.ReleaseAgentLocks(ctx, agentID); err != nil {
		return fmt.Errorf("reaper: release locks for agent %d: %w", agentID, err)
	}
	_ = r.db.RecordEvent(ctx, agentID, "reaped", "{}")
	return nil
}

// ReapAll sweeps every agent currently in a terminal state and
// invokes Reap on each. The orchestrator's Run loop calls ReapAll on
// a timer so a missed terminal-edge hook (e.g. external state mutation
// via sql) is eventually cleaned up.
func (r *Reaper) ReapAll(ctx context.Context) error {
	// W6 spec §3.5: `reaper.sweep` is a standalone span (not nested
	// under tick) — the reaper runs on its own ticker cadence.
	ctx, span := r.tracer.Start(ctx, "reaper.sweep")
	defer span.End()
	agents, err := r.db.ListAgentsByState(ctx, r.terminal...)
	if err != nil {
		return fmt.Errorf("reaper: list terminal agents: %w", err)
	}
	var firstErr error
	for _, a := range agents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.Reap(ctx, a.ID); err != nil {
			r.log.Warn(string(obs.EventReapSkipped),
				string(obs.KeyAgentID), a.ID,
				string(obs.KeyReason), "reap_failed",
				string(obs.KeyErr), err.Error(),
			)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// ErrAgentNotTerminal is returned by Reap when called on an agent
// whose state is not one of done | withdrawn | escalated.
var ErrAgentNotTerminal = errors.New("reaper: agent is not in a terminal state")

// ErrNoKiller is returned by stub ChildKillers that cannot signal a
// process. Callers may ignore it; Reaper treats it as a no-op.
var ErrNoKiller = errors.New("reaper: no child-killer configured")

func (r *Reaper) isTerminal(s state.AgentState) bool {
	for _, t := range r.terminal {
		if t == s {
			return true
		}
	}
	return false
}
