// Package dbutil holds tiny *sql.DB-shaped helpers shared by callers
// that do not import internal/orchestrator/state. Keeps the inline-
// BeginTx + defer-Rollback + Commit dance in one place so a forgotten
// Rollback (resource leak) or Commit (silent rollback) cannot regress
// per-callsite. Mirrors (*state.DB).WithTx for state-internal sites.
package dbutil

import (
	"context"
	"database/sql"
	"fmt"
)

// Beginner is the minimal BeginTx surface WithTx needs; *sql.DB
// satisfies it. Tests inject counting wrappers to assert one-tx
// invariants without sniffing internal state.
type Beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// WithTx runs fn inside a single transaction; commits on nil return,
// rolls back on any non-nil return or panic. Sentinel errors from fn
// pass through unwrapped so callers can errors.Is against package
// sentinels. A panic propagates through the deferred Rollback —
// WithTx does not recover.
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
