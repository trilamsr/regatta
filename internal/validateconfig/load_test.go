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

// TestLoad_MultiErrorEnumerated pins down the verbose-error contract:
// a config with multiple independent violations must surface each one
// in the error string, not collapse to "(and N more errors)".
func TestLoad_MultiErrorEnumerated(t *testing.T) {
	yaml := strings.Replace(minimalValid, "host: github", "host: bitbucket", 1)
	yaml = strings.Replace(yaml, "agent_creds_scope: dev_only", "agent_creds_scope: god_mode", 1)
	err := LoadBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bitbucket") {
		t.Errorf("error should mention bitbucket; got: %s", msg)
	}
	if !strings.Contains(msg, "god_mode") {
		t.Errorf("error should mention god_mode; got: %s", msg)
	}
	if strings.Contains(msg, "and 2 more errors") || strings.Contains(msg, "and 1 more error") {
		t.Errorf("error must not elide details with '(and N more errors)'; got: %s", msg)
	}
}

func TestLoad_MalformedYAML_Errors(t *testing.T) {
	err := LoadBytes([]byte("not: valid: yaml::: ["))
	if err == nil {
		t.Fatal("expected error for malformed YAML; got nil")
	}
}

func TestLoad_EmptyBytes_Errors(t *testing.T) {
	err := LoadBytes([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty config; got nil")
	}
}

func TestLoadFile_NonexistentPath_ErrorMentionsPath(t *testing.T) {
	err := LoadFile("/nonexistent/regatta.yaml")
	if err == nil {
		t.Fatal("expected error for missing file; got nil")
	}
	if !strings.Contains(err.Error(), "/nonexistent/regatta.yaml") {
		t.Errorf("error should mention the missing path; got: %s", err)
	}
}

func TestLoad_MarkdownCatalogAdapter_Valid(t *testing.T) {
	yaml := strings.Replace(minimalValid, `spec_adapter:
  type: github_issues
  selector: "label:planned"
`, `spec_adapter:
  type: markdown_catalog
  path: docs/MILESTONES.md
`, 1)
	if err := LoadBytes([]byte(yaml)); err != nil {
		t.Fatalf("expected nil error for markdown_catalog adapter; got %v", err)
	}
}

func TestLoad_DeterministicGate_Valid(t *testing.T) {
	yaml := strings.Replace(minimalValid, `gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
`, `gates:
  - id: license_check
    type: deterministic
    command: ./scripts/license.sh
`, 1)
	if err := LoadBytes([]byte(yaml)); err != nil {
		t.Fatalf("expected nil error for deterministic gate; got %v", err)
	}
}

func TestLoad_GateIDBadChars_Errors(t *testing.T) {
	yaml := strings.Replace(minimalValid, "id: spec_conformance", "id: SpecConformance", 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for gate id with uppercase (schema requires ^[a-z0-9_-]+$); got nil")
	}
}
