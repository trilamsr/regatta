package scheduler

import (
	"context"
	"fmt"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/merge"
)

// OnGatesPass is the gates_pass → auto-merge seam (#612). Caller fires
// this once an agent in GatesRunning has all required gates passing
// against the given PR head SHA; the hook atomically writes the merge
// intent + transitions to AwaitingMerge (via Coordinator.PrepareMerge)
// and offers the merge request to the Worker.
//
// No-op when auto-merge is wired off (Coordinator or Worker nil) so
// the c2 default --auto-merge=false stays operator-observable-
// equivalent to pre-c2. Enqueue is non-blocking; a full queue logs +
// drops rather than stall the caller — the Reconcile sweep is the
// long-tail safety net (spec §6.1).
//
// Intent-then-enqueue ordering matters: PrepareMerge persists the
// recoverable audit row inside the same tx as the FSM transition, so
// a crash between PrepareMerge and Enqueue leaves the agent in
// AwaitingMerge with a probable intent and Reconcile drives it home.
// The reverse order would leak un-audited enqueues.
func (s *Scheduler) OnGatesPass(ctx context.Context, agentID int64, prNumber int, headSHA string) error {
	if s.cfg.MergeCoordinator == nil || s.cfg.MergeWorker == nil {
		// Auto-merge disabled — c2 default. No-op so the daemon's
		// behavior matches the pre-c2 manual-merge path exactly.
		return nil
	}
	if err := s.cfg.MergeCoordinator.PrepareMerge(ctx, agentID, prNumber, headSHA); err != nil {
		return fmt.Errorf("scheduler: gates_pass prepare merge: %w", err)
	}
	if !s.cfg.MergeWorker.Enqueue(merge.MergeRequest{
		AgentID:  agentID,
		PRNumber: prNumber,
		HeadSHA:  headSHA,
	}) {
		s.log.Warn("scheduler.gates_pass_enqueue_dropped",
			string(obs.KeyAgentID), agentID,
			"pr_number", prNumber,
		)
	}
	s.log.Info("scheduler.gates_pass_enqueued",
		string(obs.KeyAgentID), agentID,
		"pr_number", prNumber,
		"head_sha", headSHA,
	)
	return nil
}
