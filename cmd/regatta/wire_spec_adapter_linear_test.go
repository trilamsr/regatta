package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const linearYAML = `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: linear
  team: MAY
  states: [Todo, "In Progress"]
` + wireSpecAdapterValidGateBlock

// TestBuildSpecAdapter_WiresLinear asserts type=linear builds the Linear adapter when the key is present (MAY-91).
func TestBuildSpecAdapter_WiresLinear(t *testing.T) {
	t.Setenv(envLinearAPIKey, "lin_api_test")
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte(linearYAML), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ad, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp}, logger)
	if err != nil {
		t.Fatalf("buildSpecAdapter: %v", err)
	}
	if ad == nil {
		t.Fatal("ad nil; want non-nil linear adapter")
	}
	if !strings.Contains(buf.String(), "type=linear") {
		t.Errorf("log missing type=linear:\n%s", buf.String())
	}
}

// TestBuildSpecAdapter_LinearMissingKeyFailsClosed asserts a missing LINEAR_API_KEY errors instead of silently falling back (MAY-91).
func TestBuildSpecAdapter_LinearMissingKeyFailsClosed(t *testing.T) {
	t.Setenv(envLinearAPIKey, "")
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte(linearYAML), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	if _, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp}, logger); err == nil {
		t.Fatal("want error on missing LINEAR_API_KEY, got nil")
	}
}
