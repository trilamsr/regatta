// Package reconcile drives the post-hoc Anthropic Cost/Usage API
// reconciliation loop (cost-governor spec §3.4). The reconciler
// periodically:
//
//  1. Computes the just-closed bucket window (top-of-hour + jitter).
//  2. GETs /v1/organizations/cost_report/messages (preferred) or
//     /v1/organizations/usage_report/messages (fallback).
//  3. Sums Anthropic-reported cost vs locally-recorded token_spend.
//  4. Emits a substrate kind='budget_reconciled' row.
//  5. Optionally emits a drift WARN (dedup'd per period_start).
//
// All wiring is injected via Config — no global state, no hidden
// goroutines, no boot-time network call.
package reconcile

import "time"

// WindowForTick returns the [start, end) window for the most recently
// closed bucket of width bucketWidth, given the wall clock at now.
//
// Spec §3.4 line 225: "Reconciler runs at top-of-hour + 2min jitter;
// fetches the just-closed hour's bucket."
//
// Concretely: a tick at 01:02 with bucketWidth=1h returns [00:00, 01:00).
// A tick exactly at 02:00 returns [01:00, 02:00) — the bucket whose end
// is at or before now.
func WindowForTick(now time.Time, bucketWidth time.Duration) (start, end time.Time) {
	// Truncate to bucket boundary in UTC so DST or local-zone shifts
	// never produce off-by-one buckets. Anthropic Usage API returns
	// timestamps in UTC; we stay in lockstep. An on-boundary tick
	// (now == truncate(now)) returns the bucket that just closed,
	// which is what Truncate already does — no special case needed.
	now = now.UTC()
	end = now.Truncate(bucketWidth)
	start = end.Add(-bucketWidth)
	return start, end
}

// NextTickTime returns the wall time at which the next reconcile tick
// should fire. The schedule is top-of-bucket + jitter; if now is past
// the current bucket's jitter mark, the next-next bucket is chosen.
//
// Spec §3.4 line 225 pins the 2min jitter default; callers thread the
// concrete value so tests deterministic and operators can tune.
func NextTickTime(now time.Time, interval, jitter time.Duration) time.Time {
	now = now.UTC()
	// Aligned current bucket start.
	current := now.Truncate(interval)
	candidate := current.Add(jitter)
	if !now.Before(candidate) {
		// Already past the jitter mark of the current bucket — skip ahead.
		candidate = current.Add(interval).Add(jitter)
	}
	return candidate
}
