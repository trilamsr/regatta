package dbtest_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
	// Blank-import rapid so the test binary registers -rapid.checks; the
	// repo-wide `make property-test` runs that flag across every package
	// under ./internal/orchestrator/state/... and unknown-flag aborts here
	// would block pre-push-check even though dbtest itself has no rapid tests.
	_ "pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state/dbtest"
)

// newMemDB returns a fresh in-memory sqlite *sql.DB with one tiny table the
// counter tests can hit without exercising any production schema. Pool capped
// at 1 connection so the concurrency test doesn't fight sqlite's per-connection
// :memory: isolation.
func newMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestQueryCounter_IncrementsOnEveryExec(t *testing.T) {
	db := newMemDB(t)
	qc := dbtest.NewQueryCounter(db)

	for i := 0; i < 3; i++ {
		if _, err := qc.Exec(`INSERT INTO t(v) VALUES(?)`, "x"); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	if got := qc.Count(); got != 3 {
		t.Fatalf("Count = %d, want 3", got)
	}
	if got := qc.WriteCount(); got != 3 {
		t.Fatalf("WriteCount = %d, want 3", got)
	}
	if got := qc.ReadCount(); got != 0 {
		t.Fatalf("ReadCount = %d, want 0", got)
	}
}

func TestQueryCounter_SeparatesReadsAndWrites(t *testing.T) {
	db := newMemDB(t)
	qc := dbtest.NewQueryCounter(db)
	ctx := context.Background()

	if _, err := qc.ExecContext(ctx, `INSERT INTO t(v) VALUES(?)`, "a"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if _, err := qc.Exec(`INSERT INTO t(v) VALUES(?)`, "b"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	rows, err := qc.QueryContext(ctx, `SELECT v FROM t`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	_ = rows.Close()
	rows2, err := qc.Query(`SELECT v FROM t`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	_ = rows2.Close()
	row := qc.QueryRowContext(ctx, `SELECT v FROM t LIMIT 1`)
	var v string
	_ = row.Scan(&v)
	row2 := qc.QueryRow(`SELECT v FROM t LIMIT 1`)
	_ = row2.Scan(&v)

	if got := qc.WriteCount(); got != 2 {
		t.Fatalf("WriteCount = %d, want 2", got)
	}
	if got := qc.ReadCount(); got != 4 {
		t.Fatalf("ReadCount = %d, want 4", got)
	}
	if got := qc.Count(); got != 6 {
		t.Fatalf("Count = %d, want 6", got)
	}
}

func TestQueryCounter_ResetZeroesAll(t *testing.T) {
	db := newMemDB(t)
	qc := dbtest.NewQueryCounter(db)

	if _, err := qc.Exec(`INSERT INTO t(v) VALUES(?)`, "x"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	rows, err := qc.Query(`SELECT v FROM t`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	_ = rows.Close()

	if qc.Count() == 0 {
		t.Fatalf("Count zero before Reset; want non-zero")
	}

	qc.Reset()

	if got := qc.Count(); got != 0 {
		t.Fatalf("Count after Reset = %d, want 0", got)
	}
	if got := qc.ReadCount(); got != 0 {
		t.Fatalf("ReadCount after Reset = %d, want 0", got)
	}
	if got := qc.WriteCount(); got != 0 {
		t.Fatalf("WriteCount after Reset = %d, want 0", got)
	}
}

// recReporter is a FatalReporter stub that captures Fatalf instead of aborting.
type recReporter struct {
	failed bool
}

func (r *recReporter) Helper()                           {}
func (r *recReporter) Fatalf(format string, args ...any) { r.failed = true }

func TestQueryCounter_AssertLEPassesWhenUnderBudget(t *testing.T) {
	db := newMemDB(t)
	qc := dbtest.NewQueryCounter(db)

	for i := 0; i < 2; i++ {
		if _, err := qc.Exec(`INSERT INTO t(v) VALUES(?)`, "x"); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}

	// Boundary: equal-to-budget passes (LE not LT). If this fires, the outer
	// test fails — pinning the boundary is the whole point.
	qc.AssertLE(t, 2)

	// Over budget must fire t.Fatalf. Inject a recording stub to verify
	// without aborting the parent.
	if _, err := qc.Exec(`INSERT INTO t(v) VALUES(?)`, "x"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	rec := &recReporter{}
	qc.AssertLEReporter(rec, 2)
	if !rec.failed {
		t.Fatalf("AssertLE over budget did not call Fatalf (Count=%d, budget=2)", qc.Count())
	}
}

func TestQueryCounter_ConcurrentSafety(t *testing.T) {
	db := newMemDB(t)
	qc := dbtest.NewQueryCounter(db)

	const goroutines = 100
	const callsPerG = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerG; j++ {
				if _, err := qc.Exec(`INSERT INTO t(v) VALUES(?)`, "x"); err != nil {
					t.Errorf("exec: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if got, want := qc.Count(), goroutines*callsPerG; got != want {
		t.Fatalf("Count = %d, want %d (no torn writes)", got, want)
	}
}
