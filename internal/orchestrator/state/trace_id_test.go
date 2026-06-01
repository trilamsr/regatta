package state

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// traceCtx returns ctx wrapped in a fresh in-memory span so insert
// paths see a valid SpanContextFromContext. Returns the cleanup the
// caller defers and the expected 32-hex trace_id.
func traceCtx(t *testing.T) (context.Context, func(), string) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	tr := tp.Tracer("trace_id_test")
	ctx, span := tr.Start(context.Background(), "op")
	wantHex := span.SpanContext().TraceID().String()
	if len(wantHex) != 32 {
		t.Fatalf("trace_id hex len=%d want 32", len(wantHex))
	}
	if _, err := hex.DecodeString(wantHex); err != nil {
		t.Fatalf("trace_id is not hex: %v", err)
	}
	cleanup := func() {
		span.End()
		_ = tp.Shutdown(context.Background())
	}
	return ctx, cleanup, wantHex
}

// TestMigration0005_AddsTraceIDColumns pins the schema effect of
// migration 0005: both work_items and approval_events grow a
// trace_id TEXT NOT NULL DEFAULT '' column, and both partial indexes
// exist. Schema-version bumps to 5.
func TestMigration0005_AddsTraceIDColumns(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	var v int64
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT MAX(version_id) FROM goose_db_version`).Scan(&v); err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	if v < 5 {
		t.Fatalf("schema version=%d want >= 5 (migration 0005 must apply)", v)
	}

	for _, table := range []string{"work_items", "approval_events"} {
		col := columnInfo(t, db.SQL(), table, "trace_id")
		if col == nil {
			t.Fatalf("%s.trace_id column missing after migrate", table)
		}
		if col.Type != "TEXT" {
			t.Errorf("%s.trace_id type=%q want TEXT", table, col.Type)
		}
		if col.NotNull != 1 {
			t.Errorf("%s.trace_id notnull=%d want 1", table, col.NotNull)
		}
		if col.DefaultValue != "''" {
			t.Errorf("%s.trace_id default=%q want \"''\"", table, col.DefaultValue)
		}
	}
	for _, idx := range []string{"idx_work_items_trace", "idx_approval_events_trace"} {
		var name string
		if err := db.SQL().QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name); err != nil {
			t.Fatalf("index %q missing after migrate: %v", idx, err)
		}
	}
}

// TestWorkItemInsert_PersistsTraceIDFromContext: insert from within an
// active span populates work_items.trace_id with the active TraceID hex.
// Spec §3.5 + §7 A4.
func TestWorkItemInsert_PersistsTraceIDFromContext(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx, done, wantHex := traceCtx(t)
	defer done()

	item := WorkItem{
		ID: "WI-trace", Kind: KindFeature, Title: "trace test",
		Lane: "server", Status: WorkStatusPlanned,
	}
	if err := db.UpsertWorkItem(ctx, item, SourceBrief, t0); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}

	var got string
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT trace_id FROM work_items WHERE id = ?`, item.ID).Scan(&got); err != nil {
		t.Fatalf("scan trace_id: %v", err)
	}
	if got != wantHex {
		t.Fatalf("work_items.trace_id=%q want %q", got, wantHex)
	}
}

// TestApprovalEvent_PersistsTraceIDFromContext: same invariant for
// approval_events. CreateApproval seeds the parent row; the event
// row is the one that carries the trace_id (matches §3.5 schema —
// trace_id sits on approval_events, the audit log, not on approvals).
func TestApprovalEvent_PersistsTraceIDFromContext(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx, done, wantHex := traceCtx(t)
	defer done()

	seedApprovalWorkItem(t, db, "F-trace")
	if err := db.CreateApproval(ctx, sampleApproval("a-trace000001", "F-trace", t0, t0.Add(time.Hour))); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	if err := db.AppendApprovalEvent(ctx, ApprovalEvent{
		ApprovalID: "a-trace000001",
		Ts:         t0,
		Kind:       "requested",
		Actor:      "system",
		Payload:    json.RawMessage(`{}`),
		TokenJTI:   "",
	}); err != nil {
		t.Fatalf("AppendApprovalEvent: %v", err)
	}

	var got string
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT trace_id FROM approval_events WHERE approval_id = ?`,
		"a-trace000001").Scan(&got); err != nil {
		t.Fatalf("scan trace_id: %v", err)
	}
	if got != wantHex {
		t.Fatalf("approval_events.trace_id=%q want %q", got, wantHex)
	}
}

// TestNoActiveSpan_PersistsEmptyTraceID: insert outside any span sets
// trace_id to '' — pinning legacy-path nondisruption (operators
// running without OTel get the same byte-level behaviour as MVP-2,
// rubric §7 B2).
func TestNoActiveSpan_PersistsEmptyTraceID(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()

	item := WorkItem{
		ID: "WI-nospan", Kind: KindFeature, Title: "no span",
		Lane: "server", Status: WorkStatusPlanned,
	}
	if err := db.UpsertWorkItem(ctx, item, SourceBrief, t0); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}

	seedApprovalWorkItem(t, db, "F-nospan")
	if err := db.CreateApproval(ctx, sampleApproval("a-nospan00001", "F-nospan", t0, t0.Add(time.Hour))); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	if err := db.AppendApprovalEvent(ctx, ApprovalEvent{
		ApprovalID: "a-nospan00001",
		Ts:         t0,
		Kind:       "requested",
		Actor:      "system",
	}); err != nil {
		t.Fatalf("AppendApprovalEvent: %v", err)
	}

	var wi, ev string
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT trace_id FROM work_items WHERE id = ?`, item.ID).Scan(&wi); err != nil {
		t.Fatalf("scan work_item trace_id: %v", err)
	}
	if wi != "" {
		t.Fatalf("work_items.trace_id=%q want \"\" with no active span", wi)
	}
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT trace_id FROM approval_events WHERE approval_id = ?`,
		"a-nospan00001").Scan(&ev); err != nil {
		t.Fatalf("scan approval_event trace_id: %v", err)
	}
	if ev != "" {
		t.Fatalf("approval_events.trace_id=%q want \"\" with no active span", ev)
	}
}

// TestMigration0005_BackwardCompatible: a fixture DB carrying MVP-2 rows
// (inserted at schema version 4 before 0005 ran) gets trace_id='' on
// migration, and downstream reads succeed. Single-tenant deploys must
// be non-disrupted by the bump.
func TestMigration0005_BackwardCompatible(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	dsn := DSN(dbPath)
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	raw.SetMaxOpenConns(1)
	if err := raw.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Apply migrations through 0004 only by hand-rolling the same
	// DDL the shipped files emit. We rebuild the relevant subset
	// rather than calling Migrate(), which would jump to v5.
	mustExec(t, raw, `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`)
	mustExec(t, raw, `INSERT INTO goose_db_version (version_id, is_applied) VALUES
		(0, 1), (1, 1), (2, 1), (3, 1), (4, 1)`)
	mustExec(t, raw, `CREATE TABLE work_items (
		id TEXT NOT NULL PRIMARY KEY, kind TEXT NOT NULL, title TEXT NOT NULL,
		lane TEXT NOT NULL, status TEXT NOT NULL, parent_program_id TEXT,
		depends_on_features TEXT NOT NULL DEFAULT '[]',
		acceptance_json TEXT NOT NULL DEFAULT '[]', source TEXT NOT NULL,
		last_seen_at INTEGER NOT NULL, created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL)`)
	mustExec(t, raw, `CREATE TABLE approvals (
		id TEXT NOT NULL PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id),
		gate_name TEXT NOT NULL, requested_at INTEGER NOT NULL, requested_by TEXT NOT NULL,
		reviewer_set_snapshot_json TEXT NOT NULL, quorum INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL DEFAULT 'pending', decided_at INTEGER,
		decided_by TEXT NOT NULL DEFAULT '[]', callback_token_hmac_sig TEXT NOT NULL DEFAULT '',
		timeout_at INTEGER NOT NULL, on_timeout TEXT NOT NULL DEFAULT 'fail',
		escalation_chain_json TEXT NOT NULL DEFAULT '[]',
		created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
		UNIQUE(work_item_id, gate_name))`)
	mustExec(t, raw, `CREATE TABLE approval_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		approval_id TEXT NOT NULL REFERENCES approvals(id),
		ts INTEGER NOT NULL, kind TEXT NOT NULL,
		actor TEXT NOT NULL, payload_json TEXT NOT NULL DEFAULT '{}',
		token_jti TEXT NOT NULL DEFAULT '')`)

	// Seed one legacy work_item + one legacy approval_event.
	now := time.Now().UTC().Unix()
	mustExec(t, raw, `INSERT INTO work_items
		(id, kind, title, lane, status, source, last_seen_at, created_at, updated_at)
		VALUES ('LEGACY-1', 'feature', 'legacy', 'server', 'planned', 'brief', ?, ?, ?)`,
		now, now, now)
	mustExec(t, raw, `INSERT INTO approvals
		(id, work_item_id, gate_name, requested_at, requested_by,
		 reviewer_set_snapshot_json, quorum, status, decided_by,
		 timeout_at, created_at, updated_at)
		VALUES ('a-legacy00001', 'LEGACY-1', 'g', ?, 'sys',
		 '{}', 1, 'pending', '[]', ?, ?, ?)`,
		now, now+3600, now, now)
	mustExec(t, raw, `INSERT INTO approval_events
		(approval_id, ts, kind, actor, payload_json, token_jti)
		VALUES ('a-legacy00001', ?, 'requested', 'system', '{}', '')`,
		now)
	if err := raw.Close(); err != nil {
		t.Fatalf("close pre-migrate: %v", err)
	}

	// Reopen — Open() runs Migrate() which advances to v5.
	db, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open after legacy seed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var v int64
	if err := db.SQL().QueryRowContext(context.Background(),
		`SELECT MAX(version_id) FROM goose_db_version`).Scan(&v); err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	if v != CurrentSchemaVersion {
		t.Fatalf("post-migrate version=%d want %d", v, CurrentSchemaVersion)
	}

	var wi, ev string
	if err := db.SQL().QueryRowContext(context.Background(),
		`SELECT trace_id FROM work_items WHERE id = ?`, "LEGACY-1").Scan(&wi); err != nil {
		t.Fatalf("read legacy work_item trace_id: %v", err)
	}
	if wi != "" {
		t.Fatalf("legacy work_item trace_id=%q want \"\"", wi)
	}
	if err := db.SQL().QueryRowContext(context.Background(),
		`SELECT trace_id FROM approval_events WHERE approval_id = ?`,
		"a-legacy00001").Scan(&ev); err != nil {
		t.Fatalf("read legacy approval_event trace_id: %v", err)
	}
	if ev != "" {
		t.Fatalf("legacy approval_event trace_id=%q want \"\"", ev)
	}
}

// columnInfoRow mirrors the relevant fields of PRAGMA table_info.
type columnInfoRow struct {
	Name         string
	Type         string
	NotNull      int
	DefaultValue string
}

func columnInfo(t *testing.T, db *sql.DB, table, column string) *columnInfoRow {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "PRAGMA table_info("+table+")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == column {
			return &columnInfoRow{Name: name, Type: typ, NotNull: notnull, DefaultValue: dflt.String}
		}
	}
	return nil
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// Compile-time guard: trace package import is load-bearing — without
// it the OTel SpanContext used by PersistTraceIDFromContext would not
// compile. Reference its TraceState type to keep the symbol live.
var _ = trace.TraceState{}
