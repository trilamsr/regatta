package substrate_test

import (
	"context"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// testKey is a deterministic 32-byte HMAC key. Tests use the same
// key for sign + verify; production keys live in the keyring loaded
// at Setup().
var testKey = []byte("0123456789abcdef0123456789abcdef")

// testKeyID labels the testKey in the keyring.
const testKeyID = "test-key-1"

// testKeyring returns the keyring shape Verify() consumes.
func testKeyring() map[string][]byte {
	return map[string][]byte{testKeyID: testKey}
}

// openMigratedDB opens a fresh sqlite DB at a temp path with all
// migrations applied. Resets the substrate process-local clock-
// regression watermark so test order does not bleed.
func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "subs.db")
	raw, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	raw.SetMaxOpenConns(1)
	if err := state.Migrate(context.Background(), raw); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	substrate.ResetClockForTesting()
	return raw
}

// fixedNonce returns a deterministic 16-byte hex nonce keyed by seed.
// Tests use fresh seeds per event so the UNIQUE(run_id, written_by,
// nonce) collision path can be exercised deliberately.
func fixedNonce(seed byte) string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = seed
	}
	return hex.EncodeToString(b)
}

// withinTx runs fn inside a fresh tx; commits on nil error else rolls back.
func withinTx(ctx context.Context, t *testing.T, db *sql.DB, fn func(*sql.Tx) error) error {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// appendEventTx is the standard test path: open a tx, call AppendEvent,
// commit (or rollback on error). Keeps each test free of boilerplate.
func appendEventTx(ctx context.Context, t *testing.T, db *sql.DB, e substrate.Event) error {
	t.Helper()
	return withinTx(ctx, t, db, func(tx *sql.Tx) error {
		return substrate.AppendEvent(ctx, tx, e, testKey, testKeyID)
	})
}

// testTime returns a fixed UTC timestamp tests share so ULID and
// clock-monotonicity invariants stay deterministic across cases.
func testTime() time.Time {
	return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
}

// testCtx returns context.Background — kept as a helper so tests read
// uniformly and a future ctx-with-timeout swap is one-line.
func testCtx() context.Context {
	return context.Background()
}

// mkEvent builds a baseline valid Event the tests mutate per case.
func mkEvent(seed byte, runID string, kind substrate.EventKind, payload string, at time.Time) substrate.Event {
	return substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runID,
		TenantID:      substrate.DefaultTenantID,
		Kind:          kind,
		PayloadJSON:   []byte(payload),
		WrittenBy:     "tester",
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         fixedNonce(seed),
	}
}
