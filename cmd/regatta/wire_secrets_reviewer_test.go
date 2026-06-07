package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/secrets"
)

// TestBuildSecretFetcherFromRepo_SurfacesYAMLLoadError pins HIGH-1 reviewer finding on #932 — a malformed regatta.yaml MUST NOT silently fall back to Default; it surfaces a WARN record and returns a non-nil error so serve.go can exit 2.
func TestBuildSecretFetcherFromRepo_SurfacesYAMLLoadError(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte("not: valid: yaml: structure\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	_, err := buildSecretFetcherFromRepo(context.Background(), tmp, logger)
	if err == nil {
		t.Fatalf("want error on malformed regatta.yaml, got nil (silent-swallow regression)")
	}
	out := buf.String()
	if !strings.Contains(out, "secrets.config_load_failed") {
		t.Fatalf("log missing secrets.config_load_failed warn record:\n%s", out)
	}
}

// TestBuildSecretFetcherFromRepo_MissingYAMLStaysSilent pins HIGH-1 negative half — zero-config deployments (no regatta.yaml) MUST NOT log a config_load_failed warn and MUST return Default chain.
func TestBuildSecretFetcherFromRepo_MissingYAMLStaysSilent(t *testing.T) {
	tmp := t.TempDir()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f, err := buildSecretFetcherFromRepo(context.Background(), tmp, logger)
	if err != nil {
		t.Fatalf("buildSecretFetcherFromRepo: %v", err)
	}
	if f == nil {
		t.Fatalf("want Default fetcher, got nil")
	}
	out := buf.String()
	if strings.Contains(out, "secrets.config_load_failed") {
		t.Fatalf("zero-config deployment emitted spurious config_load_failed warn:\n%s", out)
	}
}

// TestExportSecretsToEnv_BriefHMAC_FileSourceFormatsKeyring pins HIGH-2 reviewer finding on #932 — file-source brief_hmac with key_id MUST be exported in `keyID:hex,...` keyring format so parseBriefKeyring downstream succeeds.
func TestExportSecretsToEnv_BriefHMAC_FileSourceFormatsKeyring(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	t.Setenv("REGATTA_HMAC_KEYRING", "")
	dir := t.TempDir()
	keyBytes := []byte("0123456789abcdef0123456789abcdef") // 32 raw bytes, well over MinKeyLen
	keyPath := filepath.Join(dir, "brief.key")
	if err := os.WriteFile(keyPath, keyBytes, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := &secrets.Config{
		BriefHMAC: &secrets.Spec{Source: secrets.SourceFile, Path: keyPath, KeyID: "brief-2026-06"},
	}
	f, err := secrets.BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	cache := secrets.NewCache()
	cache.Load(context.Background(), f, nil)
	exportSecretsToEnv(context.Background(), cache, nil)

	got := os.Getenv("REGATTA_HMAC_KEYRING")
	want := "brief-2026-06:" + hex.EncodeToString(keyBytes)
	if got != want {
		t.Fatalf("REGATTA_HMAC_KEYRING = %q, want %q (parseBriefKeyring needs keyID:hex format)", got, want)
	}
	// Belt-and-braces: actually parse it through the production decoder.
	keys, _, perr := parseBriefKeyring(got)
	if perr != nil {
		t.Fatalf("parseBriefKeyring rejected the exported value: %v", perr)
	}
	if _, ok := keys["brief-2026-06"]; !ok {
		t.Fatalf("parseBriefKeyring did not surface the configured keyID: keys=%v", keys)
	}
}

// TestExportSecretsToEnv_SkipsAliasSourced pins MED-5 reviewer finding on #932 — alias-source values MUST NOT be re-exported (would cross schemas, e.g. single-key REGATTA_HMAC_KEY into keyring slot).
func TestExportSecretsToEnv_SkipsAliasSourced(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	// Pre-set legacy single-key env; the alias adapter resolves brief_hmac from it.
	t.Setenv("REGATTA_HMAC_KEY", "legacy-single-key")
	// Pre-clear the keyring slot so we can observe whether export touches it.
	t.Setenv("REGATTA_HMAC_KEYRING", "")
	cache := secrets.NewCache()
	cache.Load(context.Background(), secrets.Default(context.Background()), nil)
	exportSecretsToEnv(context.Background(), cache, nil)

	if got := os.Getenv("REGATTA_HMAC_KEYRING"); got != "" {
		t.Fatalf("REGATTA_HMAC_KEYRING = %q, want empty (alias-sourced values must not bleed into keyring slot)", got)
	}
}
