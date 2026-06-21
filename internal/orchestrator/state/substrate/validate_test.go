package substrate_test

import (
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_KindPayloadValidation drives per-kind validators + asserts defaultReducer(kind) returns the spec §4 strategy.
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
			wellFormed:   `{"usd":0.01,"model":"claude-sonnet-4-7","input_tokens":100,"output_tokens":10,"cache_read_tokens":0,"cache_creation_tokens":0,"operator_id":"agent-1","dag_id":"DAG-A","work_item_id":"WI-1","pricing_rev":"anthropic@2026-06-01","call_id":"msg_01"}`,
			malformed:    `{"usd":0.01,"model":"claude-sonnet-4-7","input_tokens":0,"output_tokens":0,"cache_read_tokens":0,"cache_creation_tokens":0,"operator_id":"","dag_id":"","work_item_id":"","pricing_rev":"","call_id":""}`,
			wantStrategy: substrate.StrategyAppend,
		},
		{
			name:         "budget_reconciled",
			kind:         substrate.KindBudgetReconciled,
			wellFormed:   `{"period_start":1,"period_end":2,"actual_usd":10.0,"recorded_usd":9.5,"delta_usd":0.5,"drift_pct":0.05,"model_breakdown":[],"api_response_sig":"sha256:abc"}`,
			malformed:    `{"period_start":0,"period_end":0,"actual_usd":0,"recorded_usd":0,"delta_usd":0,"drift_pct":0,"model_breakdown":[],"api_response_sig":""}`,
			wantStrategy: substrate.StrategyLWW,
		},
		{
			name:         "heartbeat",
			kind:         substrate.KindHeartbeat,
			wellFormed:   `{"work_item_id":"WI-1","timestamp":1}`,
			malformed:    `{"work_item_id":""}`,
			wantStrategy: substrate.StrategyLWW,
		},
		{
			name:         "pr_stage_transition",
			kind:         substrate.KindPRStageTransition,
			wellFormed:   `{"pr_number":42,"from_stage":"draft","to_stage":"gates_running","duration_seconds":12.5}`,
			malformed:    `{"pr_number":42,"from_stage":"bogus","to_stage":"merged"}`,
			wantStrategy: substrate.StrategyAppend,
		},
		{
			name:         "manual_merge",
			kind:         substrate.KindManualMerge,
			wellFormed:   `{"subject_id":"pr-42","actor":"tree","reason":"emergency-rollback"}`,
			malformed:    `{"subject_id":"pr-42","actor":"","reason":"x"}`,
			wantStrategy: substrate.StrategyAppend,
		},
		{
			name:         "operator_intervention",
			kind:         substrate.KindOperatorIntervention,
			wellFormed:   `{"subject_id":"branch-main","actor":"tree","reason":"force-rebase"}`,
			malformed:    `{"subject_id":"","actor":"tree","reason":"x"}`,
			wantStrategy: substrate.StrategyAppend,
		},
		{
			name:         "cost_cap_throttled",
			kind:         substrate.KindCostCapThrottled,
			wellFormed:   `{"spend_micro":40000001,"cap_micro":40000000,"auto_resume_at":"2026-06-03T00:00:00Z","tz":"UTC"}`,
			malformed:    `{"spend_micro":40000001,"cap_micro":0,"auto_resume_at":"","tz":""}`,
			wantStrategy: substrate.StrategyAppend,
		},
		{
			name:         "cost_cap_resumed",
			kind:         substrate.KindCostCapResumed,
			wellFormed:   `{"actor":"tree","reason":"operator","until":"2026-06-03T00:00:00Z"}`,
			malformed:    `{"actor":"","reason":"operator","until":"2026-06-03T00:00:00Z"}`,
			wantStrategy: substrate.StrategyAppend,
		},
		{
			name:         "merge_low_risk_decision",
			kind:         substrate.KindMergeLowRiskDecision,
			wellFormed:   `{"pr":42,"eligible":true,"reason":"eligible","loc_delta":3,"soak_remaining_sec":0}`,
			malformed:    `{"pr":0,"eligible":true,"reason":"eligible","loc_delta":0,"soak_remaining_sec":0}`,
			wantStrategy: substrate.StrategyAppend,
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
