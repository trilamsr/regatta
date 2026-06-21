package rejectionrouter

import (
	"context"
	"testing"
	"time"
)

// TestResolvePRViaGH_TimesOutWhenGHHangs asserts per-call timeout fires on hung gh (MAY-50).
func TestResolvePRViaGH_TimesOutWhenGHHangs(t *testing.T) {
	t.Parallel()
	prevTimeout := defaultGHLabelerTimeout
	defaultGHLabelerTimeout = 200 * time.Millisecond
	prevBin := ghLabelerBinary
	ghLabelerBinary = "sleep"
	prevArgs := ghHangArgs
	ghHangArgs = []string{"30"}
	t.Cleanup(func() {
		defaultGHLabelerTimeout = prevTimeout
		ghLabelerBinary = prevBin
		ghHangArgs = prevArgs
	})

	g := GHLabeler{}
	start := time.Now()
	_, err := g.resolvePRViaGH(context.Background(), "regatta/agent-1")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("want timeout error, got nil after %s", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("resolvePRViaGH returned in %s; want ≤2s — per-call timeout did not fire (err=%v)", elapsed, err)
	}
}

// TestEditPRViaGH_TimesOutWhenGHHangs asserts per-call timeout fires on hung gh (MAY-50).
func TestEditPRViaGH_TimesOutWhenGHHangs(t *testing.T) {
	t.Parallel()
	prevTimeout := defaultGHLabelerTimeout
	defaultGHLabelerTimeout = 200 * time.Millisecond
	prevBin := ghLabelerBinary
	ghLabelerBinary = "sleep"
	prevArgs := ghHangArgs
	ghHangArgs = []string{"30"}
	t.Cleanup(func() {
		defaultGHLabelerTimeout = prevTimeout
		ghLabelerBinary = prevBin
		ghHangArgs = prevArgs
	})

	g := GHLabeler{}
	start := time.Now()
	err := g.editPRViaGH(context.Background(), 42, "needs-human")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("want timeout error, got nil after %s", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("editPRViaGH returned in %s; want ≤2s — per-call timeout did not fire (err=%v)", elapsed, err)
	}
}
