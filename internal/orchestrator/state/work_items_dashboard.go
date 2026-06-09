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
	rows, err := d.sql.QueryContext(ctx,
		selectWorkItemsCols+` FROM work_items WHERE status = ? ORDER BY updated_at DESC LIMIT ?`,
		string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("state: list by status: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanWorkItems(rows)
}

// CountWorkItemsByStatus returns the row count for the given status.
func (d *DB) CountWorkItemsByStatus(ctx context.Context, status WorkItemStatus) (int, error) {
	var n int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM work_items WHERE status = ?`, string(status)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("state: count by status: %w", err)
	}
	return n, nil
}
