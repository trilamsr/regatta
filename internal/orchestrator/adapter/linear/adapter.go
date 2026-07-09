// Package linear consumes work items from Linear via the GraphQL API,
// projecting them onto schemas.WorkItemSource so the orchestrator can treat
// Linear issues identically to github_issues / markdown_catalog sources.
// Read-only: UpdateStatus returns ErrAdapterUnsupported (write-back is a
// Phase-X follow-up; the self-host loop only needs ingestion).
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
)

// defaultEndpoint is Linear's single GraphQL entrypoint.
const defaultEndpoint = "https://api.linear.app/graphql"

// defaultMinPoll pins half of Linear's documented 1500-req/h budget floor
// (spec §9.1) so concurrent adapters do not trip platform throttling.
const defaultMinPoll = 10 * time.Second

// pageSize caps each GraphQL page; Linear's issues() connection accepts up
// to 250 but 50 keeps payloads small per spec §4.1.
const pageSize = 50

// maxPages bounds cursor pagination so a runaway hasNextPage loop cannot
// hang List indefinitely (adversarial §9 traversal bound).
const maxPages = 200

// Linear's default workflow state names (spec §4.1); operators with custom
// state names extend mapState's switch.
const (
	stateBacklog    = "Backlog"
	stateTodo       = "Todo"
	stateInProgress = "In Progress"
	stateDone       = "Done"
	stateCanceled   = "Canceled"
)

// LinearCatalogConfig configures a Linear-backed schemas.WorkItemSource;
// APIKey + Team are required, the rest default-fill in NewLinearCatalog.
type LinearCatalogConfig struct {
	// APIKey is the Linear personal API key; sent verbatim in the
	// Authorization header (Linear uses the raw key, not a Bearer prefix).
	APIKey string
	// Team is the Linear team key (e.g. "MAY") the issues() filter pins.
	Team string
	// States filters issues by state name (e.g. "Todo", "In Progress");
	// empty ⇒ all states.
	States []string
	// Endpoint overrides the GraphQL URL; empty ⇒ defaultEndpoint. Tests
	// point this at an httptest.Server.
	Endpoint string
	// HTTPClient overrides the transport; empty ⇒ a 30s-timeout client.
	HTTPClient *http.Client
	// MinPoll overrides the Capabilities poll floor; <=0 ⇒ defaultMinPoll.
	MinPoll time.Duration
}

// NewLinearCatalog returns a Linear-backed schemas.WorkItemSource; fails
// closed when APIKey or Team is unset so a misconfigured boot surfaces
// loudly rather than polling an unauthenticated endpoint.
func NewLinearCatalog(cfg LinearCatalogConfig) (schemas.WorkItemSource, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("linear: Config.APIKey is required")
	}
	if cfg.Team == "" {
		return nil, errors.New("linear: Config.Team is required")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultEndpoint
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.MinPoll <= 0 {
		cfg.MinPoll = defaultMinPoll
	}
	return &adapterImpl{cfg: cfg}, nil
}

type adapterImpl struct {
	cfg LinearCatalogConfig
}

// listQuery pulls the §4.1 field set with team+state filters; pagination is
// internal per the WorkItemSource contract.
const listQuery = `query Issues($team: String!, $states: [String!], $first: Int!, $after: String) {
  issues(
    filter: { team: { key: { eq: $team } }, state: { name: { in: $states } } }
    first: $first
    after: $after
  ) {
    pageInfo { hasNextPage endCursor }
    nodes { id identifier title description updatedAt state { name } }
  }
}`

type graphqlError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code string `json:"code"`
	} `json:"extensions"`
}

type listResponse struct {
	Data struct {
		Issues struct {
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
			Nodes []issueNode `json:"nodes"`
		} `json:"issues"`
	} `json:"data"`
	Errors []graphqlError `json:"errors"`
}

type issueNode struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updatedAt"`
	State       struct {
		Name string `json:"name"`
	} `json:"state"`
}

// List projects every matching Linear issue, following cursor pagination
// internally and surfacing rate-limit signals as ErrRateLimited.
func (a *adapterImpl) List(ctx context.Context) ([]schemas.WorkItem, error) {
	var out []schemas.WorkItem
	var cursor string
	for page := 0; page < maxPages; page++ {
		resp, err := a.queryPage(ctx, cursor)
		if err != nil {
			return nil, err
		}
		for _, n := range resp.Data.Issues.Nodes {
			wi, err := a.project(n)
			if err != nil {
				return nil, err
			}
			out = append(out, wi)
		}
		if !resp.Data.Issues.PageInfo.HasNextPage || resp.Data.Issues.PageInfo.EndCursor == "" {
			return out, nil
		}
		cursor = resp.Data.Issues.PageInfo.EndCursor
	}
	return nil, fmt.Errorf("linear list: %w: pagination exceeded %d pages", schemas.ErrPermanent, maxPages)
}

// Get fetches one issue by its "linear:<uuid>" ID. Linear's filter does not
// expose a single-issue-by-uuid shortcut on the same query shape, so Get
// scans List and matches — the orchestrator's L0 re-reads via Get to detect
// mid-flight edits via Source.SHA.
func (a *adapterImpl) Get(ctx context.Context, id schemas.WorkItemID) (schemas.WorkItem, error) {
	items, err := a.List(ctx)
	if err != nil {
		return schemas.WorkItem{}, err
	}
	for _, wi := range items {
		if wi.ID == id {
			return wi, nil
		}
	}
	return schemas.WorkItem{}, fmt.Errorf("%w: %s", schemas.ErrNotFound, id)
}

// UpdateStatus is unsupported: the Linear adapter is read-only this slice
// (write-back deferred to Phase-X).
func (a *adapterImpl) UpdateStatus(_ context.Context, _ schemas.WorkItemID, _ schemas.Status, _ string) error {
	return fmt.Errorf("%w: linear UpdateStatus", schemas.ErrAdapterUnsupported)
}

// Capabilities reports Linear feature flags; webhook + bulk-update stay false
// (read-only ingestion), MinPollInterval pins the §9.1 budget floor.
func (a *adapterImpl) Capabilities() schemas.Capabilities {
	return schemas.Capabilities{
		Webhook:         false,
		BulkUpdate:      false,
		MinPollInterval: a.cfg.MinPoll,
		SupportedStatuses: []schemas.Status{
			schemas.StatusPlanned,
			schemas.StatusInProgress,
			schemas.StatusDone,
		},
	}
}

// project maps one Linear node onto a WorkItem; description acceptance-criteria
// parsing reuses the shared markdown extractor (adapter.ParseCriteria).
func (a *adapterImpl) project(n issueNode) (schemas.WorkItem, error) {
	status, err := mapState(n.State.Name)
	if err != nil {
		return schemas.WorkItem{}, err
	}
	criteria, body := adapter.ParseCriteria(n.Description)
	return schemas.WorkItem{
		ID:                 schemas.WorkItemID("linear:" + n.ID),
		Kind:               schemas.KindFeature,
		Title:              n.Title,
		Body:               strings.TrimSpace(body),
		AcceptanceCriteria: criteria,
		Status:             status,
		LinkedArtifact:     n.Identifier,
		Source: schemas.SourceRef{
			Kind:    "issue",
			Locator: "linear://" + a.cfg.Team + "/" + n.Identifier,
			SHA:     n.UpdatedAt,
		},
	}, nil
}

// mapState translates a Linear state name to a regatta Status; unknown names
// surface ErrInvalidStatus so an unmapped workflow column fails loud rather
// than silently defaulting to planned.
func mapState(name string) (schemas.Status, error) {
	switch name {
	case stateBacklog, stateTodo:
		return schemas.StatusPlanned, nil
	case stateInProgress:
		return schemas.StatusInProgress, nil
	case stateDone:
		return schemas.StatusDone, nil
	case stateCanceled:
		return schemas.StatusClosedResolved, nil
	default:
		return "", fmt.Errorf("%w: unmapped linear state %q", schemas.ErrInvalidStatus, name)
	}
}

func (a *adapterImpl) queryPage(ctx context.Context, cursor string) (*listResponse, error) {
	vars := map[string]any{
		"team":  a.cfg.Team,
		"first": pageSize,
	}
	if len(a.cfg.States) > 0 {
		vars["states"] = a.cfg.States
	}
	if cursor != "" {
		vars["after"] = cursor
	}
	payload, err := json.Marshal(map[string]any{"query": listQuery, "variables": vars})
	if err != nil {
		return nil, fmt.Errorf("linear marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("linear request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Linear authenticates with the raw API key (no "Bearer " prefix).
	req.Header.Set("Authorization", a.cfg.APIKey)

	res, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linear transport: %w", errors.Join(schemas.ErrTransient, err))
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("linear list: %w", wrapRateLimit(res.Header.Get("x-ratelimit-requests-reset")))
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear list: %w: http %d", schemas.ErrTransient, res.StatusCode)
	}

	var decoded listResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("linear decode: %w", errors.Join(schemas.ErrPermanent, err))
	}
	if rlErr := rateLimitFromErrors(decoded.Errors, res.Header.Get("x-ratelimit-requests-reset")); rlErr != nil {
		return nil, fmt.Errorf("linear list: %w", rlErr)
	}
	if len(decoded.Errors) > 0 {
		return nil, fmt.Errorf("linear list: %w: %s", schemas.ErrTransient, decoded.Errors[0].Message)
	}
	return &decoded, nil
}

// rateLimitFromErrors returns a wrapped ErrRateLimited when any GraphQL error
// carries extensions.code == "RATELIMITED", else nil.
func rateLimitFromErrors(errs []graphqlError, resetHdr string) error {
	for _, e := range errs {
		if e.Extensions.Code == "RATELIMITED" {
			return wrapRateLimit(resetHdr)
		}
	}
	return nil
}

// wrapRateLimit builds an ErrRateLimited carrying a RateLimitHint parsed from
// the x-ratelimit-requests-reset header (seconds until reset).
func wrapRateLimit(resetHdr string) error {
	hint := schemas.RateLimitHint{}
	if secs, err := strconv.Atoi(strings.TrimSpace(resetHdr)); err == nil && secs > 0 {
		hint.RetryAfter = time.Duration(secs) * time.Second
		hint.Reset = time.Now().Add(hint.RetryAfter)
	}
	return fmt.Errorf("%w (retry_after=%s)", schemas.ErrRateLimited, hint.RetryAfter)
}
