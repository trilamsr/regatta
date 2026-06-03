// Package history exports the DurableHistory interface that wraps the
// substrate event log per spec §3. Tests round-trip Append through
// substrate.Fold to prove the substrate-default impl writes the same
// rows the rest of the platform reads.
package history_test

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/history"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// testKey + testKeyID mirror the substrate test fixture so signature
// verify uses the same HMAC keyring across packages.
var testKey = []byte("0123456789abcdef0123456789abcdef")

const testKeyID = "test-key-1"

func openMigratedDB(t *testing.T) *sql.DB { return statetest.OpenMigratedRaw(t) }

func fixedNonce(seed byte) string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = seed
	}
	return hex.EncodeToString(b)
}

func nodeOutputEvent(seed byte, runID, workItemID string, at time.Time) substrate.Event {
	return substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runID,
		WorkItemID:    workItemID,
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindNodeOutput,
		PayloadJSON:   []byte(`{"work_item_id":"` + workItemID + `","attempt":1,"output":{"ok":true}}`),
		WrittenBy:     "test",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         fixedNonce(seed),
	}
}

// TestS2T1_AppendRoundTripsViaSubstrateFold proves the round-trip identity contract from §6 B2 — Append writes a row that substrate.Fold (the 
func TestS2T1_AppendRoundTripsViaSubstrateFold(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)

	h := history.NewSubstrate(db, testKey, testKeyID)

	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	runID := "run-A"
	ev := nodeOutputEvent(1, runID, "wi-A", at)

	if err := h.Append(ctx, runID, ev); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := substrate.Fold(ctx, db, runID, substrate.KindNodeOutput)
	if err != nil {
		t.Fatalf("Fold: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(folded) = %d, want 1", len(got))
	}
	if got[0].ID != ev.ID {
		t.Errorf("ID mismatch: got %q want %q", got[0].ID, ev.ID)
	}
	if got[0].WorkItemID != ev.WorkItemID {
		t.Errorf("WorkItemID mismatch: got %q want %q", got[0].WorkItemID, ev.WorkItemID)
	}
	if got[0].SigMAC == "" {
		t.Errorf("SigMAC empty — Append must sign via substrate")
	}
}

// TestS2T1_AppendRejectsRunIDMismatch — defence in depth: an Event whose RunID disagrees with the runID arg is a writer bug, not a silent over
func TestS2T1_AppendRejectsRunIDMismatch(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	h := history.NewSubstrate(db, testKey, testKeyID)

	at := time.Date(2026, 6, 1, 12, 0, 1, 0, time.UTC)
	ev := nodeOutputEvent(2, "run-A", "wi-B", at)
	// runID arg disagrees with ev.RunID.
	err := h.Append(ctx, "run-B", ev)
	if !errors.Is(err, history.ErrRunIDMismatch) {
		t.Fatalf("Append: got %v, want ErrRunIDMismatch", err)
	}
}

// TestS2T1_AppendPropagatesSubstrateValidation — bad payload must surface substrate.ErrInvalidPayload, not get swallowed.
func TestS2T1_AppendPropagatesSubstrateValidation(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	h := history.NewSubstrate(db, testKey, testKeyID)

	at := time.Date(2026, 6, 1, 12, 0, 2, 0, time.UTC)
	ev := nodeOutputEvent(3, "run-C", "wi-C", at)
	ev.PayloadJSON = []byte(`{}`) // missing work_item_id

	err := h.Append(ctx, "run-C", ev)
	if !errors.Is(err, substrate.ErrInvalidPayload) {
		t.Fatalf("Append: got %v, want ErrInvalidPayload", err)
	}
}

// TestS2T1_TailReturnsErrUnsupported — the v1 slice lands the interface + Append; Tail is Phase X (spec §1 OUT + §11 F1). Calling it must surf
func TestS2T1_TailReturnsErrUnsupported(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	h := history.NewSubstrate(db, testKey, testKeyID)

	_, _, err := h.Tail(ctx, "run-A", "")
	if !errors.Is(err, history.ErrUnsupported) {
		t.Fatalf("Tail: got %v, want ErrUnsupported", err)
	}
}

// TestS2T1_ReplayReturnsErrUnsupported — same slice rationale as Tail.
func TestS2T1_ReplayReturnsErrUnsupported(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	h := history.NewSubstrate(db, testKey, testKeyID)

	_, _, err := h.Replay(ctx, "run-A", history.ReplayOpts{TenantID: substrate.DefaultTenantID})
	if !errors.Is(err, history.ErrUnsupported) {
		t.Fatalf("Replay: got %v, want ErrUnsupported", err)
	}
}
