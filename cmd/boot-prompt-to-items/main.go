// boot-prompt-to-items converts the PRIORITY block of
// docs/engineer/autonomous-session-prompt.md into one .regatta/items/
// markdown brief per entry so the markdown_catalog adapter can ingest
// them.
//
// Spec: docs/engineer/specs/2026-06-02-s1-t3-boot-prompt-converter.md
//
// Phase X bullets are intentionally NOT emitted (spec §2.1: schema
// has no `blocked` state; sentinel-dependency adds a sharp edge).
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type convertOpts struct {
	source    string // absolute path to boot prompt
	out       string // output dir (will be created)
	sourceRel string // repo-relative path used in linked_artifact
	dryRun    bool
	stdout    io.Writer
}

func main() {
	var (
		source = flag.String("source", "docs/engineer/autonomous-session-prompt.md", "path to boot prompt")
		out    = flag.String("out", ".regatta/items", "output directory")
		dryRun = flag.Bool("dry-run", false, "print actions without writing")
	)
	flag.Parse()
	rel := *source
	if abs, err := filepath.Abs(*source); err == nil {
		if cwd, err := os.Getwd(); err == nil {
			if r, err := filepath.Rel(cwd, abs); err == nil {
				rel = r
			}
		}
	}
	if err := convert(convertOpts{source: *source, out: *out, sourceRel: rel, dryRun: *dryRun, stdout: os.Stdout}); err != nil {
		fmt.Fprintln(os.Stderr, "boot-prompt-to-items:", err)
		os.Exit(1)
	}
}

// Section walker regexes. Stable shape of the PRIORITY block per
// spec §4.
var (
	// "PHASE S1 — ..." / "PHASE X — ..."
	phaseHeaderRE = regexp.MustCompile(`^PHASE\s+(S[123]|X)\b`)
	// "1. **S1-T2 — title** — body..."  (em-dash or hyphen between id and title; em-dash between title and body)
	priorityEntryRE = regexp.MustCompile(`^\d+\.\s+\*\*([Ss][123]-[Tt]\d+)\s*[—-]\s*([^*]+?)\*\*\s*[—-]?\s*(.*)$`)
	// Lines that end the PRIORITY block.
	endOfPriorityRE = regexp.MustCompile(`^(OPEN FOLLOWUPS|Already shipped|WORKFLOW per item)\b`)
	// Sentinel comment line.
	sentinelRE = regexp.MustCompile(`<!-- source-sha256: ([0-9a-f]{64}) -->`)
)

type entry struct {
	id        string // upper-cased, e.g. "S1-T2"
	phase     string // "s1" / "s2" / "s3"
	title     string // raw title prose, trimmed
	body      string // raw body prose, trimmed
	lineNum   int    // 1-based line number in source
}

func (e entry) sha() string {
	h := sha256.Sum256([]byte(e.id + "\x00" + e.title + "\x00" + e.body))
	return hex.EncodeToString(h[:])
}

func (e entry) slug() string {
	t := strings.ToLower(e.title)
	// Build slug from [a-z0-9] runs; everything else is a separator.
	// This is intentionally aggressive — non-ASCII punctuation (em-dash,
	// arrows), backticks, parens, dots all collapse to a single hyphen
	// and never leak into a filename.
	var b strings.Builder
	prevDash := true
	for _, r := range t {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	cleaned := strings.Trim(b.String(), "-")
	tokens := strings.Split(cleaned, "-")
	stop := map[string]bool{
		"the": true, "of": true, "and": true, "a": true, "an": true,
		"to": true, "in": true, "on": true, "for": true, "with": true,
	}
	out := []string{}
	for _, tok := range tokens {
		if tok == "" || stop[tok] {
			continue
		}
		out = append(out, tok)
		if len(out) >= 5 {
			break
		}
	}
	return strings.Join(out, "-")
}

func (e entry) filename() string {
	return fmt.Sprintf("%s-%s.md", strings.ToLower(e.id), e.slug())
}

func parseEntries(data []byte) ([]entry, error) {
	lines := strings.Split(string(data), "\n")
	var (
		entries     []entry
		phase       string
		started     bool
	)
	for i, line := range lines {
		if endOfPriorityRE.MatchString(line) {
			break
		}
		if m := phaseHeaderRE.FindStringSubmatch(line); m != nil {
			phase = strings.ToLower(m[1])
			started = true
			continue
		}
		if !started {
			continue
		}
		// Only numbered S-phase entries are emitted. Phase X bullets
		// are skipped per spec §2.1 (no "blocked" state in schema).
		if phase == "x" {
			continue
		}
		if m := priorityEntryRE.FindStringSubmatch(line); m != nil {
			id := strings.ToUpper(m[1])
			title := strings.TrimSpace(m[2])
			body := strings.TrimSpace(m[3])
			entries = append(entries, entry{
				id:      id,
				phase:   phase,
				title:   title,
				body:    body,
				lineNum: i + 1,
			})
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no PRIORITY entries found")
	}
	// dup-check
	seen := map[string]int{}
	for _, e := range entries {
		seen[e.id]++
	}
	dupes := []string{}
	for id, n := range seen {
		if n > 1 {
			dupes = append(dupes, id)
		}
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		return nil, fmt.Errorf("duplicate PRIORITY entry IDs: %s", strings.Join(dupes, ", "))
	}
	return entries, nil
}

// renderItem produces the on-disk file bytes for one entry.
// Format matches internal/orchestrator/adapter/parse.go::parseMarkdownItem
// + parseCriteria. The sentinel <!-- source-sha256: ... --> on the last
// line is what convert() reads back to decide idempotency.
func renderItem(e entry, sourceRel string) []byte {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	fmt.Fprintf(&buf, "id: %s\n", e.id)
	fmt.Fprintf(&buf, "title: %s\n", e.title)
	buf.WriteString("lane: self-host\n")
	buf.WriteString("status: planned\n")
	fmt.Fprintf(&buf, "linked_artifact: %s#L%d\n", sourceRel, e.lineNum)
	buf.WriteString("---\n\n")
	if e.body != "" {
		fmt.Fprintf(&buf, "Source: %s#L%d\n\n", sourceRel, e.lineNum)
		safe := strings.ReplaceAll(e.body, "\r", "")
		buf.WriteString(safe)
		buf.WriteString("\n\n")
	}
	// Sentinel BEFORE "## Acceptance criteria" — once parseCriteria
	// enters that section every non-blank line MUST match the
	// criterion regex (parse.go:139). The HTML comment lives in the
	// body region and is captured into the trailing `rest` buffer
	// without complaint.
	fmt.Fprintf(&buf, "<!-- source-sha256: %s -->\n\n", e.sha())
	buf.WriteString("## Acceptance criteria\n\n")
	// One criterion per item; ID `c1`, text is a deterministic
	// single-line summary referencing the source. Per spec §2 the
	// criterion text must be on a single line.
	critText := fmt.Sprintf(
		"Land the PRIORITY entry %q per the boot prompt; see source line %d.",
		e.id+" "+e.title,
		e.lineNum,
	)
	critText = strings.ReplaceAll(critText, "\n", " ")
	fmt.Fprintf(&buf, "- [planned] c1: %s\n", critText)
	return buf.Bytes()
}

// existingSha returns the embedded source-sha256 from a previously
// generated file, or "" if absent.
//
// G304 nolint: path is filepath.Join(opts.out, derived-slug), bounded
// by the converter's own output dir; not user-tainted at runtime.
func existingSha(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is converter-derived under opts.out
	if err != nil {
		return "", err
	}
	m := sentinelRE.FindStringSubmatch(string(data))
	if m == nil {
		return "", nil
	}
	return m[1], nil
}

func convert(opts convertOpts) error {
	if opts.stdout == nil {
		opts.stdout = io.Discard
	}
	// G304 nolint: opts.source is operator-supplied via --source flag
	// (defaults to docs/engineer/autonomous-session-prompt.md). The
	// operator IS the trust root for this path; same precedent as
	// internal/config/validate/load.go.
	data, err := os.ReadFile(opts.source) //nolint:gosec // G304: operator-supplied path; trust root
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}
	entries, err := parseEntries(data)
	if err != nil {
		return err
	}
	if opts.dryRun {
		for _, e := range entries {
			_, _ = fmt.Fprintf(opts.stdout, "would emit %s/%s\n", opts.out, e.filename())
		}
		return nil
	}
	if err := os.MkdirAll(opts.out, 0o750); err != nil {
		return fmt.Errorf("mkdir out: %w", err)
	}
	for _, e := range entries {
		target := filepath.Join(opts.out, e.filename())
		body := renderItem(e, opts.sourceRel)
		newSha := e.sha()
		if _, err := os.Stat(target); err == nil {
			oldSha, err := existingSha(target)
			if err != nil {
				return fmt.Errorf("read existing %s: %w", target, err)
			}
			switch oldSha {
			case "":
				_, _ = fmt.Fprintf(opts.stdout, "skip (hand-authored, no sentinel) %s\n", target)
				continue
			case newSha:
				_, _ = fmt.Fprintf(opts.stdout, "ok (unchanged) %s\n", target)
				continue
			default:
				_, _ = fmt.Fprintf(opts.stdout, "rewrite (source moved) %s\n", target)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", target, err)
		} else {
			_, _ = fmt.Fprintf(opts.stdout, "create %s\n", target)
		}
		// 0o600 perms: brief files contain no secrets but follow the
		// repo's gosec G306 convention. The markdown adapter does not
		// require any execute bit; operator reads with their own
		// umask via editor.
		if err := os.WriteFile(target, body, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}
	return nil
}
