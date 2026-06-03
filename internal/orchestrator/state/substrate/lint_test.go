package substrate_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestSubstrate_NoUpdateDeleteInSubstratePackage pins append-only invariant: no UPDATE|DELETE in non-test substrate .go files per spec §9 B-tier.
func TestSubstrate_NoUpdateDeleteInSubstratePackage(t *testing.T) {
	pattern := regexp.MustCompile(`\b(UPDATE|DELETE)\b`)
	root := "." // current package directory
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var violations []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		// Test files are excluded — they may legitimately probe
		// mutable SQL paths (e.g. sqlite-level integrity tests).
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if pattern.Match(body) {
			// Locate the line for the error message.
			lines := strings.Split(string(body), "\n")
			for i, line := range lines {
				if pattern.MatchString(line) {
					violations = append(violations,
						filepath.Join(root, name)+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
				}
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("substrate production code must not contain UPDATE|DELETE:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

