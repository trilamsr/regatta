package state

import (
	"context"
	"fmt"
	"time"
)

// BackoffEntry is the persisted recheckBackoff suppression for one
// work_item. RetryAt is wall-clock UTC; AttemptCount mirrors
// recheckEntry.failures (R-MEGA-2 P4).
type BackoffEntry struct {
	WorkItemID   string
	AttemptCount int
	RetryAt      time.Time
}

// UpsertBackoff writes the suppression row for one work_item. Replaces
// any prior row so callers can reuse the same call shape for first-strike
// + subsequent re-arms (R-MEGA-2 P4).
func (d *DB) UpsertBackoff(ctx context.Context, e BackoffEntry) error {
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO scheduler_backoff (work_item_id, attempt_count, retry_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(work_item_id) DO UPDATE SET
		   attempt_count = excluded.attempt_count,
		   retry_at      = excluded.retry_at,
		   updated_at    = excluded.updated_at`,
		e.WorkItemID, e.AttemptCount, e.RetryAt.UTC().Unix(), d.now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("state: upsert scheduler_backoff: %w", err)
	}
	return nil
}

// DeleteBackoff drops the suppression row when a strike succeeds.
func (d *DB) DeleteBackoff(ctx context.Context, workItemID string) error {
	_, err := d.sql.ExecContext(ctx,
		`DELETE FROM scheduler_backoff WHERE work_item_id = ?`, workItemID)
	if err != nil {
		return fmt.Errorf("state: delete scheduler_backoff: %w", err)
	}
	return nil
}

// ListBackoff returns every persisted suppression row so Scheduler.New
// can rehydrate the in-memory entries map at boot (R-MEGA-2 P4).
func (d *DB) ListBackoff(ctx context.Context) ([]BackoffEntry, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT work_item_id, attempt_count, retry_at FROM scheduler_backoff`)
	if err != nil {
		return nil, fmt.Errorf("state: list scheduler_backoff: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []BackoffEntry
	for rows.Next() {
		var e BackoffEntry
		var retry int64
		if err := rows.Scan(&e.WorkItemID, &e.AttemptCount, &retry); err != nil {
			return nil, err
		}
		e.RetryAt = time.Unix(retry, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeExpiredBackoff drops rows whose retry_at is strictly less than
// now. Lets a long-uptime daemon reclaim entries the in-memory Tick
// already evicted (R-MEGA-2 P4).
func (d *DB) PurgeExpiredBackoff(ctx context.Context, now time.Time) (int64, error) {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM scheduler_backoff WHERE retry_at < ?`, now.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("state: purge scheduler_backoff: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

