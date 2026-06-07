package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSecrets_BuildFromConfig_EnvSource asserts a yaml-configured env source resolves to os.Getenv (8.1, #911).
func TestSecrets_BuildFromConfig_EnvSource(t *testing.T) {
	t.Setenv("REGATTA_TEST_FOO", "from-env")
	cfg := &Config{
		AnthropicAPIKey: &Spec{Source: SourceEnv, Name: "REGATTA_TEST_FOO"},
	}
	f, err := BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	v, err := f.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Bytes()) != "from-env" {
		t.Fatalf("got %q want from-env", v.Bytes())
	}
}

// TestSecrets_BuildFromConfig_FallsBackToDefault asserts an empty config delegates to Default chain (8.2, #911).
func TestSecrets_BuildFromConfig_FallsBackToDefault(t *testing.T) {
	t.Setenv(disableEnv, "1")
	t.Setenv("ANTHROPIC_API_KEY", "back-compat")
	f, err := BuildFromConfig(context.Background(), nil)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	v, err := f.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Bytes()) != "back-compat" {
		t.Fatalf("got %q want back-compat", v.Bytes())
	}
}

// TestSecrets_BuildFromConfig_FileSource_PermsTooOpen asserts 0644 file source fails closed (8.3, #911).
func TestSecrets_BuildFromConfig_FileSource_PermsTooOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("leakable"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := &Config{
		AnthropicAPIKey: &Spec{Source: SourceFile, Path: path},
	}
	f, err := BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	_, err = f.Get(context.Background(), KeyAnthropic)
	if err == nil {
		t.Fatalf("want perms error, got nil")
	}
}

// TestSecrets_BuildFromConfig_FileSource_OK asserts a 0600 file source resolves contents trimmed (#911).
func TestSecrets_BuildFromConfig_FileSource_OK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := &Config{
		AnthropicAPIKey: &Spec{Source: SourceFile, Path: path},
	}
	f, err := BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	v, err := f.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Bytes()) != "from-file" {
		t.Fatalf("got %q want from-file (trimmed)", v.Bytes())
	}
}

// TestSecrets_KeyringMigration_BackCompat asserts no secrets block keeps the legacy REGATTA_HMAC_KEY env path alive (8.4, #911).
func TestSecrets_KeyringMigration_BackCompat(t *testing.T) {
	t.Setenv(disableEnv, "1")
	t.Setenv("REGATTA_HMAC_KEY", "legacy-hmac")
	f, err := BuildFromConfig(context.Background(), nil)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	v, err := f.Get(context.Background(), KeyBriefHMACs)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Bytes()) != "legacy-hmac" {
		t.Fatalf("got %q want legacy-hmac (back-compat alias)", v.Bytes())
	}
}

// TestSecrets_YamlSourceOverridesEnv asserts a yaml-configured env name beats the raw legacy env name (8.5, #911).
func TestSecrets_YamlSourceOverridesEnv(t *testing.T) {
	t.Setenv(disableEnv, "1")
	t.Setenv("ANTHROPIC_API_KEY", "legacy")
	t.Setenv("MY_OPERATOR_KEY", "operator-chosen")
	cfg := &Config{
		AnthropicAPIKey: &Spec{Source: SourceEnv, Name: "MY_OPERATOR_KEY"},
	}
	f, err := BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	v, err := f.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Bytes()) != "operator-chosen" {
		t.Fatalf("got %q want operator-chosen (yaml beats legacy env)", v.Bytes())
	}
}

// TestSecrets_BuildFromConfig_UnknownSourceRejected asserts the loader hard-fails on unknown source enums (#911).
func TestSecrets_BuildFromConfig_UnknownSourceRejected(t *testing.T) {
	cfg := &Config{
		AnthropicAPIKey: &Spec{Source: "vault", Name: "x"},
	}
	_, err := BuildFromConfig(context.Background(), cfg)
	if err == nil {
		t.Fatalf("want error for unknown source, got nil")
	}
}

// TestSecrets_FileFetcher_MissingFile asserts a missing path is ErrNotFound (chain continues) (#911).
func TestSecrets_FileFetcher_MissingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms only")
	}
	f := NewFileFetcher(KeyAnthropic, filepath.Join(t.TempDir(), "no-such-file"))
	_, err := f.Get(context.Background(), KeyAnthropic)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v want ErrNotFound", err)
	}
}
