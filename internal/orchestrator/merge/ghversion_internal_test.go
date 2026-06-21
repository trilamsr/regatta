package merge

import (
	"context"
	"testing"
	"time"
)

// TestDefaultGhVersionProbe_TimesOutWhenGHHangs asserts per-call timeout fires when gh hangs (MAY-50).
func TestDefaultGhVersionProbe_TimesOutWhenGHHangs(t *testing.T) {
	t.Parallel()
	prevTimeout := defaultGhVersionTimeout
	defaultGhVersionTimeout = 200 * time.Millisecond
	prevBin := ghVersionBinary
	ghVersionBinary = "sleep"
	prevArg := ghVersionArg
	ghVersionArg = "30"
	t.Cleanup(func() {
		defaultGhVersionTimeout = prevTimeout
		ghVersionBinary = prevBin
		ghVersionArg = prevArg
	})

	start := time.Now()
	_, err := defaultGhVersionProbe{}.Version(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("want timeout error, got nil after %s", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Version returned in %s; want ≤2s — per-call timeout did not fire (err=%v)", elapsed, err)
	}
}
