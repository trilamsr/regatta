package state

import (
	"context"
	"fmt"
)

// ListWorkItemsByStatus returns up to limit rows in the given status, newest-updated first.
func (d *DB) ListWorkItemsByStatus(ctx context.Context, status WorkItemStatus, limit int) ([]WorkItem, error) {
	if limit <= 0 {
		return nil, nil
	}
	if status == "" {
		return nil, fmt.Errorf("state: list by status: status is required")
	}
	q := selectWorkItemsCols + ` FROM work_items WHERE status = ? ORDER BY updated_at DESC LIMIT ?`
	rows, err := d.sql.QueryContext(ctx, q, string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("state: list by status: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out, err := scanWorkItems(rows)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CountWorkItemsByStatus returns the row count for the given status.
func (d *DB) CountWorkItemsByStatus(ctx context.Context, status WorkItemStatus) (int, error) {
	if status == "" {
		return 0, fmt.Errorf("state: count by status: status is required")
	}
	var n int
	row := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM work_items WHERE status = ?`, string(status))
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("state: count by status: %w", err)
	}
	if n < 0 {
		return 0, fmt.Errorf("state: count by status: negative count %d (sqlite COUNT* invariant)", n)
	}
	return n, nil
}
