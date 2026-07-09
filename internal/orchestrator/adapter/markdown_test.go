package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
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

// TestMarkdown_LogsSkippedItems asserts malformed items are logged instead of failing silently upstream.
func TestMarkdown_LogsSkippedItems(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "good.md", sampleItem)
	writeItem(t, dir, "broken.md", "---\nthis is not valid frontmatter\n")

	var logs []string
	a, err := NewMarkdownCatalog(MarkdownCatalogConfig{
		Root:   dir,
		Logger: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	err = a.UpdateStatus(context.Background(), "ITEM-DOES-NOT-EXIST", schemas.StatusPlanned, "test=t1")
	if !errors.Is(err, schemas.ErrNotFound) {
		t.Fatalf("UpdateStatus: want ErrNotFound, got %v", err)
	}
	var sawBroken bool
	for _, line := range logs {
		if strings.Contains(line, "broken.md") {
			sawBroken = true
			break
		}
	}
	if !sawBroken {
		t.Fatalf("expected log line referencing broken.md, got %v", logs)
	}
}

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
	// "_" / "." prefixes must be ignored despite the .md suffix.
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

func TestMarkdownCatalogParsesKindProgram(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "p.md", `---
id: PROG-1
kind: program
title: program item
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: do the thing
`)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Kind != schemas.KindProgram {
		t.Fatalf("expected kind=program, got %q", items[0].Kind)
	}
}

func TestMarkdownCatalogRejectsInvalidKind(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "bad.md", `---
id: BAD
kind: bogus
title: bad kind
lane: x
status: planned
---

## Acceptance criteria

- [planned] c1: x
`)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	_, err := a.List(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid kind, got nil")
	}
	if !strings.Contains(err.Error(), "invalid kind") {
		t.Fatalf("error %v does not mention invalid kind", err)
	}
}

// TestMarkdownCatalogKindDefaultsFeature pins the omitted-kind default to "feature" per work_item_source.go:41 (#866).
func TestMarkdownCatalogKindDefaultsFeature(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "f.md", `---
id: F-1
title: no kind specified
lane: x
status: planned
---

## Acceptance criteria

- [planned] c1: x
`)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if items[0].Kind != schemas.KindFeature {
		t.Fatalf("expected kind=feature (default), got %q", items[0].Kind)
	}
}

// TestMarkdownCatalogKindFixtures asserts both kind: feature and kind: program parse to schemas.Kind* (#866).
func TestMarkdownCatalogKindFixtures(t *testing.T) {
	cases := []struct {
		file string
		want schemas.WorkItemKind
	}{
		{"kind-feature.md", schemas.KindFeature},
		{"kind-program.md", schemas.KindProgram},
		{"kind-omitted.md", schemas.KindFeature},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			item, err := ParseMarkdownItem(data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if item.Kind != tc.want {
				t.Fatalf("kind: got %q want %q", item.Kind, tc.want)
			}
		})
	}
}

// TestMarkdownCatalogParsesClosedResolved anchors the terminal-via-supersession status (issue #482).
func TestMarkdownCatalogParsesClosedResolved(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "nit.md", `---
id: NIT-1
title: nit resolved via supersession
lane: self-host
status: closed-resolved
---

## Acceptance criteria

- [closed] c1: verified — superseded by parent brief
- [closed] c2: WAVE-X owns the surface now
`)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Status != schemas.StatusClosedResolved {
		t.Fatalf("expected status=closed-resolved, got %q", items[0].Status)
	}
	if len(items[0].AcceptanceCriteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d", len(items[0].AcceptanceCriteria))
	}
	for _, c := range items[0].AcceptanceCriteria {
		if c.State != schemas.CriterionClosed {
			t.Fatalf("criterion %s: expected state=closed, got %q", c.ID, c.State)
		}
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
	// SupportedStatuses must enumerate every schema Status the parse
	// path accepts; otherwise adaptersync silently drops items the
	// adapter can in fact read+write (issue #493).
	want := map[schemas.Status]bool{
		schemas.StatusPlanned:        true,
		schemas.StatusInProgress:     true,
		schemas.StatusDone:           true,
		schemas.StatusClosedResolved: true,
	}
	got := map[schemas.Status]bool{}
	for _, s := range caps.SupportedStatuses {
		got[s] = true
	}
	for s := range want {
		if !got[s] {
			t.Fatalf("Capabilities().SupportedStatuses missing %q (have %v)", s, caps.SupportedStatuses)
		}
	}
}

// TestMarkdownCatalogUpdateStatusClosedResolved anchors that the write path accepts the schema-defined terminal-via-supersession status (issue #493).
func TestMarkdownCatalogUpdateStatusClosedResolved(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "001.md", sampleItem)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})

	if err := a.UpdateStatus(context.Background(), "ITEM-001", schemas.StatusClosedResolved, "superseded=PARENT-BRIEF"); err != nil {
		t.Fatalf("UpdateStatus(closed-resolved): %v", err)
	}
	it, err := a.Get(context.Background(), "ITEM-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if it.Status != schemas.StatusClosedResolved {
		t.Fatalf("status not updated: %s", it.Status)
	}
	contents, _ := os.ReadFile(filepath.Join(dir, ".regatta", "items", "001.md"))
	if !strings.Contains(string(contents), "status: closed-resolved") {
		t.Fatalf("status line missing after update: %s", contents)
	}
	if !strings.Contains(string(contents), "citation: superseded=PARENT-BRIEF") {
		t.Fatalf("citation line missing: %s", contents)
	}
}

// TestMarkdownCatalogUpdateStatusRejectsBogus keeps the negative path covered after widening the enum check.
func TestMarkdownCatalogUpdateStatusRejectsBogus(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "001.md", sampleItem)
	a, _ := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})

	err := a.UpdateStatus(context.Background(), "ITEM-001", schemas.Status("bogus"), "")
	if !errors.Is(err, schemas.ErrInvalidStatus) {
		t.Fatalf("UpdateStatus(bogus): want ErrInvalidStatus, got %v", err)
	}
}
