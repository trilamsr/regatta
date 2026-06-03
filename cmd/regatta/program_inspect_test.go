package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/program"
)

// writeFixtureBrief produces a signed ProgramBrief on disk and
// returns its path. The engine_version is operator-controlled so
// each test case pins record-time and replay-time SHAs independently.
func writeFixtureBrief(t *testing.T, dir, engineVersion string, dirty bool) string {
	t.Helper()
	brief := &program.ProgramBrief{
		SchemaVersion:    1,
		ProgramID:        "m-aaaaaaaaaaaa",
		ParentWorkItemID: "RFC-X",
		ParentCriteria: []program.PlanCriterion{
			{ID: "AC-1", Text: "first criterion"},
		},
		PlannerModelID: "stub:v1",
		Features: []program.PlannedFeature{
			{ID: "F-ONE", Title: "one", Fulfills: []string{"AC-1"}},
		},
		EngineVersion:    engineVersion,
		EngineBuildDirty: dirty,
		Signature:        schemas.SignatureBlock{},
	}
	signed, err := brief.Sign([]byte("test-key-bytes-padding-32-bytes!"), "k1")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	signed.ProducedAt = signed.ProducedAt.UTC()
	raw, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, "brief.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// captureStdout swaps os.Stdout for a pipe, runs fn, returns the
// captured bytes. Used to assert JSON output of the show + skew CLIs.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		buf := make([]byte, 0, 4096)
		chunk := make([]byte, 1024)
		for {
			n, err := r.Read(chunk)
			if n > 0 {
				buf = append(buf, chunk[:n]...)
			}
			if err != nil {
				break
			}
		}
		done <- buf
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return string(<-done)
}

func TestProgramShow_EmitsEngineVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeFixtureBrief(t, dir, "abc123def456abc123def456abc123def456abc1", true)

	var code int
	out := captureStdout(t, func() {
		code = runProgramShow([]string{path})
	})
	if code != 0 {
		t.Fatalf("exit code: got %d want 0; stdout=%s", code, out)
	}
	var report programShowReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("parse report: %v\n%s", err, out)
	}
	if report.EngineVersion != "abc123def456abc123def456abc123def456abc1" {
		t.Fatalf("engine_version not surfaced: %q", report.EngineVersion)
	}
	if !report.EngineBuildDirty {
		t.Fatalf("engine_build_dirty not surfaced")
	}
}

func TestProgramReplaySkewCheck_WarnModeOnMismatchExitsZero(t *testing.T) {
	dir := t.TempDir()
	path := writeFixtureBrief(t, dir, "v1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false)

	var code int
	out := captureStdout(t, func() {
		code = runProgramReplaySkewCheck([]string{path})
	})
	// WARN mode keeps the loop unblocked — exit 0 even on skew.
	if code != 0 {
		t.Fatalf("WARN exit: got %d want 0; stdout=%s", code, out)
	}
	if !strings.Contains(out, `"skewed": true`) {
		t.Fatalf("WARN output must surface skewed=true: %s", out)
	}
	if !strings.Contains(out, "engine-skew-replay-from=v1") {
		t.Fatalf("WARN output must include skew tag: %s", out)
	}
}

func TestProgramReplaySkewCheck_StrictModeOnMismatchExitsOne(t *testing.T) {
	dir := t.TempDir()
	path := writeFixtureBrief(t, dir, "v1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false)

	var code int
	captureStdout(t, func() {
		code = runProgramReplaySkewCheck([]string{"--strict", path})
	})
	if code != 1 {
		t.Fatalf("STRICT exit: got %d want 1", code)
	}
}
