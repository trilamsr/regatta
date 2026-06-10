package prwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ghJSONFields pins the fields the watcher consumes. Kept as a
// constant so a future gh schema-drift case (issue R4 in the spec)
// surfaces as a unit-test failure rather than a silent zero-value.
const ghJSONFields = "number,headRefOid,state,headRefName,title,author,mergeStateStatus"

// GHCLILister shells the gh CLI for the watcher's PR query. Production
// wiring; tests use the in-memory stubLister.
//
// Fork-PR fallback (issue #522): when --head returns no results the
// lister re-queries with `--search "in:title <prefix>"`. The fork
// case is the only one where `--head regatta/agent-N` fails — gh's
// --head filter does not match owner-qualified head refs from PRs
// pushed to a fork.
type GHCLILister struct {
	// Runner is the exec seam. Production wires exec.CommandContext;
	// tests inject an in-memory fake.
	Runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewGHCLILister returns a lister wired to os/exec. Used by serve.go.
func NewGHCLILister() *GHCLILister {
	return &GHCLILister{Runner: defaultExec}
}

// ListOpenByHead implements PRLister. Returns the deduplicated union
// of the --head query and the title-prefix fallback.
func (g *GHCLILister) ListOpenByHead(ctx context.Context, branch, titlePrefix string) ([]PullRequest, error) {
	headPRs, err := g.queryHead(ctx, branch)
	if err != nil {
		return nil, err
	}
	if titlePrefix == "" {
		return headPRs, nil
	}
	// Always run the title-prefix query: a fork-pushed PR + a
	// same-repo PR may both legitimately reference the same agent
	// during a branch-handoff window. The union resolves the case
	// the spec's §3.3 R1 mitigation (lowest PR number wins) already
	// handles. A title-query failure surfaces to the watcher so the
	// per-agent error log captures it (fork visibility lost); the
	// watcher's per-agent isolation keeps a single failure from
	// aborting the sweep.
	titlePRs, err := g.queryTitle(ctx, titlePrefix)
	if err != nil {
		return headPRs, err
	}
	return dedupePRs(append(headPRs, titlePRs...)), nil
}

func (g *GHCLILister) queryHead(ctx context.Context, branch string) ([]PullRequest, error) {
	out, err := g.Runner(ctx, "gh", "pr", "list",
		"--state", "open",
		"--head", branch,
		"--json", ghJSONFields,
	)
	if err != nil {
		if isBlankSlateExit(err, out) {
			// Normal state: agent has not pushed the branch yet (or it
			// was already merged + deleted). gh emits exit-4 + empty
			// (or `[]`) stdout. Swallow to silence the
			// `prwatch.list_failed` warning storm; real failures (auth,
			// repo-not-found) carry a non-empty stdout payload and
			// still surface as errors.
			return nil, nil
		}
		return nil, fmt.Errorf("prwatch: gh pr list --head %s: %w", branch, err)
	}
	return decodePRs(out)
}

func (g *GHCLILister) queryTitle(ctx context.Context, titlePrefix string) ([]PullRequest, error) {
	out, err := g.Runner(ctx, "gh", "pr", "list",
		"--state", "open",
		"--search", "in:title "+titlePrefix,
		"--json", ghJSONFields,
	)
	if err != nil {
		if isBlankSlateExit(err, out) {
			return nil, nil
		}
		return nil, fmt.Errorf("prwatch: gh pr list --search title %s: %w", titlePrefix, err)
	}
	prs, err := decodePRs(out)
	if err != nil {
		return nil, err
	}
	// `--search "in:title X"` matches substring; restrict to PRs
	// whose title actually carries the prefix, so an unrelated PR
	// that happens to mention `[agent-7]` mid-title does not match.
	out2 := prs[:0]
	for _, pr := range prs {
		if strings.HasPrefix(pr.Title, titlePrefix) {
			out2 = append(out2, pr)
		}
	}
	return out2, nil
}

// ghAuthorWrap mirrors gh's nested author shape so decodePRs can
// flatten authorLogin into the local PullRequest.AuthorLogin field.
type ghAuthorWrap struct {
	Number      int    `json:"number"`
	HeadRefOid  string `json:"headRefOid"`
	State       string `json:"state"`
	HeadRefName string `json:"headRefName"`
	Title       string `json:"title"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
	MergeStateStatus string `json:"mergeStateStatus"`
}

func decodePRs(raw []byte) ([]PullRequest, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var wrapped []ghAuthorWrap
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("prwatch: decode gh json: %w", err)
	}
	out := make([]PullRequest, len(wrapped))
	for i, w := range wrapped {
		out[i] = PullRequest{
			Number:           w.Number,
			HeadRefOid:       w.HeadRefOid,
			State:            w.State,
			HeadRefName:      w.HeadRefName,
			Title:            w.Title,
			AuthorLogin:      w.Author.Login,
			MergeStateStatus: w.MergeStateStatus,
		}
	}
	return out, nil
}

func dedupePRs(prs []PullRequest) []PullRequest {
	if len(prs) < 2 {
		return prs
	}
	seen := make(map[int]struct{}, len(prs))
	out := prs[:0]
	for _, pr := range prs {
		if _, ok := seen[pr.Number]; ok {
			continue
		}
		seen[pr.Number] = struct{}{}
		out = append(out, pr)
	}
	return out
}

// GHCLIVersionProbe shells `gh --version` and returns the first
// semver triple it finds. Production GHVersionProbe; tests inject a
// stub.
type GHCLIVersionProbe struct {
	Runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewGHCLIVersionProbe returns a probe wired to os/exec.
func NewGHCLIVersionProbe() *GHCLIVersionProbe {
	return &GHCLIVersionProbe{Runner: defaultExec}
}

// Version executes `gh --version` and returns the raw first line.
// The Watcher.Start caller parses out the semver triple.
func (p *GHCLIVersionProbe) Version(ctx context.Context) (string, error) {
	out, err := p.Runner(ctx, "gh", "--version")
	if err != nil {
		return "", fmt.Errorf("prwatch: exec gh --version: %w", err)
	}
	// `gh --version` first line is `gh version X.Y.Z (...)`; the
	// rest is build metadata the caller does not need.
	line := string(bytes.SplitN(out, []byte{'\n'}, 2)[0])
	return strings.TrimSpace(line), nil
}

// exitCoder is the minimal interface satisfied by `*exec.ExitError`
// (production) and by the test stand-in. Using an interface avoids
// the awkward construction of a real `*exec.ExitError` from a fake
// in tests while still matching the production type via `errors.As`.
type exitCoder interface {
	ExitCode() int
}

// isBlankSlateExit reports whether the (err, stdout) pair from a `gh
// pr list` invocation means "no PR found" rather than a real failure.
// gh exits 4 with empty (or `[]`) stdout when the query matches zero
// PRs — a normal state for agents whose branch was never pushed.
// Non-empty stdout under exit-4 carries the real failure payload
// (auth, repo-not-found) and is treated as a true error so the
// watcher logs it. Any non-exit-4 error (network, context deadline)
// is also a true error.
//
// The conflation between "no match" and "real failure" was the source
// of the `prwatch.list_failed` per-tick warning storm tracked in the
// orchestrator observability spec.
func isBlankSlateExit(err error, stdout []byte) bool {
	var ec exitCoder
	if !errors.As(err, &ec) || ec.ExitCode() != 4 {
		return false
	}
	trimmed := bytes.TrimSpace(stdout)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("[]"))
}

// defaultExec is the production Runner. The gh CLI binary name +
// args are construction-controlled (literal strings inside this
// package), not operator-supplied, so the gosec G204 warning is a
// false positive — mirrors how rejectionrouter.GHLabeler shells gh.
//
//nolint:gosec // gh CLI + literal-arg shell-out (issue #526)
func defaultExec(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}
