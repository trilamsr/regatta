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
//
// RecordEvent owns its own tx. Callers composing the event append
// with sibling writes (e.g. the rejection-router escalation tx —
// issue #477) use RecordEventTx via DB.WithTx so the audit row commits
// atomically with the state change it audits.
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

// RecordEventTx is the tx-aware variant of RecordEvent. The caller
// owns the tx lifecycle so the audit row commits atomically with the
// state mutation it audits.
func (d *DB) RecordEventTx(ctx context.Context, tx *sql.Tx, agentID int64, kind, payloadJSON string) error {
	if payloadJSON == "" {
		payloadJSON = "{}"
	}
	var agent any
	if agentID == 0 {
		agent = nil
	} else {
		agent = agentID
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO events (agent_id, kind, payload_json, created_at) VALUES (?, ?, ?, ?)`,
		agent, kind, payloadJSON, d.now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("state: record event: %w", err)
	}
	return nil
}

// ListEventsByKindSince returns events of the given kind whose id is
// strictly greater than sinceID, ordered by id ascending. Used by
// event-driven tickers (rejectionrouter, future PR-watcher) to
// resume from an in-memory cursor without re-reading the whole
// events table. A non-positive limit defaults to 100.
func (d *DB) ListEventsByKindSince(ctx context.Context, kind string, sinceID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, agent_id, kind, payload_json, created_at
		 FROM events
		 WHERE kind = ? AND id > ?
		 ORDER BY id ASC
		 LIMIT ?`, kind, sinceID, limit)
	if err != nil {
		return nil, fmt.Errorf("state: list events by kind: %w", err)
	}
	defer func() { _ = rows.Close() }()
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

// LatestEventByKind returns the most recent event of kind by
// created_at, or sql.ErrNoRows when none exists. Cost-cap derives the
// operator-resume window from the row's created_at: a Resume event
// whose created_at >= today's day_anchor lifts the throttle until the
// next rollover (spec PHASE-AUTONOMY W5 §3).
func (d *DB) LatestEventByKind(ctx context.Context, kind string) (Event, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, agent_id, kind, payload_json, created_at
		 FROM events
		 WHERE kind = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`, kind)
	var e Event
	var created int64
	if err := row.Scan(&e.ID, &e.AgentID, &e.Kind, &e.PayloadJSON, &created); err != nil {
		return Event{}, err
	}
	e.CreatedAt = time.Unix(created, 0).UTC()
	return e, nil
}

// ListEventsSince returns events whose id is strictly greater than
// sinceID AND created_at >= cutoffUnix, ordered by id ascending.
// Closes the bug where `regatta events tail --since 5m` against a
// fully-populated events table silently hid new rows behind the
// oldest LIMIT cap (ListEvents w/ ASC order returned only the
// earliest rows). cutoffUnix=0 disables the time filter; sinceID=0
// disables the resume cursor. Limit ≤0 defaults to 100.
func (d *DB) ListEventsSince(ctx context.Context, sinceID int64, cutoffUnix int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, agent_id, kind, payload_json, created_at
		 FROM events
		 WHERE id > ? AND created_at >= ?
		 ORDER BY id ASC
		 LIMIT ?`, sinceID, cutoffUnix, limit)
	if err != nil {
		return nil, fmt.Errorf("state: list events since: %w", err)
	}
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
