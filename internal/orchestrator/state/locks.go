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
// TryAcquireLocks owns its own tx. The scheduler reservation path
// uses TryAcquireLocksTx via DB.WithTx so the lock writes commit
// atomically with the sibling agent transition (issue #88).
func (d *DB) TryAcquireLocks(ctx context.Context, names []string, agentID int64, ttl time.Duration) error {
	if len(names) == 0 {
		return nil
	}
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		return d.TryAcquireLocksTx(ctx, tx, names, agentID, ttl)
	})
}

// TryAcquireLocksTx is the tx-aware variant of TryAcquireLocks. The
// caller owns the tx lifecycle. ErrLockHeld surfaces unwrapped so
// reservation closures can errors.Is and roll back the whole tx.
func (d *DB) TryAcquireLocksTx(ctx context.Context, tx *sql.Tx, names []string, agentID int64, ttl time.Duration) error {
	if len(names) == 0 {
		return nil
	}
	now := d.now().UTC()
	for _, name := range names {
		if err := acquireOne(ctx, tx, name, agentID, ttl, now); err != nil {
			return err
		}
	}
	return nil
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
		expiry := time.Unix(heartbeat, 0).Add(ttl) // allow-bare-time-unix: now.Before(expiry) comparison; Location irrelevant.
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
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		var holder, heartbeat int64
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
				return nil
			}
			expiry := time.Unix(heartbeat, 0).Add(ttl) // allow-bare-time-unix: now.Before(expiry) comparison; Location irrelevant.
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
		return nil
	})
}

// HeartbeatLock refreshes the heartbeat_at column for every lock
// currently held by agentID. Returns the number of rows refreshed.
//
// The UPDATE is CAS-guarded by `heartbeat_at < ?`: two orchestrator
// instances writing concurrently cannot lose a newer heartbeat to an
// older one's commit order. A rows-affected of zero is a no-op — the
// row already carries a newer (or equal) timestamp — and is not an
// error; the stale-lock sweep keys off MAX(heartbeat_at), not on which
// writer last touched the row.
func (d *DB) HeartbeatLock(ctx context.Context, agentID int64) (int64, error) {
	now := d.now().UTC().Unix()
	res, err := d.sql.ExecContext(ctx,
		`UPDATE locks SET heartbeat_at = ? WHERE agent_id = ? AND heartbeat_at < ?`,
		now, agentID, now)
	if err != nil {
		return 0, fmt.Errorf("state: heartbeat lock: %w", err)
	}
	return res.RowsAffected()
}

// ReleaseAgentLocks deletes every lock held by agentID. Called by the
// reaper on terminal state transitions.
//
// ReleaseAgentLocks owns its own tx. The rejection-router escalation
// path uses ReleaseAgentLocksTx via DB.WithTx so the lock delete
// commits atomically with the gates_failed -> escalated transition
// (issue #477).
func (d *DB) ReleaseAgentLocks(ctx context.Context, agentID int64) (int64, error) {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM locks WHERE agent_id = ?`, agentID)
	if err != nil {
		return 0, fmt.Errorf("state: release agent locks: %w", err)
	}
	return res.RowsAffected()
}

// ReleaseAgentLocksTx is the tx-aware variant of ReleaseAgentLocks.
// The caller owns the tx lifecycle so the delete can commit atomically
// with sibling writes (e.g. the rejection-router escalation tx that
// also flips state and appends the audit event).
func (d *DB) ReleaseAgentLocksTx(ctx context.Context, tx *sql.Tx, agentID int64) (int64, error) {
	res, err := tx.ExecContext(ctx,
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
