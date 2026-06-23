package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAtomicWriteBrief_DryRun asserts dry-run mode leaves the filesystem untouched (R-MEGA-3 LIVE-18).
func TestAtomicWriteBrief_DryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.json")
	data := []byte(`{"program_id":"P-LIVE18"}`)
	if err := atomicWriteBriefDryRun(path, data, false, true); err != nil {
		t.Fatalf("dry-run returned err: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("dry-run wrote file: stat err=%v", err)
	}
}

// TestAtomicWriteBrief_DryRun_Force asserts dry-run + existing file + force still does not write.
func TestAtomicWriteBrief_DryRun_Force(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.json")
	original := []byte(`{"orig":true}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	updated := []byte(`{"program_id":"P-LIVE18"}`)
	if err := atomicWriteBriefDryRun(path, updated, true, true); err != nil {
		t.Fatalf("dry-run+force returned err: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Errorf("dry-run modified file: got=%q want=%q", got, original)
	}
}

// TestAtomicWriteBrief_DryRunIdempotent asserts dry-run on identical content reports no-op without error.
func TestAtomicWriteBrief_DryRunIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.json")
	data := []byte(`{"program_id":"P-LIVE18"}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := atomicWriteBriefDryRun(path, data, false, true); err != nil {
		t.Errorf("dry-run idempotent: %v", err)
	}
}

// TestKeysResign_DryRunNoWrites asserts the existing --dry-run flag on `keys re-sign-briefs` leaves brief files untouched.
func TestKeysResign_DryRunNoWrites(t *testing.T) {
	dir := t.TempDir()
	briefPath := filepath.Join(dir, "P-1.json")
	original := []byte(`{"program_id":"P-1","signature":{"alg":"hmac-sha256","key_id":"k1","mac":"DEAD"}}`)
	if err := os.WriteFile(briefPath, original, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// keys re-sign-briefs uses resignOneBrief; a dry-run on a wrong-key brief
	// must not overwrite the file. We invoke the helper directly to keep
	// the test deterministic without env/key setup.
	_, _ = resignOneBrief(briefPath, "k1", []byte("0123456789abcdef0123456789abcdef"), "k2", []byte("fedcba9876543210fedcba9876543210"), true)
	got, _ := os.ReadFile(briefPath)
	if string(got) != string(original) {
		t.Errorf("dry-run modified brief: got=%q want=%q", got, original)
	}
}
