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

// TestCUEValidate_EstimationStrategy_HistoryAccepted pins spec §10 S1 (#238) — `history` passes CUE validation.
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

// TestCUEValidate_EstimationStrategy_DefaultUpperBound pins additive-default — omitted field is byte-equal to pre-#238.
func TestCUEValidate_EstimationStrategy_DefaultUpperBound(t *testing.T) {
	yaml := baseYAML + `safety:
  cost:
    per_dag_usd: 100
`
	if err := validate.ValidateConfig([]byte(yaml)); err != nil {
		t.Fatalf("ValidateConfig: %v; want nil (estimation_strategy defaults to upper_bound)", err)
	}
}

// TestCUEValidate_EstimationStrategy_InvalidRejected pins closed-enum — typos like `historical` fail at config-load.
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

func TestCUEValidate_PricingOverridePath_Accepted(t *testing.T) {
	yaml := baseYAML + `safety:
  cost:
    per_dag_usd: 100
    pricing_override_path: /etc/regatta/pricing.json
`
	if err := validate.ValidateConfig([]byte(yaml)); err != nil {
		t.Fatalf("ValidateConfig: %v; want nil (pricing_override_path is a valid optional field)", err)
	}
}

func TestCUEValidate_PricingOverridePath_NonStringRejected(t *testing.T) {
	yaml := baseYAML + `safety:
  cost:
    per_dag_usd: 100
    pricing_override_path: 42
`
	err := validate.ValidateConfig([]byte(yaml))
	if err == nil {
		t.Fatalf("ValidateConfig: nil; want CUE rejection of non-string pricing_override_path")
	}
	if !strings.Contains(err.Error(), "pricing_override_path") {
		t.Fatalf("err=%q; want CUE error naming pricing_override_path field", err)
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

// TestConfig_SoftCapRequiresExplicitAck pins issue #226 — warn mode without ack is rejected.
func TestConfig_SoftCapRequiresExplicitAck(t *testing.T) {
	yaml := baseYAML + `safety:
  soft_cap_mode: warn
  cost:
    per_dag_usd: 100
`
	err := validate.ValidateConfig([]byte(yaml))
	if err == nil {
		t.Fatalf("ValidateConfig: nil; want ErrSoftCapNotAcknowledged (warn mode without ack is a silent-correctness regression)")
	}
	if !errors.Is(err, validate.ErrSoftCapNotAcknowledged) {
		t.Fatalf("err=%v; want ErrSoftCapNotAcknowledged", err)
	}
	if !strings.Contains(err.Error(), "soft_cap_acknowledge_overrun") {
		t.Fatalf("err=%q; want operator-friendly message naming soft_cap_acknowledge_overrun", err)
	}
}

// TestConfig_SoftCapWarnWithAck_Accepted pins #226 happy path — explicit ack permits warn mode.
func TestConfig_SoftCapWarnWithAck_Accepted(t *testing.T) {
	yaml := baseYAML + `safety:
  soft_cap_mode: warn
  soft_cap_acknowledge_overrun: true
  cost:
    per_dag_usd: 100
`
	if err := validate.ValidateConfig([]byte(yaml)); err != nil {
		t.Fatalf("ValidateConfig: %v; want nil (warn mode + explicit ack is valid)", err)
	}
}

// TestConfig_SoftCapAckWithoutWarn_Accepted pins additive-default — ack is no-op when mode is enforce.
func TestConfig_SoftCapAckWithoutWarn_Accepted(t *testing.T) {
	yaml := baseYAML + `safety:
  soft_cap_acknowledge_overrun: true
  cost:
    per_dag_usd: 100
`
	if err := validate.ValidateConfig([]byte(yaml)); err != nil {
		t.Fatalf("ValidateConfig: %v; want nil (ack without warn mode is a harmless no-op)", err)
	}
}

// TestConfig_SoftCapModeEnforce_NoAckRequired pins default mode — enforce needs no ack.
func TestConfig_SoftCapModeEnforce_NoAckRequired(t *testing.T) {
	yaml := baseYAML + `safety:
  soft_cap_mode: enforce
  cost:
    per_dag_usd: 100
`
	if err := validate.ValidateConfig([]byte(yaml)); err != nil {
		t.Fatalf("ValidateConfig: %v; want nil (enforce mode never requires ack)", err)
	}
}
