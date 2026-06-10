package main

import (
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/internal/health"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/program"
)

// orchestratorWiring bundles the inputs newOrchestrator stitches into
// orchestrator.New. Pulled out of serve.go so the boot file stays under
// the 400-line god-file ceiling (audit Wave D); the same pointer the
// helper returns for HealthHeartbeat is shared with bootListener so
// /healthz reads the cell the Run loop Touches (#1218).
type orchestratorWiring struct {
	Syncer    *adaptersync.Syncer
	Loader    *program.BriefLoader
	DB        *state.DB
	Scheduler *scheduler.Scheduler
	Spawner   spawner.Spawner
	Flags     serveFlags
	Logger    *slog.Logger
	Clock     func() time.Time
}

// newOrchestrator constructs the Orchestrator + the /healthz heartbeat
// cell as one unit; returning the cell makes the shared-pointer contract
// explicit at the call site.
func newOrchestrator(w orchestratorWiring) (*orchestrator.Orchestrator, *health.HeartbeatCell) {
	hb := health.NewHeartbeatCell(w.Clock)
	o := orchestrator.New(orchestrator.Config{
		AdapterSync:       w.Syncer,
		BriefLoader:       w.Loader,
		DB:                w.DB,
		Scheduler:         w.Scheduler,
		Spawner:           w.Spawner,
		ItemBody:          buildItemBodyLoader(w.Flags.RepoRoot, w.Logger),
		RepoRoot:          w.Flags.RepoRoot,
		DBPath:            w.Flags.DBPath,
		PollInterval:      w.Flags.PollDur,
		TickInterval:      w.Flags.TickDur,
		HeartbeatInterval: w.Flags.HeartDur,
		LockTTL:           w.Flags.LockTTL,
		Logger:            w.Logger,
		Clock:             w.Clock,
		HealthHeartbeat:   hb,
	})
	return o, hb
}
