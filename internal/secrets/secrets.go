// Package secrets fetches operator credentials from an OS-resident
// store (macOS Keychain, Linux pass) with env-var fallback, so
// `regatta serve` does not block on a human typing tokens on every
// supervisor wake. Spec:
// docs/engineer/specs/2026-06-02-phase-autonomy-w6-secret-credential-fetch.md.
//
// Value is a redaction wrapper with ZERO exported fields —
// reflection-based formatters (fmt %v/%+v, encoding/json, slog
// Text+JSON handlers) cannot reach the underlying bytes. Bytes() is
// the only legitimate consumer; call-site grep is the audit gate.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
)

// ErrNotFound signals a clean miss so chains can distinguish
// missing-key from a real adapter error; anything else aborts.
var ErrNotFound = errors.New("secret not found")

// ErrUnsupported is returned by adapters whose backend is unavailable
// on the current platform (e.g. keychain on Linux); treated the same
// as ErrNotFound for chain traversal.
var ErrUnsupported = errors.New("secret backend unsupported on this platform")

// Canonical key names; CLI entry validates against keyRe so unknown
// keys reject at the boundary (no path-traversal via `pass show
// ../etc/passwd`).
const (
	KeyAnthropic      = "regatta.anthropic_api_key"
	KeyLinear         = "regatta.linear_api_key"
	KeyGHToken        = "regatta.gh_token"
	KeyBriefHMACs     = "regatta.brief_hmac_keys"
	KeyAuditHMACKey   = "regatta.audit_hmac_key"
	KeyApprovalToken  = "regatta.approval_token_key"
)

// CanonicalKeys is the boot-time fetch set; adapters are independent.
var CanonicalKeys = []string{KeyAnthropic, KeyLinear, KeyGHToken, KeyBriefHMACs, KeyAuditHMACKey, KeyApprovalToken}

const (
	redactedSentinel = "<redacted>" // single source so test + prod cannot drift
	sourceMissing    = "missing"    // chain returned ErrNotFound (vs never-resolved)
)

// Public adapter Name() values; audit-event consumers match exactly.
const (
	AdapterEnv      = "env"
	AdapterKeychain = "keychain"
	AdapterPass     = "pass"
	AdapterFile     = "file"
	AdapterAlias    = "env_alias"
)

// keyRe pins the canonical key shape; path-traversal via key name
// (R12) is structurally prevented.
var keyRe = regexp.MustCompile(`^regatta\.[a-z0-9_]+$`)

// ValidateKey returns nil iff key matches the canonical shape.
func ValidateKey(key string) error {
	if !keyRe.MatchString(key) {
		return fmt.Errorf("invalid secret key %q (must match %s)", key, keyRe.String())
	}
	return nil
}

// Fetcher reads a secret by canonical key; absent returns ErrNotFound.
// Name identifies the adapter for diagnostics + audit.
type Fetcher interface {
	Get(ctx context.Context, key string) (Value, error)
	Name() string
}

// SourceFetcher is the optional extension implemented by chains that
// report WHICH adapter resolved a key. Operators reading `regatta
// secret status` need per-key source to debug rotation drift;
// chain-name alone (e.g. "keychain→env") collapses the answer (#653).
type SourceFetcher interface {
	GetWithSource(ctx context.Context, key string) (Value, string, error)
}

// GetWithSource centralizes the SourceFetcher type-assert so callers
// do not duplicate it.
func GetWithSource(ctx context.Context, f Fetcher, key string) (Value, string, error) {
	if sf, ok := f.(SourceFetcher); ok {
		return sf.GetWithSource(ctx, key)
	}
	v, err := f.Get(ctx, key)
	if err != nil {
		return Value{}, f.Name(), err
	}
	return v, f.Name(), nil
}

// Value wraps a secret byte slice. ZERO exported fields so
// reflection-based formatters cannot reach the underlying bytes.
type Value struct {
	b []byte
}

// NewValue copies b so caller mutation does not bleed into the cache.
func NewValue(b []byte) Value {
	if len(b) == 0 {
		return Value{}
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return Value{b: cp}
}

// Bytes is the only legitimate accessor; grep-gated to the allowlist
// of call sites that hand the secret to an external API.
func (v Value) Bytes() []byte { return v.b }

// Len reports length without exposing bytes — safe for presence checks.
func (v Value) Len() int { return len(v.b) }

// String redacts; fmt %v / %s lands here.
func (v Value) String() string { return redactedSentinel }

// GoString redacts; fmt %#v lands here.
func (v Value) GoString() string { return redactedSentinel }

// MarshalJSON redacts; json.Marshal lands here.
func (v Value) MarshalJSON() ([]byte, error) { return []byte(`"` + redactedSentinel + `"`), nil }

// MarshalText redacts; encoding/text + most TOML/YAML libs land here.
func (v Value) MarshalText() ([]byte, error) { return []byte(redactedSentinel), nil }

// LogValue redacts; slog Text+JSON handlers call this instead of
// reflecting into the struct.
func (v Value) LogValue() slog.Value { return slog.StringValue(redactedSentinel) }

// Format catches the fmt.Formatter path (%+v on some types); always
// redacts.
func (v Value) Format(f fmt.State, verb rune) {
	_, _ = f.Write([]byte(redactedSentinel))
}
