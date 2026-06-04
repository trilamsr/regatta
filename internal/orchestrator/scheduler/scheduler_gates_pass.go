package scheduler

import (
	"context"
	"fmt"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/merge"
)

// OnGatesPass is the gates_pass → auto-merge seam (#612). Caller fires once
// an agent's required gates pass against headSHA; PrepareMerge atomically
// writes intent + transitions to AwaitingMerge in one tx, then the request
// goes to the Worker. Intent-then-enqueue order is load-bearing — a crash
// between leaves AwaitingMerge with a probable intent that Reconcile drives
// home; reverse order would leak un-audited enqueues. No-op when auto-merge
// is wired off (Coordinator/Worker nil); Enqueue drop-on-full is the
// caller-side guarantee, with Reconcile as the long-tail safety net.
func (s *Scheduler) OnGatesPass(ctx context.Context, agentID int64, prNumber int, headSHA string) error {
	if s.cfg.MergeCoordinator == nil || s.cfg.MergeWorker == nil {
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
