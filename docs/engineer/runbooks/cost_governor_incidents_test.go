// Package runbooks hosts gate tests for engineer-facing on-call
// runbooks under docs/engineer/runbooks/. Tests-only package — there
// is no production Go code here; the tests assert that the markdown
// files satisfy contracts that would otherwise be silent doc-drift
// bugs.
//
// Coverage (cost-governor-incidents): every load-bearing
// obs.EventCost* slog event symbol from internal/obs/events.go must
// appear in the runbook, every spec §9 R-tier section that has
// operator-runbook content (R3, R4, R6, R10, R13, R15) must be cited
// by R-number, and every relative .md link must resolve.
package runbooks

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const costIncidentsDoc = "cost-governor-incidents.md"

// costIncidentsEventNames is the load-bearing obs.EventCost* symbol
// set from internal/obs/events.go that the runbook must cite. Adding
// a new EventCost* constant requires extending the runbook AND this
// list — the test fails loudly when either side drifts.
var costIncidentsEventNames = []string{
	"EventCostReconcileFailing",
	"EventCostReconcileSkipped",
	"EventCostReconcileFallback",
	"EventCostDriftAlert",
	"EventCostSoftCapBreached",
}

// costIncidentsRiskSections is the set of spec §9 R-tier sections
// that carry operator-runbook content per the spec's "Operator
// runbook documents..." hints. Each section must be cited by
// R-number.
var costIncidentsRiskSections = []string{
	"R3",
	"R4",
	"R6",
	"R10",
	"R13",
	"R15",
}

// costIncidentsLinkAllowlist skips link-validity for forward refs to
// sibling Wave-3 docs that ship in parallel. Drop entries as the
// sibling PRs merge. Tracker: issue #278.
var costIncidentsLinkAllowlist = map[string]bool{
	"../../operator/cost-governor.md": true,
}

// TestCostGovernorIncidentsRunbook_LinksValid pins spec §6 T6 — every relative .md link resolves on disk.
func TestCostGovernorIncidentsRunbook_LinksValid(t *testing.T) {
	docPath := docFullPath(t, costIncidentsDoc)
	body := readDoc(t, costIncidentsDoc)
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
		if costIncidentsLinkAllowlist[raw] || costIncidentsLinkAllowlist[target] {
			continue
		}
		resolved := filepath.Join(docDir, target)
		if _, err := os.Stat(resolved); err != nil {
			t.Errorf("dead link in %s: %q (resolved %s): %v", costIncidentsDoc, target, resolved, err)
		}
	}
}

// TestCostGovernorIncidentsRunbook_CitesAllEventNames pins spec §6 T6 — every load-bearing EventCost* symbol cited.
func TestCostGovernorIncidentsRunbook_CitesAllEventNames(t *testing.T) {
	body := readDoc(t, costIncidentsDoc)
	for _, name := range costIncidentsEventNames {
		if !strings.Contains(body, name) {
			t.Errorf("cost-governor-incidents.md missing event symbol %q (load-bearing for on-call triage)", name)
		}
	}
}

// TestCostGovernorIncidentsRunbook_CitesAllRiskSections pins spec §6 T6 — every spec §9 R-tier runbook section cited.
func TestCostGovernorIncidentsRunbook_CitesAllRiskSections(t *testing.T) {
	body := readDoc(t, costIncidentsDoc)
	for _, r := range costIncidentsRiskSections {
		if !strings.Contains(body, r) {
			t.Errorf("cost-governor-incidents.md missing spec §9 %s cite (operator-runbook content)", r)
		}
	}
}

func docFullPath(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Join(wd, name)
}

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(docFullPath(t, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
