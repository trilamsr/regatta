// Package state holds the orchestrator's sqlite-backed durable state.
//
// Mutations go through typed helpers so the state-machine in
// docs/design.md §State, persistence, recovery stays invariant; never
// issue ad-hoc UPDATEs on the agents table, use TransitionAgent.
//
// *DB is safe for concurrent use. Open() caps the *sql.DB pool at one
// connection so writers serialize at the application layer rather than
// retry-fighting sqlite's file lock; the cap is pinned by
// TestOpenCapsConnectionPoolAtOne.
package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// CurrentSchemaVersion is the latest forward-only migration this
// binary knows; see migrations/.
const CurrentSchemaVersion = 4

// AgentState mirrors the state-machine in docs/design.md §378.
type AgentState string

// Agent lifecycle states; see docs/design.md §378.
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

// ErrInvalidTransition is returned when TransitionAgent would violate
// the edges defined in transitions().
var ErrInvalidTransition = errors.New("state: invalid agent transition")

// ErrLockHeld is returned by TryAcquireLock when another agent holds it.
var ErrLockHeld = errors.New("state: lock held by another agent")

// ErrSchemaTooNew is returned by Migrate when the DB was touched by a
// newer binary; operators must upgrade rather than downgrade.
var ErrSchemaTooNew = errors.New("state: database schema is newer than this binary supports")

// DB wraps a *sql.DB with regatta-specific helpers. Construct via Open.
type DB struct {
	sql *sql.DB
	now func() time.Time
}

// DSN returns the standard modernc.org/sqlite DSN: 5s busy_timeout +
// foreign_keys on. Use this so the pragmas stay in lockstep.
func DSN(path string) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
}

// Open opens (or creates) the sqlite DB at dsn, applies pending
// migrations, and binds the clock to time.Now. Tests needing a
// controllable clock should call OpenWithClock.
func Open(ctx context.Context, dsn string) (*DB, error) {
	return OpenWithClock(ctx, dsn, time.Now)
}

// OpenWithClock is Open with an explicit time source. The clock is
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
	if _, err := raw.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("state: enable foreign_keys: %w", err)
	}
	if err := Migrate(ctx, raw); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return &DB{sql: raw, now: now}, nil
}

// Close closes the underlying database handle.
func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the underlying *sql.DB for raw transactions. Use sparingly.
func (d *DB) SQL() *sql.DB { return d.sql }

