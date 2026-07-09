// Package cap is the global daily cost-cap autonomic enforcer (spec
// PHASE-AUTONOMY W5). Sits ABOVE the per-scope cost gate: the scheduler
// pauses ALL new spawns when sum(spend_24h) >= cap. Auto-resumes at
// TZ-aware day rollover; operator override via `regatta resume`.
//
// State is DERIVED — no durable scheduler_state row. The transition is
// recomputed each Tick from (sum_spend_24h, cap, latest_resume_event,
// now). Single source of truth = event log (spec §3).
package costcap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/obs"
)

// SchedulerState is the binary state machine the scheduler observes.
// Active = spawn freely (per-scope caps still apply). Throttled = block
// every new spawn; in-flight agents continue (spec §13 R2).
type SchedulerState string

const (
	// Active means new spawns are permitted.
	Active SchedulerState = "active"
	// Throttled means new spawns are blocked until rollover or override.
	Throttled SchedulerState = "throttled"
)

// EventKindThrottled / EventKindResumed are the audit kinds the
// Enforcer writes through Recorder. Stored on the `events` table.
// Substrate kinds (KindCostCapThrottled / KindCostCapResumed) carry the
// schema-parity surface for the forward-port (issue #622); producer
// rewiring is deferred behind the cross-link.
const (
	EventKindThrottled = "cost_cap_throttled"
	EventKindResumed   = "cost_cap_resumed"
)

// ReasonSpendReadError is the State() reason returned when the spend
// reader fails. Fail-CLOSED: a substrate hiccup must NOT silently lift
// the cap. Operator sees Throttled until the reader recovers (#650).
const ReasonSpendReadError = "spend read error: failing closed (throttled)"

// SpendReader is the spend.Reader-side seam — the only call cap makes
// is the global 24h sum.
type SpendReader interface {
	BudgetState(ctx context.Context, scope spend.ScopeKey, period time.Duration) (spend.USDMicro, error)
}

// Recorder writes audit events for state transitions and operator
// overrides. *state.DB satisfies via RecordEvent (agentID=0 since
// these events are run-level, not agent-level).
type Recorder interface {
	RecordEvent(ctx context.Context, agentID int64, kind, payloadJSON string) error
}

// ResumeReader looks up the most recent operator-override event so
// the Enforcer can compare it to today's day_anchor; returns zero time
// when no override has been written.
type ResumeReader func(ctx context.Context) (time.Time, error)

// Config wires the Enforcer. CapMicro=0 disables the global ceiling
// entirely (no overhead — Allow returns false). TZ defaults to UTC;
// the day_anchor function uses it to compute wall-clock midnight.
// MemoizeTTL bounds spend SUM reads — production default 60s.
type Config struct {
	// CapMicro is the global daily ceiling in micro-USD. Zero disables.
	CapMicro spend.USDMicro
	// TenantID scopes every spend SUM (R9; W8 forward-fits per-tenant).
	TenantID string
	// TZ is the IANA location for day-rollover anchoring. nil == UTC.
	TZ *time.Location
	// MemoizeTTL caps the rate at which the spend SUM hits the DB on
	// the scheduler hot path (spec §5). Zero disables memoization.
	MemoizeTTL time.Duration
	// Spend reads the rolling-24h global sum.
	Spend SpendReader
	// Recorder writes audit events on transitions and operator override.
	Recorder Recorder
	// Resume reads the most-recent operator override event.
	Resume ResumeReader
	// Clock injects wall-time for deterministic tests. nil → time.Now.
	Clock func() time.Time
	// Logger is the structured-event sink; nil → slog.Default.
	Logger *slog.Logger
	// Meter is the OTel instrument factory; nil → noop until Setup.
	Meter metric.Meter
}

// Enforcer is the cap-state derivation engine. One per scheduler
// instance; Allow is hot-path-safe under concurrent Tick callers
// (memoize is mutex-guarded).
type Enforcer struct {
	cfg     Config
	clock   func() time.Time
	log     *slog.Logger
	tenant  string
	tz      *time.Location
	period  time.Duration
	throttledCounter metric.Int64Counter
	resumedCounter   metric.Int64Counter
	stateGauge       metric.Int64Gauge
	spendGauge       metric.Float64Gauge

	mu        sync.Mutex
	memoAt    time.Time
	memoState SchedulerState
	lastState SchedulerState // tracks last-emitted state for once-per-transition log/event
}

// New constructs an Enforcer from cfg. Returns an error when Spend is
// nil (the reader is load-bearing) or TenantID is empty.
func New(cfg Config) (*Enforcer, error) {
	if cfg.Spend == nil {
		return nil, errors.New("cap: Spend reader required")
	}
	if cfg.TenantID == "" {
		return nil, errors.New("cap: TenantID required")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	tz := cfg.TZ
	if tz == nil {
		tz = time.UTC
	}
	meter := obs.ResolveMeter(cfg.Meter, obs.MeterScopeCostCap)
	// Pre-create instruments so Allow() stays alloc-free on the hot
	// path. Construction failures degrade to noop counters so a broken
	// SDK never blocks scheduling.
	throttled, _ := meter.Int64Counter("regatta_cost_cap_throttled_total")
	resumed, _ := meter.Int64Counter("regatta_cost_cap_resumed_total")
	stateGauge, _ := meter.Int64Gauge("regatta_cost_cap_state")
	spendGauge, _ := meter.Float64Gauge("regatta_cost_cap_24h_spend_usd")

	e := &Enforcer{
		cfg:              cfg,
		clock:            clock,
		log:              log,
		tenant:           cfg.TenantID,
		tz:               tz,
		period:           24 * time.Hour,
		throttledCounter: throttled,
		resumedCounter:   resumed,
		stateGauge:       stateGauge,
		spendGauge:       spendGauge,
		lastState:        Active,
	}
	if cfg.CapMicro == 0 {
		log.Warn("cap.disabled",
			"reason", "cost cap disabled; set cost.cap_micro > 0 to enforce",
			"tenant_id", cfg.TenantID,
		)
	}
	return e, nil
}

// dayAnchor returns wall-clock midnight in tz that is <= now. time.Date
// normalizes invalid wall-clock times (e.g. 02:00 during DST
// spring-forward), so the earliest valid midnight is always returned —
// operators never see a 25-hour throttle.
func dayAnchor(now time.Time, tz *time.Location) time.Time {
	t := now.In(tz)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, tz)
}

// Allow returns true when new spawns are permitted. False ⇒ scheduler
// MUST skip every spawn this tick. Memoized by MemoizeTTL so a
// sub-second tick rate does not amplify the SUM query load.
//
// Disabled (CapMicro=0) returns true unconditionally — closes the
// zero-overhead opt-out contract.
func (e *Enforcer) Allow(ctx context.Context) bool {
	if e.cfg.CapMicro == 0 {
		return true
	}
	st, _ := e.evaluate(ctx)
	return st == Active
}

// State returns the current SchedulerState along with a human-readable
// reason. Used by `regatta cost status`. Bypasses memoize.
func (e *Enforcer) State(ctx context.Context) (SchedulerState, string, error) {
	if e.cfg.CapMicro == 0 {
		return Active, "cap unset", nil
	}
	e.mu.Lock()
	e.memoAt = time.Time{} // force fresh read
	e.mu.Unlock()
	st, reason := e.evaluate(ctx)
	return st, reason, nil
}

// evaluate is the shared derivation path. Holds the mutex across the
// memo check + the (potentially) DB-bound SUM so two concurrent Allows
// never both fire the read.
func (e *Enforcer) evaluate(ctx context.Context) (SchedulerState, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.clock()
	if !e.memoAt.IsZero() && e.cfg.MemoizeTTL > 0 && now.Sub(e.memoAt) < e.cfg.MemoizeTTL {
		return e.memoState, "memoized"
	}
	spendMicro, err := e.cfg.Spend.BudgetState(ctx, spend.ScopeKey{
		Kind:     spend.ScopeGlobal,
		TenantID: e.tenant,
	}, e.period)
	if err != nil {
		// Fail-CLOSED on read errors (#650): silently lifting the cap
		// when substrate is unavailable risks unbounded spend during the
		// degradation window. Throttle until the reader recovers; the
		// per-scope gate continues to enforce local caps in parallel.
		// Logged at ERROR so operator paging fires immediately.
		e.log.Error("cost_cap.spend_read_error",
			"err", err.Error(),
		)
		return Throttled, ReasonSpendReadError
	}
	// Boundary policy (#651): allow spend strictly EQUAL to cap; throttle
	// only when spend exceeds. Operators set a cap as the budgetary line;
	// the line itself is permitted. Tests:
	// TestEnforcer_AtCapBoundary_Allows + TestEnforcer_AboveCap_Throttles.
	overCap := spendMicro > e.cfg.CapMicro
	overridden := false
	if overCap && e.cfg.Resume != nil {
		latestResume, rerr := e.cfg.Resume(ctx)
		if rerr != nil {
			e.log.Warn("cost_cap.resume_read_error", "err", rerr.Error())
		} else if !latestResume.IsZero() && !latestResume.Before(dayAnchor(now, e.tz)) {
			overridden = true
		}
	}
	state := Active
	var reason string
	switch {
	case overCap && !overridden:
		state = Throttled
		resumeAt := dayAnchor(now, e.tz).Add(24 * time.Hour)
		reason = fmt.Sprintf("24h spend $%.2f >= cap $%.2f (auto-resume at %s)",
			spendMicro.USD(), e.cfg.CapMicro.USD(), resumeAt.Format(time.RFC3339))
	case overridden:
		reason = "operator override active until next rollover"
	default:
		reason = fmt.Sprintf("24h spend $%.2f < cap $%.2f", spendMicro.USD(), e.cfg.CapMicro.USD())
	}
	// Record state gauge + spend gauge on every fresh evaluation.
	if e.stateGauge != nil {
		v := int64(0)
		if state == Throttled {
			v = 1
		}
		e.stateGauge.Record(ctx, v, metric.WithAttributes(attribute.String("tenant_id", e.tenant)))
	}
	if e.spendGauge != nil {
		e.spendGauge.Record(ctx, spendMicro.USD(), metric.WithAttributes(attribute.String("tenant_id", e.tenant)))
	}
	// Once-per-transition: log + audit-event + counter.
	if state != e.lastState {
		e.onTransition(ctx, state, spendMicro, now)
		e.lastState = state
	}
	if e.cfg.MemoizeTTL > 0 {
		e.memoAt = now
		e.memoState = state
	}
	return state, reason
}

// onTransition emits the side effects of a state change: INFO log,
// counter increment, and audit event. Called under e.mu — must not
// re-enter evaluate.
func (e *Enforcer) onTransition(ctx context.Context, next SchedulerState, spendMicro spend.USDMicro, now time.Time) {
	resumeAt := dayAnchor(now, e.tz).Add(24 * time.Hour)
	switch next {
	case Throttled:
		e.log.Info("scheduler.throttled",
			"24h_spend_usd", spendMicro.USD(),
			"cap_usd", e.cfg.CapMicro.USD(),
			"auto_resume_at", resumeAt.Format(time.RFC3339),
			"tz", e.tz.String(),
		)
		if e.throttledCounter != nil {
			e.throttledCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("action", "enter")))
		}
		if e.cfg.Recorder != nil {
			payload := fmt.Sprintf(
				`{"spend_micro":%d,"cap_micro":%d,"auto_resume_at":%q,"tz":%q}`,
				spendMicro, e.cfg.CapMicro, resumeAt.Format(time.RFC3339), e.tz.String(),
			)
			// UNIQUE-constraint violation when two schedulers race the
			// same Active→Throttled transition (#652). Treat as benign —
			// the partial UNIQUE index on (agent_id, kind, day-bucket)
			// pins one canonical row per day; the loser logs + moves on.
			if err := e.cfg.Recorder.RecordEvent(ctx, 0, EventKindThrottled, payload); err != nil {
				if isUniqueViolation(err) {
					e.log.Info("cost_cap.duplicate_throttled_event_suppressed",
						"err", err.Error(),
					)
				} else {
					e.log.Warn("cost_cap.record_throttled_failed",
						"err", err.Error(),
					)
				}
			}
		}
	case Active:
		e.log.Info("scheduler.resumed",
			"24h_spend_usd", spendMicro.USD(),
			"cap_usd", e.cfg.CapMicro.USD(),
		)
		if e.resumedCounter != nil {
			e.resumedCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("action", "exit_rollover")))
		}
	}
}

// Resume writes the operator-override audit event and invalidates the
// memoize cache so the next Allow recomputes. Idempotent — multiple
// resumes within the same day add audit rows but do not change state.
func (e *Enforcer) Resume(ctx context.Context, actor string) error {
	if e.cfg.Recorder == nil {
		return errors.New("cap.Resume: Recorder required")
	}
	now := e.clock()
	resumeAt := dayAnchor(now, e.tz).Add(24 * time.Hour)
	payload := fmt.Sprintf(
		`{"actor":%q,"reason":%q,"until":%q}`,
		actor, "operator", resumeAt.Format(time.RFC3339),
	)
	if err := e.cfg.Recorder.RecordEvent(ctx, 0, EventKindResumed, payload); err != nil {
		return fmt.Errorf("cap.Resume: record event: %w", err)
	}
	if e.resumedCounter != nil {
		e.resumedCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("action", "exit_operator")))
	}
	e.log.Info("scheduler.operator_resume",
		"actor", actor,
		"until", resumeAt.Format(time.RFC3339),
	)
	// Drop memo so the next evaluate picks up the new resume event.
	e.mu.Lock()
	e.memoAt = time.Time{}
	e.mu.Unlock()
	return nil
}

// CapMicro returns the configured ceiling in micro-USD. Status CLI
// uses this without re-reading config.
func (e *Enforcer) CapMicro() spend.USDMicro { return e.cfg.CapMicro }

// Snapshot returns the current spend + cap + state in a single read.
// Bypasses memoize so `regatta cost status` always reports fresh
// numbers. Returns (spend, cap, state, resumeAt).
func (e *Enforcer) Snapshot(ctx context.Context) (spend.USDMicro, spend.USDMicro, SchedulerState, time.Time, error) {
	now := e.clock()
	spendMicro, err := e.cfg.Spend.BudgetState(ctx, spend.ScopeKey{
		Kind:     spend.ScopeGlobal,
		TenantID: e.tenant,
	}, e.period)
	if err != nil {
		return 0, e.cfg.CapMicro, Active, time.Time{}, err
	}
	state := Active
	if e.cfg.CapMicro > 0 && spendMicro > e.cfg.CapMicro {
		state = Throttled
		if e.cfg.Resume != nil {
			latest, _ := e.cfg.Resume(ctx)
			if !latest.IsZero() && !latest.Before(dayAnchor(now, e.tz)) {
				state = Active
			}
		}
	}
	resumeAt := dayAnchor(now, e.tz).Add(24 * time.Hour)
	return spendMicro, e.cfg.CapMicro, state, resumeAt, nil
}

// TZ returns the configured timezone — status CLI uses it for the
// `tz=` output field.
func (e *Enforcer) TZ() *time.Location { return e.tz }

// isUniqueViolation matches the modernc.org/sqlite UNIQUE-constraint
// error shape against the cost-cap throttled audit-row guard (#652).
// Substring match couples to the driver's stable error text — same
// pattern substrate.isUniqueNonceViolation uses.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "idx_cost_cap_throttled_event_unique")
}
