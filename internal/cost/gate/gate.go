// Package gate is the cost-governor pre-call deny primitive (spec
// §3.2). Concrete type, no interface (S5 substrate-spec parity).
// Consumed by the scheduler at step 0.6 and by the spawner
// SupervisorLimits (Wave 2). One Reader services both call sites
// against the same substrate event log.
package gate

import (
	"context"
	"fmt"
	"log/slog"
	"math"
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

// Config holds the Gate's wiring. Safety==nil → Evaluate returns
// Allow=true without reading substrate (closes I6 zero-overhead).
// Tracer nil falls back to otel.Tracer("internal/cost/gate") — noop
// until obs/otel.Setup runs (uniform injection per spec §7 A8).
type Config struct {
	Safety *SafetyCost
	Tracer trace.Tracer
	Logger *slog.Logger
}

// Estimator decouples gate from internal/cost/estimate.
// *estimate.UpperBound satisfies without an adapter type.
type Estimator interface {
	Estimate(ctx context.Context, hint EstHint, model string) (float64, error)
}

// Pricing decouples gate from internal/cost/pricing — gate only needs
// the downgrade-target lookup at soft-cap time.
type Pricing interface {
	DowngradeFor(model string) string // "" when no downgrade applicable
}

// Gate — cost-governor pre-call deny primitive (spec §3.2 lines 138-145).
type Gate struct {
	cfg     Config
	pricing Pricing
	spend   *spend.Reader
	estim   Estimator
	tracer  trace.Tracer
	log     *slog.Logger
}

// New constructs a Gate; cfg.Tracer / cfg.Logger nil-defaults apply
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

// Evaluate decides whether one work_item may spawn. Allow=false when
// any active cap (dag, operator, work_item) would be breached by
// recorded + estimated. Idempotent + side-effect-free except the
// cost.evaluate span emit.
//
// Precedence (spec §3.6 + R-A2): every configured cap is checked
// independently; ANY breach denies. Most-restrictive-wins.
//
// Soft-cap (spec R10): SoftPct crossed sets SoftCapBreached=true.
// DowngradeTo populated only when scope.AllowDowngrade=true AND
// Pricing surfaces a downgrade target. WARN-only default posture.
func (g *Gate) Evaluate(ctx context.Context, w WorkItemScope) (Verdict, error) {
	if g.cfg.Safety == nil {
		// I6 zero-overhead short-circuit — skips estimator + substrate
		// read + span emission.
		return Verdict{Allow: true}, nil
	}

	// Estimate once; reused across every cap. OperatorID through hint
	// lets the History estimator (opt-in, spec §10 S1) scope cohort
	// p95 to (tenant, operator, model). Default upper_bound ignores it.
	hint := w.EstHint
	if hint.OperatorID == "" {
		hint.OperatorID = w.OperatorID
	}
	est, err := g.estim.Estimate(ctx, hint, w.Model)
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

	// Most-restrictive-wins: first cap to fire wins the reason. Cap
	// path runs in integer micro-USD (#554): float SUM in SQLite
	// accumulates ULP drift across N rows and lets cap-boundary
	// spawns slip past — integer SUM is exact. Estimator + cap input
	// arrive in float USD and convert once here; downstream
	// projected/cap compare is pure int64.
	estMicro := spend.FromUSD(est)
	type capCheck struct {
		usdMicro spend.USDMicro
		usd      float64 // mirrored onto the span; operator display only
		kind     spend.ScopeKind
		scopeID  string
		label    string // e.g. "dag", "operator"
	}
	checks := []capCheck{}
	if g.cfg.Safety.PerDAGUSD > 0 {
		checks = append(checks, capCheck{spend.FromUSD(g.cfg.Safety.PerDAGUSD), g.cfg.Safety.PerDAGUSD, spend.ScopeDAG, w.DAGID, "dag"})
	}
	if g.cfg.Safety.PerOperatorUSD > 0 {
		checks = append(checks, capCheck{spend.FromUSD(g.cfg.Safety.PerOperatorUSD), g.cfg.Safety.PerOperatorUSD, spend.ScopeOperator, w.OperatorID, "operator"})
	}
	if g.cfg.Safety.PerWorkItemUSD > 0 {
		checks = append(checks, capCheck{spend.FromUSD(g.cfg.Safety.PerWorkItemUSD), g.cfg.Safety.PerWorkItemUSD, spend.ScopeWorkItem, w.WorkItemID, "work_item"})
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
		recordedMicro, err := g.spend.BudgetState(ctx, key, period)
		if err != nil {
			return Verdict{}, fmt.Errorf("cost.gate: read %s spend: %w", c.label, err)
		}
		projectedMicro := recordedMicro + estMicro
		if projectedMicro > c.usdMicro {
			v.Allow = false
			v.Reason = fmt.Sprintf("cap_exceeded:%s:%s", c.label, c.scopeID)
			break
		}
		// Soft-cap threshold: cap_micro * softPct / 100, computed in
		// int64 with explicit rounding so the ratio is exact at the
		// percent boundary (no float * int floor surprise).
		if softPct > 0 {
			softThreshold := spend.USDMicro(int64(math.Round(float64(c.usdMicro) * float64(softPct) / 100.0)))
			if projectedMicro >= softThreshold {
				v.SoftCapBreached = true
			}
		}
	}

	if v.SoftCapBreached && v.Allow && w.AllowDowngrade {
		v.DowngradeTo = g.pricing.DowngradeFor(w.Model)
	}

	g.emitSpan(ctx, w, v)
	return v, nil
}

// emitSpan writes the cost.evaluate span (spec §3.7). One per
// Evaluate; verbatim attr set; no extra cardinality (closes R14).
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

// dagCap returns the per-DAG cap or 0 — the spec §3.7 row 2 sentinel
// for "no cap configured" on the span attribute.
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

// period defaults to 1h when unset so the Reader's substrate scan
// stays bounded.
func (s *SafetyCost) period() time.Duration {
	if s == nil || s.Period <= 0 {
		return time.Hour
	}
	return s.Period
}
