package dbtest

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
)

// QueryCounter wraps *sql.DB to tally Exec/Query/QueryRow calls so tests can
// assert per-render SQL budgets (spec §4 R7: ≤ 2 queries per /runs/{run_id}).
//
// Explicit composition (NOT embedding) keeps the surface narrow: only the six
// query-issuing methods route through the counter; PingContext, Stats, and
// future *sql.DB methods are intentionally NOT promoted so production code
// can't accidentally bypass the counter via an unwrapped method.
type QueryCounter struct {
	db     *sql.DB
	reads  atomic.Int64
	writes atomic.Int64
}

// NewQueryCounter wraps db; the returned counter starts at zero.
func NewQueryCounter(db *sql.DB) *QueryCounter {
	return &QueryCounter{db: db}
}

// Exec increments WriteCount and delegates to the underlying *sql.DB.
func (q *QueryCounter) Exec(query string, args ...any) (sql.Result, error) {
	q.writes.Add(1)
	return q.db.Exec(query, args...)
}

// ExecContext increments WriteCount and delegates to the underlying *sql.DB.
func (q *QueryCounter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	q.writes.Add(1)
	return q.db.ExecContext(ctx, query, args...)
}

// Query increments ReadCount and delegates to the underlying *sql.DB.
func (q *QueryCounter) Query(query string, args ...any) (*sql.Rows, error) {
	q.reads.Add(1)
	return q.db.Query(query, args...)
}

// QueryContext increments ReadCount and delegates to the underlying *sql.DB.
func (q *QueryCounter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.reads.Add(1)
	return q.db.QueryContext(ctx, query, args...)
}

// QueryRow increments ReadCount and delegates to the underlying *sql.DB.
func (q *QueryCounter) QueryRow(query string, args ...any) *sql.Row {
	q.reads.Add(1)
	return q.db.QueryRow(query, args...)
}

// QueryRowContext increments ReadCount and delegates to the underlying *sql.DB.
func (q *QueryCounter) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	q.reads.Add(1)
	return q.db.QueryRowContext(ctx, query, args...)
}

// Count returns total queries observed since the last Reset.
func (q *QueryCounter) Count() int {
	return int(q.reads.Load() + q.writes.Load())
}

// ReadCount returns Query/QueryContext/QueryRow/QueryRowContext calls.
func (q *QueryCounter) ReadCount() int {
	return int(q.reads.Load())
}

// WriteCount returns Exec/ExecContext calls.
func (q *QueryCounter) WriteCount() int {
	return int(q.writes.Load())
}

// Reset zeroes all counters.
func (q *QueryCounter) Reset() {
	q.reads.Store(0)
	q.writes.Store(0)
}

// fatalReporter is the minimal slice of *testing.T the budget assertion needs.
// Tests inject a recording stub; production callers pass *testing.T directly.
type fatalReporter interface {
	Helper()
	Fatalf(format string, args ...any)
}

// AssertLE fails t when Count() > budget (LE, not LT — equal-to-budget passes).
func (q *QueryCounter) AssertLE(t *testing.T, budget int) {
	q.assertLE(t, budget)
}

func (q *QueryCounter) assertLE(r fatalReporter, budget int) {
	r.Helper()
	if got := q.Count(); got > budget {
		r.Fatalf("query budget exceeded: got %d queries, want ≤ %d (reads=%d writes=%d)",
			got, budget, q.ReadCount(), q.WriteCount())
	}
}
