// keys_test pins the `regatta keys` subcommand contract from
// docs/engineer/specs/2026-06-02-s3-t3-key-rotation-drill.md §3.1 (CLI
// surface) and §3.4 (retire pre-flight against substrate_events).
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestKeysList_PrintsKeyringFromEnv covers `regatta keys list` happy path.
func TestKeysList_PrintsKeyringFromEnv(t *testing.T) {
	hex32 := strings.Repeat("aa", 32)
	hex32b := strings.Repeat("bb", 32)
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING", "k1:"+hex32+",k2:"+hex32b)

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut}, []string{"list"})
	if code != 0 {
		t.Fatalf("exit=%d want 0; stderr=%s", code, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "k1") || !strings.Contains(got, "k2") {
		t.Fatalf("list missing keyIDs: %q", got)
	}
	if !strings.Contains(got, "k2 (active)") {
		t.Fatalf("active marker missing: %q", got)
	}
}

// TestKeysList_EmptyKeyringNonZeroExit pins fail-loud on misconfig.
func TestKeysList_EmptyKeyringNonZeroExit(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING", "")

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut}, []string{"list"})
	if code == 0 {
		t.Fatalf("exit=0 want non-zero on empty keyring; stdout=%s", out.String())
	}
}

// TestKeysRotate_ValidatesNewKeyMaterial rejects weak hex on rotate.
func TestKeysRotate_ValidatesNewKeyMaterial(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	hex32 := strings.Repeat("aa", 32)
	t.Setenv("REGATTA_HMAC_KEYRING", "k1:"+hex32)
	t.Setenv("REGATTA_HMAC_KEY_FRESH", "deadbeef") // 4 bytes — below MinKeyLen

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut},
		[]string{"rotate", "--new-key-env=REGATTA_HMAC_KEY_FRESH", "--new-key-id=k2"})
	if code == 0 {
		t.Fatalf("exit=0 want non-zero on weak key; stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "weak") && !strings.Contains(errOut.String(), "MinKeyLen") {
		t.Fatalf("stderr missing weak-key signal: %q", errOut.String())
	}
}

// TestKeysRotate_RejectsDuplicateKeyID blocks rotation into an existing keyID.
func TestKeysRotate_RejectsDuplicateKeyID(t *testing.T) {
	hex32 := strings.Repeat("aa", 32)
	hex32b := strings.Repeat("bb", 32)
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING", "k1:"+hex32)
	t.Setenv("REGATTA_HMAC_KEY_FRESH", hex32b)

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut},
		[]string{"rotate", "--new-key-env=REGATTA_HMAC_KEY_FRESH", "--new-key-id=k1"})
	if code == 0 {
		t.Fatalf("exit=0 want non-zero on duplicate keyID; stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "duplicate") && !strings.Contains(errOut.String(), "k1") {
		t.Fatalf("stderr missing duplicate-key signal: %q", errOut.String())
	}
}

// TestKeysRotate_PrintsOperatorNextStep emits the env-var append line.
func TestKeysRotate_PrintsOperatorNextStep(t *testing.T) {
	hex32 := strings.Repeat("aa", 32)
	hex32b := strings.Repeat("bb", 32)
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING", "k1:"+hex32)
	t.Setenv("REGATTA_HMAC_KEY_FRESH", hex32b)

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut},
		[]string{"rotate", "--new-key-env=REGATTA_HMAC_KEY_FRESH", "--new-key-id=k2"})
	if code != 0 {
		t.Fatalf("exit=%d want 0; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "REGATTA_HMAC_KEYRING") {
		t.Fatalf("stdout missing keyring env hint: %q", out.String())
	}
	if !strings.Contains(out.String(), "k2:"+hex32b) {
		t.Fatalf("stdout missing new keyring entry: %q", out.String())
	}
}

// TestKeysRetire_BlocksWhenRowsSignedByKey is the pre-flight contract.
func TestKeysRetire_BlocksWhenRowsSignedByKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "retire.db")
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Land one substrate row signed by k1 — retire(k1) MUST refuse.
	substrate.ResetClockForTesting()
	tx, err := db.SQL().Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	keyMat, _ := hex.DecodeString(strings.Repeat("aa", 32))
	ev := substrate.Event{
		ID:            substrate.Mint(now),
		RunID:         "run-1",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindFact,
		Key:           "rotation-drill-fixture",
		PayloadJSON:   []byte(`{"key":"rotation-drill-fixture","value":{}}`),
		WrittenBy:     "tester",
		WrittenAt:     now.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         strings.Repeat("0", 16),
	}
	if err := substrate.AppendEvent(context.Background(), tx, ev, keyMat, "k1"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut, DSN: state.DSN(dbPath)},
		[]string{"retire", "--key-id=k1"})
	if code == 0 {
		t.Fatalf("exit=0 want non-zero — k1 still signs at least one row; stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "k1") {
		t.Fatalf("stderr missing key-id context: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "1") {
		t.Fatalf("stderr missing row count: %q", errOut.String())
	}
}

// TestKeysRetire_AllowsWhenZeroRowsSigned passes when no rows match.
func TestKeysRetire_AllowsWhenZeroRowsSigned(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "retire.db")
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = db.Close()

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut, DSN: state.DSN(dbPath)},
		[]string{"retire", "--key-id=k_never_used"})
	if code != 0 {
		t.Fatalf("exit=%d want 0 on empty substrate; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "safe to retire") {
		t.Fatalf("stdout missing safe-to-retire signal: %q", out.String())
	}
}

// TestKeysRetire_RequiresKeyIDFlag fails fast on missing --key-id.
func TestKeysRetire_RequiresKeyIDFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut, DSN: state.DSN(":memory:")},
		[]string{"retire"})
	if code == 0 {
		t.Fatalf("exit=0 want non-zero on missing --key-id; stdout=%s", out.String())
	}
}

// TestKeys_UnknownSubcommand surfaces usage error.
func TestKeys_UnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut}, []string{"bogus"})
	if code == 0 {
		t.Fatalf("exit=0 want non-zero on bogus subcommand")
	}
}

// TestKeys_NoSubcommand surfaces usage error.
func TestKeys_NoSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut}, nil)
	if code == 0 {
		t.Fatalf("exit=0 want non-zero on empty args")
	}
}
