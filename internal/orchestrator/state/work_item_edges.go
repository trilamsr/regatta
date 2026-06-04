// Package state — work_item_edges CRUD + query API.
//
// MVP-2 W1 conditional-DAG: edges are first-class rows with optional
// CEL predicates over upstream output JSON. The scheduler tick filters
// pending edges whose source is already merged (ListPendingEdgesFromMerged)
// and resolves the predicate against the latest journal row before
// marking the edge fired. See docs/superpowers/specs/2026-05-31-mvp-2-
// conditional-dag-design.md §3.6.
//
// EdgeRow + EdgeFromAggregate live in state/edgeagg/ (pure subpackage;
// see docs/engineer/specs/2026-06-04-state-package-split-design.md §5.T2)
// and are re-exported via aliases.go so callers keep their state.X
// spelling.
package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/edgeagg"
)

// UpsertEdges is the legacy shim that stamps timestamps from d.now().
// New production writers (BriefLoader) should call UpsertEdgesAt with
// the poll-start tick — same constraint as UpsertWorkItem.
func (d *DB) UpsertEdges(ctx context.Context, programID string, edges []EdgeRow) error {
	return d.UpsertEdgesAt(ctx, programID, edges, d.now())
}

// UpsertEdgesAt inserts or updates each edge, matched by
// (program_id, from_id, to_id). New rows start fired='pending'.
// Existing rows have predicate_cel / is_default / on_skip refreshed
// BUT fired/fired_against/evaluated_at are preserved: a re-plan that
// changes the predicate text must not silently flip an edge that
// already evaluated. Operators get an explicit slog.Warn at the
// BriefLoader callsite when that happens (W5).
func (d *DB) UpsertEdgesAt(ctx context.Context, programID string, edges []EdgeRow, at time.Time) error {
	if len(edges) == 0 {
		return nil
	}
	now := at.UTC().Unix()
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		for _, e := range edges {
			var existingID int64
			err := tx.QueryRowContext(ctx,
				`SELECT id FROM work_item_edges
				 WHERE program_id = ? AND from_id = ? AND to_id = ?`,
				programID, e.FromID, e.ToID).Scan(&existingID)
			switch {
			case err == nil:
				if _, err := tx.ExecContext(ctx, `
					UPDATE work_item_edges SET
						predicate_cel = ?, is_default = ?, on_skip = ?,
						updated_at = ?
					WHERE id = ?`,
					e.PredicateCEL, edgeagg.BoolToInt(e.IsDefault), e.OnSkip,
					now, existingID,
				); err != nil {
					return fmt.Errorf("state: update edge: %w", err)
				}
			case errors.Is(err, sql.ErrNoRows):
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO work_item_edges (
						program_id, from_id, to_id, predicate_cel, is_default,
						on_skip, fired, fired_against, created_at, updated_at
					) VALUES (?, ?, ?, ?, ?, ?, 'pending', '', ?, ?)`,
					programID, e.FromID, e.ToID, e.PredicateCEL,
					edgeagg.BoolToInt(e.IsDefault), e.OnSkip, now, now,
				); err != nil {
					return fmt.Errorf("state: insert edge: %w", err)
				}
			default:
				return fmt.Errorf("state: probe edge: %w", err)
			}
		}
		return nil
	})
}

// MarkEdgeFired is the legacy shim that pulls the stamp through d.now().
func (d *DB) MarkEdgeFired(ctx context.Context, edgeID int64, fired, contentSHA string) error {
	return d.MarkEdgeFiredAt(ctx, edgeID, fired, contentSHA, d.now())
}

// MarkEdgeFiredAt atomically sets fired + fired_against + evaluated_at.
// Idempotent at the scheduler level: re-applying the same (fired,
// contentSHA) is harmless; the row's updated_at is bumped regardless.
// Returns an error if edgeID does not exist so a typo or race against
// a tombstone cannot fail silently.
func (d *DB) MarkEdgeFiredAt(ctx context.Context, edgeID int64, fired, contentSHA string, at time.Time) error {
	now := at.UTC().Unix()
	res, err := d.sql.ExecContext(ctx, `
		UPDATE work_item_edges
		SET fired = ?, fired_against = ?, evaluated_at = ?, updated_at = ?
		WHERE id = ?`,
		fired, contentSHA, now, now, edgeID)
	if err != nil {
		return fmt.Errorf("state: mark edge fired: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("state: mark edge rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("state: edge id=%d not found", edgeID)
	}
	return nil
}

const selectEdgeCols = `SELECT id, program_id, from_id, to_id,
	predicate_cel, is_default, on_skip, fired, fired_against,
	COALESCE(evaluated_at, 0), created_at, updated_at`

// ListEdgesFrom returns all edges whose from_id matches, ordered by id
// ASC so callers see a deterministic insertion order.
func (d *DB) ListEdgesFrom(ctx context.Context, fromID string) ([]EdgeRow, error) {
	rows, err := d.sql.QueryContext(ctx,
		selectEdgeCols+` FROM work_item_edges WHERE from_id = ? ORDER BY id`, fromID)
	if err != nil {
		return nil, fmt.Errorf("state: list edges from: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return edgeagg.ScanEdges(rows)
}

// CountNonDefaultEdgeStates returns the EdgeFromAggregate for a fromID
// in a single index-only query against idx_work_item_edges_from
// (from_id, fired). It replaces the post-loop ListEdgesFrom re-read
// the scheduler used to do per pending-edge group; same post-write
// semantic (so the partial-tick-crash invariant from #98 still holds),
// without the per-row scan + slice alloc.
//
// SQL shape: one CASE-aggregated SELECT. The default-row probe uses a
// correlated subquery against the same index — sqlite plans this as a
// second seek on the same key, not a second table scan. EXPLAIN QUERY
// PLAN for N=1000 confirms SEARCH USING COVERING INDEX (verified
// manually via `make explain-count-edges` precedent, see docs).
func (d *DB) CountNonDefaultEdgeStates(ctx context.Context, fromID string) (EdgeFromAggregate, error) {
	var (
		nonDefaultCount, defaultCount int
		anyTrue, anyPending           sql.NullInt64
		defID                         sql.NullInt64
		defFired                      sql.NullString
		defProgram                    sql.NullString
	)
	err := d.sql.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN is_default = 0 THEN 1 ELSE 0 END), 0) AS non_default_count,
			COALESCE(MAX(CASE WHEN is_default = 0 AND fired = 'true' THEN 1 ELSE 0 END), 0) AS any_true,
			COALESCE(MAX(CASE WHEN is_default = 0 AND fired = 'pending' THEN 1 ELSE 0 END), 0) AS any_pending,
			COALESCE(SUM(CASE WHEN is_default = 1 THEN 1 ELSE 0 END), 0) AS default_count,
			(SELECT id        FROM work_item_edges WHERE from_id = ? AND is_default = 1 ORDER BY id LIMIT 1) AS default_id,
			(SELECT fired     FROM work_item_edges WHERE from_id = ? AND is_default = 1 ORDER BY id LIMIT 1) AS default_fired,
			(SELECT program_id FROM work_item_edges WHERE from_id = ? ORDER BY id LIMIT 1)                  AS default_program
		FROM work_item_edges
		WHERE from_id = ?`,
		fromID, fromID, fromID, fromID).Scan(
		&nonDefaultCount,
		&anyTrue,
		&anyPending,
		&defaultCount,
		&defID,
		&defFired,
		&defProgram,
	)
	if err != nil {
		return EdgeFromAggregate{}, fmt.Errorf("state: count non-default edge states: %w", err)
	}
	return edgeagg.BuildAggregate(nonDefaultCount, anyTrue, anyPending, defaultCount, defID, defFired, defProgram), nil
}

// ListEdgesTo returns all edges whose to_id matches, ordered by id ASC.
func (d *DB) ListEdgesTo(ctx context.Context, toID string) ([]EdgeRow, error) {
	rows, err := d.sql.QueryContext(ctx,
		selectEdgeCols+` FROM work_item_edges WHERE to_id = ? ORDER BY id`, toID)
	if err != nil {
		return nil, fmt.Errorf("state: list edges to: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return edgeagg.ScanEdges(rows)
}

// ListPendingEdgesFromMerged returns every edge whose from_id is the
// id of a work_item already in status='merged' AND whose fired status
// is still 'pending'. Scheduler tick step-0 fetches this slice per tick.
func (d *DB) ListPendingEdgesFromMerged(ctx context.Context) ([]EdgeRow, error) {
	rows, err := d.sql.QueryContext(ctx,
		selectEdgeCols+` FROM work_item_edges e
		WHERE e.fired = 'pending'
		  AND EXISTS (SELECT 1 FROM work_items w
		              WHERE w.id = e.from_id AND w.status = 'merged')
		ORDER BY e.id`)
	if err != nil {
		return nil, fmt.Errorf("state: list pending edges from merged: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return edgeagg.ScanEdges(rows)
}
