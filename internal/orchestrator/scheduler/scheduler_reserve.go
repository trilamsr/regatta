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
//
// Gate re-checks fire per orphan because the spawnable-path gates
// (#251/#698) ran on the pre-materialise wi set: a wi flipped to gated /
// cost-denied / l4-blocked between then and now must NOT spawn (#703).
func (s *Scheduler) reserveOrphans(ctx context.Context, tc *tickCtx, occupancy map[string]int, attempted map[int64]struct{}) ([]int64, error) {
	pending, err := s.db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list pending: %w", err)
	}
	if s.costCapDeniesOrphans(ctx, len(pending)) {
		return nil, nil
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
		if skip, err := s.recheckGates(ctx, tc, a.WorkItemID); err != nil {
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

// costCapDeniesOrphans pauses the orphan pass when the global cost-cap
// is saturated (#703 R1). Matches applyCostCap's behaviour on the
// spawnable pass so a flipped cap halts BOTH paths symmetrically.
func (s *Scheduler) costCapDeniesOrphans(ctx context.Context, n int) bool {
	if s.cfg.CostCap == nil {
		return false
	}
	if s.cfg.CostCap.Allow(ctx) {
		return false
	}
	s.log.Info("scheduler.cost_cap_throttled_orphans", "throttled_pending", n)
	return true
}

// recheckGates fans out the approval / cost-governor / L4 gate
// re-checks for one orphan (#703 R1). skip=true drops the orphan from
// this tick; err halts the tick (only the approval-gate reject
// CAS path returns one — paused/denied/blocked all skip and log).
// Gate or GateResolver nil → that sub-check is a no-op.
func (s *Scheduler) recheckGates(ctx context.Context, tc *tickCtx, workItemID string) (skip bool, err error) {
	wi, fetched, ok := s.fetchWorkItemForRecheck(ctx, workItemID)
	if !ok {
		return false, nil
	}
	if !fetched {
		// fetch failure already warned; fail-closed to keep the orphan
		// pending until next tick rather than spawn blind.
		return true, nil
	}
	if skip, err := s.recheckApproval(ctx, tc, wi); err != nil || skip {
		return skip, err
	}
	if s.recheckCost(ctx, wi) {
		return true, nil
	}
	if s.recheckL4(ctx, wi) {
		return true, nil
	}
	return false, nil
}

// fetchWorkItemForRecheck centralises the workItemGetter upcast +
// fetch. ok=false means no gate is wired so the caller short-circuits;
// fetched=false means the upcast succeeded but the row read failed and
// the orphan must pause (fail-closed).
func (s *Scheduler) fetchWorkItemForRecheck(ctx context.Context, workItemID string) (wi state.WorkItem, fetched, ok bool) {
	gatesWired := (s.cfg.Gate != nil && s.cfg.GateResolver != nil) ||
		(s.cfg.CostGate != nil && s.cfg.CostGateResolver != nil) ||
		(s.cfg.L4Gate != nil && s.cfg.L4GateResolver != nil)
	if !gatesWired {
		return state.WorkItem{}, false, false
	}
	getter, isGetter := s.db.(workItemGetter)
	if !isGetter {
		// Production schedulerDB always exposes GetWorkItem; a wrapper
		// that strips it silently disables the re-check (#703 R2, #776).
		// Warn so the operator sees the misconfiguration, then fail-closed
		// (ok=true, fetched=false) so the caller skips the orphan rather
		// than spawning it blind through a half-wired gate stack.
		s.log.Warn("scheduler.gate_recheck_unavailable",
			string(obs.KeyWorkItemID), workItemID,
			string(obs.KeyReason), "schedulerdb_missing_getworkitem",
		)
		return state.WorkItem{}, false, true
	}
	wi, err := getter.GetWorkItem(ctx, workItemID)
	if err != nil {
		s.log.Warn(string(obs.EventApprovalDecided),
			string(obs.KeyWorkItemID), workItemID,
			string(obs.KeyReason), "get_work_item_failed",
			string(obs.KeyErr), err.Error(),
		)
		return state.WorkItem{}, false, true
	}
	return wi, true, true
}

// recheckApproval mirrors applyApprovalGates for one wi. Reject runs
// the same CAS markWorkItemRejected; CAS failure is the only halt path.
func (s *Scheduler) recheckApproval(ctx context.Context, tc *tickCtx, wi state.WorkItem) (skip bool, err error) {
	if s.cfg.Gate == nil || s.cfg.GateResolver == nil {
		return false, nil
	}
	cfg, gated := s.cfg.GateResolver(wi)
	if !gated {
		return false, nil
	}
	res, evalErr := s.cfg.Gate.Evaluate(ctx, wi, cfg)
	if evalErr != nil {
		s.log.Warn(string(obs.EventApprovalDecided),
			string(obs.KeyWorkItemID), wi.ID,
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

// recheckCost mirrors applyCostGovernor for one wi. Evaluate errors and
// Allow=false both fail-closed (skip=true) — same semantics as the
// spawnable-pass.
func (s *Scheduler) recheckCost(ctx context.Context, wi state.WorkItem) (skip bool) {
	if s.cfg.CostGate == nil || s.cfg.CostGateResolver == nil {
		return false
	}
	scope, gated := s.cfg.CostGateResolver(wi)
	if !gated {
		return false
	}
	v, err := s.cfg.CostGate.Evaluate(ctx, scope)
	if err != nil {
		s.log.Warn("scheduler.cost_gate_error",
			string(obs.KeyWorkItemID), wi.ID,
			string(obs.KeyErr), err.Error(),
		)
		return true
	}
	if !v.Allow {
		s.log.Info("scheduler.cost_gate_denied",
			string(obs.KeyWorkItemID), wi.ID,
			string(obs.KeyReason), v.Reason,
		)
		return true
	}
	return false
}

// recheckL4 mirrors applyL4Gate for one wi. Blocking verdicts and
// Evaluate errors both fail-closed.
func (s *Scheduler) recheckL4(ctx context.Context, wi state.WorkItem) (skip bool) {
	if s.cfg.L4Gate == nil || s.cfg.L4GateResolver == nil {
		return false
	}
	sc, gated := s.cfg.L4GateResolver(wi)
	if !gated {
		return false
	}
	gr, err := s.cfg.L4Gate.Evaluate(ctx, sc.Cfg, sc.In)
	if err != nil {
		s.log.Warn("scheduler.l4_gate_error",
			string(obs.KeyWorkItemID), wi.ID,
			string(obs.KeyGateID), sc.Cfg.GateID,
			string(obs.KeyErr), err.Error(),
		)
		return true
	}
	if !gr.Blocking {
		return false
	}
	reason := string(gr.Verdict)
	if len(gr.Findings) > 0 {
		reason = gr.Findings[0].ID
	}
	s.log.Info("scheduler.l4_gate_blocked",
		string(obs.KeyWorkItemID), wi.ID,
		string(obs.KeyGateID), sc.Cfg.GateID,
		string(obs.KeyVerdict), string(gr.Verdict),
		string(obs.KeyReason), reason,
	)
	s.emitGateRejected(ctx, wi.ID, sc.In.PRSHA, reason)
	return true
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
