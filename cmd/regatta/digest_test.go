package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDigest_WritesFileToDocsDigests locks the --root → docs/digests/YYYY-MM-DD.md write contract.
func TestDigest_WritesFileToDocsDigests(t *testing.T) {
	root := t.TempDir()
	// Unset any inherited Prom env vars so the no-source banner path
	// fires deterministically in CI.
	t.Setenv("DIGEST_PROM_URL", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_METRICS_PROMETHEUS_PORT", "")

	code := runDigest([]string{"--date", "2026-06-03", "--root", root})
	if code != 0 {
		t.Fatalf("runDigest exit = %d; want 0", code)
	}
	out := filepath.Join(root, "docs", "digests", "2026-06-03.md")
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read written digest: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "date: 2026-06-03") {
		t.Errorf("digest missing front-matter date; got:\n%s", s)
	}
	if !strings.Contains(s, "metrics backend unreachable") {
		t.Errorf("expected backend-down banner with no Prom env; got:\n%s", s)
	}
}

// TestDigest_FutureDateRejected asserts a --date past today exits 2 (no events can exist; silent empty file misleads operators).
func TestDigest_FutureDateRejected(t *testing.T) {
	root := t.TempDir()
	code := runDigest([]string{"--date", "2099-12-31", "--root", root})
	if code != 2 {
		t.Errorf("runDigest with future date exit = %d; want 2", code)
	}
	out := filepath.Join(root, "docs", "digests", "2099-12-31.md")
	if _, err := os.Stat(out); err == nil {
		t.Errorf("digest %s should NOT have been written for future date", out)
	}
}

// TestDigest_BadDateExits2 locks the exit-2 usage-error contract on malformed --date.
func TestDigest_BadDateExits2(t *testing.T) {
	root := t.TempDir()
	code := runDigest([]string{"--date", "tomorrow", "--root", root})
	if code != 2 {
		t.Errorf("runDigest with bad date exit = %d; want 2", code)
	}
}

// TestDigest_RegisteredInDispatch backstops a forgotten append to subcommands[] when adding the verb.
func TestDigest_RegisteredInDispatch(t *testing.T) {
	var found bool
	for _, sc := range subcommands {
		if sc.name == subcmdDigest {
			found = true
			if sc.run == nil {
				t.Errorf("digest subcommand has nil run")
			}
			break
		}
	}
	if !found {
		t.Errorf("digest subcommand not registered in subcommands[]")
	}
}
