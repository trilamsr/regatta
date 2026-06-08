package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/prwatch"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// startPRWatcher wires the gh-CLI PR watcher into the orchestrator
// when both --no-pr-watch is unset AND the spawner is the claude
// backend (set.Worktrees != nil). Smoke-test fixtures with no GitHub
// remote pass --no-pr-watch so the daemon still boots; production
// always wires the Watcher.
//
// Spec docs/engineer/specs/2026-06-02-orchestrator-pr-watcher.md +
// cluster fixes for issues #520, #521, #522, #526.
func startPRWatcher(ctx context.Context, o *orchestrator.Orchestrator, db *state.DB, set spawnerSet, noPRWatch bool, slogger *slog.Logger) error {
	if noPRWatch || set.Worktrees == nil {
		slogger.Info("orchestrator.starting", "pr_watch_enabled", false)
		return nil
	}
	watcher, err := prwatch.New(prwatch.Config{
		DB:           db,
		BranchFn:     set.Worktrees.BranchFor,
		Lister:       prwatch.NewGHCLILister(),
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
