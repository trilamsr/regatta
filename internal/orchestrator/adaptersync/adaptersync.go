// Package adaptersync mirrors the read-only SpecAdapter into the
// state.work_items universal-queue table. Runs as step 3 of
// orchestrator.PollOnce (per spec §2.9). Tombstones rows the
// adapter no longer returns (source='adapter', last_seen_at <
// pollStartedAt) — see CascadeArchiveChildren for the program ->
// child fan-out.
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

// Syncer ties an adapter, the state DB, and a clock together. The
// clock is installed on db via SetClock at construction time so
// upserts within Sync stamp last_seen_at at pollStartedAt-aligned
// values; tests inject a fake clock the test advances between ticks.
type Syncer struct {
	adapter SpecAdapter
	db      *state.DB
	now     func() time.Time
}

// New constructs a Syncer. It installs `now` on db as the time
// source so UpsertWorkItem's d.now()-based last_seen_at stamps line
// up with the cutoff TombstoneBySource compares against. In
// production `now` is time.Now; tests inject a fake.
func New(adapter SpecAdapter, db *state.DB, now func() time.Time) *Syncer {
	db.SetClock(now)
	return &Syncer{adapter: adapter, db: db, now: now}
}

// Sync calls adapter.List, upserts every returned item with
// source=adapter (last_seen_at stamped from the shared clock), then
// tombstones adapter-source rows whose last_seen_at is older than
// pollStartedAt. Cascade-archives children of archived programs.
//
// per spec §3 fail-fast: any error returns immediately, leaving the
// next tick to retry.
func (s *Syncer) Sync(ctx context.Context, pollStartedAt time.Time) error {
	items, err := s.adapter.List(ctx)
	if err != nil {
		return fmt.Errorf("adaptersync: adapter list: %w", err)
	}

	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		// schemas.WorkItem.Kind, .Status, .Lane are defined string
		// types in contracts/schemas; state.WorkItemKind, etc. are
		// defined identically (string-underlying enums). Direct cast
		// is safe because the values "feature"/"program",
		// "planned"/"running"/..., and the lane names are
		// byte-equal across both packages. If a future contract
		// renames a value, fix the cast site by adding a switch.
		wi := state.WorkItem{
			ID:     string(it.ID),
			Kind:   state.WorkItemKind(string(it.Kind)),
			Title:  it.Title,
			Lane:   string(it.Lane),
			Status: state.WorkItemStatus(string(it.Status)),
		}
		if err := s.db.UpsertWorkItem(ctx, wi, state.SourceAdapter); err != nil {
			return fmt.Errorf("adaptersync: upsert %s: %w", it.ID, err)
		}
	}

	archived, err := s.db.TombstoneBySource(ctx, state.SourceAdapter, pollStartedAt)
	if err != nil {
		return fmt.Errorf("adaptersync: tombstone: %w", err)
	}
	for _, id := range archived {
		slog.Warn("adapter.tombstoned", "id", id, "at", pollStartedAt)
		// Cascade-archive children of an archived program. Children
		// of a feature have parent_program_id IS NULL so this is a
		// no-op for non-programs.
		if err := s.db.CascadeArchiveChildren(ctx, id); err != nil {
			return fmt.Errorf("adaptersync: cascade %s: %w", id, err)
		}
	}
	return nil
}
