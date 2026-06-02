// serve_keyring_test pins the multi-key HMAC parser contract from
// docs/engineer/specs/2026-06-02-s3-t3-key-rotation-drill.md §3.3.
package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestLoadBriefKeyring_LegacySingleKey holds the back-compat path.
func TestLoadBriefKeyring_LegacySingleKey(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEYRING", "")
	t.Setenv("REGATTA_HMAC_KEY", "legacy-key-material-32-bytes-AAAA")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")

	keys, active := loadBriefKeyringWithActive()
	if len(keys) != 1 {
		t.Fatalf("len(keys)=%d want 1", len(keys))
	}
	if !bytes.Equal(keys["k1"], []byte("legacy-key-material-32-bytes-AAAA")) {
		t.Fatalf("keys[k1] wrong: got %q", keys["k1"])
	}
	if active != "k1" {
		t.Fatalf("active=%q want k1", active)
	}
}

// TestLoadBriefKeyring_LegacySingleKeyCustomID covers the legacy override.
func TestLoadBriefKeyring_LegacySingleKeyCustomID(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEYRING", "")
	t.Setenv("REGATTA_HMAC_KEY", "legacy-key-material-32-bytes-AAAA")
	t.Setenv("REGATTA_HMAC_KEY_ID", "rotated-v2")

	keys, active := loadBriefKeyringWithActive()
	if _, ok := keys["rotated-v2"]; !ok {
		t.Fatalf("keys missing rotated-v2: %v", keysIDs(keys))
	}
	if active != "rotated-v2" {
		t.Fatalf("active=%q want rotated-v2", active)
	}
}

// TestLoadBriefKeyring_MultiKey pins colon-comma parse + last-active.
func TestLoadBriefKeyring_MultiKey(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING",
		"k1:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef,"+
			"k2:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210")

	keys, active := loadBriefKeyringWithActive()
	if len(keys) != 2 {
		t.Fatalf("len(keys)=%d want 2 (%v)", len(keys), keysIDs(keys))
	}
	if _, ok := keys["k1"]; !ok {
		t.Fatalf("keys missing k1: %v", keysIDs(keys))
	}
	if _, ok := keys["k2"]; !ok {
		t.Fatalf("keys missing k2: %v", keysIDs(keys))
	}
	if active != "k2" {
		t.Fatalf("active=%q want k2 (last entry)", active)
	}
}

// TestLoadBriefKeyring_MultiKeyReverseOrder pins order-matters semantics.
func TestLoadBriefKeyring_MultiKeyReverseOrder(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")
	t.Setenv("REGATTA_HMAC_KEYRING",
		"k2:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210,"+
			"k1:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	_, active := loadBriefKeyringWithActive()
	if active != "k1" {
		t.Fatalf("active=%q want k1 (last entry, even reverse-named)", active)
	}
}

// TestLoadBriefKeyring_MultiKeyExplicitActive holds REGATTA_HMAC_KEY_ID override.
func TestLoadBriefKeyring_MultiKeyExplicitActive(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "k1")
	t.Setenv("REGATTA_HMAC_KEYRING",
		"k1:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef,"+
			"k2:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210")

	_, active := loadBriefKeyringWithActive()
	if active != "k1" {
		t.Fatalf("active=%q want k1 (explicit override)", active)
	}
}

// TestLoadBriefKeyring_EmptyReturnsEmpty keeps the no-config boot path.
func TestLoadBriefKeyring_EmptyReturnsEmpty(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEYRING", "")
	t.Setenv("REGATTA_HMAC_KEY_ID", "")

	keys, active := loadBriefKeyringWithActive()
	if len(keys) != 0 {
		t.Fatalf("len(keys)=%d want 0", len(keys))
	}
	if active != "" {
		t.Fatalf("active=%q want empty", active)
	}
}

// TestParseBriefKeyring_DuplicateRejected blocks silent overwrite per spec §3.3.
func TestParseBriefKeyring_DuplicateRejected(t *testing.T) {
	_, _, err := parseBriefKeyring(
		"k1:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef," +
			"k1:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210")
	if err == nil {
		t.Fatalf("want error for duplicate keyID, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err=%v want contains 'duplicate'", err)
	}
}

// TestParseBriefKeyring_MalformedEntryRejected blocks colon-less pairs.
func TestParseBriefKeyring_MalformedEntryRejected(t *testing.T) {
	_, _, err := parseBriefKeyring("k1deadbeef")
	if err == nil {
		t.Fatalf("want error for malformed entry, got nil")
	}
}

// TestParseBriefKeyring_WeakKeyRejected blocks <MinKeyLen material.
func TestParseBriefKeyring_WeakKeyRejected(t *testing.T) {
	_, _, err := parseBriefKeyring("k1:deadbeef")
	if err == nil {
		t.Fatalf("want error for weak key, got nil")
	}
}

// keysIDs is a test-helper that lists keyring IDs for diagnostics.
func keysIDs(m map[string][]byte) []string {
	ids := make([]string, 0, len(m))
	for k := range m {
		ids = append(ids, k)
	}
	return ids
}
