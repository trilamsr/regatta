// Package operator hosts gate tests for the operator-facing
// documentation under docs/operator/. Tests-only package — there is
// no production Go code here; the tests assert that the markdown
// files satisfy contracts that would otherwise be silent doc-drift
// bugs.
//
// Coverage (cost-governor-dashboards): every regatta.cost.* span
// attribute from spec §3.7 must appear in the doc, every
// obs.EventCost* slog event symbol from internal/obs/events.go must
// appear, and every relative .md link must resolve. The
// linkAllowlist mechanism handles forward refs to sibling Wave-3
// docs that ship in parallel.
package operator

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const costDashboardsDoc = "cost-governor-dashboards.md"

// costDashboardsSpanAttrs is the regatta.cost.* attribute set from
// spec §3.7 + §3.4. Adding a new attr to the gate or reconciler
// requires extending the doc AND this list — the test fails loudly
// when either side drifts.
var costDashboardsSpanAttrs = []string{
	"regatta.cost.usd_estimate",
	"regatta.cost.cap_dag_usd",
	"regatta.cost.cap_op_usd",
	"regatta.cost.allow",
	"regatta.cost.soft_breached",
	"regatta.cost.period_start",
	"regatta.cost.period_end",
	"regatta.cost.drift_pct",
	"regatta.cost.api_source",
}

// costDashboardsSlogEvents is the load-bearing obs.EventCost* symbol
// set from internal/obs/events.go that the dashboards doc must cite.
var costDashboardsSlogEvents = []string{
	"EventCostReconcileFailing",
	"EventCostReconcileSkipped",
	"EventCostReconcileFallback",
	"EventCostDriftAlert",
	"EventCostSoftCapBreached",
}

// costDashboardsLinkAllowlist skips link-validity for forward refs
// to sibling Wave-3 docs that ship in parallel. Drop entries as the
// sibling PRs merge. Tracker: issue #278.
var costDashboardsLinkAllowlist = map[string]bool{
	"./cost-governor.md": true,
	"../engineer/runbooks/cost-governor-incidents.md": true,
}

// TestCostGovernorDashboardsDoc_LinksValid pins spec §6 T7 — every relative .md link resolves on disk.
func TestCostGovernorDashboardsDoc_LinksValid(t *testing.T) {
	docPath := dashboardsDocFullPath(t, costDashboardsDoc)
	body := readDashboardsDoc(t, costDashboardsDoc)
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
		if costDashboardsLinkAllowlist[raw] || costDashboardsLinkAllowlist[target] {
			continue
		}
		resolved := filepath.Join(docDir, target)
		if _, err := os.Stat(resolved); err != nil {
			t.Errorf("dead link in %s: %q (resolved %s): %v", costDashboardsDoc, target, resolved, err)
		}
	}
}

// TestCostGovernorDashboardsDoc_CitesAllCostSpanAttrs pins spec §6 T7 — every regatta.cost.* attr appears.
func TestCostGovernorDashboardsDoc_CitesAllCostSpanAttrs(t *testing.T) {
	body := readDashboardsDoc(t, costDashboardsDoc)
	for _, attr := range costDashboardsSpanAttrs {
		if !strings.Contains(body, attr) {
			t.Errorf("cost-governor-dashboards.md missing span attribute %q (spec §3.7)", attr)
		}
	}
}

// TestCostGovernorDashboardsDoc_CitesAllSlogEvents pins spec §6 T7 — every obs.EventCost* symbol appears.
func TestCostGovernorDashboardsDoc_CitesAllSlogEvents(t *testing.T) {
	body := readDashboardsDoc(t, costDashboardsDoc)
	for _, ev := range costDashboardsSlogEvents {
		if !strings.Contains(body, ev) {
			t.Errorf("cost-governor-dashboards.md missing slog event %q (operator alerting surface)", ev)
		}
	}
}

func dashboardsDocFullPath(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Join(wd, name)
}

func readDashboardsDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(dashboardsDocFullPath(t, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
