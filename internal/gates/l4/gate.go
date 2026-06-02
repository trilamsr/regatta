package l4

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/obs"
)

// defaultSeverityBlock matches examples/full/regatta.yaml gates[1]
// and the spec §3.6 R1/R2 baseline: any critical OR >=2 high blocks.
var defaultSeverityBlock = []string{RuleCritical, RuleTwoHigh}

// Invoker is the model call-site seam. The real implementation
// (stream-json adapter + prompt-template render + JSON-output parse)
// lands in a follow-up PR; tests inject a stub for severity-routing
// + verdict-shape coverage.
type Invoker func(ctx context.Context, req InvokeRequest) (InvokeResponse, error)

// InvokeRequest is what the gate hands the model adapter.
type InvokeRequest struct {
	Model    string
	Input    Input
	GateID   string
	MaxChars int
}

// InvokeResponse is what the adapter returns. Findings flow straight
// into the GateResult; PromptSHA pins the prompt template version in
// telemetry for audit replay.
type InvokeResponse struct {
	Findings  []schemas.Finding
	PromptSHA string
	TokensIn  int64
	TokensOut int64
}

// Run executes the L4 adversarial-reviewer gate against the input
// diff + spec + scorecard. The verdict routing follows spec §3.6:
// SeverityBlock mini-DSL ⇒ Blocking + VerdictFail; otherwise pass.
// AdvisoryMode demotes a would-be fail to VerdictAdvisory + !Blocking
// per spec §4 wave-1 rollout.
//
// Nil Invoker returns an error rather than silently passing — gate
// wiring is load-bearing and a missing adapter is a config bug.
func Run(ctx context.Context, cfg Config, in Input) (schemas.GateResult, error) {
	started := time.Now()
	resolvedModel := cfg.Model
	if resolvedModel == "" {
		resolvedModel = ResolveModel("")
	}
	rules := cfg.SeverityBlock
	if len(rules) == 0 {
		rules = defaultSeverityBlock
	}
	maxChars := cfg.MaxDiffChars
	if maxChars == 0 {
		maxChars = DefaultMaxDiffChars
	}

	gr := schemas.GateResult{
		SchemaVersion: 1,
		GateID:        cfg.GateID,
		GateKind:      schemas.GateKindAIAdversarial,
		PRSHA:         in.PRSHA,
		BaseSHA:       in.BaseSHA,
		RunID:         in.RunID,
		Verdict:       schemas.VerdictPass,
		Findings:      []schemas.Finding{},
		Telemetry:     schemas.Telemetry{Model: resolvedModel},
		Heartbeat:     schemas.TelemetryHeartbeat{StartedAt: started},
	}

	if cfg.Invoker == nil {
		err := errors.New("l4 gate: Invoker is nil; wire the model adapter at config load")
		gr.Verdict = schemas.VerdictFail
		gr.Blocking = true
		gr.Findings = append(gr.Findings, schemas.Finding{
			ID:       "L4-CONFIG-NOINVOKER",
			Severity: schemas.FindingHigh,
			Claim:    err.Error(),
		})
		finalize(&gr, started)
		emitVerdict(gr)
		return gr, err
	}

	resp, err := cfg.Invoker(ctx, InvokeRequest{
		Model:    resolvedModel,
		Input:    in,
		GateID:   cfg.GateID,
		MaxChars: maxChars,
	})
	if err != nil {
		gr.Findings = append(gr.Findings, schemas.Finding{
			ID:       "L4-INVOKE-ERR",
			Severity: schemas.FindingMedium,
			Claim:    fmt.Sprintf("model invocation errored: %v", err),
		})
		gr.Verdict = schemas.VerdictAdvisory
		finalize(&gr, started)
		emitVerdict(gr)
		return gr, nil
	}
	gr.Findings = append(gr.Findings, resp.Findings...)
	gr.Telemetry.PromptSHA = resp.PromptSHA
	gr.Telemetry.TokensInput = resp.TokensIn
	gr.Telemetry.TokensOutput = resp.TokensOut

	// Second-opinion loop (issue #353): if the PR body disputes a
	// finding the primary actually returned, re-invoke with the alt
	// model and drop any disputed finding the second opinion did NOT
	// confirm. Second-opinion error fails-closed (keeps primary).
	if disputed := ParseDisputes(in.PRBody); len(disputed) > 0 {
		toReview := intersect(disputed, gr.Findings)
		if len(toReview) > 0 {
			soModel := ResolveSecondOpinionModel(cfg.SecondOpinionModel)
			soResp, soErr := cfg.Invoker(ctx, InvokeRequest{
				Model:    soModel,
				Input:    in,
				GateID:   cfg.GateID,
				MaxChars: maxChars,
			})
			if soErr == nil {
				gr.Findings = mergeDisputed(gr.Findings, toReview, soResp.Findings)
				gr.Telemetry.TokensInput += soResp.TokensIn
				gr.Telemetry.TokensOutput += soResp.TokensOut
			}
		}
	}

	if Blocks(rules, gr.Findings) {
		if cfg.AdvisoryMode {
			gr.Verdict = schemas.VerdictAdvisory
		} else {
			gr.Verdict = schemas.VerdictFail
			gr.Blocking = true
		}
	}

	finalize(&gr, started)
	emitVerdict(gr)
	return gr, nil
}

func finalize(gr *schemas.GateResult, started time.Time) {
	gr.Telemetry.DurationMs = time.Since(started).Milliseconds()
	gr.Heartbeat.FinishedAt = time.Now()
}

// emitVerdict logs one structured gate.verdict event per Run so the
// audit reconciler can detect silent bypass — same shape as the
// security gate's logger seam. Wired-in slog.Logger seam lands in
// the follow-up that wires this gate into the scheduler step 0.7.
func emitVerdict(gr schemas.GateResult) {
	reason := ""
	if len(gr.Findings) > 0 {
		reason = gr.Findings[0].ID
	}
	slog.Default().Info(string(obs.EventGateVerdict),
		string(obs.KeyGateID), gr.GateID,
		string(obs.KeyVerdict), string(gr.Verdict),
		string(obs.KeyReason), reason,
		string(obs.KeyDurationMs), gr.Telemetry.DurationMs,
	)
}
