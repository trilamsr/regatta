// Package gate is the cost-governor pre-call deny primitive (spec
// §3.2). Concrete type, no interface (S5 — substrate spec parity);
// the second implementation forces the interface extraction.
//
// The Gate is consumed by the scheduler at step 0.6 (per-tick) and
// will be consumed by the spawner SupervisorLimits (per running-agent
// post-stream tick — Wave 2). One reader (internal/cost/spend.Reader)
// services both call sites against the same substrate event log.
package gate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/cost/spend"
)

// SafetyCost mirrors regatta.yaml safety.cost. Optional fields map to
// zero values; only non-zero caps are checked at Evaluate time. Period
// drives the substrate spend window.
type SafetyCost struct {
	PerDAGUSD       float64
	PerOperatorUSD  float64
	PerWorkItemUSD  float64
	Period          time.Duration
	SoftPct         int // 0 means soft-cap disabled
}

// Config holds the Gate's wiring. Safety==nil means "no cost gate
// configured" — Evaluate returns Allow=true without reading substrate.
// Closes I6 (zero overhead when unset).
//
// Tracer follows the W6 normalization pattern: nil falls back to
// otel.Tracer("internal/cost/gate") which resolves to the global
// provider — noop until obs/otel.Setup runs. No WithTracer setter;
// uniform injection per spec §7 A8.
type Config struct {
	Safety *SafetyCost
	Tracer trace.Tracer
	Logger *slog.Logger
}

// Estimator decouples the gate from internal/cost/estimate (T2 scope).
// One method; T2's *estimate.UpperBound satisfies it without an extra
// adapter type.
type Estimator interface {
	Estimate(ctx context.Context, hint EstHint, model string) (float64, error)
}

// Pricing decouples the gate from internal/cost/pricing (T2 scope).
// The gate only needs the downgrade-target lookup at soft-cap time;
// the full Lookup / Row API stays in T2. T2's pricing.Lookup is wrapped
// to satisfy this interface at call-site wiring.
type Pricing interface {
	DowngradeFor(model string) string // returns "" when no downgrade applicable
}

// Gate is the cost-governor pre-call deny primitive. Spec §3.2 lines
// 138-145 verbatim.
type Gate struct {
	cfg     Config
	pricing Pricing
	spend   *spend.Reader
	estim   Estimator
	tracer  trace.Tracer
	log     *slog.Logger
}

// New constructs a Gate. cfg.Tracer / cfg.Logger nil-defaults run
// here so callers never check before use.
func New(cfg Config, pricing Pricing, reader *spend.Reader, estim Estimator) *Gate {
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("internal/cost/gate")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Gate{cfg: cfg, pricing: pricing, spend: reader, estim: estim, tracer: tracer, log: log}
}

// Evaluate decides whether one work_item may spawn. Returns Allow=false
// when any active cap (dag, operator, work_item, global) would be
// breached by recorded + estimated. Idempotent + side-effect-free
// EXCEPT for the cost.evaluate span emitted via cfg.Tracer.
//
// Precedence (spec §3.6 + R-A2): every configured cap is checked
// independently; ANY breach denies. Most-restrictive-wins, no silent
// inheritance.
//
// Soft-cap policy (spec R10): SoftPct crossed at any scope sets
// SoftCapBreached=true. DowngradeTo is populated only when
// scope.AllowDowngrade=true AND Pricing surfaces a downgrade target.
// Default WARN-only posture.
func (g *Gate) Evaluate(ctx context.Context, w WorkItemScope) (Verdict, error) {
	// Zero-overhead short-circuit when no cost config is wired (I6).
	// Skips estimator + substrate read + span emission.
	if g.cfg.Safety == nil {
		return Verdict{Allow: true}, nil
	}

	// Estimate once; reused across every cap check.
	est, err := g.estim.Estimate(ctx, w.EstHint, w.Model)
	if err != nil {
		return Verdict{}, fmt.Errorf("cost.gate: estimate: %w", err)
	}

	v := Verdict{
		Allow:          true,
		USDEstimate:    est,
		CapDAGUSD:      g.cfg.Safety.dagCap(),
		CapOperatorUSD: g.cfg.Safety.operatorCap(),
	}

	period := g.cfg.Safety.period()
	softPct := g.cfg.Safety.SoftPct

	// Most-restrictive-wins: first cap to fire wins the reason.
	type capCheck struct {
		usd      float64
		kind     spend.ScopeKind
		scopeID  string
		label    string // e.g. "dag", "operator"
	}
	checks := []capCheck{}
	if g.cfg.Safety.PerDAGUSD > 0 {
		checks = append(checks, capCheck{g.cfg.Safety.PerDAGUSD, spend.ScopeDAG, w.DAGID, "dag"})
	}
	if g.cfg.Safety.PerOperatorUSD > 0 {
		checks = append(checks, capCheck{g.cfg.Safety.PerOperatorUSD, spend.ScopeOperator, w.OperatorID, "operator"})
	}
	if g.cfg.Safety.PerWorkItemUSD > 0 {
		checks = append(checks, capCheck{g.cfg.Safety.PerWorkItemUSD, spend.ScopeWorkItem, w.WorkItemID, "work_item"})
	}

	tenantID := w.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	for _, c := range checks {
		key := spend.ScopeKey{Kind: c.kind, TenantID: tenantID}
		switch c.kind {
		case spend.ScopeDAG:
			key.DAGID = c.scopeID
		case spend.ScopeOperator:
			key.OperatorID = c.scopeID
		case spend.ScopeWorkItem:
			key.WorkItemID = c.scopeID
		}
		recorded, err := g.spend.BudgetState(ctx, key, period)
		if err != nil {
			return Verdict{}, fmt.Errorf("cost.gate: read %s spend: %w", c.label, err)
		}
		projected := recorded + est
		if projected > c.usd {
			v.Allow = false
			v.Reason = fmt.Sprintf("cap_exceeded:%s:%s", c.label, c.scopeID)
			break
		}
		if softPct > 0 && projected >= c.usd*float64(softPct)/100.0 {
			v.SoftCapBreached = true
		}
	}

	if v.SoftCapBreached && v.Allow && w.AllowDowngrade {
		v.DowngradeTo = g.pricing.DowngradeFor(w.Model)
	}

	g.emitSpan(ctx, w, v)
	return v, nil
}

// emitSpan writes the cost.evaluate span — spec §3.7. One span per
// Evaluate call. Attribute set is the spec's verbatim list; no extra
// cardinality (closes R14).
func (g *Gate) emitSpan(ctx context.Context, w WorkItemScope, v Verdict) {
	_, span := g.tracer.Start(ctx, "cost.evaluate",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.Float64("regatta.cost.usd_estimate", v.USDEstimate),
			attribute.Float64("regatta.cost.cap_dag_usd", v.CapDAGUSD),
			attribute.Float64("regatta.cost.cap_op_usd", v.CapOperatorUSD),
			attribute.Bool("regatta.cost.allow", v.Allow),
			attribute.Bool("regatta.cost.soft_breached", v.SoftCapBreached),
			attribute.String("regatta.work_item_id", w.WorkItemID),
			attribute.String("regatta.dag_id", w.DAGID),
			attribute.String("regatta.operator_id", w.OperatorID),
		),
	)
	span.End()
}

// dagCap returns the per-DAG cap or 0 when Safety is nil (sentinel for
// "no cap configured" on the span attribute, per spec §3.7 row 2).
func (s *SafetyCost) dagCap() float64 {
	if s == nil {
		return 0
	}
	return s.PerDAGUSD
}

func (s *SafetyCost) operatorCap() float64 {
	if s == nil {
		return 0
	}
	return s.PerOperatorUSD
}

// period returns the configured spend window, defaulting to 1h when
// unset so the Reader's substrate scan stays bounded.
func (s *SafetyCost) period() time.Duration {
	if s == nil || s.Period <= 0 {
		return time.Hour
	}
	return s.Period
}
