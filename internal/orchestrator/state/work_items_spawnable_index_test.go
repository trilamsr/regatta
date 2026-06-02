package state_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestSpawnableIndex_PlanShowsSearch pins migration 0007: EXPLAIN QUERY PLAN must report SEARCH (index lookup) on the planned-rows scan, never SCAN.
func TestSpawnableIndex_PlanShowsSearch(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", state.DSN(filepath.Join(t.TempDir(), "qp.db")))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	raw.SetMaxOpenConns(1)
	if err := state.Migrate(ctx, raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Seed 1000 work_items so sqlite prefers an index over a scan;
	// the planner ignores indexes for small tables. ListSpawnable
	// filters status='planned', so seed mostly-merged rows with a
	// minority of planned to mirror the archived-DB shape the index
	// is designed for.
	tx, err := raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO work_items
		(id, kind, title, lane, status, source, last_seen_at, created_at, updated_at)
		VALUES (?, 'feature', ?, 'server', ?, 'brief', 0, 0, 0)`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for i := 0; i < 1000; i++ {
		status := "merged"
		if i%50 == 0 {
			status = "planned"
		}
		id := fmt.Sprintf("F-%05d", i)
		if _, err := stmt.ExecContext(ctx, id, id, status); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatalf("stmt close: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// ANALYZE so the query planner has real stats to pick the index.
	if _, err := raw.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// EXPLAIN QUERY PLAN on the planned-rows lookup that ListSpawnable
	// performs in its outermost loop. We deliberately omit the agents
	// LEFT JOIN and dep/edge subqueries — the work_items access path is
	// the load-bearing piece this migration optimises.
	rows, err := raw.QueryContext(ctx,
		`EXPLAIN QUERY PLAN SELECT id FROM work_items WHERE status = 'planned'`)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plan = append(plan, detail)
	}
	joined := strings.Join(plan, " | ")

	if !strings.Contains(joined, "SEARCH") || !strings.Contains(joined, "idx_work_items_spawnable") {
		t.Fatalf("EXPLAIN QUERY PLAN must show SEARCH using idx_work_items_spawnable; got: %s", joined)
	}
	if strings.Contains(joined, "SCAN work_items") {
		t.Fatalf("EXPLAIN QUERY PLAN still shows SCAN work_items (full-table scan); got: %s", joined)
	}
}

// TestSpawnableIndex_PresentAfterMigrate pins that migration 0007 creates idx_work_items_spawnable in sqlite_master.
func TestSpawnableIndex_PresentAfterMigrate(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", state.DSN(filepath.Join(t.TempDir(), "idx.db")))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	raw.SetMaxOpenConns(1)
	if err := state.Migrate(ctx, raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var name string
	row := raw.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_work_items_spawnable'`)
	if err := row.Scan(&name); err != nil {
		t.Fatalf("idx_work_items_spawnable missing after migrate: %v", err)
	}
	if name != "idx_work_items_spawnable" {
		t.Fatalf("name=%q want idx_work_items_spawnable", name)
	}

	// Schema version must have advanced to include migration 0007.
	var version int64
	if err := raw.QueryRow("SELECT MAX(version_id) FROM goose_db_version").Scan(&version); err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	if version != state.CurrentSchemaVersion {
		t.Fatalf("version=%d want %d (CurrentSchemaVersion)", version, state.CurrentSchemaVersion)
	}
	if state.CurrentSchemaVersion < 7 {
		t.Fatalf("CurrentSchemaVersion=%d want >= 7", state.CurrentSchemaVersion)
	}
}
