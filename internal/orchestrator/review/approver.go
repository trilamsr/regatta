// Package review wires L4 gate verdicts to GitHub's pull-request review
// API under a service-account identity (regatta-reviewer-bot), so the
// "≥1 approving review" branch-protection rule clicks over without an
// operator click. PHASE-AUTONOMY W7. See
// docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md.
package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// defaultGHBase is the live GitHub REST origin; tests override via Config.BaseURL.
const defaultGHBase = "https://api.github.com"

// Verdict is the L4 outcome the Approver consumes. It deliberately
// stays narrower than schemas.GateResult so callers can synthesize
// review verdicts from gates other than L4 (the spec defers that to
// Phase X but the seam is here).
type Verdict struct {
	Outcome  schemas.Verdict
	PRNumber int
	HeadSHA  string
	Model    string
	Reason   string
}

// Config wires the Approver's deps. Required: Owner, Repo,
// ReviewerToken, ReviewerLogin, AuthorBotLogin. BaseURL defaults to
// defaultGHBase; HTTPClient defaults to http.DefaultClient.
type Config struct {
	BaseURL        string
	Owner          string
	Repo           string
	ReviewerToken  string
	ReviewerLogin  string
	AuthorBotLogin string
	HTTPClient     *http.Client
	Logger         *slog.Logger
	Meter          metric.Meter
}

// Approver posts review verdicts under the reviewer-bot identity.
// Single-tenant per spec §7 R10; multi-tenant routing deferred to W8.
type Approver struct {
	baseURL        string
	owner          string
	repo           string
	token          string
	reviewerLogin  string
	authorBotLogin string
	http           *http.Client
	log            *slog.Logger
	metrics        *instruments
}

// instruments holds the OTEL counters; one struct so the approver
// stays cheap to copy and tests can assert registration via the
// public Approver receiver.
type instruments struct {
	postsAttempted metric.Int64Counter
	postsFailed    metric.Int64Counter
	dismissals     metric.Int64Counter
}

// scopeName is the OTEL meter scope; matches the gate-side `regatta.l4.*` shape.
const scopeName = "regatta/orchestrator/review"

// New constructs an Approver; required-deps return errors so wiring
// mistakes surface at serve boot, not at first verdict.
func New(cfg Config) (*Approver, error) {
	if cfg.Owner == "" || cfg.Repo == "" {
		return nil, fmt.Errorf("review: owner/repo required")
	}
	if cfg.ReviewerToken == "" {
		return nil, fmt.Errorf("review: reviewer token required")
	}
	if cfg.ReviewerLogin == "" || cfg.AuthorBotLogin == "" {
		return nil, fmt.Errorf("review: reviewer + author bot logins required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultGHBase
	}
	httpc := cfg.HTTPClient
	if httpc == nil {
		httpc = http.DefaultClient
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	meter := cfg.Meter
	if meter == nil {
		meter = otel.Meter(scopeName)
	}
	return &Approver{
		baseURL:        strings.TrimRight(base, "/"),
		owner:          cfg.Owner,
		repo:           cfg.Repo,
		token:          cfg.ReviewerToken,
		reviewerLogin:  cfg.ReviewerLogin,
		authorBotLogin: cfg.AuthorBotLogin,
		http:           httpc,
		log:            log,
		metrics:        newInstruments(meter),
	}, nil
}

func newInstruments(m metric.Meter) *instruments {
	att, _ := m.Int64Counter("regatta.review.posts.attempted")
	fail, _ := m.Int64Counter("regatta.review.posts.failed")
	dis, _ := m.Int64Counter("regatta.review.dismissals")
	return &instruments{postsAttempted: att, postsFailed: fail, dismissals: dis}
}

// prResp is the narrow PR shape the author-scope guard reads; live GH
// responses carry far more fields, decoded fields just stay zero-valued.
type prResp struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// reviewResp is the subset of GitHub's review record we filter on.
type reviewResp struct {
	ID       int64  `json:"id"`
	State    string `json:"state"`
	Body     string `json:"body"`
	CommitID string `json:"commit_id"`
	User     struct {
		Login string `json:"login"`
	} `json:"user"`
}

// Reconcile is the top-level entry: look up PR author, gate on the
// bot identity, find any prior approving review by the reviewer bot,
// dismiss + post per verdict transition. Soft-success on 404 (PR
// closed) per spec §7 R12.
func (a *Approver) Reconcile(ctx context.Context, v Verdict) error {
	pr, status, err := a.getPR(ctx, v.PRNumber)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		a.log.Info("review.target_gone", "pr", v.PRNumber)
		return nil
	}
	if pr.User.Login != a.authorBotLogin {
		a.log.Info("review.skipped_human_authored", "pr", v.PRNumber, "author", pr.User.Login)
		return nil
	}
	prior, err := a.lookupApprovingReview(ctx, v.PRNumber)
	if err != nil {
		return err
	}

	body := a.composeBody(v)
	event := mapVerdictToEvent(v.Outcome)

	// Idempotency: prior approval at same SHA with identical body and
	// outcome still pass → skip. Saves rate-limit budget; same shape as
	// the c0 outbox dedup path.
	if prior != nil && v.Outcome == schemas.VerdictPass && prior.CommitID == v.HeadSHA && prior.Body == body {
		a.log.Info("review.idempotent_skip", "pr", v.PRNumber, "review_id", prior.ID)
		return nil
	}

	// Flip path: prior APPROVED, new outcome != pass → dismiss BEFORE
	// posting REQUEST_CHANGES so a brief window where both are live
	// never appears (spec §5 #8). Dismissal failure does NOT abort
	// the REQUEST_CHANGES post; merge stays blocked by the new review.
	if prior != nil && v.Outcome != schemas.VerdictPass {
		if err := a.dismiss(ctx, v.PRNumber, prior.ID); err != nil {
			a.log.Warn("review.dismiss_failed", "pr", v.PRNumber, "review_id", prior.ID, "err", err)
		}
	}

	return a.postReview(ctx, v.PRNumber, event, body, v.HeadSHA)
}

// composeBody assembles the review-body string. Reproducible: same
// SHA + same verdict + same model + same reason → byte-identical
// output (A-tier (h)). Reason is redacted; paths + secret-shaped
// tokens stripped before the bytes ever leave the process.
func (a *Approver) composeBody(v Verdict) string {
	var b strings.Builder
	fmt.Fprintf(&b, "L4 verdict: %s model=%s sha=%s\n", v.Outcome, v.Model, v.HeadSHA)
	if v.Reason != "" {
		fmt.Fprintf(&b, "\n%s\n", redact(v.Reason))
	}
	return b.String()
}

// mapVerdictToEvent is the spec §3 mapping: pass→APPROVE,
// fail→REQUEST_CHANGES, advisory→COMMENT. Comment short-circuits
// auto-merge (A+ (n)) because it does not satisfy the approving-
// review count, which is exactly the defensive third path.
func mapVerdictToEvent(v schemas.Verdict) string {
	switch v {
	case schemas.VerdictPass:
		return "APPROVE"
	case schemas.VerdictFail:
		return "REQUEST_CHANGES"
	default:
		return "COMMENT"
	}
}

// getPR fetches the PR's author + head SHA. Returns the HTTP status
// alongside the parsed body so the caller can distinguish 404 from
// other transport errors without re-reading the response.
func (a *Approver) getPR(ctx context.Context, n int) (prResp, int, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", a.baseURL, a.owner, a.repo, n)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return prResp{}, 0, err
	}
	a.authorize(req)
	resp, err := a.http.Do(req)
	if err != nil {
		return prResp{}, 0, fmt.Errorf("review: get pr: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return prResp{}, resp.StatusCode, nil
	}
	if resp.StatusCode >= 400 {
		return prResp{}, resp.StatusCode, fmt.Errorf("review: get pr: %d", resp.StatusCode)
	}
	var pr prResp
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return prResp{}, resp.StatusCode, fmt.Errorf("review: decode pr: %w", err)
	}
	return pr, resp.StatusCode, nil
}

// lookupApprovingReview returns the most recent APPROVED review by the
// reviewer-bot. Single-page (GH default 30) — pagination defers to
// Phase X; PRs with >30 reviews are vanishingly rare in the auto-loop.
func (a *Approver) lookupApprovingReview(ctx context.Context, n int) (*reviewResp, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", a.baseURL, a.owner, a.repo, n)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	a.authorize(req)
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("review: list reviews: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("review: list reviews: %d", resp.StatusCode)
	}
	var rows []reviewResp
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("review: decode reviews: %w", err)
	}
	// Walk in reverse — GitHub returns chronological; last-write-wins
	// matches what a human reviewer would see in the UI.
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if r.User.Login == a.reviewerLogin && r.State == "APPROVED" {
			return &r, nil
		}
	}
	return nil, nil
}

// dismiss issues PUT /reviews/{id}/dismissals. Returns the GH error
// verbatim; caller logs + continues per spec §5.
func (a *Approver) dismiss(ctx context.Context, prNumber int, reviewID int64) error {
	if a.metrics.dismissals != nil {
		a.metrics.dismissals.Add(ctx, 1)
	}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews/%d/dismissals", a.baseURL, a.owner, a.repo, prNumber, reviewID)
	payload, _ := json.Marshal(map[string]string{"message": "L4 verdict flipped — re-scoring"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	a.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("review: dismiss: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("review: dismiss: %d", resp.StatusCode)
	}
	return nil
}

// postReview POSTs the verdict; commit_id pins the approval to the
// scored SHA so a push-during-POST race hits 422 not silent stale
// approval (spec §7 R9).
func (a *Approver) postReview(ctx context.Context, prNumber int, event, body, sha string) error {
	if a.metrics.postsAttempted != nil {
		a.metrics.postsAttempted.Add(ctx, 1)
	}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", a.baseURL, a.owner, a.repo, prNumber)
	payload, err := json.Marshal(map[string]string{
		"event":     event,
		"body":      body,
		"commit_id": sha,
	})
	if err != nil {
		return fmt.Errorf("review: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	a.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		if a.metrics.postsFailed != nil {
			a.metrics.postsFailed.Add(ctx, 1)
		}
		return fmt.Errorf("review: post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		if a.metrics.postsFailed != nil {
			a.metrics.postsFailed.Add(ctx, 1)
		}
		// Redact the body before surfacing so a GH error that echoes
		// the request payload cannot leak a path or secret-shaped
		// token in regatta's log stream.
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("review: post: %d: %s", resp.StatusCode, redact(string(respBody)))
	}
	a.log.Info("review.post",
		"pr", prNumber,
		"actor", a.reviewerLogin,
		"event", event,
		"sha", sha,
	)
	return nil
}

// authorize stamps the Authorization header; centralized so a future
// PAT-rotation hook only edits one place.
func (a *Approver) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

// pathRE matches Go-source-file-shaped tokens; conservative — only
// `foo/bar.go` and similar so prose paragraphs survive unscathed.
var pathRE = regexp.MustCompile(`\b[a-zA-Z0-9_./-]+\.(go|md|yaml|yml|json|sh|ts|tsx|js)\b`)

// secretRE matches secret-shaped tokens (long opaque strings, common
// API-key prefixes). Aggressive on prefixes, conservative on bare
// hex/base64 — we accept some false negatives over leaking real keys.
var secretRE = regexp.MustCompile(`(?:sk-[A-Za-z0-9_-]{8,}|ghp_[A-Za-z0-9]{8,}|xox[bopa]-[A-Za-z0-9-]{8,})`)

// redact strips repo-private paths + secret-shaped tokens from the
// review body. Idempotent — running redact on already-redacted output
// is a no-op (property-test pinned by TestRedactor_Idempotent).
func redact(s string) string {
	s = pathRE.ReplaceAllString(s, "<path-redacted>")
	s = secretRE.ReplaceAllString(s, "<secret-redacted>")
	return s
}
