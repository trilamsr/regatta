package substrate_test

import (
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_KindPayloadValidation drives the per-kind typed
// payload validators registered by validate.go's init(). For each of
// T-S1's 6 kinds: pass a malformed payload, expect ErrInvalidPayload;
// pass a well-formed payload, expect nil. Per the plan adversarial-
// review note (§adversarial item 3), this table-driven test ALSO
// asserts defaultReducer(kind) returns the spec §4 strategy — pinning
// validator AND reducer per kind in a single sweep.
//
// KindGateVerdict is registered by T-S2 (gate_verdict_payload.go) and
// tested in T-S2's package. This test asserts the dispatch table
// rejects an UNKNOWN kind ("totally-bogus") with ErrInvalidPayload.
func TestSubstrate_KindPayloadValidation(t *testing.T) {
	cases := []struct {
		name         string
		kind         substrate.EventKind
		wellFormed   string
		malformed    string
		wantStrategy substrate.ReducerStrategy
	}{
		{
			name:         "node_output",
			kind:         substrate.KindNodeOutput,
			wellFormed:   `{"work_item_id":"WI-1","attempt":1,"output":{}}`,
			malformed:    `{"work_item_id":""}`,
			wantStrategy: substrate.StrategyLWW,
		},
		{
			name:         "fact",
			kind:         substrate.KindFact,
			wellFormed:   `{"key":"k","value":1}`,
			malformed:    `{"key":""}`,
			wantStrategy: substrate.StrategyLWW,
		},
		{
			name:         "approval_event",
			kind:         substrate.KindApprovalEvent,
			wellFormed:   `{"approval_id":"A1","transition":"approved","actor":"u"}`,
			malformed:    `{"approval_id":"A1"}`,
			wantStrategy: substrate.StrategyAppend,
		},
		{
			name:         "token_spend",
			kind:         substrate.KindTokenSpend,
			wellFormed:   `{"llm_call_id":"L1","spend_usd":0.01,"tokens":100}`,
			malformed:    `{"llm_call_id":""}`,
			wantStrategy: substrate.StrategyAppend,
		},
		{
			name:         "budget_reconciled",
			kind:         substrate.KindBudgetReconciled,
			wellFormed:   `{"tenant_id":"t","period_start":1,"spend_usd":1.0}`,
			malformed:    `{"tenant_id":""}`,
			wantStrategy: substrate.StrategyLWW,
		},
		{
			name:         "heartbeat",
			kind:         substrate.KindHeartbeat,
			wellFormed:   `{"work_item_id":"WI-1","timestamp":1}`,
			malformed:    `{"work_item_id":""}`,
			wantStrategy: substrate.StrategyLWW,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Use the same path AppendEvent uses: per-kind dispatch via
			// the unexported validator. Exercise indirectly through an
			// in-memory append against a fresh DB; the malformed event
			// must fail with ErrInvalidPayload before INSERT.
			db := openMigratedDB(t)

			good := substrate.Event{
				ID: substrate.Mint(testTime()), RunID: "run-V",
				TenantID: substrate.DefaultTenantID, Kind: c.kind,
				PayloadJSON: []byte(c.wellFormed),
				WrittenBy:   "tester", WrittenAt: testTime().UnixMilli(),
				SchemaVersion: 1, Nonce: fixedNonce(0x10),
			}
			if err := appendEventTx(testCtx(), t, db, good); err != nil {
				t.Fatalf("%s well-formed append: %v", c.name, err)
			}

			bad := good
			bad.ID = substrate.Mint(testTime().Add(1))
			bad.Nonce = fixedNonce(0x11)
			bad.WrittenAt++
			bad.PayloadJSON = []byte(c.malformed)
			err := appendEventTx(testCtx(), t, db, bad)
			if !errors.Is(err, substrate.ErrInvalidPayload) {
				t.Fatalf("%s malformed append: err=%v want ErrInvalidPayload", c.name, err)
			}

			// Reducer assertion (plan adversarial item 3).
			if got := substrate.DefaultReducer(c.kind); got != c.wantStrategy {
				t.Errorf("%s reducer: got=%q want=%q", c.name, got, c.wantStrategy)
			}
		})
	}

	// Unknown kind: dispatch must reject with ErrInvalidPayload (no
	// validator registered for "totally-bogus").
	t.Run("unknown_kind", func(t *testing.T) {
		db := openMigratedDB(t)
		// The schema CHECK on kind would also reject — but the
		// validate-before-INSERT order means we see ErrInvalidPayload
		// first.
		e := substrate.Event{
			ID: substrate.Mint(testTime()), RunID: "run-V",
			TenantID: substrate.DefaultTenantID,
			Kind:     substrate.EventKind("totally-bogus"),
			PayloadJSON: []byte(`{}`),
			WrittenBy:   "tester", WrittenAt: testTime().UnixMilli(),
			SchemaVersion: 1, Nonce: fixedNonce(0x12),
		}
		err := appendEventTx(testCtx(), t, db, e)
		if !errors.Is(err, substrate.ErrInvalidPayload) {
			t.Fatalf("unknown kind: err=%v want ErrInvalidPayload", err)
		}
	})
}
