package merge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Runner abstracts the gh-CLI shell-out so prober tests stay
// process-fork-free. Production wires execRunner; tests substitute a
// canned-stdout/stderr fake.
type Runner interface {
	Run(ctx context.Context, args []string) (stdout, stderr []byte, err error)
}

// GhProber is the production PRProber: shells out to
// `gh pr view <N> --json state,mergedAt,headRefOid,mergeCommit` and
// maps the response onto a ProbeResult. Replaces the noopMergeProber
// c2 shipped (#613) so Reconcile drives the FSM off real PR state.
//
// The 30s timeout caps wall-clock for the worst GitHub-API edge; the
// JSON allowlist pins the request shape so a future gh schema change
// surfaces as a parse error rather than silently widening payload size.
type GhProber struct {
	runner  Runner
	timeout time.Duration
}

// NewGhProber wires a prober against the supplied runner. Empty runner
// falls back to the production exec.Command shell-out so production
// wiring stays one-liner clean.
func NewGhProber(r Runner) *GhProber {
	if r == nil {
		r = execRunner{}
	}
	return &GhProber{runner: r, timeout: 30 * time.Second}
}

// ghPRView mirrors the JSON shape `gh pr view --json` emits. Fields
// not in the allowlist are absent from the response.
type ghPRView struct {
	State       string `json:"state"`       // "OPEN" | "CLOSED" | "MERGED"
	MergedAt    string `json:"mergedAt"`    // ISO-8601 or "" when unmerged
	HeadRefOid  string `json:"headRefOid"`  // current head SHA
	MergeCommit struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
}

// Probe asks GitHub for the current state of prNumber and reduces gh's
// response onto a ProbeResult. The SHA-diverged decision is local —
// gh returns headRefOid; the prober compares against the intent's SHA.
//
// 404 ("Could not resolve to a PullRequest") is treated as merged-via-
// deleted-branch — the post-merge gh branch reaper deletes the head
// ref, and the spec §4.3 completion-event semantics make
// success-via-error the right reduce for the FSM (#655).
//
// Transient errors (network, rate limit, auth) surface as
// PRStatusUnknown + non-nil err so Reconcile leaves the agent in
// AwaitingMerge for the next sweep.
func (p *GhProber) Probe(ctx context.Context, prNumber int, expectedSHA string) (ProbeResult, error) {
	cctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	args := []string{"pr", "view", strconv.Itoa(prNumber),
		"--json", "state,mergedAt,headRefOid,mergeCommit"}
	stdout, stderr, err := p.runner.Run(cctx, args)
	if err != nil {
		if is404(stderr) {
			// gh reaper deleted the branch post-merge → equivalent to
			// merged for FSM purposes; no MergeSHA available.
			return ProbeResult{Status: PRStatusMerged}, nil
		}
		return ProbeResult{Status: PRStatusUnknown},
			fmt.Errorf("merge: gh pr view %d: %w (%s)", prNumber, err, strings.TrimSpace(string(stderr)))
	}
	var v ghPRView
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &v); err != nil {
		return ProbeResult{Status: PRStatusUnknown},
			fmt.Errorf("merge: decode gh pr view %d: %w", prNumber, err)
	}
	switch strings.ToUpper(v.State) {
	case "MERGED":
		return ProbeResult{Status: PRStatusMerged, MergeSHA: v.MergeCommit.OID}, nil
	case "CLOSED":
		return ProbeResult{Status: PRStatusClosedUnmerged}, nil
	case "OPEN":
		if v.HeadRefOid == expectedSHA {
			return ProbeResult{Status: PRStatusOpenSHAMatches}, nil
		}
		return ProbeResult{Status: PRStatusOpenSHADiverged}, nil
	}
	return ProbeResult{Status: PRStatusUnknown},
		fmt.Errorf("merge: gh pr view %d: unrecognized state %q", prNumber, v.State)
}

// is404 detects gh's "Could not resolve to a PullRequest" stderr.
// Stable substring across gh ≥ 2.40; future gh versions that reword
// will degrade to PRStatusUnknown (next sweep retries).
func is404(stderr []byte) bool {
	s := strings.ToLower(string(stderr))
	return strings.Contains(s, "could not resolve to a pullrequest") ||
		strings.Contains(s, "no pull requests found")
}

// execRunner is the production Runner — shells out to gh.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, args []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...) //nolint:gosec // G204: literal binary; args from intent row (int + allowlisted strings)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
