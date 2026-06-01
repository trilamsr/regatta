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

// TestSubstrate_SupersedesCycleProperty pins spec §10 #3 / §9 A-tier: random acyclic chains succeed; planted self-loop ⇒ ErrSupersedesCycle.
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
