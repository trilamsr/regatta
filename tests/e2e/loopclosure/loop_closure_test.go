//go:build e2e

package loopclosure_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestLoopClosureE2E drives the full self-host loop (gh issue → adapter → spawner → PR) against a real fixture repo; SKIPS without REGATTA_E2E_* env (#894).
func TestLoopClosureE2E(t *testing.T) {
	cfg := loadE2EConfig(t)

	deadline := envDuration("REGATTA_E2E_DEADLINE", 15*time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	probeRepo(t, cfg)

	issueNumber, titleStamp, cleanup := createSmokeIssue(ctx, t, cfg)
	t.Cleanup(cleanup)

	serveCmd, serveCancel := startServe(ctx, t, cfg)
	t.Cleanup(serveCancel)
	_ = serveCmd

	prNumber := waitForLinkedPR(ctx, t, cfg, issueNumber, titleStamp)
	t.Logf("loop closed: issue #%d → PR #%d", issueNumber, prNumber)

	assertPRBodyShape(ctx, t, cfg, prNumber)
}

type e2eConfig struct {
	Repo         string
	GHToken      string
	AnthropicKey string
	Binary       string
	Label        string
}

func loadE2EConfig(t *testing.T) e2eConfig {
	t.Helper()
	repo := os.Getenv("REGATTA_E2E_REPO")
	ghTok := os.Getenv("REGATTA_E2E_GH_TOKEN")
	anth := os.Getenv("REGATTA_E2E_ANTHROPIC_API_KEY")
	bin := os.Getenv("REGATTA_E2E_BINARY")
	if repo == "" || ghTok == "" || anth == "" || bin == "" {
		t.Skip("REGATTA_E2E_REPO + REGATTA_E2E_GH_TOKEN + REGATTA_E2E_ANTHROPIC_API_KEY + REGATTA_E2E_BINARY required (nightly only — see test docstring)")
	}
	label := os.Getenv("REGATTA_E2E_LABEL")
	if label == "" {
		label = "autonomous"
	}
	return e2eConfig{Repo: repo, GHToken: ghTok, AnthropicKey: anth, Binary: bin, Label: label}
}

func probeRepo(t *testing.T, cfg e2eConfig) {
	t.Helper()
	cmd := exec.Command("gh", "repo", "view", cfg.Repo, "--json", "name")
	cmd.Env = append(os.Environ(), "GH_TOKEN="+cfg.GHToken)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gh repo view %s: %v\n%s", cfg.Repo, err, out)
	}
}

func createSmokeIssue(ctx context.Context, t *testing.T, cfg e2eConfig) (int, string, func()) {
	t.Helper()
	titleStamp := fmt.Sprintf("[E2E-SMOKE %d]", time.Now().UnixNano())
	title := titleStamp + " loop-closure assertion"
	body := `## E2E smoke item

This issue is created by TestLoopClosureE2E. It asks the autonomous worker
to make a one-line documentation edit, no code changes. PR shape is asserted
by the test.

## Acceptance

- Open a PR titled with this issue number that adds a single line to
  ` + "`" + `TESTING.md` + "`" + ` saying "e2e-smoke ok".

## Reopen trigger

N/A — test fixture.
`
	cmd := exec.CommandContext(ctx, "gh", "issue", "create",
		"--repo", cfg.Repo,
		"--title", title,
		"--label", cfg.Label,
		"--body", body,
	)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+cfg.GHToken)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gh issue create: %v", err)
	}
	num := parseIssueNumberFromCreateURL(strings.TrimSpace(string(out)))
	if num == 0 {
		t.Fatalf("parse issue number from %q", out)
	}

	cleanup := func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		close := exec.CommandContext(closeCtx, "gh", "issue", "close", fmt.Sprint(num), "--repo", cfg.Repo, "--reason", "completed")
		close.Env = append(os.Environ(), "GH_TOKEN="+cfg.GHToken)
		_ = close.Run()
	}
	return num, titleStamp, cleanup
}

func parseIssueNumberFromCreateURL(s string) int {
	idx := strings.LastIndex(s, "/")
	if idx < 0 || idx == len(s)-1 {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(s[idx+1:], "%d", &n); err != nil {
		return 0
	}
	return n
}

func startServe(ctx context.Context, t *testing.T, cfg e2eConfig) (*exec.Cmd, func()) {
	t.Helper()
	repoRoot := t.TempDir()
	if err := os.MkdirAll(repoRoot+"/.regatta", 0o755); err != nil {
		t.Fatalf("mkdir .regatta: %v", err)
	}
	yaml := fmt.Sprintf(`work_item_source:
  type: github_issues
  repo: %s
  label: %s
  state: open
safety:
  spend_cap_usd_per_day: 5.0
`, cfg.Repo, cfg.Label)
	if err := os.WriteFile(repoRoot+"/regatta.yaml", []byte(yaml), 0o644); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}

	cmd := exec.CommandContext(ctx, cfg.Binary, "serve", "--repo", repoRoot, "--once=false")
	cmd.Env = append(os.Environ(),
		"GH_TOKEN="+cfg.GHToken,
		"ANTHROPIC_API_KEY="+cfg.AnthropicKey,
	)
	// Piping serve stdout/stderr to the test process is safe because the
	// audit in #918 confirmed no production code path logs env-var
	// VALUES — only names + presence + secrets.Value's redacting
	// LogValue/GoString/MarshalJSON/MarshalText (internal/secrets/secrets.go).
	// Reopen-trigger: any new slog/Fprintf that interpolates os.Getenv("GH_TOKEN"|"ANTHROPIC_API_KEY"|"REGATTA_*_KEY") raw.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("regatta serve start: %v", err)
	}
	cancel := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}
	return cmd, cancel
}

// prListRunner is the gh-pr-list invoker; tests replace it to drive the deadline-burn branch without a real gh binary.
var prListRunner = func(ctx context.Context, cfg e2eConfig) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--repo", cfg.Repo,
		"--state", "all",
		"--json", "number,title",
		"-L", "20",
	)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+cfg.GHToken)
	return cmd.Output()
}

// waitForLinkedPRTick is the polling cadence; tests shrink it to validate deadline-burn within bounded wall time.
var waitForLinkedPRTick = 20 * time.Second

func waitForLinkedPR(ctx context.Context, t *testing.T, cfg e2eConfig, issueNumber int, titleStamp string) int {
	t.Helper()
	tick := time.NewTicker(waitForLinkedPRTick)
	defer tick.Stop()

	for {
		out, err := prListRunner(ctx, cfg)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("deadline reached without linked PR for issue #%d (titleStamp %q): %v", issueNumber, titleStamp, ctx.Err())
			}
			t.Logf("gh pr list transient: %v", err)
		} else {
			var prs []struct {
				Number int    `json:"number"`
				Title  string `json:"title"`
			}
			if err := json.Unmarshal(out, &prs); err != nil {
				t.Fatalf("parse pr list: %v", err)
			}
			for _, pr := range prs {
				if strings.Contains(pr.Title, titleStamp) {
					return pr.Number
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("deadline reached without linked PR for issue #%d (titleStamp %q): %v", issueNumber, titleStamp, ctx.Err())
		case <-tick.C:
		}
	}
}

func assertPRBodyShape(ctx context.Context, t *testing.T, cfg e2eConfig, prNumber int) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", fmt.Sprint(prNumber),
		"--repo", cfg.Repo,
		"--json", "body,title",
	)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+cfg.GHToken)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gh pr view %d: %v", prNumber, err)
	}
	var pr struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(out, &pr); err != nil {
		t.Fatalf("parse pr: %v", err)
	}
	if !strings.Contains(pr.Body, "```release-notes") {
		t.Errorf("PR #%d body missing release-notes fence", prNumber)
	}
}

// TestWaitForLinkedPR_DeadlineBurn pins exit-on-deadline when gh subprocess errors immediately and repeatedly (#919).
func TestWaitForLinkedPR_DeadlineBurn(t *testing.T) {
	origRunner := prListRunner
	origTick := waitForLinkedPRTick
	t.Cleanup(func() {
		prListRunner = origRunner
		waitForLinkedPRTick = origTick
	})
	prListRunner = func(context.Context, e2eConfig) ([]byte, error) {
		return nil, errors.New("simulated gh subprocess failure")
	}
	waitForLinkedPRTick = 1 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	sub := &testing.T{}
	go func() {
		defer close(done)
		_ = waitForLinkedPR(ctx, sub, e2eConfig{Repo: "x", GHToken: "x"}, 1, "stamp")
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("waitForLinkedPR did not exit within 500ms of 50ms deadline (deadline-burn branch broken)")
	}
	if !sub.Failed() {
		t.Fatalf("expected waitForLinkedPR to t.Fatalf on deadline; sub-T not marked failed")
	}
}

func envDuration(name string, dflt time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return dflt
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return dflt
	}
	return d
}
