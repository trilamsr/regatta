package substrate_test

import (
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_GateVerdictPayloadValidation pins T-S2's KindGateVerdict validator dispatch.
func TestSubstrate_GateVerdictPayloadValidation(t *testing.T) {
	db := openMigratedDB(t)
	ctx := testCtx()

	good := substrate.Event{
		ID:            substrate.Mint(testTime()),
		RunID:         "run-G",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindGateVerdict,
		PayloadJSON:   []byte(`{"gate_name":"approval","pass":true,"reason":"ok","work_item_id":"WI-1"}`),
		WrittenBy:     "celdecider",
		WrittenAt:     testTime().UnixMilli(),
		SchemaVersion: 1,
		Nonce:         fixedNonce(0x40),
	}
	if err := appendEventTx(ctx, t, db, good); err != nil {
		t.Fatalf("well-formed gate_verdict append: %v", err)
	}

	cases := []struct {
		name    string
		payload string
	}{
		{"missing_gate_name", `{"pass":true,"reason":"ok","work_item_id":"WI-1"}`},
		{"empty_gate_name", `{"gate_name":"","pass":true,"reason":"ok","work_item_id":"WI-1"}`},
		{"missing_work_item_id", `{"gate_name":"g","pass":true,"reason":"ok"}`},
		{"trailing_garbage", `{"gate_name":"g","pass":true,"reason":"ok","work_item_id":"WI-1"}garbage`},
		{"unknown_field", `{"gate_name":"g","pass":true,"reason":"ok","work_item_id":"WI-1","extra":"x"}`},
		{"empty", ``},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad := good
			bad.ID = substrate.Mint(testTime().Add(1))
			bad.Nonce = fixedNonce(byte(0x41 + i)) //nolint:gosec // G115: i is a small loop index over the cases slice (≤ 6); no overflow possible
			bad.WrittenAt = good.WrittenAt + int64(i+1)
			bad.PayloadJSON = []byte(c.payload)
			err := appendEventTx(ctx, t, db, bad)
			if !errors.Is(err, substrate.ErrInvalidPayload) {
				t.Fatalf("%s: err=%v want ErrInvalidPayload", c.name, err)
			}
		})
	}
}

// TestSubstrate_GateVerdictReducerIsAppend pins spec §4 reducer strategy for KindGateVerdict.
func TestSubstrate_GateVerdictReducerIsAppend(t *testing.T) {
	if got := substrate.DefaultReducer(substrate.KindGateVerdict); got != substrate.StrategyAppend {
		t.Fatalf("DefaultReducer(KindGateVerdict)=%q want %q", got, substrate.StrategyAppend)
	}
}
