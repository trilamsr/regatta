package spawner

import (
	"context"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// ErrOrphanJournal — journal entry whose referenced work_item row is
// gone (path-(b) "surface to audit" from #99). Unreachable under the
// current FK; sentinel survives so a future migration relaxing the FK
// has a typed signal callers can errors.Is.
var ErrOrphanJournal = errors.New("spawner: outputs journal present but work_item not merged")

// ReconcileOrphans replays the missing merge transition for every
// work_item that has a journal row but never reached status=merged.
// Idempotent: re-running on a fully-consistent DB returns 0, nil.
//
// Path-(a) recovery from #99: journal supplies work_item_id, we re-
// load and re-issue the UpsertWorkItem Complete would have made. We
// do NOT replay AppendOutput — the journal row exists; a second write
// would allocate a fresh attempt_no and the scheduler would route
// against the duplicate.
//
// Per-row failures log spawn.reconcile_failed and continue — a single
// corrupt row must not block the queue. First ctx cancel aborts.
func (s *Stub) ReconcileOrphans(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("spawner: reconcile requires DB; Config.DB was nil")
	}
	ids, err := s.db.ListWorkItemsWithJournalNotMerged(ctx)
	if err != nil {
		return 0, fmt.Errorf("spawner: list orphans: %w", err)
	}

	var reconciled int
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return reconciled, err
		}
		if err := s.reconcileOne(ctx, id); err != nil {
			s.log.Warn(string(obs.EventSpawnReconcileFailed),
				string(obs.KeyWorkItemID), id,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		reconciled++
		s.log.Info(string(obs.EventSpawnReconciled),
			string(obs.KeyWorkItemID), id,
		)
	}
	return reconciled, nil
}

// reconcileOne re-issues the merge transition Complete would have
// committed had it not crashed. Uses the injected clock so seam tests
// pin updated_at deterministically (#100). Wraps ErrOrphanJournal
// when the journal points at a vanished work_item — operators triage.
func (s *Stub) reconcileOne(ctx context.Context, id string) error {
	wi, err := s.db.GetWorkItem(ctx, id)
	if err != nil {
		if errors.Is(err, state.ErrWorkItemNotFound) {
			return fmt.Errorf("%w: id=%s", ErrOrphanJournal, id)
		}
		return fmt.Errorf("load %s: %w", id, err)
	}
	// ListWorkItemsWithJournalNotMerged filtered by status, but a
	// concurrent Complete could land between LIST and now — noop
	// rather than re-flipping a row another writer just merged.
	if wi.Status == state.WorkStatusMerged {
		return nil
	}
	wi.Status = state.WorkStatusMerged
	if err := s.db.UpsertWorkItem(ctx, wi, wi.Source, s.clock()); err != nil {
		return fmt.Errorf("merge %s: %w", id, err)
	}
	return nil
}
