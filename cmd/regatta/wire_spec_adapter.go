package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/trilamsr/regatta/contracts/schemas"
	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/ghclient"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter/githubissues"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter/linear"
)

// buildSpecAdapter dispatches the schemas.SpecAdapter by `regatta.yaml::spec_adapter.type` (MVR-1-T4 §10.1). The slogger pin is load-bearing: pre-#867 the LoadConfigFile error path was silently swallowed, so a malformed regatta.yaml would fall back to markdown_catalog with no operator-visible signal that github_issues had been dropped. Every boot now emits one adapter.configured INFO record so operators can confirm the wired type without log archaeology, and yaml-load failures (file present but malformed) surface a WARN record naming the parse error. A missing regatta.yaml stays silent — zero-config deployments are the documented happy path.
func buildSpecAdapter(f serveFlags, logger *slog.Logger) (schemas.SpecAdapter, error) {
	cfgPath := filepath.Join(f.RepoRoot, "regatta.yaml")
	cfg, loadErr := validateconfig.LoadConfigFile(cfgPath)
	if loadErr != nil && !errors.Is(loadErr, fs.ErrNotExist) {
		logger.Warn("adapter.config_load_failed", "path", cfgPath, "err", loadErr)
	}
	if cfg != nil && cfg.SpecAdapter != nil && cfg.SpecAdapter.Type == validateconfig.SpecAdapterTypeGitHubIssues {
		if cfg.Repo == nil || cfg.Repo.Owner == "" || cfg.Repo.Name == "" {
			return nil, fmt.Errorf("spec_adapter.type=github_issues requires repo.{owner,name}")
		}
		// Mirror the Linear branch: fail-closed at boot if GH_TOKEN
		// is unresolved so the first scheduler tick does not crash
		// with a runtime unauth error (R-MEGA-3 LIVE-6).
		if os.Getenv("GH_TOKEN") == "" && os.Getenv("GITHUB_TOKEN") == "" {
			return nil, fmt.Errorf("spec_adapter.type=github_issues requires GH_TOKEN (or GITHUB_TOKEN) via env or secrets router")
		}
		logger.Info("adapter.configured",
			"type", validateconfig.SpecAdapterTypeGitHubIssues,
			"selector", cfg.SpecAdapter.Selector,
			"repo", cfg.Repo.Owner+"/"+cfg.Repo.Name,
		)
		return githubissues.NewGitHubIssues(githubissues.GitHubIssuesConfig{
			Client:            ghclient.NewGHCLIClient(cfg.Repo.Owner, cfg.Repo.Name),
			Repo:              githubissues.Repo{Owner: cfg.Repo.Owner, Name: cfg.Repo.Name},
			Selector:          cfg.SpecAdapter.Selector,
			AcceptanceSection: cfg.SpecAdapter.AcceptanceSection,
			DefaultLane:       cfg.SpecAdapter.DefaultLane,
		})
	}
	if cfg != nil && cfg.SpecAdapter != nil && cfg.SpecAdapter.Type == validateconfig.SpecAdapterTypeLinear {
		if cfg.SpecAdapter.Team == "" {
			return nil, fmt.Errorf("spec_adapter.type=linear requires spec_adapter.team")
		}
		// The secrets router exports LINEAR_API_KEY to env at boot
		// (wire_secrets exportSecretsToEnv) from env/keychain/pass; an
		// empty value here means the operator configured neither.
		apiKey := os.Getenv(envLinearAPIKey)
		if apiKey == "" {
			return nil, fmt.Errorf("spec_adapter.type=linear requires the Linear API key via %s (env or secrets router)", envLinearAPIKey)
		}
		logger.Info("adapter.configured",
			"type", validateconfig.SpecAdapterTypeLinear,
			"team", cfg.SpecAdapter.Team,
			"states", cfg.SpecAdapter.States,
		)
		return linear.NewLinearCatalog(linear.LinearCatalogConfig{
			APIKey: apiKey,
			Team:   cfg.SpecAdapter.Team,
			States: cfg.SpecAdapter.States,
		})
	}
	logger.Info("adapter.configured",
		"type", validateconfig.SpecAdapterTypeMarkdownCatalog,
		"items_root", f.ItemsRoot,
	)
	return adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: f.ItemsRoot})
}
