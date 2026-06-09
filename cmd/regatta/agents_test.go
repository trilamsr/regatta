package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentsList_TableEmpty asserts table format over empty db exits 0 (#1078).
func TestAgentsList_TableEmpty(t *testing.T) {
	tmp := t.TempDir()
	_ = openTempDB(t, tmp)
	dbPath := filepath.Join(tmp, "subs.db")
	if code := runAgentsList([]string{"--db", dbPath, "--format", "table"}); code != 0 {
		t.Fatalf("runAgentsList table empty exit = %d; want 0", code)
	}
}

// TestAgentsList_JSONFormat asserts json format over empty db exits 0 (#1078).
func TestAgentsList_JSONFormat(t *testing.T) {
	tmp := t.TempDir()
	_ = openTempDB(t, tmp)
	dbPath := filepath.Join(tmp, "subs.db")
	if code := runAgentsList([]string{"--db", dbPath, "--format", "json"}); code != 0 {
		t.Fatalf("runAgentsList json empty exit = %d; want 0", code)
	}
}

// TestAgentsList_UnknownFormat asserts --format=yaml rejects with exit 2 (#1078).
func TestAgentsList_UnknownFormat(t *testing.T) {
	tmp := t.TempDir()
	_ = openTempDB(t, tmp)
	dbPath := filepath.Join(tmp, "subs.db")
	if code := runAgentsList([]string{"--db", dbPath, "--format", "yaml"}); code != 2 {
		t.Fatalf("runAgentsList yaml exit = %d; want 2", code)
	}
}

// TestAgents_NoSubcommand asserts bare `agents` errors with usage hint (#1078).
func TestAgents_NoSubcommand(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	code := runAgents(nil)

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if code != 2 {
		t.Fatalf("runAgents([]) exit = %d; want 2", code)
	}
	if !strings.Contains(buf.String(), "expected sub-subcommand") {
		t.Fatalf("stderr = %q; want substring %q", buf.String(), "expected sub-subcommand")
	}
}

// TestAgentsList_UnknownStateFlag pins the reviewer-flagged contract: invalid --state value fails fast with a known-set hint (#1078 reviewer a57487820abd65001).
func TestAgentsList_UnknownStateFlag(t *testing.T) {
	tmp := t.TempDir()
	_ = openTempDB(t, tmp)
	dbPath := filepath.Join(tmp, "subs.db")
	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })
	code := runAgentsList([]string{"--db", dbPath, "--state", "foo"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if code != 2 {
		t.Fatalf("unknown state exit = %d; want 2", code)
	}
	if !strings.Contains(buf.String(), "unknown state") {
		t.Fatalf("stderr = %q; want 'unknown state'", buf.String())
	}
}

// TestKnownAgentStatesIncludesEscalated pins that escalated agents do not silently drop from the default --state list (reviewer-named risk).
func TestKnownAgentStatesIncludesEscalated(t *testing.T) {
	all := knownAgentStateLabels()
	found := false
	for _, s := range all {
		if s == "escalated" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("escalated missing from knownAgentStates; reaper-escalated agents will be hidden")
	}
}
