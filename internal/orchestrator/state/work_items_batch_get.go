package state

import (
	"context"
	"fmt"
	"strings"
)

// GetWorkItemsBatch returns id->WorkItem for every id in ids that exists. Missing ids drop from the map. Single SELECT replaces the scheduler's per-orphan + per-active-agent GetWorkItem loop (#1359).
func (d *DB) GetWorkItemsBatch(ctx context.Context, ids []string) (map[string]WorkItem, error) {
	if len(ids) == 0 {
		return map[string]WorkItem{}, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	q := fmt.Sprintf(selectWorkItemsCols+` FROM work_items WHERE id IN (%s)`, placeholders) //nolint:gosec // placeholders is "?,?,..." constructed from len(ids), no caller-controlled bytes interpolated
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("state: batch get work_items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items, err := scanWorkItems(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string]WorkItem, len(items))
	for _, w := range items {
		out[w.ID] = w
	}
	return out, nil
}

// GetWorkItemsByIDs returns id->title for every id in ids that exists. Missing ids drop from the map. Single SELECT replaces a per-id GetWorkItem loop — the dashboard agents loader was N+1 over the agents-in-flight list (#1102 review).
func (d *DB) GetWorkItemsByIDs(ctx context.Context, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	q := fmt.Sprintf(`SELECT id, title FROM work_items WHERE id IN (%s)`, placeholders) //nolint:gosec // placeholders is "?,?,..." constructed from len(ids), no caller-controlled bytes interpolated
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("state: batch get work_items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]string, len(ids))
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, fmt.Errorf("state: scan work_items title: %w", err)
		}
		out[id] = title
	}
	return out, rows.Err()
}
