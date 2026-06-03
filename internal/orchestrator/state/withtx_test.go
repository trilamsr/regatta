package state

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// TestWithTx_BeginTxFails_ReturnsError pins begin-failure path: closed DB → wrapped error, fn never invoked.
func TestWithTx_BeginTxFails_ReturnsError(t *testing.T) {
	db := newTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	called := false
	err := db.WithTx(context.Background(), func(*sql.Tx) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatalf("WithTx on closed db: want error, got nil")
	}
	if called {
		t.Fatalf("fn must not run when BeginTx fails")
	}
}

// TestWithTx_CommitsOnNilError pins the happy-path commit contract.
func TestWithTx_CommitsOnNilError(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO agents (work_item_id, lane, state, created_at, updated_at)
			 VALUES ('W-1', 'server', 'pending', 1, 1)`)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	var n int
	if err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agents WHERE work_item_id = 'W-1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows=%d want 1 — commit did not persist", n)
	}
}

// TestWithTx_RollsBackOnError pins the failure-path rollback + unwrapped-error contract.
func TestWithTx_RollsBackOnError(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	sentinel := errors.New("fn rejected")
	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO agents (work_item_id, lane, state, created_at, updated_at)
			 VALUES ('W-1', 'server', 'pending', 1, 1)`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx err=%v want %v", err, sentinel)
	}

	var n int
	if err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agents WHERE work_item_id = 'W-1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows=%d want 0 — rollback did not discard writes", n)
	}
}

// TestWithTx_RollsBackOnPanic guards the crash-mid-tx safety property the scheduler relies on.
func TestWithTx_RollsBackOnPanic(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	func() {
		defer func() { _ = recover() }()
		_ = db.WithTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO agents (work_item_id, lane, state, created_at, updated_at)
				 VALUES ('W-1', 'server', 'pending', 1, 1)`); err != nil {
				t.Fatalf("insert: %v", err)
			}
			panic("simulated mid-tx crash")
		})
	}()

	var n int
	if err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agents WHERE work_item_id = 'W-1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows=%d want 0 — panic-mid-tx left orphan row", n)
	}
}
