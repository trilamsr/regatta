package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrCycleDetected fires when a CycleCheck would introduce a cycle
// in work_items.depends_on_features. Defined here (and re-exported
// from internal/orchestrator/errors.go) so the state package can
// reference its own sentinel without an import cycle — same pattern
// as ErrSchemaTooNew.
var ErrCycleDetected = errors.New("orchestrator: dependency cycle detected in work_items")

// ListByParent returns every work_items row whose parent_program_id
// equals parentID, in id order.
func (d *DB) ListByParent(ctx context.Context, parentID string) ([]WorkItem, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id, kind, title, lane, status,
		       COALESCE(parent_program_id, ''), depends_on_features,
		       acceptance_json, source, last_seen_at, created_at, updated_at
		FROM work_items WHERE parent_program_id = ? ORDER BY id`, parentID)
	if err != nil {
		return nil, fmt.Errorf("state: list by parent: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanWorkItems(rows)
}

// ListSpawnable returns every work_items row whose status is
// 'planned', that has no entry in agents yet, and whose
// depends_on_features are either empty or all already 'merged'.
// per spec §2.8 — the SELECT here is the materialization-eliminator:
// scheduler.Tick consumes the rows directly into the reservation tx.
//
// Note: relies on the work_items.depends_on_features NOT NULL
// schema invariant. If a future migration allows NULL, the
// `NOT EXISTS(json_each(NULL))` clause silently treats the row
// as spawnable. Migration authors must add an `IS NOT NULL`
// guard here if they relax the column.
func (d *DB) ListSpawnable(ctx context.Context) ([]WorkItem, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT w.id, w.kind, w.title, w.lane, w.status,
		       COALESCE(w.parent_program_id, ''), w.depends_on_features,
		       w.acceptance_json, w.source, w.last_seen_at,
		       w.created_at, w.updated_at
		FROM work_items w
		LEFT JOIN agents a ON w.id = a.work_item_id
		WHERE w.status = 'planned'
		  AND a.id IS NULL
		  AND (
		    w.depends_on_features = '[]'
		    OR NOT EXISTS (
		      SELECT 1 FROM json_each(w.depends_on_features)
		      WHERE value NOT IN (SELECT id FROM work_items WHERE status = 'merged')
		    )
		  )
		ORDER BY w.id`)
	if err != nil {
		return nil, fmt.Errorf("state: list spawnable: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanWorkItems(rows)
}

// CycleCheck verifies that inserting (or updating) candidate would
// not introduce a dependency cycle. Walks the existing graph + the
// candidate's depends_on_features and looks for a back-edge to
// candidate.ID. Returns ErrCycleDetected wrapped on cycle.
//
// Self-loop (candidate depends on itself) counts as a cycle.
func (d *DB) CycleCheck(ctx context.Context, candidate WorkItem) error {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, depends_on_features FROM work_items`)
	if err != nil {
		return fmt.Errorf("state: cycle scan: %w", err)
	}
	adj := map[string][]string{}
	for rows.Next() {
		var id, depsJSON string
		if err := rows.Scan(&id, &depsJSON); err != nil {
			_ = rows.Close()
			return fmt.Errorf("state: cycle scan row: %w", err)
		}
		var deps []string
		if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil {
			_ = rows.Close()
			return fmt.Errorf("state: cycle decode deps for %s: %w", id, err)
		}
		adj[id] = deps
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("state: cycle close rows: %w", err)
	}
	adj[candidate.ID] = candidate.DependsOnFeatures

	if reachable(adj, candidate.ID, candidate.ID) {
		return fmt.Errorf("%w: %s", ErrCycleDetected, candidate.ID)
	}
	return nil
}

// reachable returns true if target is reachable from start via adj,
// excluding the zero-depth self-arrival case. A self-loop
// (start depends on start) is reachable at depth 1, returning true.
func reachable(adj map[string][]string, start, target string) bool {
	visited := map[string]bool{}
	var dfs func(node string, depth int) bool
	dfs = func(node string, depth int) bool {
		if depth > 0 && node == target {
			return true
		}
		if visited[node] {
			return false
		}
		visited[node] = true
		for _, next := range adj[node] {
			if dfs(next, depth+1) {
				return true
			}
		}
		return false
	}
	return dfs(start, 0)
}

func scanWorkItems(rows *sql.Rows) ([]WorkItem, error) {
	var out []WorkItem
	for rows.Next() {
		var w WorkItem
		var depsJSON string
		var lastSeen, created, updated int64
		if err := rows.Scan(&w.ID, &w.Kind, &w.Title, &w.Lane, &w.Status,
			&w.ParentProgramID, &depsJSON, &w.AcceptanceJSON, &w.Source,
			&lastSeen, &created, &updated); err != nil {
			return nil, fmt.Errorf("state: scan work_items: %w", err)
		}
		if err := json.Unmarshal([]byte(depsJSON), &w.DependsOnFeatures); err != nil {
			return nil, fmt.Errorf("state: decode deps for %s: %w", w.ID, err)
		}
		w.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		w.CreatedAt = time.Unix(created, 0).UTC()
		w.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, w)
	}
	return out, rows.Err()
}
