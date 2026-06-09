package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
			ad, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))
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

// TestBuildSpecAdapter_LogsConfiguredType pins the #867 contract — every boot emits one INFO record naming the wired adapter so operators can confirm regatta.yaml took effect without log archaeology.
func TestBuildSpecAdapter_LogsConfiguredType(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		wantType string
	}{
		{
			name: "github_issues_logs_type",
			yaml: `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:autonomous"
` + wireSpecAdapterValidGateBlock,
			wantType: "github_issues",
		},
		{
			name:     "absent_yaml_logs_markdown_catalog_default",
			yaml:     "",
			wantType: "markdown_catalog",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			if tc.yaml != "" {
				if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte(tc.yaml), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			buf := &bytes.Buffer{}
			logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			if _, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp}, logger); err != nil {
				t.Fatalf("buildSpecAdapter: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, "msg=adapter.configured") {
				t.Fatalf("log missing adapter.configured record:\n%s", out)
			}
			if !strings.Contains(out, "type="+tc.wantType) {
				t.Errorf("log missing type=%s:\n%s", tc.wantType, out)
			}
		})
	}
}

// TestBuildSpecAdapter_SurfacesYAMLLoadError pins the #867 contract — a malformed regatta.yaml MUST surface a WARN record naming the failure instead of silently falling back to markdown_catalog (the silent-swallow behaviour that hid #867 from operator inspection).
func TestBuildSpecAdapter_SurfacesYAMLLoadError(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte("not: valid: yaml: structure\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if _, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp}, logger); err != nil {
		t.Fatalf("buildSpecAdapter: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "msg=adapter.config_load_failed") {
		t.Fatalf("log missing adapter.config_load_failed warn record (silent-swallow regression #867):\n%s", out)
	}
}

// TestBuildSpecAdapter_MissingYAMLStaysSilent pins the negative half of the #867 diagnostic contract — a zero-config deployment (no regatta.yaml on disk) MUST NOT emit adapter.config_load_failed because file-not-present is the documented happy path, not a misconfiguration.
func TestBuildSpecAdapter_MissingYAMLStaysSilent(t *testing.T) {
	tmp := t.TempDir()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if _, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp}, logger); err != nil {
		t.Fatalf("buildSpecAdapter: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "adapter.config_load_failed") {
		t.Fatalf("zero-config deployment emitted spurious config_load_failed warn:\n%s", out)
	}
}

// TestBuildSpecAdapter_DefaultLaneYAMLAccepted pins #1117: regatta.yaml `spec_adapter.default_lane` is accepted by buildSpecAdapter and surfaces as a non-nil adapter — the wire path from validate.Load through SpecAdapter.DefaultLane into the github_issues config is exercised end-to-end. The empty-lane skip behaviour is covered by the parse-layer test `TestParseIssueBody_DefaultLaneAppliedWhenNoLabel`; this test gates the wire seam.
func TestBuildSpecAdapter_DefaultLaneYAMLAccepted(t *testing.T) {
	yaml := `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:autonomous"
  default_lane: server
` + wireSpecAdapterValidGateBlock
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "regatta.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	ad, err := buildSpecAdapter(serveFlags{RepoRoot: tmp, ItemsRoot: tmp}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))
	if err != nil {
		t.Fatalf("buildSpecAdapter with default_lane: %v", err)
	}
	if ad == nil {
		t.Fatal("ad nil; want non-nil adapter when default_lane is set")
	}
}
