package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/schemas"
)

func writeItem(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, ".regatta", "items", name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

const sampleItem = `---
id: ITEM-001
title: First item
lane: server
status: planned
dependencies: ITEM-000
linked_artifact: docs/rfc/001.md
---

Body text.

## Acceptance criteria

- [planned] c1: First criterion
- [planned] c2: Second criterion
`

func TestMarkdownCatalogList(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "001.md", sampleItem)
	writeItem(t, dir, "002.md", strings.Replace(sampleItem, "ITEM-001", "ITEM-002", 1))

	a, err := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].ID != "ITEM-001" || items[1].ID != "ITEM-002" {
		t.Fatalf("unexpected order: %s, %s", items[0].ID, items[1].ID)
	}
	got := items[0]
	if got.Title != "First item" || got.Lane != "server" || got.Status != schemas.StatusPlanned {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if len(got.AcceptanceCriteria) != 2 ||
		got.AcceptanceCriteria[0].ID != "c1" ||
		got.AcceptanceCriteria[0].Text != "First criterion" {
		t.Fatalf("criteria mismatch: %+v", got.AcceptanceCriteria)
	}
	if got.Source.Kind != "file" || !strings.HasPrefix(got.Source.SHA, "sha256:") {
		t.Fatalf("source mismatch: %+v", got.Source)
	}
}

func TestMarkdownCatalogGet(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "001.md", sampleItem)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	it, err := a.Get(context.Background(), "ITEM-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if it.Title != "First item" {
		t.Fatalf("title mismatch: %s", it.Title)
	}

	if _, err := a.Get(context.Background(), "MISSING"); !errors.Is(err, schemas.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMarkdownCatalogListMissingDir(t *testing.T) {
	dir := t.TempDir()
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("list missing dir: %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil items, got %d", len(items))
	}
}

func TestMarkdownCatalogUpdateStatus(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "001.md", sampleItem)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})

	if err := a.UpdateStatus(context.Background(), "ITEM-001", schemas.StatusInProgress, ""); err != nil {
		t.Fatalf("update 1: %v", err)
	}
	it, err := a.Get(context.Background(), "ITEM-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if it.Status != schemas.StatusInProgress {
		t.Fatalf("status not updated: %s", it.Status)
	}

	if err := a.UpdateStatus(context.Background(), "ITEM-001", schemas.StatusDone, "test=TestFoo"); err != nil {
		t.Fatalf("update 2: %v", err)
	}
	contents, _ := os.ReadFile(filepath.Join(dir, ".regatta", "items", "001.md"))
	if !strings.Contains(string(contents), "status: done") {
		t.Fatalf("status line missing after update: %s", contents)
	}
	if !strings.Contains(string(contents), "citation: test=TestFoo") {
		t.Fatalf("citation line missing: %s", contents)
	}
}

func TestMarkdownCatalogRejectsCycle(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "a.md", `---
id: A
title: A
lane: x
status: planned
dependencies: B
---

## Acceptance criteria

- [planned] c1: only criterion
`)
	writeItem(t, dir, "b.md", `---
id: B
title: B
lane: x
status: planned
dependencies: A
---

## Acceptance criteria

- [planned] c1: only criterion
`)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	_, err := a.List(context.Background())
	if !errors.Is(err, schemas.ErrDependencyCycle) {
		t.Fatalf("want ErrDependencyCycle, got %v", err)
	}
}

func TestMarkdownCatalogSkipsTemplateFiles(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "001.md", sampleItem)
	// Templates: leading "_" and leading "." must be ignored even
	// though they have a .md suffix.
	writeItem(t, dir, "_template.md", "garbage that would fail to parse")
	writeItem(t, dir, ".draft.md", "another garbage draft")

	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].ID != "ITEM-001" {
		t.Fatalf("templates leaked into list: %+v", items)
	}
}

func TestMarkdownCatalogRejectsMissingCriteria(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "bad.md", `---
id: BAD
title: missing criteria
lane: x
status: planned
---

Body but no acceptance criteria heading.
`)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	_, err := a.List(context.Background())
	if err == nil {
		t.Fatal("expected error for missing acceptance criteria, got nil")
	}
	if !strings.Contains(err.Error(), "acceptance criteria") {
		t.Fatalf("error %v does not mention acceptance criteria", err)
	}
}

func TestMarkdownCatalogRejectsMissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "bad.md", `# No frontmatter

## Acceptance criteria

- [planned] c1: ignored
`)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	_, err := a.List(context.Background())
	if err == nil {
		t.Fatal("expected error for missing frontmatter, got nil")
	}
}

func TestMarkdownCatalogCapabilities(t *testing.T) {
	dir := t.TempDir()
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	caps := a.Capabilities()
	if caps.Webhook {
		t.Fatalf("markdown_catalog should not advertise webhook support")
	}
	if caps.MinPollInterval <= 0 {
		t.Fatalf("min poll interval must be positive, got %v", caps.MinPollInterval)
	}
}
