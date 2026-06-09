package state

import (
	"context"
	"fmt"
)

// WorkItemStatusSummary is one (status, count) row plus the most-recently-updated sample within that status. The dashboard's BACKLOG panel reads four buckets per poll; merging the count + sample query into one round trip eliminates the 8-call N+1 the earlier loop produced (#1102 review).
type WorkItemStatusSummary struct {
	Status  WorkItemStatus
	Count   int
	Samples []WorkItem
}

// SummarizeWorkItemStatuses returns one summary per requested status using one COUNT(*)+GROUP BY plus one LIMIT-N sample query per status. The sample query is still per-status (sqlite lacks a portable LATERAL JOIN), but the count is collapsed to a single round trip; total queries drop from 2N to N+1.
func (d *DB) SummarizeWorkItemStatuses(ctx context.Context, statuses []WorkItemStatus, sampleSize int) (map[WorkItemStatus]WorkItemStatusSummary, error) {
	if len(statuses) == 0 {
		return map[WorkItemStatus]WorkItemStatusSummary{}, nil
	}
	out := make(map[WorkItemStatus]WorkItemStatusSummary, len(statuses))
	for _, s := range statuses {
		out[s] = WorkItemStatusSummary{Status: s}
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT status, COUNT(*) FROM work_items GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("state: summarize work_items statuses: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("state: scan summary row: %w", err)
		}
		key := WorkItemStatus(status)
		if cur, ok := out[key]; ok {
			cur.Count = n
			out[key] = cur
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: summary rows: %w", err)
	}
	for _, s := range statuses {
		if sampleSize <= 0 {
			continue
		}
		samples, err := d.ListWorkItemsByStatus(ctx, s, sampleSize)
		if err != nil {
			continue
		}
		cur := out[s]
		cur.Samples = samples
		out[s] = cur
	}
	return out, nil
}
