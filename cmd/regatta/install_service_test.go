package main

import (
	"bytes"
	"strings"
	"testing"
)

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
