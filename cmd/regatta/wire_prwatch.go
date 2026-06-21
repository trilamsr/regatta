package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/prwatch"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// startPRWatcher wires the PR watcher into the orchestrator when both
// --no-pr-watch is unset AND the spawner is the claude backend
// (set.Worktrees != nil). Smoke-test fixtures with no GitHub remote
// pass --no-pr-watch so the daemon still boots; production always
// wires the Watcher.
//
// Lister selection (MAY-52): when GH_TOKEN/GITHUB_TOKEN AND repo
// owner/name are resolvable, the watcher runs the direct-REST
// ETagLister (304 + cached snapshot per branch) and keeps the gh-CLI
// path as the fallback for rate-limit / 5xx and title-prefix probes.
// When either dep is missing the gh-CLI path runs alone — back-compat
// with the pre-MAY-52 wiring.
//
// Spec docs/engineer/specs/2026-06-02-orchestrator-pr-watcher.md +
// cluster fixes for issues #520, #521, #522, #526.
func startPRWatcher(ctx context.Context, o *orchestrator.Orchestrator, db *state.DB, set spawnerSet, repoRoot string, noPRWatch bool, slogger *slog.Logger) error {
	if noPRWatch || set.Worktrees == nil {
		slogger.Info("orchestrator.starting", "pr_watch_enabled", false)
		return nil
	}
	lister := buildPRLister(repoRoot, slogger)
	watcher, err := prwatch.New(prwatch.Config{
		DB:           db,
		BranchFn:     set.Worktrees.BranchFor,
		Lister:       lister,
		VersionProbe: prwatch.NewGHCLIVersionProbe(),
		Logger:       slogger,
		LocalHeadFn:  prwatch.NewWorktreeLocalHeadFn(set.Worktrees.PathFor),
	})
	if err != nil {
		return fmt.Errorf("pr-watch: %w", err)
	}
	if err := watcher.Start(ctx); err != nil {
		return fmt.Errorf("pr-watch: %w", err)
	}
	o.SetPRWatcher(watcher)
	return nil
}

// buildPRLister returns ETagLister(gh-CLI fallback) when GH_TOKEN +
// repo owner/name are available, else the plain gh-CLI lister. The
// returned lister always satisfies prwatch.PRLister.
func buildPRLister(repoRoot string, slogger *slog.Logger) prwatch.PRLister {
	cli := prwatch.NewGHCLILister()
	token := firstEnv("GH_TOKEN", "GITHUB_TOKEN")
	if token == "" {
		slogger.Info("prwatch.lister.gh_cli", "reason", "no_gh_token_env")
		return cli
	}
	owner, repo, ok := loadRepoOwnerName(repoRoot)
	if !ok {
		slogger.Info("prwatch.lister.gh_cli", "reason", "no_repo_owner_name_in_regatta_yaml")
		return cli
	}
	slogger.Info("prwatch.lister.etag_rest",
		"owner", owner,
		"repo", repo,
		"fallback", "gh_cli",
	)
	return prwatch.NewETagLister(prwatch.ETagListerConfig{
		Owner:    owner,
		Repo:     repo,
		Token:    token,
		Fallback: cli,
	})
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// loadRepoOwnerName reads regatta.yaml for repo.owner + repo.name. A
// missing file or empty fields returns ok=false so the caller can fall
// back to the gh-CLI lister silently.
func loadRepoOwnerName(repoRoot string) (string, string, bool) {
	cfgPath := filepath.Join(repoRoot, "regatta.yaml")
	cfg, err := validate.LoadConfigFile(cfgPath)
	if err != nil {
		// Missing file is the documented zero-config path; parse
		// failures stay silent so we don't crowd the secrets-loader
		// WARN — fall back to gh-CLI either way.
		_ = errors.Is(err, fs.ErrNotExist)
		return "", "", false
	}
	if cfg == nil || cfg.Repo == nil {
		return "", "", false
	}
	if cfg.Repo.Owner == "" || cfg.Repo.Name == "" {
		return "", "", false
	}
	return cfg.Repo.Owner, cfg.Repo.Name, true
}
