package substrate_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSubstrate_NoUpdateDeleteInSubstratePackage pins the append-only
// invariant: no UPDATE or DELETE SQL appears in any production .go
// file in the substrate package. Spec §9 B-tier.
//
// The grep is a substring match against word-boundary UPDATE|DELETE.
// String-literal context (e.g. SQL inside backticks) is the load-bearing
// case the test catches; comments in production code that happen to
// mention the words are caught too — which is correct because reviewer
// nudges towards "use Append + Fold, never mutate" should land in PR
// review, not as a substring in shipped code. Test files are excluded
// — they may legitimately exercise sqlite-level UPDATE/DELETE for
// adversarial scenarios.
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
						filepath.Join(root, name)+":"+itoa(i+1)+": "+strings.TrimSpace(line))
				}
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("substrate production code must not contain UPDATE|DELETE:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

func itoa(i int) string {
	// Tiny: avoid pulling strconv into a one-test file.
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := 0
	for i > 0 {
		b[n] = byte('0' + i%10)
		n++
		i /= 10
	}
	out := make([]byte, n)
	for j := 0; j < n; j++ {
		out[j] = b[n-1-j]
	}
	return string(out)
}
