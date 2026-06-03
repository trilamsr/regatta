package triggers_test

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// Issue #659 — pre-migration the schema CHECK silently rejected
// manual_merge + operator_intervention writes; GreenClock never saw
// the resets. These tests exercise the real substrate emit path so
// the parity between GreenClock's reader contract and the durable
// schema stays load-bearing.

var greenClockHMACKey = []byte("0123456789abcdef0123456789abcdef")

const greenClockHMACKeyID = "test-key-1"

func emitGreenClockResetThroughSchema(t *testing.T, kind substrate.EventKind, seed byte) error {
	t.Helper()
	db := statetest.OpenMigratedRaw(t)
	ctx := context.Background()
	nonceBytes := make([]byte, 16)
	for i := range nonceBytes {
		nonceBytes[i] = seed
	}
	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	e := substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         "run-greenclock-659",
		TenantID:      substrate.DefaultTenantID,
		Kind:          kind,
		PayloadJSON:   []byte(`{"subject_id":"pr-42","actor":"tree","reason":"emergency-rollback"}`),
		WrittenBy:     "greenclock-test",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         hex.EncodeToString(nonceBytes),
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := substrate.AppendEvent(ctx, tx, e, greenClockHMACKey, greenClockHMACKeyID); err != nil {
		return err
	}
	return tx.Commit()
}

// TestGreenClock_ManualMerge_PersistsThroughSchema pins #659 — manual_merge writes survive the substrate CHECK.
func TestGreenClock_ManualMerge_PersistsThroughSchema(t *testing.T) {
	if err := emitGreenClockResetThroughSchema(t, substrate.KindManualMerge, 0xa1); err != nil {
		t.Fatalf("AppendEvent(manual_merge): %v", err)
	}
}

// TestGreenClock_OperatorIntervention_PersistsThroughSchema pins #659 — operator_intervention writes survive the substrate CHECK.
func TestGreenClock_OperatorIntervention_PersistsThroughSchema(t *testing.T) {
	if err := emitGreenClockResetThroughSchema(t, substrate.KindOperatorIntervention, 0xb2); err != nil {
		t.Fatalf("AppendEvent(operator_intervention): %v", err)
	}
}
