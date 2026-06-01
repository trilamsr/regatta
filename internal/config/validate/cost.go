package validate

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ErrCostBlockEmpty fires when safety.cost is present but no cap
// fields are set. Spec §3.6 closes I4 (no overhead from empty block).
var ErrCostBlockEmpty = errors.New("safety.cost is set but no caps are configured — either set ≥ 1 cap field or omit the cost block entirely")

// ErrCostCapsAllZero fires when every configured cap (including the
// legacy safety.spend_cap_usd) is explicitly zero. Spec §3.6 + R7.
var ErrCostCapsAllZero = errors.New("all configured caps are zero — this would deny every spawn. To opt out of cost governance entirely, omit the `safety.cost` block; to allow unbounded, omit individual caps")

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
	SpendCapUSD *int      `yaml:"spend_cap_usd"`
	Cost        *rawCost  `yaml:"cost"`
	costPresent bool
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
	PerDAGUSD      *int `yaml:"per_dag_usd"`
	PerOperatorUSD *int `yaml:"per_operator_usd"`
	PerWorkItemUSD *int `yaml:"per_work_item_usd"`
}

func (c *rawCost) anyCapSet() bool {
	return c.PerDAGUSD != nil || c.PerOperatorUSD != nil || c.PerWorkItemUSD != nil
}

func (c *rawCost) allCapsZero() bool {
	return c.PerDAGUSD != nil && *c.PerDAGUSD == 0 &&
		c.PerOperatorUSD != nil && *c.PerOperatorUSD == 0 &&
		c.PerWorkItemUSD != nil && *c.PerWorkItemUSD == 0
}

func validateCost(data []byte) error {
	var cfg rawConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("cost-validate: %w", err)
	}
	if cfg.Safety == nil || !cfg.Safety.costPresent {
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
