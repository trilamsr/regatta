package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/secrets"
)

func newSecretDeps(args []string, stdin string) (secretDeps, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	d := secretDeps{
		Args:   args,
		Stdin:  strings.NewReader(stdin),
		Stdout: &stdout,
		Stderr: &stderr,
		Now:    func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
	return d, &stdout, &stderr
}

// TestCLI_SecretGet_RefusesWithoutUnsafe asserts bare `get` never prints raw token to stdout (R10).
func TestCLI_SecretGet_RefusesWithoutUnsafe(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "sk-do-not-leak")
	d, stdout, _ := newSecretDeps([]string{"get", secrets.KeyAnthropic}, "")
	rc := runSecretWithDeps(d)
	if rc != 0 {
		t.Fatalf("exit=%d, want 0", rc)
	}
	if strings.Contains(stdout.String(), "sk-do-not-leak") {
		t.Fatalf("bare get leaked secret to stdout: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "present") {
		t.Fatalf("bare get missing presence summary: %q", stdout.String())
	}
}

// TestCLI_SecretGet_UnsafeFlagPrintsValue asserts --unsafe is the only path that writes raw bytes to stdout.
func TestCLI_SecretGet_UnsafeFlagPrintsValue(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "sk-unsafe-print")
	d, stdout, _ := newSecretDeps([]string{"get", secrets.KeyAnthropic, "--unsafe"}, "")
	rc := runSecretWithDeps(d)
	if rc != 0 {
		t.Fatalf("exit=%d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "sk-unsafe-print") {
		t.Fatalf("--unsafe did not print value: %q", stdout.String())
	}
}

// TestCLI_SecretGet_EmitsAuditEventWithoutValue asserts every get invocation lands an audit row that omits the value substring.
func TestCLI_SecretGet_EmitsAuditEventWithoutValue(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "sk-audit-leak-check")
	d, _, stderr := newSecretDeps([]string{"get", secrets.KeyAnthropic}, "")
	runSecretWithDeps(d)
	if !strings.Contains(stderr.String(), "audit_event") {
		t.Fatalf("missing audit_event in stderr: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), `"action":"secret_get"`) {
		t.Fatalf("audit_event missing action=secret_get: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "sk-audit-leak-check") {
		t.Fatalf("audit_event leaked secret: %q", stderr.String())
	}
}

// TestCLI_SecretGet_UnsafeFlagAuditedAsUnsafe asserts the --unsafe flag is recorded in the audit row.
func TestCLI_SecretGet_UnsafeFlagAuditedAsUnsafe(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "sk-x")
	d, _, stderr := newSecretDeps([]string{"get", secrets.KeyAnthropic, "--unsafe"}, "")
	runSecretWithDeps(d)
	if !strings.Contains(stderr.String(), `"unsafe":true`) {
		t.Fatalf("audit_event missing unsafe=true: %q", stderr.String())
	}
}

// TestCLI_SecretStatus_ShowsSourcePerKey asserts status prints one row per canonical key and never includes values.
func TestCLI_SecretStatus_ShowsSourcePerKey(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "sk-status-leak-check")
	t.Setenv("REGATTA_GH_TOKEN", "")
	d, stdout, _ := newSecretDeps([]string{"status"}, "")
	rc := runSecretWithDeps(d)
	if rc != 0 {
		t.Fatalf("exit=%d, want 0", rc)
	}
	out := stdout.String()
	if strings.Contains(out, "sk-status-leak-check") {
		t.Fatalf("status leaked secret: %q", out)
	}
	for _, k := range secrets.CanonicalKeys {
		if !strings.Contains(out, k) {
			t.Fatalf("status missing key row %q in output %q", k, out)
		}
	}
}

// TestCLI_SecretGet_RejectsInvalidKey asserts the canonical-key regex gates CLI input against path-traversal (R12).
func TestCLI_SecretGet_RejectsInvalidKey(t *testing.T) {
	d, _, stderr := newSecretDeps([]string{"get", "../etc/passwd"}, "")
	rc := runSecretWithDeps(d)
	if rc == 0 {
		t.Fatalf("exit=0 on invalid key; want non-zero")
	}
	if !strings.Contains(stderr.String(), "invalid secret key") {
		t.Fatalf("missing validation error: %q", stderr.String())
	}
}

// TestCLI_SecretList_HintsStatus asserts the dropped `list` verb redirects to `status` (deletion-default hint).
func TestCLI_SecretList_HintsStatus(t *testing.T) {
	d, _, stderr := newSecretDeps([]string{"list"}, "")
	rc := runSecretWithDeps(d)
	if rc != 2 {
		t.Fatalf("exit=%d, want 2", rc)
	}
	if !strings.Contains(stderr.String(), "regatta secret status") {
		t.Fatalf("missing status hint: %q", stderr.String())
	}
}
