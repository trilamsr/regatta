// Package scheduler picks pending agents that are eligible to spawn
// and reserves their lanes + hotspot locks.
//
// One Tick is a single pass over the queue: it does not block, sleep,
// or call out to the spawner. The orchestrator's Run loop calls Tick
// on a timer and feeds the returned agent IDs into the spawner.
//
// Deadlock safety: hotspot locks are always acquired in lexicographic
// order (docs/design.md §Concurrency & soft-lock policy). Lane
// concurrency is gated by CountAgentsByLane against the configured
// per-lane cap.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// HotspotResolver maps a work-item ID to the hotspot lock names the
// item touches. The orchestrator typically derives this from
// regatta.yaml's `hotspots` list + per-lane `paths`. An empty slice is
// a valid return for items that touch no hotspots.
type HotspotResolver func(workItemID string) []string

// Config holds tunables for a Scheduler. All durations and caps are
// derived from regatta.yaml at orchestrator construction time.
type Config struct {
	// LaneCaps gives the max number of active agents per lane. A lane
	// missing from the map is treated as unlimited. The default lane
	// uses the empty-string key.
	LaneCaps map[string]int

	// LockTTL is the heartbeat lease duration passed to
	// state.DB.TryAcquireLocks. A lock whose heartbeat is older than
	// LockTTL is considered released and may be stolen.
	LockTTL time.Duration

	// Hotspots resolves an item to its hotspot lock names. If nil, the
	// scheduler skips lock acquisition entirely.
	Hotspots HotspotResolver
}

// Scheduler is goroutine-safe; the orchestrator may share a single
// Scheduler across the Run loop and ad-hoc admin commands.
type Scheduler struct {
	db   *state.DB
	cfg  Config
	logf func(format string, args ...any)
}

// New constructs a Scheduler. The Config is copied; later mutations to
// cfg.LaneCaps do not affect the running scheduler.
func New(db *state.DB, cfg Config) *Scheduler {
	caps := make(map[string]int, len(cfg.LaneCaps))
	for k, v := range cfg.LaneCaps {
		caps[k] = v
	}
	cfg.LaneCaps = caps
	if cfg.LockTTL <= 0 {
		cfg.LockTTL = 15 * time.Minute
	}
	return &Scheduler{db: db, cfg: cfg, logf: func(string, ...any) {}}
}

// SetLogger installs a printf-style logger used for skip diagnostics
// (e.g. an agent skipped because its hotspot lock is held). Silent
// by default.
func (s *Scheduler) SetLogger(f func(format string, args ...any)) {
	if f != nil {
		s.logf = f
	}
}

// activeStates lists agent states that count against a lane's
// concurrency cap. Pending agents do not count (they are eligible
// candidates, not active workers); terminal states never count.
var activeStates = []state.AgentState{
	state.AgentSpawning,
	state.AgentRunning,
	state.AgentPROpen,
	state.AgentGatesRunning,
	state.AgentAwaitingMerge,
	state.AgentGatesFailed,
}

// Tick performs one scheduling pass. It returns the IDs of agents
// transitioned from pending to spawning during this pass, in the order
// they were reserved.
//
// Tick never blocks on locks: an agent whose hotspot is held by
// another agent is left in pending and retried on the next tick.
func (s *Scheduler) Tick(ctx context.Context) ([]int64, error) {
	if _, err := s.db.ExpireStaleLocks(ctx, s.cfg.LockTTL); err != nil {
		return nil, fmt.Errorf("scheduler: expire stale locks: %w", err)
	}

	occupancy, err := s.db.CountAgentsByLane(ctx, activeStates...)
	if err != nil {
		return nil, err
	}

	pending, err := s.db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		return nil, err
	}

	var reserved []int64
	for _, a := range pending {
		if !s.laneHasCapacity(a.Lane, occupancy) {
			continue
		}
		locks := s.resolveLocks(a.WorkItemID)
		if err := s.db.TryAcquireLocks(ctx, locks, a.ID, s.cfg.LockTTL); err != nil {
			if errors.Is(err, state.ErrLockHeld) {
				s.logf("scheduler: agent %d (%s) skipped: hotspot locked", a.ID, a.WorkItemID)
				continue
			}
			return reserved, fmt.Errorf("scheduler: acquire locks for agent %d: %w", a.ID, err)
		}
		if _, err := s.db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
			// Release the locks we just took so we don't deadlock the
			// next tick.
			_, _ = s.db.ReleaseAgentLocks(ctx, a.ID)
			return reserved, fmt.Errorf("scheduler: mark agent %d spawning: %w", a.ID, err)
		}
		occupancy[a.Lane]++
		reserved = append(reserved, a.ID)
	}
	return reserved, nil
}

func (s *Scheduler) laneHasCapacity(lane string, occupancy map[string]int) bool {
	limit, gated := s.cfg.LaneCaps[lane]
	if !gated {
		return true
	}
	return occupancy[lane] < limit
}

func (s *Scheduler) resolveLocks(workItemID string) []string {
	if s.cfg.Hotspots == nil {
		return nil
	}
	names := s.cfg.Hotspots(workItemID)
	if len(names) == 0 {
		return nil
	}
	out := make([]string, len(names))
	copy(out, names)
	sort.Strings(out)
	return out
}
