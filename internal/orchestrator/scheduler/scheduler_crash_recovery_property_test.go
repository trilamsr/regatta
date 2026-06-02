package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// errCrashSim is the package-private fault-injection sentinel surfaced by WriteHook (spec §3.3 R6).
var errCrashSim = errors.New("crash-sim")

// stateSnapshot is the reducer-folded view this property compares across baseline and recovered runs.
//
// Agent rowids are intentionally erased — recovery may re-issue a row
// for any wi that was mid-reserve at crash; the property cares that the
// (state, lane, wi) tuple converges, not that integer ids match.
type stateSnapshot struct {
	Agents    map[string]agentSummary // wi → (state, lane); max-state row per wi
	LocksByWI map[string]string       // lock_name → owning wi (derived from agent_id)
	WorkItems map[string]string       // id → status
}

type agentSummary struct {
	State state.AgentState
	Lane  string
}

func agentStateOrdinal(s state.AgentState) int {
	switch s {
	case state.AgentPending:
		return 1
	case state.AgentSpawning:
		return 2
	case state.AgentRunning:
		return 3
	default:
		return 4
	}
}

func snapshotState(t testing.TB, ctx context.Context, db *state.DB) stateSnapshot {
	t.Helper()
	snap := stateSnapshot{
		Agents:    map[string]agentSummary{},
		LocksByWI: map[string]string{},
		WorkItems: map[string]string{},
	}
	all := []state.AgentState{
		state.AgentPending, state.AgentSpawning, state.AgentRunning,
		state.AgentPROpen, state.AgentGatesRunning, state.AgentGatesFailed,
		state.AgentAwaitingMerge, state.AgentDone, state.AgentWithdrawn,
		state.AgentCrashed, state.AgentEscalated,
	}
	agents, err := db.ListAgentsByState(ctx, all...)
	if err != nil {
		t.Fatalf("snapshot: list agents: %v", err)
	}
	agentWI := map[int64]string{}
	for _, a := range agents {
		agentWI[a.ID] = a.WorkItemID
		prev, ok := snap.Agents[a.WorkItemID]
		if !ok || agentStateOrdinal(a.State) > agentStateOrdinal(prev.State) {
			snap.Agents[a.WorkItemID] = agentSummary{State: a.State, Lane: a.Lane}
		}
	}
	locks, err := db.ListLocks(ctx)
	if err != nil {
		t.Fatalf("snapshot: list locks: %v", err)
	}
	for _, l := range locks {
		snap.LocksByWI[l.Name] = agentWI[l.AgentID]
	}
	rows, err := db.SQL().QueryContext(ctx, `SELECT id, status FROM work_items`)
	if err != nil {
		t.Fatalf("snapshot: query work_items: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("snapshot: scan work_items: %v", err)
		}
		snap.WorkItems[id] = status
	}
	return snap
}

// diffSnapshots returns "" when snapshots are reducer-equivalent, else a labelled diff string.
func diffSnapshots(want, got stateSnapshot) string {
	var diffs []string
	for id, w := range want.Agents {
		g, ok := got.Agents[id]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("agent[%s] missing post-recover", id))
			continue
		}
		if w != g {
			diffs = append(diffs, fmt.Sprintf("agent[%s] want=%+v got=%+v", id, w, g))
		}
	}
	for id := range got.Agents {
		if _, ok := want.Agents[id]; !ok {
			diffs = append(diffs, fmt.Sprintf("agent[%s] phantom post-recover", id))
		}
	}
	for name, wid := range want.LocksByWI {
		gid, ok := got.LocksByWI[name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("lock[%s] missing post-recover", name))
			continue
		}
		if wid != gid {
			diffs = append(diffs, fmt.Sprintf("lock[%s] wi want=%q got=%q", name, wid, gid))
		}
	}
	for name := range got.LocksByWI {
		if _, ok := want.LocksByWI[name]; !ok {
			diffs = append(diffs, fmt.Sprintf("lock[%s] phantom post-recover", name))
		}
	}
	for id, w := range want.WorkItems {
		g, ok := got.WorkItems[id]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("work_item[%s] missing post-recover", id))
			continue
		}
		if w != g {
			diffs = append(diffs, fmt.Sprintf("work_item[%s] status want=%q got=%q", id, w, g))
		}
	}
	sort.Strings(diffs)
	if len(diffs) == 0 {
		return ""
	}
	return fmt.Sprintf("%d divergence(s): %v", len(diffs), diffs)
}

type crashHarness struct {
	seedIDs []string
	lane    string
	lockTTL time.Duration
	hotspot func(string) []string // nil → scheduler skips locks
}

// goldenDB builds a migrated sqlite file once per test, then copyGoldenDB
// returns a fresh path for each case — bypassing goose's ~6ms migration
// cost per open and dropping per-case wallclock from ~50ms to ~10ms (spec
// §3.4 per-case budget). Guarded by sync.Once so the migration runs once
// even though the property body invokes openCrashDB twice per case.
var (
	goldenOnce  sync.Once
	goldenPath  string
	goldenBytes []byte
	goldenErr   error
)

func ensureGolden(t testing.TB) {
	t.Helper()
	goldenOnce.Do(func() {
		f, err := os.CreateTemp("", "scheduler-crash-recovery-golden-*.db")
		if err != nil {
			goldenErr = fmt.Errorf("create golden: %w", err)
			return
		}
		goldenPath = f.Name()
		_ = f.Close()
		db, err := state.Open(context.Background(), state.DSN(goldenPath))
		if err != nil {
			goldenErr = fmt.Errorf("open golden: %w", err)
			return
		}
		// Disable WAL autocheckpoint at the golden so every clone inherits
		// the setting (spec R5).
		if _, err := db.SQL().ExecContext(context.Background(), `PRAGMA wal_autocheckpoint=0`); err != nil {
			goldenErr = fmt.Errorf("golden pragma: %w", err)
			return
		}
		// Force a WAL checkpoint then close so the on-disk file is
		// fully self-contained (no stale -wal sidecar pointing at the
		// closed conn).
		if _, err := db.SQL().ExecContext(context.Background(), `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			goldenErr = fmt.Errorf("golden checkpoint: %w", err)
			return
		}
		if err := db.Close(); err != nil {
			goldenErr = fmt.Errorf("close golden: %w", err)
			return
		}
		goldenBytes, goldenErr = os.ReadFile(goldenPath)
	})
	if goldenErr != nil {
		t.Fatalf("ensureGolden: %v", goldenErr)
	}
}

func openCrashDB(t testing.TB) *state.DB {
	t.Helper()
	ensureGolden(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	if err := os.WriteFile(path, goldenBytes, 0o600); err != nil {
		t.Fatalf("openCrashDB: clone golden: %v", err)
	}
	db, err := state.Open(context.Background(), state.DSN(path))
	if err != nil {
		t.Fatalf("openCrashDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func (h *crashHarness) seedQ(t testing.TB, db *state.DB) {
	t.Helper()
	ctx := context.Background()
	for _, id := range h.seedIDs {
		w := state.WorkItem{
			ID:     id,
			Kind:   state.KindFeature,
			Title:  id,
			Lane:   h.lane,
			Status: state.WorkStatusPlanned,
		}
		if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, time.Now()); err != nil {
			t.Fatalf("seedQ %s: %v", id, err)
		}
	}
}

// runBaseline runs n ticks with no hook and returns the final snapshot plus per-tick write counts.
func (h *crashHarness) runBaseline(t testing.TB, n int) (stateSnapshot, []int) {
	t.Helper()
	db := openCrashDB(t)
	h.seedQ(t, db)
	ctx := context.Background()
	sched := New(db, Config{LockTTL: h.lockTTL, Hotspots: h.hotspot})
	writes := make([]int, n)
	for i := 0; i < n; i++ {
		var calls int
		sched.WriteHook = func(idx int) error { calls = idx + 1; return nil }
		if _, err := sched.Tick(ctx); err != nil {
			t.Fatalf("baseline tick[%d]: %v", i, err)
		}
		writes[i] = calls
	}
	return snapshotState(t, ctx, db), writes
}

// runCrashAndRecover runs n ticks across three Schedulers — pre-crash, crash, recovered —
// against one shared DB. Returns the post-final-tick snapshot.
func (h *crashHarness) runCrashAndRecover(t testing.TB, n, crashTick, k int) stateSnapshot {
	t.Helper()
	db := openCrashDB(t)
	h.seedQ(t, db)
	ctx := context.Background()
	pre := New(db, Config{LockTTL: h.lockTTL, Hotspots: h.hotspot})
	for i := 0; i < crashTick; i++ {
		if _, err := pre.Tick(ctx); err != nil {
			t.Fatalf("pre-crash tick[%d]: %v", i, err)
		}
	}
	// Fresh scheduler for the crash tick — recovery semantics require
	// the crashed process is gone; only the substrate (sqlite) persists.
	crash := New(db, Config{LockTTL: h.lockTTL, Hotspots: h.hotspot})
	crash.WriteHook = func(idx int) error {
		if idx == k {
			return errCrashSim
		}
		return nil
	}
	if _, err := crash.Tick(ctx); err != nil && !errors.Is(err, errCrashSim) {
		t.Fatalf("crash tick: unexpected err=%v", err)
	}
	recovered := New(db, Config{LockTTL: h.lockTTL, Hotspots: h.hotspot})
	for i := crashTick + 1; i < n; i++ {
		if _, err := recovered.Tick(ctx); err != nil {
			t.Fatalf("post-crash tick[%d]: %v", i, err)
		}
	}
	return snapshotState(t, ctx, db)
}

// TestSchedulerCrashRecoveryProperty proves recover→tick(N+1) ≡ tick(N)→tick(N+1) for any crash-point.
func TestSchedulerCrashRecoveryProperty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		qSize := rapid.IntRange(1, 4).Draw(rt, "queue_size")
		ids := make([]string, qSize)
		for i := 0; i < qSize; i++ {
			ids[i] = fmt.Sprintf("WI-%d", i)
		}
		nTicks := rapid.IntRange(2, 10).Draw(rt, "n_ticks")
		crashTick := rapid.IntRange(0, nTicks-1).Draw(rt, "crash_tick")
		useHotspot := rapid.Bool().Draw(rt, "use_hotspot")
		h := &crashHarness{seedIDs: ids, lane: "server", lockTTL: time.Minute}
		if useHotspot {
			h.hotspot = func(wid string) []string { return []string{"lock-" + wid} }
		}

		baseline, writes := h.runBaseline(t, nTicks)
		if writes[crashTick] == 0 {
			rt.Skip("crash tick has no writes")
		}
		k := rapid.IntRange(0, writes[crashTick]-1).Draw(rt, "crash_writeIndex")

		recovered := h.runCrashAndRecover(t, nTicks, crashTick, k)
		if d := diffSnapshots(baseline, recovered); d != "" {
			rt.Fatalf("baseline ≠ recovered (nTicks=%d crashTick=%d k=%d ids=%v hotspot=%v): %s",
				nTicks, crashTick, k, ids, useHotspot, d)
		}

		// Sub-properties per spec §3.2 — independent of the diff so a
		// future state-store churn cannot silently break this test.
		for _, id := range ids {
			a, ok := recovered.Agents[id]
			if !ok {
				rt.Fatalf("P-NoLoss: wi %s has no agent post-recover", id)
			}
			if a.State == state.AgentDone || a.State == state.AgentWithdrawn {
				rt.Fatalf("P-NoLoss: wi %s in terminal state %s after crash-recover", id, a.State)
			}
		}
	})
}
