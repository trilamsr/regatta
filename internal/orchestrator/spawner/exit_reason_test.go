package spawner

import "testing"

func TestClassifyExitReason_CreditBalance(t *testing.T) {
	got := ClassifyExitReason([]byte("Credit balance is too low\n"), 1)
	if got != ExitReasonProviderCreditExhausted {
		t.Fatalf("credit-balance signature → %q, want %q", got, ExitReasonProviderCreditExhausted)
	}
}

func TestClassifyExitReason_RateLimit(t *testing.T) {
	got := ClassifyExitReason([]byte(`{"type":"error","error":{"type":"rate_limit_error"}}`), 1)
	if got != ExitReasonProviderRateLimited {
		t.Fatalf("rate-limit signature → %q, want %q", got, ExitReasonProviderRateLimited)
	}
}

func TestClassifyExitReason_ProviderInternal(t *testing.T) {
	got := ClassifyExitReason([]byte("Internal server error from provider"), 1)
	if got != ExitReasonProviderInternal {
		t.Fatalf("internal-error signature → %q, want %q", got, ExitReasonProviderInternal)
	}
}

func TestClassifyExitReason_ToolDenied(t *testing.T) {
	got := ClassifyExitReason([]byte("tool execution failed: permission denied"), 1)
	if got != ExitReasonToolDenied {
		t.Fatalf("tool-denied signature → %q, want %q", got, ExitReasonToolDenied)
	}
}

func TestClassifyExitReason_CompletedZeroExit(t *testing.T) {
	got := ClassifyExitReason([]byte("done\n"), 0)
	if got != ExitReasonCompleted {
		t.Fatalf("exit_code=0 + no signature → %q, want %q", got, ExitReasonCompleted)
	}
}

func TestClassifyExitReason_UnknownNonZero(t *testing.T) {
	got := ClassifyExitReason([]byte("some unexpected output"), 1)
	if got != ExitReasonUnknown {
		t.Fatalf("non-zero + no signature → %q, want %q", got, ExitReasonUnknown)
	}
}

func TestClassifyExitReason_SignaturePrecedenceOverExitCode(t *testing.T) {
	got := ClassifyExitReason([]byte("Credit balance is too low"), 0)
	if got != ExitReasonProviderCreditExhausted {
		t.Fatalf("signature must win over exit_code=0; got %q", got)
	}
}
