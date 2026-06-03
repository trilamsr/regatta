package substrate_test

import (
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// validVerdictPayload is the canonical well-formed gate_verdict
// payload used across every case. Centralised so issue #550's new
// required fields (tool, tv, db_v, det) flow into every test through
// one place instead of being smeared across six string literals.
const validVerdictPayload = `{"gate_name":"approval","pass":true,"reason":"ok","work_item_id":"WI-1","tool":"cel","tv":"sha:abc123","db_v":12,"det":true}`

// TestSubstrate_GateVerdictPayloadValidation pins T-S2's KindGateVerdict validator dispatch.
func TestSubstrate_GateVerdictPayloadValidation(t *testing.T) {
	db := openMigratedDB(t)
	ctx := testCtx()

	good := substrate.Event{
		ID:            substrate.Mint(testTime()),
		RunID:         "run-G",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindGateVerdict,
		PayloadJSON:   []byte(validVerdictPayload),
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
		{"missing_gate_name", `{"pass":true,"reason":"ok","work_item_id":"WI-1","tool":"cel","tv":"sha:abc123","db_v":12,"det":true}`},
		{"empty_gate_name", `{"gate_name":"","pass":true,"reason":"ok","work_item_id":"WI-1","tool":"cel","tv":"sha:abc123","db_v":12,"det":true}`},
		{"missing_work_item_id", `{"gate_name":"g","pass":true,"reason":"ok","tool":"cel","tv":"sha:abc123","db_v":12,"det":true}`},
		{"missing_tool", `{"gate_name":"g","pass":true,"reason":"ok","work_item_id":"WI-1","tv":"sha:abc123","db_v":12,"det":true}`},
		{"missing_tool_version", `{"gate_name":"g","pass":true,"reason":"ok","work_item_id":"WI-1","tool":"cel","db_v":12,"det":true}`},
		{"missing_db_v", `{"gate_name":"g","pass":true,"reason":"ok","work_item_id":"WI-1","tool":"cel","tv":"sha:abc123","det":true}`},
		{"zero_db_v", `{"gate_name":"g","pass":true,"reason":"ok","work_item_id":"WI-1","tool":"cel","tv":"sha:abc123","db_v":0,"det":true}`},
		{"trailing_garbage", validVerdictPayload + `garbage`},
		{"unknown_field", `{"gate_name":"g","pass":true,"reason":"ok","work_item_id":"WI-1","tool":"cel","tv":"sha:abc123","db_v":12,"det":true,"extra":"x"}`},
		{"empty", ``},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad := good
			bad.ID = substrate.Mint(testTime().Add(1))
			bad.Nonce = fixedNonce(byte(0x41 + i)) //nolint:gosec // G115: i is a small loop index over the cases slice; no overflow possible
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

// TestVerdict_RequiresMetadata_RejectsEmptyTool pins issue #550's
// core invariant: a verdict payload constructed without a Tool field
// is rejected at construction time — operators cannot accidentally
// record a verdict whose producer is anonymous.
func TestVerdict_RequiresMetadata_RejectsEmptyTool(t *testing.T) {
	_, err := substrate.NewGateVerdictPayload(
		"approval", "WI-1", /*tool*/ "", "sha:abc123", 12, true, true, "ok",
	)
	if !errors.Is(err, substrate.ErrInvalidPayload) {
		t.Fatalf("empty tool: err=%v want ErrInvalidPayload", err)
	}

	for _, c := range []struct {
		name           string
		gate           string
		wi             string
		tool           string
		toolVersion    string
		dbSchema       int64
	}{
		{"empty_gate", "", "WI-1", "cel", "sha:abc", 12},
		{"empty_wi", "g", "", "cel", "sha:abc", 12},
		{"empty_tool_version", "g", "WI-1", "cel", "", 12},
		{"zero_db_schema", "g", "WI-1", "cel", "sha:abc", 0},
		{"negative_db_schema", "g", "WI-1", "cel", "sha:abc", -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := substrate.NewGateVerdictPayload(c.gate, c.wi, c.tool, c.toolVersion, c.dbSchema, true, true, "ok")
			if !errors.Is(err, substrate.ErrInvalidPayload) {
				t.Fatalf("err=%v want ErrInvalidPayload", err)
			}
		})
	}
}

// TestVerdict_NonDeterministic_AuditMarksVerifyOnly pins the issue #550
// reframe: a non-deterministic verdict's audit posture is "verify-only",
// meaning "chain is tamper-evident, verdict is NOT replayable".
func TestVerdict_NonDeterministic_AuditMarksVerifyOnly(t *testing.T) {
	p, err := substrate.NewGateVerdictPayload(
		"ai-threat-model", "WI-1", "anthropic-api", "claude-opus-4-7", 12,
		/*deterministic*/ false, false, "high-risk finding",
	)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if got := p.AuditPosture(); got != "verify-only" {
		t.Fatalf("AuditPosture=%q want verify-only (non-deterministic verdicts are tamper-evident only)", got)
	}
}

// TestVerdict_Deterministic_AuditAllowsReproduce pins the other arm of
// the issue #550 reframe: Deterministic=true ⇒ audit posture allows
// replay-compare against the recorded verdict.
func TestVerdict_Deterministic_AuditAllowsReproduce(t *testing.T) {
	p, err := substrate.NewGateVerdictPayload(
		"cel-approval", "WI-1", "cel", "sha:abc123", 12,
		/*deterministic*/ true, true, "ok",
	)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if got := p.AuditPosture(); got != "reproduce" {
		t.Fatalf("AuditPosture=%q want reproduce (deterministic verdicts must allow replay-compare)", got)
	}
}
