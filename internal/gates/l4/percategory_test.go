package l4

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/gates/severity"
)

// CategoryModels overrides emit one Invoker call per distinct resolved model.
func TestL4_PerCategory_BucketedFanOut(t *testing.T) {
	calls := []string{}
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         "claude-sonnet-4-6",
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		CategoryModels: map[string]string{
			"security": "claude-opus-4-7",
			"refactor": "claude-haiku-4-5",
		},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			calls = append(calls, req.Model)
			return InvokeResponse{}, nil
		},
	}
	_, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-pc-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sort.Strings(calls)
	want := []string{"claude-haiku-4-5", "claude-opus-4-7", "claude-sonnet-4-6"}
	if len(calls) != len(want) {
		t.Fatalf("call count: got %d (%v), want %d (%v)", len(calls), calls, len(want), want)
	}
	for i, m := range want {
		if calls[i] != m {
			t.Fatalf("bucket %d: got %q, want %q (all=%v)", i, calls[i], m, calls)
		}
	}
}

// Each bucket Invoker call receives its assigned categories on the request.
func TestL4_PerCategory_RequestCarriesCategories(t *testing.T) {
	got := map[string][]string{}
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         "claude-sonnet-4-6",
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		CategoryModels: map[string]string{
			"security": "claude-opus-4-7",
		},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			cats := append([]string(nil), req.Categories...)
			sort.Strings(cats)
			got[req.Model] = cats
			return InvokeResponse{}, nil
		},
	}
	_, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-pc-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !equalStrings(got["claude-opus-4-7"], []string{"security"}) {
		t.Fatalf("opus bucket categories: got %v, want [security]", got["claude-opus-4-7"])
	}
	want := []string{"correctness", "doc-check", "refactor", "risk", "rubric-verify", "simplification", "test-coverage"}
	if !equalStrings(got["claude-sonnet-4-6"], want) {
		t.Fatalf("primary bucket categories: got %v, want %v", got["claude-sonnet-4-6"], want)
	}
}

// Empty CategoryModels emits one primary-model call covering all categories.
func TestL4_PerCategory_EmptyOverrides_OneCall(t *testing.T) {
	calls := 0
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		Invoker: func(_ context.Context, _ InvokeRequest) (InvokeResponse, error) {
			calls++
			return InvokeResponse{}, nil
		},
	}
	_, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-pc-3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("call count: got %d, want 1 (no overrides => single call)", calls)
	}
}

// Overrides equal to the primary collapse to one call (equality bucketing).
func TestL4_PerCategory_AllSameAsPrimary_OneCall(t *testing.T) {
	calls := 0
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         "claude-sonnet-4-6",
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		CategoryModels: map[string]string{
			"security": "claude-sonnet-4-6",
			"refactor": "claude-sonnet-4-6",
		},
		Invoker: func(_ context.Context, _ InvokeRequest) (InvokeResponse, error) {
			calls++
			return InvokeResponse{}, nil
		},
	}
	_, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-pc-4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("call count: got %d, want 1 (equal-model overrides collapse)", calls)
	}
}

// Findings merge across buckets; severity routing applies to the union.
func TestL4_PerCategory_FindingsMergeAcrossBuckets(t *testing.T) {
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         "claude-sonnet-4-6",
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		CategoryModels: map[string]string{
			"security": "claude-opus-4-7",
		},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			if req.Model == "claude-opus-4-7" {
				return InvokeResponse{Findings: []schemas.Finding{{
					ID: "L4-SEC-AUTHBYPASS", Severity: schemas.FindingCritical,
				}}}, nil
			}
			return InvokeResponse{Findings: []schemas.Finding{{
				ID: "L4-REFACTOR-NAMING", Severity: schemas.FindingMedium,
			}}}, nil
		},
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-pc-5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gr.Findings) != 2 {
		t.Fatalf("merged findings: got %d, want 2", len(gr.Findings))
	}
	if gr.Verdict != schemas.VerdictFail || !gr.Blocking {
		t.Fatalf("severity union: got verdict=%s blocking=%v, want fail+blocking", gr.Verdict, gr.Blocking)
	}
}

// ResolveCategoryModel precedence is yaml > env > primary fallback.
func TestL4_PerCategory_ResolveModel(t *testing.T) {
	t.Setenv("REGATTA_GATES_L4_MODEL_SECURITY", "claude-haiku-4-5")
	cases := []struct {
		name    string
		primary string
		yaml    map[string]string
		cat     string
		want    string
	}{
		{"yaml-wins-over-env", "claude-sonnet-4-6", map[string]string{"security": "claude-opus-4-7"}, "security", "claude-opus-4-7"},
		{"env-wins-when-yaml-missing", "claude-sonnet-4-6", nil, "security", "claude-haiku-4-5"},
		{"primary-fallback-when-both-empty", "claude-sonnet-4-6", nil, "refactor", "claude-sonnet-4-6"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveCategoryModel(c.primary, c.yaml, c.cat)
			if got != c.want {
				t.Fatalf("ResolveCategoryModel(%q, %v, %q): got %q, want %q",
					c.primary, c.yaml, c.cat, got, c.want)
			}
		})
	}
}

// Later-bucket error degrades to advisory but keeps earlier-bucket findings.
func TestL4_PerCategory_BucketError_AdvisoryButKeepsFindings(t *testing.T) {
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         "claude-sonnet-4-6",
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		CategoryModels: map[string]string{
			"security": "claude-opus-4-7",
		},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			// Primary bucket (sonnet, runs first) succeeds with a
			// critical; opus bucket (security only) errors. Earlier
			// finding must survive into the merged result.
			if req.Model == "claude-sonnet-4-6" {
				return InvokeResponse{Findings: []schemas.Finding{{
					ID: "L4-CORR-OFFBYONE", Severity: schemas.FindingCritical,
				}}}, nil
			}
			return InvokeResponse{}, context.DeadlineExceeded
		},
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-pc-6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gr.Verdict != schemas.VerdictAdvisory {
		t.Fatalf("verdict: got %s, want advisory", gr.Verdict)
	}
	if gr.Blocking {
		t.Fatalf("bucket-error advisory must not block")
	}
	found := false
	for _, f := range gr.Findings {
		if f.ID == "L4-CORR-OFFBYONE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("must keep findings from earlier successful buckets; got %+v", gr.Findings)
	}
}

func equalStrings(a, b []string) bool {
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}
