//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestMigrationReplay_DeterministicSchema asserts running the forward-only migration set twice across two fresh SQLite databases yields byte-identical schema dumps (R-MEGA-3 INT-4).
//
// Brief originally specified UP→DOWN→UP byte-equal; this repo's migrations are
// forward-only (no Down SQL shipped under internal/orchestrator/state/migrations).
// The deterministic-replay variant is the testable property: a non-deterministic
// migration (e.g. one that injects a timestamp into the schema) would diverge.
func TestMigrationReplay_DeterministicSchema(t *testing.T) {
	dump1 := freshMigrateAndDump(t)
	dump2 := freshMigrateAndDump(t)
	if dump1 != dump2 {
		t.Fatalf("schema diverged across fresh-DB replays:\n--- run1 ---\n%s\n--- run2 ---\n%s", dump1, dump2)
	}
	// Sanity: the dump must mention the latest schema bump anchor so a
	// silently-truncated migration set surfaces here too.
	if !strings.Contains(dump1, "scheduler_backoff") {
		t.Errorf("schema dump missing scheduler_backoff (migration 0024) — migration set truncated?\n%s", dump1)
	}
}

func freshMigrateAndDump(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "regatta.db")
	ctx := context.Background()
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	// state.Open already runs migrations; dump pragma table_info for every table.
	rawDB := db.SQL()
	tables, err := listTables(ctx, rawDB)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var b strings.Builder
	for _, tbl := range tables {
		fmt.Fprintf(&b, "TABLE %s\n", tbl)
		rows, err := rawDB.QueryContext(ctx, "PRAGMA table_info("+tbl+")")
		if err != nil {
			t.Fatalf("table_info %s: %v", tbl, err)
		}
		for rows.Next() {
			var (
				cid     int
				name    string
				ctype   string
				notnull int
				dflt    sql.NullString
				pk      int
			)
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				t.Fatalf("scan: %v", err)
			}
			fmt.Fprintf(&b, "  %d %s %s nn=%d dflt=%q pk=%d\n", cid, name, ctype, notnull, dflt.String, pk)
		}
		rows.Close()
	}
	return b.String()
}

func listTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'goose_%'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}
