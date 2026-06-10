package scheduler

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// scopeExtractorByLane returns a FileScopeExtractor that maps each work-item
// ID to a pre-seeded path slice. Lets tests express "WI-1 touches A, WI-2
// touches A+B" without hand-rolling acceptance JSON bodies.
func scopeExtractorByID(scopes map[string][]string) func(state.WorkItem) []string {
	return func(w state.WorkItem) []string { return scopes[w.ID] }
}

// TestReserveFromSpawnable_RefusesFileScopeOverlap asserts that a same-lane candidate sharing any predicted file with an in-flight agent defers (#1065).
func TestReserveFromSpawnable_RefusesFileScopeOverlap(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-A", "server")
	seedPlanned(t, db, "WI-B", "server")
	scopes := map[string][]string{
		"WI-A": {"internal/orchestrator/spawner/claude.go"},
		"WI-B": {"internal/orchestrator/spawner/claude.go"},
	}
	sch := New(db, Config{
		LockTTL:            time.Minute,
		FileScopeExtractor: scopeExtractorByID(scopes),
	})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved=%d ids=%v want 1 (only first WI; sibling MUST defer on shared file)", len(ids), ids)
	}
	pending, err := db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending=%d want 1 (sibling parked at pending awaiting next cycle)", len(pending))
	}
}

// TestReserveFromSpawnable_AllowsDisjointFileScope asserts disjoint predicted scopes spawn in parallel (#1065).
func TestReserveFromSpawnable_AllowsDisjointFileScope(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-A", "server")
	seedPlanned(t, db, "WI-B", "server")
	scopes := map[string][]string{
		"WI-A": {"internal/orchestrator/spawner/claude.go"},
		"WI-B": {"internal/orchestrator/scheduler/scheduler.go"},
	}
	sch := New(db, Config{
		LockTTL:            time.Minute,
		FileScopeExtractor: scopeExtractorByID(scopes),
	})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("reserved=%d want 2 (disjoint scopes MUST spawn in parallel)", len(ids))
	}
}

// TestFileScopeOverlap_TableDriven covers no-scope (allow), single-file overlap, prefix-dir overlap, shared package (#1065).
func TestFileScopeOverlap_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		active   []string
		incoming []string
		want     bool
	}{
		{"empty_active_allows", nil, []string{"a.go"}, false},
		{"empty_incoming_allows", []string{"a.go"}, nil, false},
		{"single_file_collides", []string{"a.go"}, []string{"a.go"}, true},
		{"disjoint_allows", []string{"a.go"}, []string{"b.go"}, false},
		{"shared_package_collides", []string{"internal/orchestrator/spawner/claude.go"}, []string{"internal/orchestrator/spawner/claude.go"}, true},
		{"prefix_dir_collides", []string{"internal/orchestrator/spawner/"}, []string{"internal/orchestrator/spawner/claude.go"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fileScopeCollides(tc.active, tc.incoming)
			if got != tc.want {
				t.Fatalf("fileScopeCollides(%v,%v)=%v want %v", tc.active, tc.incoming, got, tc.want)
			}
		})
	}
}

// TestDefaultFileScopeExtractor parses backtick-quoted source paths + edit/add/modify forms from acceptance criteria (#1065).
func TestDefaultFileScopeExtractor(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"empty", "", nil},
		{"backtick_path", "## Acceptance criteria\n- [planned] c1: edit `internal/orchestrator/spawner/claude.go` to do X", []string{"internal/orchestrator/spawner/claude.go"}},
		{"verb_path", "## Acceptance criteria\n- [planned] c1: edit internal/orchestrator/scheduler/scheduler.go", []string{"internal/orchestrator/scheduler/scheduler.go"}},
		{"multi_paths_dedupe", "## Acceptance criteria\n- [planned] c1: edit `internal/a.go` and `internal/a.go`\n- [planned] c2: add `internal/b.go`", []string{"internal/a.go", "internal/b.go"}},
		{"ignores_non_source_prefix", "## Acceptance criteria\n- [planned] c1: edit `README.md`", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wi := state.WorkItem{AcceptanceJSON: jsonBody(tc.body)}
			got := DefaultFileScopeExtractor(wi)
			sort.Strings(got)
			sort.Strings(tc.want)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DefaultFileScopeExtractor body=%q got=%v want=%v", tc.body, got, tc.want)
			}
		})
	}
}

// jsonBody wraps a body string into the `{"body": "..."}` envelope the github_issues adapter writes to AcceptanceJSON until #1092 lands a dedicated column.
func jsonBody(b string) string {
	if b == "" {
		return ""
	}
	// minimal JSON escape: backslash + quote only — tests stay readable
	esc := make([]byte, 0, len(b)+8)
	for _, r := range []byte(b) {
		switch r {
		case '\\', '"':
			esc = append(esc, '\\', r)
		case '\n':
			esc = append(esc, '\\', 'n')
		default:
			esc = append(esc, r)
		}
	}
	return `{"body":"` + string(esc) + `"}`
}
