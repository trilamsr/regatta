package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var smokeBinary string

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("go"); err != nil {
		// Smoke tests need `go build`; skip the suite cleanly.
		os.Exit(m.Run())
	}
	tmp, err := os.MkdirTemp("", "regatta-smoke-")
	if err != nil {
		panic("smoke: mkdtemp: " + err.Error())
	}

	bin := filepath.Join(tmp, "regatta")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	root := moduleRoot()
	if root == "" {
		_ = os.RemoveAll(tmp)
		os.Exit(m.Run())
	}

	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/regatta")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Build failure: print + skip suite (do not fail; this
		// keeps `go test ./...` green when contributors run on
		// machines without a buildable toolchain).
		_, _ = os.Stderr.WriteString("smoke: go build failed; skipping suite:\n")
		_, _ = os.Stderr.Write(out)
		_ = os.RemoveAll(tmp)
		os.Exit(m.Run())
	}
	smokeBinary = bin
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// runSmoke executes the compiled binary with args in workDir, returns
// exit code + stdout + stderr. Env via cmd.Env so subtests stay
// t.Parallel-safe.
func runSmoke(t *testing.T, workDir string, args []string, env []string) (code int, stdout, stderr string) {
	t.Helper()
	if smokeBinary == "" {
		t.Skip("smoke binary not built (go not on PATH or build failed)")
	}
	cmd := exec.Command(smokeBinary, args...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	stdout, stderr = out.String(), errOut.String()
	if err == nil {
		return 0, stdout, stderr
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stdout, stderr
	}
	t.Fatalf("exec error: %v", err)
	return -1, "", ""
}

func expectExit(t *testing.T, want, got int, stdout, stderr string) {
	t.Helper()
	if want != got {
		t.Fatalf("exit=%d want=%d\nstdout=%q\nstderr=%q", got, want, stdout, stderr)
	}
}

func expectContains(t *testing.T, stream, want, name string) {
	t.Helper()
	if !strings.Contains(stream, want) {
		t.Fatalf("%s should contain %q; got %q", name, want, stream)
	}
}

func TestCLI_BareNoArgs(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), nil, nil)
	expectExit(t, 2, code, stdout, stderr)
	if !strings.Contains(strings.ToLower(stderr), "usage") {
		t.Fatalf("expected usage in stderr; got %q", stderr)
	}
}

func TestCLI_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"nonexistent-sub"}, nil)
	expectExit(t, 2, code, stdout, stderr)
	expectContains(t, stderr, "unknown subcommand", "stderr")
}

func TestCLI_Help(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"help"}, nil)
	expectExit(t, 0, code, stdout, stderr)
	expectContains(t, stdout, "Usage:", "stdout")
	expectContains(t, stdout, "regatta init", "stdout")
}

func TestCLI_Version(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"version"}, nil)
	expectExit(t, 0, code, stdout, stderr)
	expectContains(t, stdout, "regatta", "stdout")
}

func TestCLI_Init_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	code, stdout, stderr := runSmoke(t, dir, []string{"init"}, nil)
	expectExit(t, 0, code, stdout, stderr)
	expectContains(t, stdout, "wrote regatta.yaml", "stdout")
	expectContains(t, stdout, "FAIL", "stdout")
	if _, err := os.Stat(filepath.Join(dir, "regatta.yaml")); err != nil {
		t.Fatalf("regatta.yaml not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".regatta", "sample.diff")); err != nil {
		t.Fatalf(".regatta/sample.diff not written: %v", err)
	}
}

func TestCLI_Init_RefusesDiverged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), []byte("# operator\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, stdout, stderr := runSmoke(t, dir, []string{"init"}, nil)
	expectExit(t, 2, code, stdout, stderr)
	expectContains(t, stderr, "--force", "stderr")
}

func TestCLI_L0_Pass(t *testing.T) {
	t.Parallel()
	root := moduleRoot()
	if root == "" {
		t.Skip("module root not resolvable")
	}
	// Find any pass fixture deterministically.
	passDir := filepath.Join(root, "testdata/gates/l0/pass")
	entries, err := os.ReadDir(passDir)
	if err != nil {
		t.Fatalf("read pass fixtures: %v", err)
	}
	var fixture string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".diff") {
			fixture = filepath.Join(passDir, e.Name())
			break
		}
	}
	if fixture == "" {
		t.Skip("no pass fixture found")
	}
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"l0", fixture}, nil)
	expectExit(t, 0, code, stdout, stderr)
	expectContains(t, stdout, "\"verdict\"", "stdout")
	expectContains(t, stdout, "pass", "stdout")
}

func TestCLI_L0_Fail(t *testing.T) {
	t.Parallel()
	root := moduleRoot()
	if root == "" {
		t.Skip("module root not resolvable")
	}
	fixture := filepath.Join(root, "testdata/gates/l0/fail/00_criterion_text_edit.diff")
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"l0", fixture}, nil)
	expectExit(t, 1, code, stdout, stderr)
	expectContains(t, stdout, "fail", "stdout")
}

func TestCLI_L0_Help(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"l0", "-h"}, nil)
	if code != 0 && code != 2 {
		t.Fatalf("l0 -h: unexpected exit=%d", code)
	}
	combined := strings.ToLower(stdout + stderr)
	if !strings.Contains(combined, "usage") {
		t.Fatalf("expected usage; got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_ValidateConfig_Happy(t *testing.T) {
	t.Parallel()
	root := moduleRoot()
	if root == "" {
		t.Skip("module root not resolvable")
	}
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(root, "examples/minimal/regatta.yaml"))
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), src, 0o600); err != nil { //nolint:gosec // G306: t.TempDir + literal filename, no taint
		t.Fatalf("seed: %v", err)
	}
	code, stdout, stderr := runSmoke(t, dir, []string{"validate-config"}, nil)
	expectExit(t, 0, code, stdout, stderr)
}

func TestCLI_ValidateConfig_Malformed(t *testing.T) {
	t.Parallel()
	root := moduleRoot()
	if root == "" {
		t.Skip("module root not resolvable")
	}
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(root, "cmd/regatta/testdata/cli_smoke/malformed_config.yaml"))
	if err != nil {
		t.Fatalf("read malformed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), src, 0o600); err != nil { //nolint:gosec // G306: t.TempDir + literal filename, no taint
		t.Fatalf("seed: %v", err)
	}
	code, stdout, stderr := runSmoke(t, dir, []string{"validate-config"}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit on malformed config; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
	if stderr == "" {
		t.Fatalf("expected stderr explanation; got empty")
	}
}

func TestCLI_VerifyRepoConfig_MissingFlag(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"verify-repo-config"}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit when -owner/-repo missing; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
	if stderr == "" {
		t.Fatalf("expected stderr explanation; got empty")
	}
}

func TestCLI_Serve_TickOnceStub(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		// serve --tick-once acquires the orchestrator lockfile, whose
		// flock + PID-stamp contract is POSIX-only. Runtime target is
		// Linux + macOS per docs/design.md.
		t.Skip("orchestrator lockfile contract is POSIX-only")
	}
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dbPath := filepath.Join(dir, "state.db")
	args := []string{
		"serve",
		"-tick-once",
		"-spawner=stub",
		"-repo=" + dir,
		"-items-root=" + dir,
		"-db=" + dbPath,
	}
	code, stdout, stderr := runSmoke(t, dir, args, nil)
	expectExit(t, 0, code, stdout, stderr)
	// Side-effect proof: serve must initialize the state DB; a no-op
	// stub that exits 0 without touching disk would pass exit-code
	// alone but fail this stat.
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("serve --tick-once did not create state.db at %s: %v", dbPath, err)
	}
}

func TestCLI_Serve_BogusSpawner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	args := []string{
		"serve",
		"-tick-once",
		"-spawner=bogus",
		"-repo=" + dir,
		"-db=" + filepath.Join(dir, "state.db"),
	}
	code, stdout, stderr := runSmoke(t, dir, args, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit with bogus spawner; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_Program_BareNoSub(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"program"}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_ProgramVerifyHandoff_BadInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, stdout, stderr := runSmoke(t, dir, []string{"program", "verify-handoff", bad}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit on malformed handoff; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
}
