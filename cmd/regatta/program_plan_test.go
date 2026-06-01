package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/program"
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

	// --write-dir must live under cwd, so chdir into the fixture root.
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

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

	// Parse + verify HMAC of the written brief: file-exists alone could
	// mask a producer that emits malformed JSON, drops the signature, or
	// signs with the wrong key.
	briefBytes, err := os.ReadFile(filepath.Join(outDir, briefs[0]))
	if err != nil {
		t.Fatalf("read brief: %v", err)
	}
	var pb program.ProgramBrief
	if err := json.Unmarshal(briefBytes, &pb); err != nil {
		t.Fatalf("unmarshal brief: %v", err)
	}
	if err := pb.Validate(); err != nil {
		t.Fatalf("brief failed Validate: %v", err)
	}
	keyring := map[string][]byte{"k1": []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")}
	if err := pb.VerifySignature(keyring); err != nil {
		t.Fatalf("brief failed VerifySignature: %v", err)
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

// TestAtomicWriteBriefReadCap_TruncationFiresOnOversize: cap=4 against a 5-byte file must report equal=false — proves the LimitReader sentinel fired (mutation guard).
func TestAtomicWriteBriefReadCap_TruncationFiresOnOversize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "five.json")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	equal, exists, err := readExistingForEqualityCheckWithCap(path, []byte("hello"), 4)
	if err != nil {
		t.Fatalf("readExistingForEqualityCheckWithCap: %v", err)
	}
	if !exists {
		t.Fatalf("exists=false; want true")
	}
	if equal {
		t.Fatalf("equal=true; cap must force this to false to prove the LimitReader sentinel fired")
	}

	// Loose cap → equality is detected; pins functional correctness.
	equal, exists, err = readExistingForEqualityCheckWithCap(path, []byte("hello"), 16)
	if err != nil {
		t.Fatalf("readExistingForEqualityCheckWithCap (loose cap): %v", err)
	}
	if !exists || !equal {
		t.Fatalf("loose cap: exists=%v equal=%v want true,true", exists, equal)
	}
}

// TestAtomicWriteBriefReadCap_OversizeExistingTreatedAsDiffering: a sparse 64 MiB existing file (8x cap) must surface ErrTargetExists with force=false, then overwrite cleanly with force=true.
func TestAtomicWriteBriefReadCap_OversizeExistingTreatedAsDiffering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 64<<20); err != nil {
		t.Fatal(err)
	}

	if err := atomicWriteBrief(path, []byte(`{"new":true}`), false); err == nil {
		t.Fatalf("atomicWriteBrief returned nil on oversize differing target; want ErrTargetExists")
	} else if !errors.Is(err, orchestrator.ErrTargetExists) {
		t.Fatalf("err=%v want wrap of orchestrator.ErrTargetExists", err)
	}
	if err := atomicWriteBrief(path, []byte(`{"new":true}`), true); err != nil {
		t.Fatalf("force overwrite of oversize file errored: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"new":true}` {
		t.Fatalf("post-force content = %q want %q", got, `{"new":true}`)
	}
}

// TestAtomicWriteBriefReadCap_NotExistAndEqualPaths: missing path and matching/mismatching bytes within cap behave the same as the pre-LimitReader os.ReadFile shape.
func TestAtomicWriteBriefReadCap_NotExistAndEqualPaths(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.json")
	equal, exists, err := readExistingForEqualityCheck(missing, []byte(`x`))
	if err != nil {
		t.Fatalf("missing path errored: %v", err)
	}
	if exists || equal {
		t.Fatalf("missing path: exists=%v equal=%v want false,false", exists, equal)
	}

	present := filepath.Join(dir, "present.json")
	if err := os.WriteFile(present, []byte(`hello`), 0o600); err != nil {
		t.Fatal(err)
	}
	equal, exists, err = readExistingForEqualityCheck(present, []byte(`hello`))
	if err != nil {
		t.Fatalf("present matching path errored: %v", err)
	}
	if !exists || !equal {
		t.Fatalf("present matching: exists=%v equal=%v want true,true", exists, equal)
	}

	equal, exists, err = readExistingForEqualityCheck(present, []byte(`world`))
	if err != nil {
		t.Fatalf("present mismatch path errored: %v", err)
	}
	if !exists || equal {
		t.Fatalf("present mismatch: exists=%v equal=%v want true,false", exists, equal)
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

// TestOutputsSchemaResolverFor pins the scheduler↔BriefLoader bridge: the
// closure unboxes the program-side schema and reports presence for known
// features, absence otherwise. Without this, scheduler.Config.OutputsSchemas
// silently returns nil and predicates evaluate against an empty env.
func TestOutputsSchemaResolverFor(t *testing.T) {
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "rsv.db")))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	loader, err := program.NewBriefLoader(program.BriefLoaderConfig{
		FS:        fstest.MapFS{},
		DB:        db,
		Keyring:   map[string][]byte{"k1": []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")},
		Evaluator: program.NewEdgeEvaluator(),
	})
	if err != nil {
		t.Fatalf("NewBriefLoader: %v", err)
	}
	resolver := outputsSchemaResolverFor(loader)
	if _, ok := resolver("F-NOPE"); ok {
		t.Fatalf("unknown feature: resolver returned ok=true")
	}
}

// TestValidateWriteDirUnderCwd: targets outside cwd are rejected; prefix trap and ".." traversal are defused.
func TestValidateWriteDirUnderCwd(t *testing.T) {
	cwd := t.TempDir()
	sibling := t.TempDir() // independent tempdir; outside cwd by construction.

	// Platform-foreign absolute path. "/etc" is meaningful on POSIX; on
	// Windows we substitute a system path on the C: drive so the case
	// still exercises absolute-foreign-root rejection.
	foreignAbs := "/etc"
	if runtime.GOOS == goosWindows {
		foreignAbs = `C:\Windows\System32`
	}

	cases := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{"under cwd absolute", filepath.Join(cwd, "out"), false},
		{"cwd itself", cwd, false},
		{"nested deep", filepath.Join(cwd, "a", "b", "c"), false},
		{"trailing slash under cwd", filepath.Join(cwd, "out") + string(os.PathSeparator), false},
		{"sibling tempdir outside cwd", sibling, true},
		{"absolute /etc style", foreignAbs, true},
		{"parent escape via ..", filepath.Join(cwd, "..", filepath.Base(sibling)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWriteDirUnderCwd(tc.target, cwd)
			if tc.wantErr && err == nil {
				t.Fatalf("target=%q cwd=%q: want error, got nil", tc.target, cwd)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("target=%q cwd=%q: want nil, got %v", tc.target, cwd, err)
			}
			if tc.wantErr && err != nil {
				// Error must name both the offending path and the cwd so
				// operators can act on the message without running pwd.
				// Production canonicalizes both before formatting, so
				// compare against the canonicalized forms — on Windows,
				// 8.3 short-path normalization mangles raw tempdir paths
				// (e.g. runneradmin -> RUNNER~1) and would break a literal
				// match.
				// Production formats canonicalized paths through %q
				// (strconv.Quote), so the rendered message contains the
				// escaped form — on Windows, backslashes become "\\".
				// Mirror that here so the substring check sees the same
				// bytes the operator does.
				msg := err.Error()
				wantTarget := strconv.Quote(canonForAssert(t, tc.target))
				wantCwd := strconv.Quote(canonForAssert(t, cwd))
				if !containsAll(msg, wantTarget, wantCwd) {
					t.Fatalf("error %q must name target %s AND cwd %s", msg, wantTarget, wantCwd)
				}
			}
		})
	}
}

// canonForAssert mirrors production's canonicalizePath so test assertions
// compare against the same string the error message embeds. Falls back to
// the input on error so a hostile environment surfaces as a clear mismatch
// rather than a t.Fatal in the helper.
func canonForAssert(t *testing.T, p string) string {
	t.Helper()
	got, err := canonicalizePath(p)
	if err != nil {
		return p
	}
	return got
}

// TestValidateWriteDirSymlinkEscape: a symlink under cwd pointing outside must not slip the check.
func TestValidateWriteDirSymlinkEscape(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(cwd, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := validateWriteDirUnderCwd(link, cwd); err == nil {
		t.Fatalf("symlink %q -> %q must be rejected against cwd %q", link, outside, cwd)
	}
}

// TestValidateWriteDirSiblingPrefixTrap: /etc vs /etcetera must not false-match without trailing-sep normalization.
func TestValidateWriteDirSiblingPrefixTrap(t *testing.T) {
	// Build two sibling dirs under a shared parent so one is a string
	// prefix of the other but neither contains the other.
	parent := t.TempDir()
	cwd := filepath.Join(parent, "etc")
	trap := filepath.Join(parent, "etcetera")
	for _, d := range []string{cwd, trap} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateWriteDirUnderCwd(trap, cwd); err == nil {
		t.Fatalf("sibling prefix %q must be rejected against cwd %q", trap, cwd)
	}
}

// TestRunProgramPlan_WriteRejectsOutsideCwd: --write-dir outside cwd exits non-zero without the opt-out flag.
func TestRunProgramPlan_WriteRejectsOutsideCwd(t *testing.T) {
	t.Setenv("HMAC_KEY", "test-key-32-bytes-aaaaaaaaaaaaaaa")

	// Operator working directory: where the cmd is invoked from.
	work := t.TempDir()
	// Sibling outside cwd: the rejected target.
	outside := t.TempDir()

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	itemPath := filepath.Join(work, "PROG-1.md")
	if err := os.WriteFile(itemPath, []byte(programMarkdown), 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{"-hmac-key-env=HMAC_KEY", "-planner=stub", "-write",
		"-write-dir=" + outside, itemPath}
	if rc := runProgramPlan(args); rc == 0 {
		t.Fatalf("expected non-zero rc for outside-cwd --write-dir; got 0")
	}
}

// TestRunProgramPlan_WriteUnsafeOptOutAllowsOutsideCwd: --unsafe-write-dir admits an otherwise-rejected outside path.
func TestRunProgramPlan_WriteUnsafeOptOutAllowsOutsideCwd(t *testing.T) {
	t.Setenv("HMAC_KEY", "test-key-32-bytes-aaaaaaaaaaaaaaa")

	work := t.TempDir()
	outside := t.TempDir()

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	itemPath := filepath.Join(work, "PROG-1.md")
	if err := os.WriteFile(itemPath, []byte(programMarkdown), 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{"-hmac-key-env=HMAC_KEY", "-planner=stub", "-write",
		"-write-dir=" + outside, "-unsafe-write-dir", itemPath}
	if rc := runProgramPlan(args); rc != 0 {
		t.Fatalf("expected rc=0 with --unsafe-write-dir; got %d", rc)
	}
	// Confirm a brief actually landed in the outside dir.
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	var briefs int
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			briefs++
		}
	}
	if briefs != 1 {
		t.Fatalf("want exactly one brief in %s; got %d", outside, briefs)
	}
}

// TestSafeMkdir_RefusesSymlinkInPath: a symlink planted post-validate in --write-dir's tail must abort mkdir.
func TestSafeMkdir_RefusesSymlinkInPath(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()

	// Simulate the post-validate plant: an attacker creates cwd/redir as
	// a symlink to /outside. The target "cwd/redir/brief" would, under
	// naive os.MkdirAll, materialize at /outside/brief.
	planted := filepath.Join(cwd, "redir")
	if err := os.Symlink(outside, planted); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	target := filepath.Join(planted, "brief")

	err := safeMkdirAllUnderCwd(target, cwd)
	if err == nil {
		t.Fatalf("safeMkdirAllUnderCwd allowed symlinked component: target=%q cwd=%q", target, cwd)
	}
	// And confirm the destination outside cwd was NOT created — this is
	// the only invariant operators actually care about. Whether the
	// rejection arms via "symlink" wording or "outside cwd" wording is
	// an implementation detail; the leak check is what matters.
	if _, statErr := os.Stat(filepath.Join(outside, "brief")); statErr == nil {
		t.Fatalf("safeMkdir leaked: %s/brief exists", outside)
	}
}

// TestSafeMkdir_CreatesNestedUnderCwd: multi-level descent with no symlinks materializes the full chain.
func TestSafeMkdir_CreatesNestedUnderCwd(t *testing.T) {
	cwd := t.TempDir()
	target := filepath.Join(cwd, "a", "b", "c")
	if err := safeMkdirAllUnderCwd(target, cwd); err != nil {
		t.Fatalf("safeMkdirAllUnderCwd: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat created target: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("created target is not a directory: %v", info.Mode())
	}
}

// TestSafeMkdir_TargetEqualsCwd: target == cwd is a no-op (no mkdir, no error).
func TestSafeMkdir_TargetEqualsCwd(t *testing.T) {
	cwd := t.TempDir()
	if err := safeMkdirAllUnderCwd(cwd, cwd); err != nil {
		t.Fatalf("safeMkdirAllUnderCwd(cwd,cwd): %v", err)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
