package secrets

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRoutedFetcher_FallsBackToDefaultOnRoutedMiss pins MED-4 reviewer finding on #932 — when the routed source returns ErrNotFound, routedFetcher MUST consult the Default chain so an operator override that wires source=env+name to an unset env var still resolves via legacy aliases (back-compat).
func TestRoutedFetcher_FallsBackToDefaultOnRoutedMiss(t *testing.T) {
	t.Setenv(disableEnv, "1")
	// Legacy env name (consumed by aliasEnvFetcher) is set; the routed
	// source is wired to a different env name which is NOT set, so
	// the routed lookup MUST miss and fallback MUST resolve.
	t.Setenv("ANTHROPIC_API_KEY", "from-legacy-alias")
	t.Setenv("ROUTED_MISSING_ENV", "")
	cfg := &Config{
		AnthropicAPIKey: &SecretSpec{Source: SourceEnv, Name: "ROUTED_MISSING_ENV"},
	}
	f, err := BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	v, err := f.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("Get: %v (want Default-chain fallback when routed source misses)", err)
	}
	if string(v.Bytes()) != "from-legacy-alias" {
		t.Fatalf("got %q want from-legacy-alias (Default-chain fallback expected on routed miss)", v.Bytes())
	}
}

// TestBuildFromConfig_BriefHMAC_FileSource_HexFormatsKeyring pins HIGH-2 at the loader layer — file-source brief_hmac with key_id MUST surface as `keyID:hex(raw)` so the value is parseable by parseBriefKeyring at the export boundary.
func TestBuildFromConfig_BriefHMAC_FileSource_HexFormatsKeyring(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms only")
	}
	dir := t.TempDir()
	raw := []byte("0123456789abcdef0123456789abcdef")
	keyPath := filepath.Join(dir, "brief.key")
	if err := os.WriteFile(keyPath, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := &Config{
		BriefHMAC: &SecretSpec{Source: SourceFile, Path: keyPath, KeyID: "brief-2026-06"},
	}
	f, err := BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	v, err := f.Get(context.Background(), KeyBriefHMACs)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got := string(v.Bytes())
	want := "brief-2026-06:" + hex.EncodeToString(raw)
	if got != want {
		t.Fatalf("brief_hmac value = %q, want %q (keyring format expected)", got, want)
	}
}

// TestBuildFromConfig_BriefHMAC_FileSource_MissingKeyIDRejected pins HIGH-2 at the loader layer — file-source brief_hmac with no key_id is a misconfig (would produce an empty/unparseable keyring); construction MUST hard-fail.
func TestBuildFromConfig_BriefHMAC_FileSource_MissingKeyIDRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms only")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "brief.key")
	if err := os.WriteFile(keyPath, []byte("raw-no-keyid"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := &Config{
		BriefHMAC: &SecretSpec{Source: SourceFile, Path: keyPath}, // no KeyID
	}
	if _, err := BuildFromConfig(context.Background(), cfg); err == nil {
		t.Fatalf("want error on file-source brief_hmac without key_id, got nil")
	}
}
