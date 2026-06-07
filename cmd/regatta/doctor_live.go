// doctor_live.go isolates the OS-touching probes from runDoctor's
// pure-logic seam so doctor_test.go runs offline (#910).
package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	verifyrepo "github.com/trilamsr/regatta/internal/config/verify"
)

func liveGitState(ctx context.Context) (gitStateReport, error) {
	branch, err := runGit(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return gitStateReport{}, fmt.Errorf("git rev-parse: %w", err)
	}
	status, err := runGit(ctx, "status", "--porcelain")
	if err != nil {
		return gitStateReport{}, fmt.Errorf("git status: %w", err)
	}
	reach := true
	if _, err := runGit(ctx, "ls-remote", "--exit-code", "origin", "HEAD"); err != nil {
		reach = false
	}
	return gitStateReport{
		Branch:          strings.TrimSpace(branch),
		Clean:           strings.TrimSpace(status) == "",
		RemoteReachable: reach,
	}, nil
}

func runGit(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput() //nolint:gosec // args originate in liveGitState; static probe surface.
	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func liveValidateConfig(path string) error {
	return validateconfig.LoadFile(path)
}

func liveVerifyRepoConfig(ctx context.Context) (bool, []string, error) {
	cfg, err := validateconfig.LoadConfigFile("regatta.yaml")
	if err != nil {
		return false, nil, fmt.Errorf("load regatta.yaml: %w", err)
	}
	if cfg == nil || cfg.Repo == nil || cfg.Repo.Owner == "" || cfg.Repo.Name == "" {
		return false, nil, fmt.Errorf("repo.owner/name not set in regatta.yaml")
	}
	branch := cfg.Repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	res, err := verifyrepo.Run(ctx, verifyrepo.Config{
		Owner:  cfg.Repo.Owner,
		Repo:   cfg.Repo.Name,
		Branch: branch,
	})
	if err != nil {
		return false, nil, err
	}
	return res.OK, res.FailedOK, nil
}

func liveSupervisorPresent() (bool, string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, _ := exec.Command("launchctl", "list").CombinedOutput()
		if strings.Contains(string(out), "regatta") {
			return true, "launchd unit present", nil
		}
		return false, "no launchd unit (run `regatta install-service` to install)", nil
	case "linux":
		out, err := exec.Command("systemctl", "--user", "is-active", "regatta").CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) == "active" {
			return true, "systemd --user unit active", nil
		}
		return false, "no systemd --user regatta unit (run `regatta install-service`)", nil
	}
	return false, fmt.Sprintf("supervisor probe not implemented for GOOS=%s", runtime.GOOS), nil
}
