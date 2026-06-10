package prwatch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeRunner is a hermetic os/exec replacement. Keyed by the joined
// args so a test can predetermine output per gh invocation.
type fakeRunner struct {
	mu   sync.Mutex
	out  map[string][]byte
	err  map[string]error
	args [][]string
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := name + " " + strings.Join(args, " ")
	f.args = append(f.args, append([]string{name}, args...))
	// gh's real exec.Output() returns BOTH stdout AND a non-nil error
	// on non-zero exit. Tests model that by populating `out` AND `err`
	// under the same key; the runner returns both so the lister can
	// classify exit-4 + empty stdout as "no PR found".
	if e, ok := f.err[key]; ok {
		return f.out[key], e
	}
	return f.out[key], nil
}

// fakeExitErr satisfies the ExitCoder interface the lister consults
// to classify gh exit codes. `*exec.ExitError` from os/exec is the
// production type; this stand-in keeps tests hermetic without
// invoking a real subprocess to synthesize an ExitError.
type fakeExitErr struct{ code int }

func (e *fakeExitErr) Error() string { return "exit " + itoa(e.code) }
func (e *fakeExitErr) ExitCode() int { return e.code }

// itoa is a tiny strconv-free helper so the test file does not pull
// in another import for a single integer-format call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestGHCLILister_HeadMatch decodes the same-repo happy path.
func TestGHCLILister_HeadMatch(t *testing.T) {
	r := &fakeRunner{out: map[string][]byte{
		"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields: []byte(
			`[{"number":42,"headRefOid":"deadbeef","state":"OPEN","headRefName":"regatta/agent-7","title":"[agent-7] x","author":{"login":"me"}}]`,
		),
		"gh pr list --state open --search in:title [agent-7] --json " + ghJSONFields: []byte(`[]`),
	}}
	g := &GHCLILister{Runner: r.run}
	prs, err := g.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(prs) != 1 || prs[0].HeadRefOid != "deadbeef" || prs[0].AuthorLogin != "me" {
		t.Fatalf("prs=%+v", prs)
	}
}

// TestGHCLILister_ForkFallback exercises the title-prefix rescue path.
func TestGHCLILister_ForkFallback(t *testing.T) {
	r := &fakeRunner{out: map[string][]byte{
		"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields:                []byte(`[]`),
		"gh pr list --state open --search in:title [agent-7] --json " + ghJSONFields: []byte(
			`[{"number":99,"headRefOid":"forksha","state":"OPEN","headRefName":"topic","title":"[agent-7] fork PR","author":{"login":"forkuser"}}]`,
		),
	}}
	g := &GHCLILister{Runner: r.run}
	prs, err := g.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(prs) != 1 || prs[0].HeadRefOid != "forksha" || prs[0].AuthorLogin != "forkuser" {
		t.Fatalf("fork fallback failed: prs=%+v", prs)
	}
}

// TestGHCLILister_TitlePrefixMismatch_Filtered drops substring-only matches.
func TestGHCLILister_TitlePrefixMismatch_Filtered(t *testing.T) {
	// gh `--search in:title` matches substrings, so a PR titled
	// "fix something [agent-7]" leaks through; the lister filters
	// to *prefix* matches so an unrelated PR that mentions the
	// agent token mid-title does not pollute the correlation.
	r := &fakeRunner{out: map[string][]byte{
		"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields:                []byte(`[]`),
		"gh pr list --state open --search in:title [agent-7] --json " + ghJSONFields: []byte(
			`[{"number":50,"title":"fix [agent-7] thing","headRefOid":"x","state":"OPEN","author":{"login":"u"}}]`,
		),
	}}
	g := &GHCLILister{Runner: r.run}
	prs, err := g.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(prs) != 0 {
		t.Fatalf("prs=%+v, want filtered to 0 (non-prefix substring)", prs)
	}
}

// TestGHCLILister_HeadError_FailsFast propagates the underlying error.
func TestGHCLILister_HeadError_FailsFast(t *testing.T) {
	r := &fakeRunner{err: map[string]error{
		"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields: errors.New("network down"),
	}}
	g := &GHCLILister{Runner: r.run}
	_, err := g.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
	if err == nil {
		t.Fatalf("want err, got nil")
	}
}

// TestGHCLILister_Exit4EmptyStdout_NoPRFound is the regression for the
// `prwatch.list_failed` warning storm. gh exits 4 with empty (or
// `[]`) stdout when `--head <branch>` matches zero PRs — a NORMAL
// state for agents whose branch was never pushed. The lister must
// classify that case as (nil, nil) so the watcher does not emit a
// per-tick WARN. Non-empty stdout under exit 4 indicates a real gh
// error (auth, repo not found) and is still surfaced as an error.
func TestGHCLILister_Exit4EmptyStdout_NoPRFound(t *testing.T) {
	t.Run("empty stdout returns nil, nil", func(t *testing.T) {
		r := &fakeRunner{
			out: map[string][]byte{
				"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields:                    []byte(``),
				"gh pr list --state open --search in:title [agent-7] --json " + ghJSONFields: []byte(`[]`),
			},
			err: map[string]error{
				"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields: &fakeExitErr{code: 4},
			},
		}
		g := &GHCLILister{Runner: r.run}
		prs, err := g.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
		if err != nil {
			t.Fatalf("want nil err on exit-4 + empty stdout (no PR found), got %v", err)
		}
		if len(prs) != 0 {
			t.Fatalf("want 0 prs, got %+v", prs)
		}
	})

	t.Run("[] stdout returns nil, nil", func(t *testing.T) {
		r := &fakeRunner{
			out: map[string][]byte{
				"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields:                    []byte(`[]`),
				"gh pr list --state open --search in:title [agent-7] --json " + ghJSONFields: []byte(`[]`),
			},
			err: map[string]error{
				"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields: &fakeExitErr{code: 4},
			},
		}
		g := &GHCLILister{Runner: r.run}
		prs, err := g.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
		if err != nil {
			t.Fatalf("want nil err on exit-4 + `[]` stdout, got %v", err)
		}
		if len(prs) != 0 {
			t.Fatalf("want 0 prs, got %+v", prs)
		}
	})

	t.Run("exit 4 with non-empty stdout is a real error", func(t *testing.T) {
		// gh exits 4 with a stderr/stdout payload for auth + repo-not-found
		// failures. Non-`[]` stdout signals "real" failure; the lister
		// must still propagate so the watcher WARNs.
		r := &fakeRunner{
			out: map[string][]byte{
				"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields: []byte(`HTTP 401: Bad credentials`),
			},
			err: map[string]error{
				"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields: &fakeExitErr{code: 4},
			},
		}
		g := &GHCLILister{Runner: r.run}
		_, err := g.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
		if err == nil {
			t.Fatalf("want err on exit-4 + non-empty stdout, got nil")
		}
	})

	t.Run("non-exit-4 error is propagated", func(t *testing.T) {
		// e.g. context deadline, network failure — these are not
		// "blank-slate" exit-4 and must still surface as errors.
		r := &fakeRunner{
			out: map[string][]byte{
				"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields: []byte(``),
			},
			err: map[string]error{
				"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields: &fakeExitErr{code: 1},
			},
		}
		g := &GHCLILister{Runner: r.run}
		_, err := g.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
		if err == nil {
			t.Fatalf("want err on non-exit-4, got nil")
		}
	})
}

// TestGHCLIVersionProbe_ParsesFirstLine returns the trimmed first line.
func TestGHCLIVersionProbe_ParsesFirstLine(t *testing.T) {
	r := &fakeRunner{out: map[string][]byte{
		"gh --version": []byte("gh version 2.55.0 (2024-12-01)\nhttps://github.com/cli/cli/releases/tag/v2.55.0\n"),
	}}
	p := &GHCLIVersionProbe{Runner: r.run}
	got, err := p.Version(context.Background())
	if err != nil {
		t.Fatalf("ver: %v", err)
	}
	if !strings.Contains(got, "2.55.0") {
		t.Fatalf("got %q, want substring 2.55.0", got)
	}
}

// TestDedupePRs preserves first occurrence ordering.
func TestDedupePRs(t *testing.T) {
	prs := []PullRequest{
		{Number: 1, HeadRefOid: "a"},
		{Number: 2, HeadRefOid: "b"},
		{Number: 1, HeadRefOid: "a"},
	}
	got := dedupePRs(prs)
	if len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
		t.Fatalf("got=%+v", got)
	}
}
