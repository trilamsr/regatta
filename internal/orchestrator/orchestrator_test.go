//go:build unix

// PollOnce relies on lockfile.Acquire, whose flock + PID-stamp
// contract is POSIX-only (see lockfile/lockfile_test.go header).
// The orchestrator runtime targets Linux + macOS per docs/design.md;
// Windows is a build-only target via orchestrator_windows.go.

package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil"
)

func recordHasAttr(r slog.Record, key string) (slog.Value, bool) {
	var found slog.Value
	var ok bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = a.Value
			ok = true
			return false
		}
		return true
	})
	return found, ok
}

// noopBriefLoader is a zero-side-effect BriefLoader stub used by the
// internal orchestrator_test harness. Live tests against the real
// program.BriefLoader sit in pollonce_test.go (package
// orchestrator_test) to avoid the internal/program -> internal/
// orchestrator import cycle.
type noopBriefLoader struct{}

func (noopBriefLoader) Sync(context.Context, time.Time) error { return nil }

const tmplItem = `---
id: ITEM-%s
title: Item %s
kind: feature
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: only criterion
`

func newHarness(t *testing.T, count int) (*Orchestrator, *spawner.Stub, *state.DB, string) {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < count; i++ {
		name := string(rune('A' + i))
		full := filepath.Join(dir, ".regatta", "items", name+".md")
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		body := []byte(replace(tmplItem, name))
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// MinPoll=1ns disables the adaptersync MinPoll gate (#888) so back-
	// to-back PollOnce calls in this harness still propagate adapter
	// changes — the gate is verified in adaptersync/adaptersync_minpoll_test.go.
	ad, err := adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: dir, MinPoll: time.Nanosecond})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	stub := spawner.New(spawner.Config{})
	syncer, err := adaptersync.New(adaptersync.Config{Adapter: ad, DB: db})
	if err != nil {
		t.Fatalf("adaptersync.New: %v", err)
	}
	o := New(Config{
		AdapterSync:       syncer,
		BriefLoader:       noopBriefLoader{},
		DB:                db,
		Scheduler:         scheduler.New(db, scheduler.Config{LockTTL: time.Minute}),
		Spawner:           stub,
		DBPath:            dbPath,
		PollInterval:      time.Second,
		TickInterval:      time.Second,
		HeartbeatInterval: time.Second,
		LockTTL:           time.Minute,
	})
	return o, stub, db, dir
}

func replace(s, name string) string {
	return strings.ReplaceAll(s, "%s", name)
}

func TestPollAndScheduleEndToEnd(t *testing.T) {
	ctx := context.Background()
	o, stub, db, _ := newHarness(t, 2)

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	// New universal-queue semantics: PollOnce mirrors into work_items
	// only. Agent materialization happens inside ScheduleOnce's
	// scheduler.Tick (Wave 4 join-driven reservation).
	spawnable, err := db.ListSpawnable(ctx)
	if err != nil {
		t.Fatalf("list spawnable: %v", err)
	}
	if len(spawnable) != 2 {
		t.Fatalf("spawnable work_items=%d, want 2", len(spawnable))
	}

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	running, _ := db.ListAgentsByState(ctx, state.AgentRunning)
	if len(running) != 2 {
		t.Fatalf("running=%d, want 2", len(running))
	}
	calls := stub.Calls()
	if len(calls) != 2 {
		t.Fatalf("spawner calls=%d, want 2", len(calls))
	}
	for _, a := range running {
		if a.PID >= 0 || a.SessionID == "" {
			t.Fatalf("agent %d missing spawn identity: %+v", a.ID, a)
		}
	}
}

func TestPollIsIdempotent(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll 1: %v", err)
	}
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	// Idempotence now lives at the work_items level — repeated
	// adapterSync.Sync calls must not duplicate rows.
	spawnable, err := db.ListSpawnable(ctx)
	if err != nil {
		t.Fatalf("list spawnable: %v", err)
	}
	if len(spawnable) != 1 {
		t.Fatalf("spawnable=%d, want 1", len(spawnable))
	}
}

func TestRecoverRequeuesDeadAgents(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	// Stub spawner uses negative PIDs which pidAlive() treats as dead,
	// so Recover should requeue every running agent.
	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 {
		t.Fatalf("pending after recover=%d, want 1", len(pending))
	}
	running, _ := db.ListAgentsByState(ctx, state.AgentRunning)
	if len(running) != 0 {
		t.Fatalf("running after recover=%d, want 0", len(running))
	}
	locks, _ := db.ListLocks(ctx)
	if len(locks) != 0 {
		t.Fatalf("locks not released: %+v", locks)
	}
}

// failingAdapter returns an error on every List call. Used to prove
// Run survives a broken SpecAdapter without crashing.
type failingAdapter struct{ calls atomic.Int64 }

func (a *failingAdapter) List(ctx context.Context) ([]schemas.WorkItem, error) {
	n := a.calls.Add(1)
	return nil, fmt.Errorf("synthetic adapter failure %d", n)
}
func (a *failingAdapter) Get(context.Context, schemas.WorkItemID) (schemas.WorkItem, error) {
	return schemas.WorkItem{}, schemas.ErrNotFound
}
func (a *failingAdapter) UpdateStatus(context.Context, schemas.WorkItemID, schemas.Status, string) error {
	return nil
}
func (a *failingAdapter) Capabilities() schemas.Capabilities {
	return schemas.Capabilities{MinPollInterval: time.Millisecond}
}

func TestRunSurvivesFailingAdapter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ad := &failingAdapter{}
	syncer, err := adaptersync.New(adaptersync.Config{Adapter: ad, DB: db})
	if err != nil {
		t.Fatalf("adaptersync.New: %v", err)
	}
	o := New(Config{
		AdapterSync:       syncer,
		BriefLoader:       noopBriefLoader{},
		DB:                db,
		Scheduler:         scheduler.New(db, scheduler.Config{LockTTL: time.Minute}),
		Spawner:           spawner.New(spawner.Config{}),
		DBPath:            dbPath,
		PollInterval:      10 * time.Millisecond,
		TickInterval:      10 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
		LockTTL:           time.Minute,
	})

	// #1156: prior assertion timed Run for 80ms then required calls>=2.
	// Race: scheduler stall could cancel ctx before the first ticker tick,
	// leaving calls==1. Poll until the threshold is met, then cancel Run.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- o.Run(ctx) }()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	testutil.Eventually(t, waitCtx, 5*time.Millisecond, func() bool {
		return ad.calls.Load() >= 2
	}, "adapter polled <2 times despite failures")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v for failing adapter; want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRecoverIsIdempotent(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover 1: %v", err)
	}
	// Second call: every non-terminal agent is now pending, so
	// Recover should be a no-op (no panics, no extra requeues).
	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover 2: %v", err)
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 {
		t.Fatalf("pending after 2x recover=%d, want 1", len(pending))
	}
}

// TestOrchestratorRecordsEvents pins the audit-trail contract so every spawn and every crash-recovery requeue lands a typed event row for the audit sink to walk.
func TestOrchestratorRecordsEvents(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}

	events, err := db.ListEvents(ctx, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	kinds := map[string]int{}
	for _, e := range events {
		kinds[e.Kind]++
	}
	if kinds[string(obs.EventSpawnCompleted)] != 1 {
		t.Errorf("expected 1 spawn.completed event, got %d (all: %v)", kinds[string(obs.EventSpawnCompleted)], kinds)
	}
	if kinds["recovered_crashed"] != 1 {
		t.Errorf("expected 1 recovered_crashed event, got %d (all: %v)", kinds["recovered_crashed"], kinds)
	}
}

// TestPollPropagatesLaneChange pins the orchestrator end-to-end on the lane-drift bug: a markdown item whose `lane:` frontmatter is rewritten must be reflected on the existing agent row by the next PollOnce, else per-lane caps are defeated.
func TestPollPropagatesLaneChange(t *testing.T) {
	ctx := context.Background()
	o, _, db, dir := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	// Lane propagation now lives on work_items (universal-queue). The
	// agents row is materialized later by ScheduleOnce's
	// scheduler.Tick, but adapter rewrites of `lane:` must hit
	// work_items on the next PollOnce regardless.
	wi, err := db.GetWorkItem(ctx, "ITEM-A")
	if err != nil {
		t.Fatalf("get work_item: %v", err)
	}
	if wi.Lane != "server" {
		t.Fatalf("initial work_item lane=%q, want server", wi.Lane)
	}

	file := filepath.Join(dir, ".regatta", "items", "A.md")
	updated := []byte(`---
id: ITEM-A
title: Item A
kind: feature
lane: client
status: planned
---

## Acceptance criteria

- [planned] c1: only criterion
`)
	if err := os.WriteFile(file, updated, 0o644); err != nil {
		t.Fatalf("rewrite item file: %v", err)
	}

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	wi, err = db.GetWorkItem(ctx, "ITEM-A")
	if err != nil {
		t.Fatalf("get work_item after re-poll: %v", err)
	}
	if wi.Lane != "client" {
		t.Fatalf("expected work_item lane=client after re-poll; got %q", wi.Lane)
	}
}

// TestOrchestrator_Tick_EmitsStartedAndCompleted pins spec §5.1.
func TestOrchestrator_Tick_EmitsStartedAndCompleted(t *testing.T) {
	ctx := context.Background()
	o, _, _, _ := newHarness(t, 1)
	h := obstest.New()
	o.cfg.Logger = slog.New(h)
	// Re-init the logger on the live orchestrator so the test exercises
	// the same field New() would populate from cfg.Logger.
	o.log = slog.New(h)

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	started, ok := h.FindEvent(obs.EventTickStarted)
	if !ok {
		t.Fatalf("expected event %q in captured records; got %d records", obs.EventTickStarted, len(h.Records()))
	}
	if started.Level != slog.LevelInfo {
		t.Fatalf("non-zero tick.started level=%s, want INFO (#1066 — must stay loud when dispatch happens)", started.Level)
	}
	completed, ok := h.FindEvent(obs.EventTickCompleted)
	if !ok {
		t.Fatalf("expected event %q in captured records; got %d records", obs.EventTickCompleted, len(h.Records()))
	}
	if completed.Level != slog.LevelInfo {
		t.Fatalf("non-zero tick.completed level=%s, want INFO (#1066)", completed.Level)
	}
	if _, ok := recordHasAttr(completed, string(obs.KeyWorkItemsEvaluated)); !ok {
		t.Fatalf("tick.completed missing attr %q", obs.KeyWorkItemsEvaluated)
	}
}

// TestOrchestrator_Tick_EmitsOnEmptyQueue pins spec §3.3.
func TestOrchestrator_Tick_EmitsOnEmptyQueue(t *testing.T) {
	ctx := context.Background()
	o, _, _, _ := newHarness(t, 0)
	h := obstest.New()
	o.log = slog.New(h)

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if _, ok := h.FindEvent(obs.EventTickStarted); !ok {
		t.Fatalf("tick.started missing on empty-queue tick")
	}
	completed, ok := h.FindEvent(obs.EventTickCompleted)
	if !ok {
		t.Fatalf("tick.completed missing on empty-queue tick")
	}
	v, ok := recordHasAttr(completed, string(obs.KeyWorkItemsEvaluated))
	if !ok {
		t.Fatalf("tick.completed missing work_items_evaluated on empty queue")
	}
	if v.Int64() != 0 {
		t.Fatalf("work_items_evaluated=%d, want 0 on empty queue", v.Int64())
	}
}

// TestOrchestrator_Tick_EmptyQueueLogsAtDebug pins #1066: zero-work ticks demote tick.started/completed to DEBUG so the operator log surface is not 98% scheduler heartbeat.
func TestOrchestrator_Tick_EmptyQueueLogsAtDebug(t *testing.T) {
	ctx := context.Background()
	o, _, _, _ := newHarness(t, 0)
	h := obstest.New()
	o.log = slog.New(h)

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	started, ok := h.FindEvent(obs.EventTickStarted)
	if !ok {
		t.Fatalf("tick.started missing")
	}
	if started.Level != slog.LevelDebug {
		t.Fatalf("zero-work tick.started level=%s, want DEBUG (#1066)", started.Level)
	}
	completed, ok := h.FindEvent(obs.EventTickCompleted)
	if !ok {
		t.Fatalf("tick.completed missing")
	}
	if completed.Level != slog.LevelDebug {
		t.Fatalf("zero-work tick.completed level=%s, want DEBUG (#1066)", completed.Level)
	}
}

// TestOrchestrator_Tick_SlowTickEmitsWarn pins the additive tick.slow WARN: a single tick >=1s surfaces via WARN beside the existing tick.completed event (#1066 demote contract preserved).
func TestOrchestrator_Tick_SlowTickEmitsWarn(t *testing.T) {
	ctx := context.Background()
	o, _, _, _ := newHarness(t, 0)
	h := obstest.New()
	o.log = slog.New(h)
	// Clock seam: first call returns t0 (startedAt), second call (in
	// the defer) returns t0+1500ms so durationMs computes to 1500.
	base := time.Unix(1_700_000_000, 0).UTC()
	var calls int
	o.cfg.Clock = func() time.Time {
		calls++
		if calls == 1 {
			return base
		}
		return base.Add(1500 * time.Millisecond)
	}

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	slow, ok := h.FindEvent(obs.EventName("tick.slow"))
	if !ok {
		t.Fatalf("tick.slow missing on >=1s tick; records=%d", len(h.Records()))
	}
	if slow.Level != slog.LevelWarn {
		t.Fatalf("tick.slow level=%s, want WARN", slow.Level)
	}
	v, ok := recordHasAttr(slow, string(obs.KeyDurationMs))
	if !ok {
		t.Fatalf("tick.slow missing %q attr", obs.KeyDurationMs)
	}
	if v.Int64() != 1500 {
		t.Fatalf("tick.slow duration_ms=%d, want 1500", v.Int64())
	}
}

// TestOrchestrator_NilLogger_UsesDefault pins spec §4.1.
func TestOrchestrator_NilLogger_UsesDefault(t *testing.T) {
	o, _, _, _ := newHarness(t, 0)
	if o.log == nil {
		t.Fatalf("New() left log nil; slog.Default fallback not wired")
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	o, _, _, _ := newHarness(t, 1)
	o.cfg.PollInterval = 10 * time.Millisecond
	o.cfg.TickInterval = 10 * time.Millisecond
	o.cfg.HeartbeatInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- o.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// fakeHeartbeatToucher records every Touch call so tests can assert
// the orchestrator Run loop refreshes the /healthz cell every tick (#1218).
type fakeHeartbeatToucher struct {
	mu     sync.Mutex
	counts int
}

func (f *fakeHeartbeatToucher) Touch() {
	f.mu.Lock()
	f.counts++
	f.mu.Unlock()
}

func (f *fakeHeartbeatToucher) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts
}

// TestRunTouchesHealthHeartbeat asserts Run touches HealthHeartbeat every tick so /healthz stays fresh (#1218).
func TestRunTouchesHealthHeartbeat(t *testing.T) {
	o, _, _, _ := newHarness(t, 0)
	o.cfg.PollInterval = 5 * time.Millisecond
	o.cfg.TickInterval = 5 * time.Millisecond
	o.cfg.HeartbeatInterval = 5 * time.Millisecond
	hb := &fakeHeartbeatToucher{}
	o.heartbeat = hb

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- o.Run(ctx) }()
	<-done
	if hb.Count() < 2 {
		t.Fatalf("HealthHeartbeat.Touch count=%d; want >=2 ticks observed", hb.Count())
	}
}

// TestScheduleOnceThreadsItemBodyAndRepoRootIntoSpawnRequest asserts ItemBody+RepoRoot land on spawner.Request via the ItemBody loader seam.
func TestScheduleOnceThreadsItemBodyAndRepoRootIntoSpawnRequest(t *testing.T) {
	ctx := context.Background()
	o, stub, _, dir := newHarness(t, 1)
	o.cfg.RepoRoot = dir
	o.cfg.ItemBody = func(_ context.Context, workItemID string) (string, bool) {
		return "BODY-FOR-" + workItemID, true
	}

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	calls := stub.Calls()
	if len(calls) != 1 {
		t.Fatalf("spawner calls=%d, want 1", len(calls))
	}
	got := calls[0]
	if got.ItemBody != "BODY-FOR-"+got.WorkItemID {
		t.Fatalf("ItemBody=%q, want BODY-FOR-%s", got.ItemBody, got.WorkItemID)
	}
	if got.RepoRoot != dir {
		t.Fatalf("RepoRoot=%q, want %q", got.RepoRoot, dir)
	}
}

// MAY-81 changed the missing-item-body contract from "spawn blind" to
// "hold the item": the prior TestScheduleOnceMissingItemBodyStillSpawns
// is superseded by TestScheduleOnce_MissingItemBodyHoldsItem in
// orchestrator_schedule_test.go.
