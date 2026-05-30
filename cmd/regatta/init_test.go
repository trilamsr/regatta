package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// moduleRoot walks up from the current test file to the directory
// containing go.mod. Returns "" if not found (e.g. out-of-tree run);
// callers should t.Skip in that case.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func TestEmbeddedYamlMatchesExample(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	if root == "" {
		t.Skip("module root not resolvable; skipping drift test")
	}
	canonical, err := os.ReadFile(filepath.Join(root, "examples/minimal/regatta.yaml"))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	embedded, err := os.ReadFile(filepath.Join(root, "cmd/regatta/init_assets/regatta.yaml"))
	if err != nil {
		t.Fatalf("read embedded: %v. Did you run Task 2 of the init+smoke plan?", err)
	}
	if string(canonical) != string(embedded) {
		t.Fatalf("drift: cmd/regatta/init_assets/regatta.yaml diverges from examples/minimal/regatta.yaml. Re-sync with: cp examples/minimal/regatta.yaml cmd/regatta/init_assets/regatta.yaml")
	}
}

func TestEmbeddedSampleMatchesFixture(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	if root == "" {
		t.Skip("module root not resolvable; skipping drift test")
	}
	canonical, err := os.ReadFile(filepath.Join(root, "testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff"))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	embedded, err := os.ReadFile(filepath.Join(root, "cmd/regatta/init_assets/sample.diff"))
	if err != nil {
		t.Fatalf("read embedded: %v. Did you run Task 2 of the init+smoke plan?", err)
	}
	if string(canonical) != string(embedded) {
		t.Fatalf("drift: cmd/regatta/init_assets/sample.diff diverges from testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff. Re-sync with: cp testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff cmd/regatta/init_assets/sample.diff")
	}
}

// runInitInDir cd's into dir, runs runInit with args, returns exit
// code + captured stdout + stderr. Restores cwd on return.
func runInitInDir(t *testing.T, dir string, args []string) (code int, stdout, stderr string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	var out, errOut bytes.Buffer
	code = runInitWithIO(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestInit_WritesBothFiles(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr on success; got %q", stderr)
	}
	yaml, err := os.ReadFile(filepath.Join(dir, "regatta.yaml"))
	if err != nil {
		t.Fatalf("regatta.yaml not written: %v", err)
	}
	diff, err := os.ReadFile(filepath.Join(dir, ".regatta", "sample.diff"))
	if err != nil {
		t.Fatalf(".regatta/sample.diff not written: %v", err)
	}
	if len(yaml) == 0 || len(diff) == 0 {
		t.Fatalf("empty file written: yaml=%d diff=%d", len(yaml), len(diff))
	}
	if !bytes.Contains([]byte(stdout), []byte("wrote regatta.yaml")) {
		t.Fatalf("expected stdout to mention 'wrote regatta.yaml'; got %q", stdout)
	}
}

func TestInit_FriendlyOutput(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	wantPhrases := []string{
		"wrote regatta.yaml",
		"wrote .regatta/sample.diff",
		"Running L0 gate",
		"FAIL",
		"L0-TEXT-0",
		"Trap Pattern P3",
		"docs/incidents.md#pattern-3",
		"Next steps",
	}
	for _, p := range wantPhrases {
		if !bytes.Contains([]byte(stdout), []byte(p)) {
			t.Errorf("expected stdout to contain %q; if init prose changed intentionally, update this test\nfull stdout:\n%s", p, stdout)
		}
	}
}

func TestInit_ExitsZeroOnDemoFail(t *testing.T) {
	dir := t.TempDir()
	code, stdout, _ := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("init must exit 0 even when demo verdict is FAIL; got code=%d", code)
	}
	if !bytes.Contains([]byte(stdout), []byte("FAIL")) {
		t.Fatalf("expected demo verdict FAIL in stdout; got %q", stdout)
	}
}

func TestInit_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runInitInDir(t, dir, []string{"--json"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr on success with --json; got %q", stderr)
	}
	// The JSON is preceded by friendly prose lines (+ wrote ...), so extract
	// the JSON object from stdout by finding the opening brace.
	jsonStart := bytes.Index([]byte(stdout), []byte("{"))
	if jsonStart == -1 {
		t.Fatalf("no JSON object found in stdout: %q", stdout)
	}
	var env struct {
		Written     []string `json:"written"`
		Skipped     []string `json:"skipped"`
		Overwritten []string `json:"overwritten"`
		GateResult  struct {
			Verdict  string `json:"verdict"`
			GateID   string `json:"gate_id"`
			Findings []struct {
				ID          string `json:"id"`
				TrapPattern string `json:"trap_pattern"`
			} `json:"findings"`
		} `json:"gate_result"`
	}
	if err := json.Unmarshal([]byte(stdout[jsonStart:]), &env); err != nil {
		t.Fatalf("stdout contains invalid JSON: %v\nstdout=%q", err, stdout)
	}
	if got, want := env.GateResult.Verdict, "fail"; got != want {
		t.Errorf("gate_result.verdict=%q want %q", got, want)
	}
	if got, want := env.GateResult.GateID, "l0_spec_immutability"; got != want {
		t.Errorf("gate_result.gate_id=%q want %q", got, want)
	}
	if len(env.GateResult.Findings) == 0 || env.GateResult.Findings[0].TrapPattern != "P3" {
		t.Errorf("expected at least one finding with trap_pattern=P3; got %+v", env.GateResult.Findings)
	}
	if len(env.Written) != 2 {
		t.Errorf("expected 2 written files; got %v", env.Written)
	}
}

func TestInit_PatternBlurbFallback(t *testing.T) {
	t.Parallel()
	got := patternBlurb("P99")
	if got == "" {
		t.Fatal("patternBlurb returned empty string on unknown code")
	}
	if !bytes.Contains([]byte(got), []byte("docs/incidents.md")) {
		t.Errorf("fallback should point at docs/incidents.md; got %q", got)
	}
}
