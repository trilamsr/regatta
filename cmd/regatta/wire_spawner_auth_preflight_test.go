package main

import (
	"os"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// TestPreflightSpawnerAuth_StripDefaultRefusesWhenNoClaudeDir pins #1166-3: when REGATTA_SPAWNER_STRIP_API_KEY is unset or "1" (subscription path) AND no readable subscription credential is reachable, the orchestrator must refuse to boot loudly instead of letting the scheduler burn through the work-item queue with auth_precondition_failed (the failure mode that prompted #1166).
func TestPreflightSpawnerAuth_StripDefaultRefusesWhenNoClaudeDir(t *testing.T) {
	t.Setenv("REGATTA_SPAWNER_STRIP_API_KEY", "1")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")
	err := preflightSpawnerAuth("claude")
	if err == nil {
		t.Fatal("preflightSpawnerAuth(claude) with strip=1 and no $HOME/.claude must refuse to boot")
	}
	if !strings.Contains(err.Error(), "subscription") && !strings.Contains(err.Error(), "auth") {
		t.Fatalf("error must name the auth precondition; got %q", err.Error())
	}
}

// TestPreflightSpawnerAuth_OptOutPassesWhenAPIKeyPresent: explicit pay-as-you-go (STRIP=0) + ANTHROPIC_API_KEY set in env → boot passes; the spawner has a credential path.
func TestPreflightSpawnerAuth_OptOutPassesWhenAPIKeyPresent(t *testing.T) {
	t.Setenv("REGATTA_SPAWNER_STRIP_API_KEY", "0")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-fixture")
	if err := preflightSpawnerAuth("claude"); err != nil {
		t.Fatalf("preflightSpawnerAuth(claude) with strip=0 + ANTHROPIC_API_KEY set must boot; got %v", err)
	}
}

// TestPreflightSpawnerAuth_OptOutRefusesWhenAPIKeyEmpty: explicit pay-as-you-go (STRIP=0) but ANTHROPIC_API_KEY empty → still refuses. There is no credential path at all.
func TestPreflightSpawnerAuth_OptOutRefusesWhenAPIKeyEmpty(t *testing.T) {
	t.Setenv("REGATTA_SPAWNER_STRIP_API_KEY", "0")
	t.Setenv("ANTHROPIC_API_KEY", "")
	err := preflightSpawnerAuth("claude")
	if err == nil {
		t.Fatal("preflightSpawnerAuth(claude) with strip=0 + empty ANTHROPIC_API_KEY must refuse to boot")
	}
}

// TestPreflightSpawnerAuth_StripPassesWhenClaudeDirReachable: strip=1 (subscription) + ~/.claude exists + readable → boot passes. Heuristic: existence + readability of $HOME/.claude is the proxy for "subscription credentials are in reach"; a deeper probe (claude --version) is out of scope at this gate.
func TestPreflightSpawnerAuth_StripPassesWhenClaudeDirReachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REGATTA_SPAWNER_STRIP_API_KEY", "1")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home+"/.claude", 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := preflightSpawnerAuth("claude"); err != nil {
		t.Fatalf("preflightSpawnerAuth(claude) with strip=1 + readable $HOME/.claude must boot; got %v", err)
	}
}

// TestPreflightSpawnerAuth_NonClaudeSpawnerSkipped: for spawner=stub (smoke tests) the auth gate is a no-op — no claude CLI to authenticate.
func TestPreflightSpawnerAuth_NonClaudeSpawnerSkipped(t *testing.T) {
	t.Setenv("REGATTA_SPAWNER_STRIP_API_KEY", "1")
	t.Setenv("HOME", t.TempDir())
	if err := preflightSpawnerAuth("stub"); err != nil {
		t.Fatalf("preflightSpawnerAuth(stub) must skip regardless of env; got %v", err)
	}
}

// TestPreflightSpawnerAuth_StripFlag_UsesSharedFalsyLexicon pins that the boundary gate classifies REGATTA_SPAWNER_STRIP_API_KEY via spawner.IsFalsyEnv (exported per #1166-fix3 reviewer round-1). If the export drifts back to unexported, this test fails compile.
func TestPreflightSpawnerAuth_StripFlag_UsesSharedFalsyLexicon(t *testing.T) {
	if spawner.IsFalsyEnv("0") != true || spawner.IsFalsyEnv("1") != false {
		t.Fatalf("spawner.IsFalsyEnv lexicon drifted")
	}
}
