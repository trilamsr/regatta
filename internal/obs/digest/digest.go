// Package digest renders the daily operator digest per spec
// docs/engineer/specs/phase-x/2026-06-02-observability-roadmap.md §6.2.
//
// The package splits "fetch metrics" from "render markdown" so tests
// inject a stub Source and assert on the rendered bytes — the cmd
// wrapper plugs the real Prometheus-backed Source on top.
//
// First-digest degraded contract (spec §6.2 + brief amendment §4):
// sections that depend on emitters from later waves render the verbatim
// placeholder lines whose strings are grepped for by the C-T2 / D-T1
// implementers when those emitters land. The strings live in
// degradedPlaceholders[] below and MUST NOT be reworded without a
// corresponding edit in the C-T2 and D-T1 dispatch briefs.
package digest

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Snapshot is the in-memory shape the renderer consumes. Source.Fetch
// fills it; tests construct it inline. Field order mirrors spec §6.2
// front-matter declaration order so a renderer that emits "in struct
// order" stays trivially aligned with the spec.
type Snapshot struct {
	TickP95Ms       int
	PRsLandedCount  int
	CostUSDToday    float64
	CostUSDWeek     float64
	ChainBreaks     int
	DivergenceCount int
	AlarmsFired     int
	Triggers        []TriggerCountdown
	CostByDAG       map[string]float64
	FollowupsFiled  int
}

// TriggerCountdown is one row in the trigger-clock front-matter map.
// Slice (not map) preserves declaration order across renders.
type TriggerCountdown struct {
	Name          string
	DaysRemaining int
}

// Source supplies a metrics Snapshot for a given UTC date. Returns the
// snapshot, a backendUp flag (false → render fires the unreachable
// banner per spec §9 R5 mirror), and any unrecoverable fetch error.
// Implementations: PromSource (real) + stub (tests).
type Source interface {
	Fetch(date string) (Snapshot, bool, error)
}

// Options bundles the renderer inputs. Source is required; Date is the
// UTC day formatted YYYY-MM-DD.
type Options struct {
	Date   string
	Source Source
}

// degradedPlaceholders pins the spec §6.2 + brief amendment §4 verbatim
// strings. C-T2 and D-T1 implementer subagents grep for these exact
// bytes when removing the placeholder lines as part of their PRs.
const (
	prsLandedPlaceholder    = "PRs landed — emitter ships C-T2 (Wave C); see #<issue>"
	adversarialPlaceholder  = "Adversarial findings — emitter ships D-T1 (Wave D); see #<issue>"
	zeroPRDayBanner         = "Loop quiet (Phase-S relaxation hold? CI-block? operator vacation?)"
	backendDownBanner       = "metrics backend unreachable — sqlite fallback active (numbers may be stale; see §9 R5)"
	dateLayout              = "2006-01-02"
)

// Render produces the digest markdown. Deterministic for fixed inputs:
// maps sorted alphabetically, floats formatted with %.2f, integers
// printed without separators. A+4 rubric anchor.
func Render(opts Options) (string, error) {
	if _, err := time.Parse(dateLayout, opts.Date); err != nil {
		return "", fmt.Errorf("digest: --date %q: want YYYY-MM-DD: %w", opts.Date, err)
	}
	if opts.Source == nil {
		return "", fmt.Errorf("digest: Options.Source is nil")
	}
	snap, backendUp, err := opts.Source.Fetch(opts.Date)
	if err != nil {
		return "", fmt.Errorf("digest: fetch metrics: %w", err)
	}

	var b strings.Builder
	writeFrontMatter(&b, opts.Date, snap)
	writeBody(&b, snap, backendUp)
	return b.String(), nil
}

// writeFrontMatter emits the YAML block in spec-declaration order so
// downstream `yq` consumers do not depend on map iteration order
// (deterministic-output rubric).
func writeFrontMatter(b *strings.Builder, date string, s Snapshot) {
	b.WriteString("---\n")
	b.WriteString("# machine-readable front-matter — keep in lock-step with markdown body below\n")
	fmt.Fprintf(b, "date: %s\n", date)
	fmt.Fprintf(b, "tick_p95_ms: %d\n", s.TickP95Ms)
	fmt.Fprintf(b, "prs_landed_count: %d\n", s.PRsLandedCount)
	fmt.Fprintf(b, "cost_usd_today: %.2f\n", s.CostUSDToday)
	fmt.Fprintf(b, "cost_usd_week: %.2f\n", s.CostUSDWeek)
	fmt.Fprintf(b, "chain_breaks: %d\n", s.ChainBreaks)
	fmt.Fprintf(b, "divergence_count: %d\n", s.DivergenceCount)
	fmt.Fprintf(b, "alarms_fired: %d\n", s.AlarmsFired)
	b.WriteString("triggers:\n")
	// Trigger slice already carries declaration order from the source;
	// stable-sort by name as a determinism backstop in case a caller
	// hands us an out-of-order slice.
	trigs := append([]TriggerCountdown(nil), s.Triggers...)
	sort.SliceStable(trigs, func(i, j int) bool { return trigs[i].Name < trigs[j].Name })
	for _, t := range trigs {
		fmt.Fprintf(b, "  %s: %d\n", t.Name, t.DaysRemaining)
	}
	b.WriteString("---\n\n")
}

// writeBody emits the 7 markdown sections (spec §6.2). Section 2 (PRs
// landed) and section 3 (Adversarial findings) render the verbatim
// degraded-contract placeholders until the C-T2 / D-T1 emitters land.
func writeBody(b *strings.Builder, s Snapshot, backendUp bool) {
	fmt.Fprintf(b, "# Daily digest\n\n")
	if !backendUp {
		fmt.Fprintf(b, "> %s\n\n", backendDownBanner)
	}
	if s.PRsLandedCount == 0 {
		fmt.Fprintf(b, "> %s\n\n", zeroPRDayBanner)
	}

	b.WriteString("## Loop health\n\n")
	fmt.Fprintf(b, "- tick p95: %d ms\n", s.TickP95Ms)
	fmt.Fprintf(b, "- alarms fired: %d\n\n", s.AlarmsFired)

	// Section 2 — PRs landed. Degraded contract until C-T2 ships.
	// Section is rendered always (no silent zero) but the per-PR table
	// is suppressed because the emitter is not yet wired.
	b.WriteString("## PRs landed\n\n")
	fmt.Fprintf(b, "%s\n\n", prsLandedPlaceholder)
	fmt.Fprintf(b, "- count (from substrate merge events): %d\n\n", s.PRsLandedCount)

	// Section 3 — Adversarial findings. Degraded contract until D-T1 ships.
	b.WriteString("## Adversarial findings\n\n")
	fmt.Fprintf(b, "%s\n\n", adversarialPlaceholder)

	b.WriteString("## Substrate health\n\n")
	fmt.Fprintf(b, "- chain breaks (target 0): %d\n", s.ChainBreaks)
	fmt.Fprintf(b, "- divergence count: %d\n\n", s.DivergenceCount)

	b.WriteString("## Cost\n\n")
	fmt.Fprintf(b, "- USD today: %.2f\n", s.CostUSDToday)
	fmt.Fprintf(b, "- USD week-to-date: %.2f\n", s.CostUSDWeek)
	if len(s.CostByDAG) > 0 {
		b.WriteString("- by DAG:\n")
		names := make([]string, 0, len(s.CostByDAG))
		for name := range s.CostByDAG {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(b, "  - %s: %.2f\n", name, s.CostByDAG[name])
		}
	}
	b.WriteString("\n")

	b.WriteString("## Triggers\n\n")
	trigs := append([]TriggerCountdown(nil), s.Triggers...)
	sort.SliceStable(trigs, func(i, j int) bool { return trigs[i].Name < trigs[j].Name })
	for _, t := range trigs {
		fmt.Fprintf(b, "- %s: %d days remaining\n", t.Name, t.DaysRemaining)
	}
	b.WriteString("\n")

	b.WriteString("## Followups filed\n\n")
	fmt.Fprintf(b, "- count: %d\n", s.FollowupsFiled)
}
