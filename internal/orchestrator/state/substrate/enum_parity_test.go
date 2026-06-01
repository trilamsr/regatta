package substrate_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_EventKindEnumMatchesSQLCheck pins spec §6 / §9 A-tier N1: Go EventKind constants ↔ SQL CHECK kind whitelist parity.
func TestSubstrate_EventKindEnumMatchesSQLCheck(t *testing.T) {
	t.Parallel()

	migrationPath := findMigration(t, "0006_substrate.sql")
	body, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sqlKinds := parseKindCheck(t, string(body))

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

// parseKindCheck pulls the CHECK (kind IN (...)) literal list out of
// the migration body. Robust to: leading/trailing whitespace inside the
// parens; SQL comments on the same line; quoting style (single quotes);
// multi-line argument lists.
func parseKindCheck(t *testing.T, body string) []string {
	t.Helper()

	body = stripSQLComments(body)

	re := regexp.MustCompile(`(?is)CHECK\s*\(\s*kind\s+IN\s*\(([^)]*)\)\s*\)`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("CHECK (kind IN ...) not found in migration")
	}
	inner := m[1]
	litRE := regexp.MustCompile(`'([^']+)'`)
	matches := litRE.FindAllStringSubmatch(inner, -1)
	if len(matches) == 0 {
		t.Fatalf("CHECK clause has no quoted literals: %q", inner)
	}
	out := make([]string, 0, len(matches))
	for _, mm := range matches {
		out = append(out, mm[1])
	}
	return out
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

// findMigration locates the named migration file. The substrate package
// directory's sibling is migrations/.
func findMigration(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	candidate := filepath.Join(wd, "..", "migrations", name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Fall back to walking upward to find go.mod, then descend.
	dir := wd
	for i := 0; i < 6; i++ {
		dir = filepath.Dir(dir)
		gm := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(gm); err == nil {
			c := filepath.Join(dir,
				"internal", "orchestrator", "state", "migrations", name)
			if _, err := os.Stat(c); err == nil {
				return c
			}
			break
		}
	}
	t.Fatalf("migration %s not found; cwd=%s", name, wd)
	return ""
}
