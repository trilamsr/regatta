package selfimprove

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/ghclient"
)

// EventFetcher is the function shape that yields substrate events for
// the detector to scan. Tests pass an in-memory closure; production
// wires SQLEventSource.Fetch over `substrate_events` (spec §4.1).
type EventFetcher func(ctx context.Context, since time.Time, kinds []string) ([]Event, error)

// Detector is the single-pass orchestrator: load events, scatter to
// rules, dedup against open GH issues, file/comment per finding.
// Apply=false runs in dry-run mode (prints findings, no GH writes) —
// the CLI default per spec §8.1.
type Detector struct {
	Rules  []Rule
	Source EventFetcher
	GH     ghclient.Client
	Clock  func() time.Time
	Label  string
	Apply  bool
}

// NewDetector returns a detector wired with the five MVP rules and
// the operator's GH adapter. Clock defaults to time.Now; tests inject
// a fixed clock so window math is deterministic.
func NewDetector(src EventFetcher, gh ghclient.Client, apply bool) *Detector {
	return &Detector{
		Rules:  DefaultRules(),
		Source: src,
		GH:     gh,
		Clock:  time.Now,
		Label:  LabelSelfImprovement,
		Apply:  apply,
	}
}

// RunResult is the per-pass summary the CLI prints — counts let the
// operator eyeball a dry-run before flipping --apply on.
type RunResult struct {
	Findings []Finding
	Filed    []int
	Comments []int
	Skipped  []string
}

// Run executes one scan pass: unions every rule's event-kind set into
// one outer fetch, scatters rows in memory, dedups against a single
// bulk GH issue-list fetch (spec §11 risk #6 — 1 API call per scan,
// not per rule), then files / comments per finding.
func (d *Detector) Run(ctx context.Context, window time.Duration) (*RunResult, error) {
	now := d.Clock()
	since := now.Add(-window)

	kinds := unionKinds(d.Rules)
	events, err := d.Source(ctx, since, kinds)
	if err != nil {
		return nil, fmt.Errorf("selfimprove: fetch events: %w", err)
	}

	var findings []Finding
	for _, r := range d.Rules {
		findings = append(findings, r.Match(events, now)...)
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].DedupKey < findings[j].DedupKey })

	result := &RunResult{Findings: findings}
	if !d.Apply {
		return result, nil
	}

	open, err := d.GH.ListOpenIssuesByLabel(ctx, d.Label, "")
	if err != nil {
		return nil, fmt.Errorf("selfimprove: list issues: %w", err)
	}
	existing := indexByDedup(open)

	for _, f := range findings {
		title, body := renderIssue(f)
		if num, ok := existing[f.DedupKey]; ok {
			if err := d.GH.CommentOnIssue(ctx, num,
				fmt.Sprintf("Rule fired again — %d events in window. dedup-key: %s",
					f.Count, f.DedupKey)); err != nil {
				return nil, fmt.Errorf("selfimprove: comment: %w", err)
			}
			result.Comments = append(result.Comments, num)
			result.Skipped = append(result.Skipped, f.DedupKey)
			continue
		}
		num, err := d.GH.CreateIssue(ctx, title, body, []string{d.Label})
		if err != nil {
			return nil, fmt.Errorf("selfimprove: create issue: %w", err)
		}
		result.Filed = append(result.Filed, num)
	}
	return result, nil
}

func unionKinds(rules []Rule) []string {
	seen := map[string]struct{}{}
	for _, r := range rules {
		for _, k := range r.EventKinds() {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func indexByDedup(issues []ghclient.Issue) map[string]int {
	out := make(map[string]int, len(issues))
	for _, iss := range issues {
		key := extractDedupKey(iss.Body)
		if key != "" {
			out[key] = iss.Number
		}
	}
	return out
}

// extractDedupKey reads the `dedup-key: <hex>` line spec §6.3 mandates
// every body carry. Visible (not HTML-comment) form per spec §17 — GH
// renderers occasionally strip comments, the visible line survives.
func extractDedupKey(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "dedup-key:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "dedup-key:"))
		}
	}
	return ""
}

func renderIssue(f Finding) (string, string) {
	title := fmt.Sprintf("[self-improve] %s on %s", f.Rule, f.Subject)
	var b strings.Builder
	fmt.Fprintf(&b, "## What happened\n\nRule `%s` fired: %d events grouped by %s within window.\n\n",
		f.Rule, f.Count, formatGroupBy(f.GroupByVals))
	b.WriteString("## Substrate citations\n\n")
	for _, id := range f.EventIDs {
		fmt.Fprintf(&b, "- event #%s\n", id)
	}
	b.WriteString("\n## Severity\n\n")
	fmt.Fprintf(&b, "%s\n\n", f.Severity)
	b.WriteString("## Reopen trigger\n\n")
	b.WriteString("Re-fires increment the comment thread; mute via `regatta self-improve mute`.\n\n")
	b.WriteString("## Filter-out coordination (W5 cost-pause)\n\n")
	b.WriteString("This rule respects `filter_out: [regatta_pause_all]` — events tagged during a cost-pause window are excluded so the detector never blames agents for pause-induced halts.\n\n")
	fmt.Fprintf(&b, "---\ndedup-key: %s\nrule: %s\n", f.DedupKey, f.Rule)
	return title, b.String()
}

func formatGroupBy(g map[string]string) string {
	keys := make([]string, 0, len(g))
	for k := range g {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, g[k]))
	}
	return strings.Join(parts, ", ")
}
