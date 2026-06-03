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
// PRWatcher, CanaryInjector, SupervisorLimits, and LessonCapture
// land in follow-up commits. Reaper + RejectionRouter are wired in
// via SetReaper / SetRejectionRouter.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/lockfile"
	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// BriefLoader narrows program.BriefLoader to the seam PollOnce
// uses. Defined here as an interface to break the import cycle:
// program imports orchestrator (for ErrHMACInvalid / sentinels),
// so orchestrator cannot import program in turn.
type BriefLoader interface {
	Sync(ctx context.Context, pollStartedAt time.Time) error
}

// Config holds tunables and dependencies for an Orchestrator.
// Construction-time DI: callers wire AdapterSync, BriefLoader, and
// Scheduler externally so tests can swap any seam without touching the
// orchestrator's internal helpers.
type Config struct {
	// AdapterSync mirrors the spec adapter into work_items per
	// spec §2.9 step 3. Required.
	AdapterSync *adaptersync.Syncer

	// BriefLoader verifies + upserts brief children into work_items
	// per spec §2.4. Required. Interface-typed (not concrete
	// program.BriefLoader) so this package does not transitively
	// import internal/program, which already imports us for
	// ErrHMACInvalid / ErrCascadeNonConverging.
	BriefLoader BriefLoader

	// DB is the universal state store. Required.
	DB *state.DB

	// Scheduler reserves spawnable work_items into agents. Required.
	// Lane caps, lock TTL, and hotspot resolver come from the
	// scheduler's own Config — Orchestrator no longer owns those.
	Scheduler *scheduler.Scheduler

	// Spawner launches the reserved agents. Required.
	Spawner spawner.Spawner

	// DBPath is the on-disk sqlite path. Used to derive the
	// process-level lockfile (<dbPath>.lock).
	DBPath string

	// PollInterval is how often PollOnce runs in the Run loop.
	// Default: 30s.
	PollInterval time.Duration

	// TickInterval is how often the Scheduler ticks. Default: 5s.
	TickInterval time.Duration

	// HeartbeatInterval is how often Heartbeat refreshes locks held
	// by non-terminal agents. Default: 60s, matching design.md
	// §Concurrency & soft-lock policy.
	HeartbeatInterval time.Duration

	// LockTTL is the heartbeat lease used by ExpireStaleLocks.
	// Default: 15 minutes.
	LockTTL time.Duration

	// Logger is the structured-event sink for orchestrator lifecycle
	// events. Nil falls back to slog.Default() so embedded callers
	// still get output without panicking (spec §4.1).
	Logger *slog.Logger

	// Tracer is the OTel tracer this component uses to open spans.
	// Nil falls back to otel.Tracer("orchestrator") which resolves to
	// the global provider — noop until obs/otel.Setup runs. Mirrors
	// the Config.Logger DI normalization per W6 spec §3.3 +
	// feedback_spec_pattern_authority.
	Tracer trace.Tracer

	// Meter is the OTel instrument factory for orchestrator
	// lifecycle telemetry. Nil resolves to otel.Meter("orchestrator")
	// at the first ResolveMeter() call so the global MeterProvider
	// Setup wires (or a noop when Setup was skipped) wins by default.
	// Mirrors the W6 Config.Tracer pattern so callers stay on one DI
	// seam across trace + metric.
	Meter metric.Meter

	// Clock is the wall-clock source for PollOnce + ScheduleOnce
	// telemetry. Nil falls back to time.Now (production wiring). The
	// crash-recovery paths (Recover, Heartbeat) intentionally do NOT
	// route through Clock — they reflect real-world lock liveness and
	// must keep using wall time so a deterministic test clock cannot
	// mask a stuck heartbeat. Same shape as state.OpenWithClock,
	// spawner.Config.Clock so tests share one injection seam.
	Clock func() time.Time
}

// ResolveMeter returns the configured meter or falls back to the
// global provider's scoped meter. The fallback is lazy so a global
// provider swap (e.g. test injection of a noop provider) takes effect
// on the next call. Matches the W6 Config.Tracer nil-fallback shape.
func (c Config) ResolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter("orchestrator")
}

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

// SetReaper installs the Reaper used by Run to sweep terminal
// agents. Optional; without a Reaper the daemon still functions but
// leaves worktrees on disk after terminal transitions.
func (o *Orchestrator) SetReaper(r *reaper.Reaper) {
	o.reaper = r
}

// SetRejectionRouter installs the RejectionRouter used by Run to
// route gate_rejected events into agent transitions + K-escalation.
// Optional; without a Router the daemon still functions but agents
// never wake on AI-gate rejections and never escalate to needs-human.
func (o *Orchestrator) SetRejectionRouter(r *rejectionrouter.Router) {
	o.rejection = r
}

// SetMergeCoordinator installs the merge.Coordinator used by Recover
// to reconcile agents stranded in AwaitingMerge after a crash. Optional
// — without a Coordinator the daemon still functions for everything
// except the auto-merge path (PHASE AUTONOMY §11 W2), in which case
// AwaitingMerge agents stay enumerated by Recover but only get their
// locks heartbeated (no PR re-probe). Production wiring installs this
// once auto-merge ships; tests inject a fake-prober-backed Coordinator
// to exercise the recovery branches.
func (o *Orchestrator) SetMergeCoordinator(c *merge.Coordinator) {
	o.mergeCoord = c
}

// Recover implements the crash-recovery contract in docs/design.md
// §State, persistence, recovery. It MUST be called once on startup
// before Run, PollOnce, or ScheduleOnce.
//
// The contract:
//
//  1. Every non-terminal agent whose PID is no longer alive AND whose
//     state belongs to the pid-bound recovery set
//     ({spawning, running, pr_open, gates_running}) is transitioned
//     to crashed, then re-queued by re-inserting a pending row for
//     the same work_item_id on the next PollOnce.
//  2. Every agent in AwaitingMerge is reconciled against real GitHub
//     PR state by the merge.Coordinator (PHASE AUTONOMY §11 W2 c0).
//     AwaitingMerge agents have no live worktree PID by design — the
//     pid-alive probe does not apply. Without a Coordinator wired
//     (SetMergeCoordinator never called) the agent stays enumerated
//     so its lock is heartbeatable but no re-probe runs; production
//     wiring installs the Coordinator once auto-merge ships.
//  3. Stale locks (heartbeat older than 2× LockTTL) are dropped.
func (o *Orchestrator) Recover(ctx context.Context) error {
	if _, err := o.db.ExpireStaleLocks(ctx, 2*o.cfg.LockTTL); err != nil {
		return fmt.Errorf("orchestrator: expire stale locks: %w", err)
	}
	// pidBound is the set of states whose recovery uses pidAlive() to
	// decide live-vs-crashed. AwaitingMerge is intentionally excluded:
	// the agent has already produced a PR and is waiting on an
	// external merge — there is no live worktree PID, so pidAlive
	// would always read "dead" and force-crash a healthy agent.
	pidBound := []state.AgentState{
		state.AgentSpawning,
		state.AgentRunning,
		state.AgentPROpen,
		state.AgentGatesRunning,
	}
	agents, err := o.db.ListAgentsByState(ctx, pidBound...)
	if err != nil {
		return err
	}
	for _, a := range agents {
		if pidAlive(a.PID) {
			continue
		}
		if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{}); err != nil {
			if errors.Is(err, state.ErrInvalidTransition) {
				continue
			}
			return fmt.Errorf("orchestrator: mark agent %d crashed: %w", a.ID, err)
		}
		if _, err := o.db.ReleaseAgentLocks(ctx, a.ID); err != nil {
			return fmt.Errorf("orchestrator: release locks for crashed agent %d: %w", a.ID, err)
		}
		if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentPending, state.AgentMutation{}); err != nil {
			return fmt.Errorf("orchestrator: requeue agent %d: %w", a.ID, err)
		}
		_ = o.db.RecordEvent(ctx, a.ID, "recovered_crashed", "{}")
		// recovered_crashed is a state-layer audit row; the
		// orchestrator-side slog line uses the lifecycle keys so
		// dashboards can correlate without touching the events table.
		o.log.Info("orchestrator.recovered_crashed",
			string(obs.KeyAgentID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
		)
	}
	// AwaitingMerge agents reconcile against the external GitHub PR
	// state — never against pidAlive — because the merge sits on the
	// far side of an external side-effect the substrate does not own.
	// Without a Coordinator the sweep is skipped (operator pre-W2);
	// non-fatal so a Coordinator-less daemon still recovers everything
	// else.
	if o.mergeCoord != nil {
		if err := o.mergeCoord.Reconcile(ctx); err != nil {
			return fmt.Errorf("orchestrator: merge reconcile: %w", err)
		}
	}
	return nil
}

// PollOnce acquires the process flock, mirrors the adapter into
// work_items, then loads + verifies briefs into work_items. Per spec
// §2.9 the tick sequence is flock -> AdapterSync -> BriefLoader.
// Fail-fast: any error returns immediately so the next tick retries
// from a clean slate.
//
// Reservation lives in ScheduleOnce.Tick — splitting "update the queue"
// from "drain the queue" keeps the flock-window short and lets the Run
// loop independently throttle producer vs consumer.
func (o *Orchestrator) PollOnce(ctx context.Context) error {
	lock, err := lockfile.Acquire(o.dbPath + ".lock")
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	pollStartedAt := o.cfg.Clock().UTC()

	if err := o.adapterSync.Sync(ctx, pollStartedAt); err != nil {
		return fmt.Errorf("orchestrator: adapter sync: %w", err)
	}
	if err := o.briefLoader.Sync(ctx, pollStartedAt); err != nil {
		return fmt.Errorf("orchestrator: brief sync: %w", err)
	}
	return nil
}

// ScheduleOnce ticks the scheduler and spawns every newly-reserved
// agent through the spawner. Each successful Spawn moves the agent
// spawning→running with the returned PID and session ID. A failed
// Spawn rolls the agent back to crashed → pending so a future tick
// can retry.
//
// A Tick that returns a partial reservation plus an error is handled
// by spawning the partial set first, then surfacing the Tick error.
// Returning early would otherwise strand the reserved agents in the
// spawning state with their locks held until the recovery sweep on
// the next restart.
func (o *Orchestrator) ScheduleOnce(ctx context.Context) error {
	// W6 spec §3.5: `tick` is the per-scheduler-tick span — the
	// orchestrator owns the open/close because it is the loop driver;
	// scheduler.Tick opens `work_item` children under the active ctx.
	ctx, span := o.tracer.Start(ctx, "tick")
	defer span.End()

	// Spec §3.3: tick.started + tick.completed are unconditional on
	// every tick exit; no early return may skip the completion event.
	startedAt := o.cfg.Clock()
	o.log.Info(string(obs.EventTickStarted))

	ids, tickErr := o.sched.Tick(ctx)
	evaluated := len(ids)
	defer func() {
		o.log.Info(string(obs.EventTickCompleted),
			string(obs.KeyDurationMs), o.cfg.Clock().Sub(startedAt).Milliseconds(),
			string(obs.KeyWorkItemsEvaluated), int64(evaluated),
		)
	}()

	for _, id := range ids {
		a, err := o.db.GetAgent(ctx, id)
		if err != nil {
			return fmt.Errorf("orchestrator: load agent %d: %w", id, err)
		}
		// #295: DAGID = parent program id when the work_item belongs to a
		// multi-feature program; otherwise the work_item is its own DAG
		// root and DAGID falls back to the work_item id. RunID = agent id
		// so retries of the same work_item produce distinct runs.
		// Lookup failure is non-fatal — the spawn proceeds with the
		// work_item-id fallback in spend.SpawnerCallback so a transient
		// state read does not strand the reservation.
		dagID := a.WorkItemID
		if wi, werr := o.db.GetWorkItem(ctx, a.WorkItemID); werr == nil && wi.ParentProgramID != "" {
			dagID = wi.ParentProgramID
		}
		result, err := o.spawner.Spawn(ctx, spawner.Request{
			AgentID:    a.ID,
			WorkItemID: a.WorkItemID,
			Lane:       a.Lane,
			OperatorID: fmt.Sprintf("agent-%d", a.ID),
			DAGID:      dagID,
			RunID:      fmt.Sprintf("agent-%d", a.ID),
		})
		if err != nil {
			_, _ = o.db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{})
			_, _ = o.db.ReleaseAgentLocks(ctx, a.ID)
			_, _ = o.db.TransitionAgent(ctx, a.ID, state.AgentPending, state.AgentMutation{})
			_ = o.db.RecordEvent(ctx, a.ID, "spawn_failed", fmt.Sprintf(`{"error":%q}`, err.Error()))
			o.log.Warn(string(obs.EventSpawnFailed),
				string(obs.KeyAgentID), a.ID,
				string(obs.KeyWorkItemID), a.WorkItemID,
				string(obs.KeyErr), err.Error(),
			)
			continue
		}
		pid := result.PID
		sess := result.SessionID
		if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentRunning, state.AgentMutation{
			PID:       &pid,
			SessionID: &sess,
		}); err != nil {
			return fmt.Errorf("orchestrator: mark agent %d running: %w", a.ID, err)
		}
		_ = o.db.RecordEvent(ctx, a.ID, "spawned",
			fmt.Sprintf(`{"pid":%d,"session_id":%q}`, pid, sess))
		o.log.Info(string(obs.EventSpawnCompleted),
			string(obs.KeyAgentID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
			string(obs.KeyLane), a.Lane,
			"pid", pid,
			"session_id", sess,
		)
	}
	if tickErr != nil {
		return fmt.Errorf("orchestrator: scheduler tick: %w", tickErr)
	}
	return nil
}

// ReapTerminal invokes the configured Reaper.ReapAll. Safe to call
// with no Reaper set; returns nil in that case.
func (o *Orchestrator) ReapTerminal(ctx context.Context) error {
	if o.reaper == nil {
		return nil
	}
	return o.reaper.ReapAll(ctx)
}

// RouteRejections drains pending gate_rejected events into agent
// transitions + K-escalation. Safe to call with no Router set;
// returns nil in that case.
func (o *Orchestrator) RouteRejections(ctx context.Context) error {
	if o.rejection == nil {
		return nil
	}
	return o.rejection.Tick(ctx)
}

// Heartbeat refreshes every lock owned by an active agent. The
// orchestrator runs Heartbeat on cfg.HeartbeatInterval so that locks
// only age out when an agent has truly crashed.
//
// AwaitingMerge is included so an agent stalled on a long external
// merge call (gh API slowness, branch-protection wait) does not lose
// its hotspot lock to the stale-lock sweep mid-merge. PHASE AUTONOMY
// §11 W2 c0 made this load-bearing — pre-c0 AwaitingMerge was excluded
// here and would silently leak locks across long merges.
func (o *Orchestrator) Heartbeat(ctx context.Context) error {
	active, err := o.db.ListAgentsByState(ctx,
		state.AgentSpawning, state.AgentRunning, state.AgentPROpen,
		state.AgentGatesRunning, state.AgentAwaitingMerge)
	if err != nil {
		return err
	}
	for _, a := range active {
		if _, err := o.db.HeartbeatLock(ctx, a.ID); err != nil {
			return fmt.Errorf("orchestrator: heartbeat agent %d: %w", a.ID, err)
		}
	}
	return nil
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
