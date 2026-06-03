//go:build darwin

package secrets

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// nightlyKeychainEnv gates the real-Keychain round-trip behind an
// explicit operator opt-in. CI (GitHub Actions macOS runners) does not
// have an unlocked login.keychain-db; running this test there hangs on
// a pinentry prompt the runner cannot answer. Set
// REGATTA_NIGHTLY_KEYCHAIN_TEST=1 on a nightly-runner job (#616).
const nightlyKeychainEnv = "REGATTA_NIGHTLY_KEYCHAIN_TEST"

// TestKeychain_Darwin_RoundTrip_Nightly is the #616 macOS 15 (Sequoia) compat assertion — Set then Get then Delete a fresh key via real /usr/bin/security.
func TestKeychain_Darwin_RoundTrip_Nightly(t *testing.T) {
	if os.Getenv(nightlyKeychainEnv) == "" {
		t.Skipf("skip: nightly-only (%s unset); see #616", nightlyKeychainEnv)
	}
	// Use a throwaway canonical key shape so the regex passes; the
	// timestamp suffix would collide with the canonical-key regex
	// (digits OK; underscore allowed) only via the existing
	// `regatta.<lower-snake>` pattern.
	key := fmt.Sprintf("regatta.nightly_keychain_test_%d", time.Now().UnixNano())
	value := []byte("nightly-roundtrip-value")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use a unique service to avoid colliding with the operator's
	// production secrets if the runner happens to share a keychain.
	service := fmt.Sprintf("regatta-nightly-%d", time.Now().UnixNano())
	f := NewKeychainFetcher(service)
	setter, ok := f.(interface {
		Set(ctx context.Context, key string, value []byte) error
		Delete(ctx context.Context, key string) error
	})
	if !ok {
		t.Fatalf("keychain fetcher missing Set/Delete; build error")
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
	if err := setter.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.Get(ctx, key); err == nil {
		t.Fatalf("Get after Delete returned nil err; want ErrNotFound")
	}
}
