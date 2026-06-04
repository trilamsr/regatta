package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

const wireSpecAdapterValidGateBlock = `
ci:
  command: "go test ./..."
gates:
  - id: human_merge
    type: approval_gate
    name: human-merge
    risk_class: low
    reviewers: [trilamsr]
    quorum: 1
    timeout: 24h
    decision_window: 12h
    on_timeout: fail
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`

// TestBuildSpecAdapter_DispatchesByYAMLType asserts factory wires by `regatta.yaml::spec_adapter.type` (MVR-1-T4).
func TestBuildSpecAdapter_DispatchesByYAMLType(t *testing.T) {
	mdYAML := `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: markdown_catalog
  root: .
` + wireSpecAdapterValidGateBlock
	ghYAML := `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:autonomous"
` + wireSpecAdapterValidGateBlock
	cases := []struct {
		name string
		yaml string
		// wantStatusClosed differentiates the two adapters: markdown_catalog
		// supports StatusClosedResolved, github_issues does not.
		wantStatusClosed bool
	}{
		{name: "markdown_catalog_explicit", yaml: mdYAML, wantStatusClosed: true},
		{name: "no_yaml_falls_back_to_markdown_catalog", yaml: "", wantStatusClosed: true},
		{name: "github_issues_wires_gh_adapter", yaml: ghYAML, wantStatusClosed: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			if tc.yaml != "" {
				if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte(tc.yaml), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			ad, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp})
			if err != nil {
				t.Fatalf("buildSpecAdapter: %v", err)
			}
			if ad == nil {
				t.Fatal("ad nil; want non-nil adapter")
			}
			got := false
			for _, s := range ad.Capabilities().SupportedStatuses {
				if s == schemas.StatusClosedResolved {
					got = true
					break
				}
			}
			if got != tc.wantStatusClosed {
				t.Errorf("StatusClosedResolved supported=%v; want %v", got, tc.wantStatusClosed)
			}
		})
	}
}
