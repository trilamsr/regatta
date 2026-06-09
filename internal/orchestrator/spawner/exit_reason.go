package spawner

import "bytes"

const classifyHaystackCap = 4096

// ExitReason buckets agent.exited beyond bare exit_code; halt gate (#1096) dispatches on these values.
type ExitReason string

// ExitReason values stamped onto agent.exited; operators dispatch on these.
const (
	ExitReasonUnknown                 = ExitReason("unknown")
	ExitReasonCompleted               = ExitReason("completed")
	ExitReasonProviderCreditExhausted = ExitReason("provider_credit_exhausted")
	ExitReasonProviderRateLimited     = ExitReason("provider_rate_limited")
	ExitReasonProviderInternal        = ExitReason("provider_internal_error")
	ExitReasonToolDenied              = ExitReason("tool_denied")
)

// Ordering = precedence: credit_exhausted first (halt-worthy); tool_denied last.
var classifySignatures = []struct {
	reason ExitReason
	tokens [][]byte
}{
	{ExitReasonProviderCreditExhausted, [][]byte{
		bytes.ToLower([]byte("Credit balance is too low")),
		bytes.ToLower([]byte("credit_balance_low")),
		bytes.ToLower([]byte("insufficient_credits")),
	}},
	{ExitReasonProviderRateLimited, [][]byte{
		bytes.ToLower([]byte("rate_limit_error")),
		bytes.ToLower([]byte("rate limit exceeded")),
	}},
	{ExitReasonProviderInternal, [][]byte{
		bytes.ToLower([]byte("Internal server error")),
		bytes.ToLower([]byte("api_error")),
		bytes.ToLower([]byte("overloaded_error")),
	}},
	{ExitReasonToolDenied, [][]byte{
		bytes.ToLower([]byte("tool execution failed: permission denied")),
		bytes.ToLower([]byte("permission denied")),
	}},
}

// ClassifyExitReason walks the last-text ring (case-insensitive substring) for a known signature; signature match wins over exit_code. Falls through: exit_code=0 → Completed; non-zero → Unknown.
func ClassifyExitReason(lastText []byte, exitCode int) ExitReason {
	if reason, ok := matchSignature(lastText); ok {
		return reason
	}
	if exitCode == 0 {
		return ExitReasonCompleted
	}
	return ExitReasonUnknown
}

func matchSignature(lastText []byte) (ExitReason, bool) {
	if len(lastText) > classifyHaystackCap {
		lastText = lastText[len(lastText)-classifyHaystackCap:]
	}
	hay := bytes.ToLower(lastText)
	for _, s := range classifySignatures {
		for _, tok := range s.tokens {
			if bytes.Contains(hay, tok) {
				return s.reason, true
			}
		}
	}
	return "", false
}

