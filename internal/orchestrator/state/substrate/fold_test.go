package substrate_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

// TestSubstrate_FoldPropagatesScanErrorMidIteration pins that a Scan failure on a mid-loop row aborts the fold with nil slice + wrapped error (W-COV4).
func TestSubstrate_FoldPropagatesScanErrorMidIteration(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Two clean rows precede the poisoned row; sqlite's non-STRICT
	// INTEGER affinity accepts a TEXT literal into schema_version, so
	// rows.Scan into *int64 fails on that row alone. The poisoned row
	// carries a written_at between the clean rows so Fold's ORDER BY
	// (written_at, id) places the failure mid-iteration.
	clean1 := mkEvent(0xe1, "run-P", substrate.KindHeartbeat,
		`{"work_item_id":"WI-P","timestamp":1}`, now)
	clean2 := mkEvent(0xe2, "run-P", substrate.KindHeartbeat,
		`{"work_item_id":"WI-P","timestamp":2}`, now.Add(2*time.Second))
	if err := appendEventTx(ctx, t, db, clean1); err != nil {
		t.Fatalf("clean1: %v", err)
	}
	if err := appendEventTx(ctx, t, db, clean2); err != nil {
		t.Fatalf("clean2: %v", err)
	}

	poisonID := substrate.Mint(now.Add(time.Second))
	_, err := db.ExecContext(ctx,
		`INSERT INTO substrate_events
		   (id, run_id, work_item_id, tenant_id, trace_id, span_id,
		    kind, key, payload_json, blob_digest, supersedes,
		    written_by, written_at, schema_version, nonce,
		    sig_alg, sig_key_id, sig_mac)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		poisonID, "run-P", nil, substrate.DefaultTenantID, "", "",
		string(substrate.KindHeartbeat), "",
		`{"work_item_id":"WI-P","timestamp":1}`, "", nil,
		"tester", now.Add(time.Second).UnixMilli(), "not_an_int",
		"aa112233445566778899aabbccddeeff",
		"HMAC-SHA256", testKeyID,
		"0000000000000000000000000000000000000000000000000000000000000000",
	)
	if err != nil {
		t.Fatalf("poison insert: %v", err)
	}

	out, err := substrate.Fold(ctx, db, "run-P", substrate.KindHeartbeat)
	if err == nil {
		t.Fatalf("Fold: err=nil want scan error; out=%v", out)
	}
	if out != nil {
		t.Fatalf("Fold: partial slice leaked on scan error: %v", out)
	}
	if !strings.Contains(err.Error(), "fold scan") {
		t.Fatalf("Fold: err=%v want wrapped %q prefix", err, "substrate: fold scan")
	}
}

// TestSubstrate_FoldPropagatesQueryError pins that a QueryContext failure returns before defer registration without panic (W-COV4).
func TestSubstrate_FoldPropagatesQueryError(t *testing.T) {
	db := openMigratedDB(t)
	// Closing the DB before the call forces QueryContext to fail at
	// line 23; the defer on line 36 never registers because rows is nil.
	// This asserts that failure path is panic-free and error-wrapped.
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	out, err := substrate.Fold(context.Background(), db, "run-Q", substrate.KindHeartbeat)
	if err == nil {
		t.Fatalf("Fold on closed DB: err=nil want query error; out=%v", out)
	}
	if out != nil {
		t.Fatalf("Fold on closed DB: out=%v want nil", out)
	}
	if !strings.Contains(err.Error(), "fold query") {
		t.Fatalf("Fold on closed DB: err=%v want wrapped %q prefix", err, "substrate: fold query")
	}
	if !errors.Is(err, sql.ErrConnDone) && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Fold on closed DB: err=%v want ErrConnDone or closed-DB error", err)
	}
}
