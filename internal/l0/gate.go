package l0

import (
	"fmt"
	"strings"
	"time"

	"github.com/trilamsr/regatta/schemas"
)

// Config controls which files L0 treats as spec sources. For the
// markdown_catalog adapter this is a list of suffixes or globs; for
// API-backed adapters L0 fetches the source body directly and synthesizes
// a diff against the recorded SourceRef.
type Config struct {
	// SpecPathSuffixes is the list of path suffixes L0 should treat as
	// spec sources. Common values: ".md", "MILESTONES.md".
	SpecPathSuffixes []string
}

// Default returns a Config that treats any markdown file as a spec source.
func Default() Config {
	return Config{SpecPathSuffixes: []string{".md"}}
}

// Check runs L0 against a parsed diff and returns a GateResult.
func Check(cfg Config, changes []FileChange) schemas.GateResult {
	start := time.Now()
	out := schemas.GateResult{
		GateID:   "l0_spec_immutability",
		GateKind: "deterministic",
		Verdict:  schemas.VerdictPass,
	}
	for _, fc := range changes {
		if !cfg.isSpecPath(fc) {
			continue
		}
		out.Findings = append(out.Findings, compareCriteria(fc)...)
	}
	for _, f := range out.Findings {
		if f.Blocking {
			out.Verdict = schemas.VerdictFail
			break
		}
	}
	out.Telemetry = schemas.Telemetry{
		StartedAt:  start,
		DurationMs: time.Since(start).Milliseconds(),
	}
	return out
}

func (c Config) isSpecPath(fc FileChange) bool {
	p := fc.NewPath
	if p == "" || p == "/dev/null" {
		p = fc.OldPath
	}
	for _, sfx := range c.SpecPathSuffixes {
		if strings.HasSuffix(p, sfx) {
			return true
		}
	}
	return false
}

func compareCriteria(fc FileChange) []schemas.Finding {
	oldCs := Extract(fc.Old)
	newCs := Extract(fc.New)
	path := fc.NewPath
	if path == "" || path == "/dev/null" {
		path = fc.OldPath
	}

	var findings []schemas.Finding

	if len(oldCs) != len(newCs) {
		findings = append(findings, schemas.Finding{
			Severity:    schemas.SeverityCritical,
			Message:     fmt.Sprintf("criterion count changed: %d → %d (spec criteria are immutable; only state flips are permitted)", len(oldCs), len(newCs)),
			Path:        path,
			TrapPattern: "P3",
			Blocking:    true,
		})
		return findings
	}

	for i := range oldCs {
		o, n := oldCs[i], newCs[i]
		oNorm := Normalize(o.Text)
		nNorm := Normalize(n.Text)
		if oNorm != nNorm {
			findings = append(findings, schemas.Finding{
				Severity:    schemas.SeverityCritical,
				Message:     fmt.Sprintf("criterion text edited (line %d): %q → %q", n.Line, o.Text, n.Text),
				Path:        path,
				Line:        n.Line,
				TrapPattern: "P3",
				Blocking:    true,
			})
			continue
		}
		// Text identical after normalization. Validate state transitions.
		if o.State == StatePlanned && n.State == StateDone {
			if n.Citation == "" {
				findings = append(findings, schemas.Finding{
					Severity:    schemas.SeverityCritical,
					Message:     fmt.Sprintf("criterion flipped to done without required citation (line %d)", n.Line),
					Path:        path,
					Line:        n.Line,
					TrapPattern: "P6",
					Blocking:    true,
				})
			}
		}
		if o.State == StateDone && n.State == StatePlanned {
			findings = append(findings, schemas.Finding{
				Severity:    schemas.SeverityCritical,
				Message:     fmt.Sprintf("criterion reverted from done → planned (line %d); state monotonicity violated", n.Line),
				Path:        path,
				Line:        n.Line,
				TrapPattern: "P3",
				Blocking:    true,
			})
		}
	}
	return findings
}
