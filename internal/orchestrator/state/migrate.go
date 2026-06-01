// Package state's migration runner wraps pressly/goose to apply
// versioned forward-only migrations from the embedded migrations/
// directory. Open() calls Migrate(); callers should not invoke this
// directly outside tests.
package state

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate mutates goose package globals (SetBaseFS, SetLogger, SetDialect);
// not safe to call concurrently from multiple goroutines with different settings.
//
// Migrate applies every pending forward migration to db. Returns
// ErrSchemaTooNew (wrapped) when the database has been touched by
// a newer binary; the operator must upgrade rather than downgrade.
func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("state: goose dialect: %w", err)
	}

	current, err := goose.GetDBVersionContext(ctx, db)
	if err != nil && !errors.Is(err, goose.ErrNoNextVersion) {
		return fmt.Errorf("state: read goose version: %w", err)
	}
	if current > CurrentSchemaVersion {
		return fmt.Errorf("%w: db=%d binary=%d", ErrSchemaTooNew, current, CurrentSchemaVersion)
	}

	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("state: goose up: %w", err)
	}
	return nil
}
