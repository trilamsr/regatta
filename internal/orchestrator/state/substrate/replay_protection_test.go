package substrate_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_ReplayProtectionProperty pins spec §5 / §10 #11: the
// UNIQUE(run_id, written_by, nonce) index makes any second event with
// an identical triple unwriteable.
//
// Strategy: pick a triple, write the first event, write a second event
// with the same (run_id, written_by, nonce) but a fresh ID + payload
// shape — assert ErrReplay. Quantify over the nonce + the WrittenBy
// principal to span the collision surface.
//
// Reproducibility: rapid seeds from -rapid.seed (default deterministic
// per test name). Each iteration uses a fresh run_id so events from
// different iterations cannot collide.
func TestSubstrate_ReplayProtectionProperty(t *testing.T) {
	db := openMigratedDB(t)
	var tag atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		ctx := context.Background()
		runID := fmt.Sprintf("run-rep-%07d", tag.Add(1))
		// rapid-drawn writer principal — alphanumeric + underscore only
		// (the substrate schema CHECK on written_by forbids most
		// punctuation).
		writer := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{2,15}`).Draw(rt, "writer")
		nonceSeed := byte(rapid.IntRange(1, 254).Draw(rt, "nonce_seed")) //nolint:gosec // G115: rapid range bounds the value to [1,254] which fits in byte by construction
		base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC).
			Add(time.Duration(tag.Load()) * 100 * time.Millisecond)

		first := mkEvent(nonceSeed, runID, substrate.KindHeartbeat,
			`{"work_item_id":"WI-R","timestamp":1}`, base)
		first.WrittenBy = writer
		if err := appendInTx(ctx, db, first); err != nil {
			rt.Fatalf("first append: %v", err)
		}

		// Second event: fresh payload + advanced WrittenAt; identical
		// (run_id, written_by, nonce) triple ⇒ ErrReplay.
		second := mkEvent(nonceSeed, runID, substrate.KindHeartbeat,
			`{"work_item_id":"WI-R","timestamp":2}`, base.Add(time.Millisecond))
		second.WrittenBy = writer
		err := appendInTx(ctx, db, second)
		if !errors.Is(err, substrate.ErrReplay) {
			rt.Fatalf("second append: err=%v want ErrReplay (run=%s writer=%s nonceSeed=%d)",
				err, runID, writer, nonceSeed)
		}
	})
}

// appendInTx opens a tx, calls AppendEvent, and commits (rolls back on
// error). Mirrors helpers_test.go's appendEventTx but takes no testing.TB
// so it can be invoked from rapid-driven closures.
func appendInTx(ctx context.Context, db *sql.DB, e substrate.Event) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := substrate.AppendEvent(ctx, tx, e, testKey, testKeyID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
