package spawner

import "bytes"

// classifyHaystackCap bounds the lowercased buffer copy so a future ring-size bump cannot make per-exit classification quadratic. 4 KiB == lastTextRingSize today; pinned independently so the contract holds even if the ring grows.
const classifyHaystackCap = 4096

// ExitReason classifies the agent.exited stop bucket so operators and the future provider-halt gate can act on root cause instead of bare exit_code (#1063, prereq for #1096).
type ExitReason string

// Stop-bucket values stamped onto agent.exited; operators dispatch on these.
const (
	ExitReasonUnknown                 = ExitReason("unknown")
	ExitReasonCompleted               = ExitReason("completed")
	ExitReasonProviderCreditExhausted = ExitReason("provider_credit_exhausted")
	ExitReasonProviderRateLimited     = ExitReason("provider_rate_limited")
	ExitReasonProviderInternal        = ExitReason("provider_internal_error")
	ExitReasonToolDenied              = ExitReason("tool_denied")
)

// classifySignatures pairs an ExitReason with byte signatures the
// claude CLI prints to stdout before exiting. Match is case-INsensitive
// substring on the last-text ring snapshot — Anthropic-side casing
// changes (locale, capitalisation tweaks) do not silently drop signal.
// Ordering is precedence: credit_exhausted fires first because it is
// the highest-impact (halt-worthy); tool_denied last because its
// "permission denied" token is short enough to false-positive on
// downstream payload text and must lose to anything more specific.
//
// Only credit_balance_low / insufficient_credits / Credit balance is
// too low have observed precedent (2026-06-08 dogfood). The other
// signatures are speculative-but-narrow: rate_limit_error,
// api_error, overloaded_error are documented Anthropic CLI error
// types; "tool execution failed: permission denied" is the verbatim
// stream-json testdata error subtype. Speculative tokens were kept
// narrow on purpose — "429" was rejected as a substring because it
// appears in unrelated payload bytes (issue numbers, timestamps);
// only the explicit "rate_limit_error" / "rate limit exceeded" forms
// remain. New tokens get a #-citation when added.
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

// ClassifyExitReason inspects the last-text ring snapshot for known signatures and returns the matched ExitReason. exitCode is a fallback signal: exit_code=0 with no signature match returns ExitReasonCompleted; non-zero with no match returns ExitReasonUnknown. lastText is matched case-insensitively against the lowercased signature tokens.
func ClassifyExitReason(lastText []byte, exitCode int) ExitReason {
	if len(lastText) > classifyHaystackCap {
		lastText = lastText[len(lastText)-classifyHaystackCap:]
	}
	hay := bytes.ToLower(lastText)
	for _, s := range classifySignatures {
		for _, tok := range s.tokens {
			if bytes.Contains(hay, tok) {
				return s.reason
			}
		}
	}
	if exitCode == 0 {
		return ExitReasonCompleted
	}
	return ExitReasonUnknown
}
