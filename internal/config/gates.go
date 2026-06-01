// Package config provides typed loaders for the per-repo `regatta.yaml`.
// Today it surfaces only the `approval_gate` entries from the
// discriminated-union `gates[]` block. Other gate types still live in
// the CUE-only validator at internal/config/validate; the typed extractor
// lands here as each consumer grows a Go-side need.
package config

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/yaml"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Risk classes (spec §5.5 V6). Duplicated from
// internal/gates/approval/config.go (PR #144); CONSOLIDATION note at
// end of file tracks the swap to a type alias once #144 merges.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Timeout policies (spec §5.5 V8).
const (
	OnTimeoutFail        = "fail"
	OnTimeoutAutoApprove = "auto_approve"
	OnTimeoutEscalate    = "escalate"
)

// Sentinels. Each maps 1:1 to a spec §5.5 invariant (V1-V11) and its
// A2 twin in internal/gates/approval/config.go.
var (
	// ErrInvalidDecisionWindow covers V1 — decision_window must be > 0.
	ErrInvalidDecisionWindow = errors.New("approval_gate: decision_window must be > 0")
	// ErrInvalidTimeout covers V2 — timeout must be > 0.
	ErrInvalidTimeout = errors.New("approval_gate: timeout must be > 0")
	// ErrWindowExceedsTimeout covers V3+V4 — per-tier and top-level.
	ErrWindowExceedsTimeout = errors.New("approval_gate: decision_window must be <= timeout")
	// ErrAutoApproveRequiresLowRisk covers V5 — foot-gun prevention.
	ErrAutoApproveRequiresLowRisk = errors.New("approval_gate: on_timeout=auto_approve requires risk_class=low")
	// ErrInvalidRiskClass covers V6.
	ErrInvalidRiskClass = errors.New("approval_gate: risk_class must be low|medium|high")
	// ErrQuorumExceedsReviewers covers V7 — both quorum<1 and quorum>|set|.
	ErrQuorumExceedsReviewers = errors.New("approval_gate: quorum must be 1..len(reviewers+roles)")
	// ErrInvalidOnTimeout covers V8.
	ErrInvalidOnTimeout = errors.New("approval_gate: on_timeout must be fail|auto_approve|escalate")
	// ErrEscalateMissingChain covers V9.
	ErrEscalateMissingChain = errors.New("approval_gate: on_timeout=escalate requires non-empty escalation_chain")
	// ErrInvalidReviewerID covers V10 — same charset as approval_events.actor.
	ErrInvalidReviewerID = errors.New("approval_gate: reviewer id must match [a-zA-Z0-9_:.-]{1,128}")
	// ErrInvalidGateName covers V11 — URL-safe alphanum + _-.
	ErrInvalidGateName = errors.New("approval_gate: gate name must match [a-zA-Z0-9_-]{1,64}")
	// ErrInvalidDuration fires when timeout/decision_window is present
	// in YAML but time.ParseDuration rejects it. CUE catches most via
	// the duration regex; the Go validator surfaces a typed sentinel
	// rather than a wrapped strconv error.
	ErrInvalidDuration = errors.New("approval_gate: duration field unparseable")
	// ErrSchemaValidation wraps every CUE rejection so callers can
	// distinguish "yaml shape wrong" from "invariant violated."
	ErrSchemaValidation = errors.New("approval_gate: regatta.yaml fails CUE schema")
)

// ApprovalTierConfig is one rung of an escalation chain. Matches
// state.TierConfig field-for-field so the snapshot persisted via the
// approvals row round-trips byte-identically through JSON. yaml tags
// here, json tags on state.TierConfig.
type ApprovalTierConfig struct {
	Reviewers         []string      `yaml:"reviewers"`
	Roles             []string      `yaml:"roles"`
	Quorum            int           `yaml:"quorum"`
	PreventSelfReview bool          `yaml:"prevent_self_review"`
	Timeout           time.Duration `yaml:"timeout"`
	DecisionWindow    time.Duration `yaml:"decision_window"`
}

// ApprovalGateConfig is the parsed gate entry from regatta.yaml. Field
// names + yaml tags mirror A2's approval.Config in
// internal/gates/approval/config.go so the consolidation diff is a pure
// type-alias swap once PR #144 lands.
type ApprovalGateConfig struct {
	Name              string               `yaml:"name"`
	RiskClass         string               `yaml:"risk_class"`
	Reviewers         []string             `yaml:"reviewers"`
	Roles             []string             `yaml:"roles"`
	Quorum            int                  `yaml:"quorum"`
	PreventSelfReview bool                 `yaml:"prevent_self_review"`
	Timeout           time.Duration        `yaml:"timeout"`
	DecisionWindow    time.Duration        `yaml:"decision_window"`
	OnTimeout         string               `yaml:"on_timeout"`
	EscalationChain   []ApprovalTierConfig `yaml:"escalation_chain"`
	PredicateCEL      string               `yaml:"predicate_cel"`
}

// rawGateEntry is the over-the-wire YAML view: durations as strings
// (operator-friendly "24h" rather than nanoseconds). The Go loader
// re-parses to time.Duration after CUE validation so the typed Config
// never carries a bad-format duration past the boundary. json tags
// match the CUE field names so cue.Value.Decode populates the struct;
// CUE's Decode uses JSON marshalling, not yaml.v3.
type rawGateEntry struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// approval_gate fields. omitempty so the non-approval rows
	// (`ai`, `deterministic`) still decode without spurious zero values.
	Name              string         `json:"name,omitempty"`
	RiskClass         string         `json:"risk_class,omitempty"`
	Reviewers         []string       `json:"reviewers,omitempty"`
	Roles             []string       `json:"roles,omitempty"`
	Quorum            int            `json:"quorum,omitempty"`
	PreventSelfReview bool           `json:"prevent_self_review,omitempty"`
	Timeout           string         `json:"timeout,omitempty"`
	DecisionWindow    string         `json:"decision_window,omitempty"`
	OnTimeout         string         `json:"on_timeout,omitempty"`
	EscalationChain   []rawTierEntry `json:"escalation_chain,omitempty"`
	PredicateCEL      string         `json:"predicate_cel,omitempty"`
}

type rawTierEntry struct {
	Reviewers         []string `json:"reviewers,omitempty"`
	Roles             []string `json:"roles,omitempty"`
	Quorum            int      `json:"quorum,omitempty"`
	PreventSelfReview bool     `json:"prevent_self_review,omitempty"`
	Timeout           string   `json:"timeout,omitempty"`
	DecisionWindow    string   `json:"decision_window,omitempty"`
}

// LoadApprovalGates extracts every `type: approval_gate` row from a
// regatta.yaml. It runs the CUE schema first (so field-shape errors
// surface in operator-friendly form), then walks the validated gates
// list, decodes each approval_gate row, parses durations, and runs the
// Go-side invariants V1-V11 (which CUE cannot cross-reference).
//
// Returns nil + nil when no approval_gate is present so callers can
// range over the result without nil-checking. Returns nil + error when
// any approval_gate fails validation; partial success is not surfaced
// because a half-valid gates list would let an unconfigured production
// path through.
func LoadApprovalGates(data []byte) ([]ApprovalGateConfig, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty input", ErrSchemaValidation)
	}

	ctx := cuecontext.New()
	schema := ctx.CompileString(schemas.RegattaV1CUE, cue.Filename("regatta.v1.cue"))
	if err := schema.Err(); err != nil {
		return nil, fmt.Errorf("%w: schema compile: %s", ErrSchemaValidation, cueerrors.Details(err, nil))
	}
	cfgFile, err := yaml.Extract("regatta.yaml", data)
	if err != nil {
		return nil, fmt.Errorf("%w: yaml parse: %s", ErrSchemaValidation, cueerrors.Details(err, nil))
	}
	cfg := ctx.BuildFile(cfgFile)
	if err := cfg.Err(); err != nil {
		return nil, fmt.Errorf("%w: yaml build: %s", ErrSchemaValidation, cueerrors.Details(err, nil))
	}
	unified := schema.Unify(cfg)
	if err := unified.Validate(cue.Concrete(true), cue.All()); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSchemaValidation, cueerrors.Details(err, nil))
	}

	// CUE accepted. Now decode gates[] into raw rows. We decode from
	// the YAML-built value (not the unified one) because Unify carries
	// the schema's `*default` markers which would surface in Decode.
	gatesVal := cfg.LookupPath(cue.ParsePath("gates"))
	if !gatesVal.Exists() {
		return nil, nil
	}
	iter, err := gatesVal.List()
	if err != nil {
		return nil, fmt.Errorf("%w: gates is not a list: %w", ErrSchemaValidation, err)
	}

	var (
		out  []ApprovalGateConfig
		errs []error
	)
	for iter.Next() {
		var raw rawGateEntry
		if err := iter.Value().Decode(&raw); err != nil {
			// Should be unreachable after CUE validate, but a stray
			// schema drift would land here; surface explicitly.
			errs = append(errs, fmt.Errorf("decode gate entry: %w", err))
			continue
		}
		if raw.Type != "approval_gate" {
			continue
		}
		cfg, vErr := convertAndValidate(raw)
		if vErr != nil {
			errs = append(errs, vErr)
			continue
		}
		out = append(out, cfg)
	}
	if joined := errors.Join(errs...); joined != nil {
		return nil, joined
	}
	return out, nil
}

// convertAndValidate parses raw durations and runs V1-V11. Duration
// parse errors short-circuit per-field but the validator still runs
// against the partially-populated config so the operator sees every
// V1-V11 violation in one pass.
func convertAndValidate(raw rawGateEntry) (ApprovalGateConfig, error) {
	cfg := ApprovalGateConfig{
		Name:              raw.Name,
		RiskClass:         raw.RiskClass,
		Reviewers:         raw.Reviewers,
		Roles:             raw.Roles,
		Quorum:            raw.Quorum,
		PreventSelfReview: raw.PreventSelfReview,
		OnTimeout:         raw.OnTimeout,
		PredicateCEL:      raw.PredicateCEL,
	}

	var errs []error
	if raw.Timeout != "" {
		d, err := time.ParseDuration(raw.Timeout)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: timeout=%q: %w", ErrInvalidDuration, raw.Timeout, err))
		} else {
			cfg.Timeout = d
		}
	}
	if raw.DecisionWindow != "" {
		d, err := time.ParseDuration(raw.DecisionWindow)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: decision_window=%q: %w", ErrInvalidDuration, raw.DecisionWindow, err))
		} else {
			cfg.DecisionWindow = d
		}
	}
	for i, t := range raw.EscalationChain {
		tier := ApprovalTierConfig{
			Reviewers:         t.Reviewers,
			Roles:             t.Roles,
			Quorum:            t.Quorum,
			PreventSelfReview: t.PreventSelfReview,
		}
		if t.Timeout != "" {
			d, err := time.ParseDuration(t.Timeout)
			if err != nil {
				errs = append(errs, fmt.Errorf("%w: escalation_chain[%d].timeout=%q: %w",
					ErrInvalidDuration, i, t.Timeout, err))
			} else {
				tier.Timeout = d
			}
		}
		if t.DecisionWindow != "" {
			d, err := time.ParseDuration(t.DecisionWindow)
			if err != nil {
				errs = append(errs, fmt.Errorf("%w: escalation_chain[%d].decision_window=%q: %w",
					ErrInvalidDuration, i, t.DecisionWindow, err))
			} else {
				tier.DecisionWindow = d
			}
		}
		cfg.EscalationChain = append(cfg.EscalationChain, tier)
	}

	if err := cfg.Validate(); err != nil {
		errs = append(errs, err)
	}
	return cfg, errors.Join(errs...)
}

// Validate enforces V1-V11 from spec §5.5. Accumulates every failure
// via errors.Join so a misconfigured regatta.yaml surfaces every defect
// in one CI run rather than fix-one-find-next. Mirrors A2's
// approval.Config.Validate() pending consolidation.
func (c ApprovalGateConfig) Validate() error {
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
	// V6 — RiskClass enum (allow empty when on_timeout != auto_approve).
	if c.RiskClass != "" && !isValidRiskClass(c.RiskClass) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidRiskClass, c.RiskClass))
	}
	// V8 — OnTimeout enum.
	if !isValidOnTimeout(c.OnTimeout) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidOnTimeout, c.OnTimeout))
	}
	// V5 — auto_approve requires low risk. Spec calls this the foot-gun:
	// enforce at config-load, not runtime.
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

// V10 charset mirrors approval_events.actor CHECK constraint (spec §5.1)
// so config-time rejection prevents a later DB-level CHECK violation
// that operators can't easily attribute.
var reviewerIDRE = regexp.MustCompile(`^[a-zA-Z0-9_:.-]{1,128}$`)

// V11 — Names appear in URLs + slog records; restrict to alnum + _-,
// 1..64 chars. Identical to A2's gateNameRE.
var gateNameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// CONSOLIDATION: This file duplicates the type + sentinels + Validate
// in internal/gates/approval/config.go (PR #144). When that PR merges,
// fold this loader's ApprovalGateConfig into approval.Config via type
// alias, drop the duplicate sentinels, and have LoadApprovalGates
// return []approval.Config. Tracked separately so this PR remains
// independently mergeable. See gates_consolidation_followup issue.