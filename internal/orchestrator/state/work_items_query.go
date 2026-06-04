package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/cycle"
	"github.com/trilamsr/regatta/internal/orchestrator/state/jsonscan"
)

// ErrCycleDetected re-exports cycle.ErrCycleDetected so existing
// callers' errors.Is checks keep working unchanged.
var ErrCycleDetected = cycle.ErrCycleDetected

// selectWorkItemsCols is the canonical column list for unaliased
// work_items SELECTs feeding scanWorkItems. Centralising the column
// order keeps GetWorkItem and ListByParent in lockstep with the
// scanner. ListSpawnable joins on agents and uses its own aliased
// column list; the scan order must still match this constant.
const selectWorkItemsCols = `SELECT id, kind, title, lane, status,
	COALESCE(parent_program_id, ''), depends_on_features,
	acceptance_json, source, last_seen_at, created_at, updated_at`

// ListByParent returns every work_items row whose parent_program_id
// equals parentID, in id order.
func (d *DB) ListByParent(ctx context.Context, parentID string) ([]WorkItem, error) {
	rows, err := d.sql.QueryContext(ctx,
		selectWorkItemsCols+` FROM work_items WHERE parent_program_id = ? ORDER BY id`, parentID)
	if err != nil {
		return nil, fmt.Errorf("state: list by parent: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanWorkItems(rows)
}

// ListSpawnable returns every work_items row whose status is
// 'planned', that has no entry in agents yet, whose legacy
// depends_on_features are either empty or all already 'merged', and
// whose inbound work_item_edges (if any) are satisfied. per spec §2.8
// — the SELECT here is the materialization-eliminator: scheduler.Tick
// consumes the rows directly into the reservation tx.
//
// Edge satisfaction (W4-A): a planned row qualifies when any of:
//   - no inbound work_item_edges at all (v1 fast-path, unchanged);
//   - at least one inbound edge has fired='true';
//   - every inbound edge resolved (no 'pending') AND at least one
//     inbound edge carries on_skip='ignore' — the diamond-join /
//     default-next escape hatch so a downstream node still spawns
//     when every gating predicate evaluated false.
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
		  AND (
		    NOT EXISTS (SELECT 1 FROM work_item_edges WHERE to_id = w.id)
		    OR EXISTS (
		      SELECT 1 FROM work_item_edges
		      WHERE to_id = w.id AND fired = 'true'
		    )
		    OR (
		      NOT EXISTS (
		        SELECT 1 FROM work_item_edges
		        WHERE to_id = w.id AND fired = 'pending'
		      )
		      AND EXISTS (
		        SELECT 1 FROM work_item_edges
		        WHERE to_id = w.id AND on_skip = 'ignore'
		      )
		    )
		  )
		ORDER BY w.id`)
	if err != nil {
		return nil, fmt.Errorf("state: list spawnable: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanWorkItems(rows)
}

// CycleCheck rejects candidate when its depends_on_features overlay
// would close a cycle in work_items. Loads adjacency from SQLite,
// applies the candidate overlay, then delegates to cycle.Check.
// Load-bearing for scheduler reservation (#88).
func (d *DB) CycleCheck(ctx context.Context, candidate WorkItem) error {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, depends_on_features FROM work_items`)
	if err != nil {
		return fmt.Errorf("state: cycle scan: %w", err)
	}
	adj := map[string][]string{}
	for rows.Next() {
		var id string
		var depsJSON []byte
		if err := rows.Scan(&id, &depsJSON); err != nil {
			_ = rows.Close()
			return fmt.Errorf("state: cycle scan row: %w", err)
		}
		if id == candidate.ID {
			adj[id] = nil
			continue
		}
		var deps []string
		if err := jsonscan.Scan(depsJSON, func(s []byte) {
			deps = append(deps, string(s))
		}); err != nil {
			_ = rows.Close()
			return fmt.Errorf("state: cycle decode deps for %s: %w", id, err)
		}
		adj[id] = deps
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("state: cycle close rows: %w", err)
	}
	adj[candidate.ID] = append([]string(nil), candidate.DependsOnFeatures...)
	return cycle.Check(adj, candidate.ID)
}

// MaxUpdatedAtForBriefChildren returns the largest updated_at across
// all work_items whose parent_program_id == parentID and source ==
// SourceBrief, or the zero time if no such row exists. Used by
// BriefLoader to reject stale brief replays: if a freshly-loaded
// brief's ProducedAt is <= this watermark, the brief was already
// processed (or superseded by a newer one for the same program) and
// re-applying it would silently revert later state. Read-only.
func (d *DB) MaxUpdatedAtForBriefChildren(ctx context.Context, parentID string) (time.Time, error) {
	row := d.sql.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(updated_at), 0) FROM work_items
		WHERE parent_program_id = ? AND source = ?`,
		parentID, string(SourceBrief))
	var ts int64
	if err := row.Scan(&ts); err != nil {
		return time.Time{}, fmt.Errorf("state: max updated_at for brief children of %s: %w", parentID, err)
	}
	if ts == 0 {
		return time.Time{}, nil
	}
	return time.Unix(ts, 0).UTC(), nil
}

// ListArchivedProgramsWithLiveChildren returns the IDs of every
// program whose own row is archived but which still has at least one
// non-archived child via parent_program_id. AdapterSync's reconciler
// calls this every tick to converge stranded children when a prior
// tick crashed between the program-archive write and the child
// cascade. Idempotent — once no live children remain, returns empty.
func (d *DB) ListArchivedProgramsWithLiveChildren(ctx context.Context) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT DISTINCT p.id
		FROM work_items p
		JOIN work_items c ON c.parent_program_id = p.id
		WHERE p.kind = ? AND p.status = ? AND c.status != ?
		ORDER BY p.id`,
		string(KindProgram), string(WorkStatusArchived), string(WorkStatusArchived))
	if err != nil {
		return nil, fmt.Errorf("state: list archived programs w/ live children: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("state: scan archived program id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
