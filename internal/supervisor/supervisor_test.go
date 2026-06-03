package supervisor

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeOKHealthz spins up an httptest server that always returns ok —
// every helper now routes through Install() step-7 so pre-existing
// happy-path tests need a healthz URL the poller can reach.
func fakeOKHealthz(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// noopRunner stubs every subprocess call to zero-byte success so
// pre-bootstrap tests stay hermetic — no real launchctl/systemctl.
func noopRunner(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, nil
}

func newDarwinOpts(t *testing.T) (Options, string) {
	t.Helper()
	home := t.TempDir()
	return Options{
		Mode:       ModeUser,
		Out:        &bytes.Buffer{},
		Err:        &bytes.Buffer{},
		Now:        func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
		GOOS:       "darwin",
		Binary:     "/opt/homebrew/bin/regatta",
		HomeDir:    home,
		Runner:     noopRunner,
		HealthzURL: fakeOKHealthz(t),
	}, home
}

func newLinuxOpts(t *testing.T) (Options, string) {
	t.Helper()
	home := t.TempDir()
	return Options{
		Mode:       ModeUser,
		Out:        &bytes.Buffer{},
		Err:        &bytes.Buffer{},
		Now:        func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
		GOOS:       "linux",
		Binary:     "/usr/local/bin/regatta",
		HomeDir:    home,
		NoCron:     true,
		Runner:     noopRunner,
		HealthzURL: fakeOKHealthz(t),
	}, home
}

// TestInstallService_FreshDarwin_WritesPlist covers spec §3.4 happy path.
func TestInstallService_FreshDarwin_WritesPlist(t *testing.T) {
	opts, home := newDarwinOpts(t)
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.regatta.serve.plist")
	b, err := os.ReadFile(plist)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	if !strings.Contains(string(b), "<key>Label</key>") {
		t.Fatal("plist missing Label key")
	}
	if !strings.Contains(string(b), "/opt/homebrew/bin/regatta") {
		t.Fatal("plist missing binary path")
	}
}

// TestInstallService_FreshLinux_WritesSystemdUnit covers spec §3.3 happy path.
func TestInstallService_FreshLinux_WritesSystemdUnit(t *testing.T) {
	opts, home := newLinuxOpts(t)
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	unit := filepath.Join(home, ".config", "systemd", "user", "regatta.service")
	b, err := os.ReadFile(unit)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	s := string(b)
	for _, want := range []string{"[Service]", "Type=notify", "WatchdogSec=30", "Restart=on-failure"} {
		if !strings.Contains(s, want) {
			t.Errorf("unit missing %q", want)
		}
	}
}

// TestInstallService_IdempotentSameContent_Skips covers spec §3.1 branch A.
func TestInstallService_IdempotentSameContent_Skips(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Install(opts); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !strings.Contains(buf.String(), "already installed") {
		t.Fatalf("expected idempotent skip msg, got %q", buf.String())
	}
}

// TestInstallService_DifferentContentWithoutForce_Refuses covers spec §3.1 branch C.
func TestInstallService_DifferentContentWithoutForce_Refuses(t *testing.T) {
	opts, home := newDarwinOpts(t)
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.regatta.serve.plist")
	if err := os.WriteFile(plist, []byte("<modified/>\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := Install(opts)
	if err == nil {
		t.Fatal("expected refusal, got nil")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error should mention --force; got %v", err)
	}
}

// TestInstallService_DifferentContentWithForce_BacksUpAndApplies covers branch B.
func TestInstallService_DifferentContentWithForce_BacksUpAndApplies(t *testing.T) {
	opts, home := newDarwinOpts(t)
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.regatta.serve.plist")
	if err := os.WriteFile(plist, []byte("<modified/>\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	opts.Force = true
	if err := Install(opts); err != nil {
		t.Fatalf("force install: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(plist))
	if err != nil {
		t.Fatal(err)
	}
	var foundBak bool
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.") {
			foundBak = true
		}
	}
	if !foundBak {
		t.Fatal("expected .bak file after --force overwrite")
	}
}

// TestInstallService_PlutilMissing_FallsBackToTextValidate covers spec §3.1 step 5 fallback.
func TestInstallService_PlutilMissing_FallsBackToTextValidate(t *testing.T) {
	// strip $PATH so plutil is unfindable
	t.Setenv("PATH", "")
	opts, _ := newDarwinOpts(t)
	errBuf := &bytes.Buffer{}
	opts.Err = errBuf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(errBuf.String(), "plutil not on PATH") {
		t.Fatalf("expected WARN about plutil, got %q", errBuf.String())
	}
}

// TestUninstallService_AlreadyClean_ExitsZero covers spec §3.8 row 4.
func TestUninstallService_AlreadyClean_ExitsZero(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Uninstall(opts); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(buf.String(), "nothing to remove") {
		t.Fatalf("expected nothing-to-remove msg, got %q", buf.String())
	}
}

// TestUninstallService_LeftoverUnitFile_Removes covers spec §3.8 row 3.
func TestUninstallService_LeftoverUnitFile_Removes(t *testing.T) {
	opts, home := newDarwinOpts(t)
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.regatta.serve.plist")
	if _, err := os.Stat(plist); err != nil {
		t.Fatalf("pre: plist missing: %v", err)
	}
	if err := Uninstall(opts); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatal("expected plist removed")
	}
}

// TestPlistRender_AppleSiliconBrew_HasOptHomebrewInPath covers spec §3.4 PATH ordering.
func TestPlistRender_AppleSiliconBrew_HasOptHomebrewInPath(t *testing.T) {
	got := resolveMacPath("/opt/homebrew/bin/regatta")
	if !strings.HasPrefix(got, "/opt/homebrew/bin") {
		t.Fatalf("want /opt/homebrew prefix, got %q", got)
	}
}

// TestPlistRender_IntelBrew_HasUsrLocalInPath covers spec §3.4 PATH ordering.
func TestPlistRender_IntelBrew_HasUsrLocalInPath(t *testing.T) {
	got := resolveMacPath("/usr/local/bin/regatta")
	if !strings.HasPrefix(got, "/usr/local/bin") {
		t.Fatalf("want /usr/local prefix, got %q", got)
	}
}

// TestCronStripBlock_RemovesAnchoredBlock covers spec §3.7 idempotency.
func TestCronStripBlock_RemovesAnchoredBlock(t *testing.T) {
	original := "0 1 * * * /pre-existing.sh\n" + cronTemplate + "# user trailing line\n"
	stripped := stripCronBlock(original)
	if strings.Contains(stripped, "BEGIN regatta cron") {
		t.Fatal("anchored block survived strip")
	}
	if !strings.Contains(stripped, "/pre-existing.sh") {
		t.Fatal("foreign cron lines must survive")
	}
	if !strings.Contains(stripped, "trailing line") {
		t.Fatal("trailing user lines must survive")
	}
}

// TestSanitizePath_RejectsNewlineInjection covers adversarial template-injection risk.
func TestSanitizePath_RejectsNewlineInjection(t *testing.T) {
	if err := sanitizePath("/usr/local/bin/regatta\nExecStart=/bin/sh"); err == nil {
		t.Fatal("expected sanitize to reject newline")
	}
}

// TestInstallService_DryRun_WritesNothing covers spec §3.8 --dry-run.
func TestInstallService_DryRun_WritesNothing(t *testing.T) {
	opts, home := newDarwinOpts(t)
	opts.DryRun = true
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Install(opts); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.regatta.serve.plist")
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write plist")
	}
	if !strings.Contains(buf.String(), "=== rendered ===") {
		t.Fatal("dry-run must print rendered template")
	}
}
