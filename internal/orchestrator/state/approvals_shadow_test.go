package state

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// shadowTestKey is the 32-byte HMAC key used by every Phase-B shadow
// test. Production callsites load REGATTA_HMAC_KEY via serve.go.
var shadowTestKey = []byte("0123456789abcdef0123456789abcdef")

const shadowTestKeyID = "test-key-1"

func newShadowCfg() ShadowWriteConfig {
	return ShadowWriteConfig{
		Mode:     ShadowModeOn,
		Key:      shadowTestKey,
		KeyID:    shadowTestKeyID,
		TenantID: "self-host",
		RunID:    "run-shadow-test",
	}
}

// TestApproval_ShadowWrite_LandsInBothStores pins Phase B's contract.
func TestApproval_ShadowWrite_LandsInBothStores(t *testing.T) {
	t0 := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	substrate.ResetClockForTesting()
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-shadow-1", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	ev := ApprovalEvent{
		ApprovalID: a.ID,
		Ts:         t0,
		Kind:       "requested",
		Actor:      "system",
		Payload:    json.RawMessage(`{"reason":"shadow-test"}`),
	}
	if err := db.AppendApprovalEventWithShadow(ctx, ev, newShadowCfg()); err != nil {
		t.Fatalf("AppendApprovalEventWithShadow: %v", err)
	}

	legacy, err := db.ListApprovalEvents(ctx, a.ID)
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}
	if len(legacy) != 1 {
		t.Fatalf("legacy len=%d want 1", len(legacy))
	}

	var subCount int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM substrate_events
		   WHERE kind='approval_event'
		     AND json_extract(payload_json, '$.approval_id') = ?`,
		a.ID).Scan(&subCount); err != nil {
		t.Fatalf("count substrate: %v", err)
	}
	if subCount != 1 {
		t.Fatalf("substrate rows=%d want 1", subCount)
	}
}

// TestApproval_ShadowWrite_OffMeansNoSubstrateRow guards the flag default.
func TestApproval_ShadowWrite_OffMeansNoSubstrateRow(t *testing.T) {
	t0 := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	substrate.ResetClockForTesting()
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-off", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}

	cfg := newShadowCfg()
	cfg.Mode = ShadowModeOff
	if err := db.AppendApprovalEventWithShadow(ctx,
		ApprovalEvent{ApprovalID: a.ID, Ts: t0, Kind: "requested", Actor: "system"},
		cfg); err != nil {
		t.Fatalf("shadow off: %v", err)
	}
	var subCount int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM substrate_events WHERE kind='approval_event'`).Scan(&subCount); err != nil {
		t.Fatal(err)
	}
	if subCount != 0 {
		t.Fatalf("substrate rows=%d want 0 (mode=off)", subCount)
	}
}

// TestApproval_ShadowWrite_LegacyFailureSkipsSubstrate pins ordering.
func TestApproval_ShadowWrite_LegacyFailureSkipsSubstrate(t *testing.T) {
	t0 := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	substrate.ResetClockForTesting()
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	// No work_item seed → FK violation on approval insert → legacy fails.
	// Append on a non-existent approval should error and write nothing.
	ev := ApprovalEvent{ApprovalID: "a-missing", Ts: t0, Kind: "requested", Actor: "system"}
	if err := db.AppendApprovalEventWithShadow(ctx, ev, newShadowCfg()); err == nil {
		t.Fatalf("expected legacy FK error")
	}
	var subCount int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM substrate_events WHERE kind='approval_event'`).Scan(&subCount); err != nil {
		t.Fatal(err)
	}
	if subCount != 0 {
		t.Fatalf("substrate rows=%d want 0 (legacy failed first)", subCount)
	}
}

// TestApproval_ShadowWrite_SubstrateFailLogsButReturnsNil pins best-effort.
func TestApproval_ShadowWrite_SubstrateFailLogsButReturnsNil(t *testing.T) {
	t0 := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	substrate.ResetClockForTesting()
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-divg", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	cfg := newShadowCfg()
	cfg.Key = []byte("too-short") // < MinKeyLen, substrate Sign will reject.
	err := db.AppendApprovalEventWithShadow(ctx,
		ApprovalEvent{ApprovalID: a.ID, Ts: t0, Kind: "requested", Actor: "system"},
		cfg)
	if err != nil {
		t.Fatalf("substrate failure must NOT propagate: got %v", err)
	}
	var legacyCount int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM approval_events WHERE approval_id = ?`, a.ID).Scan(&legacyCount); err != nil {
		t.Fatal(err)
	}
	if legacyCount != 1 {
		t.Fatalf("legacy len=%d want 1 (legacy is source-of-truth)", legacyCount)
	}
	var divCount int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM substrate_divergence_audit
		   WHERE store='approvals' AND primary_key=?`, a.ID).Scan(&divCount); err != nil {
		t.Fatalf("count divergence: %v", err)
	}
	if divCount != 1 {
		t.Fatalf("divergence audit rows=%d want 1", divCount)
	}
}

// TestApproval_ShadowWrite_PayloadCarriesTransitionAndActor pins payload shape.
func TestApproval_ShadowWrite_PayloadCarriesTransitionAndActor(t *testing.T) {
	t0 := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	substrate.ResetClockForTesting()
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-payload", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	ev := ApprovalEvent{
		ApprovalID: a.ID, Ts: t0, Kind: "approved", Actor: "alice",
		TokenJTI: "jti-PAY",
	}
	if err := db.AppendApprovalEventWithShadow(ctx, ev, newShadowCfg()); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT payload_json FROM substrate_events
		   WHERE kind='approval_event'
		     AND json_extract(payload_json, '$.approval_id') = ?`,
		a.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("unmarshal substrate payload: %v", err)
	}
	if p["transition"] != "approved" {
		t.Fatalf("transition=%v want approved", p["transition"])
	}
	if p["actor"] != "alice" {
		t.Fatalf("actor=%v want alice", p["actor"])
	}
	if p["approval_id"] != a.ID {
		t.Fatalf("approval_id=%v want %q", p["approval_id"], a.ID)
	}
	if p["token_jti"] != "jti-PAY" {
		t.Fatalf("token_jti=%v want jti-PAY (must live in payload, not column-nonce)", p["token_jti"])
	}
}
