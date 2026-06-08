package prwatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// stubLister is a hermetic PRLister keyed by branch + titlePrefix.
type stubLister struct {
	mu             sync.Mutex
	byBranch       map[string][]PullRequest
	byTitlePrefix  map[string][]PullRequest
	err            error
	calls          int
}

func (s *stubLister) ListOpenByHead(_ context.Context, branch, titlePrefix string) ([]PullRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := append([]PullRequest(nil), s.byBranch[branch]...)
	if titlePrefix != "" {
		out = append(out, s.byTitlePrefix[titlePrefix]...)
	}
	return dedupePRs(out), nil
}

// stubVersion is a hermetic GHVersionProbe.
type stubVersion struct {
	ver string
	err error
}

func (s stubVersion) Version(context.Context) (string, error) { return s.ver, s.err }

// branchFor is the test mirror of spawner.WorktreeManager.BranchFor.
func branchFor(id int64) string { return fmt.Sprintf("regatta/agent-%d", id) }

// newTestWatcher wires a Watcher against an in-memory DB.
func newTestWatcher(t *testing.T, lister PRLister) (*Watcher, *state.DB) {
	t.Helper()
	db := statetest.OpenDB(t)
	w, err := New(Config{DB: db, BranchFn: branchFor, Lister: lister})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return w, db
}

// driveToRunning advances an agent from pending → running.
func driveToRunning(t *testing.T, db *state.DB, workItem string) *state.Agent {
	t.Helper()
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, workItem, "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
		t.Fatalf("spawning: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentRunning, state.AgentMutation{}); err != nil {
		t.Fatalf("running: %v", err)
	}
	return a
}

// TestWatcher_RunningWithOpenPR_TransitionsAndEmitsEvent guards the happy path.
func TestWatcher_RunningWithOpenPR_TransitionsAndEmitsEvent(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 42, HeadRefOid: "deadbeef", State: "OPEN"}},
	}}
	w, db := newTestWatcher(t, lister)
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != state.AgentPROpen {
		t.Fatalf("state=%s, want pr_open", got.State)
	}
	if got.PRSHA != "deadbeef" {
		t.Fatalf("pr_sha=%s, want deadbeef", got.PRSHA)
	}
	events, err := db.ListEventsByKindSince(context.Background(), "agent_pr_opened", 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("agent_pr_opened events=%d, want 1", len(events))
	}
}

// TestWatcher_BranchRenamed_DetectsAndEmitsEvent covers issue #520.
func TestWatcher_BranchRenamed_DetectsAndEmitsEvent(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 7, HeadRefOid: "abc123", State: "OPEN"}},
	}}
	w, db := newTestWatcher(t, lister)
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("initial sweep: %v", err)
	}
	// Branch renamed: gh returns no PR for the original branch.
	lister.mu.Lock()
	lister.byBranch = map[string][]PullRequest{}
	lister.mu.Unlock()
	for i := 0; i < DefaultBranchRenameThreshold-1; i++ {
		if err := w.Sweep(context.Background()); err != nil {
			t.Fatalf("sub-threshold sweep %d: %v", i, err)
		}
	}
	pre, err := db.ListEventsByKindSince(context.Background(), "agent_branch_renamed", 0, 100)
	if err != nil {
		t.Fatalf("list events pre: %v", err)
	}
	if len(pre) != 0 {
		t.Fatalf("pre-threshold agent_branch_renamed=%d, want 0", len(pre))
	}
	// One more sweep crosses the threshold and fires the event.
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("threshold sweep: %v", err)
	}
	got, err := db.ListEventsByKindSince(context.Background(), "agent_branch_renamed", 0, 100)
	if err != nil {
		t.Fatalf("list events post: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("agent_branch_renamed=%d, want 1", len(got))
	}
	if !got[0].AgentID.Valid || got[0].AgentID.Int64 != a.ID {
		t.Fatalf("event agent_id=%v, want %d", got[0].AgentID, a.ID)
	}
}

// TestWatcher_MultiHost_NoDuplicateEvents covers issue #521.
func TestWatcher_MultiHost_NoDuplicateEvents(t *testing.T) {
	lister1 := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 9, HeadRefOid: "sha-a", State: "OPEN"}},
	}}
	lister2 := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 9, HeadRefOid: "sha-a", State: "OPEN"}},
	}}
	w1, db := newTestWatcher(t, lister1)
	driveToRunning(t, db, "WORK-1")
	w2, err := New(Config{DB: db, BranchFn: branchFor, Lister: lister2})
	if err != nil {
		t.Fatalf("New w2: %v", err)
	}
	// Both watchers sweep the same DB; the second writer's INSERT
	// must hit the partial UNIQUE index on (agent_id, kind='agent_pr_opened')
	// and Sweep must swallow the violation.
	if err := w1.Sweep(context.Background()); err != nil {
		t.Fatalf("w1 sweep: %v", err)
	}
	if err := w2.Sweep(context.Background()); err != nil {
		t.Fatalf("w2 sweep: %v", err)
	}
	events, err := db.ListEventsByKindSince(context.Background(), "agent_pr_opened", 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("agent_pr_opened events=%d, want 1 (UNIQUE suppressed second writer)", len(events))
	}
}

// TestWatcher_ForkPRHead_MatchesViaTitlePrefix covers issue #522.
func TestWatcher_ForkPRHead_MatchesViaTitlePrefix(t *testing.T) {
	// Fork PR: gh `--head regatta/agent-1` returns nothing because
	// the PR's head ref is owner-qualified (`forkuser:regatta/agent-1`).
	// The title-prefix fallback rescues the correlation; the head
	// ref still ends in `agent-1` because the operator's worktree
	// originally branched from `regatta/agent-1` before re-pushing
	// to the fork.
	lister := &stubLister{
		byBranch:      map[string][]PullRequest{},
		byTitlePrefix: map[string][]PullRequest{
			"[agent-1]": {{
				Number:      11,
				HeadRefOid:  "forksha",
				State:       "OPEN",
				HeadRefName: "regatta/agent-1",
				Title:       "[agent-1] add foo",
				AuthorLogin: "forkuser",
			}},
		},
	}
	w, db := newTestWatcher(t, lister)
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != state.AgentPROpen {
		t.Fatalf("state=%s, want pr_open (title-prefix fallback)", got.State)
	}
	if got.PRSHA != "forksha" {
		t.Fatalf("pr_sha=%s, want forksha", got.PRSHA)
	}
}

// TestWatcher_ForkPRHead_RejectsImpersonator guards the issue #522 hijack vector.
func TestWatcher_ForkPRHead_RejectsImpersonator(t *testing.T) {
	// Fork user opens a PR titled `[agent-1] hijack` against an
	// unrelated head ref. The title-prefix fallback would match
	// without the head-suffix guard; the watcher must drop it.
	lister := &stubLister{
		byBranch:      map[string][]PullRequest{},
		byTitlePrefix: map[string][]PullRequest{
			"[agent-1]": {{
				Number:      99,
				HeadRefOid:  "evilsha",
				State:       "OPEN",
				HeadRefName: "topic/unrelated",
				Title:       "[agent-1] hijack",
				AuthorLogin: "attacker",
			}},
		},
	}
	w, db := newTestWatcher(t, lister)
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != state.AgentRunning {
		t.Fatalf("state=%s, want running (impersonator filtered)", got.State)
	}
	if got.PRSHA != "" {
		t.Fatalf("pr_sha=%s, want empty (no hijack)", got.PRSHA)
	}
}

// TestWatcher_ForkPRHead_AcceptsLegitimateAgentSuffix passes the head-suffix gate.
func TestWatcher_ForkPRHead_AcceptsLegitimateAgentSuffix(t *testing.T) {
	// Fork user re-uses the same agent suffix on their fork branch
	// (`forkuser/agent-1`); the head-suffix guard accepts it.
	lister := &stubLister{
		byBranch:      map[string][]PullRequest{},
		byTitlePrefix: map[string][]PullRequest{
			"[agent-1]": {{
				Number:      100,
				HeadRefOid:  "okSHA",
				State:       "OPEN",
				HeadRefName: "forkuser/agent-1",
				Title:       "[agent-1] real",
				AuthorLogin: "forkuser",
			}},
		},
	}
	w, db := newTestWatcher(t, lister)
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != state.AgentPROpen {
		t.Fatalf("state=%s, want pr_open", got.State)
	}
}

// TestWatcher_StartupProbe_RefusesOnOldGH covers issue #526.
func TestWatcher_StartupProbe_RefusesOnOldGH(t *testing.T) {
	cases := []struct {
		name     string
		ver      string
		probeErr error
		wantErr  bool
	}{
		{name: "below_floor", ver: "gh version 2.39.0 (2024-01-01)", wantErr: true},
		{name: "at_floor", ver: "gh version 2.40.0 (2024-02-01)", wantErr: false},
		{name: "above_floor", ver: "gh version 2.55.0 (2024-12-01)", wantErr: false},
		{name: "probe_error", probeErr: errors.New("not found"), wantErr: true},
		{name: "unparseable", ver: "weird output", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := statetest.OpenDB(t)
			w, err := New(Config{
				DB:           db,
				BranchFn:     branchFor,
				Lister:       &stubLister{},
				VersionProbe: stubVersion{ver: tc.ver, err: tc.probeErr},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			err = w.Start(context.Background())
			if tc.wantErr && err == nil {
				t.Fatalf("Start: got nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Start: got err=%v, want nil", err)
			}
		})
	}
}

// TestWatcher_HeadChanged_EmitsHeadChangedEvent guards the SHA-diff path.
func TestWatcher_HeadChanged_EmitsHeadChangedEvent(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 1, HeadRefOid: "sha-a", State: "OPEN"}},
	}}
	w, db := newTestWatcher(t, lister)
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	lister.mu.Lock()
	lister.byBranch["regatta/agent-1"] = []PullRequest{{Number: 1, HeadRefOid: "sha-b", State: "OPEN"}}
	lister.mu.Unlock()
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	events, err := db.ListEventsByKindSince(context.Background(), "agent_pr_head_changed", 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("agent_pr_head_changed=%d, want 1", len(events))
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PRSHA != "sha-b" {
		t.Fatalf("pr_sha=%s, want sha-b", got.PRSHA)
	}
}

// TestWatcher_RunningNoPR_NoTransition guards the wait-for-PR no-op.
func TestWatcher_RunningNoPR_NoTransition(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{}}
	w, db := newTestWatcher(t, lister)
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != state.AgentRunning {
		t.Fatalf("state=%s, want running", got.State)
	}
}

// TestWatcher_AmbiguousHead_PicksLowestPRNumber guards spec R1.
func TestWatcher_AmbiguousHead_PicksLowestPRNumber(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {
			{Number: 9, HeadRefOid: "sha-hi", State: "OPEN"},
			{Number: 3, HeadRefOid: "sha-lo", State: "OPEN"},
			{Number: 5, HeadRefOid: "sha-mid", State: "OPEN"},
		},
	}}
	w, db := newTestWatcher(t, lister)
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PRSHA != "sha-lo" {
		t.Fatalf("pr_sha=%s, want sha-lo (lowest PR number)", got.PRSHA)
	}
}

// TestVersionAtLeast covers the semver-floor comparator.
func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		got, floor string
		ok         bool
		wantErr    bool
	}{
		{got: "gh version 2.40.0", floor: "2.40.0", ok: true},
		{got: "2.39.9", floor: "2.40.0", ok: false},
		{got: "2.41.0", floor: "2.40.0", ok: true},
		{got: "3.0.0", floor: "2.40.0", ok: true},
		{got: "1.99.99", floor: "2.40.0", ok: false},
		{got: "no triple here", floor: "2.40.0", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.got+"_vs_"+tc.floor, func(t *testing.T) {
			ok, err := VersionAtLeast(tc.got, tc.floor)
			if tc.wantErr && err == nil {
				t.Fatalf("want err, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !tc.wantErr && ok != tc.ok {
				t.Fatalf("ok=%v, want %v", ok, tc.ok)
			}
		})
	}
}

// TestWatcher_ForkPRHead_RejectsAttackerSuffixWithoutAllowlist rejects `attacker-agent-1` under strict mode + empty allowlist (#587).
func TestWatcher_ForkPRHead_RejectsAttackerSuffixWithoutAllowlist(t *testing.T) {
	lister := &stubLister{
		byBranch: map[string][]PullRequest{},
		byTitlePrefix: map[string][]PullRequest{
			"[agent-1]": {{
				Number:      77,
				HeadRefOid:  "attacksha",
				State:       "OPEN",
				HeadRefName: "attacker-agent-1",
				Title:       "[agent-1] hijack",
				AuthorLogin: "attacker",
			}},
		},
	}
	db := statetest.OpenDB(t)
	// Empty allowlist + StrictForkAuthor=true ⇒ only literal branch match passes.
	w, err := New(Config{
		DB: db, BranchFn: branchFor, Lister: lister,
		StrictForkAuthor: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != state.AgentRunning {
		t.Fatalf("state=%s, want running (attacker suffix rejected under strict mode)", got.State)
	}
}

// TestWatcher_ForkPRHead_AcceptsAllowlistedAuthor accepts fork PRs from an allowlisted author under strict mode (#587).
func TestWatcher_ForkPRHead_AcceptsAllowlistedAuthor(t *testing.T) {
	lister := &stubLister{
		byBranch: map[string][]PullRequest{},
		byTitlePrefix: map[string][]PullRequest{
			"[agent-1]": {{
				Number:      88,
				HeadRefOid:  "trustedsha",
				State:       "OPEN",
				HeadRefName: "weirdname",
				Title:       "[agent-1] trusted",
				AuthorLogin: "operator",
			}},
		},
	}
	db := statetest.OpenDB(t)
	w, err := New(Config{
		DB: db, BranchFn: branchFor, Lister: lister,
		StrictForkAuthor:   true,
		AllowedForkAuthors: []string{"operator"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != state.AgentPROpen {
		t.Fatalf("state=%s, want pr_open (allowlisted author)", got.State)
	}
	if got.PRSHA != "trustedsha" {
		t.Fatalf("pr_sha=%s, want trustedsha", got.PRSHA)
	}
}

// TestWatcher_BranchRenameThreshold_Configurable fires `agent_branch_renamed` at a per-watcher override (#631).
func TestWatcher_BranchRenameThreshold_Configurable(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 7, HeadRefOid: "abc123", State: "OPEN"}},
	}}
	db := statetest.OpenDB(t)
	w, err := New(Config{
		DB: db, BranchFn: branchFor, Lister: lister,
		BranchRenameThreshold: 3, // override the default 12
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("initial sweep: %v", err)
	}
	// Branch renamed: 2 sub-threshold misses, then one threshold-crossing.
	lister.mu.Lock()
	lister.byBranch = map[string][]PullRequest{}
	lister.mu.Unlock()
	for i := 0; i < 2; i++ {
		if err := w.Sweep(context.Background()); err != nil {
			t.Fatalf("sub-threshold sweep %d: %v", i, err)
		}
	}
	pre, err := db.ListEventsByKindSince(context.Background(), "agent_branch_renamed", 0, 100)
	if err != nil {
		t.Fatalf("list events pre: %v", err)
	}
	if len(pre) != 0 {
		t.Fatalf("pre-threshold agent_branch_renamed=%d, want 0", len(pre))
	}
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("threshold sweep: %v", err)
	}
	got, err := db.ListEventsByKindSince(context.Background(), "agent_branch_renamed", 0, 100)
	if err != nil {
		t.Fatalf("list events post: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("agent_branch_renamed=%d, want 1 at threshold=3", len(got))
	}
	if !got[0].AgentID.Valid || got[0].AgentID.Int64 != a.ID {
		t.Fatalf("event agent_id=%v, want %d", got[0].AgentID, a.ID)
	}
}

// TestWatcher_BranchRenamedByAgent_LogsAndTransitions asserts WARN + pr_open on title-prefix rescue (#1047).
func TestWatcher_BranchRenamedByAgent_LogsAndTransitions(t *testing.T) {
	lister := &stubLister{
		byBranch: map[string][]PullRequest{},
		byTitlePrefix: map[string][]PullRequest{
			"[agent-1]": {{
				Number:      1045,
				HeadRefOid:  "renamedsha",
				State:       "OPEN",
				HeadRefName: "refactor/reviewer-verdict-modular-agent-1",
				Title:       "[agent-1] modularize reviewer verdict",
				AuthorLogin: "trilamsr",
			}},
		},
	}
	db := statetest.OpenDB(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	w, err := New(Config{DB: db, BranchFn: branchFor, Lister: lister, Logger: log})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != state.AgentPROpen {
		t.Fatalf("state=%s, want pr_open (title-prefix rescue)", got.State)
	}
	if got.PRSHA != "renamedsha" {
		t.Fatalf("pr_sha=%s, want renamedsha", got.PRSHA)
	}
	if !strings.Contains(buf.String(), "prwatch.branch_renamed_by_agent") {
		t.Fatalf("missing prwatch.branch_renamed_by_agent log; got:\n%s", buf.String())
	}
}

// TestWatcher_LiteralBranchMatch_DoesNotEmitBranchRenamed asserts silence on literal-branch happy path (#1047).
func TestWatcher_LiteralBranchMatch_DoesNotEmitBranchRenamed(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 42, HeadRefOid: "deadbeef", State: "OPEN"}},
	}}
	db := statetest.OpenDB(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	w, err := New(Config{DB: db, BranchFn: branchFor, Lister: lister, Logger: log})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if strings.Contains(buf.String(), "branch_renamed_by_agent") {
		t.Fatalf("unexpected branch_renamed_by_agent log on literal-branch match:\n%s", buf.String())
	}
}

// TestNew_MissingDeps surfaces wiring bugs at boot.
func TestNew_MissingDeps(t *testing.T) {
	db := statetest.OpenDB(t)
	cases := []struct {
		name string
		cfg  Config
		msg  string
	}{
		{name: "no_db", cfg: Config{BranchFn: branchFor, Lister: &stubLister{}}, msg: "DB"},
		{name: "no_branchfn", cfg: Config{DB: db, Lister: &stubLister{}}, msg: "BranchFn"},
		{name: "no_lister", cfg: Config{DB: db, BranchFn: branchFor}, msg: "Lister"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("err=%v, want mention of %q", err, tc.msg)
			}
		})
	}
}

// TestWatcher_BranchDiverged_EmitsWarnOncePerDivergenceSha asserts pr_open w/ local HEAD ≠ remote tip emits branch_diverged WARN once per (agent,sha) + no state advance (#1051).
func TestWatcher_BranchDiverged_EmitsWarnOncePerDivergenceSha(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 99, HeadRefOid: "remotesha", State: "OPEN"}},
	}}
	db := statetest.OpenDB(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	heads := map[int64]string{1: "remotesha"}
	probe := func(id int64) (string, bool) {
		sha, ok := heads[id]
		return sha, ok
	}
	w, err := New(Config{
		DB:           db,
		BranchFn:     branchFor,
		Lister:       lister,
		Logger:       log,
		LocalHeadFn:  probe,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a := driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if strings.Contains(buf.String(), "branch_diverged") {
		t.Fatalf("unexpected branch_diverged on aligned heads:\n%s", buf.String())
	}
	heads[1] = "localnewersha"
	preState, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get pre: %v", err)
	}
	if preState.State != state.AgentPROpen {
		t.Fatalf("pre state=%s, want pr_open", preState.State)
	}
	buf.Reset()
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("diverged sweep: %v", err)
	}
	if !strings.Contains(buf.String(), "prwatch.branch_diverged") {
		t.Fatalf("missing prwatch.branch_diverged WARN; got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "localnewersha") {
		t.Fatalf("WARN missing local sha; got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "remotesha") {
		t.Fatalf("WARN missing remote sha; got:\n%s", buf.String())
	}
	postState, err := db.GetAgent(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("get post: %v", err)
	}
	if postState.State != state.AgentPROpen {
		t.Fatalf("state advanced past pr_open after divergence: %s", postState.State)
	}
	buf.Reset()
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("dedupe sweep: %v", err)
	}
	if strings.Contains(buf.String(), "branch_diverged") {
		t.Fatalf("WARN re-fired on same (agent,sha) tuple; got:\n%s", buf.String())
	}
	heads[1] = "evennewersha"
	buf.Reset()
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("re-diverged sweep: %v", err)
	}
	if !strings.Contains(buf.String(), "branch_diverged") {
		t.Fatalf("WARN did not re-arm on new divergence sha; got:\n%s", buf.String())
	}
}

// TestWatcher_BranchDiverged_NilProbeStaysSilent asserts nil LocalHeadFn never emits branch_diverged (#1051).
func TestWatcher_BranchDiverged_NilProbeStaysSilent(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 1, HeadRefOid: "remotesha", State: "OPEN"}},
	}}
	w, db := newTestWatcher(t, lister)
	driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep 2: %v", err)
	}
}

// TestWatcher_BranchDiverged_AbsentLocalSkipsWarn asserts probe ok=false (worktree gone) suppresses WARN (#1051).
func TestWatcher_BranchDiverged_AbsentLocalSkipsWarn(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 1, HeadRefOid: "remotesha", State: "OPEN"}},
	}}
	db := statetest.OpenDB(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	probe := func(int64) (string, bool) { return "", false }
	w, err := New(Config{
		DB: db, BranchFn: branchFor, Lister: lister, Logger: log,
		LocalHeadFn: probe,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	driveToRunning(t, db, "WORK-1")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep 1: %v", err)
	}
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep 2: %v", err)
	}
	if strings.Contains(buf.String(), "branch_diverged") {
		t.Fatalf("WARN fired despite absent local probe; got:\n%s", buf.String())
	}
}
