package costcap_test

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// W5 cost-cap follow-up #622 — substrate-event-schema cross-link.
// Mirrors internal/triggers/greenclock_schema_test.go pattern: assert
// substrate AppendEvent accepts the new cost-cap kinds end-to-end
// through the real schema CHECK + validator + HMAC sign path.

var costCapHMACKey = []byte("0123456789abcdef0123456789abcdef")

const costCapHMACKeyID = "test-key-1"

func emitCostCapEventThroughSchema(t *testing.T, kind substrate.EventKind, payload string, seed byte) error {
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
		RunID:         "run-costcap-622",
		TenantID:      substrate.DefaultTenantID,
		Kind:          kind,
		PayloadJSON:   []byte(payload),
		WrittenBy:     "costcap-test",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         hex.EncodeToString(nonceBytes),
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := substrate.AppendEvent(ctx, tx, e, costCapHMACKey, costCapHMACKeyID); err != nil {
		return err
	}
	return tx.Commit()
}

// TestSubstrate_KindCostCapThrottled_PersistsWithSchema pins #622 — cost_cap_throttled writes survive substrate CHECK + HMAC.
func TestSubstrate_KindCostCapThrottled_PersistsWithSchema(t *testing.T) {
	payload := `{"spend_micro":40000001,"cap_micro":40000000,"auto_resume_at":"2026-06-03T00:00:00Z","tz":"UTC"}`
	if err := emitCostCapEventThroughSchema(t, substrate.KindCostCapThrottled, payload, 0xc1); err != nil {
		t.Fatalf("AppendEvent(cost_cap_throttled): %v", err)
	}
}

// TestSubstrate_KindCostCapResumed_PersistsWithSchema pins #622 — cost_cap_resumed writes survive substrate CHECK + HMAC.
func TestSubstrate_KindCostCapResumed_PersistsWithSchema(t *testing.T) {
	payload := `{"actor":"tree","reason":"operator","until":"2026-06-03T00:00:00Z"}`
	if err := emitCostCapEventThroughSchema(t, substrate.KindCostCapResumed, payload, 0xc2); err != nil {
		t.Fatalf("AppendEvent(cost_cap_resumed): %v", err)
	}
}

// TestEnforcer_MigratedSubstrateEvents_ReadablePostMigration pins #622 — kinds enumerate via substrate.AllKinds + DefaultReducer post-0017.
func TestEnforcer_MigratedSubstrateEvents_ReadablePostMigration(t *testing.T) {
	var sawThrottled, sawResumed bool
	for _, k := range substrate.AllKinds() {
		switch k {
		case substrate.KindCostCapThrottled:
			sawThrottled = true
		case substrate.KindCostCapResumed:
			sawResumed = true
		}
	}
	if !sawThrottled || !sawResumed {
		t.Fatalf("AllKinds missing cost-cap entries: throttled=%v resumed=%v",
			sawThrottled, sawResumed)
	}
	if got := substrate.DefaultReducer(substrate.KindCostCapThrottled); got != substrate.StrategyAppend {
		t.Fatalf("reducer(cost_cap_throttled)=%q want Append", got)
	}
	if got := substrate.DefaultReducer(substrate.KindCostCapResumed); got != substrate.StrategyAppend {
		t.Fatalf("reducer(cost_cap_resumed)=%q want Append", got)
	}
}
