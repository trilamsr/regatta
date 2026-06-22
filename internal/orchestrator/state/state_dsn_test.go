package state

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestDSN_HasWALPragma asserts DSN enables WAL so readers don't block on writers (R24).
func TestDSN_HasWALPragma(t *testing.T) {
	got := DSN("/tmp/x.db")
	if !strings.Contains(got, "journal_mode(WAL)") {
		t.Fatalf("DSN missing journal_mode(WAL): %q", got)
	}
}

// TestDSN_HasSynchronousNormalPragma asserts the WAL-safe synchronous=NORMAL pair (R24).
func TestDSN_HasSynchronousNormalPragma(t *testing.T) {
	got := DSN("/tmp/x.db")
	if !strings.Contains(got, "synchronous(NORMAL)") {
		t.Fatalf("DSN missing synchronous(NORMAL): %q", got)
	}
}

// TestOpen_RuntimePragmaIsWAL proves the DSN actually flips sqlite's journal_mode to WAL at runtime (R24).
func TestOpen_RuntimePragmaIsWAL(t *testing.T) {
	db := openTestDBWithProdDSN(t)
	var mode string
	if err := db.SQL().QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode=%q want %q", mode, "wal")
	}
}

func openTestDBWithProdDSN(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), DSN(filepath.Join(t.TempDir(), "state.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
