// Package adaptersync mirrors the read-only SpecAdapter into
// state.work_items. Step 3 of orchestrator.PollOnce (spec §2.9).
// Tombstones rows the adapter no longer returns; see
// cascadeChildrenOfArchivedPrograms for parent→child fan-out.
package adaptersync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// SpecAdapter mirrors the orchestrator's read surface. Declared
// locally so adaptersync does not import orchestrator (import cycle).
type SpecAdapter interface {
	List(ctx context.Context) ([]schemas.WorkItem, error)
}

// Syncer pairs an adapter with the state DB. Timestamps are passed
// to Sync rather than held here, so concurrent producers sharing a
// DB cannot race on a shared clock.
type Syncer struct {
	adapter SpecAdapter
	db      *state.DB
}

// New constructs a Syncer.
func New(adapter SpecAdapter, db *state.DB) *Syncer {
	return &Syncer{adapter: adapter, db: db}
}

// Sync upserts adapter items, tombstones rows the adapter no longer
// returns, then reconciles orphaned children. Idempotent on partial
// failure: a mid-cascade crash converges on the next tick.
//
// Empty adapter.List skips the tombstone sweep (transient upstream
// hiccups must not wipe the queue). Unmappable items are slog.Warn'd
// and skipped — one bad row must not stop a poll. Per spec §3 any DB
// error returns immediately; per-item upserts are individual txs.
func (s *Syncer) Sync(ctx context.Context, pollStartedAt time.Time) error {
	items, err := s.adapter.List(ctx)
	if err != nil {
		return fmt.Errorf("adaptersync: adapter list: %w", err)
	}

	if len(items) == 0 {
		slog.Warn("adapter.empty_list", "cutoff", pollStartedAt, "skipping_tombstone", true)
		return s.cascadeChildrenOfArchivedPrograms(ctx, pollStartedAt)
	}

	seen := map[string]bool{}
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		id := string(it.ID)
		if seen[id] {
			slog.Warn("adapter.duplicate_id", "id", id)
			continue
		}
		seen[id] = true

		kind, ok := mapAdapterKind(it.Kind)
		if !ok {
			slog.Warn("adapter.item_skipped", "id", id, "reason", "unknown_kind", "value", string(it.Kind))
			continue
		}
		status, ok := mapAdapterStatus(it.Status)
		if !ok {
			slog.Warn("adapter.item_skipped", "id", id, "reason", "unknown_status", "value", string(it.Status))
			continue
		}
		lane := string(it.Lane)
		if lane == "" {
			slog.Warn("adapter.item_skipped", "id", id, "reason", "empty_lane")
			continue
		}

		wi := state.WorkItem{
			ID:     id,
			Kind:   kind,
			Title:  it.Title,
			Lane:   lane,
			Status: status,
		}
		if err := s.db.UpsertWorkItem(ctx, wi, state.SourceAdapter, pollStartedAt); err != nil {
			return fmt.Errorf("adaptersync: upsert %s: %w", id, err)
		}
	}

	archived, err := s.db.TombstoneBySource(ctx, state.SourceAdapter, pollStartedAt)
	if err != nil {
		return fmt.Errorf("adaptersync: tombstone: %w", err)
	}
	for _, id := range archived {
		slog.Warn("adapter.tombstoned", "id", id, "cutoff", pollStartedAt)
	}
	return s.cascadeChildrenOfArchivedPrograms(ctx, pollStartedAt)
}

// cascadeChildrenOfArchivedPrograms archives live children of any
// already-archived program. Idempotent recovery for a tick that
// archived a parent but crashed before fanning out. Emits one
// child.cascade_archived per child so operators can grep by child id.
func (s *Syncer) cascadeChildrenOfArchivedPrograms(ctx context.Context, at time.Time) error {
	orphans, err := s.db.ListArchivedProgramsWithLiveChildren(ctx)
	if err != nil {
		return fmt.Errorf("adaptersync: list orphaned programs: %w", err)
	}
	for _, parentID := range orphans {
		if err := ctx.Err(); err != nil {
			return err
		}
		archived, err := s.db.CascadeArchiveChildren(ctx, parentID, at)
		if err != nil {
			return fmt.Errorf("adaptersync: cascade %s: %w", parentID, err)
		}
		for _, childID := range archived {
			slog.Warn("child.cascade_archived", "child", childID, "parent", parentID, "cutoff", at)
		}
	}
	return nil
}

// mapAdapterKind translates schemas.Kind* → state.Kind*. ok=false on
// any unrecognized value so Sync can skip + warn instead of casting
// garbage into the DB.
func mapAdapterKind(k schemas.WorkItemKind) (state.WorkItemKind, bool) {
	switch k {
	case schemas.KindFeature:
		return state.KindFeature, true
	case schemas.KindProgram:
		return state.KindProgram, true
	default:
		return "", false
	}
}

// mapAdapterStatus translates the read-only adapter Status to the
// universal-queue Status. Only `planned` maps; in_progress / done
// belong to the agent state-machine and never originate from adapters.
func mapAdapterStatus(s schemas.Status) (state.WorkItemStatus, bool) {
	switch s {
	case schemas.StatusPlanned:
		return state.WorkStatusPlanned, true
	default:
		return "", false
	}
}
