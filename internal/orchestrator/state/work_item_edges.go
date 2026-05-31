// Package state — work_item_edges CRUD + query API.
//
// MVP-2 W1 conditional-DAG: edges are first-class rows with optional
// CEL predicates over upstream output JSON. The scheduler tick filters
// pending edges whose source is already merged (ListPendingEdgesFromMerged)
// and resolves the predicate against the latest journal row before
// marking the edge fired. See docs/superpowers/specs/2026-05-31-mvp-2-
// conditional-dag-design.md §3.6.
package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// EdgeRow mirrors a row in work_item_edges. Fired uses a string sum
// rather than sql.NullBool so the "not yet evaluated" sentinel
// ("pending") is queryable via an indexed equality predicate; see
// idx_work_item_edges_from(from_id, fired) in migration 0003.
type EdgeRow struct {
	ID           int64
	ProgramID    string
	FromID       string
	ToID         string
	PredicateCEL string
	IsDefault    bool
	OnSkip       string
	Fired        string
	FiredAgainst string
	EvaluatedAt  time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UpsertEdges is the legacy shim that stamps timestamps from d.now().
// New production writers (BriefLoader) should call UpsertEdgesAt with
// the poll-start tick — same constraint as UpsertWorkItemAt.
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
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state: begin upsert edges tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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
				e.PredicateCEL, boolToInt(e.IsDefault), e.OnSkip,
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
				boolToInt(e.IsDefault), e.OnSkip, now, now,
			); err != nil {
				return fmt.Errorf("state: insert edge: %w", err)
			}
		default:
			return fmt.Errorf("state: probe edge: %w", err)
		}
	}
	return tx.Commit()
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
	return scanEdges(rows)
}

// ListEdgesTo returns all edges whose to_id matches, ordered by id ASC.
func (d *DB) ListEdgesTo(ctx context.Context, toID string) ([]EdgeRow, error) {
	rows, err := d.sql.QueryContext(ctx,
		selectEdgeCols+` FROM work_item_edges WHERE to_id = ? ORDER BY id`, toID)
	if err != nil {
		return nil, fmt.Errorf("state: list edges to: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanEdges(rows)
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
	return scanEdges(rows)
}

func scanEdges(rows *sql.Rows) ([]EdgeRow, error) {
	var out []EdgeRow
	for rows.Next() {
		var e EdgeRow
		var isDefault int
		var evaluated, created, updated int64
		if err := rows.Scan(&e.ID, &e.ProgramID, &e.FromID, &e.ToID,
			&e.PredicateCEL, &isDefault, &e.OnSkip, &e.Fired, &e.FiredAgainst,
			&evaluated, &created, &updated); err != nil {
			return nil, fmt.Errorf("state: scan edge: %w", err)
		}
		e.IsDefault = isDefault != 0
		if evaluated != 0 {
			e.EvaluatedAt = time.Unix(evaluated, 0).UTC()
		}
		e.CreatedAt = time.Unix(created, 0).UTC()
		e.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
