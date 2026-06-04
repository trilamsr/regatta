package main

import (
	"fmt"
	"path/filepath"

	"github.com/trilamsr/regatta/contracts/schemas"
	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/ghclient"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter/githubissues"
)

// buildSpecAdapter dispatches the schemas.SpecAdapter by `regatta.yaml::spec_adapter.type` (MVR-1-T4 §10.1); markdown_catalog stays the default.
func buildSpecAdapter(f serveFlags) (schemas.SpecAdapter, error) {
	cfgPath := filepath.Join(f.RepoRoot, "regatta.yaml")
	cfg, _ := validateconfig.LoadConfigFile(cfgPath)
	if cfg != nil && cfg.SpecAdapter != nil && cfg.SpecAdapter.Type == validateconfig.SpecAdapterTypeGitHubIssues {
		if cfg.Repo == nil || cfg.Repo.Owner == "" || cfg.Repo.Name == "" {
			return nil, fmt.Errorf("spec_adapter.type=github_issues requires repo.{owner,name}")
		}
		return githubissues.NewGitHubIssues(githubissues.GitHubIssuesConfig{
			Client:            ghclient.NewGHCLIClient(cfg.Repo.Owner, cfg.Repo.Name),
			Repo:              githubissues.Repo{Owner: cfg.Repo.Owner, Name: cfg.Repo.Name},
			Selector:          cfg.SpecAdapter.Selector,
			AcceptanceSection: cfg.SpecAdapter.AcceptanceSection,
		})
	}
	return adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: f.ItemsRoot})
}
