package config

import (
	"context"
	"testing"
)

// TestVerifyRun_RequiresConfig asserts empty VerifyConfig returns an error.
func TestVerifyRun_RequiresConfig(t *testing.T) {
	_, err := VerifyRun(context.Background(), VerifyConfig{})
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}

// TestVerifyRun_RequiresToken asserts VerifyRun errs when GITHUB_TOKEN is unset.
func TestVerifyRun_RequiresToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	_, err := VerifyRun(context.Background(), VerifyConfig{Owner: "x", Repo: "y"})
	if err == nil {
		t.Fatal("expected error when no token")
	}
}
