package validateconfig

import (
	"strings"
	"testing"
)

const minimalValid = `
version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:planned"
ci:
  command: "go test ./..."
gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`

func TestLoad_MinimalValid_NoError(t *testing.T) {
	if err := LoadBytes([]byte(minimalValid)); err != nil {
		t.Fatalf("expected nil error on valid config; got %v", err)
	}
}

func TestLoad_WrongVersion_Errors(t *testing.T) {
	yaml := strings.Replace(minimalValid, "version: 1", "version: 2", 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for version=2 (schema is v1); got nil")
	}
}

func TestLoad_InvalidHost_Errors(t *testing.T) {
	yaml := strings.Replace(minimalValid, "host: github", "host: bitbucket", 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for unknown host; got nil")
	}
}

func TestLoad_NoGates_Errors(t *testing.T) {
	yaml := strings.Replace(minimalValid, `gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
`, "gates: []\n", 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for empty gates list; got nil")
	}
}

func TestLoad_CanaryRateTooHigh_Errors(t *testing.T) {
	yaml := minimalValid + "  canary_rate: 0.5\n"
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for canary_rate > 0.2; got nil")
	}
}
