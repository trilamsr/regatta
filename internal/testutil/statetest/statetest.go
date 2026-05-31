// Package statetest centralizes the *state.DB harness shared by
// scheduler, reaper, and any other package that needs a fresh
// file-backed sqlite DB with migrations applied. Keeping the helper
// out of state's test package avoids the import cycle that would
// otherwise force every consumer to duplicate the Open + Cleanup
// pair.
package statetest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// OpenDB returns a fresh *state.DB rooted at t.TempDir() with the
// standard DSN. The DB is closed automatically on test cleanup.
func OpenDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "state.db")))
	if err != nil {
		t.Fatalf("statetest.OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
