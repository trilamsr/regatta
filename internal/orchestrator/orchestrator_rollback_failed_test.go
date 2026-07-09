//go:build unix

package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestScheduleOnce_RollbackTransitionFailureLoggedAndCounted asserts a TransitionAgent failure during rollbackReservation emits a WARN log carrying agent_id + lane + step=transition and increments regatta.orchestrator.rollback_failed{step=transition} (MAY: silent lane leak).
func TestScheduleOnce_RollbackTransitionFailureLoggedAndCounted(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	o, _, _, _ := newHarness(t, 1)
	o.cfg.Meter = mp.Meter("orchestrator-test")
	o.spawner = &failingSpawner{}
	h := obstest.New()
	o.log = slog.New(h)

	// Rebuild the counter against the injected meter — New() captured the
	// zero-cfg meter before the test override landed.
	rebuildRollbackCounter(t, o)

	// Fail the crashed-transition inside rollbackReservation; allow the
	// subsequent pending-transition to succeed so the release_locks step
	// still runs (deterministic scoping to the transition-step signal).
	o.transitionAgentOverride = func(ctx context.Context, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error) {
		if next == state.AgentCrashed {
			return nil, fmt.Errorf("synthetic TransitionAgent(crashed) failure on id=%d", id)
		}
		return o.db.TransitionAgent(ctx, id, next, mut)
	}

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	rec, ok := h.FindEvent(obs.EventOrchestratorRollbackFailed)
	if !ok {
		t.Fatalf("%s not emitted on rollback TransitionAgent failure — operator has zero signal for silent lane leak", obs.EventOrchestratorRollbackFailed)
	}
	if lvl := rec.Level; lvl != slog.LevelWarn {
		t.Fatalf("rollback_failed level=%v; want WARN", lvl)
	}
	if _, ok := recordHasAttr(rec, string(obs.KeyAgentID)); !ok {
		t.Fatalf("rollback_failed missing %s attr — operator cannot pin the leaked lane to a specific agent", obs.KeyAgentID)
	}
	if v, ok := recordHasAttr(rec, string(obs.KeyLane)); !ok || v.String() == "" {
		t.Fatalf("rollback_failed missing %s attr — operator cannot grep by lane for exhaustion", obs.KeyLane)
	}
	stepAttr, ok := recordHasAttr(rec, "step")
	if !ok {
		t.Fatalf("rollback_failed missing step attr — operator cannot distinguish transition-vs-release-locks failure")
	}
	if got := stepAttr.String(); got != "transition" {
		t.Fatalf("rollback_failed step=%q; want %q", got, "transition")
	}
	if _, ok := recordHasAttr(rec, string(obs.KeyErr)); !ok {
		t.Fatalf("rollback_failed missing %s attr — original error dropped", obs.KeyErr)
	}

	got := readRollbackFailedByStep(t, reader)
	if got["transition"] != 1 {
		t.Fatalf("regatta.orchestrator.rollback_failed{step=transition}=%d; want 1", got["transition"])
	}
}

// TestScheduleOnce_RollbackReleaseLocksFailureLoggedAndCounted asserts a ReleaseAgentLocks failure during rollbackReservation emits a WARN log with step=release_locks and increments regatta.orchestrator.rollback_failed{step=release_locks} (MAY: silent lane leak).
func TestScheduleOnce_RollbackReleaseLocksFailureLoggedAndCounted(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	o, _, _, _ := newHarness(t, 1)
	o.cfg.Meter = mp.Meter("orchestrator-test")
	o.spawner = &failingSpawner{}
	h := obstest.New()
	o.log = slog.New(h)
	rebuildRollbackCounter(t, o)

	o.releaseAgentLocksOverride = func(ctx context.Context, id int64) (int64, error) {
		return 0, fmt.Errorf("synthetic ReleaseAgentLocks failure on id=%d", id)
	}

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	found := false
	for _, rec := range h.Records() {
		if rec.Message != string(obs.EventOrchestratorRollbackFailed) {
			continue
		}
		step, ok := recordHasAttr(rec, "step")
		if !ok || step.String() != "release_locks" {
			continue
		}
		found = true
		if rec.Level != slog.LevelWarn {
			t.Fatalf("rollback_failed{release_locks} level=%v; want WARN", rec.Level)
		}
		if _, ok := recordHasAttr(rec, string(obs.KeyAgentID)); !ok {
			t.Fatalf("rollback_failed{release_locks} missing %s attr", obs.KeyAgentID)
		}
		if v, ok := recordHasAttr(rec, string(obs.KeyLane)); !ok || v.String() == "" {
			t.Fatalf("rollback_failed{release_locks} missing %s attr", obs.KeyLane)
		}
		if _, ok := recordHasAttr(rec, string(obs.KeyErr)); !ok {
			t.Fatalf("rollback_failed{release_locks} missing %s attr", obs.KeyErr)
		}
	}
	if !found {
		t.Fatalf("%s{step=release_locks} not emitted — ReleaseAgentLocks error silently dropped", obs.EventOrchestratorRollbackFailed)
	}

	got := readRollbackFailedByStep(t, reader)
	if got["release_locks"] != 1 {
		t.Fatalf("regatta.orchestrator.rollback_failed{step=release_locks}=%d; want 1", got["release_locks"])
	}
}

// TestScheduleOnce_RollbackBothStepsFailBothLoggedAndCounted asserts BOTH steps failing produces two log lines + two counter increments — one per step (defense: an implementation that returns after the first failure would leak the ReleaseAgentLocks signal on the same tick).
func TestScheduleOnce_RollbackBothStepsFailBothLoggedAndCounted(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	o, _, _, _ := newHarness(t, 1)
	o.cfg.Meter = mp.Meter("orchestrator-test")
	o.spawner = &failingSpawner{}
	h := obstest.New()
	o.log = slog.New(h)
	rebuildRollbackCounter(t, o)

	o.transitionAgentOverride = func(ctx context.Context, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error) {
		if next == state.AgentCrashed {
			return nil, fmt.Errorf("synthetic TransitionAgent(crashed) failure on id=%d", id)
		}
		return o.db.TransitionAgent(ctx, id, next, mut)
	}
	o.releaseAgentLocksOverride = func(ctx context.Context, id int64) (int64, error) {
		return 0, fmt.Errorf("synthetic ReleaseAgentLocks failure on id=%d", id)
	}

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	seen := map[string]int{}
	for _, rec := range h.Records() {
		if rec.Message != string(obs.EventOrchestratorRollbackFailed) {
			continue
		}
		step, ok := recordHasAttr(rec, "step")
		if !ok {
			continue
		}
		seen[step.String()]++
	}
	if seen["transition"] < 1 {
		t.Fatalf("rollback_failed{transition} log count=%d; want >=1 (both-step failure must emit both signals)", seen["transition"])
	}
	if seen["release_locks"] < 1 {
		t.Fatalf("rollback_failed{release_locks} log count=%d; want >=1 (both-step failure must emit both signals)", seen["release_locks"])
	}

	got := readRollbackFailedByStep(t, reader)
	if got["transition"] < 1 {
		t.Fatalf("counter{step=transition}=%d; want >=1", got["transition"])
	}
	if got["release_locks"] < 1 {
		t.Fatalf("counter{step=release_locks}=%d; want >=1", got["release_locks"])
	}
}

// rebuildRollbackCounter re-initialises the orchestrator's rollback-failed
// counter against the currently-wired cfg.Meter so tests that inject a
// ManualReader-backed provider observe increments emitted by ScheduleOnce.
func rebuildRollbackCounter(t *testing.T, o *Orchestrator) {
	t.Helper()
	c, err := o.cfg.ResolveMeter().Int64Counter("regatta.orchestrator.rollback_failed")
	if err != nil {
		t.Fatalf("rebuild rollback_failed counter: %v", err)
	}
	o.rollbackFailed = c
}

// readRollbackFailedByStep aggregates the rollback_failed counter's
// DataPoints keyed by the `step` label so tests can assert per-step values
// without hard-coding DataPoint ordering.
func readRollbackFailedByStep(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "regatta.orchestrator.rollback_failed" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("counter data type = %T; want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				step, ok := dp.Attributes.Value("step")
				if !ok {
					continue
				}
				out[step.AsString()] += dp.Value
			}
		}
	}
	return out
}
