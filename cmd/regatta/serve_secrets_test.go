package main

import (
	"context"
	"os"
	"testing"

	"github.com/trilamsr/regatta/internal/secrets"
)

// TestExportSecretsToEnv_PopulatesLegacyEnvNames asserts cache→env wiring lights up the legacy readers (loadBriefKeyring etc).
func TestExportSecretsToEnv_PopulatesLegacyEnvNames(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "sk-from-env")
	// Pre-clear the legacy name so we can observe the export setting it.
	t.Setenv("ANTHROPIC_API_KEY", "")

	cache := secrets.NewCache()
	cache.Load(context.Background(), secrets.Default(context.Background()), nil)
	exportSecretsToEnv(context.Background(), cache, nil)

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-from-env" {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want sk-from-env", got)
	}
}

// TestExportSecretsToEnv_DoesNotOverwriteOperatorSetEnv pins the precedence rule per spec §5 — operator-typed env wins.
func TestExportSecretsToEnv_DoesNotOverwriteOperatorSetEnv(t *testing.T) {
	t.Setenv("REGATTA_SECRETS_DISABLE_KEYCHAIN", "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "sk-from-cache")
	t.Setenv("ANTHROPIC_API_KEY", "sk-pre-set")

	cache := secrets.NewCache()
	cache.Load(context.Background(), secrets.Default(context.Background()), nil)
	exportSecretsToEnv(context.Background(), cache, nil)

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-pre-set" {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want operator-set sk-pre-set", got)
	}
}
