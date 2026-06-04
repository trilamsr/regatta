package substrate_test

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestNewSweeper_RejectsNilRODB pins caller-injection contract (G2).
func TestNewSweeper_RejectsNilRODB(t *testing.T) {
	_, err := substrate.NewSweeper(substrate.SweeperConfig{
		RODB:    nil,
		Keyring: testKeyring(),
	})
	if err == nil {
		t.Fatal("want error on nil RODB, got nil")
	}
}

// TestSweepFullChain_RejectsNilRODB pins caller-injection contract (G2).
func TestSweepFullChain_RejectsNilRODB(t *testing.T) {
	_, err := substrate.SweepFullChain(context.Background(), substrate.FullChainConfig{
		RODB:    nil,
		Keyring: testKeyring(),
	})
	if err == nil {
		t.Fatal("want error on nil RODB, got nil")
	}
}
