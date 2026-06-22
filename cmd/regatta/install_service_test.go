package main

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestInstallService_RefusesInsideContainer asserts the verb refuses when /.dockerenv signals a non-systemd/non-launchd host.
func TestInstallService_RefusesInsideContainer(t *testing.T) {
	tmpDir := t.TempDir()
	sentinel := tmpDir + "/.dockerenv"
	f, err := os.Create(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	t.Setenv("REGATTA_CONTAINER_SENTINEL_DIR", tmpDir)

	var out, errBuf bytes.Buffer
	code := runInstallServiceTo(&out, &errBuf, []string{"--dry-run"})
	if code != 1 {
		t.Fatalf("exit=%d want 1 (container detected); stderr=%q stdout=%q", code, errBuf.String(), out.String())
	}
	if !strings.Contains(errBuf.String(), "container") {
		t.Errorf("stderr should mention container detection; got %q", errBuf.String())
	}
}

// TestInstallServiceHealthzURLFlag asserts --healthz-url overrides hardcoded :8080 default (#667).
func TestInstallServiceHealthzURLFlag(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runInstallServiceTo(&out, &errBuf, []string{
		"--dry-run",
		"--healthz-url", "http://127.0.0.1:9876/healthz",
	})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), ":9876") {
		t.Fatalf("dry-run output missing operator-supplied :9876 healthz port; got:\n%s", out.String())
	}
	if strings.Contains(out.String(), ":8080/healthz") {
		t.Fatalf("dry-run leaked hardcoded :8080 default despite override; got:\n%s", out.String())
	}
}

// TestInstallServiceEnvFileFlag asserts --env-file flag threads into the macOS plist sourcing wrapper (followup #826).
func TestInstallServiceEnvFileFlag(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runInstallServiceTo(&out, &errBuf, []string{
		"--dry-run",
		"--env-file", "/tmp/regatta-cli-test.env",
	})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "/tmp/regatta-cli-test.env") {
		t.Fatalf("env-file path missing from dry-run output; got:\n%s", out.String())
	}
}

// TestInstallService_NameFlag asserts --name threads through to supervisor.Options.Name (#929).
func TestInstallService_NameFlag(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runInstallServiceTo(&out, &errBuf, []string{
		"--dry-run",
		"--name", "myrepo",
	})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	// Name="myrepo" suffixes the unit identifier (launchd Label on darwin, systemd UnitName elsewhere), WorkingDir, and EnvFile.
	unitID := "com.regatta.serve.myrepo"
	if runtime.GOOS != "darwin" {
		unitID = "regatta-myrepo.service"
	}
	for _, want := range []string{unitID, "regatta/myrepo", "regatta/myrepo/env"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry-run output missing %q; got:\n%s", want, out.String())
		}
	}
}

// TestInstallService_NameFlag_Validation rejects --name charsets outside [a-z0-9-]{1,32} (spec §6.4, #933 reviewer-HIGH).
func TestInstallService_NameFlag_Validation(t *testing.T) {
	cases := []struct {
		label string
		name  string
	}{
		{"path traversal", ".."},
		{"slash injection", "a/b"},
		{"newline injection", "a\nb"},
		{"space", "my repo"},
		{"uppercase", "MyRepo"},
		{"leading hyphen", "-repo"},
		{"empty", ""},
		{"too long (33)", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"dot", "my.repo"},
		{"underscore", "my_repo"},
		{"null byte", "a\x00b"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if tc.name == "" {
				// Empty is the back-compat default — must NOT be rejected at the CLI.
				var out, errBuf bytes.Buffer
				code := runInstallServiceTo(&out, &errBuf, []string{"--dry-run"})
				if code != 0 {
					t.Fatalf("empty --name (default) must succeed; exit=%d stderr=%s", code, errBuf.String())
				}
				return
			}
			var out, errBuf bytes.Buffer
			code := runInstallServiceTo(&out, &errBuf, []string{"--dry-run", "--name", tc.name})
			if code == 0 {
				t.Fatalf("--name=%q must be rejected; exit=0 stdout=%s", tc.name, out.String())
			}
			if !strings.Contains(errBuf.String(), "name") {
				t.Errorf("expected error mentioning 'name'; got %q", errBuf.String())
			}
		})
	}
}

// TestInstallService_NameFlag_Accepts pins valid --name charsets pass validation (#933 reviewer-HIGH).
func TestInstallService_NameFlag_Accepts(t *testing.T) {
	for _, name := range []string{"a", "myrepo", "my-repo", "repo123", "a1b2c3", "abcdefghijklmnopqrstuvwxyz012345"} {
		t.Run(name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			code := runInstallServiceTo(&out, &errBuf, []string{"--dry-run", "--name", name})
			if code != 0 {
				t.Fatalf("--name=%q must succeed; exit=%d stderr=%s", name, code, errBuf.String())
			}
		})
	}
}

// TestInstallServiceHealthzURLDefault asserts loopback :8080 fallback when --healthz-url omitted (#667).
func TestInstallServiceHealthzURLDefault(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runInstallServiceTo(&out, &errBuf, []string{"--dry-run"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "127.0.0.1:8080/healthz") {
		t.Fatalf("dry-run default healthz URL missing; got:\n%s", out.String())
	}
}
