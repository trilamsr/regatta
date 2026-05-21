package validatespec

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

func TestValidate_ValidMinimal(t *testing.T) {
	r, err := ValidateBytes(readFixture(t, "valid_minimal.json"))
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if !r.OK {
		t.Fatalf("ok=false; failures=%+v", r.ItemResults)
	}
	if len(r.ItemResults) != 1 || r.ItemResults[0].ID != "M1-001" {
		t.Fatalf("unexpected item results: %+v", r.ItemResults)
	}
}

func TestValidate_ValidFull(t *testing.T) {
	r, err := ValidateBytes(readFixture(t, "valid_full.json"))
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if !r.OK {
		t.Fatalf("ok=false; failures=%+v", r.ItemResults)
	}
}

func TestValidate_ValidArray(t *testing.T) {
	r, err := ValidateBytes(readFixture(t, "valid_array.json"))
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if !r.OK {
		t.Fatalf("ok=false; failures=%+v", r.ItemResults)
	}
	if len(r.ItemResults) != 2 {
		t.Fatalf("want 2 items; got %d", len(r.ItemResults))
	}
	if r.ItemResults[0].ID != "M1-001" || r.ItemResults[1].ID != "M1-002" {
		t.Fatalf("item IDs mismatch: %+v", r.ItemResults)
	}
}

func TestValidate_InvalidCases(t *testing.T) {
	cases := []struct {
		fixture       string
		failureSubstr string
	}{
		{"invalid_missing_id.json", "id"},
		{"invalid_empty_criteria.json", "acceptance_criteria"},
		{"invalid_bad_status.json", "status"},
		{"invalid_bad_criterion_state.json", "state"},
		{"invalid_extra_property.json", "priority"},
		{"invalid_missing_source.json", "source"},
		{"invalid_dependencies_not_string.json", "dependencies"},
		{"invalid_source_kind_enum.json", "kind"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.fixture, func(t *testing.T) {
			r, err := ValidateBytes(readFixture(t, tc.fixture))
			if err != nil {
				t.Fatalf("ValidateBytes: %v", err)
			}
			if r.OK {
				t.Fatalf("expected ok=false; got OK with results %+v", r.ItemResults)
			}
			if len(r.ItemResults) != 1 || r.ItemResults[0].OK {
				t.Fatalf("expected one failing item; got %+v", r.ItemResults)
			}
			joined := strings.Join(r.ItemResults[0].Failures, " | ")
			if !strings.Contains(joined, tc.failureSubstr) {
				t.Errorf("failure substring %q not found in failures: %s", tc.failureSubstr, joined)
			}
		})
	}
}

func TestValidate_EmptyInput(t *testing.T) {
	if _, err := ValidateBytes(nil); err == nil {
		t.Fatal("expected error on nil input")
	}
	if _, err := ValidateBytes([]byte("   \n  ")); err == nil {
		t.Fatal("expected error on whitespace-only input")
	}
}

func TestValidate_NotObjectOrArray(t *testing.T) {
	if _, err := ValidateBytes([]byte(`"a string"`)); err == nil {
		t.Fatal("expected error on string root")
	}
	if _, err := ValidateBytes([]byte(`42`)); err == nil {
		t.Fatal("expected error on number root")
	}
}

func TestValidate_ArrayWithNonObjectElement(t *testing.T) {
	input := []byte(`[
	  {"id":"M1-001","title":"ok","acceptance_criteria":[{"id":"ac1","text":"t","state":"planned"}],"status":"planned","source":{"kind":"file","locator":"x:1-2","sha":"a"}},
	  "not-an-object"
	]`)
	r, err := ValidateBytes(input)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if r.OK {
		t.Fatal("expected ok=false")
	}
	if len(r.ItemResults) != 2 {
		t.Fatalf("want 2 results; got %d", len(r.ItemResults))
	}
	if !r.ItemResults[0].OK {
		t.Errorf("first item should be ok; got %+v", r.ItemResults[0])
	}
	if r.ItemResults[1].OK {
		t.Errorf("second item should fail; got %+v", r.ItemResults[1])
	}
}

func TestValidate_MalformedJSON(t *testing.T) {
	if _, err := ValidateBytes([]byte(`{not json`)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestValidate_StreamWrapper(t *testing.T) {
	r, err := Validate(bytes.NewReader(readFixture(t, "valid_minimal.json")))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !r.OK || len(r.ItemResults) != 1 || r.ItemResults[0].ID != "M1-001" {
		t.Fatalf("unexpected result: %+v", r)
	}
}
