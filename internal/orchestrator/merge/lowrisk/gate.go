package lowrisk

import "context"

// PRFetcher resolves the PR value-object the classifier needs from a PR
// number + head SHA. It is the single coupling point to GitHub; keeping
// it an injected func keeps the classifier pure and testable.
type PRFetcher func(ctx context.Context, prNumber int, headSHA string) (PR, error)

// Gate adapts a Classifier to the scheduler's LowRiskGate interface:
// fetch the PR metadata, then classify. It fails CLOSED — a fetch error
// HOLDS the PR (reason "fetch_failed") rather than risking an
// auto-merge on stale or missing data.
type Gate struct {
	classifier *Classifier
	fetch      PRFetcher
}

// NewGate wires a Gate from a Classifier and a PRFetcher.
func NewGate(c *Classifier, fetch PRFetcher) *Gate {
	return &Gate{classifier: c, fetch: fetch}
}

// Eligible fetches the PR then runs the classifier. Fetch failure holds
// the PR (fail-closed) so a transient GitHub error never widens the
// auto-merge surface.
func (g *Gate) Eligible(ctx context.Context, prNumber int, headSHA string) (bool, string) {
	pr, err := g.fetch(ctx, prNumber, headSHA)
	if err != nil {
		return false, "fetch_failed"
	}
	return g.classifier.Classify(pr)
}

// HoldAll is the conservative-default gate: it HOLDS every PR with
// reason "low_risk_disabled". Wired when --auto-merge=true but the
// low-risk gate is disabled, so auto-merge never widens past the
// pre-MAY-86 (operator-merge-everything) surface.
type HoldAll struct{}

// Eligible always reports false so nothing auto-merges.
func (HoldAll) Eligible(_ context.Context, _ int, _ string) (bool, string) {
	return false, "low_risk_disabled"
}
