// Package reconcile drives the post-hoc Anthropic Cost/Usage API
// reconciliation loop (cost-governor spec §3.4): top-of-bucket-plus-
// jitter tick computes the just-closed window, fetches
// /v1/orgs/cost_report/messages (or usage_report fallback), sums
// Anthropic vs locally-recorded spend, emits substrate
// kind='budget_reconciled', WARNs drift (dedup'd per period_start).
// All wiring is DI; no globals, hidden goroutines, or boot-time net.
package reconcile

import "time"

// WindowForTick returns [start, end) for the most-recently-closed
// bucket (spec §3.4 line 225); UTC-truncated so DST shifts cannot
// produce off-by-one buckets.
func WindowForTick(now time.Time, bucketWidth time.Duration) (start, end time.Time) {
	now = now.UTC()
	end = now.Truncate(bucketWidth)
	start = end.Add(-bucketWidth)
	return start, end
}

// NextTickTime returns when the next reconcile tick fires; if now is
// past this bucket's jitter mark, the next bucket is chosen (spec
// §3.4 line 225, 2min jitter default).
func NextTickTime(now time.Time, interval, jitter time.Duration) time.Time {
	now = now.UTC()
	current := now.Truncate(interval)
	candidate := current.Add(jitter)
	if !now.Before(candidate) {
		candidate = current.Add(interval).Add(jitter)
	}
	return candidate
}
