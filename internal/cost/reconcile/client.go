package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// defaultAdminKeyEnv is the documented env var name holding the
// Anthropic admin API key (spec §3.4 + Anthropic docs). gosec flags
// the literal — the constant is the env-var name, not a credential.
const defaultAdminKeyEnv = "ANTHROPIC_ADMIN_KEY" //nolint:gosec // env var name, not a credential

// anthropicAPIVersion pins the documented Anthropic-version header
// (https://docs.anthropic.com/en/api/versioning). New API versions
// land here behind a code-review-able PR.
const anthropicAPIVersion = "2023-06-01"

// ErrCostAPIUnavailable signals that the Anthropic Cost API responded
// with 403/404 — caller (Tick) falls back to the Usage API + local
// pricing application.
var ErrCostAPIUnavailable = errors.New("cost API unavailable")

// ErrAdminKeyUnset signals the admin-key env var is empty. The
// reconciler logs WARN + skips this tick; pre-call deny gate keeps
// working against recorded spend (spec §3.4 fail-soft contract).
var ErrAdminKeyUnset = errors.New("admin key unset")

// RateLimitedError surfaces a 429 with the parsed retry-after value.
// Tick honours the header via Backoff.NextWithRetryAfter.
type RateLimitedError struct {
	RetryAfter time.Duration
	Status     int
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("cost api rate limited (status %d, retry-after %s)", e.Status, e.RetryAfter)
}

// UpstreamError carries 5xx + network failures so callers can match
// without string-sniffing. The wrapped path includes the request URL
// path (no query, no headers, no body) — by construction the admin
// key never lands here (pins R15).
type UpstreamError struct {
	Status int
	Path   string
	Cause  error
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream failure: status=%d path=%s cause=%v", e.Status, e.Path, e.Cause)
}

func (e *UpstreamError) Unwrap() error { return e.Cause }

// ClientConfig wires the HTTP client. Defaults keep the caller-side
// shape minimal: a missing UserAgent picks "regatta/dev", a missing
// BaseURL picks the public Anthropic host.
type ClientConfig struct {
	BaseURL         string       // default "https://api.anthropic.com"
	HTTPClient      *http.Client // default &http.Client{Timeout: 30s}
	UsageAPIKeyEnv  string       // env var name; default "ANTHROPIC_ADMIN_KEY"
	UserAgent       string       // default "regatta/dev (https://github.com/maydow/regatta)"
	AnthropicVerHdr string       // default "2023-06-01"
}

// Client wraps the two Anthropic admin endpoints. Concrete type, no
// interface — substrate-S5 parity. Tests inject the same Client via
// httptest BaseURL swap.
type Client struct {
	cfg ClientConfig
}

// NewClient constructs a Client. nil HTTPClient falls back to a 30s
// timeout client. Empty BaseURL falls back to api.anthropic.com so
// production wiring is one line.
func NewClient(cfg ClientConfig) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if cfg.UsageAPIKeyEnv == "" {
		cfg.UsageAPIKeyEnv = defaultAdminKeyEnv
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "regatta/dev (https://github.com/maydow/regatta)"
	}
	if cfg.AnthropicVerHdr == "" {
		cfg.AnthropicVerHdr = anthropicAPIVersion
	}
	return &Client{cfg: cfg}
}

// CostResponse models the Cost API response. Anthropic returns USD
// directly via cost_usd per bucket+model, eliminating the
// pricing-applied-twice defect (R-A4) on the happy path.
type CostResponse struct {
	Data []CostBucket `json:"data"`
}

// CostBucket is one row in the Cost API response. Group-by=model adds
// the Model field; we always group by model so per-model breakdown is
// available downstream.
type CostBucket struct {
	BucketStart time.Time `json:"bucket_start"`
	BucketEnd   time.Time `json:"bucket_end"`
	Model       string    `json:"model"`
	CostUSD     float64   `json:"cost_usd"`
}

// UsageResponse models the Usage API fallback shape. Spec §3.4 line
// 236 maps these to USD via local pricing: input/output/cache_read/
// cache_creation each multiplied by the corresponding $/Mtok rate.
type UsageResponse struct {
	Data []UsageBucket `json:"data"`
}

// UsageBucket field names mirror the Anthropic docs page. The
// `uncached_input_tokens` field name is the documented Anthropic
// convention for input tokens that bypassed the prompt cache.
type UsageBucket struct {
	BucketStart              time.Time `json:"bucket_start"`
	BucketEnd                time.Time `json:"bucket_end"`
	Model                    string    `json:"model"`
	UncachedInputTokens      int64     `json:"uncached_input_tokens"`
	CacheReadInputTokens     int64     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64     `json:"cache_creation_input_tokens"`
	OutputTokens             int64     `json:"output_tokens"`
}

// FetchCost calls GET /v1/organizations/cost_report/messages.
// Returns the decoded response, the verbatim response body bytes
// (for canonical-sha256 signing in tick.go), and an error sentinel.
// The body is read ONCE and shared between caller + decoder.
func (c *Client) FetchCost(ctx context.Context, start, end time.Time, bucketWidth time.Duration) (CostResponse, []byte, error) {
	body, err := c.fetch(ctx, "/v1/organizations/cost_report/messages", start, end, bucketWidth)
	if err != nil {
		var out CostResponse
		return out, nil, err
	}
	var out CostResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return CostResponse{}, nil, fmt.Errorf("decode cost response: %w", err)
	}
	return out, body, nil
}

// FetchUsage calls GET /v1/organizations/usage_report/messages.
// Same shape as FetchCost.
func (c *Client) FetchUsage(ctx context.Context, start, end time.Time, bucketWidth time.Duration) (UsageResponse, []byte, error) {
	body, err := c.fetch(ctx, "/v1/organizations/usage_report/messages", start, end, bucketWidth)
	if err != nil {
		var out UsageResponse
		return out, nil, err
	}
	var out UsageResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return UsageResponse{}, nil, fmt.Errorf("decode usage response: %w", err)
	}
	return out, body, nil
}

func (c *Client) fetch(ctx context.Context, path string, start, end time.Time, bucketWidth time.Duration) ([]byte, error) {
	// Pull the admin key from env at request time (not at NewClient)
	// so a refresh between ticks lands without process restart. The
	// key value is bound to the request header below and NEVER
	// included in any error message — pins R15.
	key := os.Getenv(c.cfg.UsageAPIKeyEnv)
	if key == "" {
		return nil, ErrAdminKeyUnset
	}

	q := url.Values{}
	q.Set("starting_at", start.UTC().Format("2006-01-02T15:04:05Z"))
	q.Set("ending_at", end.UTC().Format("2006-01-02T15:04:05Z"))
	q.Set("bucket_width", formatBucketWidth(bucketWidth))
	// group_by[]=model so per-model breakdown lands on the response.
	q.Add("group_by[]", "model")

	endpoint := c.cfg.BaseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, &UpstreamError{Path: path, Cause: err}
	}
	req.Header.Set("anthropic-version", c.cfg.AnthropicVerHdr)
	req.Header.Set("x-api-key", key)
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		// Network error — wrap and surface; never the key.
		return nil, &UpstreamError{Path: path, Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
		// fall through
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusForbidden:
		return nil, ErrCostAPIUnavailable
	case resp.StatusCode == http.StatusTooManyRequests:
		ra := parseRetryAfter(resp.Header.Get("retry-after"))
		return nil, &RateLimitedError{RetryAfter: ra, Status: resp.StatusCode}
	case resp.StatusCode >= 500:
		return nil, &UpstreamError{Status: resp.StatusCode, Path: path}
	default:
		// 4xx other than 403/404/429 — treat as upstream + surface
		// status. Never echo the response body (which could include
		// the key in echo-style 401 responses).
		return nil, &UpstreamError{Status: resp.StatusCode, Path: path}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &UpstreamError{Path: path, Cause: err}
	}
	return body, nil
}

// formatBucketWidth renders the Anthropic-accepted bucket_width
// literal. Spec §3.4 line 224 references `1h`; 1m + 1d are also
// documented in the Anthropic spec for future tuning.
func formatBucketWidth(d time.Duration) string {
	switch d {
	case time.Minute:
		return "1m"
	case time.Hour:
		return "1h"
	case 24 * time.Hour:
		return "1d"
	}
	// Fall back to integer-minutes form; Anthropic accepts the
	// canonical `Nm` shape (per docs) for arbitrary widths.
	minutes := int(d.Minutes())
	if minutes < 1 {
		minutes = 1
	}
	return strconv.Itoa(minutes) + "m"
}

// parseRetryAfter handles the HTTP Retry-After header which can be
// either an integer seconds count or an HTTP-date. Zero on unparseable
// so callers fall back to their own backoff.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	// HTTP-date form is rare for Anthropic; do not parse here. Fall
	// through to zero so callers use exponential backoff.
	return 0
}
