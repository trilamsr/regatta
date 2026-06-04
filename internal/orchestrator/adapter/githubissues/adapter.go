package githubissues

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/ghclient"
	"github.com/trilamsr/regatta/internal/obs"
)

// DefaultMinPoll is the spec §5.1 gh REST-budget cadence ceiling.
const DefaultMinPoll = 30 * time.Second

// Repo is the owner/name pair the adapter scans.
type Repo struct {
	Owner string
	Name  string
}

// Slug returns the SourceRef locator prefix "owner/name".
func (r Repo) Slug() string { return r.Owner + "/" + r.Name }

// GitHubIssuesConfig configures a GH-Issues-backed schemas.SpecAdapter; Client + Repo required, the rest default-fill in NewGitHubIssues.
type GitHubIssuesConfig struct {
	Client            ghclient.Client
	Repo              Repo
	Selector          string
	AcceptanceSection string
	MinPoll           time.Duration
	Logger            func(format string, args ...any)
	Tracer            trace.Tracer
	Meter             metric.Meter
	Clock             func() time.Time
}

// NewGitHubIssues returns a schemas.SpecAdapter consuming GH issues labelled `autonomous`; fails closed when Client or Repo is unset.
func NewGitHubIssues(cfg GitHubIssuesConfig) (schemas.SpecAdapter, error) {
	if cfg.Client == nil {
		return nil, errors.New("githubissues: Config.Client is required")
	}
	if cfg.Repo.Owner == "" || cfg.Repo.Name == "" {
		return nil, errors.New("githubissues: Config.Repo owner+name required")
	}
	if cfg.MinPoll <= 0 {
		cfg.MinPoll = DefaultMinPoll
	}
	if cfg.AcceptanceSection == "" {
		cfg.AcceptanceSection = AcceptanceHeading
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string, ...any) {}
	}
	if cfg.Tracer == nil {
		cfg.Tracer = otel.Tracer("adapter/github_issues")
	}
	if cfg.Meter == nil {
		cfg.Meter = obs.Meter(obs.MeterScopeAdapterGitHubIssues)
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &adapter{
		cfg:             cfg,
		idToNumber:      map[string]int{},
		idToUpdatedAt:   map[string]time.Time{},
		collisionLogged: map[string]struct{}{},
	}, nil
}

// adapter caches List()'s ID→number map for cfg.MinPoll (spec §7.7), records UpdatedAt per ID so Get can detect mid-flight edits (§7.9 / #850), and dedups §7.8 collision comments by owner/repo:ID:day.
type adapter struct {
	cfg             GitHubIssuesConfig
	mu              sync.Mutex
	idToNumber      map[string]int
	idToUpdatedAt   map[string]time.Time
	cachedAt        time.Time
	collisionLogged map[string]struct{}
}

// List projects open `autonomous` issues, skipping bad ones with WARN and surfacing collisions before projection per spec §7.8.
func (a *adapter) List(ctx context.Context) ([]schemas.WorkItem, error) {
	ctx, span := a.cfg.Tracer.Start(ctx, "adapter.github_issues.list")
	defer span.End()

	issues, err := a.cfg.Client.ListIssuesByLabelPaginated(ctx, AutonomousLabel, ghclient.ListIssuesOpts{State: "open", Limit: 1000})
	if err != nil {
		return nil, fmt.Errorf("github_issues list: %w", err)
	}
	if len(issues) > 1000 {
		return nil, fmt.Errorf("github_issues list: %w: >1000 open issues, file F3", schemas.ErrPermanent)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	idGroups := map[string][]ghclient.Issue{}
	idable := make([]ghclient.Issue, 0, len(issues))
	for _, iss := range issues {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !hasAutonomousLabel(iss.Labels) {
			continue
		}
		id, _, ok := extractIDFromTitle(iss.Title)
		if !ok {
			a.warnSkip(iss.Number, ReasonBadIDPrefix)
			continue
		}
		idGroups[id] = append(idGroups[id], iss)
		idable = append(idable, iss)
	}

	collided := map[string]bool{}
	for id, group := range idGroups {
		if len(group) > 1 {
			collided[id] = true
			a.handleCollision(ctx, id, group)
		}
	}

	out := make([]schemas.WorkItem, 0, len(idable))
	newCache := map[string]int{}
	newUpdatedAt := map[string]time.Time{}
	for _, iss := range idable {
		id, stripped, _ := extractIDFromTitle(iss.Title)
		if collided[id] {
			continue
		}
		p, reason, perr := parseIssueBody(iss.Body)
		if perr != nil {
			a.warnSkip(iss.Number, reason)
			continue
		}
		// Spec §4.2: SKIP on backfill write failure — projecting marker-less would dup-spawn next tick (#849).
		if p.DedupKey == "" {
			key := computeDedupKey(a.cfg.Repo.Owner, a.cfg.Repo.Name, iss.Number, iss.Body)
			newBody := withBackfilledMarker(normalize(iss.Body), key)
			if err := a.cfg.Client.EditIssueBody(ctx, iss.Number, newBody); err != nil {
				a.warnSkip(iss.Number, ReasonBackfillFailed)
				continue
			}
		}
		wi := schemas.WorkItem{
			ID:                 schemas.WorkItemID(id),
			Kind:               schemas.KindFeature,
			Title:              stripped,
			Body:               p.Body,
			AcceptanceCriteria: p.AcceptanceCriteria,
			Lane:               schemas.LaneID(p.Lane),
			LinkedArtifact:     p.LinkedArtifact,
			Status:             schemas.StatusPlanned,
			Source: schemas.SourceRef{
				Kind:    "issue",
				Locator: fmt.Sprintf("github://%s/issues/%d", a.cfg.Repo.Slug(), iss.Number),
				SHA:     bodySourceSHA(iss.Body),
			},
		}
		for _, d := range p.Dependencies {
			wi.Dependencies = append(wi.Dependencies, schemas.WorkItemID(d))
		}
		out = append(out, wi)
		newCache[id] = iss.Number
		newUpdatedAt[id] = iss.UpdatedAt
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	a.idToNumber = newCache
	a.idToUpdatedAt = newUpdatedAt
	a.cachedAt = a.cfg.Clock()
	return out, nil
}

// Get resolves an ID via the List() cache; on miss-after-TTL issues one search-by-id rebuild (spec §7.7) before returning ErrNotFound. Returns ErrSourceMutated when the live issue's UpdatedAt drifted from the snapshot captured at List (spec §7.9 / #850).
func (a *adapter) Get(ctx context.Context, id schemas.WorkItemID) (schemas.WorkItem, error) {
	a.mu.Lock()
	num, hit := a.idToNumber[string(id)]
	snapshotUpdatedAt, hasSnapshot := a.idToUpdatedAt[string(id)]
	expired := a.cfg.Clock().Sub(a.cachedAt) > a.cfg.MinPoll
	a.mu.Unlock()
	if hit && !expired {
		return a.fetchByNumber(ctx, id, num, hasSnapshot, snapshotUpdatedAt)
	}
	issues, err := a.cfg.Client.ListIssuesByLabelPaginated(ctx, AutonomousLabel, ghclient.ListIssuesOpts{State: "open"})
	if err != nil {
		return schemas.WorkItem{}, fmt.Errorf("github_issues get: %w", err)
	}
	var matches []ghclient.Issue
	for _, iss := range issues {
		if !hasAutonomousLabel(iss.Labels) {
			continue
		}
		got, _, ok := extractIDFromTitle(iss.Title)
		if !ok || got != string(id) {
			continue
		}
		matches = append(matches, iss)
	}
	if len(matches) == 0 {
		return schemas.WorkItem{}, fmt.Errorf("%w: %s", schemas.ErrNotFound, id)
	}
	if len(matches) > 1 {
		return schemas.WorkItem{}, fmt.Errorf("%w: id collision on %s", schemas.ErrPermanent, id)
	}
	a.mu.Lock()
	a.idToNumber[string(id)] = matches[0].Number
	a.idToUpdatedAt[string(id)] = matches[0].UpdatedAt
	a.cachedAt = a.cfg.Clock()
	a.mu.Unlock()
	return a.projectIssue(matches[0])
}

func (a *adapter) fetchByNumber(ctx context.Context, id schemas.WorkItemID, number int, hasSnapshot bool, snapshotUpdatedAt time.Time) (schemas.WorkItem, error) {
	iss, err := a.cfg.Client.GetIssue(ctx, number)
	if err != nil {
		return schemas.WorkItem{}, fmt.Errorf("%w: %s", schemas.ErrNotFound, id)
	}
	if hasSnapshot && !snapshotUpdatedAt.IsZero() && !iss.UpdatedAt.IsZero() && !iss.UpdatedAt.Equal(snapshotUpdatedAt) {
		return schemas.WorkItem{}, fmt.Errorf("%w: %s updated_at %s→%s", schemas.ErrSourceMutated, id, snapshotUpdatedAt.Format(time.RFC3339), iss.UpdatedAt.Format(time.RFC3339))
	}
	return a.projectIssue(iss)
}

func (a *adapter) projectIssue(iss ghclient.Issue) (schemas.WorkItem, error) {
	id, stripped, ok := extractIDFromTitle(iss.Title)
	if !ok {
		return schemas.WorkItem{}, fmt.Errorf("%w: title", schemas.ErrPermanent)
	}
	p, reason, perr := parseIssueBody(iss.Body)
	if perr != nil {
		return schemas.WorkItem{}, fmt.Errorf("%w: %s", schemas.ErrPermanent, reason)
	}
	wi := schemas.WorkItem{
		ID:                 schemas.WorkItemID(id),
		Kind:               schemas.KindFeature,
		Title:              stripped,
		Body:               p.Body,
		AcceptanceCriteria: p.AcceptanceCriteria,
		Lane:               schemas.LaneID(p.Lane),
		LinkedArtifact:     p.LinkedArtifact,
		Status:             schemas.StatusPlanned,
		Source: schemas.SourceRef{
			Kind:    "issue",
			Locator: fmt.Sprintf("github://%s/issues/%d", a.cfg.Repo.Slug(), iss.Number),
			SHA:     bodySourceSHA(iss.Body),
		},
	}
	for _, d := range p.Dependencies {
		wi.Dependencies = append(wi.Dependencies, schemas.WorkItemID(d))
	}
	return wi, nil
}

// UpdateStatus is the MVR-1 no-op (spec §1.2); F1 follow-up adds body-marker append.
func (a *adapter) UpdateStatus(_ context.Context, _ schemas.WorkItemID, _ schemas.Status, _ string) error {
	return fmt.Errorf("%w: github_issues UpdateStatus", schemas.ErrAdapterUnsupported)
}

// Capabilities reports github_issues feature flags; Webhook + BulkUpdate stay false until F7 / F1.
func (a *adapter) Capabilities() schemas.Capabilities {
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

// warnSkip emits the spec §7.6 closed-enum WARN payload (no raw body per §8.3).
func (a *adapter) warnSkip(issueNumber int, reason SkipReason) {
	a.cfg.Logger("github_issues.skip adapter=github_issues repo=%s issue_number=%d reason=%s issue_url=https://github.com/%s/issues/%d",
		a.cfg.Repo.Slug(), issueNumber, reason, a.cfg.Repo.Slug(), issueNumber)
}

// handleCollision logs ERROR + comments-on-both-issues per spec §7.8 with per-(owner/repo:ID:day) in-process dedup.
func (a *adapter) handleCollision(ctx context.Context, id string, group []ghclient.Issue) {
	day := a.cfg.Clock().UTC().Format("2006-01-02")
	for _, iss := range group {
		key := fmt.Sprintf("collision:%s:%s:%d:%s", a.cfg.Repo.Slug(), id, iss.Number, day)
		if _, done := a.collisionLogged[key]; done {
			continue
		}
		a.collisionLogged[key] = struct{}{}
		a.cfg.Logger("github_issues.collision repo=%s id=%s issue_number=%d", a.cfg.Repo.Slug(), id, iss.Number)
		msg := fmt.Sprintf("duplicate work-item ID `%s` — regatta will not consume either issue until the collision is resolved", id)
		if err := a.cfg.Client.CommentOnIssue(ctx, iss.Number, msg); err != nil {
			a.cfg.Logger("github_issues.collision_comment_failed repo=%s issue_number=%d err=%v", a.cfg.Repo.Slug(), iss.Number, err)
		}
	}
}

func hasAutonomousLabel(labels []string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, AutonomousLabel) {
			return true
		}
	}
	return false
}
