package rejectionrouter

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// labelFailuresCounterName is the OTel counter the router exports
// every gh CLI label failure under. The name follows the
// `regatta.<package>.<event>_total` convention pinned by the spend +
// l4 retrofits; the cardinality lint walks for banned
// per-request labels (pr_number, work_item_id, run_id) on this scope.
const labelFailuresCounterName = "regatta.rejection_router.label_failures_total"

// Failure-reason values for the `reason` attribute on
// labelFailuresCounterName. The closed set keeps counter cardinality
// bounded so the lint guard stays green and the operator dashboard
// has a finite legend.
const (
	reasonAbsent      = "absent"
	reasonRateLimited = "rate_limited"
	reasonUnknown     = "unknown"
)

// ghRunner is the seam tests substitute to avoid forking the real gh
// binary. Production wires execRunner; tests inject a no-network fake
// (the package's *_test.go uses a temp-dir shell script via PATH so
// classifier logic stays exercised end-to-end without an extra mock).
type ghRunner func(ctx context.Context, args []string) ([]byte, error)

// GHLabeler labels PRs via the `gh` CLI. The work_item_id is assumed
// to be the PR number expressed as a string (matching the existing
// adapter convention in cmd/gh-followup-to-items). `gh pr edit
// --add-label` is idempotent — re-labeling an already-labeled PR
// exits 0 — so retries are safe.
//
// Construct via NewGHLabeler so the failure counter wires once at
// startup against the supplied Meter (or the global provider when
// nil per the spend / l4 fallback contract).
type GHLabeler struct {
	repo     string
	run      ghRunner
	failures metric.Int64Counter
}

// GHLabelerOptions wires GHLabeler dependencies at construction time
// so the counter (and any future instruments) attach once per
// process. Meter follows the Config.Meter DI seam pinned by T0a Config.Meter retrofit.
type GHLabelerOptions struct {
	// Repo is the "owner/name" passed via --repo. Empty falls back to
	// gh's default-repo resolution (current git remote).
	Repo string

	// Meter is the OTel instrument factory. Nil resolves to the
	// global provider's scoped meter so callers that have not opted
	// into telemetry still get a zero-cost noop counter wired.
	Meter metric.Meter
}

// NewGHLabeler builds a GHLabeler with the failure counter wired
// against opts.Meter (or the global provider's scoped meter when
// nil). A counter-construction error degrades to no-op telemetry — a
// metric init failure must not block PR labeling on the escalation
// path.
func NewGHLabeler(opts GHLabelerOptions) *GHLabeler {
	g := &GHLabeler{
		repo: opts.Repo,
		run:  execRunner,
	}
	meter := resolveMeter(opts.Meter)
	if c, err := meter.Int64Counter(
		labelFailuresCounterName,
		metric.WithDescription("gh CLI label failures classified by reason"),
		metric.WithUnit("{failure}"),
	); err == nil {
		g.failures = c
	}
	return g
}

// AddLabel runs `gh pr edit <workItemID> --add-label <label>`.
// Returns a wrapped error including stderr so the daemon log carries
// enough context to diagnose without re-running.
//
// On failure, increments labelFailuresCounterName with
// reason={absent|rate_limited|unknown}. The increment happens before
// the error returns so a labeler-retry loop double-counts each
// attempt — that is intentional: the dashboard signal is per-attempt
// failure rate, not unique-PR failure count.
func (g *GHLabeler) AddLabel(ctx context.Context, workItemID, label string) error {
	args := []string{"pr", "edit", workItemID, "--add-label", label}
	if g.repo != "" {
		args = append(args, "--repo", g.repo)
	}
	run := g.run
	if run == nil {
		run = execRunner
	}
	out, err := run(ctx, args)
	if err != nil {
		g.recordFailure(ctx, classifyGHError(string(out)))
		return fmt.Errorf("gh pr edit %s --add-label %s: %w (%s)",
			workItemID, label, err, string(out))
	}
	return nil
}

func (g *GHLabeler) recordFailure(ctx context.Context, reason string) {
	if g.failures == nil {
		return
	}
	g.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// classifyGHError maps gh CLI stderr to the closed reason set. gh
// does not expose a structured error code on `pr edit`, so the
// matcher keys off stable user-facing strings the binary has emitted
// across the 2.x release line. Adding a new bucket means adding both
// a constant above AND a case here AND updating the runbook legend.
func classifyGHError(combined string) string {
	s := strings.ToLower(combined)
	switch {
	case strings.Contains(s, "rate limit") ||
		strings.Contains(s, "http 429") ||
		strings.Contains(s, "secondary rate limit"):
		return reasonRateLimited
	case strings.Contains(s, "not found") ||
		strings.Contains(s, "could not add label") ||
		strings.Contains(s, "label does not exist"):
		return reasonAbsent
	default:
		return reasonUnknown
	}
}

func execRunner(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...) //nolint:gosec // G204: literal binary; args from typed inputs (work_item_id + label)
	return cmd.CombinedOutput()
}
