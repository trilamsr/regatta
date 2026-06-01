package substrate_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_ClockRegressionRejected pins WrittenAt < watermark ⇒ ErrClockRegression per spec §8 I2.
func TestSubstrate_ClockRegressionRejected(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	first := mkEvent(0xc1, "run-K", substrate.KindHeartbeat,
		`{"work_item_id":"WI-K","timestamp":1}`, now)
	if err := appendEventTx(ctx, t, db, first); err != nil {
		t.Fatalf("first: %v", err)
	}

	// Same timestamp is monotonic-OK (>=). Earlier timestamp must fail.
	regressed := mkEvent(0xc2, "run-K", substrate.KindHeartbeat,
		`{"work_item_id":"WI-K","timestamp":2}`, now.Add(-time.Second))
	err := appendEventTx(ctx, t, db, regressed)
	if !errors.Is(err, substrate.ErrClockRegression) {
		t.Fatalf("regressed: err=%v want ErrClockRegression", err)
	}
}

// TestSubstrate_ConcurrentFoldReadSnapshot pins WAL snapshot isolation: concurrent readers never see torn rows during appends per spec §10 #2.
func TestSubstrate_ConcurrentFoldReadSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	errCh := make(chan error, 4)

	reader := func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			events, err := substrate.Fold(ctx, db, "run-S", substrate.KindHeartbeat)
			if err != nil {
				errCh <- err
				return
			}
			for _, e := range events {
				if e.ID == "" || e.RunID != "run-S" || e.Kind != substrate.KindHeartbeat {
					errCh <- errors.New("fold returned torn row")
					return
				}
			}
		}
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go reader()
	}

	for i := 0; i < 50; i++ {
		e := mkEvent(byte(0x40+i), "run-S", substrate.KindHeartbeat,
			`{"work_item_id":"WI-S","timestamp":1}`,
			now.Add(time.Duration(i)*time.Millisecond))
		if err := appendEventTx(ctx, t, db, e); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("append %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("reader: %v", err)
	}

	// Final state: 50 events all readable.
	events, err := substrate.Fold(ctx, db, "run-S", substrate.KindHeartbeat)
	if err != nil {
		t.Fatalf("final fold: %v", err)
	}
	if len(events) != 50 {
		t.Fatalf("final count: %d want 50", len(events))
	}
}

// TestSubstrate_FoldOrdersByWrittenAtThenID pins (written_at, id) tiebreaker for fold ordering per spec §10 #16.
func TestSubstrate_FoldOrdersByWrittenAtThenID(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Force two events to share written_at by using the same time.
	// ULID monotonicity within a process ensures distinct IDs.
	a := mkEvent(0xd1, "run-O", substrate.KindHeartbeat,
		`{"work_item_id":"WI-O","timestamp":1}`, now)
	b := mkEvent(0xd2, "run-O", substrate.KindHeartbeat,
		`{"work_item_id":"WI-O","timestamp":2}`, now)
	if err := appendEventTx(ctx, t, db, a); err != nil {
		t.Fatalf("a: %v", err)
	}
	if err := appendEventTx(ctx, t, db, b); err != nil {
		t.Fatalf("b: %v", err)
	}

	events, err := substrate.Fold(ctx, db, "run-O", substrate.KindHeartbeat)
	if err != nil {
		t.Fatalf("Fold: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("fold len=%d want 2", len(events))
	}
	// ULID ordering: a was minted before b (Mint is monotonic), so
	// a.ID < b.ID lex. Fold orders ASC, so events[0].ID == a.ID.
	if events[0].ID != a.ID || events[1].ID != b.ID {
		t.Fatalf("fold order: got [%s, %s] want [%s, %s]",
			events[0].ID, events[1].ID, a.ID, b.ID)
	}
}
