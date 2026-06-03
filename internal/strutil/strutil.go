// Package strutil collects tiny string helpers that two or more
// packages need — avoids duplicate definitions that drift over time.
package strutil

// Truncate returns s when len(s) <= n, otherwise s[:n] + "...".
// Used by provider/anthropic and obs/otel for log-message capping
// against budget.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// OrEmpty turns a nil slice into [] so the JSON wire shape is "[]"
// not "null"; matches the substrate/approvals column DEFAULT '[]'
// and keeps fold-of-events math symmetric across NULL-vs-empty edge
// cases.
func OrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
