// Package statetest centralizes the *state.DB harness shared by
// scheduler, reaper, and any other package that needs a fresh
// file-backed sqlite DB with migrations applied. Keeping the helper
// out of state's test package avoids the import cycle that would
// otherwise force every consumer to duplicate the Open + Cleanup
// pair.
package statetest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

// goldenOnce + goldenBytes amortise the ~6ms goose-migration cost
// across hundreds of property-test cases. Migrations run once on the
// first ensureGolden call; every subsequent GoldenClone writes the
// post-migration bytes into a fresh temp file and opens that. Shaves
// ~80% off the per-case wallclock for crash-recovery property runs
// driven by rapid (#382 spec §3.4 budget).
var (
	goldenOnce  sync.Once
	goldenBytes []byte
	goldenErr   error
)

func ensureGolden(t testing.TB) {
	t.Helper()
	goldenOnce.Do(func() {
		f, err := os.CreateTemp("", "statetest-golden-*.db")
		if err != nil {
			goldenErr = fmt.Errorf("create golden: %w", err)
			return
		}
		path := f.Name()
		_ = f.Close()
		defer func() { _ = os.Remove(path) }()
		db, err := state.Open(context.Background(), state.DSN(path))
		if err != nil {
			goldenErr = fmt.Errorf("open golden: %w", err)
			return
		}
		// Disable WAL autocheckpoint at the golden so every clone
		// inherits the same setting (mirrors #382 R5).
		if _, err := db.SQL().ExecContext(context.Background(), `PRAGMA wal_autocheckpoint=0`); err != nil {
			goldenErr = fmt.Errorf("golden pragma: %w", err)
			return
		}
		// Truncate the WAL so the on-disk file is self-contained — no
		// stale -wal sidecar pointing at the closed conn.
		if _, err := db.SQL().ExecContext(context.Background(), `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			goldenErr = fmt.Errorf("golden checkpoint: %w", err)
			return
		}
		if err := db.Close(); err != nil {
			goldenErr = fmt.Errorf("close golden: %w", err)
			return
		}
		goldenBytes, goldenErr = os.ReadFile(path) //nolint:gosec // path is os.CreateTemp output, test-only.
	})
	if goldenErr != nil {
		t.Fatalf("statetest.ensureGolden: %v", goldenErr)
	}
}

// GoldenClone returns a fresh *state.DB at t.TempDir() whose schema
// is the post-migration bytes of a singleton golden file. The clone
// bypasses goose, dropping per-case open from ~6ms to ~0.5ms — load-
// bearing for rapid property tests that open the DB hundreds of
// times. clock=nil uses time.Now.
func GoldenClone(t testing.TB, clock func() time.Time) *state.DB {
	t.Helper()
	ensureGolden(t)
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, goldenBytes, 0o600); err != nil {
		t.Fatalf("statetest.GoldenClone: write clone: %v", err)
	}
	if clock == nil {
		clock = time.Now
	}
	db, err := state.OpenWithClock(context.Background(), state.DSN(path), clock)
	if err != nil {
		t.Fatalf("statetest.GoldenClone: open clone: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
