package gate

import "testing"

// TestVerdict_FieldsMatchSpec pins the §3.2 lines 148-156 field set.
func TestVerdict_FieldsMatchSpec(t *testing.T) {
	v := Verdict{
		Allow:           true,
		Reason:          "",
		USDEstimate:     0.01,
		SoftCapBreached: false,
		DowngradeTo:     "",
		CapDAGUSD:       100,
		CapOperatorUSD:  50,
	}
	if !v.Allow || v.CapDAGUSD != 100 || v.CapOperatorUSD != 50 {
		t.Fatalf("Verdict round-trip mismatch: %+v", v)
	}
}

// TestRequestScope_FieldsMatchSpec pins §3.2 lines 168-175 WorkItemScope.
func TestRequestScope_FieldsMatchSpec(t *testing.T) {
	s := WorkItemScope{
		WorkItemID:     "WI-1",
		DAGID:          "DAG-1",
		OperatorID:     "agent-7",
		TenantID:       "default",
		Model:          "claude-opus-4-7",
		AllowDowngrade: true,
		EstHint:        EstHint{USD: 0.50, InputTokens: 1000, MaxTokens: 4096},
	}
	if s.WorkItemID != "WI-1" || s.EstHint.USD != 0.50 || !s.AllowDowngrade {
		t.Fatalf("WorkItemScope round-trip mismatch: %+v", s)
	}
}
