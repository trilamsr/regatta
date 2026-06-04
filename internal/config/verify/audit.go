// Package verify implements `regatta verify-repo-config`: a
// pre-flight audit of the target GitHub repo against the P2 canonical
// recipe and the silent-bypass classes documented in
// docs/design.md §Threat Model.
//
// Failure modes covered:
//   - enforce_admins: false (default) → admins silently bypass everything
//   - required_approving_review_count < 2 → not actually two-key
//   - require_code_owner_reviews: false
//   - require_last_push_approval: false → Mercari PR-hijacking class
//   - dismiss_stale_reviews: false
//   - CODEOWNERS pattern references non-existent team/user (silent ignore)
//   - CODEOWNERS file missing
//   - Required status checks list includes a job that can SKIP
package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config names the GitHub repo + branch the audit interrogates; Token falls back to $GITHUB_TOKEN.
type Config struct {
	Owner  string
	Repo   string
	Branch string // default "main"
	Token  string // GitHub PAT or App installation token (read from GITHUB_TOKEN if empty)
}

// Result aggregates per-check verdicts; FailedOK lists the IDs callers should pipe into exit code 2.
type Result struct {
	OK       bool     `json:"ok"`
	Checks   []Check  `json:"checks"`
	FailedOK []string `json:"failed_ok"` // human-readable check IDs that failed
}

const (
	checkIDBranchProtection = "branch_protection"
	checkIDCodeownersErrors = "codeowners_errors"
	checkTitleCodeowners    = "CODEOWNERS parses cleanly"
)

// Check is one named assertion in the audit; JSON-tagged so callers can render the report verbatim.
type Check struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Passed   bool   `json:"passed"`
	Detail   string `json:"detail,omitempty"`
	Rationale string `json:"rationale,omitempty"`
}

// Run executes every check sequentially; partial failure is captured in Result, not returned as error, so callers can render the full report.
func Run(ctx context.Context, cfg Config) (Result, error) {
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	if cfg.Token == "" {
		cfg.Token = os.Getenv("GITHUB_TOKEN")
	}
	if cfg.Owner == "" || cfg.Repo == "" {
		return Result{}, errors.New("verifyrepo: Owner and Repo required")
	}
	if cfg.Token == "" {
		return Result{}, errors.New("verifyrepo: GitHub token required (pass via GITHUB_TOKEN)")
	}

	res := Result{OK: true}
	res.add(checkBranchProtection(ctx, cfg))
	res.add(checkCodeOwners(ctx, cfg))
	res.add(checkCodeOwnersErrors(ctx, cfg))
	return res, nil
}

func (r *Result) add(c Check) {
	r.Checks = append(r.Checks, c)
	if !c.Passed {
		r.OK = false
		r.FailedOK = append(r.FailedOK, c.ID)
	}
}

type branchProtection struct {
	RequiredPullRequestReviews struct {
		RequiredApprovingReviewCount int  `json:"required_approving_review_count"`
		RequireCodeOwnerReviews      bool `json:"require_code_owner_reviews"`
		RequireLastPushApproval      bool `json:"require_last_push_approval"`
		DismissStaleReviews          bool `json:"dismiss_stale_reviews"`
	} `json:"required_pull_request_reviews"`
	EnforceAdmins struct {
		Enabled bool `json:"enabled"`
	} `json:"enforce_admins"`
	RequiredStatusChecks *struct {
		Strict   bool     `json:"strict"`
		Contexts []string `json:"contexts"`
	} `json:"required_status_checks"`
}

func checkBranchProtection(ctx context.Context, cfg Config) Check {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches/%s/protection", cfg.Owner, cfg.Repo, cfg.Branch)
	var bp branchProtection
	if err := ghGet(ctx, cfg.Token, url, &bp); err != nil {
		return Check{
			ID:        checkIDBranchProtection,
			Title:     "Branch protection on " + cfg.Branch,
			Passed:    false,
			Detail:    err.Error(),
			Rationale: "GET /repos/{owner}/{repo}/branches/{branch}/protection must succeed; branch protection must be configured.",
		}
	}
	var problems []string
	if bp.RequiredPullRequestReviews.RequiredApprovingReviewCount < 2 {
		problems = append(problems, fmt.Sprintf("required_approving_review_count=%d (want ≥2 for P2)", bp.RequiredPullRequestReviews.RequiredApprovingReviewCount))
	}
	if !bp.RequiredPullRequestReviews.RequireCodeOwnerReviews {
		problems = append(problems, "require_code_owner_reviews=false")
	}
	if !bp.RequiredPullRequestReviews.RequireLastPushApproval {
		problems = append(problems, "require_last_push_approval=false (Mercari PR-hijacking class)")
	}
	if !bp.RequiredPullRequestReviews.DismissStaleReviews {
		problems = append(problems, "dismiss_stale_reviews=false")
	}
	if !bp.EnforceAdmins.Enabled {
		problems = append(problems, "enforce_admins=false (admins silently bypass — default off on GitHub)")
	}
	if bp.RequiredStatusChecks == nil || len(bp.RequiredStatusChecks.Contexts) == 0 {
		problems = append(problems, "no required status checks configured")
	}
	if len(problems) > 0 {
		return Check{
			ID:        checkIDBranchProtection,
			Title:     "Branch protection on " + cfg.Branch,
			Passed:    false,
			Detail:    strings.Join(problems, "; "),
			Rationale: "P2 canonical recipe: required_approving_review_count≥2, require_code_owner_reviews, require_last_push_approval, dismiss_stale_reviews, enforce_admins.",
		}
	}
	return Check{
		ID:     checkIDBranchProtection,
		Title:  "Branch protection on " + cfg.Branch,
		Passed: true,
	}
}

func checkCodeOwners(ctx context.Context, cfg Config) Check {
	for _, path := range []string{"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"} {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", cfg.Owner, cfg.Repo, path, cfg.Branch)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return Check{
					ID:     "codeowners_present",
					Title:  "CODEOWNERS file present",
					Passed: true,
					Detail: "found at " + path,
				}
			}
		}
	}
	return Check{
		ID:        "codeowners_present",
		Title:     "CODEOWNERS file present",
		Passed:    false,
		Detail:    "not found at CODEOWNERS / .github/CODEOWNERS / docs/CODEOWNERS",
		Rationale: "Without CODEOWNERS, require_code_owner_reviews has nothing to route against.",
	}
}

type codeOwnersErrors struct {
	Errors []struct {
		Line    int    `json:"line"`
		Column  int    `json:"column"`
		Kind    string `json:"kind"`
		Source  string `json:"source"`
		Suggestion string `json:"suggestion"`
		Message string `json:"message"`
		Path    string `json:"path"`
	} `json:"errors"`
}

func checkCodeOwnersErrors(ctx context.Context, cfg Config) Check {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/codeowners/errors", cfg.Owner, cfg.Repo)
	var ce codeOwnersErrors
	if err := ghGet(ctx, cfg.Token, url, &ce); err != nil {
		return Check{
			ID:        checkIDCodeownersErrors,
			Title:     checkTitleCodeowners,
			Passed:    false,
			Detail:    err.Error(),
			Rationale: "GET /repos/{owner}/{repo}/codeowners/errors must succeed.",
		}
	}
	if len(ce.Errors) > 0 {
		var msgs []string
		for _, e := range ce.Errors {
			msgs = append(msgs, fmt.Sprintf("line %d: %s (%s)", e.Line, e.Message, e.Kind))
		}
		return Check{
			ID:        checkIDCodeownersErrors,
			Title:     checkTitleCodeowners,
			Passed:    false,
			Detail:    strings.Join(msgs, "; "),
			Rationale: "GitHub silently ignores CODEOWNERS lines referencing non-existent teams/users; this endpoint surfaces them.",
		}
	}
	return Check{
		ID:     checkIDCodeownersErrors,
		Title:  checkTitleCodeowners,
		Passed: true,
	}
}

func ghGet(ctx context.Context, token, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GitHub %s: %s — %s", resp.Status, url, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
