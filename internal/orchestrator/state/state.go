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
// binary knows; see migrations/. Migrate() rejects DBs whose
// goose_db_version exceeds this — see ErrSchemaTooNew.
const CurrentSchemaVersion int64 = 11

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

// DSN returns the standard modernc.org/sqlite DSN: 5s busy_timeout,
// foreign_keys on, and _txlock=immediate so every BeginTx (including
// WithTx) takes the writer lock at BEGIN-time rather than at
// first-write. See WithTx for the full rationale.
func DSN(path string) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate", path)
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
	// foreign_keys enabled via DSN pragma above; no explicit Exec needed.
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

// WithTx runs fn inside a single sqlite transaction and commits on
// nil error or rolls back on any non-nil error or panic.
//
// Why BEGIN IMMEDIATE: the default DEFERRED mode acquires the writer
// lock lazily at first-write; a tx that reads then writes can hit
// SQLITE_BUSY at the upgrade boundary if another writer arrived
// between the read and the write. IMMEDIATE acquires the writer lock
// at BEGIN, where busy_timeout retries are designed to absorb
// contention. The single-connection pool (Open caps MaxOpenConns at
// 1) already serializes writers at the process level; pinning
// IMMEDIATE keeps that invariant load-bearing at the SQL layer too,
// so a future pool resize or read-only secondary connection cannot
// silently re-introduce upgrade contention. The mode is wired
// globally via _txlock=immediate in DSN — see DSN.
//
// Sentinel errors returned by fn pass through unwrapped so callers
// may errors.Is them — e.g. the scheduler's reservation closure
// returns state.ErrLockHeld from TryAcquireLocksTx and the outer
// caller matches on it to log+continue rather than fail the tick.
//
// A panic inside fn propagates through the deferred Rollback, so a
// process crashing mid-tx leaves no half-built rows behind. WithTx
// does not recover the panic; the goroutine continues to unwind.
//
// WithTx is the single transaction primitive new code should reach
// for. Callers composing multiple mutations into one atomic unit
// (the scheduler reservation tx — UpsertPending + TryAcquireLocks +
// TransitionAgent — is the canonical case, issue #88) MUST use
// WithTx so the IMMEDIATE-mode and rollback discipline stay in one
// place. Existing single-method BeginTx call sites in package state
// predate this helper and remain correct.
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

