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

// TestClassifyExitReason_PerVariant verifies every signature variant in the precedence table fires the expected ExitReason. Each new signature added to classifySignatures MUST have a row here so a future contributor cannot silently mis-bucket an exit (per reviewer a7e408d8466d8c67b).
func TestClassifyExitReason_PerVariant(t *testing.T) {
	cases := []struct {
		name string
		text string
		want ExitReason
	}{
		{"credit-balance-prose", "Credit balance is too low", ExitReasonProviderCreditExhausted},
		{"credit-balance-snake", "{\"type\":\"error\",\"error\":{\"type\":\"credit_balance_low\"}}", ExitReasonProviderCreditExhausted},
		{"insufficient-credits", "stripe: insufficient_credits", ExitReasonProviderCreditExhausted},
		{"rate-limit-error", "rate_limit_error tripped", ExitReasonProviderRateLimited},
		{"rate-limit-prose", "Anthropic API: rate limit exceeded", ExitReasonProviderRateLimited},
		{"internal-prose", "Internal server error from upstream", ExitReasonProviderInternal},
		{"api-error", "api_error: bad gateway", ExitReasonProviderInternal},
		{"overloaded", "overloaded_error retry-after 30", ExitReasonProviderInternal},
		{"tool-denied-full", "tool execution failed: permission denied", ExitReasonToolDenied},
		{"tool-denied-short", "permission denied", ExitReasonToolDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyExitReason([]byte(tc.text), 1); got != tc.want {
				t.Fatalf("%q → %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// TestClassifyExitReason_CaseInsensitive: Anthropic-side casing changes (locale, capitalisation tweaks) MUST NOT silently drop signal — the matcher lowercases both sides per reviewer a7e408d8466d8c67b.
func TestClassifyExitReason_CaseInsensitive(t *testing.T) {
	got := ClassifyExitReason([]byte("credit balance is too low"), 1)
	if got != ExitReasonProviderCreditExhausted {
		t.Fatalf("lowercase variant must classify; got %q", got)
	}
	got = ClassifyExitReason([]byte("CREDIT BALANCE IS TOO LOW"), 1)
	if got != ExitReasonProviderCreditExhausted {
		t.Fatalf("uppercase variant must classify; got %q", got)
	}
}

// TestClassifyExitReason_No429FalsePositive: bare "429" used to be a signature; it appeared in unrelated bytes (timestamps, issue numbers) and false-positive-matched provider_rate_limited. Removed; test pins the regression so a future re-add forces a second look.
func TestClassifyExitReason_No429FalsePositive(t *testing.T) {
	got := ClassifyExitReason([]byte("BUG-1429: agent completed normally"), 0)
	if got != ExitReasonCompleted {
		t.Fatalf("bare 429 must not trigger rate-limited classification; got %q", got)
	}
}

// TestClassifyExitReason_EmptyRing: zero-byte ring snapshot falls through to the exit_code branch — protects the dashboard from crashing on a never-emitted-stdout child (per reviewer a7e408d8466d8c67b).
func TestClassifyExitReason_EmptyRing(t *testing.T) {
	if got := ClassifyExitReason(nil, 0); got != ExitReasonCompleted {
		t.Fatalf("nil + exit 0 → %q, want %q", got, ExitReasonCompleted)
	}
	if got := ClassifyExitReason([]byte{}, 1); got != ExitReasonUnknown {
		t.Fatalf("empty + exit 1 → %q, want %q", got, ExitReasonUnknown)
	}
}

// TestClassifyExitReason_HaystackCapEvictsHeader: when the ring is larger than classifyHaystackCap, only the trailing window is scanned — pins the bounded-work contract (per reviewer a7e408d8466d8c67b).
func TestClassifyExitReason_HaystackCapEvictsHeader(t *testing.T) {
	prefix := make([]byte, classifyHaystackCap+1)
	for i := range prefix {
		prefix[i] = 'x'
	}
	got := ClassifyExitReason(append(prefix, []byte("Credit balance is too low")...), 1)
	if got != ExitReasonProviderCreditExhausted {
		t.Fatalf("trailing signature must be matched even past cap; got %q", got)
	}
	prefix = append([]byte("Credit balance is too low"), prefix...)
	got = ClassifyExitReason(prefix, 1)
	if got != ExitReasonUnknown {
		t.Fatalf("leading signature past cap must be evicted; got %q", got)
	}
}
