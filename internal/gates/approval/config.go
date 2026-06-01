package approval

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Config-validation sentinels. Every invariant V1-V11 in spec §5.5
// maps to one entry below. Single block keeps the rubric's "no
// errors.New outside the sentinel block" grep tractable.
var (
	// ErrInvalidDecisionWindow covers V1 — decision_window must be > 0.
	ErrInvalidDecisionWindow = errors.New("approval: decision_window must be > 0")
	// ErrInvalidTimeout covers V2 — timeout must be > 0.
	ErrInvalidTimeout = errors.New("approval: timeout must be > 0")
	// ErrWindowExceedsTimeout covers V3+V4 — per-tier and top-level both.
	ErrWindowExceedsTimeout = errors.New("approval: decision_window must be <= timeout")
	// ErrAutoApproveRequiresLowRisk covers V5 — foot-gun prevention.
	ErrAutoApproveRequiresLowRisk = errors.New("approval: on_timeout=auto_approve requires risk_class=low")
	// ErrInvalidRiskClass covers V6.
	ErrInvalidRiskClass = errors.New("approval: risk_class must be low|medium|high")
	// ErrQuorumExceedsReviewers covers V7 — both quorum<1 and quorum>|set|.
	ErrQuorumExceedsReviewers = errors.New("approval: quorum must be 1..len(reviewers+roles)")
	// ErrInvalidOnTimeout covers V8.
	ErrInvalidOnTimeout = errors.New("approval: on_timeout must be fail|auto_approve|escalate")
	// ErrEscalateMissingChain covers V9.
	ErrEscalateMissingChain = errors.New("approval: on_timeout=escalate requires non-empty escalation_chain")
	// ErrInvalidReviewerID covers V10 — same charset as approval_events.actor.
	ErrInvalidReviewerID = errors.New("approval: reviewer id must match [a-zA-Z0-9_:.-]{1,128}")
	// ErrInvalidGateName covers V11 — URL-safe alphanum + _ -.
	ErrInvalidGateName = errors.New("approval: gate name must match [a-zA-Z0-9_-]{1,64}")
)

// Risk classes per spec §3.3.2 / §5.5 V6.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Timeout policies per spec §3.3 / §5.5 V8.
const (
	OnTimeoutFail        = "fail"
	OnTimeoutAutoApprove = "auto_approve"
	OnTimeoutEscalate    = "escalate"
)

// TierConfig mirrors state.TierConfig but with yaml tags. Defined as a
// type alias so the snapshot persisted via state.Approval round-trips
// byte-identically through the JSON column.
type TierConfig = state.TierConfig

// Config is the parsed gate entry from regatta.yaml. Field-tag yaml
// shape follows spec §5.3.
type Config struct {
	Name              string        `yaml:"name"`
	RiskClass         string        `yaml:"risk_class"`
	Reviewers         []string      `yaml:"reviewers"`
	Roles             []string      `yaml:"roles"`
	Quorum            int           `yaml:"quorum"`
	PreventSelfReview bool          `yaml:"prevent_self_review"`
	Timeout           time.Duration `yaml:"timeout"`
	DecisionWindow    time.Duration `yaml:"decision_window"`
	OnTimeout         string        `yaml:"on_timeout"`
	EscalationChain   []TierConfig  `yaml:"escalation_chain"`
	PredicateCEL      string        `yaml:"predicate_cel"`
}

// Validate runs invariants V1-V11 from spec §5.5 and accumulates every
// failure via errors.Join so a misconfigured regatta.yaml surfaces every
// defect in one CI run rather than fix-one-find-next.
func (c Config) Validate() error {
	var errs []error

	// V11 — Name shape.
	if !gateNameRE.MatchString(c.Name) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidGateName, c.Name))
	}
	// V1 — DecisionWindow > 0.
	if c.DecisionWindow <= 0 {
		errs = append(errs, fmt.Errorf("%w: got %v", ErrInvalidDecisionWindow, c.DecisionWindow))
	}
	// V2 — Timeout > 0.
	if c.Timeout <= 0 {
		errs = append(errs, fmt.Errorf("%w: got %v", ErrInvalidTimeout, c.Timeout))
	}
	// V3 — DecisionWindow <= Timeout. Skip when either is non-positive
	// so the operator sees the V1/V2 sentinel rather than a confusing
	// "window exceeds timeout" downstream of "timeout=0".
	if c.DecisionWindow > 0 && c.Timeout > 0 && c.DecisionWindow > c.Timeout {
		errs = append(errs, fmt.Errorf("%w: window=%v timeout=%v",
			ErrWindowExceedsTimeout, c.DecisionWindow, c.Timeout))
	}
	// V6 — RiskClass enum (allow empty when unset and on_timeout != auto_approve).
	if c.RiskClass != "" && !isValidRiskClass(c.RiskClass) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidRiskClass, c.RiskClass))
	}
	// V8 — OnTimeout enum.
	if !isValidOnTimeout(c.OnTimeout) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidOnTimeout, c.OnTimeout))
	}
	// V5 — auto_approve requires low risk.
	if c.OnTimeout == OnTimeoutAutoApprove && c.RiskClass != RiskLow {
		errs = append(errs, fmt.Errorf("%w: risk_class=%q", ErrAutoApproveRequiresLowRisk, c.RiskClass))
	}
	// V9 — escalate requires non-empty chain.
	if c.OnTimeout == OnTimeoutEscalate && len(c.EscalationChain) == 0 {
		errs = append(errs, ErrEscalateMissingChain)
	}
	// V7 — quorum bounds. Roles count toward the union per V7 rationale;
	// in MVP the loader is responsible for role expansion before reaching
	// Validate, so an unresolved role still counts as one slot.
	setSize := len(c.Reviewers) + len(c.Roles)
	if c.Quorum < 1 || c.Quorum > setSize {
		errs = append(errs, fmt.Errorf("%w: quorum=%d set_size=%d",
			ErrQuorumExceedsReviewers, c.Quorum, setSize))
	}
	// V10 — each reviewer id.
	for _, r := range c.Reviewers {
		if !reviewerIDRE.MatchString(r) {
			errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidReviewerID, r))
		}
	}
	// V4 — chain-tier window<=timeout (recurse).
	for i, tier := range c.EscalationChain {
		if tier.DecisionWindow > 0 && tier.Timeout > 0 && tier.DecisionWindow > tier.Timeout {
			errs = append(errs, fmt.Errorf("%w: tier[%d] window=%v timeout=%v",
				ErrWindowExceedsTimeout, i, tier.DecisionWindow, tier.Timeout))
		}
	}
	return errors.Join(errs...)
}

func isValidRiskClass(s string) bool {
	switch s {
	case RiskLow, RiskMedium, RiskHigh:
		return true
	}
	return false
}

func isValidOnTimeout(s string) bool {
	switch s {
	case OnTimeoutFail, OnTimeoutAutoApprove, OnTimeoutEscalate:
		return true
	}
	return false
}

// V10 charset mirrors the approval_events.actor CHECK constraint
// (spec §5.1) so a config-time reject prevents a later DB-level
// CHECK violation that operators can't easily attribute.
var reviewerIDRE = regexp.MustCompile(`^[a-zA-Z0-9_:.-]{1,128}$`)

// V11 — Names are surfaced in URLs and slog records; restrict to
// alnum + _-, 1..64 chars.
var gateNameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
