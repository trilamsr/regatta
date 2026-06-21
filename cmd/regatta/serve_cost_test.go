package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestBuildSpawner_ClaudeWiresCostCallback — HMAC key present wires OnResultEventFor; absent keeps it nil.
func TestBuildSpawner_ClaudeWiresCostCallback(t *testing.T) {
	tmp := t.TempDir()
	db := openTempDB(t, tmp)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	set, err := buildSpawner("claude", tmp, "claude", "HEAD", logger, db,
		[]byte("0123456789abcdef0123456789abcdef"), "k1", nil)
	if err != nil {
		t.Fatalf("buildSpawner with key: %v", err)
	}
	cs, ok := set.Spawner.(*spawner.ClaudeSpawner)
	if !ok {
		t.Fatalf("set.Spawner = %T; want *spawner.ClaudeSpawner", set.Spawner)
	}
	if cs.Config().OnResultEventFor == nil {
		t.Fatalf("OnResultEventFor nil when HMAC key supplied; wiring did not land")
	}

	set, err = buildSpawner("claude", tmp, "claude", "HEAD", logger, db, nil, "", nil)
	if err != nil {
		t.Fatalf("buildSpawner no-key: %v", err)
	}
	cs, ok = set.Spawner.(*spawner.ClaudeSpawner)
	if !ok {
		t.Fatalf("set.Spawner = %T; want *spawner.ClaudeSpawner", set.Spawner)
	}
	if cs.Config().OnResultEventFor != nil {
		t.Fatalf("OnResultEventFor wired with no HMAC key; want nil")
	}
}

func openTempDB(t *testing.T, tmp string) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(tmp, "subs.db")))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
