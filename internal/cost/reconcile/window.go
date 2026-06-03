// Package reconcile drives the post-hoc Anthropic Cost/Usage API
// reconciliation loop (cost-governor spec §3.4): top-of-hour-plus-
// jitter tick computes the just-closed bucket, fetches /v1/orgs/
// cost_report/messages (or usage_report fallback), sums Anthropic vs
// locally-recorded spend, emits substrate kind='budget_reconciled',
// optionally WARNs drift (dedup'd per period_start). All wiring is DI;
// no globals, no hidden goroutines, no boot-time network.
package reconcile

import "time"

// WindowForTick returns [start, end) for the most-recently-closed
// bucket given now. Spec §3.4 line 225. Tick at 01:02 with 1h buckets
// returns [00:00, 01:00); tick exactly at 02:00 returns [01:00, 02:00).
func WindowForTick(now time.Time, bucketWidth time.Duration) (start, end time.Time) {
	// UTC-truncate so DST/local-zone shifts never produce off-by-one
	// buckets — Anthropic Usage API returns UTC timestamps.
	now = now.UTC()
	end = now.Truncate(bucketWidth)
	start = end.Add(-bucketWidth)
	return start, end
}

// NextTickTime returns the wall time at which the next reconcile tick
// should fire. Schedule is top-of-bucket + jitter; if now is past the
// current bucket's jitter mark, the next-next bucket is chosen.
// Spec §3.4 line 225 pins 2min jitter default.
func NextTickTime(now time.Time, interval, jitter time.Duration) time.Time {
	now = now.UTC()
	current := now.Truncate(interval)
	candidate := current.Add(jitter)
	if !now.Before(candidate) {
		// Past this bucket's jitter mark — skip ahead one bucket.
		candidate = current.Add(interval).Add(jitter)
	}
	return candidate
}
