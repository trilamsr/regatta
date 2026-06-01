package main

import (
	"path/filepath"
	"testing"
)

// TestLintKeyringReadOnly_RejectsRuntimeKeyringSet pins spec §5
// keyring-readonly defense. A KeyringSet-shaped call outside init() /
// Setup() must trip the lint.
func TestLintKeyringReadOnly_RejectsRuntimeKeyringSet(t *testing.T) {
	findings, err := runLinter(filepath.Join("testdata", "runtime"))
	if err != nil {
		t.Fatalf("runLinter: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected at least one finding for runtime KeyringSet, got 0")
	}
}

// Negative case: KeyringSet inside init() / Setup() is the legitimate
// boot-time path and must NOT trip the lint.
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
