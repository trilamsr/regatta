package program

import (
	"encoding/json"
	"errors"
	"sort"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

func TestIsV2Brief_ReturnsTrueOnV2(t *testing.T) {
	raw := []byte(`{"schema_version": 2, "program_id": "m-000000000000"}`)
	if !IsV2Brief(raw) {
		t.Fatalf("IsV2Brief returned false for schema_version=2 brief")
	}
}

func TestIsV2Brief_ReturnsFalseOnV1(t *testing.T) {
	raw := []byte(`{"program_id": "m-000000000000", "schema_version": 1}`)
	if IsV2Brief(raw) {
		t.Fatalf("IsV2Brief returned true for schema_version=1 brief")
	}
	// Also: no schema_version field at all.
	if IsV2Brief([]byte(`{"program_id": "m-x"}`)) {
		t.Fatalf("IsV2Brief returned true for brief without schema_version")
	}
}

func TestIsV2Brief_RejectsMalformedJSON(t *testing.T) {
	cases := [][]byte{
		[]byte(`not json at all`),
		[]byte(`{"schema_version": `),
		[]byte(`{"schema_version": "two"}`),
		nil,
		{},
	}
	for i, raw := range cases {
		// Must not panic; must return false.
		if IsV2Brief(raw) {
			t.Fatalf("case %d: IsV2Brief returned true on malformed input %q", i, raw)
		}
	}
}

func TestLowerV1ToV2_PreservesDeps(t *testing.T) {
	v1 := &ProgramBrief{
		SchemaVersion: 1,
		ProgramID:     "m-000000000001",
		Features: []PlannedFeature{
			{ID: "F-1", Title: "first", Fulfills: []string{"c1"}, DependsOnFeatures: []string{"F-2"}},
			{ID: "F-2", Title: "second", Fulfills: []string{"c2"}},
		},
	}
	v2 := LowerV1ToV2(v1)
	if v2 == nil {
		t.Fatal("LowerV1ToV2 returned nil")
	}
	if v2.SchemaVersion != 2 {
		t.Fatalf("SchemaVersion = %d, want 2", v2.SchemaVersion)
	}
	if len(v2.FeaturesV2) != 2 {
		t.Fatalf("FeaturesV2 len = %d, want 2", len(v2.FeaturesV2))
	}

	var edges []Edge
	for _, f := range v2.FeaturesV2 {
		edges = append(edges, f.Edges...)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want exactly 1 (F-2 -> F-1)", len(edges))
	}
	e := edges[0]
	if e.From != "F-2" || e.To != "F-1" {
		t.Fatalf("edge = %s -> %s, want F-2 -> F-1", e.From, e.To)
	}
	if e.Predicate != "" {
		t.Fatalf("predicate = %q, want empty (unconditional)", e.Predicate)
	}
}

func TestLowerV1ToV2_RoundTripBehaviorEquivalent(t *testing.T) {
	v1 := &ProgramBrief{
		SchemaVersion: 1,
		ProgramID:     "m-000000000002",
		Features: []PlannedFeature{
			{ID: "F-A", Title: "alpha", Fulfills: []string{"c1"}},
			{ID: "F-B", Title: "bravo", Fulfills: []string{"c2"}, DependsOnFeatures: []string{"F-A"}},
			{ID: "F-C", Title: "charlie", Fulfills: []string{"c3"}, DependsOnFeatures: []string{"F-A", "F-B"}},
		},
	}
	v2 := LowerV1ToV2(v1)
	raw, err := json.Marshal(v2)
	if err != nil {
		t.Fatalf("marshal v2: %v", err)
	}
	var parsed ProgramBriefV2
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal v2: %v", err)
	}

	gotFeatures := map[string]bool{}
	for _, f := range parsed.FeaturesV2 {
		gotFeatures[f.ID] = true
	}
	for _, f := range v1.Features {
		if !gotFeatures[f.ID] {
			t.Fatalf("feature %s lost in v1->v2 round-trip", f.ID)
		}
	}

	// Build dependency closure from v1.
	v1Deps := map[string][]string{}
	for _, f := range v1.Features {
		deps := append([]string(nil), f.DependsOnFeatures...)
		sort.Strings(deps)
		v1Deps[f.ID] = deps
	}
	// Build dependency closure from v2 edges: target's deps = sorted From IDs of edges where To=target.
	v2Deps := map[string][]string{}
	for _, f := range parsed.FeaturesV2 {
		v2Deps[f.ID] = nil
	}
	for _, f := range parsed.FeaturesV2 {
		for _, e := range f.Edges {
			v2Deps[e.To] = append(v2Deps[e.To], e.From)
		}
	}
	for k := range v2Deps {
		sort.Strings(v2Deps[k])
	}

	for id, want := range v1Deps {
		got := v2Deps[id]
		if len(want) != len(got) {
			t.Fatalf("feature %s: v1 deps %v vs v2 inbound %v", id, want, got)
		}
		for i := range want {
			if want[i] != got[i] {
				t.Fatalf("feature %s: v1 deps %v vs v2 inbound %v", id, want, got)
			}
		}
	}
}

func TestEdge_Validate(t *testing.T) {
	selfLoop := Edge{From: "F-X", To: "F-X"}
	if err := selfLoop.Validate(); err == nil {
		t.Fatal("self-loop Edge passed Validate; want error")
	} else if !errors.Is(err, orchestrator.ErrEdgeUnknownTarget) {
		t.Fatalf("self-loop error = %v, want wraps ErrEdgeUnknownTarget", err)
	}

	missingTo := Edge{From: "F-X", To: ""}
	if err := missingTo.Validate(); err == nil {
		t.Fatal("Edge with empty To passed Validate; want error")
	}

	missingFrom := Edge{From: "", To: "F-Y"}
	if err := missingFrom.Validate(); err == nil {
		t.Fatal("Edge with empty From passed Validate; want error")
	}

	ok := Edge{From: "F-X", To: "F-Y"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid edge rejected: %v", err)
	}
}

func TestSkipMode_String(t *testing.T) {
	cases := []struct {
		mode SkipMode
		want string
	}{
		{SkipCascade, "cascade"},
		{SkipIgnore, "ignore"},
		{SkipDefault, "default"},
	}
	for _, c := range cases {
		if string(c.mode) != c.want {
			t.Fatalf("SkipMode(%q) string = %q, want %q", c.mode, string(c.mode), c.want)
		}
	}
}
