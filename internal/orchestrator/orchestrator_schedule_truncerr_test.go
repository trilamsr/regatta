package orchestrator

import (
	"errors"
	"strings"
	"testing"
)

// TestTruncErr_BelowCapReturnsAsIs asserts errors shorter than eventPayloadErrCap are returned verbatim (R31-I3).
func TestTruncErr_BelowCapReturnsAsIs(t *testing.T) {
	err := errors.New("short error")
	got := truncErr(err)
	if got != "short error" {
		t.Fatalf("truncErr(short)=%q want %q", got, "short error")
	}
}

// TestTruncErr_AboveCapTruncatesAndAnnotates asserts payloads above eventPayloadErrCap are truncated with a "…[truncated N chars]" suffix (R31-I3).
func TestTruncErr_AboveCapTruncatesAndAnnotates(t *testing.T) {
	long := strings.Repeat("x", eventPayloadErrCap+200)
	err := errors.New(long)
	got := truncErr(err)
	if len(got) <= eventPayloadErrCap {
		t.Fatalf("truncErr did not truncate; len=%d want > %d (cap + suffix)", len(got), eventPayloadErrCap)
	}
	if !strings.Contains(got, "truncated 200 chars") {
		t.Fatalf("truncErr missing truncation suffix; got tail=%q", got[max(0, len(got)-40):])
	}
}
