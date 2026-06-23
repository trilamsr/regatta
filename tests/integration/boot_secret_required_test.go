//go:build integration

package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBootSecretsRequired_FailClosed asserts `regatta serve` exits nonzero within 5s when a required secret env var is unset, and stderr names the secret (R-MEGA-3 INT-2).
//
// Catches the LIVE-6/7/8 fail-closed class: a missing GH_TOKEN with github_issues
// adapter must abort boot loudly instead of crashing at first scheduler tick.
func TestBootSecretsRequired_FailClosed(t *testing.T) {
	repoRoot := findRepoRoot(t)
	binPath := buildRegatta(t, repoRoot)

	cases := []struct {
		name      string
		unsetVars []string
		yaml      string
		wantStderr string
	}{
		{
			name:       "gh_token_missing_for_github_issues",
			unsetVars:  []string{"GH_TOKEN", "GITHUB_TOKEN"},
			yaml: `version: 1
repo: {host: github, owner: trilamsr, name: regatta}
spec_adapter: {type: github_issues, selector: "label:autonomous"}
ci: {command: "go test ./..."}
gates:
  - id: human_merge
    type: approval_gate
    name: human-merge
    risk_class: low
    reviewers: [trilamsr]
    quorum: 1
    timeout: 24h
    decision_window: 12h
    on_timeout: fail
safety: {destructive_ops_deny: [], agent_creds_scope: dev_only}
`,
			wantStderr: "GH_TOKEN",
		},
		{
			name:      "linear_api_key_missing_for_linear",
			unsetVars: []string{"LINEAR_API_KEY"},
			yaml: `version: 1
repo: {host: github, owner: trilamsr, name: regatta}
spec_adapter: {type: linear, team: TEAM}
ci: {command: "go test ./..."}
gates:
  - id: human_merge
    type: approval_gate
    name: human-merge
    risk_class: low
    reviewers: [trilamsr]
    quorum: 1
    timeout: 24h
    decision_window: 12h
    on_timeout: fail
safety: {destructive_ops_deny: [], agent_creds_scope: dev_only}
`,
			wantStderr: "LINEAR_API_KEY",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte(tc.yaml), 0o600); err != nil {
				t.Fatalf("write yaml: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binPath, "serve",
				"--repo", tmp,
				"--db", filepath.Join(tmp, "regatta.db"),
				"--tick-once",
				"--ui=false",
			)
			cmd.Env = scrubbedEnv(tc.unsetVars)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected nonzero exit; got success with output:\n%s", out)
			}
			if !strings.Contains(string(out), tc.wantStderr) {
				t.Errorf("stderr must name %q; got:\n%s", tc.wantStderr, out)
			}
		})
	}
}

func scrubbedEnv(unset []string) []string {
	want := map[string]bool{}
	for _, v := range unset {
		want[v] = true
	}
	out := []string{}
	for _, e := range os.Environ() {
		eq := strings.IndexByte(e, '=')
		if eq < 0 {
			continue
		}
		if want[e[:eq]] {
			continue
		}
		out = append(out, e)
	}
	return out
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, _ := os.Getwd()
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func buildRegatta(t *testing.T, repoRoot string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "regatta")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/regatta")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build regatta: %v\n%s", err, out)
	}
	return bin
}
