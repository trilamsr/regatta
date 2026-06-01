package substrate_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_SupersedesCycleProperty pins spec §10 #3 / §9 A-tier:
// across synthetic supersedes graphs of varying shape, the cycle-check
// contract holds — acyclic insertions succeed; a planted self-loop is
// rejected with ErrSupersedesCycle.
//
// Strategy: rapid.Check enumerates a chain of N events whose
// `supersedes` pointer references any strictly-earlier event in the
// chain. Insertion in topological order satisfies the FK constraint
// and the graph stays acyclic. After the acyclic phase, plant a final
// self-loop and assert it is rejected by the Kahn's-sort path inside
// AppendEvent (not by the FK).
//
// Multi-node back-edges aren't constructible through AppendEvent alone
// because the FK (`supersedes REFERENCES substrate_events(id)`) forbids
// referencing a row that hasn't been inserted yet. The self-loop is the
// minimum-cycle adversarial form exercised here; T-S1's existing
// SupersedesCycleRejected test pins the same shape but on a single
// fixed-shape graph. The property test adds graph-shape diversity.
//
// Determinism: rapid seeds from the -rapid.seed flag (default value
// reproducible per test name); CI reproducibility comes for free. Each
// iteration uses a unique run_id (atomic counter) so (run_id,
// written_by, nonce) collisions don't leak between iterations.
func TestSubstrate_SupersedesCycleProperty(t *testing.T) {
	db := openMigratedDB(t)
	var tag atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		ctx := context.Background()
		runID := fmt.Sprintf("run-prop-%07d", tag.Add(1))
		n := rapid.IntRange(2, 6).Draw(rt, "n")
		base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC).
			Add(time.Duration(tag.Load()) * 100 * time.Millisecond)

		ids := make([]string, 0, n)
		for i := 0; i < n; i++ {
			seedByte := byte(i + 1)
			e := mkEvent(seedByte, runID, substrate.KindHeartbeat,
				fmt.Sprintf(`{"work_item_id":"WI-P","timestamp":%d}`, i+1),
				base.Add(time.Duration(i)*time.Millisecond))
			if i > 0 {
				parent := rapid.IntRange(0, i-1).Draw(rt, fmt.Sprintf("sup-%d", i))
				e.Supersedes = ids[parent]
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				rt.Fatalf("BeginTx (acyclic %d): %v", i, err)
			}
			if err := substrate.AppendEvent(ctx, tx, e, testKey, testKeyID); err != nil {
				_ = tx.Rollback()
				rt.Fatalf("AppendEvent (acyclic %d): %v", i, err)
			}
			if err := tx.Commit(); err != nil {
				rt.Fatalf("Commit (acyclic %d): %v", i, err)
			}
			ids = append(ids, e.ID)
		}

		// Planted self-loop on a fresh event must trip Kahn's sort.
		self := mkEvent(0xFE, runID, substrate.KindHeartbeat,
			`{"work_item_id":"WI-P","timestamp":999}`,
			base.Add(time.Duration(n+1)*time.Millisecond))
		self.Supersedes = self.ID
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			rt.Fatalf("BeginTx (self-loop): %v", err)
		}
		err = substrate.AppendEvent(ctx, tx, self, testKey, testKeyID)
		_ = tx.Rollback()
		if !errors.Is(err, substrate.ErrSupersedesCycle) {
			rt.Fatalf("self-loop accepted: err=%v want ErrSupersedesCycle", err)
		}
	})
}
