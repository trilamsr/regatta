package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEnrich_FindsContributingMd asserts CONTRIBUTING.md content lands in the enrichment block (#966 L2.1).
func TestEnrich_FindsContributingMd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CONTRIBUTING.md", "# Contributing\n\nUse `make test` to run tests.\n")
	out := Enrich(context.Background(), dir, DefaultOptions())
	if !strings.Contains(out, "Target-repo conventions") {
		t.Fatalf("missing header; got: %q", out)
	}
	if !strings.Contains(out, "make test") {
		t.Fatalf("missing CONTRIBUTING content; got: %q", out)
	}
}

// TestEnrich_LanguageDetection_Go asserts a Go-majority repo emits a Go hint via go.mod (#966 L2.1).
func TestEnrich_LanguageDetection_Go(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/foo\n\ngo 1.22\n")
	out := Enrich(context.Background(), dir, DefaultOptions())
	if !strings.Contains(out, "Go") {
		t.Fatalf("missing Go language hint; got: %q", out)
	}
	if !strings.Contains(out, "go test") {
		t.Fatalf("missing go test command hint; got: %q", out)
	}
}

// TestEnrich_LanguageDetection_Python asserts a Python repo with pyproject.toml emits a Python hint (#966 L2.1).
func TestEnrich_LanguageDetection_Python(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", "[project]\nname = \"foo\"\n")
	out := Enrich(context.Background(), dir, DefaultOptions())
	if !strings.Contains(out, "Python") {
		t.Fatalf("missing Python language hint; got: %q", out)
	}
}

// TestEnrich_Polyglot_InjectsBothLanguages asserts a polyglot Go+JS repo emits both hints (#966 L2.1).
func TestEnrich_Polyglot_InjectsBothLanguages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/foo\n")
	writeFile(t, dir, "package.json", "{\"name\":\"foo\"}\n")
	out := Enrich(context.Background(), dir, DefaultOptions())
	if !strings.Contains(out, "Go") {
		t.Fatalf("missing Go hint in polyglot; got: %q", out)
	}
	if !strings.Contains(out, "Node") && !strings.Contains(out, "JavaScript") && !strings.Contains(out, "TypeScript") {
		t.Fatalf("missing JS/TS hint in polyglot; got: %q", out)
	}
}

// TestEnrich_NoConventions_YieldsBaseline asserts an empty repo yields empty enrichment (#966 L2.2).
func TestEnrich_NoConventions_YieldsBaseline(t *testing.T) {
	dir := t.TempDir()
	out := Enrich(context.Background(), dir, DefaultOptions())
	if out != "" {
		t.Fatalf("expected empty enrichment for empty repo; got: %q", out)
	}
}

// TestEnrich_TimeoutBudget asserts the scanner respects ctx deadline within ~200ms (#966 L2.3).
func TestEnrich_TimeoutBudget(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CONTRIBUTING.md", "ok\n")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = Enrich(ctx, dir, DefaultOptions())
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Enrich ran %v, exceeded 200ms budget", elapsed)
	}
}

// TestEnrich_TotalCap asserts an oversized CONTRIBUTING.md truncates with a marker and total stays under cap (#966 L2.4).
func TestEnrich_TotalCap(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("a", 50*1024)
	writeFile(t, dir, "CONTRIBUTING.md", big)
	writeFile(t, dir, "AGENTS.md", big)
	out := Enrich(context.Background(), dir, DefaultOptions())
	if len(out) > 25*1024 {
		t.Fatalf("enrichment %d bytes exceeds 25KB safety margin", len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncation marker; got tail: %q", tail(out, 200))
	}
}

// TestEnrich_AbsentRepoRoot asserts an empty RepoRoot returns empty without error (#966 L2.2).
func TestEnrich_AbsentRepoRoot(t *testing.T) {
	out := Enrich(context.Background(), "", DefaultOptions())
	if out != "" {
		t.Fatalf("expected empty enrichment for empty root; got: %q", out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
