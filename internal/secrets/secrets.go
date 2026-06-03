// Package secrets fetches operator credentials from an OS-resident
// store (macOS Keychain, Linux pass) with env-var fallback, so
// `regatta serve` does not block on a human typing tokens on every
// supervisor wake. See spec
// docs/engineer/specs/2026-06-02-phase-autonomy-w6-secret-credential-fetch.md.
//
// Value is a redaction wrapper with zero exported fields — reflection-
// based formatters (fmt %v/%+v, encoding/json, slog Text+JSON handlers)
// cannot reach the underlying bytes. Bytes() is the only legitimate
// consumer; call-site grep is the audit gate.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
)

// ErrNotFound signals a clean miss so chains can distinguish missing-
// key from a real adapter error. Anything other than ErrNotFound from
// an adapter aborts the chain.
var ErrNotFound = errors.New("secret not found")

// ErrUnsupported is returned by adapters whose backend is unavailable
// on the current platform (e.g. keychain on Linux). Treated the same
// as ErrNotFound for chain-traversal purposes.
var ErrUnsupported = errors.New("secret backend unsupported on this platform")

// Canonical key names. Library + CLI entry validates against keyRe;
// unknown keys reject at the boundary (no path-traversal via `pass
// show ../etc/passwd`).
const (
	KeyAnthropic    = "regatta.anthropic_api_key"
	KeyGHToken      = "regatta.gh_token"
	KeyBriefHMACs   = "regatta.brief_hmac_keys"
	KeyAuditHMACKey = "regatta.audit_hmac_key"
)

// CanonicalKeys is the boot-time fetch set. Order is informational
// only — adapters are independent and order does not imply priority.
var CanonicalKeys = []string{KeyAnthropic, KeyGHToken, KeyBriefHMACs, KeyAuditHMACKey}

// redactedSentinel is the canonical replacement string for any code
// path that formats a Value — single source of truth so test asserts
// and prod output cannot drift.
const redactedSentinel = "<redacted>"

// sourceMissing tags cache rows whose chain returned ErrNotFound, so
// diagnostics distinguish "fetched-empty" from "never-resolved".
const sourceMissing = "missing"

// adapterEnv / adapterKeychain / adapterPass are the public Name()
// values for the three adapters; exporting via const so audit-event
// consumers can match exactly.
const (
	AdapterEnv      = "env"
	AdapterKeychain = "keychain"
	AdapterPass     = "pass"
)

// keyRe pins the canonical key shape; path-traversal via key name (R12)
// is structurally prevented.
var keyRe = regexp.MustCompile(`^regatta\.[a-z0-9_]+$`)

// ValidateKey returns nil iff key matches the canonical shape.
func ValidateKey(key string) error {
	if !keyRe.MatchString(key) {
		return fmt.Errorf("invalid secret key %q (must match %s)", key, keyRe.String())
	}
	return nil
}

// Fetcher reads a secret by canonical key. Absent secret returns
// ErrNotFound (chains can distinguish missing-key from empty-value).
// Name identifies the adapter for diagnostics + audit.
type Fetcher interface {
	Get(ctx context.Context, key string) (Value, error)
	Name() string
}

// Value wraps a secret byte slice. The struct has ZERO exported fields
// — reflection-based formatters cannot reach the underlying bytes.
type Value struct {
	b []byte
}

// NewValue wraps raw bytes. Copies the slice so caller mutation does
// not bleed into the cache.
func NewValue(b []byte) Value {
	if len(b) == 0 {
		return Value{}
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return Value{b: cp}
}

// Bytes is the only legitimate accessor. Grep-gated to the small
// allowlist of call sites that actually hand the secret to an
// external API (HTTP header, HMAC signer).
func (v Value) Bytes() []byte { return v.b }

// Len reports the secret length without exposing bytes — safe for
// presence checks in diagnostics.
func (v Value) Len() int { return len(v.b) }

// String redacts. fmt.Sprintf("%v", v) and "%s" both land here.
func (v Value) String() string { return redactedSentinel }

// GoString redacts. fmt.Sprintf("%#v", v) lands here.
func (v Value) GoString() string { return redactedSentinel }

// MarshalJSON redacts. json.Marshal lands here.
func (v Value) MarshalJSON() ([]byte, error) { return []byte(`"` + redactedSentinel + `"`), nil }

// MarshalText redacts. encoding/text + most TOML/YAML libs land here.
func (v Value) MarshalText() ([]byte, error) { return []byte(redactedSentinel), nil }

// LogValue redacts. slog Text+JSON handlers call this instead of
// reflecting into the struct.
func (v Value) LogValue() slog.Value { return slog.StringValue(redactedSentinel) }

// Format catches the fmt.Formatter path (rare, but %+v can dispatch
// here on some types). Always redacts.
func (v Value) Format(f fmt.State, verb rune) {
	_, _ = f.Write([]byte(redactedSentinel))
}
