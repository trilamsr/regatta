package lowrisk

import "context"

// PRFetcher resolves the PR value-object the classifier needs from a PR
// number + head SHA. It is the single coupling point to GitHub; keeping
// it an injected func keeps the classifier pure and testable.
type PRFetcher func(ctx context.Context, prNumber int, headSHA string) (PR, error)

// AuditDecision is the immutable record one Eligible() call produces.
// Carries enough operator-debuggable context (which PR, why held, how
// far from the soak / LOC line) that substrate_events readers can
// reconstruct the decision without re-fetching from GitHub. Payload
// shape is the contract substrate's KindMergeLowRiskDecision validator
// pins (MAY-270).
type AuditDecision struct {
	PRNumber         int
	Eligible         bool
	Reason           string
	LOCDelta         int
	SoakRemainingSec int64
}

// AuditSink receives one AuditDecision per Eligible() call. The default
// nil sink keeps the pre-MAY-270 path byte-equivalent — emission is
// opt-in so a deployment that never enables --auto-merge pays nothing.
type AuditSink func(ctx context.Context, d AuditDecision)

// reasonFetchFailed names the fail-closed verdict the Gate uses when
// the injected PRFetcher errors. Shared by the audit-emit path and the
// return value so the operator-visible token stays a single literal.
const reasonFetchFailed = "fetch_failed"

// Gate adapts a Classifier to the scheduler's LowRiskGate interface:
// fetch the PR metadata, then classify. It fails CLOSED — a fetch error
// HOLDS the PR (reason "fetch_failed") rather than risking an
// auto-merge on stale or missing data.
type Gate struct {
	classifier *Classifier
	fetch      PRFetcher
	sink       AuditSink
}

// NewGate wires a Gate from a Classifier and a PRFetcher.
func NewGate(c *Classifier, fetch PRFetcher) *Gate {
	return &Gate{classifier: c, fetch: fetch}
}

// SetAuditSink installs sink as the audit-emission target. Subsequent
// Eligible() calls invoke sink exactly once with the decision row.
// Replacing the sink is allowed (callers MAY rebind during reconfigure);
// passing nil clears emission and restores byte-equivalent behavior.
func (g *Gate) SetAuditSink(sink AuditSink) { g.sink = sink }

// Eligible fetches the PR then runs the classifier. Fetch failure holds
// the PR (fail-closed) so a transient GitHub error never widens the
// auto-merge surface. The audit sink fires on EVERY decision path —
// hold, pass, fetch-failure — so substrate_events carries the complete
// classify history regardless of outcome.
func (g *Gate) Eligible(ctx context.Context, prNumber int, headSHA string) (bool, string) {
	pr, err := g.fetch(ctx, prNumber, headSHA)
	if err != nil {
		g.emit(ctx, AuditDecision{PRNumber: prNumber, Eligible: false, Reason: reasonFetchFailed})
		return false, reasonFetchFailed
	}
	eligible, reason := g.classifier.Classify(pr)
	g.emit(ctx, AuditDecision{
		PRNumber:         prNumber,
		Eligible:         eligible,
		Reason:           reason,
		LOCDelta:         pr.DiffLOC,
		SoakRemainingSec: g.classifier.soakRemainingSec(pr),
	})
	return eligible, reason
}

func (g *Gate) emit(ctx context.Context, d AuditDecision) {
	if g.sink == nil {
		return
	}
	g.sink(ctx, d)
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
