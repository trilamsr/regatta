package l4

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/gates/severity"
	"github.com/trilamsr/regatta/internal/obs"
)

// defaultSeverityBlock matches examples/full/regatta.yaml gates[1]
// and the spec §3.6 R1/R2 baseline: any critical OR >=2 high blocks.
var defaultSeverityBlock = []string{severity.Critical, severity.TwoHigh}

// Invoker is the model call-site seam. The real implementation
// (stream-json adapter + prompt-template render + JSON-output parse)
// lands in a follow-up PR; tests inject a stub for severity-routing
// + verdict-shape coverage.
type Invoker func(ctx context.Context, req InvokeRequest) (InvokeResponse, error)

// InvokeRequest is what the gate hands the model adapter.
//
// Categories lists the hunt-list categories the prompt should scope
// for this call. Empty means all categories (single-bucket / no
// CategoryModels override). Adapters render this into the prompt's
// hunt-list section so the bucket model is only asked about its
// assigned categories.
type InvokeRequest struct {
	Model      string
	Input      Input
	GateID     string
	MaxChars   int
	Categories []string
}

// InvokeResponse is what the adapter returns. Findings flow straight
// into the GateResult; PromptSHA pins the prompt template version in
// telemetry for audit replay.
type InvokeResponse struct {
	Findings         []schemas.Finding
	PromptSHA        string
	TokensIn         int64
	TokensOut        int64
	TokensCacheRead  int64
	TokensCacheWrite int64
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
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	started := clock()
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

	inst := newInstruments(cfg.ResolveMeter())

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
		finalize(&gr, started, clock)
		emitVerdict(gr)
		// Skip label per brief: nil-Invoker is a config-bug short-circuit;
		// dashboards should distinguish it from a model-decided deny.
		emitInvocations(ctx, inst, verdictLabelSkip, AllCategories)
		inst.recordLatency(ctx, float64(gr.Telemetry.DurationMs))
		return gr, err
	}

	buckets := categoryBuckets(resolvedModel, cfg.CategoryModels)
	for _, bucketModel := range bucketModelsSorted(buckets, resolvedModel) {
		req := InvokeRequest{
			Model:    bucketModel,
			Input:    in,
			GateID:   cfg.GateID,
			MaxChars: maxChars,
		}
		// Carry per-bucket categories only when there is more than
		// one bucket; a single bucket means "all categories" and the
		// adapter renders the full hunt list unchanged.
		if len(buckets) > 1 {
			req.Categories = buckets[bucketModel]
		}
		resp, err := cfg.Invoker(ctx, req)
		if err != nil {
			// Bucket-level error degrades the whole gate run to
			// advisory and short-circuits to avoid partial-result
			// blocking — a single category model outage cannot
			// silently flip a clean pass to a fail, nor hide an
			// earlier bucket's critical behind an advisory mask.
			// We keep findings from successful buckets so audit
			// replay sees what the gate already learned.
			gr.Findings = append(gr.Findings, schemas.Finding{
				ID:       "L4-INVOKE-ERR",
				Severity: schemas.FindingMedium,
				Claim:    fmt.Sprintf("model invocation errored (model=%s): %v", bucketModel, err),
			})
			gr.Verdict = schemas.VerdictAdvisory
			finalize(&gr, started, clock)
			emitVerdict(gr)
			emitInvocations(ctx, inst, verdictLabelNeedsReview, AllCategories)
			inst.recordLatency(ctx, float64(gr.Telemetry.DurationMs))
			return gr, nil
		}
		gr.Findings = append(gr.Findings, resp.Findings...)
		// Telemetry pins primary-bucket prompt SHA + accumulates
		// tokens across buckets so cost reporting is the union.
		if bucketModel == resolvedModel {
			gr.Telemetry.PromptSHA = resp.PromptSHA
		}
		gr.Telemetry.TokensInput += resp.TokensIn
		gr.Telemetry.TokensOutput += resp.TokensOut
	}
	if !cfg.AutoFix {
		// Operator opt-in is load-bearing per issue #358 — a
		// rogue model that surfaces a patch without the gate
		// being configured for AutoFix must not leak the diff
		// into the audit payload.
		for i := range gr.Findings {
			gr.Findings[i].AutoFixable = false
			gr.Findings[i].Patch = ""
		}
	}

	// Second-opinion loop (issue #353): if the PR body disputes a
	// finding the primary actually returned, re-invoke with the alt
	// model and drop any disputed finding the second opinion did NOT
	// confirm. Second-opinion error fails-closed (keeps primary).
	soFlipped := false
	if disputed := ParseDisputes(in.PRBody); len(disputed) > 0 {
		toReview := intersect(disputed, gr.Findings)
		if len(toReview) > 0 {
			soModel := ResolveSecondOpinionModel(cfg.SecondOpinionModel)
			inst.recordSecondOpinion(ctx)
			before := len(gr.Findings)
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
				// Fire-and-flip: SO actually dropped a primary finding.
				// Anything less (SO confirmed all disputed) is not an
				// escalate per brief.
				soFlipped = len(gr.Findings) < before
			}
		}
	}

	if severity.Blocks(rules, gr.Findings) {
		if cfg.AdvisoryMode {
			gr.Verdict = schemas.VerdictAdvisory
		} else {
			gr.Verdict = schemas.VerdictFail
			gr.Blocking = true
		}
	}

	finalize(&gr, started, clock)
	emitVerdict(gr)
	emitInvocations(ctx, inst, metricVerdict(gr.Verdict, soFlipped), AllCategories)
	inst.recordLatency(ctx, float64(gr.Telemetry.DurationMs))
	return gr, nil
}

// metricVerdict maps the gate verdict to the regatta.l4.invocations
// label set. Second-opinion fire-and-flip overrides the schema verdict
// with "escalate" so dashboards distinguish a dispute-driven downgrade
// from an unflipped pass.
func metricVerdict(v schemas.Verdict, soFlipped bool) string {
	if soFlipped {
		return verdictLabelEscalate
	}
	switch v {
	case schemas.VerdictPass:
		return verdictLabelAllow
	case schemas.VerdictFail:
		return verdictLabelDeny
	case schemas.VerdictAdvisory:
		return verdictLabelNeedsReview
	default:
		return verdictLabelSkip
	}
}

func finalize(gr *schemas.GateResult, started time.Time, clock func() time.Time) {
	now := clock()
	gr.Telemetry.DurationMs = now.Sub(started).Milliseconds()
	gr.Heartbeat.FinishedAt = now
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
