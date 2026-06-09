package state

import (
	"context"
	"fmt"
	"time"
)

// GetEvent fetches one events row by id. Read-only — dashboard surfaces this for the per-event drawer drill-down so the operator can read the raw substrate payload without grepping the log file.
func (d *DB) GetEvent(ctx context.Context, id int64) (Event, error) {
	var ev Event
	var createdAt int64
	row := d.sql.QueryRowContext(ctx, `SELECT id, agent_id, kind, payload_json, created_at FROM events WHERE id = ?`, id)
	if err := row.Scan(&ev.ID, &ev.AgentID, &ev.Kind, &ev.PayloadJSON, &createdAt); err != nil {
		return Event{}, fmt.Errorf("state: get event %d: %w", id, err)
	}
	ev.CreatedAt = time.Unix(createdAt, 0).UTC()
	return ev, nil
}
