package web

// ClampDiff caps b at MaxDiffBytes on a raw byte boundary so the inline
// approval-page diff cannot blow out the page; overflowed reports whether the
// input exceeded the cap so the template can link to the full streamed diff.
//
// The cut is byte-exact, NOT rune-aware: the inline view is advisory and the
// uncut diff is one click away at /approve/{aid}/diff, so a trailing partial
// UTF-8 sequence (rendered as U+FFFD) is an acceptable trade for a hard,
// test-pinnable byte cap (MAY-116, spec §3.4 I2).
func ClampDiff(b []byte) (clamped []byte, overflowed bool) {
	if len(b) <= MaxDiffBytes {
		return b, false
	}
	return b[:MaxDiffBytes], true
}
