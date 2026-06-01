package main

import (
	"path/filepath"
	"testing"
)

// TestLintKeyringReadOnly_RejectsRuntimeKeyringSet pins spec §5: KeyringSet outside init/Setup ⇒ finding.
func TestLintKeyringReadOnly_RejectsRuntimeKeyringSet(t *testing.T) {
	findings, err := runLinter(filepath.Join("testdata", "runtime"))
	if err != nil {
		t.Fatalf("runLinter: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected at least one finding for runtime KeyringSet, got 0")
	}
}

// TestLintKeyringReadOnly_AllowsInitAndSetup pins spec §5 negative case: KeyringSet inside boot path ⇒ clean.
func TestLintKeyringReadOnly_AllowsInitAndSetup(t *testing.T) {
	findings, err := runLinter(filepath.Join("testdata", "boot"))
	if err != nil {
		t.Fatalf("runLinter: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected zero findings for boot-time KeyringSet, got %d: %v",
			len(findings), findings)
	}
}
