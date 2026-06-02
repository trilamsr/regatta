// serve_test verifies the scheduler-side approval-gate wiring spec §3.1
// step 0.5 expects from `regatta serve`. Heavyweight end-to-end coverage
// lives in serve_claude_test.go; this file pins the minimal contract that
// regatta.yaml gates load + appear in the scheduler config.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

const serveTestGateYAML = `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:planned"
ci:
  command: "go test ./..."
gates:
  - id: prod-deploy-approval
    type: approval_gate
    name: prod
    risk_class: low
    reviewers: [alice, bob]
    quorum: 1
    timeout: 1h
    decision_window: 30m
    on_timeout: fail
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`

// TestBuildApprovalGate_LoadsAndResolvesByLane pins gate.Name == wi.Lane resolution.
func TestBuildApprovalGate_LoadsAndResolvesByLane(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(serveTestGateYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	db := openSchedulerTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gate, resolver, err := buildApprovalGate(db, repoRoot, logger)
	if err != nil {
		t.Fatalf("buildApprovalGate: %v", err)
	}
	if gate == nil {
		t.Fatal("gate is nil; want a non-nil ApprovalGate when regatta.yaml declares one")
	}
	if resolver == nil {
		t.Fatal("resolver is nil; want a non-nil GateResolver")
	}

	cfg, ok := resolver(state.WorkItem{ID: "F-1", Lane: "prod"})
	if !ok {
		t.Fatal("resolver returned !ok for lane=prod; want matched")
	}
	if cfg.Name != "prod" {
		t.Errorf("cfg.Name=%q; want prod", cfg.Name)
	}
	if cfg.Quorum != 1 {
		t.Errorf("cfg.Quorum=%d; want 1", cfg.Quorum)
	}

	if _, ok := resolver(state.WorkItem{ID: "F-2", Lane: "server"}); ok {
		t.Error("resolver matched lane=server; want !ok (no gate for that lane)")
	}
}

// TestBuildApprovalGate_NoConfigFileDisabled pins (nil, nil) when regatta.yaml absent.
func TestBuildApprovalGate_NoConfigFileDisabled(t *testing.T) {
	repoRoot := t.TempDir()
	db := openSchedulerTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gate, resolver, err := buildApprovalGate(db, repoRoot, logger)
	if err != nil {
		t.Fatalf("buildApprovalGate: %v", err)
	}
	if gate != nil || resolver != nil {
		t.Fatalf("gate=%v resolver=%v; want both nil when regatta.yaml absent", gate, resolver)
	}
}

// TestBuildApprovalGate_NoGatesDisabled pins (nil, nil) when no approval_gate rows present.
func TestBuildApprovalGate_NoGatesDisabled(t *testing.T) {
	repoRoot := t.TempDir()
	emptyYAML := `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:planned"
ci:
  command: "go test ./..."
gates:
  - id: ci-build
    type: deterministic
    command: "go build ./..."
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(emptyYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	db := openSchedulerTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gate, resolver, err := buildApprovalGate(db, repoRoot, logger)
	if err != nil {
		t.Fatalf("buildApprovalGate: %v", err)
	}
	if gate != nil || resolver != nil {
		t.Fatalf("gate=%v resolver=%v; want both nil when zero approval_gates configured", gate, resolver)
	}
}

// TestNewLogHandler_SelectsHandlerByFormat pins format→handler type.
func TestNewLogHandler_SelectsHandlerByFormat(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			h, err := newLogHandler(format, io.Discard)
			if err != nil {
				t.Fatalf("newLogHandler(%q): %v", format, err)
			}
			switch format {
			case "text":
				if _, ok := h.(*slog.TextHandler); !ok {
					t.Errorf("format=%q: got %T, want *slog.TextHandler", format, h)
				}
			case "json":
				if _, ok := h.(*slog.JSONHandler); !ok {
					t.Errorf("format=%q: got %T, want *slog.JSONHandler", format, h)
				}
			}
		})
	}
}

// TestNewLogHandler_InvalidValueErrorsClearly pins error names bad value + valid options.
func TestNewLogHandler_InvalidValueErrorsClearly(t *testing.T) {
	_, err := newLogHandler("xml", io.Discard)
	if err == nil {
		t.Fatal("newLogHandler(\"xml\"): err is nil; want non-nil")
	}
	msg := err.Error()
	// A-tier: error names the bad value AND the valid options so an
	// operator can fix their command line without grepping source.
	if !strings.Contains(msg, "xml") {
		t.Errorf("error %q missing bad value %q", msg, "xml")
	}
	if !strings.Contains(msg, "text") || !strings.Contains(msg, "json") {
		t.Errorf("error %q missing valid options text|json", msg)
	}
}

// TestLogFormatFlag_RejectsInvalidValue pins flag-Set validation surface.
func TestLogFormatFlag_RejectsInvalidValue(t *testing.T) {
	var f logFormatFlag
	if err := f.Set("text"); err != nil {
		t.Errorf("Set(\"text\"): %v", err)
	}
	if err := f.Set("json"); err != nil {
		t.Errorf("Set(\"json\"): %v", err)
	}
	err := f.Set("xml")
	if err == nil {
		t.Fatal("Set(\"xml\"): err is nil; want non-nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "xml") || !strings.Contains(msg, "text") || !strings.Contains(msg, "json") {
		t.Errorf("error %q must name bad value + valid options", msg)
	}
}

// TestNewLogHandler_OutputFormatMatches pins per-format wire-output shape.
func TestNewLogHandler_OutputFormatMatches(t *testing.T) {
	t.Run("json emits one parseable record per line", func(t *testing.T) {
		var buf bytes.Buffer
		h, err := newLogHandler("json", &buf)
		if err != nil {
			t.Fatalf("newLogHandler: %v", err)
		}
		slog.New(h).Info("tick.started", "feature", "F-1")
		out := strings.TrimRight(buf.String(), "\n")
		if out == "" {
			t.Fatal("json output empty")
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(out), &rec); err != nil {
			t.Fatalf("json output %q not parseable: %v", out, err)
		}
		if rec["msg"] != "tick.started" || rec["feature"] != "F-1" {
			t.Errorf("json record missing fields: %v", rec)
		}
	})
	t.Run("text emits human-readable key=value pairs", func(t *testing.T) {
		var buf bytes.Buffer
		h, err := newLogHandler("text", &buf)
		if err != nil {
			t.Fatalf("newLogHandler: %v", err)
		}
		slog.New(h).Info("tick.started", "feature", "F-1")
		out := buf.String()
		// slog.TextHandler renders msg=tick.started feature=F-1; a
		// leading '{' would mean the json handler was selected instead.
		if !strings.Contains(out, "msg=tick.started") || !strings.Contains(out, "feature=F-1") {
			t.Errorf("text output missing key=value markers: %q", out)
		}
		if strings.HasPrefix(strings.TrimSpace(out), "{") {
			t.Errorf("text output looks like json: %q", out)
		}
	})
}

func openSchedulerTestDB(t *testing.T) *state.DB {
	t.Helper()
	dsn := state.DSN(filepath.Join(t.TempDir(), "serve.db"))
	db, err := state.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
