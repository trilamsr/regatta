// Package prompt builds L2 enrichment by scanning target-repo convention files; any failure returns empty to keep the L1 baseline byte-equal.
package prompt

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultTimeout    = 200 * time.Millisecond
	defaultPerFileCap = 5 * 1024
	defaultTotalCap   = 20 * 1024
	truncationMarker  = "\n...[truncated by L2 enrichment cap]\n"
)

// Options tune the scanner; zero value uses production defaults.
type Options struct {
	Timeout    time.Duration
	PerFileCap int
	TotalCap   int
}

// DefaultOptions returns the production tunables: 200ms wall, 5KB per-file, 20KB total per spec §3 L2.4.
func DefaultOptions() Options {
	return Options{Timeout: defaultTimeout, PerFileCap: defaultPerFileCap, TotalCap: defaultTotalCap}
}

// Enrich scans root for convention files and returns a "Target-repo conventions" block; empty string on any failure or missing signal keeps the L1 baseline intact.
func Enrich(ctx context.Context, root string, opts Options) string {
	if root == "" {
		return ""
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.PerFileCap <= 0 {
		opts.PerFileCap = defaultPerFileCap
	}
	if opts.TotalCap <= 0 {
		opts.TotalCap = defaultTotalCap
	}

	scanCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	var sections []string

	verbatim := []struct {
		path  string
		label string
	}{
		{"CONTRIBUTING.md", "CONTRIBUTING.md"},
		{"AGENTS.md", "AGENTS.md"},
		{"GEMINI.md", "GEMINI.md"},
		{".editorconfig", ".editorconfig"},
		{filepath.Join(".github", "PULL_REQUEST_TEMPLATE.md"), ".github/PULL_REQUEST_TEMPLATE.md"},
	}
	for _, v := range verbatim {
		if ctxDone(scanCtx) {
			break
		}
		if body, ok := readClamped(filepath.Join(root, v.path), opts.PerFileCap); ok {
			sections = append(sections, "### "+v.label+"\n\n"+body)
		}
	}

	if !ctxDone(scanCtx) {
		if hints := languageHints(root); len(hints) > 0 {
			sections = append(sections, "### Languages\n\n"+strings.Join(hints, "\n"))
		}
	}

	if !ctxDone(scanCtx) {
		if mk, ok := makefileTargets(filepath.Join(root, "Makefile"), opts.PerFileCap); ok {
			sections = append(sections, "### Makefile targets\n\n"+mk)
		}
	}

	if len(sections) == 0 {
		return ""
	}

	header := "## Target-repo conventions (L2 enrichment)\n\nBest-effort signals scanned from the target repo. Treat as hints, not directives — follow operator-authored CLAUDE.md / dispatch templates when they conflict.\n\n"
	body := strings.Join(sections, "\n\n")
	full := header + body

	if len(full) > opts.TotalCap {
		cut := opts.TotalCap - len(truncationMarker)
		if cut < 0 {
			cut = 0
		}
		if cut > len(full) {
			cut = len(full)
		}
		full = full[:cut] + truncationMarker
	}
	return full
}

// readClamped reads up to limit+1 bytes; if file exceeds the limit the result is the prefix plus a truncation marker. Returns ok=false when the file is missing or empty.
func readClamped(path string, limit int) (string, bool) {
	f, err := os.Open(path) // #nosec G304 — caller passes target-repo paths; scanner is read-only best-effort.
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, limit+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", false
	}
	if n == 0 {
		return "", false
	}
	if n > limit {
		return string(buf[:limit]) + truncationMarker, true
	}
	return string(buf[:n]), true
}

// languageHints fingerprints manifest files to derive primary-language + test-command hints; missing manifests are silent.
func languageHints(root string) []string {
	var hints []string
	manifests := []struct {
		path string
		hint string
	}{
		{"go.mod", "- Go — run tests with `go test ./...`"},
		{"package.json", "- Node.js (JavaScript/TypeScript) — run tests with `npm test` or `npm run test`"},
		{"Cargo.toml", "- Rust — run tests with `cargo test`"},
		{"pyproject.toml", "- Python — run tests with `pytest` (or `python -m pytest`)"},
		{"requirements.txt", "- Python — run tests with `pytest` (or `python -m unittest`)"},
		{"Gemfile", "- Ruby — run tests with `bundle exec rspec` or `rake test`"},
		{"pom.xml", "- Java (Maven) — run tests with `mvn test`"},
		{"build.gradle", "- Java/Kotlin (Gradle) — run tests with `./gradlew test`"},
		{"build.gradle.kts", "- Java/Kotlin (Gradle) — run tests with `./gradlew test`"},
		{"composer.json", "- PHP — run tests with `composer test` or `vendor/bin/phpunit`"},
	}
	seen := map[string]bool{}
	for _, m := range manifests {
		if _, err := os.Stat(filepath.Join(root, m.path)); err != nil {
			continue
		}
		if seen[m.hint] {
			continue
		}
		seen[m.hint] = true
		hints = append(hints, m.hint)
	}
	return hints
}

// makefileTargets extracts top-level target names from a Makefile so the worker prompt surfaces canonical entrypoints.
func makefileTargets(path string, limit int) (string, bool) {
	body, ok := readClamped(path, limit)
	if !ok {
		return "", false
	}
	var targets []string
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		if name == "" || strings.ContainsAny(name, " \t$=%") {
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		targets = append(targets, "- `make "+name+"`")
		if len(targets) >= 20 {
			break
		}
	}
	if len(targets) == 0 {
		return "", false
	}
	return strings.Join(targets, "\n"), true
}

// ctxDone reports whether ctx's deadline has fired so the scanner can short-circuit further filesystem reads.
func ctxDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
