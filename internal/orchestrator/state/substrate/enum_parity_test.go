package substrate_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_EventKindEnumMatchesSQLCheck pins spec §6 / §9 A-tier N1: Go EventKind constants ↔ SQL CHECK kind whitelist parity.
//
// Scans every migration whose body declares a `CHECK (kind IN (...))`
// clause on substrate_events and takes the highest-numbered migration's
// list as canonical — recreate-rename migrations (0012 widens 0006)
// supersede the original whitelist. Spec §6 N1.
func TestSubstrate_EventKindEnumMatchesSQLCheck(t *testing.T) {
	t.Parallel()

	sqlKinds := latestKindCheck(t)

	kinds := substrate.AllKinds()
	goKinds := make([]string, 0, len(kinds))
	for _, k := range kinds {
		goKinds = append(goKinds, string(k))
	}
	sort.Strings(goKinds)
	sort.Strings(sqlKinds)

	if !equalStringSlices(goKinds, sqlKinds) {
		t.Fatalf("EventKind ↔ SQL CHECK parity drift:\n  Go:  %v\n  SQL: %v",
			goKinds, sqlKinds)
	}
}

// latestKindCheck walks migrations/ in lexicographic order and returns
// the kind set declared by the last file that contains a substrate_events
// kind CHECK. Lexicographic == version-ordered because goose migrations
// use 4-digit zero-padded prefixes.
func latestKindCheck(t *testing.T) []string {
	t.Helper()
	migDir := migrationsDir(t)
	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatalf("read %s: %v", migDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var latest []string
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(migDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		// Only the substrate_events table carries a kind CHECK; skip
		// other migrations even if they happen to use the word "kind".
		s := stripSQLComments(string(body))
		if !strings.Contains(s, "substrate_events") {
			continue
		}
		re := regexp.MustCompile(`(?is)CHECK\s*\(\s*kind\s+IN\s*\(([^)]*)\)\s*\)`)
		m := re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		litRE := regexp.MustCompile(`'([^']+)'`)
		matches := litRE.FindAllStringSubmatch(m[1], -1)
		if len(matches) == 0 {
			continue
		}
		latest = latest[:0]
		for _, mm := range matches {
			latest = append(latest, mm[1])
		}
	}
	if len(latest) == 0 {
		t.Fatalf("no substrate_events kind CHECK found across migrations")
	}
	return latest
}

// migrationsDir resolves the migrations/ path relative to this test's
// package directory, falling back to a go.mod walk when the test runs
// from an unexpected cwd (e.g. `go test ./...` from repo root).
func migrationsDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	candidate := filepath.Join(wd, "..", "migrations")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	dir := wd
	for i := 0; i < 6; i++ {
		dir = filepath.Dir(dir)
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			c := filepath.Join(dir, "internal", "orchestrator", "state", "migrations")
			if _, err := os.Stat(c); err == nil {
				return c
			}
			break
		}
	}
	t.Fatalf("migrations dir not found; cwd=%s", wd)
	return ""
}

// stripSQLComments removes `-- ...` to end-of-line and `/* ... */` blocks
// so a kind literal sitting inside a comment never enters the parsed set.
func stripSQLComments(s string) string {
	lineRE := regexp.MustCompile(`--[^\n]*`)
	s = lineRE.ReplaceAllString(s, "")
	blockRE := regexp.MustCompile(`(?s)/\*.*?\*/`)
	s = blockRE.ReplaceAllString(s, "")
	return s
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
