package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// workItemGetter is the local upcast used by reserveOrphans to re-fetch
// a wi for the orphan gate re-check (#167). Declared here rather than
// pinned onto schedulerDB so the gate-recheck stays a reservation-loop
// concern.
type workItemGetter interface {
	GetWorkItem(ctx context.Context, id string) (state.WorkItem, error)
}

// reserveOrphans rediscovers AgentPending rows that reserveFromSpawnable
// did not transition this tick — typically lane-capped items from a
// prior tick, or any future class of pending agents (e.g. crashed
// recovery requeue). Each gets a single-tx reservation (locks +
// transition); the agent row already exists so the tx omits upsert.
func (s *Scheduler) reserveOrphans(ctx context.Context, tc *tickCtx, occupancy map[string]int, attempted map[int64]struct{}) ([]int64, error) {
	pending, err := s.db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list pending: %w", err)
	}
	var reserved []int64
	for _, a := range pending {
		if _, seen := attempted[a.ID]; seen {
			// reserveFromSpawnable already tried this agent this tick;
			// skip the duplicate so logs and lock-acquire churn stay
			// one-per-agent-per-tick.
			continue
		}
		if !s.laneHasCapacity(a.Lane, occupancy) {
			continue
		}
		if skip, err := s.recheckApprovalGate(ctx, tc, a.WorkItemID); err != nil {
			return reserved, err
		} else if skip {
			continue
		}
		locks := s.resolveLocks(a.WorkItemID)
		err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
			if err := s.fireWriteHook(tc); err != nil {
				return err
			}
			if err := s.db.TryAcquireLocksTx(ctx, tx, locks, a.ID, s.cfg.LockTTL); err != nil {
				return err
			}
			if err := s.fireWriteHook(tc); err != nil {
				return err
			}
			_, err := s.db.TransitionAgentTx(ctx, tx, a.ID, state.AgentSpawning, state.AgentMutation{})
			return err
		})
		if err != nil {
			if errors.Is(err, state.ErrLockHeld) {
				s.log.Info("scheduler.agent_skipped_hotspot_locked",
					string(obs.KeyAgentID), a.ID,
					string(obs.KeyWorkItemID), a.WorkItemID,
					string(obs.KeyReason), "hotspot_locked",
				)
				continue
			}
			return reserved, fmt.Errorf("scheduler: reserve orphan agent %d: %w", a.ID, err)
		}
		occupancy[a.Lane]++
		reserved = append(reserved, a.ID)
	}
	return reserved, nil
}

// recheckApprovalGate guards the orphan reservation path against a wi
// that became gated AFTER its pending agent was materialised (#167).
// skip=true drops the orphan from this tick — pause, reject, fetch
// error, and evaluator error all fail-closed so a half-wired gate
// cannot leak a spawn. Gate or GateResolver nil → no-op.
func (s *Scheduler) recheckApprovalGate(ctx context.Context, tc *tickCtx, workItemID string) (skip bool, err error) {
	if s.cfg.Gate == nil || s.cfg.GateResolver == nil {
		return false, nil
	}
	getter, ok := s.db.(workItemGetter)
	if !ok {
		return false, nil
	}
	wi, err := getter.GetWorkItem(ctx, workItemID)
	if err != nil {
		s.log.Warn(string(obs.EventApprovalDecided),
			string(obs.KeyWorkItemID), workItemID,
			string(obs.KeyReason), "get_work_item_failed",
			string(obs.KeyErr), err.Error(),
		)
		return true, nil
	}
	cfg, gated := s.cfg.GateResolver(wi)
	if !gated {
		return false, nil
	}
	res, evalErr := s.cfg.Gate.Evaluate(ctx, wi, cfg)
	if evalErr != nil {
		s.log.Warn(string(obs.EventApprovalDecided),
			string(obs.KeyWorkItemID), workItemID,
			string(obs.KeyGateID), cfg.Name,
			string(obs.KeyVerdict), approval.ResultPause.String(),
			string(obs.KeyReason), "evaluate_error",
			string(obs.KeyErr), evalErr.Error(),
		)
		return true, nil
	}
	switch res {
	case approval.ResultProceed:
		return false, nil
	case approval.ResultReject:
		if mErr := s.markWorkItemRejected(ctx, tc, wi.ID); mErr != nil {
			return true, fmt.Errorf("mark %s rejected: %w", wi.ID, mErr)
		}
		s.log.Info(string(obs.EventApprovalDecided),
			string(obs.KeyWorkItemID), wi.ID,
			string(obs.KeyGateID), cfg.Name,
			string(obs.KeyVerdict), approval.ResultReject.String(),
		)
		return true, nil
	default:
		s.log.Info(string(obs.EventApprovalDecided),
			string(obs.KeyWorkItemID), wi.ID,
			string(obs.KeyGateID), cfg.Name,
			string(obs.KeyVerdict), approval.ResultPause.String(),
		)
		return true, nil
	}
}

func (s *Scheduler) resolveLocks(workItemID string) []string {
	if s.cfg.Hotspots == nil {
		return nil
	}
	names := s.cfg.Hotspots(workItemID)
	if len(names) == 0 {
		return nil
	}
	out := make([]string, len(names))
	copy(out, names)
	sort.Strings(out)
	return out
}
