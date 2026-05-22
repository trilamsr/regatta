package missions

import (
	"errors"
	"testing"
	"time"
)

// stub verifier — toggles per test case.
func okVerify(_ Verdict) error  { return nil }
func badVerify(_ Verdict) error { return ErrUnverifiable }

func anchored(t time.Time) (*time.Time, *time.Time) {
	start := t
	end := t.Add(5 * time.Minute)
	return &start, &end
}

func TestRouteVerdicts(t *testing.T) {
	now := time.Now()
	started, finished := anchored(now.Add(-10 * time.Minute))

	passVerdict := Verdict{
		GateID:     "l1_ci",
		PRSHA:      "abcdef0123456789abcdef0123456789abcdef01",
		Result:     "pass",
		Blocking:   true,
		Severity:   "none",
		StartedAt:  started,
		FinishedAt: finished,
		Signature:  Signature{Alg: "HMAC-SHA256", KeyID: "k1", MAC: "00"},
	}

	failVerdict := Verdict{
		GateID:     "l3_spec_conformance",
		PRSHA:      "abcdef0123456789abcdef0123456789abcdef01",
		Result:     "fail",
		Blocking:   true,
		Severity:   "high",
		StartedAt:  started,
		FinishedAt: finished,
		Findings: []Finding{
			{ID: "F-001", Severity: "high", Claim: "criterion A-AUTH-01 not addressed", TrapPattern: "P3"},
		},
		Signature: Signature{Alg: "HMAC-SHA256", KeyID: "k1", MAC: "ff"},
	}

	missingHeartbeat := passVerdict
	missingHeartbeat.FinishedAt = nil

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
		cap     DepthCap
		want    Action
		wantSub string
	}{
		{
			name:   "no verdicts halts",
			child:  Child{WorkItemID: "F-1"},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   HaltHuman, wantSub: "no verdicts",
		},
		{
			name:   "unverifiable halts",
			child:  Child{Verdicts: []Verdict{passVerdict}},
			verify: badVerify,
			cap:    DefaultDepthCap(),
			want:   HaltHuman, wantSub: "unverifiable",
		},
		{
			name:   "missing heartbeat halts",
			child:  Child{Verdicts: []Verdict{missingHeartbeat}},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   HaltHuman, wantSub: "heartbeat",
		},
		{
			name:   "all pass advances",
			child:  Child{Verdicts: []Verdict{passVerdict}},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   Advance,
		},
		{
			name:   "blocking fail with findings iterates",
			child:  Child{Verdicts: []Verdict{passVerdict, failVerdict}},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   Iterate, wantSub: "fix-feature",
		},
		{
			name:   "blocking fail no findings halts",
			child:  Child{Verdicts: []Verdict{failNoFindings}},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   HaltHuman, wantSub: "without actionable",
		},
		{
			name:   "out-of-scope uncovered halts",
			child:  Child{Verdicts: []Verdict{oosUncovered}},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   HaltHuman, wantSub: "out-of-scope",
		},
		{
			name:   "out-of-scope covered by sibling advances",
			child:  Child{Verdicts: []Verdict{oosCovered, oosCovering}},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   Advance,
		},
		{
			name: "depth functional cap halts",
			child: Child{
				Verdicts: []Verdict{passVerdict},
				Depth:    Depth{Functional: 3},
			},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   HaltHuman, wantSub: "functional",
		},
		{
			name: "depth security cap is wider",
			child: Child{
				Verdicts: []Verdict{passVerdict},
				Depth:    Depth{Security: 5},
			},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   Advance,
		},
		{
			name: "depth security cap over halts",
			child: Child{
				Verdicts: []Verdict{passVerdict},
				Depth:    Depth{Security: 6},
			},
			verify: okVerify,
			cap:    DefaultDepthCap(),
			want:   HaltHuman, wantSub: "security",
		},
		{
			name:   "nil verifier halts",
			child:  Child{Verdicts: []Verdict{passVerdict}},
			verify: nil,
			cap:    DefaultDepthCap(),
			want:   HaltHuman, wantSub: "no verifier",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RouteVerdicts(tc.child, tc.verify, tc.cap)
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

// Sanity: ErrUnverifiable comparison works with errors.Is for callers
// that wrap.
func TestErrUnverifiableMatchable(t *testing.T) {
	wrapped := errors.Join(ErrUnverifiable, errors.New("downstream context"))
	if !errors.Is(wrapped, ErrUnverifiable) {
		t.Fatal("ErrUnverifiable not matchable via errors.Is")
	}
}
