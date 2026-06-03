// Package dbutil holds tiny *sql.DB-shaped helpers shared by callers
// that do not import internal/orchestrator/state — keeps the
// inline-BeginTx + defer-Rollback + Commit dance in one place so a
// forgotten Rollback (resource leak) or forgotten Commit (silent
// rollback) cannot regress per-callsite. Mirrors the (*state.DB).WithTx
// method for the state-internal call sites.
package dbutil

import (
	"context"
	"database/sql"
	"fmt"
)

// Beginner is the minimal BeginTx surface WithTx needs. *sql.DB
// satisfies it; tests inject counting wrappers (see cel_decider) to
// assert one-tx invariants without sniffing internal state.
type Beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// WithTx runs fn inside a single transaction opened on db. Commits on
// nil return, rolls back on any non-nil return or panic. Sentinel
// errors from fn pass through unwrapped so callers can errors.Is
// against package sentinels (mirrors state.DB.WithTx's contract).
//
// A panic inside fn propagates through the deferred Rollback, so a
// process crashing mid-tx leaves no half-built rows behind. WithTx
// does not recover the panic.
func WithTx(ctx context.Context, db Beginner, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dbutil: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dbutil: commit tx: %w", err)
	}
	return nil
}
