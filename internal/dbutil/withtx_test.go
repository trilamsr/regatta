package dbutil_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/dbutil"
)

// newDB returns a private file-backed sqlite for the test. WAL is left
// default; the tests below assert tx-discipline, not WAL semantics.
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	// dbutil sits below state in the package graph (state imports dbutil); importing state.DSN would cycle. Minimal sqlite DSN for tx-discipline tests.
	dsn := "file:" + filepath.Join(t.TempDir(), "withtx.db") +
		"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)" // allow-bare-pragma: cycle-prevention
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

// TestWithTx_NilReturnCommits pins the happy-path commit contract.
func TestWithTx_NilReturnCommits(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()

	err := dbutil.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES ('keep')`)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v='keep'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows=%d want 1 — commit did not persist", n)
	}
}

// TestWithTx_ErrorReturnRollsBack pins fn-error rollback + sentinel passthrough.
func TestWithTx_ErrorReturnRollsBack(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	sentinel := errors.New("fn rejected")

	err := dbutil.WithTx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES ('drop')`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx err=%v want %v (unwrapped passthrough)", err, sentinel)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v='drop'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows=%d want 0 — rollback did not discard writes", n)
	}
}

// TestWithTx_PanicRollsBack guards mid-tx panic safety.
func TestWithTx_PanicRollsBack(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("expected panic to propagate")
			}
		}()
		_ = dbutil.WithTx(ctx, db, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES ('panic')`); err != nil {
				t.Fatalf("insert: %v", err)
			}
			panic("simulated mid-tx crash")
		})
	}()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t WHERE v='panic'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows=%d want 0 — panic-mid-tx left orphan row", n)
	}
}

// TestWithTx_BeginTxFails_ReturnsError pins the begin-failure path.
func TestWithTx_BeginTxFails_ReturnsError(t *testing.T) {
	sentinel := errors.New("simulated begin failure")
	b := &fakeBeginner{err: sentinel}

	called := false
	err := dbutil.WithTx(context.Background(), b, func(*sql.Tx) error {
		called = true
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx err=%v want wraps %v", err, sentinel)
	}
	if called {
		t.Fatalf("fn must not run when BeginTx fails")
	}
}

type fakeBeginner struct{ err error }

func (f *fakeBeginner) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	return nil, f.err
}
