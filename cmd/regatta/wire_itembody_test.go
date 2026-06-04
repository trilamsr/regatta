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

// TestBuildItemBodyLoaderRejectsSymlinkOutsideItemsDir asserts symlinks under .regatta/items are skipped so out-of-tree files do not leak (#838).
func TestBuildItemBodyLoaderRejectsSymlinkOutsideItemsDir(t *testing.T) {
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "linked.md")
	outsideBody := "---\nid: WORK-LEAK\ntitle: t\nkind: feature\nlane: docs\nstatus: planned\n---\n\noutside-tree-marker\n\n## Acceptance criteria\n\n- [planned] c1: criterion text\n"
	if err := os.WriteFile(outsidePath, []byte(outsideBody), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(itemsDir, "leak.md")); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	load := buildItemBodyLoader(dir, nil)
	if got, ok := load(context.Background(), "WORK-LEAK"); ok {
		t.Fatalf("loader followed symlink and returned body: %q", got)
	}
}

// TestBuildItemBodyLoaderAllowsRegularFile asserts a regular (non-symlink) file is still served.
func TestBuildItemBodyLoaderAllowsRegularFile(t *testing.T) {
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\nid: WORK-OK\ntitle: t\nkind: feature\nlane: docs\nstatus: planned\n---\n\nregular-file-marker\n\n## Acceptance criteria\n\n- [planned] c1: criterion text\n"
	if err := os.WriteFile(filepath.Join(itemsDir, "ok.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	load := buildItemBodyLoader(dir, nil)
	got, ok := load(context.Background(), "WORK-OK")
	if !ok {
		t.Fatal("loader rejected regular file")
	}
	if !strings.Contains(got, "regular-file-marker") {
		t.Fatalf("body missing marker: %q", got)
	}
}

// TestBuildItemBodyLoaderEmptyFile asserts an empty .md file falls through to ok=false without panicking.
func TestBuildItemBodyLoaderEmptyFile(t *testing.T) {
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(itemsDir, "empty.md"), nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	load := buildItemBodyLoader(dir, nil)
	if _, ok := load(context.Background(), "WORK-EMPTY"); ok {
		t.Fatal("empty file should not satisfy any work item id")
	}
}

// TestBuildItemBodyLoaderMissingIDInFrontmatter asserts frontmatter without an id field falls through silently.
func TestBuildItemBodyLoaderMissingIDInFrontmatter(t *testing.T) {
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\ntitle: t\nkind: feature\nlane: docs\nstatus: planned\n---\n\nno-id-marker\n\n## Acceptance criteria\n\n- [planned] c1: criterion text\n"
	if err := os.WriteFile(filepath.Join(itemsDir, "noid.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	load := buildItemBodyLoader(dir, nil)
	if _, ok := load(context.Background(), "WORK-MISSING"); ok {
		t.Fatal("frontmatter without id must not match any work item")
	}
}

// TestBuildItemBodyLoaderOversizeRejected asserts bodies over 256KB are rejected so prompt-bloat stays bounded.
func TestBuildItemBodyLoaderOversizeRejected(t *testing.T) {
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	head := "---\nid: WORK-BIG\ntitle: t\nkind: feature\nlane: docs\nstatus: planned\n---\n\n"
	pad := make([]byte, 300*1024)
	for i := range pad {
		pad[i] = 'x'
	}
	body := head + string(pad) + "\n\n## Acceptance criteria\n\n- [planned] c1: criterion text\n"
	if err := os.WriteFile(filepath.Join(itemsDir, "big.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	load := buildItemBodyLoader(dir, nil)
	if _, ok := load(context.Background(), "WORK-BIG"); ok {
		t.Fatal("loader returned ok=true for >256KB body — prompt-bloat cap missing")
	}
}
