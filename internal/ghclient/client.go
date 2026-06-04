// Package ghclient is the shared seam between regatta packages and the
// GitHub REST API. Consumers (alarmwebhook handler, selfimprove
// detector) depend on Client; production wires the HTTP impl in
// alarmwebhook.NewHTTPGitHubClient. Issue carries the union of fields
// every consumer reads — Body for the selfimprove dedup-key marker,
// Title for human-readable logging, Number for the dedup target.
package ghclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Client is the unified GitHub seam every regatta consumer needs:
// list-by-label for dedup, create for new findings, comment for refires,
// and the paginated/get/edit triple the github_issues spec adapter
// (MVR-1-T4) uses for autonomous-issue consumption. Adding a method
// here forces every implementation to grow together — that is the
// unification's load-bearing guarantee.
type Client interface {
	ListOpenIssuesByLabel(ctx context.Context, label, titleSubstr string) ([]Issue, error)
	CreateIssue(ctx context.Context, title, body string, labels []string) (int, error)
	CommentOnIssue(ctx context.Context, number int, body string) error
	ListIssuesByLabelPaginated(ctx context.Context, label string, opts ListIssuesOpts) ([]Issue, error)
	GetIssue(ctx context.Context, number int) (Issue, error)
	EditIssueBody(ctx context.Context, number int, body string) error
}

// ListIssuesOpts tunes ListIssuesByLabelPaginated; zero State means
// "open" and zero Limit means the implementation default (1000 for
// gh-CLI-backed clients).
type ListIssuesOpts struct {
	State string
	Limit int
}

// Issue is the trimmed view of a GitHub issue every consumer reads.
// Body is what selfimprove scans for the `dedup-key:` marker and what
// the github_issues spec adapter parses for the regatta metadata block;
// Title + Number are what alarmwebhook logs against. Labels + UpdatedAt
// were added for the github_issues adapter (MVR-1-T4) — additive only.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Labels    []string  `json:"labels,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// Runner is the gh-CLI subprocess seam — production wires execRunner, tests inject fixtures so stderr classification (#848) runs without a real gh binary.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// RateLimitedError wraps schemas.ErrRateLimited so callers recover the parsed X-RateLimit-Reset hint via errors.As (spec §7.4).
type RateLimitedError struct {
	Hint schemas.RateLimitHint
	Raw  string
}

// Error renders the wrapped sentinel so errors.Is(err, schemas.ErrRateLimited) holds.
func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("%s: retry_after=%s", schemas.ErrRateLimited.Error(), e.Hint.RetryAfter)
}

// Unwrap returns the sentinel for errors.Is chain traversal.
func (e *RateLimitedError) Unwrap() error { return schemas.ErrRateLimited }

// GHCLIClient is the gh-CLI subprocess backing for the github_issues spec adapter — stderr capture closes spec §7.4 / §7.2 (#848).
type GHCLIClient struct {
	owner  string
	repo   string
	runner Runner
}

// NewGHCLIClient constructs the gh-CLI-backed Client with the production exec runner.
func NewGHCLIClient(owner, name string) *GHCLIClient {
	return &GHCLIClient{owner: owner, repo: name, runner: execRunner{}}
}

// NewGHCLIClientWithRunner is the test-only constructor that injects a Runner fixture.
func NewGHCLIClientWithRunner(owner, name string, r Runner) *GHCLIClient {
	return &GHCLIClient{owner: owner, repo: name, runner: r}
}

// ListOpenIssuesByLabel is unimplemented on the gh-CLI client — alarmwebhook path returns ErrAdapterUnsupported so misroutes fail loud.
func (c *GHCLIClient) ListOpenIssuesByLabel(_ context.Context, _, _ string) ([]Issue, error) {
	return nil, fmt.Errorf("%w: ListOpenIssuesByLabel via gh CLI", schemas.ErrAdapterUnsupported)
}

// CreateIssue is unimplemented on the gh-CLI client — alarm path only.
func (c *GHCLIClient) CreateIssue(_ context.Context, _, _ string, _ []string) (int, error) {
	return 0, fmt.Errorf("%w: CreateIssue via gh CLI", schemas.ErrAdapterUnsupported)
}

// CommentOnIssue posts a comment via `gh issue comment` and classifies stderr so 403/404 surface as typed errors.
func (c *GHCLIClient) CommentOnIssue(ctx context.Context, n int, body string) error {
	_, stderr, err := c.runner.Run(ctx, "gh", "issue", "comment", strconv.Itoa(n),
		"--repo", c.owner+"/"+c.repo, "--body", body)
	if err != nil {
		return classifyGHCLIError(stderr, err, fmt.Sprintf("gh issue comment %d", n))
	}
	return nil
}

// ListIssuesByLabelPaginated lists issues via `gh issue list` and classifies stderr so X-RateLimit-Reset surfaces as RateLimitedError (#848).
func (c *GHCLIClient) ListIssuesByLabelPaginated(ctx context.Context, label string, opts ListIssuesOpts) ([]Issue, error) {
	state := opts.State
	if state == "" {
		state = "open"
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}
	stdout, stderr, err := c.runner.Run(ctx, "gh", "issue", "list",
		"--repo", c.owner+"/"+c.repo,
		"--label", label,
		"--state", state,
		"--json", "number,title,body,labels,updatedAt",
		"--limit", strconv.Itoa(limit),
	)
	if err != nil {
		return nil, classifyGHCLIError(stderr, err, "gh issue list")
	}
	return decodeGHIssues(stdout)
}

// GetIssue fetches one issue via `gh issue view` — HTTP 404 stderr maps to ErrNotFound per spec §7.2 so the adapter can tombstone-WARN.
func (c *GHCLIClient) GetIssue(ctx context.Context, n int) (Issue, error) {
	stdout, stderr, err := c.runner.Run(ctx, "gh", "issue", "view", strconv.Itoa(n),
		"--repo", c.owner+"/"+c.repo,
		"--json", "number,title,body,labels,updatedAt",
	)
	if err != nil {
		return Issue{}, classifyGHCLIError(stderr, err, fmt.Sprintf("gh issue view %d", n))
	}
	issues, err := decodeGHIssuesOrSingle(stdout)
	if err != nil || len(issues) == 0 {
		return Issue{}, fmt.Errorf("gh issue view %d: decode: %w", n, err)
	}
	return issues[0], nil
}

// EditIssueBody backfills the dedup-key marker via `gh issue edit` with the same stderr-classifier as the other write paths.
func (c *GHCLIClient) EditIssueBody(ctx context.Context, n int, body string) error {
	_, stderr, err := c.runner.Run(ctx, "gh", "issue", "edit", strconv.Itoa(n),
		"--repo", c.owner+"/"+c.repo,
		"--body", body,
	)
	if err != nil {
		return classifyGHCLIError(stderr, err, fmt.Sprintf("gh issue edit %d", n))
	}
	return nil
}

// rateLimitResetRe / http403Re / http404Re match the gh CLI verbose-stderr response-header + status-line lines (case-insensitive).
var (
	rateLimitResetRe = regexp.MustCompile(`(?i)X-RateLimit-Reset:\s*(\d+)`)
	http403Re        = regexp.MustCompile(`(?i)\bHTTP 403\b`)
	http404Re        = regexp.MustCompile(`(?i)\bHTTP 404\b`)
)

// classifyGHCLIError maps a non-zero gh exit's stderr to a wrapped sentinel: 403+X-RateLimit-Reset→*RateLimitedError, 404→ErrNotFound, else original (#848).
func classifyGHCLIError(stderr []byte, runErr error, ctxLabel string) error {
	s := string(stderr)
	if http403Re.MatchString(s) {
		if m := rateLimitResetRe.FindStringSubmatch(s); len(m) == 2 {
			ts, parseErr := strconv.ParseInt(m[1], 10, 64)
			if parseErr == nil {
				resetAt := time.Unix(ts, 0)
				retry := time.Until(resetAt)
				if retry < time.Second {
					retry = time.Second
				}
				return &RateLimitedError{Hint: schemas.RateLimitHint{RetryAfter: retry, Reset: resetAt}, Raw: truncate(s, 256)}
			}
		}
	}
	if http404Re.MatchString(s) {
		return fmt.Errorf("%s: %w", ctxLabel, schemas.ErrNotFound)
	}
	return fmt.Errorf("%s: %w (stderr=%s)", ctxLabel, runErr, truncate(s, 256))
}

// truncate caps stderr in error strings — gh CLI can spill kilobytes on verbose failures.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// execRunner is the production Runner; CombinedOutput would lose the stderr/stdout boundary classifyGHCLIError needs.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: argv-style, no shell; owner/repo CUE-validated upstream
	stdout, err := cmd.Output()
	var stderr []byte
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr = exitErr.Stderr
	}
	return stdout, stderr, err
}

type ghIssueRaw struct {
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	Labels    []ghLabelRaw `json:"labels"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

type ghLabelRaw struct {
	Name string `json:"name"`
}

func decodeGHIssues(out []byte) ([]Issue, error) {
	var raws []ghIssueRaw
	if err := json.Unmarshal(out, &raws); err != nil {
		return nil, err
	}
	return flattenGHIssues(raws), nil
}

func decodeGHIssuesOrSingle(out []byte) ([]Issue, error) {
	var single ghIssueRaw
	if err := json.Unmarshal(out, &single); err != nil {
		return decodeGHIssues(out)
	}
	return flattenGHIssues([]ghIssueRaw{single}), nil
}

func flattenGHIssues(raws []ghIssueRaw) []Issue {
	out := make([]Issue, 0, len(raws))
	for _, r := range raws {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		out = append(out, Issue{
			Number:    r.Number,
			Title:     r.Title,
			Body:      r.Body,
			Labels:    labels,
			UpdatedAt: r.UpdatedAt,
		})
	}
	return out
}
