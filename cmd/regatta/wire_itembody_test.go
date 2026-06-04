package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildItemBodyLoaderResolvesIDViaFrontmatter asserts the loader returns the brief body when the on-disk frontmatter id matches.
func TestBuildItemBodyLoaderResolvesIDViaFrontmatter(t *testing.T) {
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\nid: WORK-7\ntitle: t\nkind: feature\nlane: docs\nstatus: planned\n---\n\nbrief-marker-7\n\n## Acceptance criteria\n\n- [planned] c1: criterion text\n"
	if err := os.WriteFile(filepath.Join(itemsDir, "anything.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	load := buildItemBodyLoader(dir, nil)
	got, ok := load(context.Background(), "WORK-7")
	if !ok {
		t.Fatal("ItemBodyLoader returned ok=false for present brief")
	}
	if !strings.Contains(got, "brief-marker-7") {
		t.Fatalf("loader body missing marker: %q", got)
	}
}

// TestBuildItemBodyLoaderMissingIDReturnsFalse asserts the loader returns ok=false when no item file matches.
func TestBuildItemBodyLoaderMissingIDReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	load := buildItemBodyLoader(dir, nil)
	if _, ok := load(context.Background(), "DOES-NOT-EXIST"); ok {
		t.Fatal("loader returned ok=true on missing items dir")
	}
}
