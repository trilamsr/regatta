package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator"
)

// TestRunProgramPlanRejectsNonProgramKind verifies that `regatta program plan`
// refuses a work item whose Kind is not "program". The planner is normative
// on this; silent acceptance would let a feature item slip into the planner
// and produce a brief with no meaningful decomposition.
func TestRunProgramPlanRejectsNonProgramKind(t *testing.T) {
	t.Setenv("HMAC_KEY", "dummy")
	dir := t.TempDir()
	path := filepath.Join(dir, "wi.json")
	item := schemas.WorkItem{
		ID:    "WI-1",
		Title: "leaf",
		Kind:  schemas.KindFeature,
		AcceptanceCriteria: []schemas.Criterion{
			{ID: "c1", Text: "do thing"},
		},
		Status: schemas.StatusPlanned,
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rc := runProgramPlan([]string{"-hmac-key-env=HMAC_KEY", path})
	if rc != 2 {
		t.Fatalf("expected exit 2 for non-program kind, got %d", rc)
	}
}

// programMarkdown is the fixture shared by --write tests. Three
// criteria so the StubPlanner emits three features and the coverage
// invariant is non-trivial.
const programMarkdown = `---
id: PROG-1
kind: program
title: smoke
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: do a
- [planned] c2: do b
- [planned] c3: do c
`

// TestRunProgramPlan_WriteCreatesFile drives the --write flag end-to-end
// with the offline StubPlanner. The brief lands at
// <write-dir>/<program_id>.json via atomic temp+rename; presence of the
// file is sufficient for the smoke (full schema coverage lives in the
// internal/program planner_test suite).
func TestRunProgramPlan_WriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HMAC_KEY", "test-key-32-bytes-aaaaaaaaaaaaaaa")

	itemPath := filepath.Join(dir, "PROG-1.md")
	if err := os.WriteFile(itemPath, []byte(programMarkdown), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, ".regatta", "programs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	args := []string{"-hmac-key-env=HMAC_KEY", "-planner=stub", "-write",
		"-write-dir=" + outDir, itemPath}
	if rc := runProgramPlan(args); rc != 0 {
		t.Fatalf("runProgramPlan rc=%d want 0", rc)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var briefs []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			briefs = append(briefs, e.Name())
		}
	}
	if len(briefs) != 1 {
		t.Fatalf("want exactly one brief .json in %s, got %v", outDir, briefs)
	}
}

// TestRunProgramPlan_WriteTargetExistsErrors exercises the no-clobber
// path: the stub planner stamps a fresh produced_at on every call, so
// re-running with the same args writes a byte-different brief whose
// program_id collides only when explicitly aimed at the same path.
// We write the first brief, then plant a sibling file at a known path
// and re-target the second run at it (via --write-dir + a forced
// program_id is overkill — instead, just run twice into the same dir
// and look for the second-run failure mode: every run produces a new
// program_id, so the test asserts no-clobber by writing a sentinel
// file at the target path the second call would compute.
//
// Simpler shape: write a sentinel at <out>/m-deadbeef.json, then
// stub the program_id to "m-deadbeef" via a second runProgramPlan
// invocation using --force=false. Without --force, atomicWriteBrief
// MUST refuse. We can't pin program_id from the CLI, so we instead
// place a different brief file in the dir and verify the success
// path doesn't touch it (basic property); separately the
// atomicWriteBrief unit test below proves the sentinel.
func TestRunProgramPlan_WriteTargetExistsErrors(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, ".regatta", "programs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Plant an existing brief with content that does NOT match what
	// atomicWriteBrief is about to write; with force=false this must
	// surface ErrTargetExists.
	existing := filepath.Join(outDir, "m-000000000001.json")
	if err := os.WriteFile(existing, []byte(`{"existing":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := atomicWriteBrief(existing, []byte(`{"new":true}`), false); err == nil {
		t.Fatalf("atomicWriteBrief returned nil; want ErrTargetExists")
	} else if !errors.Is(err, orchestrator.ErrTargetExists) {
		t.Fatalf("err=%v want wrap of orchestrator.ErrTargetExists", err)
	}

	// Same bytes → no-op, no error.
	if err := atomicWriteBrief(existing, []byte(`{"existing":true}`), false); err != nil {
		t.Fatalf("identical content rewrite errored: %v", err)
	}

	// --force overrides.
	if err := atomicWriteBrief(existing, []byte(`{"new":true}`), true); err != nil {
		t.Fatalf("force overwrite errored: %v", err)
	}
	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"new":true}` {
		t.Fatalf("post-force content = %q want %q", got, `{"new":true}`)
	}
}

// TestLoadBriefKeyring_HonorsKeyIDEnv pins the sign/verify keyID
// contract: program plan's -hmac-key-id default ("k1") must match
// the keyID under which serve verifies. Without this, briefs sign
// under "k1" and serve rejects them as unknown_key_id — the bug
// A11's e2e test surfaced.
func TestLoadBriefKeyring_HonorsKeyIDEnv(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "test-key-32-bytes-aaaaaaaaaaaaaaa")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	got := loadBriefKeyring()
	if _, ok := got["k1"]; !ok {
		t.Fatalf("default keyID: got keys %v want k1", keysOf(got))
	}

	t.Setenv("REGATTA_HMAC_KEY_ID", "rotated-v2")
	got = loadBriefKeyring()
	if _, ok := got["rotated-v2"]; !ok {
		t.Fatalf("explicit keyID: got keys %v want rotated-v2", keysOf(got))
	}

	t.Setenv("REGATTA_HMAC_KEY", "")
	if got := loadBriefKeyring(); len(got) != 0 {
		t.Fatalf("empty key: got %d entries want 0", len(got))
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
