// Package state holds the orchestrator's sqlite-backed durable state.
// Mutations go through typed helpers so the state-machine in
// docs/design.md §State stays invariant — never issue ad-hoc UPDATEs
// on agents, use TransitionAgent. *DB is safe for concurrent use;
// Open() caps *sql.DB at one connection so writers serialize at the
// application layer rather than retry-fighting sqlite's file lock.
// Subpackages MUST NOT import state — enforced by
// scripts/check-state-tier-order.sh in make check.
package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// CurrentSchemaVersion is the latest forward-only migration this binary
// knows; Migrate() rejects DBs whose goose_db_version exceeds this.
const CurrentSchemaVersion int64 = 22

// AgentState mirrors the state-machine in docs/design.md §378.
type AgentState string

// AgentPending and siblings form the closed enum transitions.AgentEdges checks — adding a value without updating that map silently breaks invariants.
const (
	AgentPending       AgentState = "pending"
	AgentSpawning      AgentState = "spawning"
	AgentRunning       AgentState = "running"
	AgentPROpen        AgentState = "pr_open"
	AgentGatesRunning  AgentState = "gates_running"
	AgentAwaitingMerge AgentState = "awaiting_merge"
	AgentGatesFailed   AgentState = "gates_failed"
	AgentDone          AgentState = "done"
	AgentWithdrawn     AgentState = "withdrawn"
	AgentCrashed       AgentState = "crashed"
	AgentEscalated     AgentState = "escalated"
)

// ErrInvalidTransition — TransitionAgent would violate the edges
// defined in transitions.AgentEdges.
var ErrInvalidTransition = errors.New("state: invalid agent transition")

// ErrLockHeld — TryAcquireLock found another agent holds it.
var ErrLockHeld = errors.New("state: lock held by another agent")

// ErrSchemaTooNew — DB was touched by a newer binary; operators must
// upgrade rather than downgrade.
var ErrSchemaTooNew = errors.New("state: database schema is newer than this binary supports")

// DB wraps *sql.DB with regatta helpers. Construct via Open.
type DB struct {
	sql *sql.DB
	now func() time.Time
}

// DSN returns the standard modernc.org/sqlite DSN: 5s busy_timeout,
// foreign_keys on, _txlock=immediate so every BeginTx takes the writer
// lock at BEGIN-time rather than at first-write (see WithTx).
func DSN(path string) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate", path)
}

// Open opens (or creates) the sqlite DB, applies pending migrations,
// and binds the clock to time.Now. Tests needing a controllable clock
// use OpenWithClock.
func Open(ctx context.Context, dsn string) (*DB, error) {
	return OpenWithClock(ctx, dsn, time.Now)
}

// OpenWithClock is Open with an explicit time source. Clock is
// constructor-bound — no setter — so concurrent mutations cannot race.
func OpenWithClock(ctx context.Context, dsn string, now func() time.Time) (*DB, error) {
	if now == nil {
		return nil, fmt.Errorf("state: OpenWithClock requires a non-nil clock")
	}
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("state: open sqlite: %w", err)
	}
	raw.SetMaxOpenConns(1)
	if err := raw.PingContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("state: ping sqlite: %w", err)
	}
	if err := Migrate(ctx, raw); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return &DB{sql: raw, now: now}, nil
}

// Close releases the single sqlite connection — callers MUST defer it because SetMaxOpenConns(1) means a leak deadlocks the next Open.
func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the underlying *sql.DB for raw transactions. Use sparingly.
func (d *DB) SQL() *sql.DB { return d.sql }

// WithTx runs fn inside a single sqlite transaction; commits on nil error
// or rolls back on any non-nil error or panic. Uses BEGIN IMMEDIATE (via
// _txlock=immediate in DSN) so the writer lock is taken at BEGIN where
// busy_timeout retries absorb contention, rather than at first-write where
// a read-then-write tx can hit SQLITE_BUSY at the upgrade boundary.
// Sentinel errors from fn pass through unwrapped so callers can errors.Is
// them. Panics propagate through the deferred Rollback (not recovered).
func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("state: commit tx: %w", err)
	}
	return nil
}
