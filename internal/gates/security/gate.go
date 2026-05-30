// Package security is the MVP-3 hybrid security gate. See
// internal/gates/security/README.md for the design contract and
// docs/design.md §Programs §Security custom gate.
//
// Phase: SKELETON. The deterministic floor wires gitleaks /
// osv-scanner shell-outs and parses their JSON output into the
// existing schemas.GateResult shape. The AI phase is stubbed --
// it returns a single advisory finding pointing at the prompt
// template that needs to land before MVP-3 ships.
//
// This file is deliberately small. Real expansion happens in
// `internal/gates/security/{floor,ai,trap_pattern_map}.go` once
// the canary fixtures in testdata/gates/security/ are populated.
package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Config is the per-repo gate config (the `regatta.yaml` `gates:`
// row mapped into Go). See internal/gates/security/README.md for the YAML
// shape.
type Config struct {
	GateID            string
	DeterminismFloor  FloorConfig
	AI                AIConfig
	SeverityBlock     []string // mini-DSL: ["critical","2*high"]
}

// FloorConfig configures which deterministic-floor tools the gate
// invokes.
type FloorConfig struct {
	Gitleaks    ToolPin
	OSVScanner  ToolPin
	Semgrep     ToolPin
	Syft        ToolPin
}

// ToolPin pins one floor tool by version. Operator-supplied via
// safety.tool_versions in regatta.yaml.
type ToolPin struct {
	Enabled bool
	Version string // pinned via safety.tool_versions
}

// AIConfig configures the security gate's AI threat-modeler phase.
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
		SchemaVersion: 1,
		GateID:        cfg.GateID,
		GateKind:      schemas.GateKindDeterministic, // promoted to ai_adversarial if AI phase runs
		PRSHA:         in.PRSHA,
		RunID:         in.RunID,
		Verdict:       schemas.VerdictPass,
		Findings:      []schemas.Finding{},
		Telemetry: schemas.Telemetry{},
		Heartbeat: schemas.TelemetryHeartbeat{StartedAt: started},
	}

	floorFindings, blocking, err := runFloor(ctx, cfg.DeterminismFloor, in)
	if err != nil {
		gr.Verdict = schemas.VerdictFail
		gr.Blocking = true
		gr.Findings = append(gr.Findings, schemas.Finding{
			ID:       "FLOOR-ERR",
			Severity: schemas.FindingHigh,
			Claim:    fmt.Sprintf("deterministic floor errored: %v", err),
		})
		finalize(&gr, started)
		return gr, err
	}
	gr.Findings = append(gr.Findings, floorFindings...)
	if blocking {
		gr.Verdict = schemas.VerdictFail
		gr.Blocking = true
		finalize(&gr, started)
		return gr, nil // floor failure short-circuits; no AI spend
	}

	if cfg.AI.Enabled {
		gr.GateKind = schemas.GateKindAIAdversarial
		aiFindings, aiBlocking, err := runAI(ctx, cfg.AI, in)
		if err != nil {
			// Advisory only; deterministic floor remains the hard bar.
			gr.Findings = append(gr.Findings, schemas.Finding{
				ID:       "AI-ERR",
				Severity: schemas.FindingMedium,
				Claim:    fmt.Sprintf("AI phase errored: %v", err),
			})
		}
		gr.Findings = append(gr.Findings, aiFindings...)
		if aiBlocking {
			gr.Verdict = schemas.VerdictFail
			gr.Blocking = true
		}
	}

	finalize(&gr, started)
	return gr, nil
}

func finalize(gr *schemas.GateResult, started time.Time) {
	gr.Telemetry.DurationMs = time.Since(started).Milliseconds()
	gr.Heartbeat.FinishedAt = time.Now()
}

// runFloor invokes each enabled static tool. Returns findings, a
// boolean indicating whether any are blocking, and any tool error.
// A tool failure is itself a gate-level signal.
func runFloor(ctx context.Context, fc FloorConfig, in Input) ([]schemas.Finding, bool, error) {
	var out []schemas.Finding
	var blocking bool

	if fc.Gitleaks.Enabled {
		fs, b, err := runGitleaks(ctx, in)
		if err != nil {
			return nil, false, fmt.Errorf("gitleaks: %w", err)
		}
		out = append(out, fs...)
		blocking = blocking || b
	}
	if fc.OSVScanner.Enabled {
		fs, b, err := runOSVScanner(ctx, in)
		if err != nil {
			return nil, false, fmt.Errorf("osv-scanner: %w", err)
		}
		out = append(out, fs...)
		blocking = blocking || b
	}
	// semgrep / syft wire in the same shape. Deferred until fixtures
	// exist that exercise their output shapes deterministically.
	return out, blocking, nil
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

func runGitleaks(ctx context.Context, in Input) ([]schemas.Finding, bool, error) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		return nil, false, fmt.Errorf("gitleaks binary not on PATH (pin via safety.tool_versions): %w", err)
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
		return nil, false, fmt.Errorf("invoke gitleaks: %w", err)
	}
	if len(raw) == 0 {
		return nil, false, nil
	}
	var findings []gitleaksFinding
	if err := json.Unmarshal(raw, &findings); err != nil {
		return nil, false, fmt.Errorf("parse gitleaks output: %w", err)
	}
	out := make([]schemas.Finding, 0, len(findings))
	for i, f := range findings {
		out = append(out, schemas.Finding{
			ID:       fmt.Sprintf("GITLEAKS-%d", i),
			Severity: schemas.FindingCritical, // secret-in-source is always critical
			Claim:    fmt.Sprintf("gitleaks: %s -- %s", f.RuleID, f.Description),
			Evidence: &schemas.FindingEvidence{
				Path:      f.File,
				LineStart: f.StartLine,
			},
			TrapPattern: "P4", // P4: least-privilege ephemeral creds
		})
	}
	// Any gitleaks finding is blocking.
	return out, len(out) > 0, nil
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
				ID      string `json:"id"`
				Summary string `json:"summary"`
			} `json:"vulnerabilities"`
		} `json:"packages"`
	} `json:"results"`
}

func runOSVScanner(ctx context.Context, in Input) ([]schemas.Finding, bool, error) {
	if _, err := exec.LookPath("osv-scanner"); err != nil {
		return nil, false, fmt.Errorf("osv-scanner binary not on PATH (pin via safety.tool_versions): %w", err)
	}
	cmd := exec.CommandContext(ctx, "osv-scanner", "--format=json", in.RepoRoot)
	raw, err := cmd.Output()
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return nil, false, fmt.Errorf("invoke osv-scanner: %w", err)
	}
	if len(raw) == 0 {
		return nil, false, nil
	}
	var res osvScanResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, false, fmt.Errorf("parse osv-scanner output: %w", err)
	}
	var out []schemas.Finding
	var blocking bool
	for _, r := range res.Results {
		for _, p := range r.Packages {
			for _, v := range p.Vulnerabilities {
				sev := schemas.FindingHigh
				out = append(out, schemas.Finding{
					ID:          fmt.Sprintf("OSV-%s", v.ID),
					Severity:    sev,
					Claim:       fmt.Sprintf("osv: %s in %s@%s -- %s", v.ID, p.Package.Name, p.Package.Version, v.Summary),
					TrapPattern: "P11", // supply chain
				})
				if sev == schemas.FindingCritical || sev == schemas.FindingHigh {
					blocking = true
				}
			}
		}
	}
	return out, blocking, nil
}

// runAI is the stub for the threat-modeler subagent. It returns
// one advisory finding pointing at the prompt template that needs
// to land. Real implementation follows the contract in
// internal/gates/security/README.md §AI phase.
//
// Returns (findings, anyBlocking, error).
func runAI(_ context.Context, cfg AIConfig, _ Input) ([]schemas.Finding, bool, error) {
	if !cfg.Enabled {
		return nil, false, nil
	}
	return []schemas.Finding{{
		ID:       "AI-STUB",
		Severity: schemas.FindingInfo,
		Claim:    "security gate AI phase: prompts/security_gate.md not yet implemented (MVP-3); contract in internal/gates/security/README.md",
	}}, false, nil
}
