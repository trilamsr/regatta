// Package securitygate is the MVP-3 hybrid security gate. See
// gates/security/README.md for the design contract and
// docs/design.md §Missions §Security custom gate.
//
// Phase: SKELETON. The deterministic floor wires gitleaks /
// osv-scanner shell-outs and parses their JSON output into the
// existing schemas.GateResult shape. The AI phase is stubbed --
// it returns a single advisory finding pointing at the prompt
// template that needs to land before MVP-3 ships.
//
// This file is deliberately small. Real expansion happens in
// `internal/securitygate/{floor,ai,trap_pattern_map}.go` once
// the canary fixtures in gates/security/testdata/ are populated.
package securitygate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/trilamsr/regatta/schemas"
)

// Config is the per-repo gate config (the `regatta.yaml` `gates:`
// row mapped into Go). See gates/security/README.md for the YAML
// shape.
type Config struct {
	GateID            string
	DeterminismFloor  FloorConfig
	AI                AIConfig
	SeverityBlock     []string // mini-DSL: ["critical","2*high"]
}

type FloorConfig struct {
	Gitleaks    ToolPin
	OSVScanner  ToolPin
	Semgrep     ToolPin
	Syft        ToolPin
}

type ToolPin struct {
	Enabled bool
	Version string // pinned via safety.tool_versions
}

type AIConfig struct {
	Enabled            bool
	Model              string
	Mode               string   // "adversarial" | "judicial"
	MinFalsifications  int
	LineageIsolation   string   // "cross_family"
	TrapPatternFloor   []string // ["P1","P2",...]
}

// Input is what the gate runs against. Constructed by the gate
// runner from the PR head SHA + diff.
type Input struct {
	PRSHA    string
	BaseSHA  string
	DiffPath string // unified diff file path
	RepoRoot string // local worktree root
	RunID    string // uuid; idempotency key shared across re-runs at same PR SHA
}

// Run executes the gate against input and returns a GateResult.
//
// Order is normative: deterministic floor FIRST, AI phase only on
// floor-pass. This is P1 ("deterministic before AI on destructive
// ops") applied at gate level.
func Run(ctx context.Context, cfg Config, in Input) (schemas.GateResult, error) {
	started := time.Now()
	gr := schemas.GateResult{
		GateID:   cfg.GateID,
		GateKind: "deterministic", // promoted to ai_adversarial if AI phase runs
		PRSha:    in.PRSHA,
		RunID:    in.RunID,
		Verdict:  schemas.VerdictPass,
		Findings: []schemas.Finding{},
		Telemetry: schemas.Telemetry{
			StartedAt: started,
		},
	}

	floorFindings, err := runFloor(ctx, cfg.DeterminismFloor, in)
	if err != nil {
		gr.Verdict = schemas.VerdictFail
		gr.Findings = append(gr.Findings, schemas.Finding{
			Severity: schemas.SeverityHigh,
			Message:  fmt.Sprintf("deterministic floor errored: %v", err),
			Blocking: true,
		})
		gr.Telemetry.DurationMs = time.Since(started).Milliseconds()
		return gr, err
	}
	gr.Findings = append(gr.Findings, floorFindings...)

	if anyBlocking(gr.Findings) {
		gr.Verdict = schemas.VerdictFail
		gr.Telemetry.DurationMs = time.Since(started).Milliseconds()
		return gr, nil // floor failure short-circuits; no AI spend
	}

	if cfg.AI.Enabled {
		gr.GateKind = "ai_adversarial"
		aiFindings, err := runAI(ctx, cfg.AI, in)
		if err != nil {
			gr.Findings = append(gr.Findings, schemas.Finding{
				Severity: schemas.SeverityMedium,
				Message:  fmt.Sprintf("AI phase errored: %v", err),
				Blocking: false, // advisory; deterministic floor remains the hard bar
			})
		}
		gr.Findings = append(gr.Findings, aiFindings...)
	}

	if anyBlocking(gr.Findings) {
		gr.Verdict = schemas.VerdictFail
	}
	gr.Telemetry.DurationMs = time.Since(started).Milliseconds()
	return gr, nil
}

// runFloor invokes each enabled static tool. Errors propagate; a
// tool failure is itself a gate-level signal.
func runFloor(ctx context.Context, fc FloorConfig, in Input) ([]schemas.Finding, error) {
	var out []schemas.Finding

	if fc.Gitleaks.Enabled {
		fs, err := runGitleaks(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("gitleaks: %w", err)
		}
		out = append(out, fs...)
	}
	if fc.OSVScanner.Enabled {
		fs, err := runOSVScanner(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("osv-scanner: %w", err)
		}
		out = append(out, fs...)
	}
	// semgrep / syft wire in the same shape. Deferred until fixtures
	// exist that exercise their output shapes deterministically.
	return out, nil
}

// gitleaksFinding is the subset of gitleaks's JSON output that
// maps onto schemas.Finding. Field names match gitleaks v8.
type gitleaksFinding struct {
	RuleID      string `json:"RuleID"`
	Description string `json:"Description"`
	File        string `json:"File"`
	StartLine   int    `json:"StartLine"`
	Secret      string `json:"Secret"` // never include in Finding.Message -- leakage risk
}

func runGitleaks(ctx context.Context, in Input) ([]schemas.Finding, error) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		return nil, fmt.Errorf("gitleaks binary not on PATH (pin via safety.tool_versions): %w", err)
	}
	cmd := exec.CommandContext(ctx, "gitleaks", "detect",
		"--source", in.RepoRoot,
		"--no-git",
		"--report-format=json",
		"--report-path=-",
	)
	raw, err := cmd.Output()
	// Exit 1 from gitleaks means "secrets found" -- that's a SUCCESS
	// in the sense that we got parseable output; only treat
	// non-1/non-0 as a tool error.
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return nil, fmt.Errorf("invoke gitleaks: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var findings []gitleaksFinding
	if err := json.Unmarshal(raw, &findings); err != nil {
		return nil, fmt.Errorf("parse gitleaks output: %w", err)
	}
	out := make([]schemas.Finding, 0, len(findings))
	for _, f := range findings {
		out = append(out, schemas.Finding{
			Severity:    schemas.SeverityCritical, // secret-in-source is always critical
			Message:     fmt.Sprintf("gitleaks: %s -- %s", f.RuleID, f.Description),
			Path:        f.File,
			Line:        f.StartLine,
			TrapPattern: "P4", // P4: least-privilege ephemeral creds
			Blocking:    true,
		})
	}
	return out, nil
}

// osvFinding mirrors the subset of osv-scanner JSON we read.
type osvScanResult struct {
	Results []struct {
		Packages []struct {
			Package struct {
				Name      string `json:"name"`
				Version   string `json:"version"`
				Ecosystem string `json:"ecosystem"`
			} `json:"package"`
			Vulnerabilities []struct {
				ID       string   `json:"id"`
				Summary  string   `json:"summary"`
				Severity []struct {
					Type  string `json:"type"`
					Score string `json:"score"`
				} `json:"severity"`
			} `json:"vulnerabilities"`
		} `json:"packages"`
	} `json:"results"`
}

func runOSVScanner(ctx context.Context, in Input) ([]schemas.Finding, error) {
	if _, err := exec.LookPath("osv-scanner"); err != nil {
		return nil, fmt.Errorf("osv-scanner binary not on PATH (pin via safety.tool_versions): %w", err)
	}
	cmd := exec.CommandContext(ctx, "osv-scanner", "--format=json", in.RepoRoot)
	raw, err := cmd.Output()
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return nil, fmt.Errorf("invoke osv-scanner: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var res osvScanResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("parse osv-scanner output: %w", err)
	}
	var out []schemas.Finding
	for _, r := range res.Results {
		for _, p := range r.Packages {
			for _, v := range p.Vulnerabilities {
				sev := schemas.SeverityHigh
				if vSev := highestSeverity(v.Severity); vSev != "" {
					sev = mapCVSSToSeverity(vSev)
				}
				out = append(out, schemas.Finding{
					Severity:    sev,
					Message:     fmt.Sprintf("osv: %s in %s@%s -- %s", v.ID, p.Package.Name, p.Package.Version, v.Summary),
					TrapPattern: "P11", // supply chain
					Blocking:    sev == schemas.SeverityCritical || sev == schemas.SeverityHigh,
				})
			}
		}
	}
	return out, nil
}

func highestSeverity(scores []struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}) string {
	for _, s := range scores {
		if s.Type == "CVSS_V3" || s.Type == "CVSS_V4" {
			return s.Score
		}
	}
	return ""
}

// mapCVSSToSeverity converts a CVSS vector or numeric score to our
// severity enum. Conservative: anything we can't parse is "high".
func mapCVSSToSeverity(score string) schemas.Severity {
	if len(score) == 0 {
		return schemas.SeverityHigh
	}
	// CVSS vector strings start with "CVSS:" -- we don't parse the
	// vector at this layer. The osv-scanner output usually also
	// carries a numeric score in a sibling field for newer schemas;
	// when it does, callers should branch on that. For v1, conservative.
	return schemas.SeverityHigh
}

// runAI is the stub for the threat-modeler subagent. It returns
// one advisory finding pointing at the prompt template that needs
// to land. Real implementation follows the contract in
// gates/security/README.md §AI phase.
func runAI(_ context.Context, cfg AIConfig, _ Input) ([]schemas.Finding, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	return []schemas.Finding{{
		Severity:    schemas.SeverityInfo,
		Message:     "security gate AI phase: prompts/security_gate.md not yet implemented (MVP-3); contract in gates/security/README.md",
		TrapPattern: "",
		Blocking:    false,
	}}, nil
}

func anyBlocking(fs []schemas.Finding) bool {
	for _, f := range fs {
		if f.Blocking {
			return true
		}
	}
	return false
}
