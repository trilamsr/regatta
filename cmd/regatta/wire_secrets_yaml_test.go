package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/secrets"
)

// TestBuildSecretFetcher_FileSource_FullBoot wires regatta.yaml → loader → Fetcher → Get end-to-end (8.7, #911).
func TestBuildSecretFetcher_FileSource_FullBoot(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ak.key")
	if err := os.WriteFile(keyPath, []byte("from-file-integration\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := &validate.Config{
		Secrets: &validate.Secrets{
			AnthropicAPIKey: &validate.Secret{Source: "file", Path: keyPath},
		},
	}
	f, err := buildSecretFetcher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildSecretFetcher: %v", err)
	}
	v, err := f.Get(context.Background(), secrets.KeyAnthropic)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Bytes()) != "from-file-integration" {
		t.Fatalf("got %q want from-file-integration", v.Bytes())
	}
}

// TestBuildSecretFetcher_NoConfig_BackCompat asserts nil config returns the Default chain (back-compat) (#911).
func TestBuildSecretFetcher_NoConfig_BackCompat(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ANTHROPIC_API_KEY", "back-compat-integration")
	f, err := buildSecretFetcher(context.Background(), nil)
	if err != nil {
		t.Fatalf("buildSecretFetcher: %v", err)
	}
	v, err := f.Get(context.Background(), secrets.KeyAnthropic)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Bytes()) != "back-compat-integration" {
		t.Fatalf("got %q want back-compat-integration", v.Bytes())
	}
}
