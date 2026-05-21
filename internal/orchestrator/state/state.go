// Package state holds the orchestrator's sqlite-backed durable state.
//
// The schema is defined in schema.sql and embedded at build time. All
// mutations go through small typed helpers in this package so the
// state-machine transitions in docs/design.md §State, persistence,
// recovery stay invariant. Callers MUST NOT issue ad-hoc UPDATEs on
// the agents table; use TransitionAgent.
//
// Concurrency: a *DB is safe for concurrent use; the underlying
// modernc.org/sqlite driver serializes writes internally.
package state

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// CurrentSchemaVersion is the version this binary knows how to apply.
// Migrations are forward-only; see schema.sql.
const CurrentSchemaVersion = 1

// AgentState mirrors the state-machine in docs/design.md §378.
type AgentState string

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

// DB wraps a *sql.DB with regatta-specific helpers. Open the DB via
// Open(); never construct a DB literal directly.
type DB struct {
	sql *sql.DB
	now func() time.Time
}

// Open opens (or creates) the sqlite database at dsn and applies any
// pending migrations. dsn follows the modernc.org/sqlite syntax;
// "file:regatta.db?_pragma=busy_timeout(5000)" is a sensible default
// for a file-backed DB.
func Open(ctx context.Context, dsn string) (*DB, error) {
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("state: open sqlite: %w", err)
	}
	if err := raw.PingContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("state: ping sqlite: %w", err)
	}
	if _, err := raw.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("state: enable foreign_keys: %w", err)
	}
	db := &DB{sql: raw, now: time.Now}
	if err := db.migrate(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
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

func (d *DB) migrate(ctx context.Context) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state: begin migrate tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("state: apply schema: %w", err)
	}
	var current int
	row := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("state: read schema_version: %w", err)
	}
	if current > CurrentSchemaVersion {
		return fmt.Errorf("state: db schema_version=%d is newer than binary=%d", current, CurrentSchemaVersion)
	}
	if current < CurrentSchemaVersion {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_version(version) VALUES (?)", CurrentSchemaVersion); err != nil {
			return fmt.Errorf("state: write schema_version: %w", err)
		}
	}
	return tx.Commit()
}
