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
// PRWatcher, RejectionRouter, CanaryInjector, SupervisorLimits,
// Reaper, and LessonCapture land in follow-up commits.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/schemas"
)

// Config holds tunables for an Orchestrator. Zero values fall back to
// the defaults documented on each field.
type Config struct {
	// PollInterval is how often the SpecWatcher calls
	// SpecAdapter.List. Default: 30s. Must be ≥ adapter's
	// Capabilities().MinPollInterval; New enforces this.
	PollInterval time.Duration

	// TickInterval is how often the Scheduler ticks. Default: 5s.
	TickInterval time.Duration

	// HeartbeatInterval is how often Heartbeat refreshes locks held
	// by non-terminal agents. Default: 60s, matching design.md
	// §Concurrency & soft-lock policy.
	HeartbeatInterval time.Duration

	// LockTTL is the heartbeat lease passed to the scheduler.
	// Default: 15 minutes.
	LockTTL time.Duration

	// LaneCaps is a per-lane concurrency map; missing lanes are
	// unlimited.
	LaneCaps map[string]int

	// Hotspots resolves work-item ID to hotspot lock names. nil
	// disables hotspot locking.
	Hotspots scheduler.HotspotResolver
}

// Orchestrator coordinates the spec adapter, scheduler, and spawner.
type Orchestrator struct {
	db      *state.DB
	adapter schemas.SpecAdapter
	sched   *scheduler.Scheduler
	spawner spawner.Spawner
	cfg     Config
	logf    func(format string, args ...any)
}

// New constructs an Orchestrator with the given dependencies. The
// adapter's MinPollInterval is enforced as a floor on cfg.PollInterval
// so a misconfiguration cannot hammer the upstream source.
func New(db *state.DB, adapter schemas.SpecAdapter, sp spawner.Spawner, cfg Config) *Orchestrator {
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
	if floor := adapter.Capabilities().MinPollInterval; floor > 0 && cfg.PollInterval < floor {
		cfg.PollInterval = floor
	}
	o := &Orchestrator{
		db:      db,
		adapter: adapter,
		spawner: sp,
		cfg:     cfg,
		logf:    func(string, ...any) {},
	}
	o.sched = scheduler.New(db, scheduler.Config{
		LaneCaps: cfg.LaneCaps,
		LockTTL:  cfg.LockTTL,
		Hotspots: cfg.Hotspots,
	})
	return o
}

// SetLogger installs a printf-style logger. The Orchestrator is silent
// by default; tests pass t.Logf, production callers pass log.Printf.
func (o *Orchestrator) SetLogger(f func(format string, args ...any)) {
	if f != nil {
		o.logf = f
		o.sched.SetLogger(f)
	}
}

// Recover implements the crash-recovery contract in docs/design.md
// §State, persistence, recovery. It MUST be called once on startup
// before Run, PollOnce, or ScheduleOnce.
//
// The contract:
//
//  1. Every non-terminal agent whose PID is no longer alive is
//     transitioned to crashed, then re-queued by re-inserting a
//     pending row for the same work_item_id on the next PollOnce.
//  2. Stale locks (heartbeat older than 2× LockTTL) are dropped.
func (o *Orchestrator) Recover(ctx context.Context) error {
	if _, err := o.db.ExpireStaleLocks(ctx, 2*o.cfg.LockTTL); err != nil {
		return fmt.Errorf("orchestrator: expire stale locks: %w", err)
	}
	nonTerminal := []state.AgentState{
		state.AgentSpawning,
		state.AgentRunning,
		state.AgentPROpen,
		state.AgentGatesRunning,
	}
	agents, err := o.db.ListAgentsByState(ctx, nonTerminal...)
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
		o.logf("orchestrator: requeued crashed agent %d (%s)", a.ID, a.WorkItemID)
	}
	return nil
}

// PollOnce calls SpecAdapter.List and upserts a pending agent for
// every planned item. Already-known items are left in place. The
// loop checks ctx between items so a cancelled daemon shutdown does
// not block on a large catalog.
func (o *Orchestrator) PollOnce(ctx context.Context) error {
	items, err := o.adapter.List(ctx)
	if err != nil {
		return fmt.Errorf("orchestrator: adapter list: %w", err)
	}
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if it.Status != schemas.StatusPlanned {
			continue
		}
		if _, err := o.db.UpsertPending(ctx, string(it.ID), string(it.Lane)); err != nil {
			return fmt.Errorf("orchestrator: upsert %s: %w", it.ID, err)
		}
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
	ids, tickErr := o.sched.Tick(ctx)
	for _, id := range ids {
		a, err := o.db.GetAgent(ctx, id)
		if err != nil {
			return fmt.Errorf("orchestrator: load agent %d: %w", id, err)
		}
		result, err := o.spawner.Spawn(ctx, spawner.Request{
			AgentID:    a.ID,
			WorkItemID: a.WorkItemID,
			Lane:       a.Lane,
		})
		if err != nil {
			_, _ = o.db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{})
			_, _ = o.db.ReleaseAgentLocks(ctx, a.ID)
			_, _ = o.db.TransitionAgent(ctx, a.ID, state.AgentPending, state.AgentMutation{})
			_ = o.db.RecordEvent(ctx, a.ID, "spawn_failed", fmt.Sprintf(`{"error":%q}`, err.Error()))
			o.logf("orchestrator: spawn failed for agent %d: %v", a.ID, err)
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
		o.logf("orchestrator: spawned agent %d (%s) pid=%d session=%s", a.ID, a.WorkItemID, pid, sess)
	}
	if tickErr != nil {
		return fmt.Errorf("orchestrator: scheduler tick: %w", tickErr)
	}
	return nil
}

// Heartbeat refreshes every lock owned by an active agent. The
// orchestrator runs Heartbeat on cfg.HeartbeatInterval so that locks
// only age out when an agent has truly crashed.
func (o *Orchestrator) Heartbeat(ctx context.Context) error {
	active, err := o.db.ListAgentsByState(ctx,
		state.AgentSpawning, state.AgentRunning, state.AgentPROpen, state.AgentGatesRunning)
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
		o.logf("orchestrator: initial poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		o.logf("orchestrator: initial schedule: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pollT.C:
			if err := o.PollOnce(ctx); err != nil {
				o.logf("orchestrator: poll: %v", err)
			}
		case <-tickT.C:
			if err := o.ScheduleOnce(ctx); err != nil {
				o.logf("orchestrator: tick: %v", err)
			}
		case <-heartT.C:
			if err := o.Heartbeat(ctx); err != nil {
				o.logf("orchestrator: heartbeat: %v", err)
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
