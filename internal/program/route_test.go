package program

import (
	"errors"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

func okVerify(_ Verdict) error  { return nil }
func badVerify(_ Verdict) error { return ErrUnverifiable }

func mkVerdict(gateID, prSHA string, verdict schemas.Verdict, blocking bool, started time.Time, findings []schemas.Finding) Verdict {
	return Verdict{
		GateResult: schemas.GateResult{
			GateID:    gateID,
			PRSHA:     prSHA,
			Verdict:   verdict,
			Blocking:  blocking,
			Findings:  findings,
			Signature: schemas.SignatureBlock{Alg: "HMAC-SHA256", KeyID: "k1", MAC: "00"},
			Heartbeat: schemas.TelemetryHeartbeat{
				StartedAt:  started,
				FinishedAt: started.Add(5 * time.Minute),
			},
		},
	}
}

func TestRouteVerdicts(t *testing.T) {
	now := time.Now()
	started := now.Add(-10 * time.Minute)

	passVerdict := mkVerdict("l1_ci", "abcdef0123456789abcdef0123456789abcdef01", schemas.VerdictPass, true, started, nil)

	failVerdict := mkVerdict("l3_spec_conformance", "abcdef0123456789abcdef0123456789abcdef01", schemas.VerdictFail, true, started,
		[]schemas.Finding{{ID: "F-001", Severity: schemas.FindingHigh, Claim: "criterion A-AUTH-01 not addressed", TrapPattern: "P3"}})

	missingHeartbeat := passVerdict
	missingHeartbeat.Heartbeat.FinishedAt = time.Time{}

	failNoFindings := failVerdict
	failNoFindings.Findings = nil

	oosUncovered := passVerdict
	oosUncovered.OutOfScope = []string{"input_validation"}

	oosCovered := passVerdict
	oosCovered.OutOfScope = []string{"input_validation"}
	oosCovering := passVerdict
	oosCovering.GateID = "security"
	oosCovering.Responsibility = []string{"input_validation"}

	cases := []struct {
		name    string
		child   Child
		verify  VerifyFunc
		capCfg  DepthCap
		want    Action
		wantSub string
	}{
		{
			name:   "no verdicts halts",
			child:  Child{WorkItemID: "F-1"},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   HaltHuman, wantSub: "no verdicts",
		},
		{
			name:   "unverifiable halts",
			child:  Child{Verdicts: []Verdict{passVerdict}},
			verify: badVerify,
			capCfg: DefaultDepthCap(),
			want:   HaltHuman, wantSub: "unverifiable",
		},
		{
			name:   "missing heartbeat halts",
			child:  Child{Verdicts: []Verdict{missingHeartbeat}},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   HaltHuman, wantSub: "heartbeat",
		},
		{
			name:   "all pass advances",
			child:  Child{Verdicts: []Verdict{passVerdict}},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   Advance,
		},
		{
			name:   "blocking fail with findings iterates",
			child:  Child{Verdicts: []Verdict{passVerdict, failVerdict}},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   Iterate, wantSub: "fix-feature",
		},
		{
			name:   "blocking fail no findings halts",
			child:  Child{Verdicts: []Verdict{failNoFindings}},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   HaltHuman, wantSub: "without actionable",
		},
		{
			name:   "out-of-scope uncovered halts",
			child:  Child{Verdicts: []Verdict{oosUncovered}},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   HaltHuman, wantSub: "out-of-scope",
		},
		{
			name:   "out-of-scope covered by sibling advances",
			child:  Child{Verdicts: []Verdict{oosCovered, oosCovering}},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   Advance,
		},
		{
			name: "depth functional cap halts",
			child: Child{
				Verdicts: []Verdict{passVerdict},
				Depth:    Depth{Functional: 3},
			},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   HaltHuman, wantSub: "functional",
		},
		{
			name: "depth security cap is wider",
			child: Child{
				Verdicts: []Verdict{passVerdict},
				Depth:    Depth{Security: 5},
			},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   Advance,
		},
		{
			name: "depth security cap over halts",
			child: Child{
				Verdicts: []Verdict{passVerdict},
				Depth:    Depth{Security: 6},
			},
			verify: okVerify,
			capCfg: DefaultDepthCap(),
			want:   HaltHuman, wantSub: "security",
		},
		{
			name:   "nil verifier halts",
			child:  Child{Verdicts: []Verdict{passVerdict}},
			verify: nil,
			capCfg: DefaultDepthCap(),
			want:   HaltHuman, wantSub: "no verifier",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RouteVerdicts(tc.child, tc.verify, tc.capCfg)
			if got.Action != tc.want {
				t.Fatalf("Action: got %q, want %q (reason=%q)", got.Action, tc.want, got.Reason)
			}
			if tc.wantSub != "" && !contains(got.Reason, tc.wantSub) {
				t.Fatalf("Reason missing %q: got %q", tc.wantSub, got.Reason)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestErrUnverifiableMatchable(t *testing.T) {
	wrapped := errors.Join(ErrUnverifiable, errors.New("downstream context"))
	if !errors.Is(wrapped, ErrUnverifiable) {
		t.Fatal("ErrUnverifiable not matchable via errors.Is")
	}
}
