package digest

import (
	"math"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

// TestRender_FrontMatterShape locks the spec §6.2 front-matter key set + declaration order.
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

// TestRender_DegradedPlaceholdersVerbatim locks the amendment §4 verbatim strings C-T2 + D-T1 grep for.
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

// TestRender_ZeroPRDayShowsContext locks the spec §9 R6 banner on PRsLandedCount=0.
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

// TestRender_BackendDownShowsBanner locks the spec §9 R5 mirror banner on backendUp=false.
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

// TestRender_Deterministic locks A+4 rubric: same Snapshot → byte-equal markdown across N renders.
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

// TestRender_FrontMatterMatchesBody locks spec §6.2 lock-step: every front-matter key has a body header.
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

// TestRender_ValidatesDate rejects malformed --date at the renderer seam (defence against bad cron input).
func TestRender_ValidatesDate(t *testing.T) {
	_, err := Render(Options{Date: "yesterday", Source: stubSource{backendUp: true}})
	if err == nil {
		t.Fatal("expected error for malformed date; got nil")
	}
}

// TestDigest_RenderProducesValidYAML locks the §6.2 front-matter contract
// end-to-end (S1 finding): the rendered
// markdown's `---`-fenced YAML front-matter parses cleanly via
// yaml.Unmarshal and every numeric scalar is finite. A regression that lets
// NaN/Inf reach the formatter would either fail YAML parsing or surface as
// a non-finite float here.
func TestDigest_RenderProducesValidYAML(t *testing.T) {
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
				{Name: "phase_g_gate", DaysRemaining: 27},
				{Name: "30_day_green", DaysRemaining: 17},
			},
		},
		backendUp: true,
	}
	out, err := Render(Options{Date: "2026-06-03", Source: src})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Front-matter is the bytes between the first two `---` fences.
	const fence = "---\n"
	first := strings.Index(out, fence)
	if first != 0 {
		t.Fatalf("front-matter does not start at offset 0 (got %d):\n%s", first, out)
	}
	rest := out[len(fence):]
	second := strings.Index(rest, fence)
	if second < 0 {
		t.Fatalf("front-matter missing closing fence:\n%s", out)
	}
	fm := rest[:second]

	var parsed struct {
		Date            string         `yaml:"date"`
		TickP95Ms       float64        `yaml:"tick_p95_ms"`
		PRsLandedCount  float64        `yaml:"prs_landed_count"`
		CostUSDToday    float64        `yaml:"cost_usd_today"`
		CostUSDWeek     float64        `yaml:"cost_usd_week"`
		ChainBreaks     float64        `yaml:"chain_breaks"`
		DivergenceCount float64        `yaml:"divergence_count"`
		AlarmsFired     float64        `yaml:"alarms_fired"`
		Triggers        map[string]int `yaml:"triggers"`
	}
	if err := yaml.Unmarshal([]byte(fm), &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal front-matter: %v\n---\n%s", err, fm)
	}
	if parsed.Date != "2026-06-03" {
		t.Errorf("date = %q; want 2026-06-03", parsed.Date)
	}
	// Every numeric must be finite. A future NaN/Inf leak would either
	// trip the parse above (strict YAML) or surface here on a permissive
	// parser path.
	for name, f := range map[string]float64{
		"tick_p95_ms":      parsed.TickP95Ms,
		"prs_landed_count": parsed.PRsLandedCount,
		"cost_usd_today":   parsed.CostUSDToday,
		"cost_usd_week":    parsed.CostUSDWeek,
		"chain_breaks":     parsed.ChainBreaks,
		"divergence_count": parsed.DivergenceCount,
		"alarms_fired":     parsed.AlarmsFired,
	} {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			t.Errorf("front-matter %s = %v; want finite", name, f)
		}
	}
	if got, want := parsed.Triggers["phase_g_gate"], 27; got != want {
		t.Errorf("triggers.phase_g_gate = %d; want %d", got, want)
	}
}
