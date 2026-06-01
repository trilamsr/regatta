package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// discardLogger silences scheduler slog output during benches so
// formatter cost does not dominate ns/op for evalPendingEdges
// scenarios (one Info per edge × N from_ids).
var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// newBenchDB opens a fresh sqlite-backed DB for the lifetime of a sub-
// benchmark. Each sub-bench gets its own temp file to keep state
// isolated; reusing a DB across sub-benches would amortise UpsertPending
// cost into the wrong N bucket.
func newBenchDB(b *testing.B) *state.DB {
	b.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(b.TempDir(), "s.db")))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

func seedPlannedBench(b *testing.B, db *state.DB, id, lane string) {
	b.Helper()
	w := state.WorkItem{
		ID: id, Kind: state.KindFeature, Title: id,
		Lane: lane, Status: state.WorkStatusPlanned,
	}
	if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, time.Unix(1_700_000_000, 0)); err != nil {
		b.Fatalf("seed %s: %v", id, err)
	}
}

// BenchmarkTick measures the cost of a single scheduler.Tick over a
// pool of independent planned work items (no deps, no edges). At N=1000
// this exercises ListSpawnable + N×UpsertPending + N×TryAcquireLocks +
// N×TransitionAgent for the first tick, then steady-state ListSpawnable
// scans for subsequent ticks — the operator-visible Tick budget.
func BenchmarkTick(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			db := newBenchDB(b)
			for i := 0; i < n; i++ {
				seedPlannedBench(b, db, fmt.Sprintf("F-%05d", i), "server")
			}
			sch := New(db, Config{LockTTL: time.Minute})
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := sch.Tick(ctx); err != nil {
					b.Fatalf("Tick: %v", err)
				}
			}
		})
	}
}

// BenchmarkTickEvalEdges fixture and background.
//
// Why this bench exists: PR closing #98 swapped a tick-local
// `nonDefaultAllFalse` accumulator for a post-loop ListEdgesFrom
// re-read per pending-edge group (commit 7dbcbab). The pre-existing
// BenchmarkTick exercises no-edge work items only, so the regression
// on the multi-sibling-with-default shape was invisible. This bench
// fans out N merged from_ids each with 2 non-default predicated
// edges + 1 default + journal row, driving the fallback-fires branch
// on every group so the new ListEdgesFrom call stays on the measured
// path. Per-tick cost scales O(N) in the merged-fanout count.
//
// Tick-1 vs steady state: bench measures total Tick latency, not
// just evalPendingEdges, because operators care about the full
// poll-cycle budget. After tick 1 every edge is settled and the
// per-tick ListPendingEdgesFromMerged returns empty — that is the
// steady-state cost b.N - 1 of the runs measure. Tick 1 amortises
// across b.N, which understates the regression for small b.N but
// matches what a long-running orchestrator sees in practice.
//
// Baseline: pre-#98 commit 66816c9 on Apple M1 Max at
// `-benchtime=5x -count=10`. If a future scheduler change pushes the
// alloc-proxy regression past 5% vs that baseline, switch to the
// CountNonDefaultEdgeStates aggregate query (the H2 alternative
// tracked in #187) per issue #119's decision rule.

// benchUnconditionalEvaluator marks every predicated edge fired=false
// so the default-fallback branch in evalPendingEdges fires on the same
// tick — keeps the post-loop sibling re-read on the measured path.
type benchUnconditionalEvaluator struct{}

func (benchUnconditionalEvaluator) Eval(_ context.Context, _ state.EdgeRow, _ any, _ state.OutputJournalEntry) (bool, string, error) {
	return false, "bench-false", nil
}

// seedFanoutWithDefault writes n merged from_ids, each with 2
// non-default sibling edges + 1 default. Every from_id has a journal
// row so evalPendingEdges does not short-circuit on ErrJournalNotFound.
// Targets (T-*) are planned features so ListSpawnable surfaces them
// but the reservation loop pays a flat per-target cost separate from
// the edge-eval cost this fixture stresses.
func seedFanoutWithDefault(b *testing.B, db *state.DB, n int) {
	b.Helper()
	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0)

	// Plan targets first so the foreign-key disjunct in
	// ListPendingEdgesFromMerged sees real rows.
	for i := 0; i < n; i++ {
		from := fmt.Sprintf("M-%05d", i)
		fromW := state.WorkItem{
			ID: from, Kind: state.KindFeature, Title: from,
			Lane: "server", Status: state.WorkStatusMerged,
		}
		if err := db.UpsertWorkItem(ctx, fromW, state.SourceBrief, at); err != nil {
			b.Fatalf("seed merged %s: %v", from, err)
		}
		for _, suf := range []string{"A", "B", "D"} {
			id := fmt.Sprintf("T-%05d-%s", i, suf)
			seedPlannedBench(b, db, id, "server")
		}
	}

	var rows []state.EdgeRow
	for i := 0; i < n; i++ {
		from := fmt.Sprintf("M-%05d", i)
		rows = append(rows,
			state.EdgeRow{ProgramID: "m-bench", FromID: from,
				ToID: fmt.Sprintf("T-%05d-A", i), PredicateCEL: `false`},
			state.EdgeRow{ProgramID: "m-bench", FromID: from,
				ToID: fmt.Sprintf("T-%05d-B", i), PredicateCEL: `false`},
			state.EdgeRow{ProgramID: "m-bench", FromID: from,
				ToID: fmt.Sprintf("T-%05d-D", i), IsDefault: true},
		)
	}
	if err := db.UpsertEdgesAt(ctx, "m-bench", rows, at); err != nil {
		b.Fatalf("UpsertEdges: %v", err)
	}
	for i := 0; i < n; i++ {
		if _, err := db.AppendOutputAt(ctx, fmt.Sprintf("M-%05d", i),
			json.RawMessage(`{}`), at); err != nil {
			b.Fatalf("AppendOutput %d: %v", i, err)
		}
	}
}

// BenchmarkTickEvalEdges times the #98 evalPendingEdges hot path; see the block comment above benchUnconditionalEvaluator for fixture, Tick-1 amortisation framing, and baseline.
func BenchmarkTickEvalEdges(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			db := newBenchDB(b)
			seedFanoutWithDefault(b, db, n)
			sch := New(db, Config{
				LockTTL:   time.Minute,
				Evaluator: benchUnconditionalEvaluator{},
				Logger:    discardLogger,
			})
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := sch.Tick(ctx); err != nil {
					b.Fatalf("Tick: %v", err)
				}
			}
		})
	}
}
