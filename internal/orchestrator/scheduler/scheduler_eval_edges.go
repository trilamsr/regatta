package scheduler

import (
	"context"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// edge_fired_* sentinels mirror the work_item_edges.fired column's
// text-enum values; SQL filters in state.work_item_edges (sqlite
// indexed equality on 'pending') reference these verbatim.
const (
	edgeFiredTrue    = "true"
	edgeFiredFalse   = "false"
	edgeFiredPending = "pending"
)

// evalPendingEdges runs Tick step-0: for each (program, from_id) group
// of pending edges whose source is merged, fetch the latest journal
// entry and evaluate every outgoing edge. Writes back via
// MarkEdgeFired with the journal's content_sha so replay can reproduce
// routing (spec §3.8 + §3.9).
//
// Per-edge errors warn and mark fired='false' rather than halting the
// tick — a single bad predicate is a config bug operators see in
// logs. ErrJournalNotFound logs at info and leaves the edge pending so
// the next tick retries once the spawner catches up.
//
// Default-next fallback (spec §3.3 rule 2c): when every non-default
// outbound edge resolves to fired='false', the default_next edge flips
// to fired='true' with the same journal sha. BriefLoader validation
// (W2-A CheckReachability) already pinned the default's target, so the
// fallback never strands flow.
func (s *Scheduler) evalPendingEdges(ctx context.Context, tc *tickCtx) error {
	edges, err := s.db.ListPendingEdgesFromMerged(ctx)
	if err != nil {
		return fmt.Errorf("list pending edges: %w", err)
	}
	byFrom := map[string][]state.EdgeRow{}
	order := make([]string, 0)
	for _, e := range edges {
		if _, seen := byFrom[e.FromID]; !seen {
			order = append(order, e.FromID)
		}
		byFrom[e.FromID] = append(byFrom[e.FromID], e)
	}
	for _, fromID := range order {
		journal, ok, err := s.loadEdgeGroupJournal(ctx, fromID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		schema, _ := s.resolveSchema(fromID)
		if err := s.evalNonDefaultEdges(ctx, tc, byFrom[fromID], schema, journal); err != nil {
			return err
		}
		if err := s.applyDefaultFallback(ctx, tc, fromID, journal); err != nil {
			return err
		}
	}
	return nil
}

// loadEdgeGroupJournal fetches the latest journal entry for fromID.
// Returns ok=false (not an error) when the spawner has not caught up
// yet — the edge stays pending and the next tick retries.
func (s *Scheduler) loadEdgeGroupJournal(ctx context.Context, fromID string) (state.OutputJournalEntry, bool, error) {
	journal, err := s.db.GetLatestOutput(ctx, fromID)
	if err == nil {
		return journal, true, nil
	}
	if errors.Is(err, state.ErrJournalNotFound) {
		s.log.Info(string(obs.EventEdgeEvalSkippedNoJournal),
			string(obs.KeyFromID), fromID,
		)
		return state.OutputJournalEntry{}, false, nil
	}
	s.log.Warn(string(obs.EventEdgeJournalLoadFailed),
		string(obs.KeyFromID), fromID,
		string(obs.KeyErr), err.Error(),
	)
	return state.OutputJournalEntry{}, false, nil
}

// evalNonDefaultEdges evaluates every non-default edge in the group and
// writes MarkEdgeFired with journal.ContentSHA so replay reproduces
// routing (spec §3.9). Per-edge errors mark fired='false' rather than
// halting the tick — one bad predicate is a config bug operators see
// in logs. Returns non-nil only when the WriteHook aborts the tick.
func (s *Scheduler) evalNonDefaultEdges(ctx context.Context, tc *tickCtx, group []state.EdgeRow, schema any, journal state.OutputJournalEntry) error {
	for _, e := range group {
		if e.IsDefault {
			continue
		}
		fired, reason, evalErr := s.cfg.Evaluator.Eval(ctx, e, schema, journal)
		if evalErr != nil {
			s.log.Warn(string(obs.EventEdgeEvalError),
				string(obs.KeyEdgeID), e.ID,
				string(obs.KeyProgramID), e.ProgramID,
				string(obs.KeyFromID), e.FromID,
				string(obs.KeyToID), e.ToID,
				string(obs.KeyErr), evalErr.Error(),
			)
			fired = false
			reason = "eval-error"
		}
		firedStr := edgeFiredFalse
		if fired {
			firedStr = edgeFiredTrue
		}
		if err := s.fireWriteHook(tc); err != nil {
			return err
		}
		if err := s.db.MarkEdgeFired(ctx, e.ID, firedStr, journal.ContentSHA); err != nil {
			s.log.Warn(string(obs.EventEdgeMarkFailed),
				string(obs.KeyEdgeID), e.ID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		evt := obs.EventEdgeFired
		if !fired {
			evt = obs.EventEdgeSkipped
		}
		s.log.Info(string(evt),
			string(obs.KeyEdgeID), e.ID,
			string(obs.KeyProgramID), e.ProgramID,
			string(obs.KeyFromID), e.FromID,
			string(obs.KeyToID), e.ToID,
			"predicate", e.PredicateCEL,
			string(obs.KeyReason), reason,
			string(obs.KeyJournalSHA), journal.ContentSHA,
		)
	}
	return nil
}

// applyDefaultFallback implements spec §3.3 rule 2c: when every
// non-default outbound edge resolves fired='false', flip the default
// to fired='true' with the same journal sha. BriefLoader (W2-A
// CheckReachability) already pinned the default's target so the
// fallback never strands flow. Returns non-nil only when the WriteHook
// aborts the tick.
func (s *Scheduler) applyDefaultFallback(ctx context.Context, tc *tickCtx, fromID string, journal state.OutputJournalEntry) error {
	// Aggregate post-write sibling state in a single index scan
	// instead of re-reading the slice — equivalent semantics under
	// the single-writer flock, without the per-group slice alloc
	// that drove the +19% allocs/op regression in #187.
	agg, err := s.db.CountNonDefaultEdgeStates(ctx, fromID)
	if err != nil {
		s.log.Warn("edge.list_siblings_failed",
			string(obs.KeyFromID), fromID,
			string(obs.KeyErr), err.Error(),
		)
		return nil
	}
	if agg.DefaultCount > 1 {
		// Dedupe per (program_id, from_id) so a permanently
		// misconfigured brief does not spam every tick.
		key := agg.DefaultProgramID + "\x00" + fromID
		if _, loaded := s.multiDefaultLogged.LoadOrStore(key, struct{}{}); !loaded {
			s.log.Warn(string(obs.EventEdgeMultipleDefaultsPerFrom),
				string(obs.KeyProgramID), agg.DefaultProgramID,
				string(obs.KeyFromID), fromID,
				"count", agg.DefaultCount,
			)
		}
	}
	if agg.DefaultCount == 0 || agg.DefaultFired != edgeFiredPending {
		return nil
	}
	if agg.AnyNonDefaultTrue || agg.AnyNonDefaultPending || agg.NonDefaultCount == 0 {
		return nil
	}
	if err := s.fireWriteHook(tc); err != nil {
		return err
	}
	if err := s.db.MarkEdgeFired(ctx, agg.DefaultEdgeID, edgeFiredTrue, journal.ContentSHA); err != nil {
		s.log.Warn("edge.default_mark_failed",
			string(obs.KeyEdgeID), agg.DefaultEdgeID,
			string(obs.KeyErr), err.Error(),
		)
		return nil
	}
	s.log.Info(string(obs.EventEdgeDefaultFallback),
		string(obs.KeyEdgeID), agg.DefaultEdgeID,
		string(obs.KeyFromID), fromID,
		string(obs.KeyJournalSHA), journal.ContentSHA,
	)
	return nil
}

func (s *Scheduler) resolveSchema(featureID string) (any, bool) {
	if s.cfg.OutputsSchemas == nil {
		return nil, false
	}
	return s.cfg.OutputsSchemas(featureID)
}
