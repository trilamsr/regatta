package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/ghclient"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter/githubissues"
)

// TestScheduler_RespectsGitHubIssuesMinPoll30s pins the spec §5.4 / #840 invariant: a github_issues adapter reports MinPollInterval=30s via Capabilities so the orchestrator's per-adapter cadence audit (F19) has a concrete target.
func TestScheduler_RespectsGitHubIssuesMinPoll30s(t *testing.T) {
	ad, err := githubissues.NewGitHubIssues(githubissues.GitHubIssuesConfig{
		Client: stubGH{},
		Repo:   githubissues.Repo{Owner: "trilamsr", Name: "regatta"},
	})
	if err != nil {
		t.Fatalf("NewGitHubIssues: %v", err)
	}
	caps := ad.Capabilities()
	if caps.MinPollInterval != 30*time.Second {
		t.Fatalf("MinPollInterval=%v want 30s — github_issues adapter MUST budget gh REST 5000-req/hour", caps.MinPollInterval)
	}
	if caps.Webhook {
		t.Fatalf("Webhook=true — MVR-1 cut explicitly omits webhook surface (spec §5.3)")
	}
}

type stubGH struct{}

func (stubGH) ListOpenIssuesByLabel(_ context.Context, _, _ string) ([]ghclient.Issue, error) {
	return nil, nil
}
func (stubGH) CreateIssue(_ context.Context, _, _ string, _ []string) (int, error) { return 0, nil }
func (stubGH) CommentOnIssue(_ context.Context, _ int, _ string) error              { return nil }
func (stubGH) ListIssuesByLabelPaginated(_ context.Context, _ string, _ ghclient.ListIssuesOpts) ([]ghclient.Issue, error) {
	return nil, nil
}
func (stubGH) GetIssue(_ context.Context, _ int) (ghclient.Issue, error) {
	return ghclient.Issue{}, nil
}
func (stubGH) EditIssueBody(_ context.Context, _ int, _ string) error { return nil }
