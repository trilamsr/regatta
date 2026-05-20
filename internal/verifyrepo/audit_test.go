package verifyrepo

import (
	"context"
	"testing"
)

func TestRun_RequiresConfig(t *testing.T) {
	_, err := Run(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}

func TestRun_RequiresToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	_, err := Run(context.Background(), Config{Owner: "x", Repo: "y"})
	if err == nil {
		t.Fatal("expected error when no token")
	}
}
