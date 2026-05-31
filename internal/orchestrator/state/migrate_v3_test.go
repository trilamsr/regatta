package state

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrate_V2ToV3(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite", DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	if err := Migrate(ctx, raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, tbl := range []string{"work_item_outputs", "work_item_edges"} {
		var name string
		row := raw.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl)
		if err := row.Scan(&name); err != nil {
			t.Fatalf("table %s missing after migrate: %v", tbl, err)
		}
	}

	for _, idx := range []string{
		"idx_work_item_outputs_wi",
		"idx_work_item_edges_from",
		"idx_work_item_edges_to",
	} {
		var name string
		row := raw.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx)
		if err := row.Scan(&name); err != nil {
			t.Fatalf("index %s missing after migrate: %v", idx, err)
		}
	}

	if err := Migrate(ctx, raw); err != nil {
		t.Fatalf("migrate twice: %v", err)
	}
}
