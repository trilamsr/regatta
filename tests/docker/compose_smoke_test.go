//go:build docker

package docker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestComposeStackSmoke spins up docker-compose.yml + runs one fixture issue → PR loop (#897).
func TestComposeStackSmoke(t *testing.T) {
	requireDocker(t)

	repoRoot := repoRootFromCWD(t)
	composeFile := filepath.Join(repoRoot, "docker-compose.yml")
	if _, err := os.Stat(composeFile); err != nil {
		t.Fatalf("compose file missing at %s: %v", composeFile, err)
	}

	fixtureRepo := os.Getenv("REGATTA_GH_FIXTURE_REPO")
	ghToken := os.Getenv("REGATTA_GH_FIXTURE_TOKEN")
	if fixtureRepo == "" || ghToken == "" {
		t.Skip("REGATTA_GH_FIXTURE_REPO + REGATTA_GH_FIXTURE_TOKEN required for smoke-loop step (nightly only)")
	}

	deadline := envDuration("REGATTA_DOCKER_DEADLINE", 5*time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	netsBefore := listNetworks(t)
	volsBefore := listVolumes(t)

	t.Cleanup(func() {
		downCtx, downCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer downCancel()
		out, err := compose(downCtx, repoRoot, "down", "--volumes", "--remove-orphans").CombinedOutput()
		if err != nil {
			t.Errorf("compose down: %v\n%s", err, out)
		}
		assertNoLeakedResources(t, netsBefore, volsBefore)
	})

	upCmd := compose(ctx, repoRoot, "up", "-d", "--wait", "--wait-timeout", "120")
	out, upErr := upCmd.CombinedOutput()
	if err := composeUpErrorWithLogs(upErr, out, composeLogFetcher(ctx, repoRoot), coreServices); err != nil {
		t.Fatalf("compose up -d --wait: %v", err)
	}

	assertServicesHealthy(ctx, t, repoRoot, []string{"regatta", "prometheus", "grafana", "alertmanager"})

	runSmokeLoopIteration(ctx, t, fixtureRepo, ghToken)
}

func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not on PATH; skipping container smoke test")
	}
	infoCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(infoCtx, "docker", "info").Run(); err != nil {
		t.Skipf("docker daemon unreachable: %v", err)
	}
	cv := exec.CommandContext(infoCtx, "docker", "compose", "version")
	if err := cv.Run(); err != nil {
		t.Skipf("docker compose v2 unavailable: %v", err)
	}
}

func repoRootFromCWD(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func compose(ctx context.Context, repoRoot string, args ...string) *exec.Cmd {
	full := append([]string{"compose", "-f", filepath.Join(repoRoot, "docker-compose.yml")}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"REGATTA_HMAC_KEY="+orDefault(os.Getenv("REGATTA_HMAC_KEY"), "smoke-test-hmac-key"),
		"GH_TOKEN="+orDefault(os.Getenv("GH_TOKEN"), "smoke-test-gh-token"),
		"REGATTA_BRIEF_HMAC_KEYS="+orDefault(os.Getenv("REGATTA_BRIEF_HMAC_KEYS"), "kid1:smoke-brief-key"),
		"ANTHROPIC_API_KEY="+orDefault(os.Getenv("ANTHROPIC_API_KEY"), "smoke-test-anthropic-key"),
		"WEBHOOK_AUTH_TOKEN="+orDefault(os.Getenv("WEBHOOK_AUTH_TOKEN"), "smoke-test-webhook-token"),
	)
	return cmd
}

func orDefault(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return s
}

func assertServicesHealthy(ctx context.Context, t *testing.T, repoRoot string, services []string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		ok, status := allHealthy(ctx, t, repoRoot, services)
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("services not healthy within 60s: %s", status)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ctx canceled while waiting for healthy: %v", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func allHealthy(ctx context.Context, t *testing.T, repoRoot string, services []string) (bool, string) {
	t.Helper()
	psCmd := compose(ctx, repoRoot, "ps", "--format", "json")
	out, err := psCmd.Output()
	if err != nil {
		return false, fmt.Sprintf("compose ps: %v", err)
	}
	want := map[string]bool{}
	for _, s := range services {
		want[s] = false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var entry struct {
			Service string `json:"Service"`
			State   string `json:"State"`
			Health  string `json:"Health"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if _, tracked := want[entry.Service]; !tracked {
			continue
		}
		// Services without an explicit healthcheck report Health="" but State="running"; accept.
		if entry.State == "running" && (entry.Health == "healthy" || entry.Health == "") {
			want[entry.Service] = true
		}
	}
	var missing []string
	for s, ok := range want {
		if !ok {
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		return true, ""
	}
	return false, "not-ready: " + strings.Join(missing, ",")
}

func runSmokeLoopIteration(ctx context.Context, t *testing.T, fixtureRepo, ghToken string) {
	t.Helper()
	titleStamp := fmt.Sprintf("[DOCKER-SMOKE %d]", time.Now().UnixNano())
	title := titleStamp + " compose loop"
	body := "Container smoke iteration; auto-closed by test cleanup."

	createCmd := exec.CommandContext(ctx, "gh", "issue", "create",
		"--repo", fixtureRepo,
		"--title", title,
		"--body", body,
	)
	createCmd.Env = append(os.Environ(), "GH_TOKEN="+ghToken)
	out, err := createCmd.Output()
	if err != nil {
		t.Fatalf("gh issue create: %v", err)
	}
	num := parseIssueNumber(strings.TrimSpace(string(out)))
	if num == 0 {
		t.Fatalf("parse issue number from %q", out)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		closeCmd := exec.CommandContext(closeCtx, "gh", "issue", "close", fmt.Sprint(num),
			"--repo", fixtureRepo, "--reason", "completed")
		closeCmd.Env = append(os.Environ(), "GH_TOKEN="+ghToken)
		_ = closeCmd.Run()
	})

	// Observe that a PR linking back to the issue eventually opens. Inside-container regatta
	// drives the loop; test only asserts observability of the linked PR.
	prDeadline := time.Now().Add(envDuration("REGATTA_DOCKER_PR_DEADLINE", 3*time.Minute))
	for {
		if linkedPR(ctx, t, fixtureRepo, ghToken, titleStamp) > 0 {
			return
		}
		if time.Now().After(prDeadline) {
			t.Fatalf("no PR observed referencing %q within deadline", titleStamp)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ctx canceled while polling for PR: %v", ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}

func linkedPR(ctx context.Context, t *testing.T, repo, token, stamp string) int {
	t.Helper()
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--repo", repo, "--search", stamp, "--state", "all",
		"--json", "number,title", "-L", "10",
	)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var prs []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return 0
	}
	for _, p := range prs {
		if strings.Contains(p.Title, stamp) {
			return p.Number
		}
	}
	return 0
}

func parseIssueNumber(s string) int {
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

func listNetworks(t *testing.T) map[string]struct{} {
	t.Helper()
	return dockerLsSet(t, "network", "{{.Name}}")
}

func listVolumes(t *testing.T) map[string]struct{} {
	t.Helper()
	return dockerLsSet(t, "volume", "{{.Name}}")
}

func dockerLsSet(t *testing.T, resource, format string) map[string]struct{} {
	t.Helper()
	out, err := exec.Command("docker", resource, "ls", "--format", format).Output()
	if err != nil {
		t.Fatalf("docker %s ls: %v", resource, err)
	}
	set := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			set[line] = struct{}{}
		}
	}
	return set
}

func assertNoLeakedResources(t *testing.T, netsBefore, volsBefore map[string]struct{}) {
	t.Helper()
	for name := range listNetworks(t) {
		if _, pre := netsBefore[name]; pre {
			continue
		}
		if strings.Contains(name, "regatta") {
			t.Errorf("leaked network after compose down: %s", name)
		}
	}
	for name := range listVolumes(t) {
		if _, pre := volsBefore[name]; pre {
			continue
		}
		if strings.Contains(name, "regatta") || strings.Contains(name, "prom") ||
			strings.Contains(name, "grafana") || strings.Contains(name, "alertmanager") {
			t.Errorf("leaked volume after compose down --volumes: %s", name)
		}
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
