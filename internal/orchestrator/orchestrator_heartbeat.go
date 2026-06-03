package orchestrator

import (
	"context"
	"fmt"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Heartbeat refreshes every lock owned by an active agent. The
// orchestrator runs Heartbeat on cfg.HeartbeatInterval so that locks
// only age out when an agent has truly crashed.
//
// AwaitingMerge is included so an agent stalled on a long external
// merge call (gh API slowness, branch-protection wait) does not lose
// its hotspot lock to the stale-lock sweep mid-merge. PHASE AUTONOMY
// §11 W2 c0 made this load-bearing — pre-c0 AwaitingMerge was excluded
// here and would silently leak locks across long merges.
func (o *Orchestrator) Heartbeat(ctx context.Context) error {
	active, err := o.db.ListAgentsByState(ctx,
		state.AgentSpawning, state.AgentRunning, state.AgentPROpen,
		state.AgentGatesRunning, state.AgentAwaitingMerge)
	if err != nil {
		return err
	}
	for _, a := range active {
		if _, err := o.db.HeartbeatLock(ctx, a.ID); err != nil {
			return fmt.Errorf("orchestrator: heartbeat agent %d: %w", a.ID, err)
		}
	}
	return nil
}
