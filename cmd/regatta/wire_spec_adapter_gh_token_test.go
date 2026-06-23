package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildSpecAdapter_GhTokenRequired_FailsAtBoot asserts the github_issues adapter refuses to boot when GH_TOKEN+GITHUB_TOKEN are unset (R-MEGA-3 LIVE-6).
func TestBuildSpecAdapter_GhTokenRequired_FailsAtBoot(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	yaml := `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:autonomous"
` + wireSpecAdapterValidGateBlock

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))
	if err == nil {
		t.Fatal("buildSpecAdapter returned nil err; want fail-closed when GH_TOKEN unset")
	}
	if !strings.Contains(err.Error(), "GH_TOKEN") {
		t.Errorf("err must name GH_TOKEN; got %v", err)
	}
}

// TestBuildSpecAdapter_GhTokenAcceptsAlt asserts GITHUB_TOKEN satisfies the gate (alt-env-name compat, mirrors wire_secrets aliases).
func TestBuildSpecAdapter_GhTokenAcceptsAlt(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "alt-name-ok")
	yaml := `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:autonomous"
` + wireSpecAdapterValidGateBlock
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))); err != nil {
		t.Fatalf("buildSpecAdapter with GITHUB_TOKEN should succeed: %v", err)
	}
}
