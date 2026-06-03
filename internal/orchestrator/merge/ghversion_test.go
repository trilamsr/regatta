// ghversion_test pins the boot-time gh-CLI version gate (#656) — the
// floor that protects --match-head-commit from silently failing on gh
// < 2.40.
package merge_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
)

// fakeGhVersionProbe lets tests pin the gh --version string without
// shelling out. Err short-circuits the version comparison for the
// missing-binary case.
type fakeGhVersionProbe struct {
	Out string
	Err error
}

func (f fakeGhVersionProbe) Version(ctx context.Context) (string, error) {
	return f.Out, f.Err
}

// TestVerifyGhVersion_Above2_40_Accepts pins the happy path.
func TestVerifyGhVersion_Above2_40_Accepts(t *testing.T) {
	err := merge.VerifyGhVersion(context.Background(),
		fakeGhVersionProbe{Out: "gh version 2.55.0 (2024-12-01)"}, slog.Default())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// TestVerifyGhVersion_At2_40_Accepts pins the exact-floor boundary.
func TestVerifyGhVersion_At2_40_Accepts(t *testing.T) {
	err := merge.VerifyGhVersion(context.Background(),
		fakeGhVersionProbe{Out: "gh version 2.40.0 (2024-02-01)"}, slog.Default())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// TestVerifyGhVersion_Below2_40_RefusesWithSentinel pins the boot-refusal contract.
func TestVerifyGhVersion_Below2_40_RefusesWithSentinel(t *testing.T) {
	err := merge.VerifyGhVersion(context.Background(),
		fakeGhVersionProbe{Out: "gh version 2.39.9 (2024-01-15)"}, slog.Default())
	if err == nil {
		t.Fatalf("verify returned nil for gh 2.39.9, want ErrGhVersionUnsupported")
	}
	if !errors.Is(err, merge.ErrGhVersionUnsupported) {
		t.Fatalf("err=%v, want ErrGhVersionUnsupported wrap", err)
	}
	if !strings.Contains(err.Error(), "2.40") {
		t.Fatalf("err missing floor reference: %v", err)
	}
}

// TestVerifyGhVersion_ProbeError_PropagatesWithoutSentinel asserts a probe failure (e.g. gh missing) does not impersonate the version-too-old sentinel.
func TestVerifyGhVersion_ProbeError_PropagatesWithoutSentinel(t *testing.T) {
	probeErr := errors.New("exec gh: command not found")
	err := merge.VerifyGhVersion(context.Background(),
		fakeGhVersionProbe{Err: probeErr}, slog.Default())
	if err == nil {
		t.Fatalf("verify returned nil for probe error")
	}
	if errors.Is(err, merge.ErrGhVersionUnsupported) {
		t.Fatalf("probe error misclassified as ErrGhVersionUnsupported: %v", err)
	}
	if !errors.Is(err, probeErr) {
		t.Fatalf("err did not wrap probe error: %v", err)
	}
}

// TestVerifyGhVersion_UnparseableVersion_Refuses asserts a probe output with no semver triple refuses boot instead of silently proceeding.
func TestVerifyGhVersion_UnparseableVersion_Refuses(t *testing.T) {
	err := merge.VerifyGhVersion(context.Background(),
		fakeGhVersionProbe{Out: "gh (some custom build)"}, slog.Default())
	if err == nil {
		t.Fatalf("verify returned nil for unparseable version")
	}
}
