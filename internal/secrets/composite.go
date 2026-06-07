package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// disableEnv suppresses the platform adapters (keychain / pass) at
// boot. CI runners set this to skip the 30-second keychain-prompt
// timeout (R2) — explicit knob fails loudly and adds zero latency.
const disableEnv = "REGATTA_SECRETS_DISABLE_KEYCHAIN"

// composite chains Fetchers; first non-ErrNotFound (and non-
// ErrUnsupported) wins. A real error short-circuits the chain so the
// operator sees the underlying cause rather than a silent fallthrough.
type composite struct {
	fs []Fetcher
}

// NewComposite returns a chain in given order. Empty chain is a
// programming error — Default is the usual constructor.
func NewComposite(fs ...Fetcher) Fetcher {
	return composite{fs: fs}
}

// Name lists the chain for diagnostics, e.g. "keychain→env".
func (c composite) Name() string {
	if len(c.fs) == 0 {
		return "composite(empty)"
	}
	out := c.fs[0].Name()
	for _, f := range c.fs[1:] {
		out += "→" + f.Name()
	}
	return out
}

// Get walks the chain, returning the first hit. ErrUnsupported is
// treated identically to ErrNotFound (adapter simply not available
// here); anything else aborts so misconfig surfaces loudly rather
// than silently falling back to a stale env entry.
func (c composite) Get(ctx context.Context, key string) (Value, error) {
	v, _, err := c.GetWithSource(ctx, key)
	return v, err
}

// GetWithSource walks the chain and returns the resolving adapter's
// Name() alongside the value — the per-key source operators need to
// debug rotation drift (#653 reopen-trigger).
func (c composite) GetWithSource(ctx context.Context, key string) (Value, string, error) {
	if err := ValidateKey(key); err != nil {
		return Value{}, "", err
	}
	for _, f := range c.fs {
		v, err := f.Get(ctx, key)
		if err == nil {
			return v, f.Name(), nil
		}
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnsupported) {
			continue
		}
		return Value{}, f.Name(), fmt.Errorf("%s: %w", f.Name(), err)
	}
	return Value{}, sourceMissing, ErrNotFound
}

// Default returns the platform-correct chain. On darwin: keychain →
// legacy-env → env. On linux: pass → legacy-env → env. Anywhere else
// (or with the disable knob set): legacy-env → env. legacy-env carries
// the pre-#911 env-var names (ANTHROPIC_API_KEY, REGATTA_HMAC_KEY, …)
// so callers reading via canonical key see back-compat resolution.
func Default(_ context.Context) Fetcher {
	if os.Getenv(disableEnv) == "1" {
		return NewComposite(NewAliasEnvFetcher(), NewEnvFetcher())
	}
	fs := platformAdapters()
	fs = append(fs, NewAliasEnvFetcher(), NewEnvFetcher())
	return NewComposite(fs...)
}
