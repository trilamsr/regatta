package main

import (
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"

	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
)

const defaultGitHubIssuesLane = "server"

// applyDefaultLaneCap installs cap=1 on the default github_issues lane when the operator did not set it explicitly; without it, overlapping issues cascade-rebase per CLAUDE.md feedback_parallel_safety (#1048).
func applyDefaultLaneCap(f *serveFlags, logger *slog.Logger) {
	if _, set := f.LaneCaps[defaultGitHubIssuesLane]; set {
		return
	}
	cfgPath := filepath.Join(f.RepoRoot, "regatta.yaml")
	cfg, err := validateconfig.LoadConfigFile(cfgPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("scheduler.default_lane_cap_skipped", "reason", "regatta.yaml load failed", "err", err)
		}
		return
	}
	if cfg == nil || cfg.WorkItemSource == nil || cfg.WorkItemSource.Type != validateconfig.WorkItemSourceTypeGitHubIssues {
		return
	}
	f.LaneCaps[defaultGitHubIssuesLane] = 1
	logger.Info("scheduler.default_lane_cap_applied",
		"lane", defaultGitHubIssuesLane,
		"cap", 1,
		"reason", "no --lane flag passed with work_item_source.type=github_issues",
	)
}
