package prwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// defaultGitHubAPIBase is the GitHub REST host. Overridden by
// ETagListerConfig.BaseURL for tests + GH Enterprise.
const defaultGitHubAPIBase = "https://api.github.com"

// etagHTTPTimeout caps the wall-clock of one REST call. Mirrors the
// gh-CLI shell wrapper's 10s budget so a hung REST cannot wedge the
// 5s tick loop any worse than the existing path.
const etagHTTPTimeout = 10 * time.Second

// ETagListerConfig wires an ETagLister. Owner+Repo+Token+Fallback are
// required; BaseURL defaults to api.github.com and Client to a 10s-
// budget http.Client.
type ETagListerConfig struct {
	Owner    string
	Repo     string
	Token    string
	BaseURL  string
	Fallback PRLister
	Client   *http.Client
}

// ETagLister polls `GET /repos/{o}/{r}/pulls?head={o}:{branch}` with
// `If-None-Match` so steady-state ticks return 304 + zero quota cost.
// Title-prefix probes delegate to Fallback (gh-CLI) — the upstream
// `--search in:title` route handles fork-PR + branch-rename rescue and
// is not amenable to head-keyed ETag caching.
//
// Closes MAY-52. The 5s tick × {running,pr_open} agent count drives
// ~2880 gh-CLI shell-outs/hr per orchestrator; most return the same
// headRefOid. ETag + conditional-GET collapses the steady-state cost.
type ETagLister struct {
	cfg ETagListerConfig
	mu  sync.Mutex
	// cache keyed by branch. ETag is the last-seen header; snapshot
	// is the decoded PR list so a 304 can serve cache without re-decode.
	cache map[string]etagEntry
}

type etagEntry struct {
	etag     string
	snapshot []PullRequest
}

// NewETagLister returns a lister wired against the GitHub REST API.
// Wraps Fallback so a 304/cache-hit costs zero quota AND a 403/429/5xx
// drops through to the gh-CLI shell-out the package already trusts.
func NewETagLister(cfg ETagListerConfig) *ETagLister {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultGitHubAPIBase
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: etagHTTPTimeout}
	}
	return &ETagLister{cfg: cfg, cache: make(map[string]etagEntry)}
}

// ListOpenByHead implements PRLister. The head query goes through the
// ETag/304 fast path; a non-empty titlePrefix additionally invokes
// Fallback so the fork-PR rescue continues to fire.
func (e *ETagLister) ListOpenByHead(ctx context.Context, branch, titlePrefix string) ([]PullRequest, error) {
	headPRs, err := e.fetchHead(ctx, branch)
	if err != nil {
		// Fall through to the fallback's full path so we still
		// return whatever it observes — the fallback already covers
		// both --head and --search.
		return e.cfg.Fallback.ListOpenByHead(ctx, branch, titlePrefix)
	}
	if titlePrefix == "" {
		return headPRs, nil
	}
	// Title-prefix probes ride the gh-CLI fallback. The brief
	// explicitly scopes ETag work to the high-volume --head query;
	// `gh pr list --search` runs at most once per 12 ticks for an
	// agent whose --head returned empty.
	titlePRs, err := e.cfg.Fallback.ListOpenByHead(ctx, branch, titlePrefix)
	if err != nil {
		return headPRs, err
	}
	return dedupePRs(append(headPRs, titlePRs...)), nil
}

// fetchHead runs the REST head query, returning (snapshot, nil) on
// 200/304 and (nil, err) when the caller should fall back. err is set
// for any non-200/304 response (5xx, 403 secondary-rate-limit, 429,
// network failure).
func (e *ETagLister) fetchHead(ctx context.Context, branch string) ([]PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&head=%s:%s",
		strings.TrimRight(e.cfg.BaseURL, "/"),
		e.cfg.Owner, e.cfg.Repo, e.cfg.Owner, branch,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if e.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.Token)
	}
	e.mu.Lock()
	entry, hit := e.cache[branch]
	e.mu.Unlock()
	if hit && entry.etag != "" {
		req.Header.Set("If-None-Match", entry.etag)
	}

	resp, err := e.cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		if !hit {
			// Shouldn't happen — server returned 304 with no prior
			// ETag from us. Treat as a fallback-eligible miss.
			return nil, fmt.Errorf("prwatch: 304 with no cached entry for %s", branch)
		}
		return entry.snapshot, nil
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		snapshot, err := decodeRESTPRs(body)
		if err != nil {
			return nil, err
		}
		etag := resp.Header.Get("ETag")
		e.mu.Lock()
		e.cache[branch] = etagEntry{etag: etag, snapshot: snapshot}
		e.mu.Unlock()
		return snapshot, nil
	default:
		// 403 + secondary-rate-limit body, 429, 5xx, anything else.
		// Drop through to fallback. Read a snippet for the error so
		// the warning carries actionable context if the fallback
		// also fails.
		head := make([]byte, 256)
		n, _ := io.ReadFull(resp.Body, head)
		return nil, fmt.Errorf("prwatch: REST %d for %s: %s",
			resp.StatusCode, branch, bytes.TrimSpace(head[:n]))
	}
}

// restPullRaw mirrors the GitHub REST /pulls payload shape — separate
// from ghAuthorWrap because the REST API names fields head.sha /
// head.ref / head.user.login / mergeable_state where gh's CLI flattens
// to headRefOid / headRefName / author.login / mergeStateStatus.
type restPullRaw struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Title  string `json:"title"`
	Head   struct {
		SHA  string `json:"sha"`
		Ref  string `json:"ref"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"head"`
	MergeableState string `json:"mergeable_state"`
}

func decodeRESTPRs(body []byte) ([]PullRequest, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var raws []restPullRaw
	if err := json.Unmarshal(body, &raws); err != nil {
		return nil, fmt.Errorf("prwatch: decode REST json: %w", err)
	}
	out := make([]PullRequest, len(raws))
	for i, r := range raws {
		out[i] = PullRequest{
			Number:           r.Number,
			HeadRefOid:       r.Head.SHA,
			State:            strings.ToUpper(r.State),
			HeadRefName:      r.Head.Ref,
			Title:            r.Title,
			AuthorLogin:      r.Head.User.Login,
			MergeStateStatus: strings.ToUpper(r.MergeableState),
		}
	}
	return out, nil
}
