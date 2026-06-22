package main

import "testing"

// TestReviewStatus_DefaultDBPathMatchesPeers asserts review status default db matches the other CLI subcommands (regatta.db) instead of an orphan .regatta/state.db.
func TestReviewStatus_DefaultDBPathMatchesPeers(t *testing.T) {
	got := defaultStateDB()
	want := "regatta.db"
	if got != want {
		t.Fatalf("defaultStateDB() = %q; want %q (peer subcommands agents/audit/approval/cost/events/selfimprove/resume/merge_status all default to %q; orphan default forces operators to pass --db on every review status invocation)", got, want, want)
	}
}
