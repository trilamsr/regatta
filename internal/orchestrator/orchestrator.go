// Package orchestrator wires the spec adapter, scheduler, spawner,
// and state store into a single long-running daemon.
//
// The skeleton implements three of the nine responsibilities in
// docs/design.md §Orchestrator shape:
//
//  1. SpecWatcher  (PollOnce)
//  2. Scheduler    (ScheduleOnce)
//  3. AgentSpawner (called from ScheduleOnce)
//
// Plus crash recovery (Recover) and lock heartbeating (Heartbeat).
// CanaryInjector, SupervisorLimits, and LessonCapture land in
// follow-up commits. Reaper + RejectionRouter + PRWatcher are wired
// in via SetReaper / SetRejectionRouter / SetPRWatcher.
//
// Per-subsystem wiring lives in orchestrator_<subsystem>.go (reaper,
// rejection, prwatcher, merge) and per-phase logic in
// orchestrator_{config,recover,poll,schedule,heartbeat}.go so this
// file stays bounded to the struct, constructor, Run loop, and
// pidAlive helper.
package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/prwatch"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Orchestrator coordinates the spec adapter, scheduler, and spawner.
// Per spec §2.9, PollOnce is the universal-queue tick: flock ->
// AdapterSync -> BriefLoader. Agent reservation + spawn live in
// ScheduleOnce so a flock-protected PollOnce stays bounded to "update
// the queue" while ScheduleOnce stays bounded to "drain the queue".
type Orchestrator struct {
	adapterSync *adaptersync.Syncer
	briefLoader BriefLoader
	db          *state.DB
	sched       *scheduler.Scheduler
	spawner     spawner.Spawner
	reaper      *reaper.Reaper
	rejection   *rejectionrouter.Router
	mergeCoord  *merge.Coordinator
	prWatcher   *prwatch.Watcher
	dbPath      string
	cfg         Config
	log         *slog.Logger
	tracer      trace.Tracer
}

// New constructs an Orchestrator from a Config. All deps are wired
// externally so tests can stub any seam.
func New(cfg Config) *Orchestrator {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = 5 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 60 * time.Second
	}
	if cfg.LockTTL <= 0 {
		cfg.LockTTL = 15 * time.Minute
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("orchestrator")
	}
	return &Orchestrator{
		adapterSync: cfg.AdapterSync,
		briefLoader: cfg.BriefLoader,
		db:          cfg.DB,
		sched:       cfg.Scheduler,
		spawner:     cfg.Spawner,
		dbPath:      cfg.DBPath,
		cfg:         cfg,
		log:         log,
		tracer:      tracer,
	}
}

// Run drives the orchestrator until ctx is cancelled. Returns nil on
// clean shutdown. Per-tick errors (poll, schedule, heartbeat) are
// logged but never abort the loop: a transient adapter outage or
// sqlite contention must not take the daemon down.
func (o *Orchestrator) Run(ctx context.Context) error {
	pollT := time.NewTicker(o.cfg.PollInterval)
	defer pollT.Stop()
	tickT := time.NewTicker(o.cfg.TickInterval)
	defer tickT.Stop()
	heartT := time.NewTicker(o.cfg.HeartbeatInterval)
	defer heartT.Stop()

	// Kick off one cycle immediately so the daemon does useful work
	// before the first tick. Errors here are non-fatal for the same
	// reason as the periodic ticks.
	if err := o.PollOnce(ctx); err != nil {
		o.log.Warn("orchestrator.poll_failed", "phase", "initial", string(obs.KeyErr), err.Error())
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		o.log.Warn("orchestrator.schedule_failed", "phase", "initial", string(obs.KeyErr), err.Error())
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pollT.C:
			if err := o.PollOnce(ctx); err != nil {
				o.log.Warn("orchestrator.poll_failed", string(obs.KeyErr), err.Error())
			}
		case <-tickT.C:
			if err := o.ScheduleOnce(ctx); err != nil {
				o.log.Warn("orchestrator.schedule_failed", string(obs.KeyErr), err.Error())
			}
			if err := o.RouteRejections(ctx); err != nil {
				o.log.Warn("orchestrator.rejection_route_failed", string(obs.KeyErr), err.Error())
			}
			if err := o.ReapTerminal(ctx); err != nil {
				o.log.Warn("orchestrator.reap_failed", string(obs.KeyErr), err.Error())
			}
			if err := o.WatchPRs(ctx); err != nil {
				o.log.Warn("orchestrator.prwatch_failed", string(obs.KeyErr), err.Error())
			}
		case <-heartT.C:
			if err := o.Heartbeat(ctx); err != nil {
				o.log.Warn("orchestrator.heartbeat_failed", string(obs.KeyErr), err.Error())
			}
		}
	}
}

// pidAlive reports whether pid identifies a live process. Negative
// or zero PIDs (unset or synthetic from the stub spawner) are
// treated as dead. The OS-specific liveness probe lives in
// orchestrator_unix.go and orchestrator_windows.go.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return osPidAlive(pid)
}
