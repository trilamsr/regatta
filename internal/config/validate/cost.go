package validate

import (
	"errors"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrCostBlockEmpty fires when safety.cost is present but no cap
// fields are set. Spec §3.6 closes I4 (no overhead from empty block).
var ErrCostBlockEmpty = errors.New("safety.cost is set but no caps are configured — either set ≥ 1 cap field or omit the cost block entirely")

// ErrCostCapsAllZero fires when every configured cap (including the
// legacy safety.spend_cap_usd) is explicitly zero. Spec §3.6 + R7.
var ErrCostCapsAllZero = errors.New("all configured caps are zero — this would deny every spawn. To opt out of cost governance entirely, omit the `safety.cost` block; to allow unbounded, omit individual caps")

// ErrSoftCapNotAcknowledged fires when `safety.soft_cap_mode: warn` is
// set without the paired `safety.soft_cap_acknowledge_overrun: true`
// opt-in. Closes the silent-correctness regression: warn-but-allow lets
// spend cross the soft cap with only a log event, which a reviewer who
// skims the diff for `soft_cap_mode: warn` may not realise.
var ErrSoftCapNotAcknowledged = errors.New("safety.soft_cap_mode=warn requires safety.soft_cap_acknowledge_overrun=true — warn mode permits spawns past the 80% soft cap with only a log event; set the ack field to confirm the silent-overrun risk is understood, or change soft_cap_mode to enforce")

// ValidateConfig validates YAML bytes against the CUE schema AND runs
// the cost-governor cross-field checks the CUE language does not
// express cleanly (empty-block detection, all-zero-caps trap). Tests
// call this directly; production wiring chains it after LoadBytes in
// the operator-doc step (T5 wave 3 — followup).
//
// Returns a typed sentinel from this package on cost-specific
// failures so callers can errors.Is() rather than string-match.
func ValidateConfig(data []byte) error {
	if err := LoadBytes(data); err != nil {
		return err
	}
	return validateCost(data)
}

type rawConfig struct {
	Safety *rawSafety `yaml:"safety"`
}

type rawSafety struct {
	SpendCapUSD               *int     `yaml:"spend_cap_usd"`
	SoftCapMode               *string  `yaml:"soft_cap_mode"`
	SoftCapAcknowledgeOverrun *bool    `yaml:"soft_cap_acknowledge_overrun"`
	Cost                      *rawCost `yaml:"cost"`
	costPresent               bool
}

// UnmarshalYAML records whether the `cost:` key was present (even if
// empty) so the empty-block branch is distinguishable from "cost omitted".
func (r *rawSafety) UnmarshalYAML(node *yaml.Node) error {
	type plain rawSafety
	if err := node.Decode((*plain)(r)); err != nil {
		return err
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == "cost" {
			r.costPresent = true
			if r.Cost == nil {
				r.Cost = &rawCost{}
			}
			break
		}
	}
	return nil
}

type rawCost struct {
	PerDAGUSD          *int    `yaml:"per_dag_usd"`
	PerOperatorUSD     *int    `yaml:"per_operator_usd"`
	PerWorkItemUSD     *int    `yaml:"per_work_item_usd"`
	EstimationStrategy *string `yaml:"estimation_strategy"`
	ReconcileInterval  *string `yaml:"reconcile_interval"`
	UsageAPIKeyEnv     *string `yaml:"usage_api_key_env"`
}

func (c *rawCost) anyCapSet() bool {
	return c.PerDAGUSD != nil || c.PerOperatorUSD != nil || c.PerWorkItemUSD != nil
}

func (c *rawCost) allCapsZero() bool {
	return c.PerDAGUSD != nil && *c.PerDAGUSD == 0 &&
		c.PerOperatorUSD != nil && *c.PerOperatorUSD == 0 &&
		c.PerWorkItemUSD != nil && *c.PerWorkItemUSD == 0
}

// CostReconcileSettings is the subset of safety.cost the reconciler
// loop needs at startup. ReconcileInterval == 0 means the operator did
// not set the field (cost block omitted, cost.reconcile_interval unset);
// callers treat 0 as "do not start the reconciler goroutine".
type CostReconcileSettings struct {
	ReconcileInterval time.Duration
	UsageAPIKeyEnv    string
}

// LoadCostReconcileSettings extracts safety.cost.{reconcile_interval,
// usage_api_key_env} from regatta.yaml bytes. The CUE schema constrains
// the interval to the documented enum (1h default, 5m/15m/30m/6h/24h);
// a missing block returns the zero value so cmd/regatta can no-op.
//
// Wave-1 single-tenant wiring: this is the only Go-side surface that
// reads reconcile_interval — when W8 ships per-tenant overrides, the
// loader extends here, not at every caller.
func LoadCostReconcileSettings(data []byte) (CostReconcileSettings, error) {
	if len(data) == 0 {
		return CostReconcileSettings{}, nil
	}
	var cfg rawConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return CostReconcileSettings{}, fmt.Errorf("cost-reconcile: %w", err)
	}
	if cfg.Safety == nil || cfg.Safety.Cost == nil {
		return CostReconcileSettings{}, nil
	}
	out := CostReconcileSettings{}
	if cfg.Safety.Cost.ReconcileInterval != nil && *cfg.Safety.Cost.ReconcileInterval != "" {
		d, err := time.ParseDuration(*cfg.Safety.Cost.ReconcileInterval)
		if err != nil {
			return CostReconcileSettings{}, fmt.Errorf("cost-reconcile: parse reconcile_interval=%q: %w",
				*cfg.Safety.Cost.ReconcileInterval, err)
		}
		out.ReconcileInterval = d
	}
	if cfg.Safety.Cost.UsageAPIKeyEnv != nil {
		out.UsageAPIKeyEnv = *cfg.Safety.Cost.UsageAPIKeyEnv
	}
	return out, nil
}

func validateCost(data []byte) error {
	var cfg rawConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("cost-validate: %w", err)
	}
	if cfg.Safety == nil {
		return nil
	}
	// soft_cap_mode=warn without explicit ack is rejected even when no
	// cost block is configured — the mode field itself signals intent
	// and the operator must own the silent-overrun risk regardless of
	// whether caps are wired up yet. Issue #226.
	if cfg.Safety.SoftCapMode != nil && *cfg.Safety.SoftCapMode == "warn" {
		ack := cfg.Safety.SoftCapAcknowledgeOverrun
		if ack == nil || !*ack {
			return ErrSoftCapNotAcknowledged
		}
	}
	if !cfg.Safety.costPresent {
		return nil
	}
	cost := cfg.Safety.Cost
	if cost == nil || !cost.anyCapSet() {
		return ErrCostBlockEmpty
	}
	legacyZero := cfg.Safety.SpendCapUSD != nil && *cfg.Safety.SpendCapUSD == 0
	if legacyZero && cost.allCapsZero() {
		return ErrCostCapsAllZero
	}
	return nil
}
