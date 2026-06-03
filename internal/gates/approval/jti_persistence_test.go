// Issue #195 — minted token JTIs must land in approval_events so the
// reaper's revocation branch (spec §3.3.1.3) is reachable. These tests
// pin the producer side (gate) + the consumer side (reaper revoke +
// post-revocation replay rejection).
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"testing/quick"
	"time"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestGate_PersistsTokenMintedPerJTI — first-sighting Evaluate must append one `token_minted` row per reviewer's JTI. Pre-fix: zero such rows 
func TestGate_PersistsTokenMintedPerJTI(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	notifier := &captureNotifier{}
	g := NewGate(db, notifier, testKeyring(), "k1", func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, now)

	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	ap, err := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	if err != nil || ap == nil {
		t.Fatalf("GetApprovalForWorkItem: ap=%v err=%v", ap, err)
	}
	events, err := db.ListApprovalEvents(ctx, ap.ID)
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}

	minted := make([]string, 0)
	for _, e := range events {
		if e.Kind == EventKindTokenMinted {
			if e.TokenJTI == "" {
				t.Errorf("token_minted event has empty TokenJTI: %+v", e)
			}
			minted = append(minted, e.TokenJTI)
		}
	}
	if got, want := len(minted), len(cfg.Reviewers); got != want {
		t.Fatalf("token_minted rows=%d; want %d (one per reviewer)", got, want)
	}

	// Each JTI must be distinct: a collision would mean MintToken
	// returned the same jti twice or the gate persisted one twice.
	seen := map[string]bool{}
	for _, j := range minted {
		if seen[j] {
			t.Errorf("duplicate JTI %q in token_minted rows", j)
		}
		seen[j] = true
	}

	// outstandingJTIs must now return the full minted set — this is the
	// invariant the reaper's revocation branch depends on.
	outs := outstandingJTIs(events)
	if len(outs) != len(cfg.Reviewers) {
		t.Errorf("outstandingJTIs=%d; want %d (gate's mint markers reachable to reaper)", len(outs), len(cfg.Reviewers))
	}
}

// TestGate_PostEscalationPersistsFreshJTIs — notifyEscalatedTier mints tier-N tokens; the same `token_minted` invariant must hold so a subsequ
func TestGate_PostEscalationPersistsFreshJTIs(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	clockT := t0
	clock := func() time.Time { return clockT }
	db := newGateTestDB(t, clock)
	notifier := &captureNotifier{}
	g := NewGate(db, notifier, testKeyring(), "k1", clock,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	cfg.OnTimeout = OnTimeoutEscalate
	cfg.EscalationChain = []state.TierConfig{
		{Reviewers: []string{"dave", "erin"}, Quorum: 1, Timeout: time.Hour, DecisionWindow: 30 * time.Minute},
	}
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, t0)

	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("Evaluate#1: %v", err)
	}
	ap, err := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	if err != nil || ap == nil {
		t.Fatalf("GetApprovalForWorkItem: %v", err)
	}
	tier0Events, _ := db.ListApprovalEvents(ctx, ap.ID)
	tier0JTIs := collectMintedJTIs(tier0Events)
	if got, want := len(tier0JTIs), len(cfg.Reviewers); got != want {
		t.Fatalf("tier-0 token_minted rows=%d; want %d", got, want)
	}

	// Advance + sweep (reaper appends `escalated` + revokes tier-0 JTIs).
	clockT = t0.Add(cfg.Timeout + time.Minute)
	reaper, err := NewReaper(db, slog.New(slog.NewTextHandler(io.Discard, nil)), clock)
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}
	if err := reaper.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	// Tick #2 — notifyEscalatedTier must mint+persist fresh JTIs for tier-1.
	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("Evaluate#2: %v", err)
	}
	allEvents, _ := db.ListApprovalEvents(ctx, ap.ID)
	allJTIs := collectMintedJTIs(allEvents)
	// 3 tier-0 + 2 tier-1 = 5 total token_minted rows
	wantTotal := len(cfg.Reviewers) + len(cfg.EscalationChain[0].Reviewers)
	if got := len(allJTIs); got != wantTotal {
		t.Errorf("total token_minted rows post-escalate=%d; want %d", got, wantTotal)
	}
	// Tier-0 JTIs must remain consumed (revoked); only tier-1 JTIs are
	// outstanding now.
	outs := outstandingJTIs(allEvents)
	if len(outs) != len(cfg.EscalationChain[0].Reviewers) {
		t.Errorf("outstandingJTIs post-escalate=%d; want %d (tier-0 revoked, only tier-1 live)",
			len(outs), len(cfg.EscalationChain[0].Reviewers))
	}
}

// TestGate_ReaperRevokesMintedTokens_RevokedTokenIsUnusable — full revocation E2E: gate mints, reaper escalates and revokes, the originally-mi
func TestGate_ReaperRevokesMintedTokens_RevokedTokenIsUnusable(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	clockT := t0
	clock := func() time.Time { return clockT }
	db := newGateTestDB(t, clock)
	notifier := &captureNotifier{}
	kr := testKeyring()
	g := NewGate(db, notifier, kr, "k1", clock,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	cfg.OnTimeout = OnTimeoutEscalate
	cfg.EscalationChain = []state.TierConfig{
		// Tier-1 keeps alice so her tier-0 token is the natural replay
		// candidate (post-escalate she's still in-snapshot, but the
		// previously-minted JTI has been revoked).
		{Reviewers: []string{"alice", "dave"}, Quorum: 1, Timeout: time.Hour, DecisionWindow: 30 * time.Minute},
	}
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, t0)

	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("Evaluate#1: %v", err)
	}
	aliceTier0Wire := notifier.requests[0].Tokens["alice"]
	if aliceTier0Wire == "" {
		t.Fatalf("alice tier-0 token missing")
	}
	// Verify the token + extract its JTI so we can assert exactly that
	// JTI lands in a token_consumed-reason=escalated row.
	tier0Payload, err := approvaltoken.VerifyToken(kr, aliceTier0Wire, "alice", t0)
	if err != nil {
		t.Fatalf("VerifyToken tier-0: %v", err)
	}
	aliceTier0JTI := tier0Payload.JTI

	// Advance + sweep: reaper escalates and revokes outstanding JTIs.
	clockT = t0.Add(cfg.Timeout + time.Minute)
	reaper, err := NewReaper(db, slog.New(slog.NewTextHandler(io.Discard, nil)), clock)
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}
	if err := reaper.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	ap, _ := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	events, _ := db.ListApprovalEvents(ctx, ap.ID)

	// Assert alice's tier-0 JTI appears in a token_consumed-with-
	// reason=escalated row — this is the reaper-revoke audit signal.
	revoked := false
	for _, e := range events {
		if e.Kind != EventKindTokenConsumed || e.TokenJTI != aliceTier0JTI {
			continue
		}
		var p struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(e.Payload, &p); err == nil && p.Reason == reasonEscalated {
			revoked = true
			break
		}
	}
	if !revoked {
		t.Fatalf("alice tier-0 JTI %q not revoked (no token_consumed reason=escalated row)", aliceTier0JTI)
	}

	// Closing the loop: a decide-path attempt to consume the now-revoked
	// tier-0 token must abort with ErrTokenReplay. The UNIQUE on
	// (approval_id,kind,token_jti) collides against the revoke row.
	// Decide is post-escalate so the test verifies the revocation, not
	// the escalate-snapshot membership: alice is in tier-1 so without
	// the revoke row she'd succeed.
	tier0Payload.AID = ap.ID
	_, _, derr := DecideTx(ctx, db, tier0Payload, "alice", "allow", "ok", clock)
	if derr == nil {
		t.Fatalf("DecideTx with revoked tier-0 token: got nil err; want ErrTokenReplay")
	}
	if !errors.Is(derr, state.ErrTokenReplay) {
		t.Fatalf("DecideTx err=%v; want ErrTokenReplay (revoked token must collide on UNIQUE)", derr)
	}
}

// TestReaperMintInvariant_Property (A+) — for any K reviewers (1..6), the gate's first-sighting Evaluate plus the reaper's escalate-sweep must
func TestReaperMintInvariant_Property(t *testing.T) {
	if testing.Short() {
		t.Skip("property test; skipped in -short")
	}

	prop := func(seed uint8) bool {
		// Map seed → K in [1,6]. Avoid zero (Quorum=0 would never resolve).
		k := int(seed%6) + 1
		t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
		clockT := t0
		clock := func() time.Time { return clockT }
		db := newGateTestDB(t, clock)
		notifier := &captureNotifier{}
		g := NewGate(db, notifier, testKeyring(), "k1", clock,
			slog.New(slog.NewTextHandler(io.Discard, nil)))

		reviewers := make([]string, k)
		for i := 0; i < k; i++ {
			reviewers[i] = string(rune('a' + i))
		}
		cfg := Config{
			Name:            "ship",
			RiskClass:       RiskLow,
			Reviewers:       reviewers,
			Quorum:          1,
			Timeout:         time.Hour,
			DecisionWindow:  30 * time.Minute,
			OnTimeout:       OnTimeoutEscalate,
			EscalationChain: []state.TierConfig{{Reviewers: []string{"escalator"}, Quorum: 1, Timeout: time.Hour}},
		}
		wi := state.WorkItem{
			ID: "wi-prop", Kind: state.KindFeature,
			Title: "p", Lane: "server", Status: state.WorkStatusPlanned,
		}
		ctx := context.Background()
		seedWorkItem(t, db, wi, t0)

		if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
			t.Logf("Evaluate err=%v", err)
			return false
		}
		ap, _ := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
		preEscEvents, _ := db.ListApprovalEvents(ctx, ap.ID)
		mintedJTIs := collectMintedJTIs(preEscEvents)
		if len(mintedJTIs) != k {
			return false
		}

		clockT = t0.Add(cfg.Timeout + time.Minute)
		reaper, err := NewReaper(db, slog.New(slog.NewTextHandler(io.Discard, nil)), clock)
		if err != nil {
			return false
		}
		if err := reaper.Sweep(ctx); err != nil {
			return false
		}

		postEvents, _ := db.ListApprovalEvents(ctx, ap.ID)
		revoked := collectRevokedJTIs(postEvents)
		if len(revoked) != k {
			return false
		}
		return sameSet(revoked, mintedJTIs)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 12}); err != nil {
		t.Fatal(err)
	}
}

// collectMintedJTIs returns every JTI on a token_minted row, in the
// order they appear in the event log.
func collectMintedJTIs(events []state.ApprovalEvent) []string {
	out := make([]string, 0)
	for _, e := range events {
		if e.Kind == EventKindTokenMinted && e.TokenJTI != "" {
			out = append(out, e.TokenJTI)
		}
	}
	return out
}

// collectRevokedJTIs returns every JTI on a token_consumed-with-
// reason=escalated row. The reaper writes one per outstanding JTI; the
// presence of this set is the reaper-revoke audit signal.
func collectRevokedJTIs(events []state.ApprovalEvent) []string {
	out := make([]string, 0)
	for _, e := range events {
		if e.Kind != EventKindTokenConsumed || e.TokenJTI == "" {
			continue
		}
		var p struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(e.Payload, &p); err == nil && p.Reason == reasonEscalated {
			out = append(out, e.TokenJTI)
		}
	}
	return out
}

// sameSet returns true when a and b contain the same elements ignoring
// order and duplicates. Used by the property test's reaper-vs-mint
// invariant: the revoked set must equal the minted set.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	in := make(map[string]int, len(a))
	for _, x := range a {
		in[x]++
	}
	for _, x := range b {
		in[x]--
		if in[x] < 0 {
			return false
		}
	}
	return true
}

