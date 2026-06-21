package main

import (
	"context"
	"testing"
)

// TestSmokeStubMode asserts stub-mode exits clean against the synthetic doc-only + load-bearing inputs (MAY-270).
func TestSmokeStubMode(t *testing.T) {
	if err := stubSmoke(context.Background()); err != nil {
		t.Fatalf("stubSmoke: %v", err)
	}
}
