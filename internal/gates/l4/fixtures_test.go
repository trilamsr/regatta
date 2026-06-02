package l4

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Fixture-driven Run table — 7 model outputs route to expected verdicts.
func TestL4_Run_FixtureTable(t *testing.T) {
	cases := []struct {
		fixture      string
		wantVerdict  schemas.Verdict
		wantBlocking bool
	}{
		{fixture: "pass.json", wantVerdict: schemas.VerdictPass, wantBlocking: false},
		{fixture: "fail_critical.json", wantVerdict: schemas.VerdictFail, wantBlocking: true},
		{fixture: "fail_two_high.json", wantVerdict: schemas.VerdictFail, wantBlocking: true},
		{fixture: "pass_one_high.json", wantVerdict: schemas.VerdictPass, wantBlocking: false},
		{fixture: "oversize_diff.json", wantVerdict: schemas.VerdictPass, wantBlocking: false},
		{fixture: "refusal.json", wantVerdict: schemas.VerdictAdvisory, wantBlocking: false},
		{fixture: "malformed_json.json", wantVerdict: schemas.VerdictFail, wantBlocking: true},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			cfg := Config{
				GateID:        "l4_adversarial",
				Model:         DefaultModel,
				SeverityBlock: []string{RuleCritical, RuleTwoHigh},
				Invoker:       fixtureInvoker(t, tc.fixture),
			}
			gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-fixture"})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if gr.Verdict != tc.wantVerdict {
				t.Fatalf("verdict: got %s, want %s", gr.Verdict, tc.wantVerdict)
			}
			if gr.Blocking != tc.wantBlocking {
				t.Fatalf("blocking: got %v, want %v", gr.Blocking, tc.wantBlocking)
			}
		})
	}
}

// fixtureInvoker reads testdata/<name>, parses the envelope, and
// returns the findings as an Invoker would. Refusal-class fixtures
// surface as a parser error, which Run demotes to advisory verdict.
func fixtureInvoker(t *testing.T, name string) Invoker {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return func(_ context.Context, _ InvokeRequest) (InvokeResponse, error) {
		env, perr := ParseEnvelope(raw)
		if perr != nil {
			return InvokeResponse{}, perr
		}
		return InvokeResponse{
			Findings:  env.Findings,
			PromptSHA: PromptSHA(),
			TokensIn:  100,
			TokensOut: 50,
		}, nil
	}
}
