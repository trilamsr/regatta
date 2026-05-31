package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// moduleRoot walks up from this file to the directory containing
// go.mod. Returns "" if not found (e.g. out-of-tree install); callers
// should t.Skip in that case.
func moduleRoot() string {
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
	root := moduleRoot()
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
	root := moduleRoot()
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
	// stdout must be pure JSON — no prose prefix. A scripted caller
	// like `regatta init --json | jq` must work.
	trimmed := strings.TrimSpace(stdout)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		t.Fatalf("--json stdout must start with '{'; got %q", stdout)
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
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
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
	if len(env.Skipped) != 0 {
		t.Errorf("expected 0 skipped files on fresh init; got %v", env.Skipped)
	}
	if len(env.Overwritten) != 0 {
		t.Errorf("expected 0 overwritten files on fresh init; got %v", env.Overwritten)
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

func TestInit_IdempotentReRun(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runInitInDir(t, dir, nil); code != 0 {
		t.Fatalf("first run failed: code=%d stderr=%q", code, stderr)
	}
	code, stdout, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("second run failed: code=%d stderr=%q", code, stderr)
	}
	if !bytes.Contains([]byte(stdout), []byte("= regatta.yaml unchanged")) {
		t.Errorf("expected '= regatta.yaml unchanged' marker; got %q", stdout)
	}
	if !bytes.Contains([]byte(stdout), []byte("= .regatta/sample.diff unchanged")) {
		t.Errorf("expected '= .regatta/sample.diff unchanged' marker; got %q", stdout)
	}
	// Demo MUST still run on every invocation — operator re-running
	// init expects to see the verdict, not just file-state markers.
	if !bytes.Contains([]byte(stdout), []byte("FAIL")) {
		t.Errorf("expected demo FAIL verdict on re-run; got %q", stdout)
	}
	if !bytes.Contains([]byte(stdout), []byte("Running L0 gate")) {
		t.Errorf("expected demo prose header on re-run; got %q", stdout)
	}
}

func TestInit_RefusesDivergedYaml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), []byte("# operator hand-edit\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 2 {
		t.Fatalf("expected exit 2 on diverged yaml; got %d stderr=%q", code, stderr)
	}
	for _, want := range []string{"regatta.yaml", "--force"} {
		if !bytes.Contains([]byte(stderr), []byte(want)) {
			t.Errorf("stderr should mention %q; got %q", want, stderr)
		}
	}
}

func TestInit_DivergedSampleDiff(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".regatta"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".regatta", "sample.diff"), []byte("not the embedded blob\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 2 {
		t.Fatalf("expected exit 2 on diverged sample.diff; got %d stderr=%q", code, stderr)
	}
	if !bytes.Contains([]byte(stderr), []byte(".regatta/sample.diff")) || !bytes.Contains([]byte(stderr), []byte("--force")) {
		t.Errorf("stderr should mention path + --force; got %q", stderr)
	}
	// Atomicity reverse direction: yaml must NOT be written when
	// sample.diff diverges.
	if _, err := os.Stat(filepath.Join(dir, "regatta.yaml")); !os.IsNotExist(err) {
		t.Fatalf("regatta.yaml should not have been written when sample.diff diverged; err=%v", err)
	}
}

func TestInit_FailsAtomicallyOnDivergence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), []byte("# operator hand-edit\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, _, _ := runInitInDir(t, dir, nil)
	if code != 2 {
		t.Fatalf("expected exit 2; got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".regatta", "sample.diff")); !os.IsNotExist(err) {
		t.Fatalf("sample.diff should not have been written when yaml diverged; err=%v", err)
	}
}

func TestInit_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	seed := []byte("operator content\n")
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), seed, 0o644); err != nil {
		t.Fatalf("seed yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".regatta"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".regatta", "sample.diff"), seed, 0o644); err != nil {
		t.Fatalf("seed diff: %v", err)
	}
	code, stdout, stderr := runInitInDir(t, dir, []string{"--force"})
	if code != 0 {
		t.Fatalf("expected exit 0 with --force; got %d stderr=%q", code, stderr)
	}
	yaml, _ := os.ReadFile(filepath.Join(dir, "regatta.yaml"))
	if bytes.Equal(yaml, seed) {
		t.Errorf("--force should have overwritten regatta.yaml")
	}
	if !bytes.Contains([]byte(stdout), []byte("! overwrote regatta.yaml")) {
		t.Errorf("expected '! overwrote' marker; got %q", stdout)
	}
}

func TestInit_RefusesSymlinkRegatta(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows; defense still active in code")
	}
	dir := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(dir, ".regatta")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 2 {
		t.Fatalf("expected exit 2; got %d stderr=%q", code, stderr)
	}
	if !bytes.Contains([]byte(stderr), []byte("not a regular directory")) {
		t.Errorf("stderr should explain the refusal; got %q", stderr)
	}
}

func TestInit_LeavesPopulatedRegattaDirAlone(t *testing.T) {
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	keep := filepath.Join(itemsDir, "foo.md")
	if err := os.WriteFile(keep, []byte("# foo\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("expected exit 0; got %d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("init should not have touched .regatta/items/foo.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".regatta", "sample.diff")); err != nil {
		t.Fatalf("sample.diff should have been written: %v", err)
	}
}

func TestInit_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("init must not require git; got code=%d stderr=%q", code, stderr)
	}
}

func TestInit_HelpFlag(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		var out, errOut bytes.Buffer
		code := runInitWithIO([]string{arg}, &out, &errOut)
		if code != 0 {
			t.Errorf("%s exit=%d stderr=%q", arg, code, errOut.String())
		}
		combined := out.String() + errOut.String()
		if !bytes.Contains([]byte(combined), []byte("force")) {
			t.Errorf("%s should describe --force flag; got %q", arg, combined)
		}
	}
}

func TestInitUsesEmbeddedBytes(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runInitInDir(t, dir, nil); code != 0 {
		t.Fatalf("init: code=%d stderr=%q", code, stderr)
	}
	written, err := os.ReadFile(filepath.Join(dir, "regatta.yaml"))
	if err != nil {
		t.Fatalf("read written: %v", err)
	}
	embedded, err := initAssets.ReadFile("init_assets/regatta.yaml")
	if err != nil {
		t.Fatalf("read embed: %v", err)
	}
	if !bytes.Equal(written, embedded) {
		t.Fatalf("init wrote bytes that diverge from the embed.FS blob; if init now sources from disk instead of embed, the drift gate is defeated")
	}
}

func TestInit_ForceJSONOverwriteEnvelope(t *testing.T) {
	dir := t.TempDir()
	seed := []byte("operator edit\n")
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), seed, 0o600); err != nil {
		t.Fatalf("seed yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".regatta"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".regatta", "sample.diff"), seed, 0o600); err != nil {
		t.Fatalf("seed diff: %v", err)
	}
	code, stdout, stderr := runInitInDir(t, dir, []string{"--force", "--json"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr; got %q", stderr)
	}
	var env struct {
		Written     []string `json:"written"`
		Skipped     []string `json:"skipped"`
		Overwritten []string `json:"overwritten"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout=%q", err, stdout)
	}
	if len(env.Overwritten) != 2 {
		t.Errorf("expected 2 overwritten; got %v", env.Overwritten)
	}
	if len(env.Written) != 0 {
		t.Errorf("expected 0 written; got %v", env.Written)
	}
}

func TestInit_RefusesRegularFileAtRegattaPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".regatta"), []byte("not a dir\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 2 {
		t.Fatalf("expected exit 2; got %d stderr=%q", code, stderr)
	}
	if !bytes.Contains([]byte(stderr), []byte("not a regular directory")) {
		t.Errorf("stderr should explain the refusal; got %q", stderr)
	}
}
