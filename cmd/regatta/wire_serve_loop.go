// Extracted from serve.go to keep the boot entry point under the
// god-file threshold enforced by TestServeFileSize. Grouping: helpers
// that build the BriefLoader, install post-orchestrator handlers, and
// dispatch the tick-once vs long-run tail.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/program"
)

// buildBriefLoaderForServe constructs the BriefLoader with the audit sink
// wired to the brief HMAC keyring so issue #80's durable audit path stays
// on by default; a zero-key deployment falls back to slog-only retention.
func buildBriefLoaderForServe(db *state.DB, briefsDir string, evaluator *program.EdgeEvaluator, costKey []byte, costKeyID string, slogger *slog.Logger) (*program.BriefLoader, error) {
	return program.NewBriefLoader(program.BriefLoaderConfig{
		FS:        os.DirFS(briefsDir),
		DB:        db,
		Keyring:   loadBriefKeyring(),
		Evaluator: evaluator,
		Logger:    slogger,
		Audit: program.BriefAuditConfig{
			Key:      costKey,
			KeyID:    costKeyID,
			TenantID: substrate.DefaultTenantID,
			RunID:    "brief-loader",
		},
	})
}

// installReaperAndRejectionRouter wires the two post-orchestrator handlers
// that only fire when the spawner exposes a worktree manager (reaper) and
// unconditionally for the rejection router; kept in one helper so the
// serve entry point does not fan out on side effects.
func installReaperAndRejectionRouter(o *orchestrator.Orchestrator, set spawnerSet, db *state.DB, slogger *slog.Logger, clock func() time.Time) {
	if set.Worktrees != nil {
		o.SetReaper(reaper.New(reaper.Config{
			DB:     db,
			WM:     set.Worktrees,
			Killer: set.Killer,
			Logger: slogger,
			Clock:  clock,
		}))
	}
	// RejectionRouter wakes agents on AI-gate rejections and labels the
	// PR `needs-human` after K=3. Defaults match docs/design.md §Failure
	// modes; no regatta.yaml keys are introduced for MVR-1 — operators
	// who want richer routing land it when a real customer use-case
	// shows up.
	o.SetRejectionRouter(buildRejectionRouter(db, rejectionrouter.GHLabeler{}, slogger))
}

// runServeOrchestratorLoop dispatches the final tick-once vs long-run
// branch so runServe's tail reads as a single call instead of an if/else
// wall.
func runServeOrchestratorLoop(ctx context.Context, f serveFlags, o *orchestrator.Orchestrator, logger *log.Logger) int {
	if f.TickOnce {
		if err := runTickOnce(ctx, o); err != nil {
			logger.Printf("%v", err)
			return 1
		}
		return 0
	}

	if err := o.Run(ctx); err != nil {
		logger.Printf("run: %v", err)
		return 1
	}
	return 0
}
