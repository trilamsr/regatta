package approval

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// errReaperCrashSim is the package-private fault-injection sentinel surfaced through the reaper's txHook.
var errReaperCrashSim = errors.New("reaper-crash-sim")

// reaperCrashHarness drives baseline vs crash-and-recover Sweeps over the same DB.
type reaperCrashHarness struct {
	seedIDs []string
	t0      time.Time
	policy  string // "" → policyFail; varied by the property runner.
}

func (h *reaperCrashHarness) effectivePolicy() string {
	if h.policy == "" {
		return policyFail
	}
	return h.policy
}

func (h *reaperCrashHarness) seed(t *testing.T, db *state.DB) {
	t.Helper()
	for _, id := range h.seedIDs {
		seedWorkItem(t, db, state.WorkItem{
			ID: "wi-" + id, Kind: state.KindFeature, Title: id,
			Lane: "server", Status: state.WorkStatusPlanned,
		}, h.t0)
		seedApproval(t, db, id, "wi-"+id,
			h.t0.Add(-time.Hour), h.t0.Add(-time.Minute),
			h.effectivePolicy(), nil)
	}
}

// reaperSnapshot is the reducer-folded view: per-approval (status, event kinds in id-ASC order).
type reaperSnapshot struct {
	Status map[string]string
	Kinds  map[string][]string
}

func snapshotReaper(t *testing.T, db *state.DB, ids []string) reaperSnapshot {
	t.Helper()
	snap := reaperSnapshot{
		Status: map[string]string{},
		Kinds:  map[string][]string{},
	}
	ctx := context.Background()
	for _, id := range ids {
		a, err := db.GetApproval(ctx, id)
		if err != nil {
			t.Fatalf("snapshot get %s: %v", id, err)
		}
		snap.Status[id] = a.Status
		evs, err := db.ListApprovalEvents(ctx, id)
		if err != nil {
			t.Fatalf("snapshot events %s: %v", id, err)
		}
		kinds := make([]string, 0, len(evs))
		for _, e := range evs {
			kinds = append(kinds, e.Kind)
		}
		snap.Kinds[id] = kinds
	}
	return snap
}

func diffReaperSnapshots(want, got reaperSnapshot) string {
	var diffs []string
	for id, ws := range want.Status {
		gs, ok := got.Status[id]
		if !ok {
			diffs = append(diffs, "approval["+id+"] missing post-recover")
			continue
		}
		if ws != gs {
			diffs = append(diffs, "approval["+id+"] status want="+ws+" got="+gs)
		}
	}
	for id := range got.Status {
		if _, ok := want.Status[id]; !ok {
			diffs = append(diffs, "approval["+id+"] phantom post-recover")
		}
	}
	for id, wk := range want.Kinds {
		gk, ok := got.Kinds[id]
		if !ok {
			continue
		}
		if !sliceEq(wk, gk) {
			diffs = append(diffs, "approval["+id+"] kinds want=["+strings.Join(wk, ",")+"] got=["+strings.Join(gk, ",")+"]")
		}
	}
	if len(diffs) == 0 {
		return ""
	}
	return strings.Join(diffs, "; ")
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runBaseline sweeps all expired approvals once with no hook.
func (h *reaperCrashHarness) runBaseline(t *testing.T) reaperSnapshot {
	t.Helper()
	db := reaperCrashDB(t, h.t0)
	h.seed(t, db)
	r, err := NewReaper(db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), func() time.Time { return h.t0 })
	if err != nil {
		t.Fatalf("baseline NewReaper: %v", err)
	}
	if err := r.Sweep(context.Background()); err != nil {
		t.Fatalf("baseline Sweep: %v", err)
	}
	return snapshotReaper(t, db, h.seedIDs)
}

// runCrashAndRecover crashes the k-th sweepOne tx then runs a fresh Reaper.
func (h *reaperCrashHarness) runCrashAndRecover(t *testing.T, k int) reaperSnapshot {
	t.Helper()
	db := reaperCrashDB(t, h.t0)
	h.seed(t, db)
	crash, err := NewReaper(db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), func() time.Time { return h.t0 })
	if err != nil {
		t.Fatalf("crash NewReaper: %v", err)
	}
	i := 0
	crash.txHook = func(_ *sql.Tx) error {
		defer func() { i++ }()
		if i == k {
			return errReaperCrashSim
		}
		return nil
	}
	// Sweep returns the first per-row error; the property is that the
	// next Sweep converges regardless, so we ignore it here.
	_ = crash.Sweep(context.Background())

	recovered, err := NewReaper(db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), func() time.Time { return h.t0 })
	if err != nil {
		t.Fatalf("recovered NewReaper: %v", err)
	}
	if err := recovered.Sweep(context.Background()); err != nil {
		t.Fatalf("recovered Sweep: %v", err)
	}
	return snapshotReaper(t, db, h.seedIDs)
}

// reaperCrashDB clones the golden post-migration sqlite file — ~0.5ms per case vs ~6ms with goose.
func reaperCrashDB(t *testing.T, t0 time.Time) *state.DB {
	t.Helper()
	return statetest.GoldenClone(t, func() time.Time { return t0 })
}

// TestReaperCrashRecovery_DetectsForcedDivergence pins the diff harness — recovery-skip must fire.
func TestReaperCrashRecovery_DetectsForcedDivergence(t *testing.T) {
	h := &reaperCrashHarness{
		seedIDs: []string{"a-A", "a-B"},
		t0:      time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC),
	}
	baseline := h.runBaseline(t)
	forced := reaperSnapshot{
		Status: map[string]string{"a-A": state.ApprovalStatusPending, "a-B": state.ApprovalStatusPending},
		Kinds:  map[string][]string{"a-A": nil, "a-B": nil},
	}
	if d := diffReaperSnapshots(baseline, forced); d == "" {
		t.Fatalf("diffReaperSnapshots did not flag forced divergence — runner is blind to recovery skips")
	} else if !strings.Contains(d, "a-A") || !strings.Contains(d, "a-B") {
		t.Fatalf("forced-divergence diff missing expected approval labels: %s", d)
	}
}

// TestReaperCrashRecovery_BaselineMatchesRecover pins happy path — crash at first sweepOne tx then recover.
func TestReaperCrashRecovery_BaselineMatchesRecover(t *testing.T) {
	h := &reaperCrashHarness{
		seedIDs: []string{"a-X"},
		t0:      time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC),
	}
	baseline := h.runBaseline(t)
	recovered := h.runCrashAndRecover(t, 0)
	if d := diffReaperSnapshots(baseline, recovered); d != "" {
		t.Fatalf("baseline ≠ recovered for crash-at-first-row: %s", d)
	}
	if recovered.Status["a-X"] != state.ApprovalStatusTimedOut {
		t.Fatalf("a-X post-recover want %q got %q", state.ApprovalStatusTimedOut, recovered.Status["a-X"])
	}
}

// TestReaperCrashRecovery_CatchesMissingRecoverySweep pins TDD invariant — diff catches no-op recovery.
func TestReaperCrashRecovery_CatchesMissingRecoverySweep(t *testing.T) {
	h := &reaperCrashHarness{
		seedIDs: []string{"a-X"},
		t0:      time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC),
	}
	baseline := h.runBaseline(t)

	db := reaperCrashDB(t, h.t0)
	h.seed(t, db)
	crash, err := NewReaper(db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), func() time.Time { return h.t0 })
	if err != nil {
		t.Fatalf("crash NewReaper: %v", err)
	}
	crash.txHook = func(_ *sql.Tx) error { return errReaperCrashSim }
	_ = crash.Sweep(context.Background())
	buggy := snapshotReaper(t, db, h.seedIDs)
	if d := diffReaperSnapshots(baseline, buggy); d == "" {
		t.Fatalf("diff did not catch missing recovery sweep — runner blind to the canonical bug")
	}
}
