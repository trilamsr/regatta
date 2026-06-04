// Package ghclient is the shared seam between regatta packages and the
// GitHub REST API. Consumers (alarmwebhook handler, selfimprove
// detector) depend on Client; production wires the HTTP impl in
// alarmwebhook.NewHTTPGitHubClient. Issue carries the union of fields
// every consumer reads — Body for the selfimprove dedup-key marker,
// Title for human-readable logging, Number for the dedup target.
package ghclient

import (
	"context"
	"time"
)

// Client is the unified GitHub seam every regatta consumer needs:
// list-by-label for dedup, create for new findings, comment for refires,
// and the paginated/get/edit triple the github_issues spec adapter
// (MVR-1-T4) uses for autonomous-issue consumption. Adding a method
// here forces every implementation to grow together — that is the
// unification's load-bearing guarantee.
type Client interface {
	ListOpenIssuesByLabel(ctx context.Context, label, titleSubstr string) ([]Issue, error)
	CreateIssue(ctx context.Context, title, body string, labels []string) (int, error)
	CommentOnIssue(ctx context.Context, number int, body string) error
	ListIssuesByLabelPaginated(ctx context.Context, label string, opts ListIssuesOpts) ([]Issue, error)
	GetIssue(ctx context.Context, number int) (Issue, error)
	EditIssueBody(ctx context.Context, number int, body string) error
}

// ListIssuesOpts tunes ListIssuesByLabelPaginated; zero State means
// "open" and zero Limit means the implementation default (1000 for
// gh-CLI-backed clients).
type ListIssuesOpts struct {
	State string
	Limit int
}

// Issue is the trimmed view of a GitHub issue every consumer reads.
// Body is what selfimprove scans for the `dedup-key:` marker and what
// the github_issues spec adapter parses for the regatta metadata block;
// Title + Number are what alarmwebhook logs against. Labels + UpdatedAt
// were added for the github_issues adapter (MVR-1-T4) — additive only.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Labels    []string  `json:"labels,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}
