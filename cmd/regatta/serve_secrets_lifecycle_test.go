package main

import (
	"os"
	"strings"
	"testing"
)

// TestServeWiresSecretsRotationToSignalCtx pins R31-I1: long-lived secrets goroutines (watchSecretsExport + secretCache.Run via startSecretsRotationLoop) MUST use the signal-aware `ctx`, not the boot-scoped bootSecretsCtx (which dies on any boot-error return). Source-scan guard: catches accidental revert.
func TestServeWiresSecretsRotationToSignalCtx(t *testing.T) {
	body, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	src := string(body)
	if !strings.Contains(src, "go watchSecretsExport(ctx,") {
		t.Errorf("watchSecretsExport must be wired to signal-aware ctx, not bootSecretsCtx (R31-I1)")
	}
	if !strings.Contains(src, "startSecretsRotationLoop(ctx,") {
		t.Errorf("startSecretsRotationLoop must be wired to signal-aware ctx (R31-I1)")
	}
}
