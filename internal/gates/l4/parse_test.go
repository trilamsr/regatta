package l4

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Parse covers all 7 fixtures end-to-end (spec §3.5 truth table).
func TestParse_Fixtures(t *testing.T) {
	cases := []struct {
		name      string
		fixture   string
		wantErr   bool
		wantCount int
		wantSevs  []schemas.FindingSeverity
	}{
		{name: "pass", fixture: "pass.json", wantCount: 0},
		{name: "fail_critical", fixture: "fail_critical.json", wantCount: 1, wantSevs: []schemas.FindingSeverity{schemas.FindingCritical}},
		{name: "fail_two_high", fixture: "fail_two_high.json", wantCount: 2, wantSevs: []schemas.FindingSeverity{schemas.FindingHigh, schemas.FindingHigh}},
		{name: "pass_one_high", fixture: "pass_one_high.json", wantCount: 1, wantSevs: []schemas.FindingSeverity{schemas.FindingHigh}},
		{name: "oversize_diff", fixture: "oversize_diff.json", wantCount: 1, wantSevs: []schemas.FindingSeverity{schemas.FindingMedium}},
		{name: "refusal", fixture: "refusal.json", wantErr: true},
		{name: "malformed_json", fixture: "malformed_json.json", wantCount: 1, wantSevs: []schemas.FindingSeverity{schemas.FindingCritical}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", tc.fixture))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			env, err := ParseEnvelope(raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want err, got nil; env=%+v", env)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if len(env.Findings) != tc.wantCount {
				t.Fatalf("findings count: got %d, want %d", len(env.Findings), tc.wantCount)
			}
			for i, want := range tc.wantSevs {
				if env.Findings[i].Severity != want {
					t.Fatalf("[%d] severity: got %s, want %s", i, env.Findings[i].Severity, want)
				}
			}
		})
	}
}

// Tolerant parser strips a fenced ```json ... ``` envelope.
func TestParse_StripsFencedJSON(t *testing.T) {
	raw := []byte("Sure thing:\n```json\n{\"verdict\":\"pass\",\"findings\":[]}\n```\nDone.")
	env, err := ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if env.Verdict != "pass" {
		t.Fatalf("verdict: got %q, want pass", env.Verdict)
	}
}

// Empty / prose-only input fails loud (gate routes to advisory).
func TestParse_NoJSONReturnsError(t *testing.T) {
	if _, err := ParseEnvelope([]byte("I cannot help with that.")); err == nil {
		t.Fatalf("want err on prose-only input")
	}
	if _, err := ParseEnvelope([]byte("")); err == nil {
		t.Fatalf("want err on empty input")
	}
}

// Unknown severity tokens normalize to medium (no silent hide).
func TestParse_UnknownSeverityNormalizesToMedium(t *testing.T) {
	raw := []byte(`{"verdict":"fail","findings":[{"id":"L4-X","severity":"BLOCKER","claim":"x"}]}`)
	env, err := ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if env.Findings[0].Severity != schemas.FindingMedium {
		t.Fatalf("unknown severity should normalize to medium, got %s", env.Findings[0].Severity)
	}
}
