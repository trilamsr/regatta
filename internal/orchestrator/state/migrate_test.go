package state_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func TestMigrate_EmptyDBAppliesAllVersions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	raw.SetMaxOpenConns(1)

	if err := state.Migrate(context.Background(), raw); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var version int64
	if err := raw.QueryRow("SELECT MAX(version_id) FROM goose_db_version").Scan(&version); err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	if version != state.CurrentSchemaVersion {
		t.Fatalf("version=%d want %d", version, state.CurrentSchemaVersion)
	}

	var work int
	if err := raw.QueryRow("SELECT COUNT(*) FROM work_items").Scan(&work); err != nil {
		t.Fatalf("work_items table missing: %v", err)
	}
}

func TestMigrate_IdempotentOnSecondCall(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	raw.SetMaxOpenConns(1)

	if err := state.Migrate(context.Background(), raw); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := state.Migrate(context.Background(), raw); err != nil {
		t.Fatalf("second Migrate (should be no-op): %v", err)
	}
}

// TestMigrate_NoIfNotExistsInGooseManagedDDL pins goose-managed DDL: no IF NOT EXISTS in CREATE TABLE/INDEX.
func TestMigrate_NoIfNotExistsInGooseManagedDDL(t *testing.T) {
	migDir := "migrations"
	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatalf("read %s: %v", migDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		path := filepath.Join(migDir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(strings.ToUpper(string(body)), "IF NOT EXISTS") {
			t.Fatalf("%s: goose-managed migrations must not contain IF NOT EXISTS", path)
		}
	}
}

func TestMigrate_DowngradeResistance(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	raw.SetMaxOpenConns(1)

	if err := state.Migrate(context.Background(), raw); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := raw.Exec("INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (99, 1, CURRENT_TIMESTAMP)"); err != nil {
		t.Fatalf("inject future version: %v", err)
	}

	err = state.Migrate(context.Background(), raw)
	if !errors.Is(err, state.ErrSchemaTooNew) {
		t.Fatalf("err=%v want ErrSchemaTooNew", err)
	}
}
