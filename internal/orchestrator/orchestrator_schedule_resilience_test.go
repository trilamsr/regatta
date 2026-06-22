//go:build unix

package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestScheduleOnce_ClockSkewClampsDurationToZero asserts a backwards clock jump between tick start and end is clamped to durationMs=0 in the emitted tick.completed event (R19-A O1).
func TestScheduleOnce_ClockSkewClampsDurationToZero(t *testing.T) {
	ctx := context.Background()
	o, _, _, _ := newHarness(t, 1)

	now := time.Unix(1_700_000_000, 0).UTC()
	calls := 0
	o.cfg.Clock = func() time.Time {
		calls++
		// First call (startedAt) returns "now"; later calls (the deferred
		// durationMs computation in ScheduleOnce) return an earlier time
		// to simulate an NTP backwards jump.
		if calls == 1 {
			return now
		}
		return now.Add(-500 * time.Millisecond)
	}

	h := obstest.New()
	o.log = slog.New(h)

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	rec, ok := h.FindEvent(obs.EventTickCompleted)
	if !ok {
		t.Fatalf("tick.completed not emitted")
	}
	v, ok := recordHasAttr(rec, string(obs.KeyDurationMs))
	if !ok {
		t.Fatalf("tick.completed missing %s attr", obs.KeyDurationMs)
	}
	if got := v.Int64(); got < 0 {
		t.Fatalf("tick.completed duration_ms=%d; want >= 0 (clock-skew guard missing)", got)
	}
}

// TestScheduleOnce_GetAgentErrorProcessesSiblings asserts a per-agent GetAgent failure logs + continues rather than aborting the whole tick (R19-A O2).
func TestScheduleOnce_GetAgentErrorProcessesSiblings(t *testing.T) {
	ctx := context.Background()
	o, stub, _, _ := newHarness(t, 3)
	h := obstest.New()
	o.log = slog.New(h)

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}

	// Inject a hook that fails on the second agent the loop processes.
	calls := 0
	o.getAgentOverride = func(ctx context.Context, id int64) (*state.Agent, error) {
		calls++
		if calls == 2 {
			return nil, fmt.Errorf("synthetic GetAgent failure on id=%d", id)
		}
		return o.db.GetAgent(ctx, id)
	}

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v (expected nil; per-agent GetAgent err must not abort tick)", err)
	}

	// The two surviving agents must have been handed to the spawner.
	if got := len(stub.Calls()); got != 2 {
		t.Fatalf("spawner calls=%d; want 2 (sibling agents stranded by aborted tick)", got)
	}
	if _, ok := h.FindEvent(obs.EventTickAgentLoadFailed); !ok {
		t.Fatalf("%s not emitted on GetAgent failure", obs.EventTickAgentLoadFailed)
	}
}

// TestScheduleOnce_TransitionAgentErrorProcessesSiblings asserts a per-agent TransitionAgent failure stamps PID+crashes the row + continues rather than stranding sibling spawns OR re-emitting the work_item (R19-A O3).
func TestScheduleOnce_TransitionAgentErrorProcessesSiblings(t *testing.T) {
	ctx := context.Background()
	o, stub, db, _ := newHarness(t, 3)
	h := obstest.New()
	o.log = slog.New(h)

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}

	// scheduler.Tick returns ids ordered by ListSpawnable's `ORDER BY
	// w.id` ASC → calls==2 always hits the middle of the 3 reserved
	// agents deterministically (no need to sort the ids slice or switch
	// to a map[id]error injection).
	calls := 0
	o.transitionAgentOverride = func(ctx context.Context, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error) {
		// Only intercept the spawning→running transition; the
		// post-failure crashed-stamp transition must reach the real DB
		// so the failed agent's PID lands on the row + locks are
		// released.
		if next != state.AgentRunning {
			return o.db.TransitionAgent(ctx, id, next, mut)
		}
		calls++
		if calls == 2 {
			return nil, fmt.Errorf("synthetic TransitionAgent failure on id=%d", id)
		}
		return o.db.TransitionAgent(ctx, id, next, mut)
	}

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v (expected nil; per-agent TransitionAgent err must not abort tick)", err)
	}

	if got := len(stub.Calls()); got != 3 {
		t.Fatalf("spawner calls=%d; want 3 (all three agents must reach spawn even when one transition fails)", got)
	}
	running, err := db.ListAgentsByState(ctx, state.AgentRunning)
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	if got := len(running); got != 2 {
		t.Fatalf("running agents=%d; want 2 (one TransitionAgent failure must not strand siblings)", got)
	}
	// The failed agent must be crashed (NOT pending) so a later tick
	// cannot re-reserve the same work_item and spawn a SECOND process
	// against the still-live PID (R19-A reviewer HIGH double-spawn).
	crashed, err := db.ListAgentsByState(ctx, state.AgentCrashed)
	if err != nil {
		t.Fatalf("list crashed: %v", err)
	}
	if got := len(crashed); got != 1 {
		t.Fatalf("crashed agents=%d; want 1 (post-transition-failed must land crashed, not pending)", got)
	}
	// PID + SessionID must be stamped on the crashed row so the
	// follow-up reaper (or `regatta debug`) can kill the orphan.
	if crashed[0].PID == 0 {
		t.Fatalf("crashed agent PID=0; want stub PID stamped (orphan unkillable)")
	}
	if crashed[0].SessionID == "" {
		t.Fatalf("crashed agent SessionID empty; want stub session stamped")
	}
	pending, err := db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if got := len(pending); got != 0 {
		t.Fatalf("pending agents=%d; want 0 (rollback-to-pending would double-spawn)", got)
	}
	if _, ok := h.FindEvent(obs.EventSpawnPostTransitionFailed); !ok {
		t.Fatalf("%s not emitted on post-spawn TransitionAgent failure", obs.EventSpawnPostTransitionFailed)
	}
}
