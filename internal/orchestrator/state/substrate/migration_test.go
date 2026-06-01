package substrate_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestMigration0005_SubstrateAppliesAndCreatesSchema pins the substrate_events DDL shape per spec §2.1.
func TestMigration0005_SubstrateAppliesAndCreatesSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "subs.db")
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
		t.Fatalf("schema version=%d want %d", version, state.CurrentSchemaVersion)
	}

	// All 19 columns present.
	want := map[string]bool{
		"id": false, "run_id": false, "work_item_id": false,
		"tenant_id": false, "trace_id": false, "span_id": false,
		"kind": false, "key": false, "payload_json": false,
		"blob_digest": false, "supersedes": false, "written_by": false,
		"written_at": false, "schema_version": false, "nonce": false,
		"sig_alg": false, "sig_key_id": false, "sig_mac": false,
	}
	rows, err := raw.Query(`PRAGMA table_info(substrate_events)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for col, seen := range want {
		if !seen {
			t.Errorf("substrate_events missing column %q", col)
		}
	}

	// Five non-unique indexes + 1 unique index expected.
	idxRows, err := raw.Query(`SELECT name, "unique" FROM pragma_index_list('substrate_events')`)
	if err != nil {
		t.Fatalf("index_list: %v", err)
	}
	defer func() { _ = idxRows.Close() }()
	idxs := map[string]bool{} // name -> unique?
	for idxRows.Next() {
		var name string
		var uniq int
		if err := idxRows.Scan(&name, &uniq); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		idxs[name] = uniq == 1
	}
	for _, idx := range []string{
		"idx_substrate_events_kind",
		"idx_substrate_events_wi",
		"idx_substrate_events_tenant",
		"idx_substrate_events_supersedes",
		"idx_substrate_events_trace",
	} {
		if _, ok := idxs[idx]; !ok {
			t.Errorf("missing index %q", idx)
		}
	}
	uniq, ok := idxs["uq_substrate_events_nonce"]
	if !ok {
		t.Errorf("missing unique index uq_substrate_events_nonce")
	} else if !uniq {
		t.Errorf("uq_substrate_events_nonce is not UNIQUE")
	}

	// Supersedes FK targets substrate_events(id).
	fkRows, err := raw.Query(`SELECT "table", "from", "to" FROM pragma_foreign_key_list('substrate_events')`)
	if err != nil {
		t.Fatalf("fk_list: %v", err)
	}
	defer func() { _ = fkRows.Close() }()
	fkFound := false
	for fkRows.Next() {
		var tbl, from, to string
		if err := fkRows.Scan(&tbl, &from, &to); err != nil {
			t.Fatalf("scan fk: %v", err)
		}
		if tbl == "substrate_events" && from == "supersedes" && to == "id" {
			fkFound = true
		}
	}
	if !fkFound {
		t.Errorf("supersedes FK -> substrate_events(id) missing")
	}
}
