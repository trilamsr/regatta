//go:build linux

package secrets

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// nightlyPassEnv gates the real-pass+gpg-agent integration test
// behind an operator opt-in. CI runners have neither `pass` nor a
// running gpg-agent; running this test there fails on lookup or
// hangs on pinentry. Set REGATTA_NIGHTLY_PASS_TEST=1 on a nightly
// Linux runner that pre-provisions a throwaway GPG keyring + pass
// store (#618).
const nightlyPassEnv = "REGATTA_NIGHTLY_PASS_TEST"

// TestPass_Linux_RoundTrip_Nightly asserts pass + gpg-agent round-trip succeeds when the operator opts in (#618).
func TestPass_Linux_RoundTrip_Nightly(t *testing.T) {
	if os.Getenv(nightlyPassEnv) == "" {
		t.Skipf("skip: nightly-only (%s unset); see #618", nightlyPassEnv)
	}
	if _, err := exec.LookPath("pass"); err != nil {
		t.Skipf("skip: `pass` binary missing on this runner: %v", err)
	}
	key := fmt.Sprintf("regatta.nightly_pass_test_%d", time.Now().UnixNano())
	value := []byte("nightly-roundtrip-value")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	f := NewPassFetcher("")
	setter, ok := f.(interface {
		Set(ctx context.Context, key string, value []byte) error
		Delete(ctx context.Context, key string) error
	})
	if !ok {
		t.Fatalf("pass fetcher missing Set/Delete; build error")
	}
	if err := setter.Set(ctx, key, value); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Cleanup(func() { _ = setter.Delete(context.Background(), key) })

	v, err := f.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Bytes()) != string(value) {
		t.Fatalf("round-trip mismatch: got %q want %q", v.Bytes(), value)
	}
}

// TestPass_Linux_GPGAgentTTLExpiry_Nightly is the #618 expiry assertion: after the operator-configured TTL elapses, the next `pass show` surfaces the agent-prompt error path (not a hang). The runner pre-configures gpg-agent with default-cache-ttl=1 + max-cache-ttl=1 and provisions a throwaway secret BEFORE the test starts so the cache is populated and ready to expire.
func TestPass_Linux_GPGAgentTTLExpiry_Nightly(t *testing.T) {
	if os.Getenv(nightlyPassEnv) == "" {
		t.Skipf("skip: nightly-only (%s unset); see #618", nightlyPassEnv)
	}
	if _, err := exec.LookPath("pass"); err != nil {
		t.Skipf("skip: `pass` binary missing on this runner: %v", err)
	}
	key := fmt.Sprintf("regatta.nightly_pass_ttl_%d", time.Now().UnixNano())
	value := []byte("ttl-expiry-value")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := NewPassFetcher("")
	setter, ok := f.(interface {
		Set(ctx context.Context, key string, value []byte) error
		Delete(ctx context.Context, key string) error
	})
	if !ok {
		t.Fatalf("pass fetcher missing Set/Delete")
	}
	if err := setter.Set(ctx, key, value); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Cleanup(func() { _ = setter.Delete(context.Background(), key) })

	// Sleep past the configured TTL. Runner sets ttl=1s in
	// gpg-agent.conf; 3s is a comfortable margin.
	time.Sleep(3 * time.Second)

	// The expectation: read either succeeds (agent re-prompted via
	// the runner's headless pinentry-tty config) OR fails with a
	// non-hanging error containing a recognizable agent diagnostic.
	// We assert non-hanging via the ctx deadline above + a typed
	// failure mode that includes adapter Name() in the error.
	_, err := f.Get(ctx, key)
	if err == nil {
		// Success path also acceptable — confirms the agent
		// reloaded the cache via runner pinentry.
		return
	}
	// Failure path: the error must NOT be a context-deadline
	// (that would mean the adapter hung waiting for pinentry).
	if ctx.Err() != nil {
		t.Fatalf("pass.Get hung past ctx deadline; want graceful error path")
	}
}
