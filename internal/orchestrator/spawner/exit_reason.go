package spawner

import "bytes"

// ExitReason classifies the agent.exited stop bucket so operators and the future provider-halt gate can act on root cause instead of bare exit_code (#1063, prereq for #1096).
type ExitReason string

const (
	ExitReasonUnknown                = ExitReason("unknown")
	ExitReasonCompleted              = ExitReason("completed")
	ExitReasonProviderCreditExhausted = ExitReason("provider_credit_exhausted")
	ExitReasonProviderRateLimited    = ExitReason("provider_rate_limited")
	ExitReasonProviderInternal       = ExitReason("provider_internal_error")
	ExitReasonToolDenied             = ExitReason("tool_denied")
)

// classifySignatures pairs an ExitReason with byte signatures the
// claude CLI prints to stdout before exiting. Match is case-sensitive
// substring on the last-text ring snapshot; signatures are sourced
// from observed CLI output during 2026-06-08 dogfood (#1099 burn).
var classifySignatures = []struct {
	reason ExitReason
	tokens [][]byte
}{
	{ExitReasonProviderCreditExhausted, [][]byte{
		[]byte("Credit balance is too low"),
		[]byte("credit_balance_low"),
		[]byte("insufficient_credits"),
	}},
	{ExitReasonProviderRateLimited, [][]byte{
		[]byte("rate_limit_error"),
		[]byte("429"),
		[]byte("rate limit exceeded"),
	}},
	{ExitReasonProviderInternal, [][]byte{
		[]byte("Internal server error"),
		[]byte("api_error"),
		[]byte("overloaded_error"),
	}},
	{ExitReasonToolDenied, [][]byte{
		[]byte("tool execution failed: permission denied"),
		[]byte("permission denied"),
	}},
}

// ClassifyExitReason inspects the last-text ring snapshot for known signatures and returns the matched ExitReason. exitCode is a fallback signal: exit_code=0 with no signature match returns ExitReasonCompleted; non-zero with no match returns ExitReasonUnknown.
func ClassifyExitReason(lastText []byte, exitCode int) ExitReason {
	for _, s := range classifySignatures {
		for _, tok := range s.tokens {
			if bytes.Contains(lastText, tok) {
				return s.reason
			}
		}
	}
	if exitCode == 0 {
		return ExitReasonCompleted
	}
	return ExitReasonUnknown
}
