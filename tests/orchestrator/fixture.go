//go:build unix

// Package orchestrator is the cross-package integration harness for the
// regatta orchestrator (MAY-49). It wires real spawner+scheduler+state
// plus a shared in-memory PRLister so cross-package tests that today
// duplicate per-package fakes (spawner.ProcessStarter, prwatch.Runner,
// checks.ghCLI, merge.Prober) can boot one harness call.
//
// The harness is intentionally small per feedback_default_simpler:
// it covers spawner+scheduler+state today; dashboard / webhook / per-
// package seam unification are explicitly OUT OF SCOPE (file follow-up
// when a second cross-package test class lands).
package orchestrator

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/prwatch"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Harness bundles the real seams a cross-package integration test
// drives: state.DB, spawner.Stub (records every Spawn call), the
// real scheduler.Scheduler, and the orchestrator that wires them.
// Lister is the shared in-memory PRLister callers can pre-seed so a
// fake gh response is observable across packages.
type Harness struct {
	DB           *state.DB
	Spawner      *spawner.Stub
	Scheduler    *scheduler.Scheduler
	Orchestrator *orchestrator.Orchestrator
	Lister       *FakeLister

	// AdapterRoot is the temp dir the markdown catalog reads from;
	// tests write fixture items under <AdapterRoot>/.regatta/items/.
	AdapterRoot string
	// DBPath is the on-disk sqlite path (under t.TempDir()).
	DBPath string
}

// Option mutates the harness construction defaults. The set is kept
// deliberately small — add new knobs only when a second caller needs
// them per feedback_default_simpler.
type Option func(*options)

type options struct {
	pollInterval      time.Duration
	tickInterval      time.Duration
	heartbeatInterval time.Duration
	lockTTL           time.Duration
}

// WithPollInterval pins the orchestrator's PollInterval. Default is
// time.Second (fast enough for in-test tick loops, slow enough that
// tests not exercising Run see no extra activity).
func WithPollInterval(d time.Duration) Option {
	return func(o *options) { o.pollInterval = d }
}

// WithTickInterval pins the orchestrator's TickInterval.
func WithTickInterval(d time.Duration) Option {
	return func(o *options) { o.tickInterval = d }
}

// BootOrchestrator wires a Harness with real spawner+scheduler+state
// and a shared in-memory PRLister. t.Cleanup closes the DB so callers
// do not manage lifecycle.
func BootOrchestrator(t *testing.T, opts ...Option) *Harness {
	t.Helper()
	o := options{
		pollInterval:      time.Second,
		tickInterval:      time.Second,
		heartbeatInterval: time.Second,
		lockTTL:           time.Minute,
	}
	for _, opt := range opts {
		opt(&o)
	}

	adapterRoot := t.TempDir()
	// MinPoll=1ns matches internal/orchestrator/orchestrator_test.go
	// newHarness: the adaptersync MinPoll gate (#888) is verified at
	// the unit level, not here.
	ad, err := adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{
		Root:    adapterRoot,
		MinPoll: time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("BootOrchestrator: adapter: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("BootOrchestrator: state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	syncer, err := adaptersync.New(adaptersync.Config{Adapter: ad, DB: db})
	if err != nil {
		t.Fatalf("BootOrchestrator: adaptersync.New: %v", err)
	}

	sp := spawner.New(spawner.Config{DB: db})
	sched := scheduler.New(db, scheduler.Config{LockTTL: o.lockTTL})
	lister := &FakeLister{}

	orch := orchestrator.New(orchestrator.Config{
		AdapterSync:       syncer,
		BriefLoader:       noopBriefLoader{},
		DB:                db,
		Scheduler:         sched,
		Spawner:           sp,
		DBPath:            dbPath,
		PollInterval:      o.pollInterval,
		TickInterval:      o.tickInterval,
		HeartbeatInterval: o.heartbeatInterval,
		LockTTL:           o.lockTTL,
	})

	return &Harness{
		DB:           db,
		Spawner:      sp,
		Scheduler:    sched,
		Orchestrator: orch,
		Lister:       lister,
		AdapterRoot:  adapterRoot,
		DBPath:       dbPath,
	}
}

// FakeLister is the shared in-memory prwatch.PRLister. Tests seed
// SetPRs(branch, prs) so a scheduler or prwatch consumer observes the
// same gh response regardless of which package drives the call.
type FakeLister struct {
	mu       sync.Mutex
	byBranch map[string][]prwatch.PullRequest
	err      error
	calls    int
}

// SetPRs registers the PRs returned for a given branch. Subsequent
// calls overwrite the prior set.
func (f *FakeLister) SetPRs(branch string, prs []prwatch.PullRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byBranch == nil {
		f.byBranch = map[string][]prwatch.PullRequest{}
	}
	f.byBranch[branch] = prs
}

// SetErr makes subsequent ListOpenByHead calls return err.
func (f *FakeLister) SetErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Calls reports the cumulative ListOpenByHead call count.
func (f *FakeLister) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ListOpenByHead implements prwatch.PRLister.
func (f *FakeLister) ListOpenByHead(_ context.Context, branch, _ string) ([]prwatch.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return append([]prwatch.PullRequest(nil), f.byBranch[branch]...), nil
}

// noopBriefLoader is the zero-side-effect BriefLoader stub the harness
// wires by default; tests exercising brief ingestion swap in a live
// program.BriefLoader via direct orchestrator.Config wiring.
type noopBriefLoader struct{}

func (noopBriefLoader) Sync(context.Context, time.Time) error { return nil }
