package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestAuditVerify_DBSchemaSkew_SurfacesInOutput pins issue #550's schema-skew-aware audit posture: a verdict recorded under N with binary running at N+1 surfaces schema_skew=true even when the HMAC chain verifies.
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
	if row.HMACStatus != hmacStatusChainOK {
		t.Errorf("row.HMACStatus=%q want %s", row.HMACStatus, hmacStatusChainOK)
	}
	if row.AuditPosture != "reproduce" {
		t.Errorf("row.AuditPosture=%q want reproduce", row.AuditPosture)
	}
}

// TestAuditVerify_NonDeterministicVerdict_FlagsVerifyOnly pins the audit-CLI half of issue #550: a Deterministic=false verdict surfaces as "verify-only" so the auditor cannot mistake it for a re-runnable artifact.
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

// writeRawVerdictEvent signs ev with the test keyring and INSERTs it
// directly, bypassing the per-kind payload validator. The validator
// today rejects gate_verdict payloads missing Tool / ModelOrVersion /
// DBSchemaVersion (issue #550) — that is correct for NEW writes but
// legitimate audit-time paths exist where the fold sees rows the
// CURRENT validator would reject: pre-#550 legacy rows backfilled by
// schema migration, and rows whose payload bytes themselves got
// corrupted on disk. Tests for those paths need to plant the row
// shape directly so the audit-verify code under test sees the same
// thing it would see in production.
func writeRawVerdictEvent(t *testing.T, ctx context.Context, dbPath string, ev substrate.Event) {
	t.Helper()
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := substrate.Sign(&ev, []byte("0123456789abcdef0123456789abcdef"), "test-key-1"); err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, err = db.SQL().ExecContext(ctx,
		`INSERT INTO substrate_events
		   (id, run_id, work_item_id, tenant_id, trace_id, span_id,
		    kind, key, payload_json, blob_digest, supersedes,
		    written_by, written_at, schema_version, nonce,
		    sig_alg, sig_key_id, sig_mac)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.RunID, sqlNullable(ev.WorkItemID), ev.TenantID,
		ev.TraceID, ev.SpanID, string(ev.Kind), ev.Key,
		string(ev.PayloadJSON), ev.BlobDigest, sqlNullable(ev.Supersedes),
		ev.WrittenBy, ev.WrittenAt, ev.SchemaVersion, ev.Nonce,
		ev.SigAlg, ev.SigKeyID, ev.SigMAC,
	)
	if err != nil {
		t.Fatalf("raw insert: %v", err)
	}
}

// sqlNullable mirrors substrate.nullableString for the raw-insert helper.
func sqlNullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// TestAuditVerify_LegacyVerdictBackfill_ShowsUnknownLegacy pins the legacy fold path: a pre-#550 verdict missing Tool/Version surfaces as unknown-legacy in audit output.
func TestAuditVerify_LegacyVerdictBackfill_ShowsUnknownLegacy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY_ID", "test-key-1")

	runID := "run-legacy-1"
	at := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	// Legacy payload: pre-#550 shape with only the original fields. No
	// tool / tv / db_v / det. Mirrors what's on disk for verdicts written
	// before the validator started requiring the metadata.
	legacyPayload := []byte(`{"gate_name":"cel-approval","pass":true,"reason":"ok","work_item_id":"WI-L"}`)

	ctx := context.Background()
	writeRawVerdictEvent(t, ctx, dbPath, substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runID,
		WorkItemID:    "WI-L",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindGateVerdict,
		Key:           "cel-approval",
		PayloadJSON:   legacyPayload,
		WrittenBy:     "tester",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         "aa112233445566778899aabbccddeeff",
	})

	var stdout, stderr bytes.Buffer
	code := runAuditVerifyWith(auditDeps{Stdout: &stdout, Stderr: &stderr, DSN: state.DSN(dbPath)},
		[]string{"--run-id", runID, "--format", "json"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var got struct {
		Summary auditVerifySummary `json:"summary"`
		Rows    []auditVerifyRow   `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Rows) != 1 {
		t.Fatalf("rows=%d want 1", len(got.Rows))
	}
	row := got.Rows[0]
	if row.Tool != toolUnknownLegacy {
		t.Errorf("Tool=%q want %s", row.Tool, toolUnknownLegacy)
	}
	if row.ToolVersion != toolUnknownLegacy {
		t.Errorf("ToolVersion=%q want %s", row.ToolVersion, toolUnknownLegacy)
	}
	if row.Deterministic {
		t.Errorf("Deterministic=true want false (legacy is non-replayable by construction)")
	}
	if row.AuditPosture != "verify-only" {
		t.Errorf("AuditPosture=%q want verify-only", row.AuditPosture)
	}
	if row.HMACStatus != hmacStatusChainOK {
		t.Errorf("HMACStatus=%q want %s (chain must verify even for legacy)", row.HMACStatus, hmacStatusChainOK)
	}
	if got.Summary.VerifyOnly != 1 || got.Summary.Reproducible != 0 {
		t.Errorf("summary VerifyOnly=%d Reproducible=%d want 1/0", got.Summary.VerifyOnly, got.Summary.Reproducible)
	}
}

// TestAuditVerify_DecodeError_SurfacesInOutput pins the corrupted-payload fold path: an undecodable verdict surfaces with chain-unverifiable + decode-error tool label.
func TestAuditVerify_DecodeError_SurfacesInOutput(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY_ID", "test-key-1")

	runID := "run-decode-1"
	at := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	// Payload bytes that pass the sqlite length CHECK but fail json
	// decode. signedPayload falls back to string(PayloadJSON) so the
	// HMAC chain still signs+verifies; the audit-verify code path is
	// what tags the row decode-error / verify-only.
	brokenPayload := []byte(`{not json at all`)

	ctx := context.Background()
	writeRawVerdictEvent(t, ctx, dbPath, substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runID,
		WorkItemID:    "WI-D",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindGateVerdict,
		Key:           "broken-gate",
		PayloadJSON:   brokenPayload,
		WrittenBy:     "tester",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         "bb112233445566778899aabbccddeeff",
	})

	var stdout, stderr bytes.Buffer
	code := runAuditVerifyWith(auditDeps{Stdout: &stdout, Stderr: &stderr, DSN: state.DSN(dbPath)},
		[]string{"--run-id", runID, "--format", "json"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var got struct {
		Summary auditVerifySummary `json:"summary"`
		Rows    []auditVerifyRow   `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Rows) != 1 {
		t.Fatalf("rows=%d want 1", len(got.Rows))
	}
	row := got.Rows[0]
	if row.Tool != "decode-error" {
		t.Errorf("Tool=%q want decode-error", row.Tool)
	}
	if row.AuditPosture != "verify-only" {
		t.Errorf("AuditPosture=%q want verify-only", row.AuditPosture)
	}
	if row.Deterministic {
		t.Errorf("Deterministic=true want false (undecodable is non-replayable)")
	}
	if got.Summary.VerifyOnly != 1 || got.Summary.Reproducible != 0 {
		t.Errorf("summary VerifyOnly=%d Reproducible=%d want 1/0", got.Summary.VerifyOnly, got.Summary.Reproducible)
	}
}

// TestAuditVerify_UnsetKeyring_ExitsNonZero pins the keyring-missing contract: REGATTA_AUDIT_HMAC_KEY unset surfaces chain-unverifiable and exits non-zero instead of silently skipping verification.
func TestAuditVerify_UnsetKeyring_ExitsNonZero(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	// Explicitly unset both keyring envs so the test is hermetic against
	// a caller that pre-populated them.
	t.Setenv("REGATTA_AUDIT_HMAC_KEY", "")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY_ID", "")

	runID := "run-nokey-1"
	at := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	// The DB needs at least one row so the fold returns work; otherwise
	// the keyring-missing branch would still trip the non-zero exit, but
	// surfacing chain-unverifiable on an actual row is the load-bearing
	// observable. Plant a row using a writer-side keyring that the
	// audit-CLI does NOT have access to (env is unset).
	t.Setenv("REGATTA_AUDIT_HMAC_KEY", "0123456789abcdef0123456789abcdef")
	p, err := substrate.NewGateVerdictPayload(
		"cel-approval", "WI-K", "cel", "sha:nokey",
		state.CurrentSchemaVersion, true, true, "ok",
	)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	payloadBytes, _ := json.Marshal(p)

	ctx := context.Background()
	writeVerdictEvent(t, ctx, dbPath, substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runID,
		WorkItemID:    "WI-K",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindGateVerdict,
		Key:           "cel-approval",
		PayloadJSON:   payloadBytes,
		WrittenBy:     "tester",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         "cc112233445566778899aabbccddeeff",
	})

	// Now unset the keyring envs so the audit-verify CLI can't load them.
	t.Setenv("REGATTA_AUDIT_HMAC_KEY", "")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY_ID", "")

	var stdout, stderr bytes.Buffer
	code := runAuditVerifyWith(auditDeps{Stdout: &stdout, Stderr: &stderr, DSN: state.DSN(dbPath)},
		[]string{"--run-id", runID, "--format", "json"})
	if code == 0 {
		t.Fatalf("exit=0 want non-zero when keyring unset (stdout=%s stderr=%s)", stdout.String(), stderr.String())
	}
	var got struct {
		Summary auditVerifySummary `json:"summary"`
		Rows    []auditVerifyRow   `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v stdout=%s", err, stdout.String())
	}
	if len(got.Rows) != 1 {
		t.Fatalf("rows=%d want 1", len(got.Rows))
	}
	if got.Rows[0].HMACStatus != "chain-unverifiable" {
		t.Errorf("HMACStatus=%q want chain-unverifiable", got.Rows[0].HMACStatus)
	}
}

// TestAuditVerify_EmptyRunID_EmitsStderrHint asserts unknown run-id gets actionable stderr (R7-Bug-1).
func TestAuditVerify_EmptyRunID_EmitsStderrHint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	t.Setenv("REGATTA_AUDIT_HMAC_KEY", "0123456789abcdef0123456789abcdef")

	ctx := context.Background()
	_, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runAuditVerifyWith(auditDeps{Stdout: &stdout, Stderr: &stderr, DSN: state.DSN(dbPath)},
		[]string{"--run-id", "does-not-exist", "--format", "json"})
	if code != 0 {
		t.Fatalf("exit=%d want 0 on empty run (no rows = nothing to verify, not a failure); stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "does-not-exist") {
		t.Errorf("stderr missing run-id hint: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "no gate verdicts") {
		t.Errorf("stderr missing 'no gate verdicts' hint: %q", stderr.String())
	}
}
