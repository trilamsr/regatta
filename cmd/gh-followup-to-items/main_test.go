package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
)

// fixtureIssues mirrors the JSON shape gh issue list --json emits.
// 3 open issues with distinct lane labels + 1 closed issue (cleanup target).
var fixtureIssues = []ghIssue{
	{
		Number: 333,
		Title:  "[followup] doc-check reviewer-tag regex over-matches reviewer-Capital prose",
		Body:   "Trigger: a reviewer-named follow-up token like `reviewer-Bob` matches the doc-check banned-phrase regex.\n\nFix: anchor the regex more tightly.",
		URL:    "https://github.com/trilamsr/regatta/issues/333",
		State:  "OPEN",
		Labels: []ghLabel{{Name: "followup"}, {Name: "W6-followup"}},
	},
	{
		Number: 329,
		Title:  "[followup retro-audit] cost-governor history estimator lane scope",
		Body:   "Operator opted-in to estimator; verify cohort-p95 across lanes.",
		URL:    "https://github.com/trilamsr/regatta/issues/329",
		State:  "OPEN",
		Labels: []ghLabel{{Name: "followup"}, {Name: "cost-governor-followup"}},
	},
	{
		Number: 324,
		Title:  "[followup] tech debt: dedupe slug helper across cmd/* sibling tools",
		Body:   "Two cmd binaries (boot-prompt-to-items, gh-followup-to-items) share a ~30-line slug helper. Extract on 3rd caller.",
		URL:    "https://github.com/trilamsr/regatta/issues/324",
		State:  "OPEN",
		Labels: []ghLabel{{Name: "followup"}, {Name: "tech-debt"}},
	},
	{
		Number: 265,
		Title:  "[followup] previously deferred thing now closed",
		Body:   "This issue was closed; converter MUST delete its brief on re-run.",
		URL:    "https://github.com/trilamsr/regatta/issues/265",
		State:  "CLOSED",
		Labels: []ghLabel{{Name: "followup"}},
	},
}

func writeFixture(t *testing.T, dir string, issues []ghIssue) string {
	t.Helper()
	path := filepath.Join(dir, "issues.json")
	data, err := json.Marshal(issues)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// TestParse_EmitsOnePerOpenIssue — 3 open + 1 closed; closed brief never created.
func TestParse_EmitsOnePerOpenIssue(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, fixtureIssues)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
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
	if len(names) != 3 {
		t.Fatalf("file count: got %d %v, want 3", len(names), names)
	}
	for _, n := range names {
		if strings.Contains(n, "265") {
			t.Fatalf("closed issue emitted: %s", n)
		}
	}
}

// TestParse_FrontmatterIsAdapterIngestable — generated files round-trip through the real adapter parser.
func TestParse_FrontmatterIsAdapterIngestable(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, fixtureIssues)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	wantTitles := map[string]string{
		"GH-ISSUE-333": "doc-check reviewer-tag regex over-matches reviewer-Capital prose",
		"GH-ISSUE-329": "cost-governor history estimator lane scope",
		"GH-ISSUE-324": "tech debt: dedupe slug helper across cmd/* sibling tools",
	}
	wantLanes := map[string]string{
		"GH-ISSUE-333": "w6-followup",
		"GH-ISSUE-329": "cost-governor-followup",
		"GH-ISSUE-324": "tech-debt",
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
		gotTitle, ok := wantTitles[string(item.ID)]
		if !ok {
			t.Fatalf("unexpected ID %q", item.ID)
		}
		if item.Title != gotTitle {
			t.Fatalf("%s title: got %q, want %q", item.ID, item.Title, gotTitle)
		}
		if string(item.Lane) != wantLanes[string(item.ID)] {
			t.Fatalf("%s lane: got %q, want %q", item.ID, item.Lane, wantLanes[string(item.ID)])
		}
		if item.Kind != "feature" {
			t.Fatalf("%s kind: got %q, want feature", item.ID, item.Kind)
		}
		if !strings.HasPrefix(item.LinkedArtifact, "https://github.com/") {
			t.Fatalf("%s linked_artifact: got %q", item.ID, item.LinkedArtifact)
		}
		if len(item.AcceptanceCriteria) < 1 {
			t.Fatalf("%s: no acceptance criteria", e.Name())
		}
	}
}

// TestParse_LaneDerivedFromLabel — lane comes from the alphabetically-first non-followup label, else "followup".
func TestParse_LaneDerivedFromLabel(t *testing.T) {
	cases := []struct {
		labels []string
		want   string
	}{
		{[]string{"followup"}, "followup"},
		{[]string{"followup", "W6-followup"}, "w6-followup"},
		{[]string{"followup", "cost-governor-followup"}, "cost-governor-followup"},
		{[]string{"followup", "tech-debt"}, "tech-debt"},
		{[]string{"followup", "z-followup", "a-followup"}, "a-followup"},
	}
	for _, tc := range cases {
		labels := make([]ghLabel, 0, len(tc.labels))
		for _, n := range tc.labels {
			labels = append(labels, ghLabel{Name: n})
		}
		got, _ := deriveLane(labels)
		if got != tc.want {
			t.Fatalf("deriveLane(%v): got %q, want %q", tc.labels, got, tc.want)
		}
	}
}

// TestParse_LaneAmbiguity_Warns — two competing non-followup labels: alphabetic first wins + warning emitted.
func TestParse_LaneAmbiguity_Warns(t *testing.T) {
	labels := []ghLabel{{Name: "followup"}, {Name: "z-followup"}, {Name: "a-followup"}}
	lane, warn := deriveLane(labels)
	if lane != "a-followup" {
		t.Fatalf("lane: got %q, want a-followup", lane)
	}
	if warn == "" {
		t.Fatalf("expected ambiguity warning, got empty")
	}
}

// TestParse_IdempotentNoOp — second run touches zero files.
func TestParse_IdempotentNoOp(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, fixtureIssues)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert 1: %v", err)
	}
	mtimes := snapshotMtimes(t, out)
	time.Sleep(20 * time.Millisecond)
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
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
			t.Fatalf("file %s mtime changed", name)
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

// TestParse_BodyChange_Rewrites — body edit triggers a rewrite with new sha256.
func TestParse_BodyChange_Rewrites(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, fixtureIssues)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert 1: %v", err)
	}
	// find the 333 file
	target := findFile(t, out, "gh-issue-333-")
	before, _ := os.ReadFile(target)

	edited := make([]ghIssue, len(fixtureIssues))
	copy(edited, fixtureIssues)
	edited[0].Body = "MUTATED body content — operator edited the GH issue."
	writeFixtureAt(t, src, edited)

	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert 2: %v", err)
	}
	after, _ := os.ReadFile(target)
	if bytes.Equal(before, after) {
		t.Fatalf("expected rewrite; file unchanged")
	}
	if !strings.Contains(string(after), "MUTATED") {
		t.Fatalf("rewrite did not pick up new body:\n%s", after)
	}
}

// TestParse_HandEdit_Skipped — file with no sentinel is treated as hand-authored and skipped.
func TestParse_HandEdit_Skipped(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, fixtureIssues)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert 1: %v", err)
	}
	target := findFile(t, out, "gh-issue-333-")
	data, _ := os.ReadFile(target)
	stripped := stripSentinel(string(data)) + "\nHAND EDIT.\n"
	if err := os.WriteFile(target, []byte(stripped), 0o644); err != nil {
		t.Fatalf("rewrite target: %v", err)
	}
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert 2: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != stripped {
		t.Fatalf("hand-edit clobbered")
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

// TestParse_ClosedIssue_DeletesFile — once an issue closes its brief is removed on re-run.
func TestParse_ClosedIssue_DeletesFile(t *testing.T) {
	dir := t.TempDir()
	// First run: 333 is OPEN.
	openIssues := []ghIssue{fixtureIssues[0]}
	src := writeFixture(t, dir, openIssues)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert 1: %v", err)
	}
	target := findFile(t, out, "gh-issue-333-")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected file before close: %v", err)
	}

	// Second run: 333 is CLOSED.
	closed := []ghIssue{openIssues[0]}
	closed[0].State = "CLOSED"
	writeFixtureAt(t, src, closed)
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert 2: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected file deleted; stat err = %v", err)
	}
}

// TestParse_BracketTagStrippedFromTitle — [followup ...] prefix is stripped from the rendered title.
func TestParse_BracketTagStrippedFromTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"[followup] doc-check thing", "doc-check thing"},
		{"[followup retro-audit] PR shipped without scorecard", "PR shipped without scorecard"},
		{"[followup W7.0-T1] some w7 thing", "some w7 thing"},
		{"no bracket here", "no bracket here"},
	}
	for _, tc := range cases {
		got := stripBracketTag(tc.in)
		if got != tc.want {
			t.Fatalf("stripBracketTag(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestParse_SluggyFilenameDeterministic — same input → same filename across runs.
func TestParse_SluggyFilenameDeterministic(t *testing.T) {
	iss := fixtureIssues[0]
	a := filename(iss)
	b := filename(iss)
	if a != b {
		t.Fatalf("non-deterministic filename: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "gh-issue-333-") {
		t.Fatalf("filename prefix: got %q", a)
	}
}

// TestParse_LimitExceeded_Errors — exceeding --limit is a hard error, not silent truncation.
func TestParse_LimitExceeded_Errors(t *testing.T) {
	dir := t.TempDir()
	many := make([]ghIssue, 0, 11)
	for i := 0; i < 11; i++ {
		many = append(many, ghIssue{
			Number: 1000 + i,
			Title:  "[followup] t",
			Body:   "b",
			URL:    "https://example",
			State:  "OPEN",
			Labels: []ghLabel{{Name: "followup"}},
		})
	}
	src := writeFixture(t, dir, many)
	out := filepath.Join(dir, "items")
	err := convert(convertOpts{sourceJSON: src, out: out, limit: 10})
	if err == nil {
		t.Fatalf("expected limit-exceeded error, got nil")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error should mention limit: %v", err)
	}
	if entries, _ := os.ReadDir(out); len(entries) > 0 {
		t.Fatalf("files written despite limit error: %d", len(entries))
	}
}

// TestParse_DryRun — dry-run lists actions but does not touch the FS.
func TestParse_DryRun(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, fixtureIssues)
	out := filepath.Join(dir, "items")
	var buf bytes.Buffer
	if err := convert(convertOpts{sourceJSON: src, out: out, dryRun: true, stdout: &buf}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(out)
		if len(entries) != 0 {
			t.Fatalf("dry-run wrote %d files", len(entries))
		}
	}
	if !strings.Contains(buf.String(), "would emit") {
		t.Fatalf("dry-run output missing 'would emit':\n%s", buf.String())
	}
}

// TestParse_BodyContainsAcceptanceHeading — body containing a literal '## Acceptance criteria' line is defanged so the adapter parses correctly.
func TestParse_BodyContainsAcceptanceHeading(t *testing.T) {
	dir := t.TempDir()
	iss := []ghIssue{{
		Number: 999,
		Title:  "[followup] meta",
		Body:   "Header in body:\n\n## Acceptance criteria\n\n- not really a criterion\n",
		URL:    "https://example",
		State:  "OPEN",
		Labels: []ghLabel{{Name: "followup"}},
	}}
	src := writeFixture(t, dir, iss)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert: %v", err)
	}
	target := findFile(t, out, "gh-issue-999-")
	data, _ := os.ReadFile(target)
	item, err := adapter.ParseMarkdownItem(data)
	if err != nil {
		t.Fatalf("adapter rejected file with collision-prone body: %v\n%s", err, data)
	}
	if len(item.AcceptanceCriteria) != 1 {
		t.Fatalf("want exactly 1 criterion (converter's own), got %d", len(item.AcceptanceCriteria))
	}
}

// TestParse_BodyContainsLiteralSentinel — sentinel-shaped string mid-body is ignored; end-of-body sentinel still drives idempotency.
func TestParse_BodyContainsLiteralSentinel(t *testing.T) {
	dir := t.TempDir()
	iss := []ghIssue{{
		Number: 888,
		Title:  "[followup] sentinel literal",
		Body:   "Operator pasted: <!-- source-sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa --> mid-body.\n\nRest of body.",
		URL:    "https://example",
		State:  "OPEN",
		Labels: []ghLabel{{Name: "followup"}},
	}}
	src := writeFixture(t, dir, iss)
	out := filepath.Join(dir, "items")
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert 1: %v", err)
	}
	target := findFile(t, out, "gh-issue-888-")
	m1, _ := os.Stat(target)
	time.Sleep(20 * time.Millisecond)
	if err := convert(convertOpts{sourceJSON: src, out: out}); err != nil {
		t.Fatalf("convert 2: %v", err)
	}
	m2, _ := os.Stat(target)
	if !m1.ModTime().Equal(m2.ModTime()) {
		t.Fatalf("idempotency broken by mid-body sentinel literal")
	}
}

func writeFixtureAt(t *testing.T, path string, issues []ghIssue) {
	t.Helper()
	data, err := json.Marshal(issues)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func findFile(t *testing.T, dir, prefix string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatalf("no file with prefix %q in %s", prefix, dir)
	return ""
}
