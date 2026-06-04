package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// TestReconciler_SchemaPin_FieldSetMatchesDeclared asserts CostBucket+UsageBucket json tags equal expectedFields (#277).
func TestReconciler_SchemaPin_FieldSetMatchesDeclared(t *testing.T) {
	cases := []struct {
		name   string
		typ    reflect.Type
		expect []string
	}{
		{
			name:   "CostBucket",
			typ:    reflect.TypeOf(CostBucket{}),
			expect: expectedCostBucketFields,
		},
		{
			name:   "UsageBucket",
			typ:    reflect.TypeOf(UsageBucket{}),
			expect: expectedUsageBucketFields,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := jsonTagsOf(tc.typ)
			want := append([]string(nil), tc.expect...)
			sort.Strings(got)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decoder field set drifted: got=%v want=%v", got, want)
			}
		})
	}
}

// TestReconciler_SchemaPin_FixtureMatchesSchemaPin asserts testdata fixtures carry every pinned field (#277).
func TestReconciler_SchemaPin_FixtureMatchesSchemaPin(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		expect  []string
	}{
		{
			name:    "cost",
			fixture: "anthropic_cost_2026_06_01_01h.json",
			expect:  expectedCostBucketFields,
		},
		{
			name:    "usage",
			fixture: "anthropic_usage_2026_06_01_01h.json",
			expect:  expectedUsageBucketFields,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata", tc.fixture)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var wrap struct {
				Data []map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(raw, &wrap); err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			if len(wrap.Data) == 0 {
				t.Fatalf("fixture %s carries no rows", tc.fixture)
			}
			row := wrap.Data[0]
			for _, field := range tc.expect {
				if _, ok := row[field]; !ok {
					t.Errorf("fixture %s missing pinned field %q", tc.fixture, field)
				}
			}
		})
	}
}

func jsonTagsOf(t reflect.Type) []string {
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		for j, r := range tag {
			if r == ',' {
				tag = tag[:j]
				break
			}
		}
		out = append(out, tag)
	}
	return out
}
