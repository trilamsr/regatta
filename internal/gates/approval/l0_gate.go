package approval

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/obs"
)

// L0Config controls which files L0 treats as spec sources; for the
// markdown_catalog adapter this is a list of suffixes or globs.
type L0Config struct {
	// SpecPathSuffixes is the list of path suffixes L0 should treat as
	// spec sources. Common values: ".md", "MILESTONES.md".
	SpecPathSuffixes []string

	// Logger is the structured-event sink for gate.verdict records.
	// Nil falls back to slog.Default() so embedded callers still get
	// output without panicking (spec §4.1, §5.5).
	Logger *slog.Logger

	// Clock is the wall-clock source for telemetry stamps. Nil falls
	// back to time.Now. Same shape as the rest of the regatta clock
	// seam so tests pin one fake clock across gate + state.
	Clock func() time.Time
}

// L0Default returns a Config that treats any markdown file as a spec source.
func L0Default() L0Config {
	return L0Config{SpecPathSuffixes: []string{".md"}}
}

// L0Check runs L0 against a parsed diff and returns a GateResult.
func L0Check(cfg L0Config, changes []L0FileChange) schemas.GateResult {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	start := clock()
	out := schemas.GateResult{
		SchemaVersion: 1,
		GateID:        "l0_spec_immutability",
		GateKind:      schemas.GateKindDeterministic,
		Verdict:       schemas.VerdictPass,
	}
	for _, fc := range changes {
		if !cfg.isSpecPath(fc) {
			continue
		}
		out.Findings = append(out.Findings, compareCriteria(fc)...)
	}
	// L0 emits only critical findings; any finding is blocking.
	if len(out.Findings) > 0 {
		out.Verdict = schemas.VerdictFail
		out.Blocking = true
		out.Severity = schemas.SeverityCritical
	}
	finishedAt := clock()
	out.Telemetry = schemas.Telemetry{
		DurationMs: finishedAt.Sub(start).Milliseconds(),
	}
	out.Heartbeat = schemas.TelemetryHeartbeat{
		StartedAt:  start,
		FinishedAt: finishedAt,
	}

	emitVerdict(cfg.Logger, out)
	return out
}

// emitVerdict records a single structured gate.verdict event so
// operators can grep gate decisions without parsing GateResult JSON
// blobs out of the events table (spec §5.5).
func emitVerdict(log *slog.Logger, gr schemas.GateResult) {
	if log == nil {
		log = slog.Default()
	}
	reason := ""
	if len(gr.Findings) > 0 {
		reason = gr.Findings[0].ID
	}
	log.Info(string(obs.EventGateVerdict),
		string(obs.KeyGateID), "l0",
		string(obs.KeyVerdict), string(gr.Verdict),
		string(obs.KeyReason), reason,
		string(obs.KeyDurationMs), gr.Telemetry.DurationMs,
	)
}

func (c L0Config) isSpecPath(fc L0FileChange) bool {
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

func compareCriteria(fc L0FileChange) []schemas.Finding {
	oldCs := l0Extract(fc.Old)
	newCs := l0Extract(fc.New)
	path := fc.NewPath
	if path == "" || path == "/dev/null" {
		path = fc.OldPath
	}

	var findings []schemas.Finding

	if len(oldCs) != len(newCs) {
		findings = append(findings, schemas.Finding{
			ID:          "L0-COUNT",
			Severity:    schemas.FindingCritical,
			Claim:       fmt.Sprintf("criterion count changed: %d -> %d (spec criteria are immutable; only state flips are permitted)", len(oldCs), len(newCs)),
			Evidence:    &schemas.FindingEvidence{Path: path},
			TrapPattern: "P3",
		})
		return findings
	}

	for i := range oldCs {
		o, n := oldCs[i], newCs[i]
		oNorm := l0Normalize(o.Text)
		nNorm := l0Normalize(n.Text)
		if oNorm != nNorm {
			findings = append(findings, schemas.Finding{
				ID:       fmt.Sprintf("L0-TEXT-%d", i),
				Severity: schemas.FindingCritical,
				Claim:    fmt.Sprintf("criterion text edited (line %d): %q -> %q", n.Line, o.Text, n.Text),
				Evidence: &schemas.FindingEvidence{Path: path, LineStart: n.Line},
				TrapPattern: "P3",
			})
			continue
		}
		// Text identical after normalization. Validate state transitions.
		if o.State == L0StatePlanned && n.State == L0StateDone {
			if n.Citation == "" {
				findings = append(findings, schemas.Finding{
					ID:       fmt.Sprintf("L0-CITATION-%d", i),
					Severity: schemas.FindingCritical,
					Claim:    fmt.Sprintf("criterion flipped to done without required citation (line %d)", n.Line),
					Evidence: &schemas.FindingEvidence{Path: path, LineStart: n.Line},
					TrapPattern: "P6",
				})
			}
		}
		if o.State == L0StateDone && n.State == L0StatePlanned {
			findings = append(findings, schemas.Finding{
				ID:       fmt.Sprintf("L0-REVERT-%d", i),
				Severity: schemas.FindingCritical,
				Claim:    fmt.Sprintf("criterion reverted from done -> planned (line %d); state monotonicity violated", n.Line),
				Evidence: &schemas.FindingEvidence{Path: path, LineStart: n.Line},
				TrapPattern: "P3",
			})
		}
	}
	return findings
}
