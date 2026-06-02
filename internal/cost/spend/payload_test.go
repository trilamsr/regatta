package spend_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestPayload_TokenSpendJSONTags pins spec §3.5 lines 264-276 verbatim.
func TestPayload_TokenSpendJSONTags(t *testing.T) {
	p := spend.TokenSpendPayload{
		USD: 1.0, Model: "m", InputTokens: 1, OutputTokens: 2,
		CacheReadTokens: 3, CacheCreationTokens: 4,
		OperatorID: "o", DAGID: "d", WorkItemID: "w",
		PricingRev: "r", CallID: "c",
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, tag := range []string{
		`"usd":1`, `"model":"m"`, `"input_tokens":1`, `"output_tokens":2`,
		`"cache_read_tokens":3`, `"cache_creation_tokens":4`,
		`"operator_id":"o"`, `"dag_id":"d"`, `"work_item_id":"w"`,
		`"pricing_rev":"r"`, `"call_id":"c"`,
	} {
		if !strings.Contains(string(b), tag) {
			t.Errorf("missing %s in %s", tag, b)
		}
	}
}

// TestPayload_BudgetReconciledJSONTags pins spec §3.5 lines 278-289 verbatim.
func TestPayload_BudgetReconciledJSONTags(t *testing.T) {
	p := spend.BudgetReconciledPayload{
		PeriodStart: 1, PeriodEnd: 2, ActualUSD: 10, RecordedUSD: 9,
		DeltaUSD: 1, DriftPct: 0.1,
		ModelBreakdown: []spend.ModelBreakdownRow{{Model: "m", USD: 1}},
		APIResponseSig: "sha",
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, tag := range []string{
		`"period_start":1`, `"period_end":2`, `"actual_usd":10`,
		`"recorded_usd":9`, `"delta_usd":1`, `"drift_pct":0.1`,
		`"model_breakdown"`, `"api_response_sig":"sha"`,
	} {
		if !strings.Contains(string(b), tag) {
			t.Errorf("missing %s in %s", tag, b)
		}
	}
}

// TestSubstrate_TokenSpendPayloadValidates pins the substrate validate-dispatch shape per spec §3.5.
func TestSubstrate_TokenSpendPayloadValidates(t *testing.T) {
	db := openWriterDB(t)
	// Well-formed: pass it through RecordCall which builds the canonical
	// payload, so the validate-dispatch sees exactly what production emits.
	if err := recordOne(t, context.Background(), db, baseRecord()); err != nil {
		t.Fatalf("well-formed RecordCall failed substrate validation: %v", err)
	}

	// Malformed: hand-craft a payload missing call_id; expect ErrInvalidPayload.
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := mustParseUnixMs("2026-06-01T12:00:01Z")
	bad := substrate.Event{
		ID:            substrate.Mint(now),
		RunID:         "run-x",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindTokenSpend,
		PayloadJSON:   []byte(`{"usd":1,"model":"m","input_tokens":0,"output_tokens":0,"cache_read_tokens":0,"cache_creation_tokens":0,"operator_id":"o","dag_id":"d","work_item_id":"w","pricing_rev":"r","call_id":""}`),
		WrittenBy:     "tester",
		WrittenAt:     now.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         "1111111111111111",
	}
	err = substrate.AppendEvent(context.Background(), tx, bad, testKey, testKeyID)
	if err == nil {
		t.Fatalf("malformed AppendEvent returned nil; want ErrInvalidPayload")
	}
	if !errIsInvalid(err) {
		t.Fatalf("malformed err=%v; want ErrInvalidPayload", err)
	}
}
