// verify-repo-config audits a GitHub repo against the P2 canonical
// recipe; it's a single self-contained subcommand with no shared
// helpers, so it owns one file.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/trilamsr/regatta/internal/config"
	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/secrets"
)

func runVerifyRepoConfig(args []string) int {
	fs := flag.NewFlagSet(subcmdVerifyRepoConfig, flag.ContinueOnError)
	owner := fs.String("owner", "", "GitHub repo owner (default: regatta.yaml repo.owner)")
	repo := fs.String("repo", "", "GitHub repo name (default: regatta.yaml repo.name)")
	branch := fs.String("branch", "", "Protected branch name (default: regatta.yaml repo.default_branch, then main)")
	asJSON := fs.Bool("json", false, "Emit JSON instead of human-readable summary")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if cfg, _ := validateconfig.LoadConfigFile("regatta.yaml"); cfg != nil && cfg.Repo != nil {
		if *owner == "" {
			*owner = cfg.Repo.Owner
		}
		if *repo == "" {
			*repo = cfg.Repo.Name
		}
		if *branch == "" {
			*branch = cfg.Repo.DefaultBranch
		}
	}
	if *branch == "" {
		*branch = defaultBranchName
	}

	if *owner == "" || *repo == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "regatta verify-repo-config: -owner and -repo required (or set repo.owner + repo.name in regatta.yaml)")
		return 2
	}

	ctx := context.Background()
	token := ""
	if v, _, err := secrets.GetWithSource(ctx, secrets.Default(ctx), secrets.KeyGHToken); err == nil {
		token = string(v.Bytes())
	}
	res, err := config.VerifyRun(ctx, config.VerifyConfig{
		Owner:  *owner,
		Repo:   *repo,
		Branch: *branch,
		Token:  token,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta verify-repo-config:", err)
		return 2
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		for _, c := range res.Checks {
			mark := "✓"
			if !c.Passed {
				mark = "✗"
			}
			fmt.Printf("%s %s -- %s\n", mark, c.ID, c.Title)
			if c.Detail != "" {
				fmt.Printf("    %s\n", c.Detail)
			}
			if !c.Passed && c.Rationale != "" {
				fmt.Printf("    rationale: %s\n", c.Rationale)
			}
		}
		if res.OK {
			fmt.Println("\nverify-repo-config: PASS -- repo is configured for Regatta deployment")
		} else {
			fmt.Printf("\nverify-repo-config: FAIL -- %d check(s) failed: %v\n", len(res.FailedOK), res.FailedOK)
		}
	}
	if !res.OK {
		return 1
	}
	return 0
}
