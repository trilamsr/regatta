package validate

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

// TestLoad_MultiErrorEnumerated pins down the verbose-error contract: a config with multiple independent violations must surface each one in t
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
  root: .
`, 1)
	if err := LoadBytes([]byte(yaml)); err != nil {
		t.Fatalf("expected nil error for markdown_catalog adapter; got %v", err)
	}
}

// TestLoad_MarkdownCatalog_RootDefaults pins that omitting `root` is legal — the CUE default ("." per regatta.v1.cue §SpecAdapter markdown_cat
func TestLoad_MarkdownCatalog_RootDefaults(t *testing.T) {
	yaml := strings.Replace(minimalValid, `spec_adapter:
  type: github_issues
  selector: "label:planned"
`, `spec_adapter:
  type: markdown_catalog
`, 1)
	if err := LoadBytes([]byte(yaml)); err != nil {
		t.Fatalf("expected nil error for markdown_catalog adapter with default root; got %v", err)
	}
}

// TestLoad_MarkdownCatalog_DeadPathField_Errors pins the schema rename: the legacy `path` field (which had no runtime consumer) is rejected af
func TestLoad_MarkdownCatalog_DeadPathField_Errors(t *testing.T) {
	yaml := strings.Replace(minimalValid, `spec_adapter:
  type: github_issues
  selector: "label:planned"
`, `spec_adapter:
  type: markdown_catalog
  path: docs/MILESTONES.md
`, 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected CUE rejection for dead `path` field on markdown_catalog; got nil")
	}
}

// TestLoad_MarkdownCatalog_RootSurfacedOnConfig pins the typed accessor downstream callers use to read the adapter root from a parsed config. 
func TestLoad_MarkdownCatalog_RootSurfacedOnConfig(t *testing.T) {
	yaml := strings.Replace(minimalValid, `spec_adapter:
  type: github_issues
  selector: "label:planned"
`, `spec_adapter:
  type: markdown_catalog
  root: subdir
`, 1)
	cfg, err := LoadConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.MarkdownCatalogRoot(); got != "subdir" {
		t.Fatalf("MarkdownCatalogRoot()=%q; want %q", got, "subdir")
	}
}

// TestLoad_MarkdownCatalog_RootDefaultSurfaced pins the CUE-default flow: an operator who omits `root` reads "." back, not "".
func TestLoad_MarkdownCatalog_RootDefaultSurfaced(t *testing.T) {
	yaml := strings.Replace(minimalValid, `spec_adapter:
  type: github_issues
  selector: "label:planned"
`, `spec_adapter:
  type: markdown_catalog
`, 1)
	cfg, err := LoadConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.MarkdownCatalogRoot(); got != "." {
		t.Fatalf("MarkdownCatalogRoot()=%q; want %q (default)", got, ".")
	}
}

// TestLoad_NonMarkdownAdapter_RootEmpty pins MarkdownCatalogRoot() == "" for non-markdown adapter types, so serve.go can distinguish "yaml did
func TestLoad_NonMarkdownAdapter_RootEmpty(t *testing.T) {
	cfg, err := LoadConfig([]byte(minimalValid))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.MarkdownCatalogRoot(); got != "" {
		t.Fatalf("MarkdownCatalogRoot()=%q; want \"\" for github_issues adapter", got)
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

// TestLoad_DesignDocCanonicalExample_Valid pins the schema to the canonical example in docs/design.md §Per-repo configuration. If this fails, 
func TestLoad_DesignDocCanonicalExample_Valid(t *testing.T) {
	yaml := `
version: 1
repo: { host: github, owner: example, name: myproject }
spec_adapter: { type: github_issues, selector: 'label:planned' }
ci: { command: 'npm test && npm run lint' }
gates:
  - { id: spec_conformance, type: ai, model: claude-opus-4-7,   severity_block: ['fail'] }
  - { id: adversarial,      type: ai, model: claude-sonnet-4-6, severity_block: ['critical', '2*high'] }
  - { id: drift,            type: ai, model: claude-haiku-4-5,  severity_block: ['drift'] }
lanes:
  - { id: server, paths: ['src/server/**'], max_concurrency: 1 }
hotspots: [CHANGELOG.md, package.json, README.md]
safety: { iteration_cap: 50, spend_cap_usd: 50, canary_rate: 0.05 }
context: { agent_guidance_path: AGENTS.md, agent_guidance_codeowners_check: true }
telemetry: { audit_sink: 's3://acme-audit/regatta/?object-lock=COMPLIANCE' }
`
	if err := LoadBytes([]byte(yaml)); err != nil {
		t.Fatalf("design.md canonical example must validate; got error:\n%v", err)
	}
}

func TestLoad_AIGate_RequiresModel(t *testing.T) {
	yaml := strings.Replace(minimalValid, `gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
`, `gates:
  - id: spec_conformance
    type: ai
    severity_block: [fail]
`, 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for ai gate without model; got nil")
	}
}

func TestLoad_CustomAdapter_Valid(t *testing.T) {
	yaml := strings.Replace(minimalValid, `spec_adapter:
  type: github_issues
  selector: "label:planned"
`, `spec_adapter:
  type: custom
  command: /usr/local/bin/my-adapter
`, 1)
	if err := LoadBytes([]byte(yaml)); err != nil {
		t.Fatalf("expected nil error for custom adapter with command; got %v", err)
	}
}

func TestLoad_LaneIDBadChars_Errors(t *testing.T) {
	yaml := minimalValid + `lanes:
  - id: Server-Backend
    paths: [src/server/**]
`
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for lane id with uppercase; got nil")
	}
}

func TestLoad_LaneRequiresAtLeastOnePath(t *testing.T) {
	yaml := minimalValid + `lanes:
  - id: server
    paths: []
`
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for lane with empty paths; got nil")
	}
}

func TestLoad_IterationCapBelowMin_Errors(t *testing.T) {
	yaml := minimalValid + "  iteration_cap: 0\n"
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for iteration_cap=0 (min 1); got nil")
	}
}

func TestLoad_IterationCapAboveMax_Errors(t *testing.T) {
	yaml := minimalValid + "  iteration_cap: 501\n"
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for iteration_cap=501 (max 500); got nil")
	}
}

func TestLoad_NegativeSpendCap_Errors(t *testing.T) {
	yaml := minimalValid + "  spend_cap_usd: -1\n"
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for negative spend_cap_usd; got nil")
	}
}

func TestLoad_RepoOwnerInvalidChars_Errors(t *testing.T) {
	yaml := strings.Replace(minimalValid, "owner: trilamsr", "owner: 'trila msr'", 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for owner containing space; got nil")
	}
}

func TestLoad_ApprovalGate_CanonicalYAML_Valid(t *testing.T) {
	yaml := strings.Replace(minimalValid, `gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
`, `gates:
  - id: prod-deploy-approval
    type: approval_gate
    name: prod-deploy-approval
    risk_class: high
    reviewers: [alice, bob]
    roles: [sre]
    quorum: 2
    prevent_self_review: true
    timeout: 24h
    decision_window: 4h
    on_timeout: fail
    escalation_chain:
      - reviewers: [carol]
        quorum: 1
        timeout: 1h
        decision_window: 30m
`, 1)
	if err := LoadBytes([]byte(yaml)); err != nil {
		t.Fatalf("expected nil error for canonical approval_gate; got %v", err)
	}
}

func TestLoad_ApprovalGate_BadRiskClass_Errors(t *testing.T) {
	yaml := strings.Replace(minimalValid, `gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
`, `gates:
  - id: prod-deploy-approval
    type: approval_gate
    name: prod-deploy-approval
    risk_class: ultra
    reviewers: [alice]
    quorum: 1
    timeout: 1h
    decision_window: 30m
    on_timeout: fail
`, 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected error for risk_class=ultra (not in enum); got nil")
	}
}

// TestLoad_ApprovalGate_AutoApproveHighRisk_Errors pins V5 at CUE (validate-config rejection path).
func TestLoad_ApprovalGate_AutoApproveHighRisk_Errors(t *testing.T) {
	yaml := strings.Replace(minimalValid, `gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
`, `gates:
  - id: prod-deploy-approval
    type: approval_gate
    name: prod-deploy-approval
    risk_class: high
    reviewers: [alice]
    quorum: 1
    timeout: 1h
    decision_window: 30m
    on_timeout: auto_approve
`, 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected V5 rejection (auto_approve requires low); got nil")
	}
}

// TestLoad_ApprovalGate_EscalateNoChain_Errors pins V9 at CUE (escalate requires chain).
func TestLoad_ApprovalGate_EscalateNoChain_Errors(t *testing.T) {
	yaml := strings.Replace(minimalValid, `gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
`, `gates:
  - id: prod-deploy-approval
    type: approval_gate
    name: prod-deploy-approval
    risk_class: high
    reviewers: [alice]
    quorum: 1
    timeout: 1h
    decision_window: 30m
    on_timeout: escalate
`, 1)
	if err := LoadBytes([]byte(yaml)); err == nil {
		t.Fatal("expected V9 rejection (escalate requires non-empty chain); got nil")
	}
}

func TestLoad_DefaultsApply(t *testing.T) {
	// Strip optional safety fields entirely; defaults from schema must
	// apply (destructive_ops_deny defaults to [], agent_creds_scope to
	// "dev_only", iteration_cap to 50, etc.).
	yaml := `
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
safety: {}
`
	if err := LoadBytes([]byte(yaml)); err != nil {
		t.Fatalf("expected nil error with safety: {} (defaults must apply); got %v", err)
	}
}

// AuthzConfig surfaces the operator-supplied policy_dir for serve wiring.
func TestLoad_Authz_PolicyDirSurfacedOnConfig(t *testing.T) {
	yaml := strings.Replace(minimalValid, `safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`, `safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
  authz:
    policy_dir: /etc/regatta/policies
`, 1)
	cfg, err := LoadConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	az := cfg.AuthzConfig()
	if az == nil {
		t.Fatalf("AuthzConfig()=nil; want non-nil after operator declared safety.authz.policy_dir")
	}
	if az.PolicyDir != "/etc/regatta/policies" {
		t.Fatalf("policy_dir=%q; want /etc/regatta/policies", az.PolicyDir)
	}
}

// Absent safety.authz block returns a nil AuthzConfig — serve.go skips Reloader.
func TestLoad_Authz_AbsentSurfacesNil(t *testing.T) {
	cfg, err := LoadConfig([]byte(minimalValid))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AuthzConfig() != nil {
		t.Fatalf("AuthzConfig()=%+v; want nil when safety.authz is omitted", cfg.AuthzConfig())
	}
}

// Operator opt-out toggles surface as *bool=false; CUE default is true.
func TestLoad_Authz_ReloadToggles(t *testing.T) {
	yaml := strings.Replace(minimalValid, `safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`, `safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
  authz:
    policy_dir: /etc/regatta/policies
    reload_sighup: false
    reload_fsnotify: false
    reload_debounce: 500ms
`, 1)
	cfg, err := LoadConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	az := cfg.AuthzConfig()
	if az == nil {
		t.Fatalf("AuthzConfig()=nil")
	}
	if az.ReloadSighup == nil || *az.ReloadSighup {
		t.Fatalf("ReloadSighup=%v; want *false", az.ReloadSighup)
	}
	if az.ReloadFsnotify == nil || *az.ReloadFsnotify {
		t.Fatalf("ReloadFsnotify=%v; want *false", az.ReloadFsnotify)
	}
	if az.ReloadDebounce != "500ms" {
		t.Fatalf("ReloadDebounce=%q; want 500ms", az.ReloadDebounce)
	}
}
