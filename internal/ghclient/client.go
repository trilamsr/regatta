// Package ghclient is the shared seam between regatta packages and the
// GitHub REST API. Consumers (alarmwebhook handler, selfimprove
// detector) depend on Client; production wires the HTTP impl in
// alarmwebhook.NewHTTPGitHubClient. Issue carries the union of fields
// every consumer reads — Body for the selfimprove dedup-key marker,
// Title for human-readable logging, Number for the dedup target.
package ghclient

import "context"

// Client is the three-method GitHub seam every regatta consumer needs:
// list-by-label for dedup, create for new findings, comment for refires.
// Adding a method here forces both consumers to grow together — that is
// the unification's load-bearing guarantee.
type Client interface {
	ListOpenIssuesByLabel(ctx context.Context, label, titleSubstr string) ([]Issue, error)
	CreateIssue(ctx context.Context, title, body string, labels []string) (int, error)
	CommentOnIssue(ctx context.Context, number int, body string) error
}

// Issue is the trimmed view of a GitHub issue every consumer reads.
// Body is what selfimprove scans for the `dedup-key:` marker; Title +
// Number are what alarmwebhook logs against. Fields carry json tags so
// the alarmwebhook search/issues decoder can target the type directly.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}
