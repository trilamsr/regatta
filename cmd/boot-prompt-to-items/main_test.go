package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
)

// fixtureBootPrompt is a minimal boot-prompt PRIORITY block in the
// shape of docs/engineer/autonomous-session-prompt.md. Two S1 entries,
// one S2, one S3, two Phase-X bullets. We assert the converter emits
// 4 files (skips Phase X per spec §2.1) and that each round-trips
// through internal/orchestrator/adapter.ParseMarkdownItem.
const fixtureBootPrompt = `# Autonomous Session Trigger Prompt

Some preamble.

PRIORITY (top-down)

PHASE S1 — dogfood-ready core
1. **S1-T2 — close #282 spawner-callback wiring** — wire spend.SpawnerCallback into cmd/regatta/serve.go::buildSpawner. Single PR.
2. **S1-T4 — Cost-governor Wave 3 dispatch** — T5+T6+T7 per plan #267. File-disjoint trio.

PHASE S2 — trust-the-loop
6. **S2-T1 — W9 replay+diff harness** — substrate-default DurableHistory impl ONLY.

PHASE S3 — durability
10. **S3-T1 — W8 T-remaining slim** — OPA Authorizer impl + policy hot-reload.

PHASE X — deferred
- W7 Waves 1-3 htmx UI (deferred)
- W8 multi-tenant tenant_id scoping

OPEN FOLLOWUPS
- sweep when between phase items

Already shipped
- prior waves
`

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return full
}

// TestParse_EmitsOnePerPriorityEntry — fixture has 2 S1 + 1 S2 + 1 S3 = 4 entries. Spec §2.1: Phase X is skipped.
func TestParse_EmitsOnePerPriorityEntry(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, "boot.md", fixtureBootPrompt)
	out := filepath.Join(dir, "items")

	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"}); err != nil {
		t.Fatalf("convert: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	want := []string{
		"s1-t2-close-282-spawner-callback-wiring.md",
		"s1-t4-cost-governor-wave-3-dispatch.md",
		"s2-t1-w9-replay-diff-harness.md",
		"s3-t1-w8-t-remaining-slim.md",
	}
	if len(names) != len(want) {
		t.Fatalf("file count: got %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("file[%d]: got %q, want %q", i, names[i], n)
		}
	}
}

// TestParse_FrontmatterIsAdapterIngestable — round-trip through the real adapter parser; this is the load-bearing assertio
func TestParse_FrontmatterIsAdapterIngestable(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, "boot.md", fixtureBootPrompt)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"}); err != nil {
		t.Fatalf("convert: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		path := filepath.Join(out, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		item, err := adapter.ParseMarkdownItem(data)
		if err != nil {
			t.Fatalf("adapter.ParseMarkdownItem(%s): %v\n--- file ---\n%s", e.Name(), err, data)
		}
		if item.ID == "" {
			t.Fatalf("%s: empty ID", e.Name())
		}
		if item.Title == "" {
			t.Fatalf("%s: empty Title", e.Name())
		}
		if item.Lane != "self-host" {
			t.Fatalf("%s: lane = %q, want self-host", e.Name(), item.Lane)
		}
		if len(item.AcceptanceCriteria) < 1 {
			t.Fatalf("%s: no acceptance criteria", e.Name())
		}
	}
}

// TestParse_IdempotentNoOp — second run touches zero files. Mtime preserved.
func TestParse_IdempotentNoOp(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, "boot.md", fixtureBootPrompt)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"}); err != nil {
		t.Fatalf("convert 1: %v", err)
	}
	// snapshot mtimes
	mtimes := snapshotMtimes(t, out)
	time.Sleep(20 * time.Millisecond) // ensure clock advances past mtime resolution
	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"}); err != nil {
		t.Fatalf("convert 2: %v", err)
	}
	mtimes2 := snapshotMtimes(t, out)
	if len(mtimes) != len(mtimes2) {
		t.Fatalf("file set changed: %v vs %v", mtimes, mtimes2)
	}
	for name, t1 := range mtimes {
		t2, ok := mtimes2[name]
		if !ok {
			t.Fatalf("file %s vanished", name)
		}
		if !t1.Equal(t2) {
			t.Fatalf("file %s mtime changed: %v -> %v", name, t1, t2)
		}
	}
}

func snapshotMtimes(t *testing.T, dir string) map[string]time.Time {
	t.Helper()
	out := map[string]time.Time{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		fi, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		out[e.Name()] = fi.ModTime()
	}
	return out
}

// TestParse_SourceChange_Rewrites — when the source prose changes, the generated file is rewritten. New sha256 embedded.
func TestParse_SourceChange_Rewrites(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, "boot.md", fixtureBootPrompt)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"}); err != nil {
		t.Fatalf("convert 1: %v", err)
	}
	target := filepath.Join(out, "s1-t2-close-282-spawner-callback-wiring.md")
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// edit the fixture's S1-T2 prose
	edited := strings.Replace(
		fixtureBootPrompt,
		"wire spend.SpawnerCallback into cmd/regatta/serve.go::buildSpawner. Single PR.",
		"wire spend.SpawnerCallback EVERYWHERE. Single PR.",
		1,
	)
	if edited == fixtureBootPrompt {
		t.Fatalf("test fixture: replace did not change source")
	}
	if err := os.WriteFile(src, []byte(edited), 0o644); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"}); err != nil {
		t.Fatalf("convert 2: %v", err)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) == string(after) {
		t.Fatalf("expected rewrite; file unchanged")
	}
	if !strings.Contains(string(after), "EVERYWHERE") {
		t.Fatalf("rewrite did not pick up new prose:\n%s", after)
	}
}

// TestParse_HandEdit_Skipped — file with no source-sha256 sentinel is treated as hand-authored and skipped.
func TestParse_HandEdit_Skipped(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, "boot.md", fixtureBootPrompt)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"}); err != nil {
		t.Fatalf("convert 1: %v", err)
	}
	target := filepath.Join(out, "s1-t2-close-282-spawner-callback-wiring.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// strip the sentinel — emulate a hand-authored file
	stripped := stripSentinel(string(data))
	if stripped == string(data) {
		t.Fatalf("test fixture: sentinel not present in generated file:\n%s", data)
	}
	handAuthored := stripped + "\nOPERATOR HAND-EDIT.\n"
	if err := os.WriteFile(target, []byte(handAuthored), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"}); err != nil {
		t.Fatalf("convert 2: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(got) != handAuthored {
		t.Fatalf("hand-edit clobbered:\nwant:\n%s\ngot:\n%s", handAuthored, got)
	}
}

func stripSentinel(s string) string {
	lines := strings.Split(s, "\n")
	out := []string{}
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "<!-- source-sha256:") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// TestParse_PhaseXSkipped — Phase X bullets do not produce files.
func TestParse_PhaseXSkipped(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, "boot.md", fixtureBootPrompt)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"}); err != nil {
		t.Fatalf("convert: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "x-") {
			t.Fatalf("phase X file emitted: %s", e.Name())
		}
		if strings.Contains(e.Name(), "htmx") || strings.Contains(e.Name(), "multi-tenant") {
			t.Fatalf("phase X file emitted: %s", e.Name())
		}
	}
}

// TestParse_DuplicateID_Errors — duplicate IDs in the source are a hard error; no files written.
func TestParse_DuplicateID_Errors(t *testing.T) {
	dupSource := `PHASE S1 — x
1. **S1-T2 — first dup** — body one.
2. **S1-T2 — second dup** — body two.
`
	dir := t.TempDir()
	src := writeFixture(t, dir, "boot.md", dupSource)
	out := filepath.Join(dir, "items")
	err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"})
	if err == nil {
		t.Fatalf("expected error on duplicate IDs, got nil")
	}
	if !strings.Contains(err.Error(), "S1-T2") {
		t.Fatalf("expected error to mention S1-T2: %v", err)
	}
	// no files should have been written
	if entries, err := os.ReadDir(out); err == nil && len(entries) > 0 {
		t.Fatalf("files written despite duplicate-ID error: %d", len(entries))
	}
}

// TestParse_NoEntries_Errors — a source with zero PRIORITY entries errors loudly. No silent success.
func TestParse_NoEntries_Errors(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, "boot.md", "# Just a doc with no priority block\n")
	out := filepath.Join(dir, "items")
	err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md"})
	if err == nil {
		t.Fatalf("expected error on zero entries, got nil")
	}
}

// TestParse_DryRun — dry-run prints actions but does not touch the FS.
func TestParse_DryRun(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, "boot.md", fixtureBootPrompt)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{source: src, out: out, sourceRel: "boot.md", dryRun: true}); err != nil {
		t.Fatalf("convert dry-run: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		// out may not be created; if it is, it must be empty
		entries, _ := os.ReadDir(out)
		if len(entries) != 0 {
			t.Fatalf("dry-run wrote %d files", len(entries))
		}
	}
}

// TestParse_RealBootPrompt — the actual checked-in boot prompt MUST parse and every emitted file MUST round-trip through t
func TestParse_RealBootPrompt(t *testing.T) {
	// repo root is two levels up from cmd/boot-prompt-to-items/
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(wd))
	src := filepath.Join(repoRoot, "docs", "engineer", "autonomous-session-prompt.md")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("boot prompt not found at %s: %v", src, err)
	}
	out := filepath.Join(t.TempDir(), "items")
	rel, _ := filepath.Rel(repoRoot, src)
	if err := convert(convertOpts{source: src, out: out, sourceRel: rel}); err != nil {
		t.Fatalf("convert real boot prompt: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no entries emitted from real boot prompt")
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(out, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if _, err := adapter.ParseMarkdownItem(data); err != nil {
			t.Fatalf("adapter rejected %s: %v\n%s", e.Name(), err, data)
		}
	}
}
