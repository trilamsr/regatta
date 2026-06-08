//go:build integration_gh

// Integration coverage for issue #895: ghclient.GHCLIClient is
// unit-tested only against Runner fixtures returning canned stdout.
// A future gh release that reshapes `gh issue list --json` output,
// reorders rate-limit headers on the 403 path, or changes the 404
// status-line phrasing would silently break the spec adapter in prod
// without any local signal. This file shells the real gh binary
// against a fixture repo so the contract surfaces here first. Build-tag
// gated (matches internal/orchestrator/prwatch/ghcli_version_integ_test.go
// per docs/engineer/testing-conventions.md) so it stays out of the
// standard CI run; nightly `make ci-integration` pulls it in.

package githubissues

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/ghclient"
)

const fixtureRepoEnv = "REGATTA_GH_FIXTURE_REPO"

// newRealClient returns an owner+repo-bound GHCLIClient against REGATTA_GH_FIXTURE_REPO; skips the calling test when the env or gh binary is absent.
func newRealClient(t *testing.T) *ghclient.GHCLIClient {
	t.Helper()
	slug := os.Getenv(fixtureRepoEnv)
	if slug == "" {
		t.Skipf("%s unset; integration test requires a throwaway owner/repo with `autonomous`-labelled issues", fixtureRepoEnv)
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skipf("gh not on PATH: %v", err)
	}
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || name == "" {
		t.Fatalf("%s=%q is not owner/name", fixtureRepoEnv, slug)
	}
	return ghclient.NewGHCLIClient(owner, name)
}

// TestGHClientReal_ListNonEmpty asserts `gh issue list --json` against the fixture repo parses into ghclient.Issue with the field shape the adapter consumes (#895).
func TestGHClientReal_ListNonEmpty(t *testing.T) {
	c := newRealClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	issues, err := c.ListIssuesByLabelPaginated(ctx, AutonomousLabel, ghclient.ListIssuesOpts{State: "open", Limit: 50})
	if err != nil {
		t.Fatalf("ListIssuesByLabelPaginated: %v", err)
	}
	if len(issues) == 0 {
		t.Fatalf("fixture repo %s has no open `%s`-labelled issues — seed one before running", os.Getenv(fixtureRepoEnv), AutonomousLabel)
	}
	for _, iss := range issues {
		if iss.Number <= 0 {
			t.Errorf("issue Number=%d (expected positive)", iss.Number)
		}
		if iss.Title == "" {
			t.Errorf("issue #%d has empty Title — JSON shape drift", iss.Number)
		}
		if !hasAutonomousLabel(iss.Labels) {
			t.Errorf("issue #%d returned by label filter but missing label in response (labels=%v)", iss.Number, iss.Labels)
		}
		if iss.UpdatedAt.IsZero() {
			t.Errorf("issue #%d UpdatedAt zero — adapter's mid-flight-edit detection (#850) needs this field", iss.Number)
		}
	}
}

// TestGHClientReal_ListEmpty asserts a label that no issue carries returns an empty slice + nil error (not a decode failure) (#895).
func TestGHClientReal_ListEmpty(t *testing.T) {
	c := newRealClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	issues, err := c.ListIssuesByLabelPaginated(ctx, "regatta-integration-no-such-label-xyz", ghclient.ListIssuesOpts{State: "open", Limit: 10})
	if err != nil {
		t.Fatalf("ListIssuesByLabelPaginated empty-label: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected zero issues for unused label, got %d", len(issues))
	}
}

// TestGHClientReal_Get asserts `gh issue view --json` round-trips Body + Labels + UpdatedAt for an issue surfaced by List (#895).
func TestGHClientReal_Get(t *testing.T) {
	c := newRealClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	issues, err := c.ListIssuesByLabelPaginated(ctx, AutonomousLabel, ghclient.ListIssuesOpts{State: "open", Limit: 1})
	if err != nil {
		t.Fatalf("seed List: %v", err)
	}
	if len(issues) == 0 {
		t.Skipf("fixture repo has no open `%s` issues to view-round-trip", AutonomousLabel)
	}
	seed := issues[0]

	got, err := c.GetIssue(ctx, seed.Number)
	if err != nil {
		t.Fatalf("GetIssue(%d): %v", seed.Number, err)
	}
	if got.Number != seed.Number {
		t.Errorf("Number mismatch: list=%d view=%d", seed.Number, got.Number)
	}
	if got.Title != seed.Title {
		t.Errorf("Title round-trip drift on #%d: list=%q view=%q", seed.Number, seed.Title, got.Title)
	}
	if got.Body != seed.Body {
		t.Errorf("Body round-trip drift on #%d — selfimprove dedup-key marker scan (#849) depends on this", seed.Number)
	}
	if !hasAutonomousLabel(got.Labels) {
		t.Errorf("view dropped `%s` label on #%d (labels=%v)", AutonomousLabel, seed.Number, got.Labels)
	}
}

// TestGHClientReal_AuthFailure asserts an empty GH_TOKEN maps to a typed error (ErrNotFound on 404 or ErrPermanent on binary-missing); a bare exec error would skip the adapter's stderr classifier (#848) (#895).
func TestGHClientReal_AuthFailure(t *testing.T) {
	slug := os.Getenv(fixtureRepoEnv)
	if slug == "" {
		t.Skipf("%s unset", fixtureRepoEnv)
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skipf("gh not on PATH: %v", err)
	}
	owner, name, ok := strings.Cut(slug, "/")
	if !ok {
		t.Fatalf("%s=%q is not owner/name", fixtureRepoEnv, slug)
	}
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_CONFIG_DIR", t.TempDir())

	c := ghclient.NewGHCLIClient(owner, name)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := c.ListIssuesByLabelPaginated(ctx, AutonomousLabel, ghclient.ListIssuesOpts{State: "open", Limit: 1})
	if err == nil {
		t.Fatalf("expected auth failure with empty GH_TOKEN, got nil")
	}
	var rate *ghclient.RateLimitedError
	switch {
	case errors.As(err, &rate):
	case errors.Is(err, schemas.ErrNotFound):
	case errors.Is(err, schemas.ErrPermanent):
	default:
		t.Errorf("auth failure mapped to untyped error: %v (want RateLimitedError / ErrNotFound / ErrPermanent)", err)
	}
}
