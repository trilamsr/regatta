// resign_test pins the recovery package contract: re-sign rewrites
// only the auth tag, preserves the signed payload, and refuses to
// rewrite rows that fail verify under the supplied keyring.
package substraterecovery_test

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substraterecovery"
)

// TestResignRow_RewritesSigOnly is the round-trip happy path.
func TestResignRow_RewritesSigOnly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "resign.db")
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), func() time.Time { return now })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	substrate.ResetClockForTesting()

	oldKey, _ := hex.DecodeString(strings.Repeat("aa", 32))
	newKey, _ := hex.DecodeString(strings.Repeat("bb", 32))
	tx, _ := db.SQL().Begin()
	ev := substrate.Event{
		ID: substrate.Mint(now), RunID: "r", TenantID: substrate.DefaultTenantID,
		Kind: substrate.KindFact, Key: "fixture",
		PayloadJSON: []byte(`{"key":"fixture","value":{}}`),
		WrittenBy:   "tester", WrittenAt: now.UnixMilli(),
		SchemaVersion: 1, Nonce: strings.Repeat("0", 16),
	}
	if err := substrate.AppendEvent(context.Background(), tx, ev, oldKey, "k_old"); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = tx.Commit()

	tx2, _ := db.SQL().Begin()
	verifyRing := map[string][]byte{"k_old": oldKey, "k_new": newKey}
	if err := substraterecovery.ResignRow(context.Background(), tx2, ev.ID, verifyRing, newKey, "k_new"); err != nil {
		t.Fatalf("resign: %v", err)
	}
	_ = tx2.Commit()

	// Post-condition: row verifies under {k_new} alone.
	out, err := substrate.Fold(context.Background(), db.SQL(), "r", substrate.KindFact)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d rows want 1", len(out))
	}
	if out[0].SigKeyID != "k_new" {
		t.Fatalf("sig_key_id=%q want k_new", out[0].SigKeyID)
	}
	if err := substrate.Verify(out[0], map[string][]byte{"k_new": newKey}); err != nil {
		t.Fatalf("verify under k_new only: %v", err)
	}
}

// TestResignRow_RejectsTamperedRow guards against silent rewrite.
func TestResignRow_RejectsTamperedRow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tamper.db")
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	db, _ := state.OpenWithClock(context.Background(), state.DSN(dbPath), func() time.Time { return now })
	defer func() { _ = db.Close() }()
	substrate.ResetClockForTesting()

	oldKey, _ := hex.DecodeString(strings.Repeat("aa", 32))
	newKey, _ := hex.DecodeString(strings.Repeat("bb", 32))
	tx, _ := db.SQL().Begin()
	ev := substrate.Event{
		ID: substrate.Mint(now), RunID: "r", TenantID: substrate.DefaultTenantID,
		Kind: substrate.KindFact, Key: "fixture",
		PayloadJSON: []byte(`{"key":"fixture","value":{}}`),
		WrittenBy:   "tester", WrittenAt: now.UnixMilli(),
		SchemaVersion: 1, Nonce: strings.Repeat("1", 16),
	}
	_ = substrate.AppendEvent(context.Background(), tx, ev, oldKey, "k_old")
	_ = tx.Commit()

	// Operator forgets k_old material entirely — verifyKeyring carries
	// only k_new. ResignRow MUST refuse.
	tx2, _ := db.SQL().Begin()
	err := substraterecovery.ResignRow(context.Background(), tx2, ev.ID, map[string][]byte{"k_new": newKey}, newKey, "k_new")
	if err == nil {
		t.Fatalf("expected ErrUnverifiable, got nil")
	}
	_ = tx2.Rollback()
}
