package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// GHShell folds `gh pr checks --required` rows into a single CheckRun rollup; failure-wins, success only when every required check completed.
type GHShell struct {
	Runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

const (
	conclusionFailure = "failure"
	conclusionSuccess = "success"
	statusCompleted   = "completed"
)

func NewGHShell() *GHShell { return &GHShell{Runner: defaultExec} } //nolint:revive // wires Runner to defaultExec

// PRChecks aggregates required-check rows: "failure" wins; "success/completed" only when every row has succeeded.
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

func defaultExec(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // gh CLI binary + literal-arg shell-out (mirrors prwatch/ghcli.go)
	return cmd.Output()
}
