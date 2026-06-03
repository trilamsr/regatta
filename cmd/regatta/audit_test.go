package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestAuditVerify_DBSchemaSkew_SurfacesInOutput pins issue #550's
// schema-skew-aware audit posture: a verdict recorded under schema N
// when the binary's running schema is N+1 must surface as schema_skew
// in `audit verify` output even when the HMAC chain itself is intact.
//
// Construction: write one gate_verdict whose payload.DBSchemaVersion
// is 1 less than state.CurrentSchemaVersion, run `audit verify`, then
// assert the row carries schema_skew=true and summary.SchemaSkew=1.
func TestAuditVerify_DBSchemaSkew_SurfacesInOutput(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY_ID", "test-key-1")

	runID := "run-skew-1"
	at := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	// Build a payload that journals an OLDER schema than the binary
	// currently knows about. This is the realistic skew scenario:
	// verdict written by Vn, audit run by Vn+1 binary.
	skewedSchema := state.CurrentSchemaVersion - 1
	if skewedSchema <= 0 {
		t.Fatalf("CurrentSchemaVersion too low for skew test: %d", state.CurrentSchemaVersion)
	}
	p, err := substrate.NewGateVerdictPayload(
		"cel-approval", "WI-1", "cel", "sha:abc123",
		skewedSchema, true, true, "ok",
	)
	if err != nil {
		t.Fatalf("construct payload: %v", err)
	}
	payloadBytes, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ctx := context.Background()
	writeVerdictEvent(t, ctx, dbPath, substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runID,
		WorkItemID:    "WI-1",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindGateVerdict,
		Key:           "cel-approval",
		PayloadJSON:   payloadBytes,
		WrittenBy:     "tester",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         "00112233445566778899aabbccddeeff",
	})

	var stdout, stderr bytes.Buffer
	deps := auditDeps{
		Stdout: &stdout,
		Stderr: &stderr,
		DSN:    state.DSN(dbPath),
	}
	code := runAuditVerifyWith(deps, []string{"--run-id", runID, "--format", "json"})
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}

	var got struct {
		Summary auditVerifySummary `json:"summary"`
		Rows    []auditVerifyRow   `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode stdout: %v\nstdout=%s", err, stdout.String())
	}
	if got.Summary.SchemaSkew != 1 {
		t.Errorf("summary.SchemaSkew=%d want 1", got.Summary.SchemaSkew)
	}
	if got.Summary.Total != 1 {
		t.Errorf("summary.Total=%d want 1", got.Summary.Total)
	}
	if got.Summary.ChainOK != 1 {
		t.Errorf("summary.ChainOK=%d want 1 (chain must verify even under schema skew)", got.Summary.ChainOK)
	}
	if len(got.Rows) != 1 {
		t.Fatalf("rows=%d want 1", len(got.Rows))
	}
	row := got.Rows[0]
	if !row.SchemaSkew {
		t.Errorf("row.SchemaSkew=false want true (recorded=%d running=%d)", row.RecordedSchema, row.RunningSchema)
	}
	if row.RecordedSchema != skewedSchema {
		t.Errorf("row.RecordedSchema=%d want %d", row.RecordedSchema, skewedSchema)
	}
	if row.RunningSchema != state.CurrentSchemaVersion {
		t.Errorf("row.RunningSchema=%d want %d", row.RunningSchema, state.CurrentSchemaVersion)
	}
	if row.HMACStatus != "chain-ok" {
		t.Errorf("row.HMACStatus=%q want chain-ok", row.HMACStatus)
	}
	if row.AuditPosture != "reproduce" {
		t.Errorf("row.AuditPosture=%q want reproduce", row.AuditPosture)
	}
}

// TestAuditVerify_NonDeterministicVerdict_FlagsVerifyOnly pins the
// audit-CLI half of issue #550: a Deterministic=false verdict shows
// up as "verify-only" in `audit verify` so the auditor cannot mistake
// it for a re-runnable artifact.
func TestAuditVerify_NonDeterministicVerdict_FlagsVerifyOnly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY_ID", "test-key-1")

	runID := "run-llm-1"
	at := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	p, err := substrate.NewGateVerdictPayload(
		"ai-threat-model", "WI-2", "anthropic-api", "claude-opus-4-7",
		state.CurrentSchemaVersion, false, false, "found-risk",
	)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	payloadBytes, _ := json.Marshal(p)

	ctx := context.Background()
	writeVerdictEvent(t, ctx, dbPath, substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runID,
		WorkItemID:    "WI-2",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindGateVerdict,
		Key:           "ai-threat-model",
		PayloadJSON:   payloadBytes,
		WrittenBy:     "tester",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         "112233445566778899aabbccddeeff00",
	})

	var stdout, stderr bytes.Buffer
	code := runAuditVerifyWith(auditDeps{Stdout: &stdout, Stderr: &stderr, DSN: state.DSN(dbPath)},
		[]string{"--run-id", runID, "--format", "json"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var got struct {
		Summary auditVerifySummary `json:"summary"`
		Rows    []auditVerifyRow   `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Summary.VerifyOnly != 1 || got.Summary.Reproducible != 0 {
		t.Errorf("summary.VerifyOnly=%d Reproducible=%d want 1/0", got.Summary.VerifyOnly, got.Summary.Reproducible)
	}
	if len(got.Rows) != 1 {
		t.Fatalf("rows=%d want 1", len(got.Rows))
	}
	if got.Rows[0].AuditPosture != "verify-only" {
		t.Errorf("posture=%q want verify-only", got.Rows[0].AuditPosture)
	}
	if got.Rows[0].Tool != "anthropic-api" {
		t.Errorf("tool=%q want anthropic-api", got.Rows[0].Tool)
	}
}

// writeVerdictEvent opens the temp DB, applies migrations, appends
// one signed gate_verdict event, and closes the handle so the
// CLI-under-test can open its own connection without racing against
// sqlite's single-writer model.
func writeVerdictEvent(t *testing.T, ctx context.Context, dbPath string, ev substrate.Event) {
	t.Helper()
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		_ = db.Close()
		t.Fatalf("begin: %v", err)
	}
	if err := substrate.AppendEvent(ctx, tx, ev, []byte("0123456789abcdef0123456789abcdef"), "test-key-1"); err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		_ = db.Close()
		t.Fatalf("commit: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
