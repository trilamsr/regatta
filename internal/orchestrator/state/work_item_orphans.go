package state

import (
	"context"
	"fmt"
)

// ListWorkItemsWithJournalNotMerged returns every work_item id that has
// at least one outputs-journal row but whose status is not yet 'merged'.
// Surfaces orphans created when Spawner.Complete crashed between the
// AppendOutput commit and the UpsertWorkItem(status=merged) commit
// (issue #99). Ordered by id for determinism so reconciler logs replay
// the same sequence across runs.
//
// Archived rows are excluded: a Tombstone sweep that ran after the
// crashed Complete has already taken the row out of the live queue,
// and re-flipping it to merged would resurrect an archived item.
func (d *DB) ListWorkItemsWithJournalNotMerged(ctx context.Context) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT DISTINCT w.id
		FROM work_items w
		INNER JOIN work_item_outputs o ON o.work_item_id = w.id
		WHERE w.status != ? AND w.status != ?
		ORDER BY w.id`,
		WorkStatusMerged, WorkStatusArchived)
	if err != nil {
		return nil, fmt.Errorf("state: list orphan-journal work_items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("state: scan orphan id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: iterate orphan ids: %w", err)
	}
	return out, nil
}
