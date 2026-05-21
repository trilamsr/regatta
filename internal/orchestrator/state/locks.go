package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Lock represents a soft hotspot lock as defined in docs/design.md
// §Concurrency & soft-lock policy.
type Lock struct {
	Name        string
	AgentID     int64
	AcquiredAt  time.Time
	HeartbeatAt time.Time
}

// TryAcquireLocks atomically acquires every lock in names for
// agentID. The caller MUST pre-sort names lexicographically; the
// scheduler relies on that ordering to prevent deadlock. If any
// individual acquire fails, the entire batch is rolled back and the
// first offending error is returned.
//
// The function is a thin wrapper around TryAcquireLock that shares a
// single transaction with all rows.
func (d *DB) TryAcquireLocks(ctx context.Context, names []string, agentID int64, ttl time.Duration) error {
	if len(names) == 0 {
		return nil
	}
	now := d.now().UTC()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state: begin batch acquire tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, name := range names {
		if err := acquireOne(ctx, tx, name, agentID, ttl, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func acquireOne(ctx context.Context, tx *sql.Tx, name string, agentID int64, ttl time.Duration, now time.Time) error {
	var holder int64
	var heartbeat int64
	row := tx.QueryRowContext(ctx,
		`SELECT agent_id, heartbeat_at FROM locks WHERE name = ?`, name)
	switch err := row.Scan(&holder, &heartbeat); {
	case err == nil:
		if holder == agentID {
			_, err := tx.ExecContext(ctx,
				`UPDATE locks SET heartbeat_at = ? WHERE name = ?`, now.Unix(), name)
			return err
		}
		expiry := time.Unix(heartbeat, 0).Add(ttl)
		if now.Before(expiry) {
			return ErrLockHeld
		}
		_, err := tx.ExecContext(ctx,
			`UPDATE locks SET agent_id = ?, acquired_at = ?, heartbeat_at = ? WHERE name = ?`,
			agentID, now.Unix(), now.Unix(), name)
		return err
	case errors.Is(err, sql.ErrNoRows):
		_, err := tx.ExecContext(ctx,
			`INSERT INTO locks (name, agent_id, acquired_at, heartbeat_at) VALUES (?, ?, ?, ?)`,
			name, agentID, now.Unix(), now.Unix())
		return err
	default:
		return fmt.Errorf("state: select lock: %w", err)
	}
}

// TryAcquireLock inserts a row in locks for name on behalf of agentID.
// Returns ErrLockHeld if the lock is currently held by a different
// agent. A heartbeat that has expired (older than ttl) is treated as
// released and may be stolen by the caller in the same call.
//
// The check + insert run inside a single transaction so racing
// acquisition attempts cannot both win.
func (d *DB) TryAcquireLock(ctx context.Context, name string, agentID int64, ttl time.Duration) error {
	now := d.now().UTC()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state: begin acquire tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var holder int64
	var heartbeat int64
	row := tx.QueryRowContext(ctx,
		`SELECT agent_id, heartbeat_at FROM locks WHERE name = ?`, name)
	switch err := row.Scan(&holder, &heartbeat); {
	case err == nil:
		if holder == agentID {
			// Re-acquire by same agent: refresh heartbeat.
			if _, err := tx.ExecContext(ctx,
				`UPDATE locks SET heartbeat_at = ? WHERE name = ?`, now.Unix(), name); err != nil {
				return fmt.Errorf("state: refresh heartbeat: %w", err)
			}
			return tx.Commit()
		}
		expiry := time.Unix(heartbeat, 0).Add(ttl)
		if now.Before(expiry) {
			return ErrLockHeld
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE locks SET agent_id = ?, acquired_at = ?, heartbeat_at = ? WHERE name = ?`,
			agentID, now.Unix(), now.Unix(), name); err != nil {
			return fmt.Errorf("state: steal lock: %w", err)
		}
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO locks (name, agent_id, acquired_at, heartbeat_at) VALUES (?, ?, ?, ?)`,
			name, agentID, now.Unix(), now.Unix()); err != nil {
			return fmt.Errorf("state: insert lock: %w", err)
		}
	default:
		return fmt.Errorf("state: select lock: %w", err)
	}
	return tx.Commit()
}

// HeartbeatLock refreshes the heartbeat_at column for every lock
// currently held by agentID. Returns the number of rows refreshed.
func (d *DB) HeartbeatLock(ctx context.Context, agentID int64) (int64, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE locks SET heartbeat_at = ? WHERE agent_id = ?`,
		d.now().UTC().Unix(), agentID)
	if err != nil {
		return 0, fmt.Errorf("state: heartbeat lock: %w", err)
	}
	return res.RowsAffected()
}

// ReleaseAgentLocks deletes every lock held by agentID. Called by the
// reaper on terminal state transitions.
func (d *DB) ReleaseAgentLocks(ctx context.Context, agentID int64) (int64, error) {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM locks WHERE agent_id = ?`, agentID)
	if err != nil {
		return 0, fmt.Errorf("state: release agent locks: %w", err)
	}
	return res.RowsAffected()
}

// ExpireStaleLocks deletes every lock whose heartbeat_at is older than
// ttl. Returns the number of locks expired.
func (d *DB) ExpireStaleLocks(ctx context.Context, ttl time.Duration) (int64, error) {
	cutoff := d.now().UTC().Add(-ttl).Unix()
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM locks WHERE heartbeat_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("state: expire locks: %w", err)
	}
	return res.RowsAffected()
}

// ListLocks returns every lock currently held. Order is by name
// ascending for deterministic tests and operator dumps.
func (d *DB) ListLocks(ctx context.Context) ([]Lock, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT name, agent_id, acquired_at, heartbeat_at FROM locks ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("state: list locks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Lock
	for rows.Next() {
		var l Lock
		var acquired, heartbeat int64
		if err := rows.Scan(&l.Name, &l.AgentID, &acquired, &heartbeat); err != nil {
			return nil, err
		}
		l.AcquiredAt = time.Unix(acquired, 0).UTC()
		l.HeartbeatAt = time.Unix(heartbeat, 0).UTC()
		out = append(out, l)
	}
	return out, rows.Err()
}
