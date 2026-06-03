// prober_test pins GhProber's stdout/stderr → ProbeResult reducer.
// Replaces the noopMergeProber c2 shipped with so the Reconcile sweep
// drives the FSM off real PR state (#613).
package merge_test

import (
	"context"
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
)

// fakeRunner returns canned gh-CLI output keyed on PR number.
type fakeRunner struct {
	stdout []byte
	stderr []byte
	err    error
	args   []string
}

func (f *fakeRunner) Run(_ context.Context, args []string) ([]byte, []byte, error) {
	f.args = args
	return f.stdout, f.stderr, f.err
}

// TestGhProber_Merged_ReturnsMergedOutcome asserts state=MERGED + matching headRefOid yields PRStatusMerged (#613).
func TestGhProber_Merged_ReturnsMergedOutcome(t *testing.T) {
	r := &fakeRunner{stdout: []byte(`{"state":"MERGED","mergedAt":"2026-06-01T12:00:00Z","headRefOid":"abc123","mergeCommit":{"oid":"deadbeef"}}`)}
	p := merge.NewGhProber(r)
	got, err := p.Probe(context.Background(), 42, "abc123")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got.Status != merge.PRStatusMerged {
		t.Fatalf("status=%v; want %v", got.Status, merge.PRStatusMerged)
	}
	if got.MergeSHA != "deadbeef" {
		t.Fatalf("merge_sha=%q; want deadbeef", got.MergeSHA)
	}
}

// TestGhProber_OpenSHADiverged_ReturnsTerminal asserts OPEN + non-matching headRefOid → PRStatusOpenSHADiverged (#613).
func TestGhProber_OpenSHADiverged_ReturnsTerminal(t *testing.T) {
	r := &fakeRunner{stdout: []byte(`{"state":"OPEN","headRefOid":"newSHA"}`)}
	p := merge.NewGhProber(r)
	got, err := p.Probe(context.Background(), 42, "oldSHA")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got.Status != merge.PRStatusOpenSHADiverged {
		t.Fatalf("status=%v; want %v", got.Status, merge.PRStatusOpenSHADiverged)
	}
}

// TestGhProber_OpenSHAMatches_ReturnsMatches asserts OPEN + matching SHA → PRStatusOpenSHAMatches.
func TestGhProber_OpenSHAMatches_ReturnsMatches(t *testing.T) {
	r := &fakeRunner{stdout: []byte(`{"state":"OPEN","headRefOid":"abc"}`)}
	p := merge.NewGhProber(r)
	got, err := p.Probe(context.Background(), 7, "abc")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got.Status != merge.PRStatusOpenSHAMatches {
		t.Fatalf("status=%v; want %v", got.Status, merge.PRStatusOpenSHAMatches)
	}
}

// TestGhProber_PRClosed_ReturnsClosedUnmerged asserts CLOSED-without-merge → PRStatusClosedUnmerged (#613).
func TestGhProber_PRClosed_ReturnsClosedUnmerged(t *testing.T) {
	r := &fakeRunner{stdout: []byte(`{"state":"CLOSED","mergedAt":null,"headRefOid":"abc"}`)}
	p := merge.NewGhProber(r)
	got, err := p.Probe(context.Background(), 1, "abc")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got.Status != merge.PRStatusClosedUnmerged {
		t.Fatalf("status=%v; want %v", got.Status, merge.PRStatusClosedUnmerged)
	}
}

// TestGhProber_404_ReturnsMerged asserts gh's "could not resolve" 404 → success (branch deleted post-merge) (#613).
func TestGhProber_404_ReturnsMerged(t *testing.T) {
	r := &fakeRunner{
		stderr: []byte("GraphQL: Could not resolve to a PullRequest with the number of 999."),
		err:    errors.New("exit status 1"),
	}
	p := merge.NewGhProber(r)
	got, err := p.Probe(context.Background(), 999, "anySHA")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// 404 = post-merge branch reaper deleted ref; equivalent to Merged
	// for FSM purposes (Reconcile drives Done).
	if got.Status != merge.PRStatusMerged {
		t.Fatalf("status=%v; want %v (404 → merged-via-deleted-branch)", got.Status, merge.PRStatusMerged)
	}
}

// TestGhProber_NetworkError_TransientReturn asserts a non-404 gh error surfaces as PRStatusUnknown + non-nil err so Reconcile retries (#613).
func TestGhProber_NetworkError_TransientReturn(t *testing.T) {
	r := &fakeRunner{
		stderr: []byte("dial tcp: lookup api.github.com: no such host"),
		err:    errors.New("exit status 1"),
	}
	p := merge.NewGhProber(r)
	got, err := p.Probe(context.Background(), 5, "sha")
	if err == nil {
		t.Fatalf("err=nil; want non-nil so Reconcile leaves agent in awaiting_merge")
	}
	if got.Status != merge.PRStatusUnknown {
		t.Fatalf("status=%v; want %v (transient → unknown)", got.Status, merge.PRStatusUnknown)
	}
}

// TestGhProber_ArgsShape asserts the gh CLI invocation pins the JSON field allowlist (token-budget rule).
func TestGhProber_ArgsShape(t *testing.T) {
	r := &fakeRunner{stdout: []byte(`{"state":"OPEN","headRefOid":"x"}`)}
	p := merge.NewGhProber(r)
	if _, err := p.Probe(context.Background(), 17, "x"); err != nil {
		t.Fatalf("probe: %v", err)
	}
	// Must include explicit --json allowlist per CLAUDE.md gh-minimal-fields.
	want := []string{"pr", "view", "17", "--json", "state,mergedAt,headRefOid,mergeCommit"}
	if len(r.args) < len(want) {
		t.Fatalf("args=%v; want at least %v", r.args, want)
	}
	for i, w := range want {
		if r.args[i] != w {
			t.Fatalf("args[%d]=%q; want %q (full args=%v)", i, r.args[i], w, r.args)
		}
	}
}
