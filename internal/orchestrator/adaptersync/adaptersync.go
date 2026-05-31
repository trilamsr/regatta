// Package adaptersync mirrors the read-only SpecAdapter into the
// state.work_items universal-queue table. Runs as step 3 of
// orchestrator.PollOnce (per spec §2.9). Tombstones rows the
// adapter no longer returns (source='adapter', last_seen_at <
// pollStartedAt) — see cascadeChildrenOfArchivedPrograms for the
// program -> child fan-out.
package adaptersync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// SpecAdapter mirrors the orchestrator's existing read surface.
// Keep the interface local so adaptersync doesn't import the
// orchestrator package (would create a cycle once Wave 5 wires
// PollOnce -> adaptersync.Sync).
type SpecAdapter interface {
	List(ctx context.Context) ([]schemas.WorkItem, error)
}

// Syncer pairs an adapter with the state DB. Timestamps come from the
// caller's poll-start tick (passed to Sync), not a clock held by the
// Syncer or installed on *state.DB — concurrent producers cannot race
// on the DB's clock that way.
type Syncer struct {
	adapter SpecAdapter
	db      *state.DB
}

// New constructs a Syncer. The clock used to live on db as a mutable
// field; that pattern stranded two Syncers sharing one DB into a
// race on the shared mutator. Production now threads the poll-start
// tick directly into Sync, which calls UpsertWorkItem +
// TombstoneBySource with the explicit timestamp.
func New(adapter SpecAdapter, db *state.DB) *Syncer {
	return &Syncer{adapter: adapter, db: db}
}

// Sync calls adapter.List, upserts each returned item with
// source=adapter (last_seen_at stamped at pollStartedAt), then
// tombstones adapter-source rows whose last_seen_at < pollStartedAt.
// After tombstoning, reconciles orphaned children of any archived
// program — idempotent, so a prior tick's mid-cascade crash converges
// on the next call.
//
// Empty-list contract: if adapter.List returns 0 items, Sync skips the
// tombstone sweep (a transient upstream hiccup must not wipe the
// queue). The cascade reconciler still runs; operator-driven purge is
// out of band.
//
// Unmappable enum values are skipped with slog.Warn rather than failing
// the whole tick — one bad row must not stop a poll.
//
// per spec §3 fail-fast: any DB error returns immediately, leaving the
// next tick to retry. Per-item upsert is its own tx; a mid-loop failure
// produces partial state which the next tick re-stamps. Single-tx-per-
// Sync (rollback of every upsert on first error) is deferred — would
// need a tx-scoping helper in state package.
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

// cascadeChildrenOfArchivedPrograms converges any archived program
// whose children are still live. Runs every tick — idempotent. Catches
// the failure mode where a prior tick archived the parent (via the
// tombstone RETURNING) but crashed before fanning out, leaving the
// children stranded outside the RETURNING set of subsequent ticks.
//
// Emits one child.cascade_archived per newly-archived child (rubric §6:
// operators grep by child id, not parent id). Threads at through to
// CascadeArchiveChildren so updated_at lines up with the poll-start
// tick rather than wall-clock drift.
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

// mapAdapterKind translates the adapter-surface Kind (schemas.Kind*)
// into the universal-queue Kind (state.Kind*). Returns ok=false on any
// value the queue does not recognize so Sync can skip + warn instead of
// silently casting garbage into the DB.
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

// mapAdapterStatus translates the adapter-surface Status (planned,
// in_progress, done) into the universal-queue Status (planned, running,
// pr_open, merged, archived, blocked). Only `planned` has a direct
// match; in_progress/done belong to the agent state-machine — the
// adapter is read-only and never writes those. Returns ok=false for
// anything else.
func mapAdapterStatus(s schemas.Status) (state.WorkItemStatus, bool) {
	switch s {
	case schemas.StatusPlanned:
		return state.WorkStatusPlanned, true
	default:
		return "", false
	}
}
