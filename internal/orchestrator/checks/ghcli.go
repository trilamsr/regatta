package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// GHShell shells `gh pr checks <pr> --json conclusion,status,name --required` and folds the rows into a single CheckRun rollup.
type GHShell struct {
	Runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

const (
	conclusionFailure = "failure"
	conclusionSuccess = "success"
	statusCompleted   = "completed"
)

// NewGHShell returns a GHShell wired to os/exec.
func NewGHShell() *GHShell {
	return &GHShell{Runner: defaultExec}
}

// PRChecks returns the aggregated rollup: "failure" wins; the rollup is "success/completed" only when every required check has succeeded.
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
		if c.Conclusion == conclusionFailure {
			return CheckRun{Conclusion: conclusionFailure, Status: statusCompleted}, nil
		}
		if c.Status != statusCompleted {
			return CheckRun{Conclusion: "", Status: c.Status}, nil
		}
	}
	return CheckRun{Conclusion: conclusionSuccess, Status: statusCompleted}, nil
}

// defaultExec runs name+args via os/exec; gh CLI binary + args are construction-controlled literal strings (mirrors prwatch/ghcli.go).
//
//nolint:gosec // gh CLI + literal-arg shell-out
func defaultExec(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}
