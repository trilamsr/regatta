package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// mustExec opens dbPath, runs one stmt with args, and t.Fatals on error.
// Kept local to review_status_test.go so the helper does not pollute
// neighbouring test files; cmd/regatta has no shared test-db helper today.
func mustExec(t *testing.T, dbPath, stmt string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(stmt, args...); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}

// TestReviewSetupCodeowners_CreatesCatchAll pins #624 — fresh repo gets a catch-all written.
func TestReviewSetupCodeowners_CreatesCatchAll(t *testing.T) {
	tmp := t.TempDir()
	var out bytes.Buffer
	code := runReviewSetupCodeowners([]string{"--repo-root", tmp, "--reviewer-bot-login", "regatta-reviewer-bot"}, &out)
	if code != 0 {
		t.Fatalf("exit = %d, out=%s", code, out.String())
	}
	body, err := os.ReadFile(filepath.Join(tmp, ".github", "CODEOWNERS"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "* @regatta-reviewer-bot") {
		t.Fatalf("CODEOWNERS missing catch-all: %s", body)
	}
}

// TestReviewSetupCodeowners_IdempotentOnSecondRun pins idempotency — re-running does not duplicate the line.
func TestReviewSetupCodeowners_IdempotentOnSecondRun(t *testing.T) {
	tmp := t.TempDir()
	var out bytes.Buffer
	for i := 0; i < 2; i++ {
		out.Reset()
		if code := runReviewSetupCodeowners([]string{"--repo-root", tmp}, &out); code != 0 {
			t.Fatalf("run %d: exit = %d", i, code)
		}
	}
	body, err := os.ReadFile(filepath.Join(tmp, ".github", "CODEOWNERS"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := strings.Count(string(body), "* @regatta-reviewer-bot")
	if got != 1 {
		t.Fatalf("catch-all line count = %d, want 1: %s", got, body)
	}
}

// TestReviewSetupCodeowners_PreservesExistingLines pins safety —
// pre-existing CODEOWNERS lines must survive the append.
func TestReviewSetupCodeowners_PreservesExistingLines(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".github")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	prior := "/contracts/ @schema-team\n"
	if err := os.WriteFile(filepath.Join(dir, "CODEOWNERS"), []byte(prior), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runReviewSetupCodeowners([]string{"--repo-root", tmp}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "CODEOWNERS"))
	if !strings.Contains(string(body), "/contracts/ @schema-team") {
		t.Fatalf("prior line lost: %s", body)
	}
	if !strings.Contains(string(body), "* @regatta-reviewer-bot") {
		t.Fatalf("catch-all not appended: %s", body)
	}
}

// TestReviewStatus_EmptyDB_PrintsNoVerdicts pins #625 — empty substrate yields a helpful message, not a crash.
func TestReviewStatus_EmptyDB_PrintsNoVerdicts(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "state.db")
	// Create a minimal substrate_events table so the query succeeds.
	mustExec(t, dbPath, `CREATE TABLE substrate_events (
		id TEXT, run_id TEXT, work_item_id TEXT, tenant_id TEXT, trace_id TEXT,
		span_id TEXT, kind TEXT, key TEXT, payload_json TEXT, blob_digest TEXT,
		supersedes TEXT, written_by TEXT, written_at INTEGER, schema_version INTEGER,
		nonce TEXT, sig_alg TEXT, sig_key_id TEXT, sig_mac TEXT)`)

	var out bytes.Buffer
	if code := runReviewStatus([]string{"--db", dbPath}, &out); code != 0 {
		t.Fatalf("exit = %d, out=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "no gate verdicts") {
		t.Fatalf("missing empty-state message: %s", out.String())
	}
}

// TestReviewStatus_PrintsGateVerdictRow asserts recent gate_verdict prints in table (#625).
func TestReviewStatus_PrintsGateVerdictRow(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "state.db")
	mustExec(t, dbPath, `CREATE TABLE substrate_events (
		id TEXT, run_id TEXT, work_item_id TEXT, tenant_id TEXT, trace_id TEXT,
		span_id TEXT, kind TEXT, key TEXT, payload_json TEXT, blob_digest TEXT,
		supersedes TEXT, written_by TEXT, written_at INTEGER, schema_version INTEGER,
		nonce TEXT, sig_alg TEXT, sig_key_id TEXT, sig_mac TEXT)`)
	payload := `{"gate_name":"L4","pass":true,"reason":"clean","work_item_id":"wi-1","tool":"cel","tv":"sha1","db_v":1,"det":true}`
	mustExec(t, dbPath,
		`INSERT INTO substrate_events (id, run_id, work_item_id, tenant_id, kind, payload_json, written_at, schema_version)
		 VALUES ('e1','r1','wi-1','t','gate_verdict',?,9999999999000,1)`,
		payload)

	var out bytes.Buffer
	if code := runReviewStatus([]string{"--db", dbPath}, &out); code != 0 {
		t.Fatalf("exit = %d, out=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "wi-1") || !strings.Contains(out.String(), "L4") {
		t.Fatalf("table missing row: %s", out.String())
	}
}

// TestCollapseCol_TruncatesWithEllipsis pins the 30-col helper.
func TestCollapseCol_TruncatesWithEllipsis(t *testing.T) {
	if got := collapse("abcdefgh", 5); got != "abcd…" {
		t.Errorf("collapse(long,5) = %q, want abcd…", got)
	}
	if got := collapse("ok", 5); got != "ok" {
		t.Errorf("collapse(short,5) = %q, want ok", got)
	}
}
