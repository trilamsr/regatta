package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// openCounter records every state.Open call the Opener seam routes
// through it so tests can assert "buildEnforcer opened once per
// invocation" — the close invariant of #649 then follows from each
// invocation having a matching deferred closeDB().
type openCounter struct {
	opens int64
}

func (c *openCounter) open(ctx context.Context, dsn string) (*state.DB, error) {
	atomic.AddInt64(&c.opens, 1)
	return state.Open(ctx, dsn)
}

// TestCostStatus_CapUnset_ExplainsDegradedMode no config → unset path.
func TestCostStatus_CapUnset_ExplainsDegradedMode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	// state.Open auto-migrates the schema.
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	_ = db.Close()
	var stdout, stderr bytes.Buffer
	code := runCostStatusWith(costDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      time.Now,
		DSN:        state.DSN(dbPath),
		ConfigPath: "", // no config file
	}, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"unset", "per-scope"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q\n%s", want, out)
		}
	}
}

// TestCostStatus_WithCap_RendersCapAndState yaml config produces cap output.
func TestCostStatus_WithCap_RendersCapAndState(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	_ = db.Close()
	cfgPath := filepath.Join(dir, "regatta.yaml")
	if err := writeFile(cfgPath, `safety:
  cost:
    per_dag_usd: 5
    cap:
      daily_usd: 40.00
      timezone: "UTC"
      memoize_ttl_seconds: 60
`); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := runCostStatusWith(costDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      func() time.Time { return time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC) },
		DSN:        state.DSN(dbPath),
		ConfigPath: cfgPath,
	}, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Active", "headroom", "$40.00"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q\n%s", want, out)
		}
	}
}

// TestResume_NoCap_FailsClearly resume against unset cap exits non-zero.
func TestResume_NoCap_FailsClearly(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	_ = db.Close()
	var stdout, stderr bytes.Buffer
	code := runResumeWith(resumeDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      time.Now,
		DSN:        state.DSN(dbPath),
		ConfigPath: "",
		Actor:      "tester",
	}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit when cap is unset")
	}
	if !strings.Contains(stderr.String(), "cost.cap") {
		t.Fatalf("stderr missing cost.cap hint: %s", stderr.String())
	}
}

// TestResume_EmitsAuditEvent yaml config + resume writes cost_cap_resumed.
func TestResume_EmitsAuditEvent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	ctx := context.Background()
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	_ = db.Close()
	cfgPath := filepath.Join(dir, "regatta.yaml")
	if err := writeFile(cfgPath, `safety:
  cost:
    per_dag_usd: 5
    cap:
      daily_usd: 40.00
`); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := runResumeWith(resumeDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      func() time.Time { return time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC) },
		DSN:        state.DSN(dbPath),
		ConfigPath: cfgPath,
		Actor:      "trilamsr",
	}, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "override accepted") {
		t.Fatalf("stdout missing accept marker: %s", stdout.String())
	}
	// Re-open DB and inspect events.
	db2, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open re-read: %v", err)
	}
	defer func() { _ = db2.Close() }()
	ev, err := db2.LatestEventByKind(ctx, "cost_cap_resumed")
	if err != nil {
		t.Fatalf("LatestEventByKind: %v", err)
	}
	if !strings.Contains(ev.PayloadJSON, `"actor":"trilamsr"`) {
		t.Fatalf("audit event missing actor: %s", ev.PayloadJSON)
	}
}

// countOpenDBFiles uses lsof to count this process's open FDs that
// point at the sqlite DB file. Cross-platform readlink against /dev/fd
// works on Linux but Darwin's /dev/fd entries are character-device
// shims that do not expose the underlying path — lsof is the only
// reliable surface on both. Skip if lsof is unavailable. Resolves
// symlinks so /var/...→/private/var/... canonicalization (Darwin
// TempDir) does not produce a false miss.
func countOpenDBFiles(t *testing.T, dbPath string) int {
	t.Helper()
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skipf("lsof unavailable: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(dbPath)
	if err != nil {
		resolved = dbPath
	}
	pid := strconv.Itoa(os.Getpid())
	out, err := exec.Command("lsof", "-p", pid).Output()
	if err != nil {
		t.Fatalf("lsof: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, resolved) {
			n++
		}
	}
	return n
}

// TestRunCostStatus_ClosesDB asserts deferred close prevents per-invocation FD leak (#649).
func TestRunCostStatus_ClosesDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	_ = db.Close()
	cfgPath := filepath.Join(dir, "regatta.yaml")
	if err := writeFile(cfgPath, `safety:
  cost:
    per_dag_usd: 5
    cap:
      daily_usd: 40.00
`); err != nil {
		t.Fatalf("write config: %v", err)
	}
	const runs = 3
	baseline := countOpenDBFiles(t, dbPath)
	counter := &openCounter{}
	for i := 0; i < runs; i++ {
		var stdout, stderr bytes.Buffer
		code := runCostStatusWith(costDeps{
			Stdout:     &stdout,
			Stderr:     &stderr,
			Clock:      func() time.Time { return time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC) },
			DSN:        state.DSN(dbPath),
			ConfigPath: cfgPath,
			Opener:     counter.open,
		}, nil)
		if code != 0 {
			t.Fatalf("invocation %d: exit=%d stderr=%s", i+1, code, stderr.String())
		}
	}
	if got := atomic.LoadInt64(&counter.opens); got != runs {
		t.Fatalf("Opener called %d times, want %d", got, runs)
	}
	if leaked := countOpenDBFiles(t, dbPath) - baseline; leaked != 0 {
		t.Fatalf("post-run %d leaked FD(s) on %s — Close not invoked", leaked, dbPath)
	}
}

// TestRunResume_ClosesDB asserts resume CLI defers closeDB to prevent leak (#649).
func TestRunResume_ClosesDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	ctx := context.Background()
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	_ = db.Close()
	cfgPath := filepath.Join(dir, "regatta.yaml")
	if err := writeFile(cfgPath, `safety:
  cost:
    per_dag_usd: 5
    cap:
      daily_usd: 40.00
`); err != nil {
		t.Fatalf("write config: %v", err)
	}
	const runs = 3
	baseline := countOpenDBFiles(t, dbPath)
	counter := &openCounter{}
	for i := 0; i < runs; i++ {
		var stdout, stderr bytes.Buffer
		code := runResumeWith(resumeDeps{
			Stdout:     &stdout,
			Stderr:     &stderr,
			Clock:      func() time.Time { return time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC) },
			DSN:        state.DSN(dbPath),
			ConfigPath: cfgPath,
			Actor:      "tester",
			Opener:     counter.open,
		}, nil)
		if code != 0 {
			t.Fatalf("invocation %d: exit=%d stderr=%s", i+1, code, stderr.String())
		}
	}
	if got := atomic.LoadInt64(&counter.opens); got != runs {
		t.Fatalf("Opener called %d times, want %d", got, runs)
	}
	if leaked := countOpenDBFiles(t, dbPath) - baseline; leaked != 0 {
		t.Fatalf("post-run %d leaked FD(s) on %s — Close not invoked", leaked, dbPath)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestCostBackfill_CLI_HappyPath drives the full CLI seam end-to-end (#272).
func TestCostBackfill_CLI_HappyPath(t *testing.T) {
	substrate.ResetClockForTesting()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	keyID := "k1"
	seedCLIRunWindow(t, ctx, db.SQL(), "run-Y",
		time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 11, 30, 0, 0, time.UTC),
		key, keyID)
	_ = db.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cost_report/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"bucket_start":                "2026-06-01T10:00:00Z",
				"bucket_end":                  "2026-06-01T11:00:00Z",
				"model":                       "claude-sonnet-4-7",
				"uncached_input_tokens":       1_000_000,
				"cache_read_input_tokens":     0,
				"cache_creation_input_tokens": 0,
				"output_tokens":               500_000,
			}},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ANTHROPIC_ADMIN_KEY", "sk-admin-test")

	var stdout, stderr bytes.Buffer
	code := runCostBackfillWith(backfillDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      func() time.Time { return time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC) },
		DSN:        state.DSN(dbPath),
		ConfigPath: "",
		Keys:       map[string][]byte{keyID: key},
		ActiveKey:  keyID,
		BaseURL:    srv.URL,
	}, []string{"run-Y"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "emitted=1") {
		t.Fatalf("stdout missing emitted count: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "pricing_rev=backfill:") {
		t.Fatalf("stdout missing pricing_rev tag: %s", stdout.String())
	}
}

// TestCostBackfill_CLI_RequiresRunID exits 2 when arg missing.
func TestCostBackfill_CLI_RequiresRunID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCostBackfillWith(backfillDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      time.Now,
		DSN:        "ignored",
		ConfigPath: "",
		Keys:       map[string][]byte{"k1": []byte("0123456789abcdef0123456789abcdef")},
		ActiveKey:  "k1",
	}, []string{})
	if code != 2 {
		t.Fatalf("exit=%d, want 2; stderr=%s", code, stderr.String())
	}
}

// TestCostBackfill_CLI_RejectsMissingHMAC exits 1 when key absent.
func TestCostBackfill_CLI_RejectsMissingHMAC(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	_ = db.Close()
	var stdout, stderr bytes.Buffer
	code := runCostBackfillWith(backfillDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      time.Now,
		DSN:        state.DSN(dbPath),
		ConfigPath: "",
		Keys:       map[string][]byte{},
		ActiveKey:  "",
	}, []string{"run-Z"})
	if code != 1 {
		t.Fatalf("exit=%d, want 1; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "REGATTA_HMAC_KEY") {
		t.Fatalf("stderr missing key hint: %s", stderr.String())
	}
}

// TestCostBackfill_CLI_RejectsMissingAdminKey fails fast at parse time (#705 R3).
func TestCostBackfill_CLI_RejectsMissingAdminKey(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	_ = db.Close()
	t.Setenv("ANTHROPIC_ADMIN_KEY", "")
	var stdout, stderr bytes.Buffer
	code := runCostBackfillWith(backfillDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      time.Now,
		DSN:        state.DSN(dbPath),
		ConfigPath: "",
		Keys:       map[string][]byte{"k1": []byte("0123456789abcdef0123456789abcdef")},
		ActiveKey:  "k1",
	}, []string{"run-Z"})
	if code != 1 {
		t.Fatalf("exit=%d, want 1; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ANTHROPIC_ADMIN_KEY") {
		t.Fatalf("stderr missing admin key hint: %s", stderr.String())
	}
}

// TestCostBackfill_CLI_SingleEventWarns surfaces R4 synthetic-window warning to operator (#705).
func TestCostBackfill_CLI_SingleEventWarns(t *testing.T) {
	substrate.ResetClockForTesting()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	keyID := "k1"
	at := time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC)
	seedCLIRunWindow(t, ctx, db.SQL(), "run-single", at, at, key, keyID)
	_ = db.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cost_report/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ANTHROPIC_ADMIN_KEY", "sk-admin-test")
	var stdout, stderr bytes.Buffer
	code := runCostBackfillWith(backfillDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      func() time.Time { return time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC) },
		DSN:        state.DSN(dbPath),
		ConfigPath: "",
		Keys:       map[string][]byte{keyID: key},
		ActiveKey:  keyID,
		BaseURL:    srv.URL,
	}, []string{"run-single"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no calls in window") {
		t.Fatalf("stderr missing single-event warning: %s", stderr.String())
	}
}

// TestCostBackfill_CLI_TagFlagFlowsThrough pipes --backfill-tag into pricing_rev (#705 R1).
func TestCostBackfill_CLI_TagFlagFlowsThrough(t *testing.T) {
	substrate.ResetClockForTesting()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	keyID := "k1"
	seedCLIRunWindow(t, ctx, db.SQL(), "run-tag",
		time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 11, 30, 0, 0, time.UTC),
		key, keyID)
	_ = db.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cost_report/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"bucket_start":                "2026-06-01T10:00:00Z",
				"bucket_end":                  "2026-06-01T11:00:00Z",
				"model":                       "claude-sonnet-4-7",
				"uncached_input_tokens":       1_000_000,
				"cache_read_input_tokens":     0,
				"cache_creation_input_tokens": 0,
				"output_tokens":               500_000,
			}},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ANTHROPIC_ADMIN_KEY", "sk-admin-test")
	var stdout, stderr bytes.Buffer
	code := runCostBackfillWith(backfillDeps{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Clock:      func() time.Time { return time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC) },
		DSN:        state.DSN(dbPath),
		ConfigPath: "",
		Keys:       map[string][]byte{keyID: key},
		ActiveKey:  keyID,
		BaseURL:    srv.URL,
	}, []string{"--backfill-tag", "ops-705", "run-tag"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pricing_rev=backfill:ops-705") {
		t.Fatalf("stdout missing tag-derived pricing_rev: %s", stdout.String())
	}
}

func seedCLIRunWindow(t *testing.T, ctx context.Context, db *sql.DB, runID string, start, end time.Time, key []byte, keyID string) {
	t.Helper()
	for i, at := range []time.Time{start, end} {
		ev := substrate.Event{
			ID:            substrate.Mint(at),
			RunID:         runID,
			WorkItemID:    "WI-cli",
			TenantID:      substrate.DefaultTenantID,
			Kind:          substrate.KindHeartbeat,
			PayloadJSON:   json.RawMessage(fmt.Sprintf(`{"work_item_id":"WI-cli","timestamp":%d}`, at.UnixMilli())),
			WrittenBy:     "spawner-test",
			WrittenAt:     at.UnixMilli(),
			SchemaVersion: 1,
			Nonce:         fmt.Sprintf("cli-hb-%d", i),
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := substrate.AppendEvent(ctx, tx, ev, key, keyID); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
}
