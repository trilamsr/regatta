package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Event is the in-memory view of a row in the events table.
type Event struct {
	ID          int64
	AgentID     sql.NullInt64
	Kind        string
	PayloadJSON string
	CreatedAt   time.Time
}

// RecordEvent appends an event row. payloadJSON should be a valid JSON
// document; the caller owns marshaling. An empty string is normalized
// to "{}" so downstream readers can always parse.
func (d *DB) RecordEvent(ctx context.Context, agentID int64, kind, payloadJSON string) error {
	if payloadJSON == "" {
		payloadJSON = "{}"
	}
	var agent any
	if agentID == 0 {
		agent = nil
	} else {
		agent = agentID
	}
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO events (agent_id, kind, payload_json, created_at) VALUES (?, ?, ?, ?)`,
		agent, kind, payloadJSON, d.now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("state: record event: %w", err)
	}
	return nil
}

// ListEvents returns events ordered by id ascending. Used by tests and
// the audit log writer.
func (d *DB) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, agent_id, kind, payload_json, created_at
		 FROM events ORDER BY id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("state: list events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var created int64
		if err := rows.Scan(&e.ID, &e.AgentID, &e.Kind, &e.PayloadJSON, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}
