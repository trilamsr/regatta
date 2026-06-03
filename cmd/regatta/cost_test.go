package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

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

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
