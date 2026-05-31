package l0

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/obs"
)

// Config controls which files L0 treats as spec sources. For the
// markdown_catalog adapter this is a list of suffixes or globs; for
// API-backed adapters L0 fetches the source body directly and synthesizes
// a diff against the recorded SourceRef.
type Config struct {
	// SpecPathSuffixes is the list of path suffixes L0 should treat as
	// spec sources. Common values: ".md", "MILESTONES.md".
	SpecPathSuffixes []string

	// Logger is the structured-event sink for gate.verdict records.
	// Nil falls back to slog.Default() so embedded callers still get
	// output without panicking (spec §4.1, §5.5).
	Logger *slog.Logger
}

// Default returns a Config that treats any markdown file as a spec source.
func Default() Config {
	return Config{SpecPathSuffixes: []string{".md"}}
}

// Check runs L0 against a parsed diff and returns a GateResult.
func Check(cfg Config, changes []FileChange) schemas.GateResult {
	start := time.Now()
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
	out.Telemetry = schemas.Telemetry{
		DurationMs: time.Since(start).Milliseconds(),
	}
	out.Heartbeat = schemas.TelemetryHeartbeat{
		StartedAt:  start,
		FinishedAt: time.Now(),
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
		oNorm := Normalize(o.Text)
		nNorm := Normalize(n.Text)
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
		if o.State == StatePlanned && n.State == StateDone {
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
		if o.State == StateDone && n.State == StatePlanned {
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
