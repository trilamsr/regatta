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

// landRowSignedBy appends one substrate_events row signed by keyID with
// the supplied raw key material. Used by recover tests below to plant a
// fixture row whose key was "lost" before the test runs.
func landRowSignedBy(t *testing.T, dbPath, keyID string, keyHex string) {
	t.Helper()
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	substrate.ResetClockForTesting()
	tx, err := db.SQL().Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	keyMat, _ := hex.DecodeString(keyHex)
	ev := substrate.Event{
		ID:            substrate.Mint(now),
		RunID:         "run-recover",
		TenantID:      substrate.DefaultTenantID,
		Kind:          substrate.KindFact,
		Key:           "recover-fixture-" + keyID,
		PayloadJSON:   []byte(`{"key":"recover-fixture","value":{}}`),
		WrittenBy:     "tester",
		WrittenAt:     now.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         strings.Repeat("0", 15) + keyID[len(keyID)-1:],
	}
	if err := substrate.AppendEvent(context.Background(), tx, ev, keyMat, keyID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestKeysRecover_ResignsRowsUnderActiveKey is the recovery happy path.
func TestKeysRecover_ResignsRowsUnderActiveKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recover.db")
	oldHex := strings.Repeat("aa", 32)
	newHex := strings.Repeat("bb", 32)
	landRowSignedBy(t, dbPath, "k_old", oldHex)

	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING", "k_new:"+newHex)
	t.Setenv("K_OLD_BACKUP", oldHex)

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut, DSN: state.DSN(dbPath)},
		[]string{"recover", "--extra-key=k_old:K_OLD_BACKUP"})
	if code != 0 {
		t.Fatalf("exit=%d want 0; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "1") {
		t.Fatalf("stdout missing resigned row count: %q", out.String())
	}

	// Post-condition: retire(k_old) MUST now pass since recover rewrote
	// every k_old-signed row's sig_key_id to k_new.
	var out2, err2 bytes.Buffer
	code2 := runKeysWith(keysDeps{Stdout: &out2, Stderr: &err2, DSN: state.DSN(dbPath)},
		[]string{"retire", "--key-id=k_old"})
	if code2 != 0 {
		t.Fatalf("retire after recover failed: exit=%d stderr=%s", code2, err2.String())
	}
	if !strings.Contains(out2.String(), "safe to retire") {
		t.Fatalf("retire stdout missing safe signal: %q", out2.String())
	}
}

// TestKeysRecover_WithoutExtraKey_LeavesRowsUnchanged is the no-bypass guard.
func TestKeysRecover_WithoutExtraKey_LeavesRowsUnchanged(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recover-noextra.db")
	oldHex := strings.Repeat("aa", 32)
	newHex := strings.Repeat("bb", 32)
	landRowSignedBy(t, dbPath, "k_old", oldHex)

	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING", "k_new:"+newHex)

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut, DSN: state.DSN(dbPath)},
		[]string{"recover"})
	// Without extra keys, k_old rows cannot be verified — recover MUST
	// either refuse (exit 1) or report 0 resigned. Either way, retire
	// pre-flight still fails because the row's sig_key_id is unchanged.
	_ = code

	var out2, err2 bytes.Buffer
	code2 := runKeysWith(keysDeps{Stdout: &out2, Stderr: &err2, DSN: state.DSN(dbPath)},
		[]string{"retire", "--key-id=k_old"})
	if code2 == 0 {
		t.Fatalf("retire passed without recover ever supplying k_old material; auto-bypass leak: stdout=%s", out2.String())
	}
}

// TestKeysRecover_RejectsUnverifiableRowsWithoutKey blocks silent rewrite.
func TestKeysRecover_RejectsUnverifiableRowsWithoutKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recover-reject.db")
	oldHex := strings.Repeat("aa", 32)
	newHex := strings.Repeat("bb", 32)
	landRowSignedBy(t, dbPath, "k_old", oldHex)

	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING", "k_new:"+newHex)

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut, DSN: state.DSN(dbPath)},
		[]string{"recover"})
	if code == 0 {
		t.Fatalf("exit=0 want non-zero — recover MUST refuse when rows fail verify under supplied keyring; stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "k_old") && !strings.Contains(errOut.String(), "unverifiable") {
		t.Fatalf("stderr missing unverifiable-row signal: %q", errOut.String())
	}
}

// TestKeysRecover_DryRunDoesNotMutate previews without writing.
func TestKeysRecover_DryRunDoesNotMutate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recover-dry.db")
	oldHex := strings.Repeat("aa", 32)
	newHex := strings.Repeat("bb", 32)
	landRowSignedBy(t, dbPath, "k_old", oldHex)

	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING", "k_new:"+newHex)
	t.Setenv("K_OLD_BACKUP", oldHex)

	var out, errOut bytes.Buffer
	code := runKeysWith(keysDeps{Stdout: &out, Stderr: &errOut, DSN: state.DSN(dbPath)},
		[]string{"recover", "--extra-key=k_old:K_OLD_BACKUP", "--dry-run"})
	if code != 0 {
		t.Fatalf("dry-run exit=%d want 0; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "dry-run") && !strings.Contains(out.String(), "would") {
		t.Fatalf("dry-run stdout missing preview marker: %q", out.String())
	}

	// Retire pre-flight MUST still fail — dry-run didn't mutate.
	var out2, err2 bytes.Buffer
	code2 := runKeysWith(keysDeps{Stdout: &out2, Stderr: &err2, DSN: state.DSN(dbPath)},
		[]string{"retire", "--key-id=k_old"})
	if code2 == 0 {
		t.Fatalf("retire passed after dry-run; dry-run mutated DB: stdout=%s", out2.String())
	}
}
