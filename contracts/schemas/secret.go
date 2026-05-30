package schemas

// Secret wraps key material so accidental logging or JSON-encoding
// redacts to "[REDACTED]" instead of leaking the raw bytes. Use as
// the env-to-process boundary type; convert to []byte only at the
// HMAC seam. Stringer, GoStringer, json.Marshaler, and
// encoding.TextMarshaler are all implemented to enforce redaction
// on every formatter Go ships with.
type Secret []byte

const redactedLiteral = "[REDACTED]"

// Bytes is the only path to the raw key material; call at the HMAC seam.
func (s Secret) Bytes() []byte { return []byte(s) }

// String satisfies fmt.Stringer; redacted.
func (s Secret) String() string { return redactedLiteral }

// GoString satisfies fmt.GoStringer; redacted under %#v.
func (s Secret) GoString() string { return redactedLiteral }

// MarshalJSON satisfies json.Marshaler; redacted.
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redactedLiteral + `"`), nil
}

// MarshalText satisfies encoding.TextMarshaler; redacted.
func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redactedLiteral), nil
}
