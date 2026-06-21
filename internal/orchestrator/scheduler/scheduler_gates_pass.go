package scheduler

import (
	"context"
	"fmt"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/merge"
)

// LowRiskGate decides whether a gates-passed PR may auto-merge (MAY-86).
// Eligible reports false + a stable reason token to HOLD a PR for an
// operator glance; the load-bearing veto is the gate's first branch, so
// the interface stays minimal — the scheduler asks one question per PR.
type LowRiskGate interface {
	Eligible(ctx context.Context, prNumber int, headSHA string) (bool, string)
}

// OnGatesPass is the gates_pass → auto-merge seam (#612). Caller fires once
// an agent's required gates pass against headSHA; PrepareMerge atomically
// writes intent + transitions to AwaitingMerge in one tx, then the request
// goes to the Worker. Intent-then-enqueue order is load-bearing — a crash
// between leaves AwaitingMerge with a probable intent that Reconcile drives
// home; reverse order would leak un-audited enqueues. No-op when auto-merge
// is wired off (Coordinator/Worker nil); Enqueue drop-on-full is the
// caller-side guarantee, with Reconcile as the long-tail safety net.
//
// The low-risk filter (MAY-86) runs BEFORE PrepareMerge: when a
// LowRiskGate is wired and HOLDS the PR, OnGatesPass writes no intent
// and the agent stays in GatesRunning for an operator glance. A nil gate
// keeps the pre-MAY-86 path byte-equivalent (every PR proceeds).
func (s *Scheduler) OnGatesPass(ctx context.Context, agentID int64, prNumber int, headSHA string) error {
	if s.cfg.MergeCoordinator == nil || s.cfg.MergeWorker == nil {
		return nil
	}
	if s.cfg.LowRiskGate != nil {
		if eligible, reason := s.cfg.LowRiskGate.Eligible(ctx, prNumber, headSHA); !eligible {
			s.log.Info("scheduler.gates_pass_held",
				string(obs.KeyAgentID), agentID,
				"pr_number", prNumber,
				"reason", reason,
			)
			return nil
		}
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
