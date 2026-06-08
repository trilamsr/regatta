package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// GHShell shells `gh pr checks <pr> --json conclusion,status,name`
// and folds the per-check rows into a single CheckRun rollup.
// Production wiring; tests inject a fake GHCLI directly.
type GHShell struct {
	// Runner is the exec seam. Defaults to exec.CommandContext via
	// defaultExec; tests can stub for hermetic coverage of the JSON
	// fold logic.
	Runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewGHShell returns a GHShell wired to os/exec.
func NewGHShell() *GHShell {
	return &GHShell{Runner: defaultExec}
}

// PRChecks returns the aggregated rollup: "failure" wins over any
// not-yet-completed check; the rollup is "success/completed" only when
// every required check has succeeded.
func (g *GHShell) PRChecks(ctx context.Context, pr string) (CheckRun, error) {
	runner := g.Runner
	if runner == nil {
		runner = defaultExec
	}
	out, err := runner(ctx, "gh", "pr", "checks", pr,
		"--json", "conclusion,status,name", "--required")
	if err != nil {
		return CheckRun{}, fmt.Errorf("checks: gh pr checks %s: %w", pr, err)
	}
	var arr []struct {
		Conclusion string `json:"conclusion"`
		Status     string `json:"status"`
		Name       string `json:"name"`
	}
	if err := json.Unmarshal(out, &arr); err != nil {
		return CheckRun{}, fmt.Errorf("checks: parse gh json: %w", err)
	}
	for _, c := range arr {
		if c.Conclusion == "failure" {
			return CheckRun{Conclusion: "failure", Status: "completed"}, nil
		}
		if c.Status != "completed" {
			return CheckRun{Conclusion: "", Status: c.Status}, nil
		}
	}
	return CheckRun{Conclusion: "success", Status: "completed"}, nil
}

// defaultExec runs name + args via os/exec. gh CLI binary name + args
// are construction-controlled literal strings (not operator-supplied)
// so the gosec G204 warning is a false positive — mirrors the same
// pattern in internal/orchestrator/prwatch/ghcli.go.
//
//nolint:gosec // gh CLI + literal-arg shell-out
func defaultExec(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}
