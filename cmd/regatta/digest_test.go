package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDigest_WritesFileToDocsDigests asserts the CLI subcommand writes
// `docs/digests/YYYY-MM-DD.md` relative to --root and exits 0 on the
// happy path. Uses the noop source (no Prom env var) so the test runs
// offline.
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

// TestDigest_BadDateExits2 asserts a malformed --date surfaces a
// usage-error exit code so the cron wrapper fails loud rather than
// writing a corrupt digest file.
func TestDigest_BadDateExits2(t *testing.T) {
	root := t.TempDir()
	code := runDigest([]string{"--date", "tomorrow", "--root", root})
	if code != 2 {
		t.Errorf("runDigest with bad date exit = %d; want 2", code)
	}
}

// TestDigest_RegisteredInDispatch asserts the subcommand table carries
// the new verb. Backstop against a forgotten append to subcommands[].
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
