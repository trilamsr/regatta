package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestEventsTail_DashNLimit asserts `events tail -n 1` caps output to 1 row (R31-Bug-A).
func TestEventsTail_DashNLimit(t *testing.T) {
	t0 := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return t0 }
	dbPath := filepath.Join(t.TempDir(), "events.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := db.RecordEvent(ctx, 0, string(obs.EventMergeCompleted), `{}`); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := runEventsTailWith(eventsTailDeps{
		Stdout: &stdout, Stderr: &stderr, Clock: clock, DSN: state.DSN(dbPath),
	}, []string{"-n", "1", "--format", "json"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("json.Unmarshal: %v stdout=%q", err, stdout.String())
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1 (R31-Bug-A: -n must cap output)", len(rows))
	}
}

// TestStatus_HelpExitsZero asserts `status --help` exits 0 not 2 (R31-Bug-B).
func TestStatus_HelpExitsZero(t *testing.T) {
	code := runStatus([]string{"--help"})
	if code != 0 {
		t.Fatalf("runStatus --help exit=%d want 0 (R31-Bug-B: --help must not be a flag-parse error)", code)
	}
}

// TestConfigValidate_AliasRoutes asserts `config-validate` alias dispatches to runValidateConfig (R31-Bug-C).
func TestConfigValidate_AliasRoutes(t *testing.T) {
	found := false
	for _, sc := range subcommands {
		if sc.name == "config-validate" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("subcommand alias 'config-validate' not registered (R31-Bug-C: docs say config-validate, binary only knows validate-config)")
	}
}

// TestAgentsList_JSONFlagAlias asserts --json equals --format=json (R31-Bug-D).
func TestAgentsList_JSONFlagAlias(t *testing.T) {
	tmp := t.TempDir()
	_ = openTempDB(t, tmp)
	dbPath := filepath.Join(tmp, "subs.db")

	stdout := captureStdout(t, func() {
		if code := runAgentsList([]string{"--db", dbPath, "--json"}); code != 0 {
			t.Fatalf("runAgentsList --json exit=%d want 0 (R31-Bug-D: --json must be a recognised flag form)", code)
		}
	})

	out := strings.TrimSpace(stdout)
	if out == "" {
		t.Fatalf("expected JSON output; got empty")
	}
	if out[0] != '[' && out[0] != '{' {
		t.Fatalf("expected JSON document (leading [ or {); got %q", out)
	}
}

