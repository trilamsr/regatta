package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/ghclient"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter/githubissues"
)

// ghIssueRaw is the gh CLI JSON shape; labels arrive as objects with a
// `name` field rather than the flat string slice the adapter expects.
type ghIssueRaw struct {
	Number    int            `json:"number"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Labels    []ghLabelRaw   `json:"labels"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type ghLabelRaw struct {
	Name string `json:"name"`
}

func decodeGHIssues(out []byte) ([]ghclient.Issue, error) {
	var raws []ghIssueRaw
	if err := json.Unmarshal(out, &raws); err != nil {
		return nil, err
	}
	return flattenGHIssues(raws), nil
}

func decodeGHIssuesOrSingle(out []byte) ([]ghclient.Issue, error) {
	var single ghIssueRaw
	if err := json.Unmarshal(out, &single); err != nil {
		return decodeGHIssues(out)
	}
	return flattenGHIssues([]ghIssueRaw{single}), nil
}

func flattenGHIssues(raws []ghIssueRaw) []ghclient.Issue {
	out := make([]ghclient.Issue, 0, len(raws))
	for _, r := range raws {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		out = append(out, ghclient.Issue{
			Number:    r.Number,
			Title:     r.Title,
			Body:      r.Body,
			Labels:    labels,
			UpdatedAt: r.UpdatedAt,
		})
	}
	return out
}

// buildSpecAdapter picks the schemas.SpecAdapter implementation based on
// regatta.yaml::spec_adapter.type. The github_issues path wires a
// gh-CLI-backed client (MVR-1-T4 spec §10.1) so persona-A operators can
// drive the autonomous loop from their issue tracker without taking on a
// new HTTP dep; markdown_catalog stays the default to preserve the
// in-repo authoring workflow.
func buildSpecAdapter(f serveFlags) (schemas.SpecAdapter, error) {
	cfgPath := filepath.Join(f.RepoRoot, "regatta.yaml")
	cfg, _ := validateconfig.LoadConfigFile(cfgPath)
	if cfg != nil && cfg.SpecAdapter != nil && cfg.SpecAdapter.Type == validateconfig.SpecAdapterTypeGitHubIssues {
		if cfg.Repo == nil || cfg.Repo.Owner == "" || cfg.Repo.Name == "" {
			return nil, fmt.Errorf("spec_adapter.type=github_issues requires repo.{owner,name}")
		}
		client := newGHCLIClient(cfg.Repo.Owner, cfg.Repo.Name)
		return githubissues.NewGitHubIssues(githubissues.GitHubIssuesConfig{
			Client:            client,
			Repo:              githubissues.Repo{Owner: cfg.Repo.Owner, Name: cfg.Repo.Name},
			Selector:          cfg.SpecAdapter.Selector,
			AcceptanceSection: cfg.SpecAdapter.AcceptanceSection,
		})
	}
	return adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: f.ItemsRoot})
}

// ghCLIClient is the gh-CLI-subprocess backing for the github_issues
// adapter; only the three new methods are implemented in this build —
// alarm/selfimprove call paths route through the HTTP client at
// internal/alarmwebhook/github.go. F-followup: unify when go-github
// is adopted or the gh CLI surface widens.
type ghCLIClient struct {
	owner string
	repo  string
}

func newGHCLIClient(owner, name string) ghclient.Client {
	return &ghCLIClient{owner: owner, repo: name}
}

func (c *ghCLIClient) ListOpenIssuesByLabel(_ context.Context, _, _ string) ([]ghclient.Issue, error) {
	return nil, fmt.Errorf("ghCLIClient: ListOpenIssuesByLabel not implemented (alarmwebhook path)")
}

func (c *ghCLIClient) CreateIssue(_ context.Context, _, _ string, _ []string) (int, error) {
	return 0, fmt.Errorf("ghCLIClient: CreateIssue not implemented (alarmwebhook path)")
}

func (c *ghCLIClient) CommentOnIssue(ctx context.Context, n int, body string) error {
	// G204: argv-style exec.Command, no shell interpolation. Repo owner+name
	// come from CUE-validated regatta.yaml; body is operator-supplied issue
	// content the adapter does not interpret (spec §8.2 conditional-safety).
	cmd := exec.CommandContext(ctx, "gh", "issue", "comment", fmt.Sprint(n), "--repo", c.owner+"/"+c.repo, "--body", body) //nolint:gosec // G204: argv-style, no shell
	return cmd.Run()
}

func (c *ghCLIClient) ListIssuesByLabelPaginated(ctx context.Context, label string, opts ghclient.ListIssuesOpts) ([]ghclient.Issue, error) {
	state := opts.State
	if state == "" {
		state = "open"
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}
	cmd := exec.CommandContext(ctx, "gh", "issue", "list", //nolint:gosec // G204: argv-style, no shell; repo/label CUE-validated
		"--repo", c.owner+"/"+c.repo,
		"--label", label,
		"--state", state,
		"--json", "number,title,body,labels,updatedAt",
		"--limit", fmt.Sprint(limit),
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}
	return decodeGHIssues(out)
}

func (c *ghCLIClient) GetIssue(ctx context.Context, n int) (ghclient.Issue, error) {
	cmd := exec.CommandContext(ctx, "gh", "issue", "view", fmt.Sprint(n), //nolint:gosec // G204: argv-style, no shell
		"--repo", c.owner+"/"+c.repo,
		"--json", "number,title,body,labels,updatedAt",
	)
	out, err := cmd.Output()
	if err != nil {
		return ghclient.Issue{}, fmt.Errorf("gh issue view: %w", err)
	}
	issues, err := decodeGHIssuesOrSingle(out)
	if err != nil || len(issues) == 0 {
		return ghclient.Issue{}, fmt.Errorf("gh issue view %d: decode: %w", n, err)
	}
	return issues[0], nil
}

func (c *ghCLIClient) EditIssueBody(ctx context.Context, n int, body string) error {
	cmd := exec.CommandContext(ctx, "gh", "issue", "edit", fmt.Sprint(n), //nolint:gosec // G204: argv-style, no shell
		"--repo", c.owner+"/"+c.repo,
		"--body", body,
	)
	return cmd.Run()
}
