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

// orchestratorWiring carries the shared HealthHeartbeat pointer between Run loop + /healthz (#1218).
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

// newOrchestrator returns the orchestrator + its HealthHeartbeat cell as one unit.
func newOrchestrator(w orchestratorWiring) (*orchestrator.Orchestrator, *health.HeartbeatCell) {
	hb := health.NewHeartbeatCell(w.Clock)
	o := orchestrator.New(orchestrator.Config{
		AdapterSync:       w.Syncer,
		BriefLoader:       w.Loader,
		DB:                w.DB,
		Scheduler:         w.Scheduler,
		Spawner:           w.Spawner,
		SpawnerBackend:    w.Flags.SpawnerName,
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
