// Package githubissues — selector_test pins #1067: Selector field honored on List() so operator can retarget label/state without code change.
package githubissues

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/ghclient"
)

// TestGitHubIssues_Selector_DefaultsToAutonomousOpen asserts empty selector preserves pre-#1067 behavior.
func TestGitHubIssues_Selector_DefaultsToAutonomousOpen(t *testing.T) {
	gh := &fakeGH{listIssues: []ghclient.Issue{}}
	cfg := GitHubIssuesConfig{
		Client: gh,
		Repo:   Repo{Owner: "o", Name: "r"},
	}
	a, err := NewGitHubIssues(cfg)
	if err != nil {
		t.Fatalf("NewGitHubIssues: %v", err)
	}
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if gh.lastListLabel != AutonomousLabel {
		t.Fatalf("default selector label=%q want %q", gh.lastListLabel, AutonomousLabel)
	}
	if gh.lastListState != "open" {
		t.Fatalf("default selector state=%q want open", gh.lastListState)
	}
}

// TestGitHubIssues_Selector_LabelOnly retargets adapter to non-default label.
func TestGitHubIssues_Selector_LabelOnly(t *testing.T) {
	gh := &fakeGH{listIssues: []ghclient.Issue{}}
	cfg := GitHubIssuesConfig{
		Client:   gh,
		Repo:     Repo{Owner: "o", Name: "r"},
		Selector: "label:roadmap",
	}
	a, err := NewGitHubIssues(cfg)
	if err != nil {
		t.Fatalf("NewGitHubIssues: %v", err)
	}
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if gh.lastListLabel != "roadmap" {
		t.Fatalf("selector label=%q want roadmap", gh.lastListLabel)
	}
	if gh.lastListState != "open" {
		t.Fatalf("selector state=%q want default open", gh.lastListState)
	}
}

// TestGitHubIssues_Selector_LabelAndState honors both clauses.
func TestGitHubIssues_Selector_LabelAndState(t *testing.T) {
	gh := &fakeGH{listIssues: []ghclient.Issue{}}
	cfg := GitHubIssuesConfig{
		Client:   gh,
		Repo:     Repo{Owner: "o", Name: "r"},
		Selector: "label:priority-high state:closed",
	}
	a, err := NewGitHubIssues(cfg)
	if err != nil {
		t.Fatalf("NewGitHubIssues: %v", err)
	}
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if gh.lastListLabel != "priority-high" {
		t.Fatalf("selector label=%q want priority-high", gh.lastListLabel)
	}
	if gh.lastListState != "closed" {
		t.Fatalf("selector state=%q want closed", gh.lastListState)
	}
}

// TestGitHubIssues_Selector_FiltersOnParsedLabel asserts the in-memory filter uses parsed label not hardcoded autonomous.
func TestGitHubIssues_Selector_FiltersOnParsedLabel(t *testing.T) {
	gh := &fakeGH{listIssues: []ghclient.Issue{
		{Number: 1, Title: "ITEM-1: a", Body: "## Acceptance criteria\n- [planned] c1: x\n", Labels: []string{"roadmap"}},
		{Number: 2, Title: "ITEM-2: b", Body: "## Acceptance criteria\n- [planned] c1: x\n", Labels: []string{"autonomous"}},
	}}
	cfg := GitHubIssuesConfig{
		Client:   gh,
		Repo:     Repo{Owner: "o", Name: "r"},
		Selector: "label:roadmap",
	}
	a, err := NewGitHubIssues(cfg)
	if err != nil {
		t.Fatalf("NewGitHubIssues: %v", err)
	}
	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != "ITEM-1" {
		t.Fatalf("expected 1 roadmap item ITEM-1; got %+v", items)
	}
}

// TestGitHubIssues_Selector_Malformed fails closed at constructor with a stable error token. Reviewer pass-1 added: whitespace-only value + duplicate clause + state-only.
func TestGitHubIssues_Selector_Malformed(t *testing.T) {
	for _, sel := range []string{
		"label:",
		"labelroadmap",
		"label:roadmap unknown:foo",
		"state:open",
		"label:   ",
		"label:foo label:bar",
		"state:open state:closed",
	} {
		cfg := GitHubIssuesConfig{
			Client:   &fakeGH{},
			Repo:     Repo{Owner: "o", Name: "r"},
			Selector: sel,
		}
		if _, err := NewGitHubIssues(cfg); err == nil {
			t.Fatalf("selector=%q expected error, got nil", sel)
		}
	}
}

// TestGitHubIssues_Selector_GetHonorsCustomLabel covers the Get() cache-miss-after-TTL path under a non-default selector.
func TestGitHubIssues_Selector_GetHonorsCustomLabel(t *testing.T) {
	gh := &fakeGH{listIssues: []ghclient.Issue{
		{Number: 7, Title: "ITEM-7: hello", Body: "## Acceptance criteria\n- [planned] c1: x\n", Labels: []string{"roadmap"}},
	}}
	cfg := GitHubIssuesConfig{
		Client:   gh,
		Repo:     Repo{Owner: "o", Name: "r"},
		Selector: "label:roadmap",
	}
	a, err := NewGitHubIssues(cfg)
	if err != nil {
		t.Fatalf("NewGitHubIssues: %v", err)
	}
	if _, err := a.Get(context.Background(), "ITEM-7"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gh.lastListLabel != "roadmap" {
		t.Fatalf("Get's cache-miss path label=%q want roadmap", gh.lastListLabel)
	}
}

// TestGitHubIssues_Selector_BoundedMinPoll documents the rate-limit guard: when retargeted to a high-volume label, MinPoll still defaults to 30s.
func TestGitHubIssues_Selector_BoundedMinPoll(t *testing.T) {
	cfg := GitHubIssuesConfig{
		Client:   &fakeGH{},
		Repo:     Repo{Owner: "o", Name: "r"},
		Selector: "label:roadmap",
	}
	a, err := NewGitHubIssues(cfg)
	if err != nil {
		t.Fatalf("NewGitHubIssues: %v", err)
	}
	caps := a.Capabilities()
	if caps.MinPollInterval != 30*time.Second {
		t.Fatalf("MinPollInterval=%v want 30s", caps.MinPollInterval)
	}
}
