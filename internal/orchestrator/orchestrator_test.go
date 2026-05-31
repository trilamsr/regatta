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
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// captureHandler records every slog.Record so tests can assert the
// orchestrator emitted the canonical obs events. Threadsafe so the Run
// loop tests can hit it without -race tripping.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(name string) slog.Handler { return h }

func (h *captureHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

func (h *captureHandler) findEvent(name obs.EventName) (slog.Record, bool) {
	for _, r := range h.Records() {
		if r.Message == string(name) {
			return r, true
		}
	}
	return slog.Record{}, false
}

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
	ad, err := adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: dir})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	stub := spawner.NewStub()
	o := New(Config{
		AdapterSync:       adaptersync.New(ad, db),
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
type failingAdapter struct{ calls int }

func (a *failingAdapter) List(ctx context.Context) ([]schemas.WorkItem, error) {
	a.calls++
	return nil, fmt.Errorf("synthetic adapter failure %d", a.calls)
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
	o := New(Config{
		AdapterSync:       adaptersync.New(ad, db),
		BriefLoader:       noopBriefLoader{},
		DB:                db,
		Scheduler:         scheduler.New(db, scheduler.Config{LockTTL: time.Minute}),
		Spawner:           spawner.NewStub(),
		DBPath:            dbPath,
		PollInterval:      10 * time.Millisecond,
		TickInterval:      10 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
		LockTTL:           time.Minute,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- o.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v for failing adapter; want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
	if ad.calls < 2 {
		t.Fatalf("expected adapter polled ≥2 times despite failures, got %d", ad.calls)
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

// TestOrchestratorRecordsEvents pins the audit-trail contract: every
// spawn and every crash-recovery requeue lands a typed event row so
// the audit sink writer (future LessonCapture) has something to walk.
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
	if kinds["spawned"] != 1 {
		t.Errorf("expected 1 spawned event, got %d (all: %v)", kinds["spawned"], kinds)
	}
	if kinds["recovered_crashed"] != 1 {
		t.Errorf("expected 1 recovered_crashed event, got %d (all: %v)", kinds["recovered_crashed"], kinds)
	}
}

// TestPollPropagatesLaneChange pins the orchestrator end-to-end on the
// lane-drift bug in state.UpsertPending: a markdown item whose `lane:`
// frontmatter is rewritten must be reflected on the existing agent row
// by the next PollOnce, not stuck in its original lane (which would
// defeat per-lane caps).
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
	h := &captureHandler{}
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

	if _, ok := h.findEvent(obs.EventTickStarted); !ok {
		t.Fatalf("expected event %q in captured records; got %d records", obs.EventTickStarted, len(h.Records()))
	}
	completed, ok := h.findEvent(obs.EventTickCompleted)
	if !ok {
		t.Fatalf("expected event %q in captured records; got %d records", obs.EventTickCompleted, len(h.Records()))
	}
	if _, ok := recordHasAttr(completed, string(obs.KeyWorkItemsEvaluated)); !ok {
		t.Fatalf("tick.completed missing attr %q", obs.KeyWorkItemsEvaluated)
	}
}

// TestOrchestrator_Tick_EmitsOnEmptyQueue pins spec §3.3.
func TestOrchestrator_Tick_EmitsOnEmptyQueue(t *testing.T) {
	ctx := context.Background()
	o, _, _, _ := newHarness(t, 0)
	h := &captureHandler{}
	o.log = slog.New(h)

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if _, ok := h.findEvent(obs.EventTickStarted); !ok {
		t.Fatalf("tick.started missing on empty-queue tick")
	}
	completed, ok := h.findEvent(obs.EventTickCompleted)
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
