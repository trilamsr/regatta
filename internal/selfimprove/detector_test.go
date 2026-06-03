package selfimprove

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/ghclient"
)

type fakeSource struct{ events []Event }

func (f *fakeSource) Fetch(_ context.Context, _ time.Time, _ []string) ([]Event, error) {
	return f.events, nil
}

type fakeGH struct {
	open      []ghclient.Issue
	filed     []ghclient.Issue
	commented map[int][]string
}

func newFakeGH(open []ghclient.Issue) *fakeGH {
	return &fakeGH{open: open, commented: map[int][]string{}}
}

func (g *fakeGH) ListOpenIssuesByLabel(_ context.Context, _, _ string) ([]ghclient.Issue, error) {
	return g.open, nil
}

func (g *fakeGH) CreateIssue(_ context.Context, title, body string, _ []string) (int, error) {
	n := 100 + len(g.filed)
	g.filed = append(g.filed, ghclient.Issue{Number: n, Title: title, Body: body})
	g.open = append(g.open, ghclient.Issue{Number: n, Title: title, Body: body})
	return n, nil
}

func (g *fakeGH) CommentOnIssue(_ context.Context, number int, body string) error {
	g.commented[number] = append(g.commented[number], body)
	return nil
}

// buildGateFailEvents builds three same-fingerprint gate_fail rows so
// R1 (same-gate-fail-repeats) fires deterministically.
func buildGateFailEvents() []Event {
	return []Event{
		mkEvent("1", "gate_fail", 1*time.Hour, func(e *Event) { e.GateKind = "pr-lint"; e.GateReason = "banned-phrase" }),
		mkEvent("2", "gate_fail", 2*time.Hour, func(e *Event) { e.GateKind = "pr-lint"; e.GateReason = "banned-phrase" }),
		mkEvent("3", "gate_fail", 3*time.Hour, func(e *Event) { e.GateKind = "pr-lint"; e.GateReason = "banned-phrase" }),
	}
}

// TestDetector_DedupSha256_SkipsDuplicate asserts a second scan with an existing dedup-key body comments not creates.
func TestDetector_DedupSha256_SkipsDuplicate(t *testing.T) {
	src := &fakeSource{events: buildGateFailEvents()}
	gh := newFakeGH(nil)
	d := &Detector{Rules: DefaultRules(), Source: src, GH: gh, Label: LabelSelfImprovement, Apply: true, Clock: func() time.Time { return fixedNow }}

	r1, err := d.Run(context.Background(), 30*24*time.Hour)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if len(r1.Filed) != 1 {
		t.Fatalf("want 1 filed, got %d", len(r1.Filed))
	}

	r2, err := d.Run(context.Background(), 30*24*time.Hour)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(r2.Filed) != 0 {
		t.Fatalf("want 0 new issues on second run, got %d", len(r2.Filed))
	}
	if len(r2.Comments) != 1 {
		t.Fatalf("want 1 comment on second run, got %d", len(r2.Comments))
	}
}

// TestDetector_PauseAllEventFilteredOut asserts pause-tagged events do not count toward a fire (spec §11 risk #4).
func TestDetector_PauseAllEventFilteredOut(t *testing.T) {
	events := []Event{
		mkEvent("1", "gate_fail", 1*time.Hour, func(e *Event) {
			e.GateKind = "pr-lint"
			e.GateReason = "banned-phrase"
			e.Tags = []string{PauseAllTag}
		}),
		mkEvent("2", "gate_fail", 2*time.Hour, func(e *Event) {
			e.GateKind = "pr-lint"
			e.GateReason = "banned-phrase"
			e.Tags = []string{PauseAllTag}
		}),
		mkEvent("3", "gate_fail", 3*time.Hour, func(e *Event) {
			e.GateKind = "pr-lint"
			e.GateReason = "banned-phrase"
			e.Tags = []string{PauseAllTag}
		}),
	}
	got := DefaultRules()[0].Match(events, fixedNow)
	if len(got) != 0 {
		t.Fatalf("want 0 findings (all pause-tagged), got %d", len(got))
	}
}

// TestDetector_DryRun_FilesNothing asserts apply=false skips all GH writes.
func TestDetector_DryRun_FilesNothing(t *testing.T) {
	src := &fakeSource{events: buildGateFailEvents()}
	gh := newFakeGH(nil)
	d := &Detector{Rules: DefaultRules(), Source: src, GH: gh, Label: LabelSelfImprovement, Apply: false, Clock: func() time.Time { return fixedNow }}

	r, err := d.Run(context.Background(), 30*24*time.Hour)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(r.Findings) == 0 {
		t.Fatal("want findings reported, got 0")
	}
	if len(gh.filed) != 0 {
		t.Fatalf("dry-run should file nothing, got %d", len(gh.filed))
	}
}

// TestDetector_ApplyMode_FilesIssues asserts apply=true writes issues with dedup-key in body.
func TestDetector_ApplyMode_FilesIssues(t *testing.T) {
	src := &fakeSource{events: buildGateFailEvents()}
	gh := newFakeGH(nil)
	d := &Detector{Rules: DefaultRules(), Source: src, GH: gh, Label: LabelSelfImprovement, Apply: true, Clock: func() time.Time { return fixedNow }}

	_, err := d.Run(context.Background(), 30*24*time.Hour)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(gh.filed) != 1 {
		t.Fatalf("want 1 filed, got %d", len(gh.filed))
	}
	if !strings.Contains(gh.filed[0].Body, "dedup-key:") {
		t.Fatalf("body missing dedup-key marker: %s", gh.filed[0].Body)
	}
}

type stubLLM struct {
	gotMax int
	resp   string
	err    error
}

func (s *stubLLM) Complete(_ context.Context, _ string, maxTokens int) (string, error) {
	s.gotMax = maxTokens
	return s.resp, s.err
}

// TestLLMScanner_BudgetCap_StaysUnderMaxTokens asserts the hard 4000-token cap is passed verbatim.
func TestLLMScanner_BudgetCap_StaysUnderMaxTokens(t *testing.T) {
	llm := &stubLLM{resp: "rules: []\n"}
	dir := t.TempDir()
	s := &LLMScanner{Client: llm, OutputDir: dir, Clock: func() time.Time { return fixedNow }}
	if _, err := s.Scan(context.Background(), "digest data", "prompt template"); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if llm.gotMax != MaxLLMTokens {
		t.Fatalf("want max_tokens=%d, got %d", MaxLLMTokens, llm.gotMax)
	}
	if llm.gotMax > 4000 {
		t.Fatalf("budget cap exceeded: %d > 4000", llm.gotMax)
	}
}

// TestLLMScanner_NeverAutoFiles_OnlyWritesYAML asserts the LLM path never receives a GH client.
func TestLLMScanner_NeverAutoFiles_OnlyWritesYAML(t *testing.T) {
	llm := &stubLLM{resp: "rules:\n  - name: foo\n"}
	dir := t.TempDir()
	s := &LLMScanner{Client: llm, OutputDir: dir, Clock: func() time.Time { return fixedNow }}
	path, err := s.Scan(context.Background(), "digest", "prompt")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !strings.HasSuffix(path, ".yaml") {
		t.Fatalf("want yaml output path, got %s", path)
	}
}

// TestLLMScanner_PropagatesClientError asserts a complete-error surfaces as an error and writes nothing.
func TestLLMScanner_PropagatesClientError(t *testing.T) {
	llm := &stubLLM{err: errors.New("boom")}
	dir := t.TempDir()
	s := &LLMScanner{Client: llm, OutputDir: dir, Clock: func() time.Time { return fixedNow }}
	if _, err := s.Scan(context.Background(), "digest", "prompt"); err == nil {
		t.Fatal("want error, got nil")
	}
}
