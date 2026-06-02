// gh-followup-to-items converts GitHub [followup]-labeled issues into
// .regatta/items/gh-issue-<n>-<slug>.md briefs so the markdown_catalog
// adapter ingests them as WorkItems.
//
// Spec: docs/engineer/specs/2026-06-02-s2-t3-followup-triage.md
// Sibling of cmd/boot-prompt-to-items (S1-T3).
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// State + label constants. Spec §7 + §3.2.
const (
	stateOpen     = "OPEN"
	stateClosed   = "CLOSED"
	defaultLane   = "followup"
	defaultLabel  = "followup"
	defaultLimit  = 200
	briefFileFmt  = "gh-issue-%d-%s.md"
	itemIDPrefix  = "GH-ISSUE-"
)

type ghLabel struct {
	Name string `json:"name"`
}

type ghIssue struct {
	Number int       `json:"number"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	URL    string    `json:"url"`
	State  string    `json:"state"`
	Labels []ghLabel `json:"labels"`
}

type convertOpts struct {
	sourceJSON string // path to pre-fetched JSON (test escape hatch); empty → shell to gh
	out        string
	repo       string // owner/name; empty → gh's own default-repo resolution
	label      string
	limit      int
	dryRun     bool
	stdout     io.Writer
	stderr     io.Writer
}

func main() {
	var (
		label  = flag.String("label", "followup", "GH label to filter on")
		out    = flag.String("out", ".regatta/items", "output directory")
		repo   = flag.String("repo", "", "owner/name (default: gh's resolution)")
		limit  = flag.Int("limit", 200, "max issues to fetch")
		src    = flag.String("source-json", "", "read issues from JSON file (test escape hatch); empty → shell to gh")
		dryRun = flag.Bool("dry-run", false, "print actions without writing")
	)
	flag.Parse()
	err := convert(convertOpts{
		sourceJSON: *src,
		out:        *out,
		repo:       *repo,
		label:      *label,
		limit:      *limit,
		dryRun:     *dryRun,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gh-followup-to-items:", err)
		// exit 2 reserved for tool errors per spec §7
		var te *toolErr
		if errors.As(err, &te) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// toolErr signals an exit-code-2 condition (gh not installed, FS perms).
type toolErr struct{ msg string }

func (e *toolErr) Error() string { return e.msg }

var (
	// sentinel must sit on its own line at end of body region.
	sentinelEndRE = regexp.MustCompile(`(?m)<!-- source-sha256: ([0-9a-f]{64}) -->`)
	// bracket-tag prefix: [followup], [followup retro-audit], [followup W7.0-T1], [l4-followup], etc.
	bracketPrefixRE = regexp.MustCompile(`^\s*\[[^\]]+\]\s*`)
	// detect any '## Acceptance criteria' heading inside an issue body.
	acceptanceHeadingRE = regexp.MustCompile(`(?im)^##\s+acceptance\s+criteria\s*$`)
	// match the filename pattern for cleanup.
	briefFilenameRE = regexp.MustCompile(`^gh-issue-(\d+)-`)
)

func stripBracketTag(title string) string {
	return strings.TrimSpace(bracketPrefixRE.ReplaceAllString(title, ""))
}

// deriveLane picks a lane label from the issue's GH labels per spec §3.2:
// pick the alphabetically-first non-`followup` label (lower-cased). If
// none, fall back to "followup". Emits a one-line warning when more
// than one candidate exists so the operator sees the determinism
// choice (root cause: GH labels are unordered).
//
// Spec §3.2 prose said `*-followup$` only, but the same section's table
// maps `followup`+`tech-debt` → `tech-debt`. Picking "first non-followup"
// satisfies both the table and the alphabetic-determinism rule.
func deriveLane(labels []ghLabel) (lane, warn string) {
	matches := []string{}
	for _, l := range labels {
		name := strings.ToLower(l.Name)
		if name == defaultLane || name == "" {
			continue
		}
		matches = append(matches, name)
	}
	if len(matches) == 0 {
		return defaultLane, ""
	}
	sort.Strings(matches)
	lane = matches[0]
	if len(matches) > 1 {
		warn = fmt.Sprintf("multiple non-followup labels %v; picked %q (alphabetic first)", matches, lane)
	}
	return lane, warn
}

// slug returns the first 5 dash-joined lower-cased tokens of title
// (sans bracket-tag prefix), dropping punctuation + stop-words. Matches
// the S1-T3 slug rule per spec §3.
func slug(title string) string {
	t := strings.ToLower(stripBracketTag(title))
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

func filename(iss ghIssue) string {
	return fmt.Sprintf(briefFileFmt, iss.Number, slug(iss.Title))
}

// canonicalBody normalizes line endings + trailing whitespace + final newline.
// Hash is computed over canonical body bytes ONLY (not title, not labels).
func canonicalBody(body string) string {
	b := strings.ReplaceAll(body, "\r\n", "\n")
	b = strings.ReplaceAll(b, "\r", "\n")
	lines := strings.Split(b, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func bodySha(body string) string {
	h := sha256.Sum256([]byte(canonicalBody(body)))
	return hex.EncodeToString(h[:])
}

// renderItem produces the on-disk bytes for one open issue.
// Format mirrors S1-T3 (sentinel placed in body region — root-cause
// lesson from cmd/boot-prompt-to-items; placing under '## Acceptance criteria' breaks
// criterionRE).
func renderItem(iss ghIssue) []byte {
	var buf bytes.Buffer
	title := stripBracketTag(iss.Title)
	lane, _ := deriveLane(iss.Labels)

	buf.WriteString("---\n")
	fmt.Fprintf(&buf, "id: %s%d\n", itemIDPrefix, iss.Number)
	fmt.Fprintf(&buf, "title: %s\n", title)
	buf.WriteString("kind: feature\n")
	fmt.Fprintf(&buf, "lane: %s\n", lane)
	buf.WriteString("status: planned\n")
	fmt.Fprintf(&buf, "linked_artifact: %s\n", iss.URL)
	buf.WriteString("---\n\n")

	fmt.Fprintf(&buf, "Source: %s\n\n", iss.URL)

	body := strings.ReplaceAll(iss.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	// Spec §3.4.1 mitigation #1: if the body itself contains a
	// '## Acceptance criteria' heading, defang it before emit. The
	// adapter's criteriaHeadRE is anchored ^##; we blockquote-prefix
	// such lines ('> ## Acceptance criteria') so the regex no longer
	// matches while the line remains human-readable. Spec proposed
	// triple-backtick fencing; the adapter parser is fence-blind
	// (parseCriteria walks raw lines, not markdown blocks), so fencing
	// alone would not defang. Blockquote prefix is the minimal correct
	// defang — same heading regex as the adapter, applied here.
	body = acceptanceHeadingRE.ReplaceAllStringFunc(body, func(line string) string {
		return "> " + line
	})
	buf.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		buf.WriteByte('\n')
	}
	buf.WriteString("\n")

	// Sentinel BEFORE '## Acceptance criteria' (S1-T3 root-cause lesson).
	fmt.Fprintf(&buf, "<!-- source-sha256: %s -->\n\n", bodySha(iss.Body))

	buf.WriteString("## Acceptance criteria\n\n")
	crit := fmt.Sprintf(
		"Resolve GH issue #%d %q per the trigger condition documented in the issue body.",
		iss.Number, title,
	)
	crit = strings.ReplaceAll(crit, "\n", " ")
	fmt.Fprintf(&buf, "- [planned] c1: %s\n", crit)
	return buf.Bytes()
}

// existingSha returns the sha embedded in a prior brief, or "" if absent.
// The regex anchors to a single line; a sentinel-shaped string mid-body
// of the source issue does NOT pollute this read because renderItem
// emits exactly one sentinel line in the body region between the
// rendered body and the '## Acceptance criteria' heading.
//
// G304 nolint: path is filepath.Join(opts.out, derived-slug); not user-tainted.
func existingSha(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: converter-derived path
	if err != nil {
		return "", err
	}
	// Scan only lines that are sentinels-on-their-own (trimmed); skip
	// sentinel-shaped strings embedded in surrounding prose.
	for _, ln := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(ln)
		if m := sentinelEndRE.FindStringSubmatch(trimmed); m != nil && strings.HasPrefix(trimmed, "<!--") && strings.HasSuffix(trimmed, "-->") {
			return m[1], nil
		}
	}
	return "", nil
}

// loadIssues reads the source — either from --source-json (test path) or
// by shelling to `gh issue list --json ...`.
func loadIssues(opts convertOpts) ([]ghIssue, error) {
	var data []byte
	var err error
	if opts.sourceJSON != "" {
		// G304 nolint: operator-supplied flag; trust root.
		data, err = os.ReadFile(opts.sourceJSON) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("read source-json: %w", err)
		}
	} else {
		if _, lookErr := exec.LookPath("gh"); lookErr != nil {
			return nil, &toolErr{msg: "gh CLI not found; install from https://cli.github.com"}
		}
		args := []string{
			"issue", "list",
			"--label", opts.label,
			"--state", "all",
			"--json", "number,title,body,labels,state,url",
			"--limit", fmt.Sprintf("%d", opts.limit),
		}
		if opts.repo != "" {
			args = append(args, "--repo", opts.repo)
		}
		// G204 nolint: args slice is built from operator flags + literal
		// constants; the binary name "gh" is a literal; no shell.
		cmd := exec.Command("gh", args...) //nolint:gosec // G204: literal binary; args from flags
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("gh issue list: %w: %s", err, stderr.String())
		}
	}
	var issues []ghIssue
	if err := json.Unmarshal(data, &issues); err != nil {
		return nil, fmt.Errorf("parse issues JSON: %w", err)
	}
	return issues, nil
}

func convert(opts convertOpts) error {
	if opts.stdout == nil {
		opts.stdout = io.Discard
	}
	if opts.stderr == nil {
		opts.stderr = io.Discard
	}
	if opts.label == "" {
		opts.label = defaultLabel
	}
	if opts.limit == 0 {
		opts.limit = defaultLimit
	}

	issues, err := loadIssues(opts)
	if err != nil {
		return err
	}
	if len(issues) >= opts.limit {
		return fmt.Errorf("issue count %d hit limit %d; raise --limit", len(issues), opts.limit)
	}

	// Partition open vs closed.
	var open, closed []ghIssue
	for _, iss := range issues {
		switch strings.ToUpper(iss.State) {
		case stateOpen:
			open = append(open, iss)
		case stateClosed:
			closed = append(closed, iss)
		}
	}

	if opts.dryRun {
		for _, iss := range open {
			_, _ = fmt.Fprintf(opts.stdout, "would emit %s/%s\n", opts.out, filename(iss))
		}
		for _, iss := range closed {
			_, _ = fmt.Fprintf(opts.stdout, "would delete (closed) gh-issue-%d-*.md\n", iss.Number)
		}
		return nil
	}

	if err := os.MkdirAll(opts.out, 0o750); err != nil {
		return &toolErr{msg: fmt.Sprintf("mkdir out: %v", err)}
	}

	// Surface lane-ambiguity warnings BEFORE writes — operator catches
	// the determinism choice in the same run that produces the artifact.
	for _, iss := range open {
		if _, warn := deriveLane(iss.Labels); warn != "" {
			_, _ = fmt.Fprintf(opts.stderr, "warn: issue #%d: %s\n", iss.Number, warn)
		}
	}

	// Open-issue pass.
	for _, iss := range open {
		target := filepath.Join(opts.out, filename(iss))
		body := renderItem(iss)
		newSha := bodySha(iss.Body)
		if _, statErr := os.Stat(target); statErr == nil {
			oldSha, readErr := existingSha(target)
			if readErr != nil {
				return fmt.Errorf("read existing %s: %w", target, readErr)
			}
			switch oldSha {
			case "":
				_, _ = fmt.Fprintf(opts.stdout, "skip (hand-authored, no sentinel) %s\n", target)
				continue
			case newSha:
				_, _ = fmt.Fprintf(opts.stdout, "ok (unchanged) %s\n", target)
				continue
			default:
				_, _ = fmt.Fprintf(opts.stdout, "rewrite (body changed) %s\n", target)
			}
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("stat %s: %w", target, statErr)
		} else {
			_, _ = fmt.Fprintf(opts.stdout, "create %s\n", target)
		}
		if err := os.WriteFile(target, body, 0o600); err != nil {
			return &toolErr{msg: fmt.Sprintf("write %s: %v", target, err)}
		}
	}

	// Closed-issue cleanup pass. Filename-anchored per spec §6 — survives
	// a frontmatter hand-edit that corrupts linked_artifact.
	closedNums := map[int]bool{}
	for _, iss := range closed {
		closedNums[iss.Number] = true
	}
	if len(closedNums) > 0 {
		entries, err := os.ReadDir(opts.out)
		if err != nil {
			return fmt.Errorf("readdir %s: %w", opts.out, err)
		}
		for _, e := range entries {
			m := briefFilenameRE.FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}
			n, parseErr := strconv.Atoi(m[1])
			if parseErr != nil {
				continue
			}
			if !closedNums[n] {
				continue
			}
			target := filepath.Join(opts.out, e.Name())
			// Warn before clobbering a hand-edited closed brief
			// (sentinel stripped → operator may want git-restore).
			if sha, _ := existingSha(target); sha == "" {
				_, _ = fmt.Fprintf(opts.stderr, "warn: deleting hand-edited brief for closed issue #%d: %s (git restore to recover)\n", n, target)
			}
			if err := os.Remove(target); err != nil {
				return &toolErr{msg: fmt.Sprintf("remove %s: %v", target, err)}
			}
			_, _ = fmt.Fprintf(opts.stdout, "delete (closed) %s\n", target)
		}
	}

	return nil
}
