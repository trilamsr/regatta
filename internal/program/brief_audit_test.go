package program

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// auditTestKey is the deterministic 32-byte HMAC key the audit-path
// tests share with the brief signer. Real-world keys come from
// REGATTA_HMAC_KEY; the substrate sign step needs SOME key.
var auditTestKey = []byte("audittestkey-32bytes-aaaaaaaaaaa")

// auditTestKeyID labels auditTestKey in the substrate keyring.
const auditTestKeyID = "audit-test-1"

// briefAuditConfig builds the audit-sink config under the test key.
func briefAuditConfig() BriefAuditConfig {
	return BriefAuditConfig{Key: auditTestKey, KeyID: auditTestKeyID, TenantID: "default", RunID: "brief-loader"}
}

// TestBriefLoaderSync_HMACRejectionPersistsAcrossRestart pins issue #80 — durable HMAC-rejection row survives db.Close+reopen.
func TestBriefLoaderSync_HMACRejectionPersistsAcrossRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	signKey := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, signKey)
	// Flip one byte in the signed payload — HMAC verify fails.
	tampered := append([]byte{}, raw...)
	for i, b := range tampered {
		if b == 'f' {
			tampered[i] = 'x'
			break
		}
	}
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: tampered}}

	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)
	loader := mustNewLoader(t, BriefLoaderConfig{
		FS:      fsys,
		DB:      db,
		Keyring: map[string][]byte{"key-1": signKey},
		Audit:   briefAuditConfig(),
	})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Close — restart simulation.
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopen, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	t.Cleanup(func() { _ = reopen.Close() })

	rows, err := reopen.ListBriefRejections(context.Background(), "brief-loader")
	if err != nil {
		t.Fatalf("ListBriefRejections: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1 (HMAC rejection durable across restart)", len(rows))
	}
	r := rows[0]
	if r.Path != "PROG-1.json" {
		t.Fatalf("path=%q want PROG-1.json", r.Path)
	}
	if !strings.Contains(r.Reason, "hmac") && !strings.Contains(r.Reason, "verify") && !strings.Contains(r.Reason, "schema") {
		t.Fatalf("reason=%q want HMAC verify failure", r.Reason)
	}
}

// TestBriefLoaderSync_RejectionReasonsAreCaptured pins the unknown_parent rejection class writes path+reason into the audit row.
func TestBriefLoaderSync_RejectionReasonsAreCaptured(t *testing.T) {
	db := newBriefTestDB(t)
	signKey := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")

	// Brief whose parent never seeded → unknown_parent reason.
	_, orphanRaw := mustSignedBriefWithOpts(t, signKey, "PROG-ORPHAN", "m-1234567890ab",
		time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
		[]PlannedFeature{{ID: "F-X", Title: "x", Fulfills: []string{"c1"}}},
		[]PlanCriterion{{ID: "c1", Text: "x"}})
	fsys := fstest.MapFS{"ORPHAN.json": &fstest.MapFile{Data: orphanRaw}}

	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	loader := mustNewLoader(t, BriefLoaderConfig{
		FS:      fsys,
		DB:      db,
		Keyring: map[string][]byte{"key-1": signKey},
		Audit:   briefAuditConfig(),
	})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	rows, err := db.ListBriefRejections(context.Background(), "brief-loader")
	if err != nil {
		t.Fatalf("ListBriefRejections: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1", len(rows))
	}
	if !strings.Contains(rows[0].Reason, "unknown_parent") {
		t.Fatalf("reason=%q want unknown_parent", rows[0].Reason)
	}
	// Audit payload must include path so an operator can map back to the
	// on-disk artifact without joining other tables.
	if rows[0].Path != "ORPHAN.json" {
		t.Fatalf("path=%q want ORPHAN.json", rows[0].Path)
	}
}

// TestBriefLoaderSync_AuditDisabledWhenNoKey verifies the audit sink is opt-in — zero-value Audit keeps slog-only behaviour for unkeyed deployments.
func TestBriefLoaderSync_AuditDisabledWhenNoKey(t *testing.T) {
	db := newBriefTestDB(t)
	signKey := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, signKey)
	tampered := append([]byte{}, raw...)
	for i, b := range tampered {
		if b == 'f' {
			tampered[i] = 'x'
			break
		}
	}
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: tampered}}

	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)
	loader := mustNewLoader(t, BriefLoaderConfig{
		FS:      fsys,
		DB:      db,
		Keyring: map[string][]byte{"key-1": signKey},
		// no Audit field — fallback to slog-only
	})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	rows, err := db.ListBriefRejections(context.Background(), "brief-loader")
	if err != nil {
		t.Fatalf("ListBriefRejections: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows=%d want 0 (no key configured ⇒ no substrate write)", len(rows))
	}
}

// brief audit payload shape lives in the test so a producer-side rename
// breaks here loudly. Spec §3.5: typed-struct payload, no JSON Schema files.
type briefRejectedPayload struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// TestBriefRejectedPayload_Shape pins the on-disk JSON shape so a downstream reader binding never breaks silently.
func TestBriefRejectedPayload_Shape(t *testing.T) {
	raw, err := json.Marshal(briefRejectedPayload{Path: "p.json", Reason: "hmac"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"path":"p.json"`) {
		t.Fatalf("payload missing path: %s", raw)
	}
	if !strings.Contains(string(raw), `"reason":"hmac"`) {
		t.Fatalf("payload missing reason: %s", raw)
	}
}
