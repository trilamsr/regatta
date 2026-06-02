// Package operator hosts gate tests for the operator-facing
// documentation under docs/operator/. Tests-only package — there is no
// production Go code here; the tests assert that the markdown files
// satisfy contracts that would otherwise be silent doc-drift bugs.
//
// Coverage (cost-governor): cost-governor.md must document every
// safety.cost CUE schema field, carry the four load-bearing headings
// (Precedence, Pricing refresh, OTel cardinality, plus the
// most-restrictive-wins precedence rule), and every relative .md link
// must resolve. Forward-refs to sibling Wave-3 docs that ship in
// parallel use the costGovernorLinkAllowlist mechanism — once the
// sibling PR merges, the entry drops from the allowlist.
package operator

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const costGovernorDoc = "cost-governor.md"

// costGovernorConfigFields is the safety.cost schema from spec
// docs/engineer/specs/2026-06-01-cost-governor-design.md §3.6. Every
// entry must appear verbatim in cost-governor.md or operators reading
// the doc will not know the knob exists.
var costGovernorConfigFields = []string{
	"per_dag_usd",
	"per_operator_usd",
	"per_work_item_usd",
	"period",
	"soft_pct",
	"reconcile_interval",
	"drift_alert_threshold_pct",
	"usage_api_key_env",
	"estimation_strategy",
	"pricing_override_path",
}

// costGovernorLinkAllowlist skips link-validity for forward refs to
// sibling Wave-3 docs that ship in parallel. Drop entries as the
// sibling PRs merge. Tracker: issue #278.
var costGovernorLinkAllowlist = map[string]bool{
	"../engineer/runbooks/cost-governor-incidents.md": true,
	"./cost-governor-dashboards.md":                   true,
}

// TestCostGovernorDoc_LinksValid pins spec §6 T5 — every relative .md link in cost-governor.md resolves on disk.
func TestCostGovernorDoc_LinksValid(t *testing.T) {
	checkDocLinks(t, costGovernorDoc, costGovernorLinkAllowlist)
}

// TestCostGovernorDoc_DocumentsAllConfigFields pins spec §6 T5 — every #CostGovernor field name appears.
func TestCostGovernorDoc_DocumentsAllConfigFields(t *testing.T) {
	body := readDoc(t, costGovernorDoc)
	for _, name := range costGovernorConfigFields {
		if !strings.Contains(body, name) {
			t.Errorf("cost-governor.md is missing config field %q (spec §3.6 requires it)", name)
		}
	}
}

// TestCostGovernorDoc_PricingRefreshRunbookExists pins spec §6 T5 A2 — quarterly cadence + Anthropic pricing URL cited.
func TestCostGovernorDoc_PricingRefreshRunbookExists(t *testing.T) {
	body := readDoc(t, costGovernorDoc)
	if !strings.Contains(body, "## Pricing refresh") {
		t.Errorf("cost-governor.md missing H2 'Pricing refresh' (spec §3.8)")
	}
	if !strings.Contains(strings.ToLower(body), "quarterly") {
		t.Errorf("cost-governor.md 'Pricing refresh' missing cadence word 'quarterly' (spec §3.8)")
	}
	if !strings.Contains(body, "anthropic.com") {
		t.Errorf("cost-governor.md 'Pricing refresh' missing Anthropic pricing-page URL cite (spec §3.8)")
	}
}

// TestCostGovernorDoc_OTelCardinalityGuidanceExists pins spec §6 T5 A5 — sampler env var + W6 cite present.
func TestCostGovernorDoc_OTelCardinalityGuidanceExists(t *testing.T) {
	body := readDoc(t, costGovernorDoc)
	if !strings.Contains(body, "## OTel cardinality") {
		t.Errorf("cost-governor.md missing H2 'OTel cardinality' (spec §9 R14)")
	}
	if !strings.Contains(body, "OTEL_TRACES_SAMPLER") {
		t.Errorf("cost-governor.md 'OTel cardinality' missing OTEL_TRACES_SAMPLER cite (W6 spec §9 R6)")
	}
	if !strings.Contains(body, "observability.md") {
		t.Errorf("cost-governor.md 'OTel cardinality' missing observability.md cite (W6 spec §9 R6)")
	}
}

// TestCostGovernorDoc_PrecedenceRuleIsMostRestrictiveWins pins spec §6 T5 — "most-restrictive-wins" rule present.
func TestCostGovernorDoc_PrecedenceRuleIsMostRestrictiveWins(t *testing.T) {
	body := strings.ToLower(readDoc(t, costGovernorDoc))
	if !strings.Contains(body, "most-restrictive-wins") {
		t.Errorf("cost-governor.md missing 'most-restrictive-wins' precedence rule (spec §3.6 line 370)")
	}
}

// TestCostGovernorDoc_DocumentsSoftCapPostureFields pins issue #226 — paired posture fields are cited verbatim.
func TestCostGovernorDoc_DocumentsSoftCapPostureFields(t *testing.T) {
	body := readDoc(t, costGovernorDoc)
	for _, name := range []string{"soft_cap_mode", "soft_cap_acknowledge_overrun"} {
		if !strings.Contains(body, name) {
			t.Errorf("cost-governor.md missing posture field %q (issue #226)", name)
		}
	}
}

// checkDocLinks runs the same relative-.md-link resolver as the
// inline observability link test, with a per-doc allowlist for
// in-flight forward refs.
func checkDocLinks(t *testing.T, docName string, allowlist map[string]bool) {
	t.Helper()
	docPath := docFullPath(t, docName)
	body := readDoc(t, docName)
	docDir := filepath.Dir(docPath)
	re := regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		target := strings.TrimSpace(m[1])
		if target == "" {
			continue
		}
		if u, err := url.Parse(target); err == nil && u.Scheme != "" {
			continue
		}
		if strings.HasPrefix(target, "#") {
			continue
		}
		raw := target
		if i := strings.Index(target, "#"); i >= 0 {
			target = target[:i]
		}
		if !strings.HasSuffix(target, ".md") {
			continue
		}
		if allowlist[raw] || allowlist[target] {
			continue
		}
		resolved := filepath.Join(docDir, target)
		if _, err := os.Stat(resolved); err != nil {
			t.Errorf("dead link in %s: %q (resolved %s): %v", docName, target, resolved, err)
		}
	}
}
