// Package config provides typed loaders for the per-repo `regatta.yaml`.
// Today it surfaces only the `approval_gate` entries from the
// discriminated-union `gates[]` block. Other gate types still live in
// the CUE-only validator at internal/config/validate; the typed extractor
// lands here as each consumer grows a Go-side need.
package config

import (
	"errors"
	"fmt"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/yaml"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/gates/approval"
)

// Loader-only sentinels. The V1-V11 invariant sentinels live with the
// canonical Config type at internal/gates/approval (see config.go); this
// loader joins them through errors.Join so callers can errors.Is on
// either layer.
var (
	// ErrInvalidDuration fires when timeout/decision_window is present
	// in YAML but time.ParseDuration rejects it. CUE catches most via
	// the duration regex; the Go loader surfaces a typed sentinel
	// rather than a wrapped strconv error.
	ErrInvalidDuration = errors.New("approval_gate: duration field unparseable")
	// ErrSchemaValidation wraps every CUE rejection so callers can
	// distinguish "yaml shape wrong" from "invariant violated."
	ErrSchemaValidation = errors.New("approval_gate: regatta.yaml fails CUE schema")
)

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
// Go-side invariants V1-V11 (which CUE cannot cross-reference) via
// approval.Config.Validate.
//
// Returns nil + nil when no approval_gate is present so callers can
// range over the result without nil-checking. Returns nil + error when
// any approval_gate fails validation; partial success is not surfaced
// because a half-valid gates list would let an unconfigured production
// path through.
func LoadApprovalGates(data []byte) ([]approval.Config, error) {
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
		out  []approval.Config
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

// convertAndValidate parses raw durations and runs V1-V11 via
// approval.Config.Validate. Duration parse errors short-circuit per-field
// but the validator still runs against the partially-populated config so
// the operator sees every V1-V11 violation in one pass.
func convertAndValidate(raw rawGateEntry) (approval.Config, error) {
	cfg := approval.Config{
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
		tier := approval.TierConfig{
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
