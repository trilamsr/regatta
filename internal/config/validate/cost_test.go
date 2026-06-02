package validate_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/config/validate"
)

const baseYAML = `version: 1
repo:
  host: github
  owner: example
  name: myproject
spec_adapter:
  type: github_issues
  selector: 'label:planned'
ci:
  command: 'make test'
gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: ['fail']
`

func TestCUEValidate_CostUnset_PassesAndDefaults(t *testing.T) {
	yaml := baseYAML + "safety: {}\n"
	if err := validate.ValidateConfig([]byte(yaml)); err != nil {
		t.Fatalf("ValidateConfig: %v; want nil (cost unset is valid + MVP-2 byte-equal)", err)
	}
}

func TestCUEValidate_EmptyCostBlock_Rejected(t *testing.T) {
	yaml := baseYAML + "safety:\n  cost: {}\n"
	err := validate.ValidateConfig([]byte(yaml))
	if err == nil {
		t.Fatalf("ValidateConfig: nil; want ErrCostBlockEmpty")
	}
	if !errors.Is(err, validate.ErrCostBlockEmpty) {
		t.Fatalf("err=%v; want ErrCostBlockEmpty", err)
	}
}

func TestCUEValidate_AllCapsZero_RejectedWithMessage(t *testing.T) {
	yaml := baseYAML + `safety:
  spend_cap_usd: 0
  cost:
    per_dag_usd: 0
    per_operator_usd: 0
    per_work_item_usd: 0
`
	err := validate.ValidateConfig([]byte(yaml))
	if err == nil {
		t.Fatalf("ValidateConfig: nil; want ErrCostCapsAllZero")
	}
	if !errors.Is(err, validate.ErrCostCapsAllZero) {
		t.Fatalf("err=%v; want ErrCostCapsAllZero", err)
	}
	if !strings.Contains(err.Error(), "deny every spawn") {
		t.Fatalf("err=%q; want operator-friendly message naming the deny-every-spawn trap", err)
	}
}

// TestCUEValidate_EstimationStrategy_HistoryAccepted pins the opt-in
// flag from spec §10 S1 (issue #238): `history` is a valid value, so
// operators who set it pass CUE validation.
func TestCUEValidate_EstimationStrategy_HistoryAccepted(t *testing.T) {
	yaml := baseYAML + `safety:
  cost:
    per_dag_usd: 100
    estimation_strategy: history
`
	if err := validate.ValidateConfig([]byte(yaml)); err != nil {
		t.Fatalf("ValidateConfig: %v; want nil for estimation_strategy=history (opt-in flag)", err)
	}
}

// TestCUEValidate_EstimationStrategy_DefaultUpperBound pins the
// additive-default property: omitting the field is byte-equal to the
// pre-#238 behaviour (no breaking change).
func TestCUEValidate_EstimationStrategy_DefaultUpperBound(t *testing.T) {
	yaml := baseYAML + `safety:
  cost:
    per_dag_usd: 100
`
	if err := validate.ValidateConfig([]byte(yaml)); err != nil {
		t.Fatalf("ValidateConfig: %v; want nil (estimation_strategy defaults to upper_bound)", err)
	}
}

// TestCUEValidate_EstimationStrategy_InvalidRejected pins the
// closed-enum property: only upper_bound | history are accepted.
// Typos like `historical` must fail at config-load time, not at the
// first spawn.
func TestCUEValidate_EstimationStrategy_InvalidRejected(t *testing.T) {
	yaml := baseYAML + `safety:
  cost:
    per_dag_usd: 100
    estimation_strategy: historical
`
	err := validate.ValidateConfig([]byte(yaml))
	if err == nil {
		t.Fatalf("ValidateConfig: nil; want CUE rejection of estimation_strategy=historical")
	}
	if !strings.Contains(err.Error(), "estimation_strategy") {
		t.Fatalf("err=%q; want CUE error naming estimation_strategy field", err)
	}
}

func TestCUEValidate_SoftPctOutOfRange_Rejected(t *testing.T) {
	yaml := baseYAML + `safety:
  cost:
    per_dag_usd: 100
    soft_pct: 49
`
	err := validate.ValidateConfig([]byte(yaml))
	if err == nil {
		t.Fatalf("ValidateConfig: nil; want CUE rejection of soft_pct=49")
	}
	if !strings.Contains(err.Error(), "soft_pct") {
		t.Fatalf("err=%q; want CUE error naming soft_pct field", err)
	}
}
