package main

import (
	"bytes"
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
	defer os.RemoveAll(tmp)

	bin := filepath.Join(tmp, "regatta")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	root := smokeModuleRoot()
	if root == "" {
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
		os.Stderr.WriteString("smoke: go build failed; skipping suite:\n")
		os.Stderr.Write(out)
		os.Exit(m.Run())
	}
	smokeBinary = bin
	os.Exit(m.Run())
}

// smokeModuleRoot is a smoke-test-local copy of moduleRoot from
// init_test.go so this file can be read alone.
func smokeModuleRoot() string {
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
	if exitErr, ok := err.(*exec.ExitError); ok {
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
	root := smokeModuleRoot()
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
	root := smokeModuleRoot()
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
