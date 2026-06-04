package supervisor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newDarwinOpts(t *testing.T) (Options, string) {
	t.Helper()
	home := t.TempDir()
	return Options{
		Mode:    ModeUser,
		Out:     &bytes.Buffer{},
		Err:     &bytes.Buffer{},
		Now:     func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
		GOOS:    "darwin",
		Binary:  "/opt/homebrew/bin/regatta",
		HomeDir: home,
	}, home
}

func newLinuxOpts(t *testing.T) (Options, string) {
	t.Helper()
	home := t.TempDir()
	return Options{
		Mode:    ModeUser,
		Out:     &bytes.Buffer{},
		Err:     &bytes.Buffer{},
		Now:     func() time.Time { return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC) },
		GOOS:    "linux",
		Binary:  "/usr/local/bin/regatta",
		HomeDir: home,
		NoCron:  true,
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

// TestResolveHealthzURL_OverrideWins asserts Options.HealthzURL wins over the :8080 default (#667).
func TestResolveHealthzURL_OverrideWins(t *testing.T) {
	got := ResolveHealthzURL(Options{HealthzURL: "http://127.0.0.1:9876/healthz"})
	if got != "http://127.0.0.1:9876/healthz" {
		t.Fatalf("got %q want override", got)
	}
}

// TestResolveHealthzURL_EmptyFallsBack asserts the :8080 loopback default when override is empty (#667).
func TestResolveHealthzURL_EmptyFallsBack(t *testing.T) {
	if got := ResolveHealthzURL(Options{}); got != DefaultHealthzURL {
		t.Fatalf("got %q want %q", got, DefaultHealthzURL)
	}
}

// TestInstallService_DryRun_PrintsHealthzURL asserts dry-run surfaces the resolved healthz URL so operators verify before install (#667).
func TestInstallService_DryRun_PrintsHealthzURL(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	opts.DryRun = true
	opts.HealthzURL = "http://127.0.0.1:9876/healthz"
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(buf.String(), ":9876") {
		t.Fatalf("dry-run missing operator override; got:\n%s", buf.String())
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

// TestPlistRender_EnvFile_SourcingWrapper asserts macOS plist wraps the binary
// invocation in `/bin/sh -lc 'set -a; . <env>; set +a; exec <bin> serve ...'` so
// launchd injects ANTHROPIC_API_KEY + GH_TOKEN from the operator's .env file —
// launchd has no native EnvironmentFile equivalent (closes followup to #826).
func TestPlistRender_EnvFile_SourcingWrapper(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	opts.DryRun = true
	opts.EnvFile = "/tmp/regatta-test.env"
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		"<string>/bin/sh</string>",
		"<string>-lc</string>",
		`set -a; . "/tmp/regatta-test.env"; set +a; exec`,
		"/opt/homebrew/bin/regatta",
		"serve --repo",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered plist missing %q; got:\n%s", want, got)
		}
	}
}

// TestPlistRender_EnvFile_DefaultUserPath asserts opts.EnvFile defaults to
// $HOME/.config/regatta/env when caller omits it on macOS user-mode install
// (parity with Linux's /etc/regatta/env default).
func TestPlistRender_EnvFile_DefaultUserPath(t *testing.T) {
	opts, home := newDarwinOpts(t)
	opts.DryRun = true
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	want := filepath.Join(home, ".config", "regatta", "env")
	if !strings.Contains(buf.String(), want) {
		t.Errorf("expected default env path %q in rendered plist; got:\n%s", want, buf.String())
	}
}

// TestPlistRender_EnvFile_DefaultSystemPath asserts macOS system-mode falls back
// to /etc/regatta/env so root-installed agents land on the same path as Linux.
func TestPlistRender_EnvFile_DefaultSystemPath(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	opts.Mode = ModeSystem
	opts.DryRun = true
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(buf.String(), "/etc/regatta/env") {
		t.Errorf("expected /etc/regatta/env default; got:\n%s", buf.String())
	}
}

// TestPlistRender_EnvFile_QuotesPathForShell asserts the env-file path is
// double-quoted in the sourced shell command — a path with a space MUST not
// split-word into multiple `.` arguments and break launchd boot.
func TestPlistRender_EnvFile_QuotesPathForShell(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	opts.DryRun = true
	opts.EnvFile = "/Users/op name/regatta/env"
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(buf.String(), `. "/Users/op name/regatta/env"`) {
		t.Errorf("env-file path not double-quoted; got:\n%s", buf.String())
	}
}

// TestPlistRender_EnvFile_OverrideWinsOverDefault asserts an explicit
// opts.EnvFile beats the user-mode default fallback.
func TestPlistRender_EnvFile_OverrideWinsOverDefault(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	opts.DryRun = true
	opts.EnvFile = "/custom/path/env"
	buf := &bytes.Buffer{}
	opts.Out = buf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "/custom/path/env") {
		t.Errorf("override env-file missing; got:\n%s", out)
	}
	if strings.Contains(out, ".config/regatta/env") {
		t.Errorf("override should suppress default; got:\n%s", out)
	}
}

// TestPlistRender_EnvFile_RejectsShellMetacharacters asserts the env-file path
// is sanitised against shell-injection — operator-supplied paths containing
// `"` or `$` would otherwise break out of the sourcing string.
func TestPlistRender_EnvFile_RejectsShellMetacharacters(t *testing.T) {
	for _, bad := range []string{`/tmp/x"$(rm)"`, "/tmp/x\nFOO=BAR"} {
		opts, _ := newDarwinOpts(t)
		opts.DryRun = true
		opts.EnvFile = bad
		if err := Install(opts); err == nil {
			t.Errorf("expected rejection for %q", bad)
		}
	}
}

// TestPreInstallEnvFileCheck_MissingFile_Warns asserts a missing env-file
// surfaces a WARN (not a fail) — operator may set env via shell before launchd
// loads the agent; fail-closed would block first-time installs.
func TestPreInstallEnvFileCheck_MissingFile_Warns(t *testing.T) {
	opts, _ := newDarwinOpts(t)
	opts.EnvFile = filepath.Join(t.TempDir(), "missing.env")
	errBuf := &bytes.Buffer{}
	opts.Err = errBuf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(errBuf.String(), "env-file") || !strings.Contains(errBuf.String(), "missing") {
		t.Errorf("expected WARN about missing env-file; got:\n%s", errBuf.String())
	}
}

// TestPreInstallEnvFileCheck_LooseModeWarns asserts mode != 0600 on the
// env-file emits a hygiene WARN — secrets readable by other users is a leak
// vector worth a one-line nudge.
func TestPreInstallEnvFileCheck_LooseModeWarns(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")
	if err := os.WriteFile(envPath, []byte("ANTHROPIC_API_KEY=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, _ := newDarwinOpts(t)
	opts.EnvFile = envPath
	errBuf := &bytes.Buffer{}
	opts.Err = errBuf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(errBuf.String(), "0600") {
		t.Errorf("expected mode-hygiene WARN; got:\n%s", errBuf.String())
	}
}

// TestPreInstallEnvFileCheck_TightModeSilent asserts a 0600 env-file passes
// without WARN noise so well-configured operators see a clean install log.
func TestPreInstallEnvFileCheck_TightModeSilent(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")
	if err := os.WriteFile(envPath, []byte("ANTHROPIC_API_KEY=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, _ := newDarwinOpts(t)
	opts.EnvFile = envPath
	errBuf := &bytes.Buffer{}
	opts.Err = errBuf
	if err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if strings.Contains(errBuf.String(), "env-file") {
		t.Errorf("clean install should not WARN; got:\n%s", errBuf.String())
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
