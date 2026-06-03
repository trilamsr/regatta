package approval

import (
	"context"
	"testing"
)

// TestReplayVotes_DecideRoundtrip — issue #508: real decide.go write → real replayVotes read; rejects fixture-only coverage that masked the producer/consumer key drift.
func TestReplayVotes_DecideRoundtrip(t *testing.T) {
	// Two reviewers, quorum 2 so the first vote stays non-terminal and
	// the decided event survives in the log for replayVotes to consume
	// (a terminal sweep would freeze the log but replayVotes still walks
	// every decided row regardless — the non-terminal shape just keeps
	// the test focused on the payload-key contract).
	h := newDecideTxHarness(t, "system", []string{"alice", "bob"}, 2, false)

	if _, _, err := DecideTx(context.Background(), h.db, h.payload("alice"), "alice", "allow", "", h.clock); err != nil {
		t.Fatalf("DecideTx alice: %v", err)
	}
	if _, _, err := DecideTx(context.Background(), h.db, h.payload("bob"), "bob", "deny", "", h.clock); err != nil {
		t.Fatalf("DecideTx bob: %v", err)
	}

	events, err := h.db.ListApprovalEvents(context.Background(), h.approvalID)
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}

	// New tier overlap: both reviewers carry forward, so neither vote
	// should be discarded. Vote text MUST be the canonical decide.go
	// payload value — empty strings mean replayVotes read the wrong key.
	replayed := replayVotes(events, []string{"alice", "bob"})
	if len(replayed) != 2 {
		t.Fatalf("len(replayed)=%d; want 2 (one per decided event)", len(replayed))
	}

	byActor := map[string]replayedVote{}
	for _, rv := range replayed {
		byActor[rv.Actor] = rv
	}
	if got := byActor["alice"]; got.Vote != DecisionAllow {
		t.Errorf("alice.Vote=%q; want %q — replayVotes is reading the wrong payload key from decide.go's emit", got.Vote, DecisionAllow)
	}
	if got := byActor["bob"]; got.Vote != DecisionDeny {
		t.Errorf("bob.Vote=%q; want %q — replayVotes is reading the wrong payload key from decide.go's emit", got.Vote, DecisionDeny)
	}

	// tallyReplay must register the votes — a key mismatch would zero
	// both sides and silently break tier-replay quorum math (§3.3.1.2).
	allow, deny, deciders := tallyReplay(replayed)
	if allow != 1 || deny != 1 {
		t.Errorf("tally allow=%d deny=%d; want 1/1 — payload key drift erases vote text", allow, deny)
	}
	if len(deciders) != 2 {
		t.Errorf("deciders=%v; want 2 entries", deciders)
	}
}
