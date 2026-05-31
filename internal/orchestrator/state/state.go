// Package state holds the orchestrator's sqlite-backed durable state.
//
// The schema lives in versioned files under migrations/, applied via
// pressly/goose by Migrate() (called from Open()). All mutations go
// through small typed helpers in this package so the state-machine
// transitions in docs/design.md §State, persistence, recovery stay
// invariant. Callers MUST NOT issue ad-hoc UPDATEs on the agents
// table; use TransitionAgent.
//
// Concurrency: a *DB is safe for concurrent use. Open() caps the
// underlying *sql.DB pool at one connection so writers serialize at
// the application layer. database/sql's pool default is unbounded;
// modernc.org/sqlite serializes writes within a single *sql.Conn but
// not across pool members, and sqlite's file lock + per-connection
// busy_timeout will retry-fight rather than queue under bursty
// concurrent recovery. The MaxOpenConns(1) contract is pinned by
// TestOpenCapsConnectionPoolAtOne so a silent refactor cannot
// regress.
package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// CurrentSchemaVersion is the version this binary knows how to apply.
// Migrations are forward-only; see migrations/.
const CurrentSchemaVersion = 2

// AgentState mirrors the state-machine in docs/design.md §378.
type AgentState string

// Agent lifecycle states; full state-machine in docs/design.md §378.
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

// ErrInvalidTransition is returned when a TransitionAgent call would
// violate the state-machine edges defined in transitions().
var ErrInvalidTransition = errors.New("state: invalid agent transition")

// ErrLockHeld is returned by TryAcquireLock when the lock is already
// held by a different agent.
var ErrLockHeld = errors.New("state: lock held by another agent")

// ErrSchemaTooNew is returned by Migrate when the database has been
// touched by a newer binary's migrations than this binary knows
// about. Operators must upgrade rather than downgrade. Re-exported
// from internal/orchestrator/errors.go as the canonical sentinel for
// downstream packages — this copy lives here only because state is
// upstream of orchestrator (cannot import it).
var ErrSchemaTooNew = errors.New("state: database schema is newer than this binary supports")

// DB wraps a *sql.DB with regatta-specific helpers. Open the DB via
// Open(); never construct a DB literal directly.
type DB struct {
	sql *sql.DB
	now func() time.Time
}

// DSN returns the standard modernc.org/sqlite DSN for a file-backed
// regatta state DB: 5-second busy_timeout + foreign_keys on. Use this
// instead of hand-rolling the URL so the pragmas stay in lockstep.
func DSN(path string) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
}

// Open opens (or creates) the sqlite database at dsn and applies any
// pending migrations. Prefer DSN(path) for the standard pragma set.
func Open(ctx context.Context, dsn string) (*DB, error) {
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
	db := &DB{sql: raw, now: time.Now}
	return db, nil
}

// Close closes the underlying database handle.
func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the underlying *sql.DB for callers that need raw
// transactions (e.g. scheduler.Tick). Use sparingly.
func (d *DB) SQL() *sql.DB { return d.sql }

// SetClock overrides the time source. Tests use this for deterministic
// timestamps; production code MUST NOT call this.
func (d *DB) SetClock(now func() time.Time) { d.now = now }

