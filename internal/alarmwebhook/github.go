package alarmwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHubClient is the seam between the handler and the GitHub REST API.
// Tests fake it; production wires httpGitHubClient. The three methods
// cover exactly what the dedup-or-create rule needs — no surface
// beyond that.
type GitHubClient interface {
	// ListOpenIssuesByLabel returns open issues with the given label
	// whose title contains alertname. Caller treats first match as the
	// dedup target (oldest wins).
	ListOpenIssuesByLabel(ctx context.Context, label, alertnameSubstr string) ([]Issue, error)
	// CreateIssue posts a new issue and returns the created number.
	CreateIssue(ctx context.Context, title, body string, labels []string) (int, error)
	// CommentOnIssue appends body to the issue's comment thread.
	CommentOnIssue(ctx context.Context, number int, body string) error
}

// Issue is the trimmed view of the GitHub issue payload the dedup path
// needs. Adding fields is cheap; trim back if any goes unused.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

// ErrGitHub wraps every transport-layer or HTTP-status failure from the
// GitHub REST API. Callers branch on the boundary without coupling to
// raw 4xx/5xx codes.
var ErrGitHub = errors.New("alarmwebhook: github API call failed")

// httpGitHubClient is the production implementation. It speaks the v3
// REST API over a stdlib http.Client — no SDK dependency, so the binary
// stays small and the dep surface stays at zero new modules. The
// brief's "use go-github if present, else raw HTTP" branch chose raw
// HTTP because the SDK is not in go.mod.
type httpGitHubClient struct {
	token   string
	owner   string
	repo    string
	http    *http.Client
	baseURL string
	// searchRetrier wraps the /search/issues call with 429-aware backoff
	// and a consecutive-429 circuit breaker. Only the search endpoint is
	// wrapped — create/comment are mutations that AlertManager will retry
	// via the handler's 502, and double-mutation under retry is the
	// classic duplicate-issue hazard the dedup cache is built to prevent.
	searchRetrier *retrier
}

// NewHTTPGitHubClient is the production constructor. baseURL defaults to
// https://api.github.com when empty; tests override it.
func NewHTTPGitHubClient(token, owner, repo, baseURL string) GitHubClient {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &httpGitHubClient{
		token:   token,
		owner:   owner,
		repo:    repo,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
		searchRetrier: newRetrier(retryOpts{
			MaxAttempts:      5,
			BaseDelay:        500 * time.Millisecond,
			MaxDelay:         30 * time.Second,
			Jitter:           0.25,
			CircuitThreshold: 10,
			CircuitWindow:    1 * time.Minute,
			CircuitOpenFor:   5 * time.Minute,
		}),
	}
}

// doRaw issues a single HTTP attempt without retry. Callers that want
// backoff wrap this in retrier.Do; mutation endpoints call doRaw directly.
func (c *httpGitHubClient) doRaw(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("%w: marshal: %w", ErrGitHub, err)
		}
		reader = strings.NewReader(string(buf))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %w", ErrGitHub, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// do is the non-retrying wrapper that surfaces 4xx/5xx as ErrGitHub.
// Mutation endpoints (create/comment) use it directly.
func (c *httpGitHubClient) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	resp, err := c.doRaw(ctx, method, path, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGitHub, err)
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: %s %s -> %d: %s",
			ErrGitHub, method, path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return resp, nil
}

// ListOpenIssuesByLabel queries the search/issues endpoint. The query
// string is built with url.QueryEscape on alertnameSubstr so a hostile
// alertname (`" OR is:closed`) cannot break out of the in:title term.
func (c *httpGitHubClient) ListOpenIssuesByLabel(ctx context.Context, label, alertnameSubstr string) ([]Issue, error) {
	q := fmt.Sprintf("repo:%s/%s is:issue is:open label:%s in:title %q",
		c.owner, c.repo, label, alertnameSubstr)
	path := "/search/issues?q=" + url.QueryEscape(q)
	resp, err := c.searchRetrier.Do(ctx, func() (*http.Response, error) {
		return c.doRaw(ctx, http.MethodGet, path, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGitHub, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: GET %s -> %d: %s",
			ErrGitHub, path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var page struct {
		Items []Issue `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("%w: decode search: %w", ErrGitHub, err)
	}
	return page.Items, nil
}

func (c *httpGitHubClient) CreateIssue(ctx context.Context, title, body string, labels []string) (int, error) {
	resp, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/repos/%s/%s/issues", c.owner, c.repo),
		map[string]any{"title": title, "body": body, "labels": labels},
	)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("%w: decode create: %w", ErrGitHub, err)
	}
	return out.Number, nil
}

func (c *httpGitHubClient) CommentOnIssue(ctx context.Context, number int, body string) error {
	resp, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/repos/%s/%s/issues/%d/comments", c.owner, c.repo, number),
		map[string]any{"body": body},
	)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
