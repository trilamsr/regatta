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

	verifyrepo "github.com/trilamsr/regatta/internal/config/verify"
)

func runVerifyRepoConfig(args []string) int {
	fs := flag.NewFlagSet(subcmdVerifyRepoConfig, flag.ExitOnError)
	owner := fs.String("owner", "", "GitHub repo owner")
	repo := fs.String("repo", "", "GitHub repo name")
	branch := fs.String("branch", "main", "Protected branch name")
	asJSON := fs.Bool("json", false, "Emit JSON instead of human-readable summary")
	_ = fs.Parse(args)

	if *owner == "" || *repo == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "regatta verify-repo-config: -owner and -repo required")
		return 2
	}

	res, err := verifyrepo.Run(context.Background(), verifyrepo.Config{
		Owner:  *owner,
		Repo:   *repo,
		Branch: *branch,
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
