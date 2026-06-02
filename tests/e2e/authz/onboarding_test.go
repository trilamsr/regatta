//go:build e2e

package authz_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Exec onboarding-tutorial shell blocks in order — doc rot guard (spec §6 T4 A).
func TestTenantOnboarding_CIDocFixture(t *testing.T) {
	doc := docPath(t)
	src, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	blocks := extractShellBlocks(string(src))
	if len(blocks) < 4 {
		t.Fatalf("doc has %d shell blocks; expected ≥ 4 (one per onboarding step)", len(blocks))
	}

	tmp := t.TempDir()
	t.Setenv("REGATTA_ONBOARDING_TMP", tmp)

	for i, block := range blocks {
		i, block := i, block
		t.Run(blockLabel(i, block), func(t *testing.T) {
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", block)
			cmd.Dir = tmp
			cmd.Env = append(os.Environ(), "REGATTA_ONBOARDING_TMP="+tmp)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				t.Fatalf("block %d failed: %v\n--- script ---\n%s\n--- output ---\n%s",
					i, err, block, out.String())
			}
		})
	}
}

// Spec §6 T4 A — write policy_revision, ActiveBundle matches, Check allows; blocked on T1+T2.
func TestTenantOnboardingFlow_FromEmptyToActive(t *testing.T) {
	t.Skip("blocked on W8 T1 (Authorizer.Check) + T2 (policies.AppendPolicyRevision + ActiveBundle); rebase wires the body when both land")
}

// docPath walks parents to find the operator doc (worktree-safe).
func docPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "docs", "operator", "rbac-onboarding.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("docs/operator/rbac-onboarding.md not found above %s", dir)
		}
		dir = parent
	}
}

var fenceRE = regexp.MustCompile("(?ms)^```sh[ \\t]*\\n(.+?)^```")

// extractShellBlocks returns every ```sh fenced block in source order.
func extractShellBlocks(src string) []string {
	matches := fenceRE.FindAllStringSubmatch(src, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

func blockLabel(i int, body string) string {
	first := strings.SplitN(strings.TrimSpace(body), "\n", 2)[0]
	first = strings.TrimSpace(first)
	if len(first) > 48 {
		first = first[:48]
	}
	return strings.ReplaceAll(strings.Trim(strings.Fields(first)[0]+"_"+itoa(i), "_"), " ", "_")
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = digits[i%10]
		i /= 10
	}
	return string(b[n:])
}
