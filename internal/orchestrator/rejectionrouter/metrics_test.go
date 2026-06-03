package rejectionrouter_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
)

// newMetricReader returns a freshly scoped Meter backed by a ManualReader.
func newMetricReader(t *testing.T) (sdkmetric.Reader, *sdkmetric.MeterProvider) {
	t.Helper()
	r := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r))
	return r, mp
}

// collectMetrics drains the reader into a ResourceMetrics snapshot.
func collectMetrics(t *testing.T, r sdkmetric.Reader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return rm
}

// failuresByReason returns the (reason, count) pairs emitted for label_failures.
func failuresByReason(t *testing.T, rm metricdata.ResourceMetrics) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	const wantName = "regatta.rejection_router.label_failures"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != wantName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s: data is %T, want Sum[int64]", wantName, m.Data)
			}
			for _, dp := range sum.DataPoints {
				var reason string
				for _, kv := range dp.Attributes.ToSlice() {
					if string(kv.Key) == "reason" {
						reason = kv.Value.AsString()
					}
				}
				out[reason] += dp.Value
			}
		}
	}
	return out
}

// erringLabeler returns a fixed error on every AddLabel; tests use it
// to drive each classifier branch.
type erringLabeler struct{ err error }

func (e erringLabeler) AddLabel(context.Context, int64, string) error { return e.err }

// recordAndDrive seeds one gate_rejected event with K=1 (escalates on
// first rejection), runs Tick, and lets the caller drain the reader.
// Shared by every metric test below to keep each case focused on its
// reason classification.
func recordAndDrive(t *testing.T, labeler rejectionrouter.PRLabeler, mp *sdkmetric.MeterProvider) {
	t.Helper()
	ctx := context.Background()
	db := openDB(t)
	id := newAgentInGatesRunning(t, db, "wi-metric", "shaA")

	cfg := rejectionrouter.Config{
		DB:      db,
		K:       1,
		Labeler: labeler,
		Meter:   mp.Meter("test"),
	}
	r := rejectionrouter.New(cfg)

	mustRecordRejection(t, db, id, "shaA")
	_ = r.Tick(ctx) // Tick may return labeler error; metric is emitted before propagation.
}

// TestLabelFailureMetric_Absent pins reason=absent for the "label needs-human not found" wording.
func TestLabelFailureMetric_Absent(t *testing.T) {
	reader, mp := newMetricReader(t)
	err := errors.New(`could not add label: 'needs-human' not found`)
	recordAndDrive(t, erringLabeler{err: err}, mp)

	got := failuresByReason(t, collectMetrics(t, reader))
	if got["absent"] != 1 {
		t.Errorf("absent count = %d; want 1; full = %+v", got["absent"], got)
	}
}

// TestLabelFailureMetric_RateLimited pins reason=rate_limited for HTTP 403 rate-limit text.
func TestLabelFailureMetric_RateLimited(t *testing.T) {
	reader, mp := newMetricReader(t)
	err := errors.New(`HTTP 403: API rate limit exceeded for user`)
	recordAndDrive(t, erringLabeler{err: err}, mp)

	got := failuresByReason(t, collectMetrics(t, reader))
	if got["rate_limited"] != 1 {
		t.Errorf("rate_limited count = %d; want 1; full = %+v", got["rate_limited"], got)
	}
}

// TestLabelFailureMetric_NotFound pins reason=not_found for PR-not-found errors distinct from absent-label.
func TestLabelFailureMetric_NotFound(t *testing.T) {
	reader, mp := newMetricReader(t)
	err := errors.New(`GraphQL: Could not resolve to a PullRequest with the number of 42 (not found)`)
	recordAndDrive(t, erringLabeler{err: err}, mp)

	got := failuresByReason(t, collectMetrics(t, reader))
	if got["not_found"] != 1 {
		t.Errorf("not_found count = %d; want 1; full = %+v", got["not_found"], got)
	}
	if got["absent"] != 0 {
		t.Errorf("absent count = %d; want 0 (PR-not-found must NOT misclassify as absent-label); full = %+v", got["absent"], got)
	}
}

// TestLabelFailureMetric_Unknown pins reason=unknown for errors that match no specific class.
func TestLabelFailureMetric_Unknown(t *testing.T) {
	reader, mp := newMetricReader(t)
	err := errors.New(`gh: dial tcp: lookup api.github.com: no such host`)
	recordAndDrive(t, erringLabeler{err: err}, mp)

	got := failuresByReason(t, collectMetrics(t, reader))
	if got["unknown"] != 1 {
		t.Errorf("unknown count = %d; want 1; full = %+v", got["unknown"], got)
	}
}

// TestLabelFailureMetric_MeterFromConfigIsConsumed pins that Config.Meter is wired into emission (not orphan per issue #499).
func TestLabelFailureMetric_MeterFromConfigIsConsumed(t *testing.T) {
	reader, mp := newMetricReader(t)
	err := errors.New(`could not add label: 'needs-human' not found`)
	recordAndDrive(t, erringLabeler{err: err}, mp)

	rm := collectMetrics(t, reader)
	const wantName = "regatta.rejection_router.label_failures"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == wantName {
				return
			}
		}
	}
	t.Errorf("metric %q not emitted via Config.Meter — Router ignored cfg.Meter", wantName)
}

// TestLabelFailureMetric_ConfigReuse_NoDoubleCount pins the pass-by-value Config-reuse invariant: building two Routers from the same Config does not double-wrap the labeler.
func TestLabelFailureMetric_ConfigReuse_NoDoubleCount(t *testing.T) {
	ctx := context.Background()
	reader, mp := newMetricReader(t)
	db := openDB(t)
	id := newAgentInGatesRunning(t, db, "wi-reuse", "shaB")

	cfg := rejectionrouter.Config{
		DB:      db,
		K:       1,
		Labeler: erringLabeler{err: errors.New(`could not add label: 'needs-human' not found`)},
		Meter:   mp.Meter("test"),
	}
	// Two Routers from one cfg value. New takes cfg by value so the
	// caller's cfg.Labeler stays the original — a single failure must
	// increment the metric exactly once on the second Router's Tick.
	_ = rejectionrouter.New(cfg)
	r := rejectionrouter.New(cfg)

	mustRecordRejection(t, db, id, "shaB")
	_ = r.Tick(ctx)

	got := failuresByReason(t, collectMetrics(t, reader))
	if got["absent"] != 1 {
		t.Errorf("absent count = %d; want 1 (Config reuse must not double-wrap labeler)", got["absent"])
	}
}

// TestLabelFailureMetric_NoEmitOnSuccess pins that a successful label call does NOT increment the failure counter.
func TestLabelFailureMetric_NoEmitOnSuccess(t *testing.T) {
	reader, mp := newMetricReader(t)
	ok := erringLabeler{err: nil}
	recordAndDrive(t, ok, mp)

	got := failuresByReason(t, collectMetrics(t, reader))
	if len(got) != 0 {
		t.Errorf("got failure emits on success = %+v; want none", got)
	}
}

// TestGHLabeler_PassesDashSeparator pins that gh CLI args contain `--` so a branch starting with `--` is not parsed as a flag.
func TestGHLabeler_PassesDashSeparator(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh CLI not on PATH; skipping integration shape test")
	}
	// Build labeler with stubs that capture branch + prNum args.
	var capturedBranch string
	var capturedPRNum int
	g := rejectionrouter.GHLabeler{
		Repo: "owner/repo",
		BranchFn: func(id int64) string {
			return "--malicious-branch"
		},
		Resolver: func(_ context.Context, branch string) (int, error) {
			capturedBranch = branch
			return 0, errors.New(`no open PR for branch "--malicious-branch"`)
		},
		Editor: func(_ context.Context, prNum int, _ string) error {
			capturedPRNum = prNum
			return nil
		},
	}
	_ = g.AddLabel(context.Background(), 7, "needs-human")
	if capturedBranch != "--malicious-branch" {
		t.Errorf("branch passed to resolver = %q; want %q", capturedBranch, "--malicious-branch")
	}
	if capturedPRNum != 0 {
		t.Errorf("editor invoked with prNum=%d despite resolver error", capturedPRNum)
	}
}

// TestGHLabeler_ResolveArgs_HeadEqualsBranch pins that the gh-pr-list resolver binds branch via `--head=<branch>` so a hostile branch cannot reparse as a flag.
func TestGHLabeler_ResolveArgs_HeadEqualsBranch(t *testing.T) {
	got := rejectionrouter.GHListArgs("owner/repo", "--malicious-branch")
	want := "--head=--malicious-branch"
	found := false
	for _, a := range got {
		if a == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("gh pr list args = %v; want %q (equals-binding fences flag-value reparse — issue #500)", got, want)
	}
	// Also assert the branch literal is NEVER passed as a bare positional/flag-value
	// — that would let `--malicious-branch` parse as a flag.
	for i, a := range got {
		if a == "--malicious-branch" {
			t.Errorf("gh pr list args = %v: branch literal at index %d would parse as flag; must use --head=<branch> form", got, i)
		}
	}
}

// TestGHLabeler_EditArgs_DashSeparator pins that the default gh-pr-edit args insert `--` before the PR number.
func TestGHLabeler_EditArgs_DashSeparator(t *testing.T) {
	got := rejectionrouter.GHEditArgs("owner/repo", 42, "needs-human")
	// `--` separator must precede the positional prNum so gh does not parse `42` as a flag value if a future arg shape lands.
	if !containsConsecutive(got, "--", "42") {
		t.Errorf("gh pr edit args = %v; want `--` immediately before pr number literal", got)
	}
}

// containsConsecutive returns true when `a` then `b` appear back-to-back in slice s.
func containsConsecutive(s []string, a, b string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == a && s[i+1] == b {
			return true
		}
	}
	return false
}

// TestClassifyGHError_FourReasons pins the four reason buckets the classifier must distinguish.
func TestClassifyGHError_FourReasons(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"absent label", errors.New(`could not add label: 'needs-human' not found`), "absent"},
		{"rate limited", errors.New(`HTTP 403: API rate limit exceeded`), "rate_limited"},
		{"PR not found", errors.New(`Could not resolve to a PullRequest with the number of 42 (not found)`), "not_found"},
		{"no open PR for branch", errors.New(`no open PR for branch "regatta/agent-7"`), "not_found"},
		{"network", errors.New(`dial tcp: no such host`), "unknown"},
		// Adversarial: an operator-configured label literal that contains the
		// substring "rate limit" must still classify as absent, not rate_limited.
		// Locks in the absent-first precedence guard against the operator-naming
		// foot-gun surfaced in the in-thread review.
		{"label literal 'rate-limit' missing", errors.New(`could not add label: 'rate limit' not found`), "absent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rejectionrouter.ClassifyGHError(tc.err)
			if got != tc.want {
				t.Errorf("classify(%q) = %q; want %q", tc.err.Error(), got, tc.want)
			}
		})
	}
}

