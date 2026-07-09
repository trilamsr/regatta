package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureDBParent_CreatesMissingParent asserts nested parents are MkdirAll'd on first boot (wave-a bug 8).
func TestEnsureDBParent_CreatesMissingParent(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "deep", "nested", "regatta.db")
	if err := ensureDBParent(dbPath); err != nil {
		t.Fatalf("ensureDBParent: %v", err)
	}
	parent := filepath.Dir(dbPath)
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("stat parent %q: %v", parent, err)
	}
	if !info.IsDir() {
		t.Fatalf("parent %q is not a directory", parent)
	}
}

// TestEnsureDBParent_ExistingParentIsNoop asserts idempotency on an already-present dir (wave-a bug 8).
func TestEnsureDBParent_ExistingParentIsNoop(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "regatta.db")
	if err := ensureDBParent(dbPath); err != nil {
		t.Fatalf("ensureDBParent: %v", err)
	}
	if err := ensureDBParent(dbPath); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

// TestEnsureDBParent_ErrorMentionsParent asserts the error names the parent path so the operator can debug (wave-a bug 8).
func TestEnsureDBParent_ErrorMentionsParent(t *testing.T) {
	// Force MkdirAll to fail by placing the DB "under" a regular file: the
	// mid-path component points at a file, so MkdirAll returns "not a
	// directory". Any actionable error MUST name the parent path so the
	// operator knows which mount to fix.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	dbPath := filepath.Join(blocker, "nested", "regatta.db")
	err := ensureDBParent(dbPath)
	if err == nil {
		t.Fatal("ensureDBParent err=nil; expected failure when parent path traverses a regular file")
	}
	parent := filepath.Dir(dbPath)
	if !strings.Contains(err.Error(), parent) {
		t.Errorf("err=%q does not mention parent %q", err.Error(), parent)
	}
}

// TestEnsureDBParent_EmptyPathRejected asserts an empty path is a caller bug reported as actionable error (wave-a bug 8).
func TestEnsureDBParent_EmptyPathRejected(t *testing.T) {
	if err := ensureDBParent(""); err == nil {
		t.Fatal("ensureDBParent(\"\") err=nil; expected failure")
	}
}
