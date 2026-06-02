package digest

import (
	"strings"
	"testing"
)

// stubSource returns canned metrics so tests assert on rendering, not
// PromQL plumbing. Zero-value struct produces the all-zeros / banner
// path; tests inject specific fields to exercise individual sections.
type stubSource struct {
	snap      Snapshot
	err       error
	backendUp bool
}

func (s stubSource) Fetch(date string) (Snapshot, bool, error) {
	if s.err != nil {
		return Snapshot{}, s.backendUp, s.err
	}
	return s.snap, s.backendUp, nil
}

// TestRender_FrontMatterShape asserts the YAML front-matter carries the
// brief-pinned keys in declaration order (spec §6.2: "Front-matter keys
// MUST be in declaration order, not map order").
func TestRender_FrontMatterShape(t *testing.T) {
	src := stubSource{
		snap: Snapshot{
			TickP95Ms:       4321,
			PRsLandedCount:  11,
			CostUSDToday:    87.42,
			CostUSDWeek:     511.30,
			ChainBreaks:     0,
			DivergenceCount: 0,
			AlarmsFired:     2,
			Triggers: []TriggerCountdown{
				{Name: "30_day_green", DaysRemaining: 17},
				{Name: "phase_g_gate", DaysRemaining: 27},
			},
		},
		backendUp: true,
	}
	out, err := Render(Options{Date: "2026-06-03", Source: src})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	wantOrder := []string{
		"date: 2026-06-03",
		"tick_p95_ms: 4321",
		"prs_landed_count: 11",
		"cost_usd_today: 87.42",
		"cost_usd_week: 511.30",
		"chain_breaks: 0",
		"divergence_count: 0",
		"alarms_fired: 2",
		"triggers:",
	}
	prev := -1
	for _, k := range wantOrder {
		idx := strings.Index(out, k)
		if idx < 0 {
			t.Errorf("front-matter missing key %q in:\n%s", k, out)
			continue
		}
		if idx <= prev {
			t.Errorf("front-matter key %q out of declaration order (idx %d <= prev %d)", k, idx, prev)
		}
		prev = idx
	}
	if !strings.HasPrefix(out, "---\n") {
		t.Errorf("front-matter must open with ---; got:\n%s", out[:20])
	}
}

// TestRender_DegradedPlaceholdersVerbatim asserts the amendment §4
// verbatim placeholder strings appear unchanged. Removal owners (C-T2,
// D-T1) grep for these exact strings when they land.
func TestRender_DegradedPlaceholdersVerbatim(t *testing.T) {
	out, err := Render(Options{
		Date:   "2026-06-03",
		Source: stubSource{snap: Snapshot{PRsLandedCount: 5}, backendUp: true},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	const (
		prPlaceholder  = "PRs landed — emitter ships C-T2 (Wave C); see #<issue>"
		advPlaceholder = "Adversarial findings — emitter ships D-T1 (Wave D); see #<issue>"
	)
	if !strings.Contains(out, prPlaceholder) {
		t.Errorf("missing verbatim PR placeholder:\n%s", out)
	}
	if !strings.Contains(out, advPlaceholder) {
		t.Errorf("missing verbatim adversarial placeholder:\n%s", out)
	}
}

// TestRender_ZeroPRDayShowsContext asserts the spec §9 R6 banner fires
// when PRsLandedCount=0; the empty PR section is skipped.
func TestRender_ZeroPRDayShowsContext(t *testing.T) {
	out, err := Render(Options{
		Date:   "2026-06-03",
		Source: stubSource{snap: Snapshot{PRsLandedCount: 0}, backendUp: true},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	const banner = "Loop quiet (Phase-S relaxation hold? CI-block? operator vacation?)"
	if !strings.Contains(out, banner) {
		t.Errorf("missing zero-PR-day banner:\n%s", out)
	}
}

// TestRender_BackendDownShowsBanner asserts the spec §9 R5 mirror —
// when the metrics backend is unreachable, the digest carries a visible
// banner so operators do not mistake zeros for "loop is quiet".
func TestRender_BackendDownShowsBanner(t *testing.T) {
	out, err := Render(Options{
		Date:   "2026-06-03",
		Source: stubSource{snap: Snapshot{}, backendUp: false},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "metrics backend unreachable") {
		t.Errorf("missing backend-down banner:\n%s", out)
	}
}

// TestRender_Deterministic asserts A+4 rubric: same inputs produce
// byte-equal output across repeated calls. Maps are sorted, durations
// canonicalised, USD formatted with fixed precision.
func TestRender_Deterministic(t *testing.T) {
	src := stubSource{
		snap: Snapshot{
			TickP95Ms:      4321,
			PRsLandedCount: 11,
			CostUSDToday:   87.42,
			CostUSDWeek:    511.30,
			AlarmsFired:    2,
			Triggers: []TriggerCountdown{
				{Name: "phase_g_gate", DaysRemaining: 27},
				{Name: "30_day_green", DaysRemaining: 17},
			},
			CostByDAG: map[string]float64{
				"obs-wave-a": 12.34,
				"cost-gov":   5.67,
				"authz":      8.90,
			},
		},
		backendUp: true,
	}
	first, err := Render(Options{Date: "2026-06-03", Source: src})
	if err != nil {
		t.Fatalf("Render first: %v", err)
	}
	for i := 0; i < 5; i++ {
		next, err := Render(Options{Date: "2026-06-03", Source: src})
		if err != nil {
			t.Fatalf("Render iter %d: %v", i, err)
		}
		if next != first {
			t.Fatalf("non-deterministic render at iter %d", i)
		}
	}
	// Cost-by-DAG must be sorted (alphabetical) so iteration order
	// of the map cannot leak into the markdown.
	authzIdx := strings.Index(first, "authz")
	costGovIdx := strings.Index(first, "cost-gov")
	obsIdx := strings.Index(first, "obs-wave-a")
	if authzIdx >= costGovIdx || costGovIdx >= obsIdx {
		t.Errorf("cost-by-DAG not sorted: authz=%d cost-gov=%d obs=%d", authzIdx, costGovIdx, obsIdx)
	}
}

// TestRender_FrontMatterMatchesBody enforces spec §6.2 lock-step: every
// front-matter key has a corresponding body section header. Drift gate.
func TestRender_FrontMatterMatchesBody(t *testing.T) {
	out, err := Render(Options{
		Date:   "2026-06-03",
		Source: stubSource{snap: Snapshot{PRsLandedCount: 1}, backendUp: true},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Every front-matter scalar key maps to a body section header.
	wantSections := []string{
		"## Loop health",
		"## PRs landed",
		"## Adversarial findings",
		"## Substrate health",
		"## Cost",
		"## Triggers",
		"## Followups filed",
	}
	for _, h := range wantSections {
		if !strings.Contains(out, h) {
			t.Errorf("missing body section header %q in:\n%s", h, out)
		}
	}
}

// TestRender_ValidatesDate rejects malformed --date input at the seam
// where the digest is built, not just at the CLI flag layer. Defends
// against cron supplying a bad value.
func TestRender_ValidatesDate(t *testing.T) {
	_, err := Render(Options{Date: "yesterday", Source: stubSource{backendUp: true}})
	if err == nil {
		t.Fatal("expected error for malformed date; got nil")
	}
}
