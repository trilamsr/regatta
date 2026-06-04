package statetest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestOpenDBWithPath_ReturnsUsableDSN asserts the (db, path) pair re-opens via state.DSN(path) (G1 consolidation).
func TestOpenDBWithPath_ReturnsUsableDSN(t *testing.T) {
	db, path := statetest.OpenDBWithPath(t)
	if db == nil {
		t.Fatal("nil db")
	}
	if !strings.HasSuffix(path, "state.db") {
		t.Fatalf("path=%q; want suffix state.db", path)
	}
	// Confirm path drives a fresh Open round-trip — caller pattern from
	// cmd/regatta/merge_status_test.go re-opens via state.DSN(path) to
	// exercise CLI substrate.
	reopened, err := state.Open(context.Background(), state.DSN(path))
	if err != nil {
		t.Fatalf("reopen via DSN(path): %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
}
