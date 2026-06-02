package state

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestApproval_ReadFromSubstrate_HappyPath pins Phase C read seam.
func TestApproval_ReadFromSubstrate_HappyPath(t *testing.T) {
	t0 := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	substrate.ResetClockForTesting()
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-read-hp", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	cfg := newShadowCfg()
	cfg.ReadMode = ReadModeSubstrateFirst
	ev := ApprovalEvent{ //nolint:gosec // G101: test fixture JTI string, not a credential.
		ApprovalID: a.ID, Ts: t0, Kind: "requested", Actor: "alice",
		TokenJTI: "jti-RD-1",
	}
	if err := db.AppendApprovalEventWithShadow(ctx, ev, cfg); err != nil {
		t.Fatalf("AppendApprovalEventWithShadow: %v", err)
	}

	out, err := db.ListApprovalEventsWithShadow(ctx, a.ID, cfg)
	if err != nil {
		t.Fatalf("ListApprovalEventsWithShadow: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("read len=%d want 1", len(out))
	}
	if out[0].Kind != "requested" {
		t.Fatalf("kind=%q want requested", out[0].Kind)
	}
	if out[0].Actor != "alice" {
		t.Fatalf("actor=%q want alice", out[0].Actor)
	}
	if out[0].TokenJTI != "jti-RD-1" {
		t.Fatalf("token_jti=%q want jti-RD-1", out[0].TokenJTI)
	}
	if out[0].ApprovalID != a.ID {
		t.Fatalf("approval_id=%q want %q", out[0].ApprovalID, a.ID)
	}

	var divCount int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM substrate_divergence_audit WHERE store='approvals'`).Scan(&divCount); err != nil {
		t.Fatal(err)
	}
	if divCount != 0 {
		t.Fatalf("divergence rows=%d want 0 (happy path)", divCount)
	}
}

// TestApproval_ReadFromSubstrate_FallbackOnMiss pins the legacy-fallback path.
func TestApproval_ReadFromSubstrate_FallbackOnMiss(t *testing.T) {
	t0 := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	substrate.ResetClockForTesting()
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-read-fb", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	cfgWriteOff := newShadowCfg()
	cfgWriteOff.Mode = ShadowModeOff
	ev := ApprovalEvent{ApprovalID: a.ID, Ts: t0, Kind: "requested", Actor: "alice"}
	if err := db.AppendApprovalEventWithShadow(ctx, ev, cfgWriteOff); err != nil {
		t.Fatalf("legacy-only append: %v", err)
	}

	cfgRead := newShadowCfg()
	cfgRead.ReadMode = ReadModeSubstrateFirst
	out, err := db.ListApprovalEventsWithShadow(ctx, a.ID, cfgRead)
	if err != nil {
		t.Fatalf("ListApprovalEventsWithShadow: %v", err)
	}
	if len(out) != 1 || out[0].Kind != "requested" || out[0].Actor != "alice" {
		t.Fatalf("legacy-fallback rows=%+v", out)
	}

	var divCount int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM substrate_divergence_audit
		   WHERE store='approvals' AND detector='layer1_read' AND primary_key=?`, a.ID).Scan(&divCount); err != nil {
		t.Fatalf("count divergence: %v", err)
	}
	if divCount != 1 {
		t.Fatalf("divergence rows=%d want 1 (substrate-miss + legacy-has-data)", divCount)
	}
}

// TestApproval_ReadFromSubstrate_BothEmptyNoDivergence guards no-data case.
func TestApproval_ReadFromSubstrate_BothEmptyNoDivergence(t *testing.T) {
	t0 := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	substrate.ResetClockForTesting()
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-empty", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	cfgRead := newShadowCfg()
	cfgRead.ReadMode = ReadModeSubstrateFirst
	out, err := db.ListApprovalEventsWithShadow(ctx, a.ID, cfgRead)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("rows=%d want 0", len(out))
	}
	var divCount int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM substrate_divergence_audit WHERE store='approvals'`).Scan(&divCount); err != nil {
		t.Fatal(err)
	}
	if divCount != 0 {
		t.Fatalf("divergence rows=%d want 0 (both empty)", divCount)
	}
}

// TestApproval_ReadFromSubstrate_OffMeansLegacyRead guards the flag default.
func TestApproval_ReadFromSubstrate_OffMeansLegacyRead(t *testing.T) {
	t0 := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	substrate.ResetClockForTesting()
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedApprovalWorkItem(t, db, "F-1")
	a := sampleApproval("a-off-r", "F-1", t0, t0.Add(time.Hour))
	if err := db.CreateApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	cfg := newShadowCfg()
	cfg.Mode = ShadowModeOff
	cfg.ReadMode = ReadModeLegacy
	if err := db.AppendApprovalEventWithShadow(ctx,
		ApprovalEvent{ApprovalID: a.ID, Ts: t0, Kind: "requested", Actor: "bob"}, cfg); err != nil {
		t.Fatal(err)
	}
	out, err := db.ListApprovalEventsWithShadow(ctx, a.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Actor != "bob" {
		t.Fatalf("legacy-read rows=%+v", out)
	}
}
