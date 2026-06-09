package main

import (
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"

	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
)

// warnIfStubWithGitHubIssues fires one operator-visible WARN when the orchestrator boots with the stub spawner but the configured spec adapter is github_issues. The docker quickstart hit this silently: stub agents emit spawn.completed with pid=-1 and PR watcher is disabled, so logs look healthy while nothing real dispatches (#1090).
func warnIfStubWithGitHubIssues(spawnerName, repoRoot string, logger *slog.Logger) {
	if spawnerName != "stub" {
		return
	}
	cfgPath := filepath.Join(repoRoot, "regatta.yaml")
	cfg, err := validateconfig.LoadConfigFile(cfgPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("spawner.stub_with_github_issues_check_skipped", "reason", "regatta.yaml load failed", "err", err)
		}
		return
	}
	if cfg == nil || cfg.SpecAdapter == nil || cfg.SpecAdapter.Type != validateconfig.SpecAdapterTypeGitHubIssues {
		return
	}
	logger.Warn("spawner.stub_with_github_issues",
		"hint", "spec_adapter.type=github_issues but -spawner=stub; agents will spawn as fake stubs (pid=-1) and PR watcher will be disabled. Pass -spawner=claude to dispatch real agents.",
	)
}
